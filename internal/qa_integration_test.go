package qa_integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

// --------------------------------------------------------------------------
// IT-001: PRD without integrationTests loads correctly and QA defaults to pending
// --------------------------------------------------------------------------

func TestIT001_PRDWithoutIntegrationTestsLoadsCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	// Step 1: Create a PRD JSON file with userStories and qaVerification
	// but no integrationTests field.
	jsonData := `{
  "project": "test-project",
  "branchName": "ralph/test",
  "description": "A test PRD",
  "userStories": [
    {"id": "US-001", "title": "First story", "passes": true}
  ],
  "qaVerification": {"status": "pending", "attempts": 0}
}`
	if err := os.WriteFile(path, []byte(jsonData), 0644); err != nil {
		t.Fatal(err)
	}

	// Step 2: Call prd.Read()
	p, err := prd.Read(path)
	if err != nil {
		t.Fatalf("prd.Read failed: %v", err)
	}

	// Step 3: Verify QAVerification.Status is "pending" and QAVerificationStatus() returns "pending"
	if p.QAVerification == nil {
		t.Fatal("expected QAVerification to be non-nil")
	}
	if p.QAVerification.Status != "pending" {
		t.Errorf("expected QAVerification.Status = %q, got %q", "pending", p.QAVerification.Status)
	}
	if got := prd.QAVerificationStatus(p); got != "pending" {
		t.Errorf("expected QAVerificationStatus() = %q, got %q", "pending", got)
	}

	// Step 4: Verify IntegrationTests field does not exist on the struct.
	// We do this by checking the PRD struct using reflection — the field
	// should not be present.
	prdType := reflect.TypeFor[prd.PRD]()
	for i := 0; i < prdType.NumField(); i++ {
		if prdType.Field(i).Name == "IntegrationTests" {
			t.Error("IntegrationTests field should not exist on the PRD struct")
		}
	}

	// Additional: verify that a PRD without qaVerification defaults to "pending"
	jsonDataNoQA := `{
  "project": "old-project",
  "branchName": "ralph/old",
  "description": "Legacy PRD",
  "userStories": [{"id": "US-001", "title": "Story", "passes": true}]
}`
	pathNoQA := filepath.Join(dir, "prd-noqa.json")
	if err := os.WriteFile(pathNoQA, []byte(jsonDataNoQA), 0644); err != nil {
		t.Fatal(err)
	}

	pNoQA, err := prd.Read(pathNoQA)
	if err != nil {
		t.Fatalf("prd.Read (no QA) failed: %v", err)
	}
	if pNoQA.QAVerification != nil {
		t.Errorf("expected nil QAVerification for legacy PRD, got %+v", pNoQA.QAVerification)
	}
	if got := prd.QAVerificationStatus(pNoQA); got != "pending" {
		t.Errorf("expected QAVerificationStatus() = %q for nil QA, got %q", "pending", got)
	}

	// Verify round-trip: write and re-read preserves the QA verification
	writePath := filepath.Join(dir, "prd-roundtrip.json")
	if err := prd.Write(writePath, p); err != nil {
		t.Fatalf("prd.Write failed: %v", err)
	}
	reloaded, err := prd.Read(writePath)
	if err != nil {
		t.Fatalf("prd.Read (roundtrip) failed: %v", err)
	}
	if reloaded.QAVerification == nil || reloaded.QAVerification.Status != "pending" {
		t.Errorf("expected roundtrip to preserve QA status 'pending', got %+v", reloaded.QAVerification)
	}

	// Verify the written JSON does NOT contain integrationTests
	rawBytes, _ := os.ReadFile(writePath)
	if strings.Contains(string(rawBytes), "integrationTests") {
		t.Error("written PRD should not contain 'integrationTests' key")
	}
}

// --------------------------------------------------------------------------
// IT-006: AutoRalph state transitions: building → qa → in_review
// --------------------------------------------------------------------------

func TestIT006_StateMachineTransitions_BuildingToQAToInReview(t *testing.T) {
	database := openTestDB(t)

	// Create the state machine with QA transitions
	sm := orchestrator.New(database)
	qaVerifyPassed := false
	prCreated := false

	// Transition building → qa (unconditional)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateBuilding,
		To:   orchestrator.StateQA,
	})

	// Transition qa → in_review (when QA passes — includes PR creation)
	sm.Register(orchestrator.Transition{
		From:      orchestrator.StateQA,
		To:        orchestrator.StateInReview,
		Condition: func(i db.Issue) bool { return qaVerifyPassed },
		Action: func(i db.Issue, d *db.DB) error {
			prCreated = true
			return nil
		},
	})

	// Transition qa → qa_fix (when QA fails)
	sm.Register(orchestrator.Transition{
		From:      orchestrator.StateQA,
		To:        orchestrator.StateQAFix,
		Condition: func(i db.Issue) bool { return !qaVerifyPassed },
	})

	// Step 1: Create an issue in building state
	issue := createIssue(t, database, "building")

	// Step 2: Simulate successful loop completion — building → qa
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from building")
	}
	if tr.To != orchestrator.StateQA {
		t.Fatalf("expected transition to qa, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing building → qa: %v", err)
	}

	// Step 3: Verify issue transitions to qa state (not directly to in_review)
	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "qa" {
		t.Fatalf("expected state qa, got %s", issue.State)
	}

	// Step 4: Simulate successful QA verification
	qaVerifyPassed = true
	tr, ok = sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from qa")
	}
	if tr.To != orchestrator.StateInReview {
		t.Fatalf("expected transition to in_review, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing qa → in_review: %v", err)
	}

	// Step 5: Verify issue transitions to in_review
	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Fatalf("expected state in_review, got %s", issue.State)
	}

	// Step 6: Verify PR was created during the qa → in_review transition
	if !prCreated {
		t.Error("expected PR to be created during qa → in_review transition")
	}

	// Verify activity log recorded all transitions
	entries, _ := database.ListActivity(issue.ID, 100, 0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 activity entries (building→qa, qa→in_review), got %d", len(entries))
	}
	// ListActivity returns newest-first (DESC order)
	if entries[0].FromState != "qa" || entries[0].ToState != "in_review" {
		t.Errorf("first entry (newest): expected qa→in_review, got %s→%s", entries[0].FromState, entries[0].ToState)
	}
	if entries[1].FromState != "building" || entries[1].ToState != "qa" {
		t.Errorf("second entry (oldest): expected building→qa, got %s→%s", entries[1].FromState, entries[1].ToState)
	}
}

// --------------------------------------------------------------------------
// IT-007: AutoRalph state transitions: qa → qa_fix → qa re-verify
// --------------------------------------------------------------------------

func TestIT007_StateMachineTransitions_QAFixLoopAndReVerify(t *testing.T) {
	database := openTestDB(t)

	sm := orchestrator.New(database)
	qaVerifyPassed := false

	sm.Register(orchestrator.Transition{
		From:      orchestrator.StateQA,
		To:        orchestrator.StateInReview,
		Condition: func(i db.Issue) bool { return qaVerifyPassed },
	})
	sm.Register(orchestrator.Transition{
		From:      orchestrator.StateQA,
		To:        orchestrator.StateQAFix,
		Condition: func(i db.Issue) bool { return !qaVerifyPassed },
	})
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQAFix,
		To:   orchestrator.StateQA,
	})

	// Step 1: Create an issue in qa state
	issue := createIssue(t, database, "qa")

	// Step 2: Simulate QA verification failure — qa → qa_fix
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from qa")
	}
	if tr.To != orchestrator.StateQAFix {
		t.Fatalf("expected transition to qa_fix, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing qa → qa_fix: %v", err)
	}

	// Step 3: Verify issue transitions to qa_fix
	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "qa_fix" {
		t.Fatalf("expected state qa_fix, got %s", issue.State)
	}

	// Step 4: Simulate fix action completion — qa_fix → qa
	tr, ok = sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from qa_fix")
	}
	if tr.To != orchestrator.StateQA {
		t.Fatalf("expected transition to qa, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing qa_fix → qa: %v", err)
	}

	// Step 5: Verify issue transitions back to qa
	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "qa" {
		t.Fatalf("expected state qa, got %s", issue.State)
	}

	// Step 6: Simulate QA verification success — qa → in_review
	qaVerifyPassed = true
	tr, ok = sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from qa")
	}
	if tr.To != orchestrator.StateInReview {
		t.Fatalf("expected transition to in_review, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing qa → in_review: %v", err)
	}

	// Step 7: Verify issue transitions to in_review
	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Fatalf("expected state in_review, got %s", issue.State)
	}

	// Verify full activity log (ListActivity returns newest-first)
	entries, _ := database.ListActivity(issue.ID, 100, 0)
	expectedTransitions := [][2]string{
		{"qa", "in_review"},
		{"qa_fix", "qa"},
		{"qa", "qa_fix"},
	}
	if len(entries) != len(expectedTransitions) {
		t.Fatalf("expected %d activity entries, got %d", len(expectedTransitions), len(entries))
	}
	for i, expected := range expectedTransitions {
		if entries[i].FromState != expected[0] || entries[i].ToState != expected[1] {
			t.Errorf("entry %d: expected %s→%s, got %s→%s", i, expected[0], expected[1], entries[i].FromState, entries[i].ToState)
		}
	}
}

// --------------------------------------------------------------------------
// IT-008: AutoRalph QA pauses after max attempts
// --------------------------------------------------------------------------

func TestIT008_StateMachine_QAPausesAfterMaxAttempts(t *testing.T) {
	database := openTestDB(t)

	sm := orchestrator.New(database)
	maxQAAttempts := 3

	// qa → paused: when attempts exceed max
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQA,
		To:   orchestrator.StatePaused,
		Condition: func(i db.Issue) bool {
			return i.QAFixAttempts >= maxQAAttempts
		},
		Action: func(i db.Issue, d *db.DB) error {
			return d.LogActivity(i.ID, "qa_paused", "qa", "paused",
				fmt.Sprintf("QA paused after %d fix attempts", i.QAFixAttempts))
		},
	})
	// qa → qa_fix: normal QA failure
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQA,
		To:   orchestrator.StateQAFix,
		Condition: func(i db.Issue) bool {
			return i.QAFixAttempts < maxQAAttempts
		},
	})

	// Step 1: Create an issue in qa state with QAFixAttempts at max
	issue := createIssue(t, database, "qa")
	issue.QAFixAttempts = maxQAAttempts
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("updating issue: %v", err)
	}

	// Step 2: Trigger QA verification that fails — should match qa → paused
	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected transition from qa")
	}

	// Step 3: Verify issue transitions to paused (not qa_fix)
	if tr.To != orchestrator.StatePaused {
		t.Fatalf("expected transition to paused, got %s", tr.To)
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing qa → paused: %v", err)
	}

	issue, _ = database.GetIssue(issue.ID)
	if issue.State != "paused" {
		t.Fatalf("expected state paused, got %s", issue.State)
	}

	// Step 4: Verify activity log records qa_paused event
	entries, _ := database.ListActivity(issue.ID, 100, 0)
	foundQAPaused := false
	for _, e := range entries {
		if e.EventType == "qa_paused" {
			foundQAPaused = true
			if !strings.Contains(e.Detail, "3 fix attempts") {
				t.Errorf("expected qa_paused detail to mention attempts, got %q", e.Detail)
			}
		}
	}
	if !foundQAPaused {
		t.Error("expected qa_paused activity entry")
	}
}

// --------------------------------------------------------------------------
// IT-009: QA state recovery on startup
// --------------------------------------------------------------------------

func TestIT009_QAStateRecoveryOnStartup(t *testing.T) {
	database := openTestDB(t)
	project := createProject(t, database)

	// Step 1: Create issues in qa and qa_fix states
	qaIssue, err := database.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-qa-1",
		Identifier:    "PROJ-10",
		Title:         "QA issue",
		State:         "qa",
		WorkspaceName: "proj-10",
		BranchName:    "autoralph/proj-10",
	})
	if err != nil {
		t.Fatalf("creating qa issue: %v", err)
	}

	qaFixIssue, err := database.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-qafix-1",
		Identifier:    "PROJ-11",
		Title:         "QA fix issue",
		State:         "qa_fix",
		WorkspaceName: "proj-11",
		BranchName:    "autoralph/proj-11",
	})
	if err != nil {
		t.Fatalf("creating qa_fix issue: %v", err)
	}

	// Create a building issue (should NOT be counted by RecoverQA)
	_, err = database.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-building-1",
		Identifier:    "PROJ-12",
		Title:         "Building issue",
		State:         "building",
		WorkspaceName: "proj-12",
		BranchName:    "autoralph/proj-12",
	})
	if err != nil {
		t.Fatalf("creating building issue: %v", err)
	}

	// Step 2: Query for issues in qa and qa_fix states (simulating RecoverQA)
	issues, err := database.ListIssues(db.IssueFilter{States: []string{"qa", "qa_fix"}})
	if err != nil {
		t.Fatalf("listing qa/qa_fix issues: %v", err)
	}

	// Step 3: Verify both issues are found
	if len(issues) != 2 {
		t.Fatalf("expected 2 qa/qa_fix issues, got %d", len(issues))
	}

	// Step 4: Verify the correct issues were recovered
	issueIDs := map[string]bool{}
	for _, iss := range issues {
		issueIDs[iss.ID] = true
	}
	if !issueIDs[qaIssue.ID] {
		t.Errorf("expected qa issue %s to be recovered", qaIssue.ID)
	}
	if !issueIDs[qaFixIssue.ID] {
		t.Errorf("expected qa_fix issue %s to be recovered", qaFixIssue.ID)
	}

	// Verify that building issues are NOT included
	for _, iss := range issues {
		if iss.State != "qa" && iss.State != "qa_fix" {
			t.Errorf("unexpected state %q in recovered issues", iss.State)
		}
	}
}

// --------------------------------------------------------------------------
// IT-010: Web UI displays QA states with correct colors
// --------------------------------------------------------------------------

func TestIT010_StateBadgeQAColors(t *testing.T) {
	// This test verifies the StateBadge component code directly by parsing
	// the source file and checking that qa/qa_fix are in STATE_COLORS.

	// Find the StateBadge.tsx source file relative to the module root.
	badgePath := findProjectFile(t, "web/src/components/StateBadge.tsx")
	content, err := os.ReadFile(badgePath)
	if err != nil {
		t.Fatalf("reading StateBadge.tsx: %v", err)
	}

	src := string(content)

	// Verify STATE_COLORS contains "qa" and "qa_fix" entries
	if !strings.Contains(src, "qa:") {
		t.Error("STATE_COLORS does not contain 'qa' entry")
	}
	if !strings.Contains(src, "qa_fix:") {
		t.Error("STATE_COLORS does not contain 'qa_fix' entry")
	}

	// Verify they have hex color values
	if !strings.Contains(src, "qa: '#") {
		t.Error("qa state does not have a color value")
	}
	if !strings.Contains(src, "qa_fix: '#") {
		t.Error("qa_fix state does not have a color value")
	}

	// Verify the component renders the state text (replaces underscores with spaces)
	if !strings.Contains(src, "state.replace(/_/g, ' ')") {
		t.Error("StateBadge should replace underscores with spaces in state display")
	}

	// Verify both qa and qa_fix have distinct colors
	qaIdx := strings.Index(src, "qa: '#")
	qaFixIdx := strings.Index(src, "qa_fix: '#")
	if qaIdx == -1 || qaFixIdx == -1 {
		t.Fatal("could not find color entries for qa/qa_fix")
	}

	// Extract colors
	qaColor := extractHexColor(src[qaIdx:])
	qaFixColor := extractHexColor(src[qaFixIdx:])
	if qaColor == qaFixColor {
		t.Errorf("qa and qa_fix should have different colors, both got %s", qaColor)
	}
}

// --------------------------------------------------------------------------
// IT-011: QA events visible in TUI and plain text handlers
// --------------------------------------------------------------------------

func TestIT011_QAEventsInPlainTextHandler(t *testing.T) {
	var buf bytes.Buffer
	h := &events.PlainTextHandler{W: &buf}

	// Step 1: Emit QAVerifyStarted
	h.Handle(events.QAVerifyStarted{})
	output := buf.String()
	if !strings.Contains(output, "all stories pass — running QA verification") {
		t.Errorf("expected QAVerifyStarted message, got %q", output)
	}

	// Step 2: Emit QAFixStarted
	buf.Reset()
	h.Handle(events.QAFixStarted{})
	output = buf.String()
	if !strings.Contains(output, "QA checks failed — running QA fix") {
		t.Errorf("expected QAFixStarted message, got %q", output)
	}

	// Step 3: Emit QAComplete{Passed: true}
	buf.Reset()
	h.Handle(events.QAComplete{Passed: true})
	output = buf.String()
	if !strings.Contains(output, "QA complete — all checks passed") {
		t.Errorf("expected QAComplete passed message, got %q", output)
	}

	// Step 4: Emit QAComplete{Passed: false}
	buf.Reset()
	h.Handle(events.QAComplete{Passed: false})
	output = buf.String()
	if !strings.Contains(output, "QA complete — checks failed") {
		t.Errorf("expected QAComplete failed message, got %q", output)
	}
}

func TestIT011_QAEventsImplementEventInterface(t *testing.T) {
	// Verify all QA event types implement the Event interface
	var _ events.Event = events.QAVerifyStarted{}
	var _ events.Event = events.QAFixStarted{}
	var _ events.Event = events.QAComplete{}
	var _ events.Event = events.QAComplete{Passed: true}
	var _ events.Event = events.QAComplete{Passed: false}
}

func TestIT011_QAEventsJSONSerialization(t *testing.T) {
	// Verify QAComplete serializes correctly
	qa := events.QAComplete{Passed: true}
	data, err := json.Marshal(qa)
	if err != nil {
		t.Fatalf("marshaling QAComplete: %v", err)
	}
	if !strings.Contains(string(data), `"passed":true`) {
		t.Errorf("expected JSON to contain passed:true, got %s", data)
	}

	qa2 := events.QAComplete{Passed: false}
	data2, _ := json.Marshal(qa2)
	if !strings.Contains(string(data2), `"passed":false`) {
		t.Errorf("expected JSON to contain passed:false, got %s", data2)
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

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

func createProject(t *testing.T, d *db.DB) db.Project {
	t.Helper()
	p, err := d.CreateProject(db.Project{
		Name:      "test-project",
		LocalPath: "/tmp/test",
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

func createIssue(t *testing.T, d *db.DB, state string) db.Issue {
	t.Helper()
	p := createProject(t, d)
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Identifier:    "PROJ-42",
		Title:         "Test issue",
		State:         state,
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// findProjectFile locates a file relative to the project root by walking up from
// the current directory.
func findProjectFile(t *testing.T, relPath string) string {
	t.Helper()
	// Start from the current file's directory and walk up
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, relPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find %s in project tree", relPath)
	return ""
}

// extractHexColor pulls the first hex color value (#xxxxxx) from a string.
func extractHexColor(s string) string {
	idx := strings.Index(s, "'#")
	if idx == -1 {
		return ""
	}
	end := strings.Index(s[idx+1:], "'")
	if end == -1 {
		return ""
	}
	return s[idx+1 : idx+1+end]
}

