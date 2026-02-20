// Package internal contains the core agent workflow and integrations.
package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type AgentConfig struct {
	WorkProvider string `yaml:"work_provider"`

	Jira *JiraConfig `yaml:"jira"`

	WaitTimeSeconds int `yaml:"wait_time_seconds"`

	GitRepo       string `yaml:"git_repo"`
	GitBaseBranch string `yaml:"git_base_branch"`
	GitRemote     string `yaml:"git_remote"`

	GiteaOwner   string `yaml:"gitea_owner"`
	GiteaToken   string `yaml:"gitea_token"`
	GiteaBaseURL string `yaml:"gitea_base_url"`
}

func RunAgent(config AgentConfig) error {
	if err := validateTicketModeConfig(config); err != nil {
		return fmt.Errorf("validate ticket mode config: %w", err)
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}

	var tracker IssueProvider = NewJiraClient(httpClient, config.Jira)

	var scm PullRequestProvider = NewGiteaClient(httpClient, config.GiteaBaseURL, config.GiteaToken, config.GiteaOwner, config.GitRepo)

	cache := &ResultCache{}

	return runPollingEventLoop(pollingEventLoopConfig{
		Name:        "jira issue",
		BaseWait:    time.Duration(config.WaitTimeSeconds) * time.Second,
		MaxIdleWait: 10 * time.Minute,
	}, func(ctx context.Context) (bool, error) {
		return processIssueTick(ctx, config, tracker, scm, cache)
	})
}

func processIssueTick(
	ctx context.Context,
	config AgentConfig,
	tracker IssueProvider,
	scm PullRequestProvider,
	cache *ResultCache,
) (bool, error) {
	if cache.IsEmpty() {
		issues, err := tracker.SearchIssues(ctx)
		if err != nil {
			return false, fmt.Errorf("search issues: %w", err)
		}

		if len(issues) == 0 {
			slog.Debug("No issues found")
			return false, nil
		}

		cache.Fill(issues)

		outputInfof("Found %d issues", len(issues))
	}

	issue, ok := cache.Next()
	if !ok {
		outputInfof("No more issues in cache")
		return false, nil
	}

	outputStepf("Working on: %s [%s] %s", issue.Key, issue.Status, issue.Summary)

	if shouldSkipIssue(issue, cache) {
		return true, nil
	}

	if err := processIssue(ctx, config, tracker, scm, cache, issue); err != nil {
		return false, err
	}

	return true, nil
}

func processIssue(
	ctx context.Context,
	config AgentConfig,
	tracker IssueProvider,
	scm PullRequestProvider,
	cache *ResultCache,
	issue Issue,
) error {
	if err := SyncBaseBranch(config.GitRemote, config.GitBaseBranch); err != nil {
		return fmt.Errorf("sync base branch for issue %s: %w", issue.Key, err)
	}

	err := CheckoutOrCreateBranch(issue.Key, config.GitBaseBranch)
	if err != nil {
		return fmt.Errorf("checkout/create branch for issue %s: %w", issue.Key, err)
	}

	defer func() {
		if err := CheckoutBranch(config.GitBaseBranch); err != nil {
			slog.Error("Restore base branch after issue processing", "base_branch", config.GitBaseBranch, "issue", issue.Key, "error", err)
		}
	}()

	slog.Debug("Changed branch\n", "branch name", issue.Key)

	input := buildIssueOpencodeInput(issue)
	logPayloadMetadata("opencode task input", issue.Key, input)

	outputStepf("Running opencode for issue %s", issue.Key)

	opencodeOutputBytes, err := runOpencodeWithProgress(issue.Key, input)
	if err != nil {
		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
		return fmt.Errorf("execute opencode for issue %s: %w (output_chars=%d)", issue.Key, err, payloadCharCount(opencodeOutput))
	}

	opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
	logPayloadMetadata("opencode task output", issue.Key, opencodeOutput)
	outputInfof("Opencode output metadata for issue %s: chars=%d", issue.Key, payloadCharCount(opencodeOutput))

	outputSuccessf("Executed opencode for issue %s", issue.Key)

	reviewInput := buildIssuePostImplementationReviewInput(issue)
	logPayloadMetadata("post-implementation review input", issue.Key, reviewInput)
	outputStepf("Running post-implementation review for issue %s", issue.Key)

	reviewOutputBytes, err := runOpencodeWithProgress(issue.Key+"-review", reviewInput)
	if err != nil {
		reviewOutput := strings.TrimSpace(string(reviewOutputBytes))
		return fmt.Errorf("review changes for issue %s: %w (output_chars=%d)", issue.Key, err, payloadCharCount(reviewOutput))
	}

	reviewOutput := strings.TrimSpace(string(reviewOutputBytes))
	logPayloadMetadata("post-implementation review output", issue.Key, reviewOutput)
	outputInfof("Post-implementation review output metadata for issue %s: chars=%d", issue.Key, payloadCharCount(reviewOutput))

	applyReviewInput := buildIssueApplyReviewInput(issue, reviewOutput)
	logPayloadMetadata("apply-review input", issue.Key, applyReviewInput)
	outputStepf("Applying review feedback for issue %s", issue.Key)

	applyReviewOutputBytes, err := runOpencodeWithProgress(issue.Key+"-apply-review", applyReviewInput)
	if err != nil {
		applyReviewOutput := strings.TrimSpace(string(applyReviewOutputBytes))
		return fmt.Errorf("apply review feedback for issue %s: %w (output_chars=%d)", issue.Key, err, payloadCharCount(applyReviewOutput))
	}

	applyReviewOutput := strings.TrimSpace(string(applyReviewOutputBytes))
	logPayloadMetadata("apply-review output", issue.Key, applyReviewOutput)
	outputInfof("Applied review feedback output metadata for issue %s: chars=%d", issue.Key, payloadCharCount(applyReviewOutput))

	if err := StageAndCommitChanges(issue.Key, issue.Summary); err != nil {
		return fmt.Errorf("stage/commit changes for issue %s: %w", issue.Key, err)
	}

	if err := PushBranch(issue.Key, config.GitRemote); err != nil {
		return fmt.Errorf("push branch for issue %s: %w", issue.Key, err)
	}

	outputSuccessf("Pushed branch %s to remote %s", issue.Key, config.GitRemote)

	existingPR, err := scm.FindOpenPullRequest(ctx, issue.Key, config.GitBaseBranch)
	if err != nil {
		return fmt.Errorf("check existing merge request for issue %s: %w", issue.Key, err)
	}

	if existingPR != nil {
		outputWarnf("Merge request already exists #%d: %s. Skipping creation.", existingPR.Number, existingPR.HTMLURL)
	} else {
		pr, err := scm.CreatePullRequest(ctx, PullRequestCreateInput{
			Title: fmt.Sprintf("%s: %s", issue.Key, issue.Summary),
			Body:  formatPullRequestBody(issue.Key, issue.Summary, opencodeOutput),
			Head:  issue.Key,
			Base:  config.GitBaseBranch,
		})
		if err != nil {
			return fmt.Errorf("create merge request for issue %s: %w", issue.Key, err)
		}

		outputSuccessf("Created merge request #%d: %s", pr.Number, pr.HTMLURL)
	}

	if err := tracker.MarkIssueDone(ctx, issue.Key); err != nil {
		return fmt.Errorf("transition issue %s to done: %w", issue.Key, err)
	}

	outputSuccessf("Updated issue %s status to done", issue.Key)
	cache.Delete(issue.Key)

	return nil
}

func shouldSkipIssue(issue Issue, cache *ResultCache) bool {
	body := issue.Description
	lowerBody := strings.ToLower(body)

	if body == "" {
		return skipIssue(cache, issue.Key, "has no description")
	}

	if strings.Contains(lowerBody, "human") {
		return skipIssue(cache, issue.Key, "requires human intervention")
	}

	if strings.Contains(lowerBody, "blocked") {
		return skipIssue(cache, issue.Key, "is blocked")
	}

	return false
}

func skipIssue(cache *ResultCache, issueKey, reason string) bool {
	slog.Info("Skipping issue", "issue", issueKey, "reason", reason)
	cache.Delete(issueKey)

	return true
}

func formatPullRequestBody(issueKey, issueSummary, opencodeOutput string) string {
	output := strings.TrimSpace(opencodeOutput)
	if output == "" {
		output = "(no output)"
	}

	const maxOutputLen = 8000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n\n[truncated]"
	}

	return fmt.Sprintf(
		"Automated merge request for %s\n\n## Description\n%s\n\n## Opencode output\n\n```text\n%s\n```",
		issueKey,
		issueSummary,
		output,
	)
}

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

func logPayloadMetadata(label, issueKey, payload string) {
	slog.Debug(label, "issue", issueKey, "chars", payloadCharCount(payload))
}

func payloadCharCount(payload string) int {
	return utf8.RuneCountInString(strings.TrimSpace(payload))
}
