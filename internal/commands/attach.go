package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

// tailLogsTUIFn wraps tailLogsTUI for testability.
var tailLogsTUIFn = tailLogsTUI

// tailLogsPlainTextFn wraps tailLogsPlainText for testability.
var tailLogsPlainTextFn = func(wsPath, logsDir string) error {
	return tailLogsPlainText(context.Background(), wsPath, logsDir)
}

// Attach connects to a running daemon and displays its log output.
func Attach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	configPath := AddProjectConfigFlag(fs)
	workspaceFlag := AddWorkspaceFlag(fs)
	noTUI := fs.Bool("no-tui", false, "Disable TUI and use plain-text output")
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

	if wc.Name == "base" {
		return fmt.Errorf("cannot attach to base workspace — attach requires a running daemon")
	}

	wsPath := workspace.WorkspacePath(cfg.Repo.Path, wc.Name)

	// Verify workspace directory exists.
	if _, statErr := os.Stat(wsPath); os.IsNotExist(statErr) {
		return fmt.Errorf("workspace %s not found", wc.Name)
	}

	if !runstate.IsRunning(wsPath) {
		return fmt.Errorf("no daemon running for workspace %s", wc.Name)
	}

	logsDir := filepath.Join(wsPath, "logs")

	if *noTUI {
		return tailLogsPlainTextFn(wsPath, logsDir)
	}

	return tailLogsTUIFn(wsPath, logsDir, wc.Name, wc.PRDPath)
}
