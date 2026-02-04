package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ExampleConfig_ParsesAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "assets", "examples", "ralph.wanidroid.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Project != "Wanidroid" {
		t.Errorf("Project = %q, want %q", cfg.Project, "Wanidroid")
	}
	if cfg.Repo.DefaultBase != "main" {
		t.Errorf("Repo.DefaultBase = %q, want %q", cfg.Repo.DefaultBase, "main")
	}
	if cfg.Repo.BranchPattern != `^ralph/[a-zA-Z0-9._-]+$` {
		t.Errorf("Repo.BranchPattern = %q, unexpected", cfg.Repo.BranchPattern)
	}
	if cfg.Paths.TasksDir != ".ralph/tasks" {
		t.Errorf("Paths.TasksDir = %q, want %q", cfg.Paths.TasksDir, ".ralph/tasks")
	}
	if cfg.Paths.SkillsDir != ".ralph/skills" {
		t.Errorf("Paths.SkillsDir = %q, want %q", cfg.Paths.SkillsDir, ".ralph/skills")
	}
	if len(cfg.QualityChecks) != 4 {
		t.Errorf("QualityChecks length = %d, want 4", len(cfg.QualityChecks))
	}
}

func TestLoad_MissingFields_ReturnsError(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "missing project",
			content: "repo:\n  default_base: main\n",
			wantErr: "missing required field: project",
		},
		{
			name:    "missing repo.default_base",
			content: "project: P\n",
			wantErr: "missing required field: repo.default_base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ralph.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestLoad_NonexistentFile_ReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/path/ralph.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDiscover_FromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}
	configContent := "project: Test\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(dir, "src", "deep")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(subDir)

	cfg, err := Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if cfg.Project != "Test" {
		t.Errorf("Project = %q, want %q", cfg.Project, "Test")
	}
}

func TestResolve_ExplicitPathTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	content := "project: Custom\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if cfg.Project != "Custom" {
		t.Errorf("Project = %q, want %q", cfg.Project, "Custom")
	}
}

func TestLoad_CopyToWorktree_ParsesField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.yaml")
	content := `project: Test
repo:
  default_base: main
copy_to_worktree:
  - ".env.example"
  - "configs/*.json"
  - "fixtures/**/*.txt"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.CopyToWorktree) != 3 {
		t.Fatalf("CopyToWorktree length = %d, want 3", len(cfg.CopyToWorktree))
	}
	expected := []string{".env.example", "configs/*.json", "fixtures/**/*.txt"}
	for i, want := range expected {
		if cfg.CopyToWorktree[i] != want {
			t.Errorf("CopyToWorktree[%d] = %q, want %q", i, cfg.CopyToWorktree[i], want)
		}
	}
}

func TestLoad_CopyToWorktree_OptionalField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.yaml")
	content := `project: Test
repo:
  default_base: main
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.CopyToWorktree != nil {
		t.Errorf("CopyToWorktree = %v, want nil", cfg.CopyToWorktree)
	}
}

func TestLoad_CopyToWorktree_EmptyList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.yaml")
	content := `project: Test
repo:
  default_base: main
copy_to_worktree: []
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.CopyToWorktree) != 0 {
		t.Errorf("CopyToWorktree length = %d, want 0", len(cfg.CopyToWorktree))
	}
}

func TestLoad_BranchPrefix_DefaultValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.yaml")
	content := "project: Test\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Repo.BranchPrefix != "ralph/" {
		t.Errorf("BranchPrefix = %q, want %q", cfg.Repo.BranchPrefix, "ralph/")
	}
}

func TestLoad_BranchPrefix_CustomValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.yaml")
	content := "project: Test\nrepo:\n  default_base: main\n  branch_prefix: feature/\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Repo.BranchPrefix != "feature/" {
		t.Errorf("BranchPrefix = %q, want %q", cfg.Repo.BranchPrefix, "feature/")
	}
}

func TestDiscover_SkipsConfigInsideWorkspaceTree(t *testing.T) {
	// Simulate the real workspace structure:
	// <repo>/.ralph/ralph.yaml            ← real config (should be found)
	// <repo>/.ralph/workspaces/<name>/workspace.json
	// <repo>/.ralph/workspaces/<name>/tree/.ralph/ralph.yaml ← git checkout (should be skipped)
	repoDir := t.TempDir()

	// Create the real config at the repo root.
	realRalphDir := filepath.Join(repoDir, ".ralph")
	if err := os.MkdirAll(realRalphDir, 0755); err != nil {
		t.Fatal(err)
	}
	configContent := "project: RealRepo\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(filepath.Join(realRalphDir, "ralph.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create workspace structure with workspace.json.
	wsName := "my-feature"
	wsDir := filepath.Join(repoDir, ".ralph", "workspaces", wsName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "workspace.json"), []byte(`{"name":"my-feature"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create tree with a ralph.yaml (as if checked out by git).
	treeDir := filepath.Join(wsDir, "tree")
	treeRalphDir := filepath.Join(treeDir, ".ralph")
	if err := os.MkdirAll(treeRalphDir, 0755); err != nil {
		t.Fatal(err)
	}
	treeConfigContent := "project: TreeConfig\nrepo:\n  default_base: main\n"
	if err := os.WriteFile(filepath.Join(treeRalphDir, "ralph.yaml"), []byte(treeConfigContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Discover from inside the tree should skip the tree config and find the real one.
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(treeDir)

	cfg, err := Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if cfg.Project != "RealRepo" {
		t.Errorf("Project = %q, want %q (should have skipped tree config)", cfg.Project, "RealRepo")
	}

	// Verify Repo.Path points to the real repo, not the tree.
	// Use EvalSymlinks to handle macOS /private/var symlinks.
	absRepoDir, _ := filepath.EvalSymlinks(repoDir)
	if cfg.Repo.Path != absRepoDir {
		t.Errorf("Repo.Path = %q, want %q", cfg.Repo.Path, absRepoDir)
	}
}

func TestConfig_WorkspacesDir(t *testing.T) {
	cfg := &Config{Repo: RepoConfig{Path: "/my/repo"}}
	got := cfg.WorkspacesDir()
	want := filepath.Join("/my/repo", ".ralph", "workspaces")
	if got != want {
		t.Errorf("WorkspacesDir() = %q, want %q", got, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
