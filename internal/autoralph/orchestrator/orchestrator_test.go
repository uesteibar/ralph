package orchestrator

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
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

func createTestIssue(t *testing.T, d *db.DB, state string) db.Issue {
	t.Helper()
	p, err := d.CreateProject(db.Project{Name: "test-project", LocalPath: "/tmp/test"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:  p.ID,
		Identifier: "PROJ-42",
		Title:      "Test issue",
		State:      state,
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// --- ValidState ---

func TestValidState_AllKnownStates(t *testing.T) {
	states := []IssueState{
		StateQueued, StateRefining, StateWaitingApproval, StateApproved,
		StateBuilding, StateInReview, StateAddressingFeedback,
		StateFixingChecks, StateCompleted, StateFailed, StatePaused,
	}
	for _, s := range states {
		if !ValidState(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
}

func TestValidState_UnknownState_ReturnsFalse(t *testing.T) {
	if ValidState("nonexistent") {
		t.Error("expected unknown state to be invalid")
	}
}

// --- Register ---

func TestRegister_ValidTransition(t *testing.T) {
	sm := New(nil)
	err := sm.Register(Transition{
		From: StateQueued,
		To:   StateRefining,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegister_InvalidFromState_ReturnsError(t *testing.T) {
	sm := New(nil)
	err := sm.Register(Transition{
		From: "bogus",
		To:   StateRefining,
	})
	if err == nil {
		t.Error("expected error for invalid from state")
	}
}

func TestRegister_InvalidToState_ReturnsError(t *testing.T) {
	sm := New(nil)
	err := sm.Register(Transition{
		From: StateQueued,
		To:   "bogus",
	})
	if err == nil {
		t.Error("expected error for invalid to state")
	}
}

// --- Evaluate ---

func TestEvaluate_MatchingTransition_ReturnsIt(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From: StateQueued,
		To:   StateRefining,
	})

	issue := db.Issue{State: "queued"}
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected a matching transition")
	}
	if tr.From != StateQueued || tr.To != StateRefining {
		t.Errorf("unexpected transition: %s -> %s", tr.From, tr.To)
	}
}

func TestEvaluate_NoMatchingState_ReturnsFalse(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From: StateQueued,
		To:   StateRefining,
	})

	issue := db.Issue{State: "building"}
	_, ok := sm.Evaluate(issue)
	if ok {
		t.Error("expected no matching transition for non-matching state")
	}
}

func TestEvaluate_ConditionFalse_Skipped(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From:      StateQueued,
		To:        StateRefining,
		Condition: func(db.Issue) bool { return false },
	})

	issue := db.Issue{State: "queued"}
	_, ok := sm.Evaluate(issue)
	if ok {
		t.Error("expected no match when condition returns false")
	}
}

func TestEvaluate_ConditionTrue_Matches(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From:      StateQueued,
		To:        StateRefining,
		Condition: func(db.Issue) bool { return true },
	})

	issue := db.Issue{State: "queued"}
	_, ok := sm.Evaluate(issue)
	if !ok {
		t.Error("expected match when condition returns true")
	}
}

func TestEvaluate_FirstMatchWins(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From: StateQueued,
		To:   StateRefining,
	})
	sm.Register(Transition{
		From: StateQueued,
		To:   StateFailed,
	})

	issue := db.Issue{State: "queued"}
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected a match")
	}
	if tr.To != StateRefining {
		t.Errorf("expected first registered transition to win, got %s", tr.To)
	}
}

func TestEvaluate_SkipsFailedCondition_MatchesNext(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From:      StateQueued,
		To:        StateRefining,
		Condition: func(db.Issue) bool { return false },
	})
	sm.Register(Transition{
		From: StateQueued,
		To:   StateFailed,
	})

	issue := db.Issue{State: "queued"}
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected a match")
	}
	if tr.To != StateFailed {
		t.Errorf("expected second transition, got %s", tr.To)
	}
}

func TestEvaluate_NilCondition_TreatedAsAlwaysTrue(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From:      StateQueued,
		To:        StateRefining,
		Condition: nil,
	})

	issue := db.Issue{State: "queued"}
	_, ok := sm.Evaluate(issue)
	if !ok {
		t.Error("expected nil condition to be treated as always true")
	}
}

// --- Execute ---

func TestExecute_UpdatesStateAndLogsActivity(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "queued")

	tr := Transition{From: StateQueued, To: StateRefining}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if got.State != "refining" {
		t.Errorf("expected state %q, got %q", "refining", got.State)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "state_change" {
		t.Errorf("expected event_type %q, got %q", "state_change", entries[0].EventType)
	}
	if entries[0].FromState != "queued" {
		t.Errorf("expected from_state %q, got %q", "queued", entries[0].FromState)
	}
	if entries[0].ToState != "refining" {
		t.Errorf("expected to_state %q, got %q", "refining", entries[0].ToState)
	}
}

func TestExecute_RunsAction(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "queued")

	actionCalled := false
	tr := Transition{
		From: StateQueued,
		To:   StateRefining,
		Action: func(i db.Issue, database *db.DB) error {
			actionCalled = true
			if i.ID != issue.ID {
				t.Errorf("action received wrong issue ID: %q", i.ID)
			}
			return nil
		},
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !actionCalled {
		t.Error("expected action to be called")
	}
}

func TestExecute_ActionError_RollsBack(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "queued")

	tr := Transition{
		From: StateQueued,
		To:   StateRefining,
		Action: func(db.Issue, *db.DB) error {
			return fmt.Errorf("action failed")
		},
	}
	err := sm.Execute(tr, issue)
	if err == nil {
		t.Fatal("expected error from failing action")
	}

	got, _ := d.GetIssue(issue.ID)
	if got.State != "queued" {
		t.Errorf("expected state to remain %q after rollback, got %q", "queued", got.State)
	}

	entries, _ := d.ListActivity(issue.ID, 10, 0)
	if len(entries) != 0 {
		t.Errorf("expected no activity after rollback, got %d entries", len(entries))
	}
}

func TestExecute_WrongCurrentState_ReturnsError(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "building")

	tr := Transition{From: StateQueued, To: StateRefining}
	err := sm.Execute(tr, issue)
	if err == nil {
		t.Error("expected error when issue state doesn't match transition From")
	}
}

func TestExecute_NilAction_StillTransitions(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "approved")

	tr := Transition{From: StateApproved, To: StateBuilding, Action: nil}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := d.GetIssue(issue.ID)
	if got.State != "building" {
		t.Errorf("expected state %q, got %q", "building", got.State)
	}
}

func TestExecute_ActionCanWriteToDB(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	issue := createTestIssue(t, d, "queued")

	tr := Transition{
		From: StateQueued,
		To:   StateRefining,
		Action: func(i db.Issue, database *db.DB) error {
			return database.LogActivity(i.ID, "ai_invocation", "", "", "Called AI for refinement")
		},
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, _ := d.ListActivity(issue.ID, 10, 0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 activity entries (action + state_change), got %d", len(entries))
	}
}

// --- Full lifecycle transitions ---

func TestEvaluateAndExecute_FullLifecycleTransitions(t *testing.T) {
	d := testDB(t)
	sm := New(d)

	// Use a flag to simulate external conditions controlling which
	// in_review transition fires. In production, conditions would check
	// for new reviews, PR merged status, etc.
	prMerged := false

	sm.Register(Transition{From: StateQueued, To: StateRefining})
	sm.Register(Transition{From: StateRefining, To: StateWaitingApproval})
	sm.Register(Transition{From: StateWaitingApproval, To: StateApproved})
	sm.Register(Transition{From: StateApproved, To: StateBuilding})
	sm.Register(Transition{From: StateBuilding, To: StateInReview})
	sm.Register(Transition{
		From:      StateInReview,
		To:        StateCompleted,
		Condition: func(db.Issue) bool { return prMerged },
	})
	sm.Register(Transition{
		From:      StateInReview,
		To:        StateAddressingFeedback,
		Condition: func(db.Issue) bool { return !prMerged },
	})
	sm.Register(Transition{From: StateAddressingFeedback, To: StateInReview})

	issue := createTestIssue(t, d, "queued")

	// Walk through: queued → refining → waiting_approval → approved →
	// building → in_review → addressing_feedback → in_review → completed
	expectedSequence := []IssueState{
		StateRefining, StateWaitingApproval, StateApproved, StateBuilding,
		StateInReview, StateAddressingFeedback, StateInReview, StateCompleted,
	}

	for i, expected := range expectedSequence {
		// Simulate PR merge before the final transition
		if i == len(expectedSequence)-1 {
			prMerged = true
		}

		issue, _ = d.GetIssue(issue.ID)
		tr, ok := sm.Evaluate(issue)
		if !ok {
			t.Fatalf("step %d: expected transition from %q, got none", i, issue.State)
		}
		if tr.To != expected {
			t.Fatalf("step %d: expected transition to %q, got %q", i, expected, tr.To)
		}
		if err := sm.Execute(tr, issue); err != nil {
			t.Fatalf("step %d: executing %s -> %s: %v", i, tr.From, tr.To, err)
		}
	}

	final, _ := d.GetIssue(issue.ID)
	if final.State != "completed" {
		t.Errorf("expected final state %q, got %q", "completed", final.State)
	}

	entries, _ := d.ListActivity(issue.ID, 100, 0)
	if len(entries) != len(expectedSequence) {
		t.Errorf("expected %d activity entries, got %d", len(expectedSequence), len(entries))
	}
}

func TestRegister_FixingChecksState_ValidTransition(t *testing.T) {
	sm := New(nil)
	err := sm.Register(Transition{
		From: StateInReview,
		To:   StateFixingChecks,
	})
	if err != nil {
		t.Fatalf("unexpected error registering transition to fixing_checks: %v", err)
	}
	err = sm.Register(Transition{
		From: StateFixingChecks,
		To:   StateInReview,
	})
	if err != nil {
		t.Fatalf("unexpected error registering transition from fixing_checks: %v", err)
	}
}

func TestEvaluateAndExecute_FixingChecksTransition(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{
		From: StateInReview,
		To:   StateFixingChecks,
	})
	sm.Register(Transition{
		From: StateFixingChecks,
		To:   StateInReview,
	})

	issue := createTestIssue(t, d, "in_review")

	// in_review -> fixing_checks
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from in_review")
	}
	if tr.To != StateFixingChecks {
		t.Fatalf("expected transition to fixing_checks, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing in_review -> fixing_checks: %v", err)
	}

	issue, _ = d.GetIssue(issue.ID)
	if issue.State != "fixing_checks" {
		t.Fatalf("expected state fixing_checks, got %s", issue.State)
	}

	// fixing_checks -> in_review
	tr, ok = sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from fixing_checks")
	}
	if tr.To != StateInReview {
		t.Fatalf("expected transition to in_review, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing fixing_checks -> in_review: %v", err)
	}

	issue, _ = d.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Fatalf("expected state in_review, got %s", issue.State)
	}
}

func TestEvaluate_CompletedState_NoTransition(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{From: StateQueued, To: StateRefining})

	issue := db.Issue{State: "completed"}
	_, ok := sm.Evaluate(issue)
	if ok {
		t.Error("expected no transition from completed state")
	}
}

func TestEvaluate_FailedState_NoTransition_WhenNoneRegistered(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{From: StateQueued, To: StateRefining})

	issue := db.Issue{State: "failed"}
	_, ok := sm.Evaluate(issue)
	if ok {
		t.Error("expected no transition from failed state when none registered")
	}
}

func TestEvaluate_PausedState_NoTransition_WhenNoneRegistered(t *testing.T) {
	d := testDB(t)
	sm := New(d)
	sm.Register(Transition{From: StateQueued, To: StateRefining})

	issue := db.Issue{State: "paused"}
	_, ok := sm.Evaluate(issue)
	if ok {
		t.Error("expected no transition from paused state when none registered")
	}
}
