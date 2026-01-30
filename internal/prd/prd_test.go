package prd

import (
	"os"
	"path/filepath"
	"testing"
)

func samplePRD() *PRD {
	return &PRD{
		Project:     "TestProject",
		BranchName:  "ralph/test-feature",
		Description: "A test feature",
		UserStories: []Story{
			{ID: "US-001", Title: "First", Priority: 2, Passes: false},
			{ID: "US-002", Title: "Second", Priority: 1, Passes: false},
			{ID: "US-003", Title: "Third", Priority: 3, Passes: true},
		},
	}
}

func TestWriteAndRead_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := samplePRD()
	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if loaded.Project != original.Project {
		t.Errorf("Project = %q, want %q", loaded.Project, original.Project)
	}
	if loaded.BranchName != original.BranchName {
		t.Errorf("BranchName = %q, want %q", loaded.BranchName, original.BranchName)
	}
	if len(loaded.UserStories) != len(original.UserStories) {
		t.Fatalf("UserStories count = %d, want %d", len(loaded.UserStories), len(original.UserStories))
	}
	for i, s := range loaded.UserStories {
		if s.ID != original.UserStories[i].ID {
			t.Errorf("Story[%d].ID = %q, want %q", i, s.ID, original.UserStories[i].ID)
		}
		if s.Passes != original.UserStories[i].Passes {
			t.Errorf("Story[%d].Passes = %v, want %v", i, s.Passes, original.UserStories[i].Passes)
		}
	}
}

func TestNextUnfinished_ReturnsByPriority(t *testing.T) {
	p := samplePRD()
	next := NextUnfinished(p)
	if next == nil {
		t.Fatal("expected a story, got nil")
	}
	// US-002 has priority 1, US-001 has priority 2
	if next.ID != "US-002" {
		t.Errorf("NextUnfinished = %q, want %q (lowest priority number)", next.ID, "US-002")
	}
}

func TestNextUnfinished_AllPassing_ReturnsNil(t *testing.T) {
	p := &PRD{
		UserStories: []Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}
	if got := NextUnfinished(p); got != nil {
		t.Errorf("expected nil, got story %q", got.ID)
	}
}

func TestNextUnfinished_Empty_ReturnsNil(t *testing.T) {
	p := &PRD{}
	if got := NextUnfinished(p); got != nil {
		t.Errorf("expected nil for empty PRD, got %v", got)
	}
}

func TestAllPass_MixedStates(t *testing.T) {
	p := samplePRD()
	if AllPass(p) {
		t.Error("AllPass should be false when some stories are not passing")
	}
}

func TestAllPass_AllTrue(t *testing.T) {
	p := &PRD{
		UserStories: []Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}
	if !AllPass(p) {
		t.Error("AllPass should be true when all stories pass")
	}
}

func TestMarkPassing_ExistingStory(t *testing.T) {
	p := samplePRD()
	if !MarkPassing(p, "US-001") {
		t.Error("MarkPassing should return true for existing story")
	}
	if !p.UserStories[0].Passes {
		t.Error("Story US-001 should now have Passes=true")
	}
}

func TestMarkPassing_NonexistentStory(t *testing.T) {
	p := samplePRD()
	if MarkPassing(p, "US-999") {
		t.Error("MarkPassing should return false for nonexistent story")
	}
}

func TestRead_NonexistentFile_ReturnsError(t *testing.T) {
	_, err := Read("/nonexistent/prd.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestRead_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
