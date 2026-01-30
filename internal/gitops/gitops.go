package gitops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/shell"
)

// BranchExistsLocally checks whether a branch exists in the local repo.
func BranchExistsLocally(ctx context.Context, r *shell.Runner, branch string) bool {
	_, err := r.Run(ctx, "git", "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(ctx context.Context, r *shell.Runner, branch string) error {
	_, err := r.Run(ctx, "git", "branch", "-D", branch)
	if err != nil {
		return fmt.Errorf("deleting branch %s: %w", branch, err)
	}
	return nil
}

// WorktreePath returns the path where a worktree for the given branch would live.
func WorktreePath(repoPath, branch string) string {
	return filepath.Join(repoPath, ".ralph", "worktrees", sanitizeBranch(branch))
}

// CreateWorktree creates a git worktree for the given branch. If the branch
// already exists locally or on remote, it is checked out directly. Otherwise
// a new branch is created from base. Returns the worktree path.
func CreateWorktree(ctx context.Context, r *shell.Runner, repoPath, branch, base string) (string, error) {
	worktreeRoot := filepath.Join(repoPath, ".ralph", "worktrees")
	if err := os.MkdirAll(worktreeRoot, 0755); err != nil {
		return "", fmt.Errorf("creating worktree root: %w", err)
	}

	worktreePath := filepath.Join(worktreeRoot, sanitizeBranch(branch))

	repoRunner := &shell.Runner{Dir: repoPath}

	// Fetch latest from origin
	_, _ = repoRunner.Run(ctx, "git", "fetch", "origin", base)

	// Check if branch exists locally or on remote
	existsLocally := BranchExistsLocally(ctx, repoRunner, branch)

	_, err := repoRunner.Run(ctx, "git", "rev-parse", "--verify", "refs/remotes/origin/"+branch)
	existsRemote := err == nil

	var worktreeErr error
	if existsLocally || existsRemote {
		// Branch already exists — check it out directly
		_, worktreeErr = repoRunner.Run(ctx, "git", "worktree", "add", worktreePath, branch)
	} else {
		// New branch — create from origin/<base>
		_, worktreeErr = repoRunner.Run(ctx, "git", "worktree", "add", "-b", branch, worktreePath, "origin/"+base)
	}
	if worktreeErr != nil {
		return "", fmt.Errorf("creating worktree for %s: %w", branch, worktreeErr)
	}

	return worktreePath, nil
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(ctx context.Context, r *shell.Runner, repoPath, worktreePath string) error {
	repoRunner := &shell.Runner{Dir: repoPath}
	_, err := repoRunner.Run(ctx, "git", "worktree", "remove", "--force", worktreePath)
	if err != nil {
		return fmt.Errorf("removing worktree %s: %w", worktreePath, err)
	}
	return nil
}

// CopyDotRalph copies the .ralph directory from the repo root into the
// worktree, enabling the agent to read config and prompts.
func CopyDotRalph(repoPath, worktreePath string) error {
	src := filepath.Join(repoPath, ".ralph")
	dst := filepath.Join(worktreePath, ".ralph")

	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no .ralph dir to copy
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	// Directories to skip — these are gitignored ephemeral data that should
	// not be copied into worktrees (and worktrees/ would recurse infinitely).
	skipDirs := map[string]bool{
		"worktrees": true,
		"state":     true,
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if d.IsDir() && skipDirs[rel] {
			return fs.SkipDir
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// Commit stages all changes and creates a commit.
func Commit(ctx context.Context, r *shell.Runner, message string) error {
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// sanitizeBranch replaces path separators in branch names for safe directory names.
func sanitizeBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "__")
}
