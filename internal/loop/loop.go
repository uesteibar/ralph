package loop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/progress"
	"github.com/uesteibar/ralph/internal/prompts"
)

const (
	DefaultMaxIterations = 20
	iterationDelay       = 2 * time.Second

	// MaxTurns limits for Claude invocations.
	storyMaxTurns = 50
)

// invokeOpts holds parameters for Claude invocation (used for testability).
type invokeOpts struct {
	prompt       string
	dir          string
	verbose      bool
	maxTurns     int
	eventHandler events.EventHandler
}

// invokeClaudeFn is the function used to invoke Claude. Package-level var for testability.
var invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
	return claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:       opts.prompt,
		Dir:          opts.dir,
		Print:        true,
		Verbose:      opts.verbose,
		MaxTurns:     opts.maxTurns,
		EventHandler: opts.eventHandler,
	})
}

// gitHasUncommittedChangesFn checks if git working tree has uncommitted changes.
// Package-level var for testability.
var gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
	runner := &gitRunner{dir: dir}
	return runner.hasUncommittedChanges(ctx)
}

// gitRunner wraps git operations for the loop.
type gitRunner struct {
	dir string
}

func (g *gitRunner) hasUncommittedChanges(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = g.dir
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("checking git status: %w", err)
	}
	return len(bytes.TrimSpace(output)) > 0, nil
}

// checkGitClean verifies the working tree has no uncommitted changes before exit.
// Returns true if clean (safe to exit), false if dirty (should continue loop).
func checkGitClean(ctx context.Context, dir string, h events.EventHandler) bool {
	hasChanges, err := gitHasUncommittedChangesFn(ctx, dir)
	if err != nil {
		emitWarn(h, "failed to check git status: %v — continuing loop", err)
		return false
	}
	if hasChanges {
		emitWarn(h, "uncommitted changes detected — continuing loop to allow commit")
		return false
	}
	return true
}

// usageLimitFallbackWait is the minimum wait duration when the reset time
// cannot be parsed or appears to be in the past (e.g. clock skew).
var usageLimitFallbackWait = 30 * time.Second

// invokeWithUsageLimitWait calls invokeClaudeFn and, if a usage limit is hit,
// waits until the reset time before retrying. Non-usage-limit errors and
// successful results are returned immediately.
func invokeWithUsageLimitWait(ctx context.Context, opts invokeOpts) (string, error) {
	for {
		output, err := invokeClaudeFn(ctx, opts)

		var ulErr *claude.UsageLimitError
		if !errors.As(err, &ulErr) {
			return output, err
		}

		waitDur := time.Until(ulErr.ResetAt)
		if waitDur <= 0 {
			waitDur = usageLimitFallbackWait
		}

		emitEvent(opts.eventHandler, events.UsageLimitWait{
			WaitDuration: waitDur.Round(time.Second),
			ResetAt:      ulErr.ResetAt,
		})
		emitLog(opts.eventHandler, "usage limit reached — waiting %s until %s",
			waitDur.Round(time.Second), ulErr.ResetAt.Format(time.RFC3339))

		select {
		case <-time.After(waitDur):
			continue
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// emitEvent sends an event to the handler if non-nil.
func emitEvent(h events.EventHandler, e events.Event) {
	if h != nil {
		h.Handle(e)
	}
}

// emitLog emits an info-level LogMessage event.
func emitLog(h events.EventHandler, format string, args ...any) {
	emitEvent(h, events.LogMessage{Level: "info", Message: fmt.Sprintf(format, args...)})
}

// emitWarn emits a warning-level LogMessage event.
func emitWarn(h events.EventHandler, format string, args ...any) {
	emitEvent(h, events.LogMessage{Level: "warning", Message: fmt.Sprintf(format, args...)})
}

// buildCommitPrompt creates a prompt for the commit phase when all stories are complete
// but there are uncommitted changes that need to be committed.
func buildCommitPrompt(prdPath, progressPath, knowledgePath string) string {
	var prompt bytes.Buffer

	prompt.WriteString("# Commit Phase\n\n")
	prompt.WriteString("All user stories in the PRD have been completed! However, there are uncommitted changes in the repository.\n\n")
	prompt.WriteString("Your task:\n")
	prompt.WriteString("1. Review the uncommitted changes using `git status` and `git diff`\n")
	prompt.WriteString("2. Review recent commits using `git log` to understand the commit message style\n")
	prompt.WriteString("3. Create an appropriate commit message that summarizes the work done\n")
	prompt.WriteString("4. Commit all changes\n\n")

	if prdPath != "" {
		fmt.Fprintf(&prompt, "PRD location: %s\n", prdPath)
	}
	if progressPath != "" {
		fmt.Fprintf(&prompt, "Progress log: %s\n", progressPath)
	}
	if knowledgePath != "" {
		fmt.Fprintf(&prompt, "Knowledge base: %s\n", knowledgePath)
	}

	prompt.WriteString("\nWhen you have successfully committed all changes, respond with <promise>COMPLETE</promise> to signal completion.\n")

	return prompt.String()
}

// Config holds the parameters for a Ralph execution loop.
type Config struct {
	MaxIterations int
	MaxQAAttempts int
	WorkDir       string
	PRDPath       string
	ProgressPath  string
	PromptsDir    string
	QualityChecks []string
	KnowledgePath string
	QAReportPath  string
	QAScriptsPath string
	Verbose       bool
	EventHandler  events.EventHandler
}

// Run executes the Ralph loop: for each iteration, it reads the PRD, picks
// the next unfinished story, invokes Claude to implement it, and checks for
// the completion signal. Returns nil when all stories pass or an error if
// max iterations are reached.
func Run(ctx context.Context, cfg Config) error {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = DefaultMaxIterations
	}

	// Ensure the progress file exists (workspace-scoped at
	// .ralph/workspaces/<name>/progress.txt).
	if cfg.ProgressPath != "" {
		ensureProgressFile(cfg.ProgressPath)
	}

	for i := 1; i <= cfg.MaxIterations; i++ {
		emitEvent(cfg.EventHandler, events.IterationStart{
			Iteration:     i,
			MaxIterations: cfg.MaxIterations,
		})
		emitEvent(cfg.EventHandler, events.PRDRefresh{})

		currentPRD, err := prd.Read(cfg.PRDPath)
		if err != nil {
			return fmt.Errorf("reading PRD: %w", err)
		}

		story := prd.NextUnfinished(currentPRD)
		if story == nil {
			// All user stories pass
			if !checkGitClean(ctx, cfg.WorkDir, cfg.EventHandler) {
				// Git is dirty — invoke Claude to commit the changes
				emitLog(cfg.EventHandler, "all stories complete but uncommitted changes detected — invoking Claude to commit")

				commitPrompt := buildCommitPrompt(cfg.PRDPath, cfg.ProgressPath, cfg.KnowledgePath)
				output, err := invokeWithUsageLimitWait(ctx, invokeOpts{
					prompt:       commitPrompt,
					dir:          cfg.WorkDir,
					verbose:      cfg.Verbose,
					maxTurns:     storyMaxTurns,
					eventHandler: cfg.EventHandler,
				})
				if err != nil {
					emitWarn(cfg.EventHandler, "Claude returned error during commit phase: %v", err)
					// Non-fatal — Claude may have partially succeeded.
					// The next iteration will re-check git status.
				}

				// If Claude signaled COMPLETE, verify git is now clean
				if claude.ContainsComplete(output) {
					emitLog(cfg.EventHandler, "Claude signaled COMPLETE after commit — verifying git status")
					if checkGitClean(ctx, cfg.WorkDir, cfg.EventHandler) {
						emitLog(cfg.EventHandler, "verified: all stories pass and git is clean — done")
						return nil
					}
					emitLog(cfg.EventHandler, "COMPLETE signal received but git still has uncommitted changes — continuing loop")
				}

				if i < cfg.MaxIterations {
					time.Sleep(iterationDelay)
				}
				continue
			}
			emitLog(cfg.EventHandler, "all stories pass — done")
			return nil
		}

		emitEvent(cfg.EventHandler, events.StoryStarted{
			StoryID: story.ID,
			Title:   story.Title,
		})

		viewPath := writeProgressView(cfg.ProgressPath)
		prompt, err := prompts.RenderLoopIteration(story, cfg.QualityChecks, viewPath, cfg.PRDPath, cfg.PromptsDir, cfg.KnowledgePath)
		if err != nil {
			return fmt.Errorf("rendering prompt for %s: %w", story.ID, err)
		}

		output, err := invokeWithUsageLimitWait(ctx, invokeOpts{
			prompt:       prompt,
			dir:          cfg.WorkDir,
			verbose:      cfg.Verbose,
			maxTurns:     storyMaxTurns,
			eventHandler: cfg.EventHandler,
		})
		if err != nil {
			emitWarn(cfg.EventHandler, "Claude returned error on %s: %v", story.ID, err)
			// Non-fatal — Claude may have partially succeeded.
			// The next iteration will re-read prd.json and pick up where we left off.
		}

		emitEvent(cfg.EventHandler, events.PRDRefresh{})

		if claude.ContainsComplete(output) {
			emitLog(cfg.EventHandler, "Ralph signaled COMPLETE — verifying PRD state")

			// Re-read PRD to verify all stories actually pass.
			// This guards against Claude hallucinating completion or stale data.
			verifyPRD, err := prd.Read(cfg.PRDPath)
			if err != nil {
				emitWarn(cfg.EventHandler, "failed to verify PRD: %v — continuing loop", err)
				continue
			}

			if !prd.AllPass(verifyPRD) {
				emitLog(cfg.EventHandler, "COMPLETE signal received but not all user stories pass — continuing loop")
				continue
			}

			if !checkGitClean(ctx, cfg.WorkDir, cfg.EventHandler) {
				if i < cfg.MaxIterations {
					time.Sleep(iterationDelay)
				}
				continue
			}
			emitLog(cfg.EventHandler, "verified: all stories pass — done")
			return nil
		}

		if i < cfg.MaxIterations {
			time.Sleep(iterationDelay)
		}
	}

	return fmt.Errorf("max iterations (%d) reached without completing all stories", cfg.MaxIterations)
}

func ensureProgressFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Dir(path)
		os.MkdirAll(dir, 0755)
		header := fmt.Sprintf("# Ralph Progress Log\nStarted: %s\n---\n\n## Codebase Patterns\n\n---\n",
			time.Now().Format(time.RFC3339))
		os.WriteFile(path, []byte(header), 0644)
	}
}

// progressViewPath returns the path for the capped progress view file,
// placed alongside the original progress file.
func progressViewPath(progressPath string) string {
	dir := filepath.Dir(progressPath)
	return filepath.Join(dir, ".progress-view")
}

// writeProgressView reads the original progress file, caps it, and writes
// the capped version to a view file. Returns the view file path. If the
// progress path is empty or reading fails, returns the original path.
func writeProgressView(progressPath string) string {
	if progressPath == "" {
		return ""
	}

	content, err := os.ReadFile(progressPath)
	if err != nil {
		return progressPath
	}

	capped := progress.CapProgressEntries(string(content), progress.DefaultMaxEntries)
	viewPath := progressViewPath(progressPath)
	if err := os.WriteFile(viewPath, []byte(capped), 0644); err != nil {
		return progressPath
	}

	return viewPath
}
