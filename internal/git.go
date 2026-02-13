package internal

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

func PushBranch(branchName, remote string) error {
	output, err := exec.Command("git", "push", "-u", remote, branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("push branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(output)))
	}

	return nil
}

func CheckoutOrCreateBranch(branchName string) error {
	checkOutput, err := exec.Command("git", "rev-parse", "--verify", "--quiet", branchName).CombinedOutput()
	if err == nil {
		checkoutOutput, checkoutErr := exec.Command("git", "checkout", branchName).CombinedOutput()
		if checkoutErr != nil {
			return fmt.Errorf("checkout existing branch %s: %w (output: %s)", branchName, checkoutErr, strings.TrimSpace(string(checkoutOutput)))
		}

		return nil
	}

	if strings.TrimSpace(string(checkOutput)) != "" {
		return fmt.Errorf("verify branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(checkOutput)))
	}

	createOutput, createErr := exec.Command("git", "checkout", "-b", branchName).CombinedOutput()
	if createErr != nil {
		return fmt.Errorf("create branch %s: %w (output: %s)", branchName, createErr, strings.TrimSpace(string(createOutput)))
	}

	return nil
}

func CheckoutBranch(branchName string) error {
	checkoutOutput, err := exec.Command("git", "checkout", branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(checkoutOutput)))
	}

	return nil
}

func SyncBaseBranch(remote, baseBranch string) error {
	if err := CheckoutBranch(baseBranch); err != nil {
		return fmt.Errorf("checkout base branch %s: %w", baseBranch, err)
	}

	pullOutput, err := exec.Command("git", "pull", "--ff-only", remote, baseBranch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pull base branch %s from %s: %w (output: %s)", baseBranch, remote, err, strings.TrimSpace(string(pullOutput)))
	}

	return nil
}

func StageAndCommitChanges(issueKey, summary string) error {
	statusOutput, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("check git status: %w (output: %s)", err, strings.TrimSpace(string(statusOutput)))
	}

	if strings.TrimSpace(string(statusOutput)) == "" {
		log.Printf("No file changes detected for %s. Skipping commit.", issueKey)
		return nil
	}

	addOutput, err := exec.Command("git", "add", "-A").CombinedOutput()
	if err != nil {
		return fmt.Errorf("stage files: %w (output: %s)", err, strings.TrimSpace(string(addOutput)))
	}

	message := fmt.Sprintf("%s: %s", issueKey, summary)

	commitOutput, err := exec.Command("git", "commit", "-m", message).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create commit: %w (output: %s)", err, strings.TrimSpace(string(commitOutput)))
	}

	return nil
}
