package feedback

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
	lastPrompt string
	response   string
	err        error
}

func (m *mockInvoker) Invoke(_ context.Context, prompt, dir string) (string, error) {
	m.lastPrompt = prompt
	return m.response, m.err
}

type mockCommentFetcher struct {
	comments []github.Comment
	err      error
}

func (m *mockCommentFetcher) FetchPRComments(_ context.Context, _, _ string, _ int) ([]github.Comment, error) {
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
		Invoker:  inv,
		Comments: fetcher,
		Replier:  replier,
		Git:      git,
		Projects: projGetter,
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
		if a.EventType == "feedback_addressed" {
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
		t.Error("expected feedback_addressed activity")
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

	// Replies should not reference a commit SHA
	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls, got %d", len(replier.calls))
	}
	for _, call := range replier.calls {
		if strings.Contains(call.body, "abc1234") {
			t.Errorf("expected reply NOT to contain commit SHA, got: %s", call.body)
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

func TestNewAction_SkipsReplyComments(t *testing.T) {
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

	if strings.Contains(inv.lastPrompt, "Addressed in abc123") {
		t.Error("expected reply comments to be excluded from AI prompt")
	}

	if len(replier.calls) != 2 {
		t.Fatalf("expected 2 reply calls (skipping reply comment), got %d", len(replier.calls))
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
