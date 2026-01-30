package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRun_Echo_ReturnsOutput(t *testing.T) {
	r := &Runner{}
	out, err := r.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := strings.TrimSpace(out); got != "hello" {
		t.Errorf("output = %q, want %q", got, "hello")
	}
}

func TestRun_NonZeroExit_ReturnsExitError(t *testing.T) {
	r := &Runner{}
	_, err := r.Run(context.Background(), "sh", "-c", "echo fail >&2; exit 42")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 42 {
		t.Errorf("Code = %d, want 42", exitErr.Code)
	}
	if !strings.Contains(exitErr.Stderr, "fail") {
		t.Errorf("Stderr = %q, want to contain %q", exitErr.Stderr, "fail")
	}
}

func TestRun_WorkingDirectory(t *testing.T) {
	r := &Runner{Dir: "/tmp"}
	out, err := r.Run(context.Background(), "pwd")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// /tmp may resolve to /private/tmp on macOS
	got := strings.TrimSpace(out)
	if got != "/tmp" && got != "/private/tmp" {
		t.Errorf("pwd = %q, want /tmp or /private/tmp", got)
	}
}

func TestRunWithStdin_PipesInput(t *testing.T) {
	r := &Runner{}
	out, err := r.RunWithStdin(context.Background(), "hello from stdin", "cat")
	if err != nil {
		t.Fatalf("RunWithStdin failed: %v", err)
	}
	if got := strings.TrimSpace(out); got != "hello from stdin" {
		t.Errorf("output = %q, want %q", got, "hello from stdin")
	}
}

func TestRun_NotFound_ReturnsError(t *testing.T) {
	r := &Runner{}
	_, err := r.Run(context.Background(), "nonexistent-binary-xyz")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}
