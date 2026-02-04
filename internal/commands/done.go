package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// doneNowFn returns the current time. Overridable in tests.
var doneNowFn = time.Now

// Done squash-merges the feature branch into the base branch. In workspace
// mode it auto-removes the workspace after merging. In base mode it keeps the
// current behavior with an optional cleanup prompt.
func Done(args []string) error {
	fs := flag.NewFlagSet("done", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	workspaceFlag := AddWorkspaceFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()

	wc, err := resolveWorkContextFromFlags(*workspaceFlag, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	printWorkspaceHeader(wc, cfg.Repo.Path)

	if wc.Name == "base" {
		return doneBase(ctx, cfg, os.Stdin)
	}
	return doneWorkspace(ctx, cfg, wc, os.Stdin)
}

// doneBase handles done from base mode: squash-merge + optional cleanup prompt.
func doneBase(ctx context.Context, cfg *config.Config, stdin *os.File) error {
	r := &shell.Runner{}

	isWt, err := gitops.IsWorktree(ctx, r)
	if err != nil {
		return fmt.Errorf("checking worktree: %w", err)
	}
	if !isWt {
		return fmt.Errorf("ralph done must be run inside a worktree or workspace")
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

	commitMsg, err := generateCommitMessage(cfg.StatePRDPath())
	if err != nil {
		return fmt.Errorf("generating commit message: %w", err)
	}

	editedMsg, err := promptEditMessage(commitMsg, stdin)
	if err != nil {
		return fmt.Errorf("reading user input: %w", err)
	}

	repoPath, err := gitops.MainRepoPath(ctx, r)
	if err != nil {
		return fmt.Errorf("resolving main repo path: %w", err)
	}

	log.Printf("[done] squash-merging %s into %s", featureBranch, baseBranch)
	if err := gitops.SquashMerge(ctx, r, repoPath, featureBranch, baseBranch, editedMsg); err != nil {
		return err
	}

	log.Println("[done] squash merge completed successfully")

	if shouldCleanup(stdin) {
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

// doneWorkspace handles done from workspace mode: squash-merge, archive PRD,
// auto-remove workspace, stdout base repo path.
func doneWorkspace(ctx context.Context, cfg *config.Config, wc workspace.WorkContext, stdin *os.File) error {
	r := &shell.Runner{Dir: wc.WorkDir}

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

	commitMsg, err := generateCommitMessage(wc.PRDPath)
	if err != nil {
		return fmt.Errorf("generating commit message: %w", err)
	}

	editedMsg, err := promptEditMessage(commitMsg, stdin)
	if err != nil {
		return fmt.Errorf("reading user input: %w", err)
	}

	repoPath := cfg.Repo.Path

	log.Printf("[done] squash-merging %s into %s", featureBranch, baseBranch)
	if err := gitops.SquashMerge(ctx, r, repoPath, featureBranch, baseBranch, editedMsg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Squash-merged %s into %s\n", featureBranch, baseBranch)

	// Archive PRD from workspace level BEFORE removing the workspace.
	archivePRDFromPath(wc.PRDPath, cfg)

	// Auto-remove workspace.
	repoRunner := &shell.Runner{Dir: repoPath}
	log.Printf("[done] removing workspace %s", wc.Name)
	if err := workspace.RemoveWorkspace(ctx, repoRunner, repoPath, wc.Name); err != nil {
		return fmt.Errorf("removing workspace: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Removed workspace '%s'\n", wc.Name)

	// stdout: base repo path for shell function cd.
	fmt.Println(repoPath)

	return nil
}

// archivePRDFromPath copies a PRD from sourcePath to the archive directory.
func archivePRDFromPath(sourcePath string, cfg *config.Config) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return
	}

	var p prd.PRD
	if err := json.Unmarshal(data, &p); err != nil || p.BranchName == "" {
		return
	}

	sanitized := sanitizeBranchForArchive(p.BranchName)
	destDir := filepath.Join(cfg.StateArchiveDir(),
		doneNowFn().Format("2006-01-02")+"-"+sanitized)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		log.Printf("[done] warning: could not create archive dir: %v", err)
		return
	}

	if err := os.WriteFile(filepath.Join(destDir, "prd.json"), data, 0644); err != nil {
		log.Printf("[done] warning: could not archive PRD: %v", err)
		return
	}

	log.Printf("[done] archived PRD to %s", destDir)
}

// generateCommitMessage builds a commit message from the PRD at the given path.
func generateCommitMessage(prdPath string) (string, error) {
	p, err := prd.Read(prdPath)
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
