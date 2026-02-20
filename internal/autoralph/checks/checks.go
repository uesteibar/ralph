package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/workspace"
)

// maxTurnsChecks limits the number of agentic turns for fixing checks.
const maxTurnsChecks = 30

// CheckRunFetcher fetches check runs for a given ref.
type CheckRunFetcher interface {
	FetchCheckRuns(ctx context.Context, owner, repo, ref string) ([]github.CheckRun, error)
}

// LogFetcher fetches the log output for a check run.
type LogFetcher interface {
	FetchCheckRunLog(ctx context.Context, owner, repo string, checkRunID int64) ([]byte, error)
}

// PRFetcher fetches a pull request by number.
type PRFetcher interface {
	FetchPR(ctx context.Context, owner, repo string, prNumber int) (github.PR, error)
}

// PRCommenter posts a general comment on a pull request.
type PRCommenter interface {
	PostPRComment(ctx context.Context, owner, repo string, prNumber int, body string) (github.Comment, error)
}

// GitOps abstracts git operations for the checks action.
type GitOps interface {
	Commit(ctx context.Context, workDir, message string) error
	PushBranch(ctx context.Context, workDir, branch string) error
	HeadSHA(ctx context.Context, workDir string) (string, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// BranchPuller pulls the latest remote branch state into the local worktree.
type BranchPuller interface {
	PullBranch(ctx context.Context, workDir, branch string) error
}

// PRUpdater updates the PR description after changes are pushed.
type PRUpdater interface {
	UpdateDescription(ctx context.Context, issue db.Issue, project db.Project)
}

// ConfigLoader loads a Ralph config from a file path.
type ConfigLoader interface {
	Load(path string) (*config.Config, error)
}

// Config holds the dependencies for the check-fixing action.
type Config struct {
	Invoker      invoker.EventInvoker
	CheckRuns    CheckRunFetcher
	Logs         LogFetcher
	PRs          PRFetcher
	Comments     PRCommenter
	Git          GitOps
	BranchPuller BranchPuller
	Projects     ProjectGetter
	ConfigLoad   ConfigLoader
	PRUpdater    PRUpdater // optional: updates PR description after commit+push
	EventHandler events.EventHandler
	OnBuildEvent func(issueID, detail string)
	OverrideDir  string
	MaxAttempts  int
}

// NewAction returns an orchestrator ActionFunc for the fixing_checks -> in_review transition.
// It fetches failed check run details and logs, invokes AI to fix them, commits and pushes
// changes. When loop protection triggers (max attempts reached), it posts a PR comment and
// transitions to paused.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	return func(issue db.Issue, database *db.DB) error {
		ctx := context.Background()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		// Load quality checks from ralph.yaml if a ConfigLoader is provided.
		var qualityChecks []string
		if cfg.ConfigLoad != nil {
			ralphConfigPath := filepath.Join(project.LocalPath, project.RalphConfigPath)
			ralphCfg, err := cfg.ConfigLoad.Load(ralphConfigPath)
			if err != nil {
				return fmt.Errorf("loading ralph config: %w", err)
			}
			qualityChecks = ralphCfg.QualityChecks
		}

		if err := database.LogActivity(issue.ID, "checks_start", "", "", fmt.Sprintf("Fixing checks for %s", issue.Identifier)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		// Fetch PR head SHA
		pr, err := cfg.PRs.FetchPR(ctx, project.GithubOwner, project.GithubRepo, issue.PRNumber)
		if err != nil {
			return fmt.Errorf("fetching PR: %w", err)
		}

		// Fetch check runs for the head SHA
		checkRuns, err := cfg.CheckRuns.FetchCheckRuns(ctx, project.GithubOwner, project.GithubRepo, pr.HeadSHA)
		if err != nil {
			return fmt.Errorf("fetching check runs: %w", err)
		}

		// Filter to failed check runs
		var failed []github.CheckRun
		for _, cr := range checkRuns {
			if cr.Conclusion == "failure" {
				failed = append(failed, cr)
			}
		}

		if len(failed) == 0 {
			return nil
		}

		// Fetch logs for each failed check run, truncating to last 200 lines
		var failedChecks []ai.FailedCheckRun
		for _, cr := range failed {
			logBytes, err := cfg.Logs.FetchCheckRunLog(ctx, project.GithubOwner, project.GithubRepo, cr.ID)
			var logStr string
			if err == nil && len(logBytes) > 0 {
				logStr = truncateLog(string(logBytes), 200)
			}
			failedChecks = append(failedChecks, ai.FailedCheckRun{
				Name:       cr.Name,
				Conclusion: cr.Conclusion,
				Log:        logStr,
			})
		}

		// Invoke AI in the workspace worktree
		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		if err := cfg.BranchPuller.PullBranch(ctx, treePath, issue.BranchName); err != nil {
			return fmt.Errorf("pulling branch: %w", err)
		}

		// Render fix_checks.md template
		prompt, err := ai.RenderFixChecks(ai.FixChecksData{
			FailedChecks:  failedChecks,
			QualityChecks: qualityChecks,
			KnowledgePath: knowledge.Dir(treePath),
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering fix_checks prompt: %w", err)
		}
		handler := eventlog.New(database, issue.ID, cfg.EventHandler, cfg.OnBuildEvent)
		if _, err := cfg.Invoker.InvokeWithEvents(ctx, prompt, treePath, maxTurnsChecks, handler); err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		// Try to commit and push
		checkNames := make([]string, len(failed))
		for i, cr := range failed {
			checkNames[i] = cr.Name
		}
		commitMsg := fmt.Sprintf("Fix failing checks: %s", strings.Join(checkNames, ", "))

		committed := false
		if err := cfg.Git.Commit(ctx, treePath, commitMsg); err != nil {
			if !isNothingToCommit(err) {
				return fmt.Errorf("committing changes: %w", err)
			}
		} else {
			if err := cfg.Git.PushBranch(ctx, treePath, issue.BranchName); err != nil {
				return fmt.Errorf("pushing changes: %w", err)
			}
			committed = true
		}

		// Increment CheckFixAttempts and update LastCheckSHA
		issue.CheckFixAttempts++
		issue.LastCheckSHA = pr.HeadSHA
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("updating issue: %w", err)
		}

		// Check loop exhaustion
		if issue.CheckFixAttempts >= maxAttempts {
			// Post PR comment asking for human help (non-fatal)
			commentBody := fmt.Sprintf(
				"I could not fix the failing checks after %d attempts. Could you please have a look?",
				maxAttempts,
			)
			_, _ = cfg.Comments.PostPRComment(ctx, project.GithubOwner, project.GithubRepo, issue.PRNumber, commentBody)

			// Transition to paused
			issue.State = string(orchestrator.StatePaused)
			if err := database.UpdateIssue(issue); err != nil {
				return fmt.Errorf("pausing issue: %w", err)
			}

			if err := database.LogActivity(issue.ID, "checks_paused", "", "", fmt.Sprintf("Exhausted %d fix attempts", maxAttempts)); err != nil {
				return fmt.Errorf("logging activity: %w", err)
			}
			return nil
		}

		// Update PR description after successful commit+push (non-fatal).
		if committed && cfg.PRUpdater != nil {
			cfg.PRUpdater.UpdateDescription(ctx, issue, project)
		}

		detail := fmt.Sprintf("Fixed checks: %s", strings.Join(checkNames, ", "))
		if !committed {
			detail = fmt.Sprintf("No changes for checks: %s", strings.Join(checkNames, ", "))
		}
		if err := database.LogActivity(issue.ID, "checks_finish", "", "", detail); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// truncateLog keeps the first headLines and last tailLines of a log string,
// inserting a truncation marker between them when the log exceeds maxLines.
// Head preserves early error context; tail preserves recent state.
func truncateLog(log string, maxLines int) string {
	const headLines = 30
	tailLines := maxLines - headLines // 170 for maxLines=200

	lines := strings.Split(log, "\n")
	if len(lines) <= maxLines {
		return log
	}

	truncated := len(lines) - headLines - tailLines
	marker := fmt.Sprintf("[... %d lines truncated ...]", truncated)

	result := make([]string, 0, headLines+1+tailLines)
	result = append(result, lines[:headLines]...)
	result = append(result, marker)
	result = append(result, lines[len(lines)-tailLines:]...)
	return strings.Join(result, "\n")
}

// isNothingToCommit returns true when a git commit error indicates there was
// nothing to commit (no staged changes).
func isNothingToCommit(err error) bool {
	return strings.Contains(err.Error(), "nothing to commit") ||
		strings.Contains(err.Error(), "exited with code 1")
}
