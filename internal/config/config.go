package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project       string      `yaml:"project"`
	Repo          RepoConfig  `yaml:"repo"`
	Paths         PathsConfig `yaml:"paths"`
	QualityChecks []string    `yaml:"quality_checks"`
}

type RepoConfig struct {
	Path          string `yaml:"-"` // derived from config file location, not from YAML
	DefaultBase   string `yaml:"default_base"`
	BranchPattern string `yaml:"branch_pattern"`
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

// ProgressPath returns the path to the shared progress file.
func (c *Config) ProgressPath() string {
	return filepath.Join(c.Repo.Path, ".ralph", "progress.txt")
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

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

// Discover walks up from the current directory looking for .ralph/ralph.yaml.
func Discover() (*Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, ".ralph", "ralph.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil, fmt.Errorf("no .ralph/ralph.yaml found in current directory or parents")
}

// Resolve tries the explicit path first, then falls back to Discover.
func Resolve(explicitPath string) (*Config, error) {
	if explicitPath != "" {
		return Load(explicitPath)
	}
	return Discover()
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
