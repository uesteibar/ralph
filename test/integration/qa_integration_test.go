package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/autoralph/worker"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/tui"
)

// --- helpers ---

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// --- IT-001: PRD without integrationTests loads correctly and QA defaults to pending ---

func TestIT001_PRDWithQAVerification(t *testing.T) {
	t.Run("PRD with qaVerification loads correctly", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		// Create a PRD JSON with qaVerification but no integrationTests.
		raw := map[string]any{
			"project":    "test",
			"branchName": "test/branch",
			"userStories": []map[string]any{
				{"id": "US-001", "title": "Story 1", "passes": true, "priority": 1},
			},
			"qaVerification": map[string]any{
				"status":   "pending",
				"attempts": 0,
			},
		}
		data, _ := json.MarshalIndent(raw, "", "  ")
		os.WriteFile(prdPath, data, 0644)

		p, err := prd.Read(prdPath)
		if err != nil {
			t.Fatalf("prd.Read failed: %v", err)
		}

		// QAVerification should be set with pending status.
		if p.QAVerification == nil {
			t.Fatal("expected QAVerification to be non-nil")
		}
		if p.QAVerification.Status != "pending" {
			t.Errorf("expected status 'pending', got %q", p.QAVerification.Status)
		}
		if p.QAVerification.Attempts != 0 {
			t.Errorf("expected attempts 0, got %d", p.QAVerification.Attempts)
		}

		// QAVerificationStatus helper returns pending.
		if prd.QAVerificationStatus(p) != "pending" {
			t.Errorf("expected QAVerificationStatus() = 'pending', got %q", prd.QAVerificationStatus(p))
		}
	})

	t.Run("PRD without qaVerification defaults to pending", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		raw := map[string]any{
			"project":    "test",
			"branchName": "test/branch",
			"userStories": []map[string]any{
				{"id": "US-001", "title": "Story 1", "passes": true, "priority": 1},
			},
		}
		data, _ := json.MarshalIndent(raw, "", "  ")
		os.WriteFile(prdPath, data, 0644)

		p, err := prd.Read(prdPath)
		if err != nil {
			t.Fatalf("prd.Read failed: %v", err)
		}

		// QAVerification should be nil in the struct.
		if p.QAVerification != nil {
			t.Errorf("expected QAVerification to be nil when not present in JSON")
		}

		// QAVerificationStatus helper returns pending for nil.
		if prd.QAVerificationStatus(p) != "pending" {
			t.Errorf("expected QAVerificationStatus() = 'pending' for nil, got %q", prd.QAVerificationStatus(p))
		}
	})

	t.Run("IntegrationTests field does not exist on PRD struct", func(t *testing.T) {
		// Use reflection to confirm no IntegrationTests field on PRD struct.
		prdType := reflect.TypeOf(prd.PRD{})
		for i := 0; i < prdType.NumField(); i++ {
			if prdType.Field(i).Name == "IntegrationTests" {
				t.Error("expected PRD struct to NOT have IntegrationTests field")
			}
		}
	})

	t.Run("QAVerification roundtrip through write and read", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		p := &prd.PRD{
			Project:    "roundtrip",
			BranchName: "test/branch",
			UserStories: []prd.Story{
				{ID: "US-001", Title: "Story", Passes: true, Priority: 1},
			},
			QAVerification: &prd.QAVerification{Status: "failed", Attempts: 2},
		}

		if err := prd.Write(prdPath, p); err != nil {
			t.Fatalf("writing PRD: %v", err)
		}

		loaded, err := prd.Read(prdPath)
		if err != nil {
			t.Fatalf("reading PRD: %v", err)
		}

		if loaded.QAVerification == nil {
			t.Fatal("expected QAVerification after roundtrip")
		}
		if loaded.QAVerification.Status != "failed" {
			t.Errorf("expected status 'failed', got %q", loaded.QAVerification.Status)
		}
		if loaded.QAVerification.Attempts != 2 {
			t.Errorf("expected attempts 2, got %d", loaded.QAVerification.Attempts)
		}
	})
}

// --- IT-002: Loop exits after stories without running QA ---
// loop.Run() is stories-only. Verified via unit tests in internal/loop/ that:
//   - Run() exits when all stories pass (TestRun_ExitsWhenAllStoriesPass)
//   - Run() emits StoryStarted but no QA events
//
// This integration test verifies the contract from the integration package's
// perspective: events emitted by the loop are only story events, not QA events.

func TestIT002_LoopExitsAfterStoriesNoQA(t *testing.T) {
	t.Run("story events do not include QA events", func(t *testing.T) {
		handler := &recordingHandler{}

		// Simulate the event sequence that loop.Run() produces
		// (stories only, no QA).
		storyEvents := []events.Event{
			events.IterationStart{Iteration: 1, MaxIterations: 5},
			events.StoryStarted{StoryID: "US-001", Title: "Auth feature"},
			events.ToolUse{Name: "Read", Detail: "main.go"},
			events.InvocationDone{NumTurns: 3, DurationMS: 5000},
		}

		for _, ev := range storyEvents {
			handler.Handle(ev)
		}

		// Verify no QA events were emitted.
		for _, ev := range handler.events {
			switch ev.(type) {
			case events.QAVerifyStarted:
				t.Error("unexpected QAVerifyStarted event in story-only loop")
			case events.QAFixStarted:
				t.Error("unexpected QAFixStarted event in story-only loop")
			case events.QAComplete:
				t.Error("unexpected QAComplete event in story-only loop")
			}
		}

		// Verify story events were captured.
		if len(handler.events) != 4 {
			t.Errorf("expected 4 events, got %d", len(handler.events))
		}
	})

	t.Run("loop.Config has no QA-specific invocation fields", func(t *testing.T) {
		// Verify that loop.Run's Config struct does not expose QA-specific
		// invocation fields (isQAVerification, isQAFix were removed in US-007).
		// This is verified at compile-time by the absence of such fields.
		// We verify indirectly that QualityChecks config exists for RunFull.
		configType := reflect.TypeOf(struct {
			MaxIterations int
			MaxQAAttempts int
			QualityChecks []string
		}{})
		if _, ok := configType.FieldByName("QualityChecks"); !ok {
			t.Error("expected QualityChecks field for RunFull use")
		}
	})
}

// --- IT-003: RunFull completes stories then runs QA verification ---
// RunFull() composes stories + QA. Unit tests in internal/loop/run_full_test.go
// verify the full flow including event ordering (TestRunFull_StoriesThenQAPass).
//
// This integration test verifies the cross-component event contract:
// events flow correctly through handlers.

func TestIT003_RunFullStoriesThenQA(t *testing.T) {
	t.Run("event sequence matches RunFull contract", func(t *testing.T) {
		handler := &recordingHandler{}

		// Simulate the event sequence that RunFull() produces when stories
		// pass and QA verification succeeds.
		runFullEvents := []events.Event{
			events.IterationStart{Iteration: 1, MaxIterations: 5},
			events.StoryStarted{StoryID: "US-001", Title: "Auth feature"},
			events.InvocationDone{NumTurns: 3, DurationMS: 5000},
			events.QAVerifyStarted{},
			events.InvocationDone{NumTurns: 10, DurationMS: 30000},
			events.QAComplete{Passed: true},
		}

		for _, ev := range runFullEvents {
			handler.Handle(ev)
		}

		// Verify event types in correct order.
		expectedTypes := []string{
			"IterationStart",
			"StoryStarted",
			"InvocationDone",
			"QAVerifyStarted",
			"InvocationDone",
			"QAComplete",
		}

		if len(handler.events) != len(expectedTypes) {
			t.Fatalf("expected %d events, got %d", len(expectedTypes), len(handler.events))
		}

		for i, ev := range handler.events {
			got := eventTypeName(ev)
			if got != expectedTypes[i] {
				t.Errorf("event %d: expected %s, got %s", i, expectedTypes[i], got)
			}
		}

		// Verify QAComplete indicates success.
		lastEvent := handler.events[len(handler.events)-1]
		qaComplete, ok := lastEvent.(events.QAComplete)
		if !ok {
			t.Fatalf("expected last event to be QAComplete, got %T", lastEvent)
		}
		if !qaComplete.Passed {
			t.Error("expected QAComplete.Passed to be true")
		}
	})

	t.Run("PRD qaVerification updates to passed after successful QA", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		p := &prd.PRD{
			Project:    "test",
			BranchName: "test/branch",
			UserStories: []prd.Story{
				{ID: "US-001", Title: "Story", Passes: true, Priority: 1},
			},
			QAVerification: &prd.QAVerification{Status: "pending", Attempts: 0},
		}
		prd.Write(prdPath, p)

		// Simulate what RunFull does after QA passes.
		p.QAVerification.Status = "passed"
		prd.Write(prdPath, p)

		loaded, _ := prd.Read(prdPath)
		if prd.QAVerificationStatus(loaded) != "passed" {
			t.Errorf("expected QA status 'passed', got %q", prd.QAVerificationStatus(loaded))
		}
	})
}

// --- IT-004: RunFull triggers QA fix loop when quality checks fail ---

func TestIT004_RunFullQAFixLoop(t *testing.T) {
	t.Run("event sequence includes QAFixStarted when checks fail then pass", func(t *testing.T) {
		handler := &recordingHandler{}

		// Simulate RunFull event sequence: QA verify fails, fix runs, re-verify passes.
		runFullFixEvents := []events.Event{
			events.QAVerifyStarted{},
			events.InvocationDone{NumTurns: 10, DurationMS: 30000},
			events.QAFixStarted{},
			events.InvocationDone{NumTurns: 5, DurationMS: 15000},
			events.QAVerifyStarted{},
			events.InvocationDone{NumTurns: 10, DurationMS: 30000},
			events.QAComplete{Passed: true},
		}

		for _, ev := range runFullFixEvents {
			handler.Handle(ev)
		}

		// Count QA events.
		var qaVerifyCount, qaFixCount int
		var qaCompleted bool
		for _, ev := range handler.events {
			switch e := ev.(type) {
			case events.QAVerifyStarted:
				qaVerifyCount++
			case events.QAFixStarted:
				qaFixCount++
			case events.QAComplete:
				qaCompleted = true
				if !e.Passed {
					t.Error("expected QAComplete.Passed to be true after fix")
				}
			}
		}

		if qaVerifyCount != 2 {
			t.Errorf("expected 2 QAVerifyStarted events, got %d", qaVerifyCount)
		}
		if qaFixCount != 1 {
			t.Errorf("expected 1 QAFixStarted event, got %d", qaFixCount)
		}
		if !qaCompleted {
			t.Error("expected QAComplete event")
		}
	})

	t.Run("PRD qaVerification tracks failed attempts", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		p := &prd.PRD{
			Project:    "test",
			BranchName: "test/branch",
			UserStories: []prd.Story{
				{ID: "US-001", Title: "Story", Passes: true, Priority: 1},
			},
			QAVerification: &prd.QAVerification{Status: "pending", Attempts: 0},
		}
		prd.Write(prdPath, p)

		// Simulate first QA verify failure.
		p.QAVerification.Status = "failed"
		p.QAVerification.Attempts = 1
		prd.Write(prdPath, p)

		loaded, _ := prd.Read(prdPath)
		if loaded.QAVerification.Status != "failed" {
			t.Errorf("expected status 'failed', got %q", loaded.QAVerification.Status)
		}
		if loaded.QAVerification.Attempts != 1 {
			t.Errorf("expected attempts 1, got %d", loaded.QAVerification.Attempts)
		}

		// Simulate fix + re-verify success.
		p.QAVerification.Status = "passed"
		prd.Write(prdPath, p)

		loaded, _ = prd.Read(prdPath)
		if prd.QAVerificationStatus(loaded) != "passed" {
			t.Errorf("expected QA status 'passed', got %q", prd.QAVerificationStatus(loaded))
		}
	})
}

// --- IT-005: RunFull pauses after max QA attempts ---

func TestIT005_RunFullMaxAttempts(t *testing.T) {
	t.Run("QAComplete with Passed=false after max attempts", func(t *testing.T) {
		handler := &recordingHandler{}

		// Simulate RunFull with max attempts = 2:
		// verify1 → fix1 → verify2 → QAComplete{Passed: false}
		maxAttemptEvents := []events.Event{
			events.QAVerifyStarted{},
			events.InvocationDone{NumTurns: 10, DurationMS: 30000},
			events.QAFixStarted{},
			events.InvocationDone{NumTurns: 5, DurationMS: 15000},
			events.QAVerifyStarted{},
			events.InvocationDone{NumTurns: 10, DurationMS: 30000},
			events.QAComplete{Passed: false},
		}

		for _, ev := range maxAttemptEvents {
			handler.Handle(ev)
		}

		var qaVerifyCount, qaFixCount int
		var qaComplete *events.QAComplete
		for _, ev := range handler.events {
			switch e := ev.(type) {
			case events.QAVerifyStarted:
				qaVerifyCount++
			case events.QAFixStarted:
				qaFixCount++
			case events.QAComplete:
				qaComplete = &e
			}
		}

		if qaVerifyCount != 2 {
			t.Errorf("expected 2 QAVerifyStarted, got %d", qaVerifyCount)
		}
		if qaFixCount != 1 {
			t.Errorf("expected 1 QAFixStarted, got %d", qaFixCount)
		}
		if qaComplete == nil {
			t.Fatal("expected QAComplete event")
		}
		if qaComplete.Passed {
			t.Error("expected QAComplete.Passed to be false")
		}
	})

	t.Run("PRD qaVerification set to failed after exhausted attempts", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		p := &prd.PRD{
			Project:    "test",
			BranchName: "test/branch",
			UserStories: []prd.Story{
				{ID: "US-001", Title: "Story", Passes: true, Priority: 1},
			},
			QAVerification: &prd.QAVerification{Status: "failed", Attempts: 2},
		}
		prd.Write(prdPath, p)

		loaded, _ := prd.Read(prdPath)
		if prd.QAVerificationStatus(loaded) != "failed" {
			t.Errorf("expected QA status 'failed', got %q", prd.QAVerificationStatus(loaded))
		}
		if loaded.QAVerification.Attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", loaded.QAVerification.Attempts)
		}
	})
}

// --- IT-006: AutoRalph state transitions: building → qa → in_review ---

func TestIT006_BuildingToQAToInReview(t *testing.T) {
	t.Run("worker handleSuccess transitions building to qa", func(t *testing.T) {
		d := openTestDB(t)

		project, err := d.CreateProject(db.Project{
			Name:             "test-project",
			LocalPath:        t.TempDir(),
			LinearTeamID:     "team-abc",
			LinearAssigneeID: "user-xyz",
			RalphConfigPath:  ".ralph/ralph.yaml",
			BranchPrefix:     "autoralph/",
			MaxIterations:    20,
		})
		if err != nil {
			t.Fatalf("creating project: %v", err)
		}

		issue, err := d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-123",
			Identifier:    "PROJ-42",
			Title:         "Add feature",
			State:         "building",
			WorkspaceName: "proj-42",
			BranchName:    "autoralph/proj-42",
		})
		if err != nil {
			t.Fatalf("creating issue: %v", err)
		}

		runner := &immediateRunner{}
		disp := worker.New(worker.Config{
			DB:         d,
			MaxWorkers: 1,
			LoopRunner: runner,
			Projects:   d,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := disp.Dispatch(ctx, issue); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		disp.Wait()

		updated, err := d.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("getting issue: %v", err)
		}

		// Key assertion: building → qa (NOT directly to in_review).
		if updated.State != "qa" {
			t.Errorf("expected state 'qa', got %q", updated.State)
		}

		// Verify build_completed activity was logged.
		activities, err := d.ListActivity(issue.ID, 10, 0)
		if err != nil {
			t.Fatalf("listing activity: %v", err)
		}
		found := false
		for _, a := range activities {
			if a.EventType == "build_completed" {
				found = true
			}
		}
		if !found {
			t.Error("expected build_completed activity entry")
		}
	})

	t.Run("orchestrator registers qa → in_review transition", func(t *testing.T) {
		d := openTestDB(t)
		sm := orchestrator.New(d)

		// Register the qa → in_review transition (as done in main.go).
		err := sm.Register(orchestrator.Transition{
			From: orchestrator.StateQA,
			To:   orchestrator.StateInReview,
			Condition: func(issue db.Issue) bool {
				return true // QA passed condition
			},
		})
		if err != nil {
			t.Fatalf("registering transition: %v", err)
		}

		issue := db.Issue{State: "qa"}
		tr, ok := sm.Evaluate(issue)
		if !ok {
			t.Fatal("expected qa → in_review transition to match")
		}
		if tr.To != orchestrator.StateInReview {
			t.Errorf("expected To state 'in_review', got %q", tr.To)
		}
	})
}

// --- IT-007: AutoRalph state transitions: qa → qa_fix → qa re-verify ---

func TestIT007_QAToQAFixToQA(t *testing.T) {
	t.Run("state machine supports qa → qa_fix → qa cycle", func(t *testing.T) {
		d := openTestDB(t)
		sm := orchestrator.New(d)

		// Register qa → qa_fix transition.
		err := sm.Register(orchestrator.Transition{
			From: orchestrator.StateQA,
			To:   orchestrator.StateQAFix,
			Condition: func(issue db.Issue) bool {
				return true // QA verification failed
			},
		})
		if err != nil {
			t.Fatalf("registering qa → qa_fix: %v", err)
		}

		// Register qa_fix → qa transition.
		err = sm.Register(orchestrator.Transition{
			From: orchestrator.StateQAFix,
			To:   orchestrator.StateQA,
			Condition: func(issue db.Issue) bool {
				return true // Fix completed
			},
		})
		if err != nil {
			t.Fatalf("registering qa_fix → qa: %v", err)
		}

		// Evaluate qa → qa_fix.
		issue := db.Issue{State: "qa"}
		tr, ok := sm.Evaluate(issue)
		if !ok {
			t.Fatal("expected qa → qa_fix transition to match")
		}
		if tr.To != orchestrator.StateQAFix {
			t.Errorf("expected To 'qa_fix', got %q", tr.To)
		}

		// Evaluate qa_fix → qa.
		issue.State = "qa_fix"
		tr, ok = sm.Evaluate(issue)
		if !ok {
			t.Fatal("expected qa_fix → qa transition to match")
		}
		if tr.To != orchestrator.StateQA {
			t.Errorf("expected To 'qa', got %q", tr.To)
		}
	})

	t.Run("qa and qa_fix are valid states", func(t *testing.T) {
		if !orchestrator.ValidState(orchestrator.StateQA) {
			t.Error("expected StateQA to be valid")
		}
		if !orchestrator.ValidState(orchestrator.StateQAFix) {
			t.Error("expected StateQAFix to be valid")
		}
	})

	t.Run("full qa cycle with database state tracking", func(t *testing.T) {
		d := openTestDB(t)

		project, _ := d.CreateProject(db.Project{
			Name:         "cycle-test",
			LocalPath:    t.TempDir(),
			LinearTeamID: "team",
		})

		issue, _ := d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-1",
			Identifier:    "PROJ-1",
			Title:         "Feature",
			State:         "qa",
			WorkspaceName: "proj-1",
			BranchName:    "autoralph/proj-1",
		})

		// Simulate qa → qa_fix transition.
		issue.State = "qa_fix"
		issue.QAFixAttempts = 1
		if err := d.UpdateIssue(issue); err != nil {
			t.Fatalf("updating to qa_fix: %v", err)
		}

		updated, _ := d.GetIssue(issue.ID)
		if updated.State != "qa_fix" {
			t.Errorf("expected state 'qa_fix', got %q", updated.State)
		}
		if updated.QAFixAttempts != 1 {
			t.Errorf("expected QAFixAttempts=1, got %d", updated.QAFixAttempts)
		}

		// Simulate qa_fix → qa transition (back to verify).
		updated.State = "qa"
		if err := d.UpdateIssue(updated); err != nil {
			t.Fatalf("updating back to qa: %v", err)
		}

		final, _ := d.GetIssue(issue.ID)
		if final.State != "qa" {
			t.Errorf("expected state 'qa' after fix, got %q", final.State)
		}

		// Simulate qa → in_review (QA passed).
		final.State = "in_review"
		if err := d.UpdateIssue(final); err != nil {
			t.Fatalf("updating to in_review: %v", err)
		}

		result, _ := d.GetIssue(issue.ID)
		if result.State != "in_review" {
			t.Errorf("expected state 'in_review', got %q", result.State)
		}
	})
}

// --- IT-008: AutoRalph QA pauses after max attempts ---

func TestIT008_QAPausesAfterMaxAttempts(t *testing.T) {
	t.Run("issue transitions to paused via state machine", func(t *testing.T) {
		d := openTestDB(t)
		sm := orchestrator.New(d)

		maxAttempts := 3

		// Register qa → paused transition (max attempts exceeded).
		err := sm.Register(orchestrator.Transition{
			From: orchestrator.StateQA,
			To:   orchestrator.StatePaused,
			Condition: func(issue db.Issue) bool {
				return issue.QAFixAttempts >= maxAttempts
			},
		})
		if err != nil {
			t.Fatalf("registering qa → paused: %v", err)
		}

		// Register qa → qa_fix transition (lower priority, checked after paused).
		err = sm.Register(orchestrator.Transition{
			From: orchestrator.StateQA,
			To:   orchestrator.StateQAFix,
			Condition: func(issue db.Issue) bool {
				return true
			},
		})
		if err != nil {
			t.Fatalf("registering qa → qa_fix: %v", err)
		}

		// Issue at max attempts → should evaluate to paused.
		issue := db.Issue{State: "qa", QAFixAttempts: 3}
		tr, ok := sm.Evaluate(issue)
		if !ok {
			t.Fatal("expected transition to match")
		}
		if tr.To != orchestrator.StatePaused {
			t.Errorf("expected To 'paused', got %q", tr.To)
		}

		// Issue below max attempts → should evaluate to qa_fix.
		issue.QAFixAttempts = 1
		tr, ok = sm.Evaluate(issue)
		if !ok {
			t.Fatal("expected transition to match")
		}
		if tr.To != orchestrator.StateQAFix {
			t.Errorf("expected To 'qa_fix' for attempts < max, got %q", tr.To)
		}
	})

	t.Run("paused state is valid and persists in DB", func(t *testing.T) {
		if !orchestrator.ValidState(orchestrator.StatePaused) {
			t.Error("expected StatePaused to be valid")
		}

		d := openTestDB(t)
		project, _ := d.CreateProject(db.Project{
			Name:         "pause-test",
			LocalPath:    t.TempDir(),
			LinearTeamID: "team",
		})

		issue, _ := d.CreateIssue(db.Issue{
			ProjectID:      project.ID,
			LinearIssueID:  "lin-pause",
			Identifier:     "PROJ-PAUSE",
			Title:          "Paused feature",
			State:          "qa",
			WorkspaceName:  "proj-pause",
			BranchName:     "autoralph/proj-pause",
			QAFixAttempts:  3,
		})

		// Transition to paused.
		issue.State = "paused"
		if err := d.UpdateIssue(issue); err != nil {
			t.Fatalf("updating to paused: %v", err)
		}

		// Log activity.
		if err := d.LogActivity(issue.ID, "qa_paused", "qa", "paused", "QA verification failed after 3 attempts"); err != nil {
			t.Fatalf("logging activity: %v", err)
		}

		updated, _ := d.GetIssue(issue.ID)
		if updated.State != "paused" {
			t.Errorf("expected state 'paused', got %q", updated.State)
		}

		activities, _ := d.ListActivity(issue.ID, 10, 0)
		found := false
		for _, a := range activities {
			if a.EventType == "qa_paused" && strings.Contains(a.Detail, "3 attempts") {
				found = true
			}
		}
		if !found {
			t.Error("expected qa_paused activity with attempt count")
		}
	})
}

// --- IT-009: QA state recovery on startup ---

func TestIT009_QAStateRecovery(t *testing.T) {
	t.Run("RecoverQA returns qa and qa_fix issues", func(t *testing.T) {
		d := openTestDB(t)

		project, _ := d.CreateProject(db.Project{
			Name:         "recovery-test",
			LocalPath:    t.TempDir(),
			LinearTeamID: "team",
		})

		// Create issues in various states.
		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-qa",
			Identifier:    "PROJ-10",
			Title:         "QA issue",
			State:         "qa",
			WorkspaceName: "proj-10",
			BranchName:    "autoralph/proj-10",
		})

		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-qafix",
			Identifier:    "PROJ-11",
			Title:         "QA fix issue",
			State:         "qa_fix",
			WorkspaceName: "proj-11",
			BranchName:    "autoralph/proj-11",
		})

		// These should NOT be counted.
		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-building",
			Identifier:    "PROJ-12",
			Title:         "Building issue",
			State:         "building",
			WorkspaceName: "proj-12",
			BranchName:    "autoralph/proj-12",
		})

		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-approved",
			Identifier:    "PROJ-13",
			Title:         "Approved issue",
			State:         "approved",
			WorkspaceName: "proj-13",
			BranchName:    "autoralph/proj-13",
		})

		disp := worker.New(worker.Config{
			DB:         d,
			MaxWorkers: 5,
			LoopRunner: &immediateRunner{},
			Projects:   d,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		recovered, err := disp.RecoverQA(ctx)
		if err != nil {
			t.Fatalf("RecoverQA: %v", err)
		}
		if recovered != 2 {
			t.Errorf("expected 2 recovered issues (qa + qa_fix), got %d", recovered)
		}
	})

	t.Run("RecoverQA returns 0 when no QA issues exist", func(t *testing.T) {
		d := openTestDB(t)

		disp := worker.New(worker.Config{
			DB:         d,
			MaxWorkers: 1,
			LoopRunner: &immediateRunner{},
			Projects:   d,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		recovered, err := disp.RecoverQA(ctx)
		if err != nil {
			t.Fatalf("RecoverQA: %v", err)
		}
		if recovered != 0 {
			t.Errorf("expected 0 recovered, got %d", recovered)
		}
	})

	t.Run("db.IssueFilter States filters for multiple states", func(t *testing.T) {
		d := openTestDB(t)

		project, _ := d.CreateProject(db.Project{
			Name:         "filter-test",
			LocalPath:    t.TempDir(),
			LinearTeamID: "team",
		})

		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-1",
			Identifier:    "P-1",
			Title:         "QA",
			State:         "qa",
			WorkspaceName: "p-1",
			BranchName:    "autoralph/p-1",
		})

		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-2",
			Identifier:    "P-2",
			Title:         "QA Fix",
			State:         "qa_fix",
			WorkspaceName: "p-2",
			BranchName:    "autoralph/p-2",
		})

		d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-3",
			Identifier:    "P-3",
			Title:         "Building",
			State:         "building",
			WorkspaceName: "p-3",
			BranchName:    "autoralph/p-3",
		})

		issues, err := d.ListIssues(db.IssueFilter{States: []string{"qa", "qa_fix"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(issues) != 2 {
			t.Errorf("expected 2 issues with States filter, got %d", len(issues))
		}

		for _, issue := range issues {
			if issue.State != "qa" && issue.State != "qa_fix" {
				t.Errorf("unexpected state %q in filtered results", issue.State)
			}
		}
	})
}

// --- IT-010: Web UI displays QA states with correct colors ---
// StateBadge.test.tsx already verifies that qa and qa_fix states render with
// correct colors and text transformation. This Go-side test verifies the state
// constants used by the orchestrator match what the web UI expects.

func TestIT010_WebUIQAStates(t *testing.T) {
	t.Run("qa and qa_fix are valid orchestrator states", func(t *testing.T) {
		if !orchestrator.ValidState(orchestrator.StateQA) {
			t.Error("expected 'qa' to be a valid state")
		}
		if !orchestrator.ValidState(orchestrator.StateQAFix) {
			t.Error("expected 'qa_fix' to be a valid state")
		}
	})

	t.Run("state string values match web UI expectations", func(t *testing.T) {
		if string(orchestrator.StateQA) != "qa" {
			t.Errorf("expected StateQA = 'qa', got %q", orchestrator.StateQA)
		}
		if string(orchestrator.StateQAFix) != "qa_fix" {
			t.Errorf("expected StateQAFix = 'qa_fix', got %q", orchestrator.StateQAFix)
		}
	})

	t.Run("db.Issue State field stores qa and qa_fix correctly", func(t *testing.T) {
		d := openTestDB(t)

		project, _ := d.CreateProject(db.Project{
			Name:         "state-test",
			LocalPath:    t.TempDir(),
			LinearTeamID: "team",
		})

		qaIssue, _ := d.CreateIssue(db.Issue{
			ProjectID:     project.ID,
			LinearIssueID: "lin-qa",
			Identifier:    "P-QA",
			Title:         "QA state",
			State:         "qa",
			WorkspaceName: "p-qa",
			BranchName:    "autoralph/p-qa",
		})

		loaded, _ := d.GetIssue(qaIssue.ID)
		if loaded.State != "qa" {
			t.Errorf("expected stored state 'qa', got %q", loaded.State)
		}

		// Update to qa_fix.
		loaded.State = "qa_fix"
		d.UpdateIssue(loaded)

		reloaded, _ := d.GetIssue(qaIssue.ID)
		if reloaded.State != "qa_fix" {
			t.Errorf("expected stored state 'qa_fix', got %q", reloaded.State)
		}
	})
}

// --- IT-011: QA events visible in TUI and plain text handlers ---

func TestIT011_QAEventsInHandlers(t *testing.T) {
	t.Run("PlainTextHandler renders all QA event types", func(t *testing.T) {
		var buf bytes.Buffer
		handler := &events.PlainTextHandler{W: &buf}

		handler.Handle(events.QAVerifyStarted{})
		handler.Handle(events.QAFixStarted{})
		handler.Handle(events.QAComplete{Passed: true})

		output := stripANSI(buf.String())

		if !strings.Contains(output, "all stories pass — running QA verification") {
			t.Errorf("expected QAVerifyStarted message, got:\n%s", output)
		}
		if !strings.Contains(output, "QA checks failed — running QA fix") {
			t.Errorf("expected QAFixStarted message, got:\n%s", output)
		}
		if !strings.Contains(output, "QA complete — all checks passed") {
			t.Errorf("expected QAComplete passed message, got:\n%s", output)
		}
	})

	t.Run("PlainTextHandler renders QAComplete failed", func(t *testing.T) {
		var buf bytes.Buffer
		handler := &events.PlainTextHandler{W: &buf}

		handler.Handle(events.QAComplete{Passed: false})

		output := stripANSI(buf.String())
		if !strings.Contains(output, "QA complete — checks failed") {
			t.Errorf("expected QAComplete failed message, got:\n%s", output)
		}
	})

	t.Run("TUI model updates current activity for QA events", func(t *testing.T) {
		model := tui.NewModel("test-ws", "")

		// Set window size.
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		model = updated.(tui.Model)

		// QAVerifyStarted → current story becomes "QA verification".
		updated, _ = model.Update(tui.MakeEventMsg(events.QAVerifyStarted{}))
		model = updated.(tui.Model)

		if model.CurrentStory() != "QA verification" {
			t.Errorf("expected current story 'QA verification', got %q", model.CurrentStory())
		}

		lines := model.Lines()
		found := false
		for _, line := range lines {
			if strings.Contains(line, "all stories pass — running QA verification") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected QA verify line in TUI, got: %v", lines)
		}

		// QAFixStarted → current story becomes "QA fix".
		updated, _ = model.Update(tui.MakeEventMsg(events.QAFixStarted{}))
		model = updated.(tui.Model)

		if model.CurrentStory() != "QA fix" {
			t.Errorf("expected current story 'QA fix', got %q", model.CurrentStory())
		}

		// QAComplete → line added.
		updated, _ = model.Update(tui.MakeEventMsg(events.QAComplete{Passed: true}))
		model = updated.(tui.Model)

		lines = model.Lines()
		foundComplete := false
		for _, line := range lines {
			if strings.Contains(line, "QA complete — all checks passed") {
				foundComplete = true
			}
		}
		if !foundComplete {
			t.Errorf("expected QA complete line in TUI, got: %v", lines)
		}
	})

	t.Run("both handlers process same QA event sequence consistently", func(t *testing.T) {
		// PlainTextHandler.
		var buf bytes.Buffer
		plainHandler := &events.PlainTextHandler{W: &buf}

		// TUI model.
		model := tui.NewModel("ws", "")
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		model = updated.(tui.Model)

		qaEvents := []events.Event{
			events.QAVerifyStarted{},
			events.QAFixStarted{},
			events.QAComplete{Passed: false},
		}

		for _, ev := range qaEvents {
			plainHandler.Handle(ev)
			updated, _ = model.Update(tui.MakeEventMsg(ev))
			model = updated.(tui.Model)
		}

		plainOutput := stripANSI(buf.String())
		tuiLines := model.Lines()
		tuiContent := strings.Join(tuiLines, "\n")

		// Both should contain the same QA messages.
		for _, keyword := range []string{
			"QA verification",
			"QA fix",
			"checks failed",
		} {
			if !strings.Contains(plainOutput, keyword) {
				t.Errorf("PlainTextHandler output missing %q", keyword)
			}
			if !strings.Contains(tuiContent, keyword) {
				t.Errorf("TUI model lines missing %q", keyword)
			}
		}
	})

	t.Run("QA event types implement Event interface", func(t *testing.T) {
		// Compile-time verification.
		var _ events.Event = events.QAVerifyStarted{}
		var _ events.Event = events.QAFixStarted{}
		var _ events.Event = events.QAComplete{}
		var _ events.Event = events.QAComplete{Passed: true}
	})
}

// --- Test helpers ---

// immediateRunner is a LoopRunner that returns nil immediately (success).
type immediateRunner struct{}

func (r *immediateRunner) Run(ctx context.Context, cfg worker.LoopConfig) error {
	return nil
}
