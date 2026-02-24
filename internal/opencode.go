// Package internal provides shared coding-agent prompt and formatting utilities.
package internal

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const codingAgentSafetyInstructions = `
do not interact with GIT directly.
you may run read-only git status and git diff when reviewing local changes.
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

func buildIssueCodingAgentInput(issue Issue) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		codingAgentSafetyInstructions,
		issue.Summary,
		issue.Description,
	)

	return appendLocalAgentsInstructions(input)
}

func buildIssuePostImplementationReviewInput(issue Issue) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\nIssue key:\n%s\n\nIssue summary:\n%s\n\nReview the current local changes in this branch before push. Do not modify files in this step. Produce only review findings for a human engineer.",
		codingAgentSafetyInstructions,
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
		"%s\n\nIssue key:\n%s\n\nIssue summary:\n%s\n\nApply the following review feedback to the current branch. Implement only concrete fixes. If there are no valid findings, keep files unchanged.\n\nReview feedback:\n%s",
		codingAgentSafetyInstructions,
		issue.Key,
		issue.Summary,
		trimmedReview,
	)

	return appendLocalAgentsInstructions(input)
}

func buildPullRequestCommentCodingAgentInput(pr PullRequest, comment PullRequestComment) string {
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
		codingAgentSafetyInstructions,
		pr.Title,
		pr.Number,
		commentContext,
	)

	return appendLocalAgentsInstructions(input)
}

func buildPullRequestPreemptiveReviewInput(pr PullRequest) string {
	input := fmt.Sprintf(
		"%s\n\n%s\n\nPull request title:\n%s\n\nPull request number:\n%d\n\nPlease review this pull request as a human code reviewer would. Focus on correctness risks, edge cases, missing tests, and maintainability concerns. Provide concise actionable feedback with severity.",
		codingAgentSafetyInstructions,
		humanReadableReviewOutputInstructions,
		pr.Title,
		pr.Number,
	)

	return appendLocalAgentsInstructions(input)
}

func formatPreemptiveReviewComment(reviewOutput, codingAgentName string) string {
	output := strings.TrimSpace(reviewOutput)
	if output == "" {
		output = fmt.Sprintf("No review findings were produced by %s.", codingAgentName)
	}

	const maxOutputLen = 15000
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n\n---\nTruncated by agent22 to fit comment size."
	}

	return fmt.Sprintf("%s\nAutomated preemptive review from agent22 (%s):\n\n%s", preemptiveReviewMarker, codingAgentName, output)
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
