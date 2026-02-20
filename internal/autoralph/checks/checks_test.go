package checks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
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
		Invoker:      inv,
		CheckRuns:    checkRuns,
		Logs:         logs,
		PRs:          prFetcher,
		Comments:     commenter,
		Git:          git,
		BranchPuller: &mockBranchPuller{},
		Projects:     projGetter,
		MaxAttempts:  3,
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

func TestNewAction_LogsChecksStartAndFinish(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
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
		if a.EventType == "checks_start" {
			foundStart = true
			if !strings.Contains(a.Detail, "PROJ-42") {
				t.Errorf("expected checks_start detail to contain issue identifier, got: %s", a.Detail)
			}
		}
		if a.EventType == "checks_finish" {
			foundFinish = true
			if !strings.Contains(a.Detail, "test") {
				t.Errorf("expected checks_finish detail to contain check name, got: %s", a.Detail)
			}
		}
	}
	if !foundStart {
		t.Error("expected checks_start activity")
	}
	if !foundFinish {
		t.Error("expected checks_finish activity")
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

	// Create a log with more than 200 lines, with identifiable early and late content
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, fmt.Sprintf("log-line-%d", i))
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

	// Early lines (first 30) should be present
	if !strings.Contains(inv.lastPrompt, "log-line-0") {
		t.Error("expected early log line (line 0) in prompt")
	}
	if !strings.Contains(inv.lastPrompt, "log-line-29") {
		t.Error("expected last early log line (line 29) in prompt")
	}

	// Middle lines should be truncated
	if strings.Contains(inv.lastPrompt, "log-line-50") {
		t.Error("expected middle log line (line 50) to be truncated")
	}

	// Late lines (last 170) should be present
	if !strings.Contains(inv.lastPrompt, "log-line-299") {
		t.Error("expected last log line (line 299) in prompt")
	}

	// Truncation marker should be present
	if !strings.Contains(inv.lastPrompt, "truncated") {
		t.Error("expected truncation marker in prompt")
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
	result := truncateLog(log, 200)
	if result != log {
		t.Errorf("expected unchanged log, got %q", result)
	}
}

func TestTruncateLog_ExactlyAtLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i))
	}
	log := strings.Join(lines, "\n")
	result := truncateLog(log, 200)
	if result != log {
		t.Error("expected 200-line log returned unchanged")
	}
}

func TestTruncateLog_OverLimit_KeepsFirstAndLast(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i))
	}
	log := strings.Join(lines, "\n")
	result := truncateLog(log, 200)

	resultLines := strings.Split(result, "\n")

	// First 30 lines preserved
	for i := 0; i < 30; i++ {
		expected := fmt.Sprintf("line-%d", i)
		if resultLines[i] != expected {
			t.Errorf("line %d: expected %q, got %q", i, expected, resultLines[i])
		}
	}

	// Truncation marker present at line 30
	if !strings.Contains(resultLines[30], "... 300 lines truncated ...") {
		t.Errorf("expected truncation marker, got %q", resultLines[30])
	}

	// Last 170 lines preserved (lines 330-499)
	for i := 0; i < 170; i++ {
		expected := fmt.Sprintf("line-%d", 330+i)
		actual := resultLines[31+i]
		if actual != expected {
			t.Errorf("tail line %d: expected %q, got %q", i, expected, actual)
		}
	}

	// Total: 30 head + 1 marker + 170 tail = 201
	if len(resultLines) != 201 {
		t.Errorf("expected 201 lines total, got %d", len(resultLines))
	}
}

func TestTruncateLog_PreservesEarlyErrors(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		if i == 5 {
			lines = append(lines, "EARLY_ERROR_SENTINEL")
		} else if i == 490 {
			lines = append(lines, "RECENT_STATE_SENTINEL")
		} else {
			lines = append(lines, fmt.Sprintf("line-%d", i))
		}
	}
	log := strings.Join(lines, "\n")
	result := truncateLog(log, 200)

	if !strings.Contains(result, "EARLY_ERROR_SENTINEL") {
		t.Error("expected early error sentinel in first 30 lines")
	}
	if !strings.Contains(result, "RECENT_STATE_SENTINEL") {
		t.Error("expected recent state sentinel in last 170 lines")
	}
	if !strings.Contains(result, "... ") && !strings.Contains(result, "truncated") {
		t.Error("expected truncation marker")
	}
}

type mockConfigLoader struct {
	cfg *config.Config
	err error
}

func (m *mockConfigLoader) Load(_ string) (*config.Config, error) {
	return m.cfg, m.err
}

func TestNewAction_WithConfigLoader_IncludesQualityChecksInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)
	cfg.ConfigLoad = &mockConfigLoader{
		cfg: &config.Config{
			Project:       "test",
			Repo:          config.RepoConfig{DefaultBase: "main"},
			QualityChecks: []string{"just test", "just lint"},
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cmd := range []string{"ralph check just test", "ralph check just lint"} {
		if !strings.Contains(inv.lastPrompt, cmd) {
			t.Errorf("expected prompt to contain %q", cmd)
		}
	}
}

func TestNewAction_WithConfigLoader_Error_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)
	cfg.ConfigLoad = &mockConfigLoader{err: errors.New("config not found")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading ralph config") {
		t.Errorf("expected 'loading ralph config' in error, got: %s", err.Error())
	}
}

func TestNewAction_WithoutConfigLoader_SkipsQualityChecks(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)
	// ConfigLoad is nil — should not crash and should not include quality checks
	cfg.ConfigLoad = nil

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(inv.lastPrompt, "ralph check") {
		t.Error("expected prompt NOT to contain 'ralph check' when ConfigLoad is nil")
	}
}

func TestNewAction_PassesEventHandlerToInvoker(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastHandler == nil {
		t.Fatal("expected event handler to be passed to InvokeWithEvents")
	}
}

func TestNewAction_EventHandlerLogsBuildEvents(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	var callbackIssueID, callbackDetail string
	cfg.OnBuildEvent = func(issueID, detail string) {
		callbackIssueID = issueID
		callbackDetail = detail
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate a tool-use event through the handler
	inv.lastHandler.Handle(events.ToolUse{Name: "Bash", Detail: "go test ./..."})

	// Verify build_event was logged to the activity table
	activities, err := d.ListActivity(issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	found := false
	for _, a := range activities {
		if a.EventType == "build_event" && strings.Contains(a.Detail, "Bash") {
			found = true
		}
	}
	if !found {
		t.Error("expected build_event activity with tool name 'Bash'")
	}

	// Verify OnBuildEvent callback was called
	if callbackIssueID != issue.ID {
		t.Errorf("expected callback issueID %q, got %q", issue.ID, callbackIssueID)
	}
	if !strings.Contains(callbackDetail, "Bash") {
		t.Errorf("expected callback detail to contain 'Bash', got %q", callbackDetail)
	}
}

func TestNewAction_EventHandlerForwardsToUpstream(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	var upstreamReceived []events.Event
	cfg.EventHandler = &mockEventHandler{handleFn: func(e events.Event) {
		upstreamReceived = append(upstreamReceived, e)
	}}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Send event through the handler
	ev := events.ToolUse{Name: "Read", Detail: "main.go"}
	inv.lastHandler.Handle(ev)

	if len(upstreamReceived) != 1 {
		t.Fatalf("expected 1 upstream event, got %d", len(upstreamReceived))
	}
	if tu, ok := upstreamReceived[0].(events.ToolUse); !ok || tu.Name != "Read" {
		t.Error("expected upstream to receive ToolUse event with name 'Read'")
	}
}

// mockEventHandler is a test helper for verifying event forwarding.
type mockEventHandler struct {
	handleFn func(e events.Event)
}

func (m *mockEventHandler) Handle(e events.Event) {
	if m.handleFn != nil {
		m.handleFn(e)
	}
}

func TestNewAction_IncludesKnowledgePath(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The knowledge path is computed from workspace.TreePath
	if !strings.Contains(inv.lastPrompt, ".ralph/knowledge") {
		t.Error("expected prompt to contain knowledge path")
	}
}

// --- BranchPuller tests ---

func TestNewAction_PullsBranchBeforeAIInvocation(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	puller := &mockBranchPuller{}
	cfg.BranchPuller = puller

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(puller.calls) != 1 {
		t.Fatalf("expected 1 PullBranch call, got %d", len(puller.calls))
	}

	expectedTreePath := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if puller.calls[0].workDir != expectedTreePath {
		t.Errorf("expected workDir %q, got %q", expectedTreePath, puller.calls[0].workDir)
	}
	if puller.calls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected branch %q, got %q", "autoralph/proj-42", puller.calls[0].branch)
	}
}

func TestNewAction_PullBranchError_ReturnsErrorWithoutAI(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, git := defaultMocks(project)

	puller := &mockBranchPuller{err: errors.New("ff-only failed: diverged")}
	cfg.BranchPuller = puller

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error from PullBranch")
	}
	if !strings.Contains(err.Error(), "pulling branch") {
		t.Errorf("expected 'pulling branch' in error, got: %s", err.Error())
	}

	// AI should NOT have been invoked
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked when PullBranch fails")
	}

	// No git operations should have occurred
	if len(git.commitCalls) != 0 {
		t.Errorf("expected no commit calls, got %d", len(git.commitCalls))
	}
	if len(git.pushCalls) != 0 {
		t.Errorf("expected no push calls, got %d", len(git.pushCalls))
	}
}

func TestNewAction_PullBranchCalledBeforeInvoke(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	var order []string
	puller := &mockBranchPuller{}
	cfg.BranchPuller = &orderTrackingPuller{inner: puller, orderLog: &order}
	cfg.Invoker = &orderTrackingInvoker{inner: cfg.Invoker, orderLog: &order}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pullIdx, invokeIdx := -1, -1
	for i, op := range order {
		if op == "pull" {
			pullIdx = i
		}
		if op == "invoke" {
			invokeIdx = i
		}
	}
	if pullIdx < 0 {
		t.Fatal("expected PullBranch call")
	}
	if invokeIdx < 0 {
		t.Fatal("expected AI invocation call")
	}
	if pullIdx >= invokeIdx {
		t.Errorf("expected PullBranch (index %d) before AI invocation (index %d)", pullIdx, invokeIdx)
	}
}

type orderTrackingPuller struct {
	inner    BranchPuller
	orderLog *[]string
}

func (o *orderTrackingPuller) PullBranch(ctx context.Context, workDir, branch string) error {
	*o.orderLog = append(*o.orderLog, "pull")
	return o.inner.PullBranch(ctx, workDir, branch)
}

type orderTrackingInvoker struct {
	inner    invoker.EventInvoker
	orderLog *[]string
}

func (o *orderTrackingInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	*o.orderLog = append(*o.orderLog, "invoke")
	return o.inner.InvokeWithEvents(ctx, prompt, dir, maxTurns, handler)
}

func TestNewAction_PassesMaxTurns(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, inv, _, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastMaxTurns != maxTurnsChecks {
		t.Errorf("expected maxTurns %d, got %d", maxTurnsChecks, inv.lastMaxTurns)
	}
}

// --- PRUpdater mock ---

type mockPRUpdater struct {
	calls []prUpdaterCall
}

type prUpdaterCall struct {
	issue   db.Issue
	project db.Project
}

func (m *mockPRUpdater) UpdateDescription(_ context.Context, issue db.Issue, project db.Project) {
	m.calls = append(m.calls, prUpdaterCall{issue: issue, project: project})
}

// --- PRUpdater tests ---

func TestNewAction_PRUpdater_CalledAfterCommitPush(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)

	updater := &mockPRUpdater{}
	cfg.PRUpdater = updater

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(updater.calls) != 1 {
		t.Fatalf("expected 1 UpdateDescription call, got %d", len(updater.calls))
	}
	if updater.calls[0].issue.PRNumber != 10 {
		t.Errorf("expected PRNumber 10, got %d", updater.calls[0].issue.PRNumber)
	}
	if updater.calls[0].project.GithubOwner != "owner" {
		t.Errorf("expected GithubOwner 'owner', got %q", updater.calls[0].project.GithubOwner)
	}
}

func TestNewAction_PRUpdater_NotCalledWhenNothingCommitted(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, git := defaultMocks(project)
	git.commitErr = errors.New("nothing to commit")

	updater := &mockPRUpdater{}
	cfg.PRUpdater = updater

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(updater.calls) != 0 {
		t.Errorf("expected 0 UpdateDescription calls when nothing committed, got %d", len(updater.calls))
	}
}

func TestNewAction_PRUpdater_NilSafe(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 0)
	cfg, _, _, _, _, _, _ := defaultMocks(project)
	cfg.PRUpdater = nil // should not crash

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error with nil PRUpdater, got: %v", err)
	}
}

func TestNewAction_PRUpdater_NotCalledOnLoopExhaustion(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project, 2) // attempt 3 = max

	cfg, _, _, _, _, _, _ := defaultMocks(project)
	updater := &mockPRUpdater{}
	cfg.PRUpdater = updater

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(updater.calls) != 0 {
		t.Errorf("expected 0 UpdateDescription calls on loop exhaustion, got %d", len(updater.calls))
	}
}

// Suppress unused import warning for ai package
var _ = ai.FixChecksData{}
