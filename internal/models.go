// Package internal defines provider-agnostic domain models for the agent.
package internal

type Issue struct {
	ID          string
	Key         string
	Summary     string
	Status      string
	Description string
}

type IssueSearchRequest struct {
	Query      string
	MaxResults int
}

type PullRequest struct {
	Number     int
	HTMLURL    string
	URL        string
	Title      string
	Body       string
	HeadBranch string
	BaseBranch string
}

type PullRequestComment struct {
	ID        int64
	Type      string
	ReviewID  int64
	Body      string
	State     string
	Path      string
	Line      int
	OldLine   int
	CreatedAt string
	UpdatedAt string
}

type PullRequestCreateInput struct {
	Title string
	Body  string
	Head  string
	Base  string
}
