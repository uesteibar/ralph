package approve

import "testing"

func TestResponseNeedsApproval(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{
			name:     "plan marker present",
			response: "<!-- type: plan -->\n## Implementation Plan\n\n1. Add endpoint",
			want:     true,
		},
		{
			name:     "questions marker present",
			response: "<!-- type: questions -->\n## Clarifying Questions\n\n1. What format?",
			want:     false,
		},
		{
			name:     "no marker (safe default)",
			response: "## Implementation Plan\n\n1. Add endpoint",
			want:     true,
		},
		{
			name:     "plan marker with leading whitespace",
			response: "  <!-- type: plan -->\n## Plan",
			want:     true,
		},
		{
			name:     "questions marker with leading whitespace",
			response: "\n<!-- type: questions -->\nWhat format do you need?",
			want:     false,
		},
		{
			name:     "plan marker not at start (embedded in body)",
			response: "Some preamble\n<!-- type: plan -->\n## Plan",
			want:     true,
		},
		{
			name:     "questions marker not at start (embedded in body)",
			response: "Some preamble\n<!-- type: questions -->\nWhat format?",
			want:     false,
		},
		{
			name:     "empty response (safe default)",
			response: "",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResponseNeedsApproval(tt.response)
			if got != tt.want {
				t.Errorf("ResponseNeedsApproval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripTypeMarker(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "strips plan marker",
			response: "<!-- type: plan -->\n## Implementation Plan",
			want:     "## Implementation Plan",
		},
		{
			name:     "strips questions marker",
			response: "<!-- type: questions -->\n## Clarifying Questions",
			want:     "## Clarifying Questions",
		},
		{
			name:     "no marker unchanged",
			response: "## Implementation Plan\n\n1. Add endpoint",
			want:     "## Implementation Plan\n\n1. Add endpoint",
		},
		{
			name:     "strips marker with leading whitespace",
			response: "  <!-- type: plan -->\n## Plan",
			want:     "## Plan",
		},
		{
			name:     "strips marker with leading newline",
			response: "\n<!-- type: questions -->\nQuestions here",
			want:     "Questions here",
		},
		{
			name:     "empty response unchanged",
			response: "",
			want:     "",
		},
		{
			name:     "strips only first occurrence",
			response: "<!-- type: plan -->\nSome text\n<!-- type: plan -->\nMore text",
			want:     "Some text\n<!-- type: plan -->\nMore text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripTypeMarker(tt.response)
			if got != tt.want {
				t.Errorf("StripTypeMarker() = %q, want %q", got, tt.want)
			}
		})
	}
}
