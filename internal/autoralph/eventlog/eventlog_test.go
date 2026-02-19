package eventlog_test

import (
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/events"
)

func TestFormatDetail_ToolUse_WithDetail(t *testing.T) {
	got := eventlog.FormatDetail(events.ToolUse{Name: "Read", Detail: "main.go"})
	want := "→ Read main.go"
	if got != want {
		t.Errorf("FormatDetail(ToolUse with detail) = %q, want %q", got, want)
	}
}

func TestFormatDetail_ToolUse_WithoutDetail(t *testing.T) {
	got := eventlog.FormatDetail(events.ToolUse{Name: "Bash"})
	want := "→ Bash"
	if got != want {
		t.Errorf("FormatDetail(ToolUse without detail) = %q, want %q", got, want)
	}
}

func TestFormatDetail_IterationStart(t *testing.T) {
	got := eventlog.FormatDetail(events.IterationStart{Iteration: 3, MaxIterations: 10})
	want := "Iteration 3/10 started"
	if got != want {
		t.Errorf("FormatDetail(IterationStart) = %q, want %q", got, want)
	}
}

func TestFormatDetail_StoryStarted(t *testing.T) {
	got := eventlog.FormatDetail(events.StoryStarted{StoryID: "US-001", Title: "Extract eventlog"})
	want := "Story US-001: Extract eventlog"
	if got != want {
		t.Errorf("FormatDetail(StoryStarted) = %q, want %q", got, want)
	}
}

func TestFormatDetail_QAPhaseStarted(t *testing.T) {
	got := eventlog.FormatDetail(events.QAPhaseStarted{Phase: "verification"})
	want := "QA phase: verification"
	if got != want {
		t.Errorf("FormatDetail(QAPhaseStarted) = %q, want %q", got, want)
	}
}

func TestFormatDetail_LogMessage(t *testing.T) {
	got := eventlog.FormatDetail(events.LogMessage{Level: "info", Message: "building project"})
	want := "[info] building project"
	if got != want {
		t.Errorf("FormatDetail(LogMessage) = %q, want %q", got, want)
	}
}

func TestFormatDetail_AgentText(t *testing.T) {
	got := eventlog.FormatDetail(events.AgentText{Text: "thinking about the problem"})
	want := "thinking about the problem"
	if got != want {
		t.Errorf("FormatDetail(AgentText) = %q, want %q", got, want)
	}
}

func TestFormatDetail_InvocationDone(t *testing.T) {
	got := eventlog.FormatDetail(events.InvocationDone{NumTurns: 5, DurationMS: 12000})
	want := "Invocation done: 5 turns in 12000ms"
	if got != want {
		t.Errorf("FormatDetail(InvocationDone) = %q, want %q", got, want)
	}
}

func TestFormatDetail_UnknownEvent_ReturnsEmpty(t *testing.T) {
	got := eventlog.FormatDetail(events.PRDRefresh{})
	if got != "" {
		t.Errorf("FormatDetail(unknown event) = %q, want empty string", got)
	}
}

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	if _, err = d.CreateProject(db.Project{
		ID:   "proj-1",
		Name: "test-project",
	}); err != nil {
		t.Fatalf("creating test project: %v", err)
	}

	if _, err = d.CreateIssue(db.Issue{
		ID:        "issue-1",
		ProjectID: "proj-1",
		Title:     "test issue",
		State:     "building",
	}); err != nil {
		t.Fatalf("creating test issue: %v", err)
	}

	return d
}

func TestHandler_Handle_LogsActivity(t *testing.T) {
	d := setupTestDB(t)

	h := eventlog.New(d, "issue-1", nil, nil)
	h.Handle(events.ToolUse{Name: "Read", Detail: "file.go"})

	entries, err := d.ListBuildActivity("issue-1", 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d activity entries, want 1", len(entries))
	}
	if entries[0].EventType != "build_event" {
		t.Errorf("event type = %q, want %q", entries[0].EventType, "build_event")
	}
	if entries[0].Detail != "→ Read file.go" {
		t.Errorf("detail = %q, want %q", entries[0].Detail, "→ Read file.go")
	}
}

func TestHandler_Handle_CallsOnBuildEvent(t *testing.T) {
	d := setupTestDB(t)

	var callbackIssueID, callbackDetail string
	onBuildEvent := func(issueID, detail string) {
		callbackIssueID = issueID
		callbackDetail = detail
	}

	h := eventlog.New(d, "issue-1", nil, onBuildEvent)
	h.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 5000})

	if callbackIssueID != "issue-1" {
		t.Errorf("callback issueID = %q, want %q", callbackIssueID, "issue-1")
	}
	wantDetail := "Invocation done: 3 turns in 5000ms"
	if callbackDetail != wantDetail {
		t.Errorf("callback detail = %q, want %q", callbackDetail, wantDetail)
	}
}

func TestHandler_Handle_ForwardsToUpstream(t *testing.T) {
	d := setupTestDB(t)

	var received []events.Event
	upstream := &recordingHandler{events: &received}

	h := eventlog.New(d, "issue-1", upstream, nil)
	e := events.ToolUse{Name: "Edit"}
	h.Handle(e)

	if len(received) != 1 {
		t.Fatalf("upstream received %d events, want 1", len(received))
	}
}

func TestHandler_Handle_SkipsLoggingForUnknownEvent(t *testing.T) {
	d := setupTestDB(t)

	var callbackCalled bool
	onBuildEvent := func(_, _ string) { callbackCalled = true }

	var received []events.Event
	upstream := &recordingHandler{events: &received}

	h := eventlog.New(d, "issue-1", upstream, onBuildEvent)
	h.Handle(events.PRDRefresh{})

	entries, err := d.ListBuildActivity("issue-1", 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d activity entries, want 0 for unknown event", len(entries))
	}
	if callbackCalled {
		t.Error("onBuildEvent should not be called for unknown events")
	}
	// Unknown events should still be forwarded upstream
	if len(received) != 1 {
		t.Errorf("upstream received %d events, want 1 (forwarding should always happen)", len(received))
	}
}

func TestHandler_Handle_NilUpstreamAndCallback(t *testing.T) {
	d := setupTestDB(t)

	h := eventlog.New(d, "issue-1", nil, nil)
	// Should not panic with nil upstream and callback
	h.Handle(events.ToolUse{Name: "Bash"})

	entries, err := d.ListBuildActivity("issue-1", 10, 0)
	if err != nil {
		t.Fatalf("listing activity: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d activity entries, want 1", len(entries))
	}
}

type recordingHandler struct {
	events *[]events.Event
}

func (r *recordingHandler) Handle(e events.Event) {
	*r.events = append(*r.events, e)
}
