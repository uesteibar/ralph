package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/autoralph/server"
	"github.com/uesteibar/ralph/internal/autoralph/usagelimit"
	"github.com/uesteibar/ralph/internal/autoralph/worker"
)

func orchestratorTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func orchestratorTestIssue(t *testing.T, d *db.DB, state string) db.Issue {
	t.Helper()
	proj, err := d.CreateProject(db.Project{
		Name:          "test-proj",
		LocalPath:     "/tmp/test",
		LinearTeamID:  "team-1",
		BranchPrefix:  "autoralph/",
		MaxIterations: 20,
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		Identifier:    "TEST-1",
		Title:         "Test issue",
		State:         state,
		WorkspaceName: "test-ws",
		BranchName:    "autoralph/test-1",
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	return issue
}

// --- isAsyncTransition tests ---

func TestIsAsyncTransition_AddressingFeedback(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateAddressingFeedback,
		To:   orchestrator.StateInReview,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected addressing_feedback → in_review to be async")
	}
}

func TestIsAsyncTransition_FixingChecks(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateFixingChecks,
		To:   orchestrator.StateInReview,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected fixing_checks → in_review to be async")
	}
}

func TestIsAsyncTransition_InReviewRebase(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateInReview,
		To:   orchestrator.StateInReview,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected in_review → in_review (rebase) to be async")
	}
}

func TestIsAsyncTransition_InReviewToOther_NotAsync(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateInReview,
		To:   orchestrator.StateAddressingFeedback,
	}
	if isAsyncTransition(tr) {
		t.Error("expected in_review → addressing_feedback to NOT be async")
	}
}

func TestIsAsyncTransition_ApprovedToBuilding(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateApproved,
		To:   orchestrator.StateBuilding,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected approved → building to be async")
	}
}

func TestIsAsyncTransition_QueuedToRefining(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected queued → refining to be async")
	}
}

func TestIsAsyncTransition_RefiningIteration(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateRefining,
		To:   orchestrator.StateRefining,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected refining → refining (iteration) to be async")
	}
}

func TestIsAsyncTransition_RefiningToApproved_NotAsync(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateRefining,
		To:   orchestrator.StateApproved,
	}
	if isAsyncTransition(tr) {
		t.Error("expected refining → approved to NOT be async")
	}
}

// --- dispatchAsync tests ---

func TestDispatchAsync_RunsActionAndTransitionsState(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "addressing_feedback")

	var actionCalled bool
	tr := orchestrator.Transition{
		From: orchestrator.StateAddressingFeedback,
		To:   orchestrator.StateInReview,
		Action: func(i db.Issue, d *db.DB) error {
			actionCalled = true
			return nil
		},
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)

	// Wait for async action to complete.
	dispatcher.Wait()

	if !actionCalled {
		t.Error("expected action to be called")
	}

	// Verify state was transitioned.
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state in_review, got %s", updated.State)
	}

	// Verify activity was logged.
	activities, err := database.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "state_change" && a.FromState == "addressing_feedback" && a.ToState == "in_review" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_change activity to be logged")
	}
}

func TestDispatchAsync_SkipsWhenAlreadyRunning(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "addressing_feedback")

	actionCount := 0
	var mu sync.Mutex
	blocker := make(chan struct{})

	tr := orchestrator.Transition{
		From: orchestrator.StateAddressingFeedback,
		To:   orchestrator.StateInReview,
		Action: func(i db.Issue, d *db.DB) error {
			mu.Lock()
			actionCount++
			mu.Unlock()
			<-blocker
			return nil
		},
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	// First dispatch succeeds.
	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)

	// Wait for the action to start running.
	time.Sleep(50 * time.Millisecond)

	if !dispatcher.IsRunning(issue.ID) {
		t.Fatal("expected issue to be running after first dispatch")
	}

	// Second dispatch should be skipped by the orchestrator loop's
	// IsRunning check. Simulate that here by checking IsRunning before dispatching.
	if dispatcher.IsRunning(issue.ID) {
		// This is what the orchestrator loop does — it skips dispatch.
	} else {
		t.Error("expected IsRunning to return true while action is running")
	}

	close(blocker)
	dispatcher.Wait()

	mu.Lock()
	if actionCount != 1 {
		t.Errorf("expected action called once, got %d", actionCount)
	}
	mu.Unlock()
}

func TestDispatchAsync_ActionError_SetsFailedState(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "fixing_checks")

	tr := orchestrator.Transition{
		From: orchestrator.StateFixingChecks,
		To:   orchestrator.StateInReview,
		Action: func(i db.Issue, d *db.DB) error {
			return fmt.Errorf("AI invocation failed")
		},
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	// On action error, DispatchAction's handleActionFailure sets the state to failed.
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "failed" {
		t.Errorf("expected state failed, got %s", updated.State)
	}
	if updated.ErrorMessage == "" {
		t.Error("expected error message to be set")
	}
}

func TestDispatchAsync_RebaseTransition_TransitionsToSameState(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "in_review")

	tr := orchestrator.Transition{
		From: orchestrator.StateInReview,
		To:   orchestrator.StateInReview,
		Action: func(i db.Issue, d *db.DB) error {
			return nil
		},
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state in_review, got %s", updated.State)
	}
}

func TestDispatchAsync_WithHub_BroadcastsWithoutPanic(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "addressing_feedback")

	tr := orchestrator.Transition{
		From: orchestrator.StateAddressingFeedback,
		To:   orchestrator.StateInReview,
		Action: func(i db.Issue, d *db.DB) error {
			return nil
		},
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	// Non-nil hub — verifies broadcast code path runs without error
	// even when there are no connected WebSocket clients.
	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state in_review, got %s", updated.State)
	}
}

func TestDispatchAsync_NilAction_StillTransitions(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "addressing_feedback")

	tr := orchestrator.Transition{
		From:   orchestrator.StateAddressingFeedback,
		To:     orchestrator.StateInReview,
		Action: nil,
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	dispatchAsync(context.Background(), tr, issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state in_review, got %s", updated.State)
	}
}

// --- Usage limit guard tests ---

func TestOrchestratorLoop_UsageLimitActive_SkipsAsyncDispatch(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "queued")

	var actionCalled bool
	sm := orchestrator.New(database)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
		Action: func(i db.Issue, d *db.DB) error {
			actionCalled = true
			return nil
		},
	})

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()
	ulState := usagelimit.NewState(logger)
	ulState.Set(time.Now().Add(10 * time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, ulState)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	if actionCalled {
		t.Error("expected async transition NOT to be dispatched during usage limit")
	}

	// Verify issue state unchanged.
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "queued" {
		t.Errorf("expected state queued, got %s", updated.State)
	}
}

func TestOrchestratorLoop_UsageLimitActive_SkipsBuildingRedispatch(t *testing.T) {
	database := orchestratorTestDB(t)
	_ = orchestratorTestIssue(t, database, "building")

	sm := orchestrator.New(database)

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()
	ulState := usagelimit.NewState(logger)
	ulState.Set(time.Now().Add(10 * time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, ulState)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	if dispatcher.ActiveCount() != 0 {
		t.Error("expected no active workers during usage limit")
	}
}

func TestOrchestratorLoop_UsageLimitActive_SyncTransitionContinues(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "refining")

	var actionCalled bool
	sm := orchestrator.New(database)
	// REFINING → APPROVED is a sync transition (not async).
	sm.Register(orchestrator.Transition{
		From:      orchestrator.StateRefining,
		To:        orchestrator.StateApproved,
		Condition: func(i db.Issue) bool { return true },
		Action: func(i db.Issue, d *db.DB) error {
			actionCalled = true
			return nil
		},
	})

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()
	ulState := usagelimit.NewState(logger)
	ulState.Set(time.Now().Add(10 * time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, ulState)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()

	if !actionCalled {
		t.Error("expected sync transition (REFINING→APPROVED) to execute during usage limit")
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "approved" {
		t.Errorf("expected state approved, got %s", updated.State)
	}
}

func TestOrchestratorLoop_UsageLimitExpired_ResumesDispatch(t *testing.T) {
	database := orchestratorTestDB(t)
	_ = orchestratorTestIssue(t, database, "queued")

	var actionCalled bool
	sm := orchestrator.New(database)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
		Action: func(i db.Issue, d *db.DB) error {
			actionCalled = true
			return nil
		},
	})

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()
	ulState := usagelimit.NewState(logger)
	// Set usage limit already expired.
	ulState.Set(time.Now().Add(-1 * time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, ulState)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(200 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	if !actionCalled {
		t.Error("expected async transition to be dispatched after usage limit expired")
	}
}

func TestOrchestratorLoop_NilUsageLimitState_WorksNormally(t *testing.T) {
	database := orchestratorTestDB(t)
	_ = orchestratorTestIssue(t, database, "queued")

	var actionCalled bool
	sm := orchestrator.New(database)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
		Action: func(i db.Issue, d *db.DB) error {
			actionCalled = true
			return nil
		},
	})

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(200 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	if !actionCalled {
		t.Error("expected dispatch to work normally when usage limit state is nil")
	}
}
