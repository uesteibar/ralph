package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/shell"
)

// CreateWorkspace creates a workspace with its full directory structure:
// .ralph/workspaces/<name>/ directory, workspace.json metadata, git worktree
// at .ralph/workspaces/<name>/tree/, copies .ralph/ (skipping worktrees/,
// state/, workspaces/), .claude/ if exists, and copy_to_worktree patterns.
// Finally it updates the registry.
func CreateWorkspace(ctx context.Context, runner *shell.Runner, repoPath string, ws Workspace, base string, copyPatterns []string) error {
	wsDir := WorkspacePath(repoPath, ws.Name)
	treePath := TreePath(repoPath, ws.Name)

	// Create workspace directory.
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}

	// Write workspace.json metadata.
	if err := WriteWorkspaceJSON(repoPath, ws.Name, ws); err != nil {
		return fmt.Errorf("writing workspace.json: %w", err)
	}

	// Create git worktree at tree/.
	// Use a repoRunner to operate from the main repo.
	repoRunner := &shell.Runner{Dir: repoPath}

	// Fetch latest from origin (best effort).
	_, _ = repoRunner.Run(ctx, "git", "fetch", "origin", base)

	// Check if branch exists locally or on remote.
	existsLocally := gitops.BranchExistsLocally(ctx, repoRunner, ws.Branch)

	_, err := repoRunner.Run(ctx, "git", "rev-parse", "--verify", "refs/remotes/origin/"+ws.Branch)
	existsRemote := err == nil

	if existsLocally || existsRemote {
		// Branch already exists — check it out directly (resume scenario).
		_, err = repoRunner.Run(ctx, "git", "worktree", "add", treePath, ws.Branch)
	} else {
		// New branch — create from base.
		// Try origin/<base> first, fall back to local <base>.
		_, err = repoRunner.Run(ctx, "git", "worktree", "add", "-b", ws.Branch, treePath, "origin/"+base)
		if err != nil {
			// origin/<base> may not exist (e.g., local-only repo). Try local base.
			_, err = repoRunner.Run(ctx, "git", "worktree", "add", "-b", ws.Branch, treePath, base)
		}
	}
	if err != nil {
		// Clean up workspace directory on failure.
		os.RemoveAll(wsDir)
		return fmt.Errorf("creating worktree for %s: %w", ws.Branch, err)
	}

	// Remove directories that should not exist in the worktree.
	// These may have been checked out by git if they were committed.
	for _, skip := range []string{"state", "workspaces", "worktrees"} {
		os.RemoveAll(filepath.Join(treePath, ".ralph", skip))
	}

	// Copy .ralph/ into the worktree (skips worktrees/, state/, workspaces/).
	if err := gitops.CopyDotRalph(repoPath, treePath); err != nil {
		return fmt.Errorf("copying .ralph: %w", err)
	}

	// Copy .claude/ if it exists.
	if err := gitops.CopyDotClaude(repoPath, treePath); err != nil {
		return fmt.Errorf("copying .claude: %w", err)
	}

	// Copy user-specified patterns.
	if len(copyPatterns) > 0 {
		if err := gitops.CopyGlobPatterns(repoPath, treePath, copyPatterns, func(msg string) {
			fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
		}); err != nil {
			return fmt.Errorf("copying patterns: %w", err)
		}
	}

	// Update registry.
	if err := RegistryCreate(repoPath, ws); err != nil {
		return fmt.Errorf("updating registry: %w", err)
	}

	return nil
}

// RemoveWorkspace removes a workspace: git worktree, workspace directory,
// registry entry, and the git branch.
func RemoveWorkspace(ctx context.Context, runner *shell.Runner, repoPath, name string) error {
	treePath := TreePath(repoPath, name)
	wsDir := WorkspacePath(repoPath, name)

	// Look up branch from registry before removing.
	ws, err := RegistryGet(repoPath, name)
	if err != nil {
		// If directory is missing, try reading from registry directly.
		entries, readErr := readRegistry(repoPath)
		if readErr != nil {
			return fmt.Errorf("looking up workspace: %w", err)
		}
		for _, e := range entries {
			if e.Name == name {
				ws = &Workspace{Name: e.Name, Branch: e.Branch, CreatedAt: e.CreatedAt}
				break
			}
		}
		if ws == nil {
			return fmt.Errorf("workspace %q not found", name)
		}
	}

	// Remove git worktree (best effort — directory may already be gone).
	repoRunner := &shell.Runner{Dir: repoPath}
	if _, statErr := os.Stat(treePath); statErr == nil {
		if err := gitops.RemoveWorktree(ctx, repoRunner, repoPath, treePath); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}
	}

	// Delete entire workspace directory.
	if err := os.RemoveAll(wsDir); err != nil {
		return fmt.Errorf("removing workspace directory: %w", err)
	}

	// Remove registry entry.
	if err := RegistryRemove(repoPath, name); err != nil {
		return fmt.Errorf("removing registry entry: %w", err)
	}

	// Delete git branch (best effort — may already be gone or checked out elsewhere).
	_ = gitops.DeleteBranch(ctx, repoRunner, ws.Branch)

	return nil
}
