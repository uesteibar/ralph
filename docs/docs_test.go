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

func TestCommands_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "commands.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("commands.md not found: %v", err)
	}
	content := string(data)

	requiredCommands := []struct {
		term string
		desc string
	}{
		{"# Commands", "page title"},
		{"## `init`", "init command"},
		{"## `validate`", "validate command"},
		{"## `run`", "run command"},
		{"## `chat`", "chat command"},
		{"## `switch`", "switch command"},
		{"## `rebase`", "rebase command"},
		{"## `new`", "new command"},
		{"## `eject`", "eject command"},
		{"## `tui`", "tui command"},
		{"## `attach`", "attach command"},
		{"## `stop`", "stop command"},
		{"## `done`", "done command"},
		{"## `status`", "status command"},
		{"## `overview`", "overview command"},
		{"## `workspaces`", "workspaces command"},
		{"## `check`", "check command"},
		{"## `shell-init`", "shell-init command"},
	}
	for _, r := range requiredCommands {
		if !strings.Contains(content, r.term) {
			t.Errorf("commands.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestCommands_HasDescriptionsAndFlags(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "commands.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("commands.md not found: %v", err)
	}
	content := string(data)

	// Commands with flags should have a Flags section
	commandsWithFlags := []string{"run", "chat", "status", "attach"}
	for _, cmd := range commandsWithFlags {
		section := extractSection(content, cmd)
		if section == "" {
			t.Errorf("commands.md missing section for %s", cmd)
			continue
		}
		if !strings.Contains(section, "-") {
			t.Errorf("commands.md section for %s missing flag entries", cmd)
		}
	}
}

func TestCommands_GeneratorExists(t *testing.T) {
	path := filepath.Join(docsDir(), "gen-cli-help.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("gen-cli-help.go not found")
	}
}

func TestAutoRalphOverview_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "overview.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("overview.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Overview", "page title"},
		{"autonomous", "describes autonomous nature"},
		{"Linear", "mentions Linear integration"},
		{"GitHub", "mentions GitHub integration"},
		{"Ralph", "references Ralph execution loop"},
		{"daemon", "describes long-running daemon"},
		{"Prerequisites", "prerequisites section"},
		{"Installation", "installation section"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("overview.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestAutoRalphLifecycle_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "lifecycle.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lifecycle.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Lifecycle", "page title"},
		{"mermaid", "Mermaid state diagram"},
		{"QUEUED", "queued state"},
		{"REFINING", "refining state"},
		{"APPROVED", "approved state"},
		{"BUILDING", "building state"},
		{"IN_REVIEW", "in_review state"},
		{"ADDRESSING_FEEDBACK", "addressing_feedback state"},
		{"COMPLETED", "completed state"},
		{"FAILED", "failed state"},
		{"PAUSED", "paused state"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("lifecycle.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestAutoRalphAbilities_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "abilities.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("abilities.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Abilities", "page title"},
		{"Refine", "refine ability"},
		{"Build", "build ability"},
		{"Rebase", "rebase ability"},
		{"Feedback", "feedback ability"},
		{"Fix Checks", "fix checks ability"},
		{"Complete", "complete ability"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("abilities.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestAutoRalphConfiguration_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "configuration.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("configuration.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Configuration", "page title"},
		{"credentials.yaml", "credentials file"},
		{"~/.autoralph/", "config directory"},
		{"linear_api_key", "Linear API key config"},
		{"github_token", "GitHub token config"},
		{"local_path", "project local path"},
		{"credentials_profile", "credentials profile reference"},
		{"max_iterations", "max iterations config"},
		{"branch_prefix", "branch prefix config"},
		{"autoralph serve", "serve command"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("configuration.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestAutoRalphSecurity_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "security.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("security.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Security", "page title"},
		{"Trusted", "trusted users section"},
		{"GitHub App", "GitHub App authentication"},
		{"github_user_id", "github_user_id config"},
		{"Credential", "credential isolation section"},
		{"private key", "private key reference"},
		{"github_app_client_id", "GitHub App client ID"},
		{"github_app_installation_id", "GitHub App installation ID"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("security.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestAutoRalphDashboard_Content(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "autoralph", "dashboard.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dashboard.md not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"# Dashboard", "page title"},
		{"/api/status", "status endpoint"},
		{"/api/projects", "projects endpoint"},
		{"/api/issues", "issues endpoint"},
		{"WebSocket", "WebSocket section"},
		{"/api/ws", "WebSocket endpoint"},
		{"issue_state_changed", "issue state changed event"},
		{"build_event", "build event type"},
		{"127.0.0.1:7749", "default address"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("dashboard.md missing %s (%q)", r.desc, r.term)
		}
	}
}

func repoRoot() string {
	return filepath.Dir(docsDir())
}

func TestDeployDocsWorkflow_Exists(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "deploy-docs.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("deploy-docs.yml not found: %v", err)
	}
	content := string(data)

	required := []struct {
		term string
		desc string
	}{
		{"push:", "triggers on push"},
		{"main", "targets main branch"},
		{"actions/deploy-pages", "uses deploy-pages action"},
		{"actions/upload-pages-artifact", "uses upload-pages-artifact action"},
		{"mdbook", "installs mdBook"},
		{"docs/book", "references correct build output path"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.term) {
			t.Errorf("deploy-docs.yml missing %s (%q)", r.desc, r.term)
		}
	}
}

func TestDeployDocsWorkflow_PinsMdBookVersion(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "deploy-docs.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("deploy-docs.yml not found: %v", err)
	}
	content := string(data)

	// Should pin a specific mdBook version (e.g., v0.4.40 or similar)
	re := regexp.MustCompile(`mdbook.*v\d+\.\d+\.\d+`)
	if !re.MatchString(content) {
		t.Error("deploy-docs.yml does not pin a specific mdBook version")
	}
}

func TestDeployDocsWorkflow_RunsGenCLI(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "deploy-docs.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("deploy-docs.yml not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "gen-cli-help") {
		t.Error("deploy-docs.yml does not run docs-gen-cli step")
	}
}

func TestCIWorkflow_HasDocBuildStep(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ci.yml not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "mdbook build") {
		t.Error("ci.yml missing mdbook build step for doc validation")
	}
}

func TestCIWorkflow_DocBuildRunsOnPRs(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ci.yml not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "pull_request") {
		t.Error("ci.yml does not trigger on pull_request")
	}
}

func TestReadme_NoBaseProgressTxt(t *testing.T) {
	path := filepath.Join(repoRoot(), "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("README.md not found: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "| `.ralph/progress.txt`") {
		t.Error("README.md should not list .ralph/progress.txt in file table (removed in favor of workspace-scoped progress)")
	}

	// The directory tree should not contain a bare progress.txt line (under .ralph/ root)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "progress.txt                # shared progress log (committed)" {
			t.Error("README.md should not list progress.txt in the directory tree (removed in favor of workspace-scoped progress)")
		}
	}

	// Workspace-scoped progress should still be referenced
	if !strings.Contains(content, "progress") {
		t.Error("README.md should still reference progress (workspace-scoped)")
	}
}

func TestSetup_NoBaseProgressTxt(t *testing.T) {
	path := filepath.Join(docsDir(), "src", "ralph", "setup.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("setup.md not found: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "| `.ralph/progress.txt`") {
		t.Error("setup.md should not list .ralph/progress.txt in file table (removed in favor of workspace-scoped progress)")
	}
}

func TestReadme_HasDocsLink(t *testing.T) {
	path := filepath.Join(repoRoot(), "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("README.md not found: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "uesteibar.github.io/ralph") {
		t.Error("README.md missing link to published documentation site")
	}
}

// extractSection returns the content between ## `cmd` and the next ## heading.
func extractSection(content, cmd string) string {
	header := "## `" + cmd + "`"
	_, after, found := strings.Cut(content, header)
	if !found {
		return ""
	}
	_, next, hasNext := strings.Cut(after, "\n## ")
	if !hasNext {
		return after
	}
	return after[:len(after)-len(next)-len("\n## ")]
}
