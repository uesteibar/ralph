package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func docsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func TestBookToml_Exists(t *testing.T) {
	path := filepath.Join(docsDir(), "book.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("book.toml not found: %v", err)
	}
	content := string(data)

	required := []string{
		`title = "Ralph Documentation"`,
		`build-dir = "book"`,
		`[output.html]`,
	}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("book.toml missing required entry: %s", s)
		}
	}
}

func TestBookToml_MermaidConfigured(t *testing.T) {
	path := filepath.Join(docsDir(), "book.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("book.toml not found: %v", err)
	}
	if !strings.Contains(string(data), "mermaid") {
		t.Error("book.toml does not configure Mermaid support")
	}
}

func TestSummary_ChapterStructure(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "SUMMARY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SUMMARY.md not found: %v", err)
	}
	content := string(data)

	requiredChapters := []string{
		"introduction.md",
		"ralph/getting-started.md",
		"ralph/workflow.md",
		"ralph/commands.md",
		"ralph/configuration.md",
		"ralph/setup.md",
		"ralph/architecture.md",
		"autoralph/overview.md",
		"autoralph/lifecycle.md",
		"autoralph/abilities.md",
		"autoralph/configuration.md",
		"autoralph/security.md",
		"autoralph/dashboard.md",
	}
	for _, ch := range requiredChapters {
		if !strings.Contains(content, ch) {
			t.Errorf("SUMMARY.md missing chapter: %s", ch)
		}
	}
}

func TestSummary_RalphAndAutoRalphSections(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "SUMMARY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SUMMARY.md not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# Ralph") {
		t.Error("SUMMARY.md missing '# Ralph' section header")
	}
	if !strings.Contains(content, "# AutoRalph") {
		t.Error("SUMMARY.md missing '# AutoRalph' section header")
	}
}

func TestSummary_AllReferencedFilesExist(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "SUMMARY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SUMMARY.md not found: %v", err)
	}

	re := regexp.MustCompile(`\]\(([^)]+\.md)\)`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no markdown links found in SUMMARY.md")
	}

	srcDir := filepath.Join(docsDir(), "src")
	for _, m := range matches {
		relPath := m[1]
		fullPath := filepath.Join(srcDir, relPath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("SUMMARY.md references %s but file does not exist", relPath)
		}
	}
}

func TestIntroduction_Exists(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "introduction.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("introduction.md not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# Ralph") {
		t.Error("introduction.md missing project title")
	}
	if !strings.Contains(content, "Quick Links") {
		t.Error("introduction.md missing Quick Links section")
	}
}

func TestMermaidInit_Exists(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "mermaid-init.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mermaid-init.js not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "mermaid") {
		t.Error("mermaid-init.js does not reference mermaid")
	}
}
