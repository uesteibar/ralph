package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func TestRenderLoopIteration_ContainsStoryDetails(t *testing.T) {
	story := &prd.Story{
		ID:                 "US-001",
		Title:              "Add user login",
		Description:        "As a user, I want to log in",
		AcceptanceCriteria: []string{"Login form renders", "Tests pass"},
	}

	out, err := RenderLoopIteration(story, []string{"npm test", "npm run lint"}, ".ralph/progress.txt", "/abs/path/to/prd.json", "", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{"US-001", "Add user login", "npm test", "npm run lint", "<promise>COMPLETE</promise>", ".ralph/progress.txt", "/abs/path/to/prd.json"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderLoopIteration_CompletionRequiresBothStoriesAndIntegrationTests(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	// Verify completion criteria mentions both userStories and integrationTests
	checks := []string{
		"userStories",
		"integrationTests",
		"passes: true",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("completion criteria should mention %q", want)
		}
	}
}

func TestRenderLoopIteration_ContainsNoCoSignInstruction(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	if !strings.Contains(out, "Co-Authored-By") {
		t.Error("loop_iteration.md should contain Co-Authored-By instruction")
	}
	if !strings.Contains(out, "Do NOT add Co-Authored-By") {
		t.Error("loop_iteration.md should instruct not to add Co-Authored-By headers")
	}
}

func TestRenderLoopIteration_WithOverviewsPopulated(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	featureOverview := "This feature adds dark mode support across the entire application"
	architectureOverview := "We use a theme context provider at the root with CSS custom properties"

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", featureOverview, architectureOverview)
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{
		"## Feature Overview",
		featureOverview,
		"## Architecture Overview",
		architectureOverview,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderLoopIteration_WithEmptyOverviews(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	if strings.Contains(out, "## Feature Overview") {
		t.Error("output should not contain Feature Overview section when empty")
	}
	if strings.Contains(out, "## Architecture Overview") {
		t.Error("output should not contain Architecture Overview section when empty")
	}
}

func TestRenderQAVerification_ContainsNoCoSignInstruction(t *testing.T) {
	data := QAVerificationData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	out, err := RenderQAVerification(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerification failed: %v", err)
	}

	if !strings.Contains(out, "Co-Authored-By") {
		t.Error("qa_verification.md should contain Co-Authored-By instruction")
	}
	if !strings.Contains(out, "Do NOT add Co-Authored-By") {
		t.Error("qa_verification.md should instruct not to add Co-Authored-By headers")
	}
}

func TestRenderQAFix_ContainsNoCoSignInstruction(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
		FailedTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test", Passes: false, Failure: "failed"},
		},
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	if !strings.Contains(out, "Co-Authored-By") {
		t.Error("qa_fix.md should contain Co-Authored-By instruction")
	}
	if !strings.Contains(out, "Do NOT add Co-Authored-By") {
		t.Error("qa_fix.md should instruct not to add Co-Authored-By headers")
	}
}

func TestRenderPRDNew_ContainsProjectName(t *testing.T) {
	out, err := RenderPRDNew(PRDNewData{
		ProjectName: "MyProject",
		PRDPath:     ".ralph/state/prd.json",
	}, "")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}
	if !strings.Contains(out, "MyProject") {
		t.Error("output should contain project name")
	}
	if !strings.Contains(out, ".ralph/state/prd.json") {
		t.Error("output should contain PRD path")
	}
}

func TestRenderPRDNew_OverviewSectionsExistWithCorrectContent(t *testing.T) {
	out, err := RenderPRDNew(PRDNewData{
		ProjectName: "TestProject",
		PRDPath:     ".ralph/state/prd.json",
	}, "")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}

	// Both overview sections must exist
	checks := []string{
		"Proposing Feature Overview",
		"Proposing Architecture Overview",
		"at least 2 approaches",
		"Other options considered",
		"wait for user approval",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderPRDNew_OverviewSectionsInCorrectOrder(t *testing.T) {
	out, err := RenderPRDNew(PRDNewData{
		ProjectName: "TestProject",
		PRDPath:     ".ralph/state/prd.json",
	}, "")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}

	// The flow order must be: clarify → feature overview → architecture overview → user stories → integration tests → /finish
	markers := []struct {
		label string
		text  string
	}{
		{"clarifying questions", "clarifying questions"},
		{"Feature Overview", "Proposing Feature Overview"},
		{"Architecture Overview", "Proposing Architecture Overview"},
		{"Story writing", "Story writing guidelines"},
		{"Integration Tests", "Proposing Integration Tests"},
	}

	prevIdx := -1
	prevLabel := ""
	for _, m := range markers {
		idx := strings.Index(out, m.text)
		if idx < 0 {
			t.Fatalf("output should contain %q", m.text)
		}
		if idx <= prevIdx {
			t.Errorf("%q (pos %d) should come after %q (pos %d)", m.label, idx, prevLabel, prevIdx)
		}
		prevIdx = idx
		prevLabel = m.label
	}
}

func TestRenderPRDNew_WithWorkspaceContext(t *testing.T) {
	out, err := RenderPRDNew(PRDNewData{
		ProjectName:     "MyProject",
		PRDPath:         "/repo/.ralph/workspaces/my-feature/prd.json",
		WorkspaceBranch: "ralph/my-feature",
	}, "")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}

	checks := []string{
		"MyProject",
		"/repo/.ralph/workspaces/my-feature/prd.json",
		"ralph/my-feature",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderPRDNew_BaseMode_NoBranch(t *testing.T) {
	out, err := RenderPRDNew(PRDNewData{
		ProjectName: "MyProject",
		PRDPath:     ".ralph/state/prd.json",
	}, "")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}
	// Should not contain branch instruction when WorkspaceBranch is empty.
	if strings.Contains(out, "Use branch name") {
		t.Error("output should not contain branch instruction in base mode")
	}
}

func TestRenderChatSystem_ContainsProjectName(t *testing.T) {
	out, err := RenderChatSystem(ChatSystemData{ProjectName: "ChatProject"}, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	if !strings.Contains(out, "ChatProject") {
		t.Error("output should contain project name")
	}
}

func TestRenderChatSystem_IncludesPRDContext(t *testing.T) {
	data := ChatSystemData{
		ProjectName: "TestProject",
		PRDContext:  "Project: test\nDescription: Build a login system\nStories:\n- US-001: Add login form [done]\n",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	checks := []string{"PRD Context", "Build a login system", "US-001: Add login form"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderChatSystem_NoPRDContext_OmitsSection(t *testing.T) {
	data := ChatSystemData{
		ProjectName: "TestProject",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	if strings.Contains(out, "PRD Context") {
		t.Error("output should not contain PRD Context section when PRDContext is empty")
	}
}

func TestRenderRebaseConflict_ContainsAllSections(t *testing.T) {
	data := RebaseConflictData{
		PRDDescription: "Add rebase and done commands for worktree workflows",
		Stories:        "- US-001: Add gitops helpers\n- US-002: Add prompt template",
		Progress:       "## US-001\nImplemented gitops helpers\n",
		FeatureDiff:    "diff --git a/main.go\n+feature code here",
		BaseDiff:       "diff --git a/main.go\n+base change here",
		ConflictFiles:  "internal/main.go\ninternal/util.go",
	}

	out, err := RenderRebaseConflict(data, "")
	if err != nil {
		t.Fatalf("RenderRebaseConflict failed: %v", err)
	}

	checks := []string{
		data.PRDDescription,
		"US-001: Add gitops helpers",
		"US-002: Add prompt template",
		data.Progress,
		data.FeatureDiff,
		data.BaseDiff,
		"internal/main.go",
		"internal/util.go",
		"git add",
		"Preserve the intent of the feature",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderChatSystem_IncludesContext(t *testing.T) {
	data := ChatSystemData{
		ProjectName:   "TestProject",
		Config:        "project: TestProject\n",
		Progress:      "## US-001\nDid some work\n",
		RecentCommits: "abc1234 feat: add login\ndef5678 fix: typo\n",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}

	checks := []string{"TestProject", "project: TestProject", "US-001", "abc1234 feat: add login"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderQAVerification_ContainsAllSections(t *testing.T) {
	data := QAVerificationData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
	}

	out, err := RenderQAVerification(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerification failed: %v", err)
	}

	checks := []string{
		// Context values
		data.PRDPath,
		data.ProgressPath,
		"just test",
		"just vet",
		// Key instructions
		"integrationTests",
		"BUILD an automated test",
		"RUN the test",
		"verify autonomously",
		"UPDATE the PRD",
		// Tooling
		"Playwright",
		"npm install",
		// PRD update
		"passes",
		"failure",
		"notes",
		// Skills
		".ralph/skills/",
		"reusable",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderQAFix_ContainsAllSections(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
		FailedTests: []prd.IntegrationTest{
			{
				ID:          "IT-001",
				Description: "User can log in with valid credentials",
				Steps:       []string{"Navigate to login page", "Enter valid credentials", "Click submit"},
				Passes:      false,
				Failure:     "Login button does not respond to clicks",
				Notes:       "Button element is present but click handler missing",
			},
			{
				ID:          "IT-002",
				Description: "Form validation shows errors",
				Steps:       []string{"Submit empty form", "Check error messages"},
				Passes:      false,
				Failure:     "No validation errors shown",
				Notes:       "",
			},
		},
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	checks := []string{
		// Context values
		data.PRDPath,
		data.ProgressPath,
		"just test",
		"just vet",
		// Failed tests rendered
		"IT-001",
		"User can log in with valid credentials",
		"Navigate to login page",
		"Login button does not respond to clicks",
		"Button element is present but click handler missing",
		"IT-002",
		"Form validation shows errors",
		"No validation errors shown",
		// Key instructions
		"FIRST reproduce the failure",
		"automated test",
		"Fix the code",
		// Commit format
		"fix(QA):",
		// Rules
		"never fix blind",
		"One fix per commit",
		"Minimal changes",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

// --- Override tests ---

func TestRender_UsesOverrideTemplateWhenPresent(t *testing.T) {
	dir := t.TempDir()

	// Write a custom loop_iteration.md to the override directory
	customContent := `Custom template for {{.StoryID}}: {{.StoryTitle}}`
	if err := os.WriteFile(filepath.Join(dir, "loop_iteration.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	story := &prd.Story{
		ID:          "US-042",
		Title:       "Custom Story",
		Description: "Testing override",
	}

	out, err := RenderLoopIteration(story, nil, "", "", dir, "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration with override failed: %v", err)
	}

	if !strings.Contains(out, "Custom template for US-042: Custom Story") {
		t.Errorf("expected override template content, got: %s", out)
	}
}

func TestRender_FallsBackToEmbeddedWhenOverrideFileMissing(t *testing.T) {
	dir := t.TempDir()
	// Override directory exists but does NOT contain chat_system.md

	data := ChatSystemData{ProjectName: "FallbackProject"}
	out, err := RenderChatSystem(data, dir)
	if err != nil {
		t.Fatalf("RenderChatSystem with missing override should fall back, got error: %v", err)
	}

	if !strings.Contains(out, "FallbackProject") {
		t.Errorf("expected embedded template to render, got: %s", out)
	}
}

func TestRender_FallsBackToEmbeddedWhenOverrideDirEmpty(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test",
		Description: "Test",
	}

	// Empty string overrideDir should use embedded
	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration with empty overrideDir failed: %v", err)
	}

	if !strings.Contains(out, "US-001") {
		t.Error("expected embedded template to render")
	}
}

func TestRender_OverrideDirNonexistentFallsBackToEmbedded(t *testing.T) {
	data := QAVerificationData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	// Point to a directory that doesn't exist — should silently fall back
	out, err := RenderQAVerification(data, "/nonexistent/path/prompts")
	if err != nil {
		t.Fatalf("expected fallback to embedded, got error: %v", err)
	}

	if !strings.Contains(out, ".ralph/state/prd.json") {
		t.Error("expected embedded template to render with data")
	}
}

func TestRender_OverrideForOneTemplateFallsBackForOthers(t *testing.T) {
	dir := t.TempDir()

	// Only override loop_iteration.md
	customContent := `Override: {{.StoryID}}`
	if err := os.WriteFile(filepath.Join(dir, "loop_iteration.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	// loop_iteration.md should use override
	story := &prd.Story{ID: "US-099", Title: "Overridden", Description: "test"}
	out, err := RenderLoopIteration(story, nil, "", "", dir, "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}
	if !strings.Contains(out, "Override: US-099") {
		t.Errorf("expected override content, got: %s", out)
	}

	// chat_system.md should fall back to embedded
	chatOut, err := RenderChatSystem(ChatSystemData{ProjectName: "MixedTest"}, dir)
	if err != nil {
		t.Fatalf("RenderChatSystem should fall back: %v", err)
	}
	if !strings.Contains(chatOut, "MixedTest") {
		t.Errorf("expected embedded chat template, got: %s", chatOut)
	}
}

func TestConfig_PromptsDir(t *testing.T) {
	// Test readTemplate directly with override
	dir := t.TempDir()
	customContent := `custom rebase template`
	if err := os.WriteFile(filepath.Join(dir, "rebase_conflict.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := readTemplate("templates/rebase_conflict.md", dir)
	if err != nil {
		t.Fatalf("readTemplate failed: %v", err)
	}
	if string(content) != customContent {
		t.Errorf("expected override content, got: %s", content)
	}

	// Without override, should return embedded content
	embeddedContent, err := readTemplate("templates/rebase_conflict.md", "")
	if err != nil {
		t.Fatalf("readTemplate without override failed: %v", err)
	}
	if len(embeddedContent) == 0 {
		t.Error("expected non-empty embedded content")
	}
	if string(embeddedContent) == customContent {
		t.Error("embedded content should differ from override")
	}
}
