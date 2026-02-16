package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/prd"
)

type apiHandler struct {
	db               *db.DB
	startAt          time.Time
	workspaceRemover WorkspaceRemover
	buildChecker     BuildChecker
	prdPathFn        func(string, string) string
}

// storyResponse represents a user story in the API response.
type storyResponse struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Passes bool   `json:"passes"`
}

// integrationTestResponse represents an integration test in the API response.
type integrationTestResponse struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Passes      bool   `json:"passes"`
}

// readPRD reads the PRD for an issue from disk, returning nil if unavailable.
func (h *apiHandler) readPRD(issue db.Issue) *prd.PRD {
	if h.prdPathFn == nil || issue.WorkspaceName == "" {
		return nil
	}
	project, err := h.db.GetProject(issue.ProjectID)
	if err != nil {
		return nil
	}
	path := h.prdPathFn(project.LocalPath, issue.WorkspaceName)
	p, err := prd.Read(path)
	if err != nil {
		return nil
	}
	return p
}

// parseCurrentStory extracts the active story ID from the most recent
// StoryStarted build event in the activity log (which is DESC-ordered).
func parseCurrentStory(activities []db.ActivityEntry) string {
	for _, a := range activities {
		if a.EventType == "build_event" && strings.HasPrefix(a.Detail, "Story ") {
			rest := strings.TrimPrefix(a.Detail, "Story ")
			if idx := strings.Index(rest, ":"); idx > 0 {
				return rest[:idx]
			}
		}
	}
	return ""
}

// parseIteration extracts the current iteration number and max from the most
// recent IterationStart build event in the activity log (which is DESC-ordered).
func parseIteration(activities []db.ActivityEntry) (int, int) {
	for _, a := range activities {
		if a.EventType == "build_event" && strings.HasPrefix(a.Detail, "Iteration ") {
			rest := strings.TrimPrefix(a.Detail, "Iteration ")
			rest = strings.TrimSuffix(rest, " started")
			var iter, max int
			if _, err := fmt.Sscanf(rest, "%d/%d", &iter, &max); err == nil {
				return iter, max
			}
		}
	}
	return 0, 0
}

// apiError is the consistent error response format.
type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// handleListProjects returns all projects with active issue counts.
func (h *apiHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.db.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	counts, err := h.db.CountActiveIssuesByProject()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count issues")
		return
	}

	type projectResponse struct {
		ID               string         `json:"id"`
		Name             string         `json:"name"`
		LocalPath        string         `json:"local_path"`
		GithubOwner      string         `json:"github_owner"`
		GithubRepo       string         `json:"github_repo"`
		ActiveIssueCount int            `json:"active_issue_count"`
		StateBreakdown   map[string]int `json:"state_breakdown"`
	}

	result := make([]projectResponse, len(projects))
	for i, p := range projects {
		stateBreakdown, _ := h.db.CountIssuesByStateForProject(p.ID)
		if stateBreakdown == nil {
			stateBreakdown = make(map[string]int)
		}
		result[i] = projectResponse{
			ID:               p.ID,
			Name:             p.Name,
			LocalPath:        p.LocalPath,
			GithubOwner:      p.GithubOwner,
			GithubRepo:       p.GithubRepo,
			ActiveIssueCount: counts[p.ID],
			StateBreakdown:   stateBreakdown,
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleListIssues returns issues, filterable by project_id and state.
func (h *apiHandler) handleListIssues(w http.ResponseWriter, r *http.Request) {
	filter := db.IssueFilter{
		ProjectID: r.URL.Query().Get("project_id"),
		State:     r.URL.Query().Get("state"),
	}

	issues, err := h.db.ListIssues(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	type issueResponse struct {
		ID            string `json:"id"`
		ProjectID     string `json:"project_id"`
		Identifier    string `json:"identifier"`
		Title         string `json:"title"`
		State         string `json:"state"`
		PRNumber      int    `json:"pr_number,omitempty"`
		PRURL         string `json:"pr_url,omitempty"`
		ErrorMessage  string `json:"error_message,omitempty"`
		WorkspaceName string `json:"workspace_name,omitempty"`
		BranchName    string `json:"branch_name,omitempty"`
		BuildActive   bool   `json:"build_active"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
	}

	result := make([]issueResponse, len(issues))
	for i, iss := range issues {
		var buildActive bool
		if h.buildChecker != nil {
			buildActive = h.buildChecker.IsRunning(iss.ID)
		}
		result[i] = issueResponse{
			ID:            iss.ID,
			ProjectID:     iss.ProjectID,
			Identifier:    iss.Identifier,
			Title:         iss.Title,
			State:         iss.State,
			PRNumber:      iss.PRNumber,
			PRURL:         iss.PRURL,
			ErrorMessage:  iss.ErrorMessage,
			WorkspaceName: iss.WorkspaceName,
			BranchName:    iss.BranchName,
			BuildActive:   buildActive,
			CreatedAt:     iss.CreatedAt.Format(time.RFC3339),
			UpdatedAt:     iss.UpdatedAt.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetIssue returns issue detail with activity timeline (paginated).
func (h *apiHandler) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing issue id")
		return
	}

	issue, err := h.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "issue not found") {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	var projectName string
	if proj, err := h.db.GetProject(issue.ProjectID); err == nil {
		projectName = proj.Name
	}

	buildLimit := 200
	buildOffset := 0
	timelineLimit := 50
	timelineOffset := 0
	if l := r.URL.Query().Get("build_limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			buildLimit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			buildOffset = parsed
		}
	}
	if l := r.URL.Query().Get("timeline_limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			timelineLimit = parsed
		}
	}
	if o := r.URL.Query().Get("timeline_offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			timelineOffset = parsed
		}
	}

	buildActivity, err := h.db.ListBuildActivity(issue.ID, buildLimit, buildOffset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list build activity")
		return
	}

	timelineActivity, err := h.db.ListTimelineActivity(issue.ID, timelineLimit, timelineOffset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list timeline activity")
		return
	}

	type activityResponse struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		FromState string `json:"from_state,omitempty"`
		ToState   string `json:"to_state,omitempty"`
		Detail    string `json:"detail,omitempty"`
		CreatedAt string `json:"created_at"`
	}

	type issueDetailResponse struct {
		ID               string                    `json:"id"`
		ProjectID        string                    `json:"project_id"`
		ProjectName      string                    `json:"project_name"`
		LinearIssueID    string                    `json:"linear_issue_id"`
		Identifier       string                    `json:"identifier"`
		Title            string                    `json:"title"`
		Description      string                    `json:"description"`
		State            string                    `json:"state"`
		PlanText         string                    `json:"plan_text,omitempty"`
		WorkspaceName    string                    `json:"workspace_name,omitempty"`
		BranchName       string                    `json:"branch_name,omitempty"`
		PRNumber         int                       `json:"pr_number,omitempty"`
		PRURL            string                    `json:"pr_url,omitempty"`
		ErrorMessage     string                    `json:"error_message,omitempty"`
		BuildActive      bool                      `json:"build_active"`
		Stories          []storyResponse            `json:"stories"`
		IntegrationTests []integrationTestResponse  `json:"integration_tests"`
		CurrentStory     string                    `json:"current_story,omitempty"`
		Iteration        int                       `json:"iteration,omitempty"`
		MaxIterations    int                       `json:"max_iterations,omitempty"`
		CreatedAt        string                    `json:"created_at"`
		UpdatedAt        string                    `json:"updated_at"`
		Activity         []activityResponse         `json:"activity"`
		BuildActivityOut []activityResponse         `json:"build_activity"`
	}

	toResponse := func(entries []db.ActivityEntry) []activityResponse {
		result := make([]activityResponse, len(entries))
		for i, a := range entries {
			result[i] = activityResponse{
				ID:        a.ID,
				EventType: a.EventType,
				FromState: a.FromState,
				ToState:   a.ToState,
				Detail:    a.Detail,
				CreatedAt: a.CreatedAt.Format(time.RFC3339),
			}
		}
		return result
	}

	activityResult := toResponse(timelineActivity)
	buildActivityResult := toResponse(buildActivity)

	var buildActive bool
	if h.buildChecker != nil {
		buildActive = h.buildChecker.IsRunning(issue.ID)
	}

	// Read PRD for story/test data.
	var stories []storyResponse
	var integrationTests []integrationTestResponse
	if p := h.readPRD(issue); p != nil {
		stories = make([]storyResponse, len(p.UserStories))
		for i, s := range p.UserStories {
			stories[i] = storyResponse{ID: s.ID, Title: s.Title, Passes: s.Passes}
		}
		integrationTests = make([]integrationTestResponse, len(p.IntegrationTests))
		for i, t := range p.IntegrationTests {
			integrationTests[i] = integrationTestResponse{ID: t.ID, Description: t.Description, Passes: t.Passes}
		}
	}
	if stories == nil {
		stories = []storyResponse{}
	}
	if integrationTests == nil {
		integrationTests = []integrationTestResponse{}
	}

	currentStory := parseCurrentStory(buildActivity)
	iteration, maxIterations := parseIteration(buildActivity)

	resp := issueDetailResponse{
		ID:               issue.ID,
		ProjectID:        issue.ProjectID,
		ProjectName:      projectName,
		LinearIssueID:    issue.LinearIssueID,
		Identifier:       issue.Identifier,
		Title:            issue.Title,
		Description:      issue.Description,
		State:            issue.State,
		PlanText:         issue.PlanText,
		WorkspaceName:    issue.WorkspaceName,
		BranchName:       issue.BranchName,
		PRNumber:         issue.PRNumber,
		PRURL:            issue.PRURL,
		ErrorMessage:     issue.ErrorMessage,
		BuildActive:      buildActive,
		Stories:          stories,
		IntegrationTests: integrationTests,
		CurrentStory:     currentStory,
		Iteration:        iteration,
		MaxIterations:    maxIterations,
		CreatedAt:        issue.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        issue.UpdatedAt.Format(time.RFC3339),
		Activity:         activityResult,
		BuildActivityOut: buildActivityResult,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePauseIssue sets issue to PAUSED state.
func (h *apiHandler) handlePauseIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing issue id")
		return
	}

	issue, err := h.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "issue not found") {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	pausable := map[string]bool{
		"queued": true, "refining": true, "approved": true,
		"building": true, "in_review": true, "addressing_feedback": true,
	}
	if !pausable[issue.State] {
		writeError(w, http.StatusConflict, "issue cannot be paused from state: "+issue.State)
		return
	}

	previousState := issue.State
	issue.State = "paused"
	if err := h.db.UpdateIssue(issue); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}
	h.db.LogActivity(issue.ID, "state_change", previousState, "paused", "Issue paused via API")

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused", "previous_state": previousState})
}

// handleResumeIssue resumes from PAUSED state.
func (h *apiHandler) handleResumeIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing issue id")
		return
	}

	issue, err := h.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "issue not found") {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	if issue.State != "paused" {
		writeError(w, http.StatusConflict, "issue is not paused, current state: "+issue.State)
		return
	}

	// Determine the state to resume to based on the last activity log entry.
	resumeState := "queued"
	activity, err := h.db.ListActivity(issue.ID, 10, 0)
	if err == nil {
		for _, a := range activity {
			if a.ToState == "paused" && a.FromState != "" {
				resumeState = a.FromState
				break
			}
		}
	}

	issue.State = resumeState
	if err := h.db.UpdateIssue(issue); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}
	h.db.LogActivity(issue.ID, "state_change", "paused", resumeState, "Issue resumed via API")

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "state": resumeState})
}

// handleRetryIssue retries from FAILED state.
func (h *apiHandler) handleRetryIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing issue id")
		return
	}

	issue, err := h.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "issue not found") {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	if issue.State != "failed" {
		writeError(w, http.StatusConflict, "issue is not failed, current state: "+issue.State)
		return
	}

	// Determine the state to retry from based on the last activity log entry.
	retryState := "queued"
	activity, err := h.db.ListActivity(issue.ID, 10, 0)
	if err == nil {
		for _, a := range activity {
			if a.ToState == "failed" && a.FromState != "" {
				retryState = a.FromState
				break
			}
		}
	}

	issue.State = retryState
	issue.ErrorMessage = ""
	if err := h.db.UpdateIssue(issue); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}
	h.db.LogActivity(issue.ID, "state_change", "failed", retryState, "Issue retried via API")

	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying", "state": retryState})
}

// handleDeleteIssue deletes an issue from autoralph permanently.
func (h *apiHandler) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing issue id")
		return
	}

	issue, err := h.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "issue not found") {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	// Remove workspace if one was created (non-fatal).
	if issue.WorkspaceName != "" && h.workspaceRemover != nil {
		if project, pErr := h.db.GetProject(issue.ProjectID); pErr == nil {
			_ = h.workspaceRemover.RemoveWorkspace(r.Context(), project.LocalPath, issue.WorkspaceName)
		}
	}

	if err := h.db.DeleteIssue(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleListActivity returns recent activity across all issues.
func (h *apiHandler) handleListActivity(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	entries, err := h.db.ListRecentActivity(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activity")
		return
	}

	type activityResponse struct {
		ID        string `json:"id"`
		IssueID   string `json:"issue_id"`
		EventType string `json:"event_type"`
		FromState string `json:"from_state,omitempty"`
		ToState   string `json:"to_state,omitempty"`
		Detail    string `json:"detail,omitempty"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]activityResponse, len(entries))
	for i, a := range entries {
		result[i] = activityResponse{
			ID:        a.ID,
			IssueID:   a.IssueID,
			EventType: a.EventType,
			FromState: a.FromState,
			ToState:   a.ToState,
			Detail:    a.Detail,
			CreatedAt: a.CreatedAt.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleStatus returns orchestrator health.
func (h *apiHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startAt).Round(time.Second).String()

	// Count active builds (issues in "building" state)
	buildingIssues, _ := h.db.ListIssues(db.IssueFilter{State: "building"})
	activeBuilds := len(buildingIssues)

	type statusResponse struct {
		Status       string `json:"status"`
		Uptime       string `json:"uptime"`
		ActiveBuilds int    `json:"active_builds"`
	}

	resp := statusResponse{
		Status:       "ok",
		Uptime:       uptime,
		ActiveBuilds: activeBuilds,
	}
	writeJSON(w, http.StatusOK, resp)
}

