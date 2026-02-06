package tui

import (
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func TestRenderStoryOverlay_ContainsIDAndTitle(t *testing.T) {
	s := prd.Story{
		ID:    "US-001",
		Title: "Build auth",
	}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "US-001") {
		t.Error("expected content to contain story ID")
	}
	if !strings.Contains(content, "Build auth") {
		t.Error("expected content to contain story title")
	}
}

func TestRenderStoryOverlay_ContainsDescription(t *testing.T) {
	s := prd.Story{
		ID:          "US-001",
		Title:       "Auth",
		Description: "Implement user authentication",
	}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "Description:") {
		t.Error("expected Description label")
	}
	if !strings.Contains(content, "Implement user authentication") {
		t.Error("expected description text")
	}
}

func TestRenderStoryOverlay_ContainsAcceptanceCriteria(t *testing.T) {
	s := prd.Story{
		ID:    "US-001",
		Title: "Auth",
		AcceptanceCriteria: []string{
			"Users can log in",
			"Users can log out",
			"Session persists",
		},
	}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "Acceptance Criteria:") {
		t.Error("expected Acceptance Criteria label")
	}
	for _, ac := range s.AcceptanceCriteria {
		if !strings.Contains(content, ac) {
			t.Errorf("expected acceptance criterion %q in content", ac)
		}
	}
	// Should use bullet list format
	if !strings.Contains(content, "• Users can log in") {
		t.Error("expected bullet format for acceptance criteria")
	}
}

func TestRenderStoryOverlay_ContainsNotes(t *testing.T) {
	s := prd.Story{
		ID:    "US-001",
		Title: "Auth",
		Notes: "Use JWT tokens",
	}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "Notes:") {
		t.Error("expected Notes label")
	}
	if !strings.Contains(content, "Use JWT tokens") {
		t.Error("expected notes text")
	}
}

func TestRenderStoryOverlay_ShowsPassStatus(t *testing.T) {
	s := prd.Story{ID: "US-001", Title: "Auth", Passes: true}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "PASS") {
		t.Error("expected PASS status for passing story")
	}
}

func TestRenderStoryOverlay_ShowsFailStatus(t *testing.T) {
	s := prd.Story{ID: "US-001", Title: "Auth", Passes: false}
	content := renderStoryOverlay(s)
	if !strings.Contains(content, "FAIL") {
		t.Error("expected FAIL status for failing story")
	}
}

func TestRenderTestOverlay_ContainsIDAndDescription(t *testing.T) {
	it := prd.IntegrationTest{
		ID:          "IT-001",
		Description: "Login flow works end to end",
	}
	content := renderTestOverlay(it)
	if !strings.Contains(content, "IT-001") {
		t.Error("expected content to contain test ID")
	}
	if !strings.Contains(content, "Login flow works end to end") {
		t.Error("expected content to contain test description")
	}
}

func TestRenderTestOverlay_ContainsNumberedSteps(t *testing.T) {
	it := prd.IntegrationTest{
		ID:          "IT-001",
		Description: "Login test",
		Steps: []string{
			"Navigate to login page",
			"Enter credentials",
			"Verify redirect",
		},
	}
	content := renderTestOverlay(it)
	if !strings.Contains(content, "Steps:") {
		t.Error("expected Steps label")
	}
	if !strings.Contains(content, "1. Navigate to login page") {
		t.Error("expected numbered step 1")
	}
	if !strings.Contains(content, "2. Enter credentials") {
		t.Error("expected numbered step 2")
	}
	if !strings.Contains(content, "3. Verify redirect") {
		t.Error("expected numbered step 3")
	}
}

func TestRenderTestOverlay_ShowsPassStatus(t *testing.T) {
	it := prd.IntegrationTest{ID: "IT-001", Passes: true}
	content := renderTestOverlay(it)
	if !strings.Contains(content, "PASS") {
		t.Error("expected PASS status")
	}
}

func TestRenderTestOverlay_ShowsFailStatusAndFailure(t *testing.T) {
	it := prd.IntegrationTest{
		ID:      "IT-001",
		Passes:  false,
		Failure: "Login button not found on page",
	}
	content := renderTestOverlay(it)
	if !strings.Contains(content, "FAIL") {
		t.Error("expected FAIL status")
	}
	if !strings.Contains(content, "Failure:") {
		t.Error("expected Failure label")
	}
	if !strings.Contains(content, "Login button not found on page") {
		t.Error("expected failure message")
	}
}

func TestRenderTestOverlay_ContainsNotes(t *testing.T) {
	it := prd.IntegrationTest{
		ID:    "IT-001",
		Notes: "Requires test database",
	}
	content := renderTestOverlay(it)
	if !strings.Contains(content, "Notes:") {
		t.Error("expected Notes label")
	}
	if !strings.Contains(content, "Requires test database") {
		t.Error("expected notes text")
	}
}

func TestOverlay_ShowAndHide(t *testing.T) {
	o := newOverlay()
	if o.visible {
		t.Error("expected overlay to be hidden initially")
	}

	o.show("test content", 20)
	if !o.visible {
		t.Error("expected overlay to be visible after show")
	}
	if o.content != "test content" {
		t.Errorf("expected content 'test content', got %q", o.content)
	}

	o.hide()
	if o.visible {
		t.Error("expected overlay to be hidden after hide")
	}
}

func TestOverlay_ScrollUpDown(t *testing.T) {
	o := newOverlay()
	content := strings.Join(make([]string, 50), "\n") // 50 lines
	o.show(content, 10)

	o.scrollDown()
	if o.scroll != 1 {
		t.Errorf("expected scroll 1 after scrollDown, got %d", o.scroll)
	}

	o.scrollUp()
	if o.scroll != 0 {
		t.Errorf("expected scroll 0 after scrollUp, got %d", o.scroll)
	}

	// Should not scroll below 0
	o.scrollUp()
	if o.scroll != 0 {
		t.Errorf("expected scroll to stay at 0, got %d", o.scroll)
	}
}

func TestOverlay_View_HiddenReturnsEmpty(t *testing.T) {
	o := newOverlay()
	view := o.view(80, 24)
	if view != "" {
		t.Errorf("expected empty string when hidden, got %q", view)
	}
}

func TestRenderHelpOverlay_ContainsAllShortcuts(t *testing.T) {
	content := renderHelpOverlay()
	if !strings.Contains(content, "Keyboard Shortcuts") {
		t.Error("expected title 'Keyboard Shortcuts'")
	}

	shortcuts := []string{"Tab", "Enter", "Esc", "q", "?", "Ctrl+C"}
	for _, s := range shortcuts {
		if !strings.Contains(content, s) {
			t.Errorf("expected help overlay to contain shortcut %q", s)
		}
	}
}

func TestRenderHelpOverlay_ContainsDescriptions(t *testing.T) {
	content := renderHelpOverlay()
	descriptions := []string{
		"Switch focus",
		"Navigate up",
		"Navigate down",
		"Open detail overlay",
		"Close overlay",
		"Toggle this help",
		"Graceful stop",
		"Immediate stop",
	}
	for _, d := range descriptions {
		if !strings.Contains(content, d) {
			t.Errorf("expected help overlay to contain description %q", d)
		}
	}
}

func TestOverlay_View_VisibleReturnsBorderedContent(t *testing.T) {
	o := newOverlay()
	o.show("Hello World", 20)
	view := o.view(80, 24)
	if !strings.Contains(view, "Hello World") {
		t.Error("expected view to contain content")
	}
	// Should have rounded border chars
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╮") {
		t.Error("expected rounded border in overlay view")
	}
}
