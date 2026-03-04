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

func TestRenderQATestOverlay_ContainsIDAndDescription(t *testing.T) {
	qt := prd.QATest{
		ID:          "QT-001",
		Description: "Login form submits",
		Result:      "pass",
	}
	content := renderQATestOverlay(qt)
	if !strings.Contains(content, "QT-001") {
		t.Error("expected content to contain QA test ID")
	}
	if !strings.Contains(content, "Login form submits") {
		t.Error("expected content to contain QA test description")
	}
}

func TestRenderQATestOverlay_ShowsPassResult(t *testing.T) {
	qt := prd.QATest{ID: "QT-001", Description: "Test", Result: "pass"}
	content := renderQATestOverlay(qt)
	if !strings.Contains(content, "PASS") {
		t.Error("expected PASS for passing QA test")
	}
}

func TestRenderQATestOverlay_ShowsFailResult(t *testing.T) {
	qt := prd.QATest{ID: "QT-001", Description: "Test", Result: "fail"}
	content := renderQATestOverlay(qt)
	if !strings.Contains(content, "FAIL") {
		t.Error("expected FAIL for failing QA test")
	}
}

func TestRenderQATestOverlay_ShowsLinkedFinding(t *testing.T) {
	qt := prd.QATest{
		ID:            "QT-003",
		Description:   "Error handling",
		Result:        "fail",
		LinkedFinding: "QA-001",
	}
	content := renderQATestOverlay(qt)
	if !strings.Contains(content, "Linked Finding:") {
		t.Error("expected Linked Finding label")
	}
	if !strings.Contains(content, "QA-001") {
		t.Error("expected linked finding ID")
	}
}

func TestRenderQATestOverlay_NoLinkedFindingWhenEmpty(t *testing.T) {
	qt := prd.QATest{ID: "QT-001", Description: "Test", Result: "pass"}
	content := renderQATestOverlay(qt)
	if strings.Contains(content, "Linked Finding") {
		t.Error("expected no linked finding section when empty")
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

	shortcuts := []string{"Tab", "Enter", "Esc", "d", "q", "?", "Ctrl+C"}
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
		"Detach from TUI",
		"Stop the running loop",
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
