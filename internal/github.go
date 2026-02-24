// Package internal implements a GitHub pull-request provider.
package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const gitHubListPageSize = 50

var _ PullRequestProvider = (*GitHubClient)(nil)

type GitHubClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	owner      string
	repo       string
}

func NewGitHubClient(httpClient *http.Client, baseURL, token, owner, repo string) *GitHubClient {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = "https://api.github.com"
	}

	return &GitHubClient{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(base, "/"),
		token:      token,
		owner:      owner,
		repo:       repo,
	}
}

type gitHubPullRequestCreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type GitHubPullRequestResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type GitHubIssueComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type GitHubPullReview struct {
	ID          int64  `json:"id"`
	Body        string `json:"body"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
}

type GitHubPullReviewComment struct {
	ID        int64  `json:"id"`
	ReviewID  int64  `json:"pull_request_review_id"`
	Body      string `json:"body"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	OldLine   int    `json:"original_line"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type gitHubIssueCommentCreateRequest struct {
	Body string `json:"body"`
}

func (c *GitHubClient) FindOpenPullRequest(ctx context.Context, headBranch, baseBranch string) (*PullRequest, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", c.owner+":"+headBranch)
	query.Set("base", baseBranch)

	baseEndpoint := c.repoEndpoint("pulls") + "?" + query.Encode()

	pulls, err := listAllGitHubPages[GitHubPullRequestResponse](ctx, c, baseEndpoint, "pull request lookup")
	if err != nil {
		return nil, err
	}

	for _, pr := range pulls {
		if pr.Head.Ref == headBranch && pr.Base.Ref == baseBranch {
			converted := convertGitHubPullRequest(pr)
			return &converted, nil
		}
	}

	return nil, nil
}

func (c *GitHubClient) CreatePullRequest(ctx context.Context, payload PullRequestCreateInput) (PullRequest, error) {
	pr, err := c.createPullRequest(ctx, gitHubPullRequestCreateRequest(payload))
	if err != nil {
		return PullRequest{}, err
	}

	return convertGitHubPullRequest(pr), nil
}

func (c *GitHubClient) ListOpenPullRequests(ctx context.Context) ([]PullRequest, error) {
	baseEndpoint := c.repoEndpoint("pulls") + "?state=open"

	pulls, err := listAllGitHubPages[GitHubPullRequestResponse](ctx, c, baseEndpoint, "pull request list")
	if err != nil {
		return nil, err
	}

	result := make([]PullRequest, 0, len(pulls))
	for _, pr := range pulls {
		result = append(result, convertGitHubPullRequest(pr))
	}

	return result, nil
}

func (c *GitHubClient) ListPullRequestComments(ctx context.Context, pullNumber int) ([]PullRequestComment, error) {
	issueComments, err := c.listIssueComments(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	reviews, err := c.listPullRequestReviews(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	reviewComments, err := c.listPullRequestReviewComments(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	result := make([]PullRequestComment, 0, len(issueComments)+len(reviews)+len(reviewComments))

	for _, comment := range issueComments {
		result = append(result, PullRequestComment{
			ID:        comment.ID,
			Type:      "conversation",
			Body:      comment.Body,
			CreatedAt: comment.CreatedAt,
			UpdatedAt: comment.UpdatedAt,
		})
	}

	for _, review := range reviews {
		result = append(result, PullRequestComment{
			ID:        review.ID,
			Type:      "review",
			ReviewID:  review.ID,
			Body:      review.Body,
			State:     review.State,
			CreatedAt: review.SubmittedAt,
			UpdatedAt: review.SubmittedAt,
		})
	}

	for _, comment := range reviewComments {
		result = append(result, PullRequestComment{
			ID:        comment.ID,
			Type:      "line",
			ReviewID:  comment.ReviewID,
			Body:      comment.Body,
			Path:      comment.Path,
			Line:      comment.Line,
			OldLine:   comment.OldLine,
			CreatedAt: comment.CreatedAt,
			UpdatedAt: comment.UpdatedAt,
		})
	}

	return result, nil
}

func (c *GitHubClient) CreatePullRequestComment(ctx context.Context, pullNumber int, body string) error {
	payload := gitHubIssueCommentCreateRequest{Body: body}
	endpoint := c.repoEndpoint(fmt.Sprintf("issues/%d/comments", pullNumber))

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, nil, "pull request comment creation"); err != nil {
		return err
	}

	return nil
}

func (c *GitHubClient) createPullRequest(ctx context.Context, payload gitHubPullRequestCreateRequest) (GitHubPullRequestResponse, error) {
	var result GitHubPullRequestResponse

	endpoint := c.repoEndpoint("pulls")

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, &result, "pull request creation"); err != nil {
		return GitHubPullRequestResponse{}, err
	}

	return result, nil
}

func (c *GitHubClient) listIssueComments(ctx context.Context, pullNumber int) ([]GitHubIssueComment, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("issues/%d/comments", pullNumber))

	return listAllGitHubPages[GitHubIssueComment](ctx, c, baseEndpoint, "pull request comments lookup")
}

func (c *GitHubClient) listPullRequestReviews(ctx context.Context, pullNumber int) ([]GitHubPullReview, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("pulls/%d/reviews", pullNumber))

	return listAllGitHubPages[GitHubPullReview](ctx, c, baseEndpoint, "pull request reviews lookup")
}

func (c *GitHubClient) listPullRequestReviewComments(ctx context.Context, pullNumber int) ([]GitHubPullReviewComment, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("pulls/%d/comments", pullNumber))

	return listAllGitHubPages[GitHubPullReviewComment](ctx, c, baseEndpoint, "pull request review comments lookup")
}

func convertGitHubPullRequest(pr GitHubPullRequestResponse) PullRequest {
	return PullRequest{
		Number:     pr.Number,
		HTMLURL:    pr.HTMLURL,
		URL:        pr.URL,
		Title:      pr.Title,
		Body:       pr.Body,
		HeadBranch: pr.Head.Ref,
		BaseBranch: pr.Base.Ref,
	}
}

func listAllGitHubPages[T any](
	ctx context.Context,
	client *GitHubClient,
	endpoint, operation string,
) ([]T, error) {
	all := make([]T, 0)

	for page := 1; ; page++ {
		pagedEndpoint := withGitHubPagination(endpoint, page, gitHubListPageSize)

		var batch []T
		if err := client.doJSON(ctx, http.MethodGet, pagedEndpoint, nil, &batch, operation); err != nil {
			return nil, err
		}

		all = append(all, batch...)

		if len(batch) < gitHubListPageSize {
			break
		}
	}

	return all, nil
}

func withGitHubPagination(endpoint string, page, perPage int) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}

	return endpoint + separator + "page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
}

func (c *GitHubClient) repoEndpoint(resource string) string {
	resource = strings.TrimPrefix(resource, "/")

	return fmt.Sprintf(
		"%s/repos/%s/%s/%s",
		c.baseURL,
		url.PathEscape(c.owner),
		url.PathEscape(c.repo),
		resource,
	)
}

func (c *GitHubClient) doJSON(
	ctx context.Context,
	method, endpoint string,
	payload, out any,
	operation string,
) error {
	var body io.Reader = http.NoBody

	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal github %s payload: %w", operation, err)
		}

		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create github %s request: %w", operation, err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // endpoint is built from the configured GitHub base URL.
	if err != nil {
		return fmt.Errorf("execute github %s request: %w", operation, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github %s failed: status=%s body=%s", operation, resp.Status, strings.TrimSpace(string(rawBody)))
	}

	if out == nil || len(rawBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(rawBody, out); err != nil {
		return fmt.Errorf("decode github %s response: %w", operation, err)
	}

	return nil
}
