# AutoRalph

An autonomous agent that watches Linear for assigned issues and drives them
through a complete development lifecycle -- from refinement and planning to
building, opening pull requests, and addressing review feedback -- without
human intervention.

AutoRalph wraps Ralph's execution loop in a long-running daemon. It polls
Linear for new issues, uses AI to refine requirements, creates workspaces,
invokes Ralph to build features, opens GitHub PRs, responds to code review
feedback, and marks issues as done when PRs are merged.

---

## Table of Contents

- [How It Works](#how-it-works)
- [State Machine](#state-machine)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
  - [Credentials](#credentials)
  - [Projects](#projects)
- [Running](#running)
- [Web Dashboard](#web-dashboard)
- [REST API](#rest-api)
- [WebSocket](#websocket)
- [Testing](#testing)
- [Architecture](#architecture)
- [Development](#development)

---

## How It Works

```
   Linear                     AutoRalph                         GitHub
   ------                     ---------                         ------
  Assign issue  ------>  Poll & ingest (QUEUED)
                         AI refinement (REFINING)
                         Post questions to Linear
  User approves ------>  Detect approval (APPROVED)
                         Create workspace + PRD (BUILDING)
                         Run Ralph loop (stories, QA)
                         Push branch                  ------>  Create PR
                         Wait for review              <------  Review comments
                         Address feedback (ADDRESSING_FEEDBACK)
                         Push fixes                   ------>  Updated PR
                         Detect merge                 <------  PR merged
                         Cleanup workspace (COMPLETED)
  Move to Done  <------  Update Linear state
```

### The Lifecycle in Detail

1. **Ingest**: The Linear poller discovers new issues assigned to a configured
   user and creates them in the local database as `QUEUED`.

2. **Refine**: AI reads the issue title and description, asks clarifying
   questions, and posts them as a Linear comment. The issue moves to `REFINING`.

3. **Iterate**: When the user replies on Linear, AI incorporates the feedback
   and posts an updated plan. This loop continues until the user is satisfied.

4. **Approve**: The user comments `@autoralph approved` on the Linear issue.
   AutoRalph extracts the approved plan and moves the issue to `APPROVED`.

5. **Build**: AutoRalph creates a git workspace (worktree + branch), generates
   a PRD from the approved plan, and invokes Ralph's execution loop. The issue
   moves to `BUILDING`.

6. **Open PR**: When the build succeeds, AutoRalph pushes the branch and opens
   a GitHub pull request with an AI-generated description. The issue moves to
   `IN_REVIEW`.

7. **Address Feedback**: If reviewers request changes, AutoRalph detects the
   `CHANGES_REQUESTED` review, feeds the comments to AI, commits fixes, pushes,
   and replies to each review comment. The issue moves to `ADDRESSING_FEEDBACK`
   and back to `IN_REVIEW`.

8. **Complete**: When the PR is merged, AutoRalph cleans up the workspace,
   updates the Linear issue state to "Done", and marks the issue as `COMPLETED`.

---

## State Machine

```
QUEUED -------> REFINING -------> APPROVED -------> BUILDING -------> IN_REVIEW
                  |   ^                                |                |   ^
                  |   |                                v                |   |
                  +---+                             FAILED             v   |
               (iteration)                            ^         ADDRESSING_FEEDBACK
                                                      |
                                                   PAUSED
```

| From | To | Trigger |
|------|----|---------|
| `queued` | `refining` | Orchestrator picks up new issue |
| `refining` | `refining` | User replies with feedback (iteration) |
| `refining` | `approved` | User comments `@autoralph approved` |
| `approved` | `building` | Workspace created, PRD generated |
| `building` | `in_review` | Build succeeds, PR opened |
| `building` | `failed` | Build fails (error stored in DB) |
| `in_review` | `addressing_feedback` | GitHub review with changes requested |
| `in_review` | `completed` | PR merged |
| `addressing_feedback` | `in_review` | Feedback addressed, changes pushed |
| any active | `paused` | User pauses via API or merge conflict |
| `paused` | (previous state) | User resumes via API |
| `failed` | (previous state) | User retries via API |

---

## Prerequisites

- **Go 1.25+** (build from source)
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** (`claude` CLI) installed and authenticated
- **git** (workspace operations use worktrees)
- **Node.js 18+** (building the web dashboard)
- **A Linear account** with an API key
- **A GitHub account** with a personal access token or a GitHub App

To build from source, use [mise](https://mise.jdx.dev/) to install Go, Node, and just: run `mise install` in the repo root (see `mise.toml`).

---

## Installation

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/uesteibar/ralph/main/install-autoralph.sh | sh
```

This detects your OS and architecture, downloads the latest release from GitHub,
verifies the SHA256 checksum, and installs to `/usr/local/bin/`.

### From source

```bash
git clone https://github.com/uesteibar/ralph.git
cd ralph
mise install       # Install Go, Node, just
just web-build     # Build the React dashboard
just autoralph     # Build the Go binary
```

### Verify

```bash
autoralph --version
```

---

## Configuration

All configuration lives under `~/.autoralph/`.

```
~/.autoralph/
  credentials.yaml          # API keys (Linear + GitHub)
  autoralph.db              # SQLite database (auto-created)
  projects/
    my-project.yaml         # One file per project
    another-project.yaml
```

### Credentials

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

**Resolution order** (highest precedence first):

1. Environment variables `LINEAR_API_KEY` / `GITHUB_TOKEN`
2. Named profile (from project config's `credentials_profile`)
3. `default_profile` from credentials file

If no credentials file exists and both environment variables are set, they are
used directly.

> **Note**: When the `GITHUB_TOKEN` environment variable is set, it takes
> precedence over GitHub App credentials. This is useful for temporarily
> overriding app auth during development.

#### GitHub Authentication

AutoRalph supports two methods for GitHub authentication:

**Personal Access Token** (simpler, good for personal repos):

1. Go to GitHub → **Settings** → **Developer settings** → **Personal access tokens** → **Fine-grained tokens**
2. Click **Generate new token**
3. Select the repository (or repositories) AutoRalph will manage
4. Grant these permissions:
   - **Contents**: Read and write
   - **Pull requests**: Read and write
   - **Issues**: Read and write
5. Click **Generate token** and copy the `ghp_...` value
6. Set it as `github_token` in your credentials profile

**GitHub App** (recommended for organizations):

GitHub Apps provide better security (short-lived tokens, scoped permissions) and
don't count against a personal user's rate limit.

1. **Create the app**: Go to GitHub → **Settings** → **Developer settings** →
   **GitHub Apps** → **New GitHub App**
   - Give it a name (e.g., "AutoRalph - My Org")
   - Set **Homepage URL** to any valid URL
   - Under **Webhook**, uncheck **Active** (AutoRalph polls, it doesn't need webhooks)
   - Under **Permissions** → **Repository permissions**, grant:
     - **Contents**: Read and write
     - **Pull requests**: Read and write
     - **Issues**: Read and write
   - Click **Create GitHub App**

2. **Get the Client ID**: On the app's settings page, copy the **Client ID**
   (starts with `Iv`) → this is your `github_app_client_id`

3. **Generate a private key**: On the app page, scroll down to **Private keys**
   → click **Generate a private key** → save the downloaded `.pem` file
   (e.g., to `~/.autoralph/my-app.pem`) → this path is your
   `github_app_private_key_path`

4. **Install the app on your org/account**: Go to the **Install App** tab →
   select your organization or account → choose which repositories to grant
   access to → click **Install**

5. **Get the Installation ID**: After installing, you'll be redirected to a URL
   like `https://github.com/settings/installations/12345678` — that number
   (`12345678`) is your `github_app_installation_id`

| Field | Description |
|-------|-------------|
| `github_token` | Personal access token (`ghp_...`). Use this OR the app fields below. |
| `github_app_client_id` | GitHub App Client ID (starts with `Iv`) |
| `github_app_installation_id` | Numeric ID from the installation URL |
| `github_app_private_key_path` | Path to the `.pem` private key file |
| `github_user_id` | Your numeric GitHub user ID (optional). Used to restrict which reviewer's feedback AutoRalph acts on. |

> **Important**: If you set any `github_app_*` field, you must set all three.
> Partial configuration is an error.

#### Security: Trusted Reviewer

On public repositories, anyone can submit a PR review. By default, AutoRalph
treats all non-bot reviews as actionable feedback. Setting `github_user_id` in
your credentials profile restricts AutoRalph to only act on reviews from that
specific GitHub user.

**What it does**: When `github_user_id` is set, AutoRalph compares each
reviewer's numeric ID against the configured value. Reviews from other users
are skipped (logged as `untrusted_feedback_skipped` in the activity log) and
never trigger the `addressing_feedback` transition. When `github_user_id` is
not set (or `0`), all non-bot reviews are trusted -- the existing default
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

#### Git Identity

AutoRalph can use a dedicated git identity for all commits it creates, making
it easy to distinguish autoralph-authored changes from human ones.

| Field | Default | Description |
|-------|---------|-------------|
| `git_author_name` | `autoralph` | Name used for git author and committer |
| `git_author_email` | `autoralph@noreply` | Email used for git author and committer |

When these fields are omitted, the defaults `autoralph` / `autoralph@noreply`
are used automatically. Each profile can specify its own git identity, so
different projects can commit under different names.

**Environment variable overrides**:

| Variable | Overrides |
|----------|-----------|
| `AUTORALPH_GIT_AUTHOR_NAME` | `git_author_name` from the active profile |
| `AUTORALPH_GIT_AUTHOR_EMAIL` | `git_author_email` from the active profile |

Environment variables take precedence over profile values. This is useful for
temporarily overriding the identity in CI or during development without changing
the credentials file.

The configured identity is applied in two ways:
- **Git operations** (commit, rebase, etc.) use `GIT_AUTHOR_NAME`,
  `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, and `GIT_COMMITTER_EMAIL`
  environment variables.
- **Claude CLI builds** use repo-local `git config user.name` and
  `git config user.email` set in the worktree before the build loop starts.

### Projects

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
  # label: "autoralph"  # Optional: only pick up issues with this label (case-insensitive)

# Optional (shown with defaults)
ralph_config_path: .ralph/ralph.yaml
max_iterations: 20
branch_prefix: "autoralph/"
```

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
| `linear.label` | no | _(none)_ | Label name to filter issue ingestion; only issues with a matching label are picked up (case-insensitive) |
| `ralph_config_path` | no | `.ralph/ralph.yaml` | Path to Ralph config (relative to `local_path`) |
| `max_iterations` | no | `20` | Max Ralph loop iterations per build |
| `branch_prefix` | no | `autoralph/` | Branch name prefix |

**Finding your Linear IDs**: In Linear, go to Settings > Account > API to find
your API key. Team and user UUIDs can be found via the Linear GraphQL API
explorer or by inspecting URLs.

---

## Running

### Start the server

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

### What happens on startup

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

---

## Web Dashboard

Open `http://127.0.0.1:7749` in your browser.

### Dashboard (`/`)

- **Project cards**: Each configured project with active issue count, GitHub
  link, and state breakdown (how many issues in each state)
- **Active issues table**: All non-completed issues with identifier, title,
  state badge, progress indicator (when building), and PR link (when in review)
- **Activity feed**: Recent activity across all projects (state changes, build
  events, comments posted)

### Issue Detail (`/issues/:id`)

- **Header**: Issue identifier, state badge, project name, title
- **PR link**: When a PR exists
- **Error display**: When the issue has failed
- **Action buttons**:
  - **Pause**: Pauses any active issue
  - **Resume**: Resumes a paused issue to its previous state
  - **Retry**: Retries a failed issue from its pre-failure state
- **Live build log**: When the issue is in BUILDING state, shows a real-time
  terminal-style log of the Ralph execution loop
- **Story progress**: Sidebar showing which stories have passed/failed during
  the build
- **Timeline**: Chronological activity log with event icons, state transitions,
  and timestamps

### Live Updates

The dashboard connects via WebSocket and automatically refreshes when state
changes, build events, new issues, or activity entries occur. If the connection
drops, it reconnects automatically after 3 seconds.

---

## REST API

All endpoints return JSON. Errors use the format `{"error": "message"}`.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/status` | Health check with uptime and active build count |
| `GET` | `/api/projects` | List all projects with active issue counts and state breakdown |
| `GET` | `/api/issues` | List issues (filterable) |
| `GET` | `/api/issues/{id}` | Issue detail with activity timeline |
| `POST` | `/api/issues/{id}/pause` | Pause an active issue |
| `POST` | `/api/issues/{id}/resume` | Resume a paused issue |
| `POST` | `/api/issues/{id}/retry` | Retry a failed issue |
| `GET` | `/api/activity` | Recent activity across all projects |

### Query Parameters

**`GET /api/issues`**:
- `project_id` -- Filter by project
- `state` -- Filter by state (e.g., `building`, `in_review`)

**`GET /api/issues/{id}`**:
- `limit` -- Activity entries per page (default: 20)
- `offset` -- Activity pagination offset

**`GET /api/activity`**:
- `limit` -- Max entries to return (default: 20)

### Examples

```bash
# Health check
curl http://127.0.0.1:7749/api/status

# List all projects
curl http://127.0.0.1:7749/api/projects

# List building issues
curl http://127.0.0.1:7749/api/issues?state=building

# Get issue detail
curl http://127.0.0.1:7749/api/issues/<issue-id>

# Pause an issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/pause

# Resume a paused issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/resume

# Retry a failed issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/retry

# Recent activity
curl http://127.0.0.1:7749/api/activity?limit=10
```

---

## WebSocket

Connect to `ws://127.0.0.1:7749/api/ws` for real-time updates.

### Message Format

```json
{
  "type": "issue_state_changed",
  "payload": { ... },
  "timestamp": "2026-02-11T16:42:19Z"
}
```

### Message Types

| Type | Description |
|------|-------------|
| `issue_state_changed` | An issue transitioned to a new state |
| `build_event` | A build event occurred (story started, passed, failed, etc.) |
| `new_issue` | A new issue was ingested from Linear |
| `activity` | A new activity log entry was created |

The dashboard uses these events to refresh data in real time. You can build
custom integrations by connecting to the same WebSocket endpoint.

---

## Testing

### Unit Tests

Every package has colocated `_test.go` files with comprehensive coverage:

```bash
just test                          # All tests
just test-pkg autoralph/db         # Specific package
just test-pkg autoralph/orchestrator
just test-pkg autoralph/server
```

### Integration Tests

13 integration tests in `test/e2e/integration_test.go` wire up real AutoRalph
components against mock servers:

```bash
just e2e
```

These cover:
- **IT-001**: Full lifecycle (QUEUED through COMPLETED)
- **IT-002**: Refinement iteration loop
- **IT-003**: Review feedback loop
- **IT-004**: Build failure and retry
- **IT-005**: API failure resilience
- **IT-006**: Multi-project with separate credentials
- **IT-007**: Pause and resume
- **IT-008**: Restart recovery
- **IT-009**: Dashboard API
- **IT-010**: Issue detail API
- **IT-011**: Action buttons
- **IT-012**: Concurrent build worker limits
- **IT-013**: Installer script validation

### E2E Playground

The test playground (`test/e2e/playground.go`) creates a complete test
environment with mock Linear/GitHub servers, an in-memory SQLite database, and
an AutoRalph HTTP server:

```go
func TestMyScenario(t *testing.T) {
    pg := e2e.StartPlayground(t)

    // Seed test data
    pg.SeedIssue("PROJ-1", "Add login", "queued")

    // Simulate external events
    pg.Linear.SimulateApproval("PROJ-1")
    pg.GitHub.SimulateMerge("myorg", "myrepo", 1)

    // Assert via API
    resp, _ := http.Get(pg.BaseURL + "/api/issues")
    // ...
}
```

### Browser Tests (Playwright)

```bash
just e2e-setup     # Install Playwright browsers (once)
cd test/e2e/playwright && npx playwright test
```

### Web Unit Tests (Vitest)

```bash
cd web && npm test
```

---

## Architecture

```
cmd/autoralph/main.go                Entry point + CLI dispatch
internal/autoralph/
  server/                            HTTP server, SPA, API, WebSocket hub
  db/                                SQLite schema, CRUD, transactions
  orchestrator/                      State machine (transitions, conditions, actions)
  linear/                            Linear GraphQL client
  github/                            GitHub REST client (go-github)
  credentials/                       Credential profile resolution
  projects/                          Project config loading + validation + sync
  ai/                                AI prompt templates (go:embed)
  poller/                            Linear poller (new issue ingestion)
  ghpoller/                          GitHub poller (review + merge detection)
  worker/                            Build worker pool (Ralph loop runner)
  refine/                            QUEUED -> REFINING action
  approve/                           REFINING -> APPROVED action
  build/                             APPROVED -> BUILDING action
  pr/                                PR creation action
  feedback/                          ADDRESSING_FEEDBACK action
  complete/                          COMPLETED action (cleanup)
  retry/                             Retry with exponential backoff
web/
  src/pages/Dashboard.tsx            Dashboard page
  src/pages/IssueDetail.tsx          Issue detail page
  src/api.ts                         REST API client
  src/useWebSocket.ts                WebSocket hook
  embed.go                           go:embed for built SPA assets
test/e2e/
  mocks/linear/                      Mock Linear GraphQL server
  mocks/github/                      Mock GitHub REST server
  fixtures/test-project/             Test project fixture
  playground.go                      E2E test orchestration
  integration_test.go                13 integration tests
  playwright/                        Browser tests
```

### Design Decisions

- **SQLite for persistence**: Single-file database, no external services. Uses
  `modernc.org/sqlite` (pure Go, no CGO) with WAL mode and foreign keys.
- **Deterministic state machine**: All transitions are registered upfront with
  explicit conditions. First-match-wins evaluation. State changes are atomic
  (wrapped in SQL transactions).
- **Interface-heavy testing**: Each action package defines 3-8 interfaces for
  its dependencies. Unit tests use mock structs, never touching the network or
  filesystem.
- **Retry with backoff**: Linear and GitHub API calls are wrapped in exponential
  backoff (3 attempts, 1s/5s/15s). HTTP 5xx errors retry; 4xx errors fail
  immediately.
- **Embedded SPA**: The React dashboard is compiled into the Go binary via
  `go:embed`. No separate static file serving needed.
- **Event-driven real-time**: WebSocket hub broadcasts state changes and build
  events. The dashboard reconnects automatically and re-fetches data.
- **No external message queue**: Background workers communicate through the
  SQLite database. Pollers write issues; the orchestrator reads and transitions
  them; workers pick them up.

---

## Development

### Tool dependencies

Use [mise](https://mise.jdx.dev/) to install the required tools: run `mise install` in the repo root. See `mise.toml` for pinned versions (Go, Node, just, mdbook, shellcheck).

### Build targets

| Target | Description |
|--------|-------------|
| `just autoralph` | Build the autoralph binary |
| `just web-build` | Build the React SPA |
| `just dev-web` | Start Vite dev server (hot reload) |
| `just dev-autoralph` | Build + run with `--dev` flag |
| `just test` | Run all Go tests |
| `just test-pkg autoralph/<pkg>` | Run tests for a specific package |
| `just vet` | Run `go vet` |
| `just e2e-setup` | Install Playwright browsers |
| `just e2e` | Run E2E / integration tests |
| `just shellcheck` | Lint installer scripts |
| `just check` | Build + test + vet |

### Development workflow

**Backend changes**:

```bash
just autoralph          # Rebuild
./autoralph serve       # Run
just test               # Verify
```

**Frontend changes** (with hot reload):

```bash
# Terminal 1
just dev-web            # Vite dev server on :5173

# Terminal 2
just dev-autoralph      # AutoRalph on :7749, proxies non-API to Vite
```

Open `http://127.0.0.1:7749` -- the SPA is served from Vite with hot module
replacement. API requests go to the Go server.

### Adding a new state transition

1. Create a new package under `internal/autoralph/<action>/`
2. Define interfaces for external dependencies
3. Implement `NewAction(Config)` returning an `orchestrator.ActionFunc`
4. Write tests using mock structs
5. Register the transition in the application setup code

### Adding a new API endpoint

1. Add the handler method to `internal/autoralph/server/api.go`
2. Register the route in `server.go`'s `registerRoutes()`
3. Add tests in `api_test.go`
