# Contributing to Agent22

Thanks for contributing.

This project is intentionally small and opinionated. Please keep changes focused, incremental, and easy to review.

## Development Setup

1. Install Go (matching the version in `go.mod`).
2. Install `golangci-lint`.
3. Clone the repo and create your local config:

```bash
cp .agent22.example.yml .agent22.yml
```

4. Fill in `.agent22.yml` with local credentials (do not commit secrets).

## Workflow

1. Create a branch from `main`.
2. Make surgical changes for one concern at a time.
3. Run checks locally before opening a PR.
4. Open a pull request with a clear description of why the change is needed.

## Required Checks

Run these before submitting:

```bash
go test ./...
golangci-lint run
```

## Go Coding Guidelines

- Keep code idiomatic and simple.
- Group imports as: standard library, third-party, internal (use `goimports`).
- Use contextual wrapped errors, for example: `fmt.Errorf("load config: %w", err)`.
- Keep logs contextual and never log secrets or tokens.
- Prefer small functions; if a function grows too large, split it.
- Keep tests deterministic and table-driven when practical.
- Place tests in `_test` packages.

## Security and Secrets

- Never commit API tokens, passwords, PEM files, or `.env` secrets.
- Use local-only config for credentials.
- If you notice committed secrets, rotate them immediately and open a fix PR.

## Pull Request Expectations

- Keep PRs small and focused.
- Include test/lint updates when behavior changes.
- Explain tradeoffs and any follow-up work.
- Update docs/examples if configuration or workflow changes.

## Commit Messages

Use concise, descriptive messages that explain intent (the "why"), not just file changes.
