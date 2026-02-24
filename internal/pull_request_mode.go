// Package internal contains pull-request monitoring and automation workflows.
package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

func RunPullRequestMode(config AgentConfig) error {
	if err := validatePullRequestModeConfig(config); err != nil {
		return fmt.Errorf("validate pull request mode config: %w", err)
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}

	scm, err := NewPullRequestProvider(httpClient, config)
	if err != nil {
		return fmt.Errorf("build pull request provider: %w", err)
	}

	seenCommentsByPR := make(map[int]map[string]struct{})

	return runPollingEventLoop(pollingEventLoopConfig{
		Name:        "pull request",
		BaseWait:    time.Duration(config.WaitTimeSeconds) * time.Second,
		MaxIdleWait: 10 * time.Minute,
	}, func(ctx context.Context) (bool, error) {
		return processPullRequestComments(ctx, scm, config, seenCommentsByPR)
	})
}

func validatePullRequestModeConfig(config AgentConfig) error {
	if err := validateSharedSCMConfig(config); err != nil {
		return fmt.Errorf("scm config: %w", err)
	}

	return nil
}

func processPullRequestComments(
	ctx context.Context,
	scm PullRequestProvider,
	config AgentConfig,
	seenCommentsByPR map[int]map[string]struct{},
) (bool, error) {
	pulls, err := scm.ListOpenPullRequests(ctx)
	if err != nil {
		return false, fmt.Errorf("list open pull requests: %w", err)
	}

	if len(pulls) == 0 {
		slog.Info("No open pull requests to monitor")
		return false, nil
	}

	hadWork := false

	for _, pr := range pulls {
		processedAny, err := processPullRequest(ctx, scm, config, pr, seenCommentsByPR)
		if err != nil {
			return false, fmt.Errorf("process pull request #%d: %w", pr.Number, err)
		}

		if processedAny {
			hadWork = true
		}
	}

	return hadWork, nil
}

func processPullRequest(
	ctx context.Context,
	scm PullRequestProvider,
	config AgentConfig,
	pr PullRequest,
	seenCommentsByPR map[int]map[string]struct{},
) (bool, error) {
	comments, err := scm.ListPullRequestComments(ctx, pr.Number)
	if err != nil {
		return false, fmt.Errorf("list comments for pull request #%d: %w", pr.Number, err)
	}

	hasPreemptiveComment := hasPreemptiveReviewComment(comments)

	comments = filterOperationalPullRequestComments(pr, comments)
	comments = filterAgentGeneratedComments(comments)

	if len(comments) == 0 {
		if hasPreemptiveComment {
			return false, nil
		}

		if err := runPreemptivePullRequestReview(ctx, scm, config, pr); err != nil {
			return false, fmt.Errorf("run preemptive review for pull request #%d: %w", pr.Number, err)
		}

		return true, nil
	}

	commentKeys, exists := seenCommentsByPR[pr.Number]
	if !exists {
		commentKeys = make(map[string]struct{})
		seenCommentsByPR[pr.Number] = commentKeys
	}

	sortPullRequestComments(comments)

	hadWork := false

	for _, comment := range comments {
		key := pullRequestCommentKey(comment)
		if _, exists := commentKeys[key]; exists {
			continue
		}

		if err := applyPullRequestComment(config, pr, comment); err != nil {
			return false, fmt.Errorf("apply comment %d for pull request #%d: %w", comment.ID, pr.Number, err)
		}

		commentKeys[key] = struct{}{}
		hadWork = true
	}

	return hadWork, nil
}

func applyPullRequestComment(config AgentConfig, pr PullRequest, comment PullRequestComment) error {
	slog.Info("Detected new pull request comment", "pr_number", pr.Number, "head_branch", pr.HeadBranch, "comment_id", comment.ID)

	if strings.TrimSpace(pr.HeadBranch) == "" {
		return fmt.Errorf("pull request #%d has no head branch", pr.Number)
	}

	if err := CheckoutAndSyncBranch(pr.HeadBranch, config.GitRemote); err != nil {
		return fmt.Errorf("checkout and sync branch %s: %w", pr.HeadBranch, err)
	}

	defer func() {
		if err := CheckoutBranch(config.GitBaseBranch); err != nil {
			slog.Error(
				"Restore base branch after PR comment",
				"base_branch", config.GitBaseBranch,
				"pr_number", pr.Number,
				"comment_id", comment.ID,
				"error", err,
			)
		}
	}()

	alreadyProcessed, err := HasCommitForPullRequestComment(pr.HeadBranch, comment.ID)
	if err != nil {
		return fmt.Errorf("check git history for pull request #%d comment %d: %w", pr.Number, comment.ID, err)
	}

	if alreadyProcessed {
		slog.Info("Skipping PR comment already present in branch history", "pr_number", pr.Number, "comment_id", comment.ID)

		return nil
	}

	input := buildPullRequestCommentOpencodeInput(pr, comment)

	outputStepf("Running opencode for pull request #%d comment %d", pr.Number, comment.ID)

	opencodeOutputBytes, err := runOpencodeWithProgress(fmt.Sprintf("PR-%d", pr.Number), input)
	if err != nil {
		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
		return fmt.Errorf("execute opencode for pull request #%d comment %d: %w (output: %s)", pr.Number, comment.ID, err, opencodeOutput)
	}

	if err := StageAndCommitChanges(fmt.Sprintf("PR-%d", pr.Number), fmt.Sprintf("Address PR comment #%d [pr-comment-id:%d]", comment.ID, comment.ID)); err != nil {
		return fmt.Errorf("stage and commit pull request #%d changes: %w", pr.Number, err)
	}

	if err := PushBranch(pr.HeadBranch, config.GitRemote); err != nil {
		return fmt.Errorf("push branch %s: %w", pr.HeadBranch, err)
	}

	slog.Info("Applied PR comment and pushed branch", "pr_number", pr.Number, "comment_id", comment.ID, "head_branch", pr.HeadBranch)

	return nil
}

func runPreemptivePullRequestReview(
	ctx context.Context,
	scm PullRequestProvider,
	config AgentConfig,
	pr PullRequest,
) error {
	slog.Info("No comments found, running preemptive PR review", "pr_number", pr.Number)

	if strings.TrimSpace(pr.HeadBranch) == "" {
		return fmt.Errorf("pull request #%d has no head branch", pr.Number)
	}

	if err := CheckoutAndSyncBranch(pr.HeadBranch, config.GitRemote); err != nil {
		return fmt.Errorf("checkout and sync branch %s: %w", pr.HeadBranch, err)
	}

	defer func() {
		if err := CheckoutBranch(config.GitBaseBranch); err != nil {
			slog.Error(
				"Restore base branch after preemptive PR review",
				"base_branch", config.GitBaseBranch,
				"pr_number", pr.Number,
				"error", err,
			)
		}
	}()

	input := buildPullRequestPreemptiveReviewInput(pr)
	outputStepf("Running preemptive opencode review for pull request #%d", pr.Number)

	opencodeOutputBytes, err := runOpencodeWithProgress(fmt.Sprintf("PR-%d-review", pr.Number), input)
	if err != nil {
		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
		return fmt.Errorf("execute preemptive opencode review for pull request #%d: %w (output: %s)", pr.Number, err, opencodeOutput)
	}

	reviewComment := formatPreemptiveReviewComment(string(opencodeOutputBytes))
	if err := scm.CreatePullRequestComment(ctx, pr.Number, reviewComment); err != nil {
		return fmt.Errorf("post preemptive review comment to pull request #%d: %w", pr.Number, err)
	}

	slog.Info("Posted preemptive review comment", "pr_number", pr.Number)

	return nil
}

func sortPullRequestComments(comments []PullRequestComment) {
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].CreatedAt == comments[j].CreatedAt {
			return pullRequestCommentKey(comments[i]) < pullRequestCommentKey(comments[j])
		}

		return comments[i].CreatedAt < comments[j].CreatedAt
	})
}

func pullRequestCommentKey(comment PullRequestComment) string {
	return fmt.Sprintf("%s:%d:%d:%s:%d:%d", comment.Type, comment.ID, comment.ReviewID, comment.Path, comment.Line, comment.OldLine)
}

func hasPreemptiveReviewComment(comments []PullRequestComment) bool {
	for _, comment := range comments {
		if strings.Contains(comment.Body, preemptiveReviewMarker) {
			return true
		}
	}

	return false
}

func filterOperationalPullRequestComments(pr PullRequest, comments []PullRequestComment) []PullRequestComment {
	prBody := strings.TrimSpace(pr.Body)
	if prBody == "" {
		return comments
	}

	filtered := make([]PullRequestComment, 0, len(comments))
	skippedInitial := false

	for _, comment := range comments {
		if !skippedInitial && comment.Type == "conversation" && strings.TrimSpace(comment.Body) == prBody {
			skippedInitial = true
			continue
		}

		filtered = append(filtered, comment)
	}

	return filtered
}

func filterAgentGeneratedComments(comments []PullRequestComment) []PullRequestComment {
	filtered := make([]PullRequestComment, 0, len(comments))

	for _, comment := range comments {
		if strings.Contains(comment.Body, preemptiveReviewMarker) {
			continue
		}

		filtered = append(filtered, comment)
	}

	return filtered
}
