package invoker

import (
	"context"

	"github.com/uesteibar/ralph/internal/events"
)

// EventInvoker extends the standard AI invocation with event streaming support.
// Actions that need to stream tool-use events to the activity log and WebSocket
// hub use this interface instead of the per-package Invoker.
type EventInvoker interface {
	InvokeWithEvents(ctx context.Context, prompt, dir string, handler events.EventHandler) (string, error)
}
