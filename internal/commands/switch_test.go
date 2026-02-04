package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/uesteibar/ralph/internal/workspace"
)

func TestSwitch_MissingShellInit(t *testing.T) {
	t.Setenv("RALPH_SHELL_INIT", "")

	err := Switch(nil)
	if err == nil {
		t.Fatal("expected error for missing shell-init")
	}
	if !strings.Contains(err.Error(), "Shell integration required") {
		t.Errorf("error = %q, want to contain 'Shell integration required'", err.Error())
	}
}

func TestSwitch_InteractivePicker_SelectsWorkspace(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "pick-me"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	// Mock the picker to select the workspace.
	origPicker := switchPickerFn
	defer func() { switchPickerFn = origPicker }()
	switchPickerFn = func(options []huh.Option[string]) (string, error) {
		// Verify base is included as first option.
		if len(options) < 2 {
			t.Errorf("expected at least 2 options (base + workspace), got %d", len(options))
		}
		return "pick-me", nil
	}

	stdout, err := captureStdout(t, func() error {
		return Switch(nil)
	})
	if err != nil {
		t.Fatalf("switch error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 stdout lines (name + path), got %d: %q", len(lines), stdout)
	}
	if lines[0] != "pick-me" {
		t.Errorf("name line = %q, want %q", lines[0], "pick-me")
	}
	expectedPath := workspace.TreePath(dir, "pick-me")
	if lines[1] != expectedPath {
		t.Errorf("path line = %q, want %q", lines[1], expectedPath)
	}
}

func TestSwitch_InteractivePicker_SelectsBase(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Mock the picker to select base.
	origPicker := switchPickerFn
	defer func() { switchPickerFn = origPicker }()
	switchPickerFn = func(options []huh.Option[string]) (string, error) {
		return "base", nil
	}

	stdout, err := captureStdout(t, func() error {
		return Switch(nil)
	})
	if err != nil {
		t.Fatalf("switch error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 stdout lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "base" {
		t.Errorf("name line = %q, want %q", lines[0], "base")
	}
	if lines[1] != dir {
		t.Errorf("path line = %q, want repo root %q", lines[1], dir)
	}
}

func TestSwitch_CancelledSelection_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Mock the picker to simulate cancellation.
	origPicker := switchPickerFn
	defer func() { switchPickerFn = origPicker }()
	switchPickerFn = func(options []huh.Option[string]) (string, error) {
		return "", fmt.Errorf("user cancelled")
	}

	_, err := captureStdout(t, func() error {
		return Switch(nil)
	})
	if err == nil {
		t.Fatal("expected error for cancelled selection")
	}
	if !strings.Contains(err.Error(), "selection cancelled") {
		t.Errorf("error = %q, want to contain 'selection cancelled'", err.Error())
	}
}

func TestSwitch_DirectByName_OutputsTreePath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "direct-ws"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	stdout, err := captureStdout(t, func() error {
		return Switch([]string{"direct-ws"})
	})
	if err != nil {
		t.Fatalf("switch error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 stdout lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "direct-ws" {
		t.Errorf("name line = %q, want %q", lines[0], "direct-ws")
	}
	expectedPath := workspace.TreePath(dir, "direct-ws")
	if lines[1] != expectedPath {
		t.Errorf("path line = %q, want %q", lines[1], expectedPath)
	}
}

func TestSwitch_DirectByName_Base(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	stdout, err := captureStdout(t, func() error {
		return Switch([]string{"base"})
	})
	if err != nil {
		t.Fatalf("switch error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 stdout lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "base" {
		t.Errorf("name line = %q, want %q", lines[0], "base")
	}
	if lines[1] != dir {
		t.Errorf("path line = %q, want repo root %q", lines[1], dir)
	}
}

func TestSwitch_DirectByName_Nonexistent(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	err := Switch([]string{"no-such-workspace"})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}
