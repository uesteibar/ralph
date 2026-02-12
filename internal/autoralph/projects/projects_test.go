package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/db"
)

func writeProjectFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	projDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCredentials(t *testing.T, dir string) {
	t.Helper()
	content := `
default_profile: default
profiles:
  default:
    linear_api_key: test-linear-key
    github_token: test-github-token
  work:
    linear_api_key: work-linear-key
    github_token: work-github-token
`
	if err := os.WriteFile(filepath.Join(dir, "credentials.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_AllFields(t *testing.T) {
	dir := t.TempDir()
	localPath := t.TempDir() // real directory for validation
	writeProjectFile(t, dir, "myproject.yaml", `
name: myproject
local_path: `+localPath+`
credentials_profile: work
github:
  owner: acme
  repo: widget
linear:
  team_id: team-123
  assignee_id: user-456
ralph_config_path: .ralph/custom.yaml
max_iterations: 30
branch_prefix: custom/
`)

	cfg, err := Load(filepath.Join(dir, "projects", "myproject.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "myproject" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myproject")
	}
	if cfg.LocalPath != localPath {
		t.Errorf("LocalPath = %q, want %q", cfg.LocalPath, localPath)
	}
	if cfg.CredentialsProfile != "work" {
		t.Errorf("CredentialsProfile = %q, want %q", cfg.CredentialsProfile, "work")
	}
	if cfg.Github.Owner != "acme" {
		t.Errorf("Github.Owner = %q, want %q", cfg.Github.Owner, "acme")
	}
	if cfg.Github.Repo != "widget" {
		t.Errorf("Github.Repo = %q, want %q", cfg.Github.Repo, "widget")
	}
	if cfg.Linear.TeamID != "team-123" {
		t.Errorf("Linear.TeamID = %q, want %q", cfg.Linear.TeamID, "team-123")
	}
	if cfg.Linear.AssigneeID != "user-456" {
		t.Errorf("Linear.AssigneeID = %q, want %q", cfg.Linear.AssigneeID, "user-456")
	}
	if cfg.RalphConfigPath != ".ralph/custom.yaml" {
		t.Errorf("RalphConfigPath = %q, want %q", cfg.RalphConfigPath, ".ralph/custom.yaml")
	}
	if cfg.MaxIterations != 30 {
		t.Errorf("MaxIterations = %d, want 30", cfg.MaxIterations)
	}
	if cfg.BranchPrefix != "custom/" {
		t.Errorf("BranchPrefix = %q, want %q", cfg.BranchPrefix, "custom/")
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	localPath := t.TempDir()
	writeProjectFile(t, dir, "minimal.yaml", `
name: minimal
local_path: `+localPath+`
credentials_profile: default
github:
  owner: acme
  repo: widget
linear:
  team_id: team-1
  assignee_id: user-1
`)

	cfg, err := Load(filepath.Join(dir, "projects", "minimal.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RalphConfigPath != ".ralph/ralph.yaml" {
		t.Errorf("RalphConfigPath = %q, want default %q", cfg.RalphConfigPath, ".ralph/ralph.yaml")
	}
	if cfg.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want default 20", cfg.MaxIterations)
	}
	if cfg.BranchPrefix != "autoralph/" {
		t.Errorf("BranchPrefix = %q, want default %q", cfg.BranchPrefix, "autoralph/")
	}
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, "bad.yaml", `not: valid: yaml: :::`)

	_, err := Load(filepath.Join(dir, "projects", "bad.yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadAll_MultipleProjects(t *testing.T) {
	dir := t.TempDir()
	path1 := t.TempDir()
	path2 := t.TempDir()
	writeCredentials(t, dir)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	writeProjectFile(t, dir, "alpha.yaml", `
name: alpha
local_path: `+path1+`
credentials_profile: default
github:
  owner: acme
  repo: alpha
linear:
  team_id: team-1
  assignee_id: user-1
`)
	writeProjectFile(t, dir, "bravo.yaml", `
name: bravo
local_path: `+path2+`
credentials_profile: default
github:
  owner: acme
  repo: bravo
linear:
  team_id: team-2
  assignee_id: user-2
`)

	configs, warnings := LoadAll(dir)
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestLoadAll_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "projects")
	os.MkdirAll(projDir, 0o755)
	localPath := t.TempDir()
	writeCredentials(t, dir)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	writeProjectFile(t, dir, "good.yaml", `
name: good
local_path: `+localPath+`
credentials_profile: default
github:
  owner: acme
  repo: good
linear:
  team_id: team-1
  assignee_id: user-1
`)
	os.WriteFile(filepath.Join(projDir, "readme.txt"), []byte("not a project"), 0o644)

	configs, warnings := LoadAll(dir)
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestLoadAll_InvalidConfig_ReturnsWarning(t *testing.T) {
	dir := t.TempDir()
	localPath := t.TempDir()
	writeCredentials(t, dir)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	writeProjectFile(t, dir, "good.yaml", `
name: good
local_path: `+localPath+`
credentials_profile: default
github:
  owner: acme
  repo: good
linear:
  team_id: team-1
  assignee_id: user-1
`)
	// Missing required fields
	writeProjectFile(t, dir, "bad.yaml", `
name: bad
local_path: /nonexistent/path/that/does/not/exist
`)

	configs, warnings := LoadAll(dir)
	if len(configs) != 1 {
		t.Fatalf("expected 1 valid config, got %d", len(configs))
	}
	if len(warnings) == 0 {
		t.Error("expected warnings for invalid config, got none")
	}
}

func TestLoadAll_InvalidCredentialsProfile_ReturnsWarning(t *testing.T) {
	dir := t.TempDir()
	localPath := t.TempDir()
	writeCredentials(t, dir)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	writeProjectFile(t, dir, "bad-creds.yaml", `
name: bad-creds
local_path: `+localPath+`
credentials_profile: nonexistent
github:
  owner: acme
  repo: widget
linear:
  team_id: team-1
  assignee_id: user-1
`)

	configs, warnings := LoadAll(dir)
	if len(configs) != 0 {
		t.Fatalf("expected 0 valid configs, got %d", len(configs))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
}

func TestLoadAll_NoProjectsDir_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	configs, warnings := LoadAll(dir)
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestValidate_MissingName(t *testing.T) {
	cfg := ProjectConfig{
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidate_MissingLocalPath(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing local_path")
	}
}

func TestValidate_LocalPathNotExists(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          "/nonexistent/path/that/does/not/exist",
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent local_path")
	}
}

func TestValidate_MissingGithubOwner(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Repo: "r"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing github.owner")
	}
}

func TestValidate_MissingGithubRepo(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing github.repo")
	}
}

func TestValidate_MissingLinearTeamID(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing linear.team_id")
	}
}

func TestValidate_MissingLinearAssigneeID(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{TeamID: "t"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing linear.assignee_id")
	}
}

func TestValidate_AllFieldsPresent_NoError(t *testing.T) {
	cfg := ProjectConfig{
		Name:               "test",
		LocalPath:          t.TempDir(),
		CredentialsProfile: "default",
		Github:             GithubConfig{Owner: "o", Repo: "r"},
		Linear:             LinearConfig{TeamID: "t", AssigneeID: "a"},
	}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Sync ---

func testDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSync_CreatesNewProjects(t *testing.T) {
	d := testDB(t)
	localPath := t.TempDir()

	configs := []ProjectConfig{
		{
			Name:               "project-a",
			LocalPath:          localPath,
			CredentialsProfile: "default",
			Github:             GithubConfig{Owner: "acme", Repo: "alpha"},
			Linear:             LinearConfig{TeamID: "t1", AssigneeID: "u1"},
			RalphConfigPath:    ".ralph/ralph.yaml",
			MaxIterations:      20,
			BranchPrefix:       "autoralph/",
		},
	}

	err := Sync(d, configs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	projects, err := d.ListProjects()
	if err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "project-a" {
		t.Errorf("expected name %q, got %q", "project-a", projects[0].Name)
	}
	if projects[0].GithubOwner != "acme" {
		t.Errorf("expected github_owner %q, got %q", "acme", projects[0].GithubOwner)
	}
	if projects[0].LinearTeamID != "t1" {
		t.Errorf("expected linear_team_id %q, got %q", "t1", projects[0].LinearTeamID)
	}
}

func TestSync_UpdatesExistingProject(t *testing.T) {
	d := testDB(t)
	localPath := t.TempDir()

	// Create initial project via DB
	d.CreateProject(db.Project{
		Name:               "project-a",
		LocalPath:          localPath,
		CredentialsProfile: "old-profile",
		GithubOwner:        "old-owner",
		GithubRepo:         "old-repo",
		LinearTeamID:       "old-team",
		LinearAssigneeID:   "old-user",
		RalphConfigPath:    ".ralph/ralph.yaml",
		MaxIterations:      10,
		BranchPrefix:       "old/",
	})

	// Sync with updated config
	configs := []ProjectConfig{
		{
			Name:               "project-a",
			LocalPath:          localPath,
			CredentialsProfile: "new-profile",
			Github:             GithubConfig{Owner: "new-owner", Repo: "new-repo"},
			Linear:             LinearConfig{TeamID: "new-team", AssigneeID: "new-user"},
			RalphConfigPath:    ".ralph/custom.yaml",
			MaxIterations:      30,
			BranchPrefix:       "new/",
		},
	}

	err := Sync(d, configs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	projects, _ := d.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	p := projects[0]
	if p.CredentialsProfile != "new-profile" {
		t.Errorf("CredentialsProfile = %q, want %q", p.CredentialsProfile, "new-profile")
	}
	if p.GithubOwner != "new-owner" {
		t.Errorf("GithubOwner = %q, want %q", p.GithubOwner, "new-owner")
	}
	if p.MaxIterations != 30 {
		t.Errorf("MaxIterations = %d, want 30", p.MaxIterations)
	}
}

func TestSync_EmptyConfigs_NoError(t *testing.T) {
	d := testDB(t)

	err := Sync(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	projects, _ := d.ListProjects()
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestSync_MultipleProjects(t *testing.T) {
	d := testDB(t)
	path1 := t.TempDir()
	path2 := t.TempDir()

	configs := []ProjectConfig{
		{
			Name:               "alpha",
			LocalPath:          path1,
			CredentialsProfile: "default",
			Github:             GithubConfig{Owner: "acme", Repo: "alpha"},
			Linear:             LinearConfig{TeamID: "t1", AssigneeID: "u1"},
			RalphConfigPath:    ".ralph/ralph.yaml",
			MaxIterations:      20,
			BranchPrefix:       "autoralph/",
		},
		{
			Name:               "bravo",
			LocalPath:          path2,
			CredentialsProfile: "work",
			Github:             GithubConfig{Owner: "acme", Repo: "bravo"},
			Linear:             LinearConfig{TeamID: "t2", AssigneeID: "u2"},
			RalphConfigPath:    ".ralph/ralph.yaml",
			MaxIterations:      15,
			BranchPrefix:       "auto/",
		},
	}

	err := Sync(d, configs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	projects, _ := d.ListProjects()
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}
