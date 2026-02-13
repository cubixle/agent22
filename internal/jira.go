package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type JiraSearchRequest struct {
	JQL        string   `json:"jql"`
	MaxResults int      `json:"maxResults"`
	Fields     []string `json:"fields,omitempty"`
}

type JiraSearchResponse struct {
	Total  int         `json:"total"`
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

func SearchIssues(ctx context.Context, httpClient *http.Client, baseURL, email, apiToken string, payload JiraSearchRequest) ([]JiraIssue, error) {
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

	var result JiraSearchResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("decode jira response: %w", err)
	}

	return result.Issues, nil
}
