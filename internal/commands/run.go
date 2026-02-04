package commands

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/shell"
)

// Run executes the Ralph loop using workspace context to determine
// the working directory and PRD location.
func Run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	maxIter := fs.Int("max-iterations", loop.DefaultMaxIterations, "Maximum loop iterations")
	verbose := fs.Bool("verbose", false, "Enable verbose debug logging")
	workspaceFlag := AddWorkspaceFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	ctx := context.Background()

	wc, err := resolveWorkContextFromFlags(*workspaceFlag, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	printWorkspaceHeader(wc, cfg.Repo.Path)

	if wc.Name == "base" {
		r := &shell.Runner{Dir: cfg.Repo.Path}
		branch, _ := gitops.CurrentBranch(ctx, r)
		if branch == "" {
			branch = cfg.Repo.DefaultBase
		}
		fmt.Fprintf(os.Stderr, "Running in base. Changes commit to %s. Consider: ralph workspaces new <name>\n", branch)
	}

	// Verify PRD exists
	if _, err := os.Stat(wc.PRDPath); os.IsNotExist(err) {
		return fmt.Errorf("PRD not found at %s\n\nRun 'ralph prd new' first to create a PRD.", wc.PRDPath)
	}

	log.Printf("[run] workspace=%s workDir=%s prdPath=%s", wc.Name, wc.WorkDir, wc.PRDPath)

	return runLoop(ctx, wc.WorkDir, wc.PRDPath, *maxIter, cfg.QualityChecks, *verbose, filepath.Join(cfg.Repo.Path, ".ralph", "progress.txt"))
}

func runLoop(ctx context.Context, workDir, prdPath string, maxIter int, qualityChecks []string, verbose bool, progressPath string) error {
	err := loop.Run(ctx, loop.Config{
		MaxIterations: maxIter,
		WorkDir:       workDir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: qualityChecks,
		Verbose:       verbose,
	})
	if err != nil {
		log.Printf("[run] loop ended: %v", err)
		return err
	}

	log.Println("[run] complete")
	return nil
}
