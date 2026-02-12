package ghpoller

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
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
		Name:        "test-project",
		LocalPath:   t.TempDir(),
		GithubOwner: "owner",
		GithubRepo:  "repo",
	})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	return p
}

func testIssue(t *testing.T, d *db.DB, proj db.Project, state string, prNumber int) db.Issue {
	t.Helper()
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-" + state,
		Identifier:    "TEST-1",
		Title:         "Test issue",
		State:         state,
		PRNumber:      prNumber,
		PRURL:         "https://github.com/owner/repo/pull/1",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// mockGitHub implements ReviewFetcher and MergeChecker for testing.
type mockGitHub struct {
	reviews     []github.Review
	merged      bool
	fetchErr    error
	mergeErr    error
	fetchCalls  int
	mergeCalls  int
	lastOwner   string
	lastRepo    string
	lastPR      int
}

func (m *mockGitHub) FetchPRReviews(_ context.Context, owner, repo string, prNumber int) ([]github.Review, error) {
	m.fetchCalls++
	m.lastOwner = owner
	m.lastRepo = repo
	m.lastPR = prNumber
	return m.reviews, m.fetchErr
}

func (m *mockGitHub) IsPRMerged(_ context.Context, owner, repo string, prNumber int) (bool, error) {
	m.mergeCalls++
	m.lastOwner = owner
	m.lastRepo = repo
	m.lastPR = prNumber
	return m.merged, m.mergeErr
}

func TestPollProject_ChangesRequestedReview_TransitionsToAddressingFeedback(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix the tests", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("expected state %q, got %q", orchestrator.StateAddressingFeedback, updated.State)
	}
	if updated.LastReviewID != "100" {
		t.Errorf("expected last_review_id %q, got %q", "100", updated.LastReviewID)
	}
}

func TestPollProject_MergedPR_TransitionsToCompleted(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		merged: true,
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestPollProject_CommentedReview_TransitionsToAddressingFeedback(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", Body: "", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("expected state %q, got %q", orchestrator.StateAddressingFeedback, updated.State)
	}
	if updated.LastReviewID != "100" {
		t.Errorf("expected last_review_id %q, got %q", "100", updated.LastReviewID)
	}
}

func TestPollProject_ApprovedReview_NoTransition(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 200, State: "APPROVED", Body: "LGTM", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state unchanged %q, got %q", orchestrator.StateInReview, updated.State)
	}
	// LastReviewID should still be updated to track we've seen this review
	if updated.LastReviewID != "200" {
		t.Errorf("expected last_review_id %q, got %q", "200", updated.LastReviewID)
	}
}

func TestPollProject_SkipsAlreadySeenReviews(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Create an issue that already has LastReviewID set to 100
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-seen",
		Identifier:    "TEST-2",
		Title:         "Already seen",
		State:         string(orchestrator.StateInReview),
		PRNumber:      42,
		LastReviewID:  "100",
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Old review", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	// State should NOT have changed because the review was already seen
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state %q, got %q", orchestrator.StateInReview, updated.State)
	}
}

func TestPollProject_SkipsIssuesWithoutPR(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Issue in IN_REVIEW but without a PR number (shouldn't happen but be defensive)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 0)

	mock := &mockGitHub{}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	// Should not have called GitHub
	if mock.fetchCalls != 0 {
		t.Errorf("expected 0 fetch calls, got %d", mock.fetchCalls)
	}
	if mock.mergeCalls != 0 {
		t.Errorf("expected 0 merge calls, got %d", mock.mergeCalls)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state unchanged, got %q", updated.State)
	}
}

func TestPollProject_AddressingFeedback_MergedTransitionsToCompleted(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateAddressingFeedback), 42)

	mock := &mockGitHub{
		merged: true,
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestPollProject_LogsActivity(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 300, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "changes_requested" {
		t.Errorf("expected event_type %q, got %q", "changes_requested", entries[0].EventType)
	}
	if entries[0].FromState != string(orchestrator.StateInReview) {
		t.Errorf("expected from_state %q, got %q", orchestrator.StateInReview, entries[0].FromState)
	}
	if entries[0].ToState != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("expected to_state %q, got %q", orchestrator.StateAddressingFeedback, entries[0].ToState)
	}
}

func TestPollProject_MergedLogsActivity(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		merged: true,
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "pr_merged" {
		t.Errorf("expected event_type %q, got %q", "pr_merged", entries[0].EventType)
	}
}

func TestPollProject_GitHubFetchError_ContinuesGracefully(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		mergeErr: fmt.Errorf("network error"),
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	// Issue should remain unchanged
	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state unchanged, got %q", updated.State)
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	d := testDB(t)

	p := New(d, nil, 100*time.Millisecond, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(250 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2 seconds after context cancellation")
	}
}

func TestPollProject_MultipleNewReviews_UsesLatest(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 10, State: "COMMENTED", Body: "Looks good overall", User: "reviewer1"},
			{ID: 20, State: "CHANGES_REQUESTED", Body: "Fix line 5", User: "reviewer2"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	// Should have transitioned due to CHANGES_REQUESTED
	if updated.State != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("expected state %q, got %q", orchestrator.StateAddressingFeedback, updated.State)
	}
	// LastReviewID should be the highest review ID seen
	if updated.LastReviewID != "20" {
		t.Errorf("expected last_review_id %q, got %q", "20", updated.LastReviewID)
	}
}

func TestPollProject_MergeCheckedFirst(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	// Both merged AND has changes_requested review — merge should win
	mock := &mockGitHub{
		merged: true,
		reviews: []github.Review{
			{ID: 500, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	// Merge should take precedence
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestPoll_MultipleProjects(t *testing.T) {
	d := testDB(t)
	proj1 := testProject(t, d)
	proj2, _ := d.CreateProject(db.Project{
		Name:        "test-project-2",
		LocalPath:   t.TempDir(),
		GithubOwner: "owner2",
		GithubRepo:  "repo2",
	})

	issue1 := testIssue(t, d, proj1, string(orchestrator.StateInReview), 1)
	// Create issue for proj2 separately
	issue2, _ := d.CreateIssue(db.Issue{
		ProjectID:     proj2.ID,
		LinearIssueID: "lin-p2",
		Identifier:    "P2-1",
		Title:         "Proj2 issue",
		State:         string(orchestrator.StateInReview),
		PRNumber:      2,
	})

	mock1 := &mockGitHub{merged: true}
	mock2 := &mockGitHub{
		reviews: []github.Review{
			{ID: 10, State: "CHANGES_REQUESTED", Body: "Fix", User: "rev"},
		},
	}

	p := New(d, []ProjectInfo{
		{ProjectID: proj1.ID, GithubOwner: "owner1", GithubRepo: "repo1", GitHub: mock1},
		{ProjectID: proj2.ID, GithubOwner: "owner2", GithubRepo: "repo2", GitHub: mock2},
	}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	u1, _ := d.GetIssue(issue1.ID)
	if u1.State != string(orchestrator.StateCompleted) {
		t.Errorf("proj1 issue expected %q, got %q", orchestrator.StateCompleted, u1.State)
	}

	u2, _ := d.GetIssue(issue2.ID)
	if u2.State != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("proj2 issue expected %q, got %q", orchestrator.StateAddressingFeedback, u2.State)
	}
}

func TestPollProject_MergedPR_CallsCompleteFunc(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{merged: true}

	var completeCalled bool
	var completedIssueID string
	completeFn := func(iss db.Issue, database *db.DB) error {
		completeCalled = true
		completedIssueID = iss.ID
		// Simulate what the real complete action does
		iss.State = string(orchestrator.StateCompleted)
		return database.UpdateIssue(iss)
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), completeFn)

	ctx := context.Background()
	p.poll(ctx)

	if !completeCalled {
		t.Fatal("expected CompleteFunc to be called")
	}
	if completedIssueID != issue.ID {
		t.Errorf("expected issue ID %q, got %q", issue.ID, completedIssueID)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestPollProject_BotReview_DoesNotTransition(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 1)

	mock := &mockGitHub{
		reviews: []github.Review{
			{ID: 100, State: "COMMENTED", User: "my-bot[bot]"},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state to remain %q, got %q", orchestrator.StateInReview, updated.State)
	}
	// LastReviewID should still advance past the bot review
	if updated.LastReviewID != "100" {
		t.Errorf("expected last_review_id to be %q, got %q", "100", updated.LastReviewID)
	}
}

func TestHasFeedback_IgnoresBotReviews(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "COMMENTED", User: "autoralph[bot]"},
		{ID: 2, State: "APPROVED", User: "human-reviewer"},
	}
	if hasFeedback(reviews) {
		t.Error("expected hasFeedback to return false when only bot has COMMENTED")
	}
}

func TestHasFeedback_HumanCommentedTriggersFeedback(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "COMMENTED", User: "autoralph[bot]"},
		{ID: 2, State: "COMMENTED", User: "human-reviewer"},
	}
	if !hasFeedback(reviews) {
		t.Error("expected hasFeedback to return true when human has COMMENTED")
	}
}
