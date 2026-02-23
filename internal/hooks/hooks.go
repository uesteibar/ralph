package hooks

import (
	"context"
	"log/slog"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/shell"
)

// Runner executes hook commands and auto-commits any file changes they produce.
type Runner struct {
	cfg    config.HooksConfig
	gitEnv []string
}

// New creates a Runner with the given hooks configuration and git environment
// variables (e.g. GIT_AUTHOR_NAME, GIT_COMMITTER_EMAIL). Pass nil for gitEnv
// to inherit the parent process environment.
func New(cfg config.HooksConfig, gitEnv []string) *Runner {
	return &Runner{cfg: cfg, gitEnv: gitEnv}
}

// RunPreCommit executes each pre_commit hook sequentially.
func (r *Runner) RunPreCommit(ctx context.Context, workDir string) error {
	r.runHooks(ctx, workDir, r.cfg.PreCommit)
	return nil
}

// RunPostCommit executes each post_commit hook sequentially.
func (r *Runner) RunPostCommit(ctx context.Context, workDir string) error {
	r.runHooks(ctx, workDir, r.cfg.PostCommit)
	return nil
}

// RunPrePR executes each pre_pr hook sequentially.
func (r *Runner) RunPrePR(ctx context.Context, workDir string) error {
	r.runHooks(ctx, workDir, r.cfg.PrePR)
	return nil
}

func (r *Runner) runHooks(ctx context.Context, workDir string, hooks []string) {
	if len(hooks) == 0 {
		return
	}

	sh := &shell.Runner{Dir: workDir, Env: r.gitEnv}

	for _, cmd := range hooks {
		slog.Info("running hook", "command", cmd)

		if _, err := sh.Run(ctx, "sh", "-c", cmd); err != nil {
			slog.Warn("hook failed", "command", cmd, "error", err)
			continue
		}

		dirty, err := sh.GitHasUncommittedChanges(ctx)
		if err != nil {
			slog.Warn("checking dirty tree after hook", "command", cmd, "error", err)
			continue
		}
		if !dirty {
			continue
		}

		slog.Info("hook produced file changes, auto-committing", "command", cmd)
		if err := gitops.Commit(ctx, sh, cmd); err != nil {
			slog.Warn("auto-commit after hook failed", "command", cmd, "error", err)
		}
	}
}
