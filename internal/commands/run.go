package commands

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/tui"
	"github.com/uesteibar/ralph/internal/workspace"
)

// spawnDaemonFn spawns the ralph _daemon process. Package-level var for testability.
var spawnDaemonFn = func(workspaceName string, maxIter int) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding executable path: %w", err)
	}
	cmd := exec.Command(exe, "_daemon",
		"--workspace", workspaceName,
		"--max-iterations", fmt.Sprintf("%d", maxIter))
	cmd.SysProcAttr = spawnSysProcAttr()
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting daemon: %w", err)
	}
	// Release the child so it can outlive us.
	go cmd.Wait()
	return cmd, nil
}

// waitForPIDFileFn waits for the PID file to appear. Package-level var for testability.
var waitForPIDFileFn = func(wsPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runstate.IsRunning(wsPath) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start within %s", timeout)
}

// Run executes the Ralph loop by spawning a daemon and attaching a viewer.
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
	// verbose is parsed but only used if we fall back to in-process mode in the future
	_ = verbose

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
		return fmt.Errorf("PRD not found at %s\n\nCreate a workspace first: ralph new <name>", wc.PRDPath)
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

	wsPath := workspace.WorkspacePath(cfg.Repo.Path, wc.Name)

	// Check if daemon is already running; if not, spawn one.
	alreadyRunning := runstate.IsRunning(wsPath)
	if !alreadyRunning {
		fmt.Fprintf(os.Stderr, "workspace=%s workDir=%s prdPath=%s\n", wc.Name, wc.WorkDir, wc.PRDPath)
		_, err := spawnDaemonFn(wc.Name, *maxIter)
		if err != nil {
			return fmt.Errorf("spawning daemon: %w", err)
		}
		if err := waitForPIDFileFn(wsPath, 5*time.Second); err != nil {
			return fmt.Errorf("daemon failed to start: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "daemon already running for workspace %s\n", wc.Name)
	}

	logsDir := filepath.Join(wsPath, "logs")

	if *noTUI {
		return tailLogsPlainText(ctx, wsPath, logsDir)
	}

	return tailLogsTUI(wsPath, logsDir, wc.Name, wc.PRDPath)
}

// tailLogsPlainText tails JSONL log files and streams events to stderr via PlainTextHandler.
// On Ctrl+C (SIGINT), it stops tailing and sends SIGTERM to the daemon.
func tailLogsPlainText(ctx context.Context, wsPath, logsDir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle Ctrl+C: stop tailing + kill daemon.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		select {
		case <-sigCh:
			stopDaemon(wsPath)
			cancel()
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigCh)

	handler := &events.PlainTextHandler{W: os.Stderr}

	// Track which files we've read and where we left off.
	offsets := make(map[string]int64)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Check if daemon is still running.
		if !runstate.IsRunning(wsPath) {
			// Daemon stopped — do one final read then exit.
			readNewLogEntries(logsDir, offsets, handler)
			return printDaemonResult(wsPath)
		}

		readNewLogEntries(logsDir, offsets, handler)
		time.Sleep(200 * time.Millisecond)
	}
}

// tailLogsTUI opens a BubbleTea TUI that reads events from JSONL log files.
// Historical events are replayed on startup; live events appear in real-time.
// d=detach (quit TUI, daemon continues), q=stop (SIGTERM to daemon, then quit).
func tailLogsTUI(wsPath, logsDir, workspaceName, prdPath string) error {
	model := tui.NewModel(workspaceName, prdPath)
	model.SetStopDaemonFn(func() {
		stopDaemon(wsPath)
		// Wait briefly for daemon to exit
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if !runstate.IsRunning(wsPath) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	handler := tui.NewHandler(p)

	// Read log events via LogReader in a goroutine, forwarding to the TUI handler.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := events.NewLogReader(logsDir)
	go func() {
		reader.Run(ctx)
	}()
	go func() {
		for evt := range reader.Events() {
			handler.Handle(evt)
		}
		// LogReader channel closed — daemon likely exited.
		p.Send(tui.MakeLogReaderDoneMsg())
	}()

	// Monitor daemon liveness: when daemon exits, cancel LogReader so it closes its channel.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				if !runstate.IsRunning(wsPath) {
					// Give LogReader one final poll cycle to drain remaining events.
					time.Sleep(300 * time.Millisecond)
					cancel()
					return
				}
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return printDaemonResult(wsPath)
}

// readNewLogEntries reads new JSONL lines from all log files in logsDir.
func readNewLogEntries(logsDir string, offsets map[string]int64, handler events.EventHandler) {
	files, err := filepath.Glob(filepath.Join(logsDir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return
	}
	sort.Strings(files)

	for _, path := range files {
		offset := offsets[path]
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				continue
			}
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			evt, err := events.UnmarshalEvent([]byte(line))
			if err != nil {
				continue
			}
			handler.Handle(evt)
		}

		newOffset, _ := f.Seek(0, io.SeekCurrent)
		offsets[path] = newOffset
		f.Close()
	}
}

// stopDaemon reads the daemon PID and sends SIGTERM.
func stopDaemon(wsPath string) {
	pid, err := runstate.ReadPID(wsPath)
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	sendTermSignal(proc)
}

// printDaemonResult reads the daemon's status file and prints the outcome.
func printDaemonResult(wsPath string) error {
	status, err := runstate.ReadStatus(wsPath)
	if err != nil {
		return nil
	}
	switch status.Result {
	case runstate.ResultSuccess:
		doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
		fmt.Fprintf(os.Stderr, "\n%s All work complete.\n\n", doneStyle.Render("✓"))
		fmt.Fprintf(os.Stderr, "Run `ralph done` to squash and merge your changes back to base.\n")
	case runstate.ResultCancelled:
		fmt.Fprintln(os.Stderr, "\nStopped.")
	case runstate.ResultFailed:
		if status.Error != "" {
			return errors.New(status.Error)
		}
		return errors.New("daemon failed")
	}
	return nil
}
