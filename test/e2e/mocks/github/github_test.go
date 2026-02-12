package github

import (
	"context"
	"testing"

	gh "github.com/uesteibar/ralph/internal/autoralph/github"
)

func mustNew(t *testing.T, token string, opts ...gh.Option) *gh.Client {
	t.Helper()
	c, err := gh.New(token, opts...)
	if err != nil {
		t.Fatalf("gh.New: %v", err)
	}
	return c
}

func TestMock_CreatePR_Success(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	pr, err := client.CreatePullRequest(context.Background(), "owner", "repo", "feat", "main", "Title", "Body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.Number != 1 {
		t.Errorf("expected PR #1, got #%d", pr.Number)
	}
	if pr.Title != "Title" {
		t.Errorf("expected title 'Title', got %q", pr.Title)
	}
	if pr.State != "open" {
		t.Errorf("expected state 'open', got %q", pr.State)
	}
	if len(mock.CreatedPRs) != 1 {
		t.Fatalf("expected 1 created PR, got %d", len(mock.CreatedPRs))
	}
	if mock.CreatedPRs[0].Head != "feat" {
		t.Errorf("expected head 'feat', got %q", mock.CreatedPRs[0].Head)
	}
}

func TestMock_IsPRMerged_NotMerged(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 42, State: "open"})
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	merged, err := client.IsPRMerged(context.Background(), "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged {
		t.Error("expected PR not merged")
	}
}

func TestMock_IsPRMerged_Merged(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 42, State: "open"})
	mock.SimulateMerge("owner", "repo", 42)
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	merged, err := client.IsPRMerged(context.Background(), "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !merged {
		t.Error("expected PR to be merged")
	}
}

func TestMock_FetchPRReviews_Success(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 10, State: "open"})
	mock.AddReview("owner", "repo", 10, Review{
		ID: 100, State: "APPROVED", Body: "LGTM", User: "reviewer",
	})
	mock.SimulateChangesRequested("owner", "repo", 10, 101, "Needs work")
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	reviews, err := client.FetchPRReviews(context.Background(), "owner", "repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(reviews))
	}
	if reviews[0].State != "APPROVED" {
		t.Errorf("expected first review APPROVED, got %q", reviews[0].State)
	}
	if reviews[1].State != "CHANGES_REQUESTED" {
		t.Errorf("expected second review CHANGES_REQUESTED, got %q", reviews[1].State)
	}
}

func TestMock_FetchPRReviews_Empty(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 10, State: "open"})
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	reviews, err := client.FetchPRReviews(context.Background(), "owner", "repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviews) != 0 {
		t.Errorf("expected 0 reviews, got %d", len(reviews))
	}
}

func TestMock_PostPRComment_Success(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 5, State: "open"})
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	comment, err := client.PostPRComment(context.Background(), "owner", "repo", 5, "PR comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if comment.Body != "PR comment" {
		t.Errorf("expected body 'PR comment', got %q", comment.Body)
	}
	if len(mock.PostedComments) != 1 {
		t.Fatalf("expected 1 posted comment, got %d", len(mock.PostedComments))
	}
	if mock.PostedComments[0].Body != "PR comment" {
		t.Errorf("expected tracked body 'PR comment', got %q", mock.PostedComments[0].Body)
	}
}

func TestMock_FindOpenPR_Found(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 7, Head: "feat", Base: "main", State: "open"})
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	pr, err := client.FindOpenPR(context.Background(), "owner", "repo", "feat", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR to be found")
	}
	if pr.Number != 7 {
		t.Errorf("expected PR #7, got #%d", pr.Number)
	}
}

func TestMock_FindOpenPR_NotFound(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	pr, err := client.FindOpenPR(context.Background(), "owner", "repo", "feat", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Error("expected no PR found")
	}
}

func TestMock_FetchPRComments_Success(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 3, State: "open"})
	mock.AddComment("owner", "repo", 3, Comment{
		ID: 200, Body: "Fix this", Path: "main.go", User: "reviewer", InReplyTo: 0,
	})
	srv := mock.Server(t)

	client := mustNew(t, "test-token", gh.WithBaseURL(srv.URL+"/"))
	comments, err := client.FetchPRComments(context.Background(), "owner", "repo", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "Fix this" {
		t.Errorf("expected body 'Fix this', got %q", comments[0].Body)
	}
	if comments[0].Path != "main.go" {
		t.Errorf("expected path 'main.go', got %q", comments[0].Path)
	}
}

func TestMock_GetPR(t *testing.T) {
	mock := New()
	mock.AddPR("owner", "repo", PR{Number: 99, Title: "My PR", State: "open"})

	pr := mock.GetPR("owner", "repo", 99)
	if pr == nil {
		t.Fatal("expected PR to exist")
	}
	if pr.Title != "My PR" {
		t.Errorf("expected title 'My PR', got %q", pr.Title)
	}

	pr = mock.GetPR("owner", "repo", 100)
	if pr != nil {
		t.Error("expected nil for non-existent PR")
	}
}
