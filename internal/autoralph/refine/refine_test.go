package refine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/approve"
	"github.com/uesteibar/ralph/internal/autoralph/db"
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

func createTestIssue(t *testing.T, d *db.DB, state string) db.Issue {
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
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}
	return issue
}

// mockInvoker records the prompt and returns a fixed response.
type mockInvoker struct {
	lastPrompt string
	response   string
	err        error
}

func (m *mockInvoker) Invoke(ctx context.Context, prompt, dir string) (string, error) {
	m.lastPrompt = prompt
	return m.response, m.err
}

// mockPoster records calls to PostComment.
type mockPoster struct {
	calls []posterCall
	err   error
}

type posterCall struct {
	linearIssueID string
	body          string
}

func (m *mockPoster) PostComment(ctx context.Context, linearIssueID, body string) (string, error) {
	m.calls = append(m.calls, posterCall{linearIssueID: linearIssueID, body: body})
	return "mock-comment-id", m.err
}

func TestRefineAction_InvokesAIWithPrompt(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	invoker := &mockInvoker{response: "Here are my clarifying questions..."}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called with a prompt")
	}
	if !strings.Contains(invoker.lastPrompt, "Add user avatars") {
		t.Error("expected prompt to contain issue title")
	}
	if !strings.Contains(invoker.lastPrompt, "Users should be able to upload profile pictures.") {
		t.Error("expected prompt to contain issue description")
	}
}

func TestRefineAction_PostsCommentOnLinear(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	aiResponse := "## Clarifying Questions\n\n1. What image formats should be supported?"
	invoker := &mockInvoker{response: aiResponse}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
		Projects: d,
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(poster.calls) != 1 {
		t.Fatalf("expected 1 PostComment call, got %d", len(poster.calls))
	}
	if poster.calls[0].linearIssueID != "lin-123" {
		t.Errorf("expected LinearIssueID %q, got %q", "lin-123", poster.calls[0].linearIssueID)
	}
	expectedBody := aiResponse + approve.ApprovalHint
	if poster.calls[0].body != expectedBody {
		t.Errorf("expected comment body to contain AI response + approval hint, got %q", poster.calls[0].body)
	}
	if !strings.Contains(poster.calls[0].body, "I approve this") {
		t.Error("expected posted body to contain approval hint")
	}
}

func TestRefineAction_LogsActivity(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	invoker := &mockInvoker{response: "Some AI output"}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
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
	// Most recent entry is the refinement result (ListActivity returns DESC).
	if entries[0].EventType != "ai_refinement" {
		t.Errorf("expected event_type %q, got %q", "ai_refinement", entries[0].EventType)
	}
	if !strings.Contains(entries[0].Detail, "Some AI output") {
		t.Error("expected activity detail to contain AI output")
	}
}

func TestRefineAction_AIError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	invoker := &mockInvoker{err: fmt.Errorf("AI service unavailable")}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
		Projects: d,
	})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when AI invocation fails")
	}
	if !strings.Contains(err.Error(), "AI service unavailable") {
		t.Errorf("expected error to contain AI failure message, got: %v", err)
	}
	if len(poster.calls) != 0 {
		t.Error("expected no comment to be posted when AI fails")
	}
}

func TestRefineAction_PostCommentError_ReturnsError(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	invoker := &mockInvoker{response: "AI output"}
	poster := &mockPoster{err: fmt.Errorf("Linear API error")}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
		Projects: d,
	})

	err := action(issue, d)
	if err == nil {
		t.Fatal("expected error when posting comment fails")
	}
	if !strings.Contains(err.Error(), "Linear API error") {
		t.Errorf("expected error to contain Linear failure message, got: %v", err)
	}
}

func TestRefineAction_WithOverrideDir(t *testing.T) {
	d := testDB(t)
	issue := createTestIssue(t, d, "queued")

	invoker := &mockInvoker{response: "Custom template output"}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:     invoker,
		Poster:      poster,
		Projects:    d,
		OverrideDir: "/nonexistent/path", // falls back to embedded
	})

	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still work (falls back to embedded template)
	if invoker.lastPrompt == "" {
		t.Fatal("expected AI invoker to be called")
	}
}

func TestRefineAction_EmptyDescription_StillWorks(t *testing.T) {
	d := testDB(t)
	p, err := d.CreateProject(db.Project{Name: "test-project", LocalPath: "/tmp/test"})
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	issue, err := d.CreateIssue(db.Issue{
		ProjectID:     p.ID,
		LinearIssueID: "lin-empty",
		Identifier:    "PROJ-99",
		Title:         "Minimal issue",
		Description:   "",
		State:         "queued",
	})
	if err != nil {
		t.Fatalf("creating test issue: %v", err)
	}

	invoker := &mockInvoker{response: "Need more details"}
	poster := &mockPoster{}

	action := NewAction(Config{
		Invoker:  invoker,
		Poster:   poster,
		Projects: d,
	})

	err = action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(invoker.lastPrompt, "Minimal issue") {
		t.Error("expected prompt to contain issue title")
	}
}
