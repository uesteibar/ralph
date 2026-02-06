package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
)

// Handler implements events.EventHandler by converting events into
// BubbleTea messages and sending them to the running program.
// Events arrive from the loop goroutine; p.Send is goroutine-safe.
type Handler struct {
	program *tea.Program
}

// NewHandler creates a TUI event handler that sends events to the given program.
func NewHandler(p *tea.Program) *Handler {
	return &Handler{program: p}
}

func (h *Handler) Handle(event events.Event) {
	if h.program != nil {
		h.program.Send(eventMsg{event: event})
	}
}
