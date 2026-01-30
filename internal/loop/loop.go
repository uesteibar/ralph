package loop

import (
	"context"
	"fmt"
	"log"
	"os"
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

// Config holds the parameters for a Ralph execution loop.
type Config struct {
	MaxIterations int
	WorkDir       string
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
}

// Run executes the Ralph loop: for each iteration, it reads the PRD, picks
// the next unfinished story, invokes Claude to implement it, and checks for
// the completion signal. Returns nil when all stories are done or an error
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
			log.Println("[loop] all stories pass — done")
			return nil
		}

		log.Printf("[loop] working on %s: %s", story.ID, story.Title)

		prompt, err := prompts.RenderLoopIteration(story, cfg.QualityChecks, cfg.ProgressPath)
		if err != nil {
			return fmt.Errorf("rendering prompt for %s: %w", story.ID, err)
		}

		output, err := claude.Invoke(ctx, claude.InvokeOpts{
			Prompt: prompt,
			Dir:    cfg.WorkDir,
			Print:  true,
		})
		if err != nil {
			log.Printf("[loop] Claude returned error on %s: %v", story.ID, err)
			// Non-fatal — Claude may have partially succeeded.
			// The next iteration will re-read prd.json and pick up where we left off.
		}

		if claude.ContainsComplete(output) {
			log.Println("[loop] Ralph signaled COMPLETE")
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
