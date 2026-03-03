package qa

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
)

// --- Fix-specific mock ---

type mockHookRunner struct {
	preCommitCalls  []string
	postCommitCalls []string
	preCommitErr    error
	postCommitErr   error
}

func (m *mockHookRunner) RunPreCommit(_ context.Context, workDir string) error {
	m.preCommitCalls = append(m.preCommitCalls, workDir)
	return m.preCommitErr
}

func (m *mockHookRunner) RunPostCommit(_ context.Context, workDir string) error {
	m.postCommitCalls = append(m.postCommitCalls, workDir)
	return m.postCommitErr
}

func defaultFixMocks(project db.Project) (FixConfig, *mockInvoker, *mockGitOps, *mockHookRunner) {
	inv := &mockInvoker{response: "Fixed QA issues"}
	git := &mockGitOps{}
	hooks := &mockHookRunner{}

	cfg := FixConfig{
		Invoker:      inv,
		Projects:     &mockProjectGetter{project: project},
		ConfigLoad:   &mockConfigLoader{cfg: &config.Config{Project: "test", Repo: config.RepoConfig{DefaultBase: "main"}, QualityChecks: []string{"just test", "just vet"}}},
		BranchPuller: &mockBranchPuller{},
		Git:          git,
		Hooks:        hooks,
	}
	return cfg, inv, git, hooks
}

// --- Tests ---

func TestNewFixAction_InvokesAIWithCorrectPromptAndDir(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
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

	if inv.lastMaxTurns != maxTurnsFix {
		t.Errorf("expected maxTurns %d, got %d", maxTurnsFix, inv.lastMaxTurns)
	}
}

func TestNewFixAction_RendersQAFixPromptWithReportPath(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(inv.lastPrompt, "qa-report.md") {
		t.Error("expected prompt to contain QA report path")
	}
}

func TestNewFixAction_RunsPreCommitHooks(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, hooks := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDir := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "tree")
	if len(hooks.preCommitCalls) != 1 {
		t.Fatalf("expected 1 pre-commit call, got %d", len(hooks.preCommitCalls))
	}
	if hooks.preCommitCalls[0] != expectedDir {
		t.Errorf("expected pre-commit dir %q, got %q", expectedDir, hooks.preCommitCalls[0])
	}
}

func TestNewFixAction_CommitsAndPushes(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, git, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(git.commitCalls) != 1 {
		t.Fatalf("expected 1 commit call, got %d", len(git.commitCalls))
	}
	if len(git.pushCalls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(git.pushCalls))
	}

	expectedDir := filepath.Join(project.LocalPath, ".ralph", "workspaces", "proj-42", "tree")
	if git.commitCalls[0].workDir != expectedDir {
		t.Errorf("expected commit dir %q, got %q", expectedDir, git.commitCalls[0].workDir)
	}
	if git.pushCalls[0].workDir != expectedDir {
		t.Errorf("expected push dir %q, got %q", expectedDir, git.pushCalls[0].workDir)
	}
	if git.pushCalls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected push branch %q, got %q", "autoralph/proj-42", git.pushCalls[0].branch)
	}
}

func TestNewFixAction_RunsPostCommitHooksAfterCommit(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, hooks := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hooks.postCommitCalls) != 1 {
		t.Fatalf("expected 1 post-commit call, got %d", len(hooks.postCommitCalls))
	}
}

func TestNewFixAction_NothingToCommit_SkipsGracefully(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, git, hooks := defaultFixMocks(project)
	git.commitErr = fmt.Errorf("nothing to commit")

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(git.pushCalls) != 0 {
		t.Error("expected no push when nothing to commit")
	}
	if len(hooks.postCommitCalls) != 0 {
		t.Error("expected no post-commit hooks when nothing to commit")
	}
}

func TestNewFixAction_CommitError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, git, _ := defaultFixMocks(project)
	git.commitErr = fmt.Errorf("permission denied")

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "committing changes") {
		t.Errorf("expected 'committing changes' in error, got: %s", err.Error())
	}
}

func TestNewFixAction_PushError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, git, _ := defaultFixMocks(project)
	git.pushErr = fmt.Errorf("rejected")

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pushing changes") {
		t.Errorf("expected 'pushing changes' in error, got: %s", err.Error())
	}
}

func TestNewFixAction_IncrementsQAFixAttempts(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.QAFixAttempts != 1 {
		t.Errorf("expected QAFixAttempts=1, got %d", updated.QAFixAttempts)
	}
}

func TestNewFixAction_IncrementsFromExistingAttempts(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	issue.QAFixAttempts = 2
	if err := d.UpdateIssue(issue); err != nil {
		t.Fatalf("updating issue: %v", err)
	}
	setupWorkspace(t, project)
	cfg, _, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.QAFixAttempts != 3 {
		t.Errorf("expected QAFixAttempts=3, got %d", updated.QAFixAttempts)
	}
}

func TestNewFixAction_PullsBranchBeforeAI(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, _ := defaultFixMocks(project)

	puller := &mockBranchPuller{}
	cfg.BranchPuller = puller

	action := NewFixAction(cfg)
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

func TestNewFixAction_ProjectNotFound_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _ := defaultFixMocks(project)
	cfg.Projects = &mockProjectGetter{err: errors.New("not found")}

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading project") {
		t.Errorf("expected 'loading project' in error, got: %s", err.Error())
	}
}

func TestNewFixAction_PullBranchError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)
	cfg.BranchPuller = &mockBranchPuller{err: errors.New("ff-only failed")}

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pulling branch") {
		t.Errorf("expected 'pulling branch' in error, got: %s", err.Error())
	}
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked when PullBranch fails")
	}
}

func TestNewFixAction_RemoteRefNotFound_ContinuesGracefully(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)
	cfg.BranchPuller = &mockBranchPuller{err: fmt.Errorf("pulling origin/autoralph/proj-42 (ff-only): git pull --ff-only origin autoralph/proj-42 exited with code 1: fatal: couldn't find remote ref autoralph/proj-42")}

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error when remote ref not found, got: %v", err)
	}

	// AI should still be invoked despite the pull skip.
	if inv.lastPrompt == "" {
		t.Error("expected AI to be invoked even when remote ref not found")
	}
}

func TestNewFixAction_AIError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)
	inv.err = errors.New("AI timeout")

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invoking AI") {
		t.Errorf("expected 'invoking AI' in error, got: %s", err.Error())
	}
}

func TestNewFixAction_LogsStartAndFinish(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
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
		if a.EventType == "qa_fix_start" {
			foundStart = true
			if !strings.Contains(a.Detail, "PROJ-42") {
				t.Errorf("expected qa_fix_start detail to contain identifier, got: %s", a.Detail)
			}
		}
		if a.EventType == "qa_fix_finish" {
			foundFinish = true
		}
	}
	if !foundStart {
		t.Error("expected qa_fix_start activity")
	}
	if !foundFinish {
		t.Error("expected qa_fix_finish activity")
	}
}

func TestNewFixAction_PassesEventHandlerToInvoker(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastHandler == nil {
		t.Fatal("expected event handler to be passed to InvokeWithEvents")
	}
}

func TestNewFixAction_EventHandlerForwardsToUpstream(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	var upstreamReceived []events.Event
	cfg.EventHandler = &mockEventHandler{handleFn: func(e events.Event) {
		upstreamReceived = append(upstreamReceived, e)
	}}

	action := NewFixAction(cfg)
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

func TestNewFixAction_IncludesKnowledgePath(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(inv.lastPrompt, ".ralph/knowledge") {
		t.Error("expected prompt to contain knowledge path")
	}
}

func TestNewFixAction_IncludesQualityChecksInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cmd := range []string{"just test", "just vet"} {
		if !strings.Contains(inv.lastPrompt, cmd) {
			t.Errorf("expected prompt to contain %q", cmd)
		}
	}
}

func TestNewFixAction_NilHooks_SkipsHookCalls(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, _, _, _ := defaultFixMocks(project)
	cfg.Hooks = nil

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No panic means hooks were properly guarded.
}

func TestNewFixAction_ConfigLoadError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _ := defaultFixMocks(project)
	cfg.ConfigLoad = &mockConfigLoader{err: errors.New("config not found")}

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading ralph config") {
		t.Errorf("expected 'loading ralph config' in error, got: %s", err.Error())
	}
}

func TestNewFixAction_OnBuildEventCallback(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	setupWorkspace(t, project)
	cfg, inv, _, _ := defaultFixMocks(project)

	var callbackIssueID, callbackDetail string
	cfg.OnBuildEvent = func(issueID, detail string) {
		callbackIssueID = issueID
		callbackDetail = detail
	}

	action := NewFixAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv.lastHandler.Handle(events.ToolUse{Name: "Bash", Detail: "go test ./..."})

	if callbackIssueID != issue.ID {
		t.Errorf("expected callback issueID %q, got %q", issue.ID, callbackIssueID)
	}
	if !strings.Contains(callbackDetail, "Bash") {
		t.Errorf("expected callback detail to contain 'Bash', got %q", callbackDetail)
	}
}
