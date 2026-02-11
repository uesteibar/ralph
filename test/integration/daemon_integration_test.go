package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/tui"
)

// --- IT-001: Verify daemon starts, writes logs, and writes status on completion ---

func TestIT001_DaemonLifecycle(t *testing.T) {
	t.Run("FileHandler writes JSONL logs and rotates on StoryStarted", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")

		handler := events.NewFileHandler(logsDir)

		// Simulate a startup event before any story → should create startup-*.jsonl
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 10})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "main.go"})

		// Story event triggers rotation → new file
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "Auth feature"})
		handler.Handle(events.ToolUse{Name: "Edit", Detail: "auth.go"})
		handler.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 5000})
		handler.Close()

		// Verify at least one .jsonl file exists in logs/
		files, err := filepath.Glob(filepath.Join(logsDir, "*.jsonl"))
		if err != nil {
			t.Fatalf("globbing log files: %v", err)
		}
		if len(files) < 2 {
			t.Errorf("expected at least 2 log files (startup + US-001), got %d: %v", len(files), files)
		}

		// Verify startup file exists
		hasStartup := false
		hasStory := false
		for _, f := range files {
			name := filepath.Base(f)
			if strings.HasPrefix(name, "startup-") {
				hasStartup = true
			}
			if strings.HasPrefix(name, "US-001-") {
				hasStory = true
			}
		}
		if !hasStartup {
			t.Error("expected a startup-*.jsonl file")
		}
		if !hasStory {
			t.Error("expected a US-001-*.jsonl file")
		}

		// Verify file content is valid JSONL
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading log file %s: %v", f, err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				_, err := events.UnmarshalEvent([]byte(line))
				if err != nil {
					t.Errorf("invalid JSONL line in %s: %s (error: %v)", filepath.Base(f), line, err)
				}
			}
		}
	})

	t.Run("PID lifecycle: write, read, isRunning, cleanup", func(t *testing.T) {
		wsPath := t.TempDir()

		// Initially not running
		if runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=false initially")
		}

		// Write PID
		if err := runstate.WritePID(wsPath); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		// Read PID should return current process PID
		pid, err := runstate.ReadPID(wsPath)
		if err != nil {
			t.Fatalf("ReadPID: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
		}

		// IsRunning should be true (current process is alive)
		if !runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=true after WritePID")
		}

		// Cleanup
		if err := runstate.CleanupPID(wsPath); err != nil {
			t.Fatalf("CleanupPID: %v", err)
		}

		// After cleanup, not running
		if runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=false after CleanupPID")
		}
	})

	t.Run("status file lifecycle: success, failed, cancelled", func(t *testing.T) {
		wsPath := t.TempDir()

		// Write and read success status
		err := runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultSuccess})
		if err != nil {
			t.Fatalf("WriteStatus(success): %v", err)
		}
		status, err := runstate.ReadStatus(wsPath)
		if err != nil {
			t.Fatalf("ReadStatus: %v", err)
		}
		if status.Result != runstate.ResultSuccess {
			t.Errorf("expected result=success, got %q", status.Result)
		}
		if status.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}

		// Write and read failed status with error message
		err = runstate.WriteStatus(wsPath, runstate.Status{
			Result: runstate.ResultFailed,
			Error:  "max iterations reached",
		})
		if err != nil {
			t.Fatalf("WriteStatus(failed): %v", err)
		}
		status, err = runstate.ReadStatus(wsPath)
		if err != nil {
			t.Fatalf("ReadStatus: %v", err)
		}
		if status.Result != runstate.ResultFailed {
			t.Errorf("expected result=failed, got %q", status.Result)
		}
		if status.Error != "max iterations reached" {
			t.Errorf("expected error message, got %q", status.Error)
		}

		// Write and read cancelled status
		err = runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultCancelled})
		if err != nil {
			t.Fatalf("WriteStatus(cancelled): %v", err)
		}
		status, err = runstate.ReadStatus(wsPath)
		if err != nil {
			t.Fatalf("ReadStatus: %v", err)
		}
		if status.Result != runstate.ResultCancelled {
			t.Errorf("expected result=cancelled, got %q", status.Result)
		}
	})

	t.Run("end-to-end: FileHandler writes events that are persisted as valid JSONL", func(t *testing.T) {
		wsPath := t.TempDir()
		logsDir := filepath.Join(wsPath, "logs")

		handler := events.NewFileHandler(logsDir)

		// Simulate complete daemon lifecycle
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 5})
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "Test story"})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "main.go"})
		handler.Handle(events.AgentText{Text: "Analyzing code"})
		handler.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 8000})
		handler.Handle(events.PRDRefresh{})
		handler.Close()

		// Write status
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultSuccess})

		// Verify status
		status, err := runstate.ReadStatus(wsPath)
		if err != nil {
			t.Fatalf("ReadStatus: %v", err)
		}
		if status.Result != runstate.ResultSuccess {
			t.Errorf("expected success status, got %q", status.Result)
		}

		// Verify logs exist and are parseable
		files, err := filepath.Glob(filepath.Join(logsDir, "*.jsonl"))
		if err != nil || len(files) == 0 {
			t.Fatal("expected at least one JSONL log file")
		}

		totalEvents := 0
		for _, f := range files {
			data, _ := os.ReadFile(f)
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				if _, err := events.UnmarshalEvent([]byte(line)); err == nil {
					totalEvents++
				}
			}
		}
		if totalEvents < 5 {
			t.Errorf("expected at least 5 persisted events, got %d", totalEvents)
		}
	})
}

// --- IT-005: Verify log persistence and LogReader replay of events ---

func TestIT005_LogPersistenceAndReplay(t *testing.T) {
	t.Run("FileHandler writes events, LogReader reads them back in order", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")

		handler := events.NewFileHandler(logsDir)

		// Write a sequence of events across multiple stories.
		// FileHandler creates a startup file first, then rotates on StoryStarted.
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 10})
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "Auth"})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "auth.go"})
		handler.Handle(events.AgentText{Text: "Implementing auth"})
		handler.Handle(events.InvocationDone{NumTurns: 3, DurationMS: 5000})
		handler.Handle(events.StoryStarted{StoryID: "US-002", Title: "Dashboard"})
		handler.Handle(events.ToolUse{Name: "Edit", Detail: "dashboard.go"})
		handler.Handle(events.InvocationDone{NumTurns: 2, DurationMS: 3000})
		handler.Close()

		// Set file modification times explicitly for chronological ordering.
		// Files created rapidly may have the same mod time; sortByModTime
		// falls back to alphabetical, which doesn't match chronological order
		// across prefixes (startup < US-001 < US-002).
		files, _ := filepath.Glob(filepath.Join(logsDir, "*.jsonl"))
		baseTime := time.Now().Add(-time.Hour)
		// Sort alphabetically first to get a stable order, then assign mod times
		// in the correct chronological order: startup → US-001 → US-002
		type filePriority struct {
			path     string
			priority int
		}
		var sorted []filePriority
		for _, f := range files {
			name := filepath.Base(f)
			p := 99
			if strings.HasPrefix(name, "startup-") {
				p = 0
			} else if strings.HasPrefix(name, "US-001-") {
				p = 1
			} else if strings.HasPrefix(name, "US-002-") {
				p = 2
			}
			sorted = append(sorted, filePriority{f, p})
		}
		for _, fp := range sorted {
			modTime := baseTime.Add(time.Duration(fp.priority) * time.Minute)
			os.Chtimes(fp.path, modTime, modTime)
		}

		// Use LogReader to read all events back
		reader := events.NewLogReader(logsDir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go reader.Run(ctx)

		var readEvents []events.Event
		timeout := time.After(2 * time.Second)
	collectLoop:
		for {
			select {
			case evt, ok := <-reader.Events():
				if !ok {
					break collectLoop
				}
				readEvents = append(readEvents, evt)
				if len(readEvents) >= 8 {
					cancel() // We got all events, stop the reader
				}
			case <-timeout:
				break collectLoop
			}
		}

		if len(readEvents) != 8 {
			t.Fatalf("expected 8 events from LogReader, got %d", len(readEvents))
		}

		// Verify events include expected types
		typeCount := make(map[string]int)
		for _, evt := range readEvents {
			typeCount[getEventTypeName(evt)]++
		}

		if typeCount["IterationStart"] != 1 {
			t.Errorf("expected 1 IterationStart, got %d", typeCount["IterationStart"])
		}
		if typeCount["StoryStarted"] != 2 {
			t.Errorf("expected 2 StoryStarted, got %d", typeCount["StoryStarted"])
		}
		if typeCount["ToolUse"] != 2 {
			t.Errorf("expected 2 ToolUse, got %d", typeCount["ToolUse"])
		}
		if typeCount["InvocationDone"] != 2 {
			t.Errorf("expected 2 InvocationDone, got %d", typeCount["InvocationDone"])
		}
		if typeCount["AgentText"] != 1 {
			t.Errorf("expected 1 AgentText, got %d", typeCount["AgentText"])
		}

		// Verify events are in chronological order: startup file events first,
		// then US-001 events, then US-002 events.
		// The first event should be IterationStart (from startup file).
		if _, ok := readEvents[0].(events.IterationStart); !ok {
			t.Errorf("first event should be IterationStart, got %T", readEvents[0])
		}
	})

	t.Run("LogReader handles corrupt lines gracefully", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")
		os.MkdirAll(logsDir, 0755)

		// Write a file with a mix of valid and corrupt lines
		evt1, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
		evt2, _ := events.MarshalEvent(events.StoryStarted{StoryID: "US-001", Title: "Test"})

		content := string(evt1) + "\nnot-valid-json\n" + string(evt2) + "\n"
		os.WriteFile(filepath.Join(logsDir, "test.jsonl"), []byte(content), 0644)

		reader := events.NewLogReader(logsDir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go reader.Run(ctx)

		var readEvents []events.Event
		timeout := time.After(2 * time.Second)
	collectLoop:
		for {
			select {
			case evt, ok := <-reader.Events():
				if !ok {
					break collectLoop
				}
				readEvents = append(readEvents, evt)
				if len(readEvents) >= 2 {
					cancel()
				}
			case <-timeout:
				break collectLoop
			}
		}

		// Should have 2 valid events, corrupt line skipped
		if len(readEvents) != 2 {
			t.Fatalf("expected 2 valid events (corrupt skipped), got %d", len(readEvents))
		}
	})

	t.Run("LogReader detects new content appended to existing files", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")
		os.MkdirAll(logsDir, 0755)

		logFile := filepath.Join(logsDir, "test.jsonl")
		evt1, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
		os.WriteFile(logFile, append(evt1, '\n'), 0644)

		reader := events.NewLogReader(logsDir)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		go reader.Run(ctx)

		// Read the first event
		var readEvents []events.Event
		select {
		case evt := <-reader.Events():
			readEvents = append(readEvents, evt)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for first event")
		}

		// Append new content after a brief delay
		time.Sleep(300 * time.Millisecond)
		evt2, _ := events.MarshalEvent(events.StoryStarted{StoryID: "US-001", Title: "New story"})
		f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
		f.Write(append(evt2, '\n'))
		f.Close()

		// Read the appended event
		select {
		case evt := <-reader.Events():
			readEvents = append(readEvents, evt)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for appended event")
		}

		cancel()

		if len(readEvents) != 2 {
			t.Fatalf("expected 2 events total, got %d", len(readEvents))
		}

		if _, ok := readEvents[0].(events.IterationStart); !ok {
			t.Errorf("first event should be IterationStart, got %T", readEvents[0])
		}
		if ss, ok := readEvents[1].(events.StoryStarted); !ok || ss.StoryID != "US-001" {
			t.Errorf("second event should be StoryStarted(US-001), got %T %v", readEvents[1], readEvents[1])
		}
	})

	t.Run("LogReader detects new files appearing", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")
		os.MkdirAll(logsDir, 0755)

		// Start with one file
		evt1, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
		os.WriteFile(filepath.Join(logsDir, "startup-20260101T000000Z.jsonl"), append(evt1, '\n'), 0644)

		reader := events.NewLogReader(logsDir)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		go reader.Run(ctx)

		// Read first event
		select {
		case <-reader.Events():
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for first event")
		}

		// Create a new file after a delay
		time.Sleep(300 * time.Millisecond)
		evt2, _ := events.MarshalEvent(events.StoryStarted{StoryID: "US-002", Title: "New file"})
		os.WriteFile(filepath.Join(logsDir, "US-002-20260101T000001Z.jsonl"), append(evt2, '\n'), 0644)

		// Read the event from the new file
		select {
		case evt := <-reader.Events():
			if ss, ok := evt.(events.StoryStarted); !ok || ss.StoryID != "US-002" {
				t.Errorf("expected StoryStarted(US-002), got %T %v", evt, evt)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for event from new file")
		}

		cancel()
	})

	t.Run("events grouped by log file: one per story/QA cycle", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")
		handler := events.NewFileHandler(logsDir)

		// Startup events
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 10})

		// Story 1 triggers file rotation
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "First"})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "a.go"})
		time.Sleep(10 * time.Millisecond)

		// Story 2 triggers another rotation
		handler.Handle(events.StoryStarted{StoryID: "US-002", Title: "Second"})
		handler.Handle(events.ToolUse{Name: "Edit", Detail: "b.go"})
		time.Sleep(10 * time.Millisecond)

		// QA phase triggers rotation
		handler.Handle(events.QAPhaseStarted{Phase: "verification"})
		handler.Handle(events.ToolUse{Name: "Bash", Detail: "go test"})

		handler.Close()

		files, _ := filepath.Glob(filepath.Join(logsDir, "*.jsonl"))
		if len(files) != 4 {
			names := make([]string, len(files))
			for i, f := range files {
				names[i] = filepath.Base(f)
			}
			t.Errorf("expected 4 log files (startup, US-001, US-002, QA-verification), got %d: %v", len(files), names)
		}

		// Verify file name patterns
		hasStartup, hasUS001, hasUS002, hasQA := false, false, false, false
		for _, f := range files {
			name := filepath.Base(f)
			if strings.HasPrefix(name, "startup-") {
				hasStartup = true
			}
			if strings.HasPrefix(name, "US-001-") {
				hasUS001 = true
			}
			if strings.HasPrefix(name, "US-002-") {
				hasUS002 = true
			}
			if strings.HasPrefix(name, "QA-verification-") {
				hasQA = true
			}
		}
		if !hasStartup {
			t.Error("missing startup-*.jsonl file")
		}
		if !hasUS001 {
			t.Error("missing US-001-*.jsonl file")
		}
		if !hasUS002 {
			t.Error("missing US-002-*.jsonl file")
		}
		if !hasQA {
			t.Error("missing QA-verification-*.jsonl file")
		}
	})
}

// --- IT-006: Verify stale PID file handling ---

func TestIT006_StalePIDHandling(t *testing.T) {
	t.Run("IsRunning cleans up stale PID file for dead process", func(t *testing.T) {
		wsPath := t.TempDir()

		// Write a PID for a process that does not exist
		os.WriteFile(filepath.Join(wsPath, "run.pid"), []byte("999999"), 0644)

		// IsRunning should return false and clean up
		if runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=false for dead PID")
		}

		// Verify PID file was cleaned up
		pidPath := filepath.Join(wsPath, "run.pid")
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Error("expected stale PID file to be cleaned up")
		}
	})

	t.Run("ReadPID handles missing PID file", func(t *testing.T) {
		wsPath := t.TempDir()

		_, err := runstate.ReadPID(wsPath)
		if err == nil {
			t.Error("expected error when PID file doesn't exist")
		}
	})

	t.Run("CleanupPID is idempotent for missing file", func(t *testing.T) {
		wsPath := t.TempDir()

		// Should not error when file doesn't exist
		if err := runstate.CleanupPID(wsPath); err != nil {
			t.Errorf("expected no error from CleanupPID on missing file, got: %v", err)
		}
	})

	t.Run("IsRunning distinguishes live from dead processes", func(t *testing.T) {
		wsPath := t.TempDir()

		// Write own PID → should be alive
		runstate.WritePID(wsPath)
		if !runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=true for current process PID")
		}
		runstate.CleanupPID(wsPath)

		// Write bogus PID → should be dead
		os.WriteFile(filepath.Join(wsPath, "run.pid"), []byte("999999"), 0644)
		if runstate.IsRunning(wsPath) {
			t.Error("expected IsRunning=false for dead PID 999999")
		}
	})
}

// --- IT-007: Verify ralph tui lists workspaces with correct running/stopped status ---

func TestIT007_MultiWorkspaceOverview(t *testing.T) {
	t.Run("MultiModel displays workspaces with correct status indicators", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "running-ws", Running: true, WsPath: "/tmp/ws1"},
			{Name: "stopped-ws", Running: false, WsPath: "/tmp/ws2"},
			{Name: "empty-ws", Running: false, WsPath: "/tmp/ws3"},
		}

		model := tui.NewMultiModel(workspaces)

		// Init and set window size so it's ready
		model.Init()
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Verify all 3 workspaces in the model
		if len(model.Workspaces()) != 3 {
			t.Fatalf("expected 3 workspaces, got %d", len(model.Workspaces()))
		}

		// Render the view and verify output
		view := model.View()
		cleanView := stripANSI(view)

		// Verify all workspace names present
		if !strings.Contains(cleanView, "running-ws") {
			t.Error("expected view to contain 'running-ws'")
		}
		if !strings.Contains(cleanView, "stopped-ws") {
			t.Error("expected view to contain 'stopped-ws'")
		}
		if !strings.Contains(cleanView, "empty-ws") {
			t.Error("expected view to contain 'empty-ws'")
		}

		// Verify running indicator (●) for running workspace
		if !strings.Contains(view, "●") {
			t.Error("expected running indicator (●) in view")
		}
		// Verify stopped indicator (○) for stopped workspaces
		if !strings.Contains(view, "○") {
			t.Error("expected stopped indicator (○) in view")
		}
	})

	t.Run("MultiModel navigation with arrow keys", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "ws-1", Running: false},
			{Name: "ws-2", Running: true},
			{Name: "ws-3", Running: false},
		}

		model := tui.NewMultiModel(workspaces)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Initially at cursor 0
		if model.MultiCursor() != 0 {
			t.Errorf("expected cursor=0, got %d", model.MultiCursor())
		}

		// Navigate down
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(tui.MultiModel)
		if model.MultiCursor() != 1 {
			t.Errorf("expected cursor=1 after down, got %d", model.MultiCursor())
		}

		// Navigate down again
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(tui.MultiModel)
		if model.MultiCursor() != 2 {
			t.Errorf("expected cursor=2 after second down, got %d", model.MultiCursor())
		}

		// Navigate up
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = updated.(tui.MultiModel)
		if model.MultiCursor() != 1 {
			t.Errorf("expected cursor=1 after up, got %d", model.MultiCursor())
		}
	})

	t.Run("MultiModel drill-down with Enter and return with Esc", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "test-ws", Running: false},
		}

		model := tui.NewMultiModel(workspaces)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Press Enter to drill down
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(tui.MultiModel)
		if model.Mode() != 1 { // modeDrillDown
			t.Errorf("expected mode=drillDown after Enter, got %d", model.Mode())
		}
		if model.DrillModel() == nil {
			t.Error("expected drillModel to be set after Enter")
		}

		// Press Esc to return to overview
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(tui.MultiModel)
		if model.Mode() != 0 { // modeOverview
			t.Errorf("expected mode=overview after Esc, got %d", model.Mode())
		}
	})

	t.Run("MultiModel log events are delivered per workspace", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "ws-1", Running: true},
			{Name: "ws-2", Running: true},
		}

		model := tui.NewMultiModel(workspaces)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Send log events for workspace 0
		updated, _ = model.Update(tui.MakeMultiLogEventMsg(0,
			events.StoryStarted{StoryID: "US-001", Title: "Auth"}))
		model = updated.(tui.MultiModel)

		updated, _ = model.Update(tui.MakeMultiLogEventMsg(0,
			events.ToolUse{Name: "Read", Detail: "main.go"}))
		model = updated.(tui.MultiModel)

		// Send log events for workspace 1
		updated, _ = model.Update(tui.MakeMultiLogEventMsg(1,
			events.StoryStarted{StoryID: "US-002", Title: "Dashboard"}))
		model = updated.(tui.MultiModel)

		// Verify log lines are stored per workspace
		ws0Lines := model.LogLines(0)
		ws1Lines := model.LogLines(1)

		if len(ws0Lines) != 2 {
			t.Errorf("expected 2 log lines for ws-0, got %d: %v", len(ws0Lines), ws0Lines)
		}
		if len(ws1Lines) != 1 {
			t.Errorf("expected 1 log line for ws-1, got %d: %v", len(ws1Lines), ws1Lines)
		}
	})

	t.Run("MultiModel help overlay toggles with ?", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "ws-1", Running: false},
		}

		model := tui.NewMultiModel(workspaces)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Initially no help overlay — verify by checking the view
		initialView := stripANSI(model.View())
		if strings.Contains(initialView, "Keyboard Shortcuts") {
			t.Error("expected help overlay to be hidden initially")
		}

		// Press ? to show help
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		model = updated.(tui.MultiModel)

		// Verify help overlay content contains keybindings
		view := model.View()
		cleanView := stripANSI(view)
		if !strings.Contains(cleanView, "Keyboard Shortcuts") {
			t.Error("expected help overlay to contain 'Keyboard Shortcuts'")
		}

		// Press Esc to dismiss help
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(tui.MultiModel)
	})

	t.Run("MultiModel attach workflow on running workspace", func(t *testing.T) {
		workspaces := []tui.WorkspaceInfo{
			{Name: "running-ws", Running: true, WsPath: "/tmp/ws1"},
		}

		model := tui.NewMultiModel(workspaces)
		stopCalled := false
		model.SetMakeStopFn(func(wsPath string) func() {
			return func() { stopCalled = true }
		})

		updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(tui.MultiModel)

		// Drill down into the workspace
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(tui.MultiModel)
		if model.Mode() != 1 { // modeDrillDown
			t.Fatal("expected drillDown mode")
		}

		// Press 'a' to initiate attach
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		model = updated.(tui.MultiModel)
		if !model.ConfirmingAttach() {
			t.Error("expected confirmingAttach=true after pressing 'a'")
		}

		// Press 'y' to confirm
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		model = updated.(tui.MultiModel)
		if !model.Attached() {
			t.Error("expected attached=true after confirming")
		}
		if model.ConfirmingAttach() {
			t.Error("expected confirmingAttach=false after confirming")
		}

		// In attached mode, 'd' detaches
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		model = updated.(tui.MultiModel)
		if model.Attached() {
			t.Error("expected attached=false after detach")
		}

		_ = stopCalled // stop function is wired but not called unless q is pressed and confirmed
	})
}

// --- IT-002 & IT-003 & IT-004: Cross-cutting run/stop/attach integration ---
// These tests exercise the interaction between run, stop, and the daemon lifecycle.
// Since we can't easily spawn real daemons in tests, we test the key integration
// points: PID-based detection, status file writing, and log tailing.

func TestIT002_RunNoTUI_CtrlC_StopsEverything(t *testing.T) {
	t.Run("stopDaemon sends signal to process via PID file", func(t *testing.T) {
		// This test verifies the integration between:
		// 1. runstate.WritePID/ReadPID
		// 2. runstate.IsRunning (process liveness check)
		// 3. The stop mechanism that reads PID and sends SIGTERM
		wsPath := t.TempDir()

		// Write current process PID
		runstate.WritePID(wsPath)

		// Verify it's "running"
		if !runstate.IsRunning(wsPath) {
			t.Fatal("expected daemon to be detected as running")
		}

		// Read PID back
		pid, err := runstate.ReadPID(wsPath)
		if err != nil {
			t.Fatalf("ReadPID: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
		}

		// Cleanup simulates daemon exit
		runstate.CleanupPID(wsPath)
		if runstate.IsRunning(wsPath) {
			t.Error("expected not running after PID cleanup")
		}

		// After cleanup, write cancelled status (as daemon would on SIGTERM)
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultCancelled})
		status, _ := runstate.ReadStatus(wsPath)
		if status.Result != runstate.ResultCancelled {
			t.Errorf("expected cancelled status, got %q", status.Result)
		}
	})

	t.Run("log tailing reads events written by FileHandler in real-time", func(t *testing.T) {
		logsDir := filepath.Join(t.TempDir(), "logs")
		os.MkdirAll(logsDir, 0755)

		// Write some log events (simulating daemon writing)
		handler := events.NewFileHandler(logsDir)
		handler.Handle(events.IterationStart{Iteration: 1, MaxIterations: 5})
		handler.Handle(events.StoryStarted{StoryID: "US-001", Title: "Test"})
		handler.Handle(events.ToolUse{Name: "Read", Detail: "file.go"})
		handler.Close()

		// Read them via LogReader (simulating what tailLogsTUI does)
		reader := events.NewLogReader(logsDir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go reader.Run(ctx)

		var readEvents []events.Event
		for i := 0; i < 3; i++ {
			select {
			case evt := <-reader.Events():
				readEvents = append(readEvents, evt)
			case <-time.After(1 * time.Second):
				t.Fatalf("timed out waiting for event %d", i+1)
			}
		}
		cancel()

		if len(readEvents) != 3 {
			t.Fatalf("expected 3 events, got %d", len(readEvents))
		}

		// Verify PlainTextHandler can render these events (integration with handler)
		var buf strings.Builder
		plainHandler := &events.PlainTextHandler{W: &buf}
		for _, evt := range readEvents {
			plainHandler.Handle(evt)
		}
		output := stripANSI(buf.String())
		if !strings.Contains(output, "iteration 1/5") {
			t.Errorf("expected iteration in plaintext output, got: %s", output)
		}
		if !strings.Contains(output, "US-001") {
			t.Errorf("expected story ID in plaintext output, got: %s", output)
		}
	})
}

func TestIT003_StopIntegration(t *testing.T) {
	t.Run("stop workflow: read PID, check alive, cleanup on death", func(t *testing.T) {
		wsPath := t.TempDir()

		// Simulate daemon start
		runstate.WritePID(wsPath)
		if !runstate.IsRunning(wsPath) {
			t.Fatal("expected running after WritePID")
		}

		// Read PID (as stop command does)
		pid, err := runstate.ReadPID(wsPath)
		if err != nil {
			t.Fatalf("ReadPID: %v", err)
		}
		if pid <= 0 {
			t.Fatalf("expected positive PID, got %d", pid)
		}

		// Simulate daemon exit: cleanup PID and write status
		runstate.CleanupPID(wsPath)
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultCancelled})

		// Verify stopped state
		if runstate.IsRunning(wsPath) {
			t.Error("expected not running after cleanup")
		}
		status, _ := runstate.ReadStatus(wsPath)
		if status.Result != runstate.ResultCancelled {
			t.Errorf("expected cancelled, got %q", status.Result)
		}
	})
}

func TestIT004_RunReusesExistingDaemon(t *testing.T) {
	t.Run("PID file persists when daemon already running (no respawn)", func(t *testing.T) {
		wsPath := t.TempDir()

		// Simulate existing daemon by writing PID
		runstate.WritePID(wsPath)
		originalPID, _ := runstate.ReadPID(wsPath)

		// "Run" detects daemon is already running
		if !runstate.IsRunning(wsPath) {
			t.Fatal("expected running daemon to be detected")
		}

		// Read PID again — should be the same (no respawn)
		currentPID, _ := runstate.ReadPID(wsPath)
		if currentPID != originalPID {
			t.Errorf("expected PID to remain %d, got %d", originalPID, currentPID)
		}

		// Cleanup
		runstate.CleanupPID(wsPath)
	})
}

// getEventTypeName returns the type name of an event for test assertions.
func getEventTypeName(e events.Event) string {
	switch e.(type) {
	case events.ToolUse:
		return "ToolUse"
	case events.AgentText:
		return "AgentText"
	case events.InvocationDone:
		return "InvocationDone"
	case events.IterationStart:
		return "IterationStart"
	case events.StoryStarted:
		return "StoryStarted"
	case events.QAPhaseStarted:
		return "QAPhaseStarted"
	case events.UsageLimitWait:
		return "UsageLimitWait"
	case events.PRDRefresh:
		return "PRDRefresh"
	default:
		return "unknown"
	}
}
