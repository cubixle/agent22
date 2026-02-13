package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ----------------------------------
// agent work flow.
// ----------------------------------
// no need to do everything with AI and waste tokens!!!
// - get task from JIRA
// - validate task
//   - this is done by using the 'ready' status in JIRA.
//
// - create a new git branch
// - execute task with opencode, validate, test and commit changes.
// - push changes to remote.
// - update JIRA task with commit message and move to 'Ready for deployment' status.

type jiraSearchRequest struct {
	JQL        string   `json:"jql"`
	MaxResults int      `json:"maxResults"`
	Fields     []string `json:"fields,omitempty"`
}

type jiraSearchResponse struct {
	Total  int         `json:"total"`
	Issues []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary     string          `json:"summary"`
	Status      jiraStatus      `json:"status"`
	Description jiraDescription `json:"description,omitempty"`
}

type jiraDescription struct {
	Type    string     `json:"type,omitempty"`
	Content []jiraNode `json:"content,omitempty"`
}

type jiraNode struct {
	Type    string     `json:"type,omitempty"`
	Text    string     `json:"text,omitempty"`
	Content []jiraNode `json:"content,omitempty"`
}

func (d jiraDescription) PlainText() string {
	var b strings.Builder
	for _, node := range d.Content {
		appendNodeText(&b, node)
	}

	return strings.TrimSpace(b.String())
}

func appendNodeText(b *strings.Builder, node jiraNode) {
	if node.Text != "" {
		b.WriteString(node.Text)
	}

	for _, child := range node.Content {
		appendNodeText(b, child)
	}

	if node.Type == "paragraph" || node.Type == "heading" {
		b.WriteString("\n")
	}
}

type jiraStatus struct {
	Name string `json:"name"`
}

type giteaPullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type giteaPullRequestResponse struct {
	Number      int    `json:"number"`
	HTMLURL     string `json:"html_url"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	HeadBranch  string `json:"-"`
	BaseBranch  string `json:"-"`
	Merged      bool   `json:"merged"`
	Draft       bool   `json:"draft"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	MergeStatus string `json:"mergeable_state"`
}

func (r *giteaPullRequestResponse) UnmarshalJSON(data []byte) error {
	type alias giteaPullRequestResponse

	var aux struct {
		alias
		Head json.RawMessage `json:"head"`
		Base json.RawMessage `json:"base"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*r = giteaPullRequestResponse(aux.alias)
	r.HeadBranch = decodeBranchRef(aux.Head)
	r.BaseBranch = decodeBranchRef(aux.Base)

	return nil
}

func decodeBranchRef(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}

	var ref struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(raw, &ref); err == nil {
		return ref.Ref
	}

	return ""
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

type ResultCache struct {
	cache map[string]jiraIssue
}

func (r *ResultCache) Next() (jiraIssue, bool) {
	for _, issue := range r.cache {
		return issue, true
	}

	return jiraIssue{}, false
}

func (r *ResultCache) Get(key string) (jiraIssue, bool) {
	issue, exists := r.cache[key]
	return issue, exists
}

func (r *ResultCache) Fill(issues []jiraIssue) {
	r.cache = make(map[string]jiraIssue)
	for _, issue := range issues {
		r.cache[issue.Key] = issue
	}
}

func (r *ResultCache) Delete(key string) {
	delete(r.cache, key)
}

func (r *ResultCache) IsEmpty() bool {
	return len(r.cache) == 0
}

func main() {
	baseURL := strings.TrimRight(getEnvOrDefault("JIRA_BASE_URL", "https://scopra.atlassian.net"), "/")
	email, err := requireEnv("JIRA_EMAIL")
	if err != nil {
		log.Fatal(err)
	}

	apiToken, err := requireEnv("JIRA_API_TOKEN")
	if err != nil {
		log.Fatal(err)
	}

	maxTries := getEnvAsIntOrDefault("MAX_TRIES", 3)

	jql := getEnvOrDefault("JIRA_JQL", `project = DEV AND status = 'To Do'`)
	maxResults := getEnvAsIntOrDefault("JIRA_MAX_RESULTS", 10)
	giteaBaseURL, err := requireEnv("GITEA_BASE_URL")
	if err != nil {
		log.Fatal(err)
	}

	giteaToken, err := requireEnv("GITEA_TOKEN")
	if err != nil {
		log.Fatal(err)
	}

	giteaOwner, err := requireEnv("GITEA_OWNER")
	if err != nil {
		log.Fatal(err)
	}

	giteaRepo, err := requireEnv("GITEA_REPO")
	if err != nil {
		log.Fatal(err)
	}

	gitRemote := getEnvOrDefault("GIT_REMOTE", "origin")
	baseBranch := getEnvOrDefault("GIT_BASE_BRANCH", "main")

	httpClient := &http.Client{Timeout: 20 * time.Second}

	// internal result cache
	cache := &ResultCache{}

	tries := 0

	for {
		if tries >= maxTries {
			log.Printf("Reached maximum number of tries (%d). Exiting.", maxTries)
			break
		}

		if cache.IsEmpty() {
			tries++
			issues, err := searchIssues(context.Background(), httpClient, baseURL, email, apiToken, jiraSearchRequest{
				JQL:        jql,
				MaxResults: maxResults,
				Fields:     []string{"key", "summary", "status", "assignee", "priority", "description"},
			})
			if err != nil {
				log.Fatal(err)
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

		body := issue.Fields.Description.PlainText()
		if body == "" {
			log.Printf("Issue %s has no description. Skipping.", issue.Key)
			cache.Delete(issue.Key)
		}

		if strings.Contains(strings.ToLower(body), "HUMAN") {
			log.Printf("Issue %s requires human intervention. Skipping.", issue.Key)
			cache.Delete(issue.Key)
			continue
		}

		if strings.Contains(strings.ToLower(body), "blocked") {
			log.Printf("Issue %s is blocked. Skipping.", issue.Key)
			cache.Delete(issue.Key)
			continue
		}

		if !strings.Contains(strings.ToLower(body), "tester") {
			log.Printf("Issue %s does not mention tester. Skipping.", issue.Key)
			cache.Delete(issue.Key)
			continue
		}

		if err := syncBaseBranch(gitRemote, baseBranch); err != nil {
			log.Printf("Failed to pull latest changes from %s/%s before processing issue %s: %v. Exiting.", gitRemote, baseBranch, issue.Key, err)
			return
		}

		err := checkoutOrCreateBranch(issue.Key)
		if err != nil {
			log.Printf("Failed to checkout/create branch for issue %s: %v. Exiting.", issue.Key, err)
			return
		}

		fmt.Printf("Changed branch to %s\n", issue.Key)

		// run opencode with body as input, make sure to pass AGENTS.md as context and instructions
		// to not use any tools and to only output code.

		input := fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			"do not interact with GIT directly, do not use any tools. do not ask for human input.",
			issue.Fields.Summary,
			issue.Fields.Description.PlainText(),
		)

		opencodeOutputBytes, err := exec.Command("opencode", "run", input).CombinedOutput()
		opencodeOutput := strings.TrimSpace(string(opencodeOutputBytes))
		if err != nil {
			log.Printf("Failed to execute opencode for issue %s: %v (output: %s). Exiting.", issue.Key, err, opencodeOutput)
			return
		}

		fmt.Printf("Executed opencode for issue %s\n", issue.Key)

		if err := stageAndCommitChanges(issue.Key, issue.Fields.Summary); err != nil {
			log.Printf("Failed to stage/commit changes for issue %s: %v. Exiting.", issue.Key, err)
			return
		}

		if err := pushBranch(issue.Key, gitRemote); err != nil {
			log.Printf("Failed to push branch for issue %s: %v. Exiting.", issue.Key, err)
			return
		}

		fmt.Printf("Pushed branch %s to remote %s\n", issue.Key, gitRemote)

		existingPR, err := findOpenPullRequest(
			context.Background(),
			httpClient,
			giteaBaseURL,
			giteaToken,
			giteaOwner,
			giteaRepo,
			issue.Key,
			baseBranch,
		)
		if err != nil {
			log.Printf("Failed to check existing merge request for issue %s: %v. Exiting.", issue.Key, err)
			return
		}

		if existingPR != nil {
			fmt.Printf("Merge request already exists #%d: %s. Skipping creation.\n", existingPR.Number, existingPR.HTMLURL)
		} else {
			pr, err := createGiteaMergeRequest(
				context.Background(),
				httpClient,
				giteaBaseURL,
				giteaToken,
				giteaOwner,
				giteaRepo,
				giteaPullRequestRequest{
					Title: fmt.Sprintf("%s: %s", issue.Key, issue.Fields.Summary),
					Body:  fmt.Sprintf("Automated merge request for %s", issue.Key),
					Head:  issue.Key,
					Base:  baseBranch,
				},
			)
			if err != nil {
				log.Printf("Failed to create merge request for issue %s: %v. Exiting.", issue.Key, err)
				return
			}

			fmt.Printf("Created merge request #%d: %s\n", pr.Number, pr.HTMLURL)
		}
		fmt.Printf("Changing branch back to main\n")
		err = exec.Command("git", "checkout", "main").Run()
		if err != nil {
			log.Printf("Failed to change back to main branch after working on issue %s: %v. Exiting.", issue.Key, err)
			return
		}

		// remove from cache if work is complete.
		time.Sleep(8 * time.Second) // simulate work
	}

}

func searchIssues(ctx context.Context, httpClient *http.Client, baseURL, email, apiToken string, payload jiraSearchRequest) ([]jiraIssue, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jira search payload: %w", err)
	}

	log.Printf("Searching JIRA with JQL: %s", body)

	endpoint := baseURL + "/rest/api/3/search/jql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create jira request: %w", err)
	}

	req.SetBasicAuth(email, apiToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute jira request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira search failed: status=%s body=%s", resp.Status, strings.TrimSpace(string(rawBody)))
	}

	// log body
	rawBody, _ := io.ReadAll(resp.Body)

	var result jiraSearchResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("decode jira response: %w", err)
	}

	return result.Issues, nil
}

func pushBranch(branchName, remote string) error {
	cmd := exec.Command("git", "push", "-u", remote, branchName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("push branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(output)))
	}

	return nil
}

func checkoutOrCreateBranch(branchName string) error {
	checkCmd := exec.Command("git", "rev-parse", "--verify", "--quiet", branchName)
	checkOutput, err := checkCmd.CombinedOutput()

	if err == nil {
		checkoutCmd := exec.Command("git", "checkout", branchName)
		checkoutOutput, checkoutErr := checkoutCmd.CombinedOutput()
		if checkoutErr != nil {
			return fmt.Errorf("checkout existing branch %s: %w (output: %s)", branchName, checkoutErr, strings.TrimSpace(string(checkoutOutput)))
		}

		return nil
	}

	if strings.TrimSpace(string(checkOutput)) != "" {
		return fmt.Errorf("verify branch %s: %w (output: %s)", branchName, err, strings.TrimSpace(string(checkOutput)))
	}

	createCmd := exec.Command("git", "checkout", "-b", branchName)
	createOutput, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		return fmt.Errorf("create branch %s: %w (output: %s)", branchName, createErr, strings.TrimSpace(string(createOutput)))
	}

	return nil
}

func syncBaseBranch(remote, baseBranch string) error {
	checkoutCmd := exec.Command("git", "checkout", baseBranch)
	checkoutOutput, err := checkoutCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout base branch %s: %w (output: %s)", baseBranch, err, strings.TrimSpace(string(checkoutOutput)))
	}

	pullCmd := exec.Command("git", "pull", "--ff-only", remote, baseBranch)
	pullOutput, err := pullCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pull base branch %s from %s: %w (output: %s)", baseBranch, remote, err, strings.TrimSpace(string(pullOutput)))
	}

	return nil
}

func stageAndCommitChanges(issueKey, summary string) error {
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusOutput, err := statusCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("check git status: %w (output: %s)", err, strings.TrimSpace(string(statusOutput)))
	}

	if strings.TrimSpace(string(statusOutput)) == "" {
		log.Printf("No file changes detected for %s. Skipping commit.", issueKey)
		return nil
	}

	addCmd := exec.Command("git", "add", "-A")
	addOutput, err := addCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stage files: %w (output: %s)", err, strings.TrimSpace(string(addOutput)))
	}

	message := fmt.Sprintf("%s: %s", issueKey, summary)
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitOutput, err := commitCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create commit: %w (output: %s)", err, strings.TrimSpace(string(commitOutput)))
	}

	return nil
}

func createGiteaMergeRequest(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, token, owner, repo string,
	payload giteaPullRequestRequest,
) (giteaPullRequestResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return giteaPullRequestResponse{}, fmt.Errorf("marshal gitea pull request payload: %w", err)
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/pulls",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return giteaPullRequestResponse{}, fmt.Errorf("create gitea request: %w", err)
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return giteaPullRequestResponse{}, fmt.Errorf("execute gitea request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return giteaPullRequestResponse{}, fmt.Errorf("gitea pull request creation failed: status=%s body=%s", resp.Status, strings.TrimSpace(string(rawBody)))
	}

	var result giteaPullRequestResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return giteaPullRequestResponse{}, fmt.Errorf("decode gitea response: %w", err)
	}

	return result, nil
}

func findOpenPullRequest(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, token, owner, repo, headBranch, baseBranch string,
) (*giteaPullRequestResponse, error) {
	endpoint := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/pulls?state=open",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create gitea pull request lookup request: %w", err)
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute gitea pull request lookup request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitea pull request lookup failed: status=%s body=%s", resp.Status, strings.TrimSpace(string(rawBody)))
	}

	var pulls []giteaPullRequestResponse
	if err := json.Unmarshal(rawBody, &pulls); err != nil {
		return nil, fmt.Errorf("decode gitea pull request lookup response: %w", err)
	}

	for _, pr := range pulls {
		if pr.HeadBranch == headBranch && pr.BaseBranch == baseBranch {
			matched := pr
			return &matched, nil
		}
	}

	return nil, nil
}

func requireEnv(key string) (string, error) {
	value := normalizeEnvValue(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing required environment variable: %s", key)
	}

	return value, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	value := normalizeEnvValue(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	return value
}

func getEnvAsIntOrDefault(key string, defaultValue int) int {
	value := normalizeEnvValue(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}

func normalizeEnvValue(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.TrimSuffix(cleaned, ",")
	cleaned = strings.TrimSpace(cleaned)

	if len(cleaned) >= 2 {
		if (cleaned[0] == '"' && cleaned[len(cleaned)-1] == '"') || (cleaned[0] == '\'' && cleaned[len(cleaned)-1] == '\'') {
			cleaned = cleaned[1 : len(cleaned)-1]
		}
	}

	return strings.TrimSpace(cleaned)
}
