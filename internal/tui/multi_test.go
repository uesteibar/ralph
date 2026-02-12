package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

func makeTestWorkspaces() []WorkspaceInfo {
	return []WorkspaceInfo{
		{
			Name:    "auth-feature",
			Branch:  "ralph/auth-feature",
			Running: true,
			PRD: &prd.PRD{
				UserStories: []prd.Story{
					{ID: "US-001", Title: "Login", Passes: true},
					{ID: "US-002", Title: "Logout", Passes: false},
				},
				IntegrationTests: []prd.IntegrationTest{
					{ID: "IT-001", Description: "Login flow", Passes: false},
				},
			},
			LogsDir: "/tmp/logs/auth",
			WsPath:  "/tmp/ws/auth",
		},
		{
			Name:    "payment",
			Branch:  "ralph/payment",
			Running: false,
			PRD: &prd.PRD{
				UserStories: []prd.Story{
					{ID: "US-001", Title: "Checkout", Passes: true},
				},
			},
			LogsDir: "/tmp/logs/payment",
			WsPath:  "/tmp/ws/payment",
		},
		{
			Name:    "dashboard",
			Branch:  "ralph/dashboard",
			Running: false,
			PRD:     nil,
			LogsDir: "/tmp/logs/dashboard",
			WsPath:  "/tmp/ws/dashboard",
		},
		{
			Name:    "search",
			Branch:  "ralph/search",
			Running: false,
			PRD: &prd.PRD{
				UserStories: []prd.Story{
					{ID: "US-001", Title: "Basic search", Passes: true},
					{ID: "US-002", Title: "Advanced search", Passes: false},
				},
			},
			LogsDir: "/tmp/logs/search",
			WsPath:  "/tmp/ws/search",
		},
	}
}

func TestNewMultiModel_InitialState(t *testing.T) {
	workspaces := makeTestWorkspaces()
	m := NewMultiModel(workspaces)

	if m.Mode() != modeOverview {
		t.Error("expected initial mode to be overview")
	}
	if m.MultiCursor() != 0 {
		t.Errorf("expected initial cursor 0, got %d", m.MultiCursor())
	}
	if len(m.Workspaces()) != 4 {
		t.Errorf("expected 4 workspaces, got %d", len(m.Workspaces()))
	}
	if m.DrillModel() != nil {
		t.Error("expected no drill model initially")
	}
}

func TestMultiModel_CursorNavigation_Down(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	if m.MultiCursor() != 1 {
		t.Errorf("expected cursor 1, got %d", m.MultiCursor())
	}

	// Move down again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(MultiModel)
	if m.MultiCursor() != 2 {
		t.Errorf("expected cursor 2, got %d", m.MultiCursor())
	}

	// Move to last item
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	if m.MultiCursor() != 3 {
		t.Errorf("expected cursor 3, got %d", m.MultiCursor())
	}

	// Should not go past last item
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	if m.MultiCursor() != 3 {
		t.Errorf("expected cursor to stay at 3, got %d", m.MultiCursor())
	}
}

func TestMultiModel_CursorNavigation_Up(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Move down first
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)

	// Move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(MultiModel)
	if m.MultiCursor() != 1 {
		t.Errorf("expected cursor 1, got %d", m.MultiCursor())
	}

	// Move up with k
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(MultiModel)
	if m.MultiCursor() != 0 {
		t.Errorf("expected cursor 0, got %d", m.MultiCursor())
	}

	// Should not go before first item
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(MultiModel)
	if m.MultiCursor() != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", m.MultiCursor())
	}
}

func TestMultiModel_Q_Quits(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("expected quit command from q key")
	}
}

func TestMultiModel_Esc_Quits(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Error("expected quit command from Esc key")
	}
}

func TestMultiModel_CtrlC_Quits(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected quit command from Ctrl+C")
	}
}

func TestMultiModel_Enter_DrillsDown(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	if m.Mode() != modeDrillDown {
		t.Error("expected drill-down mode after Enter")
	}
	if m.DrillModel() == nil {
		t.Fatal("expected drill model to be set")
	}
	if m.DrillModel().WorkspaceName() != "auth-feature" {
		t.Errorf("expected drill model workspace 'auth-feature', got %q", m.DrillModel().WorkspaceName())
	}
}

func TestMultiModel_Esc_ReturnFromDrillDown(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	if m.Mode() != modeDrillDown {
		t.Fatal("expected drill-down mode")
	}

	// Press Esc to return
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(MultiModel)

	if m.Mode() != modeOverview {
		t.Error("expected overview mode after Esc")
	}
	if m.DrillModel() != nil {
		t.Error("expected drill model to be nil after return")
	}
}

func TestMultiModel_HelpOverlay(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(MultiModel)

	if !m.MultiHelpOverlay().visible {
		t.Fatal("expected help overlay to be visible after ? key")
	}

	content := m.MultiHelpOverlay().content
	shortcuts := []string{"Enter", "Esc", "q", "?", "Ctrl+C"}
	for _, s := range shortcuts {
		if !strings.Contains(content, s) {
			t.Errorf("expected help overlay to contain shortcut %q", s)
		}
	}

	// Close with Esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(MultiModel)
	if m.MultiHelpOverlay().visible {
		t.Error("expected help overlay to be hidden after Esc")
	}
}

func TestMultiModel_HelpOverlay_DismissWithQuestionMark(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Open
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(MultiModel)
	// Close with ?
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(MultiModel)

	if m.MultiHelpOverlay().visible {
		t.Error("expected help overlay to be hidden after second ?")
	}
}

func TestMultiModel_HelpOverlay_BlocksOtherKeys(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(MultiModel)

	// Down key should not move cursor
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	if m.MultiCursor() != 0 {
		t.Errorf("expected cursor to stay at 0 while help is open, got %d", m.MultiCursor())
	}
}

func TestMultiModel_View_BeforeReady(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	view := m.View()
	if !strings.Contains(view, "Initializing") {
		t.Errorf("expected 'Initializing' before ready, got %q", view)
	}
}

func TestMultiModel_View_ShowsWorkspaceList(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "Workspaces") {
		t.Error("expected view to contain 'Workspaces' title")
	}
	if !strings.Contains(view, "auth-feature") {
		t.Error("expected view to contain 'auth-feature'")
	}
	if !strings.Contains(view, "payment") {
		t.Error("expected view to contain 'payment'")
	}
	if !strings.Contains(view, "dashboard") {
		t.Error("expected view to contain 'dashboard'")
	}
}

func TestMultiModel_View_ShowsPassSummary(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	view := m.View()
	// auth-feature: 1/2 stories, 0/1 tests
	if !strings.Contains(view, "1/2 stories") {
		t.Error("expected view to contain '1/2 stories' for auth-feature")
	}
	if !strings.Contains(view, "0/1 tests") {
		t.Error("expected view to contain '0/1 tests' for auth-feature")
	}
	// payment: 1/1 stories, no integration tests
	if !strings.Contains(view, "1/1 stories") {
		t.Error("expected view to contain '1/1 stories' for payment")
	}
	// dashboard has no PRD — should not show summary
}

func TestMultiModel_View_ShowsRunningIndicators(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	view := m.View()
	// Running workspace should have ● indicator
	if !strings.Contains(view, "●") {
		t.Error("expected view to contain running indicator ●")
	}
	// Stopped workspaces should have ○ indicator
	if !strings.Contains(view, "○") {
		t.Error("expected view to contain stopped indicator ○")
	}
}

func TestMultiModel_View_ShowsStoriesAndTests(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "User Stories") {
		t.Error("expected view to contain 'User Stories' section")
	}
	if !strings.Contains(view, "Integration Tests") {
		t.Error("expected view to contain 'Integration Tests' section")
	}
	if !strings.Contains(view, "US-001") {
		t.Error("expected view to contain 'US-001'")
	}
	if !strings.Contains(view, "IT-001") {
		t.Error("expected view to contain 'IT-001'")
	}
}

func TestMultiModel_View_ShowsNoPRDForEmptyWorkspace(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to dashboard (no PRD)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "No PRD loaded") {
		t.Error("expected 'No PRD loaded' for workspace without PRD")
	}
}

func TestMultiModel_MultiPrdLoadedMsg_UpdatesWorkspace(t *testing.T) {
	workspaces := []WorkspaceInfo{
		{Name: "ws-1", WsPath: "/tmp/ws1"},
	}
	m := NewMultiModel(workspaces)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	testPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
	}

	updated, _ := m.Update(multiPrdLoadedMsg{index: 0, prd: testPRD})
	m = updated.(MultiModel)

	if m.Workspaces()[0].PRD == nil {
		t.Fatal("expected PRD to be loaded for workspace 0")
	}
	if m.Workspaces()[0].PRD.UserStories[0].ID != "US-001" {
		t.Error("expected PRD to contain US-001")
	}
}

func TestMultiModel_HandleLogEvent_AccumulatesLines(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	updated, _ := m.Update(multiLogEventMsg{index: 0, event: events.IterationStart{Iteration: 1, MaxIterations: 5}})
	m = updated.(MultiModel)
	updated, _ = m.Update(multiLogEventMsg{index: 0, event: events.StoryStarted{StoryID: "US-001", Title: "Login"}})
	m = updated.(MultiModel)

	lines := m.LogLines(0)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "iteration 1/5") {
		t.Errorf("expected first line to contain 'iteration 1/5', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "working on US-001") {
		t.Errorf("expected second line to contain 'working on US-001', got %q", lines[1])
	}
}

func TestMultiModel_HandleLogEvent_PerWorkspace(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Add events for different workspaces
	updated, _ := m.Update(multiLogEventMsg{index: 0, event: events.ToolUse{Name: "Read"}})
	m = updated.(MultiModel)
	updated, _ = m.Update(multiLogEventMsg{index: 1, event: events.ToolUse{Name: "Write"}})
	m = updated.(MultiModel)

	if len(m.LogLines(0)) != 1 {
		t.Errorf("expected 1 log line for ws 0, got %d", len(m.LogLines(0)))
	}
	if len(m.LogLines(1)) != 1 {
		t.Errorf("expected 1 log line for ws 1, got %d", len(m.LogLines(1)))
	}
	if !strings.Contains(m.LogLines(0)[0], "Read") {
		t.Error("expected ws 0 log to contain 'Read'")
	}
	if !strings.Contains(m.LogLines(1)[0], "Write") {
		t.Error("expected ws 1 log to contain 'Write'")
	}
}

func TestMultiModel_DrillDown_D_ReturnsToOverview(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	if m.Mode() != modeDrillDown {
		t.Fatal("expected drill-down mode")
	}

	// Press d to return to overview (detach from drill-down)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(MultiModel)

	if m.Mode() != modeOverview {
		t.Error("expected overview mode after d in drill-down")
	}
	if m.DrillModel() != nil {
		t.Error("expected drill model to be nil after d")
	}
}

func TestMultiModel_DrillDown_CtrlC_Quits(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Ctrl+C should quit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected Ctrl+C to return quit command in drill-down")
	}
}

func TestMultiModel_View_InDrillDown_ShowsSingleWorkspaceTUI(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, cmds := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Execute the batch commands (Init + WindowSizeMsg) so the drill model gets ready
	if cmds != nil {
		msg := cmds()
		if msg != nil {
			// Batch returns a batchMsg; process sub-messages
			updated, _ = m.Update(msg)
			m = updated.(MultiModel)
		}
	}

	// The drill model should exist
	if m.DrillModel() == nil {
		t.Fatal("expected drill model to exist")
	}
}

func TestMultiModel_EmptyWorkspaces(t *testing.T) {
	m := NewMultiModel([]WorkspaceInfo{})
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Should not panic on Enter with no workspaces
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	if m.Mode() != modeOverview {
		t.Error("expected to stay in overview with empty workspaces")
	}
}

func TestMultiModel_WindowSizeMsg_SetsReady(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(MultiModel)

	if !m.ready {
		t.Error("expected model to be ready after WindowSizeMsg")
	}
}

func TestMultiModel_View_ShowsHelpOverlayWhenVisible(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Open help
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Error("expected view to contain 'Keyboard Shortcuts' when help is visible")
	}
}

func TestMultiModel_View_StatusBar(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "auth-feature") {
		t.Error("expected status bar to contain selected workspace name")
	}
}

func TestRenderWorkspaceSummary_AllPassing(t *testing.T) {
	ws := WorkspaceInfo{
		PRD: &prd.PRD{
			UserStories: []prd.Story{
				{ID: "US-001", Passes: true},
				{ID: "US-002", Passes: true},
			},
			IntegrationTests: []prd.IntegrationTest{
				{ID: "IT-001", Passes: true},
			},
		},
	}
	summary := renderWorkspaceSummary(ws)
	if !strings.Contains(summary, "2/2 stories") {
		t.Errorf("expected '2/2 stories', got %q", summary)
	}
	if !strings.Contains(summary, "1/1 tests") {
		t.Errorf("expected '1/1 tests', got %q", summary)
	}
}

func TestRenderWorkspaceSummary_SomeFailing(t *testing.T) {
	ws := WorkspaceInfo{
		PRD: &prd.PRD{
			UserStories: []prd.Story{
				{ID: "US-001", Passes: true},
				{ID: "US-002", Passes: false},
				{ID: "US-003", Passes: false},
			},
		},
	}
	summary := renderWorkspaceSummary(ws)
	if !strings.Contains(summary, "1/3 stories") {
		t.Errorf("expected '1/3 stories', got %q", summary)
	}
	// No integration tests — should not mention tests
	if strings.Contains(summary, "tests") {
		t.Errorf("expected no test count when no integration tests, got %q", summary)
	}
}

func TestRenderMultiHelpOverlay_ContainsAllShortcuts(t *testing.T) {
	content := renderMultiHelpOverlay()
	shortcuts := []string{"Enter", "Esc", "a", "q", "?", "Ctrl+C"}
	for _, s := range shortcuts {
		if !strings.Contains(content, s) {
			t.Errorf("expected help content to contain %q", s)
		}
	}
}

// --- Attach capability tests ---

func TestMultiModel_DrillDown_Q_ReturnsToOverview_WhenNotAttached(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	if m.Mode() != modeDrillDown {
		t.Fatal("expected drill-down mode")
	}

	// Press q — should return to overview without affecting daemon
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(MultiModel)

	if m.Mode() != modeOverview {
		t.Error("expected overview mode after q in non-attached drill-down")
	}
	if m.DrillModel() != nil {
		t.Error("expected drill model to be nil after q")
	}
	if cmd != nil {
		t.Error("expected no command (no quit) from q in non-attached mode")
	}
}

func TestMultiModel_DrillDown_A_ShowsAttachConfirmation_Running(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// auth-feature is running (index 0)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Press a to trigger attach
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)

	if !m.ConfirmingAttach() {
		t.Error("expected confirmingAttach after a key on running workspace")
	}
}

func TestMultiModel_DrillDown_A_NoEffect_Stopped(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to payment (stopped, index 1)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Press a — should have no effect on stopped workspace
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)

	if m.ConfirmingAttach() {
		t.Error("expected no attach confirmation for stopped workspace")
	}
}

func TestMultiModel_AttachConfirm_Y_EntersAttachedMode(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down on running workspace
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Press a then y
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	if !m.Attached() {
		t.Error("expected attached mode after y confirmation")
	}
	if m.ConfirmingAttach() {
		t.Error("expected confirmingAttach to be cleared")
	}
}

func TestMultiModel_AttachConfirm_N_CancelsAttach(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(MultiModel)

	if m.Attached() {
		t.Error("expected not attached after n")
	}
	if m.ConfirmingAttach() {
		t.Error("expected confirmingAttach to be cleared after n")
	}
}

func TestMultiModel_AttachConfirm_Esc_CancelsAttach(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(MultiModel)

	if m.Attached() {
		t.Error("expected not attached after Esc")
	}
}

func TestMultiModel_Attached_Q_ShowsStopConfirmation(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	// Press q — should delegate to embedded model (shows stop confirmation)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(MultiModel)

	if m.DrillModel() == nil {
		t.Fatal("expected drill model to exist")
	}
	if !m.DrillModel().ConfirmingStop() {
		t.Error("expected drill model to be in confirmingStop after q in attached mode")
	}
}

func TestMultiModel_Attached_Q_Y_StopsDaemon(t *testing.T) {
	stopCalled := false
	m := NewMultiModel(makeTestWorkspaces())
	m.SetMakeStopFn(func(wsPath string) func() {
		return func() { stopCalled = true }
	})
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	// q then y — should call stopDaemonFn
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	if !stopCalled {
		t.Error("expected stopDaemonFn to be called")
	}
}

func TestMultiModel_Attached_D_DetachesBackToOverview(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	// Press d — should detach back to overview
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(MultiModel)

	if m.Mode() != modeOverview {
		t.Error("expected overview mode after d in attached mode")
	}
	if m.Attached() {
		t.Error("expected attached to be cleared after detach")
	}
}

func TestMultiModel_Attached_StatusBar_ShowsAttachedBadge(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	if m.DrillModel() == nil {
		t.Fatal("expected drill model to exist")
	}
	bar := m.DrillModel().statusBar()
	if !strings.Contains(bar, "ATTACHED") {
		t.Errorf("expected status bar to contain 'ATTACHED', got %q", bar)
	}
}

func TestMultiModel_NonAttached_StatusBar_NoAttachedBadge(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down without attaching
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	if m.DrillModel() == nil {
		t.Fatal("expected drill model to exist")
	}
	bar := m.DrillModel().statusBar()
	if strings.Contains(bar, "ATTACHED") {
		t.Error("expected status bar to NOT contain 'ATTACHED' in non-attached mode")
	}
}

func TestMultiModel_AttachConfirm_View_ShowsDialog(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)

	// Press a
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "Attach to auth-feature") {
		t.Errorf("expected attach confirmation to contain workspace name, got %q", view)
	}
	if !strings.Contains(view, "(y/n)") {
		t.Error("expected attach confirmation to contain (y/n)")
	}
}

func TestMultiModel_Attached_Esc_DetachesBackToOverview(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	// Esc in attached mode — should also detach
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(MultiModel)

	if m.Mode() != modeOverview {
		t.Error("expected overview mode after Esc in attached mode")
	}
	if m.Attached() {
		t.Error("expected attached to be cleared after Esc detach")
	}
}

func TestMultiModel_Attached_StopConfirm_N_ReturnsToAttached(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Enter drill-down and attach
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	// q then n — should remain in attached drill-down
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(MultiModel)

	if m.Mode() != modeDrillDown {
		t.Error("expected to remain in drill-down after stop cancel")
	}
	if !m.Attached() {
		t.Error("expected to remain attached after stop cancel")
	}
	if m.DrillModel().ConfirmingStop() {
		t.Error("expected confirmingStop to be cleared")
	}
}

// --- isResumable tests ---

func TestIsResumable_StoppedWithIncompleteWork(t *testing.T) {
	ws := WorkspaceInfo{
		Running: false,
		PRD: &prd.PRD{
			UserStories: []prd.Story{
				{ID: "US-001", Passes: true},
				{ID: "US-002", Passes: false},
			},
		},
	}
	if !isResumable(ws) {
		t.Error("expected stopped workspace with incomplete work to be resumable")
	}
}

func TestIsResumable_Running(t *testing.T) {
	ws := WorkspaceInfo{
		Running: true,
		PRD: &prd.PRD{
			UserStories: []prd.Story{{ID: "US-001", Passes: false}},
		},
	}
	if isResumable(ws) {
		t.Error("expected running workspace to not be resumable")
	}
}

func TestIsResumable_NoPRD(t *testing.T) {
	ws := WorkspaceInfo{Running: false, PRD: nil}
	if isResumable(ws) {
		t.Error("expected workspace with nil PRD to not be resumable")
	}
}

func TestIsResumable_AllPassing(t *testing.T) {
	ws := WorkspaceInfo{
		Running: false,
		PRD: &prd.PRD{
			UserStories: []prd.Story{
				{ID: "US-001", Passes: true},
				{ID: "US-002", Passes: true},
			},
			IntegrationTests: []prd.IntegrationTest{
				{ID: "IT-001", Passes: true},
			},
		},
	}
	if isResumable(ws) {
		t.Error("expected finished workspace to not be resumable")
	}
}

func TestIsResumable_AllStoriesPassButTestsFailing(t *testing.T) {
	ws := WorkspaceInfo{
		Running: false,
		PRD: &prd.PRD{
			UserStories: []prd.Story{
				{ID: "US-001", Passes: true},
			},
			IntegrationTests: []prd.IntegrationTest{
				{ID: "IT-001", Passes: false},
			},
		},
	}
	if !isResumable(ws) {
		t.Error("expected workspace with failing tests to be resumable")
	}
}

// --- Resume key handling tests ---

func TestMultiModel_Overview_R_ShowsResumeConfirmation_Resumable(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3, stopped with incomplete work)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)

	// Press r
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	if !m.ConfirmingResume() {
		t.Error("expected confirmingResume after r key on resumable workspace")
	}
}

func TestMultiModel_Overview_R_NoEffect_Running(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// auth-feature is running (index 0)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	if m.ConfirmingResume() {
		t.Error("expected no resume confirmation for running workspace")
	}
}

func TestMultiModel_Overview_R_NoEffect_Finished(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "payment" (index 1, all stories pass, no integration tests)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	if m.ConfirmingResume() {
		t.Error("expected no resume confirmation for finished workspace")
	}
}

func TestMultiModel_Overview_R_NoEffect_NoPRD(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "dashboard" (index 2, nil PRD)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(MultiModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	if m.ConfirmingResume() {
		t.Error("expected no resume confirmation for workspace without PRD")
	}
}

func TestMultiModel_ResumeConfirm_Y_CallsResumeFn(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	resumeCalled := false
	var calledName, calledPath string
	var calledIndex int
	m.SetMakeResumeFn(func(index int, wsName, wsPath string) tea.Cmd {
		resumeCalled = true
		calledIndex = index
		calledName = wsName
		calledPath = wsPath
		return nil
	})
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	// r then y
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(MultiModel)

	if !resumeCalled {
		t.Error("expected makeResumeFn to be called")
	}
	if calledIndex != 3 {
		t.Errorf("expected index 3, got %d", calledIndex)
	}
	if calledName != "search" {
		t.Errorf("expected name 'search', got %q", calledName)
	}
	if calledPath != "/tmp/ws/search" {
		t.Errorf("expected path '/tmp/ws/search', got %q", calledPath)
	}
	if m.ConfirmingResume() {
		t.Error("expected confirmingResume to be cleared after y")
	}
}

func TestMultiModel_ResumeConfirm_N_CancelsResume(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(MultiModel)

	if m.ConfirmingResume() {
		t.Error("expected confirmingResume to be cleared after n")
	}
}

func TestMultiModel_ResumeConfirm_Esc_CancelsResume(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(MultiModel)

	if m.ConfirmingResume() {
		t.Error("expected confirmingResume to be cleared after Esc")
	}
}

func TestMultiModel_ResumeConfirm_BlocksOtherKeys(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	// Enter resume confirmation
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	// Navigation keys should be blocked
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(MultiModel)
	if m.MultiCursor() != 3 {
		t.Errorf("expected cursor to stay at 3 during confirmation, got %d", m.MultiCursor())
	}
	if !m.ConfirmingResume() {
		t.Error("expected confirmingResume to still be set")
	}
}

func TestMultiModel_ResumeConfirm_View_ShowsDialog(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(MultiModel)

	view := m.View()
	if !strings.Contains(view, "Resume search") {
		t.Errorf("expected resume confirmation to contain workspace name, got %q", view)
	}
	if !strings.Contains(view, "(y/n)") {
		t.Error("expected resume confirmation to contain (y/n)")
	}
}

func TestMultiModel_DaemonResumedMsg_UpdatesRunning(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// search is at index 3, stopped
	if m.Workspaces()[3].Running {
		t.Fatal("expected search to be stopped initially")
	}

	updated, _ := m.Update(multiDaemonResumedMsg{index: 3, err: nil})
	m = updated.(MultiModel)

	if !m.Workspaces()[3].Running {
		t.Error("expected search to be running after successful resume")
	}
}

func TestMultiModel_DaemonResumedMsg_Error_NoChange(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	updated, _ := m.Update(multiDaemonResumedMsg{index: 3, err: fmt.Errorf("spawn failed")})
	m = updated.(MultiModel)

	if m.Workspaces()[3].Running {
		t.Error("expected search to remain stopped after failed resume")
	}
}

func TestMultiModel_StatusBar_ShowsResumeHint(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Navigate to "search" (index 3, resumable)
	for range 3 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(MultiModel)
	}

	view := m.View()
	if !strings.Contains(view, "r to resume") {
		t.Error("expected status bar to contain 'r to resume' for resumable workspace")
	}
}

func TestRenderMultiHelpOverlay_ContainsResumeShortcut(t *testing.T) {
	content := renderMultiHelpOverlay()
	if !strings.Contains(content, "r") {
		t.Error("expected help overlay to contain 'r' shortcut")
	}
	if !strings.Contains(content, "Resume") {
		t.Error("expected help overlay to contain 'Resume' description")
	}
}

func TestMultiModel_Init_ReturnsTickCmd(t *testing.T) {
	m := NewMultiModel(makeTestWorkspaces())
	cmd := m.Init()
	if cmd == nil {
		t.Error("expected Init to return a tick command for PRD refresh")
	}
}

func TestMultiModel_PrdRefreshTick_ReReadsPRDFromDisk(t *testing.T) {
	// Create a temp PRD file with initial state (no stories passing)
	dir := t.TempDir()
	prdPath := dir + "/prd.json"
	initialPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Login", Passes: false},
		},
	}
	if err := prd.Write(prdPath, initialPRD); err != nil {
		t.Fatalf("writing initial PRD: %v", err)
	}

	// Override isRunningFn to avoid filesystem checks
	origIsRunning := isRunningFn
	isRunningFn = func(string) bool { return true }
	t.Cleanup(func() { isRunningFn = origIsRunning })

	workspaces := []WorkspaceInfo{
		{Name: "test-ws", WsPath: dir, Running: false, PRD: initialPRD},
	}
	m := NewMultiModel(workspaces)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Verify initial state
	if m.Workspaces()[0].PRD.UserStories[0].Passes {
		t.Fatal("expected initial PRD to have US-001 not passing")
	}
	if m.Workspaces()[0].Running {
		t.Fatal("expected initial running to be false")
	}

	// Update PRD file on disk — mark US-001 as passing
	updatedPRD := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Login", Passes: true},
		},
	}
	if err := prd.Write(prdPath, updatedPRD); err != nil {
		t.Fatalf("writing updated PRD: %v", err)
	}

	// Send prdRefreshTickMsg
	updated, cmd := m.Update(prdRefreshTickMsg{})
	m = updated.(MultiModel)

	// PRD should be re-read from disk
	if !m.Workspaces()[0].PRD.UserStories[0].Passes {
		t.Error("expected PRD refresh to pick up updated story status from disk")
	}

	// Running should be updated from isRunningFn
	if !m.Workspaces()[0].Running {
		t.Error("expected running state to be updated from isRunningFn")
	}

	// Should return a new tick command
	if cmd == nil {
		t.Error("expected prdRefreshTickMsg to return a new tick command")
	}
}

func TestMultiModel_PrdRefreshTick_HandlesNonExistentPRD(t *testing.T) {
	// Override isRunningFn
	origIsRunning := isRunningFn
	isRunningFn = func(string) bool { return false }
	t.Cleanup(func() { isRunningFn = origIsRunning })

	initialPRD := &prd.PRD{
		UserStories: []prd.Story{{ID: "US-001", Passes: false}},
	}
	workspaces := []WorkspaceInfo{
		{Name: "test-ws", WsPath: "/nonexistent/path", Running: true, PRD: initialPRD},
	}
	m := NewMultiModel(workspaces)
	ready, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = ready.(MultiModel)

	// Send tick — should not panic, should keep existing PRD
	updated, cmd := m.Update(prdRefreshTickMsg{})
	m = updated.(MultiModel)

	// PRD should still be the original (read failed, no update)
	if m.Workspaces()[0].PRD == nil {
		t.Error("expected PRD to remain set when disk read fails")
	}

	// Running should be updated from isRunningFn
	if m.Workspaces()[0].Running {
		t.Error("expected running to be updated to false")
	}

	// Should still return a new tick command
	if cmd == nil {
		t.Error("expected tick to return next tick command even on PRD read failure")
	}
}
