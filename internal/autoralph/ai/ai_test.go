package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderRefineIssue_ContainsIssueDetails(t *testing.T) {
	data := RefineIssueData{
		Title:       "Add dark mode support",
		Description: "Users want a dark theme toggle in settings.",
	}

	out, err := RenderRefineIssue(data, "")
	if err != nil {
		t.Fatalf("RenderRefineIssue failed: %v", err)
	}

	checks := []string{
		"Add dark mode support",
		"Users want a dark theme toggle in settings.",
		"Clarifying Questions",
		"Implementation Plan",
		"Feature Overview",
		"Architecture Overview",
		"Trade-offs",
		"Mermaid",
		"Changes",
		"Acceptance criteria",
		"Risks",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}

	forbidden := []string{
		"Option A:",
		"Option B:",
		"Option A",
		"Option B",
	}
	for _, bad := range forbidden {
		if strings.Contains(out, bad) {
			t.Errorf("output should not contain %q", bad)
		}
	}
}

func TestRenderRefineIssue_ContainsTypeMarkerInstruction(t *testing.T) {
	data := RefineIssueData{
		Title:       "Test issue",
		Description: "Test description.",
	}

	out, err := RenderRefineIssue(data, "")
	if err != nil {
		t.Fatalf("RenderRefineIssue failed: %v", err)
	}

	markers := []string{
		"<!-- type: plan -->",
		"<!-- type: questions -->",
	}
	for _, marker := range markers {
		if !strings.Contains(out, marker) {
			t.Errorf("output should contain type marker instruction %q", marker)
		}
	}
}

func TestRenderRefineIssue_ContainsAntiPreambleGuideline(t *testing.T) {
	data := RefineIssueData{
		Title:       "Test issue",
		Description: "Test description.",
	}

	out, err := RenderRefineIssue(data, "")
	if err != nil {
		t.Fatalf("RenderRefineIssue failed: %v", err)
	}

	// The template should instruct the AI not to start with formulaic preambles
	if !strings.Contains(out, "preamble") {
		t.Error("output should contain anti-preamble guideline")
	}
}

func TestRenderRefineIssue_WithComments(t *testing.T) {
	data := RefineIssueData{
		Title:       "Fix login bug",
		Description: "Login fails intermittently.",
		Comments: []RefineIssueComment{
			{Author: "alice", CreatedAt: "2026-01-15T10:00:00Z", Body: "Happens on Chrome only."},
			{Author: "bob", CreatedAt: "2026-01-15T11:00:00Z", Body: "Cannot reproduce on Firefox."},
		},
	}

	out, err := RenderRefineIssue(data, "")
	if err != nil {
		t.Fatalf("RenderRefineIssue failed: %v", err)
	}

	checks := []string{
		"alice",
		"2026-01-15T10:00:00Z",
		"Happens on Chrome only.",
		"bob",
		"Cannot reproduce on Firefox.",
		"Existing Comments",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderRefineIssue_WithoutComments_OmitsSection(t *testing.T) {
	data := RefineIssueData{
		Title:       "Add feature",
		Description: "Some feature.",
	}

	out, err := RenderRefineIssue(data, "")
	if err != nil {
		t.Fatalf("RenderRefineIssue failed: %v", err)
	}

	if strings.Contains(out, "Existing Comments") {
		t.Error("output should not contain Existing Comments section when no comments")
	}
}

func TestRenderGeneratePRD_ContainsPlanAndProject(t *testing.T) {
	data := GeneratePRDData{
		PlanText:    "1. Add auth middleware\n2. Create login endpoint",
		ProjectName: "my-app",
	}

	out, err := RenderGeneratePRD(data, "")
	if err != nil {
		t.Fatalf("RenderGeneratePRD failed: %v", err)
	}

	checks := []string{
		"Add auth middleware",
		"Create login endpoint",
		"my-app",
		"userStories",
		"integrationTests",
		"acceptanceCriteria",
		"All quality checks pass",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderGeneratePRD_WithOverviews(t *testing.T) {
	data := GeneratePRDData{
		PlanText:             "Build the feature",
		ProjectName:          "test-project",
		FeatureOverview:      "This adds real-time notifications.",
		ArchitectureOverview: "WebSocket-based push from server.",
	}

	out, err := RenderGeneratePRD(data, "")
	if err != nil {
		t.Fatalf("RenderGeneratePRD failed: %v", err)
	}

	checks := []string{
		"This adds real-time notifications.",
		"WebSocket-based push from server.",
		"Feature Overview",
		"Architecture Overview",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderGeneratePRD_WithoutOverviews_OmitsSections(t *testing.T) {
	data := GeneratePRDData{
		PlanText:    "Build it",
		ProjectName: "test",
	}

	out, err := RenderGeneratePRD(data, "")
	if err != nil {
		t.Fatalf("RenderGeneratePRD failed: %v", err)
	}

	if strings.Contains(out, "Feature Overview") {
		t.Error("output should not contain Feature Overview when empty")
	}
	if strings.Contains(out, "Architecture Overview") {
		t.Error("output should not contain Architecture Overview when empty")
	}
}

func TestRenderPRDescription_ContainsAllSections(t *testing.T) {
	data := PRDescriptionData{
		PRDSummary: "Add user authentication with JWT tokens.",
		Stories: []PRDescriptionStory{
			{ID: "US-001", Title: "Add auth middleware"},
			{ID: "US-002", Title: "Create login endpoint"},
		},
		DiffStats: "5 files changed, 200 insertions(+), 10 deletions(-)",
	}

	out, err := RenderPRDescription(data, "")
	if err != nil {
		t.Fatalf("RenderPRDescription failed: %v", err)
	}

	checks := []string{
		"Add user authentication with JWT tokens.",
		"US-001",
		"Add auth middleware",
		"US-002",
		"Create login endpoint",
		"5 files changed",
		"Stories Implemented",
		"imperative mood",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderPRDescription_WithoutStories_OmitsSection(t *testing.T) {
	data := PRDescriptionData{
		PRDSummary: "Quick fix.",
		DiffStats:  "1 file changed",
	}

	out, err := RenderPRDescription(data, "")
	if err != nil {
		t.Fatalf("RenderPRDescription failed: %v", err)
	}

	if strings.Contains(out, "Stories Implemented") {
		t.Error("output should not contain Stories Implemented when no stories")
	}
}

func TestRenderAddressFeedback_ContainsComments(t *testing.T) {
	data := AddressFeedbackData{
		Comments: []AddressFeedbackComment{
			{Path: "main.go", Line: 42, Author: "reviewer1", Body: "This should use a constant."},
			{Path: "utils.go", Line: 0, Author: "reviewer2", Body: "Missing error check."},
		},
		CodeContext: "func main() { ... }",
	}

	out, err := RenderAddressFeedback(data, "")
	if err != nil {
		t.Fatalf("RenderAddressFeedback failed: %v", err)
	}

	checks := []string{
		"main.go",
		"line 42",
		"reviewer1",
		"This should use a constant.",
		"utils.go",
		"reviewer2",
		"Missing error check.",
		"Code Context",
		"func main() { ... }",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderAddressFeedback_WithoutLineNumber(t *testing.T) {
	data := AddressFeedbackData{
		Comments: []AddressFeedbackComment{
			{Path: "README.md", Author: "reviewer", Body: "Fix typo."},
		},
	}

	out, err := RenderAddressFeedback(data, "")
	if err != nil {
		t.Fatalf("RenderAddressFeedback failed: %v", err)
	}

	if strings.Contains(out, "line 0") {
		t.Error("output should not contain line number when zero")
	}
	if !strings.Contains(out, "README.md") {
		t.Error("output should contain file path")
	}
}

func TestRenderAddressFeedback_WithoutCodeContext_OmitsSection(t *testing.T) {
	data := AddressFeedbackData{
		Comments: []AddressFeedbackComment{
			{Path: "main.go", Author: "reviewer", Body: "Fix this."},
		},
	}

	out, err := RenderAddressFeedback(data, "")
	if err != nil {
		t.Fatalf("RenderAddressFeedback failed: %v", err)
	}

	if strings.Contains(out, "Code Context") {
		t.Error("output should not contain Code Context section when empty")
	}
}

// --- Override tests ---

func TestRender_UsesOverrideWhenPresent(t *testing.T) {
	dir := t.TempDir()
	customContent := `Custom refine for {{.Title}}`
	if err := os.WriteFile(filepath.Join(dir, "refine_issue.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	data := RefineIssueData{Title: "Override Test", Description: "test"}
	out, err := RenderRefineIssue(data, dir)
	if err != nil {
		t.Fatalf("RenderRefineIssue with override failed: %v", err)
	}

	if !strings.Contains(out, "Custom refine for Override Test") {
		t.Errorf("expected override content, got: %s", out)
	}
}

func TestRender_FallsBackToEmbeddedWhenOverrideMissing(t *testing.T) {
	dir := t.TempDir()
	// Override dir exists but does not contain generate_prd.md

	data := GeneratePRDData{PlanText: "plan", ProjectName: "fallback-test"}
	out, err := RenderGeneratePRD(data, dir)
	if err != nil {
		t.Fatalf("RenderGeneratePRD should fall back: %v", err)
	}

	if !strings.Contains(out, "fallback-test") {
		t.Errorf("expected embedded template to render, got: %s", out)
	}
}

func TestRender_FallsBackWhenOverrideDirEmpty(t *testing.T) {
	data := PRDescriptionData{PRDSummary: "test summary", DiffStats: "1 file"}
	out, err := RenderPRDescription(data, "")
	if err != nil {
		t.Fatalf("RenderPRDescription with empty overrideDir failed: %v", err)
	}

	if !strings.Contains(out, "test summary") {
		t.Error("expected embedded template to render")
	}
}

func TestRender_FallsBackWhenOverrideDirNonexistent(t *testing.T) {
	data := AddressFeedbackData{
		Comments: []AddressFeedbackComment{
			{Path: "x.go", Author: "r", Body: "fix"},
		},
	}

	out, err := RenderAddressFeedback(data, "/nonexistent/override/path")
	if err != nil {
		t.Fatalf("expected fallback, got error: %v", err)
	}

	if !strings.Contains(out, "x.go") {
		t.Error("expected embedded template to render")
	}
}

func TestRender_PartialOverride(t *testing.T) {
	dir := t.TempDir()
	customContent := `Override: {{.PlanText}}`
	if err := os.WriteFile(filepath.Join(dir, "generate_prd.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	// generate_prd.md should use override
	prdData := GeneratePRDData{PlanText: "custom plan", ProjectName: "test"}
	prdOut, err := RenderGeneratePRD(prdData, dir)
	if err != nil {
		t.Fatalf("RenderGeneratePRD failed: %v", err)
	}
	if !strings.Contains(prdOut, "Override: custom plan") {
		t.Errorf("expected override content, got: %s", prdOut)
	}

	// refine_issue.md should fall back to embedded
	refineData := RefineIssueData{Title: "Test", Description: "test"}
	refineOut, err := RenderRefineIssue(refineData, dir)
	if err != nil {
		t.Fatalf("RenderRefineIssue should fall back: %v", err)
	}
	if !strings.Contains(refineOut, "Issue Refinement") {
		t.Errorf("expected embedded template header, got: %s", refineOut)
	}
}

func TestTemplateFS_ReturnsEmbeddedFS(t *testing.T) {
	fs := TemplateFS()
	for _, name := range TemplateNames {
		content, err := fs.ReadFile("templates/" + name)
		if err != nil {
			t.Errorf("TemplateFS should contain %s: %v", name, err)
		}
		if len(content) == 0 {
			t.Errorf("template %s should not be empty", name)
		}
	}
}

func TestRenderFixChecks_ContainsFailureDetails(t *testing.T) {
	data := FixChecksData{
		FailedChecks: []FailedCheckRun{
			{Name: "lint", Conclusion: "failure", Log: "Error: unused variable 'x' at main.go:10"},
			{Name: "test", Conclusion: "failure", Log: "FAIL TestLogin: expected 200, got 500"},
		},
	}

	out, err := RenderFixChecks(data, "")
	if err != nil {
		t.Fatalf("RenderFixChecks failed: %v", err)
	}

	checks := []string{
		"lint",
		"failure",
		"unused variable",
		"test",
		"FAIL TestLogin",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestRenderFixChecks_InstructsRootCauseFix(t *testing.T) {
	data := FixChecksData{
		FailedChecks: []FailedCheckRun{
			{Name: "ci", Conclusion: "failure", Log: "error log"},
		},
	}

	out, err := RenderFixChecks(data, "")
	if err != nil {
		t.Fatalf("RenderFixChecks failed: %v", err)
	}

	if !strings.Contains(out, "root cause") {
		t.Error("template should instruct AI to fix root causes")
	}
	if !strings.Contains(out, "minimal") {
		t.Error("template should instruct AI to make minimal changes")
	}
}

func TestRenderFixChecks_WithEmptyLog(t *testing.T) {
	data := FixChecksData{
		FailedChecks: []FailedCheckRun{
			{Name: "build", Conclusion: "failure", Log: ""},
		},
	}

	out, err := RenderFixChecks(data, "")
	if err != nil {
		t.Fatalf("RenderFixChecks failed: %v", err)
	}

	if !strings.Contains(out, "build") {
		t.Error("output should contain check name even without log")
	}
}

func TestRenderFixChecks_OverrideTemplate(t *testing.T) {
	dir := t.TempDir()
	customContent := `Custom fix: {{range .FailedChecks}}{{.Name}}{{end}}`
	if err := os.WriteFile(filepath.Join(dir, "fix_checks.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	data := FixChecksData{
		FailedChecks: []FailedCheckRun{
			{Name: "mycheck", Conclusion: "failure", Log: "log"},
		},
	}

	out, err := RenderFixChecks(data, dir)
	if err != nil {
		t.Fatalf("RenderFixChecks with override failed: %v", err)
	}

	if !strings.Contains(out, "Custom fix: mycheck") {
		t.Errorf("expected override content, got: %s", out)
	}
}

func TestTemplateNames_MatchesEmbeddedFiles(t *testing.T) {
	expected := []string{
		"refine_issue.md",
		"generate_prd.md",
		"pr_description.md",
		"address_feedback.md",
		"fix_checks.md",
	}
	if len(TemplateNames) != len(expected) {
		t.Fatalf("TemplateNames has %d entries, want %d", len(TemplateNames), len(expected))
	}
	for i, name := range expected {
		if TemplateNames[i] != name {
			t.Errorf("TemplateNames[%d] = %q, want %q", i, TemplateNames[i], name)
		}
	}
}

func TestReadTemplate_Override(t *testing.T) {
	dir := t.TempDir()
	customContent := "custom address_feedback template"
	if err := os.WriteFile(filepath.Join(dir, "address_feedback.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := readTemplate("templates/address_feedback.md", dir)
	if err != nil {
		t.Fatalf("readTemplate failed: %v", err)
	}
	if string(content) != customContent {
		t.Errorf("expected override content, got: %s", content)
	}

	// Without override, returns embedded
	embedded, err := readTemplate("templates/address_feedback.md", "")
	if err != nil {
		t.Fatalf("readTemplate without override failed: %v", err)
	}
	if len(embedded) == 0 {
		t.Error("expected non-empty embedded content")
	}
	if string(embedded) == customContent {
		t.Error("embedded content should differ from override")
	}
}
