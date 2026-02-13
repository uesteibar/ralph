package main

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestGitPullerAdapter_PullDefaultBase_Success(t *testing.T) {
	var pulledBranch string
	var pulledDir string
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "main", nil
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			pulledBranch = branch
			pulledDir = r.Dir
			return nil
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pulledBranch != "main" {
		t.Fatalf("expected branch main, got %s", pulledBranch)
	}
	if pulledDir != "/repo" {
		t.Fatalf("expected dir /repo, got %s", pulledDir)
	}
}

func TestGitPullerAdapter_PullDefaultBase_DefaultBaseError(t *testing.T) {
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "", fmt.Errorf("config not found")
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			t.Fatal("pullFn should not be called when defaultBaseFn fails")
			return nil
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "resolving default base: config not found" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestGitPullerAdapter_PullDefaultBase_PullError(t *testing.T) {
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "main", nil
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			return fmt.Errorf("network error")
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "network error" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestWorkspaceCreatorAdapter_Create_PullsBeforeCreateWorkspace(t *testing.T) {
	var callOrder []string
	adapter := &workspaceCreatorAdapter{
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			callOrder = append(callOrder, "pull:"+branch)
			return nil
		},
	}

	// Create will fail on the actual git operations (no real repo), but we
	// can verify pull was called first by checking order before the error.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)

	if len(callOrder) == 0 {
		t.Fatal("expected pullFn to be called")
	}
	if callOrder[0] != "pull:main" {
		t.Fatalf("expected pull:main, got %s", callOrder[0])
	}
}

func TestWorkspaceCreatorAdapter_Create_ProceedsWhenPullFails(t *testing.T) {
	pullCalled := false
	adapter := &workspaceCreatorAdapter{
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			pullCalled = true
			return fmt.Errorf("network error")
		},
	}

	// Even though pull fails, Create should still attempt workspace creation.
	// It will fail on actual git ops, but that's fine — we're testing that
	// pullFn failure doesn't prevent the call from proceeding.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)

	if !pullCalled {
		t.Fatal("expected pullFn to be called")
	}
}

func TestWorkspaceCreatorAdapter_Create_SkipsPullWhenNilFn(t *testing.T) {
	adapter := &workspaceCreatorAdapter{
		pullFn: nil,
	}

	// Should not panic — nil pullFn is simply skipped.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)
}

func TestRebaseRunnerAdapter_RunRebase_BuildsCorrectCommand(t *testing.T) {
	var capturedArgs []string
	adapter := &rebaseRunnerAdapter{
		cmdFn: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = append([]string{name}, args...)
			// Return a command that succeeds immediately
			return exec.CommandContext(ctx, "true")
		},
	}

	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/.ralph/ralph.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"ralph", "rebase",
		"--workspace", "proj-42",
		"--project-config", "/config/.ralph/ralph.yaml",
		"main",
	}
	if len(capturedArgs) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(capturedArgs), capturedArgs)
	}
	for i, want := range expected {
		if capturedArgs[i] != want {
			t.Errorf("arg[%d]: expected %q, got %q", i, want, capturedArgs[i])
		}
	}
}

func TestRebaseRunnerAdapter_RunRebase_ReturnsErrorOnFailure(t *testing.T) {
	adapter := &rebaseRunnerAdapter{
		cmdFn: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
	}

	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/.ralph/ralph.yaml")
	if err == nil {
		t.Fatal("expected error when command fails")
	}
}

func TestRebaseRunnerAdapter_RunRebase_DefaultUsesExecCommand(t *testing.T) {
	adapter := &rebaseRunnerAdapter{}

	// With nil cmdFn, it defaults to exec.CommandContext.
	// This will fail because "ralph" binary likely isn't available in tests,
	// but we verify it doesn't panic and returns an error.
	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/ralph.yaml")
	if err == nil {
		t.Fatal("expected error when ralph binary is not available")
	}
}
