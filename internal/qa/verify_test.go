package qa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

// --- Test helpers ---

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
		LocalPath:        t.TempDir(),
		GithubOwner:      "owner",
		GithubRepo:       "repo",
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

func createTestIssue(t *testing.T, d *db.DB, project db.Project) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         "qa",
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// setupWorkspace creates the workspace directories and a PRD file needed by the action.
func setupWorkspace(t *testing.T, project db.Project) string {
	t.Helper()
	wsDir := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42")
	treePath := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treePath, 0755); err != nil {
		t.Fatalf("creating workspace tree: %v", err)
	}

	// Create a minimal PRD.
	p := &prd.PRD{
		Project:    "test-project",
		BranchName: "autoralph/proj-42",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Test story", Passes: true},
		},
		QAVerification: &prd.QAVerification{Status: "pending", Attempts: 0},
	}
	prdPath := filepath.Join(wsDir, "prd.json")
	if err := prd.Write(prdPath, p); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	return treePath
}

// --- Mocks ---

type mockInvoker struct {
	lastPrompt   string
	lastDir      string
	lastMaxTurns int
	lastHandler  events.EventHandler
	response     string
	err          error
}

func (m *mockInvoker) InvokeWithEvents(_ context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	m.lastPrompt = prompt
	m.lastDir = dir
	m.lastMaxTurns = maxTurns
	m.lastHandler = handler
	return m.response, m.err
}

type mockProjectGetter struct {
	project db.Project
	err     error
}

func (m *mockProjectGetter) GetProject(_ string) (db.Project, error) {
	return m.project, m.err
}

type mockConfigLoader struct {
	cfg *config.Config
	err error
}

func (m *mockConfigLoader) Load(_ string) (*config.Config, error) {
	return m.cfg, m.err
}

type mockBranchPuller struct {
	calls []pullBranchCall
	err   error
}

type pullBranchCall struct {
	workDir string
	branch  string
}

func (m *mockBranchPuller) PullBranch(_ context.Context, workDir, branch string) error {
	m.calls = append(m.calls, pullBranchCall{workDir: workDir, branch: branch})
	return m.err
}

type mockGitOps struct {
	commitCalls []commitCall
	pushCalls   []pushCall
	commitErr   error
	pushErr     error
}

type commitCall struct {
	workDir string
	message string
}

type pushCall struct {
	workDir string
	branch  string
}

func (m *mockGitOps) Commit(_ context.Context, workDir, message string) error {
	m.commitCalls = append(m.commitCalls, commitCall{workDir: workDir, message: message})
	return m.commitErr
}

func (m *mockGitOps) PushBranch(_ context.Context, workDir, branch string) error {
	m.pushCalls = append(m.pushCalls, pushCall{workDir: workDir, branch: branch})
	return m.pushErr
}

type mockCommandRunner struct {
	calls   []runCall
	results map[string]error // command -> error
}

type runCall struct {
	dir     string
	command string
}

func (m *mockCommandRunner) Run(_ context.Context, dir, command string) error {
	m.calls = append(m.calls, runCall{dir: dir, command: command})
	if m.results != nil {
		if err, ok := m.results[command]; ok {
			return err
		}
	}
	return nil
}

type mockEventHandler struct {
	handleFn func(e events.Event)
}

func (m *mockEventHandler) Handle(e events.Event) {
	if m.handleFn != nil {
		m.handleFn(e)
	}
}

func defaultMocks(project db.Project) (Config, *mockInvoker, *mockCommandRunner) {
	inv := &mockInvoker{response: "QA verification complete"}
	runner := &mockCommandRunner{}

	cfg := Config{
		Invoker:      inv,
		Projects:     &mockProjectGetter{project: project},
		ConfigLoad:   &mockConfigLoader{cfg: &config.Config{Project: "test", Repo: config.RepoConfig{DefaultBase: "main"}, QualityChecks: []string{"just test", "just vet"}}},
		BranchPuller: &mockBranchPuller{},
		Git:          &mockGitOps{},
		Runner:       runner,
		MaxAttempts:  3,
	}
	return cfg, inv, runner
}

// --- Tests ---

func TestNewVerifyAction_InvokesAIWithCorrectPromptAndDir(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}

	expectedDir := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "tree")
	if inv.lastDir != expectedDir {
		t.Errorf("expected AI dir %q, got %q", expectedDir, inv.lastDir)
	}

	if inv.lastMaxTurns != maxTurnsVerify {
		t.Errorf("expected maxTurns %d, got %d", maxTurnsVerify, inv.lastMaxTurns)
	}
}

func TestNewVerifyAction_RunsQualityChecksAfterAI(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, runner := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 command runner calls, got %d", len(runner.calls))
	}
	if runner.calls[0].command != "ralph check just test" {
		t.Errorf("expected first check 'ralph check just test', got %q", runner.calls[0].command)
	}
	if runner.calls[1].command != "ralph check just vet" {
		t.Errorf("expected second check 'ralph check just vet', got %q", runner.calls[1].command)
	}

	expectedDir := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "tree")
	for _, call := range runner.calls {
		if call.dir != expectedDir {
			t.Errorf("expected check dir %q, got %q", expectedDir, call.dir)
		}
	}
}

func TestNewVerifyAction_AllChecksPassed_UpdatesPRDToPassed(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back PRD and verify status.
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, err := prd.Read(prdPath)
	if err != nil {
		t.Fatalf("reading PRD: %v", err)
	}
	if p.QAVerification == nil {
		t.Fatal("expected QAVerification to be set")
	}
	if p.QAVerification.Status != "passed" {
		t.Errorf("expected status 'passed', got %q", p.QAVerification.Status)
	}
}

func TestNewVerifyAction_ChecksFail_UpdatesPRDToFailed(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, runner := defaultMocks(project)

	runner.results = map[string]error{
		"ralph check just test": fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when checks fail")
	}
	if !strings.Contains(err.Error(), "qa verification failed") {
		t.Errorf("expected 'qa verification failed' in error, got: %s", err.Error())
	}

	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, err := prd.Read(prdPath)
	if err != nil {
		t.Fatalf("reading PRD: %v", err)
	}
	if p.QAVerification.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", p.QAVerification.Status)
	}
	if p.QAVerification.Attempts != 1 {
		t.Errorf("expected Attempts=1, got %d", p.QAVerification.Attempts)
	}
}

func TestNewVerifyAction_ChecksFail_IncrementsAttempts(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)

	// Set PRD to already have 1 attempt.
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, _ := prd.Read(prdPath)
	p.QAVerification.Attempts = 1
	prd.Write(prdPath, p)

	cfg, _, runner := defaultMocks(project)
	runner.results = map[string]error{
		"ralph check just vet": fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}

	p, _ = prd.Read(prdPath)
	if p.QAVerification.Attempts != 2 {
		t.Errorf("expected Attempts=2, got %d", p.QAVerification.Attempts)
	}
}

func TestNewVerifyAction_MaxAttemptsReached_PausesIssue(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)

	// Set PRD to already have 2 attempts (max is 3, so next failure triggers pause).
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, _ := prd.Read(prdPath)
	p.QAVerification.Attempts = 2
	prd.Write(prdPath, p)

	cfg, _, runner := defaultMocks(project)
	runner.results = map[string]error{
		"ralph check just test": fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error on pause, got: %v", err)
	}

	// Issue should be paused.
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state 'paused', got %q", updated.State)
	}

	// Activity should contain qa_paused.
	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "qa_paused" {
			found = true
			if !strings.Contains(a.Detail, "3 attempts") {
				t.Errorf("expected detail to mention attempts, got: %s", a.Detail)
			}
		}
	}
	if !found {
		t.Error("expected qa_paused activity")
	}
}

func TestNewVerifyAction_MaxAttemptsReached_UpdatesPRDToFailed(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)

	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, _ := prd.Read(prdPath)
	p.QAVerification.Attempts = 2
	prd.Write(prdPath, p)

	cfg, _, runner := defaultMocks(project)
	runner.results = map[string]error{
		"ralph check just test": fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	action(issue, d)

	p, _ = prd.Read(prdPath)
	if p.QAVerification.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", p.QAVerification.Status)
	}
	if p.QAVerification.Attempts != 3 {
		t.Errorf("expected Attempts=3, got %d", p.QAVerification.Attempts)
	}
}

func TestNewVerifyAction_DefaultMaxAttempts(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)

	// Set attempts to 2, default max is 3.
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, _ := prd.Read(prdPath)
	p.QAVerification.Attempts = 2
	prd.Write(prdPath, p)

	cfg, _, runner := defaultMocks(project)
	cfg.MaxAttempts = 0 // should default to 3
	runner.results = map[string]error{
		"ralph check just test": fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error (pause), got: %v", err)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != "paused" {
		t.Errorf("expected state 'paused' with default max attempts, got %q", updated.State)
	}
}

func TestNewVerifyAction_NilQAVerification_InitializesIt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)

	// Write PRD without QAVerification.
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	data, _ := json.MarshalIndent(map[string]any{
		"project":     "test-project",
		"branchName":  "autoralph/proj-42",
		"userStories": []map[string]any{{
			"id": "US-001", "title": "Test", "passes": true, "priority": 1,
		}},
	}, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	cfg, _, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, _ := prd.Read(prdPath)
	if p.QAVerification == nil {
		t.Fatal("expected QAVerification to be initialized")
	}
	if p.QAVerification.Status != "passed" {
		t.Errorf("expected status 'passed', got %q", p.QAVerification.Status)
	}
}

func TestNewVerifyAction_ProjectNotFound_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _ := defaultMocks(project)
	cfg.Projects = &mockProjectGetter{err: errors.New("not found")}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading project") {
		t.Errorf("expected 'loading project' in error, got: %s", err.Error())
	}
}

func TestNewVerifyAction_ConfigLoadError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _ := defaultMocks(project)
	cfg.ConfigLoad = &mockConfigLoader{err: errors.New("config not found")}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading ralph config") {
		t.Errorf("expected 'loading ralph config' in error, got: %s", err.Error())
	}
}

func TestNewVerifyAction_PullBranchError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _ := defaultMocks(project)
	cfg.BranchPuller = &mockBranchPuller{err: errors.New("ff-only failed")}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pulling branch") {
		t.Errorf("expected 'pulling branch' in error, got: %s", err.Error())
	}
	// AI should NOT have been invoked.
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked when PullBranch fails")
	}
}

func TestNewVerifyAction_RemoteRefNotFound_ContinuesGracefully(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)
	cfg.BranchPuller = &mockBranchPuller{err: fmt.Errorf("pulling origin/autoralph/proj-42 (ff-only): git pull --ff-only origin autoralph/proj-42 exited with code 1: fatal: couldn't find remote ref autoralph/proj-42")}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error when remote ref not found, got: %v", err)
	}

	// AI should still be invoked despite the pull skip.
	if inv.lastPrompt == "" {
		t.Error("expected AI to be invoked even when remote ref not found")
	}
}

func TestNewVerifyAction_AIError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)
	inv.err = errors.New("AI timeout")

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invoking AI") {
		t.Errorf("expected 'invoking AI' in error, got: %s", err.Error())
	}
}

func TestNewVerifyAction_PullsBranchBeforeAI(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _ := defaultMocks(project)

	puller := &mockBranchPuller{}
	cfg.BranchPuller = puller

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(puller.calls) != 1 {
		t.Fatalf("expected 1 PullBranch call, got %d", len(puller.calls))
	}
	expectedTreePath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "tree")
	if puller.calls[0].workDir != expectedTreePath {
		t.Errorf("expected workDir %q, got %q", expectedTreePath, puller.calls[0].workDir)
	}
	if puller.calls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected branch %q, got %q", "autoralph/proj-42", puller.calls[0].branch)
	}
}

func TestNewVerifyAction_LogsStartAndFinish(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	activities, err := d.ListActivity(issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	var foundStart, foundFinish bool
	for _, a := range activities {
		if a.EventType == "qa_verify_start" {
			foundStart = true
			if !strings.Contains(a.Detail, "PROJ-42") {
				t.Errorf("expected qa_verify_start detail to contain identifier, got: %s", a.Detail)
			}
		}
		if a.EventType == "qa_verify_finish" {
			foundFinish = true
		}
	}
	if !foundStart {
		t.Error("expected qa_verify_start activity")
	}
	if !foundFinish {
		t.Error("expected qa_verify_finish activity")
	}
}

func TestNewVerifyAction_PassesEventHandlerToInvoker(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastHandler == nil {
		t.Fatal("expected event handler to be passed to InvokeWithEvents")
	}
}

func TestNewVerifyAction_EventHandlerForwardsToUpstream(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	var upstreamReceived []events.Event
	cfg.EventHandler = &mockEventHandler{handleFn: func(e events.Event) {
		upstreamReceived = append(upstreamReceived, e)
	}}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ev := events.ToolUse{Name: "Bash", Detail: "go test ./..."}
	inv.lastHandler.Handle(ev)

	if len(upstreamReceived) != 1 {
		t.Fatalf("expected 1 upstream event, got %d", len(upstreamReceived))
	}
}

func TestNewVerifyAction_WithoutConfigLoader_SkipsQualityChecks(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, runner := defaultMocks(project)
	cfg.ConfigLoad = nil

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no quality check runs when ConfigLoad is nil, got %d", len(runner.calls))
	}
}

func TestNewVerifyAction_IncludesQualityChecksInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cmd := range []string{"ralph check just test", "ralph check just vet"} {
		if !strings.Contains(inv.lastPrompt, cmd) {
			t.Errorf("expected prompt to contain %q", cmd)
		}
	}
}

func TestNewVerifyAction_IncludesKnowledgePath(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(inv.lastPrompt, ".ralph/knowledge") {
		t.Error("expected prompt to contain knowledge path")
	}
}

func TestNewVerifyAction_MultipleChecksFail_ReportsAll(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, runner := defaultMocks(project)

	runner.results = map[string]error{
		"ralph check just test": fmt.Errorf("exit code 1"),
		"ralph check just vet":  fmt.Errorf("exit code 1"),
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "qa verification failed") {
		t.Errorf("expected error to mention 'qa verification failed', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "2 findings") {
		t.Errorf("expected error to mention '2 findings', got: %s", err.Error())
	}

	// Both quality check failures should be recorded as findings in the PRD.
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, readErr := prd.Read(prdPath)
	if readErr != nil {
		t.Fatalf("reading PRD: %v", readErr)
	}
	if p.QAVerification == nil {
		t.Fatal("expected QAVerification to be set")
	}
	foundTest, foundVet := false, false
	for _, f := range p.QAVerification.Findings {
		if strings.Contains(f.Title, "just test") {
			foundTest = true
		}
		if strings.Contains(f.Title, "just vet") {
			foundVet = true
		}
	}
	if !foundTest {
		t.Error("expected finding for 'just test' quality check failure")
	}
	if !foundVet {
		t.Error("expected finding for 'just vet' quality check failure")
	}
}

func TestNewVerifyAction_OnBuildEventCallback(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	var callbackIssueID, callbackDetail string
	cfg.OnBuildEvent = func(issueID, detail string) {
		callbackIssueID = issueID
		callbackDetail = detail
	}

	action := NewVerifyAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate an event through the handler.
	inv.lastHandler.Handle(events.ToolUse{Name: "Bash", Detail: "go test ./..."})

	if callbackIssueID != issue.ID {
		t.Errorf("expected callback issueID %q, got %q", issue.ID, callbackIssueID)
	}
	if !strings.Contains(callbackDetail, "Bash") {
		t.Errorf("expected callback detail to contain 'Bash', got %q", callbackDetail)
	}
}

func TestNewVerifyAction_InvokeErrorButPRDShowsPassed_Succeeds(t *testing.T) {
	// Regression test for process exit hang issue.
	// If the AI completed successfully and updated the PRD to "passed",
	// but the process didn't exit cleanly (e.g., hung), we should still
	// treat QA as successful rather than incorrectly transitioning to QA_FIX.
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	// Simulate AI completing successfully but process not exiting cleanly
	inv.err = errors.New("process exit timeout")

	// Pre-set the PRD to show QA passed (as if AI updated it before hanging)
	prdPath := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "prd.json")
	p, _ := prd.Read(prdPath)
	p.QAVerification.Status = "passed"
	prd.Write(prdPath, p)

	action := NewVerifyAction(cfg)
	err := action(issue, d)

	// Should succeed despite invocation error, since PRD shows passed
	if err != nil {
		t.Errorf("expected success despite invocation error when PRD shows passed, got: %v", err)
	}

	// Verify activity log shows success
	activities, err := d.ListActivity(issue.ID, 100, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	var found bool
	for _, a := range activities {
		if a.EventType == "qa_verify_finish" && strings.Contains(a.Detail, "passed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected qa_verify_finish activity with 'passed' detail")
	}
}

func TestNewVerifyAction_InvokeErrorAndPRDNotPassed_ReturnsError(t *testing.T) {
	// When invocation fails AND PRD doesn't show passed, we should return the error.
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _ := defaultMocks(project)

	// Simulate AI failing (timeout, network error, etc.)
	inv.err = errors.New("AI invocation timeout")

	// PRD remains in "pending" state (AI didn't complete)
	action := NewVerifyAction(cfg)
	err := action(issue, d)

	// Should fail with invocation error
	if err == nil {
		t.Fatal("expected error when invocation fails and PRD not passed")
	}
	if !strings.Contains(err.Error(), "invoking AI") {
		t.Errorf("expected 'invoking AI' in error, got: %s", err.Error())
	}
}
