package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/ccusage"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/server"
)

// mockCCUsageProvider implements server.CCUsageProvider for testing.
type mockCCUsageProvider struct {
	data []ccusage.UsageGroup
}

func (m *mockCCUsageProvider) Current() []ccusage.UsageGroup {
	return m.data
}

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

func TestAPI_CCUsage_NilProvider(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
	if _, ok := result["groups"]; ok {
		t.Fatal("expected no groups key when unavailable")
	}
}

func TestAPI_CCUsage_ProviderReturnsNil(t *testing.T) {
	provider := &mockCCUsageProvider{data: nil}
	srv := newTestServer(t, server.Config{CCUsageProvider: provider})

	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
}

func TestAPI_CCUsage_WithData(t *testing.T) {
	provider := &mockCCUsageProvider{
		data: []ccusage.UsageGroup{
			{
				GroupLabel: "Claude Code Usage Statistics",
				Lines: []ccusage.UsageLine{
					{Label: "5-hour", Percentage: 50, ResetTime: "3h 13m"},
					{Label: "7-day", Percentage: 83, ResetTime: "2d 5h"},
				},
			},
		},
	}
	srv := newTestServer(t, server.Config{CCUsageProvider: provider})

	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}

	groups, ok := result["groups"].([]any)
	if !ok {
		t.Fatalf("expected groups array, got %T", result["groups"])
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	group := groups[0].(map[string]any)
	if group["group_label"] != "Claude Code Usage Statistics" {
		t.Fatalf("expected group_label 'Claude Code Usage Statistics', got %v", group["group_label"])
	}

	lines, ok := group["lines"].([]any)
	if !ok {
		t.Fatalf("expected lines array, got %T", group["lines"])
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	line1 := lines[0].(map[string]any)
	if line1["label"] != "5-hour" {
		t.Fatalf("expected label '5-hour', got %v", line1["label"])
	}
	if int(line1["percentage"].(float64)) != 50 {
		t.Fatalf("expected percentage 50, got %v", line1["percentage"])
	}
	if line1["reset_duration"] != "3h 13m" {
		t.Fatalf("expected reset_duration '3h 13m', got %v", line1["reset_duration"])
	}

	line2 := lines[1].(map[string]any)
	if line2["label"] != "7-day" {
		t.Fatalf("expected label '7-day', got %v", line2["label"])
	}
	if int(line2["percentage"].(float64)) != 83 {
		t.Fatalf("expected percentage 83, got %v", line2["percentage"])
	}
	if line2["reset_duration"] != "2d 5h" {
		t.Fatalf("expected reset_duration '2d 5h', got %v", line2["reset_duration"])
	}
}

func TestAPI_CCUsage_MultipleGroups(t *testing.T) {
	provider := &mockCCUsageProvider{
		data: []ccusage.UsageGroup{
			{
				GroupLabel: "Claude Code Usage Statistics",
				Lines: []ccusage.UsageLine{
					{Label: "5-hour", Percentage: 25, ResetTime: "4h 30m"},
				},
			},
			{
				GroupLabel: "Codex Usage Limits (Plan: Free)",
				Lines: []ccusage.UsageLine{
					{Label: "daily", Percentage: 90, ResetTime: "1h 10m"},
				},
			},
		},
	}
	srv := newTestServer(t, server.Config{CCUsageProvider: provider})

	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}

	groups := result["groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	g1 := groups[0].(map[string]any)
	if g1["group_label"] != "Claude Code Usage Statistics" {
		t.Fatalf("expected first group label 'Claude Code Usage Statistics', got %v", g1["group_label"])
	}

	g2 := groups[1].(map[string]any)
	if g2["group_label"] != "Codex Usage Limits (Plan: Free)" {
		t.Fatalf("expected second group label 'Codex Usage Limits (Plan: Free)', got %v", g2["group_label"])
	}
}

func TestAPI_CCUsage_ContentType(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}
}

func TestAPI_ListIssues_IncludesModelName(t *testing.T) {
	d := testDB(t)
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, ModelName: "Sonnet 4.5"})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "refining"})

	resp, err := http.Get(apiURL(srv, "/api/issues"))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	if result[0]["model"] != "Sonnet 4.5" {
		t.Fatalf("expected model 'Sonnet 4.5', got %v", result[0]["model"])
	}
}

func TestAPI_GetIssue_IncludesModelName(t *testing.T) {
	d := testDB(t)
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, ModelName: "Opus 4.6"})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "building"})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["model"] != "Opus 4.6" {
		t.Fatalf("expected model 'Opus 4.6', got %v", result["model"])
	}
}

func TestAPI_ListIssues_EmptyModelName(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "queued"})

	resp, err := http.Get(apiURL(srv, "/api/issues"))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	// model field must be present as empty string, not omitted
	model, ok := result[0]["model"]
	if !ok {
		t.Fatal("expected 'model' field to be present in response")
	}
	if model != "" {
		t.Fatalf("expected model '', got %v", model)
	}
}

func TestAPI_GetIssue_EmptyModelName(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue1", State: "building"})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	model, ok := result["model"]
	if !ok {
		t.Fatal("expected 'model' field to be present in response")
	}
	if model != "" {
		t.Fatalf("expected model '', got %v", model)
	}
}

// mockBuildChecker implements server.BuildChecker for testing.
type mockBuildChecker struct {
	running   map[string]bool
	cancelled map[string]bool
}

func (m *mockBuildChecker) IsRunning(issueID string) bool {
	return m.running[issueID]
}

func (m *mockBuildChecker) Cancel(issueID string) bool {
	if !m.running[issueID] {
		return false
	}
	if m.cancelled == nil {
		m.cancelled = make(map[string]bool)
	}
	m.cancelled[issueID] = true
	return true
}

func TestAPI_BuildActive_ReturnedForNonBuildingStates(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "refining issue", State: "refining"})

	checker := &mockBuildChecker{running: map[string]bool{iss.ID: true}}
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, BuildChecker: checker})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	// Verify list endpoint returns build_active true for refining issue
	resp, err := http.Get(apiURL(srv, "/api/issues"))
	if err != nil {
		t.Fatalf("GET /api/issues failed: %v", err)
	}
	defer resp.Body.Close()

	var listResult []map[string]any
	json.NewDecoder(resp.Body).Decode(&listResult)
	if len(listResult) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(listResult))
	}
	if listResult[0]["build_active"] != true {
		t.Fatalf("expected build_active true for refining issue, got %v", listResult[0]["build_active"])
	}

	// Verify detail endpoint returns build_active true for refining issue
	detailResp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET /api/issues/:id failed: %v", err)
	}
	defer detailResp.Body.Close()

	var detailResult map[string]any
	json.NewDecoder(detailResp.Body).Decode(&detailResult)
	if detailResult["build_active"] != true {
		t.Fatalf("expected build_active true for refining issue detail, got %v", detailResult["build_active"])
	}
}

func TestAPI_PauseIssue_CancelsRunningWorker(t *testing.T) {
	d := testDB(t)
	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "building issue", State: "building"})

	checker := &mockBuildChecker{running: map[string]bool{iss.ID: true}}
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, BuildChecker: checker})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "application/json", nil)
	if err != nil {
		t.Fatalf("POST pause failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the worker was cancelled
	if !checker.cancelled[iss.ID] {
		t.Error("expected Cancel to be called for the paused issue")
	}

	// Verify state changed to paused
	updated, err := d.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != "paused" {
		t.Errorf("expected state %q, got %q", "paused", updated.State)
	}
}

func TestAPI_PauseIssue_FromNonPausableState_Returns409(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "done issue", State: "completed"})

	resp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "application/json", nil)
	if err != nil {
		t.Fatalf("POST pause failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	return resp
}

func TestAPI_TransitionIssue_FailedToQueued(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "failed",
		ErrorMessage:     "timeout",
		CheckFixAttempts: 3,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "queued",
		"reset_fields": []string{"error_message", "check_fix_attempts"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "transitioned" {
		t.Fatalf("expected status 'transitioned', got %v", result["status"])
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "queued" {
		t.Fatalf("expected state 'queued', got %v", updated.State)
	}
	if updated.ErrorMessage != "" {
		t.Fatalf("expected error_message cleared, got %v", updated.ErrorMessage)
	}
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}

	activity, _ := d.ListActivity(iss.ID, 10, 0)
	if len(activity) < 1 {
		t.Fatal("expected at least 1 activity entry")
	}
	if activity[0].EventType != "state_change" {
		t.Fatalf("expected event_type 'state_change', got %v", activity[0].EventType)
	}
	if activity[0].FromState != "failed" {
		t.Fatalf("expected from_state 'failed', got %v", activity[0].FromState)
	}
	if activity[0].ToState != "queued" {
		t.Fatalf("expected to_state 'queued', got %v", activity[0].ToState)
	}
	if activity[0].Detail != "Manual transition via API" {
		t.Fatalf("expected detail 'Manual transition via API', got %v", activity[0].Detail)
	}
}

func TestAPI_TransitionIssue_PausedToBuilding_RequiresWorkspace(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "issue",
		State:         "paused",
		WorkspaceName: "",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "building",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["error"] == "" {
		t.Fatal("expected error message about missing workspace")
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "paused" {
		t.Fatalf("expected state unchanged 'paused', got %v", updated.State)
	}
}

func TestAPI_TransitionIssue_PausedToBuilding_WithWorkspace(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "issue",
		State:         "paused",
		WorkspaceName: "ws-1",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "building",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "building" {
		t.Fatalf("expected state 'building', got %v", updated.State)
	}
}

func TestAPI_TransitionIssue_ToInReview_RequiresPR(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "paused",
		PRNumber:  0,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "in_review",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_ToAddressingFeedback_RequiresPR(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
		PRNumber:  0,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "addressing_feedback",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_ToFixingChecks_RequiresPR(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
		PRNumber:  0,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "fixing_checks",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_FromCompleted_Rejected(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "completed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "queued",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_ToCompleted_Rejected(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "completed",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_ToWaitingApproval_Rejected(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "waiting_approval",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_InvalidTransition_Rejected(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	// queued can only go to refining per the map
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "queued",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "building",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_MissingTargetState(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp := postJSON(t, apiURL(srv, "/api/issues/nonexistent/transition"), map[string]any{
		"target_state": "queued",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_TransitionIssue_ResetFields(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "failed",
		ErrorMessage:     "some error",
		LastReviewID:     "rev-123",
		LastCheckSHA:     "abc123",
		CheckFixAttempts: 5,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "queued",
		"reset_fields": []string{"check_fix_attempts", "error_message", "last_review_id", "last_check_sha"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}
	if updated.ErrorMessage != "" {
		t.Fatalf("expected error_message empty, got %v", updated.ErrorMessage)
	}
	if updated.LastReviewID != "" {
		t.Fatalf("expected last_review_id empty, got %v", updated.LastReviewID)
	}
	if updated.LastCheckSHA != "" {
		t.Fatalf("expected last_check_sha empty, got %v", updated.LastCheckSHA)
	}
}

func TestAPI_TransitionIssue_AllowedPairs(t *testing.T) {
	// Test a representative set of valid transitions
	tests := []struct {
		name        string
		fromState   string
		toState     string
		prNumber    int
		workspace   string
		wantSuccess bool
	}{
		{"paused to queued", "paused", "queued", 0, "", true},
		{"paused to refining", "paused", "refining", 0, "", true},
		{"paused to approved", "paused", "approved", 0, "", true},
		{"paused to in_review with PR", "paused", "in_review", 42, "", true},
		{"failed to queued", "failed", "queued", 0, "", true},
		{"failed to building with workspace", "failed", "building", 0, "ws-1", true},
		{"in_review to addressing_feedback", "in_review", "addressing_feedback", 42, "", true},
		{"in_review to fixing_checks", "in_review", "fixing_checks", 42, "", true},
		{"in_review to building with ws", "in_review", "building", 42, "ws-1", true},
		{"in_review to refining", "in_review", "refining", 42, "", true},
		{"in_review to queued", "in_review", "queued", 42, "", true},
		{"addressing_feedback to in_review", "addressing_feedback", "in_review", 42, "", true},
		{"addressing_feedback to building", "addressing_feedback", "building", 42, "ws-1", true},
		{"building to approved", "building", "approved", 0, "ws-1", true},
		{"building to refining", "building", "refining", 0, "ws-1", true},
		{"building to queued", "building", "queued", 0, "ws-1", true},
		{"fixing_checks to in_review", "fixing_checks", "in_review", 42, "", true},
		{"fixing_checks to building", "fixing_checks", "building", 42, "ws-1", true},
		{"refining to queued", "refining", "queued", 0, "", true},
		{"refining to approved", "refining", "approved", 0, "", true},
		{"queued to refining", "queued", "refining", 0, "", true},
		{"approved to queued", "approved", "queued", 0, "", true},
		{"approved to refining", "approved", "refining", 0, "", true},
		{"approved to building with ws", "approved", "building", 0, "ws-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDB(t)
			srv := newAPIServer(t, d)

			p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
			iss, _ := d.CreateIssue(db.Issue{
				ProjectID:     p.ID,
				Title:         "issue",
				State:         tt.fromState,
				PRNumber:      tt.prNumber,
				WorkspaceName: tt.workspace,
			})

			resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
				"target_state": tt.toState,
			})
			defer resp.Body.Close()

			if tt.wantSuccess && resp.StatusCode != http.StatusOK {
				var errResp map[string]string
				json.NewDecoder(resp.Body).Decode(&errResp)
				t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errResp["error"])
			}
			if tt.wantSuccess {
				updated, _ := d.GetIssue(iss.ID)
				if updated.State != tt.toState {
					t.Fatalf("expected state %q, got %q", tt.toState, updated.State)
				}
			}
		})
	}
}

func TestAPI_TransitionIssue_NotifyWake(t *testing.T) {
	d := testDB(t)
	wake := make(chan struct{}, 1)
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, Wake: wake})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "failed",
	})

	resp := postJSON(t, "http://"+srv.Addr()+"/api/issues/"+iss.ID+"/transition", map[string]any{
		"target_state": "queued",
	})
	resp.Body.Close()

	select {
	case <-wake:
		// OK: wake was notified
	default:
		t.Fatal("expected notifyWake to be called")
	}
}

// --- GET /api/issues/{id}/transitions tests ---

func TestAPI_GetTransitions_FailedState_ReturnsValidTargets(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:    p.ID,
		Title:        "issue",
		State:        "failed",
		ErrorMessage: "timeout",
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Transitions      []map[string]string `json:"transitions"`
		ResettableFields []string            `json:"resettable_fields"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	// failed with no PR and no workspace: should include queued, refining, approved
	// but NOT building (no workspace), in_review, addressing_feedback, fixing_checks (no PR)
	targets := make(map[string]bool)
	for _, tr := range result.Transitions {
		targets[tr["target_state"]] = true
	}
	for _, expected := range []string{"queued", "refining", "approved"} {
		if !targets[expected] {
			t.Fatalf("expected %q in transitions, got %v", expected, result.Transitions)
		}
	}
	for _, excluded := range []string{"building", "in_review", "addressing_feedback", "fixing_checks"} {
		if targets[excluded] {
			t.Fatalf("did not expect %q in transitions (missing prerequisites), got %v", excluded, result.Transitions)
		}
	}

	// error_message is non-empty so should be in resettable_fields
	found := false
	for _, f := range result.ResettableFields {
		if f == "error_message" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'error_message' in resettable_fields, got %v", result.ResettableFields)
	}
}

func TestAPI_GetTransitions_InReviewState_WithPrereqs(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "issue",
		State:         "in_review",
		PRNumber:      42,
		WorkspaceName: "ws-1",
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Transitions      []map[string]string `json:"transitions"`
		ResettableFields []string            `json:"resettable_fields"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	// in_review with PR and workspace: all targets should be present
	targets := make(map[string]bool)
	for _, tr := range result.Transitions {
		targets[tr["target_state"]] = true
	}
	for _, expected := range []string{"addressing_feedback", "fixing_checks", "building", "refining", "queued"} {
		if !targets[expected] {
			t.Fatalf("expected %q in transitions, got %v", expected, result.Transitions)
		}
	}
}

func TestAPI_GetTransitions_CompletedState_EmptyTransitions(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "completed",
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Transitions      []map[string]string `json:"transitions"`
		ResettableFields []string            `json:"resettable_fields"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Transitions) != 0 {
		t.Fatalf("expected empty transitions for completed state, got %v", result.Transitions)
	}
}

func TestAPI_GetTransitions_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp, err := http.Get(apiURL(srv, "/api/issues/nonexistent/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_GetTransitions_ResettableFields(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "fixing_checks",
		PRNumber:         1,
		CheckFixAttempts: 5,
		ErrorMessage:     "check failed",
		LastReviewID:     "rev-1",
		LastCheckSHA:     "abc123",
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		ResettableFields []string `json:"resettable_fields"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	expected := map[string]bool{
		"check_fix_attempts": true,
		"error_message":      true,
		"last_review_id":     true,
		"last_check_sha":     true,
	}
	got := make(map[string]bool)
	for _, f := range result.ResettableFields {
		got[f] = true
	}
	for field := range expected {
		if !got[field] {
			t.Fatalf("expected %q in resettable_fields, got %v", field, result.ResettableFields)
		}
	}
}

func TestAPI_GetTransitions_ResettableFields_OmitsZeroValues(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	// All tracking fields at zero/empty
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "issue",
		State:     "queued",
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		ResettableFields []string `json:"resettable_fields"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.ResettableFields) != 0 {
		t.Fatalf("expected empty resettable_fields when all values are zero, got %v", result.ResettableFields)
	}
}

func TestAPI_GetTransitions_PrereqFiltering(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	// paused with workspace but no PR
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "issue",
		State:         "paused",
		WorkspaceName: "ws-1",
		PRNumber:      0,
	})

	resp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Transitions []map[string]string `json:"transitions"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	targets := make(map[string]bool)
	for _, tr := range result.Transitions {
		targets[tr["target_state"]] = true
	}
	// building should be present (workspace exists)
	if !targets["building"] {
		t.Fatal("expected 'building' in transitions (workspace exists)")
	}
	// PR-dependent states should be absent
	for _, excluded := range []string{"in_review", "addressing_feedback", "fixing_checks"} {
		if targets[excluded] {
			t.Fatalf("did not expect %q in transitions (no PR)", excluded)
		}
	}
}

// --- POST /api/issues/{id}/reset tests ---

func TestAPI_ResetFields_Success(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "fixing_checks",
		PRNumber:         1,
		CheckFixAttempts: 5,
		ErrorMessage:     "check failed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"check_fix_attempts"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "reset" {
		t.Fatalf("expected status 'reset', got %v", result["status"])
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}
	// State should be unchanged
	if updated.State != "fixing_checks" {
		t.Fatalf("expected state 'fixing_checks', got %v", updated.State)
	}
	// error_message should NOT have been reset (wasn't in fields)
	if updated.ErrorMessage != "check failed" {
		t.Fatalf("expected error_message unchanged, got %v", updated.ErrorMessage)
	}
}

func TestAPI_ResetFields_MultipleFields(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "failed",
		CheckFixAttempts: 3,
		ErrorMessage:     "error",
		LastReviewID:     "rev-1",
		LastCheckSHA:     "sha-1",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"check_fix_attempts", "error_message", "last_review_id", "last_check_sha"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}
	if updated.ErrorMessage != "" {
		t.Fatalf("expected error_message empty, got %v", updated.ErrorMessage)
	}
	if updated.LastReviewID != "" {
		t.Fatalf("expected last_review_id empty, got %v", updated.LastReviewID)
	}
	if updated.LastCheckSHA != "" {
		t.Fatalf("expected last_check_sha empty, got %v", updated.LastCheckSHA)
	}
}

func TestAPI_ResetFields_EmptyFieldsArray_Returns400(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "failed"})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAPI_ResetFields_AllUnrecognized_Returns400(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{ProjectID: p.ID, Title: "issue", State: "failed"})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"bogus_field", "another_bogus"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAPI_ResetFields_UnrecognizedIgnored_WithValidField(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:    p.ID,
		Title:        "issue",
		State:        "failed",
		ErrorMessage: "some error",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"bogus_field", "error_message"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (unrecognized silently ignored), got %d", resp.StatusCode)
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.ErrorMessage != "" {
		t.Fatalf("expected error_message cleared, got %v", updated.ErrorMessage)
	}
}

func TestAPI_ResetFields_NotFound(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	resp := postJSON(t, apiURL(srv, "/api/issues/nonexistent/reset"), map[string]any{
		"fields": []string{"error_message"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_ResetFields_LogsActivity(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "fixing_checks",
		PRNumber:         1,
		CheckFixAttempts: 5,
		ErrorMessage:     "check failed",
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"check_fix_attempts", "error_message"},
	})
	resp.Body.Close()

	activity, _ := d.ListActivity(iss.ID, 10, 0)
	if len(activity) < 1 {
		t.Fatal("expected at least 1 activity entry")
	}
	if activity[0].EventType != "field_reset" {
		t.Fatalf("expected event_type 'field_reset', got %v", activity[0].EventType)
	}
	// Detail should list the reset fields
	if activity[0].Detail == "" {
		t.Fatal("expected non-empty detail listing reset fields")
	}
}

func TestAPI_ResetFields_NotifyWake(t *testing.T) {
	d := testDB(t)
	wake := make(chan struct{}, 1)
	srv, err := server.New("127.0.0.1:0", server.Config{DB: d, Wake: wake})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:    p.ID,
		Title:        "issue",
		State:        "failed",
		ErrorMessage: "error",
	})

	resp := postJSON(t, "http://"+srv.Addr()+"/api/issues/"+iss.ID+"/reset", map[string]any{
		"fields": []string{"error_message"},
	})
	resp.Body.Close()

	select {
	case <-wake:
		// OK: wake was notified
	default:
		t.Fatal("expected notifyWake to be called after reset")
	}
}

func TestAPI_ResetFields_StateUnchanged(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, _ := d.CreateProject(db.Project{Name: "proj", LocalPath: "/tmp/p"})
	iss, _ := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "issue",
		State:            "fixing_checks",
		PRNumber:         1,
		CheckFixAttempts: 5,
	})

	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"check_fix_attempts"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updated, _ := d.GetIssue(iss.ID)
	if updated.State != "fixing_checks" {
		t.Fatalf("expected state unchanged 'fixing_checks', got %v", updated.State)
	}
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}
}
