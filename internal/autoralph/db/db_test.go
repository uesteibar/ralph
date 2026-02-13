package db

import (
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpen_CreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.db")

	d, err := Open(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer d.Close()
}

func TestOpen_MigratesSchema(t *testing.T) {
	d := testDB(t)

	tables := []string{"projects", "issues", "activity_log", "settings"}
	for _, table := range tables {
		var name string
		err := d.conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpen_IdempotentMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	d1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	d1.Close()

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("second open should be idempotent: %v", err)
	}
	d2.Close()
}

// --- Projects ---

func TestCreateProject_AssignsID(t *testing.T) {
	d := testDB(t)

	p, err := d.CreateProject(Project{Name: "test-project", LocalPath: "/tmp/test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
	if p.Name != "test-project" {
		t.Errorf("expected name %q, got %q", "test-project", p.Name)
	}
}

func TestCreateProject_DuplicateName_ReturnsError(t *testing.T) {
	d := testDB(t)

	_, err := d.CreateProject(Project{Name: "dup", LocalPath: "/tmp/a"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = d.CreateProject(Project{Name: "dup", LocalPath: "/tmp/b"})
	if err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestListProjects_Empty_ReturnsNil(t *testing.T) {
	d := testDB(t)

	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projects != nil {
		t.Errorf("expected nil, got %v", projects)
	}
}

func TestListProjects_ReturnsAllOrderedByName(t *testing.T) {
	d := testDB(t)

	d.CreateProject(Project{Name: "bravo", LocalPath: "/tmp/b"})
	d.CreateProject(Project{Name: "alpha", LocalPath: "/tmp/a"})

	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0].Name != "alpha" {
		t.Errorf("expected first project %q, got %q", "alpha", projects[0].Name)
	}
}

func TestGetProject_Found(t *testing.T) {
	d := testDB(t)

	created, _ := d.CreateProject(Project{
		Name:            "my-project",
		LocalPath:       "/tmp/my-project",
		GithubOwner:     "owner",
		GithubRepo:      "repo",
		LinearTeamID:    "team-123",
		MaxIterations:   30,
		BranchPrefix:    "custom/",
		RalphConfigPath: ".ralph/ralph.yaml",
	})

	got, err := d.GetProject(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "my-project" {
		t.Errorf("expected name %q, got %q", "my-project", got.Name)
	}
	if got.GithubOwner != "owner" {
		t.Errorf("expected github_owner %q, got %q", "owner", got.GithubOwner)
	}
	if got.MaxIterations != 30 {
		t.Errorf("expected max_iterations 30, got %d", got.MaxIterations)
	}
	if got.BranchPrefix != "custom/" {
		t.Errorf("expected branch_prefix %q, got %q", "custom/", got.BranchPrefix)
	}
}

func TestGetProject_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	_, err := d.GetProject("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestUpdateProject_Success(t *testing.T) {
	d := testDB(t)

	p, _ := d.CreateProject(Project{Name: "before", LocalPath: "/tmp/before"})
	p.Name = "after"
	p.LocalPath = "/tmp/after"

	if err := d.UpdateProject(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := d.GetProject(p.ID)
	if got.Name != "after" {
		t.Errorf("expected name %q, got %q", "after", got.Name)
	}
	if got.LocalPath != "/tmp/after" {
		t.Errorf("expected local_path %q, got %q", "/tmp/after", got.LocalPath)
	}
}

func TestUpdateProject_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	err := d.UpdateProject(Project{ID: "nonexistent", Name: "x", LocalPath: "/tmp"})
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestDeleteProject_Success(t *testing.T) {
	d := testDB(t)

	p, _ := d.CreateProject(Project{Name: "to-delete", LocalPath: "/tmp"})
	if err := d.DeleteProject(p.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := d.GetProject(p.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteProject_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	err := d.DeleteProject("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestGetProjectByName_Found(t *testing.T) {
	d := testDB(t)

	created, _ := d.CreateProject(Project{Name: "findme", LocalPath: "/tmp/findme", GithubOwner: "owner"})

	got, err := d.GetProjectByName("findme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
	if got.GithubOwner != "owner" {
		t.Errorf("expected github_owner %q, got %q", "owner", got.GithubOwner)
	}
}

func TestGetProjectByName_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	_, err := d.GetProjectByName("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent project name")
	}
}

// --- Issues ---

func createTestProject(t *testing.T, d *DB) Project {
	t.Helper()
	p, err := d.CreateProject(Project{Name: "test-project", LocalPath: "/tmp/test"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

func TestCreateIssue_AssignsID(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, err := d.CreateIssue(Issue{
		ProjectID: p.ID,
		Title:     "Test issue",
		State:     "queued",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestListIssues_NoFilter_ReturnsAll(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	d.CreateIssue(Issue{ProjectID: p.ID, Title: "Issue 1", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "Issue 2", State: "building"})

	issues, err := d.ListIssues(IssueFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestListIssues_FilterByProjectID(t *testing.T) {
	d := testDB(t)
	p1, _ := d.CreateProject(Project{Name: "proj-1", LocalPath: "/tmp/p1"})
	p2, _ := d.CreateProject(Project{Name: "proj-2", LocalPath: "/tmp/p2"})

	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "P1 Issue", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p2.ID, Title: "P2 Issue", State: "queued"})

	issues, err := d.ListIssues(IssueFilter{ProjectID: p1.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Title != "P1 Issue" {
		t.Errorf("expected title %q, got %q", "P1 Issue", issues[0].Title)
	}
}

func TestListIssues_FilterByState(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	d.CreateIssue(Issue{ProjectID: p.ID, Title: "Queued", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "Building", State: "building"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "Also Queued", State: "queued"})

	issues, err := d.ListIssues(IssueFilter{State: "queued"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestListIssues_FilterByProjectAndState(t *testing.T) {
	d := testDB(t)
	p1, _ := d.CreateProject(Project{Name: "proj-1", LocalPath: "/tmp/p1"})
	p2, _ := d.CreateProject(Project{Name: "proj-2", LocalPath: "/tmp/p2"})

	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "P1 Queued", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "P1 Building", State: "building"})
	d.CreateIssue(Issue{ProjectID: p2.ID, Title: "P2 Queued", State: "queued"})

	issues, err := d.ListIssues(IssueFilter{ProjectID: p1.ID, State: "queued"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Title != "P1 Queued" {
		t.Errorf("expected title %q, got %q", "P1 Queued", issues[0].Title)
	}
}

func TestGetIssue_Found(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	created, _ := d.CreateIssue(Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Fix the bug",
		Description:   "Something is broken",
		State:         "queued",
	})

	got, err := d.GetIssue(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Identifier != "PROJ-42" {
		t.Errorf("expected identifier %q, got %q", "PROJ-42", got.Identifier)
	}
	if got.Title != "Fix the bug" {
		t.Errorf("expected title %q, got %q", "Fix the bug", got.Title)
	}
}

func TestGetIssue_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	_, err := d.GetIssue("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent issue")
	}
}

func TestUpdateIssue_Success(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, _ := d.CreateIssue(Issue{
		ProjectID: p.ID,
		Title:     "Original",
		State:     "queued",
	})
	issue.State = "building"
	issue.BranchName = "autoralph/fix-42"
	issue.WorkspaceName = "fix-42"

	if err := d.UpdateIssue(issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := d.GetIssue(issue.ID)
	if got.State != "building" {
		t.Errorf("expected state %q, got %q", "building", got.State)
	}
	if got.BranchName != "autoralph/fix-42" {
		t.Errorf("expected branch_name %q, got %q", "autoralph/fix-42", got.BranchName)
	}
}

func TestUpdateIssue_NotFound_ReturnsError(t *testing.T) {
	d := testDB(t)

	err := d.UpdateIssue(Issue{ID: "nonexistent", ProjectID: "x"})
	if err == nil {
		t.Error("expected error for nonexistent issue")
	}
}

// --- Activity Log ---

func TestLogActivity_AndListActivity(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)
	issue, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "Test", State: "queued"})

	err := d.LogActivity(issue.ID, "state_change", "queued", "refining", "Started refinement")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = d.LogActivity(issue.ID, "comment_posted", "", "", "Posted clarifying questions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first
	if entries[0].EventType != "comment_posted" {
		t.Errorf("expected newest entry first, got %q", entries[0].EventType)
	}
}

func TestListActivity_Pagination(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)
	issue, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "Test", State: "queued"})

	for range 5 {
		d.LogActivity(issue.ID, "event", "", "", "")
	}

	page1, _ := d.ListActivity(issue.ID, 2, 0)
	if len(page1) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page1))
	}

	page2, _ := d.ListActivity(issue.ID, 2, 2)
	if len(page2) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page2))
	}

	page3, _ := d.ListActivity(issue.ID, 2, 4)
	if len(page3) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page3))
	}
}

func TestListActivity_EmptyIssue_ReturnsNil(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)
	issue, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "Test", State: "queued"})

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil, got %v", entries)
	}
}

// --- Settings ---

func TestGetSetting_NotSet_ReturnsEmpty(t *testing.T) {
	d := testDB(t)

	val, err := d.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}
}

func TestSetSetting_AndGetSetting(t *testing.T) {
	d := testDB(t)

	if err := d.SetSetting("poll_interval", "30"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, err := d.GetSetting("poll_interval")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "30" {
		t.Errorf("expected %q, got %q", "30", val)
	}
}

func TestSetSetting_Upsert(t *testing.T) {
	d := testDB(t)

	d.SetSetting("key", "first")
	d.SetSetting("key", "second")

	val, _ := d.GetSetting("key")
	if val != "second" {
		t.Errorf("expected %q, got %q", "second", val)
	}
}

// --- GetIssueByLinearID ---

func TestGetIssueByLinearID_Found(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	created, _ := d.CreateIssue(Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-abc-123",
		Identifier:    "PROJ-99",
		Title:         "Find by linear ID",
		State:         "queued",
	})

	got, err := d.GetIssueByLinearID("lin-abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
	if got.Identifier != "PROJ-99" {
		t.Errorf("expected identifier %q, got %q", "PROJ-99", got.Identifier)
	}
}

func TestGetIssueByLinearID_NotFound(t *testing.T) {
	d := testDB(t)

	_, err := d.GetIssueByLinearID("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent linear_issue_id")
	}
}

// --- Multi-State Filter ---

func TestListIssues_StatesFilter_ReturnsMatchingStates(t *testing.T) {
	d := testDB(t)

	p, _ := d.CreateProject(Project{Name: "states-proj", LocalPath: t.TempDir()})

	d.CreateIssue(Issue{ProjectID: p.ID, Identifier: "S-1", State: "in_review"})
	d.CreateIssue(Issue{ProjectID: p.ID, Identifier: "S-2", State: "addressing_feedback"})
	d.CreateIssue(Issue{ProjectID: p.ID, Identifier: "S-3", State: "building"})
	d.CreateIssue(Issue{ProjectID: p.ID, Identifier: "S-4", State: "completed"})

	issues, err := d.ListIssues(IssueFilter{
		ProjectID: p.ID,
		States:    []string{"in_review", "addressing_feedback"},
	})
	if err != nil {
		t.Fatalf("listing issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	states := map[string]bool{}
	for _, iss := range issues {
		states[iss.State] = true
	}
	if !states["in_review"] || !states["addressing_feedback"] {
		t.Errorf("expected in_review and addressing_feedback, got %v", states)
	}
}

// --- Foreign Key Enforcement ---

func TestCreateIssue_InvalidProjectID_ReturnsError(t *testing.T) {
	d := testDB(t)

	_, err := d.CreateIssue(Issue{
		ProjectID: "nonexistent-project",
		Title:     "Orphan issue",
		State:     "queued",
	})
	if err == nil {
		t.Error("expected error for invalid project_id foreign key")
	}
}

// --- CountActiveIssuesByProject ---

func TestCountActiveIssuesByProject_ReturnsCorrectCounts(t *testing.T) {
	d := testDB(t)

	p1, _ := d.CreateProject(Project{Name: "proj1", LocalPath: "/tmp/p1"})
	p2, _ := d.CreateProject(Project{Name: "proj2", LocalPath: "/tmp/p2"})

	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "active1", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "active2", State: "building"})
	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "done", State: "completed"})
	d.CreateIssue(Issue{ProjectID: p1.ID, Title: "err", State: "failed"})
	d.CreateIssue(Issue{ProjectID: p2.ID, Title: "active3", State: "in_review"})

	counts, err := d.CountActiveIssuesByProject()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts[p1.ID] != 2 {
		t.Fatalf("expected 2 active issues for proj1, got %d", counts[p1.ID])
	}
	if counts[p2.ID] != 1 {
		t.Fatalf("expected 1 active issue for proj2, got %d", counts[p2.ID])
	}
}

func TestCountActiveIssuesByProject_EmptyDB(t *testing.T) {
	d := testDB(t)

	counts, err := d.CountActiveIssuesByProject()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(counts))
	}
}

func TestListRecentActivity_ReturnsAcrossIssues(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(Project{Name: "proj", LocalPath: "/tmp/p"})
	iss1, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "issue1", State: "queued"})
	iss2, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "issue2", State: "building"})

	d.LogActivity(iss1.ID, "state_change", "queued", "refining", "refined")
	d.LogActivity(iss2.ID, "state_change", "queued", "building", "built")
	d.LogActivity(iss1.ID, "state_change", "refining", "approved", "approved")

	entries, err := d.ListRecentActivity(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Most recent first
	if entries[0].Detail != "approved" {
		t.Fatalf("expected most recent entry 'approved', got %q", entries[0].Detail)
	}
}

func TestListRecentActivity_RespectsLimit(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "issue", State: "queued"})

	for range 5 {
		d.LogActivity(iss.ID, "state_change", "a", "b", "detail")
	}

	entries, err := d.ListRecentActivity(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries with limit=2, got %d", len(entries))
	}
}

func TestListRecentActivity_EmptyDB(t *testing.T) {
	d := testDB(t)

	entries, err := d.ListRecentActivity(20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty list, got %d entries", len(entries))
	}
}

func TestCountIssuesByStateForProject(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(Project{Name: "proj", LocalPath: "/tmp/p"})

	d.CreateIssue(Issue{ProjectID: p.ID, Title: "i1", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "i2", State: "queued"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "i3", State: "building"})
	d.CreateIssue(Issue{ProjectID: p.ID, Title: "i4", State: "completed"})

	counts, err := d.CountIssuesByStateForProject(p.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts["queued"] != 2 {
		t.Fatalf("expected 2 queued, got %d", counts["queued"])
	}
	if counts["building"] != 1 {
		t.Fatalf("expected 1 building, got %d", counts["building"])
	}
	if counts["completed"] != 1 {
		t.Fatalf("expected 1 completed, got %d", counts["completed"])
	}
}

func TestCountIssuesByStateForProject_Empty(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(Project{Name: "proj", LocalPath: "/tmp/p"})

	counts, err := d.CountIssuesByStateForProject(p.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(counts))
	}
}

func TestDeleteIssue_RemovesIssueAndActivity(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(Project{Name: "proj", LocalPath: "/tmp/p"})
	issue, _ := d.CreateIssue(Issue{ProjectID: p.ID, Title: "test", State: "queued"})
	d.LogActivity(issue.ID, "state_change", "queued", "refining", "test")

	if err := d.DeleteIssue(issue.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := d.GetIssue(issue.ID)
	if err == nil {
		t.Fatal("expected error getting deleted issue")
	}
	activity, _ := d.ListActivity(issue.ID, 10, 0)
	if len(activity) != 0 {
		t.Fatalf("expected no activity, got %d entries", len(activity))
	}
}

func TestDeleteIssue_NotFound(t *testing.T) {
	d := testDB(t)
	err := d.DeleteIssue("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent issue")
	}
}

// --- Check Tracking Columns ---

func TestCreateIssue_CheckTrackingColumns_DefaultValues(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, err := d.CreateIssue(Issue{
		ProjectID: p.ID,
		Title:     "No check fields set",
		State:     "queued",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastCheckSHA != "" {
		t.Errorf("expected empty LastCheckSHA, got %q", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 0 {
		t.Errorf("expected CheckFixAttempts 0, got %d", got.CheckFixAttempts)
	}
}

func TestCreateIssue_CheckTrackingColumns_SetValues(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, err := d.CreateIssue(Issue{
		ProjectID:        p.ID,
		Title:            "With check fields",
		State:            "fixing_checks",
		LastCheckSHA:     "abc123",
		CheckFixAttempts: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastCheckSHA != "abc123" {
		t.Errorf("expected LastCheckSHA %q, got %q", "abc123", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 2 {
		t.Errorf("expected CheckFixAttempts 2, got %d", got.CheckFixAttempts)
	}
}

func TestUpdateIssue_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, _ := d.CreateIssue(Issue{
		ProjectID: p.ID,
		Title:     "Update check fields",
		State:     "in_review",
	})
	issue.LastCheckSHA = "sha-456"
	issue.CheckFixAttempts = 3

	if err := d.UpdateIssue(issue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := d.GetIssue(issue.ID)
	if got.LastCheckSHA != "sha-456" {
		t.Errorf("expected LastCheckSHA %q, got %q", "sha-456", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 3 {
		t.Errorf("expected CheckFixAttempts 3, got %d", got.CheckFixAttempts)
	}
}

func TestListIssues_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	d.CreateIssue(Issue{
		ProjectID:        p.ID,
		Title:            "Check issue",
		State:            "fixing_checks",
		LastCheckSHA:     "list-sha",
		CheckFixAttempts: 1,
	})

	issues, err := d.ListIssues(IssueFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].LastCheckSHA != "list-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "list-sha", issues[0].LastCheckSHA)
	}
	if issues[0].CheckFixAttempts != 1 {
		t.Errorf("expected CheckFixAttempts 1, got %d", issues[0].CheckFixAttempts)
	}
}

func TestGetIssueByLinearID_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	d.CreateIssue(Issue{
		ProjectID:        p.ID,
		LinearIssueID:    "lin-check-1",
		Title:            "Check by linear ID",
		State:            "fixing_checks",
		LastCheckSHA:     "linear-sha",
		CheckFixAttempts: 2,
	})

	got, err := d.GetIssueByLinearID("lin-check-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastCheckSHA != "linear-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "linear-sha", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 2 {
		t.Errorf("expected CheckFixAttempts 2, got %d", got.CheckFixAttempts)
	}
}

func TestGetIssueByLinearIDAndProject_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	d.CreateIssue(Issue{
		ProjectID:        p.ID,
		LinearIssueID:    "lin-check-2",
		Title:            "Check by linear ID and project",
		State:            "fixing_checks",
		LastCheckSHA:     "proj-sha",
		CheckFixAttempts: 1,
	})

	got, err := d.GetIssueByLinearIDAndProject("lin-check-2", p.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastCheckSHA != "proj-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "proj-sha", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 1 {
		t.Errorf("expected CheckFixAttempts 1, got %d", got.CheckFixAttempts)
	}
}

func TestTxUpdateIssue_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, _ := d.CreateIssue(Issue{
		ProjectID: p.ID,
		Title:     "Tx check fields",
		State:     "in_review",
	})

	err := d.Tx(func(tx *Tx) error {
		issue.LastCheckSHA = "tx-sha"
		issue.CheckFixAttempts = 2
		return tx.UpdateIssue(issue)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := d.GetIssue(issue.ID)
	if got.LastCheckSHA != "tx-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "tx-sha", got.LastCheckSHA)
	}
	if got.CheckFixAttempts != 2 {
		t.Errorf("expected CheckFixAttempts 2, got %d", got.CheckFixAttempts)
	}
}

func TestTxGetIssue_CheckTrackingColumns(t *testing.T) {
	d := testDB(t)
	p := createTestProject(t, d)

	issue, _ := d.CreateIssue(Issue{
		ProjectID:        p.ID,
		Title:            "Tx get check fields",
		State:            "fixing_checks",
		LastCheckSHA:     "txget-sha",
		CheckFixAttempts: 3,
	})

	err := d.Tx(func(tx *Tx) error {
		got, err := tx.GetIssue(issue.ID)
		if err != nil {
			return err
		}
		if got.LastCheckSHA != "txget-sha" {
			t.Errorf("expected LastCheckSHA %q, got %q", "txget-sha", got.LastCheckSHA)
		}
		if got.CheckFixAttempts != 3 {
			t.Errorf("expected CheckFixAttempts 3, got %d", got.CheckFixAttempts)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpen_MigratesCheckTrackingColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	d, err := Open(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var lastCheckSHA, checkFixAttempts int
	err = d.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('issues') WHERE name='last_check_sha'`).Scan(&lastCheckSHA)
	if err != nil {
		t.Fatalf("querying column info: %v", err)
	}
	if lastCheckSHA != 1 {
		t.Errorf("expected last_check_sha column to exist")
	}

	err = d.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('issues') WHERE name='check_fix_attempts'`).Scan(&checkFixAttempts)
	if err != nil {
		t.Fatalf("querying column info: %v", err)
	}
	if checkFixAttempts != 1 {
		t.Errorf("expected check_fix_attempts column to exist")
	}
	d.Close()
}
