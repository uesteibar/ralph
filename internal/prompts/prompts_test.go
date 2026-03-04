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

	out, err := RenderLoopIteration(story, []string{"npm test", "npm run lint"}, ".ralph/progress.txt", "/abs/path/to/prd.json", "", "")
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

func TestRenderLoopIteration_CompletionChecksOnlyUserStories(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	// Verify completion criteria mentions userStories only (not integrationTests)
	if !strings.Contains(out, "userStories") {
		t.Error("completion criteria should mention userStories")
	}
	if !strings.Contains(out, "passes: true") {
		t.Error("completion criteria should mention passes: true")
	}
	if strings.Contains(out, "integrationTests") {
		t.Error("completion criteria should NOT mention integrationTests — QA is managed separately")
	}
}

func TestRenderLoopIteration_ContainsWorkspaceBoundary(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{
		"Workspace Boundary",
		"MUST target files within your current working directory",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderLoopIteration_ContainsNoCoSignInstruction(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
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

func TestRenderLoopIteration_DoesNotContainOverviewSections(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	if strings.Contains(out, "## Feature Overview") {
		t.Error("output should not contain Feature Overview section")
	}
	if strings.Contains(out, "## Architecture Overview") {
		t.Error("output should not contain Architecture Overview section")
	}

	// The template should still reference the PRD path so Claude can read overviews from the file
	if !strings.Contains(out, ".ralph/state/prd.json") {
		t.Error("output should still contain PRD path reference")
	}
}

func TestRenderQAVerify_ContainsNoCoSignInstruction(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	if !strings.Contains(out, "Co-Authored-By") {
		t.Error("qa_verify.md should contain Co-Authored-By instruction")
	}
	if !strings.Contains(out, "Do NOT add Co-Authored-By") {
		t.Error("qa_verify.md should instruct not to add Co-Authored-By headers")
	}
}

func TestRenderQAFix_ContainsNoCoSignInstruction(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
		QAReportPath:  "/path/to/qa-report.md",
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

	// The flow order must be: clarify → feature overview → architecture overview → user stories → QA verification → /finish
	markers := []struct {
		label string
		text  string
	}{
		{"clarifying questions", "clarifying questions"},
		{"Feature Overview", "Proposing Feature Overview"},
		{"Architecture Overview", "Proposing Architecture Overview"},
		{"Story writing", "Story writing guidelines"},
		{"QA Verification", "QA Verification"},
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

func TestRenderChatSystem_WorkspaceBoundary_RenderedForWorkspace(t *testing.T) {
	data := ChatSystemData{
		ProjectName:   "TestProject",
		WorkspaceName: "my-feature",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	checks := []string{
		"Workspace Boundary",
		"my-feature",
		"MUST target files within your current working directory",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("workspace chat output should contain %q", want)
		}
	}
}

func TestRenderChatSystem_WorkspaceBoundary_OmittedForBase(t *testing.T) {
	data := ChatSystemData{
		ProjectName:   "TestProject",
		WorkspaceName: "base",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	if strings.Contains(out, "Workspace Boundary") {
		t.Error("base workspace should not contain Workspace Boundary section")
	}
}

func TestRenderChatSystem_WorkspaceBoundary_OmittedWhenEmpty(t *testing.T) {
	data := ChatSystemData{
		ProjectName: "TestProject",
	}
	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	if strings.Contains(out, "Workspace Boundary") {
		t.Error("empty workspace name should not contain Workspace Boundary section")
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

func TestRenderQAVerify_ContainsAllSections(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
		QAReportPath:  "/ws/qa-report.md",
		QAScriptsPath: "/ws/qa-scripts",
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	checks := []string{
		// Context values
		data.PRDPath,
		data.ProgressPath,
		"just test",
		"just vet",
		data.QAReportPath,
		data.QAScriptsPath,
		// Efficiency guidance
		"Stay focused",
		"Batch work",
		"Browser reuse",
		// Key instructions — hands-on testing
		"Hands-On Testing",
		"hands-on testing is mandatory",
		"QA report",
		// Structured findings
		"qaVerification.findings",
		// Workspace boundary
		"Workspace Boundary",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderQAVerify_ContainsTestRecordingInstructions(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	checks := []string{
		// Tests array reference
		"qaVerification.tests",
		// Required fields in schema example
		"QT-001",
		"description",
		"result",
		"linkedFinding",
		// Both pass and fail must be recorded
		"pass",
		"fail",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q for test recording instructions", want)
		}
	}
}

func TestRenderQAFix_ContainsAllSections(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
		QAReportPath:  "/path/to/qa-report.md",
		QAScriptsPath: "/path/to/qa-scripts",
		Findings: []prd.QAFinding{
			{ID: "QA-001", Title: "Login fails", Severity: "error", TestScript: "test-login.sh", Status: "found"},
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
		// QA paths
		"/path/to/qa-report.md",
		"/path/to/qa-scripts",
		// Finding details
		"QA-001", "Login fails", "error", "test-login.sh",
		// Key instructions — fix persona
		"Reproduce the Failure",
		"You MUST reproduce",
		"Fix the Code",
		// Commit format
		"fix(QA):",
		// Rules
		"One fix per commit",
		"Keep changes surgical",
		"minimal changes",
		// Mark findings addressed
		"addressed",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderRebaseConflict_WithQualityChecks_RendersExplicitCommands(t *testing.T) {
	data := RebaseConflictData{
		PRDDescription: "Test feature",
		Stories:        "- US-001: Test [pending]",
		ConflictFiles:  "main.go",
		QualityChecks:  []string{"just test", "just vet"},
	}

	out, err := RenderRebaseConflict(data, "")
	if err != nil {
		t.Fatalf("RenderRebaseConflict failed: %v", err)
	}

	checks := []string{
		"ralph check just test",
		"ralph check just vet",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderRebaseConflict_WithoutQualityChecks_OmitsCheckSection(t *testing.T) {
	data := RebaseConflictData{
		PRDDescription: "Test feature",
		Stories:        "- US-001: Test [pending]",
		ConflictFiles:  "main.go",
	}

	out, err := RenderRebaseConflict(data, "")
	if err != nil {
		t.Fatalf("RenderRebaseConflict failed: %v", err)
	}

	if strings.Contains(out, "ralph check") {
		t.Error("output should not contain 'ralph check' when QualityChecks is empty")
	}
	// Should still contain the standard rebase instructions
	if !strings.Contains(out, "git rebase --continue") {
		t.Error("output should contain 'git rebase --continue'")
	}
}

func TestRenderRebaseConflict_WithQualityChecks_ContainsLogFileNote(t *testing.T) {
	data := RebaseConflictData{
		PRDDescription: "Test feature",
		ConflictFiles:  "main.go",
		QualityChecks:  []string{"just test"},
	}

	out, err := RenderRebaseConflict(data, "")
	if err != nil {
		t.Fatalf("RenderRebaseConflict failed: %v", err)
	}

	if !strings.Contains(out, "log file") {
		t.Error("output should contain a note about the log file for debugging")
	}
}

// --- ralph check wrapping tests ---

func TestRenderLoopIteration_WrapsQualityChecksWithRalphCheck(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, []string{"just test", "just vet"}, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{
		"ralph check just test",
		"ralph check just vet",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderLoopIteration_ContainsLogFileDebuggingNote(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, []string{"just test"}, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	if !strings.Contains(out, "log file") {
		t.Error("output should contain a note about the log file for debugging")
	}
}

func TestRenderQAVerify_WrapsQualityChecksWithRalphCheck(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	checks := []string{
		"ralph check just test",
		"ralph check just vet",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderQAVerify_ContainsLogFileDebuggingNote(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	if !strings.Contains(out, "log file") {
		t.Error("output should contain a note about the log file for debugging")
	}
}

func TestRenderQAFix_WrapsQualityChecksWithRalphCheck(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test", "just vet"},
		QAReportPath: "/path/to/qa-report.md",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	checks := []string{
		"ralph check just test",
		"ralph check just vet",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderQAFix_ContainsLogFileDebuggingNote(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
		QAReportPath: "/path/to/qa-report.md",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	if !strings.Contains(out, "log file") {
		t.Error("output should contain a note about the log file for debugging")
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

	out, err := RenderLoopIteration(story, nil, "", "", dir, "")
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
	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration with empty overrideDir failed: %v", err)
	}

	if !strings.Contains(out, "US-001") {
		t.Error("expected embedded template to render")
	}
}

func TestRender_OverrideDirNonexistentFallsBackToEmbedded(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
	}

	// Point to a directory that doesn't exist — should silently fall back
	out, err := RenderQAVerify(data, "/nonexistent/path/prompts")
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
	out, err := RenderLoopIteration(story, nil, "", "", dir, "")
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

// --- KnowledgePath field tests ---

func TestRenderLoopIteration_KnowledgePath_PassedThrough(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", ".ralph/knowledge/")
	if err != nil {
		t.Fatalf("RenderLoopIteration with KnowledgePath failed: %v", err)
	}

	// Template renders without error; KnowledgePath is accepted
	if !strings.Contains(out, "US-001") {
		t.Error("output should contain story ID")
	}
}

func TestLoopIterationData_KnowledgePath_Field(t *testing.T) {
	data := LoopIterationData{
		StoryID:       "US-001",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}
	if data.KnowledgePath != "/repo/.ralph/knowledge/" {
		t.Errorf("KnowledgePath = %q, want %q", data.KnowledgePath, "/repo/.ralph/knowledge/")
	}
}

func TestChatSystemData_KnowledgePath_Field(t *testing.T) {
	data := ChatSystemData{
		ProjectName:   "TestProject",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}
	if data.KnowledgePath != "/repo/.ralph/knowledge/" {
		t.Errorf("KnowledgePath = %q, want %q", data.KnowledgePath, "/repo/.ralph/knowledge/")
	}
}

func TestQAVerifyData_KnowledgePath_Field(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}
	if data.KnowledgePath != "/repo/.ralph/knowledge/" {
		t.Errorf("KnowledgePath = %q, want %q", data.KnowledgePath, "/repo/.ralph/knowledge/")
	}
}

func TestQAFixData_KnowledgePath_Field(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}
	if data.KnowledgePath != "/repo/.ralph/knowledge/" {
		t.Errorf("KnowledgePath = %q, want %q", data.KnowledgePath, "/repo/.ralph/knowledge/")
	}
}

// --- Knowledge Base section rendering tests ---

func TestRenderLoopIteration_KnowledgeBase_RenderedWhenPathSet(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "/repo/.ralph/knowledge/")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{
		"Knowledge Base",
		"/repo/.ralph/knowledge/",
		"search",
		"learnings",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when KnowledgePath is set", want)
		}
	}
}

func TestRenderLoopIteration_KnowledgeBase_OmittedWhenPathEmpty(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	if strings.Contains(out, "Knowledge Base") {
		t.Error("output should not contain Knowledge Base section when KnowledgePath is empty")
	}
}

func TestRenderLoopIteration_KnowledgeBase_HasWriteInstructions(t *testing.T) {
	story := &prd.Story{
		ID:          "US-001",
		Title:       "Test Story",
		Description: "Test",
	}

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt", ".ralph/state/prd.json", "", "/repo/.ralph/knowledge/")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	// loop_iteration has read+write: check for write instruction
	if !strings.Contains(out, "write") && !strings.Contains(out, "Write") {
		t.Error("loop_iteration knowledge section should include write instructions")
	}
}

func TestRenderChatSystem_KnowledgeBase_RenderedWhenPathSet(t *testing.T) {
	data := ChatSystemData{
		ProjectName:   "TestProject",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}

	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}

	checks := []string{
		"Knowledge Base",
		"/repo/.ralph/knowledge/",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when KnowledgePath is set", want)
		}
	}
}

func TestRenderChatSystem_KnowledgeBase_OmittedWhenPathEmpty(t *testing.T) {
	data := ChatSystemData{
		ProjectName: "TestProject",
	}

	out, err := RenderChatSystem(data, "")
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}

	if strings.Contains(out, "Knowledge Base") {
		t.Error("output should not contain Knowledge Base section when KnowledgePath is empty")
	}
}

func TestRenderQAVerify_KnowledgeBase_RenderedWhenPathSet(t *testing.T) {
	data := QAVerifyData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
		KnowledgePath: "/repo/.ralph/knowledge/",
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	checks := []string{
		"Knowledge Base",
		"/repo/.ralph/knowledge/",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when KnowledgePath is set", want)
		}
	}
}

func TestRenderQAVerify_KnowledgeBase_OmittedWhenPathEmpty(t *testing.T) {
	data := QAVerifyData{
		PRDPath:      ".ralph/state/prd.json",
		ProgressPath: ".ralph/progress.txt",
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	if strings.Contains(out, "Knowledge Base") {
		t.Error("output should not contain Knowledge Base section when KnowledgePath is empty")
	}
}

func TestRenderQAFix_KnowledgeBase_RenderedWhenPathSet(t *testing.T) {
	data := QAFixData{
		PRDPath:       ".ralph/state/prd.json",
		ProgressPath:  ".ralph/progress.txt",
		QualityChecks: []string{"just test"},
		QAReportPath:  "/path/to/qa-report.md",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	checks := []string{
		"Knowledge Base",
		"/repo/.ralph/knowledge/",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when KnowledgePath is set", want)
		}
	}
}

func TestRenderQAFix_KnowledgeBase_OmittedWhenPathEmpty(t *testing.T) {
	data := QAFixData{
		PRDPath:      ".ralph/state/prd.json",
		ProgressPath: ".ralph/progress.txt",
		QAReportPath: "/path/to/qa-report.md",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	if strings.Contains(out, "Knowledge Base") {
		t.Error("output should not contain Knowledge Base section when KnowledgePath is empty")
	}
}

func TestRenderQAFix_KnowledgeBase_HasWriteInstructions(t *testing.T) {
	data := QAFixData{
		PRDPath:      ".ralph/state/prd.json",
		ProgressPath: ".ralph/progress.txt",
		QAReportPath:  "/path/to/qa-report.md",
		KnowledgePath: "/repo/.ralph/knowledge/",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	// qa_fix has read+write: check for write instruction
	if !strings.Contains(out, "write") && !strings.Contains(out, "Write") {
		t.Error("qa_fix knowledge section should include write instructions")
	}
}

// --- QAInstructions field tests ---

func TestRenderQAVerify_QAInstructions_RenderedWhenPresent(t *testing.T) {
	data := QAVerifyData{
		PRDPath:        ".ralph/state/prd.json",
		ProgressPath:   ".ralph/progress.txt",
		QualityChecks:  []string{"just test"},
		QAInstructions: []string{"Start the app with `just dev`", "Skip flaky network tests"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	checks := []string{
		"Project-Specific QA Instructions",
		"Start the app with `just dev`",
		"Skip flaky network tests",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when QAInstructions is set", want)
		}
	}
}

func TestRenderQAVerify_QAInstructions_OmittedWhenEmpty(t *testing.T) {
	data := QAVerifyData{
		PRDPath:      ".ralph/state/prd.json",
		ProgressPath: ".ralph/progress.txt",
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	if strings.Contains(out, "Project-Specific QA Instructions") {
		t.Error("output should not contain QA Instructions section when QAInstructions is empty")
	}
}

func TestRenderQAVerify_QAInstructions_PlacedAfterContextBeforeWorkflow(t *testing.T) {
	data := QAVerifyData{
		PRDPath:        ".ralph/state/prd.json",
		ProgressPath:   ".ralph/progress.txt",
		QualityChecks:  []string{"just test"},
		QAInstructions: []string{"Custom instruction"},
	}

	out, err := RenderQAVerify(data, "")
	if err != nil {
		t.Fatalf("RenderQAVerify failed: %v", err)
	}

	contextIdx := strings.Index(out, "## Context")
	instructionsIdx := strings.Index(out, "Project-Specific QA Instructions")
	workflowIdx := strings.Index(out, "## Workflow")

	if contextIdx < 0 || instructionsIdx < 0 || workflowIdx < 0 {
		t.Fatalf("expected all sections to be present: context=%d, instructions=%d, workflow=%d", contextIdx, instructionsIdx, workflowIdx)
	}

	if instructionsIdx <= contextIdx {
		t.Error("QA Instructions should appear after Context section")
	}
	if instructionsIdx >= workflowIdx {
		t.Error("QA Instructions should appear before Workflow section")
	}
}

func TestRenderQAFix_QAInstructions_RenderedWhenPresent(t *testing.T) {
	data := QAFixData{
		PRDPath:        ".ralph/state/prd.json",
		ProgressPath:   ".ralph/progress.txt",
		QualityChecks:  []string{"just test"},
		QAReportPath:   "/path/to/qa-report.md",
		QAInstructions: []string{"Use `just dev` to start the app", "Run tests with verbose output"},
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	checks := []string{
		"Project-Specific QA Instructions",
		"Use `just dev` to start the app",
		"Run tests with verbose output",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q when QAInstructions is set", want)
		}
	}
}

func TestRenderQAFix_QAInstructions_OmittedWhenEmpty(t *testing.T) {
	data := QAFixData{
		PRDPath:      ".ralph/state/prd.json",
		ProgressPath: ".ralph/progress.txt",
		QAReportPath: "/path/to/qa-report.md",
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	if strings.Contains(out, "Project-Specific QA Instructions") {
		t.Error("output should not contain QA Instructions section when QAInstructions is empty")
	}
}

func TestRenderQAFix_QAInstructions_PlacedAfterContextBeforeFindings(t *testing.T) {
	data := QAFixData{
		PRDPath:        ".ralph/state/prd.json",
		ProgressPath:   ".ralph/progress.txt",
		QualityChecks:  []string{"just test"},
		QAReportPath:   "/path/to/qa-report.md",
		QAInstructions: []string{"Custom fix instruction"},
		Findings: []prd.QAFinding{
			{ID: "QA-001", Title: "Test bug", Severity: "error", Status: "found"},
		},
	}

	out, err := RenderQAFix(data, "")
	if err != nil {
		t.Fatalf("RenderQAFix failed: %v", err)
	}

	contextIdx := strings.Index(out, "## Context")
	instructionsIdx := strings.Index(out, "Project-Specific QA Instructions")
	findingsIdx := strings.Index(out, "QA Findings to Fix")

	if contextIdx < 0 || instructionsIdx < 0 || findingsIdx < 0 {
		t.Fatalf("expected all sections to be present: context=%d, instructions=%d, findings=%d", contextIdx, instructionsIdx, findingsIdx)
	}

	if instructionsIdx <= contextIdx {
		t.Error("QA Instructions should appear after Context section")
	}
	if instructionsIdx >= findingsIdx {
		t.Error("QA Instructions should appear before QA Findings section")
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
