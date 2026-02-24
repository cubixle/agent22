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
	}
}
