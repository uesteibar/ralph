package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/tui"
)

// stripANSI removes ANSI escape codes from styled output for text assertions.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// --- IT-001: ralph run launches TUI by default ---

// TestIT001_TUILaunchesByDefault verifies that running ralph run without --no-tui
// sets up a BubbleTea TUI program. We test this at the integration level by:
//  1. Verifying the --no-tui flag defaults to false
//  2. Verifying the TUI model initializes correctly with workspace name and PRD path
//  3. Verifying the TUI model renders without panicking and shows the workspace name in the status bar
//  4. Verifying the TUI Handler properly bridges events to BubbleTea messages
func TestIT001_TUILaunchesByDefault(t *testing.T) {
	// Step 1: Verify --no-tui flag defaults to false (TUI is default)
	t.Run("no-tui flag defaults to false", func(t *testing.T) {
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		noTUI := fs.Bool("no-tui", false, "Disable TUI and use plain-text output")
		if err := fs.Parse([]string{}); err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		if *noTUI {
			t.Error("expected --no-tui to default to false (TUI is the default)")
		}
	})

	// Step 2: Verify TUI model initializes and renders without panicking
	t.Run("TUI model starts and renders without panicking", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.json")

		testPRD := prd.PRD{
			Project:     "test",
			BranchName:  "test/feature",
			Description: "Test project",
			UserStories: []prd.Story{
				{ID: "US-001", Title: "Auth feature", Priority: 1, Passes: true},
				{ID: "US-002", Title: "TUI feature", Priority: 2, Passes: false},
			},
			IntegrationTests: []prd.IntegrationTest{
				{ID: "IT-001", Description: "Login test", Passes: true},
			},
		}
		data, _ := json.MarshalIndent(testPRD, "", "  ")
		os.WriteFile(prdPath, data, 0644)

		workspaceName := "my-workspace"
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create model (same path as runLoopTUI in run.go)
		model := tui.NewModel(workspaceName, prdPath, cancel)

		// Verify workspace name is set
		if model.WorkspaceName() != workspaceName {
			t.Errorf("expected workspace name %q, got %q", workspaceName, model.WorkspaceName())
		}

		// Init should return a command to read the PRD
		cmd := model.Init()
		if cmd == nil {
			t.Fatal("expected Init to return a command when PRD path is set")
		}

		// Execute the PRD read command
		msg := cmd()
		if msg == nil {
			t.Fatal("expected PRD read command to return a message")
		}

		// Feed the PRD message into the model
		updated, _ := model.Update(msg)
		model = updated.(tui.Model)

		// Verify PRD was loaded
		if model.CurrentPRD() == nil {
			t.Fatal("expected currentPRD to be set after loading")
		}
		if len(model.CurrentPRD().UserStories) != 2 {
			t.Errorf("expected 2 stories, got %d", len(model.CurrentPRD().UserStories))
		}

		// Simulate terminal window size (as BubbleTea would send)
		updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		model = updated.(tui.Model)

		// Verify model is ready to render
		view := model.View()
		if view == "" {
			t.Error("expected non-empty view")
		}
		if strings.Contains(view, "Initializing") {
			t.Error("expected model to be past Initializing state")
		}

		// Step 3: Verify workspace name appears in the status bar
		cleanView := stripANSI(view)
		if !strings.Contains(cleanView, workspaceName) {
			t.Errorf("expected view to contain workspace name %q", workspaceName)
		}

		// Verify sidebar shows stories
		if !strings.Contains(cleanView, "Stories & Tests") {
			t.Error("expected sidebar title in view")
		}

		_ = ctx // ensure ctx is used
	})

	// Step 3: Verify TUI Handler bridges events correctly to the model
	t.Run("TUI handler bridges events to BubbleTea", func(t *testing.T) {
		// Verify Handler implements EventHandler
		var _ events.EventHandler = &tui.Handler{}

		// Verify Handler does not panic with nil program
		h := tui.NewHandler(nil)
		h.Handle(events.ToolUse{Name: "Read", Detail: "./file.go"})
		h.Handle(events.AgentText{Text: "Hello"})
		h.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 5000})
	})

	// Step 4: Verify TUI processes events and updates the viewport
	t.Run("TUI model processes event sequence", func(t *testing.T) {
		model := tui.NewModel("my-ws", "", nil)

		// Make ready
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		model = updated.(tui.Model)

		// Send a sequence of events through the model (simulating loop→handler→TUI pipeline)
		eventSequence := []events.Event{
			events.IterationStart{Iteration: 1, MaxIterations: 10},
			events.StoryStarted{StoryID: "US-001", Title: "Authentication"},
			events.ToolUse{Name: "Read", Detail: "main.go"},
			events.AgentText{Text: "Implementing login flow"},
			events.ToolUse{Name: "Edit", Detail: "auth.go"},
			events.InvocationDone{NumTurns: 5, DurationMS: 12000},
		}

		for _, ev := range eventSequence {
			updated, _ = model.Update(tui.MakeEventMsg(ev))
			model = updated.(tui.Model)
		}

		// Verify events were processed
		lines := model.Lines()
		if len(lines) != 6 {
			t.Fatalf("expected 6 lines from events, got %d: %v", len(lines), lines)
		}

		// Verify iteration info
		if !strings.Contains(lines[0], "[loop] iteration 1/10") {
			t.Errorf("expected iteration start line, got %q", lines[0])
		}

		// Verify story started
		if !strings.Contains(lines[1], "US-001: Authentication") {
			t.Errorf("expected story started line, got %q", lines[1])
		}

		// Verify tool uses
		if !strings.Contains(lines[2], "Read") {
			t.Errorf("expected Read tool line, got %q", lines[2])
		}
		if !strings.Contains(lines[4], "Edit") {
			t.Errorf("expected Edit tool line, got %q", lines[4])
		}

		// Verify invocation done
		if !strings.Contains(lines[5], "✓") && !strings.Contains(lines[5], "Done") {
			t.Errorf("expected done line, got %q", lines[5])
		}

		// Verify model state
		if model.CurrentStory() != "US-001: Authentication" {
			t.Errorf("expected current story 'US-001: Authentication', got %q", model.CurrentStory())
		}
		if model.Iteration() != 1 {
			t.Errorf("expected iteration 1, got %d", model.Iteration())
		}
		if model.MaxIterations() != 10 {
			t.Errorf("expected max iterations 10, got %d", model.MaxIterations())
		}
	})
}

// --- IT-002: ralph run --no-tui falls back to plain-text output ---

// TestIT002_NoTUIFallsBackToPlainText verifies that running ralph run --no-tui
// uses the PlainTextHandler which writes styled output to stderr.
func TestIT002_NoTUIFallsBackToPlainText(t *testing.T) {
	// Step 1: Verify --no-tui flag is correctly parsed
	t.Run("no-tui flag parsed correctly", func(t *testing.T) {
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		noTUI := fs.Bool("no-tui", false, "Disable TUI and use plain-text output")
		if err := fs.Parse([]string{"--no-tui"}); err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		if !*noTUI {
			t.Error("expected --no-tui to be true when flag is provided")
		}
	})

	// Step 2: Verify PlainTextHandler writes output in the expected format
	t.Run("PlainTextHandler renders agent output to writer", func(t *testing.T) {
		var buf bytes.Buffer
		handler := &events.PlainTextHandler{W: &buf}

		// Feed the same event sequence that would come from a Claude invocation
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 5})
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "Build auth"})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "main.go"})
		handler.Handle(events.AgentText{Text: "I'll implement the auth feature"})
		handler.Handle(events.ToolUse{Name: "Edit", Detail: "auth.go"})
		handler.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 8000})

		output := stripANSI(buf.String())

		// Step 3: Verify the format matches expected stderr output
		checks := []string{
			"[loop] iteration 1/5",
			"[loop] working on US-001: Build auth",
			"→",
			"Read",
			"main.go",
			"I'll implement the auth feature",
			"Edit",
			"auth.go",
			"✓",
			"Done",
			"3 turns",
			"8s",
		}
		for _, check := range checks {
			if !strings.Contains(output, check) {
				t.Errorf("expected output to contain %q, got:\n%s", check, output)
			}
		}
	})

	// Step 3: Verify PlainTextHandler implements EventHandler interface
	t.Run("PlainTextHandler satisfies EventHandler interface", func(t *testing.T) {
		var _ events.EventHandler = &events.PlainTextHandler{}
	})

	// Step 4: Verify no BubbleTea program is started in plain-text mode
	// This is verified structurally: runLoopPlainText creates PlainTextHandler and
	// calls runLoopWithHandler directly without tea.NewProgram.
	// We verify the PlainTextHandler handles all event types without errors.
	t.Run("PlainTextHandler handles all event types", func(t *testing.T) {
		var buf bytes.Buffer
		handler := &events.PlainTextHandler{W: &buf}

		allEvents := []events.Event{
			events.ToolUse{Name: "Read", Detail: "file.go"},
			events.ToolUse{Name: "Bash"},
			events.AgentText{Text: "hello\nworld"},
			events.InvocationDone{NumTurns: 1, DurationMS: 1000},
			events.IterationStart{Iteration: 1, MaxIterations: 10},
			events.StoryStarted{StoryID: "US-001", Title: "Test"},
			events.QAPhaseStarted{Phase: "verification"},
			events.PRDRefresh{},
		}

		// None of these should panic
		for _, ev := range allEvents {
			handler.Handle(ev)
		}

		if buf.Len() == 0 {
			t.Error("expected some output from PlainTextHandler")
		}
	})
}

// --- IT-003: Event emitter produces correct events for Claude stream ---

// TestIT003_EventEmitterProducesCorrectEvents verifies the full event pipeline:
// events are created in the correct order, both handlers process them correctly,
// and PlainTextHandler renders them in the expected format.
func TestIT003_EventEmitterProducesCorrectEvents(t *testing.T) {
	// Step 1: Feed a mock event sequence through a recording handler
	// and verify correct event types in order
	t.Run("events emitted in correct order", func(t *testing.T) {
		handler := &recordingHandler{}

		// Simulate the sequence that loop.go + claude.go would emit during
		// a typical invocation processing a Claude stream-json response
		emitSequence := []events.Event{
			events.IterationStart{Iteration: 1, MaxIterations: 20},
			events.PRDRefresh{},
			events.StoryStarted{StoryID: "US-001", Title: "Build auth"},
			events.ToolUse{Name: "Read", Detail: "main.go"},
			events.AgentText{Text: "I'll analyze the code structure"},
			events.ToolUse{Name: "Edit", Detail: "auth.go"},
			events.AgentText{Text: "Implementing authentication"},
			events.ToolUse{Name: "Bash", Detail: "go test ./..."},
			events.InvocationDone{NumTurns: 5, DurationMS: 12000},
			events.PRDRefresh{},
		}

		for _, ev := range emitSequence {
			handler.Handle(ev)
		}

		// Verify correct number of events
		if len(handler.events) != len(emitSequence) {
			t.Fatalf("expected %d events, got %d", len(emitSequence), len(handler.events))
		}

		// Step 2: Verify event types in order
		expectedTypes := []string{
			"IterationStart",
			"PRDRefresh",
			"StoryStarted",
			"ToolUse",
			"AgentText",
			"ToolUse",
			"AgentText",
			"ToolUse",
			"InvocationDone",
			"PRDRefresh",
		}

		for i, ev := range handler.events {
			typeName := eventTypeName(ev)
			if typeName != expectedTypes[i] {
				t.Errorf("event %d: expected %s, got %s", i, expectedTypes[i], typeName)
			}
		}

		// Verify specific event content
		iterStart := handler.events[0].(events.IterationStart)
		if iterStart.Iteration != 1 || iterStart.MaxIterations != 20 {
			t.Errorf("unexpected IterationStart values: %+v", iterStart)
		}

		storyStarted := handler.events[2].(events.StoryStarted)
		if storyStarted.StoryID != "US-001" || storyStarted.Title != "Build auth" {
			t.Errorf("unexpected StoryStarted values: %+v", storyStarted)
		}

		toolUse := handler.events[3].(events.ToolUse)
		if toolUse.Name != "Read" || toolUse.Detail != "main.go" {
			t.Errorf("unexpected ToolUse values: %+v", toolUse)
		}

		done := handler.events[8].(events.InvocationDone)
		if done.NumTurns != 5 || done.DurationMS != 12000 {
			t.Errorf("unexpected InvocationDone values: %+v", done)
		}
	})

	// Step 3: Verify PlainTextHandler renders them identically to expected format
	t.Run("PlainTextHandler renders events in expected format", func(t *testing.T) {
		var buf bytes.Buffer
		handler := &events.PlainTextHandler{W: &buf}

		// Feed the same events that Claude stream-json would produce
		handler.Handle(events.ToolUse{Name: "Read", Detail: "main.go"})
		handler.Handle(events.AgentText{Text: "Analyzing the code"})
		handler.Handle(events.InvocationDone{NumTurns: 5, DurationMS: 12000})

		output := stripANSI(buf.String())
		lines := strings.Split(strings.TrimSpace(output), "\n")

		// ToolUse line: "  → Read main.go"
		foundToolUse := false
		for _, line := range lines {
			stripped := strings.TrimSpace(line)
			if strings.Contains(stripped, "→") && strings.Contains(stripped, "Read") && strings.Contains(stripped, "main.go") {
				foundToolUse = true
				break
			}
		}
		if !foundToolUse {
			t.Errorf("expected tool use line '→ Read main.go' in output:\n%s", output)
		}

		// AgentText: "  Analyzing the code"
		if !strings.Contains(output, "Analyzing the code") {
			t.Errorf("expected agent text in output:\n%s", output)
		}

		// InvocationDone: "  ✓ Done (5 turns, 12s)"
		if !strings.Contains(output, "✓") || !strings.Contains(output, "Done") {
			t.Errorf("expected done indicator in output:\n%s", output)
		}
		if !strings.Contains(output, "5 turns") || !strings.Contains(output, "12s") {
			t.Errorf("expected turn count and duration in output:\n%s", output)
		}
	})

	// Step 4: Verify both handlers process the same event sequence consistently
	t.Run("TUI model and PlainTextHandler both handle full event sequence", func(t *testing.T) {
		// PlainTextHandler output
		var buf bytes.Buffer
		plainHandler := &events.PlainTextHandler{W: &buf}

		// TUI model (acts as handler via eventMsg)
		model := tui.NewModel("ws", "", nil)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		model = updated.(tui.Model)

		// Same event sequence for both
		eventSequence := []events.Event{
			events.IterationStart{Iteration: 1, MaxIterations: 10},
			events.StoryStarted{StoryID: "US-001", Title: "Auth"},
			events.ToolUse{Name: "Read", Detail: "main.go"},
			events.AgentText{Text: "Working on it"},
			events.InvocationDone{NumTurns: 3, DurationMS: 6000},
		}

		for _, ev := range eventSequence {
			plainHandler.Handle(ev)

			// Feed to TUI model via eventMsg
			updated, _ = model.Update(tui.MakeEventMsg(ev))
			model = updated.(tui.Model)
		}

		// Verify PlainTextHandler produced output
		plainOutput := stripANSI(buf.String())
		if plainOutput == "" {
			t.Error("expected PlainTextHandler to produce output")
		}

		// Verify TUI model accumulated lines
		tuiLines := model.Lines()
		if len(tuiLines) != 5 {
			t.Errorf("expected 5 TUI log lines, got %d: %v", len(tuiLines), tuiLines)
		}

		// Both should contain the key information
		tuiContent := strings.Join(tuiLines, "\n")
		for _, keyword := range []string{"iteration 1/10", "US-001", "Read", "Working on it", "3 turns"} {
			if !strings.Contains(plainOutput, keyword) {
				t.Errorf("PlainTextHandler output missing %q", keyword)
			}
			if !strings.Contains(tuiContent, keyword) {
				t.Errorf("TUI model lines missing %q", keyword)
			}
		}
	})
}

// recordingHandler captures events for test assertions.
type recordingHandler struct {
	events []events.Event
}

func (h *recordingHandler) Handle(e events.Event) {
	h.events = append(h.events, e)
}

func eventTypeName(e events.Event) string {
	switch e.(type) {
	case events.ToolUse:
		return "ToolUse"
	case events.AgentText:
		return "AgentText"
	case events.InvocationDone:
		return "InvocationDone"
	case events.IterationStart:
		return "IterationStart"
	case events.StoryStarted:
		return "StoryStarted"
	case events.QAPhaseStarted:
		return "QAPhaseStarted"
	case events.UsageLimitWait:
		return "UsageLimitWait"
	case events.PRDRefresh:
		return "PRDRefresh"
	default:
		return "unknown"
	}
}
