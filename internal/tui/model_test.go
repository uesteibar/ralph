package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

func TestNewModel_SetsWorkspaceName(t *testing.T) {
	m := NewModel("my-feature", "", nil)
	if m.WorkspaceName() != "my-feature" {
		t.Errorf("expected workspace name 'my-feature', got %q", m.WorkspaceName())
	}
}

func TestNewModel_DefaultFocusRight(t *testing.T) {
	m := NewModel("ws", "", nil)
	if m.Focus() != focusRight {
		t.Errorf("expected default focus on right pane, got %d", m.Focus())
	}
}

func TestModel_HandleEvent_ToolUse_WithDetail(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.ToolUse{Name: "Read", Detail: "./file.go"})

	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	if !strings.Contains(m.Lines()[0], "→ Read") {
		t.Errorf("expected line to contain '→ Read', got %q", m.Lines()[0])
	}
	if !strings.Contains(m.Lines()[0], "./file.go") {
		t.Errorf("expected line to contain './file.go', got %q", m.Lines()[0])
	}
}

func TestModel_HandleEvent_ToolUse_WithoutDetail(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.ToolUse{Name: "Glob"})

	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	if !strings.Contains(m.Lines()[0], "→ Glob") {
		t.Errorf("expected line to contain '→ Glob', got %q", m.Lines()[0])
	}
}

func TestModel_HandleEvent_AgentText(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.AgentText{Text: "Hello\nWorld"})

	if len(m.Lines()) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(m.Lines()), m.Lines())
	}
	if !strings.Contains(m.Lines()[0], "Hello") {
		t.Errorf("expected first line to contain 'Hello', got %q", m.Lines()[0])
	}
	if !strings.Contains(m.Lines()[1], "World") {
		t.Errorf("expected second line to contain 'World', got %q", m.Lines()[1])
	}
}

func TestModel_HandleEvent_InvocationDone(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.InvocationDone{NumTurns: 5, DurationMS: 12000})

	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	line := m.Lines()[0]
	if !strings.Contains(line, "✓") {
		t.Error("expected checkmark")
	}
	if !strings.Contains(line, "5 turns") {
		t.Errorf("expected '5 turns', got %q", line)
	}
	if !strings.Contains(line, "12s") {
		t.Errorf("expected '12s', got %q", line)
	}
}

func TestModel_HandleEvent_IterationStart(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.IterationStart{Iteration: 3, MaxIterations: 20})

	if m.Iteration() != 3 {
		t.Errorf("expected iteration 3, got %d", m.Iteration())
	}
	if m.MaxIterations() != 20 {
		t.Errorf("expected maxIterations 20, got %d", m.MaxIterations())
	}
	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	if !strings.Contains(m.Lines()[0], "iteration 3/20") {
		t.Errorf("expected 'iteration 3/20', got %q", m.Lines()[0])
	}
}

func TestModel_HandleEvent_StoryStarted(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.StoryStarted{StoryID: "US-001", Title: "Build auth"})

	if m.CurrentStory() != "US-001: Build auth" {
		t.Errorf("expected current story 'US-001: Build auth', got %q", m.CurrentStory())
	}
	if m.ActiveStoryID() != "US-001" {
		t.Errorf("expected activeStoryID 'US-001', got %q", m.ActiveStoryID())
	}
	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	if !strings.Contains(m.Lines()[0], "working on US-001: Build auth") {
		t.Errorf("expected story started line, got %q", m.Lines()[0])
	}
}

func TestModel_HandleEvent_StoryStarted_UpdatesSidebar(t *testing.T) {
	m := NewModel("ws", "", nil)
	// Pre-populate sidebar
	m.sidebar.items = []sidebarItem{
		{id: "US-001", title: "First", passes: false},
		{id: "US-002", title: "Second", passes: false},
	}

	m.handleEvent(events.StoryStarted{StoryID: "US-002", Title: "Second"})

	items := m.Sidebar().Items()
	if items[0].active {
		t.Error("expected US-001 to not be active")
	}
	if !items[1].active {
		t.Error("expected US-002 to be active")
	}
}

func TestModel_HandleEvent_QAPhaseStarted(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.QAPhaseStarted{Phase: "verification"})

	if m.CurrentStory() != "QA verification" {
		t.Errorf("expected current story 'QA verification', got %q", m.CurrentStory())
	}
	if m.ActiveStoryID() != "" {
		t.Errorf("expected activeStoryID to be empty, got %q", m.ActiveStoryID())
	}
	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	if !strings.Contains(m.Lines()[0], "all stories pass — running QA verification") {
		t.Errorf("expected QA phase line, got %q", m.Lines()[0])
	}
}

func TestModel_HandleEvent_UsageLimitWait(t *testing.T) {
	m := NewModel("ws", "", nil)
	resetAt := time.Date(2026, 2, 5, 15, 30, 0, 0, time.UTC)
	m.handleEvent(events.UsageLimitWait{
		WaitDuration: 30 * time.Minute,
		ResetAt:      resetAt,
	})

	if len(m.Lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.Lines()))
	}
	line := m.Lines()[0]
	if !strings.Contains(line, "Usage limit reached") {
		t.Errorf("expected 'Usage limit reached', got %q", line)
	}
	if !strings.Contains(line, "⏳") {
		t.Errorf("expected hourglass emoji, got %q", line)
	}
}

func TestModel_MultipleEvents_AccumulateLines(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.handleEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
	m.handleEvent(events.StoryStarted{StoryID: "US-001", Title: "Auth"})
	m.handleEvent(events.ToolUse{Name: "Read", Detail: "main.go"})
	m.handleEvent(events.InvocationDone{NumTurns: 3, DurationMS: 5000})

	if len(m.Lines()) != 4 {
		t.Errorf("expected 4 lines, got %d: %v", len(m.Lines()), m.Lines())
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := NewModel("ws", "", nil)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	um := updated.(Model)

	if !um.ready {
		t.Error("expected model to be ready after WindowSizeMsg")
	}
}

func TestModel_Update_WindowSize_SetsSidebarDimensions(t *testing.T) {
	m := NewModel("ws", "", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	um := updated.(Model)

	if um.Sidebar().width != sidebarWidth {
		t.Errorf("expected sidebar width %d, got %d", sidebarWidth, um.Sidebar().width)
	}
	if um.Sidebar().height != 29 { // 30 - 1 (status bar)
		t.Errorf("expected sidebar height 29, got %d", um.Sidebar().height)
	}
}

func TestModel_Update_EventMsg(t *testing.T) {
	m := NewModel("ws", "", nil)
	// First make it ready
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = ready.(Model)

	updated, _ := m.Update(eventMsg{event: events.StoryStarted{StoryID: "US-002", Title: "TUI"}})
	um := updated.(Model)

	if um.CurrentStory() != "US-002: TUI" {
		t.Errorf("expected current story 'US-002: TUI', got %q", um.CurrentStory())
	}
}

func TestModel_Update_CtrlC_ReturnsQuit(t *testing.T) {
	m := NewModel("ws", "", nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected quit command from Ctrl+C")
	}
}

func TestModel_Update_Q_InitiatesGracefulStop(t *testing.T) {
	cancelled := false
	cancel := func() { cancelled = true }
	m := NewModel("ws", "", cancel)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	um := updated.(Model)

	if cmd != nil {
		t.Error("expected no quit command from q (graceful stop)")
	}
	if !um.Quitting() {
		t.Error("expected model to be in quitting state")
	}
	if !cancelled {
		t.Error("expected cancel function to be called")
	}
}

func TestModel_View_BeforeReady(t *testing.T) {
	m := NewModel("ws", "", nil)
	view := m.View()
	if !strings.Contains(view, "Initializing") {
		t.Errorf("expected 'Initializing' before ready, got %q", view)
	}
}

func TestModel_StatusBar_ShowsWorkspaceName(t *testing.T) {
	m := NewModel("login-page", "", nil)
	m.width = 80
	bar := m.statusBar()
	if !strings.Contains(bar, "login-page") {
		t.Errorf("expected status bar to contain 'login-page', got %q", bar)
	}
}

func TestModel_StatusBar_ShowsCurrentStory(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.width = 80
	m.currentStory = "US-001: Auth"
	bar := m.statusBar()
	if !strings.Contains(bar, "US-001: Auth") {
		t.Errorf("expected status bar to contain 'US-001: Auth', got %q", bar)
	}
}

func TestModel_StatusBar_ShowsIteration(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.width = 80
	m.iteration = 3
	m.maxIterations = 20
	bar := m.statusBar()
	if !strings.Contains(bar, "3/20") {
		t.Errorf("expected status bar to contain '3/20', got %q", bar)
	}
}

func TestHandler_ImplementsEventHandler(t *testing.T) {
	var _ events.EventHandler = &Handler{}
}

func TestHandler_SendsEventMsg(t *testing.T) {
	// We can't easily test p.Send without a running program,
	// but we can verify the Handler has the expected structure.
	h := NewHandler(nil)
	// Should not panic when program is nil
	h.Handle(events.ToolUse{Name: "Read"})
}

func TestModel_Init_ReturnsNilWithoutPRDPath(t *testing.T) {
	m := NewModel("ws", "", nil)
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected Init to return nil without PRD path")
	}
}

func TestModel_Init_ReturnsCmdWithPRDPath(t *testing.T) {
	m := NewModel("ws", "/some/path/prd.json", nil)
	cmd := m.Init()
	if cmd == nil {
		t.Error("expected Init to return a command when PRD path is set")
	}
}

func TestModel_HandleEvent_ToolUse_MultipleTools(t *testing.T) {
	m := NewModel("ws", "", nil)
	tools := []string{"Read", "Edit", "Bash", "Write", "Glob"}
	for _, tool := range tools {
		m.handleEvent(events.ToolUse{Name: tool, Detail: fmt.Sprintf("detail-%s", tool)})
	}

	if len(m.Lines()) != len(tools) {
		t.Errorf("expected %d lines, got %d", len(tools), len(m.Lines()))
	}
	for i, tool := range tools {
		if !strings.Contains(m.Lines()[i], tool) {
			t.Errorf("line %d: expected to contain %q, got %q", i, tool, m.Lines()[i])
		}
	}
}

// --- Split-pane and keyboard navigation tests ---

func TestModel_TabKey_SwitchesFocus(t *testing.T) {
	m := NewModel("ws", "", nil)
	if m.Focus() != focusRight {
		t.Fatalf("expected initial focus right")
	}

	// Tab to left
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Focus() != focusLeft {
		t.Errorf("expected focus left after tab, got %d", m.Focus())
	}
	if !m.Sidebar().focused {
		t.Error("expected sidebar to be focused")
	}

	// Tab back to right
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Focus() != focusRight {
		t.Errorf("expected focus right after second tab, got %d", m.Focus())
	}
	if m.Sidebar().focused {
		t.Error("expected sidebar to not be focused")
	}
}

func TestModel_ArrowKeys_LeftFocus_NavigatesSidebar(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.sidebar.items = []sidebarItem{
		{id: "US-001", title: "First"},
		{id: "US-002", title: "Second"},
		{id: "US-003", title: "Third"},
	}
	m.focus = focusLeft
	m.sidebar.focused = true

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.Sidebar().Cursor() != 1 {
		t.Errorf("expected cursor at 1 after down, got %d", m.Sidebar().Cursor())
	}

	// Move down with j
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.Sidebar().Cursor() != 2 {
		t.Errorf("expected cursor at 2 after j, got %d", m.Sidebar().Cursor())
	}

	// Move up with k
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(Model)
	if m.Sidebar().Cursor() != 1 {
		t.Errorf("expected cursor at 1 after k, got %d", m.Sidebar().Cursor())
	}

	// Move up with arrow
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.Sidebar().Cursor() != 0 {
		t.Errorf("expected cursor at 0 after up, got %d", m.Sidebar().Cursor())
	}
}

func TestModel_ArrowKeys_RightFocus_DoesNotNavigateSidebar(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.sidebar.items = []sidebarItem{
		{id: "US-001", title: "First"},
		{id: "US-002", title: "Second"},
	}
	m.focus = focusRight

	// Down key should not move sidebar cursor
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.Sidebar().Cursor() != 0 {
		t.Errorf("expected sidebar cursor to stay at 0, got %d", m.Sidebar().Cursor())
	}
}

func TestModel_PRDLoadedMsg_UpdatesSidebar(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Login works", Passes: true},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	items := m.Sidebar().Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 sidebar items, got %d", len(items))
	}

	// Stories
	if items[0].id != "US-001" || !items[0].passes {
		t.Errorf("expected US-001 passing, got %+v", items[0])
	}
	if items[1].id != "US-002" || items[1].passes {
		t.Errorf("expected US-002 not passing, got %+v", items[1])
	}

	// Integration test
	if items[2].id != "IT-001" || !items[2].isTest || !items[2].passes {
		t.Errorf("expected IT-001 as passing test, got %+v", items[2])
	}
}

func TestModel_PRDLoadedMsg_PreservesActiveStory(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.activeStoryID = "US-002"

	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	items := m.Sidebar().Items()
	if !items[1].active {
		t.Error("expected US-002 to be active after PRD reload")
	}
	if items[0].active {
		t.Error("expected US-001 to not be active")
	}
}

func TestModel_PRDRefreshEvent_TriggersPRDRead(t *testing.T) {
	// Create a temp PRD file
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	testPRD := prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}
	data, _ := json.MarshalIndent(testPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	m := NewModel("ws", prdPath, nil)
	// Make model ready
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Send PRDRefresh event
	updated, cmd := m.Update(eventMsg{event: events.PRDRefresh{}})
	m = updated.(Model)

	// Should return a command to read the PRD
	if cmd == nil {
		t.Fatal("expected a command from PRDRefresh event")
	}

	// Execute the command and check the result
	msg := cmd()
	if msg == nil {
		t.Fatal("expected command to return a prdLoadedMsg")
	}
	loaded, ok := msg.(prdLoadedMsg)
	if !ok {
		t.Fatalf("expected prdLoadedMsg, got %T", msg)
	}
	if len(loaded.prd.UserStories) != 1 {
		t.Errorf("expected 1 story, got %d", len(loaded.prd.UserStories))
	}
}

func TestModel_View_ShowsSplitPaneLayout(t *testing.T) {
	m := NewModel("ws", "", nil)
	// Make ready
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Load PRD
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}
	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	// Add a log line
	m.handleEvent(events.ToolUse{Name: "Read", Detail: "main.go"})
	m.viewport.SetContent(strings.Join(m.lines, "\n"))

	view := m.View()
	// Should contain sidebar content
	if !strings.Contains(view, "Stories & Tests") {
		t.Errorf("expected view to contain sidebar title 'Stories & Tests'")
	}
	if !strings.Contains(view, "US-001") {
		t.Errorf("expected view to contain 'US-001'")
	}
	if !strings.Contains(view, "US-002") {
		t.Errorf("expected view to contain 'US-002'")
	}
}

// --- Overlay integration tests ---

func TestModel_EnterKey_OpensStoryOverlay(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Description: "Implement auth", Passes: true,
				AcceptanceCriteria: []string{"Login works", "Logout works"}, Notes: "Use JWT"},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}

	// Load PRD
	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	// Make ready and set dimensions
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Switch focus to sidebar
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	// Press Enter on first item (US-001)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.Overlay().visible {
		t.Fatal("expected overlay to be visible after Enter")
	}
	if !strings.Contains(m.Overlay().content, "US-001") {
		t.Error("expected overlay to contain story ID")
	}
	if !strings.Contains(m.Overlay().content, "Auth") {
		t.Error("expected overlay to contain story title")
	}
	if !strings.Contains(m.Overlay().content, "Implement auth") {
		t.Error("expected overlay to contain description")
	}
	if !strings.Contains(m.Overlay().content, "Login works") {
		t.Error("expected overlay to contain acceptance criteria")
	}
	if !strings.Contains(m.Overlay().content, "Use JWT") {
		t.Error("expected overlay to contain notes")
	}
}

func TestModel_EnterKey_OpensTestOverlay(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Login test",
				Steps:   []string{"Go to login", "Enter creds", "Verify"},
				Passes:  false,
				Failure: "Button not found",
				Notes:   "Needs headless browser"},
		},
	}

	// Load PRD
	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	// Make ready
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Switch to sidebar and navigate to test item
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

	// Press Enter on IT-001
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.Overlay().visible {
		t.Fatal("expected overlay to be visible")
	}
	if !strings.Contains(m.Overlay().content, "IT-001") {
		t.Error("expected overlay to contain test ID")
	}
	if !strings.Contains(m.Overlay().content, "Login test") {
		t.Error("expected overlay to contain description")
	}
	if !strings.Contains(m.Overlay().content, "1. Go to login") {
		t.Error("expected overlay to contain numbered steps")
	}
	if !strings.Contains(m.Overlay().content, "Button not found") {
		t.Error("expected overlay to contain failure details")
	}
}

func TestModel_EscKey_ClosesOverlay(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.Overlay().visible {
		t.Fatal("expected overlay to be visible")
	}

	// Press Esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.Overlay().visible {
		t.Error("expected overlay to be hidden after Esc")
	}
}

func TestModel_OverlayVisible_BlocksOtherKeys(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	// Tab should not switch focus
	cursorBefore := m.Sidebar().Cursor()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Overlay().visible != true {
		t.Error("expected overlay to remain visible after tab")
	}
	if m.Sidebar().Cursor() != cursorBefore {
		t.Error("expected sidebar cursor unchanged while overlay open")
	}
}

func TestModel_EnterKey_RightFocus_NoOverlay(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}
	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	// Enter with right focus should not open overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.Overlay().visible {
		t.Error("expected overlay not to open when right pane focused")
	}
}

func TestModel_PRDLoadedMsg_StoresCurrentPRD(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)

	if m.CurrentPRD() == nil {
		t.Fatal("expected currentPRD to be set")
	}
	if m.CurrentPRD().UserStories[0].ID != "US-001" {
		t.Error("expected stored PRD to contain US-001")
	}
}

func TestModel_View_ShowsOverlayWhenVisible(t *testing.T) {
	m := NewModel("ws", "", nil)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Description: "Build auth", Passes: true},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	view := m.View()
	// When overlay is visible, view should show overlay content
	if !strings.Contains(view, "US-001") {
		t.Error("expected view to contain overlay content")
	}
	// Should have border
	if !strings.Contains(view, "╭") {
		t.Error("expected rounded border in overlay view")
	}
}

// --- IT-004: Story list reflects PRD state and updates on change ---

func TestIT004_StoryListReflectsPRDState(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")

	// Initial PRD: 2 pending, 1 passing
	initialPRD := prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Priority: 1, Passes: false},
			{ID: "US-002", Title: "TUI", Priority: 2, Passes: false},
			{ID: "US-003", Title: "Split pane", Priority: 3, Passes: true},
		},
	}
	data, _ := json.MarshalIndent(initialPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	m := NewModel("ws", prdPath, nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Execute Init() to read the PRD
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Verify 3 stories with correct indicators
	items := m.Sidebar().Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].passes || items[1].passes {
		t.Error("expected first two stories to be failing")
	}
	if !items[2].passes {
		t.Error("expected third story to be passing")
	}

	// Simulate PRD update: mark US-002 as passing
	updatedPRD := initialPRD
	updatedPRD.UserStories[1].Passes = true
	data, _ = json.MarshalIndent(updatedPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	// Trigger refresh
	updated2, cmd := m.Update(eventMsg{event: events.PRDRefresh{}})
	m = updated2.(Model)
	if cmd != nil {
		msg := cmd()
		updated3, _ := m.Update(msg)
		m = updated3.(Model)
	}

	// Verify: 2 passing, 1 pending
	items = m.Sidebar().Items()
	passing := 0
	for _, item := range items {
		if item.passes {
			passing++
		}
	}
	if passing != 2 {
		t.Errorf("expected 2 passing after update, got %d", passing)
	}
}

// --- IT-005: Story detail overlay shows correct data ---

func TestIT005_StoryDetailOverlayShowsCorrectData(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")

	testPRD := prd.PRD{
		UserStories: []prd.Story{
			{
				ID:          "US-042",
				Title:       "User authentication",
				Description: "Implement secure user login and registration",
				AcceptanceCriteria: []string{
					"Users can register with email",
					"Users can log in with password",
					"Sessions persist across refreshes",
				},
				Priority: 1,
				Passes:   true,
				Notes:    "Use bcrypt for hashing",
			},
		},
	}
	data, _ := json.MarshalIndent(testPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	m := NewModel("ws", prdPath, nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Load PRD via Init
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Navigate to the story and press Enter
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	// Verify overlay mode
	if !m.Overlay().visible {
		t.Fatal("expected overlay to be visible")
	}

	// Verify rendered view contains story details
	view := m.View()
	checks := []string{
		"US-042",
		"User authentication",
		"Implement secure user login and registration",
		"Users can register with email",
		"Users can log in with password",
		"Sessions persist across refreshes",
		"Use bcrypt for hashing",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Errorf("expected view to contain %q", check)
		}
	}

	// Press Esc and verify overlay closes
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.Overlay().visible {
		t.Error("expected overlay to be hidden after Esc")
	}

	// View should no longer show overlay border
	view = m.View()
	if !strings.Contains(view, "Stories & Tests") {
		t.Error("expected split-pane view to be restored after Esc")
	}
}

// --- IT-006: Integration test detail overlay shows failure info ---

func TestIT006_IntegrationTestDetailOverlayShowsFailureInfo(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")

	testPRD := prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
		IntegrationTests: []prd.IntegrationTest{
			{
				ID:          "IT-099",
				Description: "End-to-end checkout flow",
				Steps: []string{
					"Add item to cart",
					"Proceed to checkout",
					"Enter payment details",
					"Confirm order",
				},
				Passes:  false,
				Failure: "Payment gateway returned 503 Service Unavailable",
				Notes:   "Requires mock payment service",
			},
		},
	}
	data, _ := json.MarshalIndent(testPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	m := NewModel("ws", prdPath, nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Load PRD
	cmd := m.Init()
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Navigate to the integration test (item index 1, after US-001)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	// Verify overlay is visible
	if !m.Overlay().visible {
		t.Fatal("expected overlay to be visible")
	}

	// Verify rendered view contains test details
	view := m.View()
	checks := []string{
		"IT-099",
		"End-to-end checkout flow",
		"1. Add item to cart",
		"2. Proceed to checkout",
		"3. Enter payment details",
		"4. Confirm order",
		"FAIL",
		"Payment gateway returned 503 Service Unavailable",
		"Requires mock payment service",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Errorf("expected view to contain %q", check)
		}
	}

	// Press Esc to close
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.Overlay().visible {
		t.Error("expected overlay to be closed after Esc")
	}
}

// --- Help overlay tests ---

func TestModel_QuestionMark_OpensHelpOverlay(t *testing.T) {
	m := NewModel("ws", "", nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	if !m.HelpOverlay().visible {
		t.Fatal("expected help overlay to be visible after ? key")
	}
	content := m.HelpOverlay().content
	shortcuts := []string{"Tab", "Enter", "Esc", "q", "?", "Ctrl+C"}
	for _, s := range shortcuts {
		if !strings.Contains(content, s) {
			t.Errorf("expected help overlay to contain shortcut %q", s)
		}
	}
}

func TestModel_HelpOverlay_DismissWithEsc(t *testing.T) {
	m := NewModel("ws", "", nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.HelpOverlay().visible {
		t.Fatal("expected help overlay to be visible")
	}

	// Close with Esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.HelpOverlay().visible {
		t.Error("expected help overlay to be hidden after Esc")
	}
}

func TestModel_HelpOverlay_DismissWithQuestionMark(t *testing.T) {
	m := NewModel("ws", "", nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.HelpOverlay().visible {
		t.Fatal("expected help overlay to be visible")
	}

	// Close with ? again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.HelpOverlay().visible {
		t.Error("expected help overlay to be hidden after second ? key")
	}
}

func TestModel_HelpOverlay_BlocksOtherKeys(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.sidebar.items = []sidebarItem{
		{id: "US-001", title: "First"},
		{id: "US-002", title: "Second"},
	}
	m.focus = focusLeft
	m.sidebar.focused = true
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	// Tab should not switch focus
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if !m.HelpOverlay().visible {
		t.Error("expected help overlay to remain visible after tab")
	}
}

func TestModel_HelpOverlay_CtrlC_StillWorks(t *testing.T) {
	m := NewModel("ws", "", nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	// Ctrl+C should still quit even with help overlay open
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected Ctrl+C to return quit command even with help overlay open")
	}
}

func TestModel_View_ShowsHelpOverlayWhenVisible(t *testing.T) {
	m := NewModel("ws", "", nil)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Error("expected view to contain 'Keyboard Shortcuts' when help overlay is visible")
	}
	if !strings.Contains(view, "Tab") {
		t.Error("expected view to contain 'Tab' shortcut")
	}
}

// --- Graceful stop tests ---

func TestModel_GracefulStop_CallsCancelFunc(t *testing.T) {
	cancelled := false
	cancel := func() { cancelled = true }
	m := NewModel("ws", "", cancel)

	m.initiateGracefulStop()

	if !m.quitting {
		t.Error("expected quitting to be true")
	}
	if !cancelled {
		t.Error("expected cancel function to be called")
	}
}

func TestModel_GracefulStop_Idempotent(t *testing.T) {
	callCount := 0
	cancel := func() { callCount++ }
	m := NewModel("ws", "", cancel)

	m.initiateGracefulStop()
	m.initiateGracefulStop()

	if callCount != 1 {
		t.Errorf("expected cancel called once, got %d", callCount)
	}
}

func TestModel_GracefulStop_NilCancelFunc(t *testing.T) {
	m := NewModel("ws", "", nil)
	// Should not panic
	m.initiateGracefulStop()
	if !m.quitting {
		t.Error("expected quitting to be true even with nil cancel func")
	}
}

func TestModel_StatusBar_ShowsStopping(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.width = 80
	m.quitting = true
	bar := m.statusBar()
	if !strings.Contains(bar, "Stopping...") {
		t.Errorf("expected status bar to contain 'Stopping...', got %q", bar)
	}
}

func TestModel_StatusBar_NoStoppingWhenNotQuitting(t *testing.T) {
	m := NewModel("ws", "", nil)
	m.width = 80
	bar := m.statusBar()
	if strings.Contains(bar, "Stopping") {
		t.Error("expected status bar to not contain 'Stopping' when not quitting")
	}
}

func TestModel_QKey_InOverlay_InitiatesGracefulStop(t *testing.T) {
	cancelled := false
	cancel := func() { cancelled = true }
	m := NewModel("ws", "", cancel)
	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}

	updated, _ := m.Update(prdLoadedMsg{prd: testPRD})
	m = updated.(Model)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Open detail overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.Overlay().visible {
		t.Fatal("expected detail overlay to be visible")
	}

	// Press q while in overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)

	if !m.Quitting() {
		t.Error("expected quitting state after q in overlay")
	}
	if !cancelled {
		t.Error("expected cancel called after q in overlay")
	}
}

// --- IT-007: Keyboard shortcuts and graceful stop ---

func TestIT007_KeyboardShortcutsAndGracefulStop(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")

	testPRD := prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}
	data, _ := json.MarshalIndent(testPRD, "", "  ")
	os.WriteFile(prdPath, data, 0644)

	cancelled := false
	cancel := func() { cancelled = true }
	m := NewModel("ws", prdPath, cancel)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(Model)

	// Load PRD via Init
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Step 1: ? key opens help overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)

	if !m.HelpOverlay().visible {
		t.Fatal("expected help overlay to be visible after ? key")
	}

	view := m.View()
	shortcutChecks := []string{"Tab", "Enter", "Esc", "q", "?", "Ctrl+C"}
	for _, sc := range shortcutChecks {
		if !strings.Contains(view, sc) {
			t.Errorf("expected help view to contain shortcut %q", sc)
		}
	}

	// Step 2: Esc closes the help overlay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.HelpOverlay().visible {
		t.Error("expected help overlay to be hidden after Esc")
	}

	// Step 3: q key initiates graceful quit
	updated, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)

	if !m.Quitting() {
		t.Error("expected model to be in quitting state after q")
	}
	if !cancelled {
		t.Error("expected cancel function to be called on q")
	}
	if cmd2 != nil {
		t.Error("expected no immediate quit command from q (graceful stop waits for loop)")
	}

	// Verify status bar shows Stopping...
	bar := m.statusBar()
	if !strings.Contains(bar, "Stopping...") {
		t.Errorf("expected status bar to contain 'Stopping...', got %q", bar)
	}
}
