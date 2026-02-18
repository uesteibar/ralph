package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDir(t *testing.T) {
	got := Dir("/repo")
	want := filepath.Join("/repo", ".ralph", "knowledge")
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestEnsureDir_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, ".ralph", "knowledge")

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir error: %v", err)
	}

	info, err := os.Stat(knowledgeDir)
	if err != nil {
		t.Fatalf("knowledge directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("knowledge path should be a directory")
	}
}

func TestEnsureDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, ".ralph", "knowledge")

	// Create a file inside to verify it's not overwritten
	if err := os.MkdirAll(knowledgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(knowledgeDir, "existing.md")
	if err := os.WriteFile(testFile, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	// EnsureDir again should not fail or remove existing files
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir (second call) error: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("existing file should still exist: %v", err)
	}
	if string(data) != "keep me" {
		t.Errorf("existing file content changed: got %q", string(data))
	}
}

func TestSeedReadme_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, ".ralph", "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := SeedReadme(dir); err != nil {
		t.Fatalf("SeedReadme error: %v", err)
	}

	readmePath := filepath.Join(knowledgeDir, "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("README.md should exist: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("README.md should not be empty")
	}
	// Verify it contains tagging convention info
	if !contains(content, "Tags") {
		t.Error("README.md should mention Tags format")
	}
}

func TestSeedReadme_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, ".ralph", "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0755); err != nil {
		t.Fatal(err)
	}

	readmePath := filepath.Join(knowledgeDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("custom content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SeedReadme(dir); err != nil {
		t.Fatalf("SeedReadme error: %v", err)
	}

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom content" {
		t.Errorf("README.md should not be overwritten, got %q", string(data))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
