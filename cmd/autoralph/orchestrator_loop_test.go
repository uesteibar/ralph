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

// --- Orchestrator loop integration tests ---

func TestOrchestratorLoop_SyncTransitionExecutes(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil, nil)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()

	if !actionCalled {
		t.Error("expected sync transition (REFINING→APPROVED) to execute")
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "approved" {
		t.Errorf("expected state approved, got %s", updated.State)
	}
}

func TestOrchestratorLoop_AsyncTransitionDispatches(t *testing.T) {
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

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil, nil)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(200 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	if !actionCalled {
		t.Error("expected async transition to be dispatched")
	}
}

// --- isAsyncTransition QA tests ---

func TestIsAsyncTransition_QAToInReview(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateQA,
		To:   orchestrator.StateInReview,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected qa → in_review to be async")
	}
}

func TestIsAsyncTransition_QAToQAFix(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateQA,
		To:   orchestrator.StateQAFix,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected qa → qa_fix to be async")
	}
}

func TestIsAsyncTransition_QAFixToQA(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateQAFix,
		To:   orchestrator.StateQA,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected qa_fix → qa to be async")
	}
}

func TestIsAsyncTransition_QAToPaused(t *testing.T) {
	tr := orchestrator.Transition{
		From: orchestrator.StateQA,
		To:   orchestrator.StatePaused,
	}
	if !isAsyncTransition(tr) {
		t.Error("expected qa → paused to be async")
	}
}

// --- qaDispatcher tests ---

func TestQADispatcher_VerifySuccess_TransitionsToInReviewAndCreatesPR(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	var prCalled bool
	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			return nil // QA passes
		},
		fixFn: func(i db.Issue, d *db.DB) error {
			t.Error("fixFn should not be called on verify success")
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error {
			prCalled = true
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

	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	if !prCalled {
		t.Error("expected PR creation function to be called after QA verify success")
	}

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
		if a.EventType == "state_change" && a.FromState == "qa" && a.ToState == "in_review" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_change activity from qa to in_review")
	}
}

func TestQADispatcher_VerifyFailure_TransitionsToQAFix(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			return fmt.Errorf("qa verification failed: ralph check just test")
		},
		fixFn: func(i db.Issue, d *db.DB) error {
			t.Error("fixFn should not be called during verify dispatch")
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error {
			t.Error("prFn should not be called on verify failure")
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

	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "qa_fix" {
		t.Errorf("expected state qa_fix, got %s", updated.State)
	}

	// Verify activity was logged.
	activities, err := database.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "state_change" && a.FromState == "qa" && a.ToState == "qa_fix" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_change activity from qa to qa_fix")
	}
}

func TestQADispatcher_VerifyMaxAttempts_TransitionsToPaused(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			// Simulate verify action setting state to paused (max attempts exceeded).
			current, err := d.GetIssue(i.ID)
			if err != nil {
				return err
			}
			current.State = string(orchestrator.StatePaused)
			return d.UpdateIssue(current)
		},
		fixFn: func(i db.Issue, d *db.DB) error {
			t.Error("fixFn should not be called when paused")
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error {
			t.Error("prFn should not be called when paused")
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

	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state paused, got %s", updated.State)
	}
}

func TestQADispatcher_FixSuccess_TransitionsBackToQA(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa_fix")

	var fixCalled bool
	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			t.Error("verifyFn should not be called for qa_fix state")
			return nil
		},
		fixFn: func(i db.Issue, d *db.DB) error {
			fixCalled = true
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error {
			t.Error("prFn should not be called during fix")
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

	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	if !fixCalled {
		t.Error("expected fix function to be called")
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "qa" {
		t.Errorf("expected state qa, got %s", updated.State)
	}

	// Verify activity was logged.
	activities, err := database.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "state_change" && a.FromState == "qa_fix" && a.ToState == "qa" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_change activity from qa_fix to qa")
	}
}

func TestQADispatcher_SkipsAlreadyRunning(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	callCount := 0
	var mu sync.Mutex
	blocker := make(chan struct{})

	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			mu.Lock()
			callCount++
			mu.Unlock()
			<-blocker
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error { return nil },
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	// First dispatch.
	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	time.Sleep(50 * time.Millisecond)

	// Second dispatch should be skipped (IsRunning check in dispatch).
	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)

	close(blocker)
	dispatcher.Wait()

	mu.Lock()
	if callCount != 1 {
		t.Errorf("expected verifyFn called once, got %d", callCount)
	}
	mu.Unlock()
}

func TestQADispatcher_IgnoresNonQAState(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "building")

	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			t.Error("verifyFn should not be called for building state")
			return nil
		},
		fixFn: func(i db.Issue, d *db.DB) error {
			t.Error("fixFn should not be called for building state")
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

	qaDisp.dispatch(context.Background(), issue, database, dispatcher, hub, logger)
	dispatcher.Wait()

	// Issue state should remain unchanged.
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "building" {
		t.Errorf("expected state building, got %s", updated.State)
	}
}

// --- Orchestrator loop QA dispatch test ---

func TestOrchestratorLoop_DispatchesQAIssues(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	sm := orchestrator.New(database)

	var verifyCalled bool
	var mu sync.Mutex
	qaDisp := &qaDispatcher{
		verifyFn: func(i db.Issue, d *db.DB) error {
			mu.Lock()
			verifyCalled = true
			mu.Unlock()
			return nil
		},
		prFn: func(i db.Issue, d *db.DB) error { return nil },
	}

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil, qaDisp)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(200 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	mu.Lock()
	if !verifyCalled {
		t.Error("expected QA verify to be dispatched for qa issue")
	}
	mu.Unlock()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "in_review" {
		t.Errorf("expected state in_review after QA pass, got %s", updated.State)
	}
}

func TestOrchestratorLoop_DispatchesQAFixIssues(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa_fix")

	sm := orchestrator.New(database)

	var fixCalled bool
	var mu sync.Mutex
	qaDisp := &qaDispatcher{
		fixFn: func(i db.Issue, d *db.DB) error {
			mu.Lock()
			fixCalled = true
			mu.Unlock()
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

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil, qaDisp)

	// Trigger a tick.
	wake <- struct{}{}
	time.Sleep(200 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	mu.Lock()
	if !fixCalled {
		t.Error("expected QA fix to be dispatched for qa_fix issue")
	}
	mu.Unlock()

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "qa" {
		t.Errorf("expected state qa after fix, got %s", updated.State)
	}
}

func TestOrchestratorLoop_SkipsQAWhenNoQADispatcher(t *testing.T) {
	database := orchestratorTestDB(t)
	issue := orchestratorTestIssue(t, database, "qa")

	sm := orchestrator.New(database)

	dispatcher := worker.New(worker.Config{
		DB:         database,
		MaxWorkers: 2,
		Logger:     slog.Default(),
	})

	hub := server.NewHub(slog.Default())
	logger := slog.Default()

	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)

	// Pass nil qaDisp — should not panic or dispatch.
	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger, wake, nil, nil)

	wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()
	dispatcher.Wait()

	// State should remain unchanged (no dispatch happened).
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("reading issue: %v", err)
	}
	if updated.State != "qa" {
		t.Errorf("expected state to remain qa, got %s", updated.State)
	}
}

