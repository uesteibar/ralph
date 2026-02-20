package credentials

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Credentials struct {
	LinearAPIKey string
	GithubToken  string

	// GitHub App authentication (alternative to GithubToken).
	GithubAppClientID       string
	GithubAppInstallationID int64
	GithubAppPrivateKeyPath string

	GithubUserID   int64
	GithubUsername string

	GitAuthorName  string
	GitAuthorEmail string
}

// HasGithubApp returns true if GitHub App credentials are configured.
func (c Credentials) HasGithubApp() bool {
	return c.GithubAppClientID != "" && c.GithubAppInstallationID != 0 && c.GithubAppPrivateKeyPath != ""
}

type profileEntry struct {
	LinearAPIKey            string `yaml:"linear_api_key"`
	GithubToken             string `yaml:"github_token"`
	GithubAppClientID       string `yaml:"github_app_client_id"`
	GithubAppInstallationID int64  `yaml:"github_app_installation_id"`
	GithubAppPrivateKeyPath string `yaml:"github_app_private_key_path"`
	GithubUserID            int64  `yaml:"github_user_id"`
	GithubUsername          string `yaml:"github_username"`
	GitAuthorName           string `yaml:"git_author_name"`
	GitAuthorEmail          string `yaml:"git_author_email"`
}

type credentialsFile struct {
	DefaultProfile string                  `yaml:"default_profile"`
	Profiles       map[string]profileEntry `yaml:"profiles"`
}

// DefaultPath returns the default credentials directory (~/.autoralph).
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".autoralph")
}

// Resolve returns Credentials for the given profile name with precedence:
// env vars (LINEAR_API_KEY, GITHUB_TOKEN) > named profile > default profile.
// If profileName is empty, the default_profile from the file is used.
// If the credentials file is missing and no profileName is requested, env vars
// alone are used (both must be set).
func Resolve(configDir, profileName string) (Credentials, error) {
	envLinear := os.Getenv("LINEAR_API_KEY")
	envGithub := os.Getenv("GITHUB_TOKEN")

	filePath := filepath.Join(configDir, "credentials.yaml")
	data, err := os.ReadFile(filePath)

	if err != nil {
		if !os.IsNotExist(err) {
			return Credentials{}, fmt.Errorf("reading credentials file: %w", err)
		}
		// File missing: if a specific profile was requested, that's an error.
		if profileName != "" {
			return Credentials{}, fmt.Errorf("credentials file not found: %s", filePath)
		}
		// No file and no profile requested — rely on env vars.
		if envLinear == "" || envGithub == "" {
			return Credentials{}, fmt.Errorf("credentials file not found (%s) and environment variables LINEAR_API_KEY/GITHUB_TOKEN not set", filePath)
		}
		return Credentials{
			LinearAPIKey:   envLinear,
			GithubToken:    envGithub,
			GitAuthorName:  gitAuthorNameWithDefault(os.Getenv("AUTORALPH_GIT_AUTHOR_NAME")),
			GitAuthorEmail: gitAuthorEmailWithDefault(os.Getenv("AUTORALPH_GIT_AUTHOR_EMAIL")),
		}, nil
	}

	var cf credentialsFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return Credentials{}, fmt.Errorf("parsing credentials file: %w", err)
	}

	if profileName == "" {
		profileName = cf.DefaultProfile
	}
	if profileName == "" {
		return Credentials{}, fmt.Errorf("no profile name provided and no default_profile set in %s", filePath)
	}

	profile, ok := cf.Profiles[profileName]
	if !ok {
		return Credentials{}, fmt.Errorf("profile %q not found in %s", profileName, filePath)
	}

	if err := validateGithubAppFields(profile); err != nil {
		return Credentials{}, fmt.Errorf("profile %q: %w", profileName, err)
	}

	creds := Credentials{
		LinearAPIKey:            profile.LinearAPIKey,
		GithubToken:             profile.GithubToken,
		GithubAppClientID:       profile.GithubAppClientID,
		GithubAppInstallationID: profile.GithubAppInstallationID,
		GithubAppPrivateKeyPath: profile.GithubAppPrivateKeyPath,
		GithubUserID:            profile.GithubUserID,
		GithubUsername:          profile.GithubUsername,
		GitAuthorName:           gitAuthorNameWithDefault(profile.GitAuthorName),
		GitAuthorEmail:          gitAuthorEmailWithDefault(profile.GitAuthorEmail),
	}

	if envLinear != "" {
		creds.LinearAPIKey = envLinear
	}
	// GITHUB_TOKEN env var overrides both token and app auth.
	if envGithub != "" {
		creds.GithubToken = envGithub
		creds.GithubAppClientID = ""
		creds.GithubAppInstallationID = 0
		creds.GithubAppPrivateKeyPath = ""
	}
	if v := os.Getenv("AUTORALPH_GIT_AUTHOR_NAME"); v != "" {
		creds.GitAuthorName = v
	}
	if v := os.Getenv("AUTORALPH_GIT_AUTHOR_EMAIL"); v != "" {
		creds.GitAuthorEmail = v
	}

	return creds, nil
}

func gitAuthorNameWithDefault(v string) string {
	if v == "" {
		return "autoralph"
	}
	return v
}

func gitAuthorEmailWithDefault(v string) string {
	if v == "" {
		return "autoralph@noreply"
	}
	return v
}

// validateGithubAppFields checks that if any github_app_* field is set, all
// three must be set. Returns nil if none are set or all are set.
func validateGithubAppFields(p profileEntry) error {
	hasClientID := p.GithubAppClientID != ""
	hasInstallID := p.GithubAppInstallationID != 0
	hasKeyPath := p.GithubAppPrivateKeyPath != ""

	set := 0
	if hasClientID {
		set++
	}
	if hasInstallID {
		set++
	}
	if hasKeyPath {
		set++
	}

	if set > 0 && set < 3 {
		var missing []string
		if !hasClientID {
			missing = append(missing, "github_app_client_id")
		}
		if !hasInstallID {
			missing = append(missing, "github_app_installation_id")
		}
		if !hasKeyPath {
			missing = append(missing, "github_app_private_key_path")
		}
		return fmt.Errorf("incomplete GitHub App config, missing: %v", missing)
	}
	return nil
}
