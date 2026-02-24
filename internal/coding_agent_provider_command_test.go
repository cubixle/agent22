// Package internal_test verifies coding-agent command execution wiring.
package internal_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	agentinternal "gitea.lan/cubixle/agent/internal"
)

func TestCursorProviderCommandWithArgs(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell script test is not portable to Windows")
	}

	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args.txt")
	scriptPath := filepath.Join(tempDir, "capture-args.sh")

	script := "#!/bin/sh\nout=\"$1\"\nshift\nprintf '%s\\n' \"$@\" > \"$out\"\nprintf 'ok'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	provider, err := agentinternal.NewCodingAgentProvider(agentinternal.AgentConfig{
		CodingAgentProvider: "cursor",
		CursorCLICommand:    fmt.Sprintf("%q %q \"%s\"", scriptPath, argsPath, "profile prod"),
	})
	if err != nil {
		t.Fatalf("NewCodingAgentProvider() error = %v", err)
	}

	output, err := provider.RunWithProgress("AG-3", "Implement Cursor provider")
	if err != nil {
		t.Fatalf("RunWithProgress() error = %v, output = %s", err, string(output))
	}

	if got := string(output); got != "ok" {
		t.Fatalf("RunWithProgress() output = %q, want %q", got, "ok")
	}

	argsFileBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	gotArgs := strings.Split(strings.TrimSpace(string(argsFileBytes)), "\n")
	wantArgs := []string{"profile prod", "run", "Implement Cursor provider"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("captured args = %#v, want %#v", gotArgs, wantArgs)
	}
}
