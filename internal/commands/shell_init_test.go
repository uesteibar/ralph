package commands

import (
	"bytes"
	"os/exec"
	"strings"
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

func TestShellInit_BashOutput_ContainsPruneHandler(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/bash", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		"prune)",             // nested case for workspaces prune
		"unset RALPH_WORKSPACE", // should unset env if current was pruned
	}
	for _, s := range mustContain {
		if !containsSubstring(out, s) {
			t.Errorf("bash output missing %q for prune handler", s)
		}
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

func TestShellInit_BashOutput_ContainsNewAlias(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/bash", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// The shell function should handle "ralph new" with PRD creation.
	mustContain := []string{
		`new)`,               // outer case for new alias
		"RALPH_WORKSPACE",    // sets workspace env var
		"prd new",            // chains prd new if missing
	}
	for _, s := range mustContain {
		if !containsSubstring(out, s) {
			t.Errorf("bash output missing %q for new alias", s)
		}
	}
}

func TestShellInit_WorkspacesNew_DoesNotContainPRDCreation(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/bash", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Extract the "workspaces new" block: from "new)" inside the workspaces case
	// to the next ";;" terminator.
	wsBlock := extractCaseBlock(out, "workspaces)", "new)")
	if wsBlock == "" {
		t.Fatal("could not extract workspaces new block from shell output")
	}

	// workspaces new should NOT contain PRD creation
	if containsSubstring(wsBlock, "prd new") {
		t.Error("workspaces new block should NOT contain 'prd new'")
	}
	if containsSubstring(wsBlock, "prd.json") {
		t.Error("workspaces new block should NOT contain 'prd.json'")
	}

	// workspaces new should still create workspace, cd, and set RALPH_WORKSPACE
	mustContain := []string{
		"command ralph",
		"cd \"$__path\"",
		"RALPH_WORKSPACE",
	}
	for _, s := range mustContain {
		if !containsSubstring(wsBlock, s) {
			t.Errorf("workspaces new block missing %q", s)
		}
	}
}

func TestShellInit_RalphNew_ContainsPRDCreation(t *testing.T) {
	var buf bytes.Buffer
	err := shellInit("/bin/bash", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Extract the outer "new)" block (ralph new, not workspaces new).
	// This is the "new)" case at the outer level, after the "workspaces)" block ends.
	outerNewBlock := extractOuterCaseBlock(out, "new)")
	if outerNewBlock == "" {
		t.Fatal("could not extract outer new block from shell output")
	}

	// ralph new SHOULD contain PRD creation
	if !containsSubstring(outerNewBlock, "prd new") {
		t.Error("ralph new block should contain 'prd new'")
	}
	if !containsSubstring(outerNewBlock, "prd.json") {
		t.Error("ralph new block should contain 'prd.json'")
	}

	// ralph new should also create workspace, cd, and set RALPH_WORKSPACE
	mustContain := []string{
		"command ralph",
		"cd \"$__path\"",
		"RALPH_WORKSPACE",
	}
	for _, s := range mustContain {
		if !containsSubstring(outerNewBlock, s) {
			t.Errorf("ralph new block missing %q", s)
		}
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

// extractCaseBlock extracts a nested case block from inside an outer case.
// It finds outerCase first, then finds innerCase within it, and returns
// the content up to the next ";;".
func extractCaseBlock(s, outerCase, innerCase string) string {
	lines := splitLines(s)
	inOuter := false
	for i, line := range lines {
		trimmed := trimSpace(line)
		if trimmed == outerCase {
			inOuter = true
			continue
		}
		if inOuter && trimmed == innerCase {
			// Found the nested case; collect until ";;"
			var b strings.Builder
			for j := i; j < len(lines); j++ {
				b.WriteString(lines[j])
				b.WriteByte('\n')
				if trimSpace(lines[j]) == ";;" {
					return b.String()
				}
			}
		}
		// Stop if we leave the outer case (hit another top-level case)
		if inOuter && trimmed == "esac" {
			break
		}
	}
	return ""
}

// extractOuterCaseBlock extracts a top-level case block from the shell function.
// It skips nested occurrences (those inside a "workspaces)" block) and returns
// the first top-level match.
func extractOuterCaseBlock(s, caseLabel string) string {
	lines := splitLines(s)
	depth := 0 // track case/esac nesting
	for i, line := range lines {
		trimmed := trimSpace(line)
		if trimmed == "case \"$2\" in" || trimmed == "case \"$1\" in" {
			depth++
			continue
		}
		if trimmed == "esac" {
			depth--
			continue
		}
		// Only match at the outermost case level (depth == 1 = inside the $1 case)
		if depth == 1 && trimmed == caseLabel {
			var b strings.Builder
			for j := i; j < len(lines); j++ {
				b.WriteString(lines[j])
				b.WriteByte('\n')
				if trimSpace(lines[j]) == ";;" {
					return b.String()
				}
			}
		}
	}
	return ""
}

