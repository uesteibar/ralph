package claude

import "testing"

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

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v should contain %q", args, want)
}
