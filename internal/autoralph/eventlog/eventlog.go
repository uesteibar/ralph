package eventlog

import (
	"fmt"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/events"
)

// UsageLimitSetter is called when a UsageLimitWait event is received.
type UsageLimitSetter interface {
	Set(resetAt time.Time)
}

// Handler captures AI events to the activity log and broadcasts them via a
// callback. It implements events.EventHandler.
type Handler struct {
	db           *db.DB
	issueID      string
	upstream     events.EventHandler
	onBuildEvent func(issueID, detail string)
	setter       UsageLimitSetter
}

// New creates a Handler that logs events for the given issue. The upstream
// handler, onBuildEvent callback, and usage limit setter are optional (nil-safe).
func New(database *db.DB, issueID string, upstream events.EventHandler, onBuildEvent func(issueID, detail string), setter UsageLimitSetter) *Handler {
	return &Handler{
		db:           database,
		issueID:      issueID,
		upstream:     upstream,
		onBuildEvent: onBuildEvent,
		setter:       setter,
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

	if ev, ok := e.(events.UsageLimitWait); ok && h.setter != nil {
		h.setter.Set(ev.ResetAt)
	}

	if ev, ok := e.(events.InvocationDone); ok && (ev.InputTokens > 0 || ev.OutputTokens > 0) {
		_ = h.db.IncrementTokens(h.issueID, ev.InputTokens, ev.OutputTokens)
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
		base := fmt.Sprintf("Invocation done: %d turns in %dms", ev.NumTurns, ev.DurationMS)
		if ev.InputTokens > 0 || ev.OutputTokens > 0 {
			return fmt.Sprintf("%s (%d in / %d out tokens)", base, ev.InputTokens, ev.OutputTokens)
		}
		return base
	case events.UsageLimitWait:
		return fmt.Sprintf("Usage limit hit, waiting %s (resets at %s)", ev.WaitDuration, ev.ResetAt)
	default:
		return ""
	}
}
