package commands

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/shell"
)

// Rebase rebases the current worktree branch onto the latest changes from a
// target branch (defaulting to the configured default_base). If conflicts
// occur, Claude is invoked interactively to resolve them.
func Rebase(args []string) error {
	fs := flag.NewFlagSet("rebase", flag.ExitOnError)
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
		return fmt.Errorf("ralph rebase must be run inside a worktree, not the main repo")
	}

	targetBranch := cfg.Repo.DefaultBase
	if fs.NArg() > 0 {
		targetBranch = fs.Arg(0)
	}

	log.Printf("[rebase] fetching origin/%s", targetBranch)
	if err := gitops.FetchBranch(ctx, r, targetBranch); err != nil {
		return err
	}

	log.Printf("[rebase] rebasing onto origin/%s", targetBranch)
	result, err := gitops.StartRebase(ctx, r, "origin/"+targetBranch)
	if err != nil {
		return err
	}

	if result.Success {
		log.Println("[rebase] rebase completed successfully — no conflicts")
		return nil
	}

	for result.HasConflicts {
		if err := resolveConflicts(ctx, r, cfg, targetBranch); err != nil {
			return err
		}

		inProgress, err := gitops.HasRebaseInProgress(ctx, r)
		if err != nil {
			return fmt.Errorf("checking rebase status: %w", err)
		}
		if !inProgress {
			// Claude (or the user) either completed or aborted the rebase.
			// Check if we're back on the original branch (abort restores it)
			// vs. on the rebased branch (success).
			log.Println("[rebase] rebase is no longer in progress after conflict resolution")
			return nil
		}

		// Rebase is still in progress — Claude resolved the current conflict
		// but didn't continue, or there are more commits to replay.
		result, err = gitops.ContinueRebase(ctx, r)
		if err != nil {
			return err
		}
	}

	log.Println("[rebase] rebase completed successfully")
	return nil
}

func resolveConflicts(ctx context.Context, r *shell.Runner, cfg *config.Config, targetBranch string) error {
	conflictFiles, err := gitops.ConflictFiles(ctx, r)
	if err != nil {
		return fmt.Errorf("listing conflict files: %w", err)
	}

	log.Printf("[rebase] conflicts detected in %d file(s): %s", len(conflictFiles), strings.Join(conflictFiles, ", "))

	prompt, err := buildConflictPrompt(ctx, r, cfg, targetBranch, conflictFiles)
	if err != nil {
		return fmt.Errorf("building conflict prompt: %w", err)
	}

	log.Println("[rebase] invoking Claude to resolve conflicts...")
	_, err = claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:   prompt,
		Print:    true,
		MaxTurns: 30,
	})
	if err != nil {
		log.Printf("[rebase] Claude session ended with error: %v", err)
	}

	return nil
}

func buildConflictPrompt(ctx context.Context, r *shell.Runner, cfg *config.Config, targetBranch string, conflictFiles []string) (string, error) {
	data := prompts.RebaseConflictData{
		ConflictFiles: strings.Join(conflictFiles, "\n"),
	}

	prdData, err := prd.Read(cfg.StatePRDPath())
	if err == nil {
		data.PRDDescription = prdData.Description
		data.Stories = formatStories(prdData.UserStories)
	}

	progress, err := os.ReadFile(cfg.ProgressPath())
	if err == nil {
		data.Progress = string(progress)
	}

	currentBranch, _ := gitops.CurrentBranch(ctx, r)
	mergeBase, err := r.Run(ctx, "git", "merge-base", currentBranch, "origin/"+targetBranch)
	if err == nil {
		mb := strings.TrimSpace(mergeBase)
		featureDiff, _ := r.Run(ctx, "git", "diff", mb+"..."+currentBranch)
		data.FeatureDiff = featureDiff

		baseDiff, _ := r.Run(ctx, "git", "diff", mb+"...origin/"+targetBranch)
		data.BaseDiff = baseDiff
	}

	return prompts.RenderRebaseConflict(data)
}

func formatStories(stories []prd.Story) string {
	var buf strings.Builder
	for _, s := range stories {
		status := "pending"
		if s.Passes {
			status = "done"
		}
		fmt.Fprintf(&buf, "- %s: %s [%s]\n", s.ID, s.Title, status)
	}
	return buf.String()
}
