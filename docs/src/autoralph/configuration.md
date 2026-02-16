# Configuration

All AutoRalph configuration lives under `~/.autoralph/`.

```
~/.autoralph/
  credentials.yaml          # API keys (Linear + GitHub)
  autoralph.db              # SQLite database (auto-created)
  projects/
    my-project.yaml         # One file per project
    another-project.yaml
```

## Credentials

Create `~/.autoralph/credentials.yaml`:

```yaml
default_profile: personal

profiles:
  # Option A: Personal access token (simpler)
  personal:
    linear_api_key: lin_api_xxxxxxxxxxxxx
    github_token: ghp_xxxxxxxxxxxxx
    git_author_name: autoralph-personal
    git_author_email: autoralph-personal@example.com

  # Option B: GitHub App (recommended for organizations)
  work:
    linear_api_key: lin_api_yyyyyyyyyyyyy
    github_app_client_id: "Iv23liXXXXXX"
    github_app_installation_id: 12345678
    github_app_private_key_path: ~/.autoralph/my-app.pem
    git_author_name: autoralph-work
    git_author_email: autoralph@myorg.com
```

### Resolution Order

Credentials are resolved in this order (highest precedence first):

1. Environment variables `LINEAR_API_KEY` / `GITHUB_TOKEN`
2. Named profile (from project config's `credentials_profile`)
3. `default_profile` from credentials file

If no credentials file exists and both environment variables are set, they are
used directly.

> **Note**: When the `GITHUB_TOKEN` environment variable is set, it takes
> precedence over GitHub App credentials. This is useful for temporarily
> overriding app auth during development.

### Git Identity

AutoRalph can use a dedicated git identity for commits, making it easy to
distinguish AutoRalph-authored changes from human ones.

| Field | Default | Description |
|-------|---------|-------------|
| `git_author_name` | `autoralph` | Name used for git author and committer |
| `git_author_email` | `autoralph@noreply` | Email used for git author and committer |

Each profile can specify its own git identity so different projects can commit
under different names.

**Environment variable overrides**:

| Variable | Overrides |
|----------|-----------|
| `AUTORALPH_GIT_AUTHOR_NAME` | `git_author_name` from the active profile |
| `AUTORALPH_GIT_AUTHOR_EMAIL` | `git_author_email` from the active profile |

## Projects

Create one YAML file per project in `~/.autoralph/projects/`:

```yaml
# ~/.autoralph/projects/my-project.yaml

name: my-project
local_path: ~/code/my-project
credentials_profile: personal

github:
  owner: myorg
  repo: my-project

linear:
  team_id: "abc-123-def"
  assignee_id: "user-456-ghi"
  project_id: "proj-789-jkl"
  # label: "autoralph"  # Optional: only pick up issues with this label

# Optional (shown with defaults)
ralph_config_path: .ralph/ralph.yaml
max_iterations: 20
branch_prefix: "autoralph/"
```

### Project Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | yes | | Human-readable project name (must be unique) |
| `local_path` | yes | | Absolute path to the local git clone |
| `credentials_profile` | no | `default_profile` | Which credentials profile to use |
| `github.owner` | yes | | GitHub org or user |
| `github.repo` | yes | | GitHub repository name |
| `linear.team_id` | yes | | Linear team UUID |
| `linear.assignee_id` | yes | | Linear user UUID to watch for assigned issues |
| `linear.project_id` | yes | | Linear project UUID |
| `linear.label` | no | _(none)_ | Label name to filter issue ingestion (case-insensitive) |
| `ralph_config_path` | no | `.ralph/ralph.yaml` | Path to Ralph config (relative to `local_path`) |
| `max_iterations` | no | `20` | Max Ralph loop iterations per build |
| `branch_prefix` | no | `autoralph/` | Branch name prefix |

**Finding your Linear IDs**: In Linear, go to Settings > Account > API to find
your API key. Team and user UUIDs can be found via the Linear GraphQL API
explorer or by inspecting URLs.

## Running

### Start the Server

```bash
autoralph serve
```

This starts the HTTP server on `127.0.0.1:7749` with the web dashboard, REST
API, and all background workers (Linear poller, GitHub poller, orchestrator,
build workers).

### Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--addr` | | `127.0.0.1:7749` | Address to listen on |
| `--dev` | | | Proxy non-API requests to Vite dev server |
| `--linear-url` | `AUTORALPH_LINEAR_URL` | | Override Linear API (for testing) |
| `--github-url` | `AUTORALPH_GITHUB_URL` | | Override GitHub API (for testing) |

### Subcommands

```bash
autoralph serve [flags]   # Start the server
autoralph version         # Print version
autoralph help            # Show usage
```

### What Happens on Startup

1. Opens (or creates) the SQLite database at `~/.autoralph/autoralph.db`
2. Loads and validates all project configs from `~/.autoralph/projects/*.yaml`
3. Syncs project configs to the database
4. Resolves credentials for each project
5. Starts the Linear poller (polls every 30s for new assigned issues)
6. Starts the GitHub poller (polls every 30s for PR reviews and merges)
7. Starts the orchestrator loop (evaluates state transitions)
8. Starts the build worker pool
9. Recovers any BUILDING issues from a previous run
10. Serves the web dashboard and API on the configured address
