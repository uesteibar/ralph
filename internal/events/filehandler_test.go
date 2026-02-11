package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileHandler_ImplementsEventHandler(t *testing.T) {
	var h EventHandler = &FileHandler{logsDir: t.TempDir()}
	_ = h
}

func TestFileHandler_StartsWithStartupFile(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(ToolUse{Name: "Read", Detail: "file.go"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if !strings.HasPrefix(filepath.Base(files[0]), "startup-") {
		t.Errorf("expected startup- prefix, got %s", filepath.Base(files[0]))
	}

	lines := readLines(t, files[0])
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	e, err := UnmarshalEvent([]byte(lines[0]))
	if err != nil {
		t.Fatalf("UnmarshalEvent: %v", err)
	}
	tu, ok := e.(ToolUse)
	if !ok {
		t.Fatalf("expected ToolUse, got %T", e)
	}
	if tu.Name != "Read" {
		t.Errorf("expected Name=Read, got %s", tu.Name)
	}
}

func TestFileHandler_StoryStarted_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(StoryStarted{StoryID: "US-001", Title: "Build auth"})
	h.Handle(ToolUse{Name: "Edit"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	name := filepath.Base(files[0])
	if !strings.HasPrefix(name, "US-001-") {
		t.Errorf("expected US-001- prefix, got %s", name)
	}
	if !strings.HasSuffix(name, ".jsonl") {
		t.Errorf("expected .jsonl suffix, got %s", name)
	}

	lines := readLines(t, files[0])
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (StoryStarted + ToolUse), got %d", len(lines))
	}
}

func TestFileHandler_QAPhaseStarted_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(QAPhaseStarted{Phase: "verification"})
	h.Handle(ToolUse{Name: "Bash"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	name := filepath.Base(files[0])
	if !strings.HasPrefix(name, "QA-verification-") {
		t.Errorf("expected QA-verification- prefix, got %s", name)
	}
}

func TestFileHandler_MultiplePhases_CreatesMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	callCount := 0
	timestamps := []time.Time{
		time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 6, 10, 5, 0, 0, time.UTC),
		time.Date(2026, 2, 6, 10, 10, 0, 0, time.UTC),
	}
	h := newFileHandler(dir, func() time.Time {
		ts := timestamps[callCount%len(timestamps)]
		callCount++
		return ts
	})

	// Events before any story → startup file
	h.Handle(IterationStart{Iteration: 1, MaxIterations: 10})
	// Story starts → new file
	h.Handle(StoryStarted{StoryID: "US-001", Title: "Auth"})
	h.Handle(ToolUse{Name: "Read"})
	// QA starts → new file
	h.Handle(QAPhaseStarted{Phase: "verification"})
	h.Handle(ToolUse{Name: "Bash"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(files), files)
	}

	// Check file names (sorted alphabetically by filepath.Glob)
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}

	hasStartup := false
	hasStory := false
	hasQA := false
	for _, n := range names {
		if strings.HasPrefix(n, "startup-") {
			hasStartup = true
		}
		if strings.HasPrefix(n, "US-001-") {
			hasStory = true
		}
		if strings.HasPrefix(n, "QA-verification-") {
			hasQA = true
		}
	}
	if !hasStartup {
		t.Error("expected startup file")
	}
	if !hasStory {
		t.Error("expected US-001 story file")
	}
	if !hasQA {
		t.Error("expected QA-verification file")
	}
}

func TestFileHandler_CreatesLogsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	ts := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(ToolUse{Name: "Read"})
	h.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected logs directory to be created")
	}
}

func TestFileHandler_TimestampInFilename(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 2, 6, 10, 30, 45, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(StoryStarted{StoryID: "US-002", Title: "Test"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	name := filepath.Base(files[0])
	if !strings.Contains(name, "20260206T103045Z") {
		t.Errorf("expected timestamp in filename, got %s", name)
	}
}

func TestFileHandler_EventsAreValidJSON(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	h := newFileHandler(dir, func() time.Time { return ts })

	h.Handle(StoryStarted{StoryID: "US-001", Title: "Auth"})
	h.Handle(ToolUse{Name: "Read", Detail: "main.go"})
	h.Handle(AgentText{Text: "Hello"})
	h.Handle(InvocationDone{NumTurns: 3, DurationMS: 5000})
	h.Close()

	files := listJSONLFiles(t, dir)
	lines := readLines(t, files[0])
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	for i, line := range lines {
		_, err := UnmarshalEvent([]byte(line))
		if err != nil {
			t.Errorf("line %d: UnmarshalEvent failed: %v\nline: %s", i, err, line)
		}
	}
}

func TestFileHandler_SecondStoryStarted_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	callCount := 0
	timestamps := []time.Time{
		time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 6, 10, 5, 0, 0, time.UTC),
	}
	h := newFileHandler(dir, func() time.Time {
		ts := timestamps[callCount%len(timestamps)]
		callCount++
		return ts
	})

	h.Handle(StoryStarted{StoryID: "US-001", Title: "Auth"})
	h.Handle(ToolUse{Name: "Read"})
	h.Handle(StoryStarted{StoryID: "US-002", Title: "Logs"})
	h.Handle(ToolUse{Name: "Write"})
	h.Close()

	files := listJSONLFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
}

// --- helpers ---

func listJSONLFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
