package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/prd"
)

// Pane focus states.
const (
	focusLeft  = 0
	focusRight = 1
)

// sidebarItem represents a single entry in the sidebar (story or test).
type sidebarItem struct {
	id     string
	title  string
	passes bool
	active bool // currently being worked on
	isTest bool // true for integration tests
}

// sidebar holds the state for the left pane story/test list.
type sidebar struct {
	items    []sidebarItem
	cursor   int
	width    int
	height   int
	focused  bool
	scrollOff int // scroll offset for long lists
}

var (
	sidebarTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("8")).
		Padding(0, 1)

	passStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red

	activeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Bold(true) // yellow bold for active story

	cursorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")).
		Bold(true) // cyan cursor

	sidebarBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("8"))
)

func newSidebar() sidebar {
	return sidebar{}
}

// updateFromPRD refreshes the sidebar items from a PRD and active story ID.
func (s *sidebar) updateFromPRD(p *prd.PRD, activeStoryID string) {
	s.items = nil

	for _, story := range p.UserStories {
		s.items = append(s.items, sidebarItem{
			id:     story.ID,
			title:  story.Title,
			passes: story.Passes,
			active: story.ID == activeStoryID,
		})
	}

	for _, test := range p.IntegrationTests {
		s.items = append(s.items, sidebarItem{
			id:     test.ID,
			title:  test.Description,
			passes: test.Passes,
			isTest: true,
		})
	}

	// Keep cursor in bounds
	if s.cursor >= len(s.items) {
		s.cursor = max(len(s.items)-1, 0)
	}
}

// setActiveStory updates the active flag on all items based on the story ID.
func (s *sidebar) setActiveStory(storyID string) {
	for i := range s.items {
		s.items[i].active = s.items[i].id == storyID
	}
}

func (s *sidebar) moveUp() {
	if s.cursor > 0 {
		s.cursor--
	}
}

func (s *sidebar) moveDown() {
	if s.cursor < len(s.items)-1 {
		s.cursor++
	}
}

func (s sidebar) view() string {
	if len(s.items) == 0 {
		return sidebarBorderStyle.Width(s.width).Height(s.height).Render("No PRD loaded")
	}

	// Title
	title := sidebarTitleStyle.Width(s.width - 2).Render("Stories & Tests")

	// Available height for items (minus title line and separator)
	contentHeight := max(s.height-2, 1)

	// Calculate scroll offset to keep cursor visible
	s.adjustScroll(contentHeight)

	var lines []string

	// Find where tests start (to add a separator)
	firstTestIdx := -1
	for i, item := range s.items {
		if item.isTest {
			firstTestIdx = i
			break
		}
	}

	visibleEnd := min(s.scrollOff+contentHeight, len(s.items))

	for i := s.scrollOff; i < visibleEnd; i++ {
		item := s.items[i]

		// Add separator before tests section
		if i == firstTestIdx && i > 0 {
			lines = append(lines, strings.Repeat("─", s.width-2))
			contentHeight-- // separator takes a line
			visibleEnd = min(s.scrollOff+contentHeight+1, len(s.items)) // +1 for separator line just added
			if i >= visibleEnd {
				break
			}
		}

		line := s.renderItem(i, item)
		lines = append(lines, line)
	}

	// Pad to full height
	for len(lines) < s.height-1 {
		lines = append(lines, "")
	}

	content := title + "\n" + strings.Join(lines, "\n")
	return sidebarBorderStyle.Width(s.width).Height(s.height).Render(content)
}

func (s *sidebar) adjustScroll(contentHeight int) {
	if s.cursor < s.scrollOff {
		s.scrollOff = s.cursor
	}
	if s.cursor >= s.scrollOff+contentHeight {
		s.scrollOff = s.cursor - contentHeight + 1
	}
}

func (s sidebar) renderItem(idx int, item sidebarItem) string {
	// Status indicator
	var indicator string
	if item.passes {
		indicator = passStyle.Render("✓")
	} else {
		indicator = failStyle.Render("✗")
	}

	// Cursor or space
	var prefix string
	if s.focused && idx == s.cursor {
		prefix = cursorStyle.Render("▸")
	} else {
		prefix = " "
	}

	// Item text: "ID Title" truncated to width
	text := fmt.Sprintf("%s %s", item.id, item.title)
	maxTextWidth := s.width - 6 // prefix + indicator + spaces
	if maxTextWidth > 0 && len(text) > maxTextWidth {
		text = text[:maxTextWidth-1] + "…"
	}

	// Active story highlighting
	if item.active {
		text = activeStyle.Render(text)
	}

	return fmt.Sprintf("%s %s %s", prefix, indicator, text)
}

// Items returns the current items (for testing).
func (s sidebar) Items() []sidebarItem {
	return s.items
}

// Cursor returns the current cursor position (for testing).
func (s sidebar) Cursor() int {
	return s.cursor
}
