package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/huh"
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

	// Change into the selected worktree and exec a new shell.
	if err := os.Chdir(targetDir); err != nil {
		return fmt.Errorf("changing directory: %w", err)
	}

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	fmt.Fprintf(os.Stderr, "Switching to %s (exit to return)\n", strings.ReplaceAll(selected, "__", "/"))
	return syscall.Exec(shellPath, []string{shellPath}, os.Environ())
}
