package feedback

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

func createTestIssue(t *testing.T, d *db.DB, project db.Project) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     project.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         "addressing_feedback",
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
		PRNumber:      10,
		PRURL:         "https://github.com/owner/repo/pull/10",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// --- Mocks ---

type mockInvoker struct {
	lastPrompt   string
	lastMaxTurns int
	lastHandler  events.EventHandler
	response     string
	err          error
}

func (m *mockInvoker) InvokeWithEvents(_ context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	m.lastPrompt = prompt
	m.lastMaxTurns = maxTurns
	m.lastHandler = handler
	return m.response, m.err
}

type mockCommentFetcher struct {
	comments []github.Comment
	err      error
}

func (m *mockCommentFetcher) FetchPRComments(_ context.Context, _, _ string, _ int) ([]github.Comment, error) {
	return m.comments, m.err
}

type mockReviewFetcher struct {
	reviews []github.Review
	err     error
}

func (m *mockReviewFetcher) FetchPRReviews(_ context.Context, _, _ string, _ int) ([]github.Review, error) {
	return m.reviews, m.err
}

type mockIssueCommentFetcher struct {
	comments []github.Comment
	err      error
}

func (m *mockIssueCommentFetcher) FetchPRIssueComments(_ context.Context, _, _ string, _ int) ([]github.Comment, error) {
	return m.comments, m.err
}

type mockReviewReplier struct {
	calls []replyCall
	err   error
}

type replyCall struct {
	owner, repo string
	prNumber    int
	commentID   int64
	body        string
}

func (m *mockReviewReplier) PostReviewReply(_ context.Context, owner, repo string, prNumber int, commentID int64, body string) (github.Comment, error) {
	m.calls = append(m.calls, replyCall{owner: owner, repo: repo, prNumber: prNumber, commentID: commentID, body: body})
	return github.Comment{ID: commentID + 100, Body: body}, m.err
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

func defaultMocks(project db.Project) (Config, *mockInvoker, *mockCommentFetcher, *mockReviewReplier, *mockGitOps) {
	inv := &mockInvoker{response: "AI addressed all feedback"}
	fetcher := &mockCommentFetcher{
		comments: []github.Comment{
			{ID: 1, Path: "main.go", Body: "Please add error handling here", User: "reviewer"},
			{ID: 2, Path: "utils.go", Body: "This function needs a docstring", User: "reviewer"},
		},
	}
	replier := &mockReviewReplier{}
	git := &mockGitOps{headSHA: "abc1234"}
	projGetter := &mockProjectGetter{project: project}

	cfg := Config{
		Invoker:      inv,
		Comments:     fetcher,
		Replier:      replier,
		Git:          git,
		BranchPuller: &mockBranchPuller{},
		Projects:     projGetter,
	}
	return cfg, inv, fetcher, replier, git
}

// --- Tests ---

func TestNewAction_InvokesAIWithFeedbackPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}
	if !strings.Contains(inv.lastPrompt, "main.go") {
		t.Error("expected prompt to contain comment file path")
	}
	if !strings.Contains(inv.lastPrompt, "Please add error handling here") {
		t.Error("expected prompt to contain comment body")
	}
	if !strings.Contains(inv.lastPrompt, "reviewer") {
		t.Error("expected prompt to contain comment author")
	}
}

func TestNewAction_CommitsAndPushesChanges(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, git := defaultMocks(project)

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
	if !strings.Contains(git.commitCalls[0].message, "Address review feedback") {
		t.Errorf("expected commit message to contain 'Address review feedback', got %q", git.commitCalls[0].message)
	}

	if len(git.pushCalls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(git.pushCalls))
	}
	if git.pushCalls[0].workDir != expectedWorkDir {
		t.Errorf("expected push workDir %q, got %q", expectedWorkDir, git.pushCalls[0].workDir)
	}
	if git.pushCalls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected push branch %q, got %q", "autoralph/proj-42", git.pushCalls[0].branch)
	}
}

func TestNewAction_RepliesToEachComment(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, replier, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls, got %d", len(replier.calls))
	}

	for _, call := range replier.calls {
		if call.owner != "owner" {
			t.Errorf("expected owner %q, got %q", "owner", call.owner)
		}
		if call.repo != "repo" {
			t.Errorf("expected repo %q, got %q", "repo", call.repo)
		}
		if call.prNumber != 10 {
			t.Errorf("expected prNumber 10, got %d", call.prNumber)
		}
		if !strings.Contains(call.body, "abc1234") {
			t.Errorf("expected reply to contain commit SHA, got: %s", call.body)
		}
	}

	if replier.calls[0].commentID != 1 {
		t.Errorf("expected first reply to comment 1, got %d", replier.calls[0].commentID)
	}
	if replier.calls[1].commentID != 2 {
		t.Errorf("expected second reply to comment 2, got %d", replier.calls[1].commentID)
	}
}

func TestNewAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	found := false
	for _, a := range activities {
		if a.EventType == "feedback_finish" {
			found = true
			if !strings.Contains(a.Detail, "abc1234") {
				t.Errorf("expected detail to contain commit SHA, got: %s", a.Detail)
			}
			if !strings.Contains(a.Detail, "2 comment") {
				t.Errorf("expected detail to mention comment count, got: %s", a.Detail)
			}
		}
	}
	if !found {
		t.Error("expected feedback_finish activity")
	}
}

func TestNewAction_NoComments_SkipsAIAndCommit(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, git := defaultMocks(project)
	fetcher.comments = nil

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked when no comments")
	}
	if len(git.commitCalls) != 0 {
		t.Errorf("expected no commit calls, got %d", len(git.commitCalls))
	}
	if len(git.pushCalls) != 0 {
		t.Errorf("expected no push calls, got %d", len(git.pushCalls))
	}
	if len(replier.calls) != 0 {
		t.Errorf("expected no reply calls, got %d", len(replier.calls))
	}
}

func TestNewAction_FetchCommentsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.err = errors.New("github 500")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetching PR comments") {
		t.Errorf("expected 'fetching PR comments' in error, got: %s", err.Error())
	}
}

func TestNewAction_AIError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)
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

func TestNewAction_NothingToCommit_SucceedsWithNoCommitRef(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, replier, git := defaultMocks(project)
	git.commitErr = errors.New("nothing to commit")

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error for nothing-to-commit, got: %v", err)
	}

	// Should not push when nothing was committed
	if len(git.pushCalls) != 0 {
		t.Errorf("expected no push calls, got %d", len(git.pushCalls))
	}

	// Replies should not reference a commit SHA but should relay the AI response
	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls, got %d", len(replier.calls))
	}
	for _, call := range replier.calls {
		if strings.Contains(call.body, "abc1234") {
			t.Errorf("expected reply NOT to contain commit SHA, got: %s", call.body)
		}
		// Should relay the AI's response instead of a canned message
		if call.body == "Reviewed — no code changes needed." {
			t.Errorf("expected reply to relay AI response, not canned message, got: %s", call.body)
		}
	}
}

func TestNewAction_RealCommitError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, git := defaultMocks(project)
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

func TestNewAction_PushError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, git := defaultMocks(project)
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

func TestNewAction_ReplyError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, replier, _ := defaultMocks(project)
	replier.err = errors.New("github 403")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "replying to comment") {
		t.Errorf("expected 'replying to comment' in error, got: %s", err.Error())
	}
}

func TestNewAction_ProjectNotFound(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)
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

func TestNewAction_GroupsReplyThreads(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 2, Path: "main.go", Body: "Addressed in abc123", User: "bot", InReplyTo: 1},
		{ID: 3, Path: "utils.go", Body: "Add tests", User: "reviewer"},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reply body should appear in the prompt as context under the parent.
	if !strings.Contains(inv.lastPrompt, "Addressed in abc123") {
		t.Error("expected reply body to be included in AI prompt as context")
	}
	// Parent body should also be present.
	if !strings.Contains(inv.lastPrompt, "Fix this") {
		t.Error("expected parent comment body in prompt")
	}

	// Only top-level comments get replies (not the reply comment itself).
	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls (to top-level comments only), got %d", len(replier.calls))
	}
	if replier.calls[0].commentID != 1 {
		t.Errorf("expected first reply to comment 1, got %d", replier.calls[0].commentID)
	}
	if replier.calls[1].commentID != 3 {
		t.Errorf("expected second reply to comment 3, got %d", replier.calls[1].commentID)
	}
}

func TestNewAction_HeadSHAError_UsesUnknown(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, replier, git := defaultMocks(project)
	git.headErr = errors.New("git error")

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls, got %d", len(replier.calls))
	}
	if !strings.Contains(replier.calls[0].body, "latest commit") {
		t.Errorf("expected reply to contain 'latest commit' fallback, got: %s", replier.calls[0].body)
	}
}

func TestNewAction_WithConfigLoader_IncludesQualityChecksInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)
	cfg.ConfigLoad = &mockConfigLoader{
		cfg: &config.Config{
			Project:       "test",
			Repo:          config.RepoConfig{DefaultBase: "main"},
			QualityChecks: []string{"just test", "just vet"},
		},
	}

	action := NewAction(cfg)
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

func TestNewAction_WithConfigLoader_Error_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)
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
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)
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
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

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
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

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
	inv.lastHandler.Handle(events.ToolUse{Name: "Edit", Detail: "main.go"})

	// Verify build_event was logged to the activity table
	activities, err := d.ListActivity(issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	found := false
	for _, a := range activities {
		if a.EventType == "build_event" && strings.Contains(a.Detail, "Edit") {
			found = true
		}
	}
	if !found {
		t.Error("expected build_event activity with tool name 'Edit'")
	}

	// Verify OnBuildEvent callback was called
	if callbackIssueID != issue.ID {
		t.Errorf("expected callback issueID %q, got %q", issue.ID, callbackIssueID)
	}
	if !strings.Contains(callbackDetail, "Edit") {
		t.Errorf("expected callback detail to contain 'Edit', got %q", callbackDetail)
	}
}

func TestNewAction_EventHandlerForwardsToUpstream(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

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
	ev := events.ToolUse{Name: "Bash", Detail: "go test ./..."}
	inv.lastHandler.Handle(ev)

	if len(upstreamReceived) != 1 {
		t.Fatalf("expected 1 upstream event, got %d", len(upstreamReceived))
	}
	if tu, ok := upstreamReceived[0].(events.ToolUse); !ok || tu.Name != "Bash" {
		t.Error("expected upstream to receive ToolUse event with name 'Bash'")
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

func TestNewAction_LogsFeedbackStartAndFinish(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)

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
		if a.EventType == "feedback_start" {
			foundStart = true
			if !strings.Contains(a.Detail, "PROJ-42") {
				t.Errorf("expected feedback_start detail to contain issue identifier, got: %s", a.Detail)
			}
		}
		if a.EventType == "feedback_finish" {
			foundFinish = true
			if !strings.Contains(a.Detail, "2 comment") {
				t.Errorf("expected feedback_finish detail to mention comment count, got: %s", a.Detail)
			}
		}
	}
	if !foundStart {
		t.Error("expected feedback_start activity")
	}
	if !foundFinish {
		t.Error("expected feedback_finish activity")
	}
}

func TestNewAction_NoComments_LogsStartButSkipsFinish(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

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
		if a.EventType == "feedback_start" {
			foundStart = true
		}
		if a.EventType == "feedback_finish" {
			foundFinish = true
		}
	}
	if !foundStart {
		t.Error("expected feedback_start activity even with no comments")
	}
	if foundFinish {
		t.Error("expected no feedback_finish activity when no comments")
	}
}

// --- CommentReactor mock ---

type reactCall struct {
	owner, repo string
	commentID   int64
	reaction    string
}

type mockCommentReactor struct {
	calls []reactCall
	err   error
}

func (m *mockCommentReactor) ReactToReviewComment(_ context.Context, owner, repo string, commentID int64, reaction string) error {
	m.calls = append(m.calls, reactCall{owner: owner, repo: repo, commentID: commentID, reaction: reaction})
	return m.err
}

// --- IssueCommentReactor mock ---

type mockIssueCommentReactor struct {
	calls []reactCall
	err   error
}

func (m *mockIssueCommentReactor) ReactToIssueComment(_ context.Context, owner, repo string, commentID int64, reaction string) error {
	m.calls = append(m.calls, reactCall{owner: owner, repo: repo, commentID: commentID, reaction: reaction})
	return m.err
}

// --- Reactor tests ---

func TestNewAction_ReactsToTopLevelComments(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)

	fetcher.comments = []github.Comment{
		{ID: 10, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 20, Path: "main.go", Body: "reply", User: "bot", InReplyTo: 10},
		{ID: 30, Path: "utils.go", Body: "Add tests", User: "reviewer"},
	}
	reactor := &mockCommentReactor{}
	cfg.Reactor = reactor

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only react to top-level comments (IDs 10 and 30), not reply (ID 20)
	if len(reactor.calls) != 2 {
		t.Fatalf("expected 2 reaction calls, got %d", len(reactor.calls))
	}
	if reactor.calls[0].commentID != 10 {
		t.Errorf("expected first reaction on comment 10, got %d", reactor.calls[0].commentID)
	}
	if reactor.calls[1].commentID != 30 {
		t.Errorf("expected second reaction on comment 30, got %d", reactor.calls[1].commentID)
	}
	for i, call := range reactor.calls {
		if call.reaction != "eyes" {
			t.Errorf("call %d: expected reaction 'eyes', got %q", i, call.reaction)
		}
		if call.owner != "owner" {
			t.Errorf("call %d: expected owner 'owner', got %q", i, call.owner)
		}
		if call.repo != "repo" {
			t.Errorf("call %d: expected repo 'repo', got %q", i, call.repo)
		}
	}
}

func TestNewAction_ReactsBeforeAIInvocation(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)

	var order []string
	reactor := &mockCommentReactor{}
	cfg.Reactor = reactor
	cfg.Invoker = &mockInvoker{
		response: "done",
	}

	// Wrap the invoker to track call order
	origInvoker := cfg.Invoker
	cfg.Invoker = &orderTrackingInvoker{
		inner:    origInvoker,
		orderLog: &order,
	}
	// Wrap the reactor to track call order
	cfg.Reactor = &orderTrackingReactor{
		inner:    reactor,
		orderLog: &order,
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify reactions happen before AI invocation
	reactIdx := -1
	invokeIdx := -1
	for i, entry := range order {
		if entry == "react" && reactIdx < 0 {
			reactIdx = i
		}
		if entry == "invoke" {
			invokeIdx = i
		}
	}
	if reactIdx < 0 {
		t.Fatal("expected at least one reaction call")
	}
	if invokeIdx < 0 {
		t.Fatal("expected AI invocation call")
	}
	if reactIdx >= invokeIdx {
		t.Errorf("expected reactions (index %d) before AI invocation (index %d)", reactIdx, invokeIdx)
	}
}

type orderTrackingInvoker struct {
	inner    invoker.EventInvoker
	orderLog *[]string
}

func (o *orderTrackingInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	*o.orderLog = append(*o.orderLog, "invoke")
	return o.inner.InvokeWithEvents(ctx, prompt, dir, maxTurns, handler)
}

type orderTrackingReactor struct {
	inner    CommentReactor
	orderLog *[]string
}

func (o *orderTrackingReactor) ReactToReviewComment(ctx context.Context, owner, repo string, commentID int64, reaction string) error {
	*o.orderLog = append(*o.orderLog, "react")
	return o.inner.ReactToReviewComment(ctx, owner, repo, commentID, reaction)
}

func TestNewAction_ReactionError_NonFatal(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

	reactor := &mockCommentReactor{err: errors.New("github 500")}
	cfg.Reactor = reactor

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error despite reaction failure, got: %v", err)
	}

	// AI should still be invoked despite reaction errors
	if inv.lastPrompt == "" {
		t.Fatal("expected AI to be invoked even after reaction errors")
	}
}

func TestNewAction_NilReactor_Safe(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)
	cfg.Reactor = nil

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error with nil reactor, got: %v", err)
	}

	// AI should still be invoked
	if inv.lastPrompt == "" {
		t.Fatal("expected AI to be invoked with nil reactor")
	}
}

func TestIsAddressingFeedback_CorrectState(t *testing.T) {
	issue := db.Issue{State: "addressing_feedback"}
	if !IsAddressingFeedback(issue) {
		t.Error("expected true for addressing_feedback state")
	}
}

func TestIsAddressingFeedback_WrongState(t *testing.T) {
	issue := db.Issue{State: "in_review"}
	if IsAddressingFeedback(issue) {
		t.Error("expected false for in_review state")
	}
}

// --- Review body and issue comment tests ---

func TestNewAction_ReviewBody_IncludedInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil // no line comments

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "Please fix the naming convention", User: "reviewer"},
		},
	}
	cfg.PRCommenter = &mockPRCommenter{}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}
	if !strings.Contains(inv.lastPrompt, "Please fix the naming convention") {
		t.Error("expected prompt to contain review body text")
	}
	if !strings.Contains(inv.lastPrompt, "General feedback") {
		t.Error("expected prompt to contain 'General feedback' for non-line comment")
	}
}

func TestNewAction_IssueComment_IncludedInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil // no line comments

	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "The tests are flaky on CI", User: "reviewer"},
		},
	}
	cfg.PRCommenter = &mockPRCommenter{}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}
	if !strings.Contains(inv.lastPrompt, "The tests are flaky on CI") {
		t.Error("expected prompt to contain issue comment body")
	}
}

func TestNewAction_OnlyReviewBody_ProcessedCorrectly(t *testing.T) {
	// This is the original bug case: review with only a body, no line comments.
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)
	fetcher.comments = nil // no line comments

	prCommenter := &mockPRCommenter{}
	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Please refactor the auth module", User: "reviewer"},
		},
	}
	cfg.PRCommenter = prCommenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// AI should be invoked with the review body
	if !strings.Contains(inv.lastPrompt, "Please refactor the auth module") {
		t.Error("expected AI to receive review body in prompt")
	}

	// Should NOT use PostReviewReply (no inline comments)
	if len(replier.calls) != 0 {
		t.Errorf("expected 0 review reply calls, got %d", len(replier.calls))
	}

	// Should use PostPRComment for the reply
	if len(prCommenter.calls) != 1 {
		t.Fatalf("expected 1 PR comment call, got %d", len(prCommenter.calls))
	}
	if prCommenter.calls[0].prNumber != 10 {
		t.Errorf("expected prNumber 10, got %d", prCommenter.calls[0].prNumber)
	}
}

func TestNewAction_MixedSources_AllIncluded(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)

	// Line comment
	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this line", User: "reviewer"},
	}
	// Review body
	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "Overall architecture needs work", User: "reviewer"},
		},
	}
	// Issue comment
	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "CI is broken", User: "tester"},
		},
	}
	prCommenter := &mockPRCommenter{}
	cfg.PRCommenter = prCommenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All three should appear in the prompt
	if !strings.Contains(inv.lastPrompt, "Fix this line") {
		t.Error("expected prompt to contain line comment")
	}
	if !strings.Contains(inv.lastPrompt, "Overall architecture needs work") {
		t.Error("expected prompt to contain review body")
	}
	if !strings.Contains(inv.lastPrompt, "CI is broken") {
		t.Error("expected prompt to contain issue comment")
	}

	// Line comment replied via PostReviewReply
	if len(replier.calls) != 1 {
		t.Fatalf("expected 1 review reply call, got %d", len(replier.calls))
	}
	if replier.calls[0].commentID != 1 {
		t.Errorf("expected reply to comment 1, got %d", replier.calls[0].commentID)
	}

	// Review body + issue comment consolidated into a single PR comment
	if len(prCommenter.calls) != 1 {
		t.Fatalf("expected 1 consolidated PR comment call, got %d", len(prCommenter.calls))
	}
	// The single comment should contain content from both non-inline sources
	body := prCommenter.calls[0].body
	if !strings.Contains(body, "---") {
		t.Error("expected PR comment to contain '---' separator between non-inline responses")
	}
}

func TestNewAction_MultipleNonInline_SingleConsolidatedComment(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, replier, _ := defaultMocks(project)
	fetcher.comments = nil // no line comments

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix naming convention", User: "reviewer1"},
			{ID: 101, State: "COMMENTED", Body: "Add more tests", User: "reviewer2"},
		},
	}
	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "CI is broken", User: "tester"},
		},
	}
	prCommenter := &mockPRCommenter{}
	cfg.PRCommenter = prCommenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No inline replies
	if len(replier.calls) != 0 {
		t.Errorf("expected 0 review reply calls, got %d", len(replier.calls))
	}

	// All three non-inline items consolidated into exactly 1 PR comment
	if len(prCommenter.calls) != 1 {
		t.Fatalf("expected 1 consolidated PR comment call, got %d", len(prCommenter.calls))
	}

	// The single comment should contain separators between responses
	body := prCommenter.calls[0].body
	if strings.Count(body, "---") < 2 {
		t.Errorf("expected at least 2 '---' separators for 3 non-inline items, got body: %s", body)
	}
}

func TestNewAction_OnlyInlineComments_NoPRComment(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, replier, _ := defaultMocks(project)

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 2, Path: "utils.go", Body: "Add tests", User: "reviewer"},
	}
	prCommenter := &mockPRCommenter{}
	cfg.PRCommenter = prCommenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inline comments replied individually
	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 review reply calls, got %d", len(replier.calls))
	}

	// No PR comment should be posted when all items are inline
	if len(prCommenter.calls) != 0 {
		t.Errorf("expected 0 PR comment calls for inline-only feedback, got %d", len(prCommenter.calls))
	}
}

func TestNewAction_BotReviewBody_Filtered(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "Automated review", User: "ci-bot[bot]"},
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Bot review should be filtered, so no AI invocation (no feedback at all)
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked for bot-only reviews")
	}
}

func TestNewAction_EmptyReviewBody_Filtered(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "", User: "reviewer"},
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty body should be filtered
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked for empty review body")
	}
}

func TestNewAction_ApprovedReviewBody_Filtered(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "APPROVED", Body: "LGTM!", User: "reviewer"},
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// APPROVED reviews should be filtered (not actionable)
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked for APPROVED reviews")
	}
}

func TestNewAction_BotIssueComment_Filtered(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "Addressed in abc1234", User: "autoralph[bot]"},
		},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Bot issue comments should be filtered
	if inv.lastPrompt != "" {
		t.Error("expected AI not to be invoked for bot-only issue comments")
	}
}

func TestNewAction_FetchReviewsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{err: errors.New("github 500")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetching PR reviews") {
		t.Errorf("expected 'fetching PR reviews' in error, got: %s", err.Error())
	}
}

func TestNewAction_FetchIssueCommentsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.IssueComments = &mockIssueCommentFetcher{err: errors.New("github 500")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetching PR issue comments") {
		t.Errorf("expected 'fetching PR issue comments' in error, got: %s", err.Error())
	}
}

func TestNewAction_PRCommentError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "Fix this", User: "reviewer"},
		},
	}
	cfg.PRCommenter = &mockPRCommenter{err: errors.New("github 403")}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "posting consolidated PR comment") {
		t.Errorf("expected 'posting consolidated PR comment' in error, got: %s", err.Error())
	}
}

func TestNewAction_IssueCommentReaction(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)
	fetcher.comments = nil

	issueReactor := &mockIssueCommentReactor{}
	cfg.IssueReactor = issueReactor
	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "Please fix the flaky test", User: "reviewer"},
		},
	}
	cfg.PRCommenter = &mockPRCommenter{}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issueReactor.calls) != 1 {
		t.Fatalf("expected 1 issue comment reaction, got %d", len(issueReactor.calls))
	}
	if issueReactor.calls[0].commentID != 200 {
		t.Errorf("expected reaction on comment 200, got %d", issueReactor.calls[0].commentID)
	}
	if issueReactor.calls[0].reaction != "eyes" {
		t.Errorf("expected 'eyes' reaction, got %q", issueReactor.calls[0].reaction)
	}
}

func TestNewAction_NilOptionalInterfaces_Safe(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)
	// All new optional interfaces are nil — should still work with just line comments
	cfg.Reviews = nil
	cfg.IssueComments = nil
	cfg.PRCommenter = nil
	cfg.IssueReactor = nil

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI to be invoked with just line comments")
	}
}

func TestNewAction_NoLineComments_ReviewBodyOnly_NoReviewReply(t *testing.T) {
	// Verifies that review body items don't attempt PostReviewReply
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, replier, _ := defaultMocks(project)
	fetcher.comments = nil

	prCommenter := &mockPRCommenter{}
	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "General feedback", User: "reviewer"},
		},
	}
	cfg.PRCommenter = prCommenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No inline replies
	if len(replier.calls) != 0 {
		t.Errorf("expected 0 review reply calls for review body, got %d", len(replier.calls))
	}
	// One general PR comment
	if len(prCommenter.calls) != 1 {
		t.Errorf("expected 1 PR comment call, got %d", len(prCommenter.calls))
	}
}

func TestNewAction_NilPRCommenter_SkipsGeneralReply(t *testing.T) {
	// When PRCommenter is nil, non-inline feedback is processed but no reply is posted.
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)
	fetcher.comments = nil

	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "General feedback", User: "reviewer"},
		},
	}
	cfg.PRCommenter = nil // no PR commenter

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// AI should still be invoked
	if inv.lastPrompt == "" {
		t.Fatal("expected AI to be invoked")
	}
	// No replies of any kind
	if len(replier.calls) != 0 {
		t.Errorf("expected 0 review reply calls, got %d", len(replier.calls))
	}
}

func TestNewAction_FeedbackCountIncludesAllSources(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, fetcher, _, _ := defaultMocks(project)

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "line comment", User: "reviewer"},
	}
	cfg.Reviews = &mockReviewFetcher{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "review body", User: "reviewer"},
		},
	}
	cfg.IssueComments = &mockIssueCommentFetcher{
		comments: []github.Comment{
			{ID: 200, Body: "issue comment", User: "tester"},
		},
	}
	cfg.PRCommenter = &mockPRCommenter{}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	activities, err := d.ListActivity(issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}

	for _, a := range activities {
		if a.EventType == "feedback_finish" {
			if !strings.Contains(a.Detail, "3 comment") {
				t.Errorf("expected detail to mention 3 comments (all sources), got: %s", a.Detail)
			}
			return
		}
	}
	t.Error("expected feedback_finish activity")
}

func TestNewAction_IncludesKnowledgePath(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)
	issue := createTestIssue(t, d, p)

	invoker := &mockInvoker{response: "Addressed feedback"}
	comments := &mockCommentFetcher{comments: []github.Comment{
		{ID: 1, Path: "main.go", User: "reviewer", Body: "Fix this"},
	}}

	action := NewAction(Config{
		Invoker:      invoker,
		Comments:     comments,
		Replier:      &mockReviewReplier{},
		Git:          &mockGitOps{},
		BranchPuller: &mockBranchPuller{},
		Projects:     d,
		ConfigLoad:   &mockConfigLoader{cfg: &config.Config{}},
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The knowledge path is computed from workspace.TreePath(project.LocalPath, issue.WorkspaceName)
	if !strings.Contains(invoker.lastPrompt, ".ralph/knowledge") {
		t.Error("expected prompt to contain knowledge path")
	}
}

// --- BranchPuller tests ---

func TestNewAction_PullsBranchBeforeAIInvocation(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)

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
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, git := defaultMocks(project)

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
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _ := defaultMocks(project)

	var order []string
	puller := &mockBranchPuller{}
	cfg.BranchPuller = &orderTrackingPuller{inner: puller, orderLog: &order}
	cfg.Invoker = &orderTrackingInvoker{inner: cfg.Invoker, orderLog: &order}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pullIdx := -1
	invokeIdx := -1
	for i, entry := range order {
		if entry == "pull" && pullIdx < 0 {
			pullIdx = i
		}
		if entry == "invoke" && invokeIdx < 0 {
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

// --- buildReplyForComment tests ---

func TestBuildReplyForComment_CommitRef_WithSection(t *testing.T) {
	aiResponse := "### main.go\n**Action:** changed\n**Response:** Added error handling for nil pointer"
	got := buildReplyForComment(aiResponse, "main.go", "abc1234")
	if !strings.Contains(got, "abc1234") {
		t.Errorf("expected commit SHA in reply, got: %s", got)
	}
	if !strings.Contains(got, "Added error handling") {
		t.Errorf("expected AI explanation in reply, got: %s", got)
	}
}

func TestBuildReplyForComment_CommitRef_NoSection(t *testing.T) {
	got := buildReplyForComment("unstructured response", "main.go", "abc1234")
	if got != "Addressed in abc1234" {
		t.Errorf("expected 'Addressed in abc1234', got: %s", got)
	}
}

func TestBuildReplyForComment_NoCommit_GeneralFeedback_ExtractsSection(t *testing.T) {
	aiResponse := "### General feedback\n**Action:** no_change\n**Response:** The naming convention is already consistent with the project style guide."
	got := buildReplyForComment(aiResponse, "", "")
	if !strings.Contains(got, "naming convention is already consistent") {
		t.Errorf("expected AI explanation for general feedback, got: %s", got)
	}
}

func TestBuildReplyForComment_NoCommit_NoSection_FallsBackToFullResponse(t *testing.T) {
	aiResponse := "I reviewed the feedback and the code is correct as-is because the tests cover this edge case."
	got := buildReplyForComment(aiResponse, "", "")
	if !strings.Contains(got, "reviewed the feedback") {
		t.Errorf("expected full AI response as fallback, got: %s", got)
	}
}

func TestBuildReplyForComment_NoCommit_EmptyAIResponse_FallsBackToCanned(t *testing.T) {
	got := buildReplyForComment("", "", "")
	if got != "Reviewed — no code changes needed." {
		t.Errorf("expected canned message for empty AI response, got: %s", got)
	}
}

func TestBuildReplyForComment_NoCommit_PathSpecific_ExtractsSection(t *testing.T) {
	aiResponse := "### main.go\n**Action:** no_change\n**Response:** The function already handles this case on line 42."
	got := buildReplyForComment(aiResponse, "main.go", "")
	if !strings.Contains(got, "already handles this case") {
		t.Errorf("expected AI explanation for file-specific feedback, got: %s", got)
	}
}

func TestNewAction_PassesMaxTurns(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _ := defaultMocks(project)

	action := NewAction(cfg)
	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastMaxTurns != maxTurnsFeedback {
		t.Errorf("expected maxTurns %d, got %d", maxTurnsFeedback, inv.lastMaxTurns)
	}
}

func TestBuildReplyForComment_LongAIResponse_Truncated(t *testing.T) {
	longResponse := strings.Repeat("x", 2000)
	got := buildReplyForComment(longResponse, "", "")
	if len(got) > 1100 {
		t.Errorf("expected truncated response, got length %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("expected truncation marker at end")
	}
}

// --- groupThreads tests ---

func TestGroupThreads_NoComments(t *testing.T) {
	result := groupThreads(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestGroupThreads_TopLevelOnly(t *testing.T) {
	comments := []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 2, Path: "utils.go", Body: "Add tests", User: "other"},
	}
	result := groupThreads(comments)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].id != 1 || result[1].id != 2 {
		t.Errorf("unexpected IDs: %d, %d", result[0].id, result[1].id)
	}
	if len(result[0].replies) != 0 {
		t.Errorf("expected 0 replies for first item, got %d", len(result[0].replies))
	}
	if len(result[1].replies) != 0 {
		t.Errorf("expected 0 replies for second item, got %d", len(result[1].replies))
	}
}

func TestGroupThreads_RepliesAttachedToParent(t *testing.T) {
	comments := []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 2, Path: "main.go", Body: "I agree with above", User: "dev", InReplyTo: 1},
		{ID: 3, Path: "main.go", Body: "Done, fixed it", User: "author", InReplyTo: 1},
		{ID: 4, Path: "utils.go", Body: "Add tests", User: "reviewer"},
	}
	result := groupThreads(comments)
	if len(result) != 2 {
		t.Fatalf("expected 2 top-level items, got %d", len(result))
	}

	// First item should have 2 replies.
	if len(result[0].replies) != 2 {
		t.Fatalf("expected 2 replies for comment 1, got %d", len(result[0].replies))
	}
	if result[0].replies[0].author != "dev" || result[0].replies[0].body != "I agree with above" {
		t.Errorf("unexpected first reply: %+v", result[0].replies[0])
	}
	if result[0].replies[1].author != "author" || result[0].replies[1].body != "Done, fixed it" {
		t.Errorf("unexpected second reply: %+v", result[0].replies[1])
	}

	// Second item should have no replies.
	if len(result[1].replies) != 0 {
		t.Errorf("expected 0 replies for comment 4, got %d", len(result[1].replies))
	}
}

func TestGroupThreads_OrphanReplyIgnored(t *testing.T) {
	comments := []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 5, Path: "main.go", Body: "orphan reply", User: "someone", InReplyTo: 999},
	}
	result := groupThreads(comments)
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level item, got %d", len(result))
	}
	if len(result[0].replies) != 0 {
		t.Errorf("expected 0 replies (orphan discarded), got %d", len(result[0].replies))
	}
}

// --- Integration test: reply threads in AI prompt ---

func TestNewAction_ReplyThreadsInPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)

	reactor := &mockCommentReactor{}
	cfg.Reactor = reactor

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix the null check", User: "reviewer"},
		{ID: 2, Path: "main.go", Body: "I think this is related to issue #42", User: "other-dev", InReplyTo: 1},
		{ID: 3, Path: "main.go", Body: "Yes, same root cause", User: "reviewer", InReplyTo: 1},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parent comment body should be in prompt.
	if !strings.Contains(inv.lastPrompt, "Fix the null check") {
		t.Error("expected parent comment body in prompt")
	}
	// Both reply bodies should appear in prompt.
	if !strings.Contains(inv.lastPrompt, "I think this is related to issue #42") {
		t.Error("expected first reply body in prompt")
	}
	if !strings.Contains(inv.lastPrompt, "Yes, same root cause") {
		t.Error("expected second reply body in prompt")
	}

	// Only 1 reply call (to parent comment ID 1, not to reply IDs 2,3).
	if len(replier.calls) != 1 {
		t.Fatalf("expected 1 reply call (parent only), got %d", len(replier.calls))
	}
	if replier.calls[0].commentID != 1 {
		t.Errorf("expected reply to comment 1, got %d", replier.calls[0].commentID)
	}

	// Only 1 reaction call (to parent comment ID 1).
	if len(reactor.calls) != 1 {
		t.Fatalf("expected 1 reaction call, got %d", len(reactor.calls))
	}
	if reactor.calls[0].commentID != 1 {
		t.Errorf("expected reaction on comment 1, got %d", reactor.calls[0].commentID)
	}
}

// --- annotateTrust unit tests ---

func TestAnnotateTrust_MarksMatchingAuthor(t *testing.T) {
	items := []feedbackItem{
		{author: "trusted-reviewer", body: "fix this"},
		{author: "other-dev", body: "looks good"},
	}
	annotateTrust(items, "trusted-reviewer")
	if !items[0].isTrusted {
		t.Error("expected trusted-reviewer to be marked trusted")
	}
	if items[1].isTrusted {
		t.Error("expected other-dev NOT to be marked trusted")
	}
}

func TestAnnotateTrust_CaseInsensitive(t *testing.T) {
	items := []feedbackItem{
		{author: "Trusted-Reviewer", body: "fix this"},
	}
	annotateTrust(items, "trusted-reviewer")
	if !items[0].isTrusted {
		t.Error("expected case-insensitive match to be trusted")
	}
}

func TestAnnotateTrust_EmptyTrustedUser_NoAnnotation(t *testing.T) {
	items := []feedbackItem{
		{author: "reviewer", body: "fix this"},
	}
	annotateTrust(items, "")
	if items[0].isTrusted {
		t.Error("expected no trust annotation when TrustedUser is empty")
	}
}

func TestAnnotateTrust_NoMatchingAuthor(t *testing.T) {
	items := []feedbackItem{
		{author: "other-dev", body: "fix this"},
	}
	annotateTrust(items, "trusted-reviewer")
	if items[0].isTrusted {
		t.Error("expected non-matching author NOT to be trusted")
	}
}

// --- Integration test: trusted annotation in AI prompt (IT-002) ---

func TestNewAction_TrustedUserAnnotation_InPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	cfg.TrustedUser = "trusted-reviewer"

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix the null check", User: "trusted-reviewer"},
		{ID: 2, Path: "utils.go", Body: "Add tests", User: "other-dev"},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Trusted reviewer should have "(trusted)" marker in prompt.
	if !strings.Contains(inv.lastPrompt, "(trusted)") {
		t.Error("expected prompt to contain '(trusted)' for trusted reviewer")
	}

	// The trusted marker should appear next to the trusted reviewer's name.
	if !strings.Contains(inv.lastPrompt, "trusted-reviewer") {
		t.Error("expected prompt to contain trusted reviewer name")
	}

	// The untrusted reviewer should NOT have "(trusted)" next to their name.
	// Check that "other-dev" and "(trusted)" do NOT appear together.
	otherDevIdx := strings.Index(inv.lastPrompt, "other-dev")
	if otherDevIdx < 0 {
		t.Fatal("expected prompt to contain 'other-dev'")
	}
	// Look at the text around other-dev's entry — it should not have "(trusted)".
	otherDevSection := inv.lastPrompt[otherDevIdx : otherDevIdx+50]
	if strings.Contains(otherDevSection, "(trusted)") {
		t.Error("expected '(trusted)' NOT to appear next to 'other-dev'")
	}

	// Prompt should contain guidance about prioritizing trusted feedback.
	if !strings.Contains(inv.lastPrompt, "prioritize trusted") {
		t.Error("expected prompt to contain trusted prioritization guidance")
	}
}

// --- Integration test: no trust annotation when TrustedUser is empty (IT-005) ---

func TestNewAction_NoTrustAnnotation_WhenTrustedUserEmpty(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, _, _ := defaultMocks(project)
	cfg.TrustedUser = "" // empty — no trust annotation

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer-a"},
		{ID: 2, Path: "utils.go", Body: "Add tests", User: "reviewer-b"},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No "(trusted)" should appear in the prompt.
	if strings.Contains(inv.lastPrompt, "(trusted)") {
		t.Error("expected NO '(trusted)' annotations when TrustedUser is empty")
	}

	// All comments should still be present.
	if !strings.Contains(inv.lastPrompt, "Fix this") {
		t.Error("expected first comment body in prompt")
	}
	if !strings.Contains(inv.lastPrompt, "Add tests") {
		t.Error("expected second comment body in prompt")
	}

	// Should NOT contain trusted prioritization guidance.
	if strings.Contains(inv.lastPrompt, "prioritize trusted") {
		t.Error("expected NO trusted prioritization guidance when TrustedUser is empty")
	}
}

func TestNewAction_NoReplies_BackwardCompatible(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, fetcher, replier, _ := defaultMocks(project)

	fetcher.comments = []github.Comment{
		{ID: 1, Path: "main.go", Body: "Fix this", User: "reviewer"},
		{ID: 2, Path: "utils.go", Body: "Add tests", User: "reviewer"},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both parent comments in prompt.
	if !strings.Contains(inv.lastPrompt, "Fix this") {
		t.Error("expected first comment body in prompt")
	}
	if !strings.Contains(inv.lastPrompt, "Add tests") {
		t.Error("expected second comment body in prompt")
	}

	// 2 reply calls for 2 top-level comments.
	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls, got %d", len(replier.calls))
	}
}
