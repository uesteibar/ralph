package rebase

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/workspace"
)

// BranchFetcher fetches a branch from origin.
type BranchFetcher interface {
	FetchBranch(ctx context.Context, workDir, branch string) error
}

// AncestorChecker checks if one commit is an ancestor of another.
type AncestorChecker interface {
	IsAncestor(ctx context.Context, workDir, ancestor, descendant string) (bool, error)
}

// ForcePusher force-pushes a branch to origin.
type ForcePusher interface {
	ForcePushBranch(ctx context.Context, workDir, branch string) error
}

// RebaseRunner runs ralph rebase as a subprocess.
type RebaseRunner interface {
	RunRebase(ctx context.Context, base, workspaceName, projectConfigPath string) error
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// DefaultBaseResolver resolves the default base branch from project config.
type DefaultBaseResolver interface {
	DefaultBase(projectLocalPath, ralphConfigPath string) (string, error)
}

// Config holds the dependencies for the rebase condition and action.
type Config struct {
	Fetcher  BranchFetcher
	Checker  AncestorChecker
	Pusher   ForcePusher
	Runner   RebaseRunner
	Projects ProjectGetter
	Resolver DefaultBaseResolver
}

// NeedsRebase returns a ConditionFunc that checks whether the issue's branch
// is behind origin/<base>. It returns false if the issue has no PR, if any
// git operation fails, or if the branch is already up-to-date.
func NeedsRebase(cfg Config) func(issue db.Issue) bool {
	return func(issue db.Issue) bool {
		if issue.PRNumber == 0 {
			return false
		}

		ctx := context.Background()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			slog.Warn("rebase condition: loading project", "error", err)
			return false
		}

		base, err := cfg.Resolver.DefaultBase(project.LocalPath, project.RalphConfigPath)
		if err != nil {
			slog.Warn("rebase condition: resolving default base", "error", err)
			return false
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		if err := cfg.Fetcher.FetchBranch(ctx, treePath, base); err != nil {
			slog.Warn("rebase condition: fetching base branch", "branch", base, "error", err)
			return false
		}

		isAnc, err := cfg.Checker.IsAncestor(ctx, treePath, "origin/"+base, "HEAD")
		if err != nil {
			slog.Warn("rebase condition: checking ancestry", "error", err)
			return false
		}

		// Branch needs rebase when origin/<base> is NOT an ancestor of HEAD
		return !isAnc
	}
}

// NewAction returns an ActionFunc that rebases the issue's branch onto the
// latest base branch and force pushes. It invokes ralph rebase as a subprocess
// for AI-powered conflict resolution.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		ctx := context.Background()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		base, err := cfg.Resolver.DefaultBase(project.LocalPath, project.RalphConfigPath)
		if err != nil {
			return fmt.Errorf("resolving default base: %w", err)
		}

		if err := database.LogActivity(issue.ID, "rebase_start", "", "", fmt.Sprintf("Rebasing %s onto %s", issue.Identifier, base)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		projectConfigPath := filepath.Join(project.LocalPath, project.RalphConfigPath)

		if err := cfg.Runner.RunRebase(ctx, base, issue.WorkspaceName, projectConfigPath); err != nil {
			return fmt.Errorf("running rebase: %w", err)
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		if err := cfg.Pusher.ForcePushBranch(ctx, treePath, issue.BranchName); err != nil {
			return fmt.Errorf("force pushing branch: %w", err)
		}

		if err := database.LogActivity(issue.ID, "rebase_finish", "", "", fmt.Sprintf("Rebased onto %s and force pushed", base)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}
