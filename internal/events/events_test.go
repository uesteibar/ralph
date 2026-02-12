package events

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stripANSI removes ANSI escape codes from styled output for text assertions.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestPlainTextHandler_ToolUse_WithDetail(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(ToolUse{Name: "Read", Detail: "./file.go"})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "→") {
		t.Error("expected arrow symbol")
	}
	if !strings.Contains(output, "Read") {
		t.Error("expected tool name 'Read'")
	}
	if !strings.Contains(output, "./file.go") {
		t.Error("expected detail './file.go'")
	}
}

func TestPlainTextHandler_ToolUse_WithoutDetail(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(ToolUse{Name: "Glob"})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "→") {
		t.Error("expected arrow symbol")
	}
	if !strings.Contains(output, "Glob") {
		t.Error("expected tool name 'Glob'")
	}
}

func TestPlainTextHandler_AgentText(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(AgentText{Text: "Hello\nWorld"})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "Hello") {
		t.Error("expected text 'Hello'")
	}
	if !strings.Contains(output, "World") {
		t.Error("expected text 'World'")
	}
	// Should have blank lines around text
	if !strings.HasPrefix(output, "\n") {
		t.Error("expected leading blank line")
	}
	if !strings.HasSuffix(output, "\n\n") {
		t.Error("expected trailing blank line")
	}
}

func TestPlainTextHandler_InvocationDone(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(InvocationDone{NumTurns: 5, DurationMS: 12000})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "✓") {
		t.Error("expected checkmark")
	}
	if !strings.Contains(output, "Done") {
		t.Error("expected 'Done'")
	}
	if !strings.Contains(output, "5 turns") {
		t.Error("expected '5 turns'")
	}
	if !strings.Contains(output, "12s") {
		t.Error("expected '12s'")
	}
}

func TestPlainTextHandler_IterationStart(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(IterationStart{Iteration: 3, MaxIterations: 20})

	output := buf.String()
	if !strings.Contains(output, "iteration 3/20") {
		t.Errorf("expected 'iteration 3/20', got %q", output)
	}
}

func TestPlainTextHandler_StoryStarted(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(StoryStarted{StoryID: "US-001", Title: "Build auth"})

	output := buf.String()
	if !strings.Contains(output, "working on US-001: Build auth") {
		t.Errorf("expected story started output, got %q", output)
	}
}

func TestPlainTextHandler_QAPhaseStarted(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(QAPhaseStarted{Phase: "verification"})

	output := buf.String()
	if !strings.Contains(output, "all stories pass — running QA verification") {
		t.Errorf("expected QA phase output, got %q", output)
	}
}

func TestPlainTextHandler_UsageLimitWait(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	resetAt := time.Date(2026, 2, 5, 15, 30, 0, 0, time.UTC)
	h.Handle(UsageLimitWait{
		WaitDuration: 30 * time.Minute,
		ResetAt:      resetAt,
	})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "Usage limit reached") {
		t.Error("expected 'Usage limit reached'")
	}
	if !strings.Contains(output, "waiting") {
		t.Error("expected 'waiting'")
	}
}

func TestPlainTextHandler_ImplementsEventHandler(t *testing.T) {
	var h EventHandler = &PlainTextHandler{W: &bytes.Buffer{}}
	_ = h // Compile-time check that PlainTextHandler satisfies EventHandler
}

func TestPlainTextHandler_LogMessage_Info(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(LogMessage{Level: "info", Message: "all stories pass"})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "all stories pass") {
		t.Errorf("expected message in output, got %q", output)
	}
}

func TestPlainTextHandler_LogMessage_Warning(t *testing.T) {
	var buf bytes.Buffer
	h := &PlainTextHandler{W: &buf}

	h.Handle(LogMessage{Level: "warning", Message: "git check failed"})

	output := stripANSI(buf.String())
	if !strings.Contains(output, "git check failed") {
		t.Errorf("expected message in output, got %q", output)
	}
}

func TestEventTypes_ImplementEvent(t *testing.T) {
	// Compile-time verification that all event types implement Event
	var _ Event = ToolUse{}
	var _ Event = AgentText{}
	var _ Event = InvocationDone{}
	var _ Event = IterationStart{}
	var _ Event = StoryStarted{}
	var _ Event = QAPhaseStarted{}
	var _ Event = UsageLimitWait{}
	var _ Event = LogMessage{}
}
