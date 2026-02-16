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

func TestGettingStarted_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "getting-started.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("getting-started.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"Prerequisites", "prerequisites section"},
		{"Installation", "installation section"},
		{"Quick Start", "quick-start workflow section"},
		{"Go 1.25", "Go version prerequisite"},
		{"Claude Code", "Claude Code prerequisite"},
		{"ralph init", "init command in quick start"},
		{"ralph new", "new command in quick start"},
		{"ralph run", "run command in quick start"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("getting-started.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestWorkflow_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "workflow.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("workflow.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"ralph init", "init step"},
		{"ralph new", "new step"},
		{"ralph run", "run step"},
		{"ralph done", "done step"},
		{"mermaid", "Mermaid diagram"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("workflow.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestConfiguration_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "configuration.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("configuration.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"ralph.yaml", "ralph.yaml reference"},
		{"PRD", "PRD format section"},
		{"Prompt", "prompt customization section"},
		{"quality_checks", "quality checks config"},
		{"copy_to_worktree", "copy_to_worktree config"},
		{"ralph eject", "eject command for prompts"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("configuration.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestSetup_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "setup.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("setup.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"ralph init", "init command"},
		{"Shell Integration", "shell integration section"},
		{"shell-init", "shell-init command"},
		{".ralph/", "ralph directory structure"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("setup.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestArchitecture_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "architecture.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("architecture.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"Execution Loop", "execution loop section"},
		{"Workspace", "workspace isolation section"},
		{"mermaid", "Mermaid diagram"},
		{"worktree", "git worktree concept"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("architecture.md missing %s (%q)", r.desc, r.term)
		}
	}
}
