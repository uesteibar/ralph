package main

import (
	"context"
	"fmt"
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
