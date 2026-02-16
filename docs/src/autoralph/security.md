# Security

AutoRalph interacts with external services (Linear, GitHub) and runs AI-driven
code generation. This chapter covers the security measures in place to protect
your repositories and credentials.

## Trusted Reviewer

On public repositories, anyone can submit a PR review. By default, AutoRalph
treats all non-bot reviews as actionable feedback. The `github_user_id`
setting restricts AutoRalph to only act on reviews from a specific GitHub
user.

**What it does**: When `github_user_id` is set, AutoRalph compares each
reviewer's numeric ID against the configured value. Reviews from other users
are skipped (logged as `untrusted_feedback_skipped` in the activity log) and
never trigger the `addressing_feedback` transition. When `github_user_id` is
not set (or `0`), all non-bot reviews are trusted — the existing default
behavior.

**How to find your GitHub user ID**:

```bash
gh api /user --jq .id
```

**Example configuration**:

```yaml
profiles:
  personal:
    linear_api_key: lin_api_xxxxxxxxxxxxx
    github_token: ghp_xxxxxxxxxxxxx
    github_user_id: 12345678
```

This prevents untrusted third parties from injecting malicious review
comments that AutoRalph would otherwise act on.

## GitHub App Authentication

GitHub Apps provide better security than personal access tokens for
organization use:

- **Short-lived tokens**: GitHub App installation tokens expire after 1 hour,
  limiting the window of exposure if compromised
- **Scoped permissions**: Apps are granted only the specific permissions they
  need (Contents, Pull requests, Issues — all read/write)
- **No personal rate limits**: API calls don't count against a personal user's
  rate limit
- **Audit trail**: All actions are attributed to the app, not a personal
  account

### Required Fields

| Field | Description |
|-------|-------------|
| `github_app_client_id` | GitHub App Client ID (starts with `Iv`) |
| `github_app_installation_id` | Numeric ID from the installation URL |
| `github_app_private_key_path` | Path to the `.pem` private key file |

> **Important**: If you set any `github_app_*` field, you must set all three.
> Partial configuration is an error.

### Setting Up a GitHub App

1. **Create the app**: GitHub → Settings → Developer settings → GitHub Apps →
   New GitHub App
   - Give it a name (e.g., "AutoRalph - My Org")
   - Set Homepage URL to any valid URL
   - Under Webhook, uncheck Active (AutoRalph polls, no webhooks needed)
   - Under Permissions → Repository permissions, grant:
     - **Contents**: Read and write
     - **Pull requests**: Read and write
     - **Issues**: Read and write
   - Click Create GitHub App

2. **Get the Client ID**: On the app settings page, copy the Client ID
   (starts with `Iv`) → `github_app_client_id`

3. **Generate a private key**: On the app page → Private keys → Generate a
   private key → save the `.pem` file (e.g., `~/.autoralph/my-app.pem`) →
   `github_app_private_key_path`

4. **Install the app**: Install App tab → select your org/account → choose
   repositories → Install

5. **Get the Installation ID**: After installing, the URL will be
   `https://github.com/settings/installations/12345678` — that number is
   your `github_app_installation_id`

## Credential Isolation

AutoRalph uses a profile-based credential system that isolates credentials
per project:

- **Separate profiles**: Each project can reference a different credentials
  profile, preventing cross-project credential leakage
- **File-based storage**: Credentials live in `~/.autoralph/credentials.yaml`,
  separate from project configs
- **Environment overrides**: `LINEAR_API_KEY` and `GITHUB_TOKEN` environment
  variables override file-based credentials, useful for CI/CD environments
  where secrets are injected
- **Private key files**: GitHub App private keys are stored as separate `.pem`
  files referenced by path, not embedded in YAML

### Git Identity Isolation

Each credentials profile can specify its own git identity
(`git_author_name`, `git_author_email`). This ensures:

- AutoRalph commits are clearly distinguished from human commits
- Different projects can use different identities
- Environment variables (`AUTORALPH_GIT_AUTHOR_NAME`,
  `AUTORALPH_GIT_AUTHOR_EMAIL`) can override profile values without modifying
  the credentials file

### Workspace Isolation

Each issue gets its own git worktree, preventing concurrent builds from
interfering with each other. Worktrees are cleaned up when issues complete,
and the build worker pool uses a semaphore to limit concurrent builds.
