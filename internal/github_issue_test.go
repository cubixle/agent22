// Package internal_test verifies the GitHub issue provider behavior.
package internal_test

import (
	"context"
	"encoding/json"
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

func TestGitHubIssueClientMarkDoneParsesNumericKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	receivedPath := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}

		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:    []string{"ready"},
		DoneLabel: "done",
	})

	if err := client.MarkIssueDone(ctx, "42"); err != nil {
		t.Fatalf("MarkIssueDone() error = %v", err)
	}

	if receivedPath != "/repos/acme/repo/issues/42/labels" {
		t.Fatalf("request path = %q, want %q", receivedPath, "/repos/acme/repo/issues/42/labels")
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

				if r.Method != http.MethodPost {
					t.Fatalf("method = %s, want POST", r.Method)
				}

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
			}))
			defer server.Close()

			client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
				Labels:          []string{"ready"},
				InProgressLabel: tc.inProgressLabel,
				DoneLabel:       "done",
			})

			if err := client.MarkIssueInProgress(ctx, "GH-101"); err != nil {
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

func TestGitHubIssueClientSearchIssuesSkipsInProgressAndDone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}

		if r.URL.Path != "/repos/acme/repo/issues" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/repos/acme/repo/issues")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id": 1, "number": 101, "state": "open", "title": "Ready issue", "body": "body", "labels": [{"name": "ready"}]},
			{"id": 2, "number": 102, "state": "open", "title": "In progress issue", "body": "body", "labels": [{"name": "ready"}, {"name": " in-progress "}]},
			{"id": 3, "number": 103, "state": "open", "title": "Done issue", "body": "body", "labels": [{"name": "READY"}, {"name": "DONE"}]},
			{"id": 4, "number": 104, "state": "closed", "title": "Closed issue", "body": "body", "labels": [{"name": "ready"}]}
		]`))
	}))
	defer server.Close()

	client := agentinternal.NewGitHubIssueClient(server.Client(), server.URL, "token", "acme", "repo", &agentinternal.GitHubWorkConfig{
		Labels:          []string{"ready"},
		InProgressLabel: "in-progress",
		DoneLabel:       "done",
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
}
