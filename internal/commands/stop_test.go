package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestStop_WorkspaceNotFound_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Stop([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestStop_NotRunning_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "stopped-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Stop([]string{wsName})
	if err == nil {
		t.Fatal("expected error for non-running workspace")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Errorf("expected 'is not running' error, got: %v", err)
	}
}

func TestStop_StalePID_CleansUpAndReportsNotRunning(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "stale-pid-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	// Write a PID for a process that does not exist.
	os.WriteFile(wsPath+"/run.pid", []byte("999999"), 0644)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	err := Stop([]string{wsName})
	if err == nil {
		t.Fatal("expected error for stale PID workspace")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Errorf("expected 'is not running' error, got: %v", err)
	}

	// Verify stale PID file was cleaned up.
	if runstate.IsRunning(wsPath) {
		t.Error("stale PID file should have been cleaned up")
	}
}

func TestStop_RunningDaemon_StopsAndPrintsSuccess(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "running-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Mock stopAndWaitFn to simulate a successful stop.
	origStopAndWait := stopAndWaitFn
	stopAndWaitFn = func(path string, timeoutSec int) error {
		runstate.CleanupPID(path)
		return nil
	}
	defer func() { stopAndWaitFn = origStopAndWait }()

	// Write current process PID to simulate a running daemon.
	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}

	// Capture stderr.
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Stop([]string{wsName})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(stderr, "Stopped workspace "+wsName) {
		t.Errorf("expected success message 'Stopped workspace %s', got: %s", wsName, stderr)
	}
}

func TestStop_WorkspaceFlag(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "flag-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origStopAndWait := stopAndWaitFn
	stopAndWaitFn = func(path string, timeoutSec int) error {
		runstate.CleanupPID(path)
		return nil
	}
	defer func() { stopAndWaitFn = origStopAndWait }()

	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}

	// Suppress stderr.
	oldStderr := os.Stderr
	_, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Stop([]string{"--workspace", wsName})

	wPipe.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Fatalf("expected no error with --workspace flag, got: %v", err)
	}
}

func TestStop_PositionalArg(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "positional-ws"
	setupWorkspace(t, dir, wsName, allPassingPRD(wsName))

	wsPath := workspace.WorkspacePath(dir, wsName)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origStopAndWait := stopAndWaitFn
	stopAndWaitFn = func(path string, timeoutSec int) error {
		runstate.CleanupPID(path)
		return nil
	}
	defer func() { stopAndWaitFn = origStopAndWait }()

	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}

	// Capture stderr.
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Stop([]string{wsName})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error with positional arg, got: %v", err)
	}

	if !strings.Contains(stderr, "Stopped workspace "+wsName) {
		t.Errorf("expected success message, got: %s", stderr)
	}
}

func TestStop_BaseWorkspace_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Running from repo root without specifying workspace should resolve to "base"
	// and "base" has no daemon to stop.
	err := Stop([]string{})
	if err == nil {
		t.Fatal("expected error when stopping base workspace")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected meaningful error for base workspace, got: %v", err)
	}
}
