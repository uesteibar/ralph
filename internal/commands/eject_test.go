package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prompts"
)

func TestEject_CreatesAllTemplateFiles(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := ejectTemplates(dir)
	if err != nil {
		t.Fatalf("ejectTemplates() error: %v", err)
	}

	promptsDir := filepath.Join(ralphDir, "prompts")
	for _, name := range prompts.TemplateNames {
		path := filepath.Join(promptsDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected template file %s to exist", name)
		}
	}
}

func TestEject_PreservesGoTemplatePlaceholders(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := ejectTemplates(dir)
	if err != nil {
		t.Fatalf("ejectTemplates() error: %v", err)
	}

	// Read loop_iteration.md and verify it contains raw Go template placeholders.
	content, err := os.ReadFile(filepath.Join(ralphDir, "prompts", "loop_iteration.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "{{") || !strings.Contains(string(content), "}}") {
		t.Error("expected ejected template to contain {{ }} Go template placeholders")
	}
}

func TestEject_FailsWhenPromptsDirAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".ralph", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := ejectTemplates(dir)
	if err == nil {
		t.Fatal("expected error when .ralph/prompts/ already exists")
	}

	if !strings.Contains(err.Error(), "already") {
		t.Errorf("expected error to mention 'already', got: %v", err)
	}
}

func TestEject_FailsWhenRalphDirMissing(t *testing.T) {
	dir := t.TempDir()
	// No .ralph/ directory created.

	err := ejectTemplates(dir)
	if err == nil {
		t.Fatal("expected error when .ralph/ does not exist")
	}
}

func TestEject_TemplateContentMatchesEmbedded(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := ejectTemplates(dir)
	if err != nil {
		t.Fatalf("ejectTemplates() error: %v", err)
	}

	fs := prompts.TemplateFS()
	for _, name := range prompts.TemplateNames {
		embedded, err := fs.ReadFile("templates/" + name)
		if err != nil {
			t.Fatalf("reading embedded template %s: %v", name, err)
		}

		ejected, err := os.ReadFile(filepath.Join(ralphDir, "prompts", name))
		if err != nil {
			t.Fatalf("reading ejected template %s: %v", name, err)
		}

		if string(embedded) != string(ejected) {
			t.Errorf("ejected %s does not match embedded content", name)
		}
	}
}

func TestEject_WritesExactlySixFiles(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := ejectTemplates(dir)
	if err != nil {
		t.Fatalf("ejectTemplates() error: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(ralphDir, "prompts"))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 6 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 6 template files, got %d: %v", len(entries), names)
	}
}
