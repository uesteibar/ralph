package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/prd"
)

func TestGenerateCommitMessage_IncludesDescriptionAndPassingStories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .ralph/state directory and a PRD file.
	stateDir := filepath.Join(tmpDir, ".ralph", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	prdPath := filepath.Join(stateDir, "prd.json")
	p := &prd.PRD{
		Description: "Add rebase and done commands",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Gitops helpers", Passes: true},
			{ID: "US-002", Title: "Rebase prompt", Passes: true},
			{ID: "US-003", Title: "Rebase command", Passes: false},
		},
	}
	if err := prd.Write(prdPath, p); err != nil {
		t.Fatal(err)
	}

	msg, err := generateCommitMessage(prdPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(msg, "Add rebase and done commands") {
		t.Errorf("expected description in message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "US-001: Gitops helpers") {
		t.Errorf("expected US-001 in completed stories, got:\n%s", msg)
	}
	if !strings.Contains(msg, "US-002: Rebase prompt") {
		t.Errorf("expected US-002 in completed stories, got:\n%s", msg)
	}
	if strings.Contains(msg, "US-003") {
		t.Errorf("did not expect US-003 (pending) in completed stories, got:\n%s", msg)
	}
}

func TestPromptEditMessage_AcceptsDefault(t *testing.T) {
	// Simulate pressing Enter (empty line).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("\n")
	w.Close()

	result, err := promptEditMessage("draft message", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "draft message" {
		t.Errorf("expected draft message, got: %q", result)
	}
}

func TestPromptEditMessage_ReplacesWithUserInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("my custom message\n")
	w.Close()

	result, err := promptEditMessage("draft message", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my custom message" {
		t.Errorf("expected 'my custom message', got: %q", result)
	}
}

func TestShouldCleanup_Yes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("y\n")
	w.Close()

	if !shouldCleanup(r) {
		t.Error("expected true for 'y' input")
	}
}

func TestShouldCleanup_No(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("n\n")
	w.Close()

	if shouldCleanup(r) {
		t.Error("expected false for 'n' input")
	}
}

func TestArchivePRDFromPath_ArchivesToCorrectLocation(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Repo: config.RepoConfig{Path: tmpDir},
	}

	// Create archive directory.
	archiveDir := cfg.StateArchiveDir()
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create PRD at a workspace-level path.
	wsDir := filepath.Join(tmpDir, ".ralph", "workspaces", "my-feature")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	prdPath := filepath.Join(wsDir, "prd.json")
	p := &prd.PRD{
		BranchName:  "ralph/my-feature",
		Description: "My feature",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story one", Passes: true},
		},
	}
	if err := prd.Write(prdPath, p); err != nil {
		t.Fatal(err)
	}

	// Override time for deterministic archive dir name.
	oldNow := doneNowFn
	doneNowFn = func() time.Time {
		return time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
	}
	defer func() { doneNowFn = oldNow }()

	archivePRDFromPath(prdPath, cfg)

	// Verify archive was created.
	expectedDir := filepath.Join(archiveDir, "2026-02-05-ralph__my-feature")
	archivedPRD := filepath.Join(expectedDir, "prd.json")
	if _, err := os.Stat(archivedPRD); err != nil {
		t.Fatalf("archived PRD not found at %s: %v", archivedPRD, err)
	}

	// Verify content matches.
	archived, err := prd.Read(archivedPRD)
	if err != nil {
		t.Fatalf("reading archived PRD: %v", err)
	}
	if archived.BranchName != "ralph/my-feature" {
		t.Errorf("archived branchName = %q, want %q", archived.BranchName, "ralph/my-feature")
	}
	if archived.Description != "My feature" {
		t.Errorf("archived description = %q, want %q", archived.Description, "My feature")
	}
}

func TestArchivePRDFromPath_NoPRDFile_NoError(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Repo: config.RepoConfig{Path: tmpDir},
	}

	// Should not panic or error when source doesn't exist.
	archivePRDFromPath(filepath.Join(tmpDir, "nonexistent.json"), cfg)
}

func TestGenerateCommitMessage_ReadsFromGivenPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workspace-level PRD.
	wsDir := filepath.Join(tmpDir, ".ralph", "workspaces", "test-ws")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	prdPath := filepath.Join(wsDir, "prd.json")
	p := &prd.PRD{
		Description: "Test workspace feature",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "First story", Passes: true},
		},
	}
	if err := prd.Write(prdPath, p); err != nil {
		t.Fatal(err)
	}

	msg, err := generateCommitMessage(prdPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(msg, "Test workspace feature") {
		t.Errorf("expected description, got:\n%s", msg)
	}
	if !strings.Contains(msg, "US-001: First story") {
		t.Errorf("expected passing story, got:\n%s", msg)
	}
}
