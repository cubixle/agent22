// Package internal provides shared opencode input and execution utilities.
package internal

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

const opencodeSafetyInstructions = `
do not interact with GIT directly.
do not ask for human input.
never run git clean (including -fd, -fdx, or -fdX) and never delete local config files like .env* or .agent22.yml.
`

const preemptiveReviewMarker = "[agent22-preemptive-review]"

const humanReadableReviewOutputInstructions = `
Output format requirements:
- Write for a human engineer reviewing a PR.
- Use Markdown with these sections exactly:
  1) ## Verdict
  2) ## Must Fix
  3) ## Should Improve
  4) ## Nice to Have
  5) ## Test Gaps
- Use short bullet points.
- For each finding, include: file path (if known), impact, and suggested fix.
- If there are no findings for a section, write "- None".
- Keep total response under 2200 words.
`

func buildIssueOpencodeInput(issue Issue) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		opencodeSafetyInstructions,
		issue.Summary,
		issue.Description,
	)

	return appendLocalAgentsInstructions(input)
}

func buildIssuePostImplementationReviewInput(issue Issue) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\nJIRA issue key:\n%s\n\nJIRA issue summary:\n%s\n\nReview the current local changes in this branch before push. Do not modify files in this step. Produce only review findings for a human engineer.",
		opencodeSafetyInstructions,
		humanReadableReviewOutputInstructions,
		issue.Key,
		issue.Summary,
	)

	return appendLocalAgentsInstructions(input)
}

func buildIssueApplyReviewInput(issue Issue, reviewOutput string) string {
	trimmedReview := strings.TrimSpace(reviewOutput)
	if trimmedReview == "" {
		trimmedReview = "No findings provided."
	}

	input := fmt.Sprintf(
		"%s\n\nJIRA issue key:\n%s\n\nJIRA issue summary:\n%s\n\nApply the following review feedback to the current branch. Implement only concrete fixes. If there are no valid findings, keep files unchanged.\n\nReview feedback:\n%s",
		opencodeSafetyInstructions,
		issue.Key,
		issue.Summary,
		trimmedReview,
	)

	return appendLocalAgentsInstructions(input)
}

func buildPullRequestCommentOpencodeInput(pr PullRequest, comment PullRequestComment) string {
	commentContext := strings.TrimSpace(comment.Body)
	if comment.Type == "line" {
		commentContext = fmt.Sprintf(
			"Type: line comment\nReview ID: %d\nFile: %s\nLine: %d\nOld line: %d\n\n%s",
			comment.ReviewID,
			comment.Path,
			comment.Line,
			comment.OldLine,
			strings.TrimSpace(comment.Body),
		)
	}

	if comment.Type == "review" {
		commentContext = fmt.Sprintf(
			"Type: review\nReview ID: %d\nState: %s\n\n%s",
			comment.ReviewID,
			strings.TrimSpace(comment.State),
			strings.TrimSpace(comment.Body),
		)
	}

	input := fmt.Sprintf(
		"%s\n\nPull request title:\n%s\n\nPull request number:\n%d\n\nReviewer comment:\n%s",
		opencodeSafetyInstructions,
		pr.Title,
		pr.Number,
		commentContext,
	)

	return appendLocalAgentsInstructions(input)
}

func buildPullRequestPreemptiveReviewInput(pr PullRequest) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\nPull request title:\n%s\n\nPull request number:\n%d\n\nPlease review this pull request as a human code reviewer would. Focus on correctness risks, edge cases, missing tests, and maintainability concerns. Provide concise actionable feedback with severity.",
		opencodeSafetyInstructions,
		humanReadableReviewOutputInstructions,
		pr.Title,
		pr.Number,
	)

	return appendLocalAgentsInstructions(input)
}

func formatPreemptiveReviewComment(reviewOutput string) string {
	output := strings.TrimSpace(reviewOutput)
	if output == "" {
		output = "No review findings were produced by opencode."
	}

	const maxOutputLen = 15000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n\n---\nTruncated by agent22 to fit comment size."
	}

	return fmt.Sprintf("%s\nAutomated preemptive review from agent22:\n\n%s", preemptiveReviewMarker, output)
}

func runOpencodeWithProgress(issueKey, input string) ([]byte, error) {
	spinner := []string{"|", "/", "-", "\\"}

	const barWidth = 20

	done := make(chan struct{})

	var output []byte

	var runErr error

	go func() {
		output, runErr = exec.Command("opencode", "run", input).CombinedOutput()

		close(done)
	}()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	progress := 0
	spinIdx := 0
	startedAt := time.Now()

	for {
		select {
		case <-done:
			elapsed := time.Since(startedAt).Round(time.Second)
			fmt.Printf("\rRunning opencode for issue %s... [====================] 100%% (%s)\n", issueKey, elapsed)

			return output, runErr
		case <-ticker.C:
			if progress < 95 {
				progress++
			}

			filled := (progress * barWidth) / 100
			bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
			frame := spinnerFrame(spinner, spinIdx)
			fmt.Printf("\rRunning opencode for issue %s... [%s] %3d%% %s", issueKey, bar, progress, frame)

			spinIdx++
		}
	}
}

func spinnerFrame(spinner []string, index int) string {
	if len(spinner) == 0 {
		return ""
	}

	return spinner[index%len(spinner)]
}

func appendLocalAgentsInstructions(input string) string {
	content, err := os.ReadFile("AGENTS.md")
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("appendLocalAgentsInstructions@internal: read AGENTS.md", "error", err)
		}

		return input
	}

	agentsInstructions := strings.TrimSpace(string(content))
	if agentsInstructions == "" {
		return input
	}

	return input + "\n\nAdditional instructions from AGENTS.md:\n" + agentsInstructions
}
