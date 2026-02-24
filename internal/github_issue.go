// Package internal implements a GitHub issue-based work provider.
package internal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const gitHubIssueKeyPrefix = "GH-"

var _ IssueProvider = (*GitHubIssueClient)(nil)

type GitHubWorkConfig struct {
	Labels          []string `yaml:"labels"`
	InProgressLabel string   `yaml:"in_progress_label"`
	DoneLabel       string   `yaml:"done_label"`
}

type GitHubIssueClient struct {
	client          *GitHubClient
	labels          []string
	inProgressLabel string
	doneLabel       string
	issueNumbers    map[string]int
}

type gitHubIssueResponse struct {
	ID          int64              `json:"id"`
	Number      int                `json:"number"`
	Title       string             `json:"title"`
	Body        string             `json:"body"`
	Labels      []gitHubIssueLabel `json:"labels"`
	PullRequest *struct{}          `json:"pull_request,omitempty"`
}

type gitHubIssueLabel struct {
	Name string `json:"name"`
}

type gitHubIssueLabelsRequest struct {
	Labels []string `json:"labels"`
}

func NewGitHubIssueClient(
	httpClient *http.Client,
	baseURL, token, owner, repo string,
	cfg *GitHubWorkConfig,
) *GitHubIssueClient {
	labels := make([]string, 0)
	inProgressLabel := ""
	doneLabel := ""

	if cfg != nil {
		labels = trimAndDeduplicateLabels(cfg.Labels)
		inProgressLabel = strings.TrimSpace(cfg.InProgressLabel)
		doneLabel = strings.TrimSpace(cfg.DoneLabel)
	}

	return &GitHubIssueClient{
		client:          NewGitHubClient(httpClient, baseURL, token, owner, repo),
		labels:          labels,
		inProgressLabel: inProgressLabel,
		doneLabel:       doneLabel,
		issueNumbers:    make(map[string]int),
	}
}

func (c *GitHubIssueClient) SearchIssues(ctx context.Context) ([]Issue, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("labels", strings.Join(c.labels, ","))

	baseEndpoint := c.client.repoEndpoint("issues") + "?" + query.Encode()

	issues, err := listAllGitHubPages[gitHubIssueResponse](ctx, c.client, baseEndpoint, "issue search")
	if err != nil {
		return nil, err
	}

	result := make([]Issue, 0, len(issues))
	issueNumbers := make(map[string]int, len(issues))

	for _, issue := range issues {
		if issue.PullRequest != nil {
			continue
		}

		key := formatGitHubIssueKey(issue.Number)
		issueNumbers[key] = issue.Number

		result = append(result, Issue{
			ID:          strconv.Itoa(issue.Number),
			Key:         key,
			Summary:     strings.TrimSpace(issue.Title),
			Status:      formatGitHubIssueStatus(issue.Labels),
			Description: strings.TrimSpace(issue.Body),
		})
	}

	c.issueNumbers = issueNumbers

	return result, nil
}

func (c *GitHubIssueClient) MarkIssueInProgress(ctx context.Context, issueKey string) error {
	if strings.TrimSpace(c.inProgressLabel) == "" {
		return nil
	}

	if err := c.addIssueLabel(ctx, issueKey, c.inProgressLabel, "in-progress label update"); err != nil {
		return err
	}

	return nil
}

func (c *GitHubIssueClient) MarkIssueDone(ctx context.Context, issueKey string) error {
	if err := c.addIssueLabel(ctx, issueKey, c.doneLabel, "done label update"); err != nil {
		return err
	}

	return nil
}

func (c *GitHubIssueClient) addIssueLabel(ctx context.Context, issueKey, label, operation string) error {
	trimmedLabel := strings.TrimSpace(label)
	if trimmedLabel == "" {
		return nil
	}

	issueNumber, err := c.resolveIssueNumber(issueKey)
	if err != nil {
		return fmt.Errorf("resolve github issue number for %s: %w", operation, err)
	}

	endpoint := c.client.repoEndpoint(fmt.Sprintf("issues/%d/labels", issueNumber))

	payload := gitHubIssueLabelsRequest{Labels: []string{trimmedLabel}}

	if err := c.client.doJSON(ctx, http.MethodPost, endpoint, payload, nil, operation); err != nil {
		return err
	}

	return nil
}

func (c *GitHubIssueClient) resolveIssueNumber(issueKey string) (int, error) {
	normalizedKey, err := normalizeGitHubIssueKey(issueKey)
	if err != nil {
		return 0, err
	}

	if number, ok := c.issueNumbers[normalizedKey]; ok {
		return number, nil
	}

	return 0, fmt.Errorf("unknown github issue key %q: not found in current search results", issueKey)
}

func normalizeGitHubIssueKey(issueKey string) (string, error) {
	trimmed := strings.TrimSpace(issueKey)
	if !strings.HasPrefix(trimmed, gitHubIssueKeyPrefix) {
		return "", fmt.Errorf("parse github issue key %q: expected %s<number>", issueKey, gitHubIssueKeyPrefix)
	}

	numberPart := strings.TrimSpace(strings.TrimPrefix(trimmed, gitHubIssueKeyPrefix))

	number, err := strconv.Atoi(numberPart)
	if err != nil {
		return "", fmt.Errorf("parse github issue key %q: %w", issueKey, err)
	}

	return formatGitHubIssueKey(number), nil
}

func formatGitHubIssueKey(issueNumber int) string {
	return fmt.Sprintf("%s%d", gitHubIssueKeyPrefix, issueNumber)
}

func formatGitHubIssueStatus(labels []gitHubIssueLabel) string {
	statusLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		name := strings.TrimSpace(label.Name)
		if name == "" {
			continue
		}

		statusLabels = append(statusLabels, name)
	}

	if len(statusLabels) == 0 {
		return "open"
	}

	return strings.Join(statusLabels, ", ")
}

func trimAndDeduplicateLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(labels))

	result := make([]string, 0, len(labels))

	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}

		normalized := strings.ToLower(trimmed)

		if _, exists := seen[normalized]; exists {
			continue
		}

		seen[normalized] = struct{}{}

		result = append(result, trimmed)
	}

	return result
}
