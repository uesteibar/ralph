package qa

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/workspace"
)

// maxTurnsVerify limits the number of agentic turns for QA verification.
const maxTurnsVerify = 30

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// ConfigLoader loads a Ralph config from a file path.
type ConfigLoader interface {
	Load(path string) (*config.Config, error)
}

// BranchPuller pulls the latest remote branch state into the local worktree.
type BranchPuller interface {
	PullBranch(ctx context.Context, workDir, branch string) error
}

// GitOps abstracts git operations for the QA verify action.
type GitOps interface {
	Commit(ctx context.Context, workDir, message string) error
	PushBranch(ctx context.Context, workDir, branch string) error
}

// CommandRunner executes a shell command in a directory and returns the error
// (nil on exit code 0).
type CommandRunner interface {
	Run(ctx context.Context, dir, command string) error
}

// Config holds the dependencies for the QA verification action.
type Config struct {
	Invoker      invoker.EventInvoker
	Projects     ProjectGetter
	ConfigLoad   ConfigLoader
	BranchPuller BranchPuller
	Git          GitOps
	Runner       CommandRunner
	EventHandler events.EventHandler
	OnBuildEvent func(issueID, detail string)
	OverrideDir  string
	MaxAttempts  int
}

// NewVerifyAction returns an orchestrator ActionFunc for QA verification.
// It invokes an adversarial QA agent that writes test scripts to the workspace
// qa-scripts/ directory and reports findings in prd.json. Quality check
// failures are also recorded as findings. The PRD qaVerification field is
// updated accordingly.
func NewVerifyAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	return func(issue db.Issue, database *db.DB) error {
		// Create a context with timeout to prevent indefinite hangs.
		// QA verification involves AI invocation, quality checks, and git operations,
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

		if err := database.LogActivity(issue.ID, "qa_verify_start", "", "", fmt.Sprintf("QA verification for %s", issue.Identifier)); err != nil {
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

		// Resolve paths for the workspace.
		prdPath := workspace.PRDPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		progressPath := workspace.ProgressPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		qaReportPath := workspace.QAReportPathForWorkspace(project.LocalPath, issue.WorkspaceName)
		qaScriptsPath := workspace.QAScriptsPathForWorkspace(project.LocalPath, issue.WorkspaceName)

		// Ensure qa-scripts/ directory exists.
		if err := os.MkdirAll(qaScriptsPath, 0755); err != nil {
			return fmt.Errorf("creating qa-scripts directory: %w", err)
		}

		// Render qa_verify.md prompt.
		prompt, err := prompts.RenderQAVerify(prompts.QAVerifyData{
			PRDPath:       prdPath,
			ProgressPath:  progressPath,
			QualityChecks: qualityChecks,
			KnowledgePath: knowledge.Dir(treePath),
			QAReportPath:  qaReportPath,
			QAScriptsPath: qaScriptsPath,
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering qa_verify prompt: %w", err)
		}

		handler := eventlog.New(database, issue.ID, cfg.EventHandler, cfg.OnBuildEvent, nil)
		_, invokeErr := cfg.Invoker.InvokeWithEvents(ctx, prompt, treePath, maxTurnsVerify, handler)

		// Always try to read the PRD, even if invocation had an error.
		// The AI might have completed its work successfully even if the process
		// didn't exit cleanly (e.g., hung after writing results).
		p, err := prd.Read(prdPath)
		if err != nil {
			// PRD is unreadable - this is a true failure.
			// If invocation also failed, that's the primary error.
			if invokeErr != nil {
				return fmt.Errorf("invoking AI: %w", invokeErr)
			}
			return fmt.Errorf("reading PRD: %w", err)
		}

		if p.QAVerification == nil {
			p.QAVerification = &prd.QAVerification{Status: "pending", Attempts: 0}
		}

		// If invocation failed but PRD already shows "passed", the AI completed
		// successfully even though the process didn't exit cleanly. We can proceed.
		// Otherwise, if invocation failed and status is not "passed", return the error.
		if invokeErr != nil && p.QAVerification.Status != "passed" {
			return fmt.Errorf("invoking AI: %w", invokeErr)
		}

		// Run quality checks independently and reconcile findings.
		for _, check := range qualityChecks {
			cmd := fmt.Sprintf("ralph check %s", check)
			if err := cfg.Runner.Run(ctx, treePath, cmd); err != nil {
				// Add a finding for the quality check failure if not already reported.
				if !hasQualityCheckFinding(p.QAVerification, check) {
					findingID := nextFindingID(p.QAVerification)
					p.QAVerification.Findings = append(p.QAVerification.Findings, prd.QAFinding{
						ID:          findingID,
						Title:       fmt.Sprintf("Quality check '%s' failed", check),
						Description: fmt.Sprintf("The quality check command '%s' exited with a non-zero status.", cmd),
						Severity:    "error",
						Status:      "found",
					})
				}
			} else {
				// Quality check passes now — remove any previously reported finding for it.
				removeQualityCheckFinding(p.QAVerification, check)
			}
		}

		if len(p.QAVerification.Findings) == 0 || !prd.HasUnfixedFindings(p.QAVerification) {
			// All findings resolved (or none found) and quality checks passed.
			// Even if the invocation had an error (e.g., process didn't exit cleanly),
			// the AI successfully completed its work and QA passed.
			p.QAVerification.Status = "passed"
			if err := prd.Write(prdPath, p); err != nil {
				return fmt.Errorf("writing PRD: %w", err)
			}

			if err := database.LogActivity(issue.ID, "qa_verify_finish", "", "", "QA verification passed"); err != nil {
				return fmt.Errorf("logging activity: %w", err)
			}
			return nil
		}

		// Findings exist — mark as failed.
		// If we had an invocation error AND findings exist, return the invocation error
		// as it indicates the AI didn't complete successfully.
		if invokeErr != nil {
			return fmt.Errorf("invoking AI (with %d findings): %w", len(p.QAVerification.Findings), invokeErr)
		}
		p.QAVerification.Status = "failed"
		p.QAVerification.Attempts++
		if err := prd.Write(prdPath, p); err != nil {
			return fmt.Errorf("writing PRD: %w", err)
		}

		// Check loop exhaustion.
		if p.QAVerification.Attempts >= maxAttempts {
			issue.State = string(orchestrator.StatePaused)
			if err := database.UpdateIssue(issue); err != nil {
				return fmt.Errorf("pausing issue: %w", err)
			}

			detail := fmt.Sprintf("QA verification failed after %d attempts with %d findings", maxAttempts, len(p.QAVerification.Findings))
			if err := database.LogActivity(issue.ID, "qa_paused", "", "", detail); err != nil {
				return fmt.Errorf("logging activity: %w", err)
			}
			return nil
		}

		findingTitles := make([]string, 0, len(p.QAVerification.Findings))
		for _, f := range p.QAVerification.Findings {
			if f.Status == "found" {
				findingTitles = append(findingTitles, f.ID+": "+f.Title)
			}
		}
		failureDetail := fmt.Sprintf("QA findings: %s", strings.Join(findingTitles, "; "))
		if err := database.LogActivity(issue.ID, "qa_verify_finish", "", "", failureDetail); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return fmt.Errorf("qa verification failed: %d findings", len(p.QAVerification.Findings))
	}
}

// hasQualityCheckFinding returns true if a finding for the given quality check
// already exists in the QA verification.
func hasQualityCheckFinding(qa *prd.QAVerification, check string) bool {
	title := fmt.Sprintf("Quality check '%s' failed", check)
	for _, f := range qa.Findings {
		if f.Title == title {
			return true
		}
	}
	return false
}

// removeQualityCheckFinding removes a quality check finding (by title match) from the list.
func removeQualityCheckFinding(qa *prd.QAVerification, check string) {
	title := fmt.Sprintf("Quality check '%s' failed", check)
	for i, f := range qa.Findings {
		if f.Title == title {
			qa.Findings = append(qa.Findings[:i], qa.Findings[i+1:]...)
			return
		}
	}
}

// nextFindingID returns the next sequential finding ID (QA-001, QA-002, etc.).
func nextFindingID(qa *prd.QAVerification) string {
	maxNum := 0
	for _, f := range qa.Findings {
		var num int
		if _, err := fmt.Sscanf(f.ID, "QA-%d", &num); err == nil && num > maxNum {
			maxNum = num
		}
	}
	return fmt.Sprintf("QA-%03d", maxNum+1)
}

// isRemoteRefNotFound returns true when a git error indicates the remote
// branch does not exist (e.g. first QA run before PR creation).
func isRemoteRefNotFound(err error) bool {
	return strings.Contains(err.Error(), "couldn't find remote ref")
}

// shellRunner is the default CommandRunner that executes commands via sh -c.
type shellRunner struct{}

// Run executes a command string via sh -c in the given directory.
func (r *shellRunner) Run(ctx context.Context, dir, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	return cmd.Run()
}

// NewShellRunner returns a CommandRunner that executes commands via sh -c.
func NewShellRunner() CommandRunner {
	return &shellRunner{}
}
