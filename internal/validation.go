// Package internal provides shared configuration validation helpers.
package internal

import (
	"fmt"
	"strings"
)

func validateTicketModeConfig(config AgentConfig) error {
	workProvider := strings.ToLower(strings.TrimSpace(config.WorkProvider))
	if workProvider == "" {
		return fmt.Errorf("work_provider is required")
	}

	if workProvider != "jira" {
		return fmt.Errorf("unsupported work_provider %q: only jira is supported", config.WorkProvider)
	}

	if err := validateRequiredJiraFields(config.Jira); err != nil {
		return fmt.Errorf("jira config: %w", err)
	}

	if err := validateSharedSCMConfig(config); err != nil {
		return fmt.Errorf("scm config: %w", err)
	}

	if err := validateCodingAgentConfig(config); err != nil {
		return fmt.Errorf("coding agent config: %w", err)
	}

	return nil
}

func validatePullRequestModeConfig(config AgentConfig) error {
	if err := validateSharedSCMConfig(config); err != nil {
		return fmt.Errorf("scm config: %w", err)
	}

	if err := validateCodingAgentConfig(config); err != nil {
		return fmt.Errorf("coding agent config: %w", err)
	}

	return nil
}

func validateCodingAgentConfig(config AgentConfig) error {
	provider := strings.ToLower(strings.TrimSpace(config.CodingAgentProvider))

	switch provider {
	case "", codingAgentProviderOpencode:
		return nil
	case codingAgentProviderCursor:
		if _, _, err := parseCLICommand(config.CursorCLICommand, "cursor-agent"); err != nil {
			return fmt.Errorf("invalid cursor_cli_command: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("unsupported coding_agent_provider %q: expected opencode or cursor", config.CodingAgentProvider)
	}
}

func validateSharedSCMConfig(config AgentConfig) error {
	requiredFields := []struct {
		name  string
		value string
	}{
		{name: "git_base_branch", value: config.GitBaseBranch},
		{name: "git_remote", value: config.GitRemote},
	}

	for _, field := range requiredFields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}

	scmProvider := strings.ToLower(strings.TrimSpace(config.SCMProvider))

	switch scmProvider {
	case "", "gitea":
		requiredGiteaFields := []struct {
			name  string
			value string
		}{
			{name: "gitea_base_url", value: config.GiteaBaseURL},
			{name: "gitea_token", value: config.GiteaToken},
			{name: "gitea_owner", value: config.GiteaOwner},
			{name: "git_repo", value: config.GitRepo},
		}

		for _, field := range requiredGiteaFields {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%s is required for gitea scm_provider", field.name)
			}
		}
	case "gitlab":
		requiredGitLabFields := []struct {
			name  string
			value string
		}{
			{name: "gitlab_base_url", value: config.GitlabBaseURL},
			{name: "gitlab_token", value: config.GitlabToken},
			{name: "gitlab_project_path", value: config.GitlabProjectPath},
		}

		for _, field := range requiredGitLabFields {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%s is required for gitlab scm_provider", field.name)
			}
		}
	case "github":
		requiredGitHubFields := []struct {
			name  string
			value string
		}{
			{name: "github_token", value: config.GithubToken},
			{name: "github_owner", value: config.GithubOwner},
			{name: "git_repo", value: config.GitRepo},
		}

		for _, field := range requiredGitHubFields {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%s is required for github scm_provider", field.name)
			}
		}
	default:
		return fmt.Errorf("unsupported scm_provider %q: expected gitea, gitlab, or github", config.SCMProvider)
	}

	return nil
}

func validateRequiredJiraFields(jira *JiraConfig) error {
	if jira == nil {
		return fmt.Errorf("jira block is required")
	}

	requiredFields := []struct {
		name  string
		value string
	}{
		{name: "jira.base_url", value: jira.BaseURL},
		{name: "jira.email", value: jira.Email},
		{name: "jira.api_token", value: jira.APIToken},
		{name: "jira.jql", value: jira.JQL},
		{name: "jira.done_status", value: jira.DoneStatus},
	}

	for _, field := range requiredFields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}

	return nil
}
