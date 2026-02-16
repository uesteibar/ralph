package pr

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
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
		State:         "building",
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
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

type mockGitPusher struct {
	calls []pushCall
	err   error
}

type pushCall struct {
	workDir string
	branch  string
}

func (m *mockGitPusher) PushBranch(_ context.Context, workDir, branch string) error {
	m.calls = append(m.calls, pushCall{workDir: workDir, branch: branch})
	return m.err
}

type mockDiffStatter struct {
	stats string
	err   error
}

func (m *mockDiffStatter) DiffStats(_ context.Context, workDir, base string) (string, error) {
	return m.stats, m.err
}

type mockPRDReader struct {
	info PRDInfo
	err  error
	path string
}

func (m *mockPRDReader) ReadPRD(path string) (PRDInfo, error) {
	m.path = path
	return m.info, m.err
}

type mockGitHubPRCreator struct {
	result    PRResult
	err       error
	calls     []ghCreateCall
	findPR    *PRResult
	findError error
}

type ghCreateCall struct {
	owner, repo, head, base, title, body string
}

func (m *mockGitHubPRCreator) CreatePullRequest(_ context.Context, owner, repo, head, base, title, body string) (PRResult, error) {
	m.calls = append(m.calls, ghCreateCall{owner: owner, repo: repo, head: head, base: base, title: title, body: body})
	return m.result, m.err
}

func (m *mockGitHubPRCreator) FindOpenPR(_ context.Context, owner, repo, head, base string) (*PRResult, error) {
	return m.findPR, m.findError
}

type mockRebaser struct {
	fetchErr     error
	rebaseResult RebaseResult
	rebaseErr    error
	abortErr     error
	conflicts    []string
	conflictsErr error
}

func (m *mockRebaser) FetchBranch(_ context.Context, workDir, branch string) error {
	return m.fetchErr
}

func (m *mockRebaser) StartRebase(_ context.Context, workDir, onto string) (RebaseResult, error) {
	return m.rebaseResult, m.rebaseErr
}

func (m *mockRebaser) AbortRebase(_ context.Context, workDir string) error {
	return m.abortErr
}

func (m *mockRebaser) ConflictFiles(_ context.Context, workDir string) ([]string, error) {
	return m.conflicts, m.conflictsErr
}

type mockLinearPoster struct {
	calls     []linearPostCall
	commentID string
	err       error
}

type linearPostCall struct {
	issueID string
	body    string
}

func (m *mockLinearPoster) PostComment(_ context.Context, linearIssueID, body string) (string, error) {
	m.calls = append(m.calls, linearPostCall{issueID: linearIssueID, body: body})
	return m.commentID, m.err
}

type mockConfigLoader struct {
	base string
	err  error
}

func (m *mockConfigLoader) DefaultBase(localPath, configPath string) (string, error) {
	return m.base, m.err
}

func defaultConfig() (Config, *mockInvoker, *mockGitPusher, *mockDiffStatter, *mockPRDReader, *mockGitHubPRCreator, *mockLinearPoster, *mockConfigLoader) {
	inv := &mockInvoker{response: "feat(avatars): add user avatar upload\n\n## Summary\nAdds avatar upload support so users can personalize their profiles.\n\n## Technical Architecture\nNew upload endpoint stores images in S3 via the storage service.\n\n## Key Design Decisions\nChose S3 over local storage for scalability.\n\n## Testing\n- Unit tests added"}
	git := &mockGitPusher{}
	diff := &mockDiffStatter{stats: " 3 files changed, 120 insertions(+), 5 deletions(-)"}
	prdReader := &mockPRDReader{info: PRDInfo{
		Description: "Add user avatar functionality",
		Stories: []StoryInfo{
			{ID: "US-001", Title: "Avatar upload"},
			{ID: "US-002", Title: "Avatar display"},
		},
	}}
	gh := &mockGitHubPRCreator{result: PRResult{Number: 42, HTMLURL: "https://github.com/owner/repo/pull/42"}}
	linear := &mockLinearPoster{commentID: "comment-abc"}
	cfgLoader := &mockConfigLoader{base: "main"}

	cfg := Config{
		Invoker:    inv,
		Git:        git,
		Diff:       diff,
		PRD:        prdReader,
		GitHub:     gh,
		Linear:     linear,
		ConfigLoad: cfgLoader,
	}
	return cfg, inv, git, diff, prdReader, gh, linear, cfgLoader
}

// --- Tests ---

func TestNewAction_PushesBranch(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, git, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(git.calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(git.calls))
	}

	expectedWorkDir := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if git.calls[0].workDir != expectedWorkDir {
		t.Errorf("expected workDir %q, got %q", expectedWorkDir, git.calls[0].workDir)
	}
	if git.calls[0].branch != "autoralph/proj-42" {
		t.Errorf("expected branch %q, got %q", "autoralph/proj-42", git.calls[0].branch)
	}
}

func TestNewAction_InvokesAIWithPRPrompt(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastPrompt == "" {
		t.Fatal("expected AI prompt to be set")
	}
	if !strings.Contains(inv.lastPrompt, "Add user avatar functionality") {
		t.Errorf("expected prompt to contain PRD summary, got: %s", inv.lastPrompt[:200])
	}
	if !strings.Contains(inv.lastPrompt, "US-001") {
		t.Errorf("expected prompt to contain story ID")
	}
	if !strings.Contains(inv.lastPrompt, "3 files changed") {
		t.Errorf("expected prompt to contain diff stats")
	}
}

func TestNewAction_CreatesGitHubPR(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}
	call := gh.calls[0]
	if call.owner != "owner" {
		t.Errorf("expected owner %q, got %q", "owner", call.owner)
	}
	if call.repo != "repo" {
		t.Errorf("expected repo %q, got %q", "repo", call.repo)
	}
	if call.head != "autoralph/proj-42" {
		t.Errorf("expected head %q, got %q", "autoralph/proj-42", call.head)
	}
	if call.base != "main" {
		t.Errorf("expected base %q, got %q", "main", call.base)
	}
	if call.title != "feat(avatars): add user avatar upload" {
		t.Errorf("expected title %q, got %q", "feat(avatars): add user avatar upload", call.title)
	}
	if !strings.Contains(call.body, "## Summary") {
		t.Errorf("expected body to contain ## Summary, got: %s", call.body)
	}
}

func TestNewAction_StoresPRInfoInDB(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, _, _ := defaultConfig()
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
	if updated.PRNumber != 42 {
		t.Errorf("expected PRNumber = 42, got %d", updated.PRNumber)
	}
	if updated.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("expected PRURL = %q, got %q", "https://github.com/owner/repo/pull/42", updated.PRURL)
	}
}

func TestNewAction_PostsLinearComment(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, linear, _ := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(linear.calls) != 1 {
		t.Fatalf("expected 1 Linear comment call, got %d", len(linear.calls))
	}
	if linear.calls[0].issueID != "lin-123" {
		t.Errorf("expected Linear issue ID %q, got %q", "lin-123", linear.calls[0].issueID)
	}
	if !strings.Contains(linear.calls[0].body, "#42") {
		t.Errorf("expected comment to contain PR number, got: %s", linear.calls[0].body)
	}
	if !strings.Contains(linear.calls[0].body, "https://github.com/owner/repo/pull/42") {
		t.Errorf("expected comment to contain PR URL")
	}
}

func TestNewAction_UpdatesLastCommentID(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, _, _ := defaultConfig()
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
	if updated.LastCommentID != "comment-abc" {
		t.Errorf("expected LastCommentID = %q, got %q", "comment-abc", updated.LastCommentID)
	}
}

func TestNewAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

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
		if a.EventType == "pr_created" {
			found = true
			if !strings.Contains(a.Detail, "#42") {
				t.Errorf("expected detail to contain PR number, got: %s", a.Detail)
			}
		}
	}
	if !found {
		t.Error("expected pr_created activity")
	}
}

func TestNewAction_PushError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, git, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d
	git.err = errors.New("push rejected")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pushing branch") {
		t.Errorf("expected 'pushing branch' in error, got: %s", err.Error())
	}
}

func TestNewAction_AIError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d
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

func TestNewAction_GitHubPRError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d
	gh.err = errors.New("422 validation failed")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "creating GitHub PR") {
		t.Errorf("expected 'creating GitHub PR' in error, got: %s", err.Error())
	}
}

func TestNewAction_LinearPostError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, linear, _ := defaultConfig()
	cfg.Projects = d
	linear.err = errors.New("linear 500")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "posting PR link to Linear") {
		t.Errorf("expected 'posting PR link to Linear' in error, got: %s", err.Error())
	}
}

func TestNewAction_PRDReadError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, prdReader, _, _, _ := defaultConfig()
	cfg.Projects = d
	prdReader.err = errors.New("prd not found")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "reading PRD") {
		t.Errorf("expected 'reading PRD' in error, got: %s", err.Error())
	}
}

func TestNewAction_ConfigLoadError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, _, cfgLoader := defaultConfig()
	cfg.Projects = d
	cfgLoader.err = errors.New("config not found")

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading default base") {
		t.Errorf("expected 'loading default base' in error, got: %s", err.Error())
	}
}

func TestNewAction_DiffStatsErrorFallback(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, inv, _, diff, _, _, _, _ := defaultConfig()
	cfg.Projects = d
	diff.err = errors.New("no upstream")

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(inv.lastPrompt, "(diff stats unavailable)") {
		t.Error("expected fallback diff stats message in prompt")
	}
}

func TestNewAction_ReadsPRDFromCorrectPath(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, prdReader, _, _, _ := defaultConfig()
	cfg.Projects = d

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "prd.json")
	if prdReader.path != expectedPath {
		t.Errorf("expected PRD path %q, got %q", expectedPath, prdReader.path)
	}
}

func TestNewAction_UsesDefaultBaseFromConfig(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, gh, _, cfgLoader := defaultConfig()
	cfg.Projects = d
	cfgLoader.base = "develop"

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}
	if gh.calls[0].base != "develop" {
		t.Errorf("expected base %q, got %q", "develop", gh.calls[0].base)
	}
}

func TestParsePROutput_TitleAndBody(t *testing.T) {
	input := "feat(auth): add login\n\n## Summary\n- Added login flow"
	title, body := parsePROutput(input)
	if title != "feat(auth): add login" {
		t.Errorf("expected title %q, got %q", "feat(auth): add login", title)
	}
	if !strings.Contains(body, "## Summary") {
		t.Errorf("expected body to contain ## Summary, got: %s", body)
	}
}

func TestParsePROutput_TitleOnly(t *testing.T) {
	title, body := parsePROutput("feat: quick fix")
	if title != "feat: quick fix" {
		t.Errorf("expected title %q, got %q", "feat: quick fix", title)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParsePROutput_TrimsWhitespace(t *testing.T) {
	input := "\n  feat: something  \n\n  body text  \n"
	title, body := parsePROutput(input)
	if title != "feat: something" {
		t.Errorf("expected title %q, got %q", "feat: something", title)
	}
	if body != "body text" {
		t.Errorf("expected body %q, got %q", "body text", body)
	}
}

func TestNewAction_ProjectNotFound(t *testing.T) {
	d := testDB(t)
	_ = createTestProject(t, d)
	cfg, _, _, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

	issue := db.Issue{
		ID:        "nonexistent-issue-id",
		ProjectID: "nonexistent-project-id",
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading project") {
		t.Errorf("expected 'loading project' in error, got: %s", err.Error())
	}
}

func TestNewAction_IdempotentPR_ExistingPRFound(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	// Simulate existing PR found
	gh.findPR = &PRResult{Number: 99, HTMLURL: "https://github.com/owner/repo/pull/99"}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT call CreatePullRequest since PR already exists
	if len(gh.calls) != 0 {
		t.Errorf("expected 0 create PR calls (existing found), got %d", len(gh.calls))
	}

	// Should store the existing PR info
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.PRNumber != 99 {
		t.Errorf("expected PRNumber = 99, got %d", updated.PRNumber)
	}
}

func TestNewAction_MergeConflict_ReturnConflictError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, git, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

	// Push fails
	git.err = errors.New("push rejected")

	// Rebaser configured, rebase has conflicts
	cfg.Rebase = &mockRebaser{
		rebaseResult: RebaseResult{HasConflicts: true},
		conflicts:    []string{"file1.go", "file2.go"},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error")
	}

	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ConflictError, got: %T: %v", err, err)
	}
	if len(conflictErr.Files) != 2 {
		t.Errorf("expected 2 conflict files, got %d", len(conflictErr.Files))
	}
}

func TestNewAction_PushFailsRebaseSucceeds(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)
	cfg, _, _, _, _, _, _, _ := defaultConfig()
	cfg.Projects = d

	// Push fails first, succeeds after rebase
	pushCalls := 0
	failingGit := &mockGitPusher{err: nil}
	failingGit.err = nil
	cfg.Git = &trackingPusher{
		pushFunc: func() error {
			pushCalls++
			if pushCalls == 1 {
				return errors.New("push rejected")
			}
			return nil
		},
	}

	cfg.Rebase = &mockRebaser{
		rebaseResult: RebaseResult{Success: true},
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pushCalls != 2 {
		t.Errorf("expected 2 push calls (first fails, second succeeds after rebase), got %d", pushCalls)
	}
}

// trackingPusher is a custom pusher that allows per-call behavior.
type trackingPusher struct {
	pushFunc func() error
}

func (m *trackingPusher) PushBranch(_ context.Context, workDir, branch string) error {
	return m.pushFunc()
}

