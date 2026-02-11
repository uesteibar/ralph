package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

const sidebarWidth = 60

// eventMsg wraps an events.Event for delivery as a tea.Msg.
type eventMsg struct {
	event events.Event
}

// prdLoadedMsg is sent when a PRD file has been read from disk.
type prdLoadedMsg struct {
	prd *prd.PRD
}

// daemonStoppedMsg is sent when the daemon process has exited after SIGTERM.
type daemonStoppedMsg struct{}

// logReaderDoneMsg is sent when the LogReader channel is closed (daemon exited).
type logReaderDoneMsg struct{}

// Model is the BubbleTea model for the Ralph TUI.
type Model struct {
	viewport viewport.Model
	lines    []string
	ready    bool

	sidebar     sidebar
	overlay     overlay
	helpOverlay overlay
	focus       int // focusLeft or focusRight

	// PRD path for file-based refresh
	prdPath    string
	currentPRD *prd.PRD // cached PRD for overlay lookups

	// Status bar fields
	workspaceName string
	currentStory  string
	activeStoryID string
	iteration     int
	maxIterations int

	// Stop / detach
	quitting       bool
	confirmingStop bool
	stopDaemonFn   func() // sends SIGTERM to daemon PID, waits for exit
	attached       bool   // true when attached to daemon in multi-workspace TUI

	width  int
	height int
}

var (
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Light: "#d8dee4", Dark: "#30363d"}).
			Foreground(lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#e6edf3"}).
			Padding(0, 1)
	statusKeyStyle = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Light: "#d8dee4", Dark: "#30363d"}).
			Foreground(lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"}).
			Padding(0, 1).
			Bold(true)
)

// NewModel creates a new TUI model with the given workspace name and PRD path.
func NewModel(workspaceName string, prdPath string) Model {
	return Model{
		workspaceName: workspaceName,
		prdPath:       prdPath,
		sidebar:       newSidebar(),
		focus:         focusRight, // default focus on agent log
	}
}

// SetStopDaemonFn sets the function called when the user confirms stopping the daemon.
func (m *Model) SetStopDaemonFn(fn func()) {
	m.stopDaemonFn = fn
}

// readPRDCmd returns a tea.Cmd that reads the PRD from disk.
func readPRDCmd(path string) tea.Cmd {
	return func() tea.Msg {
		p, err := prd.Read(path)
		if err != nil {
			return nil
		}
		return prdLoadedMsg{prd: p}
	}
}

func (m Model) Init() tea.Cmd {
	if m.prdPath != "" {
		return readPRDCmd(m.prdPath)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Ctrl+C always triggers immediate stop
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// When confirming stop, only accept y/n/Esc
		if m.confirmingStop {
			switch msg.String() {
			case "y":
				m.confirmingStop = false
				m.quitting = true
				if m.stopDaemonFn != nil {
					m.stopDaemonFn()
				}
				return m, waitForDaemonExit()
			case "n", "esc":
				m.confirmingStop = false
				return m, nil
			}
			return m, nil
		}

		// When help overlay is visible, intercept keys
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

		// When detail overlay is visible, intercept keys
		if m.overlay.visible {
			switch msg.String() {
			case "esc":
				m.overlay.hide()
				return m, nil
			case "up", "k":
				m.overlay.scrollUp()
				return m, nil
			case "down", "j":
				m.overlay.scrollDown()
				return m, nil
			case "q":
				m.overlay.hide()
				m.confirmingStop = true
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q":
			m.confirmingStop = true
			return m, nil
		case "d", "esc":
			return m, tea.Quit
		case "?":
			m.helpOverlay.show(renderHelpOverlay(), m.height)
			return m, nil
		case "tab":
			if m.focus == focusLeft {
				m.focus = focusRight
			} else {
				m.focus = focusLeft
			}
			m.sidebar.focused = m.focus == focusLeft
			return m, nil
		case "enter":
			if m.focus == focusLeft {
				m.openOverlay()
				return m, nil
			}
		case "up", "k":
			if m.focus == focusLeft {
				m.sidebar.moveUp()
				return m, nil
			}
		case "down", "j":
			if m.focus == focusLeft {
				m.sidebar.moveDown()
				return m, nil
			}
		}

	case daemonStoppedMsg:
		return m, tea.Quit

	case logReaderDoneMsg:
		return m, tea.Quit

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		footerHeight := 1 // status bar
		contentHeight := max(m.height-footerHeight, 1)
		viewportWidth := max(m.width-sidebarWidth, 1)
		if !m.ready {
			m.viewport = viewport.New(viewportWidth, contentHeight)
			m.viewport.SetContent(strings.Join(m.lines, "\n"))
			m.ready = true
		} else {
			m.viewport.Width = viewportWidth
			m.viewport.Height = contentHeight
		}
		m.sidebar.width = sidebarWidth
		m.sidebar.height = contentHeight

	case prdLoadedMsg:
		if msg.prd != nil {
			m.currentPRD = msg.prd
			m.sidebar.updateFromPRD(msg.prd, m.activeStoryID)
		}
		return m, nil

	case eventMsg:
		m.handleEvent(msg.event)
		if m.ready {
			m.viewport.SetContent(strings.Join(m.lines, "\n"))
			m.viewport.GotoBottom()
		}
		// If this was a PRDRefresh event, trigger a file read
		if _, ok := msg.event.(events.PRDRefresh); ok && m.prdPath != "" {
			return m, readPRDCmd(m.prdPath)
		}
	}

	if m.ready {
		m.viewport, cmd = m.viewport.Update(msg)
	}

	return m, cmd
}

// waitForDaemonExit returns a tea.Cmd that signals daemon has stopped.
// The actual SIGTERM was already sent synchronously; this just signals the TUI.
func waitForDaemonExit() tea.Cmd {
	return func() tea.Msg {
		return daemonStoppedMsg{}
	}
}

// openOverlay opens the detail overlay for the currently selected sidebar item.
func (m *Model) openOverlay() {
	items := m.sidebar.Items()
	if len(items) == 0 || m.currentPRD == nil {
		return
	}
	cursor := m.sidebar.Cursor()
	if cursor < 0 || cursor >= len(items) {
		return
	}

	item := items[cursor]
	var content string
	if item.isTest {
		for _, t := range m.currentPRD.IntegrationTests {
			if t.ID == item.id {
				content = renderTestOverlay(t)
				break
			}
		}
	} else {
		for _, s := range m.currentPRD.UserStories {
			if s.ID == item.id {
				content = renderStoryOverlay(s)
				break
			}
		}
	}

	if content != "" {
		m.overlay.show(content, m.height)
	}
}

func (m *Model) handleEvent(e events.Event) {
	switch e := e.(type) {
	case events.ToolUse:
		line := fmt.Sprintf("  → %s", e.Name)
		if e.Detail != "" {
			line += " " + e.Detail
		}
		m.lines = append(m.lines, line)

	case events.AgentText:
		text := strings.TrimSpace(e.Text)
		for line := range strings.SplitSeq(text, "\n") {
			m.lines = append(m.lines, "  "+line)
		}

	case events.InvocationDone:
		durationSec := e.DurationMS / 1000
		m.lines = append(m.lines, fmt.Sprintf("  ✓ Done (%d turns, %ds)", e.NumTurns, durationSec))

	case events.IterationStart:
		m.iteration = e.Iteration
		m.maxIterations = e.MaxIterations
		m.lines = append(m.lines, fmt.Sprintf("iteration %d/%d", e.Iteration, e.MaxIterations))

	case events.StoryStarted:
		m.activeStoryID = e.StoryID
		m.currentStory = fmt.Sprintf("%s: %s", e.StoryID, e.Title)
		m.lines = append(m.lines, fmt.Sprintf("working on %s: %s", e.StoryID, e.Title))
		// Update sidebar to highlight the active story
		m.sidebar.setActiveStory(e.StoryID)

	case events.QAPhaseStarted:
		m.activeStoryID = ""
		m.currentStory = fmt.Sprintf("QA %s", e.Phase)
		m.lines = append(m.lines, fmt.Sprintf("all stories pass — running QA %s", e.Phase))
		m.sidebar.setActiveStory("")

	case events.UsageLimitWait:
		m.lines = append(m.lines, fmt.Sprintf("  ⏳ Usage limit reached — waiting %s (until %s)",
			e.WaitDuration, e.ResetAt.Format("3:04pm MST")))

	case events.PRDRefresh:
		// Handled in Update() — triggers readPRDCmd
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	left := m.sidebar.view()
	right := m.viewport.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	base := content + "\n" + m.statusBar()

	if m.confirmingStop {
		prompt := confirmPromptStyle.Render("Stop the running loop? (y/n)")
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			prompt)
	}

	if m.helpOverlay.visible {
		return m.helpOverlay.view(m.width, m.height)
	}

	if m.overlay.visible {
		return m.overlay.view(m.width, m.height)
	}

	return base
}

var stoppingStyle = lipgloss.NewStyle().
	Background(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}).
	Foreground(lipgloss.Color("#ffffff")).
	Padding(0, 1).
	Bold(true)

var attachedStyle = lipgloss.NewStyle().
	Background(lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}).
	Foreground(lipgloss.Color("#ffffff")).
	Padding(0, 1).
	Bold(true)

var confirmPromptStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}).
	Padding(1, 3).
	Bold(true)

func (m Model) statusBar() string {
	ws := statusKeyStyle.Render(m.workspaceName)

	var story string
	if m.currentStory != "" {
		story = statusBarStyle.Render(m.currentStory)
	}

	var iter string
	if m.maxIterations > 0 {
		iter = statusBarStyle.Render(fmt.Sprintf("%d/%d", m.iteration, m.maxIterations))
	}

	left := ws
	if story != "" {
		left += " " + story
	}
	if iter != "" {
		left += " " + iter
	}
	if m.attached {
		left += " " + attachedStyle.Render("ATTACHED")
	}
	if m.quitting {
		left += " " + stoppingStyle.Render("Stopping...")
	}

	// Pad the status bar to full width
	bar := statusBarStyle.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Left, left),
	)
	return bar
}

// Lines returns the current log lines (for testing).
func (m Model) Lines() []string {
	return m.lines
}

// CurrentStory returns the current story being worked on (for testing).
func (m Model) CurrentStory() string {
	return m.currentStory
}

// WorkspaceName returns the workspace name (for testing).
func (m Model) WorkspaceName() string {
	return m.workspaceName
}

// Iteration returns the current iteration (for testing).
func (m Model) Iteration() int {
	return m.iteration
}

// MaxIterations returns the max iterations (for testing).
func (m Model) MaxIterations() int {
	return m.maxIterations
}

// Focus returns the current focus pane (for testing).
func (m Model) Focus() int {
	return m.focus
}

// Sidebar returns the sidebar (for testing).
func (m Model) Sidebar() sidebar {
	return m.sidebar
}

// ActiveStoryID returns the active story ID (for testing).
func (m Model) ActiveStoryID() string {
	return m.activeStoryID
}

// Overlay returns the overlay (for testing).
func (m Model) Overlay() overlay {
	return m.overlay
}

// CurrentPRD returns the cached PRD (for testing).
func (m Model) CurrentPRD() *prd.PRD {
	return m.currentPRD
}

// HelpOverlay returns the help overlay (for testing).
func (m Model) HelpOverlay() overlay {
	return m.helpOverlay
}

// Quitting returns whether the model is in graceful stop mode (for testing).
func (m Model) Quitting() bool {
	return m.quitting
}

// ConfirmingStop returns whether the model is showing the stop confirmation prompt (for testing).
func (m Model) ConfirmingStop() bool {
	return m.confirmingStop
}

// MakeEventMsg wraps an events.Event as a tea.Msg for use in integration tests.
func MakeEventMsg(e events.Event) tea.Msg {
	return eventMsg{event: e}
}

// MakeLogReaderDoneMsg creates a logReaderDoneMsg for external use.
func MakeLogReaderDoneMsg() tea.Msg {
	return logReaderDoneMsg{}
}
