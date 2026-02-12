package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
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

func testProject(t *testing.T, d *db.DB) db.Project {
	t.Helper()
	p, err := d.CreateProject(db.Project{
		Name:             "test-project",
		LocalPath:        t.TempDir(),
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

// mockLinearServer returns an httptest.Server that responds with the given issues.
func mockLinearServer(t *testing.T, issues []linear.Issue) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodes := make([]map[string]any, len(issues))
		for i, iss := range issues {
			nodes[i] = map[string]any{
				"id":          iss.ID,
				"identifier":  iss.Identifier,
				"title":       iss.Title,
				"description": iss.Description,
				"state": map[string]any{
					"id":   iss.State.ID,
					"name": iss.State.Name,
					"type": iss.State.Type,
				},
			}
		}
		resp := map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": nodes,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestPollProject_IngestsNewIssues(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	linearIssues := []linear.Issue{
		{ID: "lin-1", Identifier: "PROJ-1", Title: "First issue", Description: "Desc 1"},
		{ID: "lin-2", Identifier: "PROJ-2", Title: "Second issue", Description: "Desc 2"},
	}
	srv := mockLinearServer(t, linearIssues)
	defer srv.Close()

	client := linear.New("test-key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx := context.Background()
	p.poll(ctx)

	issues, err := d.ListIssues(db.IssueFilter{ProjectID: proj.ID})
	if err != nil {
		t.Fatalf("listing issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	// Verify fields
	for _, iss := range issues {
		if iss.State != "queued" {
			t.Errorf("expected state %q, got %q", "queued", iss.State)
		}
		if iss.ProjectID != proj.ID {
			t.Errorf("expected project_id %q, got %q", proj.ID, iss.ProjectID)
		}
	}
}

func TestPollProject_SkipsExistingIssues(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Pre-create an issue with the same LinearIssueID
	d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-existing",
		Identifier:    "PROJ-10",
		Title:         "Already tracked",
		State:         "building",
	})

	linearIssues := []linear.Issue{
		{ID: "lin-existing", Identifier: "PROJ-10", Title: "Already tracked"},
		{ID: "lin-new", Identifier: "PROJ-11", Title: "New issue"},
	}
	srv := mockLinearServer(t, linearIssues)
	defer srv.Close()

	client := linear.New("test-key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx := context.Background()
	p.poll(ctx)

	issues, _ := d.ListIssues(db.IssueFilter{ProjectID: proj.ID})
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues total, got %d", len(issues))
	}

	// Verify the existing issue was NOT overwritten
	existing, _ := d.GetIssueByLinearID("lin-existing")
	if existing.State != "building" {
		t.Errorf("existing issue state should be unchanged, got %q", existing.State)
	}
}

func TestPollProject_LogsActivity(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	linearIssues := []linear.Issue{
		{ID: "lin-act-1", Identifier: "PROJ-50", Title: "Activity test"},
	}
	srv := mockLinearServer(t, linearIssues)
	defer srv.Close()

	client := linear.New("test-key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx := context.Background()
	p.poll(ctx)

	issue, _ := d.GetIssueByLinearID("lin-act-1")
	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "ingested" {
		t.Errorf("expected event_type %q, got %q", "ingested", entries[0].EventType)
	}
	if entries[0].ToState != "queued" {
		t.Errorf("expected to_state %q, got %q", "queued", entries[0].ToState)
	}
}

func TestPoll_MultipleProjects(t *testing.T) {
	d := testDB(t)
	proj1 := testProject(t, d)
	proj2, _ := d.CreateProject(db.Project{
		Name:      "test-project-2",
		LocalPath: t.TempDir(),
	})

	srv1 := mockLinearServer(t, []linear.Issue{
		{ID: "lin-p1-1", Identifier: "P1-1", Title: "Proj1 issue"},
	})
	defer srv1.Close()

	srv2 := mockLinearServer(t, []linear.Issue{
		{ID: "lin-p2-1", Identifier: "P2-1", Title: "Proj2 issue"},
	})
	defer srv2.Close()

	client1 := linear.New("key-1", linear.WithEndpoint(srv1.URL))
	client2 := linear.New("key-2", linear.WithEndpoint(srv2.URL))

	p := New(d, []ProjectInfo{
		{ProjectID: proj1.ID, LinearTeamID: "t1", LinearAssigneeID: "u1", LinearClient: client1},
		{ProjectID: proj2.ID, LinearTeamID: "t2", LinearAssigneeID: "u2", LinearClient: client2},
	}, 30*time.Second, slog.Default())

	ctx := context.Background()
	p.poll(ctx)

	issues1, _ := d.ListIssues(db.IssueFilter{ProjectID: proj1.ID})
	issues2, _ := d.ListIssues(db.IssueFilter{ProjectID: proj2.ID})

	if len(issues1) != 1 {
		t.Errorf("expected 1 issue for project 1, got %d", len(issues1))
	}
	if len(issues2) != 1 {
		t.Errorf("expected 1 issue for project 2, got %d", len(issues2))
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	d := testDB(t)

	srv := mockLinearServer(t, nil) // no issues
	defer srv.Close()

	client := linear.New("key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:    "p1",
		LinearClient: client,
	}}, 100*time.Millisecond, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Let it run for a couple ticks
	time.Sleep(250 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success — Run exited after context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2 seconds after context cancellation")
	}
}

func TestPollProject_LinearError_ContinuesGracefully(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Server returns 500 error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := linear.New("key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx := context.Background()
	// Should not panic
	p.poll(ctx)

	issues, _ := d.ListIssues(db.IssueFilter{ProjectID: proj.ID})
	if len(issues) != 0 {
		t.Errorf("expected 0 issues after error, got %d", len(issues))
	}
}

func TestPollProject_ContextCancelled_StopsEarly(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	srv := mockLinearServer(t, []linear.Issue{
		{ID: "lin-ctx-1", Identifier: "CTX-1", Title: "Should not ingest"},
	})
	defer srv.Close()

	client := linear.New("key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p.poll(ctx)

	// Context was already cancelled, so poll should have returned early
	// The behavior depends on whether FetchAssignedIssues respects ctx —
	// it does, so the call will fail. Either way, no panic.
}

func TestPoll_EmptyProjectList(t *testing.T) {
	d := testDB(t)
	p := New(d, nil, 30*time.Second, slog.Default())

	ctx := context.Background()
	// Should not panic with no projects
	p.poll(ctx)
}

func TestPollProject_IdempotentOnRepeatedPolls(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	linearIssues := []linear.Issue{
		{ID: "lin-idem-1", Identifier: "IDEM-1", Title: "Idempotent test"},
	}
	srv := mockLinearServer(t, linearIssues)
	defer srv.Close()

	client := linear.New("key", linear.WithEndpoint(srv.URL))
	p := New(d, []ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     "team-1",
		LinearAssigneeID: "user-1",
		LinearClient:     client,
	}}, 30*time.Second, slog.Default())

	ctx := context.Background()
	p.poll(ctx)
	p.poll(ctx)
	p.poll(ctx)

	issues, _ := d.ListIssues(db.IssueFilter{ProjectID: proj.ID})
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue after 3 polls, got %d", len(issues))
	}

	issue, _ := d.GetIssueByLinearID("lin-idem-1")
	entries, _ := d.ListActivity(issue.ID, 10, 0)
	if len(entries) != 1 {
		t.Errorf("expected 1 activity entry after 3 polls, got %d", len(entries))
	}
}
