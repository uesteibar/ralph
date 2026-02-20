package invoker_test

import (
	"context"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/events"
)

// mockEventInvoker implements EventInvoker for testing.
type mockEventInvoker struct {
	capturedHandler  events.EventHandler
	capturedPrompt   string
	capturedDir      string
	capturedMaxTurns int
}

func (m *mockEventInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	m.capturedPrompt = prompt
	m.capturedDir = dir
	m.capturedMaxTurns = maxTurns
	m.capturedHandler = handler
	return "mock response", nil
}

// mockHandler implements events.EventHandler for testing.
type mockHandler struct {
	handled []events.Event
}

func (m *mockHandler) Handle(e events.Event) {
	m.handled = append(m.handled, e)
}

func TestEventInvoker_InterfaceSatisfaction(t *testing.T) {
	var ei invoker.EventInvoker = &mockEventInvoker{}
	if ei == nil {
		t.Fatal("mock should satisfy EventInvoker interface")
	}
}

func TestEventInvoker_PassesHandlerThrough(t *testing.T) {
	mock := &mockEventInvoker{}
	handler := &mockHandler{}

	result, err := mock.InvokeWithEvents(context.Background(), "test prompt", "/work/dir", 10, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "mock response" {
		t.Errorf("expected 'mock response', got %q", result)
	}
	if mock.capturedPrompt != "test prompt" {
		t.Errorf("expected prompt 'test prompt', got %q", mock.capturedPrompt)
	}
	if mock.capturedDir != "/work/dir" {
		t.Errorf("expected dir '/work/dir', got %q", mock.capturedDir)
	}
	if mock.capturedMaxTurns != 10 {
		t.Errorf("expected maxTurns 10, got %d", mock.capturedMaxTurns)
	}
	if mock.capturedHandler != handler {
		t.Error("expected handler to be passed through")
	}
}

func TestEventInvoker_NilHandlerAccepted(t *testing.T) {
	mock := &mockEventInvoker{}

	_, err := mock.InvokeWithEvents(context.Background(), "prompt", "/dir", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.capturedHandler != nil {
		t.Error("expected nil handler to be passed through as nil")
	}
}
