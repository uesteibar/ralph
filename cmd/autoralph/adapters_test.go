package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/checks"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/ghpoller"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Compile-time check: gitOpsAdapter satisfies checks.GitOps.
var _ checks.GitOps = (*gitOpsAdapter)(nil)

// Compile-time check: claudeInvoker satisfies invoker.EventInvoker.
var _ invoker.EventInvoker = (*claudeInvoker)(nil)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Create an initial commit so the repo has HEAD.
	f, err := os.Create(dir + "/init.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	return dir
}

func TestGitPullerAdapter_PullDefaultBase_Success(t *testing.T) {
	var pulledBranch string
	var pulledDir string
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "main", nil
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			pulledBranch = branch
			pulledDir = r.Dir
			return nil
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pulledBranch != "main" {
		t.Fatalf("expected branch main, got %s", pulledBranch)
	}
	if pulledDir != "/repo" {
		t.Fatalf("expected dir /repo, got %s", pulledDir)
	}
}

func TestGitPullerAdapter_PullDefaultBase_DefaultBaseError(t *testing.T) {
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "", fmt.Errorf("config not found")
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			t.Fatal("pullFn should not be called when defaultBaseFn fails")
			return nil
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "resolving default base: config not found" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestGitPullerAdapter_PullDefaultBase_PullError(t *testing.T) {
	adapter := &gitPullerAdapter{
		defaultBaseFn: func(repoPath, ralphConfigPath string) (string, error) {
			return "main", nil
		},
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			return fmt.Errorf("network error")
		},
	}

	err := adapter.PullDefaultBase(context.Background(), "/repo", ".ralph.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "network error" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestWorkspaceCreatorAdapter_Create_PullsBeforeCreateWorkspace(t *testing.T) {
	var callOrder []string
	adapter := &workspaceCreatorAdapter{
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			callOrder = append(callOrder, "pull:"+branch)
			return nil
		},
	}

	// Create will fail on the actual git operations (no real repo), but we
	// can verify pull was called first by checking order before the error.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)

	if len(callOrder) == 0 {
		t.Fatal("expected pullFn to be called")
	}
	if callOrder[0] != "pull:main" {
		t.Fatalf("expected pull:main, got %s", callOrder[0])
	}
}

func TestWorkspaceCreatorAdapter_Create_ProceedsWhenPullFails(t *testing.T) {
	pullCalled := false
	adapter := &workspaceCreatorAdapter{
		pullFn: func(ctx context.Context, r *shell.Runner, branch string) error {
			pullCalled = true
			return fmt.Errorf("network error")
		},
	}

	// Even though pull fails, Create should still attempt workspace creation.
	// It will fail on actual git ops, but that's fine — we're testing that
	// pullFn failure doesn't prevent the call from proceeding.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)

	if !pullCalled {
		t.Fatal("expected pullFn to be called")
	}
}

func TestWorkspaceCreatorAdapter_Create_SkipsPullWhenNilFn(t *testing.T) {
	adapter := &workspaceCreatorAdapter{
		pullFn: nil,
	}

	// Should not panic — nil pullFn is simply skipped.
	_ = adapter.Create(context.Background(), t.TempDir(), workspace.Workspace{Name: "test-ws"}, "main", nil)
}

func TestRebaseRunnerAdapter_RunRebase_BuildsCorrectCommand(t *testing.T) {
	var capturedArgs []string
	adapter := &rebaseRunnerAdapter{
		cmdFn: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = append([]string{name}, args...)
			// Return a command that succeeds immediately
			return exec.CommandContext(ctx, "true")
		},
	}

	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/.ralph/ralph.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"ralph", "rebase",
		"--workspace", "proj-42",
		"--project-config", "/config/.ralph/ralph.yaml",
		"main",
	}
	if len(capturedArgs) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(capturedArgs), capturedArgs)
	}
	for i, want := range expected {
		if capturedArgs[i] != want {
			t.Errorf("arg[%d]: expected %q, got %q", i, want, capturedArgs[i])
		}
	}
}

func TestRebaseRunnerAdapter_RunRebase_ReturnsErrorOnFailure(t *testing.T) {
	adapter := &rebaseRunnerAdapter{
		cmdFn: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
	}

	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/.ralph/ralph.yaml")
	if err == nil {
		t.Fatal("expected error when command fails")
	}
}

func TestRebaseRunnerAdapter_RunRebase_DefaultUsesExecCommand(t *testing.T) {
	adapter := &rebaseRunnerAdapter{}

	// With nil cmdFn, it defaults to exec.CommandContext.
	// This will fail because "ralph" binary likely isn't available in tests,
	// but we verify it doesn't panic and returns an error.
	err := adapter.RunRebase(context.Background(), "main", "proj-42", "/config/ralph.yaml")
	if err == nil {
		t.Fatal("expected error when ralph binary is not available")
	}
}

func TestGitOpsAdapter_Commit_UsesConfiguredIdentity(t *testing.T) {
	dir := initTestRepo(t)
	ctx := context.Background()

	adapter := &gitOpsAdapter{
		gitAuthorName:  "test-autoralph",
		gitAuthorEmail: "test@autoralph.dev",
	}

	// Create a file to commit.
	if err := os.WriteFile(dir+"/hello.txt", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := adapter.Commit(ctx, dir, "test commit"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify author identity.
	cmd := exec.Command("git", "log", "-1", "--format=%an <%ae>")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "test-autoralph <test@autoralph.dev>"
	if got != want {
		t.Errorf("author = %q, want %q", got, want)
	}

	// Verify committer identity.
	cmd = exec.Command("git", "log", "-1", "--format=%cn <%ce>")
	cmd.Dir = dir
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	got = strings.TrimSpace(string(out))
	if got != want {
		t.Errorf("committer = %q, want %q", got, want)
	}
}

func TestGitOpsAdapter_HeadSHA_UsesConfiguredIdentity(t *testing.T) {
	dir := initTestRepo(t)
	ctx := context.Background()

	adapter := &gitOpsAdapter{
		gitAuthorName:  "test-autoralph",
		gitAuthorEmail: "test@autoralph.dev",
	}

	sha, err := adapter.HeadSHA(ctx, dir)
	if err != nil {
		t.Fatalf("HeadSHA failed: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("HeadSHA returned %q, want 40-char SHA", sha)
	}
}

func TestGitOpsAdapter_GitEnv_SetsAllFourVars(t *testing.T) {
	adapter := &gitOpsAdapter{
		gitAuthorName:  "ralph",
		gitAuthorEmail: "ralph@test.dev",
	}

	env := adapter.gitEnv()

	want := map[string]string{
		"GIT_AUTHOR_NAME":     "ralph",
		"GIT_AUTHOR_EMAIL":    "ralph@test.dev",
		"GIT_COMMITTER_NAME":  "ralph",
		"GIT_COMMITTER_EMAIL": "ralph@test.dev",
	}

	got := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			got[parts[0]] = parts[1]
		}
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("env %s = %q, want %q", k, got[k], v)
		}
	}

	if len(env) != len(want) {
		t.Errorf("gitEnv() returned %d entries, want %d", len(env), len(want))
	}
}

// mockGitHubClient implements ghpoller.GitHubClient and the checks package interfaces.
type mockGitHubClient struct {
	reviews   []github.Review
	merged    bool
	checkRuns []github.CheckRun
	pr        github.PR
	logs      map[int64][]byte
	comments  []github.Comment
}

func (m *mockGitHubClient) FetchPRReviews(_ context.Context, _, _ string, _ int) ([]github.Review, error) {
	return m.reviews, nil
}
func (m *mockGitHubClient) IsPRMerged(_ context.Context, _, _ string, _ int) (bool, error) {
	return m.merged, nil
}
func (m *mockGitHubClient) FetchCheckRuns(_ context.Context, _, _, _ string) ([]github.CheckRun, error) {
	return m.checkRuns, nil
}
func (m *mockGitHubClient) FetchPR(_ context.Context, _, _ string, _ int) (github.PR, error) {
	return m.pr, nil
}
func (m *mockGitHubClient) FetchCheckRunLog(_ context.Context, _, _ string, id int64) ([]byte, error) {
	if m.logs != nil {
		return m.logs[id], nil
	}
	return nil, nil
}
func (m *mockGitHubClient) PostPRComment(_ context.Context, _, _ string, _ int, body string) (github.Comment, error) {
	c := github.Comment{ID: 1, Body: body}
	m.comments = append(m.comments, c)
	return c, nil
}

// TestEndToEnd_CheckFailure_FixApplied_ChecksPass verifies the full lifecycle
// from check failure detection through fixing to re-evaluation (IT-008).
func TestEndToEnd_CheckFailure_FixApplied_ChecksPass(t *testing.T) {
	// 1. Set up database with a project and issue in in_review state.
	database := openTestDB(t)

	project := db.Project{
		Name:        "test-project",
		GithubOwner: "owner",
		GithubRepo:  "repo",
		LocalPath:   t.TempDir(),
	}
	project, err := database.CreateProject(project)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	issue := db.Issue{
		ProjectID:     project.ID,
		Identifier:    "TEST-1",
		Title:         "Test issue",
		State:         string(orchestrator.StateInReview),
		PRNumber:      42,
		WorkspaceName: "ws-1",
		BranchName:    "autoralph/test-1",
	}
	issue, err = database.CreateIssue(issue)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// 2. Mock GitHub: first poll returns failed checks.
	mock := &mockGitHubClient{
		pr: github.PR{Number: 42, HeadSHA: "sha-1"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "completed", Conclusion: "failure"},
		},
	}

	// 3. Run poller: issue transitions to fixing_checks.
	poller := ghpoller.New(database, []ghpoller.ProjectInfo{{
		ProjectID:   project.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, time.Minute, slog.Default(), nil)

	ctx := context.Background()
	runSinglePollCycle(t, poller, ctx)

	// Verify issue is now in fixing_checks.
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if got.State != string(orchestrator.StateFixingChecks) {
		t.Fatalf("expected state fixing_checks, got %s", got.State)
	}

	// 4. Run orchestrator with the check-fixing action.
	actionExecuted := false
	sm := orchestrator.New(database)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateFixingChecks,
		To:   orchestrator.StateInReview,
		Action: func(issue db.Issue, database *db.DB) error {
			actionExecuted = true
			return nil
		},
	})

	tr, ok := sm.Evaluate(got)
	if !ok {
		t.Fatal("expected a matching transition for fixing_checks")
	}
	if err := sm.Execute(tr, got); err != nil {
		t.Fatalf("executing transition: %v", err)
	}
	if !actionExecuted {
		t.Fatal("expected check-fixing action to be executed")
	}

	// Verify issue is back in in_review.
	got, err = database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if got.State != string(orchestrator.StateInReview) {
		t.Fatalf("expected state in_review, got %s", got.State)
	}

	// 5. Run poller with passing checks: issue stays in in_review.
	mock.checkRuns = []github.CheckRun{
		{ID: 3, Name: "build", Status: "completed", Conclusion: "success"},
		{ID: 4, Name: "test", Status: "completed", Conclusion: "success"},
	}
	mock.pr = github.PR{Number: 42, HeadSHA: "sha-2"}

	runSinglePollCycle(t, poller, ctx)

	got, err = database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if got.State != string(orchestrator.StateInReview) {
		t.Fatalf("expected state in_review after passing checks, got %s", got.State)
	}

	// Verify activity log shows the complete flow.
	activities, err := database.ListActivity(issue.ID, 100, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	if len(activities) == 0 {
		t.Fatal("expected at least one activity log entry")
	}

	hasChecksFailedEvent := false
	for _, a := range activities {
		if a.EventType == "checks_failed" {
			hasChecksFailedEvent = true
		}
	}
	if !hasChecksFailedEvent {
		t.Fatal("expected a checks_failed activity log entry")
	}
}

// TestGitHubClientSatisfiesPollerInterface verifies that the mock (and by
// extension the real github.Client via compile-time checks in adapters.go)
// satisfies the ghpoller.GitHubClient interface.
func TestGitHubClientSatisfiesPollerInterface(t *testing.T) {
	var client ghpoller.GitHubClient = &mockGitHubClient{}
	if client == nil {
		t.Fatal("mock should satisfy GitHubClient interface")
	}
}

// TestGitHubClientSatisfiesChecksInterfaces verifies the mock satisfies
// the checks package interfaces (mirroring the compile-time checks for
// the real github.Client in adapters.go).
func TestGitHubClientSatisfiesChecksInterfaces(t *testing.T) {
	mock := &mockGitHubClient{}
	var _ checks.CheckRunFetcher = mock
	var _ checks.LogFetcher = mock
	var _ checks.PRFetcher = mock
	var _ checks.PRCommenter = mock
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := fmt.Sprintf("%s/test.db", t.TempDir())
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// runSinglePollCycle uses the poller's exported Run in a cancellable context
// to execute a single poll cycle. The poller runs one cycle immediately on start.
func runSinglePollCycle(t *testing.T, p *ghpoller.Poller, ctx context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	p.Run(ctx)
}
