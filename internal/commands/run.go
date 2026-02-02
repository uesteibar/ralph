package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
)

// Run executes the Ralph loop from a staged PRD.
func Run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	maxIter := fs.Int("max-iterations", loop.DefaultMaxIterations, "Maximum loop iterations")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	ctx := context.Background()

	// Check if we're already inside a worktree — if so, run the loop here directly.
	if wtRoot, ok := worktreeRoot(cfg.Repo.Path); ok {
		log.Printf("[run] detected worktree at %s, resuming", wtRoot)
		return runLoop(ctx, wtRoot, *maxIter, cfg.QualityChecks)
	}

	// 1. Read staged PRD
	stagedPath := cfg.StatePRDPath()
	p, err := prd.Read(stagedPath)
	if err != nil {
		return fmt.Errorf("reading staged PRD at %s: %w\n\nRun 'ralph prd new' first to create a PRD.", stagedPath, err)
	}

	if p.BranchName == "" {
		return fmt.Errorf("staged PRD is missing branchName")
	}
	if len(p.UserStories) == 0 {
		return fmt.Errorf("staged PRD has no user stories")
	}

	log.Printf("[run] PRD: %s (%d stories, branch: %s)", p.Description, len(p.UserStories), p.BranchName)

	// 2. Validate branch name
	if cfg.Repo.BranchPattern != "" {
		re, err := regexp.Compile(cfg.Repo.BranchPattern)
		if err != nil {
			return fmt.Errorf("invalid branch pattern %q: %w", cfg.Repo.BranchPattern, err)
		}
		if !re.MatchString(p.BranchName) {
			return fmt.Errorf("branch %q does not match pattern %q", p.BranchName, cfg.Repo.BranchPattern)
		}
	}

	// 3. Handle existing branch
	r := &shell.Runner{Dir: cfg.Repo.Path}
	var worktreePath string
	resuming := false

	if gitops.BranchExistsLocally(ctx, r, p.BranchName) {
		choice, err := promptBranchConflict(p.BranchName, cfg.Repo.DefaultBase)
		if err != nil {
			return err
		}
		if choice == "fresh" {
			wtPath := gitops.WorktreePath(cfg.Repo.Path, p.BranchName)
			if _, err := os.Stat(wtPath); err == nil {
				_ = gitops.RemoveWorktree(ctx, r, cfg.Repo.Path, wtPath)
			}
			if err := gitops.DeleteBranch(ctx, r, p.BranchName); err != nil {
				return fmt.Errorf("deleting existing branch: %w", err)
			}
			log.Printf("[run] deleted branch %s, starting fresh", p.BranchName)
		} else {
			worktreePath = gitops.WorktreePath(cfg.Repo.Path, p.BranchName)
			resuming = true
			log.Printf("[run] resuming in existing worktree at %s", worktreePath)
		}
	}

	if !resuming {
		// 4. Create worktree
		var createErr error
		worktreePath, createErr = gitops.CreateWorktree(ctx, r, cfg.Repo.Path, p.BranchName, cfg.Repo.DefaultBase)
		if createErr != nil {
			return fmt.Errorf("creating worktree: %w", createErr)
		}
		log.Printf("[run] created worktree at %s", worktreePath)

		// 5. Copy .ralph into worktree
		if err := gitops.CopyDotRalph(cfg.Repo.Path, worktreePath); err != nil {
			return fmt.Errorf("copying .ralph: %w", err)
		}

		// 6. Write prd.json into worktree
		prdPath := filepath.Join(worktreePath, ".ralph", "state", "prd.json")
		if err := os.MkdirAll(filepath.Dir(prdPath), 0755); err != nil {
			return fmt.Errorf("creating worktree state directory: %w", err)
		}
		if err := prd.Write(prdPath, p); err != nil {
			return fmt.Errorf("writing PRD to worktree: %w", err)
		}
	}

	return runLoop(ctx, worktreePath, *maxIter, cfg.QualityChecks)
}

func runLoop(ctx context.Context, workDir string, maxIter int, qualityChecks []string) error {
	worktreePRDPath := filepath.Join(workDir, ".ralph", "state", "prd.json")
	err := loop.Run(ctx, loop.Config{
		MaxIterations: maxIter,
		WorkDir:       workDir,
		PRDPath:       worktreePRDPath,
		ProgressPath:  filepath.Join(workDir, ".ralph", "progress.txt"),
		QualityChecks: qualityChecks,
	})
	if err != nil {
		log.Printf("[run] loop ended: %v", err)
		return err
	}

	log.Println("[run] complete")
	return nil
}

// worktreeRoot checks if repoPath (derived from config file location) is
// inside a .ralph/worktrees/ directory. This happens when the user runs
// `ralph run` from within an existing worktree (which has a copied
// .ralph/ralph.yaml). Returns the worktree root path and true if detected.
func worktreeRoot(repoPath string) (string, bool) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return "", false
	}
	sep := string(filepath.Separator)
	marker := sep + ".ralph" + sep + "worktrees" + sep
	idx := strings.Index(absPath, marker)
	if idx < 0 {
		return "", false
	}
	// The worktree root is the first path component after .ralph/worktrees/
	afterMarker := absPath[idx+len(marker):]
	slashIdx := strings.Index(afterMarker, sep)
	if slashIdx >= 0 {
		return absPath[:idx+len(marker)+slashIdx], true
	}
	return absPath, true
}

// promptBranchConflict asks the user whether to start fresh or resume an
// existing branch. Returns "fresh" or "resume".
func promptBranchConflict(branch, base string) (string, error) {
	fmt.Fprintf(os.Stderr, "\nBranch %q already exists.\n", branch)
	fmt.Fprintf(os.Stderr, "  [1] Start fresh (delete branch and recreate from %s) [default]\n", base)
	fmt.Fprintf(os.Stderr, "  [2] Resume (reuse existing branch)\n")
	fmt.Fprintf(os.Stderr, "Choice [1]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "2" {
			return "resume", nil
		}
	}
	return "fresh", nil
}
