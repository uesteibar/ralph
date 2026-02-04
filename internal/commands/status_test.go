package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func writePRD(t *testing.T, path string, p *prd.PRD) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStatus_InBase(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	var buf bytes.Buffer
	err := statusRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "Workspace:") {
		t.Error("expected output to contain 'Workspace:'")
	}
	if !containsText(output, "base") {
		t.Error("expected output to contain 'base'")
	}
	if !containsText(output, "Branch:") {
		t.Error("expected output to contain 'Branch:'")
	}
	if !containsText(output, "main") {
		t.Error("expected output to contain 'main' branch")
	}
	if !containsText(output, "Tip: ralph workspaces new <name>") {
		t.Error("expected output to contain workspace creation hint")
	}
}

func TestStatus_InWorkspaceWithPRD(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create a workspace entry in the registry manually.
	wsDir := filepath.Join(dir, ".ralph", "workspaces", "login-page")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write workspace.json.
	wsJSON := `{"name":"login-page","branch":"ralph/login-page","createdAt":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wsDir, "workspace.json"), []byte(wsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Write registry.
	registryJSON := `[{"name":"login-page","branch":"ralph/login-page","createdAt":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, ".ralph", "state", "workspaces.json"), []byte(registryJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Write PRD with stories.
	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "ralph/login-page",
		Description: "Login page feature",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
			{ID: "US-002", Title: "Story 2", Passes: true},
			{ID: "US-003", Title: "Story 3", Passes: false},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
			{ID: "IT-002", Description: "Test 2", Passes: false},
		},
	}
	writePRD(t, filepath.Join(wsDir, "prd.json"), testPRD)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "login-page")

	var buf bytes.Buffer
	err := statusRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "login-page") {
		t.Errorf("expected workspace name in output, got: %s", output)
	}
	if !containsText(output, "ralph/login-page") {
		t.Errorf("expected branch in output, got: %s", output)
	}
	if !containsText(output, "2/3 passing") {
		t.Errorf("expected story progress '2/3 passing' in output, got: %s", output)
	}
	if !containsText(output, "0/2 passing") {
		t.Errorf("expected integration test progress '0/2 passing' in output, got: %s", output)
	}
}

func TestStatus_Short_InBase(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	var buf bytes.Buffer
	err := statusRun([]string{"--short"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output != "base" {
		t.Errorf("expected 'base', got %q", output)
	}
}

func TestStatus_Short_InWorkspaceWithPRD(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace structure with PRD.
	wsDir := filepath.Join(dir, ".ralph", "workspaces", "login-page")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	testPRD := &prd.PRD{
		Project: "test",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
			{ID: "US-002", Title: "Story 2", Passes: false},
			{ID: "US-003", Title: "Story 3", Passes: true},
			{ID: "US-004", Title: "Story 4", Passes: false},
			{ID: "US-005", Title: "Story 5", Passes: true},
		},
	}
	writePRD(t, filepath.Join(wsDir, "prd.json"), testPRD)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "login-page")

	var buf bytes.Buffer
	err := statusRun([]string{"--short"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output != "login-page 3/5" {
		t.Errorf("expected 'login-page 3/5', got %q", output)
	}
}

func TestStatus_Short_WorkspaceWithoutPRD(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace structure without PRD.
	wsDir := filepath.Join(dir, ".ralph", "workspaces", "my-feature")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "my-feature")

	var buf bytes.Buffer
	err := statusRun([]string{"--short"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output != "my-feature (no prd)" {
		t.Errorf("expected 'my-feature (no prd)', got %q", output)
	}
}

func TestStatus_WorkspaceWithoutPRD(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace structure without PRD.
	wsDir := filepath.Join(dir, ".ralph", "workspaces", "my-feature")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write registry.
	registryJSON := `[{"name":"my-feature","branch":"ralph/my-feature","createdAt":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, ".ralph", "state", "workspaces.json"), []byte(registryJSON), 0644); err != nil {
		t.Fatal(err)
	}

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "my-feature")

	var buf bytes.Buffer
	err := statusRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "my-feature") {
		t.Errorf("expected workspace name in output, got: %s", output)
	}
	if !containsText(output, "No PRD") {
		t.Errorf("expected 'No PRD' in output, got: %s", output)
	}
}

func TestStatus_IntegrationTestProgress(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	wsDir := filepath.Join(dir, ".ralph", "workspaces", "checkout")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	registryJSON := `[{"name":"checkout","branch":"ralph/checkout","createdAt":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, ".ralph", "state", "workspaces.json"), []byte(registryJSON), 0644); err != nil {
		t.Fatal(err)
	}

	testPRD := &prd.PRD{
		Project: "test",
		UserStories: []prd.Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Passes: true},
			{ID: "IT-002", Passes: false},
			{ID: "IT-003", Passes: true},
		},
	}
	writePRD(t, filepath.Join(wsDir, "prd.json"), testPRD)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "checkout")

	var buf bytes.Buffer
	err := statusRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "2/2 passing") {
		t.Errorf("expected story progress '2/2 passing', got: %s", output)
	}
	if !containsText(output, "2/3 passing") {
		t.Errorf("expected integration test progress '2/3 passing', got: %s", output)
	}
}

// containsText strips ANSI escape sequences and checks for substring.
func containsText(s, substr string) bool {
	return bytesContainText([]byte(s), substr)
}

func bytesContainText(b []byte, substr string) bool {
	stripped := stripANSI(b)
	return bytes.Contains(stripped, []byte(substr))
}

// stripANSI removes ANSI escape sequences from a byte slice.
func stripANSI(b []byte) []byte {
	var result []byte
	i := 0
	for i < len(b) {
		if b[i] == '\x1b' {
			// Skip escape sequence.
			i++
			if i < len(b) && b[i] == '[' {
				i++
				for i < len(b) && !((b[i] >= 'A' && b[i] <= 'Z') || (b[i] >= 'a' && b[i] <= 'z')) {
					i++
				}
				if i < len(b) {
					i++ // skip final letter
				}
			}
		} else {
			result = append(result, b[i])
			i++
		}
	}
	return result
}
