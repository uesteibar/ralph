package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/workspace"
)

func TestPrintWorkspaceHeader_Base(t *testing.T) {
	// Capture stderr output
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	wc := workspace.WorkContext{Name: "base", WorkDir: "/tmp", PRDPath: "/tmp/prd.json"}
	printWorkspaceHeader(wc, t.TempDir())

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if output == "" {
		t.Error("expected header output, got empty string")
	}
	if !strings.Contains(output, "[workspace: base]") {
		t.Errorf("expected header to contain '[workspace: base]', got %q", output)
	}
}

func TestPrintWorkspaceHeader_Workspace(t *testing.T) {
	repoPath := t.TempDir()
	stateDir := filepath.Join(repoPath, ".ralph", "state")
	os.MkdirAll(stateDir, 0755)

	// Create a workspace registry entry
	ws := workspace.Workspace{Name: "login-page", Branch: "ralph/login-page"}
	data, _ := json.Marshal([]workspace.Workspace{ws})
	os.WriteFile(filepath.Join(stateDir, "workspaces.json"), data, 0644)

	// Create the workspace directory so RegistryGet doesn't fail
	wsDir := filepath.Join(repoPath, ".ralph", "workspaces", "login-page")
	os.MkdirAll(wsDir, 0755)

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	wc := workspace.WorkContext{Name: "login-page", WorkDir: "/tmp/tree", PRDPath: "/tmp/prd.json"}
	printWorkspaceHeader(wc, repoPath)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "[workspace: login-page | ralph/login-page]") {
		t.Errorf("expected header with branch, got %q", output)
	}
}

func TestCheckLegacyWorktrees_WarnsWhenPresent(t *testing.T) {
	repoPath := t.TempDir()
	legacyDir := filepath.Join(repoPath, ".ralph", "worktrees")
	os.MkdirAll(legacyDir, 0755)

	// Also need a config for CheckLegacyWorktrees
	// Use the internal function instead (it takes repoPath directly)
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkLegacyWorktreesInDir(repoPath)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "Legacy worktrees directory at .ralph/worktrees/ is no longer used. Consider removing it."
	if !strings.Contains(output, expected) {
		t.Errorf("expected migration warning, got %q", output)
	}
}

func TestCheckLegacyWorktrees_SilentWhenAbsent(t *testing.T) {
	repoPath := t.TempDir()

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkLegacyWorktreesInDir(repoPath)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if output != "" {
		t.Errorf("expected no output when legacy dir absent, got %q", output)
	}
}

