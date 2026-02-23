// Package internal defines provider interfaces and default implementations.
package internal

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type IssueProvider interface {
	SearchIssues(ctx context.Context) ([]Issue, error)
	MarkIssueInProgress(ctx context.Context, issueKey string) error
	MarkIssueDone(ctx context.Context, issueKey string) error
}

type PullRequestProvider interface {
	FindOpenPullRequest(ctx context.Context, headBranch, baseBranch string) (*PullRequest, error)
	CreatePullRequest(ctx context.Context, payload PullRequestCreateInput) (PullRequest, error)
	ListOpenPullRequests(ctx context.Context) ([]PullRequest, error)
	ListPullRequestComments(ctx context.Context, pullNumber int) ([]PullRequestComment, error)
	CreatePullRequestComment(ctx context.Context, pullNumber int, body string) error
}

func NewPullRequestProvider(httpClient *http.Client, config AgentConfig) (PullRequestProvider, error) {
	scmProvider := strings.ToLower(strings.TrimSpace(config.SCMProvider))

	switch scmProvider {
	case "", "gitea":
		if strings.TrimSpace(config.GiteaBaseURL) == "" {
			return nil, fmt.Errorf("gitea_base_url is required for gitea SCM provider")
		}

		if strings.TrimSpace(config.GiteaToken) == "" {
			return nil, fmt.Errorf("gitea_token is required for gitea SCM provider")
		}

		if strings.TrimSpace(config.GiteaOwner) == "" {
			return nil, fmt.Errorf("gitea_owner is required for gitea SCM provider")
		}

		if strings.TrimSpace(config.GitRepo) == "" {
			return nil, fmt.Errorf("git_repo is required for gitea SCM provider")
		}

		return NewGiteaClient(httpClient, config.GiteaBaseURL, config.GiteaToken, config.GiteaOwner, config.GitRepo), nil
	case "gitlab":
		if strings.TrimSpace(config.GitlabBaseURL) == "" {
			return nil, fmt.Errorf("gitlab_base_url is required for gitlab SCM provider")
		}

		if strings.TrimSpace(config.GitlabToken) == "" {
			return nil, fmt.Errorf("gitlab_token is required for gitlab SCM provider")
		}

		if strings.TrimSpace(config.GitlabProjectPath) == "" {
			return nil, fmt.Errorf("gitlab_project_path is required for gitlab SCM provider")
		}

		return NewGitLabClient(httpClient, config.GitlabBaseURL, config.GitlabToken, config.GitlabProjectPath), nil
	default:
		return nil, fmt.Errorf("unsupported scm_provider %q: expected gitea or gitlab", config.SCMProvider)
	}
}
