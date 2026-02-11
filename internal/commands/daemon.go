package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

// daemonRunLoopFn is the function used to run the loop. Package-level var for testability.
var daemonRunLoopFn = func(ctx context.Context, cfg loop.Config) error {
	return loop.Run(ctx, cfg)
}

// Daemon runs the Ralph loop headlessly as a background-ready process.
// It writes PID/status files and uses only FileHandler for event output.
func Daemon(args []string) error {
	fs := flag.NewFlagSet("_daemon", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	maxIter := fs.Int("max-iterations", loop.DefaultMaxIterations, "Maximum loop iterations")
	workspaceFlag := AddWorkspaceFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	wc, err := resolveWorkContextFromFlags(*workspaceFlag, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	if _, err := os.Stat(wc.PRDPath); os.IsNotExist(err) {
		return fmt.Errorf("PRD not found at %s", wc.PRDPath)
	}

	wsPath := workspace.WorkspacePath(cfg.Repo.Path, wc.Name)

	// Detach from controlling terminal.
	detachFromTerminal()

	// Redirect stdout/stderr to /dev/null for silent operation.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = devNull
		os.Stderr = devNull
		defer devNull.Close()
	}

	// Write PID file and ensure cleanup on exit.
	if err := runstate.WritePID(wsPath); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer runstate.CleanupPID(wsPath)

	// Set up signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Set up FileHandler for JSONL logging.
	logsDir := filepath.Join(wsPath, "logs")
	handler := events.NewFileHandler(logsDir)
	defer handler.Close()

	progressPath := filepath.Join(cfg.Repo.Path, ".ralph", "progress.txt")
	promptsDir := cfg.PromptsDir()

	// Run the loop.
	loopErr := daemonRunLoopFn(ctx, loop.Config{
		MaxIterations: *maxIter,
		WorkDir:       wc.WorkDir,
		PRDPath:       wc.PRDPath,
		ProgressPath:  progressPath,
		PromptsDir:    promptsDir,
		QualityChecks: cfg.QualityChecks,
		EventHandler:  handler,
	})

	// Write status file based on outcome.
	status := runstate.Status{}
	switch {
	case loopErr == nil:
		status.Result = runstate.ResultSuccess
	case errors.Is(loopErr, context.Canceled):
		status.Result = runstate.ResultCancelled
	default:
		status.Result = runstate.ResultFailed
		status.Error = loopErr.Error()
	}
	runstate.WriteStatus(wsPath, status)

	return nil
}
