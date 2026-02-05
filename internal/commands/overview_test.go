package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestOverview_NoWorkspaces(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	var buf bytes.Buffer
	err := overviewRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "base") {
		t.Errorf("expected 'base' in output, got: %s", output)
	}
	if !containsText(output, "main") {
		t.Errorf("expected 'main' branch in output, got: %s", output)
	}
}

func TestOverview_WithWorkspaces(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	// Create two workspace entries in registry.
	if err := workspace.RegistryCreate(dir, workspace.Workspace{
		Name:   "login-page",
		Branch: "ralph/login-page",
	}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RegistryCreate(dir, workspace.Workspace{
		Name:   "dark-mode",
		Branch: "ralph/dark-mode",
	}); err != nil {
		t.Fatal(err)
	}

	// Create workspace directories.
	for _, name := range []string{"login-page", "dark-mode"} {
		wsDir := filepath.Join(dir, ".ralph", "workspaces", name)
		if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Write PRD for login-page with progress.
	loginPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "ralph/login-page",
		Description: "Login page feature",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
			{ID: "US-002", Title: "Story 2", Passes: true},
			{ID: "US-003", Title: "Story 3", Passes: false},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: true},
			{ID: "IT-002", Description: "Test 2", Passes: false},
		},
	}
	writePRD(t, workspace.PRDPathForWorkspace(dir, "login-page"), loginPRD)

	// dark-mode has no PRD.

	var buf bytes.Buffer
	err := overviewRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Base entry.
	if !containsText(output, "base") {
		t.Errorf("expected 'base' in output, got: %s", output)
	}

	// login-page with progress.
	if !containsText(output, "login-page") {
		t.Errorf("expected 'login-page' in output, got: %s", output)
	}
	if !containsText(output, "ralph/login-page") {
		t.Errorf("expected branch 'ralph/login-page' in output, got: %s", output)
	}
	if !containsText(output, "2/3") {
		t.Errorf("expected story progress '2/3' in output, got: %s", output)
	}
	if !containsText(output, "1/2") {
		t.Errorf("expected test progress '1/2' in output, got: %s", output)
	}

	// dark-mode without PRD.
	if !containsText(output, "dark-mode") {
		t.Errorf("expected 'dark-mode' in output, got: %s", output)
	}
	if !containsText(output, "no prd") {
		t.Errorf("expected 'no prd' for dark-mode in output, got: %s", output)
	}
}

func TestOverview_AllStoriesPassing(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	if err := workspace.RegistryCreate(dir, workspace.Workspace{
		Name:   "done-feature",
		Branch: "ralph/done-feature",
	}); err != nil {
		t.Fatal(err)
	}

	wsDir := filepath.Join(dir, ".ralph", "workspaces", "done-feature")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	donePRD := &prd.PRD{
		Project: "test",
		UserStories: []prd.Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}
	writePRD(t, workspace.PRDPathForWorkspace(dir, "done-feature"), donePRD)

	var buf bytes.Buffer
	err := overviewRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "2/2") {
		t.Errorf("expected '2/2' for all-passing stories, got: %s", output)
	}
}

func TestOverview_MissingWorkspace(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	t.Setenv("RALPH_WORKSPACE", "")

	// Register workspace but don't create directory.
	if err := workspace.RegistryCreate(dir, workspace.Workspace{
		Name:   "ghost",
		Branch: "ralph/ghost",
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := overviewRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !containsText(output, "ghost") {
		t.Errorf("expected 'ghost' in output, got: %s", output)
	}
	if !containsText(output, "missing") {
		t.Errorf("expected 'missing' marker in output, got: %s", output)
	}
}

func TestOverview_CurrentWorkspaceMarked(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	if err := workspace.RegistryCreate(dir, workspace.Workspace{
		Name:   "active-ws",
		Branch: "ralph/active-ws",
	}); err != nil {
		t.Fatal(err)
	}

	wsDir := filepath.Join(dir, ".ralph", "workspaces", "active-ws")
	if err := os.MkdirAll(filepath.Join(wsDir, "tree"), 0755); err != nil {
		t.Fatal(err)
	}

	// Set env to mark active-ws as current.
	t.Setenv("RALPH_WORKSPACE", "active-ws")

	var buf bytes.Buffer
	err := overviewRun(nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// The current workspace should have the marker.
	if !containsText(output, "*") {
		t.Errorf("expected '*' marker for current workspace, got: %s", output)
	}
}
