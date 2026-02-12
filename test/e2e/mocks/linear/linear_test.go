package linear

import (
	"context"
	"strings"
	"testing"

	linearClient "github.com/uesteibar/ralph/internal/autoralph/linear"
)

func TestMock_FetchAssignedIssues_Success(t *testing.T) {
	mock := New()
	mock.AddIssue(Issue{
		ID: IssueUUID("test-1"), Identifier: "TEST-1", Title: "Test issue",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	issues, err := client.FetchAssignedIssues(context.Background(), TestTeamID, TestAssigneeID, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Identifier != "TEST-1" {
		t.Errorf("expected identifier TEST-1, got %q", issues[0].Identifier)
	}
}

func TestMock_FetchAssignedIssues_RejectsNonUUIDTeamID(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	_, err := client.FetchAssignedIssues(context.Background(), "not-a-uuid", TestAssigneeID, "", "")
	if err == nil {
		t.Fatal("expected error for non-UUID teamID, got nil")
	}
	if !strings.Contains(err.Error(), "Argument Validation Error") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestMock_FetchAssignedIssues_RejectsNonUUIDAssigneeID(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	_, err := client.FetchAssignedIssues(context.Background(), TestTeamID, "human-name", "", "")
	if err == nil {
		t.Fatal("expected error for non-UUID assigneeID, got nil")
	}
	if !strings.Contains(err.Error(), "Argument Validation Error") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestMock_FetchIssueComments_Success(t *testing.T) {
	issueID := IssueUUID("test-1")
	mock := New()
	mock.AddIssue(Issue{ID: issueID, Identifier: "TEST-1", Title: "Test"})
	mock.AddComment(issueID, Comment{
		ID: CommentUUID("c-1"), Body: "Hello", UserName: "dev", CreatedAt: "2026-01-01T00:00:00Z",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	comments, err := client.FetchIssueComments(context.Background(), issueID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "Hello" {
		t.Errorf("expected body 'Hello', got %q", comments[0].Body)
	}
}

func TestMock_PostComment_Success(t *testing.T) {
	issueID := IssueUUID("test-1")
	mock := New()
	mock.AddIssue(Issue{ID: issueID, Identifier: "TEST-1", Title: "Test"})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	comment, err := client.PostComment(context.Background(), issueID, "My comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if comment.Body != "My comment" {
		t.Errorf("expected body 'My comment', got %q", comment.Body)
	}
	if len(mock.ReceivedComments) != 1 {
		t.Fatalf("expected 1 received comment, got %d", len(mock.ReceivedComments))
	}
	if mock.ReceivedComments[0].Body != "My comment" {
		t.Errorf("expected tracked body 'My comment', got %q", mock.ReceivedComments[0].Body)
	}
}

func TestMock_UpdateIssueState_Success(t *testing.T) {
	issueID := IssueUUID("test-1")
	mock := New()
	mock.AddIssue(Issue{
		ID: issueID, Identifier: "TEST-1", Title: "Test",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	err := client.UpdateIssueState(context.Background(), issueID, StateDoneID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StateUpdates) != 1 {
		t.Fatalf("expected 1 state update, got %d", len(mock.StateUpdates))
	}
	if mock.StateUpdates[0].StateID != StateDoneID {
		t.Errorf("expected state %q, got %q", StateDoneID, mock.StateUpdates[0].StateID)
	}

	iss := mock.GetIssue(issueID)
	if iss.StateName != "Done" {
		t.Errorf("expected state name 'Done', got %q", iss.StateName)
	}
}

func TestMock_FetchWorkflowStates_Success(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	states, err := client.FetchWorkflowStates(context.Background(), TestTeamID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(states) != 5 {
		t.Fatalf("expected 5 default states, got %d", len(states))
	}
}

func TestMock_FetchWorkflowStates_RejectsNonUUIDTeamID(t *testing.T) {
	mock := New()
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	_, err := client.FetchWorkflowStates(context.Background(), "team-slug")
	if err == nil {
		t.Fatal("expected error for non-UUID teamID, got nil")
	}
	if !strings.Contains(err.Error(), "Argument Validation Error") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestMock_SimulateApproval(t *testing.T) {
	issueID := IssueUUID("test-1")
	mock := New()
	mock.AddIssue(Issue{ID: issueID, Identifier: "TEST-1", Title: "Test"})
	mock.SimulateApproval(issueID, CommentUUID("approval"))

	comments := mock.GetComments(issueID)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "I approve this" {
		t.Errorf("expected approval body, got %q", comments[0].Body)
	}
}

func TestMock_CompletedIssuesExcluded(t *testing.T) {
	mock := New()
	mock.AddIssue(Issue{
		ID: IssueUUID("active"), Identifier: "TEST-1", Title: "Active",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
	})
	mock.AddIssue(Issue{
		ID: IssueUUID("done"), Identifier: "TEST-2", Title: "Done",
		StateID: StateDoneID, StateName: "Done", StateType: "completed",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	issues, err := client.FetchAssignedIssues(context.Background(), TestTeamID, TestAssigneeID, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 active issue, got %d", len(issues))
	}
	if issues[0].Title != "Active" {
		t.Errorf("expected 'Active', got %q", issues[0].Title)
	}
}

func TestMock_FetchTeams(t *testing.T) {
	mock := New()
	mock.AddTeam(Team{ID: TestTeamID, Key: "TEST", Name: "Test Team"})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	teamID, err := client.ResolveTeamID(context.Background(), "Test Team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if teamID != TestTeamID {
		t.Errorf("expected %q, got %q", TestTeamID, teamID)
	}
}

func TestMock_FetchTeams_ByKey(t *testing.T) {
	mock := New()
	mock.AddTeam(Team{ID: TestTeamID, Key: "TEST", Name: "Test Team"})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	teamID, err := client.ResolveTeamID(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if teamID != TestTeamID {
		t.Errorf("expected %q, got %q", TestTeamID, teamID)
	}
}

func TestMock_FetchUsers(t *testing.T) {
	mock := New()
	mock.AddUser(User{
		ID: TestAssigneeID, Name: "Test User",
		DisplayName: "testuser", Email: "test@example.com",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	userID, err := client.ResolveUserID(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != TestAssigneeID {
		t.Errorf("expected %q, got %q", TestAssigneeID, userID)
	}
}

func TestMock_ResolveTeamID_UUIDPassthrough(t *testing.T) {
	mock := New()
	_ = mock.Server(t) // start but unused — UUID passthrough doesn't hit API

	client := linearClient.New("test-key", linearClient.WithEndpoint("http://unused"))
	teamID, err := client.ResolveTeamID(context.Background(), TestTeamID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if teamID != TestTeamID {
		t.Errorf("expected %q, got %q", TestTeamID, teamID)
	}
}

func TestMock_FetchAssignedIssues_LabelFilter(t *testing.T) {
	mock := New()
	mock.AddIssue(Issue{
		ID: IssueUUID("labeled"), Identifier: "TEST-1", Title: "Labeled",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
		Labels: []string{"autoralph"},
	})
	mock.AddIssue(Issue{
		ID: IssueUUID("unlabeled"), Identifier: "TEST-2", Title: "Unlabeled",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))

	// With label filter: only labeled issue returned.
	issues, err := client.FetchAssignedIssues(context.Background(), TestTeamID, TestAssigneeID, "", "autoralph")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue with label filter, got %d", len(issues))
	}
	if issues[0].Title != "Labeled" {
		t.Errorf("expected 'Labeled', got %q", issues[0].Title)
	}

	// Without label filter: both issues returned.
	all, err := client.FetchAssignedIssues(context.Background(), TestTeamID, TestAssigneeID, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 issues without label filter, got %d", len(all))
	}
}

func TestMock_FetchAssignedIssues_LabelFilter_CaseInsensitive(t *testing.T) {
	mock := New()
	mock.AddIssue(Issue{
		ID: IssueUUID("ci-label"), Identifier: "TEST-1", Title: "Mixed Case",
		StateID: StateTodoID, StateName: "Todo", StateType: "unstarted",
		Labels: []string{"AutoRalph"},
	})
	srv := mock.Server(t)

	client := linearClient.New("test-key", linearClient.WithEndpoint(srv.URL))
	issues, err := client.FetchAssignedIssues(context.Background(), TestTeamID, TestAssigneeID, "", "autoralph")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (case-insensitive match), got %d", len(issues))
	}
}

func TestMock_IssueUUID_Deterministic(t *testing.T) {
	a := IssueUUID("avatars")
	b := IssueUUID("avatars")
	c := IssueUUID("different")

	if a != b {
		t.Errorf("expected deterministic UUID: %q != %q", a, b)
	}
	if a == c {
		t.Error("expected different UUIDs for different names")
	}
	if !isValidUUID(a) {
		t.Errorf("expected valid UUID format, got %q", a)
	}
}
