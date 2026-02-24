// Package internal_test verifies coding-agent provider selection behavior.
package internal_test

import (
	"strings"
	"testing"

	agentinternal "gitea.lan/cubixle/agent/internal"
)

func TestNewCodingAgentProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      string
		cursorCommand string
		wantName      string
		wantDisplay   string
		wantErr       string
	}{
		{
			name:        "default provider is opencode",
			provider:    "",
			wantName:    "opencode",
			wantDisplay: "OpenCode",
		},
		{
			name:        "explicit opencode provider",
			provider:    "opencode",
			wantName:    "opencode",
			wantDisplay: "OpenCode",
		},
		{
			name:        "cursor provider",
			provider:    "cursor",
			wantName:    "cursor",
			wantDisplay: "Cursor",
		},
		{
			name:          "cursor provider with explicit command override",
			provider:      "cursor",
			cursorCommand: "bunx cursor-agent",
			wantName:      "cursor",
			wantDisplay:   "Cursor",
		},
		{
			name:          "cursor provider with whitespace command uses default",
			provider:      "cursor",
			cursorCommand: "   ",
			wantName:      "cursor",
			wantDisplay:   "Cursor",
		},
		{
			name:          "cursor provider rejects malformed command",
			provider:      "cursor",
			cursorCommand: "cursor-agent \"bad",
			wantErr:       "parse cursor_cli_command: command contains an unclosed quote",
		},
		{
			name:     "unsupported provider",
			provider: "nope",
			wantErr:  "unsupported coding_agent_provider \"nope\": expected opencode or cursor",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, err := agentinternal.NewCodingAgentProvider(agentinternal.AgentConfig{
				CodingAgentProvider: tc.provider,
				CursorCLICommand:    tc.cursorCommand,
			})

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("NewCodingAgentProvider() error = %v, want contains %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("NewCodingAgentProvider() error = %v", err)
			}

			if provider.Name() != tc.wantName {
				t.Fatalf("provider.Name() = %q, want %q", provider.Name(), tc.wantName)
			}

			if provider.DisplayName() != tc.wantDisplay {
				t.Fatalf("provider.DisplayName() = %q, want %q", provider.DisplayName(), tc.wantDisplay)
			}
		})
	}
}
