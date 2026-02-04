package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Workspaces handles the `ralph workspaces` subcommand.
func Workspaces(args []string) error {
	return workspacesDispatch(args, os.Stdin)
}

func workspacesDispatch(args []string, in io.Reader) error {
	if len(args) == 0 {
		// No subcommand defaults to list.
		return workspacesList(args)
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "new":
		return workspacesNew(rest, in)
	case "list":
		return workspacesList(rest)
	case "switch":
		return workspacesSwitch(rest)
	case "remove":
		return workspacesRemove(rest)
	default:
		return fmt.Errorf("unknown workspaces subcommand: %s (use 'new', 'list', 'switch', or 'remove')", subcmd)
	}
}

func workspacesNew(args []string, in io.Reader) error {
	fs := flag.NewFlagSet("workspaces new", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Require workspace name as positional arg.
	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("usage: ralph workspaces new <name> [--project-config path]")
	}
	name := remaining[0]

	// Validate workspace name.
	if err := workspace.ValidateName(name); err != nil {
		return err
	}

	// Check shell integration.
	if os.Getenv("RALPH_SHELL_INIT") == "" {
		return fmt.Errorf("Shell integration required. Add to your shell config:\n\n  eval \"$(ralph shell-init)\"\n\nThen restart your shell.")
	}

	// Load config.
	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// Print workspace header for current context.
	wc, _ := resolveWorkContextFromFlags("", cfg.Repo.Path)
	printWorkspaceHeader(wc, cfg.Repo.Path)

	// Check if workspace already exists in registry.
	existing, err := workspace.RegistryGet(cfg.Repo.Path, name)
	if err == nil && existing != nil {
		return fmt.Errorf("Workspace %q already exists. Switch to it: ralph workspaces switch %s", name, name)
	}

	// Derive branch name.
	branch, err := workspace.DeriveBranch(cfg.Repo.BranchPrefix, name, cfg.Repo.BranchPattern)
	if err != nil {
		return err
	}

	ctx := context.Background()
	repoRunner := &shell.Runner{Dir: cfg.Repo.Path}

	// Check if branch already exists.
	existsLocally := gitops.BranchExistsLocally(ctx, repoRunner, branch)
	if existsLocally {
		choice, err := promptBranchChoice(branch, in)
		if err != nil {
			return err
		}
		if choice == "fresh" {
			// Delete the existing branch so CreateWorkspace creates a new one.
			_ = gitops.DeleteBranch(ctx, repoRunner, branch)
		}
		// "resume" — CreateWorkspace will detect the existing branch and use it.
	}

	// Create workspace.
	ws := workspace.Workspace{
		Name:      name,
		Branch:    branch,
		CreatedAt: time.Now(),
	}

	if err := workspace.CreateWorkspace(ctx, repoRunner, cfg.Repo.Path, ws, cfg.Repo.DefaultBase, cfg.CopyToWorktree); err != nil {
		return fmt.Errorf("creating workspace: %w", err)
	}

	treePath := workspace.TreePath(cfg.Repo.Path, name)

	// stderr: human-readable confirmation.
	fmt.Fprintf(os.Stderr, "✓ Created workspace '%s' (branch: %s)\n", name, branch)

	// stdout: absolute path to tree/ for shell function to cd into.
	fmt.Println(treePath)

	return nil
}

func promptBranchChoice(branch string, in io.Reader) (string, error) {
	fmt.Fprintf(os.Stderr, "Branch %s already exists. Start fresh or resume? (fresh/resume) ", branch)
	scanner := bufio.NewScanner(in)
	if scanner.Scan() {
		choice := scanner.Text()
		switch choice {
		case "fresh", "resume":
			return choice, nil
		default:
			return "", fmt.Errorf("invalid choice %q: must be 'fresh' or 'resume'", choice)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no input received")
}

func workspacesList(args []string) error {
	fs := flag.NewFlagSet("workspaces list", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// Resolve current workspace context.
	cwd, _ := os.Getwd()
	wc, _ := workspace.ResolveWorkContext("", os.Getenv("RALPH_WORKSPACE"), cwd, cfg.Repo.Path)

	printWorkspaceHeader(wc, cfg.Repo.Path)

	// Read registry with missing detection.
	entries, err := workspace.RegistryListWithMissing(cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("reading workspace registry: %w", err)
	}

	// Print base entry first.
	if wc.Name == "base" {
		fmt.Println("* base [current]")
	} else {
		fmt.Println("  base")
	}

	// Print workspace entries.
	for _, e := range entries {
		prefix := "  "
		suffix := ""
		if e.Name == wc.Name {
			prefix = "* "
			suffix = " [current]"
		}
		if e.Missing {
			suffix += " [missing]"
		}
		fmt.Printf("%s%s%s\n", prefix, e.Name, suffix)
	}

	// Hint if no workspaces.
	if len(entries) == 0 {
		fmt.Println()
		fmt.Println("Create a workspace: ralph workspaces new <name>")
	}

	return nil
}

func workspacesSwitch(args []string) error {
	fs := flag.NewFlagSet("workspaces switch", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Check shell integration.
	if os.Getenv("RALPH_SHELL_INIT") == "" {
		return fmt.Errorf("Shell integration required. Add to your shell config:\n\n  eval \"$(ralph shell-init)\"\n\nThen restart your shell.")
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("usage: ralph workspaces switch <name> [--project-config path]")
	}
	name := remaining[0]

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// Print workspace header for current context.
	curWC, _ := resolveWorkContextFromFlags("", cfg.Repo.Path)
	printWorkspaceHeader(curWC, cfg.Repo.Path)

	// Handle "base" specially.
	if name == "base" {
		fmt.Fprintf(os.Stderr, "Switched to workspace %s\n", name)
		fmt.Println(cfg.Repo.Path)
		return nil
	}

	// Validate workspace exists in registry.
	_, err = workspace.RegistryGet(cfg.Repo.Path, name)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			return fmt.Errorf("Workspace %q not found. Run ralph workspaces list to see available.", name)
		}
		if strings.Contains(errMsg, "directory is missing") {
			return fmt.Errorf("Workspace %q directory is missing. Remove it: ralph workspaces remove %s", name, name)
		}
		return err
	}

	treePath := workspace.TreePath(cfg.Repo.Path, name)

	fmt.Fprintf(os.Stderr, "Switched to workspace %s\n", name)
	fmt.Println(treePath)

	return nil
}

func workspacesRemove(args []string) error {
	fs := flag.NewFlagSet("workspaces remove", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("usage: ralph workspaces remove <name> [--project-config path]")
	}
	name := remaining[0]

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	// Print workspace header for current context.
	curWC, _ := resolveWorkContextFromFlags("", cfg.Repo.Path)
	printWorkspaceHeader(curWC, cfg.Repo.Path)

	// Validate workspace exists in registry (allow missing directory).
	entries, err := workspace.RegistryListWithMissing(cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("reading workspace registry: %w", err)
	}
	found := false
	for _, e := range entries {
		if e.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Workspace %q not found. Run ralph workspaces list to see available.", name)
	}

	// Detect if removing the current workspace.
	cwd, _ := os.Getwd()
	wc, _ := workspace.ResolveWorkContext("", os.Getenv("RALPH_WORKSPACE"), cwd, cfg.Repo.Path)
	isCurrent := wc.Name == name

	ctx := context.Background()
	repoRunner := &shell.Runner{Dir: cfg.Repo.Path}

	if err := workspace.RemoveWorkspace(ctx, repoRunner, cfg.Repo.Path, name); err != nil {
		return fmt.Errorf("removing workspace: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Removed workspace '%s'\n", name)

	// If removing current workspace, output base repo path for shell function cd.
	if isCurrent {
		fmt.Println(cfg.Repo.Path)
	}

	return nil
}
