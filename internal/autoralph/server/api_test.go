package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/server"
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

func newAPIServer(t *testing.T, d *db.DB) *server.Server {
	t.Helper()
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()
	return srv
}

func apiURL(srv *server.Server, path string) string {
	return "http://" + srv.Addr() + path
}

func TestAPI_ListProjects_Empty(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp, err := http.Get(apiURL(srv, "/api/projects"))
	if err != nil {
		t.Fatalf("GET /api/projects failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 0 {
		t.Fatalf("expected empty list, got %d items", len(result))
	}
}

func TestAPI_ListProjects_WithActiveIssueCounts(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj1", LocalPath: "/tmp/p1"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "active", State: "building"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "done", State: "completed"})

	resp, err := http.Get(apiURL(srv, "/api/projects"))
	if err != nil {
		t.Fatalf("GET /api/projects failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result))
	}
	if result[0]["name"] != "proj1" {
		t.Fatalf("expected name 'proj1', got %v", result[0]["name"])
	}
	count := int(result[0]["active_issue_count"].(float64))
	if count != 1 {
		t.Fatalf("expected active_issue_count 1, got %d", count)
	}
}

func TestAPI_ListIssues_Unfiltered(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "queued"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue2", State: "building"})

	resp, err := http.Get(apiURL(srv, "/api/issues"))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(result))
	}
}

func TestAPI_ListIssues_FilterByProjectID(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p1, _ := d.CreateProject(db.Project{Name: "proj1", LocalPath: "/tmp/p1"})
	p2, _ := d.CreateProject(db.Project{Name: "proj2", LocalPath: "/tmp/p2"})
	d.CreateIssue(db.Issue{ProjectID: p1.ID, Title: "issue1", State: "queued"})
	d.CreateIssue(db.Issue{ProjectID: p2.ID, Title: "issue2", State: "queued"})

	resp, err := http.Get(apiURL(srv, "/api/issues?project_id="+p1.ID))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	if result[0]["title"] != "issue1" {
		t.Fatalf("expected title 'issue1', got %v", result[0]["title"])
	}
}

func TestAPI_ListIssues_FilterByState(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "queued1", State: "queued"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "building1", State: "building"})

	resp, err := http.Get(apiURL(srv, "/api/issues?state=building"))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	if result[0]["title"] != "building1" {
		t.Fatalf("expected title 'building1', got %v", result[0]["title"])
	}
}

func TestAPI_GetIssue_Success(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-1",
		Identifier:    "PROJ-1",
		Title:         "Test Issue",
		Description:   "A test issue",
		State:         "building",
	})
	d.LogActivity(iss.ID, "state_change", "queued", "building", "auto transition")

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["title"] != "Test Issue" {
		t.Fatalf("expected title 'Test Issue', got %v", result["title"])
	}
	if result["identifier"] != "PROJ-1" {
		t.Fatalf("expected identifier 'PROJ-1', got %v", result["identifier"])
	}
	if result["linear_issue_id"] != "lin-1" {
		t.Fatalf("expected linear_issue_id 'lin-1', got %v", result["linear_issue_id"])
	}
	if result["project_name"] != "proj" {
		t.Fatalf("expected project_name 'proj', got %v", result["project_name"])
	}

	activity, ok := result["activity"].([]any)
	if !ok {
		t.Fatalf("expected activity array, got %T", result["activity"])
	}
	if len(activity) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(activity))
	}
}

func TestAPI_GetIssue_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp, err := http.Get(apiURL(srv, "/api/issues/nonexistent"))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["error"] != "issue not found" {
		t.Fatalf("expected error 'issue not found', got %v", result["error"])
	}
}

func TestAPI_GetIssue_ActivityPagination(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "queued"})
	for range 5 {
		d.LogActivity(iss.ID, "state_change", "queued", "building", "transition")
	}

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"?timeline_limit=2&timeline_offset=0"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	activity := result["activity"].([]any)
	if len(activity) != 2 {
		t.Fatalf("expected 2 activity entries with timeline_limit=2, got %d", len(activity))
	}
}

func TestAPI_PauseIssue_Success(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "", nil)
	if err != nil {
		t.Fatalf("POST /api/issues/:id/pause failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "paused" {
		t.Fatalf("expected status 'paused', got %v", result["status"])
	}
	if result["previous_state"] != "building" {
		t.Fatalf("expected previous_state 'building', got %v", result["previous_state"])
	}

	// Verify DB was updated
	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "paused" {
		t.Fatalf("expected DB state 'paused', got %v", updated.State)
	}
}

func TestAPI_PauseIssue_NotPausableState(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "completed"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_PauseIssue_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp, err := http.Post(apiURL(srv, "/api/issues/nonexistent/pause"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_ResumeIssue_Success(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "paused"})
	d.LogActivity(iss.ID, "state_change", "building", "paused", "paused")

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/resume"), "", nil)
	if err != nil {
		t.Fatalf("POST /api/issues/:id/resume failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "resumed" {
		t.Fatalf("expected status 'resumed', got %v", result["status"])
	}
	if result["state"] != "building" {
		t.Fatalf("expected state 'building', got %v", result["state"])
	}

	// Verify DB was updated
	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "building" {
		t.Fatalf("expected DB state 'building', got %v", updated.State)
	}
}

func TestAPI_ResumeIssue_NotPaused(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/resume"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_RetryIssue_Success(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:    p.ID,
		Title:        "issue",
		State:        "failed",
		ErrorMessage: "build timed out",
	})
	d.LogActivity(iss.ID, "state_change", "building", "failed", "build failed")

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/retry"), "", nil)
	if err != nil {
		t.Fatalf("POST /api/issues/:id/retry failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "retrying" {
		t.Fatalf("expected status 'retrying', got %v", result["status"])
	}
	if result["state"] != "building" {
		t.Fatalf("expected state 'building', got %v", result["state"])
	}

	// Verify DB was updated
	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "building" {
		t.Fatalf("expected DB state 'building', got %v", updated.State)
	}
	if updated.ErrorMessage != "" {
		t.Fatalf("expected error_message cleared, got %v", updated.ErrorMessage)
	}
}

func TestAPI_RetryIssue_NotFailed(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/retry"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_Status_ReturnsHealthInfo(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "building", State: "building"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "queued", State: "queued"})

	resp, err := http.Get(apiURL(srv, "/api/status"))
	if err != nil {
		t.Fatalf("GET /api/status failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %v", result["status"])
	}
	if result["uptime"] == nil || result["uptime"] == "" {
		t.Fatal("expected uptime to be set")
	}
	activeBuilds := int(result["active_builds"].(float64))
	if activeBuilds != 1 {
		t.Fatalf("expected active_builds 1, got %d", activeBuilds)
	}
}

func TestAPI_Status_WithoutDB_ReturnsSimpleOK(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %v", result["status"])
	}
}

func TestAPI_ConsistentErrorFormat(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp, err := http.Get(apiURL(srv, "/api/issues/nonexistent"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if _, ok := result["error"]; !ok {
		t.Fatal("expected 'error' key in response")
	}
}

func TestAPI_PauseIssue_LogsActivity(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "queued"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()

	activity, _ := d.ListActivity(iss.ID, 10, 0)
	if len(activity) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(activity))
	}
	if activity[0].EventType != "state_change" {
		t.Fatalf("expected event_type 'state_change', got %v", activity[0].EventType)
	}
	if activity[0].FromState != "queued" {
		t.Fatalf("expected from_state 'queued', got %v", activity[0].FromState)
	}
	if activity[0].ToState != "paused" {
		t.Fatalf("expected to_state 'paused', got %v", activity[0].ToState)
	}
}

func TestAPI_ResumeIssue_DefaultsToQueued(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	// Paused issue with no activity log — should default to queued
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "paused"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/resume"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["state"] != "queued" {
		t.Fatalf("expected fallback state 'queued', got %v", result["state"])
	}
}

func TestAPI_RetryIssue_DefaultsToQueued(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "failed"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/retry"), "", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["state"] != "queued" {
		t.Fatalf("expected fallback state 'queued', got %v", result["state"])
	}
}

func TestAPI_ListActivity_ReturnsRecentEntries(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss1, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "queued"})
	iss2, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue2", State: "building"})

	d.LogActivity(iss1.ID, "state_change", "queued", "refining", "refined")
	d.LogActivity(iss2.ID, "state_change", "queued", "building", "built")

	resp, err := http.Get(apiURL(srv, "/api/activity"))
	if err != nil {
		t.Fatalf("GET /api/activity failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 2 {
		t.Fatalf("expected 2 activity entries, got %d", len(result))
	}
	if result[0]["issue_id"] == nil || result[0]["issue_id"] == "" {
		t.Fatal("expected issue_id in activity entry")
	}
}

func TestAPI_ListActivity_RespectsLimit(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "queued"})

	for range 5 {
		d.LogActivity(iss.ID, "state_change", "a", "b", "detail")
	}

	resp, err := http.Get(apiURL(srv, "/api/activity?limit=2"))
	if err != nil {
		t.Fatalf("GET /api/activity failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 2 {
		t.Fatalf("expected 2 activity entries with limit=2, got %d", len(result))
	}
}

func TestAPI_ListProjects_IncludesStateBreakdown(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj1", LocalPath: "/tmp/p1"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "i1", State: "queued"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "i2", State: "queued"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "i3", State: "building"})

	resp, err := http.Get(apiURL(srv, "/api/projects"))
	if err != nil {
		t.Fatalf("GET /api/projects failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result))
	}

	breakdown, ok := result[0]["state_breakdown"].(map[string]any)
	if !ok {
		t.Fatalf("expected state_breakdown map, got %T", result[0]["state_breakdown"])
	}
	if int(breakdown["queued"].(float64)) != 2 {
		t.Fatalf("expected 2 queued, got %v", breakdown["queued"])
	}
	if int(breakdown["building"].(float64)) != 1 {
		t.Fatalf("expected 1 building, got %v", breakdown["building"])
	}
}

func TestAPI_DeleteIssue_Success(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	issue, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "to delete", State: "queued"})
	d.LogActivity(issue.ID, "ingested", "", "queued", "test")

	srv := newAPIServer(t, d)

	req, _ := http.NewRequest("DELETE", apiURL(srv, "/api/issues/"+issue.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "deleted" {
		t.Fatalf("expected deleted status, got %q", result["status"])
	}

	// Verify issue is gone from DB.
	getResp, getErr := http.Get(apiURL(srv, "/api/issues/"+issue.ID))
	if getErr != nil {
		t.Fatalf("GET after delete failed: %v", getErr)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestAPI_DeleteIssue_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	req, _ := http.NewRequest("DELETE", apiURL(srv, "/api/issues/nonexistent"), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_GetIssue_SplitActivityResponse(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	// Log timeline events (state_change, pr_created, etc.)
	d.LogActivity(iss.ID, "state_change", "queued", "refining", "auto transition")
	d.LogActivity(iss.ID, "state_change", "refining", "building", "auto transition")
	d.LogActivity(iss.ID, "pr_created", "", "", "PR #42 opened")

	// Log build events
	d.LogActivity(iss.ID, "build_event", "", "", "Iteration 1/3 started")
	d.LogActivity(iss.ID, "build_event", "", "", "Story US-001: Add feature")
	d.LogActivity(iss.ID, "build_event", "", "", "Building code...")

	resp, err := http.Get(apiURL(srv, "/api/issues/" + iss.ID))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify activity contains only timeline events (no build_event)
	activity, ok := result["activity"].([]any)
	if !ok {
		t.Fatalf("expected activity array, got %T", result["activity"])
	}
	if len(activity) != 3 {
		t.Fatalf("expected 3 timeline events in activity, got %d", len(activity))
	}
	for _, a := range activity {
		entry := a.(map[string]any)
		if entry["event_type"] == "build_event" {
			t.Fatal("activity array should not contain build_event entries")
		}
	}

	// Verify build_activity contains only build events
	buildActivity, ok := result["build_activity"].([]any)
	if !ok {
		t.Fatalf("expected build_activity array, got %T", result["build_activity"])
	}
	if len(buildActivity) != 3 {
		t.Fatalf("expected 3 build events in build_activity, got %d", len(buildActivity))
	}
	for _, a := range buildActivity {
		entry := a.(map[string]any)
		if entry["event_type"] != "build_event" {
			t.Fatalf("build_activity should only contain build_event, got %v", entry["event_type"])
		}
	}
}

func TestAPI_GetIssue_ParseCurrentStoryFromBuildActivity(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	// Log build events with story and iteration info
	d.LogActivity(iss.ID, "build_event", "", "", "Story US-003: Implement auth")
	d.LogActivity(iss.ID, "build_event", "", "", "Iteration 2/5 started")

	// Log timeline events — these should NOT be used for story/iteration parsing
	d.LogActivity(iss.ID, "state_change", "queued", "building", "auto")

	resp, err := http.Get(apiURL(srv, "/api/issues/" + iss.ID))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["current_story"] != "US-003" {
		t.Fatalf("expected current_story 'US-003', got %v", result["current_story"])
	}
	iteration := int(result["iteration"].(float64))
	if iteration != 2 {
		t.Fatalf("expected iteration 2, got %d", iteration)
	}
	maxIterations := int(result["max_iterations"].(float64))
	if maxIterations != 5 {
		t.Fatalf("expected max_iterations 5, got %d", maxIterations)
	}
}

func TestAPI_GetIssue_DefaultAndCustomLimitsOffsets(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	// Create 10 timeline events
	for i := range 10 {
		d.LogActivity(iss.ID, "state_change", "a", "b", fmt.Sprintf("timeline-%d", i))
	}
	// Create 20 build events
	for i := range 20 {
		d.LogActivity(iss.ID, "build_event", "", "", fmt.Sprintf("build-%d", i))
	}

	// Test custom limits and offsets
	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"?build_limit=5&timeline_limit=3&offset=2&timeline_offset=1"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	buildActivity := result["build_activity"].([]any)
	if len(buildActivity) != 5 {
		t.Fatalf("expected 5 build_activity entries with build_limit=5, got %d", len(buildActivity))
	}

	activity := result["activity"].([]any)
	if len(activity) != 3 {
		t.Fatalf("expected 3 activity entries with timeline_limit=3, got %d", len(activity))
	}
}

func TestAPI_GetIssue_DefaultLimits(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "building"})

	// Create 60 timeline events (more than default 50)
	for i := range 60 {
		d.LogActivity(iss.ID, "state_change", "a", "b", fmt.Sprintf("timeline-%d", i))
	}
	// Create 250 build events (more than default 200)
	for i := range 250 {
		d.LogActivity(iss.ID, "build_event", "", "", fmt.Sprintf("build-%d", i))
	}

	resp, err := http.Get(apiURL(srv, "/api/issues/" + iss.ID))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	buildActivity := result["build_activity"].([]any)
	if len(buildActivity) != 200 {
		t.Fatalf("expected 200 build_activity entries (default build_limit), got %d", len(buildActivity))
	}

	activity := result["activity"].([]any)
	if len(activity) != 50 {
		t.Fatalf("expected 50 activity entries (default timeline_limit), got %d", len(activity))
	}
}
