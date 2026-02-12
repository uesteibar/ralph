package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/credentials"
	"gopkg.in/yaml.v3"
)

type GithubConfig struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

type LinearConfig struct {
	TeamID     string `yaml:"team_id"`
	AssigneeID string `yaml:"assignee_id"`
	ProjectID  string `yaml:"project_id"`
	Label      string `yaml:"label,omitempty"`
}

type ProjectConfig struct {
	Name               string       `yaml:"name"`
	LocalPath          string       `yaml:"local_path"`
	CredentialsProfile string       `yaml:"credentials_profile"`
	Github             GithubConfig `yaml:"github"`
	Linear             LinearConfig `yaml:"linear"`
	RalphConfigPath    string       `yaml:"ralph_config_path"`
	MaxIterations      int          `yaml:"max_iterations"`
	BranchPrefix       string       `yaml:"branch_prefix"`
}

// Load reads and parses a single project config YAML file.
// Applies defaults for optional fields.
func Load(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("reading project config: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ProjectConfig{}, fmt.Errorf("parsing project config %s: %w", path, err)
	}

	applyDefaults(&cfg)
	return cfg, nil
}

// LoadAll scans configDir/projects/*.yaml, loads each file, validates it,
// and returns valid configs plus warnings for any invalid ones.
func LoadAll(configDir string) (configs []ProjectConfig, warnings []string) {
	projDir := filepath.Join(configDir, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return nil, nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		cfg, err := Load(filepath.Join(projDir, name))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		if err := Validate(cfg); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		if _, err := credentials.Resolve(configDir, cfg.CredentialsProfile); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: credentials_profile %q: %v", name, cfg.CredentialsProfile, err))
			continue
		}

		configs = append(configs, cfg)
	}

	return configs, warnings
}

// Validate checks that all required fields are present and local_path exists.
func Validate(cfg ProjectConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	if cfg.LocalPath == "" {
		return fmt.Errorf("missing required field: local_path")
	}
	if _, err := os.Stat(cfg.LocalPath); err != nil {
		return fmt.Errorf("local_path %q does not exist: %w", cfg.LocalPath, err)
	}
	if cfg.Github.Owner == "" {
		return fmt.Errorf("missing required field: github.owner")
	}
	if cfg.Github.Repo == "" {
		return fmt.Errorf("missing required field: github.repo")
	}
	if cfg.Linear.TeamID == "" {
		return fmt.Errorf("missing required field: linear.team_id")
	}
	if cfg.Linear.AssigneeID == "" {
		return fmt.Errorf("missing required field: linear.assignee_id")
	}
	if cfg.Linear.ProjectID == "" {
		return fmt.Errorf("missing required field: linear.project_id")
	}
	return nil
}

func applyDefaults(cfg *ProjectConfig) {
	if cfg.RalphConfigPath == "" {
		cfg.RalphConfigPath = ".ralph/ralph.yaml"
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 20
	}
	if cfg.BranchPrefix == "" {
		cfg.BranchPrefix = "autoralph/"
	}
	cfg.LocalPath = expandHome(cfg.LocalPath)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
