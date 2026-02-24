// Package internal defines coding-agent provider abstractions and CLI runners.
package internal

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

type CodingAgentProvider interface {
	Name() string
	DisplayName() string
	RunWithProgress(taskKey, input string) ([]byte, error)
}

const (
	codingAgentProviderOpencode = "opencode"
	codingAgentProviderCursor   = "cursor"
)

func NewCodingAgentProvider(config AgentConfig) (CodingAgentProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(config.CodingAgentProvider))

	switch provider {
	case "", codingAgentProviderOpencode:
		return &cliCodingAgentProvider{
			name:        codingAgentProviderOpencode,
			displayName: "OpenCode",
			commandName: "opencode",
			commandArgs: nil,
		}, nil
	case codingAgentProviderCursor:
		commandName, commandArgs, err := parseCLICommand(config.CursorCLICommand, "cursor-agent")
		if err != nil {
			return nil, fmt.Errorf("parse cursor_cli_command: %w", err)
		}

		return &cliCodingAgentProvider{
			name:        codingAgentProviderCursor,
			displayName: "Cursor",
			commandName: commandName,
			commandArgs: commandArgs,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported coding_agent_provider %q: expected opencode or cursor", config.CodingAgentProvider)
	}
}

type cliCodingAgentProvider struct {
	name        string
	displayName string
	commandName string
	commandArgs []string
}

func (p *cliCodingAgentProvider) Name() string {
	return p.name
}

func (p *cliCodingAgentProvider) DisplayName() string {
	return p.displayName
}

func (p *cliCodingAgentProvider) RunWithProgress(taskKey, input string) ([]byte, error) {
	return runCodingAgentCLIWithProgress(p.commandName, p.commandArgs, p.displayName, taskKey, input)
}

func runCodingAgentCLIWithProgress(commandName string, commandArgs []string, displayName, taskKey, input string) ([]byte, error) {
	spinner := []string{"|", "/", "-", "\\"}

	const barWidth = 20

	done := make(chan struct{})

	var output []byte

	var runErr error

	go func() {
		commandWithArgs := append(append([]string{}, commandArgs...), "run", input)
		output, runErr = exec.Command(commandName, commandWithArgs...).CombinedOutput()

		close(done)
	}()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	progress := 0
	spinIdx := 0
	startedAt := time.Now()

	for {
		select {
		case <-done:
			elapsed := time.Since(startedAt).Round(time.Second)
			fmt.Printf("\rRunning %s for task %s... [====================] 100%% (%s)\n", displayName, taskKey, elapsed)

			return output, runErr
		case <-ticker.C:
			if progress < 95 {
				progress++
			}

			filled := (progress * barWidth) / 100
			bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
			frame := spinnerFrame(spinner, spinIdx)
			fmt.Printf("\rRunning %s for task %s... [%s] %3d%% %s", displayName, taskKey, bar, progress, frame)

			spinIdx++
		}
	}
}

func parseCLICommand(raw, fallback string) (commandName string, commandArgs []string, err error) {
	commandLine := strings.TrimSpace(raw)
	if commandLine == "" {
		commandLine = fallback
	}

	tokens, err := splitCommandLine(commandLine)
	if err != nil {
		return "", nil, err
	}

	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("command must not be empty")
	}

	return tokens[0], tokens[1:], nil
}

func splitCommandLine(commandLine string) ([]string, error) {
	args := make([]string, 0, 4)

	var token strings.Builder

	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for _, r := range commandLine {
		switch {
		case escaped:
			token.WriteRune(r)

			escaped = false
		case r == '\\' && !inSingleQuote:
			escaped = true
		case r == '\'' && !inDoubleQuote:
			inSingleQuote = !inSingleQuote
		case r == '"' && !inSingleQuote:
			inDoubleQuote = !inDoubleQuote
		case unicode.IsSpace(r) && !inSingleQuote && !inDoubleQuote:
			if token.Len() > 0 {
				args = append(args, token.String())
				token.Reset()
			}
		default:
			token.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf("command ends with an unfinished escape")
	}

	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("command contains an unclosed quote")
	}

	if token.Len() > 0 {
		args = append(args, token.String())
	}

	return args, nil
}

func spinnerFrame(spinner []string, index int) string {
	if len(spinner) == 0 {
		return ""
	}

	return spinner[index%len(spinner)]
}
