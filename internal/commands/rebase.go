package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Rebase rebases the current workspace branch onto the latest changes from a
// target branch (defaulting to the configured default_base). If conflicts
// occur, Claude is invoked interactively to resolve them.
func Rebase(args []string) error {
	fs := flag.NewFlagSet("rebase", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	workspaceFlag := AddWorkspaceFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	wc, err := resolveWorkContextFromFlags(*workspaceFlag, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	printWorkspaceHeader(wc, cfg.Repo.Path)

	if wc.Name == "base" {
		return fmt.Errorf("Rebase requires workspace context. Use --workspace <name> or switch to a workspace.")
	}

	ctx := context.Background()
	r := &shell.Runner{Dir: wc.WorkDir}

	targetBranch := cfg.Repo.DefaultBase
	if fs.NArg() > 0 {
		targetBranch = fs.Arg(0)
	}

	fmt.Fprintf(os.Stderr, "fetching origin/%s\n", targetBranch)
	if err := gitops.FetchBranch(ctx, r, targetBranch); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "rebasing onto origin/%s\n", targetBranch)
	result, err := gitops.StartRebase(ctx, r, "origin/"+targetBranch)
	if err != nil {
		return err
	}

	if result.Success {
		fmt.Fprintln(os.Stderr, "rebase completed successfully — no conflicts")
		return nil
	}

	promptsDir := cfg.PromptsDir()

	for result.HasConflicts {
		if err := resolveConflicts(ctx, r, wc, targetBranch, promptsDir); err != nil {
			return err
		}

		inProgress, err := gitops.HasRebaseInProgress(ctx, r)
		if err != nil {
			return fmt.Errorf("checking rebase status: %w", err)
		}
		if !inProgress {
			fmt.Fprintln(os.Stderr, "rebase completed successfully")
			return nil
		}

		result, err = gitops.ContinueRebase(ctx, r)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "rebase completed successfully")
	return nil
}

func resolveConflicts(ctx context.Context, r *shell.Runner, wc workspace.WorkContext, targetBranch, promptsDir string) error {
	conflictFiles, err := gitops.ConflictFiles(ctx, r)
	if err != nil {
		return fmt.Errorf("listing conflict files: %w", err)
	}

	fmt.Fprintf(os.Stderr, "conflicts detected in %d file(s): %s\n", len(conflictFiles), strings.Join(conflictFiles, ", "))

	prompt, err := buildConflictPrompt(ctx, r, wc, targetBranch, conflictFiles, promptsDir)
	if err != nil {
		return fmt.Errorf("building conflict prompt: %w", err)
	}

	fmt.Fprintln(os.Stderr, "invoking Claude to resolve conflicts...")
	_, err = claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:   prompt,
		Print:    true,
		MaxTurns: 30,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Claude session ended with error: %v\n", err)
	}

	return nil
}

func buildConflictPrompt(ctx context.Context, r *shell.Runner, wc workspace.WorkContext, targetBranch string, conflictFiles []string, promptsDir string) (string, error) {
	data := prompts.RebaseConflictData{
		ConflictFiles: strings.Join(conflictFiles, "\n"),
	}

	// Read PRD from workspace-level path for conflict resolution context.
	prdData, err := prd.Read(wc.PRDPath)
	if err == nil {
		data.PRDDescription = prdData.Description
		data.Stories = formatStories(prdData.UserStories)
	}

	progressPath := filepath.Join(wc.WorkDir, ".ralph", "progress.txt")
	progress, err := os.ReadFile(progressPath)
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

	return prompts.RenderRebaseConflict(data, promptsDir)
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
