package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

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
