package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

const multiSidebarWidth = 38

// WorkspaceInfo holds the data needed to display a workspace in the overview.
type WorkspaceInfo struct {
	Name    string
	Branch  string
	Running bool
	PRD     *prd.PRD
	LogsDir string
	WsPath  string
}

// viewMode tracks whether we are in the overview or drill-down view.
type viewMode int

const (
	modeOverview  viewMode = 0
	modeDrillDown viewMode = 1
)

// multiPrdLoadedMsg is sent when a workspace's PRD has been read.
type multiPrdLoadedMsg struct {
	index int
	prd   *prd.PRD
}

// multiLogEventMsg wraps an event from a specific workspace's log reader.
type multiLogEventMsg struct {
	index int
	event events.Event
}

// multiLogReaderDoneMsg is sent when a workspace's log reader channel closes.
type multiLogReaderDoneMsg struct {
	index int
}

// multiDaemonResumedMsg is sent after a daemon spawn attempt completes.
type multiDaemonResumedMsg struct {
	index int
	err   error
}

// prdRefreshTickMsg is sent periodically to re-read PRDs and running state.
type prdRefreshTickMsg struct{}

// MultiModel is the BubbleTea model for the multi-workspace overview.
type MultiModel struct {
	workspaces []WorkspaceInfo
	cursor     int
	mode       viewMode

	// Right panes
	logViewport viewport.Model
	logLines    map[int][]string // per-workspace log lines

	// Overlays
	helpOverlay overlay

	// Drill-down: embedded single-workspace model
	drillModel *Model

	// Attach state
	attached         bool
	confirmingAttach bool
	makeStopFn       func(wsPath string) func() // factory: creates a stop function for a given workspace path

	// Resume state
	confirmingResume bool
	makeResumeFn     func(index int, wsName, wsPath string) tea.Cmd // factory: creates a resume command for a workspace

	ready bool
	width  int
	height int
}

// isRunningFn is a package-level variable for testability.
var isRunningFn = runstate.IsRunning

// NewMultiModel creates a new multi-workspace overview model.
func NewMultiModel(workspaces []WorkspaceInfo) MultiModel {
	return MultiModel{
		workspaces: workspaces,
		logLines:   make(map[int][]string),
	}
}

func (m MultiModel) Init() tea.Cmd {
	return prdRefreshTick()
}

func prdRefreshTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return prdRefreshTickMsg{}
	})
}

func (m MultiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If in drill-down mode, delegate to the embedded model
	if m.mode == modeDrillDown && m.drillModel != nil {
		return m.updateDrillDown(msg)
	}

	return m.updateOverview(msg)
}

func (m MultiModel) updateDrillDown(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Handle attach confirmation dialog
		if m.confirmingAttach {
			switch msg.String() {
			case "y":
				m.confirmingAttach = false
				m.attached = true
				// Wire stopDaemonFn into the drill model using the current workspace path
				if m.drillModel != nil {
					if m.makeStopFn != nil && m.cursor >= 0 && m.cursor < len(m.workspaces) {
						m.drillModel.SetStopDaemonFn(m.makeStopFn(m.workspaces[m.cursor].WsPath))
					}
					m.drillModel.attached = true
				}
				return m, nil
			case "n", "esc":
				m.confirmingAttach = false
				return m, nil
			}
			return m, nil
		}

		if m.attached {
			// In attached mode: d and Esc detach, q delegates to embedded model
			if msg.String() == "d" || msg.String() == "esc" {
				m.mode = modeOverview
				m.attached = false
				m.drillModel = nil
				return m, nil
			}
			// Delegate all other keys (including q) to the single-workspace model
		} else {
			// Non-attached mode: q, d, Esc all return to overview
			if msg.String() == "q" || msg.String() == "esc" || msg.String() == "d" {
				m.mode = modeOverview
				m.drillModel = nil
				return m, nil
			}
			// 'a' triggers attach confirmation for running workspaces
			if msg.String() == "a" {
				if m.cursor >= 0 && m.cursor < len(m.workspaces) && m.workspaces[m.cursor].Running {
					m.confirmingAttach = true
				}
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	// Delegate to the single-workspace model
	updated, cmd := m.drillModel.Update(msg)
	um := updated.(Model)
	m.drillModel = &um

	// Intercept tea.Quit from the embedded model when attached (stop confirmed)
	// — let it propagate so the TUI exits after daemon stop
	return m, cmd
}

func (m MultiModel) updateOverview(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.confirmingResume {
			switch msg.String() {
			case "y":
				m.confirmingResume = false
				if m.makeResumeFn != nil && m.cursor >= 0 && m.cursor < len(m.workspaces) {
					ws := m.workspaces[m.cursor]
					return m, m.makeResumeFn(m.cursor, ws.Name, ws.WsPath)
				}
				return m, nil
			case "n", "esc":
				m.confirmingResume = false
				return m, nil
			}
			return m, nil
		}

		if m.helpOverlay.visible {
			switch msg.String() {
			case "esc", "?":
				m.helpOverlay.hide()
				return m, nil
			case "up", "k":
				m.helpOverlay.scrollUp()
				return m, nil
			case "down", "j":
				m.helpOverlay.scrollDown()
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "?":
			m.helpOverlay.show(renderMultiHelpOverlay(), m.height)
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				return m, nil
			}
		case "down", "j":
			if m.cursor < len(m.workspaces)-1 {
				m.cursor++
				return m, nil
			}
		case "enter":
			return m.enterDrillDown()
		case "r":
			if m.cursor >= 0 && m.cursor < len(m.workspaces) {
				if isResumable(m.workspaces[m.cursor]) {
					m.confirmingResume = true
				}
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		footerHeight := 1
		contentHeight := max(m.height-footerHeight, 1)
		vpWidth := max(m.width-multiSidebarWidth, 1)
		logHeight := max(contentHeight/2, 1)
		if !m.ready {
			m.logViewport = viewport.New(vpWidth, logHeight)
			m.ready = true
		} else {
			m.logViewport.Width = vpWidth
			m.logViewport.Height = logHeight
		}

	case prdRefreshTickMsg:
		for i := range m.workspaces {
			ws := &m.workspaces[i]
			ws.Running = isRunningFn(ws.WsPath)
			if ws.WsPath != "" {
				prdPath := ws.WsPath + "/prd.json"
				if p, err := prd.Read(prdPath); err == nil {
					ws.PRD = p
				}
			}
		}
		return m, prdRefreshTick()

	case multiPrdLoadedMsg:
		if msg.index >= 0 && msg.index < len(m.workspaces) && msg.prd != nil {
			m.workspaces[msg.index].PRD = msg.prd
		}
		return m, nil

	case multiLogEventMsg:
		m.handleLogEvent(msg.index, msg.event)
		if m.ready && msg.index == m.cursor {
			m.logViewport.SetContent(strings.Join(m.logLines[m.cursor], "\n"))
			m.logViewport.GotoBottom()
		}
		return m, nil

	case multiLogReaderDoneMsg:
		return m, nil

	case multiDaemonResumedMsg:
		if msg.err == nil && msg.index >= 0 && msg.index < len(m.workspaces) {
			m.workspaces[msg.index].Running = true
		}
		return m, nil
	}

	if m.ready {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *MultiModel) handleLogEvent(index int, e events.Event) {
	lines := m.logLines[index]
	switch e := e.(type) {
	case events.ToolUse:
		line := fmt.Sprintf("  → %s", e.Name)
		if e.Detail != "" {
			line += " " + e.Detail
		}
		lines = append(lines, line)
	case events.AgentText:
		text := strings.TrimSpace(e.Text)
		for line := range strings.SplitSeq(text, "\n") {
			lines = append(lines, "  "+line)
		}
	case events.InvocationDone:
		durationSec := e.DurationMS / 1000
		lines = append(lines, fmt.Sprintf("  ✓ Done (%d turns, %ds)", e.NumTurns, durationSec))
	case events.IterationStart:
		lines = append(lines, fmt.Sprintf("iteration %d/%d", e.Iteration, e.MaxIterations))
	case events.StoryStarted:
		lines = append(lines, fmt.Sprintf("working on %s: %s", e.StoryID, e.Title))
	case events.QAPhaseStarted:
		lines = append(lines, fmt.Sprintf("all stories pass — running QA %s", e.Phase))
	case events.UsageLimitWait:
		lines = append(lines, fmt.Sprintf("  ⏳ Usage limit reached — waiting %s (until %s)",
			e.WaitDuration, e.ResetAt.Format("3:04pm MST")))
	}
	m.logLines[index] = lines
}

func (m MultiModel) enterDrillDown() (tea.Model, tea.Cmd) {
	if len(m.workspaces) == 0 {
		return m, nil
	}
	ws := m.workspaces[m.cursor]
	prdPath := ""
	if ws.WsPath != "" {
		prdPath = ws.WsPath + "/prd.json"
	}
	model := NewModel(ws.Name, prdPath)
	m.mode = modeDrillDown
	m.drillModel = &model

	// Feed existing PRD data if available
	var cmds []tea.Cmd
	cmds = append(cmds, model.Init())
	cmds = append(cmds, func() tea.Msg {
		return tea.WindowSizeMsg{Width: m.width, Height: m.height}
	})

	return m, tea.Batch(cmds...)
}

func (m MultiModel) View() string {
	if m.mode == modeDrillDown && m.drillModel != nil {
		if m.confirmingAttach {
			ws := m.workspaces[m.cursor]
			prompt := confirmPromptStyle.Render(
				fmt.Sprintf("Attach to %s? This allows you to stop the running loop. (y/n)", ws.Name))
			return lipgloss.Place(m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				prompt)
		}
		return m.drillModel.View()
	}

	if !m.ready {
		return "Initializing..."
	}

	if m.confirmingResume && m.cursor >= 0 && m.cursor < len(m.workspaces) {
		ws := m.workspaces[m.cursor]
		prompt := confirmPromptStyle.Render(
			fmt.Sprintf("Resume %s? This will spawn a new daemon. (y/n)", ws.Name))
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			prompt)
	}

	if m.helpOverlay.visible {
		return m.helpOverlay.view(m.width, m.height)
	}

	left := m.renderWorkspaceList()

	// Right pane: top (stories/tests) + bottom (logs)
	rightWidth := max(m.width-multiSidebarWidth, 1)
	footerHeight := 1
	contentHeight := max(m.height-footerHeight, 1)
	topHeight := max(contentHeight/2, 1)
	bottomHeight := max(contentHeight-topHeight, 1)

	topPane := m.renderPRDPane(rightWidth, topHeight)
	m.logViewport.Width = rightWidth
	m.logViewport.Height = bottomHeight
	if lines, ok := m.logLines[m.cursor]; ok {
		m.logViewport.SetContent(strings.Join(lines, "\n"))
	}
	bottomPane := m.logViewport.View()

	right := lipgloss.JoinVertical(lipgloss.Left, topPane, bottomPane)
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return content + "\n" + m.multiStatusBar()
}

var (
	wsSidebarBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, true, false, false).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#484f58"})

	wsTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#e6edf3"}).
			Background(lipgloss.AdaptiveColor{Light: "#d8dee4", Dark: "#30363d"}).
			Padding(0, 1)

	wsRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"})
	wsStoppedStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#8b949e"})

	wsCursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"}).
			Bold(true)

	wsSelectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#e6edf3"}).
			Bold(true)

	prdPaneBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#484f58"})

	prdSectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"})
)

func (m MultiModel) renderWorkspaceList() string {
	title := wsTitleStyle.Width(multiSidebarWidth - 2).Render("Workspaces")

	contentHeight := max(m.height-2, 1) // -1 for status bar, -1 for title
	var lines []string

	for i, ws := range m.workspaces {
		var indicator string
		if ws.Running {
			indicator = wsRunningStyle.Render("●")
		} else {
			indicator = wsStoppedStyle.Render("○")
		}

		name := ws.Name
		maxNameWidth := multiSidebarWidth - 6
		if len(name) > maxNameWidth {
			name = name[:maxNameWidth-1] + "…"
		}

		var prefix string
		if i == m.cursor {
			prefix = wsCursorStyle.Render("▸")
			name = wsSelectedStyle.Render(name)
		} else {
			prefix = " "
		}

		lines = append(lines, fmt.Sprintf("%s %s %s", prefix, indicator, name))

		// Summary line: pass counts for stories and tests
		if ws.PRD != nil {
			summary := renderWorkspaceSummary(ws)
			lines = append(lines, "    "+summary)
		}
	}

	// Pad to full height
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}

	content := title + "\n" + strings.Join(lines, "\n")
	return wsSidebarBorderStyle.Width(multiSidebarWidth).Height(m.height - 1).Render(content)
}

func renderWorkspaceSummary(ws WorkspaceInfo) string {
	storiesPass, storiesTotal := 0, len(ws.PRD.UserStories)
	for _, s := range ws.PRD.UserStories {
		if s.Passes {
			storiesPass++
		}
	}

	testsPass, testsTotal := 0, len(ws.PRD.IntegrationTests)
	for _, t := range ws.PRD.IntegrationTests {
		if t.Passes {
			testsPass++
		}
	}

	storyStyle := wsStoppedStyle
	if storiesTotal > 0 && storiesPass == storiesTotal {
		storyStyle = wsRunningStyle
	} else if storiesTotal > 0 && storiesPass < storiesTotal {
		storyStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"})
	}
	parts := []string{storyStyle.Render(fmt.Sprintf("%d/%d stories", storiesPass, storiesTotal))}

	if testsTotal > 0 {
		testStyle := wsStoppedStyle
		if testsPass == testsTotal {
			testStyle = wsRunningStyle
		} else {
			testStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"})
		}
		parts = append(parts, testStyle.Render(fmt.Sprintf("%d/%d tests", testsPass, testsTotal)))
	}

	return strings.Join(parts, "  ")
}

func (m MultiModel) renderPRDPane(width, height int) string {
	if m.cursor < 0 || m.cursor >= len(m.workspaces) {
		return prdPaneBorderStyle.Width(width).Height(height).Render("No workspace selected")
	}

	ws := m.workspaces[m.cursor]
	if ws.PRD == nil {
		return prdPaneBorderStyle.Width(width).Height(height).Render("No PRD loaded")
	}

	// Split into stories (left) and tests (right)
	halfWidth := max(width/2, 1)

	// User Stories column
	var storyLines []string
	storyLines = append(storyLines, prdSectionTitleStyle.Render("User Stories"))
	for _, s := range ws.PRD.UserStories {
		var indicator string
		if s.Passes {
			indicator = passStyle.Render("✓")
		} else {
			indicator = failStyle.Render("✗")
		}
		text := fmt.Sprintf("%s %s", s.ID, s.Title)
		maxW := halfWidth - 5
		if maxW > 0 && len(text) > maxW {
			text = text[:maxW-1] + "…"
		}
		storyLines = append(storyLines, fmt.Sprintf(" %s %s", indicator, text))
	}
	storiesCol := strings.Join(storyLines, "\n")

	// Integration Tests column
	var testLines []string
	testLines = append(testLines, prdSectionTitleStyle.Render("Integration Tests"))
	if len(ws.PRD.IntegrationTests) == 0 {
		testLines = append(testLines, " (none)")
	} else {
		for _, t := range ws.PRD.IntegrationTests {
			var indicator string
			if t.Passes {
				indicator = passStyle.Render("✓")
			} else {
				indicator = failStyle.Render("✗")
			}
			text := fmt.Sprintf("%s %s", t.ID, t.Description)
			maxW := halfWidth - 5
			if maxW > 0 && len(text) > maxW {
				text = text[:maxW-1] + "…"
			}
			testLines = append(testLines, fmt.Sprintf(" %s %s", indicator, text))
		}
	}
	testsCol := strings.Join(testLines, "\n")

	leftCol := lipgloss.NewStyle().Width(halfWidth).Render(storiesCol)
	rightCol := lipgloss.NewStyle().Width(halfWidth).Render(testsCol)

	pane := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)
	return prdPaneBorderStyle.Width(width).Height(height).Render(pane)
}

func (m MultiModel) multiStatusBar() string {
	ws := ""
	if m.cursor >= 0 && m.cursor < len(m.workspaces) {
		ws = statusKeyStyle.Render(m.workspaces[m.cursor].Name)
	}

	status := ""
	if m.cursor >= 0 && m.cursor < len(m.workspaces) {
		ws := m.workspaces[m.cursor]
		if ws.Running {
			status = wsRunningStyle.Render("running")
		} else if isResumable(ws) {
			status = wsStoppedStyle.Render("stopped (r to resume)")
		} else {
			status = statusBarStyle.Render("stopped")
		}
	}

	left := ws
	if status != "" {
		left += " " + status
	}

	bar := statusBarStyle.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Left, left),
	)
	return bar
}

// renderMultiHelpOverlay builds the help overlay content for the multi-workspace view.
func renderMultiHelpOverlay() string {
	var b strings.Builder

	b.WriteString(overlayTitleStyle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	shortcuts := []struct {
		key  string
		desc string
	}{
		{"↑/k", "Navigate to previous workspace"},
		{"↓/j", "Navigate to next workspace"},
		{"Enter", "Drill into selected workspace"},
		{"r", "Resume stopped workspace"},
		{"a", "Attach to running workspace (in drill-down)"},
		{"d", "Detach from attached workspace"},
		{"Esc", "Quit TUI / return from drill-down"},
		{"?", "Toggle this help overlay"},
		{"q", "Quit TUI / Stop daemon (when attached)"},
		{"Ctrl+C", "Immediate quit"},
	}

	for _, s := range shortcuts {
		key := overlayLabelStyle.Render(fmt.Sprintf("  %-10s", s.key))
		fmt.Fprintf(&b, "%s %s\n", key, s.desc)
	}

	return b.String()
}

// SetMakeStopFn sets the factory function that creates a stop function for a given workspace path.
func (m *MultiModel) SetMakeStopFn(fn func(wsPath string) func()) {
	m.makeStopFn = fn
}

// SetMakeResumeFn sets the factory function that creates a resume command for a workspace.
func (m *MultiModel) SetMakeResumeFn(fn func(index int, wsName, wsPath string) tea.Cmd) {
	m.makeResumeFn = fn
}

// isResumable returns true if a workspace is stopped, has a PRD, and is not finished.
func isResumable(ws WorkspaceInfo) bool {
	if ws.Running || ws.PRD == nil {
		return false
	}
	return !(prd.AllPass(ws.PRD) && prd.AllIntegrationTestsPass(ws.PRD))
}

// --- Test accessors ---

// Mode returns the current view mode (for testing).
func (m MultiModel) Mode() viewMode { return m.mode }

// Cursor returns the current workspace cursor position (for testing).
func (m MultiModel) MultiCursor() int { return m.cursor }

// Workspaces returns the workspace list (for testing).
func (m MultiModel) Workspaces() []WorkspaceInfo { return m.workspaces }

// DrillModel returns the embedded single-workspace model (for testing).
func (m MultiModel) DrillModel() *Model { return m.drillModel }

// MultiHelpOverlay returns the help overlay (for testing).
func (m MultiModel) MultiHelpOverlay() overlay { return m.helpOverlay }

// LogLines returns log lines for a workspace index (for testing).
func (m MultiModel) LogLines(index int) []string { return m.logLines[index] }

// Attached returns whether the multi-model is in attached mode (for testing).
func (m MultiModel) Attached() bool { return m.attached }

// ConfirmingAttach returns whether the attach confirmation is visible (for testing).
func (m MultiModel) ConfirmingAttach() bool { return m.confirmingAttach }

// ConfirmingResume returns whether the resume confirmation is visible (for testing).
func (m MultiModel) ConfirmingResume() bool { return m.confirmingResume }

// MakeMultiLogEventMsg wraps an event for external use in tests.
func MakeMultiLogEventMsg(index int, e events.Event) tea.Msg {
	return multiLogEventMsg{index: index, event: e}
}

// MakeMultiDaemonResumedMsg wraps a daemon resumed result for external use.
func MakeMultiDaemonResumedMsg(index int, err error) tea.Msg {
	return multiDaemonResumedMsg{index: index, err: err}
}

// loadWorkspaceInfos gathers WorkspaceInfo for all registered workspaces.
func LoadWorkspaceInfos(repoPath string) ([]WorkspaceInfo, error) {
	entries, err := workspace.RegistryListWithMissing(repoPath)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}

	var infos []WorkspaceInfo
	for _, entry := range entries {
		if entry.Missing {
			continue
		}
		wsPath := workspace.WorkspacePath(repoPath, entry.Name)
		logsDir := wsPath + "/logs"
		running := isRunningFn(wsPath)

		prdPath := workspace.PRDPathForWorkspace(repoPath, entry.Name)
		p, _ := prd.Read(prdPath)

		infos = append(infos, WorkspaceInfo{
			Name:    entry.Name,
			Branch:  entry.Branch,
			Running: running,
			PRD:     p,
			LogsDir: logsDir,
			WsPath:  wsPath,
		})
	}

	return infos, nil
}
