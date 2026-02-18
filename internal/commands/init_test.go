package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/claude"
)

func TestInit_GitTrackingOption1_GitignoresWorkspacesAndState(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Simulate: user picks "1" (track in git), then "n" (no LLM)
	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	content := string(gitignore)
	if !containsLine(content, ".ralph/workspaces/") {
		t.Error(".gitignore should contain .ralph/workspaces/")
	}
	if !containsLine(content, ".ralph/state/") {
		t.Error(".gitignore should contain .ralph/state/")
	}
	if containsLine(content, ".ralph/") {
		// ".ralph/" alone should NOT be present when tracking in git
		// (only the specific subdirs)
		// But we need to be careful: ".ralph/workspaces/" contains ".ralph/"
		// so check for exact line match
		for _, l := range splitLines(content) {
			if trimSpace(l) == ".ralph/" {
				t.Error(".gitignore should NOT contain bare .ralph/ when tracking in git")
			}
		}
	}
}

func TestInit_GitTrackingOption2_GitignoresEntireRalphDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Simulate: user picks "2" (keep local), then "n" (no LLM)
	in := strings.NewReader("2\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	content := string(gitignore)
	hasBareLine := false
	for _, l := range splitLines(content) {
		if trimSpace(l) == ".ralph/" {
			hasBareLine = true
			break
		}
	}
	if !hasBareLine {
		t.Error(".gitignore should contain .ralph/ when keeping local")
	}
}

func TestInit_LLMOptIn_InjectsDetectedChecks(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Mock Claude to return a YAML list of quality checks
	origInvoker := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvoker }()
	invokeClaudeFn = func(_ context.Context, opts claude.InvokeOpts) (string, error) {
		if !opts.Print {
			t.Error("expected Print mode")
		}
		return "- \"go test ./...\"\n- \"go vet ./...\"\n", nil
	}

	in := strings.NewReader("1\nY\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	configPath := filepath.Join(dir, ".ralph", "ralph.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "go test ./...") {
		t.Error("config should contain 'go test ./...' from Claude detection")
	}
	if !strings.Contains(content, "go vet ./...") {
		t.Error("config should contain 'go vet ./...' from Claude detection")
	}
}

func TestInit_LLMOptIn_FallsBackOnClaudeFailure(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Mock Claude to fail
	origInvoker := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvoker }()
	invokeClaudeFn = func(_ context.Context, _ claude.InvokeOpts) (string, error) {
		return "", fmt.Errorf("claude: command not found")
	}

	in := strings.NewReader("1\nY\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should fall back to default template
	configPath := filepath.Join(dir, ".ralph", "ralph.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "npm test") {
		t.Error("config should contain default 'npm test' when Claude fails")
	}
}

func TestInit_LLMOptOut_WritesDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	configPath := filepath.Join(dir, ".ralph", "ralph.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	if !strings.Contains(string(data), "quality_checks:") {
		t.Error("config should contain quality_checks section")
	}
}

func TestInit_Idempotent_SkipsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// First run
	in1 := strings.NewReader("1\nn\n")
	if err := Init(nil, in1); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	configPath := filepath.Join(dir, ".ralph", "ralph.yaml")
	original, _ := os.ReadFile(configPath)

	// Modify config to detect if it gets overwritten
	os.WriteFile(configPath, []byte("project: Modified\nrepo:\n  default_base: main\n"), 0644)

	// Second run — should skip config and NOT prompt (idempotent)
	in2 := strings.NewReader("1\nn\n")
	if err := Init(nil, in2); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	after, _ := os.ReadFile(configPath)
	if string(after) == string(original) {
		t.Error("config should have been preserved (not overwritten) on second run")
	}
	if !strings.Contains(string(after), "Modified") {
		t.Error("modified config should be preserved on re-run")
	}
}

func TestInit_InvalidGitTrackingChoice_RetriesUntilValid(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// User types "3" (invalid), then "1" (valid), then "n"
	in := strings.NewReader("3\n1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should succeed with option 1 behavior
	gitignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	content := string(gitignore)
	if !containsLine(content, ".ralph/workspaces/") {
		t.Error(".gitignore should contain .ralph/workspaces/")
	}
}

func TestParseQualityChecks_ValidYAML(t *testing.T) {
	checks, err := parseQualityChecks("- \"go test ./...\"\n- \"go vet ./...\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}
	if checks[0] != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", checks[0])
	}
	if checks[1] != "go vet ./..." {
		t.Errorf("expected 'go vet ./...', got %q", checks[1])
	}
}

func TestParseQualityChecks_InvalidYAML(t *testing.T) {
	_, err := parseQualityChecks("this is not yaml list: {{{")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseQualityChecks_EmptyList(t *testing.T) {
	_, err := parseQualityChecks("[]")
	if err == nil {
		t.Error("expected error for empty list")
	}
}

func TestInit_LLMDefaultYes_AcceptsEmptyInput(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// User picks "1", then just presses enter (default Y)
	in := strings.NewReader("1\n\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should succeed — empty input means "yes" for [Y/n]
	configPath := filepath.Join(dir, ".ralph", "ralph.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal("ralph.yaml should exist")
	}
}

func TestInit_CreatesClaudeMDWithNoCoSignRule(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	claudeMD, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}

	content := string(claudeMD)
	if !strings.Contains(content, "Co-Authored-By") {
		t.Error("CLAUDE.md should contain Co-Authored-By instruction")
	}
	if !strings.Contains(content, "Do NOT add Co-Authored-By") {
		t.Error("CLAUDE.md should instruct not to add Co-Authored-By headers")
	}
}

func TestInit_ClaudeMD_IdempotentOnRerun(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// First run
	in1 := strings.NewReader("1\nn\n")
	if err := Init(nil, in1); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	first, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md after first run: %v", err)
	}

	// Second run
	in2 := strings.NewReader("1\nn\n")
	if err := Init(nil, in2); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	second, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md after second run: %v", err)
	}

	if string(first) != string(second) {
		t.Error("CLAUDE.md content should be identical after re-run (idempotent)")
	}

	// Verify content is not duplicated
	count := strings.Count(string(second), "Co-Authored-By")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of Co-Authored-By, got %d", count)
	}
}

func TestInit_OutputHasNoTimestampsOrBracketLabels(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		w.Close()
		os.Stderr = oldStderr
		t.Fatalf("Init failed: %v", err)
	}

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderr := buf.String()

	// Verify no Go log timestamp pattern (YYYY/MM/DD HH:MM:SS)
	timestampPattern := regexp.MustCompile(`\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`)
	if timestampPattern.MatchString(stderr) {
		t.Errorf("stderr should not contain timestamps, got: %s", stderr)
	}

	// Verify no bracket labels
	bracketPattern := regexp.MustCompile(`\[(init|loop|run|rebase|workspace|ralph)\]`)
	if bracketPattern.MatchString(stderr) {
		t.Errorf("stderr should not contain bracket labels, got: %s", stderr)
	}

	// Verify it still has the init output
	if !strings.Contains(stderr, "initialized .ralph/") {
		t.Errorf("stderr should contain initialization message, got: %s", stderr)
	}
}

func TestInit_CreatesWorkspaceInfrastructure(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Check .ralph/workspaces/ directory exists
	wsDir := filepath.Join(dir, ".ralph", "workspaces")
	info, err := os.Stat(wsDir)
	if err != nil {
		t.Fatalf("workspaces directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("workspaces should be a directory")
	}

	// Check .ralph/state/workspaces.json exists with empty array
	wsJSON := filepath.Join(dir, ".ralph", "state", "workspaces.json")
	data, err := os.ReadFile(wsJSON)
	if err != nil {
		t.Fatalf("workspaces.json should exist: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("workspaces.json should contain empty array, got %q", string(data))
	}
}

func TestInit_CreatesKnowledgeDirectory(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	in := strings.NewReader("1\nn\n")
	if err := Init(nil, in); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Knowledge directory should exist
	knowledgeDir := filepath.Join(dir, ".ralph", "knowledge")
	info, err := os.Stat(knowledgeDir)
	if err != nil {
		t.Fatalf("knowledge directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("knowledge should be a directory")
	}

	// README.md should exist with tagging info
	readmePath := filepath.Join(knowledgeDir, "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("README.md should exist: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Tags") {
		t.Error("README.md should mention Tags format")
	}
	if !strings.Contains(content, "Knowledge Base") {
		t.Error("README.md should explain the knowledge base purpose")
	}
}

func TestInit_KnowledgeDirectory_Idempotent(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// First run
	in1 := strings.NewReader("1\nn\n")
	if err := Init(nil, in1); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	// Write a custom file to the knowledge directory
	customFile := filepath.Join(dir, ".ralph", "knowledge", "my-learning.md")
	if err := os.WriteFile(customFile, []byte("## Tags: go\nmy learning"), 0644); err != nil {
		t.Fatal(err)
	}

	// Modify README to verify it's not overwritten
	readmePath := filepath.Join(dir, ".ralph", "knowledge", "README.md")
	if err := os.WriteFile(readmePath, []byte("custom readme"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second run
	in2 := strings.NewReader("1\nn\n")
	if err := Init(nil, in2); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	// Custom file should still exist
	data, err := os.ReadFile(customFile)
	if err != nil {
		t.Fatalf("custom knowledge file should still exist: %v", err)
	}
	if !strings.Contains(string(data), "my learning") {
		t.Error("custom knowledge file content should be preserved")
	}

	// README should not be overwritten
	readmeData, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readmeData) != "custom readme" {
		t.Error("README.md should not be overwritten on re-run")
	}
}

func TestFinishSkillContent_ContainsOverviewFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"featureOverview in JSON schema", `"featureOverview"`},
		{"architectureOverview in JSON schema", `"architectureOverview"`},
		{"instruction to capture feature overview", "feature overview"},
		{"instruction to capture architecture overview", "architecture overview"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(finishSkillContent, tt.content) {
				t.Errorf("finishSkillContent should contain %q", tt.content)
			}
		})
	}
}
