package checks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/github"
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

func createTestIssue(t *testing.T, d *db.DB, project db.Project, attempts int) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:        project.ID,
		LinearIssueID:    "lin-123",
		Identifier:       "PROJ-42",
		Title:            "Add user avatars",
		Description:      "Users should be able to upload profile pictures.",
		State:            "fixing_checks",
		WorkspaceName:    "proj-42",
		BranchName:       "autoralph/proj-42",
		PRNumber:         10,
		PRURL:            "https://github.com/owner/repo/pull/10",
		CheckFixAttempts: attempts,
		LastCheckSHA:     "old-sha",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// --- Mocks ---

type mockInvoker struct {
	lastPrompt string
	lastDir    string
	response   string
	err        error
}

func (m *mockInvoker) Invoke(_ context.Context, prompt, dir string) (string, error) {
	m.lastPrompt = prompt
	m.lastDir = dir
	return m.response, m.err
}

type mockCheckRunFetcher struct {
	checkRuns []github.CheckRun
	err       error
}

func (m *mockCheckRunFetcher) FetchCheckRuns(_ context.Context, _, _, _ string) ([]github.CheckRun, error) {
	return m.checkRuns, m.err
}

type mockLogFetcher struct {
	logs map[int64][]byte
	err  error
}

func (m *mockLogFetcher) FetchCheckRunLog(_ context.Context, _, _ string, id int64) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.logs[id], nil
}

type mockPRFetcher struct {
	pr  github.PR
	err error
}

func (m *mockPRFetcher) FetchPR(_ context.Context, _, _ string, _ int) (github.PR, error) {
	return m.pr, m.err
}

type mockPRCommenter struct {
	calls []prCommentCall
	err   error
}

type prCommentCall struct {
	owner, repo string
	prNumber    int
	body        string
}

func (m *mockPRCommenter) PostPRComment(_ context.Context, owner, repo string, prNumber int, body string) (github.Comment, error) {
	m.calls = append(m.calls, prCommentCall{owner: owner, repo: repo, prNumber: prNumber, body: body})
	return github.Comment{ID: 999, Body: body}, m.err
}

type mockGitOps struct {
	commitCalls []commitCall
	pushCalls   []pushCall
	headSHA     string
	commitErr   error
	pushErr     error
	headErr     error
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

func (m *mockGitOps) HeadSHA(_ context.Context, _ string) (string, error) {
	return m.headSHA, m.headErr
}

type mockProjectGetter struct {
	project db.Project
	err     error
}

func (m *mockProjectGetter) GetProject(_ string) (db.Project, error) {
	return m.project, m.err
}

func defaultMocks(project db.Project) (Config, *mockInvoker, *mockCheckRunFetcher, *mockLogFetcher, *mockPRFetcher, *mockPRCommenter, *mockGitOps) {
	inv := &mockInvoker{response: "Fixed the failing checks"}
	checkRuns := &mockCheckRunFetcher{
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "lint", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "completed", Conclusion: "failure"},
			{ID: 3, Name: "build", Status: "completed", Conclusion: "failure"},
		},
	}
	logs := &mockLogFetcher{
		logs: map[int64][]byte{
			2: []byte("FAIL: TestSomething\nExpected 1 got 2"),
			3: []byte("Error: missing import"),
		},
	}
	prFetcher := &mockPRFetcher{
		pr: github.PR{Number: 10, HeadSHA: "abc123"},
	}
	commenter := &mockPRCommenter{}
	git := &mockGitOps{headSHA: "def456"}
	projGetter := &mockProjectGetter{project: project}

	cfg := Config{
		Invoker:     inv,
		CheckRuns:   checkRuns,
		Logs:        logs,
		PRs:         prFetcher,
		Comments:    commenter,
		Git:         git,
		Projects:    projGetter,
		MaxAttempts: 3,
	}
	return cfg, inv, checkRuns, logs, prFetcher, commenter, git
}

// --- Tests ---

func TestNewAction_FetchesFailedChecksAndInvokesAI(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}
	if !strings.Contains(inv.lastPrompt, "test") {
		t.Error("expected prompt to contain failed check name 'test'")
	}
	if !strings.Contains(inv.lastPrompt, "build") {
		t.Error("expected prompt to contain failed check name 'build'")
	}
	if strings.Contains(inv.lastPrompt, "lint") {
		t.Error("expected prompt NOT to contain passing check 'lint'")
	}
	if !strings.Contains(inv.lastPrompt, "FAIL: TestSomething") {
		t.Error("expected prompt to contain log content")
	}

	expectedDir := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if inv.lastDir != expectedDir {
		t.Errorf("expected AI dir %q, got %q", expectedDir, inv.lastDir)
	}
}

func TestNewAction_CommitsAndPushesChanges(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, git := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(git.commitCalls) != 1 {
		t.Fatalf("expected 1 commit call, got %d", len(git.commitCalls))
	}

	expectedWorkDir := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if git.commitCalls[0].workDir != expectedWorkDir {
		t.Errorf("expected workDir %q, got %q", expectedWorkDir, git.commitCalls[0].workDir)
	}
	if !strings.Contains(git.commitCalls[0].message, "test") {
		t.Errorf("expected commit message to contain check name 'test', got %q", git.commitCalls[0].message)
	}
	if !strings.Contains(git.commitCalls[0].message, "build") {
		t.Errorf("expected commit message to contain check name 'build', got %q", git.commitCalls[0].message)
	}

	if len(git.pushCalls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(git.pushCalls))
	}
	if git.pushCalls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected push branch %q, got %q", "autoralph/proj-42", git.pushCalls[0].branch)
	}
}

func TestNewAction_IncrementsCheckFixAttempts(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 1)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.CheckFixAttempts != 2 {
		t.Errorf("expected CheckFixAttempts=2, got %d", updated.CheckFixAttempts)
	}
	if updated.LastCheckSHA != "abc123" {
		t.Errorf("expected LastCheckSHA='abc123', got %q", updated.LastCheckSHA)
	}
}

func TestNewAction_LogsChecksFixingActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	foundFixing := false
	foundFixed := false
	for _, a := range activities {
		if a.EventType == "checks_fixing" {
			foundFixing = true
		}
		if a.EventType == "checks_fixed" {
			foundFixed = true
			if !strings.Contains(a.Detail, "test") {
				t.Errorf("expected checks_fixed detail to contain check name, got: %s", a.Detail)
			}
		}
	}
	if !foundFixing {
		t.Error("expected checks_fixing activity")
	}
	if !foundFixed {
		t.Error("expected checks_fixed activity")
	}
}

func TestNewAction_NothingToCommit_HandlesGracefully(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, git := defaultMocks(project)
	git.commitErr = errors.New("nothing to commit")

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error for nothing-to-commit, got: %v", err)
	}

	if len(git.pushCalls) != 0 {
		t.Errorf("expected no push calls, got %d", len(git.pushCalls))
	}

	// CheckFixAttempts should still be incremented
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.CheckFixAttempts != 1 {
		t.Errorf("expected CheckFixAttempts=1, got %d", updated.CheckFixAttempts)
	}
}

func TestNewAction_LoopExhaustion_PostsCommentAndPauses(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 2) // attempt 3 = max
	cfg, _, _, _, _, commenter, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// PR comment should be posted
	if len(commenter.calls) != 1 {
		t.Fatalf("expected 1 PR comment call, got %d", len(commenter.calls))
	}
	if !strings.Contains(commenter.calls[0].body, "could not fix") {
		t.Errorf("expected comment to explain failure, got: %s", commenter.calls[0].body)
	}
	if !strings.Contains(commenter.calls[0].body, "please have a look") {
		t.Errorf("expected comment to ask user to look, got: %s", commenter.calls[0].body)
	}
	if commenter.calls[0].prNumber != 10 {
		t.Errorf("expected PR number 10, got %d", commenter.calls[0].prNumber)
	}

	// Issue state should be paused
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state 'paused', got %q", updated.State)
	}

	// Activity should contain checks_paused
	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "checks_paused" {
			found = true
		}
	}
	if !found {
		t.Error("expected checks_paused activity")
	}
}

func TestNewAction_TruncatesLogs(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, logFetcher, _, _, _ := defaultMocks(project)

	// Create a log with more than 500 lines
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, "log line")
	}
	logFetcher.logs = map[int64][]byte{
		2: []byte(strings.Join(lines, "\n")),
		3: []byte("short log"),
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The prompt should not contain all 600 lines — it should be truncated
	lineCount := strings.Count(inv.lastPrompt, "log line")
	if lineCount > 500 {
		t.Errorf("expected at most 500 'log line' occurrences, got %d", lineCount)
	}
}

func TestNewAction_MissingLogs_HandlesGracefully(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, logFetcher, _, _, _ := defaultMocks(project)

	// Return nil logs for all check runs
	logFetcher.logs = map[int64][]byte{}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still include check names and conclusions in prompt
	if !strings.Contains(inv.lastPrompt, "test") {
		t.Error("expected prompt to contain check name 'test' even without log")
	}
	if !strings.Contains(inv.lastPrompt, "failure") {
		t.Error("expected prompt to contain conclusion 'failure' even without log")
	}
}

func TestNewAction_FetchPRError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, prFetcher, _, _ := defaultMocks(project)
	prFetcher.err = errors.New("github 500")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetching PR") {
		t.Errorf("expected 'fetching PR' in error, got: %s", err.Error())
	}
}

func TestNewAction_FetchCheckRunsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, checkRuns, _, _, _, _ := defaultMocks(project)
	checkRuns.err = errors.New("github 500")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetching check runs") {
		t.Errorf("expected 'fetching check runs' in error, got: %s", err.Error())
	}
}

func TestNewAction_AIError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)
	inv.err = errors.New("AI timeout")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invoking AI") {
		t.Errorf("expected 'invoking AI' in error, got: %s", err.Error())
	}
}

func TestNewAction_PushError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, git := defaultMocks(project)
	git.pushErr = errors.New("push rejected")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pushing changes") {
		t.Errorf("expected 'pushing changes' in error, got: %s", err.Error())
	}
}

func TestNewAction_ProjectNotFound(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)
	cfg.Projects = &mockProjectGetter{err: errors.New("not found")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading project") {
		t.Errorf("expected 'loading project' in error, got: %s", err.Error())
	}
}

func TestNewAction_RealCommitError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, git := defaultMocks(project)
	git.commitErr = errors.New("fatal: unable to write tree")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "committing changes") {
		t.Errorf("expected 'committing changes' in error, got: %s", err.Error())
	}
}

func TestNewAction_LoopExhaustion_PostCommentError_StillPauses(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 2)
	cfg, _, _, _, _, commenter, _ := defaultMocks(project)
	commenter.err = errors.New("github 500")

	action := NewAction(cfg)
	err := action(issue, d)
	// Comment failure should not block pausing
	if err != nil {
		t.Fatalf("expected no error even with comment failure, got: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state 'paused', got %q", updated.State)
	}
}

func TestTruncateLog_UnderLimit(t *testing.T) {
	log := "line1\nline2\nline3"
	result := truncateLog(log, 500)
	if result != log {
		t.Errorf("expected unchanged log, got %q", result)
	}
}

func TestTruncateLog_OverLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, "line")
	}
	log := strings.Join(lines, "\n")
	result := truncateLog(log, 500)
	resultLines := strings.Split(result, "\n")
	if len(resultLines) != 500 {
		t.Errorf("expected 500 lines, got %d", len(resultLines))
	}
}

// Suppress unused import warning for ai package
var _ = ai.FixChecksData{}
