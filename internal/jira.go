package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type JiraConfig struct {
	Email      string `yaml:"email"`
	APIToken   string `yaml:"api_token"` //nolint:gosec // Jira API token is loaded from external configuration.
	JQL        string `yaml:"jql"`
	BaseURL    string `yaml:"base_url"`
	MaxResults int    `yaml:"max_results"`
	PRStatus   string `yaml:"pr_status"`
	DoneStatus string `yaml:"done_status"`
}

type JiraClient struct {
	httpClient *http.Client
	cfg        *JiraConfig
	baseURL    string
	email      string
	apiToken   string
}

func NewJiraClient(httpClient *http.Client, cfg *JiraConfig) *JiraClient {
	return &JiraClient{
		httpClient: httpClient,
		cfg:        cfg,
		baseURL:    cfg.BaseURL,
		email:      cfg.Email,
		apiToken:   cfg.APIToken,
	}
}

type JiraSearchRequest struct {
	JQL        string   `json:"jql"`
	MaxResults int      `json:"maxResults"`
	Fields     []string `json:"fields,omitempty"`
}

type JiraSearchResponse struct {
	Issues []JiraIssue `json:"issues"`
}

type JiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields JiraIssueFields `json:"fields"`
}

type JiraIssueFields struct {
	Summary     string          `json:"summary"`
	Status      JiraStatus      `json:"status"`
	Description JiraDescription `json:"description"`
}

type JiraDescription struct {
	Type    string     `json:"type,omitempty"`
	Content []JiraNode `json:"content,omitempty"`
}

type JiraNode struct {
	Type    string     `json:"type,omitempty"`
	Text    string     `json:"text,omitempty"`
	Content []JiraNode `json:"content,omitempty"`
}

func (d JiraDescription) PlainText() string {
	var b strings.Builder
	for _, node := range d.Content {
		appendNodeText(&b, node)
	}

	return strings.TrimSpace(b.String())
}

func appendNodeText(b *strings.Builder, node JiraNode) {
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

type JiraStatus struct {
	Name string `json:"name"`
}

type jiraTransitionsResponse struct {
	Transitions []jiraTransition `json:"transitions"`
}

type jiraTransition struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	To   JiraStatus `json:"to"`
}

type jiraTransitionRequest struct {
	Transition jiraTransitionTarget `json:"transition"`
}

type jiraTransitionTarget struct {
	ID string `json:"id"`
}

func (c *JiraClient) SearchIssues(ctx context.Context) ([]Issue, error) {
	issues, err := c.searchJiraIssues(ctx, JiraSearchRequest{
		JQL:        c.cfg.JQL,
		MaxResults: c.cfg.MaxResults,
		Fields:     []string{"key", "summary", "status", "assignee", "priority", "description"},
	})
	if err != nil {
		return nil, err
	}

	result := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, Issue{
			ID:          issue.ID,
			Key:         issue.Key,
			Summary:     issue.Fields.Summary,
			Status:      issue.Fields.Status.Name,
			Description: issue.Fields.Description.PlainText(),
		})
	}

	return result, nil
}

func (c *JiraClient) searchJiraIssues(ctx context.Context, payload JiraSearchRequest) ([]JiraIssue, error) {
	searchPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jira search payload: %w", err)
	}

	slog.Debug("Searching JIRA", "jql_payload", string(searchPayload))

	var result JiraSearchResponse

	endpoint := c.endpoint("search/jql")

	if err := c.doJiraJSON(ctx, http.MethodPost, endpoint, payload, &result, "search"); err != nil {
		return nil, err
	}

	return result.Issues, nil
}

func (c *JiraClient) MarkIssueInProgress(ctx context.Context, issueKey string) error {
	return c.transitionIssueToStatus(ctx, issueKey, c.cfg.PRStatus)
}

func (c *JiraClient) MarkIssueDone(ctx context.Context, issueKey string) error {
	return c.transitionIssueToStatus(ctx, issueKey, c.cfg.DoneStatus)
}

func (c *JiraClient) transitionIssueToStatus(ctx context.Context, issueKey, targetStatus string) error {
	status := strings.TrimSpace(targetStatus)
	if status == "" {
		return nil
	}

	transitions, err := c.getIssueTransitions(ctx, issueKey)
	if err != nil {
		return fmt.Errorf("get transitions: %w", err)
	}

	transitionID, ok := findTransitionIDByStatus(transitions, status)
	if !ok {
		available := make([]string, 0, len(transitions))
		for _, t := range transitions {
			name := strings.TrimSpace(t.To.Name)
			if name == "" {
				name = strings.TrimSpace(t.Name)
			}

			if name != "" {
				available = append(available, name)
			}
		}

		return fmt.Errorf("no transition found for status %q (available: %s)", status, strings.Join(available, ", "))
	}

	payload := jiraTransitionRequest{Transition: jiraTransitionTarget{ID: transitionID}}

	endpoint := c.endpoint(fmt.Sprintf("issue/%s/transitions", issueKey))

	if err := c.doJiraJSON(ctx, http.MethodPost, endpoint, payload, nil, "transition"); err != nil {
		return err
	}

	return nil
}

func (c *JiraClient) getIssueTransitions(ctx context.Context, issueKey string) ([]jiraTransition, error) {
	endpoint := c.endpoint(fmt.Sprintf("issue/%s/transitions", issueKey))

	var result jiraTransitionsResponse

	if err := c.doJiraJSON(ctx, http.MethodGet, endpoint, nil, &result, "transitions lookup"); err != nil {
		return nil, err
	}

	return result.Transitions, nil
}

func (c *JiraClient) endpoint(resource string) string {
	return fmt.Sprintf("%s/rest/api/3/%s", strings.TrimRight(c.baseURL, "/"), resource)
}

func (c *JiraClient) doJiraJSON(
	ctx context.Context,
	method, endpoint string,
	payload, out any,
	operation string,
) error {
	var body io.Reader = http.NoBody

	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal jira %s payload: %w", operation, err)
		}

		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create jira %s request: %w", operation, err)
	}

	req.SetBasicAuth(c.email, c.apiToken)
	req.Header.Set("Accept", "application/json")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // endpoint is built from the configured Jira base URL.
	if err != nil {
		return fmt.Errorf("execute jira %s request: %w", operation, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jira %s failed: status=%s body=%s", operation, resp.Status, strings.TrimSpace(string(rawBody)))
	}

	if out == nil || len(rawBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(rawBody, out); err != nil {
		return fmt.Errorf("decode jira %s response: %w", operation, err)
	}

	return nil
}

func findTransitionIDByStatus(transitions []jiraTransition, targetStatus string) (string, bool) {
	target := strings.ToLower(strings.TrimSpace(targetStatus))
	for _, transition := range transitions {
		if strings.ToLower(strings.TrimSpace(transition.Name)) == target {
			return transition.ID, true
		}

		if strings.ToLower(strings.TrimSpace(transition.To.Name)) == target {
			return transition.ID, true
		}
	}

	return "", false
}
