package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCredentialsFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "credentials.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolve_EnvVarsOverrideProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: yaml-linear
    github_token: yaml-github
`)
	t.Setenv("LINEAR_API_KEY", "env-linear")
	t.Setenv("GITHUB_TOKEN", "env-github")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LinearAPIKey != "env-linear" {
		t.Errorf("LinearAPIKey = %q, want %q", creds.LinearAPIKey, "env-linear")
	}
	if creds.GithubToken != "env-github" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "env-github")
	}
}

func TestResolve_NamedProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: personal
profiles:
  personal:
    linear_api_key: personal-linear
    github_token: personal-github
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LinearAPIKey != "work-linear" {
		t.Errorf("LinearAPIKey = %q, want %q", creds.LinearAPIKey, "work-linear")
	}
	if creds.GithubToken != "work-github" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "work-github")
	}
}

func TestResolve_DefaultProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: default-linear
    github_token: default-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LinearAPIKey != "default-linear" {
		t.Errorf("LinearAPIKey = %q, want %q", creds.LinearAPIKey, "default-linear")
	}
	if creds.GithubToken != "default-github" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "default-github")
	}
}

func TestResolve_PartialEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: yaml-linear
    github_token: yaml-github
`)
	t.Setenv("LINEAR_API_KEY", "env-linear")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LinearAPIKey != "env-linear" {
		t.Errorf("LinearAPIKey = %q, want %q", creds.LinearAPIKey, "env-linear")
	}
	if creds.GithubToken != "yaml-github" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "yaml-github")
	}
}

func TestResolve_ProfileNotFound(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := Resolve(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile, got nil")
	}
}

func TestResolve_FileMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := Resolve(dir, "work")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestResolve_FileMissing_EnvVarsFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LINEAR_API_KEY", "env-linear")
	t.Setenv("GITHUB_TOKEN", "env-github")

	creds, err := Resolve(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LinearAPIKey != "env-linear" {
		t.Errorf("LinearAPIKey = %q, want %q", creds.LinearAPIKey, "env-linear")
	}
	if creds.GithubToken != "env-github" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "env-github")
	}
}

func TestResolve_EmptyDefaultProfile_WithNoProfileName(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := Resolve(dir, "")
	if err == nil {
		t.Fatal("expected error when no profile name and no default_profile, got nil")
	}
}

func TestResolve_DefaultProfileNotInProfiles(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: missing
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := Resolve(dir, "")
	if err == nil {
		t.Fatal("expected error when default_profile references nonexistent profile, got nil")
	}
}

func TestResolve_GithubApp_FullProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_app_client_id: "Iv23liABC"
    github_app_installation_id: 12345
    github_app_private_key_path: /path/to/key.pem
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GithubToken != "" {
		t.Errorf("GithubToken = %q, want empty", creds.GithubToken)
	}
	if creds.GithubAppClientID != "Iv23liABC" {
		t.Errorf("GithubAppClientID = %q, want %q", creds.GithubAppClientID, "Iv23liABC")
	}
	if creds.GithubAppInstallationID != 12345 {
		t.Errorf("GithubAppInstallationID = %d, want %d", creds.GithubAppInstallationID, 12345)
	}
	if creds.GithubAppPrivateKeyPath != "/path/to/key.pem" {
		t.Errorf("GithubAppPrivateKeyPath = %q, want %q", creds.GithubAppPrivateKeyPath, "/path/to/key.pem")
	}
	if !creds.HasGithubApp() {
		t.Error("HasGithubApp() = false, want true")
	}
}

func TestResolve_GithubApp_PartialFields_Error(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_app_client_id: "Iv23liABC"
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := Resolve(dir, "work")
	if err == nil {
		t.Fatal("expected error for partial GitHub App config, got nil")
	}
}

func TestResolve_GithubApp_EnvTokenOverridesApp(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_app_client_id: "Iv23liABC"
    github_app_installation_id: 12345
    github_app_private_key_path: /path/to/key.pem
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "env-token")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GithubToken != "env-token" {
		t.Errorf("GithubToken = %q, want %q", creds.GithubToken, "env-token")
	}
	if creds.HasGithubApp() {
		t.Error("HasGithubApp() = true, want false (env token should clear app fields)")
	}
}

func TestHasGithubApp_False_WhenTokenOnly(t *testing.T) {
	creds := Credentials{GithubToken: "ghp_xxx"}
	if creds.HasGithubApp() {
		t.Error("HasGithubApp() = true, want false")
	}
}

func TestHasGithubApp_False_WhenEmpty(t *testing.T) {
	creds := Credentials{}
	if creds.HasGithubApp() {
		t.Error("HasGithubApp() = true, want false")
	}
}

func TestResolve_GithubUserID_Set(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
    github_user_id: 12345
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GithubUserID != 12345 {
		t.Errorf("GithubUserID = %d, want %d", creds.GithubUserID, 12345)
	}
}

func TestResolve_GithubUserID_Unset(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GithubUserID != 0 {
		t.Errorf("GithubUserID = %d, want %d", creds.GithubUserID, 0)
	}
}

func TestResolve_GitIdentity_FromProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
    git_author_name: my-bot
    git_author_email: my-bot@example.com
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "my-bot" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "my-bot")
	}
	if creds.GitAuthorEmail != "my-bot@example.com" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "my-bot@example.com")
	}
}

func TestResolve_GitIdentity_DefaultsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "autoralph" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "autoralph")
	}
	if creds.GitAuthorEmail != "autoralph@noreply" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "autoralph@noreply")
	}
}

func TestResolve_GitIdentity_EnvVarsOverrideProfile(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
    git_author_name: yaml-bot
    git_author_email: yaml-bot@example.com
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "env-bot")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "env-bot@example.com")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "env-bot" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "env-bot")
	}
	if creds.GitAuthorEmail != "env-bot@example.com" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "env-bot@example.com")
	}
}

func TestResolve_GitIdentity_PartialEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeCredentialsFile(t, dir, `
default_profile: work
profiles:
  work:
    linear_api_key: work-linear
    github_token: work-github
    git_author_name: yaml-bot
    git_author_email: yaml-bot@example.com
`)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "env-bot")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "")

	creds, err := Resolve(dir, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "env-bot" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "env-bot")
	}
	if creds.GitAuthorEmail != "yaml-bot@example.com" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "yaml-bot@example.com")
	}
}

func TestResolve_GitIdentity_FileMissing_EnvVarsFallback_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LINEAR_API_KEY", "env-linear")
	t.Setenv("GITHUB_TOKEN", "env-github")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "")

	creds, err := Resolve(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "autoralph" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "autoralph")
	}
	if creds.GitAuthorEmail != "autoralph@noreply" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "autoralph@noreply")
	}
}

func TestResolve_GitIdentity_FileMissing_EnvVarsFallback_WithGitEnvVars(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LINEAR_API_KEY", "env-linear")
	t.Setenv("GITHUB_TOKEN", "env-github")
	t.Setenv("AUTORALPH_GIT_AUTHOR_NAME", "env-bot")
	t.Setenv("AUTORALPH_GIT_AUTHOR_EMAIL", "env-bot@example.com")

	creds, err := Resolve(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.GitAuthorName != "env-bot" {
		t.Errorf("GitAuthorName = %q, want %q", creds.GitAuthorName, "env-bot")
	}
	if creds.GitAuthorEmail != "env-bot@example.com" {
		t.Errorf("GitAuthorEmail = %q, want %q", creds.GitAuthorEmail, "env-bot@example.com")
	}
}

func TestDefaultPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := DefaultPath()
	want := filepath.Join(home, ".autoralph")
	if got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
