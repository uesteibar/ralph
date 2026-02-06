package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestFormatStories_IncludesAllStories(t *testing.T) {
	stories := []prd.Story{
		{ID: "US-001", Title: "First story", Passes: true},
		{ID: "US-002", Title: "Second story", Passes: false},
	}

	result := formatStories(stories)

	if !strings.Contains(result, "US-001: First story [done]") {
		t.Errorf("expected US-001 with done status, got:\n%s", result)
	}
	if !strings.Contains(result, "US-002: Second story [pending]") {
		t.Errorf("expected US-002 with pending status, got:\n%s", result)
	}
}

func TestFormatStories_EmptyList(t *testing.T) {
	result := formatStories(nil)
	if result != "" {
		t.Errorf("expected empty string for nil stories, got: %q", result)
	}
}

func TestRebase_FromBase_ErrorsWithoutWorkspace(t *testing.T) {
	t.Setenv("RALPH_WORKSPACE", "") // Clear env to ensure base context
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Change to repo root (base mode)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err = Rebase([]string{})
	if err == nil {
		t.Fatal("expected error when running rebase from base without --workspace")
	}

	if !strings.Contains(err.Error(), "Rebase requires workspace context") {
		t.Errorf("expected error to contain 'Rebase requires workspace context', got: %v", err)
	}
	if !strings.Contains(err.Error(), "--workspace") {
		t.Errorf("expected error to mention --workspace flag, got: %v", err)
	}
}

func TestRebase_WithWorkspace_ResolvesWorkspaceContext(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace structure
	wsName := "rebase-ws"
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

	// Resolve workspace context with --workspace flag
	wc, err := workspace.ResolveWorkContext(wsName, "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	if wc.Name != wsName {
		t.Errorf("expected Name=%s, got %s", wsName, wc.Name)
	}
	if wc.WorkDir != treeDir {
		t.Errorf("expected WorkDir=%s, got %s", treeDir, wc.WorkDir)
	}
}

func TestRebase_PRDContext_LoadedFromWorkspacePath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace with PRD
	wsName := "rebase-prd"
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
  "project": "rebase-test",
  "branchName": "ralph/rebase-prd",
  "description": "PRD for rebase conflict resolution",
  "userStories": [
    {"id": "US-001", "title": "Rebase feature", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": false}
  ]
}`
	prdPath := filepath.Join(wsDir, "prd.json")
	if err := os.WriteFile(prdPath, []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify PRD can be read from workspace path
	wc, err := workspace.ResolveWorkContext(wsName, "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	if wc.PRDPath != prdPath {
		t.Errorf("expected PRDPath=%s, got %s", prdPath, wc.PRDPath)
	}

	p, err := prd.Read(wc.PRDPath)
	if err != nil {
		t.Fatalf("reading PRD from workspace path: %v", err)
	}
	if p.Description != "PRD for rebase conflict resolution" {
		t.Errorf("expected PRD description, got %q", p.Description)
	}
}
