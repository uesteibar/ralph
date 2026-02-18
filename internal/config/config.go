package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project        string      `yaml:"project"`
	Repo           RepoConfig  `yaml:"repo"`
	Paths          PathsConfig `yaml:"paths"`
	QualityChecks  []string    `yaml:"quality_checks"`
	CopyToWorktree []string    `yaml:"copy_to_worktree,omitempty"`
}

type RepoConfig struct {
	Path          string `yaml:"-"` // derived from config file location, not from YAML
	DefaultBase   string `yaml:"default_base"`
	BranchPattern string `yaml:"branch_pattern"`
	BranchPrefix  string `yaml:"branch_prefix"`
}

type PathsConfig struct {
	TasksDir  string `yaml:"tasks_dir"`
	SkillsDir string `yaml:"skills_dir"`
}

// StatePRDPath returns the path to the current PRD staging file.
func (c *Config) StatePRDPath() string {
	return filepath.Join(c.Repo.Path, ".ralph", "state", "prd.json")
}

// StateArchiveDir returns the path to the PRD archive directory.
func (c *Config) StateArchiveDir() string {
	return filepath.Join(c.Repo.Path, ".ralph", "state", "archive")
}

// WorkspacesDir returns the path to the workspaces directory.
func (c *Config) WorkspacesDir() string {
	return filepath.Join(c.Repo.Path, ".ralph", "workspaces")
}

// PromptsDir returns the path to the ejected prompts directory.
func (c *Config) PromptsDir() string {
	return filepath.Join(c.Repo.Path, ".ralph", "prompts")
}

// Load reads and parses a config file at the given path.
// Repo.Path is derived from the config file location (grandparent of the file,
// i.e. the directory containing .ralph/).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Derive repo path: path is <repo>/.ralph/ralph.yaml
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}
	cfg.Repo.Path = filepath.Dir(filepath.Dir(absPath))

	if cfg.Repo.BranchPrefix == "" {
		cfg.Repo.BranchPrefix = "ralph/"
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

// Discover walks up from workDir looking for .ralph/ralph.yaml. When workDir
// is empty it defaults to the current working directory (os.Getwd).
// It skips configs found inside workspace trees (where a workspace.json exists
// in the parent directory) to ensure Repo.Path always points to the real repo root.
func Discover(workDir string) (*Config, error) {
	dir := workDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
	}

	for {
		candidate := filepath.Join(dir, ".ralph", "ralph.yaml")
		if _, err := os.Stat(candidate); err == nil {
			if !isInsideWorkspaceTree(dir) {
				return Load(candidate)
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil, fmt.Errorf("no .ralph/ralph.yaml found in current directory or parents")
}

// isInsideWorkspaceTree checks whether dir is a workspace tree directory.
// Workspace trees live at .ralph/workspaces/<name>/tree/, with a workspace.json
// in the parent directory. If dir/../workspace.json exists, dir is a tree.
func isInsideWorkspaceTree(dir string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(dir), "workspace.json"))
	return err == nil
}

// Resolve tries the explicit path first, then falls back to Discover.
// When workDir is empty, Discover uses the current working directory.
func Resolve(explicitPath, workDir string) (*Config, error) {
	if explicitPath != "" {
		return Load(explicitPath)
	}
	return Discover(workDir)
}

func (c *Config) validate() error {
	if c.Project == "" {
		return fmt.Errorf("missing required field: project")
	}
	if c.Repo.DefaultBase == "" {
		return fmt.Errorf("missing required field: repo.default_base")
	}
	return nil
}

// Validate checks the config for required fields, path existence, and
// consistency. Returns a list of issues found (empty if valid).
func (c *Config) Validate() []string {
	var issues []string

	if c.Project == "" {
		issues = append(issues, "missing required field: project")
	}
	if c.Repo.DefaultBase == "" {
		issues = append(issues, "missing required field: repo.default_base")
	}

	if c.Repo.BranchPattern != "" {
		if _, err := regexp.Compile(c.Repo.BranchPattern); err != nil {
			issues = append(issues, fmt.Sprintf("repo.branch_pattern is not valid regex: %v", err))
		}
	}

	if len(c.QualityChecks) == 0 {
		issues = append(issues, "warning: no quality_checks defined — the loop will commit without verification")
	}

	return issues
}
