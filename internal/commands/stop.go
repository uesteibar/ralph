package commands

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

// stopAndWaitFn sends SIGTERM, waits for clean exit, and escalates to SIGKILL.
// Package-level var for testability.
var stopAndWaitFn = stopAndWait

// Stop stops a running daemon for a workspace.
func Stop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	configPath := AddProjectConfigFlag(fs)
	workspaceFlag := AddWorkspaceFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// Resolve workspace name: positional arg > --workspace flag > cwd detection.
	wsName := *workspaceFlag
	if remaining := fs.Args(); len(remaining) > 0 {
		wsName = remaining[0]
	}

	wc, err := resolveWorkContextFromFlags(wsName, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	if wc.Name == "base" {
		return fmt.Errorf("Workspace base is not running")
	}

	wsPath := workspace.WorkspacePath(cfg.Repo.Path, wc.Name)

	// Verify workspace directory exists.
	if _, statErr := os.Stat(wsPath); os.IsNotExist(statErr) {
		return fmt.Errorf("Workspace %s not found", wc.Name)
	}

	// Check if daemon is running (also cleans up stale PIDs).
	if !runstate.IsRunning(wsPath) {
		return fmt.Errorf("Workspace %s is not running", wc.Name)
	}

	if err := stopAndWaitFn(wsPath, 30); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Stopped workspace %s\n", wc.Name)
	return nil
}

// stopAndWait reads the PID, sends SIGTERM, waits up to timeoutSec for exit,
// then sends SIGKILL if the process is still alive.
func stopAndWait(wsPath string, timeoutSec int) error {
	pid, err := runstate.ReadPID(wsPath)
	if err != nil {
		return fmt.Errorf("reading PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	// Send SIGTERM for graceful shutdown.
	sendTermSignal(proc)

	// Wait for process to exit.
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if !runstate.IsRunning(wsPath) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Process did not exit within timeout — escalate to SIGKILL.
	sendKillSignal(proc)

	// Brief wait for SIGKILL to take effect.
	killDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(killDeadline) {
		if !runstate.IsRunning(wsPath) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("failed to stop workspace daemon (PID %d)", pid)
}
