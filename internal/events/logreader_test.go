package events

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEvent(t *testing.T, path string, evt Event) {
	t.Helper()
	data, err := MarshalEvent(evt)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

func collectEvents(ch <-chan Event, timeout time.Duration) []Event {
	var result []Event
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return result
			}
			result = append(result, evt)
		case <-deadline:
			return result
		}
	}
}

func TestLogReader_ReadsExistingFiles(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write two events to a log file.
	logFile := filepath.Join(logsDir, "startup-20260206T120000Z.jsonl")
	writeEvent(t, logFile, IterationStart{Iteration: 1, MaxIterations: 5})
	writeEvent(t, logFile, StoryStarted{StoryID: "US-001", Title: "First"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	// Give it time to read then cancel.
	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if _, ok := events[0].(IterationStart); !ok {
		t.Errorf("expected IterationStart, got %T", events[0])
	}
	if ss, ok := events[1].(StoryStarted); !ok || ss.StoryID != "US-001" {
		t.Errorf("expected StoryStarted US-001, got %T %v", events[1], events[1])
	}
}

func TestLogReader_ReadsMultipleFilesInOrder(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write files with different prefixes. Set explicit mod times to
	// ensure chronological ordering is by modification time, not name.
	baseTime := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)

	file1 := filepath.Join(logsDir, "startup-20260206T100000Z.jsonl")
	writeEvent(t, file1, IterationStart{Iteration: 1, MaxIterations: 5})
	os.Chtimes(file1, baseTime, baseTime)

	file2 := filepath.Join(logsDir, "US-001-20260206T100100Z.jsonl")
	writeEvent(t, file2, StoryStarted{StoryID: "US-001", Title: "First"})
	os.Chtimes(file2, baseTime.Add(time.Minute), baseTime.Add(time.Minute))

	file3 := filepath.Join(logsDir, "US-002-20260206T100200Z.jsonl")
	writeEvent(t, file3, StoryStarted{StoryID: "US-002", Title: "Second"})
	os.Chtimes(file3, baseTime.Add(2*time.Minute), baseTime.Add(2*time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Verify order: startup event first, then US-001, then US-002
	if _, ok := events[0].(IterationStart); !ok {
		t.Errorf("expected event[0]=IterationStart, got %T", events[0])
	}
	if ss, ok := events[1].(StoryStarted); !ok || ss.StoryID != "US-001" {
		t.Errorf("expected event[1]=StoryStarted US-001, got %T %v", events[1], events[1])
	}
	if ss, ok := events[2].(StoryStarted); !ok || ss.StoryID != "US-002" {
		t.Errorf("expected event[2]=StoryStarted US-002, got %T %v", events[2], events[2])
	}
}

func TestLogReader_TailsNewLines(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logFile := filepath.Join(logsDir, "startup-20260206T120000Z.jsonl")
	writeEvent(t, logFile, IterationStart{Iteration: 1, MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	// Wait for initial read
	time.Sleep(300 * time.Millisecond)

	// Append a new line
	writeEvent(t, logFile, StoryStarted{StoryID: "US-001", Title: "New"})

	// Wait for tail to pick it up
	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (initial + tailed), got %d", len(events))
	}
	if ss, ok := events[1].(StoryStarted); !ok || ss.StoryID != "US-001" {
		t.Errorf("expected tailed event StoryStarted US-001, got %T %v", events[1], events[1])
	}
}

func TestLogReader_DetectsNewFiles(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logFile1 := filepath.Join(logsDir, "startup-20260206T120000Z.jsonl")
	writeEvent(t, logFile1, IterationStart{Iteration: 1, MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	// Wait for initial read
	time.Sleep(300 * time.Millisecond)

	// Create a new file
	logFile2 := filepath.Join(logsDir, "US-001-20260206T120100Z.jsonl")
	writeEvent(t, logFile2, StoryStarted{StoryID: "US-001", Title: "New File"})

	// Wait for detection
	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if ss, ok := events[1].(StoryStarted); !ok || ss.StoryID != "US-001" {
		t.Errorf("expected new file event StoryStarted US-001, got %T %v", events[1], events[1])
	}
}

func TestLogReader_SkipsCorruptLines(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logFile := filepath.Join(logsDir, "test.jsonl")
	// Write corrupt line first
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not valid json\n")
	f.WriteString("{\"type\":\"unknown_garbage\"}\n")
	f.WriteString("\n") // blank line
	f.Close()

	// Then a valid event
	writeEvent(t, logFile, IterationStart{Iteration: 1, MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 1 {
		t.Fatalf("expected 1 valid event (corrupt lines skipped), got %d", len(events))
	}
	if _, ok := events[0].(IterationStart); !ok {
		t.Errorf("expected IterationStart, got %T", events[0])
	}
}

func TestLogReader_StopsOnContextCancellation(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	done := make(chan struct{})
	go func() {
		lr.Run(ctx)
		close(done)
	}()

	// Cancel immediately
	cancel()

	// Run should return quickly and close the channel
	select {
	case <-done:
		// Good — Run exited
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected events channel to be closed after Run returns")
	}
}

func TestLogReader_HandlesEmptyLogsDir(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	// Let it poll a couple times
	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty dir, got %d", len(events))
	}
}

func TestLogReader_HandlesMissingLogsDir(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "nonexistent", "logs")

	ctx, cancel := context.WithCancel(context.Background())

	lr := NewLogReader(logsDir)
	ch := lr.Events()

	go lr.Run(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()

	events := collectEvents(ch, 1*time.Second)
	if len(events) != 0 {
		t.Errorf("expected 0 events from missing dir, got %d", len(events))
	}
}
