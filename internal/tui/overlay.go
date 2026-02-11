package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/prd"
)

// overlay holds the state for a detail overlay modal.
type overlay struct {
	visible bool
	content string // rendered content for display
	scroll  int    // scroll offset for long content
	lines   int    // total lines in content
	height  int    // visible height
}

var (
	overlayBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#6639ba", Dark: "#d2a8ff"}).
				Padding(1, 2)

	overlayTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#6639ba", Dark: "#d2a8ff"})

	overlayLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"})

	overlayPassStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}).
				Bold(true)

	overlayFailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}).
				Bold(true)
)

func newOverlay() overlay {
	return overlay{}
}

func (o *overlay) show(content string, height int) {
	o.visible = true
	o.content = content
	o.scroll = 0
	o.lines = strings.Count(content, "\n") + 1
	o.height = height
}

func (o *overlay) hide() {
	o.visible = false
	o.content = ""
	o.scroll = 0
}

func (o *overlay) scrollUp() {
	if o.scroll > 0 {
		o.scroll--
	}
}

func (o *overlay) scrollDown() {
	maxScroll := max(o.lines-o.height, 0)
	if o.scroll < maxScroll {
		o.scroll++
	}
}

// renderStoryOverlay builds the overlay content for a user story.
func renderStoryOverlay(s prd.Story) string {
	var b strings.Builder

	b.WriteString(overlayTitleStyle.Render(fmt.Sprintf("%s: %s", s.ID, s.Title)))
	b.WriteString("\n\n")

	// Status
	if s.Passes {
		b.WriteString(overlayLabelStyle.Render("Status: "))
		b.WriteString(overlayPassStyle.Render("PASS"))
	} else {
		b.WriteString(overlayLabelStyle.Render("Status: "))
		b.WriteString(overlayFailStyle.Render("FAIL"))
	}
	b.WriteString("\n\n")

	// Description
	if s.Description != "" {
		b.WriteString(overlayLabelStyle.Render("Description:"))
		b.WriteString("\n")
		b.WriteString(s.Description)
		b.WriteString("\n\n")
	}

	// Acceptance Criteria
	if len(s.AcceptanceCriteria) > 0 {
		b.WriteString(overlayLabelStyle.Render("Acceptance Criteria:"))
		b.WriteString("\n")
		for _, ac := range s.AcceptanceCriteria {
			fmt.Fprintf(&b, "  • %s\n", ac)
		}
		b.WriteString("\n")
	}

	// Notes
	if s.Notes != "" {
		b.WriteString(overlayLabelStyle.Render("Notes:"))
		b.WriteString("\n")
		b.WriteString(s.Notes)
		b.WriteString("\n")
	}

	return b.String()
}

// renderTestOverlay builds the overlay content for an integration test.
func renderTestOverlay(t prd.IntegrationTest) string {
	var b strings.Builder

	b.WriteString(overlayTitleStyle.Render(t.ID))
	b.WriteString("\n\n")

	// Status
	if t.Passes {
		b.WriteString(overlayLabelStyle.Render("Status: "))
		b.WriteString(overlayPassStyle.Render("PASS"))
	} else {
		b.WriteString(overlayLabelStyle.Render("Status: "))
		b.WriteString(overlayFailStyle.Render("FAIL"))
	}
	b.WriteString("\n\n")

	// Description
	if t.Description != "" {
		b.WriteString(overlayLabelStyle.Render("Description:"))
		b.WriteString("\n")
		b.WriteString(t.Description)
		b.WriteString("\n\n")
	}

	// Steps
	if len(t.Steps) > 0 {
		b.WriteString(overlayLabelStyle.Render("Steps:"))
		b.WriteString("\n")
		for i, step := range t.Steps {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
		}
		b.WriteString("\n")
	}

	// Failure details
	if t.Failure != "" {
		b.WriteString(overlayLabelStyle.Render("Failure:"))
		b.WriteString("\n")
		b.WriteString(t.Failure)
		b.WriteString("\n\n")
	}

	// Notes
	if t.Notes != "" {
		b.WriteString(overlayLabelStyle.Render("Notes:"))
		b.WriteString("\n")
		b.WriteString(t.Notes)
		b.WriteString("\n")
	}

	return b.String()
}

// renderHelpOverlay builds the overlay content for the keyboard shortcuts help.
func renderHelpOverlay() string {
	var b strings.Builder

	b.WriteString(overlayTitleStyle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	shortcuts := []struct {
		key  string
		desc string
	}{
		{"Tab", "Switch focus between sidebar and log"},
		{"↑/k", "Navigate up (sidebar items / scroll)"},
		{"↓/j", "Navigate down (sidebar items / scroll)"},
		{"Enter", "Open detail overlay for selected item"},
		{"Esc", "Close overlay / detach from TUI"},
		{"?", "Toggle this help overlay"},
		{"d", "Detach from TUI (daemon keeps running)"},
		{"q", "Stop the running loop (with confirmation)"},
		{"Ctrl+C", "Immediate stop (exit now)"},
	}

	for _, s := range shortcuts {
		key := overlayLabelStyle.Render(fmt.Sprintf("  %-10s", s.key))
		b.WriteString(fmt.Sprintf("%s %s\n", key, s.desc))
	}

	return b.String()
}

// view renders the overlay centered in the given dimensions.
func (o overlay) view(width, height int) string {
	if !o.visible {
		return ""
	}

	// Overlay takes ~80% of width, ~80% of height
	overlayW := min(width*4/5, width-4)
	overlayH := min(height*4/5, height-4)
	if overlayW < 20 {
		overlayW = 20
	}
	if overlayH < 5 {
		overlayH = 5
	}

	// Apply scroll to content
	contentLines := strings.Split(o.content, "\n")
	end := min(o.scroll+overlayH, len(contentLines))
	if o.scroll > len(contentLines) {
		o.scroll = 0
	}
	visibleContent := strings.Join(contentLines[o.scroll:end], "\n")

	// Inner content width accounts for padding (2 on each side) and border (1 on each side)
	innerW := max(overlayW-6, 10)

	rendered := overlayBorderStyle.
		Width(innerW).
		MaxHeight(overlayH).
		Render(visibleContent)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		rendered)
}
