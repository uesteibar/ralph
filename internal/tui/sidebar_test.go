package tui

import (
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func TestSidebar_UpdateFromPRD_Stories(t *testing.T) {
	s := newSidebar()
	p := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}

	s.updateFromPRD(p, "")

	if len(s.Items()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s.Items()))
	}
	if s.Items()[0].id != "US-001" || !s.Items()[0].passes {
		t.Errorf("expected US-001 passing, got %+v", s.Items()[0])
	}
	if s.Items()[1].id != "US-002" || s.Items()[1].passes {
		t.Errorf("expected US-002 failing, got %+v", s.Items()[1])
	}
}

func TestSidebar_UpdateFromPRD_IntegrationTests(t *testing.T) {
	s := newSidebar()
	p := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Login test", Passes: true},
			{ID: "IT-002", Description: "Signup test", Passes: false},
		},
	}

	s.updateFromPRD(p, "")

	if len(s.Items()) != 3 {
		t.Fatalf("expected 3 items, got %d", len(s.Items()))
	}

	// First item is story
	if s.Items()[0].isTest {
		t.Error("expected first item to be a story, not a test")
	}

	// Second and third are tests
	if !s.Items()[1].isTest || s.Items()[1].id != "IT-001" || !s.Items()[1].passes {
		t.Errorf("expected IT-001 as passing test, got %+v", s.Items()[1])
	}
	if !s.Items()[2].isTest || s.Items()[2].id != "IT-002" || s.Items()[2].passes {
		t.Errorf("expected IT-002 as failing test, got %+v", s.Items()[2])
	}
}

func TestSidebar_UpdateFromPRD_ActiveStory(t *testing.T) {
	s := newSidebar()
	p := &prd.PRD{
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Auth", Passes: true},
			{ID: "US-002", Title: "TUI", Passes: false},
		},
	}

	s.updateFromPRD(p, "US-002")

	if s.Items()[0].active {
		t.Error("expected US-001 to not be active")
	}
	if !s.Items()[1].active {
		t.Error("expected US-002 to be active")
	}
}

func TestSidebar_SetActiveStory(t *testing.T) {
	s := newSidebar()
	s.items = []sidebarItem{
		{id: "US-001", title: "First", active: false},
		{id: "US-002", title: "Second", active: false},
		{id: "US-003", title: "Third", active: false},
	}

	s.setActiveStory("US-002")

	if s.items[0].active || s.items[2].active {
		t.Error("expected only US-002 to be active")
	}
	if !s.items[1].active {
		t.Error("expected US-002 to be active")
	}

	// Clear active
	s.setActiveStory("")
	for _, item := range s.items {
		if item.active {
			t.Errorf("expected no active items after clearing, but %s is active", item.id)
		}
	}
}

func TestSidebar_MoveUpDown(t *testing.T) {
	s := newSidebar()
	s.items = []sidebarItem{
		{id: "US-001"},
		{id: "US-002"},
		{id: "US-003"},
	}

	if s.Cursor() != 0 {
		t.Fatalf("expected initial cursor at 0")
	}

	s.moveDown()
	if s.Cursor() != 1 {
		t.Errorf("expected cursor at 1 after moveDown, got %d", s.Cursor())
	}

	s.moveDown()
	if s.Cursor() != 2 {
		t.Errorf("expected cursor at 2, got %d", s.Cursor())
	}

	// At the end, moveDown should not go past last item
	s.moveDown()
	if s.Cursor() != 2 {
		t.Errorf("expected cursor to stay at 2, got %d", s.Cursor())
	}

	s.moveUp()
	if s.Cursor() != 1 {
		t.Errorf("expected cursor at 1 after moveUp, got %d", s.Cursor())
	}

	s.moveUp()
	s.moveUp() // should not go below 0
	if s.Cursor() != 0 {
		t.Errorf("expected cursor at 0, got %d", s.Cursor())
	}
}

func TestSidebar_UpdateFromPRD_KeepsCursorInBounds(t *testing.T) {
	s := newSidebar()
	s.items = []sidebarItem{
		{id: "US-001"}, {id: "US-002"}, {id: "US-003"},
	}
	s.cursor = 2 // pointing to US-003

	// Update with fewer items
	s.updateFromPRD(&prd.PRD{
		UserStories: []prd.Story{{ID: "US-001", Title: "Auth"}},
	}, "")

	if s.Cursor() != 0 {
		t.Errorf("expected cursor to be clamped to 0, got %d", s.Cursor())
	}
}

func TestSidebar_View_Empty(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	v := s.view()
	if !strings.Contains(v, "No PRD loaded") {
		t.Error("expected 'No PRD loaded' when empty")
	}
}

func TestSidebar_View_ShowsTitle(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	s.items = []sidebarItem{
		{id: "US-001", title: "Auth", passes: true},
	}
	v := s.view()
	if !strings.Contains(v, "Stories & Tests") {
		t.Error("expected view to contain 'Stories & Tests' title")
	}
}

func TestSidebar_View_ShowsStatusIndicators(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	s.items = []sidebarItem{
		{id: "US-001", title: "Auth", passes: true},
		{id: "US-002", title: "TUI", passes: false},
	}

	v := s.view()
	if !strings.Contains(v, "✓") {
		t.Error("expected checkmark for passing story")
	}
	if !strings.Contains(v, "✗") {
		t.Error("expected cross for failing story")
	}
	if !strings.Contains(v, "US-001") {
		t.Error("expected US-001 in view")
	}
	if !strings.Contains(v, "US-002") {
		t.Error("expected US-002 in view")
	}
}

func TestSidebar_View_ShowsCursorWhenFocused(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	s.focused = true
	s.items = []sidebarItem{
		{id: "US-001", title: "Auth", passes: true},
		{id: "US-002", title: "TUI", passes: false},
	}

	v := s.view()
	if !strings.Contains(v, "▸") {
		t.Error("expected cursor indicator '▸' when focused")
	}
}

func TestSidebar_View_NoCursorWhenUnfocused(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	s.focused = false
	s.items = []sidebarItem{
		{id: "US-001", title: "Auth", passes: true},
	}

	v := s.view()
	if strings.Contains(v, "▸") {
		t.Error("expected no cursor indicator when unfocused")
	}
}

func TestSidebar_RenderItem_ActiveStoryHighlighted(t *testing.T) {
	s := newSidebar()
	s.width = 40

	item := sidebarItem{id: "US-001", title: "Auth", passes: false, active: true}
	rendered := s.renderItem(0, item)

	// Active items use a bold yellow style; hard to test style directly,
	// but we can verify the text is present
	if !strings.Contains(rendered, "US-001") {
		t.Error("expected rendered item to contain 'US-001'")
	}
}

func TestSidebar_View_ShowsSeparatorBetweenStoriesAndTests(t *testing.T) {
	s := newSidebar()
	s.width = 40
	s.height = 20
	s.items = []sidebarItem{
		{id: "US-001", title: "Auth", passes: true},
		{id: "IT-001", title: "Login test", passes: true, isTest: true},
	}

	v := s.view()
	if !strings.Contains(v, "─") {
		t.Error("expected separator between stories and tests")
	}
}
