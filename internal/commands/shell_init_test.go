package commands

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestShellInit_BashOutput_ContainsFunctionDeclaration(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/bash", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		"ralph()",
		"RALPH_SHELL_INIT=1",
		"export RALPH_SHELL_INIT",
		"command ralph",
		"workspaces)",      // outer case for workspaces subcommand
		"new)",             // nested case for workspaces new
		"remove)",          // nested case for workspaces remove
		"done)",            // case for done
		"RALPH_WORKSPACE",  // env var for workspace tracking
		"prd new",          // chain prd new if missing
	}
	for _, s := range mustContain {
		if !containsSubstring(out, s) {
			t.Errorf("bash output missing %q", s)
		}
	}
}

func TestShellInit_ZshOutput_ContainsFunctionDeclaration(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/zsh", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		"ralph()",
		"RALPH_SHELL_INIT=1",
		"export RALPH_SHELL_INIT",
		"command ralph",
	}
	for _, s := range mustContain {
		if !containsSubstring(out, s) {
			t.Errorf("zsh output missing %q", s)
		}
	}
}

func TestShellInit_UnsupportedShell_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/usr/bin/fish", &buf)
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	errMsg := err.Error()
	if !containsSubstring(errMsg, "Currently only bash and zsh are supported") {
		t.Errorf("error should mention supported shells, got: %s", errMsg)
	}
	if !containsSubstring(errMsg, "/usr/bin/fish") {
		t.Errorf("error should mention detected shell, got: %s", errMsg)
	}
}

func TestShellInit_BashOutput_PassesSyntaxCheck(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	var buf bytes.Buffer
	if err := shellInit("/bin/bash", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := exec.Command("bash", "-n")
	cmd.Stdin = &buf
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n syntax check failed: %v\noutput: %s", err, out)
	}
}

func TestShellInit_ZshOutput_PassesSyntaxCheck(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}

	var buf bytes.Buffer
	if err := shellInit("/bin/zsh", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := exec.Command("zsh", "-n")
	cmd.Stdin = &buf
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh -n syntax check failed: %v\noutput: %s", err, out)
	}
}

func TestShellInit_BashUsrBinBash_Supported(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/usr/bin/bash", &buf)
	if err != nil {
		t.Fatalf("expected /usr/bin/bash to be supported, got: %v", err)
	}
}

func TestShellInit_ZshUsrLocalBinZsh_Supported(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/usr/local/bin/zsh", &buf)
	if err != nil {
		t.Fatalf("expected /usr/local/bin/zsh to be supported, got: %v", err)
	}
}

func TestShellInit_BashOutput_CapturesStdoutNotStderr(t *testing.T) {
	var buf bytes.Buffer
	if err := shellInit("/bin/bash", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// The function should capture stdout from 'command ralph' but let stderr pass through
	// Verify the pattern: __output=$(...) captures stdout only
	if !containsSubstring(out, "__output=$(command ralph") {
		t.Error("bash output should capture stdout via $() command substitution")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && bytesContains(s, sub)
}

func bytesContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
