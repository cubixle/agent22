# Agent22

Agent22 is an opinionated automation loop for handling JIRA tickets and Gitea pull requests with OpenCode.

The agent is intended to run continuously on a machine that has repository access, JIRA access, and `opencode` available.

## Prerequisites

Before running Agent22, make sure:

- `git` is installed and available in your PATH.
- `opencode` is installed and available in your PATH.
- `opencode` is authenticated (`opencode auth`) for the account you want Agent22 to use.

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
5. Runs `opencode run <prompt>`.
6. Stages and commits changes.
7. Pushes the issue branch.
8. Creates a Gitea pull request (or reuses an existing open one).
9. Transitions the JIRA issue to the configured done status.
10. Returns to `main` and continues.

### Ticket Filtering

Issues are skipped when:

- description is empty,
- description contains `human`,
- description contains `blocked`,
- description does not contain `tester`.

## Pull Request Mode Flow

When started with `--pull-request-mode`, Agent22:

1. Polls open pull requests every 10 seconds.
2. Tracks the latest seen comment per pull request.
3. For each new comment, checks out and syncs the PR branch.
4. Builds an OpenCode prompt from guardrails, PR metadata, reviewer comment, and local `AGENTS.md` (if present).
5. Runs `opencode run <prompt>`, stages/commits changes, and pushes the branch.
6. Checks out the configured base branch again.

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
***REMOVED***
***REMOVED***

***REMOVED***
  email: "you@example.com"
  api_token: ""
  jql: "project = EXAMPLE AND status = 'ready'"
  base_url: "https://your-org.atlassian.net"
***REMOVED***
***REMOVED***
***REMOVED***

***REMOVED***
***REMOVED***
***REMOVED***

***REMOVED***
gitea_token: ""
gitea_base_url: "https://gitea.example.com"
```

Notes:

- `wait_time_seconds` defaults to `30` when omitted or <= 0.
- Polling uses exponential backoff when no issues are found (capped at 10 minutes) and resets when issues are found.

## Running

```bash
# Ticket mode (default)
go run .

# Pull request mode
go run . --pull-request-mode
```
