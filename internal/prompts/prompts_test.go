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
