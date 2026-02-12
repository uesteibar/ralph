package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/approve"
	"github.com/uesteibar/ralph/internal/autoralph/complete"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/ghpoller"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/autoralph/poller"
	"github.com/uesteibar/ralph/internal/autoralph/refine"
	"github.com/uesteibar/ralph/internal/autoralph/worker"
	mockgithub "github.com/uesteibar/ralph/test/e2e/mocks/github"
	mocklinear "github.com/uesteibar/ralph/test/e2e/mocks/linear"
)

// mockInvoker is a mock AI invoker that returns a fixed response.
type mockInvoker struct {
	response string
	calls    int32
}

func (m *mockInvoker) Invoke(_ context.Context, _, _ string) (string, error) {
	atomic.AddInt32(&m.calls, 1)
	return m.response, nil
}

func (m *mockInvoker) CallCount() int {
	return int(atomic.LoadInt32(&m.calls))
}

// linearCommentPoster wraps a linear.Client for the refine.Poster interface.
type linearCommentPoster struct {
	client *linear.Client
}

func (p *linearCommentPoster) PostComment(ctx context.Context, issueID, body string) (string, error) {
	c, err := p.client.PostComment(ctx, issueID, body)
	return c.ID, err
}

// mockLoopRunner mocks the Ralph build loop for worker tests.
type mockLoopRunner struct {
	err     error
	delay   time.Duration
	started chan string // sends issue WorkDir when started
}

func newMockLoopRunner() *mockLoopRunner {
	return &mockLoopRunner{
		started: make(chan string, 10),
	}
}

func (m *mockLoopRunner) Run(_ context.Context, cfg worker.LoopConfig) error {
	m.started <- cfg.WorkDir
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.err
}

// mockWorkspaceRemover is a no-op workspace remover.
type mockWorkspaceRemover struct{}

func (m *mockWorkspaceRemover) RemoveWorkspace(_ context.Context, _, _ string) error {
	return nil
}

// mockLinearStateUpdater wraps the Linear client for complete.LinearStateUpdater.
type mockLinearStateUpdater struct {
	client *linear.Client
}

func (m *mockLinearStateUpdater) FetchWorkflowStates(ctx context.Context, teamID string) ([]complete.WorkflowState, error) {
	states, err := m.client.FetchWorkflowStates(ctx, teamID)
	if err != nil {
		return nil, err
	}
	var result []complete.WorkflowState
	for _, s := range states {
		result = append(result, complete.WorkflowState{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return result, nil
}

func (m *mockLinearStateUpdater) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	return m.client.UpdateIssueState(ctx, issueID, stateID)
}

// --- IT-001: Full issue lifecycle happy path ---

func TestIT001_FullLifecycleHappyPath(t *testing.T) {
	pg := StartPlayground(t)
	linearClient := linear.New("test-key", linear.WithEndpoint(pg.LinearURL))
	githubClient, err := github.New("test-token", github.WithBaseURL(pg.GitHubURL+"/"))
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	projects, _ := pg.DB.ListProjects()
	proj := projects[0]

	// Step 1: Seed mock Linear with an assigned issue.
	avatarIssueID := mocklinear.IssueUUID("avatars")
	pg.Linear.AddIssue(mocklinear.Issue{
		ID:         avatarIssueID,
		Identifier: "TEST-1",
		Title:      "Add user avatars",
		StateID:    mocklinear.StateTodoID,
		StateName:  "Todo",
		StateType:  "unstarted",
	})

	// Step 2: Run the poller to ingest it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := poller.New(pg.DB, []poller.ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     proj.LinearTeamID,
		LinearAssigneeID: proj.LinearAssigneeID,
		LinearClient:     linearClient,
	}}, time.Hour, nil)

	// Single poll cycle.
	go func() {
		p.Run(ctx)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify issue ingested as QUEUED.
	issues, err := pg.DB.ListIssues(db.IssueFilter{State: "queued"})
	if err != nil {
		t.Fatalf("listing issues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 queued issue, got %d", len(issues))
	}
	issue := issues[0]
	if issue.Identifier != "TEST-1" {
		t.Errorf("expected identifier TEST-1, got %s", issue.Identifier)
	}

	// Step 3: Transition QUEUED → REFINING (via orchestrator + refine action).
	aiInvoker := &mockInvoker{response: "Here are some clarifying questions:\n1. What avatar size?\n2. Default avatar?"}

	sm := orchestrator.New(pg.DB)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
		Action: refine.NewAction(refine.Config{
			Invoker:  aiInvoker,
			Poster:   &linearCommentPoster{client: linearClient},
			Projects: pg.DB,
		}),
	})

	tr, ok := sm.Evaluate(issue)
	if !ok {
		t.Fatal("expected QUEUED → REFINING transition")
	}
	if err := sm.Execute(tr, issue); err != nil {
		t.Fatalf("executing refine transition: %v", err)
	}

	// Verify issue is now REFINING.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "refining" {
		t.Errorf("expected state refining, got %s", issue.State)
	}

	// Verify mock Linear received a comment.
	if len(pg.Linear.ReceivedComments) == 0 {
		t.Fatal("expected Linear to receive a refinement comment")
	}

	// Step 4: Simulate human reply.
	pg.Linear.AddComment(avatarIssueID, mocklinear.Comment{
		ID:        mocklinear.CommentUUID("human-reply-1"),
		Body:      "Avatar size should be 128x128, use gravatar for defaults",
		UserName:  "test-user",
		CreatedAt: "2026-01-01T00:01:00Z",
	})

	// Step 5: Register approval transitions and simulate '@autoralph approved'.
	commentClient := linearClient // linear.Client implements approve.CommentClient

	// First simulate a plan response from autoralph.
	pg.Linear.AddComment(avatarIssueID, mocklinear.Comment{
		ID:        mocklinear.CommentUUID("plan-comment"),
		Body:      "Here's the plan:\n1. Add gravatar integration\n2. Resize to 128x128\n3. Cache results",
		UserName:  "autoralph",
		CreatedAt: "2026-01-01T00:02:00Z",
	})

	// Then simulate approval.
	pg.Linear.SimulateApproval(avatarIssueID, mocklinear.CommentUUID("approval-comment"))

	sm2 := orchestrator.New(pg.DB)
	sm2.Register(orchestrator.Transition{
		From:      orchestrator.StateRefining,
		To:        orchestrator.StateApproved,
		Condition: approve.IsApproval(commentClient),
		Action:    approve.NewApprovalAction(approve.Config{Comments: commentClient, Projects: pg.DB}),
	})

	issue, _ = pg.DB.GetIssue(issue.ID)
	t.Logf("DEBUG: issue.LastCommentID=%q, issue.State=%q", issue.LastCommentID, issue.State)
	cs, _ := commentClient.FetchIssueComments(context.Background(), issue.LinearIssueID)
	for i, c := range cs {
		t.Logf("DEBUG: comment[%d] id=%s body=%.50s", i, c.ID, c.Body)
	}
	tr, ok = sm2.Evaluate(issue)
	if !ok {
		t.Fatal("expected REFINING → APPROVED transition")
	}
	if err := sm2.Execute(tr, issue); err != nil {
		t.Fatalf("executing approval transition: %v", err)
	}

	// Verify state is APPROVED.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "approved" {
		t.Errorf("expected state approved, got %s", issue.State)
	}

	// Step 6: Transition APPROVED → BUILDING (simulate by directly setting state).
	// The real build.NewAction requires workspace creation which needs the full git setup.
	// For integration testing, we simulate the build setup.
	issue.State = "building"
	issue.WorkspaceName = "test-1"
	issue.BranchName = "autoralph/test-1"
	pg.DB.UpdateIssue(issue)
	pg.DB.LogActivity(issue.ID, "state_change", "approved", "building", "Workspace created")

	// Step 7: Simulate build completion via worker dispatch with a mock loop runner.
	runner := newMockLoopRunner()
	dispatcher := worker.New(worker.Config{
		DB:         pg.DB,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   pg.DB,
		Logger:     nil,
	})

	issue, _ = pg.DB.GetIssue(issue.ID)
	if err := dispatcher.Dispatch(context.Background(), issue); err != nil {
		t.Fatalf("dispatching build: %v", err)
	}

	// Wait for build to complete.
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("build worker did not start within 5s")
	}
	dispatcher.Wait()

	// Verify state transitioned to in_review (no PRCreator → direct transition).
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Errorf("expected state in_review after build, got %s", issue.State)
	}

	// Step 8: Simulate PR creation (manually seed PR info since we don't have a real PR action).
	issue.PRNumber = 42
	issue.PRURL = "https://github.com/test-owner/test-repo/pull/42"
	pg.DB.UpdateIssue(issue)

	pg.GitHub.AddPR("test-owner", "test-repo", mockgithub.PR{
		Number: 42,
		Head:   "autoralph/test-1",
		Base:   "main",
		State:  "open",
	})

	// Step 9: Simulate PR merge.
	pg.GitHub.SimulateMerge("test-owner", "test-repo", 42)

	// Step 10: Run GitHub poller with completion action.
	completeAction := complete.NewAction(complete.Config{
		Workspace: &mockWorkspaceRemover{},
		Linear:    &mockLinearStateUpdater{client: linearClient},
		Projects:  pg.DB,
	})

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	ghp := ghpoller.New(pg.DB, []ghpoller.ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: proj.GithubOwner,
		GithubRepo:  proj.GithubRepo,
		GitHub:      githubClient,
	}}, time.Hour, nil, completeAction)

	go ghp.Run(ctx2)
	time.Sleep(200 * time.Millisecond)
	cancel2()

	// Step 11: Verify state transitioned to COMPLETED.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "completed" {
		t.Errorf("expected state completed, got %s", issue.State)
	}

	// Step 12: Verify Linear state updated to 'Done'.
	doneUpdates := 0
	for _, su := range pg.Linear.StateUpdates {
		if su.StateID == mocklinear.StateDoneID {
			doneUpdates++
		}
	}
	if doneUpdates == 0 {
		t.Error("expected Linear state to be updated to 'Done'")
	}

	// Verify activity log records the full lifecycle.
	activity, _ := pg.DB.ListActivity(issue.ID, 100, 0)
	if len(activity) < 3 {
		t.Errorf("expected at least 3 activity entries, got %d", len(activity))
	}
}

// --- IT-002: Refinement loop with multiple rounds ---

func TestIT002_RefinementLoop(t *testing.T) {
	pg := StartPlayground(t)
	linearClient := linear.New("test-key", linear.WithEndpoint(pg.LinearURL))

	projects, _ := pg.DB.ListProjects()
	proj := projects[0]

	// Step 1: Seed a vague issue.
	perfIssueID := mocklinear.IssueUUID("perf")
	pg.Linear.AddIssue(mocklinear.Issue{
		ID:          perfIssueID,
		Identifier:  "TEST-2",
		Title:       "Improve performance",
		Description: "The app is slow",
		StateID:     mocklinear.StateTodoID,
		StateName:   "Todo",
		StateType:   "unstarted",
	})

	// Ingest via poller.
	ctx, cancel := context.WithCancel(context.Background())
	p := poller.New(pg.DB, []poller.ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     proj.LinearTeamID,
		LinearAssigneeID: proj.LinearAssigneeID,
		LinearClient:     linearClient,
	}}, time.Hour, nil)
	go p.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	issues, _ := pg.DB.ListIssues(db.IssueFilter{State: "queued"})
	if len(issues) != 1 {
		t.Fatalf("expected 1 queued issue, got %d", len(issues))
	}
	issue := issues[0]

	// Step 2: Transition QUEUED → REFINING.
	aiInvoker := &mockInvoker{response: "What specific part of the app is slow? Frontend or backend?"}

	sm := orchestrator.New(pg.DB)
	sm.Register(orchestrator.Transition{
		From: orchestrator.StateQueued,
		To:   orchestrator.StateRefining,
		Action: refine.NewAction(refine.Config{
			Invoker:  aiInvoker,
			Poster:   &linearCommentPoster{client: linearClient},
			Projects: pg.DB,
		}),
	})
	tr, _ := sm.Evaluate(issue)
	sm.Execute(tr, issue)

	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "refining" {
		t.Fatalf("expected refining, got %s", issue.State)
	}

	// Step 3: Verify Linear received a comment.
	if len(pg.Linear.ReceivedComments) == 0 {
		t.Fatal("expected at least one Linear comment")
	}

	// Step 4: Simulate partial human reply (must be after the AI's comment).
	pg.Linear.AddComment(perfIssueID, mocklinear.Comment{
		ID:        mocklinear.CommentUUID("reply-1"),
		Body:      "Backend API calls are slow, especially the user listing endpoint",
		UserName:  "test-user",
		CreatedAt: "2098-01-01T00:00:00Z",
	})

	// Step 5: Iteration (AI responds with follow-up).
	aiInvoker2 := &mockInvoker{response: "Updated plan:\n1. Profile user listing endpoint\n2. Add DB indexes\n3. Implement pagination"}

	sm2 := orchestrator.New(pg.DB)
	sm2.Register(orchestrator.Transition{
		From:      orchestrator.StateRefining,
		To:        orchestrator.StateApproved,
		Condition: approve.IsApproval(linearClient),
		Action:    approve.NewApprovalAction(approve.Config{Comments: linearClient, Projects: pg.DB}),
	})
	sm2.Register(orchestrator.Transition{
		From:      orchestrator.StateRefining,
		To:        orchestrator.StateRefining,
		Condition: approve.IsIteration(linearClient),
		Action: approve.NewIterationAction(approve.Config{
			Invoker:  aiInvoker2,
			Comments: linearClient,
			Projects: pg.DB,
		}),
	})

	issue, _ = pg.DB.GetIssue(issue.ID)
	tr, ok := sm2.Evaluate(issue)
	if !ok {
		t.Fatal("expected iteration transition")
	}
	if tr.To != orchestrator.StateRefining {
		t.Errorf("expected self-transition to refining, got %s", tr.To)
	}
	if err := sm2.Execute(tr, issue); err != nil {
		t.Fatalf("executing iteration: %v", err)
	}

	// Verify Linear received a follow-up comment.
	if len(pg.Linear.ReceivedComments) < 2 {
		t.Error("expected at least 2 Linear comments (refinement + iteration)")
	}

	// Step 6: Now approve.
	pg.Linear.SimulateApproval(perfIssueID, mocklinear.CommentUUID("approval-2"))

	issue, _ = pg.DB.GetIssue(issue.ID)
	tr, ok = sm2.Evaluate(issue)
	if !ok {
		t.Fatal("expected approval transition")
	}
	if tr.To != orchestrator.StateApproved {
		t.Errorf("expected transition to approved, got %s", tr.To)
	}
	sm2.Execute(tr, issue)

	// Step 7: Verify state is APPROVED.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "approved" {
		t.Errorf("expected approved, got %s", issue.State)
	}
}

// --- IT-003: Review feedback loop ---

func TestIT003_ReviewFeedbackLoop(t *testing.T) {
	pg := StartPlayground(t)
	githubClient, err := github.New("test-token", github.WithBaseURL(pg.GitHubURL+"/"))
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	projects, _ := pg.DB.ListProjects()
	proj := projects[0]

	// Step 1: Seed an issue in IN_REVIEW state with a PR.
	issue := pg.SeedIssue("TEST-3", "Fix login bug", "in_review")
	issue.PRNumber = 10
	issue.PRURL = "https://github.com/test-owner/test-repo/pull/10"
	issue.WorkspaceName = "test-3"
	issue.BranchName = "autoralph/test-3"
	pg.DB.UpdateIssue(issue)

	pg.GitHub.AddPR("test-owner", "test-repo", mockgithub.PR{
		Number: 10,
		Head:   "autoralph/test-3",
		Base:   "main",
		State:  "open",
	})

	// Step 2: Simulate CHANGES_REQUESTED review.
	pg.GitHub.SimulateChangesRequested("test-owner", "test-repo", 10, 100, "Please add error handling")

	// Step 3: Run GitHub poller to detect review.
	ctx, cancel := context.WithCancel(context.Background())
	ghp := ghpoller.New(pg.DB, []ghpoller.ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: proj.GithubOwner,
		GithubRepo:  proj.GithubRepo,
		GitHub:      githubClient,
	}}, time.Hour, nil, nil)
	go ghp.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify state transitioned to ADDRESSING_FEEDBACK.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "addressing_feedback" {
		t.Errorf("expected addressing_feedback, got %s", issue.State)
	}

	// Step 4: Verify activity logged.
	activity, _ := pg.DB.ListActivity(issue.ID, 10, 0)
	found := false
	for _, a := range activity {
		if a.EventType == "changes_requested" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected changes_requested activity logged")
	}

	// Step 5: Simulate addressing feedback (transition back to IN_REVIEW).
	// The real feedback action would invoke AI, commit, push, and reply.
	// For integration testing, we simulate the state transition.
	issue.State = "in_review"
	pg.DB.UpdateIssue(issue)
	pg.DB.LogActivity(issue.ID, "feedback_addressed", "addressing_feedback", "in_review", "Addressed 1 comments")

	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Errorf("expected in_review after feedback, got %s", issue.State)
	}
}

// --- IT-004: Build failure and retry ---

func TestIT004_BuildFailureAndRetry(t *testing.T) {
	pg := StartPlayground(t)

	// Seed issue in BUILDING state.
	issue := pg.SeedIssue("TEST-4", "Add validation", "building")
	issue.WorkspaceName = "test-4"
	issue.BranchName = "autoralph/test-4"
	pg.DB.UpdateIssue(issue)

	// Configure mock loop runner to fail (simulating quality check failures).
	runner := newMockLoopRunner()
	runner.err = fmt.Errorf("quality checks failed: go test returned exit code 1")

	dispatcher := worker.New(worker.Config{
		DB:         pg.DB,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   pg.DB,
		Logger:     nil,
	})

	if err := dispatcher.Dispatch(context.Background(), issue); err != nil {
		t.Fatalf("dispatching build: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("build didn't start")
	}
	dispatcher.Wait()

	// Verify state is FAILED with error message.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "failed" {
		t.Errorf("expected failed, got %s", issue.State)
	}
	if issue.ErrorMessage == "" {
		t.Error("expected error_message to be set")
	}
	if !strings.Contains(issue.ErrorMessage, "quality checks failed") {
		t.Errorf("expected error message about quality checks, got: %s", issue.ErrorMessage)
	}

	// Step 5: Call POST /api/issues/:id/retry via REST API.
	retryURL := fmt.Sprintf("%s/api/issues/%s/retry", pg.BaseURL(), issue.ID)
	resp, err := http.Post(retryURL, "application/json", nil)
	if err != nil {
		t.Fatalf("calling retry API: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 from retry, got %d: %s", resp.StatusCode, body)
	}

	// Verify state reset.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State == "failed" {
		t.Error("expected state to be reset from failed")
	}
	if issue.ErrorMessage != "" {
		t.Error("expected error_message to be cleared")
	}
}

// --- IT-005: API failure resilience ---

func TestIT005_APIFailureResilience(t *testing.T) {
	pg := StartPlayground(t)

	projects, _ := pg.DB.ListProjects()
	proj := projects[0]

	// Create a Linear client that talks to our mock.
	// The mock itself doesn't support 500 injection, so we test the retry
	// logic at the client level using a custom HTTP handler.
	failCount := int32(0)
	failHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&failCount, 1)
		if current <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"errors":[{"message":"internal error"}]}`))
			return
		}
		// After 2 failures, proxy to the real mock.
		pg.Linear.Handler().ServeHTTP(w, r)
	})

	failSrv := startHTTPServer(t, failHandler)

	// Seed an issue on the real mock (so it's available when requests succeed).
	pg.Linear.AddIssue(mocklinear.Issue{
		ID:         mocklinear.IssueUUID("retry-1"),
		Identifier: "TEST-5",
		Title:      "Retry test issue",
		StateID:    mocklinear.StateTodoID,
		StateName:  "Todo",
		StateType:  "unstarted",
	})

	// Create a client with no retry backoff (instant retries for test speed).
	linearClient := linear.New("test-key",
		linear.WithEndpoint(failSrv),
		linear.WithRetryBackoff(time.Millisecond, time.Millisecond, time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	p := poller.New(pg.DB, []poller.ProjectInfo{{
		ProjectID:        proj.ID,
		LinearTeamID:     proj.LinearTeamID,
		LinearAssigneeID: proj.LinearAssigneeID,
		LinearClient:     linearClient,
	}}, time.Hour, nil)
	go p.Run(ctx)
	time.Sleep(500 * time.Millisecond)
	cancel()

	// Verify issue was eventually ingested despite initial failures.
	issues, _ := pg.DB.ListIssues(db.IssueFilter{State: "queued"})
	if len(issues) != 1 {
		t.Fatalf("expected 1 queued issue (after retries), got %d", len(issues))
	}
	if issues[0].Identifier != "TEST-5" {
		t.Errorf("expected TEST-5, got %s", issues[0].Identifier)
	}

	// Verify no duplicate issues.
	allIssues, _ := pg.DB.ListIssues(db.IssueFilter{})
	duplicates := 0
	for _, iss := range allIssues {
		if iss.Identifier == "TEST-5" {
			duplicates++
		}
	}
	if duplicates > 1 {
		t.Errorf("expected 1 TEST-5 issue, found %d (duplicates!)", duplicates)
	}
}

// --- IT-006: Multi-project with different credential profiles ---

func TestIT006_MultiProjectCredentials(t *testing.T) {
	pg := StartPlayground(t, func(cfg *PlaygroundConfig) {
		cfg.SeedProject = false
	})

	// Create two separate Linear mock servers (simulating different credentials).
	// Use deterministic UUIDs for each project's team/assignee.
	const (
		teamPersonalID     = "a0000000-0000-0000-0000-000000000001"
		assigneePersonalID = "a0000000-0000-0000-0000-000000000002"
		teamWorkID         = "b0000000-0000-0000-0000-000000000001"
		assigneeWorkID     = "b0000000-0000-0000-0000-000000000002"
	)

	linear1 := mocklinear.New()
	linear1.AddTeam(mocklinear.Team{ID: teamPersonalID, Key: "PERS", Name: "Personal Team"})
	linear1.AddUser(mocklinear.User{ID: assigneePersonalID, Name: "Personal Dev", DisplayName: "personaldev", Email: "dev@personal.com"})
	srv1 := linear1.Server(t)

	linear2 := mocklinear.New()
	linear2.AddTeam(mocklinear.Team{ID: teamWorkID, Key: "WORK", Name: "Work Team"})
	linear2.AddUser(mocklinear.User{ID: assigneeWorkID, Name: "Work Dev", DisplayName: "workdev", Email: "dev@work.com"})
	srv2 := linear2.Server(t)

	// Create two projects in DB.
	projA, _ := pg.DB.CreateProject(db.Project{
		Name:               "project-personal",
		LocalPath:          pg.ProjectDir,
		CredentialsProfile: "personal",
		GithubOwner:        "user-personal",
		GithubRepo:         "repo-personal",
		LinearTeamID:       teamPersonalID,
		LinearAssigneeID:   assigneePersonalID,
		RalphConfigPath:    ".ralph/ralph.yaml",
		MaxIterations:      20,
		BranchPrefix:       "autoralph/",
	})
	projB, _ := pg.DB.CreateProject(db.Project{
		Name:               "project-work",
		LocalPath:          pg.ProjectDir,
		CredentialsProfile: "work",
		GithubOwner:        "org-work",
		GithubRepo:         "repo-work",
		LinearTeamID:       teamWorkID,
		LinearAssigneeID:   assigneeWorkID,
		RalphConfigPath:    ".ralph/ralph.yaml",
		MaxIterations:      20,
		BranchPrefix:       "autoralph/",
	})

	// Seed an issue on each mock.
	linear1.AddIssue(mocklinear.Issue{
		ID:         mocklinear.IssueUUID("personal-1"),
		Identifier: "PERS-1",
		Title:      "Personal project issue",
		StateID:    mocklinear.StateTodoID,
		StateName:  "Todo",
		StateType:  "unstarted",
	})
	linear2.AddIssue(mocklinear.Issue{
		ID:         mocklinear.IssueUUID("work-1"),
		Identifier: "WORK-1",
		Title:      "Work project issue",
		StateID:    mocklinear.StateTodoID,
		StateName:  "Todo",
		StateType:  "unstarted",
	})

	// Create separate Linear clients for each project (simulating different API keys).
	clientA := linear.New("personal-key", linear.WithEndpoint(srv1.URL))
	clientB := linear.New("work-key", linear.WithEndpoint(srv2.URL))

	// Run poller with both projects.
	ctx, cancel := context.WithCancel(context.Background())
	p := poller.New(pg.DB, []poller.ProjectInfo{
		{
			ProjectID:        projA.ID,
			LinearTeamID:     projA.LinearTeamID,
			LinearAssigneeID: projA.LinearAssigneeID,
			LinearClient:     clientA,
		},
		{
			ProjectID:        projB.ID,
			LinearTeamID:     projB.LinearTeamID,
			LinearAssigneeID: projB.LinearAssigneeID,
			LinearClient:     clientB,
		},
	}, time.Hour, nil)
	go p.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify issues ingested from both projects.
	issues, _ := pg.DB.ListIssues(db.IssueFilter{})
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues (one per project), got %d", len(issues))
	}

	foundPersonal := false
	foundWork := false
	for _, iss := range issues {
		if iss.Identifier == "PERS-1" && iss.ProjectID == projA.ID {
			foundPersonal = true
		}
		if iss.Identifier == "WORK-1" && iss.ProjectID == projB.ID {
			foundWork = true
		}
	}
	if !foundPersonal {
		t.Error("expected PERS-1 issue from personal project")
	}
	if !foundWork {
		t.Error("expected WORK-1 issue from work project")
	}
}

// --- IT-007: Pause and resume ---

func TestIT007_PauseAndResume(t *testing.T) {
	pg := StartPlayground(t)

	// Seed an issue in BUILDING state.
	issue := pg.SeedIssue("TEST-7", "Add dark mode", "building")
	issue.WorkspaceName = "test-7"
	issue.BranchName = "autoralph/test-7"
	pg.DB.UpdateIssue(issue)

	// Step 1: Call POST /api/issues/:id/pause.
	pauseURL := fmt.Sprintf("%s/api/issues/%s/pause", pg.BaseURL(), issue.ID)
	resp, err := http.Post(pauseURL, "application/json", nil)
	if err != nil {
		t.Fatalf("calling pause API: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from pause, got %d", resp.StatusCode)
	}

	// Verify state is PAUSED.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "paused" {
		t.Errorf("expected paused, got %s", issue.State)
	}

	// Verify pause activity logged.
	activity, _ := pg.DB.ListActivity(issue.ID, 10, 0)
	pauseLogged := false
	for _, a := range activity {
		if a.FromState == "building" && a.ToState == "paused" {
			pauseLogged = true
			break
		}
	}
	if !pauseLogged {
		t.Error("expected pause activity to be logged")
	}

	// Step 2: Call POST /api/issues/:id/resume.
	resumeURL := fmt.Sprintf("%s/api/issues/%s/resume", pg.BaseURL(), issue.ID)
	resp, err = http.Post(resumeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("calling resume API: %v", err)
	}
	defer resp.Body.Close()

	var resumeResult map[string]string
	json.NewDecoder(resp.Body).Decode(&resumeResult)

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from resume, got %d", resp.StatusCode)
	}

	// Verify state went back to BUILDING (the previous state before pause).
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "building" {
		t.Errorf("expected building after resume, got %s", issue.State)
	}
}

// --- IT-008: Restart recovery ---

func TestIT008_RestartRecovery(t *testing.T) {
	pg := StartPlayground(t)

	// Seed an issue in BUILDING state (simulating interrupted build).
	issue := pg.SeedIssue("TEST-8", "Add search feature", "building")
	issue.WorkspaceName = "test-8"
	issue.BranchName = "autoralph/test-8"
	pg.DB.UpdateIssue(issue)

	// Verify issue is in BUILDING state.
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "building" {
		t.Fatalf("expected building, got %s", issue.State)
	}

	// Simulate restart: create a new dispatcher and call RecoverBuilding.
	runner := newMockLoopRunner()
	dispatcher := worker.New(worker.Config{
		DB:         pg.DB,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   pg.DB,
		Logger:     nil,
	})

	count, err := dispatcher.RecoverBuilding(context.Background())
	if err != nil {
		t.Fatalf("RecoverBuilding failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 recovered issue, got %d", count)
	}

	// Verify the build worker started.
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("recovered build worker did not start")
	}

	dispatcher.Wait()

	// Verify issue completed the build (in_review).
	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "in_review" {
		t.Errorf("expected in_review after recovered build, got %s", issue.State)
	}
}

// --- IT-012: Concurrent builds respect worker limit ---

func TestIT012_ConcurrentBuildWorkerLimit(t *testing.T) {
	pg := StartPlayground(t)

	// Seed 2 issues in BUILDING state.
	issue1 := pg.SeedIssue("TEST-12A", "Feature A", "building")
	issue1.WorkspaceName = "test-12a"
	issue1.BranchName = "autoralph/test-12a"
	pg.DB.UpdateIssue(issue1)

	issue2 := pg.SeedIssue("TEST-12B", "Feature B", "building")
	issue2.WorkspaceName = "test-12b"
	issue2.BranchName = "autoralph/test-12b"
	pg.DB.UpdateIssue(issue2)

	// Create dispatcher with max 1 worker and a slow runner.
	runner := &blockingLoopRunner{
		ready:   make(chan struct{}, 2),
		release: make(chan struct{}),
	}

	dispatcher := worker.New(worker.Config{
		DB:         pg.DB,
		MaxWorkers: 1,
		LoopRunner: runner,
		Projects:   pg.DB,
		Logger:     nil,
	})

	// Dispatch first issue — should succeed.
	issue1, _ = pg.DB.GetIssue(issue1.ID)
	err := dispatcher.Dispatch(context.Background(), issue1)
	if err != nil {
		t.Fatalf("first dispatch failed: %v", err)
	}

	// Wait for first worker to start.
	select {
	case <-runner.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker didn't start")
	}

	// Dispatch second issue — should fail (no slot available).
	issue2, _ = pg.DB.GetIssue(issue2.ID)
	err = dispatcher.Dispatch(context.Background(), issue2)
	if err == nil {
		t.Fatal("expected second dispatch to fail (worker limit reached)")
	}
	if !strings.Contains(err.Error(), "no worker slot available") {
		t.Errorf("expected worker slot error, got: %v", err)
	}

	// Verify only 1 active worker.
	if dispatcher.ActiveCount() != 1 {
		t.Errorf("expected 1 active worker, got %d", dispatcher.ActiveCount())
	}

	// Release first worker.
	close(runner.release)
	dispatcher.Wait()

	// Now second dispatch should succeed.
	err = dispatcher.Dispatch(context.Background(), issue2)
	if err != nil {
		t.Fatalf("second dispatch after first completed should succeed: %v", err)
	}

	// Create a new release channel for the second worker.
	runner.release = make(chan struct{})
	close(runner.release)
	dispatcher.Wait()

	// Both issues should have completed.
	issue1, _ = pg.DB.GetIssue(issue1.ID)
	issue2, _ = pg.DB.GetIssue(issue2.ID)
	if issue1.State != "in_review" {
		t.Errorf("issue1 expected in_review, got %s", issue1.State)
	}
	if issue2.State != "in_review" {
		t.Errorf("issue2 expected in_review, got %s", issue2.State)
	}
}

// blockingLoopRunner blocks until release channel is closed.
type blockingLoopRunner struct {
	ready   chan struct{}
	release chan struct{}
}

func (r *blockingLoopRunner) Run(_ context.Context, _ worker.LoopConfig) error {
	r.ready <- struct{}{}
	<-r.release
	return nil
}

// --- IT-009: Dashboard UI tracking via API ---
// Tests the API endpoints that back the dashboard (since Playwright requires
// a full web build which may not be available in all test environments).

func TestIT009_DashboardAPITracking(t *testing.T) {
	pg := StartPlayground(t)

	projects, _ := pg.DB.ListProjects()
	proj := projects[0]

	// Seed 3 issues in different states.
	pg.SeedIssue("TEST-9A", "Issue Alpha", "queued")
	pg.SeedIssue("TEST-9B", "Issue Beta", "building")
	pg.SeedIssue("TEST-9C", "Issue Gamma", "in_review")

	// Verify project summary via API.
	resp, err := http.Get(pg.BaseURL() + "/api/projects")
	if err != nil {
		t.Fatalf("fetching projects: %v", err)
	}
	defer resp.Body.Close()

	var projectsResp []map[string]any
	json.NewDecoder(resp.Body).Decode(&projectsResp)

	if len(projectsResp) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projectsResp))
	}

	activeCount := int(projectsResp[0]["active_issue_count"].(float64))
	if activeCount != 3 {
		t.Errorf("expected 3 active issues, got %d", activeCount)
	}

	stateBreakdown, ok := projectsResp[0]["state_breakdown"].(map[string]any)
	if !ok {
		t.Fatal("expected state_breakdown in response")
	}
	if stateBreakdown["queued"] != float64(1) {
		t.Errorf("expected 1 queued, got %v", stateBreakdown["queued"])
	}
	if stateBreakdown["building"] != float64(1) {
		t.Errorf("expected 1 building, got %v", stateBreakdown["building"])
	}
	if stateBreakdown["in_review"] != float64(1) {
		t.Errorf("expected 1 in_review, got %v", stateBreakdown["in_review"])
	}

	// Verify issues list via API.
	resp2, err2 := http.Get(pg.BaseURL() + "/api/issues?project_id=" + proj.ID)
	if err2 != nil {
		t.Fatalf("fetching issues: %v", err2)
	}
	defer resp2.Body.Close()

	var issuesResp []map[string]any
	json.NewDecoder(resp2.Body).Decode(&issuesResp)

	if len(issuesResp) != 3 {
		t.Errorf("expected 3 issues, got %d", len(issuesResp))
	}

	// Verify state filter works.
	resp3, err3 := http.Get(pg.BaseURL() + "/api/issues?state=building")
	if err3 != nil {
		t.Fatalf("fetching building issues: %v", err3)
	}
	defer resp3.Body.Close()

	var buildingResp []map[string]any
	json.NewDecoder(resp3.Body).Decode(&buildingResp)

	if len(buildingResp) != 1 {
		t.Errorf("expected 1 building issue, got %d", len(buildingResp))
	}

	// Verify activity feed.
	resp4, err4 := http.Get(pg.BaseURL() + "/api/activity?limit=20")
	if err4 != nil {
		t.Fatalf("fetching activity: %v", err4)
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != 200 {
		t.Errorf("expected 200 from activity, got %d", resp4.StatusCode)
	}

	// Verify status endpoint.
	resp5, err5 := http.Get(pg.BaseURL() + "/api/status")
	if err5 != nil {
		t.Fatalf("fetching status: %v", err5)
	}
	defer resp5.Body.Close()

	var statusResp map[string]any
	json.NewDecoder(resp5.Body).Decode(&statusResp)

	if statusResp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", statusResp["status"])
	}
	if _, ok := statusResp["uptime"]; !ok {
		t.Error("expected uptime in status response")
	}
}

// --- IT-010: Issue detail API ---

func TestIT010_IssueDetailAPI(t *testing.T) {
	pg := StartPlayground(t)

	// Seed an issue and add activity.
	issue := pg.SeedIssue("TEST-10", "Add caching", "building")
	issue.WorkspaceName = "test-10"
	issue.BranchName = "autoralph/test-10"
	pg.DB.UpdateIssue(issue)

	// Add some activity entries.
	pg.DB.LogActivity(issue.ID, "state_change", "queued", "refining", "Transitioned to refining")
	pg.DB.LogActivity(issue.ID, "state_change", "refining", "approved", "Plan approved")
	pg.DB.LogActivity(issue.ID, "state_change", "approved", "building", "Build started")
	pg.DB.LogActivity(issue.ID, "build_event", "", "", "Iteration 1/20 started")

	// Fetch issue detail via API.
	resp, err := http.Get(pg.BaseURL() + "/api/issues/" + issue.ID)
	if err != nil {
		t.Fatalf("fetching issue: %v", err)
	}
	defer resp.Body.Close()

	var detail map[string]any
	json.NewDecoder(resp.Body).Decode(&detail)

	if detail["identifier"] != "TEST-10" {
		t.Errorf("expected identifier TEST-10, got %v", detail["identifier"])
	}
	if detail["state"] != "building" {
		t.Errorf("expected state building, got %v", detail["state"])
	}
	if detail["project_name"] != "test-project" {
		t.Errorf("expected project_name test-project, got %v", detail["project_name"])
	}

	// Verify activity timeline.
	activity, ok := detail["activity"].([]any)
	if !ok {
		t.Fatal("expected activity array in response")
	}
	if len(activity) < 4 {
		t.Errorf("expected at least 4 activity entries, got %d", len(activity))
	}
}

// --- IT-011: Action buttons via API ---

func TestIT011_ActionButtonsAPI(t *testing.T) {
	pg := StartPlayground(t)

	// Test Pause button.
	issue := pg.SeedIssue("TEST-11A", "Refactor auth", "building")
	pauseResp, _ := http.Post(pg.BaseURL()+"/api/issues/"+issue.ID+"/pause", "", nil)
	if pauseResp.StatusCode != 200 {
		t.Fatalf("pause returned %d", pauseResp.StatusCode)
	}
	pauseResp.Body.Close()

	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "paused" {
		t.Errorf("expected paused, got %s", issue.State)
	}

	// Test Resume button.
	resumeResp, _ := http.Post(pg.BaseURL()+"/api/issues/"+issue.ID+"/resume", "", nil)
	if resumeResp.StatusCode != 200 {
		t.Fatalf("resume returned %d", resumeResp.StatusCode)
	}
	resumeResp.Body.Close()

	issue, _ = pg.DB.GetIssue(issue.ID)
	if issue.State != "building" {
		t.Errorf("expected building after resume, got %s", issue.State)
	}

	// Test Retry button on failed issue.
	failedIssue := pg.SeedIssue("TEST-11B", "Fix crash", "building")
	failedIssue.State = "failed"
	failedIssue.ErrorMessage = "build error: tests failed"
	pg.DB.UpdateIssue(failedIssue)
	pg.DB.LogActivity(failedIssue.ID, "state_change", "building", "failed", "Build failed")

	retryResp, _ := http.Post(pg.BaseURL()+"/api/issues/"+failedIssue.ID+"/retry", "", nil)
	if retryResp.StatusCode != 200 {
		body, _ := io.ReadAll(retryResp.Body)
		t.Fatalf("retry returned %d: %s", retryResp.StatusCode, body)
	}
	retryResp.Body.Close()

	failedIssue, _ = pg.DB.GetIssue(failedIssue.ID)
	if failedIssue.State == "failed" {
		t.Error("expected state to change from failed")
	}
	if failedIssue.ErrorMessage != "" {
		t.Error("expected error_message to be cleared after retry")
	}

	// Test invalid operations.
	completedIssue := pg.SeedIssue("TEST-11C", "Done issue", "completed")
	pauseCompletedResp, _ := http.Post(pg.BaseURL()+"/api/issues/"+completedIssue.ID+"/pause", "", nil)
	if pauseCompletedResp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for pausing completed issue, got %d", pauseCompletedResp.StatusCode)
	}
	pauseCompletedResp.Body.Close()
}

// --- IT-013: Installer script validation ---

func TestIT013_InstallerScripts(t *testing.T) {
	// Find the project root (where install scripts live).
	root := findProjectRoot(t)

	scripts := []string{
		filepath.Join(root, "install-ralph.sh"),
		filepath.Join(root, "install-autoralph.sh"),
	}

	// Step 1: Verify scripts exist and are readable.
	for _, script := range scripts {
		info, err := os.Stat(script)
		if err != nil {
			t.Errorf("script %s not found: %v", filepath.Base(script), err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("script %s is empty", filepath.Base(script))
		}
	}

	// Step 2: Verify scripts start with proper shebang.
	for _, script := range scripts {
		data, err := os.ReadFile(script)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(string(data), "#!/") {
			t.Errorf("script %s missing shebang line", filepath.Base(script))
		}
	}

	// Step 3: Run shellcheck if available.
	if _, err := exec.LookPath("shellcheck"); err == nil {
		for _, script := range scripts {
			cmd := exec.Command("shellcheck", "-S", "warning", script)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("shellcheck failed for %s:\n%s", filepath.Base(script), output)
			}
		}
	} else {
		t.Log("shellcheck not found, skipping lint check")
	}

	// Step 4: Verify binaries exist and are executable (if available).
	for _, bin := range []string{"ralph", "autoralph"} {
		binPath, err := exec.LookPath(bin)
		if err != nil {
			t.Logf("%s binary not in PATH, skipping binary check", bin)
			continue
		}
		// Verify the binary is executable by running a no-op or help command.
		// Not all binaries support --version, so we just verify they can start.
		info, err := os.Stat(binPath)
		if err != nil {
			t.Errorf("%s stat failed: %v", bin, err)
			continue
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("%s is not executable", bin)
		}
	}

	// Step 5: Verify scripts contain idempotency logic (re-run detection).
	for _, script := range scripts {
		data, err := os.ReadFile(script)
		if err != nil {
			continue
		}
		content := string(data)
		// Check for OS detection.
		if !strings.Contains(content, "uname") {
			t.Errorf("script %s missing OS detection (uname)", filepath.Base(script))
		}
		// Check for checksum verification.
		if !strings.Contains(content, "sha256") && !strings.Contains(content, "shasum") {
			t.Errorf("script %s missing checksum verification", filepath.Base(script))
		}
	}
}

// --- Helper functions ---

func startHTTPServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from test/e2e to find go.mod.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}
