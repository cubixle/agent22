// Package internal verifies prompt construction for coding-agent integrations.
package internal

import (
	"strings"
	"testing"
)

func TestBuildIssuePostImplementationReviewInputSafetyInstructions(t *testing.T) {
	t.Parallel()

	issue := Issue{Key: "GH-1", Summary: "Need to test Github integration"}
	input := buildIssuePostImplementationReviewInput(issue)

	if !strings.Contains(input, "do not interact with GIT directly.") {
		t.Fatalf("buildIssuePostImplementationReviewInput() missing git safety rule")
	}

	if strings.Contains(input, "you may run read-only git status and git diff") {
		t.Fatalf("buildIssuePostImplementationReviewInput() unexpectedly allows direct git commands")
	}
}

func TestBuildIssueApplyReviewInputSafetyInstructions(t *testing.T) {
	t.Parallel()

	issue := Issue{Key: "GH-1", Summary: "Need to test Github integration"}
	input := buildIssueApplyReviewInput(issue, "- None")

	if !strings.Contains(input, "do not interact with GIT directly.") {
		t.Fatalf("buildIssueApplyReviewInput() missing git safety rule")
	}

	if strings.Contains(input, "you may run read-only git status and git diff") {
		t.Fatalf("buildIssueApplyReviewInput() unexpectedly allows direct git commands")
	}
}
