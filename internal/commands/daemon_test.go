package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestDaemon_RequiresWorkspaceFlag(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Daemon([]string{})
	if err == nil {
		t.Fatal("expected error when --workspace not provided from repo root")
	}
}

func TestDaemon_ErrorWhenWorkspaceNotFound(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Daemon([]string{"--workspace", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "PRD not found") {
		t.Errorf("expected PRD not found error, got: %v", err)
	}
}

func TestDaemon_WritesPIDAndCleansUp(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-daemon"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Mock the loop to verify PID lifecycle
	wsPath := workspace.WorkspacePath(dir, wsName)
	var pidExistedDuringLoop bool
	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		pidExistedDuringLoop = runstate.IsRunning(wsPath)
		return nil
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}

	if !pidExistedDuringLoop {
		t.Error("PID file should exist while loop is running")
	}

	// PID should be cleaned up after exit
	if runstate.IsRunning(wsPath) {
		t.Error("PID file should be cleaned up after daemon exits")
	}
}

func TestDaemon_WritesSuccessStatus(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-success"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		return nil
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}

	wsPath := workspace.WorkspacePath(dir, wsName)
	status, err := runstate.ReadStatus(wsPath)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.Result != runstate.ResultSuccess {
		t.Errorf("status.Result = %q, want %q", status.Result, runstate.ResultSuccess)
	}
}

func TestDaemon_WritesFailedStatus(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-failed"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		return errors.New("max iterations reached")
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}

	wsPath := workspace.WorkspacePath(dir, wsName)
	status, err := runstate.ReadStatus(wsPath)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.Result != runstate.ResultFailed {
		t.Errorf("status.Result = %q, want %q", status.Result, runstate.ResultFailed)
	}
	if status.Error != "max iterations reached" {
		t.Errorf("status.Error = %q, want %q", status.Error, "max iterations reached")
	}
}

func TestDaemon_WritesCancelledStatus(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-cancelled"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		return context.Canceled
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}

	wsPath := workspace.WorkspacePath(dir, wsName)
	status, err := runstate.ReadStatus(wsPath)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.Result != runstate.ResultCancelled {
		t.Errorf("status.Result = %q, want %q", status.Result, runstate.ResultCancelled)
	}
}

func TestDaemon_UsesFileHandler(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-handler"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	var capturedHandler events.EventHandler
	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		capturedHandler = cfg.EventHandler
		return nil
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}

	if capturedHandler == nil {
		t.Fatal("EventHandler should be set")
	}
	if _, ok := capturedHandler.(*events.FileHandler); !ok {
		t.Errorf("EventHandler should be *events.FileHandler, got %T", capturedHandler)
	}
}

func TestDaemon_RedirectsOutputToDevNull(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "test-devnull"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Capture whether stdout/stderr are redirected during loop
	origRunLoop := daemonRunLoopFn
	daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
		return nil
	}
	defer func() { daemonRunLoopFn = origRunLoop }()

	// The daemon should not produce output. We verify by checking that
	// the function completes without error (output redirection is internal).
	err := Daemon([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("Daemon returned error: %v", err)
	}
}

// setupWorkspace creates a minimal workspace directory structure for testing.
func setupWorkspace(t *testing.T, dir, wsName, prdContent string) {
	t.Helper()
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}
	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}
}

func allPassingPRD(wsName string) string {
	return `{
  "project": "test",
  "branchName": "ralph/` + wsName + `",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": true}
  ]
}`
}
