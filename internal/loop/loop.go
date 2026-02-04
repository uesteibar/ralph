package loop

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/prompts"
)

const (
	DefaultMaxIterations = 20
	iterationDelay       = 2 * time.Second
)

// invokeOpts holds parameters for Claude invocation (used for testability).
type invokeOpts struct {
	prompt           string
	dir              string
	verbose          bool
	isQAVerification bool
	isQAFix          bool
}

// invokeClaudeFn is the function used to invoke Claude. Package-level var for testability.
var invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
	return claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:  opts.prompt,
		Dir:     opts.dir,
		Print:   true,
		Verbose: opts.verbose,
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
func checkGitClean(ctx context.Context, dir string) bool {
	hasChanges, err := gitHasUncommittedChangesFn(ctx, dir)
	if err != nil {
		log.Printf("[loop] WARNING: failed to check git status: %v — continuing loop", err)
		return false
	}
	if hasChanges {
		log.Println("[loop] WARNING: uncommitted changes detected — continuing loop to allow commit")
		return false
	}
	return true
}

// Config holds the parameters for a Ralph execution loop.
type Config struct {
	MaxIterations int
	WorkDir       string
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
	Verbose       bool
}

// Run executes the Ralph loop: for each iteration, it reads the PRD, picks
// the next unfinished story, invokes Claude to implement it, and checks for
// the completion signal. When all stories pass, it invokes QA verification.
// Returns nil when all stories and integration tests are done or an error
// if max iterations are reached.
func Run(ctx context.Context, cfg Config) error {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = DefaultMaxIterations
	}

	// Progress file lives at the repo root (.ralph/progress.txt), shared
	// across all runs. It is created by `ralph init` and committed to git.
	// If it doesn't exist yet, create it so the loop can proceed.
	ensureProgressFile(cfg.ProgressPath)

	for i := 1; i <= cfg.MaxIterations; i++ {
		log.Printf("[loop] iteration %d/%d", i, cfg.MaxIterations)

		currentPRD, err := prd.Read(cfg.PRDPath)
		if err != nil {
			return fmt.Errorf("reading PRD: %w", err)
		}

		story := prd.NextUnfinished(currentPRD)
		if story == nil {
			// All user stories pass — check if QA verification is needed
			if len(currentPRD.IntegrationTests) == 0 {
				if !checkGitClean(ctx, cfg.WorkDir) {
					if i < cfg.MaxIterations {
						time.Sleep(iterationDelay)
					}
					continue
				}
				log.Println("[loop] all stories pass, no integration tests — done")
				return nil
			}

			if prd.AllIntegrationTestsPass(currentPRD) {
				if !checkGitClean(ctx, cfg.WorkDir) {
					if i < cfg.MaxIterations {
						time.Sleep(iterationDelay)
					}
					continue
				}
				log.Println("[loop] all stories and integration tests pass — done")
				return nil
			}

			// Run QA verification phase
			log.Println("[loop] all stories pass — running QA verification")
			if err := runQAVerification(ctx, cfg); err != nil {
				log.Printf("[loop] QA verification error: %v", err)
			}

			// Re-read PRD after QA verification and check if all tests pass
			verifyPRD, err := prd.Read(cfg.PRDPath)
			if err != nil {
				log.Printf("[loop] failed to read PRD after QA: %v — continuing loop", err)
			} else if prd.AllIntegrationTestsPass(verifyPRD) {
				if !checkGitClean(ctx, cfg.WorkDir) {
					if i < cfg.MaxIterations {
						time.Sleep(iterationDelay)
					}
					continue
				}
				log.Println("[loop] QA verification complete — all integration tests pass")
				return nil
			}

			// Get failed tests and invoke fix agent
			failedTests := prd.FailedIntegrationTests(verifyPRD)
			if len(failedTests) > 0 {
				log.Printf("[loop] %d integration tests failed — running QA fix agent", len(failedTests))
				if err := runQAFix(ctx, cfg, failedTests); err != nil {
					log.Printf("[loop] QA fix error: %v", err)
				}
			}

			// Continue loop to allow re-verification after fix
			if i < cfg.MaxIterations {
				time.Sleep(iterationDelay)
			}
			continue
		}

		log.Printf("[loop] working on %s: %s", story.ID, story.Title)

		prompt, err := prompts.RenderLoopIteration(story, cfg.QualityChecks, cfg.ProgressPath)
		if err != nil {
			return fmt.Errorf("rendering prompt for %s: %w", story.ID, err)
		}

		output, err := invokeClaudeFn(ctx, invokeOpts{
			prompt:  prompt,
			dir:     cfg.WorkDir,
			verbose: cfg.Verbose,
		})
		if err != nil {
			log.Printf("[loop] Claude returned error on %s: %v", story.ID, err)
			// Non-fatal — Claude may have partially succeeded.
			// The next iteration will re-read prd.json and pick up where we left off.
		}

		if claude.ContainsComplete(output) {
			log.Println("[loop] Ralph signaled COMPLETE — verifying PRD state")

			// Re-read PRD to verify all stories and integration tests actually pass.
			// This guards against Claude hallucinating completion or stale data.
			verifyPRD, err := prd.Read(cfg.PRDPath)
			if err != nil {
				log.Printf("[loop] failed to verify PRD: %v — continuing loop", err)
				continue
			}

			if !prd.AllPass(verifyPRD) {
				log.Println("[loop] COMPLETE signal received but not all user stories pass — continuing loop")
				continue
			}

			if len(verifyPRD.IntegrationTests) == 0 {
				if !checkGitClean(ctx, cfg.WorkDir) {
					if i < cfg.MaxIterations {
						time.Sleep(iterationDelay)
					}
					continue
				}
				log.Println("[loop] verified: all stories pass, no integration tests — done")
				return nil
			}

			if !prd.AllIntegrationTestsPass(verifyPRD) {
				log.Println("[loop] COMPLETE signal received but not all integration tests pass — continuing loop")
				continue
			}

			if !checkGitClean(ctx, cfg.WorkDir) {
				if i < cfg.MaxIterations {
					time.Sleep(iterationDelay)
				}
				continue
			}
			log.Println("[loop] verified: all stories and integration tests pass — done")
			return nil
		}

		if i < cfg.MaxIterations {
			time.Sleep(iterationDelay)
		}
	}

	return fmt.Errorf("max iterations (%d) reached without completing all stories", cfg.MaxIterations)
}

// runQAVerification invokes the QA verification agent with the qa_verification.md prompt.
func runQAVerification(ctx context.Context, cfg Config) error {
	prompt, err := prompts.RenderQAVerification(prompts.QAVerificationData{
		PRDPath:       cfg.PRDPath,
		ProgressPath:  cfg.ProgressPath,
		QualityChecks: cfg.QualityChecks,
	})
	if err != nil {
		return fmt.Errorf("rendering QA verification prompt: %w", err)
	}

	_, err = invokeClaudeFn(ctx, invokeOpts{
		prompt:           prompt,
		dir:              cfg.WorkDir,
		verbose:          cfg.Verbose,
		isQAVerification: true,
	})
	return err
}

// runQAFix invokes the QA fix agent with the qa_fix.md prompt to resolve failing integration tests.
func runQAFix(ctx context.Context, cfg Config, failedTests []prd.IntegrationTest) error {
	prompt, err := prompts.RenderQAFix(prompts.QAFixData{
		PRDPath:       cfg.PRDPath,
		ProgressPath:  cfg.ProgressPath,
		QualityChecks: cfg.QualityChecks,
		FailedTests:   failedTests,
	})
	if err != nil {
		return fmt.Errorf("rendering QA fix prompt: %w", err)
	}

	_, err = invokeClaudeFn(ctx, invokeOpts{
		prompt:  prompt,
		dir:     cfg.WorkDir,
		verbose: cfg.Verbose,
		isQAFix: true,
	})
	return err
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
