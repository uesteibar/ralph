package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/uesteibar/ralph/internal/shell"
)

// Switch lists available ralph worktrees, lets the user pick one, and spawns
// a new shell session inside it. Exiting the shell returns to the original
// directory.
func Switch(args []string) error {
	fs := flag.NewFlagSet("switch", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// If we're inside a worktree, resolve the real repo root.
	repoPath := cfg.Repo.Path
	if realRoot, ok := worktreeRoot(repoPath); ok {
		repoPath = filepath.Dir(filepath.Dir(filepath.Dir(realRoot)))
	}

	worktreesDir := filepath.Join(repoPath, ".ralph", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no worktrees found — run 'ralph run' first to create one")
		}
		return fmt.Errorf("reading worktrees directory: %w", err)
	}

	var worktrees []string
	for _, e := range entries {
		if e.IsDir() {
			worktrees = append(worktrees, e.Name())
		}
	}

	if len(worktrees) == 0 {
		return fmt.Errorf("no worktrees found — run 'ralph run' first to create one")
	}

	options := make([]huh.Option[string], len(worktrees))
	for i, wt := range worktrees {
		label := strings.ReplaceAll(wt, "__", "/")
		options[i] = huh.NewOption(label, wt)
	}

	var selected string
	err = huh.NewSelect[string]().
		Title("Select worktree").
		Options(options...).
		Value(&selected).
		Run()
	if err != nil {
		return fmt.Errorf("selection cancelled")
	}

	targetDir := filepath.Join(worktreesDir, selected)

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	fmt.Fprintf(os.Stderr, "Switching to %s (exit to return)\n", strings.ReplaceAll(selected, "__", "/"))

	// Spawn an interactive shell as a subprocess. Using RunInteractive ensures
	// proper TTY handling so commands like ls and git status work correctly.
	r := &shell.Runner{Dir: targetDir}
	return r.RunInteractive(context.Background(), shellPath, "-i")
}
