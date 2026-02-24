// Package internal verifies coding-agent configuration validation behavior.
package internal

import "testing"

func TestValidateCodingAgentConfigCursorCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cursorCommand string
		wantErr       bool
	}{
		{name: "empty command uses default", cursorCommand: "", wantErr: false},
		{name: "whitespace command uses default", cursorCommand: "   ", wantErr: false},
		{name: "command with args", cursorCommand: "bunx cursor-agent --profile prod", wantErr: false},
		{name: "unclosed quote fails", cursorCommand: "cursor-agent \"broken", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateCodingAgentConfig(AgentConfig{
				CodingAgentProvider: "cursor",
				CursorCLICommand:    tc.cursorCommand,
			})

			if tc.wantErr && err == nil {
				t.Fatalf("validateCodingAgentConfig() error = nil, want error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("validateCodingAgentConfig() error = %v, want nil", err)
			}
		})
	}
}
