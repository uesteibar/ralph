package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/pr"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/shell"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func createTestProject(t *testing.T, d *db.DB) db.Project {
	t.Helper()
	p, err := d.CreateProject(db.Project{
		Name:             "test-project",
		LocalPath:        "/tmp/test",
		LinearTeamID:     "team-abc",
		LinearAssigneeID: "user-xyz",
		RalphConfigPath:  ".ralph/ralph.yaml",
		BranchPrefix:     "autoralph/",
		MaxIterations:    20,
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

func createTestIssue(t *testing.T, d *db.DB, project db.Project, state string) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         state,
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// --- Mocks ---

type mockLoopRunner struct {
	mu       sync.Mutex
	calls    []loopRunCall
	err      error
	blockCtx bool // if true, block until ctx is cancelled
}

type loopRunCall struct {
	workDir      string
	prdPath      string
	progressPath string
}

func (m *mockLoopRunner) Run(ctx context.Context, cfg LoopConfig) error {
	m.mu.Lock()
	m.calls = append(m.calls, loopRunCall{
		workDir:      cfg.WorkDir,
		prdPath:      cfg.PRDPath,
		progressPath: cfg.ProgressPath,
	})
	err := m.err
	block := m.blockCtx
	m.mu.Unlock()

	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (m *mockLoopRunner) getCalls() []loopRunCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]loopRunCall, len(m.calls))
	copy(calls, m.calls)
	return calls
}

type collectingHandler struct {
	mu     sync.Mutex
	events []events.Event
}

func (h *collectingHandler) Handle(e events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *collectingHandler) getEvents() []events.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	evts := make([]events.Event, len(h.events))
	copy(evts, h.events)
	return evts
}

// --- Tests ---

func TestDispatcher_New_DefaultWorkers(t *testing.T) {
	d := testDB(t)
	disp := New(Config{
		DB: d,
	})
	if disp.maxWorkers != 1 {
		t.Errorf("expected default maxWorkers = 1, got %d", disp.maxWorkers)
	}
}

func TestDispatcher_New_ConfiguredWorkers(t *testing.T) {
	d := testDB(t)
	disp := New(Config{
		DB:         d,
		MaxWorkers: 3,
	})
	if disp.maxWorkers != 3 {
		t.Errorf("expected maxWorkers = 3, got %d", disp.maxWorkers)
	}
}

func TestDispatcher_Dispatch_CallsLoopRun(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the worker to complete
	disp.Wait()

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 loop.Run call, got %d", len(calls))
	}
}

func TestDispatcher_Dispatch_CorrectPaths(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	call := calls[0]

	expectedWorkDir := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if call.workDir != expectedWorkDir {
		t.Errorf("expected workDir %q, got %q", expectedWorkDir, call.workDir)
	}

	expectedPRD := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "prd.json")
	if call.prdPath != expectedPRD {
		t.Errorf("expected prdPath %q, got %q", expectedPRD, call.prdPath)
	}

	expectedProgress := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "progress.txt")
	if call.progressPath != expectedProgress {
		t.Errorf("expected progressPath %q, got %q", expectedProgress, call.progressPath)
	}
}

func TestDispatcher_Dispatch_SuccessTransitionsToInReview(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{} // returns nil (success)
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state %q, got %q", "in_review", updated.State)
	}
}

func TestDispatcher_Dispatch_SuccessLogsActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.EventType == "build_completed" {
			found = true
			if !strings.Contains(e.Detail, "success") {
				t.Errorf("expected activity detail to contain 'success', got %q", e.Detail)
			}
		}
	}
	if !found {
		t.Error("expected build_completed activity entry")
	}
}

func TestDispatcher_Dispatch_FailureTransitionsToFailed(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{err: errors.New("max iterations (20) reached without completing all stories")}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "failed" {
		t.Errorf("expected state %q, got %q", "failed", updated.State)
	}
	if !strings.Contains(updated.ErrorMessage, "max iterations") {
		t.Errorf("expected error_message to contain 'max iterations', got %q", updated.ErrorMessage)
	}
}

func TestDispatcher_Dispatch_FailureLogsActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{err: errors.New("build failed")}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.EventType == "build_failed" {
			found = true
			if !strings.Contains(e.Detail, "build failed") {
				t.Errorf("expected activity detail to contain error, got %q", e.Detail)
			}
		}
	}
	if !found {
		t.Error("expected build_failed activity entry")
	}
}

func TestDispatcher_Dispatch_ContextCancellation_StaysInBuilding(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context — simulates shutdown
	cancel()

	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "building" {
		t.Errorf("expected state to stay %q on context cancellation, got %q", "building", updated.State)
	}
}

func TestDispatcher_Dispatch_ForwardsEventsToHandler(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	handler := &collectingHandler{}

	// Use a runner that triggers the handler via the LoopConfig's EventHandler
	runner := &eventForwardingRunner{}
	disp := New(Config{
		DB:           d,
		MaxWorkers:   1,
		LoopRunner:   runner,
		Projects:     d,
		EventHandler: handler,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	evts := handler.getEvents()
	if len(evts) == 0 {
		t.Error("expected at least one event forwarded to handler")
	}

	// Check that a LogMessage was forwarded
	found := false
	for _, e := range evts {
		if lm, ok := e.(events.LogMessage); ok {
			if strings.Contains(lm.Message, "test event") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected LogMessage with 'test event' to be forwarded")
	}
}

func TestDispatcher_Dispatch_StoresEventsInActivityLog(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &eventForwardingRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	entries, err := d.ListActivity(issue.ID, 50, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.EventType == "build_event" {
			found = true
		}
	}
	if !found {
		t.Error("expected build_event activity entries")
	}
}

func TestDispatcher_Dispatch_RespectsWorkerLimit(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)

	// Create two building issues
	issue1, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-1",
		Identifier:    "PROJ-1",
		Title:         "Issue 1",
		State:         "building",
		WorkspaceName: "proj-1",
		BranchName:    "autoralph/proj-1",
	})
	if err != nil {
		t.Fatalf("creating issue 1: %v", err)
	}
	issue2, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-2",
		Identifier:    "PROJ-2",
		Title:         "Issue 2",
		State:         "building",
		WorkspaceName: "proj-2",
		BranchName:    "autoralph/proj-2",
	})
	if err != nil {
		t.Fatalf("creating issue 2: %v", err)
	}

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First dispatch should succeed
	err = disp.Dispatch(ctx, issue1)
	if err != nil {
		t.Fatalf("first dispatch: unexpected error: %v", err)
	}

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Second dispatch should return error (no worker slot available)
	err = disp.Dispatch(ctx, issue2)
	if err == nil {
		t.Fatal("expected error when worker limit exceeded")
	}
	if !strings.Contains(err.Error(), "no worker slot") {
		t.Errorf("expected 'no worker slot' error, got: %v", err)
	}

	cancel()
	disp.Wait()

	_ = issue2 // used for dispatch
}

func TestDispatcher_Dispatch_DuplicateIssue_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 2,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("first dispatch: unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Dispatching the same issue again should fail
	err = disp.Dispatch(ctx, issue)
	if err == nil {
		t.Fatal("expected error when dispatching duplicate issue")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}

	cancel()
	disp.Wait()
}

func TestDispatcher_IsRunning(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if disp.IsRunning(issue.ID) {
		t.Error("expected issue to not be running before dispatch")
	}

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !disp.IsRunning(issue.ID) {
		t.Error("expected issue to be running after dispatch")
	}

	cancel()
	disp.Wait()

	if disp.IsRunning(issue.ID) {
		t.Error("expected issue to not be running after completion")
	}
}

func TestDispatcher_ActiveCount(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 2,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if disp.ActiveCount() != 0 {
		t.Errorf("expected 0 active workers, got %d", disp.ActiveCount())
	}

	err := disp.Dispatch(ctx, issue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if disp.ActiveCount() != 1 {
		t.Errorf("expected 1 active worker, got %d", disp.ActiveCount())
	}

	cancel()
	disp.Wait()

	if disp.ActiveCount() != 0 {
		t.Errorf("expected 0 active workers after completion, got %d", disp.ActiveCount())
	}
}

// eventForwardingRunner emits an event through the LoopConfig's EventHandler
// to verify that events are forwarded to the Dispatcher's handler.
type eventForwardingRunner struct{}

func (r *eventForwardingRunner) Run(ctx context.Context, cfg LoopConfig) error {
	if cfg.EventHandler != nil {
		cfg.EventHandler.Handle(events.LogMessage{
			Level:   "info",
			Message: "test event from build worker",
		})
	}
	return nil
}

// mockPRCreator is a PRCreator that returns a configurable error.
type mockPRCreator struct {
	err error
}

func (m *mockPRCreator) CreatePR(issue db.Issue, database *db.DB) error {
	return m.err
}

func TestDispatcher_Dispatch_ConflictError_TransitionsToPaused(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{} // success
	prCreator := &mockPRCreator{err: &pr.ConflictError{Files: []string{"main.go", "handler.go"}}}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
		PR:         prCreator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disp.Dispatch(ctx, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state %q, got %q", "paused", updated.State)
	}
	if !strings.Contains(updated.ErrorMessage, "merge conflicts") {
		t.Errorf("expected error message to contain 'merge conflicts', got %q", updated.ErrorMessage)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.EventType == "merge_conflict" {
			found = true
		}
	}
	if !found {
		t.Error("expected merge_conflict activity entry")
	}
}

func TestDispatcher_Dispatch_PRFailure_TransitionsToFailed(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "building")

	runner := &mockLoopRunner{} // success
	prCreator := &mockPRCreator{err: errors.New("github API error")}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
		PR:         prCreator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disp.Dispatch(ctx, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "failed" {
		t.Errorf("expected state %q, got %q", "failed", updated.State)
	}
	if !strings.Contains(updated.ErrorMessage, "github API error") {
		t.Errorf("expected error message to contain 'github API error', got %q", updated.ErrorMessage)
	}
}

func TestDispatcher_RecoverBuilding_RedispatchesBuildingIssues(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)

	// Create two issues: one building, one approved (should be ignored)
	building := createTestIssue(t, d, project, "building")
	_, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-other",
		Identifier:    "PROJ-99",
		Title:         "Approved issue",
		State:         "approved",
		WorkspaceName: "proj-99",
		BranchName:    "autoralph/proj-99",
	})
	if err != nil {
		t.Fatalf("creating approved issue: %v", err)
	}

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 2,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recovered, err := disp.RecoverBuilding(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recovered != 1 {
		t.Errorf("expected 1 recovered issue, got %d", recovered)
	}

	disp.Wait()

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 loop.Run call, got %d", len(calls))
	}

	// The building issue should have been processed (moved to in_review since runner succeeds)
	updated, err := d.GetIssue(building.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state %q after recovery, got %q", "in_review", updated.State)
	}
}

func TestDispatcher_RecoverBuilding_NoIssues(t *testing.T) {
	d := testDB(t)

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recovered, err := disp.RecoverBuilding(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recovered != 0 {
		t.Errorf("expected 0 recovered, got %d", recovered)
	}
}

// initTreeRepo creates a project directory structure with a real git repo at the
// tree path so that worker.run() can call git config in it.
func initTreeRepo(t *testing.T, projectPath, workspaceName string) {
	t.Helper()
	treePath := filepath.Join(projectPath, ".ralph", "workspaces", workspaceName, "tree")
	if err := os.MkdirAll(treePath, 0755); err != nil {
		t.Fatalf("creating tree dir: %v", err)
	}
	r := &shell.Runner{Dir: treePath}
	ctx := context.Background()
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "user.email", "test@test.com"},
	} {
		if _, err := r.Run(ctx, args[0], args[1:]...); err != nil {
			t.Fatalf("init tree repo %v: %v", args, err)
		}
	}
	// Create an initial commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(treePath, "init.txt"), []byte("init"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcher_Dispatch_ConfiguresGitIdentityInWorktree(t *testing.T) {
	d := testDB(t)
	projectPath := t.TempDir()
	wsName := "proj-42"

	// Create a real git repo at the tree path.
	initTreeRepo(t, projectPath, wsName)

	p, err := d.CreateProject(db.Project{
		Name:             "git-id-test",
		LocalPath:        projectPath,
		LinearTeamID:     "team-abc",
		LinearAssigneeID: "user-xyz",
		RalphConfigPath:  ".ralph/ralph.yaml",
		BranchPrefix:     "autoralph/",
		MaxIterations:    5,
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	issue := createTestIssue(t, d, p, "building")

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:             d,
		MaxWorkers:     1,
		LoopRunner:     runner,
		Projects:       d,
		GitAuthorName:  "autoralph-bot",
		GitAuthorEmail: "autoralph-bot@noreply",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disp.Dispatch(ctx, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	disp.Wait()

	// Verify git config was set in the tree directory.
	treePath := filepath.Join(projectPath, ".ralph", "workspaces", wsName, "tree")
	r := &shell.Runner{Dir: treePath}

	out, err := r.Run(ctx, "git", "config", "--local", "user.name")
	if err != nil {
		t.Fatalf("reading user.name: %v", err)
	}
	if got := strings.TrimSpace(out); got != "autoralph-bot" {
		t.Errorf("user.name = %q, want %q", got, "autoralph-bot")
	}

	out, err = r.Run(ctx, "git", "config", "--local", "user.email")
	if err != nil {
		t.Fatalf("reading user.email: %v", err)
	}
	if got := strings.TrimSpace(out); got != "autoralph-bot@noreply" {
		t.Errorf("user.email = %q, want %q", got, "autoralph-bot@noreply")
	}
}

func TestDispatcher_Dispatch_GitIdentityUsedBySubsequentCommits(t *testing.T) {
	d := testDB(t)
	projectPath := t.TempDir()
	wsName := "proj-42"

	initTreeRepo(t, projectPath, wsName)

	p, err := d.CreateProject(db.Project{
		Name:             "git-commit-test",
		LocalPath:        projectPath,
		LinearTeamID:     "team-abc",
		LinearAssigneeID: "user-xyz",
		RalphConfigPath:  ".ralph/ralph.yaml",
		BranchPrefix:     "autoralph/",
		MaxIterations:    5,
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	issue := createTestIssue(t, d, p, "building")

	treePath := filepath.Join(projectPath, ".ralph", "workspaces", wsName, "tree")

	// Use a runner that creates a commit during the loop (simulating Claude CLI).
	commitRunner := &commitDuringLoopRunner{treePath: treePath}
	disp := New(Config{
		DB:             d,
		MaxWorkers:     1,
		LoopRunner:     commitRunner,
		Projects:       d,
		GitAuthorName:  "autoralph-ci",
		GitAuthorEmail: "autoralph-ci@noreply",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disp.Dispatch(ctx, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	disp.Wait()

	// Verify the commit made during the loop used the configured identity.
	r := &shell.Runner{Dir: treePath}
	out, err := r.Run(ctx, "git", "log", "-1", "--format=%an <%ae>")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	got := strings.TrimSpace(out)
	want := "autoralph-ci <autoralph-ci@noreply>"
	if got != want {
		t.Errorf("commit author = %q, want %q", got, want)
	}
}

// commitDuringLoopRunner creates a commit in the worktree during the loop,
// simulating what Claude CLI would do.
type commitDuringLoopRunner struct {
	treePath string
}

func (r *commitDuringLoopRunner) Run(ctx context.Context, cfg LoopConfig) error {
	// Create a file and commit it, simulating Claude CLI behavior.
	if err := os.WriteFile(filepath.Join(r.treePath, "claude-change.txt"), []byte("change"), 0644); err != nil {
		return err
	}
	runner := &shell.Runner{Dir: r.treePath}
	if _, err := runner.Run(ctx, "git", "add", "-A"); err != nil {
		return err
	}
	if _, err := runner.Run(ctx, "git", "commit", "-m", "claude commit"); err != nil {
		return err
	}
	return nil
}

// --- DispatchAction tests ---

func TestDispatcher_DispatchAction_RunsActionAndCleansUp(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "addressing_feedback")

	runner := &mockLoopRunner{}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 2,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := false
	err := disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	if !called {
		t.Error("expected action function to be called")
	}
	if disp.IsRunning(issue.ID) {
		t.Error("expected issue to not be running after action completes")
	}
	if disp.ActiveCount() != 0 {
		t.Errorf("expected 0 active workers after completion, got %d", disp.ActiveCount())
	}
}

func TestDispatcher_DispatchAction_ReusesExistingSemaphore(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue1 := createTestIssue(t, d, project, "building")

	issue2, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-action",
		Identifier:    "PROJ-99",
		Title:         "Feedback issue",
		State:         "addressing_feedback",
		WorkspaceName: "proj-99",
		BranchName:    "autoralph/proj-99",
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	runner := &mockLoopRunner{blockCtx: true}
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fill the single worker slot with a build
	if err := disp.Dispatch(ctx, issue1); err != nil {
		t.Fatalf("dispatch build: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// DispatchAction should fail because the semaphore is full
	err = disp.DispatchAction(ctx, issue2, func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error when worker limit exceeded")
	}
	if !strings.Contains(err.Error(), "no worker slot") {
		t.Errorf("expected 'no worker slot' error, got: %v", err)
	}

	cancel()
	disp.Wait()
}

func TestDispatcher_DispatchAction_PreventsConurrentActionsOnSameIssue(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "addressing_feedback")

	blockCh := make(chan struct{})
	disp := New(Config{
		DB:         d,
		MaxWorkers: 2,
		LoopRunner: &mockLoopRunner{},
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		<-blockCh
		return nil
	})
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Second dispatch of the same issue should fail
	err = disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate issue")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}

	close(blockCh)
	disp.Wait()
}

func TestDispatcher_DispatchAction_FailureLogsActivityAndSetsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "addressing_feedback")

	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: &mockLoopRunner{},
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		return errors.New("feedback action failed")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "failed" {
		t.Errorf("expected state %q, got %q", "failed", updated.State)
	}
	if !strings.Contains(updated.ErrorMessage, "feedback action failed") {
		t.Errorf("expected error message to contain 'feedback action failed', got %q", updated.ErrorMessage)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.EventType == "action_failed" && strings.Contains(e.Detail, "feedback action failed") {
			found = true
		}
	}
	if !found {
		t.Error("expected action_failed activity entry with error detail")
	}
}

func TestDispatcher_DispatchAction_ContextCancellation_NoFailure(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "addressing_feedback")

	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: &mockLoopRunner{},
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())

	err := disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	disp.Wait()

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	// Should NOT transition to failed on context cancellation
	if updated.State == "failed" {
		t.Error("expected issue NOT to transition to failed on context cancellation")
	}
}

func TestDispatcher_DispatchAction_IsRunningDuringExecution(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, "addressing_feedback")

	blockCh := make(chan struct{})
	disp := New(Config{
		DB:         d,
		MaxWorkers: 1,
		LoopRunner: &mockLoopRunner{},
		Projects:   d,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := disp.DispatchAction(ctx, issue, func(ctx context.Context) error {
		<-blockCh
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !disp.IsRunning(issue.ID) {
		t.Error("expected issue to be running during action execution")
	}
	if disp.ActiveCount() != 1 {
		t.Errorf("expected 1 active worker, got %d", disp.ActiveCount())
	}

	close(blockCh)
	disp.Wait()

	if disp.IsRunning(issue.ID) {
		t.Error("expected issue to not be running after completion")
	}
}
