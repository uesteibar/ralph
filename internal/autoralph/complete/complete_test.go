package complete

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
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

func testProject(t *testing.T, d *db.DB) db.Project {
	t.Helper()
	p, err := d.CreateProject(db.Project{
		Name:         "test-project",
		LocalPath:    t.TempDir(),
		LinearTeamID: "team-123",
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

func testIssue(t *testing.T, d *db.DB, proj db.Project) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-123",
		Identifier:    "TEST-1",
		Title:         "Test issue",
		State:         string(orchestrator.StateInReview),
		PRNumber:      42,
		PRURL:         "https://github.com/owner/repo/pull/42",
		WorkspaceName: "test-1",
		BranchName:    "autoralph/test-1",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

type mockWorkspaceRemover struct {
	removeCalls int
	lastRepo    string
	lastName    string
	err         error
}

func (m *mockWorkspaceRemover) RemoveWorkspace(_ context.Context, repoPath, name string) error {
	m.removeCalls++
	m.lastRepo = repoPath
	m.lastName = name
	return m.err
}

type mockLinear struct {
	fetchStatesCalls int
	updateStateCalls int
	lastIssueID      string
	lastStateID      string
	states           []WorkflowState
	fetchErr         error
	updateErr        error
}

func (m *mockLinear) FetchWorkflowStates(_ context.Context, teamID string) ([]WorkflowState, error) {
	m.fetchStatesCalls++
	return m.states, m.fetchErr
}

func (m *mockLinear) UpdateIssueState(_ context.Context, issueID, stateID string) error {
	m.updateStateCalls++
	m.lastIssueID = issueID
	m.lastStateID = stateID
	return m.updateErr
}

func TestComplete_UpdatesIssueStateToCompleted(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestComplete_DeletesWorkspace(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.removeCalls != 1 {
		t.Errorf("expected 1 remove call, got %d", ws.removeCalls)
	}
	if ws.lastRepo != proj.LocalPath {
		t.Errorf("expected repo path %q, got %q", proj.LocalPath, ws.lastRepo)
	}
	if ws.lastName != "test-1" {
		t.Errorf("expected workspace name %q, got %q", "test-1", ws.lastName)
	}
}

func TestComplete_UpdatesLinearStateToDone(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if linear.updateStateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", linear.updateStateCalls)
	}
	if linear.lastIssueID != "lin-123" {
		t.Errorf("expected issue ID %q, got %q", "lin-123", linear.lastIssueID)
	}
	if linear.lastStateID != "done-id" {
		t.Errorf("expected state ID %q, got %q", "done-id", linear.lastStateID)
	}
}

func TestComplete_LogsActivity(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.EventType == "issue_completed" && e.Detail == "Issue completed. PR #42 merged." {
			found = true
			if e.FromState != string(orchestrator.StateInReview) {
				t.Errorf("expected from_state %q, got %q", orchestrator.StateInReview, e.FromState)
			}
			if e.ToState != string(orchestrator.StateCompleted) {
				t.Errorf("expected to_state %q, got %q", orchestrator.StateCompleted, e.ToState)
			}
		}
	}
	if !found {
		t.Error("expected activity entry with event_type 'issue_completed' and correct detail")
	}
}

func TestComplete_WorkspaceRemoveError_NonFatal(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{err: fmt.Errorf("worktree not found")}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	// Workspace removal error should not prevent completion
	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestComplete_LinearUpdateError_NonFatal(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states:   []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
		updateErr: fmt.Errorf("network error"),
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	// Linear update error should not prevent completion
	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestComplete_LinearFetchStatesError_NonFatal(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		fetchErr: fmt.Errorf("connection refused"),
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

type mockProjectGetter struct {
	project db.Project
	err     error
}

func (m *mockProjectGetter) GetProject(id string) (db.Project, error) {
	return m.project, m.err
}

func TestComplete_ProjectLoadError_ReturnsFatal(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj)

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  &mockProjectGetter{err: fmt.Errorf("project not found")},
	})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestComplete_NoWorkspaceName_SkipsRemoval(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Issue without workspace name
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-nows",
		Identifier:    "TEST-NWS",
		Title:         "No workspace",
		State:         string(orchestrator.StateInReview),
		PRNumber:      10,
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	ws := &mockWorkspaceRemover{}
	linear := &mockLinear{
		states: []WorkflowState{{ID: "done-id", Name: "Done", Type: "completed"}},
	}

	action := NewAction(Config{
		Workspace: ws,
		Linear:    linear,
		Projects:  d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.removeCalls != 0 {
		t.Errorf("expected 0 remove calls when no workspace name, got %d", ws.removeCalls)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}
