// Package internal contains the core agent workflow and integrations.
package internal

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type AgentConfig struct {
	JiraEmail      string `yaml:"JIRA_EMAIL"`
	JiraJQL        string `yaml:"JIRA_JQL"`
	JiraAPIToken   string `yaml:"JIRA_API_TOKEN"`
	JiraBaseURL    string `yaml:"JIRA_BASE_URL"`
	JiraMaxResults int    `yaml:"JIRA_MAX_RESULTS"`

	MaxTries int `yaml:"MAX_TRIES"`

	GiteaRepo       string `yaml:"GITEA_REPO"`
	GiteaOwner      string `yaml:"GITEA_OWNER"`
	GiteaToken      string `yaml:"GITEA_TOKEN"`
	GiteaBaseBranch string `yaml:"GITEA_BASE_BRANCH"`
	GiteaRemote     string `yaml:"GITEA_REMOTE"`
	GiteaBaseURL    string `yaml:"GITEA_BASE_URL"`
}

func RunAgent(config AgentConfig) error {
	httpClient := &http.Client{Timeout: 20 * time.Second}
	cache := &ResultCache{}
	tries := 0

	for {
		if tries >= config.MaxTries {
			log.Printf("Reached maximum number of tries (%d). Exiting.", config.MaxTries)
			break
		}

		if cache.IsEmpty() {
			tries++

			issues, err := SearchIssues(context.Background(), httpClient, config.JiraBaseURL, config.JiraEmail, config.JiraAPIToken, JiraSearchRequest{
				JQL:        config.JiraJQL,
				MaxResults: config.JiraMaxResults,
				Fields:     []string{"key", "summary", "status", "assignee", "priority", "description"},
			})
			if err != nil {
				return fmt.Errorf("search issues: %w", err)
			}

			if len(issues) == 0 {
				fmt.Println("No issues found.")
				continue
			}

			cache.Fill(issues)

			fmt.Printf("Found %d issues:\n", len(issues))
		}

		issue, ok := cache.Next()
		if !ok {
			fmt.Println("No more issues in cache.")

			continue
		}

		fmt.Printf("- Working on: %s [%s] %s\n", issue.Key, issue.Fields.Status.Name, issue.Fields.Summary)

		if shouldSkipIssue(issue, cache) {
			continue
		}

		if err := SyncBaseBranch(config.GiteaRemote, config.GiteaBaseBranch); err != nil {
			return fmt.Errorf("sync base branch for issue %s: %w", issue.Key, err)
		}

		err := CheckoutOrCreateBranch(issue.Key, config.GiteaBaseBranch)
		if err != nil {
			return fmt.Errorf("checkout/create branch for issue %s: %w", issue.Key, err)
		}

		fmt.Printf("Changed branch to %s\n", issue.Key)

		input := fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			"do not interact with GIT directly, do not ask for human input.",
			issue.Fields.Summary,
			issue.Fields.Description.PlainText(),
		)

		fmt.Printf("Running opencode for issue %s with input:\n%s\n", issue.Key, input)

		opencodeOutputBytes, err := exec.Command("opencode", "run", input).CombinedOutput()
		if err != nil {
			opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
			return fmt.Errorf("execute opencode for issue %s: %w (output: %s)", issue.Key, err, opencodeOutput)
		}

		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))

		fmt.Printf("Executed opencode for issue %s\n", issue.Key)

		if err := StageAndCommitChanges(issue.Key, issue.Fields.Summary); err != nil {
			return fmt.Errorf("stage/commit changes for issue %s: %w", issue.Key, err)
		}

		if err := PushBranch(issue.Key, config.GiteaRemote); err != nil {
			return fmt.Errorf("push branch for issue %s: %w", issue.Key, err)
		}

		fmt.Printf("Pushed branch %s to remote %s\n", issue.Key, config.GiteaRemote)

		existingPR, err := FindOpenPullRequest(
			context.Background(),
			httpClient,
			config.GiteaBaseURL,
			config.GiteaToken,
			config.GiteaOwner,
			config.GiteaRepo,
			issue.Key,
			config.GiteaBaseBranch,
		)
		if err != nil {
			return fmt.Errorf("check existing merge request for issue %s: %w", issue.Key, err)
		}

		if existingPR != nil {
			fmt.Printf("Merge request already exists #%d: %s. Skipping creation.\n", existingPR.Number, existingPR.HTMLURL)
		} else {
			pr, err := CreateGiteaMergeRequest(
				context.Background(),
				httpClient,
				config.GiteaBaseURL,
				config.GiteaToken,
				config.GiteaOwner,
				config.GiteaRepo,
				GiteaPullRequestRequest{
					Title: fmt.Sprintf("%s: %s", issue.Key, issue.Fields.Summary),
					Body:  formatPullRequestBody(issue.Key, opencodeOutput),
					Head:  issue.Key,
					Base:  config.GiteaBaseBranch,
				},
			)
			if err != nil {
				return fmt.Errorf("create merge request for issue %s: %w", issue.Key, err)
			}

			fmt.Printf("Created merge request #%d: %s\n", pr.Number, pr.HTMLURL)
		}

		fmt.Printf("Changing branch back to main\n")

		err = CheckoutBranch("main")
		if err != nil {
			return fmt.Errorf("change back to main branch after issue %s: %w", issue.Key, err)
		}
	}

	return nil
}

func shouldSkipIssue(issue JiraIssue, cache *ResultCache) bool {
	body := issue.Fields.Description.PlainText()
	lowerBody := strings.ToLower(body)

	if body == "" {
		log.Printf("Issue %s has no description. Skipping.", issue.Key)
		cache.Delete(issue.Key)

		return true
	}

	if strings.Contains(lowerBody, "human") {
		log.Printf("Issue %s requires human intervention. Skipping.", issue.Key)
		cache.Delete(issue.Key)

		return true
	}

	if strings.Contains(lowerBody, "blocked") {
		log.Printf("Issue %s is blocked. Skipping.", issue.Key)
		cache.Delete(issue.Key)

		return true
	}

	if !strings.Contains(lowerBody, "tester") {
		log.Printf("Issue %s does not mention tester. Skipping.", issue.Key)
		cache.Delete(issue.Key)

		return true
	}

	return false
}

func formatPullRequestBody(issueKey, opencodeOutput string) string {
	output := strings.TrimSpace(opencodeOutput)
	if output == "" {
		output = "(no output)"
	}

	const maxOutputLen = 8000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n\n[truncated]"
	}

	return fmt.Sprintf(
		"Automated merge request for %s\n\n## Opencode output\n\n```text\n%s\n```",
		issueKey,
		output,
	)
}
