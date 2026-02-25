package pr

import (
	"strings"
	"testing"
)

// Integration tests verifying end-to-end PR creation flow through NewAction,
// focusing on how parsePROutput handles AI preamble in the full pipeline.

func TestIntegration_NewAction_PreambleInAIOutput_ExtractsCorrectTitle(t *testing.T) {
	// IT-001: When the AI returns output with preamble text before the
	// conventional commit title, the full PR creation flow should extract
	// the correct title and body, discarding the preamble.

	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	cfg, inv, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	// Mock invoker returns preamble before the actual PR title
	inv.response = "Here is the PR description based on the changes:\n\nfeat(avatars): add user avatar upload\n\n## Summary\nAdds avatar upload support."

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify GitHub PR creation was called
	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}

	call := gh.calls[0]

	// Title must be the conventional commit line, NOT the preamble
	if call.title != "feat(avatars): add user avatar upload" {
		t.Errorf("expected title %q, got %q", "feat(avatars): add user avatar upload", call.title)
	}

	// Body must contain the Summary section
	if !strings.Contains(call.body, "## Summary") {
		t.Errorf("expected body to contain '## Summary', got: %s", call.body)
	}

	// Body must NOT contain the preamble text
	if strings.Contains(call.body, "Here is the PR description") {
		t.Errorf("expected body to NOT contain preamble text, got: %s", call.body)
	}

	// Title must NOT contain the preamble text
	if strings.Contains(call.title, "Here is the PR description") {
		t.Errorf("expected title to NOT contain preamble text")
	}
}

func TestIntegration_NewAction_NoPreamble_WorksUnchanged(t *testing.T) {
	// IT-002: When the AI returns clean output without preamble (the normal
	// happy path), the full PR creation flow should work identically to the
	// existing behavior — title and body extracted correctly.

	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	cfg, inv, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	// Mock invoker returns clean output with no preamble
	inv.response = "feat(avatars): add user avatar upload\n\n## Summary\nAdds avatar upload support."

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify GitHub PR creation was called
	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}

	call := gh.calls[0]

	// Title must be the first line (conventional commit)
	if call.title != "feat(avatars): add user avatar upload" {
		t.Errorf("expected title %q, got %q", "feat(avatars): add user avatar upload", call.title)
	}

	// Body must contain the Summary section
	if !strings.Contains(call.body, "## Summary") {
		t.Errorf("expected body to contain '## Summary', got: %s", call.body)
	}

	// Body must contain the actual content
	if !strings.Contains(call.body, "Adds avatar upload support.") {
		t.Errorf("expected body to contain 'Adds avatar upload support.', got: %s", call.body)
	}

	// Verify this matches existing TestNewAction_CreatesGitHubPR behavior:
	// correct owner, repo, head, base
	if call.owner != "owner" {
		t.Errorf("expected owner %q, got %q", "owner", call.owner)
	}
	if call.repo != "repo" {
		t.Errorf("expected repo %q, got %q", "repo", call.repo)
	}
	if call.head != "autoralph/proj-42" {
		t.Errorf("expected head %q, got %q", "autoralph/proj-42", call.head)
	}
	if call.base != "main" {
		t.Errorf("expected base %q, got %q", "main", call.base)
	}
}

// TestIntegration_NewAction_MultiLinePreamble_ExtractsCorrectTitle exercises
// a more complex preamble scenario where the AI includes multiple lines of
// chain-of-thought reasoning before the structured output.
func TestIntegration_NewAction_MultiLinePreamble_ExtractsCorrectTitle(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	cfg, inv, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	// Multi-line preamble mimicking chain-of-thought
	inv.response = "Let me analyze the changes in this branch.\nBased on the diff, here is the PR:\n\nfeat(avatars): add user avatar upload\n\n## Summary\nAdds avatar upload support."

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}

	call := gh.calls[0]

	if call.title != "feat(avatars): add user avatar upload" {
		t.Errorf("expected title %q, got %q", "feat(avatars): add user avatar upload", call.title)
	}

	if !strings.Contains(call.body, "## Summary") {
		t.Errorf("expected body to contain '## Summary', got: %s", call.body)
	}

	// Preamble must not leak into body
	if strings.Contains(call.body, "Let me analyze") {
		t.Error("expected body to not contain preamble text")
	}
	if strings.Contains(call.body, "Based on the diff") {
		t.Error("expected body to not contain preamble text")
	}
}

// TestIntegration_NewAction_PreambleWithNonConventionalTitle_FallsBack verifies
// that when the AI returns preamble followed by a non-conventional title, the
// fallback logic uses the first non-empty line as the title.
func TestIntegration_NewAction_PreambleWithNonConventionalTitle_FallsBack(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	cfg, inv, _, _, _, gh, _, _ := defaultConfig()
	cfg.Projects = d

	// No conventional commit prefix anywhere — fallback behavior
	inv.response = "Add user avatar upload\n\n## Summary\nAdds avatar upload support."

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gh.calls) != 1 {
		t.Fatalf("expected 1 PR create call, got %d", len(gh.calls))
	}

	call := gh.calls[0]

	// Falls back to first non-empty line as title
	if call.title != "Add user avatar upload" {
		t.Errorf("expected title %q, got %q", "Add user avatar upload", call.title)
	}

	if !strings.Contains(call.body, "## Summary") {
		t.Errorf("expected body to contain '## Summary', got: %s", call.body)
	}
}

// TestIntegration_NewAction_DBAndLinearUpdatedWithCorrectPRInfo verifies
// that when preamble is present, the downstream effects (DB update, Linear
// comment, activity log) all work correctly with the parsed PR data.
func TestIntegration_NewAction_DBAndLinearUpdatedWithCorrectPRInfo(t *testing.T) {
	d := testDB(t)
	project := createTestProject(t, d)
	issue := createTestIssue(t, d, project)

	cfg, inv, _, _, _, _, linear, _ := defaultConfig()
	cfg.Projects = d

	// AI output with preamble
	inv.response = "Here is my analysis:\n\nfeat(avatars): add user avatar upload\n\n## Summary\nAdds avatar upload support."

	action := NewAction(cfg)
	err := action(issue, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify DB was updated with PR info
	updated, err := d.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if updated.PRNumber != 42 {
		t.Errorf("expected PRNumber = 42, got %d", updated.PRNumber)
	}
	if updated.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("expected PRURL = %q, got %q", "https://github.com/owner/repo/pull/42", updated.PRURL)
	}

	// Verify Linear comment was posted
	if len(linear.calls) != 1 {
		t.Fatalf("expected 1 Linear comment call, got %d", len(linear.calls))
	}
	if !strings.Contains(linear.calls[0].body, "#42") {
		t.Errorf("expected Linear comment to contain PR number, got: %s", linear.calls[0].body)
	}

	// Verify activity was logged
	activities, err := d.ListActivity(issue.ID, 10, 0)
	if err != nil {
		t.Fatalf("listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.EventType == "pr_created" {
			found = true
		}
	}
	if !found {
		t.Error("expected pr_created activity to be logged")
	}
}
