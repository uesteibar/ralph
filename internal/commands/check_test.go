package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_Success_PrintsPassAndLogPath(t *testing.T) {
	dir := realPath(t, t.TempDir())

	var stdout bytes.Buffer
	exitCode := checkRun([]string{"echo", "hello"}, dir, &stdout)

	output := stdout.String()

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(output, "PASS: echo hello") {
		t.Errorf("expected PASS line, got: %s", output)
	}
	if !strings.Contains(output, "Full log:") {
		t.Errorf("expected 'Full log:' in output, got: %s", output)
	}

	// Extract log path and verify file contents.
	logPath := extractLogPath(t, output)
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logData), "hello") {
		t.Errorf("expected log file to contain 'hello', got: %s", string(logData))
	}
}

func TestCheck_Failure_PrintsFailWithTail(t *testing.T) {
	dir := realPath(t, t.TempDir())

	var stdout bytes.Buffer
	exitCode := checkRun([]string{"sh", "-c", "echo failure && exit 1"}, dir, &stdout)

	output := stdout.String()

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(output, "FAIL: sh -c echo failure && exit 1") {
		t.Errorf("expected FAIL line, got: %s", output)
	}
	if !strings.Contains(output, "--- last 20 lines ---") {
		t.Errorf("expected tail header, got: %s", output)
	}
	if !strings.Contains(output, "failure") {
		t.Errorf("expected output to contain 'failure', got: %s", output)
	}
	if !strings.Contains(output, "Full log:") {
		t.Errorf("expected 'Full log:' in output, got: %s", output)
	}
}

func TestCheck_TailFlag_LimitsOutputLines(t *testing.T) {
	dir := realPath(t, t.TempDir())

	// Write a script that outputs 50 numbered lines and exits 1.
	scriptPath := filepath.Join(dir, "gen.sh")
	scriptContent := "#!/bin/sh\ni=1\nwhile [ \"$i\" -le 50 ]; do\n  echo \"$i\"\n  i=$((i+1))\ndone\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	exitCode := checkRun([]string{"--tail", "5", scriptPath}, dir, &stdout)

	output := stdout.String()

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(output, "--- last 5 lines ---") {
		t.Errorf("expected '--- last 5 lines ---', got: %s", output)
	}

	// Extract the tail section between the header and the "Full log:" line.
	tailStart := strings.Index(output, "--- last 5 lines ---\n")
	tailEnd := strings.Index(output, "Full log:")
	if tailStart < 0 || tailEnd < 0 {
		t.Fatalf("could not find tail section in output: %s", output)
	}
	tailSection := output[tailStart+len("--- last 5 lines ---\n") : tailEnd]
	tailLines := strings.Split(strings.TrimRight(tailSection, "\n"), "\n")

	if len(tailLines) != 5 {
		t.Errorf("expected exactly 5 tail lines, got %d: %v", len(tailLines), tailLines)
	}
	// The last 5 lines should be 46-50.
	for i, want := range []string{"46", "47", "48", "49", "50"} {
		if i < len(tailLines) && strings.TrimSpace(tailLines[i]) != want {
			t.Errorf("tail line %d = %q, want %q", i, tailLines[i], want)
		}
	}

	// Full output should be in the log file.
	logPath := extractLogPath(t, output)
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	logStr := string(logData)
	if !strings.Contains(logStr, "\n1\n") && !strings.HasPrefix(logStr, "1\n") {
		t.Errorf("expected log file to contain line 1")
	}
	if !strings.Contains(logStr, "\n50\n") {
		t.Errorf("expected log file to contain line 50")
	}
}

func TestCheck_WorkspaceContext_WritesLogToWorkspaceDir(t *testing.T) {
	dir := realPath(t, t.TempDir())

	// Simulate workspace structure: <dir>/.ralph/workspaces/my-ws/tree/
	treeDir := filepath.Join(dir, ".ralph", "workspaces", "my-ws", "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	exitCode := checkRun([]string{"echo", "test"}, treeDir, &stdout)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	output := stdout.String()
	logPath := extractLogPath(t, output)

	expectedDir := filepath.Join(dir, ".ralph", "workspaces", "my-ws", "logs")
	if !strings.HasPrefix(logPath, expectedDir) {
		t.Errorf("expected log in workspace logs dir %s, got: %s", expectedDir, logPath)
	}
}

func TestCheck_BaseContext_WritesLogToBaseDir(t *testing.T) {
	dir := realPath(t, t.TempDir())

	// Create .ralph dir to simulate a ralph project root (no workspace).
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	exitCode := checkRun([]string{"echo", "test"}, dir, &stdout)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	output := stdout.String()
	logPath := extractLogPath(t, output)

	expectedDir := filepath.Join(dir, ".ralph", "logs")
	if !strings.HasPrefix(logPath, expectedDir) {
		t.Errorf("expected log in base logs dir %s, got: %s", expectedDir, logPath)
	}
}

func TestCheck_SanitizesCommandForFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"echo hello", "echo_hello"},
		{"just test", "just_test"},
		{"sh -c 'echo foo && exit 1'", "sh_-c__echo_foo____exit_1_"},
		{"go test ./...", "go_test_._..."},
	}

	for _, tt := range tests {
		got := sanitizeCmd(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeCmd(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCheck_NoArgs_ReturnsError(t *testing.T) {
	dir := realPath(t, t.TempDir())

	var stdout bytes.Buffer
	exitCode := checkRun([]string{}, dir, &stdout)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	output := stdout.String()
	if !strings.Contains(output, "usage:") {
		t.Errorf("expected usage message, got: %s", output)
	}
}

func TestCheck_DurationInOutput(t *testing.T) {
	dir := realPath(t, t.TempDir())

	var stdout bytes.Buffer
	checkRun([]string{"echo", "fast"}, dir, &stdout)

	output := stdout.String()
	// Should contain duration like "(0.01s)" or similar.
	if !strings.Contains(output, "(") || !strings.Contains(output, "s)") {
		t.Errorf("expected duration in output, got: %s", output)
	}
}

func TestCheck_CreatesLogDirectory(t *testing.T) {
	dir := realPath(t, t.TempDir())

	// Do NOT pre-create .ralph/logs — the command should create it.
	var stdout bytes.Buffer
	exitCode := checkRun([]string{"echo", "autocreate"}, dir, &stdout)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	logPath := extractLogPath(t, stdout.String())
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("expected log file to be created at %s", logPath)
	}
}

// extractLogPath parses the "Full log: <path>" line from output.
func extractLogPath(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Full log: ") {
			return strings.TrimPrefix(line, "Full log: ")
		}
	}
	t.Fatalf("no 'Full log:' line found in output:\n%s", output)
	return ""
}
