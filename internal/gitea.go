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

const giteaListPageSize = 50

type GiteaClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	owner      string
	repo       string
}

func NewGiteaClient(httpClient *http.Client, baseURL, token, owner, repo string) *GiteaClient {
	return &GiteaClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		token:      token,
		owner:      owner,
		repo:       repo,
	}
}

type GiteaPullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type GiteaPullRequestResponse struct {
	Number     int    `json:"number"`
	HTMLURL    string `json:"html_url"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"-"`
	BaseBranch string `json:"-"`
}

type GiteaIssueComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type GiteaPullReviewComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	OldLine   int    `json:"old_line"`
	ReviewID  int64  `json:"pull_request_review_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type GiteaPullReview struct {
	ID          int64  `json:"id"`
	Body        string `json:"body"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
}

type giteaIssueCommentCreateRequest struct {
	Body string `json:"body"`
}

func (r *GiteaPullRequestResponse) UnmarshalJSON(data []byte) error {
	type alias GiteaPullRequestResponse

	var aux struct {
		alias
		Head json.RawMessage `json:"head"`
		Base json.RawMessage `json:"base"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*r = GiteaPullRequestResponse(aux.alias)
	r.HeadBranch = decodeBranchRef(aux.Head)
	r.BaseBranch = decodeBranchRef(aux.Base)

	return nil
}

func (c *GiteaClient) FindOpenPullRequest(ctx context.Context, headBranch, baseBranch string) (*PullRequest, error) {
	baseEndpoint := c.repoEndpoint("pulls") + "?state=open"

	pulls, err := listAllGiteaPages[GiteaPullRequestResponse](ctx, c, baseEndpoint, "pull request lookup")
	if err != nil {
		return nil, err
	}

	for _, pr := range pulls {
		if pr.HeadBranch == headBranch && pr.BaseBranch == baseBranch {
			converted := convertGiteaPullRequest(pr)
			return &converted, nil
		}
	}

	return nil, nil
}

func (c *GiteaClient) CreatePullRequest(ctx context.Context, payload PullRequestCreateInput) (PullRequest, error) {
	pr, err := c.createMergeRequest(ctx, GiteaPullRequestRequest(payload))
	if err != nil {
		return PullRequest{}, err
	}

	return convertGiteaPullRequest(pr), nil
}

func (c *GiteaClient) ListOpenPullRequests(ctx context.Context) ([]PullRequest, error) {
	baseEndpoint := c.repoEndpoint("pulls") + "?state=open"

	pulls, err := listAllGiteaPages[GiteaPullRequestResponse](ctx, c, baseEndpoint, "pull request list")
	if err != nil {
		return nil, err
	}

	result := make([]PullRequest, 0, len(pulls))
	for _, pr := range pulls {
		result = append(result, convertGiteaPullRequest(pr))
	}

	return result, nil
}

func (c *GiteaClient) ListPullRequestComments(ctx context.Context, pullNumber int) ([]PullRequestComment, error) {
	issueComments, err := c.listIssueComments(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	reviews, err := c.listPullRequestReviews(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	result := make([]PullRequestComment, 0, len(issueComments)+len(reviews))
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
		reviewDetail, err := c.getPullRequestReview(ctx, pullNumber, review.ID)
		if err != nil {
			return nil, err
		}

		reviewToUse := review
		if reviewDetail.ID != 0 {
			reviewToUse = reviewDetail
		}

		result = append(result, PullRequestComment{
			ID:        reviewToUse.ID,
			Type:      "review",
			ReviewID:  reviewToUse.ID,
			Body:      reviewToUse.Body,
			State:     reviewToUse.State,
			CreatedAt: reviewToUse.SubmittedAt,
			UpdatedAt: reviewToUse.SubmittedAt,
		})

		reviewComments, err := c.listPullRequestReviewCommentsForReview(ctx, pullNumber, review.ID)
		if err != nil {
			return nil, err
		}

		for _, comment := range reviewComments {
			reviewID := comment.ReviewID
			if reviewID == 0 {
				reviewID = review.ID
			}

			result = append(result, PullRequestComment{
				ID:        comment.ID,
				Type:      "line",
				ReviewID:  reviewID,
				Body:      comment.Body,
				Path:      comment.Path,
				Line:      comment.Line,
				OldLine:   comment.OldLine,
				CreatedAt: comment.CreatedAt,
				UpdatedAt: comment.UpdatedAt,
			})
		}
	}

	return result, nil
}

func (c *GiteaClient) CreatePullRequestComment(ctx context.Context, pullNumber int, body string) error {
	payload := giteaIssueCommentCreateRequest{Body: body}
	endpoint := c.repoEndpoint(fmt.Sprintf("issues/%d/comments", pullNumber))

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, nil, "pull request comment creation"); err != nil {
		return err
	}

	return nil
}

func (c *GiteaClient) createMergeRequest(ctx context.Context, payload GiteaPullRequestRequest) (GiteaPullRequestResponse, error) {
	var result GiteaPullRequestResponse

	endpoint := c.repoEndpoint("pulls")

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, &result, "pull request creation"); err != nil {
		return GiteaPullRequestResponse{}, err
	}

	return result, nil
}

func (c *GiteaClient) listIssueComments(ctx context.Context, pullNumber int) ([]GiteaIssueComment, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("issues/%d/comments", pullNumber))

	return listAllGiteaPages[GiteaIssueComment](ctx, c, baseEndpoint, "pull request comments lookup")
}

func (c *GiteaClient) listPullRequestReviewCommentsForReview(ctx context.Context, pullNumber int, reviewID int64) ([]GiteaPullReviewComment, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("pulls/%d/reviews/%d/comments", pullNumber, reviewID))

	comments, err := listAllGiteaPages[GiteaPullReviewComment](ctx, c, baseEndpoint, "pull request review comments lookup")
	if err != nil {
		if isGiteaNotFoundError(err) {
			return nil, nil
		}

		return nil, err
	}

	return comments, nil
}

func (c *GiteaClient) listPullRequestReviews(ctx context.Context, pullNumber int) ([]GiteaPullReview, error) {
	baseEndpoint := c.repoEndpoint(fmt.Sprintf("pulls/%d/reviews", pullNumber))

	reviews, err := listAllGiteaPages[GiteaPullReview](ctx, c, baseEndpoint, "pull request reviews lookup")
	if err != nil {
		if isGiteaNotFoundError(err) {
			return nil, nil
		}

		return nil, err
	}

	return reviews, nil
}

func (c *GiteaClient) getPullRequestReview(ctx context.Context, pullNumber int, reviewID int64) (GiteaPullReview, error) {
	var review GiteaPullReview

	endpoint := c.repoEndpoint(fmt.Sprintf("pulls/%d/reviews/%d", pullNumber, reviewID))

	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &review, "pull request review lookup"); err != nil {
		if isGiteaNotFoundError(err) {
			return GiteaPullReview{}, nil
		}

		return GiteaPullReview{}, err
	}

	return review, nil
}

func convertGiteaPullRequest(pr GiteaPullRequestResponse) PullRequest {
	return PullRequest{ //nolint:staticcheck // ignore
		Number:     pr.Number,
		HTMLURL:    pr.HTMLURL,
		URL:        pr.URL,
		Title:      pr.Title,
		Body:       pr.Body,
		HeadBranch: pr.HeadBranch,
		BaseBranch: pr.BaseBranch,
	}
}

func listAllGiteaPages[T any](
	ctx context.Context,
	client *GiteaClient,
	endpoint, operation string,
) ([]T, error) {
	all := make([]T, 0)

	for page := 1; ; page++ {
		pagedEndpoint := withGiteaPagination(endpoint, page, giteaListPageSize)

		var batch []T
		if err := client.doJSON(ctx, http.MethodGet, pagedEndpoint, nil, &batch, operation); err != nil {
			return nil, err
		}

		all = append(all, batch...)

		if len(batch) < giteaListPageSize {
			break
		}
	}

	return all, nil
}

func withGiteaPagination(endpoint string, page, limit int) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}

	return endpoint + separator + "page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(limit)
}

func isGiteaNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "status=404")
}

func (c *GiteaClient) repoEndpoint(resource string) string {
	return fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/%s",
		strings.TrimRight(c.baseURL, "/"),
		url.PathEscape(c.owner),
		url.PathEscape(c.repo),
		resource,
	)
}

func (c *GiteaClient) doJSON(
	ctx context.Context,
	method, endpoint string,
	payload, out any,
	operation string,
) error {
	var body io.Reader = http.NoBody

	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal gitea %s payload: %w", operation, err)
		}

		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create gitea %s request: %w", operation, err)
	}

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute gitea %s request: %w", operation, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitea %s failed: status=%s body=%s", operation, resp.Status, strings.TrimSpace(string(rawBody)))
	}

	if out == nil || len(rawBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(rawBody, out); err != nil {
		return fmt.Errorf("decode gitea %s response: %w", operation, err)
	}

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
