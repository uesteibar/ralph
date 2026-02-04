package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/shell"
)

// initRepo creates a bare-minimum git repo in dir with one initial commit.
func initRepo(t *testing.T, dir string) *shell.Runner {
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

	// Create an initial commit so HEAD exists.
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "initial"); err != nil {
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

func TestCreateWorkspace_CreatesStructure(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	// Set up .ralph/ with config and a prompt file.
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(filepath.Join(ralphDir, "prompts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte("project: test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "prompts", "loop.md"), []byte("# loop"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up .ralph/state/ (should NOT be copied into tree)
	stateDir := filepath.Join(ralphDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "prd.json"), []byte(`{"project":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up .ralph/workspaces/ (should NOT be copied into tree)
	wsRootDir := filepath.Join(ralphDir, "workspaces")
	if err := os.MkdirAll(wsRootDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsRootDir, "dummy.txt"), []byte("should not copy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit the .ralph directory so git worktree add works cleanly.
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "add ralph config"); err != nil {
		t.Fatal(err)
	}

	// Get default branch.
	branchOut, err := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	defaultBranch := strings.TrimSpace(branchOut)

	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	ws := Workspace{Name: "test-ws", Branch: "ralph/test-ws", CreatedAt: now}

	err = CreateWorkspace(ctx, r, dir, ws, defaultBranch, nil)
	if err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Verify workspace.json exists.
	wsJSON, err := ReadWorkspaceJSON(dir, "test-ws")
	if err != nil {
		t.Fatalf("reading workspace.json: %v", err)
	}
	if wsJSON.Name != "test-ws" {
		t.Errorf("workspace.json Name = %q, want %q", wsJSON.Name, "test-ws")
	}
	if wsJSON.Branch != "ralph/test-ws" {
		t.Errorf("workspace.json Branch = %q, want %q", wsJSON.Branch, "ralph/test-ws")
	}

	// Verify tree/ directory exists and is a git worktree.
	treePath := TreePath(dir, "test-ws")
	if _, err := os.Stat(treePath); err != nil {
		t.Fatalf("tree/ directory should exist: %v", err)
	}

	// Verify .ralph/ was copied into tree (config + prompts).
	copiedConfig := filepath.Join(treePath, ".ralph", "ralph.yaml")
	data, err := os.ReadFile(copiedConfig)
	if err != nil {
		t.Fatalf("expected .ralph/ralph.yaml in tree: %v", err)
	}
	if string(data) != "project: test" {
		t.Errorf("copied config content = %q", string(data))
	}

	copiedPrompt := filepath.Join(treePath, ".ralph", "prompts", "loop.md")
	if _, err := os.Stat(copiedPrompt); err != nil {
		t.Fatalf("expected .ralph/prompts/loop.md in tree: %v", err)
	}

	// Verify .ralph/state/ was NOT copied into tree.
	treeStatePath := filepath.Join(treePath, ".ralph", "state")
	if _, err := os.Stat(treeStatePath); !os.IsNotExist(err) {
		t.Error("tree/.ralph/state/ should NOT exist")
	}

	// Verify .ralph/workspaces/ was NOT copied into tree.
	treeWorkspacesPath := filepath.Join(treePath, ".ralph", "workspaces")
	if _, err := os.Stat(treeWorkspacesPath); !os.IsNotExist(err) {
		t.Error("tree/.ralph/workspaces/ should NOT exist")
	}

	// Verify registry was updated.
	list, err := RegistryList(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("registry has %d entries, want 1", len(list))
	}
	if list[0].Name != "test-ws" {
		t.Errorf("registry entry = %q, want %q", list[0].Name, "test-ws")
	}
}

func TestCreateWorkspace_CopiesDotClaude(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	// Set up .claude/ directory.
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"k":"v"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "commands", "finish.md"), []byte("# finish"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit so worktree creation works cleanly.
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "add claude"); err != nil {
		t.Fatal(err)
	}

	branchOut, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := strings.TrimSpace(branchOut)

	ws := Workspace{Name: "claude-ws", Branch: "ralph/claude-ws", CreatedAt: time.Now()}

	err := CreateWorkspace(ctx, r, dir, ws, defaultBranch, nil)
	if err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Verify .claude/ was copied into tree/.
	treePath := TreePath(dir, "claude-ws")
	copiedSettings := filepath.Join(treePath, ".claude", "settings.json")
	data, err := os.ReadFile(copiedSettings)
	if err != nil {
		t.Fatalf("expected .claude/settings.json in tree: %v", err)
	}
	if string(data) != `{"k":"v"}` {
		t.Errorf("settings content = %q", string(data))
	}

	copiedFinish := filepath.Join(treePath, ".claude", "commands", "finish.md")
	if _, err := os.Stat(copiedFinish); err != nil {
		t.Fatalf("expected .claude/commands/finish.md in tree: %v", err)
	}
}

func TestCreateWorkspace_CopiesGlobPatterns(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	// Create a file to match the pattern.
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "setup.sh"), []byte("#!/bin/bash"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit.
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "add scripts"); err != nil {
		t.Fatal(err)
	}

	branchOut, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := strings.TrimSpace(branchOut)

	ws := Workspace{Name: "glob-ws", Branch: "ralph/glob-ws", CreatedAt: time.Now()}

	err := CreateWorkspace(ctx, r, dir, ws, defaultBranch, []string{"scripts/setup.sh"})
	if err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Verify the pattern file was copied.
	treePath := TreePath(dir, "glob-ws")
	copiedScript := filepath.Join(treePath, "scripts", "setup.sh")
	data, err := os.ReadFile(copiedScript)
	if err != nil {
		t.Fatalf("expected scripts/setup.sh in tree: %v", err)
	}
	if string(data) != "#!/bin/bash" {
		t.Errorf("script content = %q", string(data))
	}
}

func TestRemoveWorkspace_Cleanup(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	branchOut, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := strings.TrimSpace(branchOut)

	ws := Workspace{Name: "remove-me", Branch: "ralph/remove-me", CreatedAt: time.Now()}

	// Create workspace first.
	if err := CreateWorkspace(ctx, r, dir, ws, defaultBranch, nil); err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Verify it exists.
	wsDir := WorkspacePath(dir, "remove-me")
	if _, err := os.Stat(wsDir); err != nil {
		t.Fatalf("workspace directory should exist: %v", err)
	}

	// Remove it.
	if err := RemoveWorkspace(ctx, r, dir, "remove-me"); err != nil {
		t.Fatalf("RemoveWorkspace error: %v", err)
	}

	// Verify directory is gone.
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Error("workspace directory should be removed")
	}

	// Verify registry is empty.
	list, err := RegistryList(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("registry has %d entries, want 0", len(list))
	}

	// Verify branch is deleted.
	repoRunner := &shell.Runner{Dir: dir}
	_, err = repoRunner.Run(ctx, "git", "rev-parse", "--verify", "refs/heads/ralph/remove-me")
	if err == nil {
		t.Error("branch should have been deleted")
	}
}

func TestCreateWorkspace_ExistingBranch(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	branchOut, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := strings.TrimSpace(branchOut)

	// Create a branch manually with a commit.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "ralph/existing"); err != nil {
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

	// Go back to default branch.
	if _, err := r.Run(ctx, "git", "checkout", defaultBranch); err != nil {
		t.Fatal(err)
	}

	// Create workspace from existing branch (resume scenario).
	ws := Workspace{Name: "existing", Branch: "ralph/existing", CreatedAt: time.Now()}
	if err := CreateWorkspace(ctx, r, dir, ws, defaultBranch, nil); err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Verify the worktree has the existing commit's file.
	treePath := TreePath(dir, "existing")
	data, err := os.ReadFile(filepath.Join(treePath, "existing.txt"))
	if err != nil {
		t.Fatalf("expected existing.txt in worktree: %v", err)
	}
	if string(data) != "from existing branch" {
		t.Errorf("file content = %q, want %q", string(data), "from existing branch")
	}

	// Verify workspace.json metadata.
	wsJSON, err := ReadWorkspaceJSON(dir, "existing")
	if err != nil {
		t.Fatal(err)
	}
	if wsJSON.Branch != "ralph/existing" {
		t.Errorf("Branch = %q, want %q", wsJSON.Branch, "ralph/existing")
	}
}

func TestCreateWorkspace_NoDotRalphState(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	// Set up .ralph with config and state.
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(filepath.Join(ralphDir, "state"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte("project: test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "state", "prd.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "add config"); err != nil {
		t.Fatal(err)
	}

	branchOut, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := strings.TrimSpace(branchOut)

	ws := Workspace{Name: "no-state", Branch: "ralph/no-state", CreatedAt: time.Now()}
	if err := CreateWorkspace(ctx, r, dir, ws, defaultBranch, nil); err != nil {
		t.Fatalf("CreateWorkspace error: %v", err)
	}

	// Explicitly verify no .ralph/state/ inside the tree.
	treePath := TreePath(dir, "no-state")
	stateDir := filepath.Join(treePath, ".ralph", "state")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Errorf("tree/.ralph/state/ should NOT exist, got err: %v", err)
	}
}
