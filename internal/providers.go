// Package internal defines provider interfaces and default implementations.
package internal

import (
	"context"
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
