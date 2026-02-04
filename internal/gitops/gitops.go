package gitops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
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
		"worktrees":  true,
		"state":      true,
		"workspaces": true,
	}

	// Files to skip — ralph.yaml must not be copied into workspace trees
	// because config.Discover() would find it there and derive Repo.Path as
	// the tree path instead of the real repo root, causing doubled paths in
	// workspace resolution.
	skipFiles := map[string]bool{
		"ralph.yaml": true,
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

		if !d.IsDir() && skipFiles[rel] {
			return nil
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

// CopyDotClaude copies the .claude directory from the repo root into the
// worktree, enabling Claude settings and skills to be available in the
// isolated environment.
func CopyDotClaude(repoPath, worktreePath string) error {
	src := filepath.Join(repoPath, ".claude")
	dst := filepath.Join(worktreePath, ".claude")

	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no .claude dir to copy
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
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

// IsWorktree returns true when the current working directory (runner.Dir) is
// inside a git worktree rather than the main repository.
func IsWorktree(ctx context.Context, r *shell.Runner) (bool, error) {
	out, err := r.Run(ctx, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, fmt.Errorf("checking work tree: %w", err)
	}
	if strings.TrimSpace(out) != "true" {
		return false, nil
	}

	gitDir, err := r.Run(ctx, "git", "rev-parse", "--git-dir")
	if err != nil {
		return false, fmt.Errorf("checking git dir: %w", err)
	}
	// Inside a worktree the git dir contains "/worktrees/", in the main repo
	// it is simply ".git".
	return strings.Contains(strings.TrimSpace(gitDir), "worktrees"), nil
}

// CurrentBranch returns the name of the currently checked-out branch.
func CurrentBranch(ctx context.Context, r *shell.Runner) (string, error) {
	out, err := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// IsAncestor returns true when ancestor is an ancestor of descendant.
func IsAncestor(ctx context.Context, r *shell.Runner, ancestor, descendant string) (bool, error) {
	_, err := r.Run(ctx, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		var exitErr *shell.ExitError
		if errors.As(err, &exitErr) && exitErr.Code == 1 {
			return false, nil
		}
		return false, fmt.Errorf("checking ancestry: %w", err)
	}
	return true, nil
}

// FetchBranch fetches origin/<branch>.
func FetchBranch(ctx context.Context, r *shell.Runner, branch string) error {
	_, err := r.Run(ctx, "git", "fetch", "origin", branch)
	if err != nil {
		return fmt.Errorf("fetching origin/%s: %w", branch, err)
	}
	return nil
}

// RebaseResult describes the outcome of a rebase operation.
type RebaseResult struct {
	Success       bool
	HasConflicts  bool
}

// StartRebase runs git rebase onto the given ref and returns whether conflicts
// occurred.
func StartRebase(ctx context.Context, r *shell.Runner, onto string) (RebaseResult, error) {
	_, err := r.Run(ctx, "git", "rebase", onto)
	if err != nil {
		var exitErr *shell.ExitError
		if errors.As(err, &exitErr) {
			// Check if a rebase is now in progress (meaning conflicts).
			inProgress, checkErr := HasRebaseInProgress(ctx, r)
			if checkErr != nil {
				return RebaseResult{}, fmt.Errorf("starting rebase: %w", err)
			}
			if inProgress {
				return RebaseResult{HasConflicts: true}, nil
			}
			return RebaseResult{}, fmt.Errorf("starting rebase: %w", err)
		}
		return RebaseResult{}, fmt.Errorf("starting rebase: %w", err)
	}
	return RebaseResult{Success: true}, nil
}

// HasRebaseInProgress detects if a rebase is currently in progress.
func HasRebaseInProgress(ctx context.Context, r *shell.Runner) (bool, error) {
	gitDir, err := r.Run(ctx, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false, fmt.Errorf("getting git dir: %w", err)
	}
	absGitDir := strings.TrimSpace(gitDir)
	rebaseMerge := filepath.Join(absGitDir, "rebase-merge")
	rebaseApply := filepath.Join(absGitDir, "rebase-apply")

	if _, err := os.Stat(rebaseMerge); err == nil {
		return true, nil
	}
	if _, err := os.Stat(rebaseApply); err == nil {
		return true, nil
	}
	return false, nil
}

// ContinueRebase runs git rebase --continue and returns whether more conflicts
// occurred.
func ContinueRebase(ctx context.Context, r *shell.Runner) (RebaseResult, error) {
	_, err := r.Run(ctx, "git", "-c", "core.editor=true", "rebase", "--continue")
	if err != nil {
		var exitErr *shell.ExitError
		if errors.As(err, &exitErr) {
			inProgress, checkErr := HasRebaseInProgress(ctx, r)
			if checkErr != nil {
				return RebaseResult{}, fmt.Errorf("continuing rebase: %w", err)
			}
			if inProgress {
				return RebaseResult{HasConflicts: true}, nil
			}
			return RebaseResult{}, fmt.Errorf("continuing rebase: %w", err)
		}
		return RebaseResult{}, fmt.Errorf("continuing rebase: %w", err)
	}
	return RebaseResult{Success: true}, nil
}

// AbortRebase runs git rebase --abort.
func AbortRebase(ctx context.Context, r *shell.Runner) error {
	_, err := r.Run(ctx, "git", "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("aborting rebase: %w", err)
	}
	return nil
}

// ConflictFiles returns the list of files with conflict markers.
func ConflictFiles(ctx context.Context, r *shell.Runner) ([]string, error) {
	out, err := r.Run(ctx, "git", "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, fmt.Errorf("listing conflict files: %w", err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// SquashMerge checks out baseBranch in the main repo, runs git merge --squash
// from featureBranch, and commits with the given message.
func SquashMerge(ctx context.Context, r *shell.Runner, repoPath, featureBranch, baseBranch, commitMsg string) error {
	repoRunner := &shell.Runner{Dir: repoPath}

	if _, err := repoRunner.Run(ctx, "git", "checkout", baseBranch); err != nil {
		return fmt.Errorf("checking out %s: %w", baseBranch, err)
	}
	if _, err := repoRunner.Run(ctx, "git", "merge", "--squash", featureBranch); err != nil {
		return fmt.Errorf("squash merging %s: %w", featureBranch, err)
	}
	if _, err := repoRunner.Run(ctx, "git", "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("committing squash merge: %w", err)
	}
	return nil
}

// MainRepoPath returns the root of the main repository, even when called from
// inside a worktree. It uses git's common dir to find the shared .git directory,
// then returns its parent.
func MainRepoPath(ctx context.Context, r *shell.Runner) (string, error) {
	out, err := r.Run(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("getting git common dir: %w", err)
	}
	// --git-common-dir returns <main-repo>/.git for both worktrees and the
	// main repo itself. The parent of .git is the repo root.
	gitCommonDir := strings.TrimSpace(out)
	return filepath.Dir(gitCommonDir), nil
}

// CopyGlobPatterns copies files matching glob patterns from srcDir to dstDir.
// Supports single-level wildcards (*.json), recursive wildcards (**/*.json),
// literal paths (scripts/setup.sh), and directory paths (copied recursively).
// Preserves relative path structure in the destination.
// Patterns that match nothing invoke the warn callback but do not error.
func CopyGlobPatterns(srcDir, dstDir string, patterns []string, warn func(string)) error {
	for _, pattern := range patterns {
		srcPath := filepath.Join(srcDir, pattern)

		// Check if the pattern is a literal path to a directory.
		info, err := os.Stat(srcPath)
		if err == nil && info.IsDir() {
			// Copy directory recursively.
			if err := copyDir(srcPath, filepath.Join(dstDir, pattern)); err != nil {
				return fmt.Errorf("copying directory %s: %w", pattern, err)
			}
			continue
		}

		// Use doublestar for glob matching (supports **).
		matches, err := doublestar.Glob(os.DirFS(srcDir), pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}

		if len(matches) == 0 {
			warn(fmt.Sprintf("pattern %q matched no files", pattern))
			continue
		}

		for _, match := range matches {
			src := filepath.Join(srcDir, match)
			dst := filepath.Join(dstDir, match)

			// Skip directories — we only copy files (directories are created as needed).
			info, err := os.Stat(src)
			if err != nil {
				return fmt.Errorf("stat %s: %w", src, err)
			}
			if info.IsDir() {
				continue
			}

			// Ensure destination directory exists.
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return fmt.Errorf("creating directory for %s: %w", dst, err)
			}

			// Copy the file.
			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("reading %s: %w", src, err)
			}
			if err := os.WriteFile(dst, data, 0644); err != nil {
				return fmt.Errorf("writing %s: %w", dst, err)
			}
		}
	}
	return nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
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
