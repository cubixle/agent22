<p align="center">
  <img src=".github/assets/agent22.png"  />
</p>

<h1 align="center">Agent22</h1>

Agent22 is an opinionated Go automation service for handling JIRA tickets and Gitea pull requests with OpenCode. It runs as a continuous loop on a machine that has repository access, JIRA access, Gitea access, and `opencode` available.

[![Watch the Agent22 demo](.github/assets/agent22.png)](.github/assets/agent22.mp4)

## Why This Project Exists

Agent22 was created to reduce wasted tokens in workflows that are already well-defined and repetitive.

Instead of spending tokens re-explaining common command patterns (like everyday `git` workflows) or routing API work through extra CLI layers (like third-party wrappers), Agent22 executes those flows directly and consistently. The goal is to keep simple automation simple: connect to the APIs you need, run the known workflow, and avoid unnecessary agent chatter for tasks teams have been doing for years.

It also intentionally creates pull requests instead of pushing directly to `main`. Even if a maintainer can merge quickly, that review step acts as a useful nudge to keep developers engaged with what changed and why, so understanding of the codebase does not fade behind automation.

## Prerequisites

Before running Agent22, make sure:

- Go is installed if you plan to run with `go run .`.
- `git` is installed and available in your PATH.
- `opencode` is installed and available in your PATH.
- `opencode` is authenticated (`opencode auth`) for the account you want Agent22 to use.
- `.agent22.yml` exists in the working directory.

## Operating Modes

Agent22 supports two modes:

- `ticket mode` (default): polls JIRA and drives branch/PR automation from issues.
- `pull request mode` (`--pull-request-mode`): monitors open Gitea pull requests and applies new review comments with OpenCode.

## Ticket Mode Flow

For each matching JIRA issue, Agent22:

1. Searches JIRA using your configured JQL.
2. Syncs the base branch locally.
3. Creates or checks out an issue branch (`<ISSUE-KEY>`).
4. Builds an OpenCode prompt from:
   - internal guardrail instructions,
   - issue summary,
   - issue description,
   - and local `AGENTS.md` content (if present in the working directory).
5. Runs implementation with `opencode run <prompt>`.
6. Runs a post-implementation OpenCode review pass.
7. Runs an OpenCode pass to apply valid review feedback.
8. Stages and commits local changes (if any non-sensitive files changed).
9. Pushes the issue branch.
10. Creates a Gitea pull request (or reuses an existing open one).
11. Transitions the JIRA issue to the configured done status.
12. Returns to the configured base branch and continues.

### Ticket Filtering

Issues are skipped when:

- description is empty,
- description contains `human`,
- description contains `blocked`.

## Pull Request Mode Flow

When started with `--pull-request-mode`, Agent22:

1. Polls open pull requests using `wait_time_seconds` with exponential idle backoff.
2. Loads conversation comments, review comments, and line comments.
3. Filters operational noise (for example, duplicated PR body comments and agent-generated preemptive comments).
4. If a PR has no review comments yet, runs a preemptive OpenCode review and posts it as a PR comment.
5. For each new reviewer comment, checks out and syncs the PR branch.
6. Builds an OpenCode prompt from guardrails, PR metadata, reviewer comment context, and local `AGENTS.md` (if present).
7. Runs `opencode run <prompt>`, stages/commits changes, and pushes the branch.
8. Checks out the configured base branch again.

## Console Output

- The JIRA task prompt is displayed in an ASCII box for easier visual scanning.
- OpenCode execution uses a progress bar and spinner.

## Integrations

- JIRA
- Gitea
- OpenCode

## Configuration

Copy `.agent22.example.yml` to `.agent22.yml` and fill in your values.

Example:

```yaml
work_provider: "jira"
wait_time_seconds: 30

jira:
  email: "you@example.com"
  api_token: ""
  jql: "project = EXAMPLE AND status = 'ready'"
  base_url: "https://your-org.atlassian.net"
  max_results: 25
  pr_status: "PR Ready"
  done_status: "Done"

git_repo: "your-repo"
git_base_branch: "main"
git_remote: "origin"

gitea_owner: "your-gitea-owner"
gitea_token: ""
gitea_base_url: "https://gitea.example.com"
```

Notes:

- `wait_time_seconds` defaults to `30` when omitted or <= 0.
- Polling in both modes uses exponential idle backoff (capped at 10 minutes) and resets after work is found.
- `work_provider` currently supports `jira`.

## Running

```bash
# Ticket mode (default)
go run .

# Pull request mode
go run . --pull-request-mode
```
