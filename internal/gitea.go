package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type GiteaPullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type GiteaPullRequestResponse struct {
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

func CreateGiteaMergeRequest(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, token, owner, repo string,
	payload GiteaPullRequestRequest,
) (GiteaPullRequestResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return GiteaPullRequestResponse{}, fmt.Errorf("marshal gitea pull request payload: %w", err)
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/pulls",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GiteaPullRequestResponse{}, fmt.Errorf("create gitea request: %w", err)
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return GiteaPullRequestResponse{}, fmt.Errorf("execute gitea request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GiteaPullRequestResponse{}, fmt.Errorf("gitea pull request creation failed: status=%s body=%s", resp.Status, strings.TrimSpace(string(rawBody)))
	}

	var result GiteaPullRequestResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return GiteaPullRequestResponse{}, fmt.Errorf("decode gitea response: %w", err)
	}

	return result, nil
}

func FindOpenPullRequest(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, token, owner, repo, headBranch, baseBranch string,
) (*GiteaPullRequestResponse, error) {
	endpoint := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/pulls?state=open",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
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

	var pulls []GiteaPullRequestResponse
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
