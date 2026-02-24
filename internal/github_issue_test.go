// Package internal_test verifies the GitHub issue provider behavior.
package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	agentinternal "gitea.lan/cubixle/agent/internal"
)

func TestGitHubIssueClientSearchAndMarkDone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var postedLabels []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertGitHubRequestHeaders(t, r)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/issues":
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("state query = %q, want %q", got, "open")
			}

			if got := r.URL.Query().Get("labels"); got != "ready" {
				t.Fatalf("labels query = %q, want %q", got, "ready")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id": 1, "number": 101, "title": "Implement provider", "body": "Build it", "labels": [{"name": "ready"}]},
				{"id": 2, "number": 202, "title": "PR mirror", "body": "", "labels": [{"name": "ready"}], "pull_request": {"url": "x"}}
			]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/issues/101/labels":
			defer r.Body.Close()

			var payload struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode label payload: %v", err)
			}

			postedLabels = payload.Labels
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:    []string{"ready"},
		DoneLabel: "done",
	})

	issues, err := client.SearchIssues(ctx)
	if err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}

	if issues[0].Key != "GH-101" {
		t.Fatalf("issue key = %q, want %q", issues[0].Key, "GH-101")
	}

	if err := client.MarkIssueDone(ctx, issues[0].Key); err != nil {
		t.Fatalf("MarkIssueDone() error = %v", err)
	}

	if !reflect.DeepEqual(postedLabels, []string{"done"}) {
		t.Fatalf("posted labels = %v, want %v", postedLabels, []string{"done"})
	}
}

func TestGitHubIssueClientMarkDoneStrictIssueKeyFormat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := agentinternal.NewGitHubIssueClient(&http.Client{}, "https://api.github.com", "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:    []string{"ready"},
		DoneLabel: "done",
	})

	tests := []struct {
		name          string
		issueKey      string
		expectedError string
	}{
		{
			name:          "plain number",
			issueKey:      "42",
			expectedError: "expected GH-<number>",
		},
		{
			name:          "hash prefix",
			issueKey:      "#42",
			expectedError: "expected GH-<number>",
		},
		{
			name:          "lowercase prefix",
			issueKey:      "gh-42",
			expectedError: "expected GH-<number>",
		},
		{
			name:          "non numeric suffix",
			issueKey:      "GH-abc",
			expectedError: "invalid syntax",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := client.MarkIssueDone(ctx, tc.issueKey)
			if err == nil || !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("MarkIssueDone() error = %v, want contains %q", err, tc.expectedError)
			}
		})
	}
}

func TestGitHubIssueClientMarkDoneUnknownIssueKeyFailsFast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertGitHubRequestHeaders(t, r)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id": 1, "number": 101, "title": "Issue 101", "body": "Body", "labels": [{"name": "ready"}]}
		]`))
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:    []string{"ready"},
		DoneLabel: "done",
	})

	if _, err := client.SearchIssues(ctx); err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}

	err := client.MarkIssueDone(ctx, "GH-999")
	if err == nil || !strings.Contains(err.Error(), `unknown github issue key "GH-999"`) {
		t.Fatalf("MarkIssueDone() error = %v, want unknown key error", err)
	}
}

func TestGitHubIssueClientMarkIssueInProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		inProgressLabel string
		wantCalls       int
		wantLabels      []string
	}{
		{
			name:            "adds configured in-progress label",
			inProgressLabel: "in-progress",
			wantCalls:       1,
			wantLabels:      []string{"in-progress"},
		},
		{
			name:            "skips when in-progress label empty",
			inProgressLabel: "   ",
			wantCalls:       0,
			wantLabels:      nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			calls := 0
			var postedLabels []string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Helper()
				assertGitHubRequestHeaders(t, r)

				switch r.Method {
				case http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[
						{"id": 1, "number": 101, "title": "Issue 101", "body": "Body", "labels": [{"name": "ready"}]}
					]`))
				case http.MethodPost:
					if r.URL.Path != "/repos/acme/repo/issues/101/labels" {
						t.Fatalf("request path = %q, want %q", r.URL.Path, "/repos/acme/repo/issues/101/labels")
					}

					defer r.Body.Close()

					var payload struct {
						Labels []string `json:"labels"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatalf("decode label payload: %v", err)
					}

					calls++
					postedLabels = payload.Labels
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[]`))
				default:
					t.Fatalf("unexpected method = %s", r.Method)
				}
			}))
			defer server.Close()

			client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
				Labels:          []string{"ready"},
				InProgressLabel: tc.inProgressLabel,
				DoneLabel:       "done",
			})

			if _, err := client.SearchIssues(ctx); err != nil {
				t.Fatalf("SearchIssues() error = %v", err)
			}

			if err := client.MarkIssueInProgress(ctx, " GH-101 "); err != nil {
				t.Fatalf("MarkIssueInProgress() error = %v", err)
			}

			if calls != tc.wantCalls {
				t.Fatalf("label post calls = %d, want %d", calls, tc.wantCalls)
			}

			if !reflect.DeepEqual(postedLabels, tc.wantLabels) {
				t.Fatalf("posted labels = %v, want %v", postedLabels, tc.wantLabels)
			}
		})
	}
}

func TestGitHubIssueClientSearchIssuesPagination(t *testing.T) {
	t.Parallel()

	const firstPageSize = 50

	ctx := context.Background()
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertGitHubRequestHeaders(t, r)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}

		if r.URL.Path != "/repos/acme/repo/issues" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/repos/acme/repo/issues")
		}

		if got := r.URL.Query().Get("state"); got != "open" {
			t.Fatalf("state query = %q, want %q", got, "open")
		}

		if got := r.URL.Query().Get("labels"); got != "ready" {
			t.Fatalf("labels query = %q, want %q", got, "ready")
		}

		page := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("per_page")
		if perPage != "50" {
			t.Fatalf("per_page query = %q, want %q", perPage, "50")
		}

		requests++
		w.Header().Set("Content-Type", "application/json")

		switch page {
		case "1":
			rows := make([]string, 0, firstPageSize)
			for i := 1; i <= firstPageSize; i++ {
				rows = append(rows, fmt.Sprintf(`{"id": %d, "number": %d, "title": "Issue %d", "body": "body", "labels": [{"name": "ready"}]}`,
					i,
					i,
					i,
				))
			}

			_, _ = w.Write([]byte("[" + strings.Join(rows, ",") + "]"))
		case "2":
			_, _ = w.Write([]byte(`[
				{"id": 101, "number": 101, "title": "Issue 101", "body": "body", "labels": [{"name": "ready"}]},
				{"id": 102, "number": 102, "title": "Issue 102", "body": "body", "labels": [{"name": "ready"}]}
			]`))
		default:
			t.Fatalf("unexpected page query = %q", page)
		}
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:    []string{"ready"},
		DoneLabel: "done",
	})

	issues, err := client.SearchIssues(ctx)
	if err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}

	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}

	if len(issues) != 52 {
		t.Fatalf("len(issues) = %d, want 52", len(issues))
	}

	if issues[0].Key != "GH-1" {
		t.Fatalf("first issue key = %q, want %q", issues[0].Key, "GH-1")
	}

	if issues[len(issues)-1].Key != "GH-102" {
		t.Fatalf("last issue key = %q, want %q", issues[len(issues)-1].Key, "GH-102")
	}
}

func TestNewGitHubIssueClientTrimAndDeduplicateLabelsCaseInsensitive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertGitHubRequestHeaders(t, r)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}

		gotLabels := r.URL.Query().Get("labels")
		if gotLabels != "Ready,DONE" {
			t.Fatalf("labels query = %q, want %q", gotLabels, "Ready,DONE")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels: []string{"Ready", "ready", " DONE ", "done"},
	})

	if _, err := client.SearchIssues(ctx); err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}
}

func TestGitHubIssueClientSearchIssuesErrorPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name          string
		baseURL       string
		httpClient    *http.Client
		handler       http.HandlerFunc
		expectedError string
	}{
		{
			name: "non 2xx status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`upstream`))
			},
			expectedError: "github issue search failed: status=502 Bad Gateway body=upstream",
		},
		{
			name: "malformed json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"broken":`))
			},
			expectedError: "decode github issue search response",
		},
		{
			name:          "transport error",
			baseURL:       "https://api.github.com",
			httpClient:    &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })},
			expectedError: "network down",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			baseURL := tc.baseURL
			httpClient := tc.httpClient

			if tc.handler != nil {
				server := httptest.NewServer(tc.handler)
				defer server.Close()
				baseURL = server.URL
				httpClient = server.Client()
			}

			client := agentinternal.NewGitHubIssueClient(httpClient, baseURL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
				Labels: []string{"ready"},
			})

			_, err := client.SearchIssues(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("SearchIssues() error = %v, want contains %q", err, tc.expectedError)
			}
		})
	}
}

func TestGitHubIssueClientMarkIssueLabelErrorPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name          string
		issueKey      string
		handler       http.HandlerFunc
		expectedError string
	}{
		{
			name:     "unknown issue key",
			issueKey: "GH-999",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[
					{"id": 1, "number": 101, "title": "Issue 101", "body": "Body", "labels": [{"name": "ready"}]}
				]`))
			},
			expectedError: `resolve github issue number for done label update: unknown github issue key "GH-999"`,
		},
		{
			name:     "label update non 2xx",
			issueKey: "GH-101",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[
						{"id": 1, "number": 101, "title": "Issue 101", "body": "Body", "labels": [{"name": "ready"}]}
					]`))
				case http.MethodPost:
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte("bad token"))
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			},
			expectedError: "github done label update failed: status=401 Unauthorized body=bad token",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Helper()
				assertGitHubRequestHeaders(t, r)
				tc.handler(w, r)
			}))
			defer server.Close()

			client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
				Labels:    []string{"ready"},
				DoneLabel: "done",
			})

			if _, err := client.SearchIssues(ctx); err != nil {
				t.Fatalf("SearchIssues() error = %v", err)
			}

			err := client.MarkIssueDone(ctx, tc.issueKey)
			if err == nil || !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("MarkIssueDone() error = %v, want contains %q", err, tc.expectedError)
			}
		})
	}
}

func assertGitHubRequestHeaders(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer token")
	}

	if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Fatalf("Accept header = %q, want %q", got, "application/vnd.github+json")
	}

	if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Fatalf("X-GitHub-Api-Version header = %q, want %q", got, "2022-11-28")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
