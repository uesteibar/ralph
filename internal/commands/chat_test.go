package commands

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestChat_ContinueFlagParsing(t *testing.T) {
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
			name:     "--continue sets to true",
			args:     []string{"--continue"},
			expected: true,
		},
		{
			name:     "--continue=true sets to true",
			args:     []string{"--continue=true"},
			expected: true,
		},
		{
			name:     "--continue=false sets to false",
			args:     []string{"--continue=false"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("chat", flag.ContinueOnError)
			continueFlag := fs.Bool("continue", false, "Resume the most recent conversation")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if *continueFlag != tt.expected {
				t.Errorf("expected continue=%v, got %v", tt.expected, *continueFlag)
			}
		})
	}
}

func TestChat_WorkspaceFlagParsing(t *testing.T) {
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
			fs := flag.NewFlagSet("chat", flag.ContinueOnError)
			ws := AddWorkspaceFlag(fs)
			_ = fs.Bool("continue", false, "Resume the most recent conversation")

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

func TestChat_FromBase_ResolvesBaseContext(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Resolve from base (no workspace flag, no env, cwd is repo root)
	wc, err := workspace.ResolveWorkContext("", "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	if wc.Name != "base" {
		t.Errorf("expected Name=base, got %s", wc.Name)
	}
	if wc.WorkDir != dir {
		t.Errorf("expected WorkDir=%s, got %s", dir, wc.WorkDir)
	}
}

func TestChat_WithWorkspace_ResolvesCorrectContext(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace structure
	wsName := "chat-ws"
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

	// Write PRD at workspace level
	prdContent := `{
  "project": "test",
  "branchName": "ralph/chat-ws",
  "description": "Test PRD for chat",
  "userStories": [
    {"id": "US-001", "title": "Add feature", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": true}
  ]
}`
	prdPath := filepath.Join(wsDir, "prd.json")
	if err := os.WriteFile(prdPath, []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Resolve workspace context
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
	if wc.PRDPath != prdPath {
		t.Errorf("expected PRDPath=%s, got %s", prdPath, wc.PRDPath)
	}

	// Verify PRD can be read from resolved path
	p, err := prd.Read(wc.PRDPath)
	if err != nil {
		t.Fatalf("reading PRD from workspace path: %v", err)
	}
	if p.Description != "Test PRD for chat" {
		t.Errorf("expected PRD description='Test PRD for chat', got %q", p.Description)
	}
}

func TestChat_PRDContextLoaded_FromWorkspacePath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Create workspace with PRD
	wsName := "prd-ctx"
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
  "project": "prd-ctx-test",
  "branchName": "ralph/prd-ctx",
  "description": "PRD for context loading test",
  "userStories": [
    {"id": "US-001", "title": "Context story", "description": "Test", "acceptanceCriteria": ["Works"], "priority": 1, "passes": false}
  ]
}`
	prdPath := filepath.Join(wsDir, "prd.json")
	if err := os.WriteFile(prdPath, []byte(prdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify PRD context can be formatted from workspace path
	wc, err := workspace.ResolveWorkContext(wsName, "", dir, dir)
	if err != nil {
		t.Fatalf("ResolveWorkContext failed: %v", err)
	}

	p, err := prd.Read(wc.PRDPath)
	if err != nil {
		t.Fatalf("reading PRD: %v", err)
	}

	ctx := formatPRDContext(p)
	if ctx == "" {
		t.Error("expected non-empty PRD context")
	}

	checks := []string{"prd-ctx-test", "PRD for context loading test", "US-001", "Context story"}
	for _, want := range checks {
		if !strings.Contains(ctx, want) {
			t.Errorf("PRD context should contain %q, got: %s", want, ctx)
		}
	}
}
