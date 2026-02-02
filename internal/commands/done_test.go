package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/prd"
)

func TestGenerateCommitMessage_IncludesDescriptionAndPassingStories(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Repo: config.RepoConfig{Path: tmpDir},
	}

	// Create .ralph/state directory and a PRD file.
	statePath := cfg.StatePRDPath()
	if err := os.MkdirAll(statePath[:len(statePath)-len("/prd.json")], 0755); err != nil {
		t.Fatal(err)
	}
	p := &prd.PRD{
		Description: "Add rebase and done commands",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Gitops helpers", Passes: true},
			{ID: "US-002", Title: "Rebase prompt", Passes: true},
			{ID: "US-003", Title: "Rebase command", Passes: false},
		},
	}
	if err := prd.Write(statePath, p); err != nil {
		t.Fatal(err)
	}

	msg, err := generateCommitMessage(cfg)
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
