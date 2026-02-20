// Package internal contains the core agent workflow and integrations.
package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
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
	httpClient := &http.Client{Timeout: 20 * time.Second}

	var tracker IssueProvider
	if config.WorkProvider == "jira" {
		tracker = NewJiraClient(httpClient, config.Jira)
	}

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

	taskBox := formatASCIIBox(fmt.Sprintf("task for %s", issue.Key), input)
	slog.Debug("opencode input", "issue", issue.Key, "task", taskBox)

	outputStepf("Running opencode for issue %s", issue.Key)

	opencodeOutputBytes, err := runOpencodeWithProgress(issue.Key, input)
	if err != nil {
		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
		return fmt.Errorf("execute opencode for issue %s: %w (output: %s)", issue.Key, err, opencodeOutput)
	}

	opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))

	outputInfof("Opencode output for issue %s:\n%s", issue.Key, opencodeOutput)

	outputSuccessf("Executed opencode for issue %s", issue.Key)

	reviewInput := buildIssuePostImplementationReviewInput(issue)
	slog.Debug("issue post-implementation review input", "issue", issue.Key, "input", reviewInput)
	outputStepf("Running post-implementation review for issue %s", issue.Key)

	reviewOutputBytes, err := runOpencodeWithProgress(issue.Key+"-review", reviewInput)
	if err != nil {
		reviewOutput := strings.TrimSpace(string(reviewOutputBytes))
		return fmt.Errorf("review changes for issue %s: %w (output: %s)", issue.Key, err, reviewOutput)
	}

	reviewOutput := strings.TrimSpace(string(reviewOutputBytes))
	outputInfof("Post-implementation review output for issue %s:\n%s", issue.Key, reviewOutput)

	applyReviewInput := buildIssueApplyReviewInput(issue, reviewOutput)
	slog.Debug("issue apply-review input", "issue", issue.Key, "input", applyReviewInput)
	outputStepf("Applying review feedback for issue %s", issue.Key)

	applyReviewOutputBytes, err := runOpencodeWithProgress(issue.Key+"-apply-review", applyReviewInput)
	if err != nil {
		applyReviewOutput := strings.TrimSpace(string(applyReviewOutputBytes))
		return fmt.Errorf("apply review feedback for issue %s: %w (output: %s)", issue.Key, err, applyReviewOutput)
	}

	applyReviewOutput := strings.TrimSpace(string(applyReviewOutputBytes))
	outputInfof("Applied review feedback output for issue %s:\n%s", issue.Key, applyReviewOutput)

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

func formatASCIIBox(title, body string) string {
	normalizedBody := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalizedBody, "\n")

	trimmedTitle := strings.TrimSpace(title)
	maxWidth := len(trimmedTitle)

	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	if maxWidth == 0 {
		maxWidth = 1
	}

	border := "+" + strings.Repeat("-", maxWidth+2) + "+"

	var b strings.Builder
	b.WriteString(border)
	b.WriteString("\n")

	if trimmedTitle != "" {
		b.WriteString(fmt.Sprintf("| %-*s |\n", maxWidth, trimmedTitle))
		b.WriteString(border)
		b.WriteString("\n")
	}

	for _, line := range lines {
		b.WriteString(fmt.Sprintf("| %-*s |\n", maxWidth, line))
	}

	b.WriteString(border)

	return b.String()
}
