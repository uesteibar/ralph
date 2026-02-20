package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
)

// IT-001: Valid transition from failed state to queued succeeds and resets fields.
func TestIntegration_IT001_TransitionFailedToQueued_ResetsFields(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	// Step 1: Create project and issue in failed state with error_message and check_fix_attempts.
	p, err := d.CreateProject(db.Project{Name: "it001-proj", LocalPath: "/tmp/it001"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	iss, err := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "IT-001 issue",
		State:            "failed",
		ErrorMessage:     "timeout",
		CheckFixAttempts: 3,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/transition with target_state=queued and reset_fields.
	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "queued",
		"reset_fields": []string{"error_message", "check_fix_attempts"},
	})
	defer resp.Body.Close()

	// Step 3: Assert response status 200 with status 'transitioned'.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var transResult map[string]string
	json.NewDecoder(resp.Body).Decode(&transResult)
	if transResult["status"] != "transitioned" {
		t.Fatalf("expected status 'transitioned', got %v", transResult["status"])
	}
	if transResult["from_state"] != "failed" {
		t.Fatalf("expected from_state 'failed', got %v", transResult["from_state"])
	}
	if transResult["to_state"] != "queued" {
		t.Fatalf("expected to_state 'queued', got %v", transResult["to_state"])
	}

	// Step 4: GET /api/issues/{id} and verify state is queued and error_message is empty.
	getResp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET issue failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
	var issueData map[string]any
	json.NewDecoder(getResp.Body).Decode(&issueData)
	if issueData["state"] != "queued" {
		t.Fatalf("expected state 'queued', got %v", issueData["state"])
	}
	// error_message uses omitempty, so it's absent from JSON when empty.
	if em, ok := issueData["error_message"]; ok && em != "" {
		t.Fatalf("expected error_message empty/absent, got %v", em)
	}

	// Verify check_fix_attempts and error_message via direct DB query (not in GET response).
	dbIssue, err := d.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("get issue from DB: %v", err)
	}
	if dbIssue.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0 in DB, got %d", dbIssue.CheckFixAttempts)
	}
	if dbIssue.ErrorMessage != "" {
		t.Fatalf("expected error_message empty in DB, got %v", dbIssue.ErrorMessage)
	}

	// Step 5: Verify activity log contains state_change from failed to queued.
	activity, err := d.ListActivity(iss.ID, 10, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(activity) < 1 {
		t.Fatal("expected at least 1 activity entry")
	}
	found := false
	for _, a := range activity {
		if a.EventType == "state_change" && a.FromState == "failed" && a.ToState == "queued" && a.Detail == "Manual transition via API" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected activity entry with state_change from failed to queued with detail 'Manual transition via API'")
	}
}

// IT-002: Transition to in_review without PR returns 409.
func TestIntegration_IT002_TransitionToInReview_WithoutPR_Returns409(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	// Step 1: Create project and issue in paused state with pr_number=0.
	p, err := d.CreateProject(db.Project{Name: "it002-proj", LocalPath: "/tmp/it002"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	iss, err := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "IT-002 issue",
		State:     "paused",
		PRNumber:  0,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/transition with target_state=in_review.
	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "in_review",
	})
	defer resp.Body.Close()

	// Step 3: Assert response status 409 with error mentioning missing PR.
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var errResult map[string]string
	json.NewDecoder(resp.Body).Decode(&errResult)
	if errResult["error"] == "" {
		t.Fatal("expected error message about missing PR")
	}

	// Step 4: Verify issue state is still paused.
	updated, err := d.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if updated.State != "paused" {
		t.Fatalf("expected state still 'paused', got %v", updated.State)
	}
}

// IT-003: Transition to building without workspace returns 409.
func TestIntegration_IT003_TransitionToBuilding_WithoutWorkspace_Returns409(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	// Step 1: Create project and issue in failed state with workspace_name='' (empty).
	p, err := d.CreateProject(db.Project{Name: "it003-proj", LocalPath: "/tmp/it003"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	iss, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "IT-003 issue",
		State:         "failed",
		WorkspaceName: "",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/transition with target_state=building.
	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "building",
	})
	defer resp.Body.Close()

	// Step 3: Assert response status 409 with error mentioning missing workspace.
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var errResult map[string]string
	json.NewDecoder(resp.Body).Decode(&errResult)
	if errResult["error"] == "" {
		t.Fatal("expected error message about missing workspace")
	}

	// Step 4: Verify issue state is still failed.
	updated, err := d.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if updated.State != "failed" {
		t.Fatalf("expected state still 'failed', got %v", updated.State)
	}
}

// IT-004: Transition from completed state is rejected.
func TestIntegration_IT004_TransitionFromCompleted_Rejected(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	// Step 1: Create project and issue in completed state.
	p, err := d.CreateProject(db.Project{Name: "it004-proj", LocalPath: "/tmp/it004"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	iss, err := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "IT-004 issue",
		State:     "completed",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/transition with target_state=queued.
	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/transition"), map[string]any{
		"target_state": "queued",
	})
	defer resp.Body.Close()

	// Step 3: Assert response status 409.
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}

	// Step 4: GET /api/issues/{id}/transitions and verify empty transitions array.
	transResp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET transitions failed: %v", err)
	}
	defer transResp.Body.Close()
	if transResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", transResp.StatusCode)
	}
	var transData map[string]any
	json.NewDecoder(transResp.Body).Decode(&transData)
	transitions := transData["transitions"].([]any)
	if len(transitions) != 0 {
		t.Fatalf("expected empty transitions for completed state, got %d entries", len(transitions))
	}
}

// IT-005: Valid transitions endpoint returns correct states based on issue data.
func TestIntegration_IT005_ValidTransitions_ReflectIssueData(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, err := d.CreateProject(db.Project{Name: "it005-proj", LocalPath: "/tmp/it005"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Step 1: Create issue in in_review state with pr_number=42 and workspace_name='ws-1'.
	iss1, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "IT-005 issue with PR and workspace",
		State:         "in_review",
		PRNumber:      42,
		WorkspaceName: "ws-1",
	})
	if err != nil {
		t.Fatalf("create issue 1: %v", err)
	}

	// Step 2: GET /api/issues/{id}/transitions.
	resp1, err := http.Get(apiURL(srv, "/api/issues/"+iss1.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET transitions for issue 1 failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp1.StatusCode)
	}
	var data1 map[string]any
	json.NewDecoder(resp1.Body).Decode(&data1)
	transitions1 := data1["transitions"].([]any)

	// Step 3: Assert response includes addressing_feedback, fixing_checks, building, refining, queued.
	expected1 := map[string]bool{
		"addressing_feedback": false,
		"fixing_checks":       false,
		"building":            false,
		"refining":            false,
		"queued":              false,
	}
	for _, t1 := range transitions1 {
		entry := t1.(map[string]any)
		target := entry["target_state"].(string)
		if _, ok := expected1[target]; ok {
			expected1[target] = true
		}
	}
	for target, found := range expected1 {
		if !found {
			t.Fatalf("expected '%s' in transitions for in_review issue with PR+workspace, got transitions: %v", target, transitions1)
		}
	}

	// Step 4: Create another issue in failed state with no workspace and no PR.
	iss2, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		Title:         "IT-005 issue without PR or workspace",
		State:         "failed",
		PRNumber:      0,
		WorkspaceName: "",
	})
	if err != nil {
		t.Fatalf("create issue 2: %v", err)
	}

	// Step 5: GET /api/issues/{id}/transitions for the second issue.
	resp2, err := http.Get(apiURL(srv, "/api/issues/"+iss2.ID+"/transitions"))
	if err != nil {
		t.Fatalf("GET transitions for issue 2 failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	var data2 map[string]any
	json.NewDecoder(resp2.Body).Decode(&data2)
	transitions2 := data2["transitions"].([]any)

	// Step 6: Assert response includes queued, refining, approved but NOT building, in_review, addressing_feedback, fixing_checks.
	expectedPresent := map[string]bool{"queued": false, "refining": false, "approved": false}
	expectedAbsent := map[string]bool{"building": true, "in_review": true, "addressing_feedback": true, "fixing_checks": true}

	for _, t2 := range transitions2 {
		entry := t2.(map[string]any)
		target := entry["target_state"].(string)
		if _, ok := expectedPresent[target]; ok {
			expectedPresent[target] = true
		}
		if _, ok := expectedAbsent[target]; ok {
			t.Fatalf("'%s' should NOT be in transitions for failed issue without PR/workspace, got transitions: %v", target, transitions2)
		}
	}
	for target, found := range expectedPresent {
		if !found {
			t.Fatalf("expected '%s' in transitions for failed issue, got transitions: %v", target, transitions2)
		}
	}
}

// IT-006: Field reset without state change works correctly.
func TestIntegration_IT006_ResetFields_WithoutStateChange(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	// Step 1: Create project and issue in fixing_checks state with check_fix_attempts=5 and pr_number=1.
	p, err := d.CreateProject(db.Project{Name: "it006-proj", LocalPath: "/tmp/it006"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	iss, err := d.CreateIssue(db.Issue{
		ProjectID:        p.ID,
		Title:            "IT-006 issue",
		State:            "fixing_checks",
		CheckFixAttempts: 5,
		PRNumber:         1,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/reset with body {"fields": ["check_fix_attempts"]}.
	resp := postJSON(t, apiURL(srv, "/api/issues/"+iss.ID+"/reset"), map[string]any{
		"fields": []string{"check_fix_attempts"},
	})
	defer resp.Body.Close()

	// Step 3: Assert response status 200.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var resetResult map[string]any
	json.NewDecoder(resp.Body).Decode(&resetResult)
	if resetResult["status"] != "reset" {
		t.Fatalf("expected status 'reset', got %v", resetResult["status"])
	}

	// Step 4: Verify issue state is still fixing_checks.
	updated, err := d.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if updated.State != "fixing_checks" {
		t.Fatalf("expected state unchanged 'fixing_checks', got %v", updated.State)
	}

	// Step 5: Verify check_fix_attempts is 0 in database.
	if updated.CheckFixAttempts != 0 {
		t.Fatalf("expected check_fix_attempts 0, got %d", updated.CheckFixAttempts)
	}

	// Step 6: Verify activity log entry with event_type field_reset exists.
	activity, err := d.ListActivity(iss.ID, 10, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	found := false
	for _, a := range activity {
		if a.EventType == "field_reset" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected activity entry with event_type 'field_reset'")
	}
}

// IT-007: Existing Pause/Resume/Retry/Delete flows still work unchanged (regression).
func TestIntegration_IT007_ExistingFlows_PauseResumeRetryDelete(t *testing.T) {
	d := testDB(t)
	srv := newAPIServer(t, d)

	p, err := d.CreateProject(db.Project{Name: "it007-proj", LocalPath: "/tmp/it007"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Step 1: Create issue in building state.
	iss, err := d.CreateIssue(db.Issue{
		ProjectID: p.ID,
		Title:     "IT-007 issue",
		State:     "building",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: POST /api/issues/{id}/pause and verify state is paused.
	pauseResp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/pause"), "", nil)
	if err != nil {
		t.Fatalf("POST pause failed: %v", err)
	}
	defer pauseResp.Body.Close()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d", pauseResp.StatusCode)
	}
	var pauseResult map[string]string
	json.NewDecoder(pauseResp.Body).Decode(&pauseResult)
	if pauseResult["status"] != "paused" {
		t.Fatalf("expected status 'paused', got %v", pauseResult["status"])
	}
	if pauseResult["previous_state"] != "building" {
		t.Fatalf("expected previous_state 'building', got %v", pauseResult["previous_state"])
	}
	pausedIssue, _ := d.GetIssue(iss.ID)
	if pausedIssue.State != "paused" {
		t.Fatalf("expected DB state 'paused', got %v", pausedIssue.State)
	}

	// Step 3: POST /api/issues/{id}/resume and verify state restored to building.
	resumeResp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/resume"), "", nil)
	if err != nil {
		t.Fatalf("POST resume failed: %v", err)
	}
	defer resumeResp.Body.Close()
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", resumeResp.StatusCode)
	}
	var resumeResult map[string]string
	json.NewDecoder(resumeResp.Body).Decode(&resumeResult)
	if resumeResult["status"] != "resumed" {
		t.Fatalf("expected status 'resumed', got %v", resumeResult["status"])
	}
	if resumeResult["state"] != "building" {
		t.Fatalf("expected resumed state 'building', got %v", resumeResult["state"])
	}
	resumedIssue, _ := d.GetIssue(iss.ID)
	if resumedIssue.State != "building" {
		t.Fatalf("expected DB state 'building', got %v", resumedIssue.State)
	}

	// Step 4: Set issue state to failed with error_message.
	failedIssue, _ := d.GetIssue(iss.ID)
	failedIssue.State = "failed"
	failedIssue.ErrorMessage = "build error"
	if err := d.UpdateIssue(failedIssue); err != nil {
		t.Fatalf("update issue to failed: %v", err)
	}
	d.LogActivity(iss.ID, "state_change", "building", "failed", "build failed")

	// Step 5: POST /api/issues/{id}/retry and verify state restored and error cleared.
	retryResp, err := http.Post(apiURL(srv, "/api/issues/"+iss.ID+"/retry"), "", nil)
	if err != nil {
		t.Fatalf("POST retry failed: %v", err)
	}
	defer retryResp.Body.Close()
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry: expected 200, got %d", retryResp.StatusCode)
	}
	var retryResult map[string]string
	json.NewDecoder(retryResp.Body).Decode(&retryResult)
	if retryResult["status"] != "retrying" {
		t.Fatalf("expected status 'retrying', got %v", retryResult["status"])
	}
	if retryResult["state"] != "building" {
		t.Fatalf("expected retry state 'building', got %v", retryResult["state"])
	}
	retriedIssue, _ := d.GetIssue(iss.ID)
	if retriedIssue.State != "building" {
		t.Fatalf("expected DB state 'building', got %v", retriedIssue.State)
	}
	if retriedIssue.ErrorMessage != "" {
		t.Fatalf("expected error_message cleared, got %v", retriedIssue.ErrorMessage)
	}

	// Step 6: DELETE /api/issues/{id} and verify GET returns 404.
	deleteReq, _ := http.NewRequest("DELETE", apiURL(srv, "/api/issues/"+iss.ID), nil)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", deleteResp.StatusCode)
	}
	var deleteResult map[string]string
	json.NewDecoder(deleteResp.Body).Decode(&deleteResult)
	if deleteResult["status"] != "deleted" {
		t.Fatalf("expected status 'deleted', got %v", deleteResult["status"])
	}

	// Verify GET returns 404.
	getResp, err := http.Get(apiURL(srv, "/api/issues/"+iss.ID))
	if err != nil {
		t.Fatalf("GET after delete failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getResp.StatusCode)
	}
}
