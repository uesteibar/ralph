package loop

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
)

const (
	// DefaultMaxQAAttempts is the default number of QA verify+fix cycles before giving up.
	DefaultMaxQAAttempts = 3

	qaVerifyMaxTurns = 30
	qaFixMaxTurns    = 30
)

// runQualityCheckFn runs a single quality check command in a directory.
// Package-level var for testability.
var runQualityCheckFn = func(ctx context.Context, dir, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	return cmd.Run()
}

// RunFull executes the stories loop via Run(), then runs QA verification
// and fix cycles. It returns nil when all stories pass and QA verification
// succeeds, or an error if max QA attempts are exhausted.
func RunFull(ctx context.Context, cfg Config) error {
	if err := Run(ctx, cfg); err != nil {
		return err
	}

	maxAttempts := cfg.MaxQAAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxQAAttempts
	}

	// Derive workspace-level paths from WorkDir.
	// WorkDir is the tree/ directory; the workspace root is its parent.
	qaReportPath := cfg.QAReportPath
	qaScriptsPath := cfg.QAScriptsPath

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// QA verification phase.
		emitEvent(cfg.EventHandler, events.QAVerifyStarted{})
		emitEvent(cfg.EventHandler, events.PRDRefresh{})

		if err := runQAVerify(ctx, cfg, qaReportPath, qaScriptsPath); err != nil {
			return fmt.Errorf("qa verification: %w", err)
		}

		// Read PRD to check findings and quality check status.
		p, err := prd.Read(cfg.PRDPath)
		if err != nil {
			return fmt.Errorf("reading PRD after QA verify: %w", err)
		}
		if p.QAVerification == nil {
			p.QAVerification = &prd.QAVerification{Status: "pending", Attempts: 0}
		}

		// Run quality checks independently and reconcile findings.
		for _, check := range cfg.QualityChecks {
			cmd := fmt.Sprintf("ralph check %s", check)
			if err := runQualityCheckFn(ctx, cfg.WorkDir, cmd); err != nil {
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

		if !prd.HasUnfixedFindings(p.QAVerification) {
			p.QAVerification.Status = "passed"
			if err := prd.Write(cfg.PRDPath, p); err != nil {
				return fmt.Errorf("writing PRD: %w", err)
			}
			emitEvent(cfg.EventHandler, events.QAComplete{Passed: true})
			emitLog(cfg.EventHandler, "QA verification passed")
			return nil
		}

		// Findings exist — mark as failed.
		p.QAVerification.Status = "failed"
		p.QAVerification.Attempts = attempt
		if err := prd.Write(cfg.PRDPath, p); err != nil {
			return fmt.Errorf("writing PRD: %w", err)
		}

		emitWarn(cfg.EventHandler, "QA findings reported: %d unfixed", countUnfixed(p.QAVerification))

		if attempt >= maxAttempts {
			break
		}

		// QA fix phase.
		emitEvent(cfg.EventHandler, events.QAFixStarted{})

		if err := runQAFix(ctx, cfg, qaReportPath, qaScriptsPath); err != nil {
			emitWarn(cfg.EventHandler, "QA fix error: %v", err)
		}
	}

	emitEvent(cfg.EventHandler, events.QAComplete{Passed: false})
	return fmt.Errorf("QA verification failed after %d attempts", maxAttempts)
}

// runQAVerify renders the qa_verify prompt and invokes Claude.
func runQAVerify(ctx context.Context, cfg Config, qaReportPath, qaScriptsPath string) error {
	// Ensure qa-scripts/ directory exists.
	if qaScriptsPath != "" {
		os.MkdirAll(qaScriptsPath, 0755)
	}

	prompt, err := prompts.RenderQAVerify(prompts.QAVerifyData{
		PRDPath:        cfg.PRDPath,
		ProgressPath:   cfg.ProgressPath,
		QualityChecks:  cfg.QualityChecks,
		QAInstructions: cfg.QAInstructions,
		KnowledgePath:  knowledge.Dir(cfg.WorkDir),
		QAReportPath:   qaReportPath,
		QAScriptsPath:  qaScriptsPath,
	}, cfg.PromptsDir)
	if err != nil {
		return fmt.Errorf("rendering qa_verify prompt: %w", err)
	}

	_, err = invokeWithUsageLimitWait(ctx, invokeOpts{
		prompt:       prompt,
		dir:          cfg.WorkDir,
		verbose:      cfg.Verbose,
		maxTurns:     qaVerifyMaxTurns,
		eventHandler: cfg.EventHandler,
	})
	return err
}

// runQAFix renders the qa_fix prompt and invokes Claude.
func runQAFix(ctx context.Context, cfg Config, qaReportPath, qaScriptsPath string) error {
	// Read PRD to get findings.
	var findings []prd.QAFinding
	if p, err := prd.Read(cfg.PRDPath); err == nil && p.QAVerification != nil {
		for _, f := range p.QAVerification.Findings {
			if f.Status == "found" {
				findings = append(findings, f)
			}
		}
	}

	prompt, err := prompts.RenderQAFix(prompts.QAFixData{
		PRDPath:        cfg.PRDPath,
		ProgressPath:   cfg.ProgressPath,
		QualityChecks:  cfg.QualityChecks,
		QAInstructions: cfg.QAInstructions,
		QAReportPath:   qaReportPath,
		QAScriptsPath:  qaScriptsPath,
		KnowledgePath:  knowledge.Dir(cfg.WorkDir),
		Findings:       findings,
	}, cfg.PromptsDir)
	if err != nil {
		return fmt.Errorf("rendering qa_fix prompt: %w", err)
	}

	_, err = invokeWithUsageLimitWait(ctx, invokeOpts{
		prompt:       prompt,
		dir:          cfg.WorkDir,
		verbose:      cfg.Verbose,
		maxTurns:     qaFixMaxTurns,
		eventHandler: cfg.EventHandler,
	})
	return err
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

// countUnfixed returns the number of findings with status "found".
func countUnfixed(qa *prd.QAVerification) int {
	count := 0
	for _, f := range qa.Findings {
		if f.Status == "found" {
			count++
		}
	}
	return count
}
