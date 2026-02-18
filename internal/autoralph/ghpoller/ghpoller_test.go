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

// mockGitHub implements GitHubClient for testing.
type mockGitHub struct {
	reviews    []github.Review
	merged     bool
	fetchErr   error
	mergeErr   error
	fetchCalls int
	mergeCalls int
	lastOwner  string
	lastRepo   string
	lastPR     int

	pr         github.PR
	prErr      error
	prCalls    int
	checkRuns  []github.CheckRun
	checkErr   error
	checkCalls int
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

func (m *mockGitHub) FetchPR(_ context.Context, owner, repo string, prNumber int) (github.PR, error) {
	m.prCalls++
	return m.pr, m.prErr
}

func (m *mockGitHub) FetchCheckRuns(_ context.Context, owner, repo, ref string) ([]github.CheckRun, error) {
	m.checkCalls++
	return m.checkRuns, m.checkErr
}

func TestPollProject_ChangesRequestedReview_TransitionsToAddressingFeedback(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr:     github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
		pr: github.PR{HeadSHA: "abc123"},
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
	if hasFeedback(reviews, 0) {
		t.Error("expected hasFeedback to return false when only bot has COMMENTED")
	}
}

func TestHasFeedback_HumanCommentedTriggersFeedback(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "COMMENTED", User: "autoralph[bot]"},
		{ID: 2, State: "COMMENTED", User: "human-reviewer"},
	}
	if !hasFeedback(reviews, 0) {
		t.Error("expected hasFeedback to return true when human has COMMENTED")
	}
}

func TestHasFeedback_TrustedUserID_OnlyTrustedUserTriggers(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "CHANGES_REQUESTED", User: "untrusted", UserID: 99999},
		{ID: 2, State: "COMMENTED", User: "trusted", UserID: 12345},
	}
	if !hasFeedback(reviews, 12345) {
		t.Error("expected hasFeedback to return true when trusted user has feedback")
	}
}

func TestHasFeedback_TrustedUserID_UntrustedUserIgnored(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "CHANGES_REQUESTED", User: "untrusted", UserID: 99999},
	}
	if hasFeedback(reviews, 12345) {
		t.Error("expected hasFeedback to return false when only untrusted user has feedback")
	}
}

func TestHasFeedback_TrustedUserID_Zero_AllNonBotTrusted(t *testing.T) {
	reviews := []github.Review{
		{ID: 1, State: "CHANGES_REQUESTED", User: "anyone", UserID: 99999},
	}
	if !hasFeedback(reviews, 0) {
		t.Error("expected hasFeedback to return true when trustedUserID is 0 (backward compat)")
	}
}

func TestPollProject_TrustedUserFeedback_TransitionsToAddressingFeedback(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "abc123"},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "trusted-user", UserID: 12345},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:     proj.ID,
		GithubOwner:   "owner",
		GithubRepo:    "repo",
		GitHub:        mock,
		TrustedUserID: 12345,
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
}

func TestPollProject_UntrustedFeedback_SkippedAndLogged(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "abc123"},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "untrusted-user", UserID: 99999},
		},
	}

	p := New(d, []ProjectInfo{{
		ProjectID:     proj.ID,
		GithubOwner:   "owner",
		GithubRepo:    "repo",
		GitHub:        mock,
		TrustedUserID: 12345,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	// State should NOT transition
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state %q, got %q", orchestrator.StateInReview, updated.State)
	}
	// LastReviewID should still advance past the untrusted review
	if updated.LastReviewID != "100" {
		t.Errorf("expected last_review_id %q, got %q", "100", updated.LastReviewID)
	}

	// Activity log should have the skip entry
	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "untrusted_feedback_skipped" {
		t.Errorf("expected event_type %q, got %q", "untrusted_feedback_skipped", entries[0].EventType)
	}
}

func TestPollProject_TrustedUserID_Zero_BackwardCompatible(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "abc123"},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix", User: "anyone", UserID: 55555},
		},
	}

	// TrustedUserID is 0 (default/unset) — all non-bot reviews should be trusted
	p := New(d, []ProjectInfo{{
		ProjectID:     proj.ID,
		GithubOwner:   "owner",
		GithubRepo:    "repo",
		GitHub:        mock,
		TrustedUserID: 0,
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
}

func TestPollProject_ChecksFailedOnNewSHA_TransitionsToFixingChecks(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "deadbeef1234567"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "completed", Conclusion: "failure"},
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
	if updated.State != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFixingChecks, updated.State)
	}
	if updated.LastCheckSHA != "deadbeef1234567" {
		t.Errorf("expected LastCheckSHA %q, got %q", "deadbeef1234567", updated.LastCheckSHA)
	}

	// Verify activity was logged
	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "checks_failed" {
		t.Errorf("expected event_type %q, got %q", "checks_failed", entries[0].EventType)
	}
	if entries[0].FromState != string(orchestrator.StateInReview) {
		t.Errorf("expected from_state %q, got %q", orchestrator.StateInReview, entries[0].FromState)
	}
	if entries[0].ToState != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected to_state %q, got %q", orchestrator.StateFixingChecks, entries[0].ToState)
	}
}

func TestPollProject_ChecksFailedSameSHA_NoRetrigger(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Create issue already in fixing_checks with LastCheckSHA matching current head
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-fixing",
		Identifier:    "TEST-FIX",
		Title:         "Fixing issue",
		State:         string(orchestrator.StateFixingChecks),
		PRNumber:      42,
		LastCheckSHA:  "same-sha-123",
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "same-sha-123"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
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
	// State should remain fixing_checks — no duplicate transition
	if updated.State != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFixingChecks, updated.State)
	}

	// No activity should be logged for same-SHA check failure
	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 activity entries, got %d", len(entries))
	}
}

func TestPollProject_ChecksPending_NoTransition(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "pending-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "in_progress", Conclusion: ""},
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
	// State should remain in_review — checks still pending
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state %q, got %q", orchestrator.StateInReview, updated.State)
	}
	// Reviews should NOT have been fetched since checks are pending
	if mock.fetchCalls != 0 {
		t.Errorf("expected 0 review fetch calls, got %d", mock.fetchCalls)
	}
}

func TestPollProject_AllChecksPass_ReviewsEvaluated(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "pass-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "completed", Conclusion: "success"},
		},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
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
	// Checks passed, so reviews should have been evaluated → addressing_feedback
	if updated.State != string(orchestrator.StateAddressingFeedback) {
		t.Errorf("expected state %q, got %q", orchestrator.StateAddressingFeedback, updated.State)
	}
	if updated.LastCheckSHA != "pass-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "pass-sha", updated.LastCheckSHA)
	}
}

func TestPollProject_ChecksFailedTakesPriorityOverFeedback(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateInReview), 42)

	// Both check failures AND review feedback — check failures should win
	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "fail-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
		},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
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
	// Check failure should take priority over review feedback
	if updated.State != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFixingChecks, updated.State)
	}
	// Reviews should NOT have been fetched at all
	if mock.fetchCalls != 0 {
		t.Errorf("expected 0 review fetch calls, got %d", mock.fetchCalls)
	}
}

func TestPollProject_ExternalSHAChange_ResetsCheckFixAttempts(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Create issue in in_review with 2 prior fix attempts and old SHA
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:        proj.ID,
		LinearIssueID:    "lin-reset",
		Identifier:       "TEST-RST",
		Title:            "Reset attempts",
		State:            string(orchestrator.StateInReview),
		PRNumber:         42,
		LastCheckSHA:     "old-sha",
		CheckFixAttempts: 2,
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "new-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
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
	// CheckFixAttempts should be reset to 0
	if updated.CheckFixAttempts != 0 {
		t.Errorf("expected CheckFixAttempts 0, got %d", updated.CheckFixAttempts)
	}
	// SHA should be updated
	if updated.LastCheckSHA != "new-sha" {
		t.Errorf("expected LastCheckSHA %q, got %q", "new-sha", updated.LastCheckSHA)
	}
	// State remains in_review (checks passed)
	if updated.State != string(orchestrator.StateInReview) {
		t.Errorf("expected state %q, got %q", orchestrator.StateInReview, updated.State)
	}
}

func TestPollProject_FixingChecks_NoReviewEvaluation(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Issue in fixing_checks with all checks passing on a new SHA
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     proj.ID,
		LinearIssueID: "lin-fc",
		Identifier:    "TEST-FC",
		Title:         "Fixing checks",
		State:         string(orchestrator.StateFixingChecks),
		PRNumber:      42,
		LastCheckSHA:  "old-sha",
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "new-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
		},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
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
	// State should stay fixing_checks — reviews not evaluated for this state
	if updated.State != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFixingChecks, updated.State)
	}
	// Reviews should NOT have been fetched
	if mock.fetchCalls != 0 {
		t.Errorf("expected 0 review fetch calls, got %d", mock.fetchCalls)
	}
}

func TestPollProject_AddressingFeedback_ChecksFailed_TransitionsToFixingChecks(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateAddressingFeedback), 42)

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "af-fail-sha"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "lint", Status: "completed", Conclusion: "failure"},
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
	if updated.State != string(orchestrator.StateFixingChecks) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFixingChecks, updated.State)
	}
}

func TestEvaluateCheckRuns_AllCompleted_OneFailed(t *testing.T) {
	runs := []github.CheckRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "failure"},
	}
	allCompleted, hasFailed := evaluateCheckRuns(runs)
	if !allCompleted {
		t.Error("expected allCompleted to be true")
	}
	if !hasFailed {
		t.Error("expected hasFailed to be true")
	}
}

func TestEvaluateCheckRuns_NotAllCompleted(t *testing.T) {
	runs := []github.CheckRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "in_progress", Conclusion: ""},
	}
	allCompleted, hasFailed := evaluateCheckRuns(runs)
	if allCompleted {
		t.Error("expected allCompleted to be false")
	}
	if hasFailed {
		t.Error("expected hasFailed to be false")
	}
}

func TestEvaluateCheckRuns_AllPassed(t *testing.T) {
	runs := []github.CheckRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "success"},
	}
	allCompleted, hasFailed := evaluateCheckRuns(runs)
	if !allCompleted {
		t.Error("expected allCompleted to be true")
	}
	if hasFailed {
		t.Error("expected hasFailed to be false")
	}
}

func TestEvaluateCheckRuns_Empty(t *testing.T) {
	allCompleted, hasFailed := evaluateCheckRuns(nil)
	if !allCompleted {
		t.Error("expected allCompleted to be true for empty slice")
	}
	if hasFailed {
		t.Error("expected hasFailed to be false for empty slice")
	}
}

func TestPollProject_FailedIssueWithMergedPR_TransitionsToCompleted(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateFailed), 42)

	mock := &mockGitHub{merged: true}

	var completeCalled bool
	completeFn := func(iss db.Issue, database *db.DB) error {
		completeCalled = true
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
		t.Fatal("expected CompleteFunc to be called for failed issue with merged PR")
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.State != string(orchestrator.StateCompleted) {
		t.Errorf("expected state %q, got %q", orchestrator.StateCompleted, updated.State)
	}
}

func TestPollProject_FailedIssueWithUnmergedPR_StaysInFailed(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateFailed), 42)

	mock := &mockGitHub{
		merged: false,
		pr:     github.PR{HeadSHA: "abc123"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
		},
		reviews: []github.Review{
			{ID: 100, State: "CHANGES_REQUESTED", Body: "Fix it", User: "reviewer"},
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
	// State should remain failed — unmerged PRs don't recover
	if updated.State != string(orchestrator.StateFailed) {
		t.Errorf("expected state %q, got %q", orchestrator.StateFailed, updated.State)
	}
	// Check runs and reviews should NOT have been fetched for failed issues
	if mock.prCalls != 0 {
		t.Errorf("expected 0 PR fetch calls for failed issue, got %d", mock.prCalls)
	}
	if mock.checkCalls != 0 {
		t.Errorf("expected 0 check run calls for failed issue, got %d", mock.checkCalls)
	}
	if mock.fetchCalls != 0 {
		t.Errorf("expected 0 review fetch calls for failed issue, got %d", mock.fetchCalls)
	}
}

func TestPollProject_FailedIssueWithoutPR_Skipped(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)
	issue := testIssue(t, d, proj, string(orchestrator.StateFailed), 0)

	mock := &mockGitHub{}

	p := New(d, []ProjectInfo{{
		ProjectID:   proj.ID,
		GithubOwner: "owner",
		GithubRepo:  "repo",
		GitHub:      mock,
	}}, 30*time.Second, slog.Default(), nil)

	ctx := context.Background()
	p.poll(ctx)

	// Should not have called GitHub at all
	if mock.mergeCalls != 0 {
		t.Errorf("expected 0 merge calls, got %d", mock.mergeCalls)
	}

	updated, _ := d.GetIssue(issue.ID)
	if updated.State != string(orchestrator.StateFailed) {
		t.Errorf("expected state unchanged, got %q", updated.State)
	}
}

func TestPollProject_FixingChecks_NoCheckFixAttemptsReset(t *testing.T) {
	d := testDB(t)
	proj := testProject(t, d)

	// Issue in fixing_checks with 2 attempts and a new SHA pushed by the fix action
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:        proj.ID,
		LinearIssueID:    "lin-fc-nrst",
		Identifier:       "TEST-NRST",
		Title:            "No reset in fixing_checks",
		State:            string(orchestrator.StateFixingChecks),
		PRNumber:         42,
		LastCheckSHA:     "old-sha",
		CheckFixAttempts: 2,
	})
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	mock := &mockGitHub{
		pr: github.PR{HeadSHA: "new-sha-from-fix"},
		checkRuns: []github.CheckRun{
			{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
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
	// CheckFixAttempts should NOT be reset because issue is in fixing_checks
	if updated.CheckFixAttempts != 2 {
		t.Errorf("expected CheckFixAttempts 2 (no reset in fixing_checks), got %d", updated.CheckFixAttempts)
	}
}
