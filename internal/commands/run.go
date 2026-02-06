package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/tui"
)

// Run executes the Ralph loop using workspace context to determine
// the working directory and PRD location.
func Run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	maxIter := fs.Int("max-iterations", loop.DefaultMaxIterations, "Maximum loop iterations")
	verbose := fs.Bool("verbose", false, "Enable verbose debug logging")
	workspaceFlag := AddWorkspaceFlag(fs)
	noTUI := fs.Bool("no-tui", false, "Disable TUI and use plain-text output")
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

	// Check if all work is already done
	currentPRD, err := prd.Read(wc.PRDPath)
	if err != nil {
		return fmt.Errorf("reading PRD: %w", err)
	}

	if prd.AllPass(currentPRD) && prd.AllIntegrationTestsPass(currentPRD) {
		doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
		fmt.Fprintf(os.Stderr, "\n%s All stories and integration tests pass — nothing to do.\n\n", doneStyle.Render("✓"))
		fmt.Fprintf(os.Stderr, "Run `ralph done` to squash and merge your changes back to base.\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "workspace=%s workDir=%s prdPath=%s\n", wc.Name, wc.WorkDir, wc.PRDPath)

	progressPath := filepath.Join(cfg.Repo.Path, ".ralph", "progress.txt")
	promptsDir := cfg.PromptsDir()

	if *noTUI {
		return runLoopPlainText(ctx, wc.WorkDir, wc.PRDPath, *maxIter, cfg.QualityChecks, *verbose, progressPath, promptsDir)
	}

	return runLoopTUI(ctx, wc.WorkDir, wc.PRDPath, *maxIter, cfg.QualityChecks, *verbose, progressPath, promptsDir, wc.Name)
}

func runLoopPlainText(ctx context.Context, workDir, prdPath string, maxIter int, qualityChecks []string, verbose bool, progressPath, promptsDir string) error {
	handler := &events.PlainTextHandler{W: os.Stderr}
	err := runLoopWithHandler(ctx, workDir, prdPath, maxIter, qualityChecks, verbose, progressPath, promptsDir, handler)
	printRunResult(err)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runLoopTUI(ctx context.Context, workDir, prdPath string, maxIter int, qualityChecks []string, verbose bool, progressPath, promptsDir string, workspaceName string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	model := tui.NewModel(workspaceName, prdPath, cancel)
	p := tea.NewProgram(model, tea.WithAltScreen())
	handler := tui.NewHandler(p)

	// Run the loop in a background goroutine; the BubbleTea event loop
	// owns the main goroutine. When the loop finishes, we send tea.Quit.
	errCh := make(chan error, 1)
	go func() {
		err := runLoopWithHandler(ctx, workDir, prdPath, maxIter, qualityChecks, verbose, progressPath, promptsDir, handler)
		errCh <- err
		p.Send(tea.Quit())
	}()

	if _, err := p.Run(); err != nil {
		return err
	}

	// The goroutine should have finished before p.Run() returns (it sends
	// tea.Quit). Use a short timeout as a safety net for Ctrl+C exits
	// where the goroutine may still be running.
	select {
	case loopErr := <-errCh:
		printRunResult(loopErr)
		if errors.Is(loopErr, context.Canceled) {
			return nil
		}
		return loopErr
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func printRunResult(err error) {
	if err == nil {
		doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
		fmt.Fprintf(os.Stderr, "\n%s All work complete.\n\n", doneStyle.Render("✓"))
		fmt.Fprintf(os.Stderr, "Run `ralph done` to squash and merge your changes back to base.\n")
	} else if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "\nStopped.")
	}
}

func runLoopWithHandler(ctx context.Context, workDir, prdPath string, maxIter int, qualityChecks []string, verbose bool, progressPath, promptsDir string, handler events.EventHandler) error {
	return loop.Run(ctx, loop.Config{
		MaxIterations: maxIter,
		WorkDir:       workDir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		PromptsDir:    promptsDir,
		QualityChecks: qualityChecks,
		Verbose:       verbose,
		EventHandler:  handler,
	})
}
