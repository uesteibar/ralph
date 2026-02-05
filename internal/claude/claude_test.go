package claude

import (
	"testing"
	"time"
)

func TestContainsComplete_WithSignal(t *testing.T) {
	output := "Some output\n<promise>COMPLETE</promise>\nMore output"
	if !ContainsComplete(output) {
		t.Error("expected ContainsComplete to return true")
	}
}

func TestContainsComplete_WithoutSignal(t *testing.T) {
	output := "Some output without the signal"
	if ContainsComplete(output) {
		t.Error("expected ContainsComplete to return false")
	}
}

func TestContainsComplete_Empty(t *testing.T) {
	if ContainsComplete("") {
		t.Error("expected ContainsComplete to return false for empty string")
	}
}

func TestBuildArgs_PrintMode(t *testing.T) {
	args := buildArgs(InvokeOpts{Print: true, Prompt: "test"})
	assertContains(t, args, "--print")
	assertContains(t, args, "--dangerously-skip-permissions")
}

func TestBuildArgs_MaxTurns(t *testing.T) {
	args := buildArgs(InvokeOpts{Print: true, MaxTurns: 10})
	assertContains(t, args, "--max-turns")
	assertContains(t, args, "10")
}

func TestBuildArgs_InteractiveWithPrompt(t *testing.T) {
	args := buildArgs(InvokeOpts{Interactive: true, Prompt: "hello"})
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--system-prompt")
	assertContains(t, args, "hello")
	// Should NOT have --print in interactive mode
	for _, a := range args {
		if a == "--print" {
			t.Error("--print should not be present in interactive mode")
		}
	}
}

func TestBuildArgs_Verbose(t *testing.T) {
	args := buildArgs(InvokeOpts{Print: true, Verbose: true})
	assertContains(t, args, "--verbose")
}

func TestBuildArgs_VerboseFalse(t *testing.T) {
	args := buildArgs(InvokeOpts{Print: true, Verbose: false})
	for _, a := range args {
		if a == "--verbose" {
			t.Error("--verbose should not be present when Verbose is false")
		}
	}
}

func TestBuildArgs_PrintWithPrompt_PromptNotInArgs(t *testing.T) {
	args := buildArgs(InvokeOpts{Print: true, Prompt: "test prompt"})
	// Prompt is passed via stdin, not as CLI argument
	for _, a := range args {
		if a == "test prompt" {
			t.Error("prompt should not be in args (uses stdin)")
		}
	}
}

func TestBuildArgs_Continue(t *testing.T) {
	args := buildArgs(InvokeOpts{Interactive: true, Continue: true})
	assertContains(t, args, "--continue")
}

func TestBuildArgs_ContinueFalse(t *testing.T) {
	args := buildArgs(InvokeOpts{Interactive: true, Continue: false})
	for _, a := range args {
		if a == "--continue" {
			t.Error("--continue should not be present when Continue is false")
		}
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v should contain %q", args, want)
}

func TestIsUsageLimitError(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"hit your limit", "You've hit your limit · resets Jan 1, 2026, 9am (UTC)", true},
		{"usage limit reached", "Claude AI usage limit reached", true},
		{"usage limit reached with reset", "Claude usage limit reached. Your limit will reset at 1pm (Etc/GMT+5)", true},
		{"case insensitive", "you've hit your limit · resets tomorrow", true},
		{"normal output", "Here is the implementation you requested", false},
		{"empty", "", false},
		{"contains limit but not usage", "The rate limit for this API is 100 requests", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUsageLimitError(tt.output)
			if got != tt.want {
				t.Errorf("IsUsageLimitError(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestParseUsageLimit_ReturnsNilForNonLimitOutput(t *testing.T) {
	tests := []string{
		"",
		"Normal Claude output",
		"The code looks correct",
	}
	for _, output := range tests {
		if got := parseUsageLimit(output); got != nil {
			t.Errorf("parseUsageLimit(%q) = %+v, want nil", output, got)
		}
	}
}

func TestParseUsageLimit_ResetsPattern(t *testing.T) {
	output := "You've hit your limit · resets Feb 5, 2026, 3pm (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	want := time.Date(2026, time.February, 5, 15, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
	if got.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestParseUsageLimit_ResetsPatternWithMinutes(t *testing.T) {
	output := "You've hit your limit · resets Feb 5, 2026, 3:30pm (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	want := time.Date(2026, time.February, 5, 15, 30, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

func TestParseUsageLimit_ResetsPatternAM(t *testing.T) {
	output := "You've hit your limit · resets Jan 1, 2026, 9am (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	want := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

func TestParseUsageLimit_ResetAtPatternTimeOnly(t *testing.T) {
	output := "Claude usage limit reached. Your limit will reset at 1pm (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	now := time.Now().UTC()
	wantHour := 13
	wantDay := now.Day()
	if now.Hour() >= wantHour {
		wantDay++ // should be tomorrow
	}

	if got.ResetAt.Hour() != wantHour {
		t.Errorf("ResetAt.Hour() = %d, want %d", got.ResetAt.Hour(), wantHour)
	}
	if got.ResetAt.Day() != wantDay {
		t.Errorf("ResetAt.Day() = %d, want %d", got.ResetAt.Day(), wantDay)
	}
}

func TestParseUsageLimit_FallbackWhenUnparseable(t *testing.T) {
	output := "You've hit your limit"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	// Should fall back to ~30 minutes from now
	expectedMin := time.Now().Add(29 * time.Minute)
	expectedMax := time.Now().Add(31 * time.Minute)
	if got.ResetAt.Before(expectedMin) || got.ResetAt.After(expectedMax) {
		t.Errorf("ResetAt = %v, want between %v and %v", got.ResetAt, expectedMin, expectedMax)
	}
}

func TestParseUsageLimit_MultilineOutput(t *testing.T) {
	output := "Some preamble\nYou've hit your limit · resets Mar 10, 2026, 2pm (UTC)\nSome trailing text"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil, want non-nil")
	}

	want := time.Date(2026, time.March, 10, 14, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

func TestUsageLimitError_ErrorString(t *testing.T) {
	err := &UsageLimitError{
		ResetAt: time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC),
		Message: "You've hit your limit",
	}
	got := err.Error()
	if got == "" {
		t.Error("Error() should not return empty string")
	}
}
