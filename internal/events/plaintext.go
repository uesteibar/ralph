package events

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles for Claude output (previously in claude.go)
	arrowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))            // cyan
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true) // blue bold
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // gray
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // light gray
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // dim gray

	// Styles for loop output (previously in loop.go)
	waitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // dim gray
)

// PlainTextHandler writes events to an io.Writer using the same
// lipgloss-styled formatting that was previously written directly to os.Stderr.
type PlainTextHandler struct {
	W io.Writer
}

func (h *PlainTextHandler) Handle(event Event) {
	switch e := event.(type) {
	case ToolUse:
		h.handleToolUse(e)
	case AgentText:
		h.handleAgentText(e)
	case InvocationDone:
		h.handleInvocationDone(e)
	case IterationStart:
		h.handleIterationStart(e)
	case StoryStarted:
		h.handleStoryStarted(e)
	case QAPhaseStarted:
		h.handleQAPhaseStarted(e)
	case UsageLimitWait:
		h.handleUsageLimitWait(e)
	}
}

func (h *PlainTextHandler) handleToolUse(e ToolUse) {
	arrow := arrowStyle.Render("→")
	tool := toolStyle.Render(e.Name)
	if e.Detail != "" {
		path := pathStyle.Render(e.Detail)
		fmt.Fprintf(h.W, "  %s %s %s\n", arrow, tool, path)
	} else {
		fmt.Fprintf(h.W, "  %s %s\n", arrow, tool)
	}
}

func (h *PlainTextHandler) handleAgentText(e AgentText) {
	lines := strings.Split(strings.TrimSpace(e.Text), "\n")
	fmt.Fprintln(h.W)
	for _, line := range lines {
		styled := textStyle.Render(line)
		fmt.Fprintf(h.W, "  %s\n", styled)
	}
	fmt.Fprintln(h.W)
}

func (h *PlainTextHandler) handleInvocationDone(e InvocationDone) {
	durationSec := e.DurationMS / 1000
	check := successStyle.Render("✓")
	info := dimStyle.Render(fmt.Sprintf("(%d turns, %ds)", e.NumTurns, durationSec))
	fmt.Fprintf(h.W, "  %s Done %s\n", check, info)
}

func (h *PlainTextHandler) handleIterationStart(e IterationStart) {
	fmt.Fprintf(h.W, "[loop] iteration %d/%d\n", e.Iteration, e.MaxIterations)
}

func (h *PlainTextHandler) handleStoryStarted(e StoryStarted) {
	fmt.Fprintf(h.W, "[loop] working on %s: %s\n", e.StoryID, e.Title)
}

func (h *PlainTextHandler) handleQAPhaseStarted(e QAPhaseStarted) {
	fmt.Fprintf(h.W, "[loop] all stories pass — running QA %s\n", e.Phase)
}

func (h *PlainTextHandler) handleUsageLimitWait(e UsageLimitWait) {
	icon := waitStyle.Render("⏳")
	msg := waitStyle.Render("Usage limit reached")
	detail := infoStyle.Render(fmt.Sprintf("— waiting %s (until %s)",
		e.WaitDuration, e.ResetAt.Format("3:04pm MST")))
	fmt.Fprintf(h.W, "\n  %s %s %s\n\n", icon, msg, detail)
}
