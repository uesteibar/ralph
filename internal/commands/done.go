package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
)

// Done squash-merges the current worktree's feature branch into the base
// branch as a single commit, with an auto-generated commit message the user
// can edit inline.
func Done(args []string) error {
	fs := flag.NewFlagSet("done", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()
	r := &shell.Runner{}

	isWt, err := gitops.IsWorktree(ctx, r)
	if err != nil {
		return fmt.Errorf("checking worktree: %w", err)
	}
	if !isWt {
		return fmt.Errorf("ralph done must be run inside a worktree, not the main repo")
	}

	baseBranch := cfg.Repo.DefaultBase

	featureBranch, err := gitops.CurrentBranch(ctx, r)
	if err != nil {
		return fmt.Errorf("getting current branch: %w", err)
	}

	log.Printf("[done] fetching origin/%s", baseBranch)
	if err := gitops.FetchBranch(ctx, r, baseBranch); err != nil {
		return err
	}

	upToDate, err := gitops.IsAncestor(ctx, r, "origin/"+baseBranch, "HEAD")
	if err != nil {
		return fmt.Errorf("checking if base is ancestor of HEAD: %w", err)
	}
	if !upToDate {
		return fmt.Errorf("origin/%s is not an ancestor of HEAD — run `ralph rebase` first to incorporate the latest changes", baseBranch)
	}

	commitMsg, err := generateCommitMessage(cfg)
	if err != nil {
		return fmt.Errorf("generating commit message: %w", err)
	}

	editedMsg, err := promptEditMessage(commitMsg, os.Stdin)
	if err != nil {
		return fmt.Errorf("reading user input: %w", err)
	}

	// Resolve the main repo path (cfg.Repo.Path points to the worktree when
	// running from inside one, but squash merge needs the actual main repo).
	repoPath, err := gitops.MainRepoPath(ctx, r)
	if err != nil {
		return fmt.Errorf("resolving main repo path: %w", err)
	}

	log.Printf("[done] squash-merging %s into %s", featureBranch, baseBranch)
	if err := gitops.SquashMerge(ctx, r, repoPath, featureBranch, baseBranch, editedMsg); err != nil {
		return err
	}

	log.Println("[done] squash merge completed successfully")

	if shouldCleanup(os.Stdin) {
		wtPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting worktree path: %w", err)
		}
		log.Printf("[done] removing worktree and deleting branch %s", featureBranch)
		if err := gitops.RemoveWorktree(ctx, r, repoPath, wtPath); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}
		repoRunner := &shell.Runner{Dir: repoPath}
		if err := gitops.DeleteBranch(ctx, repoRunner, featureBranch); err != nil {
			return fmt.Errorf("deleting branch: %w", err)
		}
		log.Println("[done] cleanup complete")
	} else {
		log.Println("[done] leaving worktree and branch in place")
	}

	return nil
}

// generateCommitMessage builds a commit message from the PRD description and
// completed story titles.
func generateCommitMessage(cfg *config.Config) (string, error) {
	p, err := prd.Read(cfg.StatePRDPath())
	if err != nil {
		return "", fmt.Errorf("reading PRD: %w", err)
	}

	var buf strings.Builder
	buf.WriteString(p.Description)
	buf.WriteString("\n\nCompleted stories:\n")
	for _, s := range p.UserStories {
		if s.Passes {
			fmt.Fprintf(&buf, "- %s: %s\n", s.ID, s.Title)
		}
	}
	return buf.String(), nil
}

// promptEditMessage displays the draft message and lets the user accept or
// replace it via stdin.
func promptEditMessage(draft string, stdin *os.File) (string, error) {
	fmt.Println("--- Generated commit message ---")
	fmt.Println(draft)
	fmt.Println("--- End of message ---")
	fmt.Print("Press Enter to accept, or type a new message: ")

	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			return line, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return draft, nil
}

// shouldCleanup prompts the user whether to remove the worktree and delete the
// feature branch.
func shouldCleanup(stdin *os.File) bool {
	fmt.Print("Clean up worktree and delete feature branch? (y/n): ")
	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}
