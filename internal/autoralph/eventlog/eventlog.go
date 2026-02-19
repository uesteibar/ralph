package eventlog

import (
	"fmt"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/events"
)

// Handler captures AI events to the activity log and broadcasts them via a
// callback. It implements events.EventHandler.
type Handler struct {
	db           *db.DB
	issueID      string
	upstream     events.EventHandler
	onBuildEvent func(issueID, detail string)
}

// New creates a Handler that logs events for the given issue. The upstream
// handler and onBuildEvent callback are optional (nil-safe).
func New(database *db.DB, issueID string, upstream events.EventHandler, onBuildEvent func(issueID, detail string)) *Handler {
	return &Handler{
		db:           database,
		issueID:      issueID,
		upstream:     upstream,
		onBuildEvent: onBuildEvent,
	}
}

// Handle processes an event: formats it, logs non-empty results to the DB,
// invokes the onBuildEvent callback, and forwards to the upstream handler.
func (h *Handler) Handle(e events.Event) {
	detail := FormatDetail(e)
	if detail != "" {
		_ = h.db.LogActivity(h.issueID, "build_event", "", "", detail)

		if h.onBuildEvent != nil {
			h.onBuildEvent(h.issueID, detail)
		}
	}

	if h.upstream != nil {
		h.upstream.Handle(e)
	}
}

// FormatDetail converts an event to a human-readable string for the activity
// log. Returns an empty string for events that should not be logged.
func FormatDetail(e events.Event) string {
	switch ev := e.(type) {
	case events.ToolUse:
		if ev.Detail != "" {
			return fmt.Sprintf("→ %s %s", ev.Name, ev.Detail)
		}
		return fmt.Sprintf("→ %s", ev.Name)
	case events.IterationStart:
		return fmt.Sprintf("Iteration %d/%d started", ev.Iteration, ev.MaxIterations)
	case events.StoryStarted:
		return fmt.Sprintf("Story %s: %s", ev.StoryID, ev.Title)
	case events.QAPhaseStarted:
		return fmt.Sprintf("QA phase: %s", ev.Phase)
	case events.LogMessage:
		return fmt.Sprintf("[%s] %s", ev.Level, ev.Message)
	case events.AgentText:
		return ev.Text
	case events.InvocationDone:
		return fmt.Sprintf("Invocation done: %d turns in %dms", ev.NumTurns, ev.DurationMS)
	default:
		return ""
	}
}
