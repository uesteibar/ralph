package rebase

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
		Name:            "test-project",
		LocalPath:       "/tmp/test",
		GithubOwner:     "owner",
		GithubRepo:      "repo",
		RalphConfigPath: ".ralph/ralph.yaml",
		BranchPrefix:    "autoralph/",
		MaxIterations:   20,
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
		State:         "in_review",
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

type mockBranchFetcher struct {
	fetchedBranches []string
	err             error
}

func (m *mockBranchFetcher) FetchBranch(ctx context.Context, workDir, branch string) error {
	m.fetchedBranches = append(m.fetchedBranches, branch)
	return m.err
}

type mockAncestorChecker struct {
	isAncestor bool
	err        error
	calls      []ancestorCall
}

type ancestorCall struct {
	workDir, ancestor, descendant string
}

func (m *mockAncestorChecker) IsAncestor(ctx context.Context, workDir, ancestor, descendant string) (bool, error) {
	m.calls = append(m.calls, ancestorCall{workDir: workDir, ancestor: ancestor, descendant: descendant})
	return m.isAncestor, m.err
}

type mockForcePusher struct {
	pushedBranches []string
	err            error
}

func (m *mockForcePusher) ForcePushBranch(ctx context.Context, workDir, branch string) error {
	m.pushedBranches = append(m.pushedBranches, branch)
	return m.err
}

type mockRebaseRunner struct {
	calls []rebaseCall
	err   error
}

type rebaseCall struct {
	ctx  context.Context
	base string
	ws   string
	cfg  string
}

func (m *mockRebaseRunner) RunRebase(ctx context.Context, base, workspaceName, projectConfigPath string) error {
	m.calls = append(m.calls, rebaseCall{ctx: ctx, base: base, ws: workspaceName, cfg: projectConfigPath})
	return m.err
}

type mockProjectGetter struct {
	project db.Project
	err     error
}

func (m *mockProjectGetter) GetProject(id string) (db.Project, error) {
	return m.project, m.err
}

type mockDefaultBaseResolver struct {
	base string
	err  error
}

func (m *mockDefaultBaseResolver) DefaultBase(projectLocalPath, ralphConfigPath string) (string, error) {
	return m.base, m.err
}

// --- NeedsRebase Tests ---

func TestNeedsRebase_NoPRNumber_ReturnsFalse(t *testing.T) {
	cfg := Config{}
	condition := NeedsRebase(cfg)

	issue := db.Issue{PRNumber: 0, WorkspaceName: "proj-42"}
	if condition(issue) {
		t.Error("expected false when issue has no PR number")
	}
}

func TestNeedsRebase_BranchBehind_ReturnsTrue(t *testing.T) {
	fetcher := &mockBranchFetcher{}
	checker := &mockAncestorChecker{isAncestor: false}
	projects := &mockProjectGetter{project: db.Project{
		LocalPath:       "/tmp/test",
		RalphConfigPath: ".ralph/ralph.yaml",
	}}
	resolver := &mockDefaultBaseResolver{base: "main"}

	cfg := Config{
		Fetcher:  fetcher,
		Checker:  checker,
		Projects: projects,
		Resolver: resolver,
	}
	condition := NeedsRebase(cfg)

	issue := db.Issue{
		PRNumber:      10,
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
		ProjectID:     "proj-1",
	}

	if !condition(issue) {
		t.Error("expected true when branch is behind (IsAncestor returns false)")
	}

	if len(fetcher.fetchedBranches) != 1 || fetcher.fetchedBranches[0] != "main" {
		t.Errorf("expected fetch of 'main', got %v", fetcher.fetchedBranches)
	}

	if len(checker.calls) != 1 {
		t.Fatalf("expected 1 IsAncestor call, got %d", len(checker.calls))
	}
	expectedWorkDir := filepath.Join("/tmp/test", ".ralph", "workspaces", "proj-42", "tree")
	if checker.calls[0].workDir != expectedWorkDir {
		t.Errorf("expected workDir %q, got %q", expectedWorkDir, checker.calls[0].workDir)
	}
	if checker.calls[0].ancestor != "origin/main" {
		t.Errorf("expected ancestor 'origin/main', got %q", checker.calls[0].ancestor)
	}
	if checker.calls[0].descendant != "HEAD" {
		t.Errorf("expected descendant 'HEAD', got %q", checker.calls[0].descendant)
	}
}

func TestNeedsRebase_BranchUpToDate_ReturnsFalse(t *testing.T) {
	fetcher := &mockBranchFetcher{}
	checker := &mockAncestorChecker{isAncestor: true}
	projects := &mockProjectGetter{project: db.Project{
		LocalPath:       "/tmp/test",
		RalphConfigPath: ".ralph/ralph.yaml",
	}}
	resolver := &mockDefaultBaseResolver{base: "main"}

	cfg := Config{
		Fetcher:  fetcher,
		Checker:  checker,
		Projects: projects,
		Resolver: resolver,
	}
	condition := NeedsRebase(cfg)

	issue := db.Issue{
		PRNumber:      10,
		WorkspaceName: "proj-42",
		BranchName:    "autoralph/proj-42",
		ProjectID:     "proj-1",
	}

	if condition(issue) {
		t.Error("expected false when branch is up-to-date (IsAncestor returns true)")
	}
}

func TestNeedsRebase_FetchError_ReturnsFalse(t *testing.T) {
	fetcher := &mockBranchFetcher{err: errors.New("fetch failed")}
	checker := &mockAncestorChecker{}
	projects := &mockProjectGetter{project: db.Project{
		LocalPath:       "/tmp/test",
		RalphConfigPath: ".ralph/ralph.yaml",
	}}
	resolver := &mockDefaultBaseResolver{base: "main"}

	cfg := Config{
		Fetcher:  fetcher,
		Checker:  checker,
		Projects: projects,
		Resolver: resolver,
	}
	condition := NeedsRebase(cfg)

	issue := db.Issue{PRNumber: 10, WorkspaceName: "proj-42", ProjectID: "proj-1"}
	if condition(issue) {
		t.Error("expected false when fetch fails")
	}
}

func TestNeedsRebase_ProjectGetError_ReturnsFalse(t *testing.T) {
	projects := &mockProjectGetter{err: errors.New("not found")}

	cfg := Config{Projects: projects}
	condition := NeedsRebase(cfg)

	issue := db.Issue{PRNumber: 10, WorkspaceName: "proj-42", ProjectID: "proj-1"}
	if condition(issue) {
		t.Error("expected false when project lookup fails")
	}
}

func TestNeedsRebase_DefaultBaseError_ReturnsFalse(t *testing.T) {
	projects := &mockProjectGetter{project: db.Project{
		LocalPath:       "/tmp/test",
		RalphConfigPath: ".ralph/ralph.yaml",
	}}
	resolver := &mockDefaultBaseResolver{err: errors.New("config error")}

	cfg := Config{
		Projects: projects,
		Resolver: resolver,
	}
	condition := NeedsRebase(cfg)

	issue := db.Issue{PRNumber: 10, WorkspaceName: "proj-42", ProjectID: "proj-1"}
	if condition(issue) {
		t.Error("expected false when default base resolution fails")
	}
}

func TestNeedsRebase_AncestorCheckError_ReturnsFalse(t *testing.T) {
	fetcher := &mockBranchFetcher{}
	checker := &mockAncestorChecker{err: errors.New("git error")}
	projects := &mockProjectGetter{project: db.Project{
		LocalPath:       "/tmp/test",
		RalphConfigPath: ".ralph/ralph.yaml",
	}}
	resolver := &mockDefaultBaseResolver{base: "main"}

	cfg := Config{
		Fetcher:  fetcher,
		Checker:  checker,
		Projects: projects,
		Resolver: resolver,
	}
	condition := NeedsRebase(cfg)

	issue := db.Issue{PRNumber: 10, WorkspaceName: "proj-42", ProjectID: "proj-1"}
	if condition(issue) {
		t.Error("expected false when ancestor check fails")
	}
}

// --- NewAction Tests ---

func TestNewAction_Success_RebasesAndForcePushes(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	runner := &mockRebaseRunner{}
	pusher := &mockForcePusher{}
	resolver := &mockDefaultBaseResolver{base: "main"}
	projects := &mockProjectGetter{project: project}

	cfg := Config{
		Runner:   runner,
		Pusher:   pusher,
		Resolver: resolver,
		Projects: projects,
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify rebase was called with correct args
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 rebase call, got %d", len(runner.calls))
	}
	if runner.calls[0].base != "main" {
		t.Errorf("expected base 'main', got %q", runner.calls[0].base)
	}
	if runner.calls[0].ws != "proj-42" {
		t.Errorf("expected workspace 'proj-42', got %q", runner.calls[0].ws)
	}
	expectedCfg := filepath.Join("/tmp/test", ".ralph/ralph.yaml")
	if runner.calls[0].cfg != expectedCfg {
		t.Errorf("expected config path %q, got %q", expectedCfg, runner.calls[0].cfg)
	}

	// Verify force push was called
	if len(pusher.pushedBranches) != 1 {
		t.Fatalf("expected 1 force push, got %d", len(pusher.pushedBranches))
	}
	if pusher.pushedBranches[0] != "autoralph/proj-42" {
		t.Errorf("expected push branch 'autoralph/proj-42', got %q", pusher.pushedBranches[0])
	}
}

func TestNewAction_LogsAutoRebaseActivity(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	runner := &mockRebaseRunner{}
	pusher := &mockForcePusher{}
	resolver := &mockDefaultBaseResolver{base: "main"}
	projects := &mockProjectGetter{project: project}

	cfg := Config{
		Runner:   runner,
		Pusher:   pusher,
		Resolver: resolver,
		Projects: projects,
	}

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
		if a.EventType == "auto_rebase" {
			found = true
			if !strings.Contains(a.Detail, "main") {
				t.Errorf("expected detail to mention base branch, got: %s", a.Detail)
			}
		}
	}
	if !found {
		t.Error("expected auto_rebase activity to be logged")
	}
}

func TestNewAction_RebaseError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	runner := &mockRebaseRunner{err: errors.New("unresolvable conflicts")}
	pusher := &mockForcePusher{}
	resolver := &mockDefaultBaseResolver{base: "main"}
	projects := &mockProjectGetter{project: project}

	cfg := Config{
		Runner:   runner,
		Pusher:   pusher,
		Resolver: resolver,
		Projects: projects,
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when rebase fails")
	}
	if !strings.Contains(err.Error(), "running rebase") {
		t.Errorf("expected 'running rebase' in error, got: %s", err.Error())
	}

	// Force push should NOT be called on failure
	if len(pusher.pushedBranches) != 0 {
		t.Errorf("expected no force push on rebase failure, got %d", len(pusher.pushedBranches))
	}

	// No activity should be logged on failure
	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	for _, a := range activities {
		if a.EventType == "auto_rebase" {
			t.Error("expected no auto_rebase activity on failure")
		}
	}
}

func TestNewAction_ForcePushError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	runner := &mockRebaseRunner{}
	pusher := &mockForcePusher{err: errors.New("push rejected")}
	resolver := &mockDefaultBaseResolver{base: "main"}
	projects := &mockProjectGetter{project: project}

	cfg := Config{
		Runner:   runner,
		Pusher:   pusher,
		Resolver: resolver,
		Projects: projects,
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when force push fails")
	}
	if !strings.Contains(err.Error(), "force pushing") {
		t.Errorf("expected 'force pushing' in error, got: %s", err.Error())
	}
}

func TestNewAction_ProjectNotFound_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	projects := &mockProjectGetter{err: errors.New("not found")}

	cfg := Config{Projects: projects}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when project not found")
	}
	if !strings.Contains(err.Error(), "loading project") {
		t.Errorf("expected 'loading project' in error, got: %s", err.Error())
	}
}

func TestNewAction_DefaultBaseError_ReturnsError(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	resolver := &mockDefaultBaseResolver{err: errors.New("config error")}
	projects := &mockProjectGetter{project: project}

	cfg := Config{
		Resolver: resolver,
		Projects: projects,
	}

	action := NewAction(cfg)
	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when default base resolution fails")
	}
	if !strings.Contains(err.Error(), "resolving default base") {
		t.Errorf("expected 'resolving default base' in error, got: %s", err.Error())
	}
}
