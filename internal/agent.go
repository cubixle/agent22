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
	WorkProvider        string `yaml:"work_provider"`
	SCMProvider         string `yaml:"scm_provider"`
	CodingAgentProvider string `yaml:"coding_agent_provider"`

	Jira *JiraConfig `yaml:"jira"`

	WaitTimeSeconds int `yaml:"wait_time_seconds"`

	GitRepo       string `yaml:"git_repo"`
	GitBaseBranch string `yaml:"git_base_branch"`
	GitRemote     string `yaml:"git_remote"`

	GiteaOwner   string `yaml:"gitea_owner"`
	GiteaToken   string `yaml:"gitea_token"`
	GiteaBaseURL string `yaml:"gitea_base_url"`

	GitlabToken       string `yaml:"gitlab_token"`
	GitlabBaseURL     string `yaml:"gitlab_base_url"`
	GitlabProjectPath string `yaml:"gitlab_project_path"`

	GithubOwner   string `yaml:"github_owner"`
	GithubToken   string `yaml:"github_token"`
	GithubBaseURL string `yaml:"github_base_url"`

	CursorCLICommand string `yaml:"cursor_cli_command"`
}

func RunAgent(config AgentConfig) error {
	if err := validateTicketModeConfig(config); err != nil {
		return fmt.Errorf("validate ticket mode config: %w", err)
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}

	var tracker IssueProvider = NewJiraClient(httpClient, config.Jira)

	scm, err := NewPullRequestProvider(httpClient, config)
	if err != nil {
		return fmt.Errorf("build pull request provider: %w", err)
	}

	codingAgent, err := NewCodingAgentProvider(config)
	if err != nil {
		return fmt.Errorf("build coding agent provider: %w", err)
	}

	cache := &ResultCache{}

	return runPollingEventLoop(pollingEventLoopConfig{
		Name:        "jira issue",
		BaseWait:    time.Duration(config.WaitTimeSeconds) * time.Second,
		MaxIdleWait: 10 * time.Minute,
	}, func(ctx context.Context) (bool, error) {
		return processIssueTick(ctx, config, tracker, scm, codingAgent, cache)
	})
}

func processIssueTick(
	ctx context.Context,
	config AgentConfig,
	tracker IssueProvider,
	scm PullRequestProvider,
	codingAgent CodingAgentProvider,
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

	if err := processIssue(ctx, config, tracker, scm, codingAgent, cache, issue); err != nil {
		return false, err
	}

	return true, nil
}

func processIssue(
	ctx context.Context,
	config AgentConfig,
	tracker IssueProvider,
	scm PullRequestProvider,
	codingAgent CodingAgentProvider,
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

	input := buildIssueCodingAgentInput(issue)
	logPayloadMetadata(codingAgent.DisplayName()+" task input", issue.Key, input)

	outputStepf("Running %s for issue %s", codingAgent.DisplayName(), issue.Key)

	codingAgentOutputBytes, err := codingAgent.RunWithProgress(issue.Key, input)
	if err != nil {
		codingAgentOutput := strings.TrimSpace(string(codingAgentOutputBytes))
		return fmt.Errorf("execute %s for issue %s: %w (output_chars=%d)", codingAgent.Name(), issue.Key, err, payloadCharCount(codingAgentOutput))
	}

	codingAgentOutput := strings.TrimSpace(string(codingAgentOutputBytes))
	logPayloadMetadata(codingAgent.DisplayName()+" task output", issue.Key, codingAgentOutput)
	outputInfof("%s output metadata for issue %s: chars=%d", codingAgent.DisplayName(), issue.Key, payloadCharCount(codingAgentOutput))

	outputSuccessf("Executed %s for issue %s", codingAgent.DisplayName(), issue.Key)

	reviewInput := buildIssuePostImplementationReviewInput(issue)
	logPayloadMetadata("post-implementation review input", issue.Key, reviewInput)
	outputStepf("Running post-implementation review for issue %s", issue.Key)

	reviewOutputBytes, err := codingAgent.RunWithProgress(issue.Key+"-review", reviewInput)
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

	applyReviewOutputBytes, err := codingAgent.RunWithProgress(issue.Key+"-apply-review", applyReviewInput)
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
			Body:  formatPullRequestBody(issue.Key, issue.Summary, codingAgentOutput, codingAgent.DisplayName()),
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

func formatPullRequestBody(issueKey, issueSummary, codingAgentOutput, codingAgentName string) string {
	output := strings.TrimSpace(codingAgentOutput)
	if output == "" {
		output = "(no output)"
	}

	const maxOutputLen = 8000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n\n[truncated]"
	}

	return fmt.Sprintf(
		"Automated merge request for %s\n\n## Description\n%s\n\n## %s output\n\n```text\n%s\n```",
		issueKey,
		issueSummary,
		codingAgentName,
		output,
	)
}

func logPayloadMetadata(label, issueKey, payload string) {
	slog.Debug(label, "issue", issueKey, "chars", payloadCharCount(payload))
}

func payloadCharCount(payload string) int {
	return utf8.RuneCountInString(strings.TrimSpace(payload))
}
