package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
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
	p, err := d.CreateProject(db.Project{
		Name:             "test-project",
		LocalPath:        "/tmp/test",
		LinearTeamID:     "team-abc",
		LinearAssigneeID: "user-xyz",
		RalphConfigPath:  ".ralph/ralph.yaml",
		BranchPrefix:     "autoralph/",
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         state,
		PlanText:      "## Plan\n\n1. Add avatar upload endpoint\n2. Display avatars in UI",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// --- Mocks ---

type mockInvoker struct {
	lastPrompt string
	response   string
	err        error
}

func (m *mockInvoker) Invoke(ctx context.Context, prompt, dir string) (string, error) {
	m.lastPrompt = prompt
	return m.response, m.err
}

type mockWorkspaceCreator struct {
	calls []wsCreateCall
	err   error
}

type wsCreateCall struct {
	repoPath     string
	ws           workspace.Workspace
	base         string
	copyPatterns []string
}

func (m *mockWorkspaceCreator) Create(ctx context.Context, repoPath string, ws workspace.Workspace, base string, copyPatterns []string) error {
	m.calls = append(m.calls, wsCreateCall{repoPath: repoPath, ws: ws, base: base, copyPatterns: copyPatterns})
	return m.err
}

type mockConfigLoader struct {
	cfg *config.Config
	err error
}

func (m *mockConfigLoader) Load(path string) (*config.Config, error) {
	return m.cfg, m.err
}

type mockLinearState struct {
	states      []WorkflowState
	fetchErr    error
	updateCalls []linearUpdateCall
	updateErr   error
}

type linearUpdateCall struct {
	issueID string
	stateID string
}

func (m *mockLinearState) FetchWorkflowStates(ctx context.Context, teamID string) ([]WorkflowState, error) {
	return m.states, m.fetchErr
}

func (m *mockLinearState) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	m.updateCalls = append(m.updateCalls, linearUpdateCall{issueID: issueID, stateID: stateID})
	return m.updateErr
}

// mockPRDReader returns a fixed PRD or error.
type mockPRDReader struct {
	prd *prd.PRD
	err error
}

func (m *mockPRDReader) Read(path string) (*prd.PRD, error) {
	return m.prd, m.err
}

func validPRD() *prd.PRD {
	return &prd.PRD{
		Project:     "test-project",
		BranchName:  "autoralph/proj-42",
		Description: "Add user avatar support",
		UserStories: []prd.Story{
			{
				ID:                 "US-001",
				Title:              "Avatar upload endpoint",
				Description:        "As a user, I want to upload an avatar",
				AcceptanceCriteria: []string{"Upload works", "Tests pass"},
				Priority:           1,
			},
		},
	}
}

// writingInvoker simulates Claude writing the PRD to disk during invocation.
type writingInvoker struct {
	lastPrompt string
	prd        *prd.PRD
	err        error
}

func (m *writingInvoker) Invoke(ctx context.Context, prompt, dir string) (string, error) {
	m.lastPrompt = prompt
	if m.err != nil {
		return "", m.err
	}
	// Extract the PRD path from the prompt (it's between ``` fences).
	lines := strings.Split(prompt, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "```" && i > 0 {
			candidate := strings.TrimSpace(lines[i-1])
			if strings.HasSuffix(candidate, "prd.json") {
				// Write PRD to this path (simulate what Claude does).
				if m.prd != nil {
					data, _ := json.MarshalIndent(m.prd, "", "  ")
					os.MkdirAll(filepath.Dir(candidate), 0o755)
					os.WriteFile(candidate, data, 0o644)
				}
				break
			}
		}
	}
	return "PRD written", nil
}

func defaultConfig() Config {
	return Config{
		Invoker:   &mockInvoker{response: "PRD written"},
		Workspace: &mockWorkspaceCreator{},
		ConfigLoad: &mockConfigLoader{cfg: &config.Config{
			Project: "test-project",
			Repo: config.RepoConfig{
				Path:        "/tmp/test",
				DefaultBase: "main",
			},
		}},
		Linear: &mockLinearState{states: []WorkflowState{
			{ID: "state-1", Name: "Todo", Type: "unstarted"},
			{ID: "state-2", Name: "In Progress", Type: "started"},
			{ID: "state-3", Name: "Done", Type: "completed"},
		}},
		PRDRead:  &mockPRDReader{prd: validPRD()},
		Projects: nil, // set per test since it needs the real DB
	}
}

// --- Tests ---

func TestBuildAction_CreatesWorkspace(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	wsc := cfg.Workspace.(*mockWorkspaceCreator)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(wsc.calls) != 1 {
		t.Fatalf("expected 1 workspace creation call, got %d", len(wsc.calls))
	}
	call := wsc.calls[0]
	if call.ws.Name != "proj-42" {
		t.Errorf("expected workspace name %q, got %q", "proj-42", call.ws.Name)
	}
	if call.ws.Branch != "autoralph/proj-42" {
		t.Errorf("expected branch %q, got %q", "autoralph/proj-42", call.ws.Branch)
	}
	if call.base != "main" {
		t.Errorf("expected base %q, got %q", "main", call.base)
	}
}

func TestBuildAction_InvokesAIWithPRDPrompt(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	invoker := cfg.Invoker.(*mockInvoker)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called with a prompt")
	}
	if !strings.Contains(invoker.lastPrompt, issue.PlanText) {
		t.Error("expected prompt to contain plan text")
	}
	if !strings.Contains(invoker.lastPrompt, "test-project") {
		t.Error("expected prompt to contain project name")
	}
	if !strings.Contains(invoker.lastPrompt, "prd.json") {
		t.Error("expected prompt to contain PRD path")
	}
	if !strings.Contains(invoker.lastPrompt, "autoralph/proj-42") {
		t.Error("expected prompt to contain branch name")
	}
}

func TestBuildAction_ReadsPRDAfterAIWrites(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the action read the PRD (used it for activity log).
	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 activity entry, got %d", len(entries))
	}
	// The "workspace_created" entry (most recent, i.e. first in the list) should mention story count.
	var found bool
	for _, e := range entries {
		if strings.Contains(e.Detail, "1 stories") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected activity to mention story count")
	}
}

func TestBuildAction_StoresWorkspaceInfoInDB(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.WorkspaceName != "proj-42" {
		t.Errorf("expected workspace_name %q, got %q", "proj-42", updated.WorkspaceName)
	}
	if updated.BranchName != "autoralph/proj-42" {
		t.Errorf("expected branch_name %q, got %q", "autoralph/proj-42", updated.BranchName)
	}
}

func TestBuildAction_UpdatesLinearState(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	linear := cfg.Linear.(*mockLinearState)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(linear.updateCalls) != 1 {
		t.Fatalf("expected 1 Linear state update, got %d", len(linear.updateCalls))
	}
	call := linear.updateCalls[0]
	if call.issueID != "lin-123" {
		t.Errorf("expected Linear issue ID %q, got %q", "lin-123", call.issueID)
	}
	if call.stateID != "state-2" {
		t.Errorf("expected state ID %q (In Progress), got %q", "state-2", call.stateID)
	}
}

func TestBuildAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 activity entries (Creating PRD + workspace_created), got %d", len(entries))
	}
	// Find the workspace_created entry.
	var wsEntry *db.ActivityEntry
	var prdEntry *db.ActivityEntry
	for i := range entries {
		switch entries[i].EventType {
		case "workspace_created":
			wsEntry = &entries[i]
		case "build_event":
			if strings.Contains(entries[i].Detail, "Creating PRD") {
				prdEntry = &entries[i]
			}
		}
	}
	if prdEntry == nil {
		t.Error("expected 'Creating PRD...' build_event activity entry")
	}
	if wsEntry == nil {
		t.Fatal("expected workspace_created activity entry")
	}
	if !strings.Contains(wsEntry.Detail, "proj-42") {
		t.Error("expected activity detail to contain workspace name")
	}
	if !strings.Contains(wsEntry.Detail, "autoralph/proj-42") {
		t.Error("expected activity detail to contain branch name")
	}
}

func TestBuildAction_ConfigLoadError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	cfg.ConfigLoad = &mockConfigLoader{err: fmt.Errorf("config file not found")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when config loading fails")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Errorf("expected error to contain config failure message, got: %v", err)
	}
}

func TestBuildAction_WorkspaceCreateError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	cfg.Workspace = &mockWorkspaceCreator{err: fmt.Errorf("git worktree failed")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when workspace creation fails")
	}
	if !strings.Contains(err.Error(), "git worktree failed") {
		t.Errorf("expected error to contain workspace failure message, got: %v", err)
	}
}

func TestBuildAction_AIError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	cfg.Invoker = &mockInvoker{err: fmt.Errorf("AI service unavailable")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when AI invocation fails")
	}
	if !strings.Contains(err.Error(), "AI service unavailable") {
		t.Errorf("expected error to contain AI failure message, got: %v", err)
	}
}

func TestBuildAction_PRDReadError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	cfg.PRDRead = &mockPRDReader{err: fmt.Errorf("PRD file not found")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when PRD read fails")
	}
	if !strings.Contains(err.Error(), "PRD file not found") {
		t.Errorf("expected error to contain read failure message, got: %v", err)
	}
}

func TestBuildAction_LinearStateNotFound_IsNonFatal(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	cfg.Linear = &mockLinearState{
		states: []WorkflowState{
			{ID: "state-1", Name: "Todo", Type: "unstarted"},
			// "In Progress" is missing — but this is non-fatal
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error (Linear state update is non-fatal), got: %v", err)
	}

	// Verify a warning was logged in the activity log.
	activities, _ := d.ListActivity(issue.ID, 10, 0)
	var foundWarning bool
	for _, a := range activities {
		if a.EventType == "warning" && strings.Contains(a.Detail, "In Progress") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning activity for missing Linear state")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"PROJ-42", "proj-42"},
		{"proj-42", "proj-42"},
		{"ABC-123", "abc-123"},
		{"My Issue", "my-issue"},
		{"test_name.1", "test_name.1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildAction_IncludesKnowledgePath(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "approved")

	cfg := defaultConfig()
	cfg.Projects = d
	invoker := cfg.Invoker.(*mockInvoker)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The knowledge path should be computed from project.LocalPath (/tmp/test)
	if !strings.Contains(invoker.lastPrompt, ".ralph/knowledge") {
		t.Error("expected prompt to contain knowledge path")
	}
}

// Branch pattern validation is intentionally skipped by autoralph —
// autoralph uses its own branch prefix which may not match the ralph config pattern.
