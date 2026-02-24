// Package internal_test verifies configuration validation behavior.
package internal_test

import (
	"strings"
	"testing"

	agentinternal "gitea.lan/cubixle/agent/internal"
)

func TestRunModeConfigValidationUsesSharedSCMRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mutate        func(cfg *agentinternal.AgentConfig)
		expectedError string
	}{
		{
			name: "missing git remote",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.GitRemote = ""
			},
			expectedError: "scm config: git_remote is required",
		},
		{
			name: "missing github owner",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.SCMProvider = "github"
				cfg.GithubOwner = ""
			},
			expectedError: "scm config: github_owner is required for github scm_provider",
		},
		{
			name: "unsupported coding agent provider",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.CodingAgentProvider = "bad"
			},
			expectedError: "coding agent config: unsupported coding_agent_provider \"bad\": expected opencode or cursor",
		},
		{
			name: "invalid cursor command format",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.CodingAgentProvider = "cursor"
				cfg.CursorCLICommand = "cursor-agent \"broken"
			},
			expectedError: "coding agent config: invalid cursor_cli_command: command contains an unclosed quote",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validAgentConfig()
			tc.mutate(&cfg)

			ticketErr := agentinternal.RunAgent(cfg)
			if ticketErr == nil || !strings.Contains(ticketErr.Error(), tc.expectedError) {
				t.Fatalf("RunAgent error = %v, want contains %q", ticketErr, tc.expectedError)
			}

			pullRequestErr := agentinternal.RunPullRequestMode(cfg)
			if pullRequestErr == nil || !strings.Contains(pullRequestErr.Error(), tc.expectedError) {
				t.Fatalf("RunPullRequestMode error = %v, want contains %q", pullRequestErr, tc.expectedError)
			}
		})
	}
}

func TestRunAgentConfigValidationGitHubWorkProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mutate        func(cfg *agentinternal.AgentConfig)
		expectedError string
	}{
		{
			name: "missing github work block",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GitHubWork = nil
			},
			expectedError: "github config: github_work block is required",
		},
		{
			name: "missing github done label",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GitHubWork.DoneLabel = ""
			},
			expectedError: "github config: github_work.done_label is required",
		},
		{
			name: "missing github selection labels",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GitHubWork.Labels = nil
			},
			expectedError: "github config: github_work.labels must contain at least one label",
		},
		{
			name: "missing github owner in ticket mode",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GithubOwner = ""
			},
			expectedError: "github config: github_owner is required",
		},
		{
			name: "missing github token in ticket mode",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GithubToken = ""
			},
			expectedError: "github config: github_token is required",
		},
		{
			name: "missing git repo in ticket mode",
			mutate: func(cfg *agentinternal.AgentConfig) {
				cfg.WorkProvider = "github"
				cfg.GitRepo = ""
			},
			expectedError: "github config: git_repo is required",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validAgentConfig()
			tc.mutate(&cfg)

			err := agentinternal.RunAgent(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("RunAgent error = %v, want contains %q", err, tc.expectedError)
			}
		})
	}
}

func validAgentConfig() agentinternal.AgentConfig {
	return agentinternal.AgentConfig{
		WorkProvider: "jira",
		SCMProvider:  "gitea",
		Jira: &agentinternal.JiraConfig{
			BaseURL:    "https://jira.example.com",
			Email:      "bot@example.com",
			APIToken:   "token",
			JQL:        "project = AG",
			DoneStatus: "Done",
		},
		WaitTimeSeconds:   1,
		GitRepo:           "repo",
		GitBaseBranch:     "main",
		GitRemote:         "origin",
		GiteaOwner:        "owner",
		GiteaToken:        "token",
		GiteaBaseURL:      "https://gitea.example.com",
		GitlabToken:       "token",
		GitlabBaseURL:     "https://gitlab.example.com",
		GitlabProjectPath: "group/repo",
		GithubOwner:       "owner",
		GithubToken:       "token",
		GithubBaseURL:     "https://api.github.com",
		GitHubWork: &agentinternal.GitHubWorkConfig{
			Labels:    []string{"ready"},
			DoneLabel: "done",
		},
	}
}
