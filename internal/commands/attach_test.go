package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestAttach_NoDaemonRunning_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "idle-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Attach([]string{"--workspace", wsName})
	if err == nil {
		t.Fatal("expected error when no daemon is running")
	}
	if !strings.Contains(err.Error(), "no daemon running") {
		t.Errorf("expected 'no daemon running' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), wsName) {
		t.Errorf("expected error to mention workspace name %q, got: %v", wsName, err)
	}
}

func TestAttach_StalePID_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "stale-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)
	// Write a PID for a process that does not exist.
	os.WriteFile(wsPath+"/run.pid", []byte("999999"), 0644)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Attach([]string{"--workspace", wsName})
	if err == nil {
		t.Fatal("expected error for stale PID")
	}
	if !strings.Contains(err.Error(), "no daemon running") {
		t.Errorf("expected 'no daemon running' error, got: %v", err)
	}

	// Verify stale PID file was cleaned up.
	if runstate.IsRunning(wsPath) {
		t.Error("stale PID file should have been cleaned up")
	}
}

func TestAttach_BaseWorkspace_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Attach([]string{})
	if err == nil {
		t.Fatal("expected error when attaching to base workspace")
	}
	if !strings.Contains(err.Error(), "base") {
		t.Errorf("expected error to mention 'base', got: %v", err)
	}
}

func TestAttach_WorkspaceNotFound_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Attach([]string{"--workspace", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
}

func TestAttach_RunningDaemon_DelegatesToTailLogs(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "attached-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Write current process PID to simulate a running daemon.
	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}
	defer runstate.CleanupPID(wsPath)

	// Mock tailLogsTUI to verify it gets called with correct args.
	called := false
	var gotWsPath, gotLogsDir, gotWsName, gotPrdPath string
	origTailLogsTUIFn := tailLogsTUIFn
	tailLogsTUIFn = func(wsPath, logsDir, workspaceName, prdPath string) error {
		called = true
		gotWsPath = wsPath
		gotLogsDir = logsDir
		gotWsName = workspaceName
		gotPrdPath = prdPath
		return nil
	}
	defer func() { tailLogsTUIFn = origTailLogsTUIFn }()

	err := Attach([]string{"--workspace", wsName})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected tailLogsTUI to be called")
	}
	if gotWsPath != wsPath {
		t.Errorf("expected wsPath %q, got %q", wsPath, gotWsPath)
	}
	if gotWsName != wsName {
		t.Errorf("expected workspace name %q, got %q", wsName, gotWsName)
	}
	if gotLogsDir == "" {
		t.Error("expected logsDir to be non-empty")
	}
	if gotPrdPath == "" {
		t.Error("expected prdPath to be non-empty")
	}
}

func TestAttach_NoTUI_DelegatesToTailLogsPlainText(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "notui-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Write current process PID to simulate a running daemon.
	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}
	defer runstate.CleanupPID(wsPath)

	// Mock tailLogsPlainTextFn to verify it gets called.
	called := false
	origTailLogsPlainTextFn := tailLogsPlainTextFn
	tailLogsPlainTextFn = func(wsPath, logsDir string) error {
		called = true
		return nil
	}
	defer func() { tailLogsPlainTextFn = origTailLogsPlainTextFn }()

	// Also mock tailLogsTUI to ensure it's NOT called.
	origTailLogsTUIFn := tailLogsTUIFn
	tailLogsTUIFn = func(wsPath, logsDir, workspaceName, prdPath string) error {
		t.Fatal("tailLogsTUI should not be called with --no-tui")
		return nil
	}
	defer func() { tailLogsTUIFn = origTailLogsTUIFn }()

	err := Attach([]string{"--workspace", wsName, "--no-tui"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected tailLogsPlainText to be called")
	}
}
