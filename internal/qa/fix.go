package qa

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/workspace"
)

// maxTurnsFix limits the number of agentic turns for QA fix.
const maxTurnsFix = 30

// HookRunner runs lifecycle hooks around commits.
type HookRunner interface {
	RunPreCommit(ctx context.Context, workDir string) error
	RunPostCommit(ctx context.Context, workDir string) error
}

// FixConfig holds the dependencies for the QA fix action.
type FixConfig struct {
	Invoker      invoker.EventInvoker
	Projects     ProjectGetter
	ConfigLoad   ConfigLoader
	BranchPuller BranchPuller
	Git          GitOps
	Hooks        HookRunner
	EventHandler events.EventHandler
	OnBuildEvent func(issueID, detail string)
	OverrideDir  string
}

// NewFixAction returns an orchestrator ActionFunc for QA fix.
// It reads findings from the PRD, passes them to the fix agent prompt,
// runs pre-commit hooks, commits and pushes changes, then increments
// QAFixAttempts.
func NewFixAction(cfg FixConfig) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		// Create a context with timeout to prevent indefinite hangs.
		// QA fix involves AI invocation, quality checks, and git operations,
		// which should complete within 60 minutes under normal circumstances.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		// Load quality checks from ralph config.
		var qualityChecks []string
		if cfg.ConfigLoad != nil {
			ralphConfigPath := filepath.Join(project.LocalPath, project.RalphConfigPath)
			ralphCfg, err := cfg.ConfigLoad.Load(ralphConfigPath)
			if err != nil {
				return fmt.Errorf("loading ralph config: %w", err)
			}
			qualityChecks = ralphCfg.QualityChecks
		}

		if err := database.LogActivity(issue.ID, "qa_fix_start", "", "", fmt.Sprintf("QA fix for %s", issue.Identifier)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		if err := cfg.BranchPuller.PullBranch(ctx, treePath, issue.BranchName); err != nil {
			// When QA runs right after build (before PR creation), the branch
			// only exists locally — skip the pull gracefully in that case.
			if !isRemoteRefNotFound(err) {
				return fmt.Errorf("pulling branch: %w", err)
			}
		}

		// Resolve paths for the prompt.
		prdPath := workspace.PRDPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		progressPath := workspace.ProgressPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		qaReportPath := workspace.QAReportPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		qaScriptsPath := workspace.QAScriptsPathForWorkspace(project.LocalPath, issue.WorkspaceName)

		// Read PRD to get findings.
		p, err := prd.Read(prdPath)
		if err != nil {
			return fmt.Errorf("reading PRD: %w", err)
		}

		var findings []prd.QAFinding
		if p.QAVerification != nil {
			for _, f := range p.QAVerification.Findings {
				if f.Status == "found" {
					findings = append(findings, f)
				}
			}
		}

		prompt, err := prompts.RenderQAFix(prompts.QAFixData{
			PRDPath:       prdPath,
			ProgressPath:  progressPath,
			QualityChecks: qualityChecks,
			QAReportPath:  qaReportPath,
			QAScriptsPath: qaScriptsPath,
			KnowledgePath: knowledge.Dir(treePath),
			Findings:      findings,
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering qa_fix prompt: %w", err)
		}

		handler := eventlog.New(database, issue.ID, cfg.EventHandler, cfg.OnBuildEvent, nil)
		if _, err := cfg.Invoker.InvokeWithEvents(ctx, prompt, treePath, maxTurnsFix, handler); err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		// Run pre-commit hooks (e.g. formatters, generators).
		if cfg.Hooks != nil {
			_ = cfg.Hooks.RunPreCommit(ctx, treePath)
		}

		// Try to commit and push. If nothing changed, skip gracefully.
		if err := cfg.Git.Commit(ctx, treePath, "Fix QA issues"); err != nil {
			if !isNothingToCommitFix(err) {
				return fmt.Errorf("committing changes: %w", err)
			}
		} else {
			// Run post-commit hooks after a successful commit.
			if cfg.Hooks != nil {
				_ = cfg.Hooks.RunPostCommit(ctx, treePath)
			}
			if err := cfg.Git.PushBranch(ctx, treePath, issue.BranchName); err != nil {
				return fmt.Errorf("pushing changes: %w", err)
			}
		}

		// Increment QAFixAttempts.
		issue.QAFixAttempts++
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("updating issue: %w", err)
		}

		if err := database.LogActivity(issue.ID, "qa_fix_finish", "", "", fmt.Sprintf("QA fix attempt %d completed", issue.QAFixAttempts)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// isNothingToCommitFix returns true when a git commit error indicates there
// was nothing to commit (no staged changes).
func isNothingToCommitFix(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "nothing to commit") ||
		strings.Contains(msg, "exited with code 1")
}
