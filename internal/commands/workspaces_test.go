package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// initTestRepo creates a bare-minimum git repo in dir with one initial commit
// and a .ralph/ralph.yaml config.
func initTestRepo(t *testing.T, dir string) *shell.Runner {
	t.Helper()
	r := &shell.Runner{Dir: dir}
	ctx := context.Background()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		if _, err := r.Run(ctx, c[0], c[1:]...); err != nil {
			t.Fatalf("init repo %v: %v", c, err)
		}
	}

	// Create .ralph/ralph.yaml config.
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(filepath.Join(ralphDir, "state"), 0755); err != nil {
		t.Fatal(err)
	}
	configContent := "project: test-project\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create README and initial commit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}

	// Rename default branch to main.
	if _, err := r.Run(ctx, "git", "branch", "-M", "main"); err != nil {
		t.Fatal(err)
	}

	return r
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving symlinks for %s: %v", path, err)
	}
	return resolved
}

// captureStdout captures stdout from a function call, returning the output and error.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fnErr := fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	return buf.String(), fnErr
}

func TestWorkspacesNew_SuccessfulCreation(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	// Set shell-init env and chdir to repo so config discovery works.
	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "my-feature"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("workspacesNew error: %v", err)
	}

	// Verify stdout contains path to tree/ directory.
	expectedTreePath := workspace.TreePath(dir, "my-feature")
	stdoutTrimmed := strings.TrimSpace(stdout)
	if stdoutTrimmed != expectedTreePath {
		t.Errorf("stdout = %q, want %q", stdoutTrimmed, expectedTreePath)
	}

	// Verify tree/ directory exists.
	if _, err := os.Stat(expectedTreePath); err != nil {
		t.Errorf("tree/ directory should exist: %v", err)
	}

	// Verify registry entry.
	list, err := workspace.RegistryList(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("registry has %d entries, want 1", len(list))
	}
	if list[0].Name != "my-feature" {
		t.Errorf("registry entry = %q, want %q", list[0].Name, "my-feature")
	}
	if list[0].Branch != "ralph/my-feature" {
		t.Errorf("registry branch = %q, want %q", list[0].Branch, "ralph/my-feature")
	}

	// Verify workspace.json exists.
	wsJSON, err := workspace.ReadWorkspaceJSON(dir, "my-feature")
	if err != nil {
		t.Fatalf("reading workspace.json: %v", err)
	}
	if wsJSON.Name != "my-feature" {
		t.Errorf("workspace.json Name = %q, want %q", wsJSON.Name, "my-feature")
	}
}

func TestWorkspacesNew_DuplicateNameError(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create first workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "duplicate"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("first creation error: %v", err)
	}

	// Try to create the same workspace again.
	_, err = captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "duplicate"}, strings.NewReader(""))
	})
	if err == nil {
		t.Fatal("expected error for duplicate workspace")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want to contain 'already exists'", err.Error())
	}
	if !strings.Contains(err.Error(), "ralph workspaces switch") {
		t.Errorf("error = %q, want to contain 'ralph workspaces switch'", err.Error())
	}
}

func TestWorkspacesNew_InvalidNameError(t *testing.T) {
	t.Setenv("RALPH_SHELL_INIT", "1")

	tests := []struct {
		name    string
		wantErr string
	}{
		{"", "usage: ralph workspaces new"},
		{"has spaces", "must match"},
		{"has/slash", "must match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"new"}
			if tt.name != "" {
				args = append(args, tt.name)
			}
			err := workspacesDispatch(args, strings.NewReader(""))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestWorkspacesNew_MissingShellInitError(t *testing.T) {
	t.Setenv("RALPH_SHELL_INIT", "")

	err := workspacesDispatch([]string{"new", "my-feature"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for missing shell-init")
	}
	if !strings.Contains(err.Error(), "Shell integration required") {
		t.Errorf("error = %q, want to contain 'Shell integration required'", err.Error())
	}
	if !strings.Contains(err.Error(), `eval "$(ralph shell-init)"`) {
		t.Errorf("error = %q, want to contain eval command", err.Error())
	}
}

func TestWorkspacesNew_ExistingBranch_Resume(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initTestRepo(t, dir)
	ctx := context.Background()

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create a branch manually with a commit.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "ralph/existing-feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("from existing branch"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "commit on existing branch"); err != nil {
		t.Fatal(err)
	}

	// Go back to main.
	if _, err := r.Run(ctx, "git", "checkout", "main"); err != nil {
		t.Fatal(err)
	}

	// Simulate "resume" choice.
	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "existing-feature"}, strings.NewReader("resume\n"))
	})
	if err != nil {
		t.Fatalf("workspacesNew error: %v", err)
	}

	// Verify workspace was created.
	expectedTreePath := workspace.TreePath(dir, "existing-feature")
	stdoutTrimmed := strings.TrimSpace(stdout)
	if stdoutTrimmed != expectedTreePath {
		t.Errorf("stdout = %q, want %q", stdoutTrimmed, expectedTreePath)
	}

	// Verify the existing commit's file is present in the worktree.
	data, err := os.ReadFile(filepath.Join(expectedTreePath, "existing.txt"))
	if err != nil {
		t.Fatalf("expected existing.txt in worktree: %v", err)
	}
	if string(data) != "from existing branch" {
		t.Errorf("file content = %q, want %q", string(data), "from existing branch")
	}
}

func TestWorkspacesNew_ExistingBranch_Fresh(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initTestRepo(t, dir)
	ctx := context.Background()

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create a branch manually.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "ralph/fresh-test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old-file.txt"), []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "old commit"); err != nil {
		t.Fatal(err)
	}

	// Go back to main.
	if _, err := r.Run(ctx, "git", "checkout", "main"); err != nil {
		t.Fatal(err)
	}

	// Simulate "fresh" choice — deletes branch and creates new.
	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "fresh-test"}, strings.NewReader("fresh\n"))
	})
	if err != nil {
		t.Fatalf("workspacesNew error: %v", err)
	}

	expectedTreePath := workspace.TreePath(dir, "fresh-test")
	stdoutTrimmed := strings.TrimSpace(stdout)
	if stdoutTrimmed != expectedTreePath {
		t.Errorf("stdout = %q, want %q", stdoutTrimmed, expectedTreePath)
	}

	// Verify old file is NOT present (fresh start).
	if _, err := os.Stat(filepath.Join(expectedTreePath, "old-file.txt")); !os.IsNotExist(err) {
		t.Error("old-file.txt should NOT exist in fresh workspace")
	}
}

func TestWorkspaces_SubRouting(t *testing.T) {
	err := workspacesDispatch([]string{"invalid"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown workspaces subcommand") {
		t.Errorf("error = %q, want to contain 'unknown workspaces subcommand'", err.Error())
	}
}

// --- workspacesList tests ---

func TestWorkspacesList_EmptyRegistry(t *testing.T) {
	t.Setenv("RALPH_WORKSPACE", "") // Clear env to ensure base context
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"list"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should show base with current marker.
	if !strings.Contains(stdout, "base") {
		t.Errorf("stdout should contain 'base', got %q", stdout)
	}
	if !strings.Contains(stdout, "[current]") {
		t.Errorf("stdout should contain '[current]' for base, got %q", stdout)
	}
	// Should show hint to create a workspace.
	if !strings.Contains(stdout, "ralph workspaces new") {
		t.Errorf("stdout should contain creation hint, got %q", stdout)
	}
}

func TestWorkspacesList_WithWorkspaces(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create two workspaces.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "feature-a"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating feature-a: %v", err)
	}
	_, err = captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "feature-b"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating feature-b: %v", err)
	}

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"list"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if !strings.Contains(stdout, "base") {
		t.Errorf("stdout should contain 'base', got %q", stdout)
	}
	if !strings.Contains(stdout, "feature-a") {
		t.Errorf("stdout should contain 'feature-a', got %q", stdout)
	}
	if !strings.Contains(stdout, "feature-b") {
		t.Errorf("stdout should contain 'feature-b', got %q", stdout)
	}
}

func TestWorkspacesList_CurrentMarked(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "active-ws"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	// Set env var to mark active-ws as current.
	t.Setenv("RALPH_WORKSPACE", "active-ws")

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"list"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	// active-ws should be marked as current.
	lines := strings.Split(stdout, "\n")
	foundCurrent := false
	for _, line := range lines {
		if strings.Contains(line, "active-ws") && strings.Contains(line, "[current]") {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Errorf("active-ws should be marked [current], got:\n%s", stdout)
	}

	// base should NOT be marked current.
	for _, line := range lines {
		if strings.Contains(line, "base") && strings.Contains(line, "[current]") {
			t.Errorf("base should NOT be marked [current] when in workspace, got:\n%s", stdout)
		}
	}
}

func TestWorkspacesList_MissingDir(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "ghost-ws"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	// Remove workspace directory manually (simulating deletion).
	wsDir := workspace.WorkspacePath(dir, "ghost-ws")
	if err := os.RemoveAll(wsDir); err != nil {
		t.Fatal(err)
	}

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"list"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if !strings.Contains(stdout, "[missing]") {
		t.Errorf("stdout should contain '[missing]' for ghost-ws, got %q", stdout)
	}
}

// --- workspacesSwitch tests ---

func TestWorkspacesSwitch_OutputsCorrectPath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "switch-target"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"switch", "switch-target"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("switch error: %v", err)
	}

	expectedPath := workspace.TreePath(dir, "switch-target")
	if strings.TrimSpace(stdout) != expectedPath {
		t.Errorf("stdout = %q, want %q", strings.TrimSpace(stdout), expectedPath)
	}
}

func TestWorkspacesSwitch_ToBase(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"switch", "base"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("switch to base error: %v", err)
	}

	if strings.TrimSpace(stdout) != dir {
		t.Errorf("stdout = %q, want repo root %q", strings.TrimSpace(stdout), dir)
	}
}

func TestWorkspacesSwitch_Nonexistent(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"switch", "nonexistent"}, strings.NewReader(""))
	})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
	if !strings.Contains(err.Error(), "ralph workspaces list") {
		t.Errorf("error = %q, want to contain 'ralph workspaces list'", err.Error())
	}
}

func TestWorkspacesSwitch_MissingDirectory(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace then delete its directory.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "missing-dir"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	wsDir := workspace.WorkspacePath(dir, "missing-dir")
	if err := os.RemoveAll(wsDir); err != nil {
		t.Fatal(err)
	}

	_, err = captureStdout(t, func() error {
		return workspacesDispatch([]string{"switch", "missing-dir"}, strings.NewReader(""))
	})
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if !strings.Contains(err.Error(), "directory is missing") {
		t.Errorf("error = %q, want to contain 'directory is missing'", err.Error())
	}
	if !strings.Contains(err.Error(), "ralph workspaces remove") {
		t.Errorf("error = %q, want to contain 'ralph workspaces remove'", err.Error())
	}
}

func TestWorkspacesSwitch_MissingShellInit(t *testing.T) {
	t.Setenv("RALPH_SHELL_INIT", "")

	err := workspacesDispatch([]string{"switch", "some-ws"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for missing shell-init")
	}
	if !strings.Contains(err.Error(), "Shell integration required") {
		t.Errorf("error = %q, want to contain 'Shell integration required'", err.Error())
	}
}

func TestPromptBranchChoice_ValidChoices(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"fresh\n", "fresh"},
		{"resume\n", "resume"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := promptBranchChoice("ralph/test", strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptBranchChoice_InvalidChoice(t *testing.T) {
	_, err := promptBranchChoice("ralph/test", strings.NewReader("neither\n"))
	if err == nil {
		t.Fatal("expected error for invalid choice")
	}
	if !strings.Contains(err.Error(), "invalid choice") {
		t.Errorf("error = %q, want to contain 'invalid choice'", err.Error())
	}
}

// --- workspacesRemove tests ---

func TestWorkspacesRemove_VerifyCleanup(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "remove-me"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	// Verify it exists.
	list, _ := workspace.RegistryList(dir)
	if len(list) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(list))
	}

	// Remove it (not current workspace since we're in repo root).
	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"remove", "remove-me"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("remove error: %v", err)
	}

	// No stdout when removing non-current workspace.
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty for non-current removal, got %q", stdout)
	}

	// Verify workspace directory removed.
	wsDir := workspace.WorkspacePath(dir, "remove-me")
	if _, statErr := os.Stat(wsDir); !os.IsNotExist(statErr) {
		t.Error("workspace directory should be removed")
	}

	// Verify registry cleaned.
	list, _ = workspace.RegistryList(dir)
	if len(list) != 0 {
		t.Errorf("registry should be empty, got %d entries", len(list))
	}
}

func TestWorkspacesRemove_CurrentWorkspace_OutputsBasePath(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	t.Setenv("RALPH_SHELL_INIT", "1")
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create workspace.
	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"new", "current-ws"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("creating workspace: %v", err)
	}

	// Set RALPH_WORKSPACE to make it the current workspace.
	t.Setenv("RALPH_WORKSPACE", "current-ws")

	stdout, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"remove", "current-ws"}, strings.NewReader(""))
	})
	if err != nil {
		t.Fatalf("remove error: %v", err)
	}

	// Should output base repo path when removing current workspace.
	if strings.TrimSpace(stdout) != dir {
		t.Errorf("stdout = %q, want repo root %q", strings.TrimSpace(stdout), dir)
	}
}

func TestWorkspacesRemove_Nonexistent_Error(t *testing.T) {
	dir := realPath(t, t.TempDir())
	initTestRepo(t, dir)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	_, err := captureStdout(t, func() error {
		return workspacesDispatch([]string{"remove", "nonexistent"}, strings.NewReader(""))
	})
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}
