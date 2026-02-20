package internal

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var gitRefPartPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func PushBranch(branchName, remote string) error {
	output, err := exec.Command("git", "push", "-u", remote, branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("push branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(output)))
	}

	return nil
}

func CheckoutOrCreateBranch(branchName, baseBranch string) error {
	exists, checkOutput, err := branchExists(branchName)
	if err == nil && exists {
		checkoutOutput, checkoutErr := exec.Command("git", "checkout", branchName).CombinedOutput()
		if checkoutErr != nil {
			return fmt.Errorf("checkout existing branch %s: %w (output: %s)", branchName, checkoutErr, strings.TrimSpace(string(checkoutOutput)))
		}

		mergeOutput, mergeErr := exec.Command("git", "merge", baseBranch).CombinedOutput()
		if mergeErr != nil {
			return fmt.Errorf("merge base branch %s into %s: %w (output: %s)", baseBranch, branchName, mergeErr, strings.TrimSpace(string(mergeOutput)))
		}

		return nil
	}

	if err != nil && strings.TrimSpace(string(checkOutput)) != "" {
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

func CheckoutAndSyncBranch(branchName, remote string) error {
	safeBranchName, err := validateGitRefPart("branch", branchName)
	if err != nil {
		return err
	}

	safeRemote, err := validateGitRefPart("remote", remote)
	if err != nil {
		return err
	}

	fetchOutput, fetchErr := exec.Command("git", "fetch", "--prune", safeRemote).CombinedOutput()
	if fetchErr != nil {
		return fmt.Errorf("fetch remote %s: %w (output: %s)", safeRemote, fetchErr, strings.TrimSpace(string(fetchOutput)))
	}

	exists, checkOutput, err := branchExists(safeBranchName)
	if err == nil && exists {
		if err := CheckoutBranch(safeBranchName); err != nil {
			return fmt.Errorf("checkout existing branch %s: %w", safeBranchName, err)
		}
	} else {
		if err != nil && strings.TrimSpace(string(checkOutput)) != "" {
			return fmt.Errorf("verify branch %s: %w (output: %s)", safeBranchName, err, strings.TrimSpace(string(checkOutput)))
		}

		remoteBranchRef := safeRemote + "/" + safeBranchName

		checkoutOutput, checkoutErr := exec.Command("git", "checkout", "-b", safeBranchName, "--track", remoteBranchRef).CombinedOutput()
		if checkoutErr != nil {
			return fmt.Errorf("create tracking branch %s from %s/%s: %w (output: %s)", safeBranchName, safeRemote, safeBranchName, checkoutErr, strings.TrimSpace(string(checkoutOutput)))
		}
	}

	pullOutput, pullErr := exec.Command("git", "pull", "--ff-only", safeRemote, safeBranchName).CombinedOutput()
	if pullErr != nil {
		return fmt.Errorf("sync branch %s from %s: %w (output: %s)", safeBranchName, safeRemote, pullErr, strings.TrimSpace(string(pullOutput)))
	}

	return nil
}

func branchExists(branchName string) (exists bool, output []byte, err error) {
	output, err = exec.Command("git", "rev-parse", "--verify", "--quiet", branchName).CombinedOutput()
	if err != nil {
		return false, output, err
	}

	return true, output, nil
}

func validateGitRefPart(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}

	if !gitRefPartPattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid %s %q", name, value)
	}

	return trimmed, nil
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
		slog.Info("No file changes detected, skipping commit", "issue", issueKey)
		return nil
	}

	paths := changedPathsFromPorcelain(string(statusOutput))
	if len(paths) == 0 {
		slog.Info("No stageable file changes detected, skipping commit", "issue", issueKey)
		return nil
	}

	blockedPaths := sensitivePaths(paths)
	if len(blockedPaths) > 0 {
		return fmt.Errorf("refusing to commit sensitive files: %s", strings.Join(blockedPaths, ", "))
	}

	addArgs := append([]string{"add", "--"}, paths...)

	addOutput, err := exec.Command("git", addArgs...).CombinedOutput()
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

func changedPathsFromPorcelain(status string) []string {
	lines := strings.Split(status, "\n")
	seen := make(map[string]struct{})

	for _, line := range lines {
		if len(line) < 4 {
			continue
		}

		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}

		if idx := strings.LastIndex(path, " -> "); idx >= 0 {
			path = strings.TrimSpace(path[idx+4:])
		}

		path = strings.Trim(path, "\"")
		if path == "" {
			continue
		}

		seen[path] = struct{}{}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	return paths
}

func sensitivePaths(paths []string) []string {
	blocked := make([]string, 0)

	for _, path := range paths {
		if isSensitivePath(path) {
			blocked = append(blocked, path)
		}
	}

	sort.Strings(blocked)

	return blocked
}

func isSensitivePath(path string) bool {
	cleanPath := filepath.ToSlash(strings.TrimSpace(path))
	if cleanPath == "" {
		return false
	}

	lowerPath := strings.ToLower(cleanPath)
	baseName := strings.ToLower(filepath.Base(cleanPath))

	if baseName == ".agent22.yml" {
		return true
	}

	if strings.HasPrefix(baseName, ".env") {
		return true
	}

	if strings.HasSuffix(baseName, ".pem") || strings.HasSuffix(baseName, ".key") || strings.HasSuffix(baseName, ".p12") || strings.HasSuffix(baseName, ".pfx") || strings.HasSuffix(baseName, ".p8") {
		return true
	}

	if baseName == "id_rsa" || baseName == "id_ed25519" || baseName == "credentials.json" {
		return true
	}

	if strings.Contains(lowerPath, "/.ssh/") {
		return true
	}

	return false
}

func HasCommitForPullRequestComment(branchName string, commentID int64) (bool, error) {
	safeBranchName, err := validateGitRefPart("branch", branchName)
	if err != nil {
		return false, err
	}

	marker := fmt.Sprintf("pr-comment-id:%d", commentID)

	matched, err := hasCommitSubjectMatch(safeBranchName, marker)
	if err != nil {
		return false, err
	}

	if matched {
		return true, nil
	}

	legacyNeedle := fmt.Sprintf("Address PR comment #%d", commentID)

	return hasCommitSubjectMatch(safeBranchName, legacyNeedle)
}

func hasCommitSubjectMatch(branchName, needle string) (bool, error) {
	output, err := exec.Command("git", "log", branchName, "--format=%s", "--grep", needle, "--fixed-strings", "-n", "1").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("search commit subjects on %s for %q: %w (output: %s)", branchName, needle, err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)) != "", nil
}
