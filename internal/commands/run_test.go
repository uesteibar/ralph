package commands

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/runstate"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestRun_WorkspaceFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "no flag defaults to empty",
			args:     []string{},
			expected: "",
		},
		{
			name:     "--workspace sets name",
			args:     []string{"--workspace", "my-feature"},
			expected: "my-feature",
		},
		{
			name:     "--workspace=value sets name",
			args:     []string{"--workspace=my-feature"},
			expected: "my-feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			ws := fs.String("workspace", "", "Workspace name to run in")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if *ws != tt.expected {
				t.Errorf("expected workspace=%q, got %q", tt.expected, *ws)
			}
		})
	}
}

func TestRun_NoTUIFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no flag defaults to false",
			args:     []string{},
			expected: false,
		},
		{
			name:     "--no-tui enables plain text",
			args:     []string{"--no-tui"},
			expected: true,
		},
		{
			name:     "--no-tui=true enables plain text",
			args:     []string{"--no-tui=true"},
			expected: true,
		},
		{
			name:     "--no-tui=false keeps TUI",
			args:     []string{"--no-tui=false"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			noTUI := fs.Bool("no-tui", false, "Disable TUI")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if *noTUI != tt.expected {
				t.Errorf("expected no-tui=%v, got %v", tt.expected, *noTUI)
			}
		})
	}
}

func TestRun_InWorkspace_CorrectWorkDirAndPRDPath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	wsName := "my-feature"
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}

	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	prdContent := `{
  "project": "test",
  "branchName": "ralph/my-feature",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": true}
  ]
}`
	prdPath := filepath.Join(wsDir, "prd.json")
	if err := os.WriteFile(prdPath, []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	wc, err := workspace.ResolveWorkContext(wsName, "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	if wc.WorkDir != treeDir {
		t.Errorf("expected WorkDir=%s, got %s", treeDir, wc.WorkDir)
	}
	if wc.PRDPath != prdPath {
		t.Errorf("expected PRDPath=%s, got %s", prdPath, wc.PRDPath)
	}
	if wc.Name != wsName {
		t.Errorf("expected Name=%s, got %s", wsName, wc.Name)
	}
}

func TestRun_InBase_WarningPrinted(t *testing.T) {
	t.Setenv("RALPH_WORKSPACE", "")
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	stateDir := filepath.Join(dir, ".ralph", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	prdContent := `{
  "project": "test",
  "branchName": "test/feature",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": false}
  ]
}`
	if err := os.WriteFile(filepath.Join(stateDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Mock spawn+wait so Run doesn't try to start a real daemon.
	origSpawn := spawnDaemonFn
	spawnDaemonFn = func(name string, maxIter int) (*exec.Cmd, error) {
		return nil, nil
	}
	defer func() { spawnDaemonFn = origSpawn }()

	origWait := waitForPIDFileFn
	waitForPIDFileFn = func(wsPath string, timeout time.Duration) error {
		return nil
	}
	defer func() { waitForPIDFileFn = origWait }()

	// Capture stderr
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	// tailLogsPlainText will exit immediately since no PID file exists
	// (daemon not running).
	runErr := Run([]string{"--max-iterations=1", "--no-tui"})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	_ = runErr

	if !strings.Contains(stderr, "Running in base") {
		t.Errorf("expected stderr to contain 'Running in base', got: %s", stderr)
	}
	if !strings.Contains(stderr, "Consider: ralph workspaces new") {
		t.Errorf("expected stderr to contain workspace hint, got: %s", stderr)
	}
}

func TestRun_MissingPRD_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err = Run([]string{"--max-iterations=1"})
	if err == nil {
		t.Fatal("expected error when PRD does not exist, got nil")
	}

	if !strings.Contains(err.Error(), "PRD not found") {
		t.Errorf("expected error to contain 'PRD not found', got: %v", err)
	}
}

func TestRun_WithWorkspaceFlag(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	wsName := "test-ws"
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}
	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	prdContent := `{
  "project": "test",
  "branchName": "ralph/test-ws",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": false}
  ]
}`
	if err := os.WriteFile(filepath.Join(wsDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	wc, err := workspace.ResolveWorkContext(wsName, "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	if wc.Name != wsName {
		t.Errorf("expected workspace name=%q, got %q", wsName, wc.Name)
	}
	expectedWorkDir := treeDir
	if wc.WorkDir != expectedWorkDir {
		t.Errorf("expected WorkDir=%s, got %s", expectedWorkDir, wc.WorkDir)
	}
	expectedPRD := filepath.Join(wsDir, "prd.json")
	if wc.PRDPath != expectedPRD {
		t.Errorf("expected PRDPath=%s, got %s", expectedPRD, wc.PRDPath)
	}
}

func TestRun_WorkspaceMissingPRD_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	wsName := "no-prd-ws"
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}
	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err = Run([]string{"--workspace", wsName, "--max-iterations=1"})
	if err == nil {
		t.Fatal("expected error when workspace PRD does not exist")
	}

	if !strings.Contains(err.Error(), "PRD not found") {
		t.Errorf("expected error to contain 'PRD not found', got: %v", err)
	}
}

func TestRun_AllStoriesPass_PrintsDoneMessage(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	wsName := "done-ws"
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}
	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	prdContent := `{
  "project": "test",
  "branchName": "ralph/done-ws",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": true}
  ],
  "integrationTests": [
    {"id": "IT-001", "description": "Test works", "steps": ["Run test"], "passes": true}
  ]
}`
	if err := os.WriteFile(filepath.Join(wsDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err = Run([]string{"--workspace", wsName})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error when all stories pass, got: %v", err)
	}

	if !strings.Contains(stderr, "All stories and integration tests pass") {
		t.Errorf("expected stderr to contain done message, got: %s", stderr)
	}
	if !strings.Contains(stderr, "squash and merge your changes back to base") {
		t.Errorf("expected stderr to mention squash and merge, got: %s", stderr)
	}
}

// --- New tests for US-005: daemon spawning and attach ---

func TestRun_SpawnsDaemon_WhenNotRunning(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "spawn-test"
	setupWorkspace(t, dir, wsName, notPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	var spawnedName string
	var spawnedMaxIter int
	origSpawn := spawnDaemonFn
	spawnDaemonFn = func(name string, maxIter int) (*exec.Cmd, error) {
		spawnedName = name
		spawnedMaxIter = maxIter
		return nil, nil
	}
	defer func() { spawnDaemonFn = origSpawn }()

	origWait := waitForPIDFileFn
	waitForPIDFileFn = func(path string, timeout time.Duration) error {
		// Return nil to indicate daemon started; no PID file written so
		// tailLogsPlainText will see daemon not running and exit immediately.
		return nil
	}
	defer func() { waitForPIDFileFn = origWait }()

	// Suppress stderr
	oldStderr := os.Stderr
	_, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Run([]string{"--workspace", wsName, "--max-iterations=5", "--no-tui"})
	wPipe.Close()
	os.Stderr = oldStderr

	_ = err

	if spawnedName != wsName {
		t.Errorf("expected spawn with workspace=%q, got %q", wsName, spawnedName)
	}
	if spawnedMaxIter != 5 {
		t.Errorf("expected spawn with maxIter=5, got %d", spawnedMaxIter)
	}
}

func TestRun_SkipsSpawn_WhenDaemonAlreadyRunning(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "already-running"
	setupWorkspace(t, dir, wsName, notPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	wsPath := workspace.WorkspacePath(dir, wsName)

	// Write current process PID to simulate a running daemon.
	if err := runstate.WritePID(wsPath); err != nil {
		t.Fatal(err)
	}

	spawnCalled := false
	origSpawn := spawnDaemonFn
	spawnDaemonFn = func(name string, maxIter int) (*exec.Cmd, error) {
		spawnCalled = true
		return nil, nil
	}
	defer func() { spawnDaemonFn = origSpawn }()

	// Capture stderr
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	// Clean up PID after a short delay so tailLogsPlainText exits.
	go func() {
		time.Sleep(300 * time.Millisecond)
		runstate.CleanupPID(wsPath)
	}()

	err := Run([]string{"--workspace", wsName, "--no-tui"})
	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	_ = err

	if spawnCalled {
		t.Error("spawn should not be called when daemon is already running")
	}

	if !strings.Contains(stderr, "daemon already running") {
		t.Errorf("expected 'daemon already running' message, got: %s", stderr)
	}
}

func TestRun_ErrorWhenDaemonFailsToStart(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "fail-start"
	setupWorkspace(t, dir, wsName, notPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	origSpawn := spawnDaemonFn
	spawnDaemonFn = func(name string, maxIter int) (*exec.Cmd, error) {
		return nil, nil
	}
	defer func() { spawnDaemonFn = origSpawn }()

	origWait := waitForPIDFileFn
	waitForPIDFileFn = func(path string, timeout time.Duration) error {
		return os.ErrDeadlineExceeded
	}
	defer func() { waitForPIDFileFn = origWait }()

	// Suppress stderr
	oldStderr := os.Stderr
	_, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Run([]string{"--workspace", wsName, "--no-tui"})
	wPipe.Close()
	os.Stderr = oldStderr

	if err == nil {
		t.Fatal("expected error when daemon fails to start")
	}
	if !strings.Contains(err.Error(), "daemon failed to start") {
		t.Errorf("expected 'daemon failed to start' error, got: %v", err)
	}
}

func TestRun_TailsLogs_AfterDaemonStops(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)
	wsName := "tail-test"
	setupWorkspace(t, dir, wsName, notPassingPRD(wsName))

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	wsPath := workspace.WorkspacePath(dir, wsName)

	origSpawn := spawnDaemonFn
	spawnDaemonFn = func(name string, maxIter int) (*exec.Cmd, error) {
		return nil, nil
	}
	defer func() { spawnDaemonFn = origSpawn }()

	origWait := waitForPIDFileFn
	waitForPIDFileFn = func(path string, timeout time.Duration) error {
		// Write PID and create a log file to verify tailing works.
		runstate.WritePID(wsPath)
		logsDir := filepath.Join(wsPath, "logs")
		os.MkdirAll(logsDir, 0755)
		evt, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
		os.WriteFile(filepath.Join(logsDir, "startup-20260206T120000Z.jsonl"),
			append(evt, '\n'), 0644)
		return nil
	}
	defer func() { waitForPIDFileFn = origWait }()

	// Write a status file and clean up PID after a short delay so tail loop exits.
	go func() {
		time.Sleep(400 * time.Millisecond)
		runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultSuccess})
		runstate.CleanupPID(wsPath)
	}()

	// Capture stderr
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := Run([]string{"--workspace", wsName, "--no-tui"})
	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify that log events were tailed to stderr
	if !strings.Contains(stderr, "iteration 1/5") {
		t.Errorf("expected 'iteration 1/5' in tailed output, got: %s", stderr)
	}
	// Verify success message printed
	if !strings.Contains(stderr, "All work complete") {
		t.Errorf("expected success message, got: %s", stderr)
	}
}

func TestReadNewLogEntries_ReadsJSONL(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	evt1, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
	evt2, _ := events.MarshalEvent(events.StoryStarted{StoryID: "US-001", Title: "Test story"})
	content := string(evt1) + "\n" + string(evt2) + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "startup-20260206T120000Z.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var handled []events.Event
	handler := &captureHandler{events: &handled}

	offsets := make(map[string]int64)
	readNewLogEntries(logsDir, offsets, handler)

	if len(handled) != 2 {
		t.Fatalf("expected 2 events, got %d", len(handled))
	}

	if _, ok := handled[0].(events.IterationStart); !ok {
		t.Errorf("expected first event to be IterationStart, got %T", handled[0])
	}
	if _, ok := handled[1].(events.StoryStarted); !ok {
		t.Errorf("expected second event to be StoryStarted, got %T", handled[1])
	}

	// Reading again with same offsets should yield no new events
	handled = nil
	readNewLogEntries(logsDir, offsets, handler)
	if len(handled) != 0 {
		t.Errorf("expected 0 new events on re-read, got %d", len(handled))
	}
}

func TestReadNewLogEntries_SkipsCorruptLines(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	evt, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
	content := "not-valid-json\n" + string(evt) + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "test.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var handled []events.Event
	handler := &captureHandler{events: &handled}

	offsets := make(map[string]int64)
	readNewLogEntries(logsDir, offsets, handler)

	if len(handled) != 1 {
		t.Fatalf("expected 1 event (corrupt line skipped), got %d", len(handled))
	}
}

func TestReadNewLogEntries_TracksOffset(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logFile := filepath.Join(logsDir, "test.jsonl")
	evt1, _ := events.MarshalEvent(events.IterationStart{Iteration: 1, MaxIterations: 5})
	if err := os.WriteFile(logFile, append(evt1, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	var handled []events.Event
	handler := &captureHandler{events: &handled}
	offsets := make(map[string]int64)

	readNewLogEntries(logsDir, offsets, handler)
	if len(handled) != 1 {
		t.Fatalf("expected 1 event, got %d", len(handled))
	}

	// Append more data
	evt2, _ := events.MarshalEvent(events.StoryStarted{StoryID: "US-002", Title: "Another"})
	f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	f.Write(append(evt2, '\n'))
	f.Close()

	// Second read: only the new event
	handled = nil
	readNewLogEntries(logsDir, offsets, handler)
	if len(handled) != 1 {
		t.Fatalf("expected 1 new event, got %d", len(handled))
	}
	if ss, ok := handled[0].(events.StoryStarted); !ok || ss.StoryID != "US-002" {
		t.Errorf("expected StoryStarted US-002, got %T %v", handled[0], handled[0])
	}
}

func TestPrintDaemonResult_Success(t *testing.T) {
	wsPath := t.TempDir()
	runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultSuccess})

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := printDaemonResult(wsPath)

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stderr, "All work complete") {
		t.Errorf("expected success message, got: %s", stderr)
	}
}

func TestPrintDaemonResult_Cancelled(t *testing.T) {
	wsPath := t.TempDir()
	runstate.WriteStatus(wsPath, runstate.Status{Result: runstate.ResultCancelled})

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := printDaemonResult(wsPath)

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stderr, "Stopped") {
		t.Errorf("expected stopped message, got: %s", stderr)
	}
}

func TestPrintDaemonResult_Failed(t *testing.T) {
	wsPath := t.TempDir()
	runstate.WriteStatus(wsPath, runstate.Status{
		Result: runstate.ResultFailed,
		Error:  "max iterations reached",
	})

	// Suppress stderr
	oldStderr := os.Stderr
	_, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err := printDaemonResult(wsPath)

	wPipe.Close()
	os.Stderr = oldStderr

	if err == nil {
		t.Fatal("expected error for failed daemon")
	}
	if err.Error() != "max iterations reached" {
		t.Errorf("expected error 'max iterations reached', got: %v", err)
	}
}

// --- Helpers ---

func notPassingPRD(wsName string) string {
	return `{
  "project": "test",
  "branchName": "ralph/` + wsName + `",
  "description": "Test PRD",
  "userStories": [
    {"id": "US-001", "title": "Test", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": false}
  ]
}`
}

type captureHandler struct {
	events *[]events.Event
}

func (h *captureHandler) Handle(e events.Event) {
	*h.events = append(*h.events, e)
}
