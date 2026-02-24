// Package internal implements a GitLab merge-request provider.
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

const gitlabListPageSize = 50

var _ PullRequestProvider = (*GitLabClient)(nil)

type GitLabClient struct {
	httpClient  *http.Client
	baseURL     string
	token       string
	projectPath string
}

func NewGitLabClient(httpClient *http.Client, baseURL, token, projectPath string) *GitLabClient {
	return &GitLabClient{
		httpClient:  httpClient,
		baseURL:     baseURL,
		token:       token,
		projectPath: projectPath,
	}
}

type GitLabMergeRequestCreateRequest struct {
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

type GitLabMergeRequestResponse struct {
	IID          int    `json:"iid"`
	WebURL       string `json:"web_url"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

type GitLabMergeRequestNote struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type GitLabMergeRequestDiscussion struct {
	Notes []GitLabMergeRequestDiscussionNote `json:"notes"`
}

type GitLabMergeRequestDiscussionNote struct {
	ID        int64                       `json:"id"`
	Body      string                      `json:"body"`
	System    bool                        `json:"system"`
	Position  *GitLabMergeRequestPosition `json:"position"`
	CreatedAt string                      `json:"created_at"`
	UpdatedAt string                      `json:"updated_at"`
}

type GitLabMergeRequestPosition struct {
	NewPath string `json:"new_path"`
	OldPath string `json:"old_path"`
	NewLine int    `json:"new_line"`
	OldLine int    `json:"old_line"`
}

type gitlabMergeRequestCommentCreateRequest struct {
	Body string `json:"body"`
}

func (c *GitLabClient) FindOpenPullRequest(ctx context.Context, headBranch, baseBranch string) (*PullRequest, error) {
	query := url.Values{}
	query.Set("state", "opened")
	query.Set("source_branch", headBranch)
	query.Set("target_branch", baseBranch)

	baseEndpoint := c.projectEndpoint("merge_requests") + "?" + query.Encode()

	mrs, err := listAllGitLabPages[GitLabMergeRequestResponse](ctx, c, baseEndpoint, "merge request lookup")
	if err != nil {
		return nil, err
	}

	for _, mr := range mrs {
		if mr.SourceBranch == headBranch && mr.TargetBranch == baseBranch {
			converted := convertGitLabMergeRequest(mr)
			return &converted, nil
		}
	}

	return nil, nil
}

func (c *GitLabClient) CreatePullRequest(ctx context.Context, payload PullRequestCreateInput) (PullRequest, error) {
	mr, err := c.createMergeRequest(ctx, GitLabMergeRequestCreateRequest{
		Title:        payload.Title,
		Description:  payload.Body,
		SourceBranch: payload.Head,
		TargetBranch: payload.Base,
	})
	if err != nil {
		return PullRequest{}, err
	}

	return convertGitLabMergeRequest(mr), nil
}

func (c *GitLabClient) ListOpenPullRequests(ctx context.Context) ([]PullRequest, error) {
	baseEndpoint := c.projectEndpoint("merge_requests") + "?state=opened"

	mrs, err := listAllGitLabPages[GitLabMergeRequestResponse](ctx, c, baseEndpoint, "merge request list")
	if err != nil {
		return nil, err
	}

	result := make([]PullRequest, 0, len(mrs))
	for _, mr := range mrs {
		result = append(result, convertGitLabMergeRequest(mr))
	}

	return result, nil
}

func (c *GitLabClient) ListPullRequestComments(ctx context.Context, pullNumber int) ([]PullRequestComment, error) {
	notes, err := c.listMergeRequestNotes(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	discussions, err := c.listMergeRequestDiscussions(ctx, pullNumber)
	if err != nil {
		return nil, err
	}

	result := make([]PullRequestComment, 0, len(notes))
	seenNoteIDs := make(map[int64]struct{}, len(notes))

	for _, note := range notes {
		if note.System {
			continue
		}

		if !markGitLabCommentSeen(seenNoteIDs, note.ID) {
			continue
		}

		result = append(result, PullRequestComment{
			ID:        note.ID,
			Type:      "conversation",
			Body:      note.Body,
			CreatedAt: note.CreatedAt,
			UpdatedAt: note.UpdatedAt,
		})
	}

	for _, discussion := range discussions {
		for _, note := range discussion.Notes {
			if note.System {
				continue
			}

			if !markGitLabCommentSeen(seenNoteIDs, note.ID) {
				continue
			}

			commentType := "conversation"
			path := ""
			line := 0
			oldLine := 0

			if note.Position != nil {
				commentType = "line"

				path = strings.TrimSpace(note.Position.NewPath)
				if path == "" {
					path = strings.TrimSpace(note.Position.OldPath)
				}

				line = note.Position.NewLine
				if line == 0 {
					line = note.Position.OldLine
				}

				oldLine = note.Position.OldLine
			}

			result = append(result, PullRequestComment{
				ID:        note.ID,
				Type:      commentType,
				Body:      note.Body,
				Path:      path,
				Line:      line,
				OldLine:   oldLine,
				CreatedAt: note.CreatedAt,
				UpdatedAt: note.UpdatedAt,
			})
		}
	}

	return result, nil
}

func markGitLabCommentSeen(seen map[int64]struct{}, id int64) bool {
	if id <= 0 {
		return true
	}

	if _, exists := seen[id]; exists {
		return false
	}

	seen[id] = struct{}{}

	return true
}

func (c *GitLabClient) CreatePullRequestComment(ctx context.Context, pullNumber int, body string) error {
	payload := gitlabMergeRequestCommentCreateRequest{Body: body}
	endpoint := c.projectEndpoint(fmt.Sprintf("merge_requests/%d/notes", pullNumber))

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, nil, "merge request comment creation"); err != nil {
		return err
	}

	return nil
}

func (c *GitLabClient) createMergeRequest(ctx context.Context, payload GitLabMergeRequestCreateRequest) (GitLabMergeRequestResponse, error) {
	var result GitLabMergeRequestResponse

	endpoint := c.projectEndpoint("merge_requests")

	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, &result, "merge request creation"); err != nil {
		return GitLabMergeRequestResponse{}, err
	}

	return result, nil
}

func (c *GitLabClient) listMergeRequestNotes(ctx context.Context, pullNumber int) ([]GitLabMergeRequestNote, error) {
	baseEndpoint := c.projectEndpoint(fmt.Sprintf("merge_requests/%d/notes", pullNumber))

	return listAllGitLabPages[GitLabMergeRequestNote](ctx, c, baseEndpoint, "merge request notes lookup")
}

func (c *GitLabClient) listMergeRequestDiscussions(ctx context.Context, pullNumber int) ([]GitLabMergeRequestDiscussion, error) {
	baseEndpoint := c.projectEndpoint(fmt.Sprintf("merge_requests/%d/discussions", pullNumber))

	return listAllGitLabPages[GitLabMergeRequestDiscussion](ctx, c, baseEndpoint, "merge request discussions lookup")
}

func convertGitLabMergeRequest(mr GitLabMergeRequestResponse) PullRequest {
	return PullRequest{
		Number:     mr.IID,
		HTMLURL:    mr.WebURL,
		URL:        mr.URL,
		Title:      mr.Title,
		Body:       mr.Description,
		HeadBranch: mr.SourceBranch,
		BaseBranch: mr.TargetBranch,
	}
}

func listAllGitLabPages[T any](
	ctx context.Context,
	client *GitLabClient,
	endpoint, operation string,
) ([]T, error) {
	all := make([]T, 0)

	for page := 1; ; page++ {
		pagedEndpoint := withGitLabPagination(endpoint, page, gitlabListPageSize)

		var batch []T
		if err := client.doJSON(ctx, http.MethodGet, pagedEndpoint, nil, &batch, operation); err != nil {
			return nil, err
		}

		all = append(all, batch...)

		if len(batch) < gitlabListPageSize {
			break
		}
	}

	return all, nil
}

func withGitLabPagination(endpoint string, page, perPage int) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}

	return endpoint + separator + "page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
}

func (c *GitLabClient) projectEndpoint(resource string) string {
	base := fmt.Sprintf(
		"%s/api/v4/projects/%s",
		strings.TrimRight(c.baseURL, "/"),
		url.PathEscape(c.projectPath),
	)

	resource = strings.TrimPrefix(resource, "/")
	if resource == "" {
		return base
	}

	return base + "/" + resource
}

func (c *GitLabClient) doJSON(
	ctx context.Context,
	method, endpoint string,
	payload, out any,
	operation string,
) error {
	var body io.Reader = http.NoBody

	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal gitlab %s payload: %w", operation, err)
		}

		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create gitlab %s request: %w", operation, err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // endpoint is built from the configured GitLab base URL.
	if err != nil {
		return fmt.Errorf("execute gitlab %s request: %w", operation, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab %s failed: status=%s body=%s", operation, resp.Status, strings.TrimSpace(string(rawBody)))
	}

	if out == nil || len(rawBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(rawBody, out); err != nil {
		return fmt.Errorf("decode gitlab %s response: %w", operation, err)
	}

	return nil
}
