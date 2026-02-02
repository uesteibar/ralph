package commands

import (
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func TestFormatStories_IncludesAllStories(t *testing.T) {
	stories := []prd.Story{
		{ID: "US-001", Title: "First story", Passes: true},
		{ID: "US-002", Title: "Second story", Passes: false},
	}

	result := formatStories(stories)

	if !strings.Contains(result, "US-001: First story [done]") {
		t.Errorf("expected US-001 with done status, got:\n%s", result)
	}
	if !strings.Contains(result, "US-002: Second story [pending]") {
		t.Errorf("expected US-002 with pending status, got:\n%s", result)
	}
}

func TestFormatStories_EmptyList(t *testing.T) {
	result := formatStories(nil)
	if result != "" {
		t.Errorf("expected empty string for nil stories, got: %q", result)
	}
}
