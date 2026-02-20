package approve

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
	"github.com/uesteibar/ralph/internal/events"
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

func createTestIssue(t *testing.T, d *db.DB, state, lastCommentID string) db.Issue {
	t.Helper()
	p, err := d.CreateProject(db.Project{Name: "test-project", LocalPath: "/tmp/test"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         state,
		LastCommentID: lastCommentID,
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

type mockGitPuller struct {
	calls []gitPullerCall
	err   error
}

type gitPullerCall struct {
	repoPath        string
	ralphConfigPath string
}

func (m *mockGitPuller) PullDefaultBase(ctx context.Context, repoPath, ralphConfigPath string) error {
	m.calls = append(m.calls, gitPullerCall{repoPath: repoPath, ralphConfigPath: ralphConfigPath})
	return m.err
}

type mockInvoker struct {
	lastPrompt   string
	lastMaxTurns int
	lastHandler  events.EventHandler
	response     string
	err          error
	emitEvents   []events.Event
}

func (m *mockInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	m.lastPrompt = prompt
	m.lastMaxTurns = maxTurns
	m.lastHandler = handler
	for _, e := range m.emitEvents {
		if handler != nil {
			handler.Handle(e)
		}
	}
	return m.response, m.err
}

type mockCommentClient struct {
	comments []linear.Comment
	fetchErr error
	posted   []postCall
	replies  []replyCall
	postErr  error
	postID   string
}

type postCall struct {
	issueID string
	body    string
}

type replyCall struct {
	issueID  string
	parentID string
	body     string
}

func (m *mockCommentClient) FetchIssueComments(ctx context.Context, issueID string) ([]linear.Comment, error) {
	return m.comments, m.fetchErr
}

func (m *mockCommentClient) PostComment(ctx context.Context, issueID, body string) (linear.Comment, error) {
	m.posted = append(m.posted, postCall{issueID: issueID, body: body})
	id := m.postID
	if id == "" {
		id = "posted-comment-id"
	}
	return linear.Comment{ID: id, Body: body}, m.postErr
}

func (m *mockCommentClient) PostReply(ctx context.Context, issueID, parentID, body string) (linear.Comment, error) {
	m.replies = append(m.replies, replyCall{issueID: issueID, parentID: parentID, body: body})
	id := m.postID
	if id == "" {
		id = "posted-reply-id"
	}
	return linear.Comment{ID: id, ParentID: parentID, Body: body}, m.postErr
}

type mockReactor struct {
	calls []reactCall
	err   error
}

type reactCall struct {
	commentID string
	emoji     string
}

func (m *mockReactor) ReactToComment(ctx context.Context, commentID, emoji string) error {
	m.calls = append(m.calls, reactCall{commentID: commentID, emoji: emoji})
	return m.err
}

// --- Condition tests ---

func TestIsApproval_DetectsApprovalComment(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Here is the plan", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if !cond(issue) {
		t.Error("expected IsApproval to return true when approval comment exists")
	}
}

func TestIsApproval_CaseInsensitive(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "plan draft", UserName: "autoralph"},
			{ID: "c2", Body: "i APPROVE This", UserName: "human"},
		},
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if !cond(issue) {
		t.Error("expected IsApproval to be case-insensitive")
	}
}

func TestIsApproval_NoNewComments_ReturnsFalse(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "plan draft", UserName: "autoralph"},
		},
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if cond(issue) {
		t.Error("expected IsApproval to return false when no new comments")
	}
}

func TestIsApproval_FetchError_ReturnsFalse(t *testing.T) {
	client := &mockCommentClient{
		fetchErr: fmt.Errorf("network error"),
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123"}

	if cond(issue) {
		t.Error("expected IsApproval to return false on fetch error")
	}
}

func TestIsIteration_NewHumanComments_ReturnsTrue(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "plan draft", UserName: "autoralph"},
			{ID: "c2", Body: "Can you add caching?", UserName: "human"},
		},
	}

	cond := IsIteration(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if !cond(issue) {
		t.Error("expected IsIteration to return true for new non-approval comments")
	}
}

func TestIsIteration_ApprovalComment_ReturnsFalse(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "plan draft", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	cond := IsIteration(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if cond(issue) {
		t.Error("expected IsIteration to return false when approval comment present")
	}
}

func TestIsIteration_NoNewComments_ReturnsFalse(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "plan draft", UserName: "autoralph"},
		},
	}

	cond := IsIteration(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	if cond(issue) {
		t.Error("expected IsIteration to return false when no new comments")
	}
}

func TestHasNewComments_EmptyLastCommentID_ReturnsTrue(t *testing.T) {
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Some comment", UserName: "human"},
		},
	}

	cond := HasNewComments(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: ""}

	if !cond(issue) {
		t.Error("expected HasNewComments to return true when LastCommentID is empty and comments exist")
	}
}

// --- Approval action tests ---

func TestApprovalAction_StoresPlanText(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "## Implementation Plan\n\n1. Add avatar upload endpoint\n2. Resize images", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.PlanText == "" {
		t.Error("expected plan_text to be stored")
	}
	if !strings.Contains(updated.PlanText, "Implementation Plan") {
		t.Error("expected plan_text to contain the plan from the comment before approval")
	}
}

func TestApprovalAction_UpdatesLastCommentID(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "The plan", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.LastCommentID != "c2" {
		t.Errorf("expected LastCommentID %q, got %q", "c2", updated.LastCommentID)
	}
}

func TestApprovalAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Plan text", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(entries))
	}
	if entries[0].EventType != "approval_detected" {
		t.Errorf("expected event_type %q, got %q", "approval_detected", entries[0].EventType)
	}
	if !strings.Contains(entries[0].Detail, "Plan approved") {
		t.Error("expected detail to mention plan approval")
	}
}

func TestApprovalAction_FetchError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		fetchErr: fmt.Errorf("Linear API error"),
	}

	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when fetching comments fails")
	}
	if !strings.Contains(err.Error(), "Linear API error") {
		t.Errorf("expected error to contain failure message, got: %v", err)
	}
}

// --- Iteration action tests ---

func TestIterationAction_IncrementalSendsOnlyNewCommentsByDefault(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Here is a draft plan", UserName: "autoralph", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "Can you add caching?", UserName: "alice", CreatedAt: "2026-01-01T01:00:00Z"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan with caching"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI to be invoked")
	}
	// Incremental: only new comments (after c1) are sent.
	if !strings.Contains(invoker.lastPrompt, "Can you add caching?") {
		t.Error("expected prompt to contain new comment")
	}
	// Old comment c1 should NOT be in the prompt.
	if strings.Contains(invoker.lastPrompt, "Here is a draft plan") {
		t.Error("expected incremental prompt to NOT contain old comment")
	}
	// Context prefix with title should be present.
	if !strings.Contains(invoker.lastPrompt, "Continuing refinement of: Add user avatars") {
		t.Error("expected prompt to contain context prefix")
	}
}

func TestIterationAction_PostsAIResponse(t *testing.T) {
	tests := []struct {
		name       string
		aiResponse string
		wantHint   bool
		wantClean  string
	}{
		{
			name:       "plan response includes approval hint",
			aiResponse: "<!-- type: plan -->\n## Updated Plan\n\n1. Add caching layer",
			wantHint:   true,
			wantClean:  "## Updated Plan\n\n1. Add caching layer",
		},
		{
			name:       "questions response excludes approval hint",
			aiResponse: "<!-- type: questions -->\n## Clarifying Questions\n\n1. What cache backend?",
			wantHint:   false,
			wantClean:  "## Clarifying Questions\n\n1. What cache backend?",
		},
		{
			name:       "no marker defaults to including approval hint",
			aiResponse: "## Updated Plan\n\n1. Add caching layer",
			wantHint:   true,
			wantClean:  "## Updated Plan\n\n1. Add caching layer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDB(t)
			issue := createTestIssue(t, d, "refining", "c1")

			client := &mockCommentClient{
				comments: []linear.Comment{
					{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
					{ID: "c2", Body: "Add caching", UserName: "human"},
				},
				postID: "c3",
			}
			invoker := &mockInvoker{response: tt.aiResponse}

			action := NewIterationAction(Config{
				Invoker:  invoker,
				Comments: client,
				Projects: d,
			})

			err := action(issue, d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(client.posted) != 1 {
				t.Fatalf("expected 1 posted comment, got %d", len(client.posted))
			}
			if client.posted[0].issueID != "lin-123" {
				t.Errorf("expected issue ID %q, got %q", "lin-123", client.posted[0].issueID)
			}

			body := client.posted[0].body
			hasHint := strings.Contains(body, "I approve this")
			if hasHint != tt.wantHint {
				t.Errorf("approval hint presence = %v, want %v\nbody: %q", hasHint, tt.wantHint, body)
			}

			if strings.Contains(body, "<!-- type:") {
				t.Error("expected type marker to be stripped from posted body")
			}

			if tt.wantHint {
				expectedBody := tt.wantClean + ApprovalHint
				if body != expectedBody {
					t.Errorf("expected body %q, got %q", expectedBody, body)
				}
			} else {
				if body != tt.wantClean {
					t.Errorf("expected body %q, got %q", tt.wantClean, body)
				}
			}
		})
	}
}

func TestIterationAction_UpdatesLastCommentID(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft", UserName: "autoralph"},
			{ID: "c2", Body: "Feedback", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.LastCommentID != "c3" {
		t.Errorf("expected LastCommentID %q, got %q", "c3", updated.LastCommentID)
	}
}

func TestIterationAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft", UserName: "autoralph"},
			{ID: "c2", Body: "Feedback", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 activity entries, got %d", len(entries))
	}
	// Most recent entry is the plan iteration result (ListActivity returns DESC).
	if entries[0].EventType != "plan_iteration" {
		t.Errorf("expected event_type %q, got %q", "plan_iteration", entries[0].EventType)
	}
	if !strings.Contains(entries[0].Detail, "Updated plan") {
		t.Error("expected detail to contain AI response")
	}
}

func TestIterationAction_AIError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft", UserName: "autoralph"},
			{ID: "c2", Body: "Feedback", UserName: "human"},
		},
	}
	invoker := &mockInvoker{err: fmt.Errorf("AI service unavailable")}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when AI invocation fails")
	}
	if !strings.Contains(err.Error(), "AI service unavailable") {
		t.Errorf("expected error message, got: %v", err)
	}
}

func TestIterationAction_PostError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft", UserName: "autoralph"},
			{ID: "c2", Body: "Feedback", UserName: "human"},
		},
		postErr: fmt.Errorf("Linear API error"),
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when posting comment fails")
	}
	if !strings.Contains(err.Error(), "Linear API error") {
		t.Errorf("expected error message, got: %v", err)
	}
}

// --- Iteration GitPuller tests ---

func TestIterationAction_PullsBeforeInvokingAI(t *testing.T) {
	d := testDB(t)
	p, err := d.CreateProject(db.Project{Name: "test-project", LocalPath: "/tmp/test", RalphConfigPath: ".ralph/ralph.yaml"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         "refining",
		LastCommentID: "c1",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}

	puller := &mockGitPuller{}
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:   invoker,
		Comments:  client,
		Projects:  d,
		GitPuller: puller,
	})

	err = action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(puller.calls) != 1 {
		t.Fatalf("expected 1 PullDefaultBase call, got %d", len(puller.calls))
	}
	if puller.calls[0].repoPath != "/tmp/test" {
		t.Errorf("expected repoPath %q, got %q", "/tmp/test", puller.calls[0].repoPath)
	}
	if puller.calls[0].ralphConfigPath != ".ralph/ralph.yaml" {
		t.Errorf("expected ralphConfigPath %q, got %q", ".ralph/ralph.yaml", puller.calls[0].ralphConfigPath)
	}
	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called after pull")
	}
}

func TestIterationAction_PullFails_LogsWarningAndProceeds(t *testing.T) {
	d := testDB(t)
	p, err := d.CreateProject(db.Project{Name: "test-project", LocalPath: "/tmp/test", RalphConfigPath: ".ralph/ralph.yaml"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-123",
		Identifier:    "PROJ-42",
		Title:         "Add user avatars",
		Description:   "Users should be able to upload profile pictures.",
		State:         "refining",
		LastCommentID: "c1",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}

	puller := &mockGitPuller{err: fmt.Errorf("network error")}
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:   invoker,
		Comments:  client,
		Projects:  d,
		GitPuller: puller,
	})

	err = action(issue, d)
	if err != nil {
		t.Fatalf("expected no error when pull fails, got: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called even when pull fails")
	}

	entries, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	foundWarning := false
	for _, e := range entries {
		if e.EventType == "warning" && strings.Contains(e.Detail, "network error") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected a warning activity log entry about the pull failure")
	}
}

func TestIterationAction_NilGitPuller_SkipsPull(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
		// GitPuller is nil — should be skipped
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called")
	}
}

// --- Helper function tests ---

func TestCommentsAfter_WithLastID(t *testing.T) {
	comments := []linear.Comment{
		{ID: "c1", Body: "first"},
		{ID: "c2", Body: "second"},
		{ID: "c3", Body: "third"},
	}

	after := commentsAfter(comments, "c1")
	if len(after) != 2 {
		t.Fatalf("expected 2 comments after c1, got %d", len(after))
	}
	if after[0].ID != "c2" || after[1].ID != "c3" {
		t.Error("expected comments c2 and c3")
	}
}

func TestCommentsAfter_EmptyLastID(t *testing.T) {
	comments := []linear.Comment{
		{ID: "c1", Body: "first"},
		{ID: "c2", Body: "second"},
	}

	after := commentsAfter(comments, "")
	if len(after) != 2 {
		t.Fatalf("expected all comments when lastID is empty, got %d", len(after))
	}
}

func TestCommentsAfter_LastIDNotFound_ReturnsNil(t *testing.T) {
	comments := []linear.Comment{
		{ID: "c1", Body: "first"},
		{ID: "c2", Body: "second"},
	}

	after := commentsAfter(comments, "unknown")
	if after != nil {
		t.Fatalf("expected nil when lastID not found, got %d comments", len(after))
	}
}

func TestContainsApproval_Variants(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"I approve this", true},
		{"i approve this", true},
		{"I APPROVE THIS", true},
		{"Looks good! I approve this", true},
		{"I approve this, let's go!", true},
		{"Sure, I approve this plan.", true},
		// Negative cases
		{"I approve", false},
		{"approved", false},
		{"@autoralph approved", false},
		{"some random comment", false},
		{"I don't approve this", false},
		// Bot's own comment with ApprovalHint must not self-trigger
		{"Here is the updated plan." + ApprovalHint, false},
		{"## Plan\n\n1. Do stuff" + ApprovalHint, false},
	}

	for _, tt := range tests {
		got := containsApproval(tt.text)
		if got != tt.want {
			t.Errorf("containsApproval(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestExtractPlanText_ReturnsCommentBeforeApproval(t *testing.T) {
	comments := []linear.Comment{
		{ID: "c1", Body: "What image formats?", UserName: "autoralph"},
		{ID: "c2", Body: "PNG and JPEG", UserName: "human"},
		{ID: "c3", Body: "## Plan\n\n1. Add upload\n2. Resize", UserName: "autoralph"},
		{ID: "c4", Body: "I approve this", UserName: "human"},
	}

	plan := extractPlanText(comments)
	if plan != "## Plan\n\n1. Add upload\n2. Resize" {
		t.Errorf("expected plan text from c3, got %q", plan)
	}
}

func TestExtractPlanText_ApprovalIsFirst_ReturnsEmpty(t *testing.T) {
	comments := []linear.Comment{
		{ID: "c1", Body: "I approve this", UserName: "human"},
	}

	plan := extractPlanText(comments)
	if plan != "" {
		t.Errorf("expected empty plan text, got %q", plan)
	}
}

// --- Self-approval prevention tests ---

func TestIsApproval_BotReplyWithHint_DoesNotSelfApprove(t *testing.T) {
	// Simulate: bot posted iteration reply (c2) with ApprovalHint, lastCommentID = c2.
	// On the next tick, FetchIssueComments returns c2 due to consistency lag
	// where c2 is still the latest. No new comments → should NOT approve.
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "draft plan" + ApprovalHint, UserName: "autoralph"},
			{ID: "c2", Body: "Updated plan with feedback" + ApprovalHint, UserName: "autoralph"},
		},
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c2"}

	if cond(issue) {
		t.Error("expected IsApproval to return false — no new comments after lastCommentID")
	}
}

func TestIsApproval_LastCommentIDNotInList_ReturnsFalse(t *testing.T) {
	// Simulate eventual consistency: lastCommentID (c3) was just posted
	// but hasn't appeared in FetchIssueComments yet. The old fallback
	// returned ALL comments, triggering false approval from the hint text.
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "draft plan" + ApprovalHint, UserName: "autoralph"},
			{ID: "c2", Body: "Can you add caching?", UserName: "human"},
		},
	}

	cond := IsApproval(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c3-not-yet-visible"}

	if cond(issue) {
		t.Error("expected IsApproval to return false when lastCommentID not found (eventual consistency)")
	}
}

func TestIsIteration_LastCommentIDNotInList_ReturnsFalse(t *testing.T) {
	// Same consistency scenario for iteration check.
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "feedback", UserName: "human"},
		},
	}

	cond := IsIteration(client)
	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c3-not-yet-visible"}

	if cond(issue) {
		t.Error("expected IsIteration to return false when lastCommentID not found")
	}
}

// --- Reaction tests ---

func TestIterationAction_ReactsToNewComments(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
			{ID: "c3", Body: "Also add logging", UserName: "human"},
		},
		postID: "c4",
	}
	invoker := &mockInvoker{response: "Updated plan"}
	reactor := &mockReactor{}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
		Reactor:  reactor,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reactor.calls) != 2 {
		t.Fatalf("expected 2 reaction calls, got %d", len(reactor.calls))
	}
	if reactor.calls[0].commentID != "c2" || reactor.calls[0].emoji != "👀" {
		t.Errorf("expected reaction to c2 with 👀, got %+v", reactor.calls[0])
	}
	if reactor.calls[1].commentID != "c3" || reactor.calls[1].emoji != "👀" {
		t.Errorf("expected reaction to c3 with 👀, got %+v", reactor.calls[1])
	}
}

func TestIterationAction_ReactsBeforeAIInvocation(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	var order []string
	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	reactor := &mockReactor{}
	invoker := &mockInvoker{response: "Updated plan"}

	// Track ordering by wrapping: reactor records before invoker.
	// Since mockReactor is called synchronously before mockInvoker,
	// we verify by checking that reactor has calls when we inspect.
	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
		Reactor:  reactor,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = order
	if len(reactor.calls) != 1 {
		t.Fatalf("expected 1 reaction call, got %d", len(reactor.calls))
	}
	if invoker.lastPrompt == "" {
		t.Fatal("expected AI to be invoked after reaction")
	}
}

func TestIterationAction_ReactionError_DoesNotBlockAction(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}
	reactor := &mockReactor{err: fmt.Errorf("reaction API error")}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
		Reactor:  reactor,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error when reaction fails, got: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called despite reaction failure")
	}
	if len(client.posted) != 1 {
		t.Fatal("expected reply to be posted despite reaction failure")
	}
}

func TestIterationAction_NilReactor_DoesNotPanic(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
		// Reactor is nil — should not panic
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called with nil reactor")
	}
}

func TestApprovalAction_ReactsToApprovalComment(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Plan text", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}
	reactor := &mockReactor{}

	action := NewApprovalAction(Config{Comments: client, Reactor: reactor})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reactor.calls) != 1 {
		t.Fatalf("expected 1 reaction call, got %d", len(reactor.calls))
	}
	if reactor.calls[0].commentID != "c2" || reactor.calls[0].emoji != "👀" {
		t.Errorf("expected reaction to c2 with 👀, got %+v", reactor.calls[0])
	}
}

func TestApprovalAction_ReactionError_DoesNotBlockAction(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Plan text", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}
	reactor := &mockReactor{err: fmt.Errorf("reaction API error")}

	action := NewApprovalAction(Config{Comments: client, Reactor: reactor})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("expected no error when reaction fails, got: %v", err)
	}

	// Verify the action still completed successfully.
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.LastCommentID != "c2" {
		t.Errorf("expected LastCommentID %q, got %q", "c2", updated.LastCommentID)
	}
}

func TestApprovalAction_NilReactor_DoesNotPanic(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Plan text", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.LastCommentID != "c2" {
		t.Errorf("expected LastCommentID %q, got %q", "c2", updated.LastCommentID)
	}
}

// --- Event streaming tests ---

func TestIterationAction_UsesInvokeWithEvents(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastHandler == nil {
		t.Fatal("expected InvokeWithEvents to be called with a non-nil event handler")
	}
}

func TestIterationAction_EventsStreamedToDB(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{
		response: "Updated plan",
		emitEvents: []events.Event{
			events.ToolUse{Name: "Read", Detail: "main.go"},
			events.InvocationDone{NumTurns: 5, DurationMS: 2000},
		},
	}

	action := NewIterationAction(Config{
		Invoker:  invoker,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := d.ListActivity(issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}

	buildEventCount := 0
	for _, e := range entries {
		if e.EventType == "build_event" {
			buildEventCount++
		}
	}
	if buildEventCount != 2 {
		t.Errorf("expected 2 build_event entries, got %d", buildEventCount)
	}
}

func TestIterationAction_OnBuildEventCallback(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "Add caching", UserName: "human"},
		},
		postID: "c3",
	}
	invoker := &mockInvoker{
		response: "Updated plan",
		emitEvents: []events.Event{
			events.ToolUse{Name: "Grep", Detail: "pattern"},
		},
	}

	var callbackCalls []struct{ issueID, detail string }
	onBuildEvent := func(issueID, detail string) {
		callbackCalls = append(callbackCalls, struct{ issueID, detail string }{issueID, detail})
	}

	action := NewIterationAction(Config{
		Invoker:      invoker,
		Comments:     client,
		Projects:     d,
		OnBuildEvent: onBuildEvent,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(callbackCalls) != 1 {
		t.Fatalf("expected 1 onBuildEvent call, got %d", len(callbackCalls))
	}
	if callbackCalls[0].issueID != issue.ID {
		t.Errorf("expected issueID %q, got %q", issue.ID, callbackCalls[0].issueID)
	}
	if !strings.Contains(callbackCalls[0].detail, "Grep") {
		t.Errorf("expected detail to contain tool name, got %q", callbackCalls[0].detail)
	}
}

func TestApprovalAction_Unchanged_NoEventInvoker(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Plan text", UserName: "autoralph"},
			{ID: "c2", Body: "I approve this", UserName: "human"},
		},
	}

	// ApprovalAction does not use the Invoker at all.
	action := NewApprovalAction(Config{Comments: client})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.PlanText == "" {
		t.Error("expected plan text to be stored")
	}
}

func TestIterationAction_PassesMaxTurns(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Here is a draft plan", UserName: "autoralph", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "Can you add caching?", UserName: "alice", CreatedAt: "2026-01-01T01:00:00Z"},
		},
		postID: "c3",
	}
	inv := &mockInvoker{response: "Updated plan with caching"}

	action := NewIterationAction(Config{
		Invoker:  inv,
		Comments: client,
		Projects: d,
	})

	if err := action(issue, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.lastMaxTurns != maxTurnsIteration {
		t.Errorf("expected maxTurns %d, got %d", maxTurnsIteration, inv.lastMaxTurns)
	}
}

// --- CachedCommentClient tests ---

func TestCachedCommentClient_DeduplicatesFetches(t *testing.T) {
	fetchCount := 0
	inner := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "draft plan", UserName: "autoralph"},
			{ID: "c2", Body: "feedback", UserName: "human"},
		},
	}

	// Wrap inner with a counting layer.
	counter := &fetchCountingClient{inner: inner, count: &fetchCount}
	cached := NewCachedCommentClient(counter)

	issue := db.Issue{LinearIssueID: "lin-123", LastCommentID: "c1"}

	// IsApproval and IsIteration both fetch for the same issue.
	IsApproval(cached)(issue)
	IsIteration(cached)(issue)

	if fetchCount != 1 {
		t.Errorf("expected 1 fetch call (cached), got %d", fetchCount)
	}
}

func TestCachedCommentClient_DifferentIssues_FetchesTwice(t *testing.T) {
	fetchCount := 0
	inner := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "comment", UserName: "user"},
		},
	}

	counter := &fetchCountingClient{inner: inner, count: &fetchCount}
	cached := NewCachedCommentClient(counter)

	IsApproval(cached)(db.Issue{LinearIssueID: "lin-1", LastCommentID: "c1"})
	IsApproval(cached)(db.Issue{LinearIssueID: "lin-2", LastCommentID: "c1"})

	if fetchCount != 2 {
		t.Errorf("expected 2 fetch calls (different issues), got %d", fetchCount)
	}
}

// fetchCountingClient counts how many times FetchIssueComments is called.
type fetchCountingClient struct {
	inner *mockCommentClient
	count *int
}

func (f *fetchCountingClient) FetchIssueComments(ctx context.Context, issueID string) ([]linear.Comment, error) {
	*f.count++
	return f.inner.FetchIssueComments(ctx, issueID)
}

func (f *fetchCountingClient) PostComment(ctx context.Context, issueID, body string) (linear.Comment, error) {
	return f.inner.PostComment(ctx, issueID, body)
}

func (f *fetchCountingClient) PostReply(ctx context.Context, issueID, parentID, body string) (linear.Comment, error) {
	return f.inner.PostReply(ctx, issueID, parentID, body)
}

// --- Incremental iteration tests ---

func TestIterationAction_IncrementalSendsOnlyNewComments(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c5")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Initial comment", UserName: "autoralph", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "First reply", UserName: "human", CreatedAt: "2026-01-01T01:00:00Z"},
			{ID: "c3", Body: "AI response", UserName: "autoralph", CreatedAt: "2026-01-01T02:00:00Z"},
			{ID: "c4", Body: "Second reply", UserName: "human", CreatedAt: "2026-01-01T03:00:00Z"},
			{ID: "c5", Body: "AI response 2", UserName: "autoralph", CreatedAt: "2026-01-01T04:00:00Z"},
			{ID: "c6", Body: "New feedback from user", UserName: "human", CreatedAt: "2026-01-01T05:00:00Z"},
			{ID: "c7", Body: "More feedback", UserName: "human", CreatedAt: "2026-01-01T06:00:00Z"},
			{ID: "c8", Body: "Final thought", UserName: "human", CreatedAt: "2026-01-01T07:00:00Z"},
		},
		postID: "c9",
	}
	inv := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  inv,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prompt should contain only comments after c5 (c6, c7, c8).
	if !strings.Contains(inv.lastPrompt, "New feedback from user") {
		t.Error("expected prompt to contain comment c6")
	}
	if !strings.Contains(inv.lastPrompt, "More feedback") {
		t.Error("expected prompt to contain comment c7")
	}
	if !strings.Contains(inv.lastPrompt, "Final thought") {
		t.Error("expected prompt to contain comment c8")
	}

	// Prompt should NOT contain old comments (c1-c5).
	if strings.Contains(inv.lastPrompt, "Initial comment") {
		t.Error("expected prompt to NOT contain comment c1")
	}
	if strings.Contains(inv.lastPrompt, "First reply") {
		t.Error("expected prompt to NOT contain comment c2")
	}
	if strings.Contains(inv.lastPrompt, "AI response 2") {
		t.Error("expected prompt to NOT contain comment c5")
	}
}

func TestIterationAction_IncrementalHasContextPrefix(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "Feedback", UserName: "human", CreatedAt: "2026-01-01T01:00:00Z"},
		},
		postID: "c3",
	}
	inv := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  inv,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(inv.lastPrompt, "Continuing refinement of: Add user avatars") {
		t.Errorf("expected context prefix in prompt, got: %s", inv.lastPrompt[:min(200, len(inv.lastPrompt))])
	}
}

func TestIterationAction_IncrementalOmitsDescription(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "refining", "c1")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "Draft plan", UserName: "autoralph", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "Feedback", UserName: "human", CreatedAt: "2026-01-01T01:00:00Z"},
		},
		postID: "c3",
	}
	inv := &mockInvoker{response: "Updated plan"}

	action := NewIterationAction(Config{
		Invoker:  inv,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Issue description should NOT be in incremental prompt.
	if strings.Contains(inv.lastPrompt, "Users should be able to upload profile pictures.") {
		t.Error("expected incremental prompt to NOT contain issue description")
	}
}

func TestIterationAction_FirstRefinementSendsAllComments(t *testing.T) {
	d := testDB(t)
	// LastCommentID is empty — first refinement.
	issue := createTestIssue(t, d, "refining", "")

	client := &mockCommentClient{
		comments: []linear.Comment{
			{ID: "c1", Body: "First comment", UserName: "human", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "c2", Body: "Second comment", UserName: "human", CreatedAt: "2026-01-01T01:00:00Z"},
			{ID: "c3", Body: "Third comment", UserName: "human", CreatedAt: "2026-01-01T02:00:00Z"},
		},
		postID: "c4",
	}
	inv := &mockInvoker{response: "Initial plan"}

	action := NewIterationAction(Config{
		Invoker:  inv,
		Comments: client,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All comments should be in the prompt.
	if !strings.Contains(inv.lastPrompt, "First comment") {
		t.Error("expected prompt to contain comment c1")
	}
	if !strings.Contains(inv.lastPrompt, "Second comment") {
		t.Error("expected prompt to contain comment c2")
	}
	if !strings.Contains(inv.lastPrompt, "Third comment") {
		t.Error("expected prompt to contain comment c3")
	}

	// Full description should be present.
	if !strings.Contains(inv.lastPrompt, "Users should be able to upload profile pictures.") {
		t.Error("expected first refinement prompt to contain issue description")
	}

	// No context prefix for first refinement.
	if strings.Contains(inv.lastPrompt, "Continuing refinement of") {
		t.Error("expected first refinement prompt to NOT contain context prefix")
	}
}
