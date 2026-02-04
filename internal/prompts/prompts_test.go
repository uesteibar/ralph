package prompts

import (
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

	out, err := RenderLoopIteration(story, []string{"npm test", "npm run lint"}, ".ralph/progress.txt")
	if err != nil {
		t.Fatalf("RenderLoopIteration failed: %v", err)
	}

	checks := []string{"US-001", "Add user login", "npm test", "npm run lint", "<promise>COMPLETE</promise>", ".ralph/progress.txt"}
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

	out, err := RenderLoopIteration(story, nil, ".ralph/progress.txt")
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

func TestRenderPRDNew_ContainsProjectName(t *testing.T) {
	out, err := RenderPRDNew("MyProject")
	if err != nil {
		t.Fatalf("RenderPRDNew failed: %v", err)
	}
	if !strings.Contains(out, "MyProject") {
		t.Error("output should contain project name")
	}
}

func TestRenderChatSystem_ContainsProjectName(t *testing.T) {
	out, err := RenderChatSystem(ChatSystemData{ProjectName: "ChatProject"})
	if err != nil {
		t.Fatalf("RenderChatSystem failed: %v", err)
	}
	if !strings.Contains(out, "ChatProject") {
		t.Error("output should contain project name")
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

	out, err := RenderRebaseConflict(data)
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
	out, err := RenderChatSystem(data)
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

	out, err := RenderQAVerification(data)
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

	out, err := RenderQAFix(data)
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
