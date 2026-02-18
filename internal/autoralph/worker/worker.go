package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/pr"
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

// PRCreator creates a GitHub PR for a completed build.
type PRCreator interface {
	CreatePR(issue db.Issue, database *db.DB) error
}

// Config holds the dependencies for the build worker dispatcher.
type Config struct {
	DB           *db.DB
	MaxWorkers   int
	LoopRunner   LoopRunner
	Projects     ProjectGetter
	PR           PRCreator
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
}

// Dispatcher manages build worker goroutines. It limits the number of
// concurrent builds and tracks which issues are currently being built.
type Dispatcher struct {
	db             *db.DB
	maxWorkers     int
	runner         LoopRunner
	projects       ProjectGetter
	pr             PRCreator
	handler        events.EventHandler
	onBuildEvent   func(issueID, detail string)
	logger         *slog.Logger
	gitIdentityFn func(projectID string) (name, email string)

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
		pr:             cfg.PR,
		handler:        cfg.EventHandler,
		onBuildEvent:   cfg.OnBuildEvent,
		logger:         logger,
		gitIdentityFn:  cfg.GitIdentityFn,
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

	current.State = "failed"
	current.ErrorMessage = actionErr.Error()
	if err := d.db.UpdateIssue(current); err != nil {
		d.logger.Error("updating issue to failed after action", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "action_failed", "", "failed", actionErr.Error()); err != nil {
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

	handler := &buildEventHandler{
		db:           d.db,
		issueID:      issue.ID,
		upstream:     d.handler,
		onBuildEvent: d.onBuildEvent,
	}

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
		d.handleSuccess(issue)
		return
	}

	// Context cancellation: clean exit, issue stays in BUILDING
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		d.logger.Info("build cancelled", "issue", issue.ID)
		return
	}

	d.handleFailure(issue, runErr)
}

func (d *Dispatcher) handleSuccess(issue db.Issue) {
	if d.pr != nil {
		if err := d.pr.CreatePR(issue, d.db); err != nil {
			d.logger.Error("creating PR", "issue", issue.ID, "error", err)
			// Re-read issue from DB since CreatePR may have partially updated it.
			fresh, readErr := d.db.GetIssue(issue.ID)
			if readErr != nil {
				d.logger.Error("re-reading issue after PR failure", "issue", issue.ID, "error", readErr)
				fresh = issue
			}
			var conflictErr *pr.ConflictError
			if errors.As(err, &conflictErr) {
				d.handleConflict(fresh, conflictErr)
				return
			}
			d.handleFailure(fresh, fmt.Errorf("creating PR: %w", err))
			return
		}
		// Re-read issue to pick up PR info stored by CreatePR.
		fresh, err := d.db.GetIssue(issue.ID)
		if err != nil {
			d.logger.Error("re-reading issue after PR creation", "issue", issue.ID, "error", err)
		} else {
			issue = fresh
		}
	}

	issue.State = "in_review"
	if err := d.db.UpdateIssue(issue); err != nil {
		d.logger.Error("updating issue to in_review", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "build_completed", "building", "in_review", "Build completed successfully"); err != nil {
		d.logger.Error("logging build_completed activity", "issue", issue.ID, "error", err)
	}
}

func (d *Dispatcher) handleConflict(issue db.Issue, conflictErr *pr.ConflictError) {
	issue.State = "paused"
	issue.ErrorMessage = conflictErr.Error()
	if err := d.db.UpdateIssue(issue); err != nil {
		d.logger.Error("updating issue to paused", "issue", issue.ID, "error", err)
		return
	}
	if err := d.db.LogActivity(issue.ID, "merge_conflict", "building", "paused", conflictErr.Error()); err != nil {
		d.logger.Error("logging merge_conflict activity", "issue", issue.ID, "error", err)
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

// buildEventHandler wraps events from the build loop, stores them in the
// activity log, and forwards them to an optional upstream handler (e.g.
// WebSocket hub).
type buildEventHandler struct {
	db           *db.DB
	issueID      string
	upstream     events.EventHandler
	onBuildEvent func(issueID, detail string)
}

func (h *buildEventHandler) Handle(e events.Event) {
	// Store significant events in the activity log
	detail := formatEventDetail(e)
	if detail != "" {
		_ = h.db.LogActivity(h.issueID, "build_event", "", "", detail)

		// Notify caller (e.g. WebSocket broadcast) for real-time streaming.
		if h.onBuildEvent != nil {
			h.onBuildEvent(h.issueID, detail)
		}
	}

	// Forward to upstream handler (e.g. WebSocket hub)
	if h.upstream != nil {
		h.upstream.Handle(e)
	}
}

// formatEventDetail converts an event to a human-readable string for the
// activity log. Returns empty string for events that shouldn't be logged.
func formatEventDetail(e events.Event) string {
	switch ev := e.(type) {
	case events.ToolUse:
		if ev.Detail != "" {
			return fmt.Sprintf("→ %s %s", ev.Name, ev.Detail)
		}
		return fmt.Sprintf("→ %s", ev.Name)
	case events.IterationStart:
		return fmt.Sprintf("Iteration %d/%d started", ev.Iteration, ev.MaxIterations)
	case events.StoryStarted:
		return fmt.Sprintf("Story %s: %s", ev.StoryID, ev.Title)
	case events.QAPhaseStarted:
		return fmt.Sprintf("QA phase: %s", ev.Phase)
	case events.LogMessage:
		return fmt.Sprintf("[%s] %s", ev.Level, ev.Message)
	case events.AgentText:
		return ev.Text
	case events.InvocationDone:
		return fmt.Sprintf("Invocation done: %d turns in %dms", ev.NumTurns, ev.DurationMS)
	default:
		return ""
	}
}
