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
		{"hit your usage limit", "You've hit your usage limit for Claude", true},
		{"usage limit reached", "Claude AI usage limit reached", true},
		{"usage limit reached with reset", "Claude usage limit reached. Your limit will reset at 1pm (Etc/GMT+5)", true},
		{"usage limit generic", "Your usage limit will reset at 3pm", true},
		{"case insensitive", "you've hit your limit · resets tomorrow", true},
		{"case insensitive usage limit", "USAGE LIMIT reached", true},
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

// TestParseUsageLimit_DetectsLimitInStderrPortion verifies that usage limit
// detection works when the limit message appears in the stderr portion of
// a combined "result + \n + stderr" string (as used by runWithStreamJSON).
func TestParseUsageLimit_DetectsLimitInStderrPortion(t *testing.T) {
	// Simulate: result is empty, stderr contains the limit message.
	// This is the most common real-world scenario — Claude CLI writes
	// the rate limit message to stderr and exits.
	combined := "\n" + "You've hit your limit · resets Feb 11, 2026, 3pm (UTC)"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect limit in stderr portion")
	}

	want := time.Date(2026, time.February, 11, 15, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

// TestParseUsageLimit_HitYourUsageLimit verifies detection of the actual
// Claude CLI message format "You've hit your usage limit" (note the word
// "usage" between "your" and "limit").
func TestParseUsageLimit_HitYourUsageLimit(t *testing.T) {
	output := "You've hit your usage limit for Claude · resets Feb 11, 2026, 5pm (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect 'hit your usage limit'")
	}

	want := time.Date(2026, time.February, 11, 17, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

// TestParseUsageLimit_HitYourUsageLimitInStderr verifies the actual real-world
// scenario: result is empty, stderr has the "usage limit" message.
func TestParseUsageLimit_HitYourUsageLimitInStderr(t *testing.T) {
	// Simulate combined output: empty result + stderr with usage limit
	combined := "\n\n" + "You've hit your usage limit for Claude. Your limit will reset at 5pm (UTC)"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect usage limit in stderr")
	}

	if got.ResetAt.Hour() != 17 {
		t.Errorf("ResetAt.Hour() = %d, want 17", got.ResetAt.Hour())
	}
}

// TestParseUsageLimit_DetectsLimitInResultPortion verifies detection still
// works when the message is in the result portion (original behavior).
func TestParseUsageLimit_DetectsLimitInResultPortion(t *testing.T) {
	// Simulate: result has the limit, stderr is empty.
	combined := "You've hit your limit · resets Feb 11, 2026, 3pm (UTC)" + "\n"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect limit in result portion")
	}

	want := time.Date(2026, time.February, 11, 15, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want)
	}
}

// TestParseUsageLimit_CombinedOutputNoLimit verifies that normal output
// combined with normal stderr does NOT trigger false positive detection.
func TestParseUsageLimit_CombinedOutputNoLimit(t *testing.T) {
	combined := "Here is the implementation\n" + "Some stderr debug output\n"
	got := parseUsageLimit(combined)

	if got != nil {
		t.Errorf("parseUsageLimit should return nil for non-limit output, got %+v", got)
	}
}

// TestParseUsageLimit_StderrWithResetAtPattern verifies the "reset at" time-only
// pattern works when it appears in stderr.
func TestParseUsageLimit_StderrWithResetAtPattern(t *testing.T) {
	combined := "\n" + "Claude usage limit reached. Your limit will reset at 5pm (UTC)"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect limit with reset-at pattern in stderr")
	}

	if got.ResetAt.Hour() != 17 {
		t.Errorf("ResetAt.Hour() = %d, want 17", got.ResetAt.Hour())
	}
}

// TestParseUsageLimit_StderrWithNoResetTime verifies fallback works when
// stderr has limit message but no parseable reset time.
func TestParseUsageLimit_StderrWithNoResetTime(t *testing.T) {
	combined := "\n" + "You've hit your limit"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect bare limit message in stderr")
	}

	// Should fall back to ~30 minutes from now
	expectedMin := time.Now().Add(29 * time.Minute)
	expectedMax := time.Now().Add(31 * time.Minute)
	if got.ResetAt.Before(expectedMin) || got.ResetAt.After(expectedMax) {
		t.Errorf("ResetAt = %v, want between %v and %v", got.ResetAt, expectedMin, expectedMax)
	}
}

// TestParseUsageLimit_ResetsTimeOnly verifies the "resets 8pm (Europe/Madrid)"
// format — the actual format observed from Claude CLI in stream-json mode.
func TestParseUsageLimit_ResetsTimeOnly(t *testing.T) {
	output := "You've hit your limit · resets 8pm (Europe/Madrid)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect 'resets <time> (<tz>)' format")
	}

	if got.ResetAt.Hour() != 20 {
		t.Errorf("ResetAt.Hour() = %d, want 20", got.ResetAt.Hour())
	}

	loc, _ := time.LoadLocation("Europe/Madrid")
	if got.ResetAt.Location().String() != loc.String() {
		t.Errorf("ResetAt.Location() = %s, want %s", got.ResetAt.Location(), loc)
	}
}

// TestParseUsageLimit_ResetsTimeOnlyWithMinutes verifies "resets 3:30pm (UTC)".
func TestParseUsageLimit_ResetsTimeOnlyWithMinutes(t *testing.T) {
	output := "You've hit your limit · resets 3:30pm (UTC)"
	got := parseUsageLimit(output)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil")
	}

	if got.ResetAt.Hour() != 15 || got.ResetAt.Minute() != 30 {
		t.Errorf("ResetAt = %v, want 15:30", got.ResetAt)
	}
}

func TestDisplayName_KnownModels(t *testing.T) {
	tests := []struct {
		rawID string
		want  string
	}{
		{"claude-sonnet-4-5-20250514", "Sonnet 4.5"},
		{"claude-sonnet-4-5-20250929", "Sonnet 4.5"},
		{"claude-opus-4-6", "Opus 4.6"},
		{"claude-haiku-4-5-20251001", "Haiku 4.5"},
	}
	for _, tt := range tests {
		t.Run(tt.rawID, func(t *testing.T) {
			got := displayName(tt.rawID)
			if got != tt.want {
				t.Errorf("displayName(%q) = %q, want %q", tt.rawID, got, tt.want)
			}
		})
	}
}

func TestDisplayName_UnknownModel(t *testing.T) {
	raw := "claude-unknown-model-99"
	got := displayName(raw)
	if got != raw {
		t.Errorf("displayName(%q) = %q, want %q (raw passthrough)", raw, got, raw)
	}
}

func TestDisplayName_EmptyString(t *testing.T) {
	got := displayName("")
	if got != "" {
		t.Errorf("displayName(%q) = %q, want empty", "", got)
	}
}

// TestParseUsageLimit_InAssistantText verifies detection when the rate limit
// message arrives as assistant event text (the most common real-world scenario).
// In stream-json mode, the limit message comes through as Claude's text
// response in an assistant event, not in the result event's result field.
func TestParseUsageLimit_InAssistantText(t *testing.T) {
	// Simulate: result is empty (result event has no text), but assistant
	// event text contains the rate limit message.
	result := ""
	assistantText := "You've hit your limit · resets 8pm (Europe/Madrid)\n"
	stderr := ""

	combined := result + "\n" + assistantText + "\n" + stderr + "\n"
	got := parseUsageLimit(combined)

	if got == nil {
		t.Fatal("parseUsageLimit returned nil — failed to detect limit in assistant text")
	}

	if got.ResetAt.Hour() != 20 {
		t.Errorf("ResetAt.Hour() = %d, want 20", got.ResetAt.Hour())
	}
}
