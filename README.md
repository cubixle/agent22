<p align="center">
  <img src=".github/assets/agent22.png"  />
</p>

<h1 align="center">Agent22</h1>

Agent22 is an opinionated Go automation service for handling JIRA tickets and pull/merge requests with a CLI coding agent. It runs as a continuous loop on a machine that has repository access, JIRA access, SCM access (Gitea or GitLab), and a supported coding-agent CLI available.

[![Watch the Agent22 demo](.github/assets/agent22.png)](.github/assets/agent22.webm)

## Why This Project Exists

Agent22 was created to reduce wasted tokens in workflows that are already well-defined and repetitive.

Instead of spending tokens re-explaining common command patterns (like everyday `git` workflows) or routing API work through extra CLI layers (like third-party wrappers), Agent22 executes those flows directly and consistently. The goal is to keep simple automation simple: connect to the APIs you need, run the known workflow, and avoid unnecessary agent chatter for tasks teams have been doing for years.

It also intentionally creates pull requests instead of pushing directly to `main`. Even if a maintainer can merge quickly, that review step acts as a useful nudge to keep developers engaged with what changed and why, so understanding of the codebase does not fade behind automation.

## Prerequisites

Before running Agent22, make sure:

- Go is installed if you plan to run with `go run .`.
- `git` is installed and available in your PATH.
- A supported coding-agent CLI is installed and available in your PATH.
- If using OpenCode, `opencode` is authenticated (`opencode auth`) for the account you want Agent22 to use.
- `.agent22.yml` exists in the working directory.

## Operating Modes

Agent22 supports two modes:

- `ticket mode` (default): polls JIRA and drives branch/PR automation from issues.
- `pull request mode` (`--pull-request-mode`): monitors open pull/merge requests and applies new review comments with the configured coding agent.

## Ticket Mode Flow

For each matching JIRA issue, Agent22:

1. Searches JIRA using your configured JQL.
2. Syncs the base branch locally.
3. Creates or checks out an issue branch (`<ISSUE-KEY>`).
4. Builds a coding-agent prompt from:
   - internal guardrail instructions,
   - issue summary,
   - issue description,
   - and local `AGENTS.md` content (if present in the working directory).
5. Runs implementation with `<agent-cli> run <prompt>`.
6. Runs a post-implementation coding-agent review pass.
7. Runs a coding-agent pass to apply valid review feedback.
8. Stages and commits local changes (if any non-sensitive files changed).
9. Pushes the issue branch.
10. Creates a pull/merge request (or reuses an existing open one).
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
4. If a PR has no review comments yet, runs a preemptive coding-agent review and posts it as a PR comment.
5. For each new reviewer comment, checks out and syncs the PR branch.
6. Builds a coding-agent prompt from guardrails, PR metadata, reviewer comment context, and local `AGENTS.md` (if present).
7. Runs `<agent-cli> run <prompt>`, stages/commits changes, and pushes the branch.
8. Checks out the configured base branch again.

## Console Output

- The JIRA task prompt is displayed in an ASCII box for easier visual scanning.
- Coding-agent execution uses a progress bar and spinner.

## Integrations

- JIRA
- Gitea
- GitLab
- GitHub
- OpenCode
- Cursor CLI coding agent

## Configuration

Copy `.agent22.example.yml` to `.agent22.yml` and fill in your values.

Example:

```yaml
work_provider: "jira"
scm_provider: "gitea" # supported: gitea, gitlab, github
coding_agent_provider: "opencode" # supported: opencode, cursor
wait_time_seconds: 30

# Optional when coding_agent_provider is "cursor"
cursor_cli_command: "cursor-agent"

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

gitlab_token: ""
gitlab_base_url: "https://gitlab.example.com"
gitlab_project_path: "group/subgroup/project"

github_owner: "your-github-org"
github_token: ""
github_base_url: "https://api.github.com"
```

Notes:

- `wait_time_seconds` defaults to `30` when omitted or <= 0.
- Polling in both modes uses exponential idle backoff (capped at 10 minutes) and resets after work is found.
- `work_provider` currently supports `jira`.
- `scm_provider` defaults to `gitea` when omitted.
- `coding_agent_provider` defaults to `opencode` when omitted.
- For `gitea`, configure `gitea_owner`, `gitea_token`, and `gitea_base_url`.
- For `gitlab`, configure `gitlab_token`, `gitlab_base_url`, and `gitlab_project_path`.
- For `github`, configure `github_owner`, `github_token`, and `git_repo` (optionally `github_base_url` for GitHub Enterprise).
- For `cursor`, configure `coding_agent_provider: "cursor"`; `cursor_cli_command` accepts an executable or executable with fixed args (for example `cursor-agent`, `bunx cursor-agent`, `/opt/cursor/bin/cursor-agent --profile ci`).
- Cursor CLI command expectations: the executable must be available in `PATH` (or use an absolute path), and Linux/macOS/CI runners can use wrapper commands when needed.

## Running

```bash
# Ticket mode (default)
go run .

# Pull request mode
go run . --pull-request-mode
```

## Docker

The container image builds `agent22` with multi-architecture support (`linux/amd64` and `linux/arm64`) and includes:

- `agent22` in `PATH`
- `git`
- `opencode` (pinned release artifact with SHA-256 verification)

Preflight checklist:

- `opencode auth` has been completed on the host.
- `.agent22.yml` exists in the repository root (or set `AGENT22_CONFIG_PATH`).
- SSH key and `known_hosts` are available (defaults: `$HOME/.ssh/id_ed25519` and `$HOME/.ssh/known_hosts`).
- OpenCode auth file exists (default: `$HOME/.local/share/opencode/auth.json`, override with `AGENT22_OPENCODE_AUTH_PATH`).

Build and run with Docker Compose:

```bash
docker compose build
docker compose run --rm agent22
```

The provided `docker-compose.yml` mounts:

- your repository to `/workspace`
- one SSH key and `known_hosts` (defaults shown above)
- your local `.agent22.yml` to `/workspace/.agent22.yml`
- your OpenCode auth file from `$HOME/.local/share/opencode/auth.json`

Optional: set `AGENT22_REPO_PATH`, `AGENT22_CONFIG_PATH`, `AGENT22_SSH_KEY_PATH`, `AGENT22_SSH_KNOWN_HOSTS_PATH`, and `AGENT22_OPENCODE_AUTH_PATH` to override defaults.
