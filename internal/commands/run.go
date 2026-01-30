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
		}
	}

	// 4. Create worktree
	worktreePath, err := gitops.CreateWorktree(ctx, r, cfg.Repo.Path, p.BranchName, cfg.Repo.DefaultBase)
	if err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}
	log.Printf("[run] created worktree at %s", worktreePath)

	// 5. Copy .ralph into worktree
	if err := gitops.CopyDotRalph(cfg.Repo.Path, worktreePath); err != nil {
		return fmt.Errorf("copying .ralph: %w", err)
	}

	// 6. Write prd.json into worktree
	worktreePRDPath := filepath.Join(worktreePath, ".ralph", "state", "prd.json")
	if err := os.MkdirAll(filepath.Dir(worktreePRDPath), 0755); err != nil {
		return fmt.Errorf("creating worktree state directory: %w", err)
	}
	if err := prd.Write(worktreePRDPath, p); err != nil {
		return fmt.Errorf("writing PRD to worktree: %w", err)
	}

	// 7. Run the execution loop
	err = loop.Run(ctx, loop.Config{
		MaxIterations: *maxIter,
		WorkDir:       worktreePath,
		PRDPath:       worktreePRDPath,
		ProgressPath:  filepath.Join(worktreePath, ".ralph", "progress.txt"),
		QualityChecks: cfg.QualityChecks,
	})
	if err != nil {
		log.Printf("[run] loop ended: %v", err)
		return err
	}

	log.Printf("[run] complete for branch %s", p.BranchName)
	return nil
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
