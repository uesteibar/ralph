package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// LoopConfig holds the parameters passed to a loop runner. This mirrors
// loop.Config but avoids importing the loop package directly to keep the
// worker testable with mock runners.
type LoopConfig struct {
	MaxIterations int
	WorkDir       string
	PRDPath       string
	ProgressPath  string
	PromptsDir    string
	QualityChecks []string
	KnowledgePath string
	Verbose       bool
	EventHandler  events.EventHandler
}

// LoopRunner abstracts the Ralph build loop. The real implementation wraps
// loop.Run; tests inject a mock.
type LoopRunner interface {
	Run(ctx context.Context, cfg LoopConfig) error
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// HookRunner runs lifecycle hooks at specific points in the build lifecycle.
type HookRunner interface {
	RunPrePR(ctx context.Context, workDir string) error
}

// Config holds the dependencies for the build worker dispatcher.
type Config struct {
	DB           *db.DB
	MaxWorkers   int
	LoopRunner   LoopRunner
	Projects     ProjectGetter
	EventHandler events.EventHandler
	Logger       *slog.Logger

	// GitIdentityFn resolves the git author name and email for a given
	// project ID. This is used to configure repo-local git identity in
	// worktrees before starting a build loop, ensuring that commits
	// created by Claude CLI's internal git operations use the correct
	// per-project identity.
	GitIdentityFn func(projectID string) (name, email string)

	// OnBuildEvent is called whenever a build event is logged to the activity
	// table. The callback receives the issue ID and event detail string. This
	// allows the caller (e.g. main.go) to broadcast real-time updates via
	// WebSocket without the worker package importing the server package.
	OnBuildEvent func(issueID, detail string)

	// UsageLimitSetter is called when a UsageLimitWait event is received
	// from the ralph loop, bridging loop-level detection to global state.
	UsageLimitSetter eventlog.UsageLimitSetter

	// HookRunner runs pre-PR hooks after the build loop completes but
	// before PR creation. When nil, no hooks are executed.
	// For multi-project setups, prefer HookRunnerFn which creates
	// per-project runners.
	HookRunner HookRunner

	// HookRunnerFn creates a HookRunner for a given project. When set,
	// this takes precedence over HookRunner in the run() method, allowing
	// per-project hook configuration. The function receives the project
	// and returns a HookRunner (or nil for no hooks).
	HookRunnerFn func(project db.Project) HookRunner
}

// Dispatcher manages build worker goroutines. It limits the number of
// concurrent builds and tracks which issues are currently being built.
type Dispatcher struct {
	db             *db.DB
	maxWorkers     int
	runner         LoopRunner
	projects       ProjectGetter
	handler          events.EventHandler
	onBuildEvent     func(issueID, detail string)
	ulSetter         eventlog.UsageLimitSetter
	logger           *slog.Logger
	gitIdentityFn    func(projectID string) (name, email string)

	hookRunner       HookRunner
	hookRunnerFn     func(project db.Project) HookRunner

	mu       sync.Mutex
	active   map[string]context.CancelFunc // issue ID → cancel func
	sem      chan struct{}                  // semaphore limiting concurrency
	wg       sync.WaitGroup
}

// New creates a Dispatcher with the given configuration.
func New(cfg Config) *Dispatcher {
	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		db:             cfg.DB,
		maxWorkers:     maxWorkers,
		runner:         cfg.LoopRunner,
		projects:       cfg.Projects,
		handler:        cfg.EventHandler,
		onBuildEvent:   cfg.OnBuildEvent,
		ulSetter:       cfg.UsageLimitSetter,
		logger:         logger,
		gitIdentityFn:  cfg.GitIdentityFn,
		hookRunner:     cfg.HookRunner,
		hookRunnerFn:   cfg.HookRunnerFn,
		active:         make(map[string]context.CancelFunc),
		sem:            make(chan struct{}, maxWorkers),
	}
}

// Dispatch starts a build worker goroutine for the given issue. It returns
// an error if no worker slot is available or the issue is already being built.
func (d *Dispatcher) Dispatch(ctx context.Context, issue db.Issue) error {
	d.mu.Lock()
	if _, ok := d.active[issue.ID]; ok {
		d.mu.Unlock()
		return fmt.Errorf("issue %s is already running", issue.ID)
	}
	d.mu.Unlock()

	// Try to acquire a worker slot (non-blocking).
	select {
	case d.sem <- struct{}{}:
	default:
		return fmt.Errorf("no worker slot available (max %d)", d.maxWorkers)
	}

	workerCtx, cancel := context.WithCancel(ctx)

	d.mu.Lock()
	d.active[issue.ID] = cancel
	d.mu.Unlock()

	d.wg.Add(1)
	go d.run(workerCtx, cancel, issue)

	return nil
}

// Wait blocks until all active workers have completed.
func (d *Dispatcher) Wait() {
	d.wg.Wait()
}

// IsRunning returns true if a build worker is active for the given issue ID.
func (d *Dispatcher) IsRunning(issueID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.active[issueID]
	return ok
}

// Cancel cancels the running worker for the given issue ID. It returns true
// if a running worker was found and cancelled, false otherwise.
func (d *Dispatcher) Cancel(issueID string) bool {
	d.mu.Lock()
	cancel, ok := d.active[issueID]
	d.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// ActiveCount returns the number of currently active build workers.
func (d *Dispatcher) ActiveCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.active)
}

// DispatchAction starts a goroutine that runs an arbitrary action function for
// the given issue. It reuses the same semaphore and per-issue tracking as
// Dispatch to prevent concurrent actions on the same issue. On failure,
// it logs to the activity table and sets issue.ErrorMessage (same as
// handleFailure). Context cancellation is treated as a clean exit.
func (d *Dispatcher) DispatchAction(ctx context.Context, issue db.Issue, actionFn func(ctx context.Context) error) error {
	d.mu.Lock()
	if _, ok := d.active[issue.ID]; ok {
		d.mu.Unlock()
		return fmt.Errorf("issue %s is already running", issue.ID)
	}
	d.mu.Unlock()

	// Try to acquire a worker slot (non-blocking).
	select {
	case d.sem <- struct{}{}:
	default:
		return fmt.Errorf("no worker slot available (max %d)", d.maxWorkers)
	}

	actionCtx, cancel := context.WithCancel(ctx)

	d.mu.Lock()
	d.active[issue.ID] = cancel
	d.mu.Unlock()

	d.wg.Add(1)
	go d.runAction(actionCtx, cancel, issue, actionFn)

	return nil
}

// runAction executes an arbitrary action function in a goroutine. It handles
// cleanup (semaphore release, active map removal) and error handling (logging
// to activity table and setting issue.ErrorMessage on failure).
func (d *Dispatcher) runAction(ctx context.Context, cancel context.CancelFunc, issue db.Issue, actionFn func(ctx context.Context) error) {
	defer d.wg.Done()
	defer func() {
		<-d.sem // release worker slot
		d.mu.Lock()
		delete(d.active, issue.ID)
		d.mu.Unlock()
		cancel()
	}()

	actionErr := actionFn(ctx)
	if actionErr == nil {
		return
	}

	// Context cancellation: clean exit, don't mark as failed
	if errors.Is(actionErr, context.Canceled) || errors.Is(actionErr, context.DeadlineExceeded) {
		d.logger.Info("action cancelled", "issue", issue.ID)
		return
	}

	d.handleActionFailure(issue, actionErr)
}

func (d *Dispatcher) handleActionFailure(issue db.Issue, actionErr error) {
	// Re-read to avoid overwriting a concurrent completed/paused transition.
	current, err := d.db.GetIssue(issue.ID)
	if err != nil {
		d.logger.Error("re-reading issue before marking failed", "issue", issue.ID, "error", err)
		return
	}
	if current.State == "completed" || current.State == "paused" {
		d.logger.Info("skipping action failure — issue already in terminal state",
			"issue", issue.ID, "state", current.State, "error", actionErr)
		return
	}

	fromState := current.State
	current.State = "failed"
	current.ErrorMessage = actionErr.Error()
	if err := d.db.UpdateIssue(current); err != nil {
		d.logger.Error("updating issue to failed after action", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "action_failed", fromState, "failed", actionErr.Error()); err != nil {
		d.logger.Error("logging action_failed activity", "issue", issue.ID, "error", err)
	}
}

// RecoverBuilding queries the database for issues in the BUILDING state and
// re-dispatches them. This is called on startup to resume builds that were
// interrupted by a process restart.
func (d *Dispatcher) RecoverBuilding(ctx context.Context) (int, error) {
	issues, err := d.db.ListIssues(db.IssueFilter{State: "building"})
	if err != nil {
		return 0, fmt.Errorf("listing building issues: %w", err)
	}
	recovered := 0
	for _, issue := range issues {
		if dispErr := d.Dispatch(ctx, issue); dispErr != nil {
			d.logger.Warn("could not recover building issue", "issue", issue.ID, "error", dispErr)
			continue
		}
		d.logger.Info("recovered building issue", "issue", issue.ID, "identifier", issue.Identifier)
		recovered++
	}
	return recovered, nil
}

// RecoverQA queries the database for issues in the QA or QA_FIX state and
// re-dispatches them. This is called on startup to resume QA actions that
// were interrupted by a process restart. The orchestrator loop picks up
// the actual QA verify/fix dispatch on its next tick.
func (d *Dispatcher) RecoverQA(ctx context.Context) (int, error) {
	issues, err := d.db.ListIssues(db.IssueFilter{States: []string{"qa", "qa_fix"}})
	if err != nil {
		return 0, fmt.Errorf("listing qa/qa_fix issues: %w", err)
	}
	for _, issue := range issues {
		d.logger.Info("recovered QA issue", "issue", issue.ID, "identifier", issue.Identifier, "state", issue.State)
	}
	return len(issues), nil
}

// run executes a single build worker. It loads the project, constructs paths,
// and calls the loop runner. On completion it updates the issue state in the DB.
func (d *Dispatcher) run(ctx context.Context, cancel context.CancelFunc, issue db.Issue) {
	defer d.wg.Done()
	defer func() {
		<-d.sem // release worker slot
		d.mu.Lock()
		delete(d.active, issue.ID)
		d.mu.Unlock()
		cancel()
	}()

	project, err := d.projects.GetProject(issue.ProjectID)
	if err != nil {
		d.logger.Error("loading project for build", "issue", issue.ID, "error", err)
		d.handleFailure(issue, fmt.Errorf("loading project: %w", err))
		return
	}

	wsPath := workspace.WorkspacePath(project.LocalPath, issue.WorkspaceName)
	workDir := workspace.TreePath(project.LocalPath, issue.WorkspaceName)
	prdPath := workspace.PRDPathForWorkspace(project.LocalPath, issue.WorkspaceName)
	progressPath := workspace.ProgressPathForWorkspace(project.LocalPath, issue.WorkspaceName)

	// Write PID file so ralph tui shows the build as running.
	if err := runstate.WritePID(wsPath); err != nil {
		d.logger.Warn("writing PID file", "issue", issue.ID, "error", err)
	}
	defer func() {
		runstate.CleanupPID(wsPath)
	}()

	handler := eventlog.New(d.db, issue.ID, d.handler, d.onBuildEvent, d.ulSetter)

	// Set repo-local git identity in the worktree so that commits created
	// by Claude CLI's internal git operations use the correct per-project identity.
	if d.gitIdentityFn != nil {
		gitName, gitEmail := d.gitIdentityFn(issue.ProjectID)
		if gitName != "" && gitEmail != "" {
			r := &shell.Runner{Dir: workDir}
			if err := gitops.ConfigureGitIdentity(ctx, r, gitName, gitEmail); err != nil {
				d.logger.Error("configuring git identity", "issue", issue.ID, "error", err)
				d.handleFailure(issue, fmt.Errorf("configuring git identity: %w", err))
				return
			}
		}
	}

	loopCfg := LoopConfig{
		MaxIterations: project.MaxIterations,
		WorkDir:       workDir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: nil, // loaded by loop from Ralph config
		KnowledgePath: knowledge.Dir(workDir),
		EventHandler:  handler,
	}

	runErr := d.runner.Run(ctx, loopCfg)

	// Write status file for ralph tui compatibility.
	switch {
	case runErr == nil:
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultSuccess})
	case errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded):
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultCancelled})
	default:
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultFailed, Error: runErr.Error()})
	}

	if runErr == nil {
		// Resolve the hook runner for this project. HookRunnerFn (per-project)
		// takes precedence over the static HookRunner.
		hr := d.hookRunner
		if d.hookRunnerFn != nil {
			hr = d.hookRunnerFn(project)
		}
		d.handleSuccess(ctx, issue, workDir, hr)
		return
	}

	// Context cancellation: clean exit, issue stays in BUILDING
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		d.logger.Info("build cancelled", "issue", issue.ID)
		return
	}

	d.handleFailure(issue, runErr)
}

func (d *Dispatcher) handleSuccess(ctx context.Context, issue db.Issue, workDir string, hookRunner HookRunner) {
	// Run pre-PR hooks (e.g. code generators, formatters) before moving on.
	if hookRunner != nil {
		if err := hookRunner.RunPrePR(ctx, workDir); err != nil {
			d.logger.Warn("pre-PR hooks failed", "issue", issue.ID, "error", err)
		}
	}

	issue.State = "qa"
	if err := d.db.UpdateIssue(issue); err != nil {
		d.logger.Error("updating issue to qa", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "build_completed", "building", "qa", "Build completed, moving to QA"); err != nil {
		d.logger.Error("logging build_completed activity", "issue", issue.ID, "error", err)
	}
}

func (d *Dispatcher) handleFailure(issue db.Issue, buildErr error) {
	// Re-read to avoid overwriting a concurrent completed/paused transition.
	current, err := d.db.GetIssue(issue.ID)
	if err != nil {
		d.logger.Error("re-reading issue before marking failed", "issue", issue.ID, "error", err)
		return
	}
	if current.State == "completed" || current.State == "paused" {
		d.logger.Info("skipping build failure — issue already in terminal state",
			"issue", issue.ID, "state", current.State, "error", buildErr)
		return
	}

	current.State = "failed"
	current.ErrorMessage = buildErr.Error()
	if err := d.db.UpdateIssue(current); err != nil {
		d.logger.Error("updating issue to failed", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "build_failed", "building", "failed", buildErr.Error()); err != nil {
		d.logger.Error("logging build_failed activity", "issue", issue.ID, "error", err)
	}
}

