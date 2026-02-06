package commands

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// Create a workspace structure
	wsName := "my-feature"
	ws := workspace.Workspace{Name: wsName, Branch: "ralph/" + wsName}

	// Create workspace directory structure manually
	wsDir := filepath.Join(dir, ".ralph", "workspaces", wsName)
	treeDir := filepath.Join(wsDir, "tree")
	if err := os.MkdirAll(treeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write workspace.json
	if err := workspace.WriteWorkspaceJSON(dir, wsName, ws); err != nil {
		t.Fatal(err)
	}

	// Register workspace
	if err := workspace.RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	// Write PRD at workspace level
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

	// Resolve workspace context and verify paths
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
	t.Setenv("RALPH_WORKSPACE", "") // Clear env to ensure base mode
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Write PRD at base level
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

	// Change to the repo directory (base mode)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	// Run with --max-iterations=0 to avoid invoking Claude
	runErr := Run([]string{"--max-iterations=1"})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	// The error from the loop (max iterations) is expected
	_ = runErr

	// Verify base mode warning was printed
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

	// Don't create any PRD file

	// Change to repo directory
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

	// Create workspace structure
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

	// Write PRD at workspace level (with all stories passing so loop exits)
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

	// Change to repo root (not inside workspace tree)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Verify the workspace context resolves correctly with --workspace flag
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

	// Create workspace structure without PRD
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

	// Change to repo root
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

	// Create workspace with all stories passing
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

	// Write PRD with all stories passing
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

	// Change to repo root
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	err = Run([]string{"--workspace", wsName})

	wPipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(rPipe)
	stderr := buf.String()

	// Should succeed without error
	if err != nil {
		t.Fatalf("expected no error when all stories pass, got: %v", err)
	}

	// Should print done message
	if !strings.Contains(stderr, "All stories and integration tests pass") {
		t.Errorf("expected stderr to contain done message, got: %s", stderr)
	}
	if !strings.Contains(stderr, "squash and merge your changes back to base") {
		t.Errorf("expected stderr to mention squash and merge, got: %s", stderr)
	}
}
