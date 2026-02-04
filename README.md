# Ralph

An autonomous coding agent loop. Ralph takes a feature description, breaks
it into right-sized user stories, and implements them one by one -- running
quality checks and committing after each step -- until the feature is done.

You work from your terminal. Ralph works in an isolated workspace.

## How It Works

1. You create a **workspace** -- an isolated git worktree with its own branch
2. You describe a feature interactively with Claude
3. When you're happy, type `/finish` -- Claude generates a structured PRD
   with user stories and integration tests
4. Ralph loops through stories in your workspace:
   - Pick next unfinished story
   - Implement and test it
   - Run quality checks
   - Commit
   - Repeat until all stories are done
5. QA verification phase:
   - QA agent runs integration tests defined in the PRD
   - If tests fail, QA fix agent resolves the issues
   - Loop continues until all integration tests pass

## Workspaces

A **workspace** is an isolated environment for a single feature. Each
workspace gets its own git worktree (branch), PRD, and working directory.
You can have multiple workspaces active at once and switch between them.

```
.ralph/workspaces/
  login-page/              # workspace directory
    tree/                  # git worktree (checked out code)
    workspace.json         # metadata (name, branch, createdAt)
    prd.json               # workspace-specific PRD
  dark-mode/
    tree/
    workspace.json
    prd.json
```

**Why workspaces?**

- Each feature is fully isolated -- no conflicts between parallel work
- Shell integration tracks which workspace you're in
- `ralph run`, `ralph chat`, `ralph rebase` automatically use the right
  branch and PRD based on your current workspace
- When you're done, `ralph done` squash-merges and cleans up

You can also run commands in **base** mode (the main repo) without a
workspace, but workspaces are the recommended workflow.

## Prerequisites

- **Go 1.25+** (build from source)
- **[Claude CLI](https://docs.anthropic.com/en/docs/claude-code)** (`claude`) installed and authenticated
- **git** (for worktree operations)

## Installation

```bash
go install github.com/uesteibar/ralph/cmd/ralph@latest
```

Or clone and install with [just](https://github.com/casey/just):

```bash
git clone https://github.com/uesteibar/ralph.git
cd ralph
just install
```

## Quick Start

```bash
# 1. Navigate to your project
cd ~/code/my-project

# 2. Initialize Ralph
ralph init

# 3. Set up shell integration (add to ~/.bashrc or ~/.zshrc)
eval "$(ralph shell-init)"

# 4. Edit the generated config (set quality checks, etc.)
$EDITOR .ralph/ralph.yaml

# 5. Create a workspace for your feature
ralph workspaces new login-page

# 6. Create a PRD interactively (use /finish when ready)
ralph prd new

# 7. Run the autonomous loop
ralph run

# 8. When all stories pass, merge and clean up
ralph done
```

## Shell Integration

Ralph requires shell integration for workspace switching. Add this to your
`~/.bashrc` or `~/.zshrc`:

```bash
eval "$(ralph shell-init)"
```

This wraps the `ralph` command so that workspace operations (`workspaces new`,
`workspaces switch`, `switch`, `done`) automatically change your working
directory to the correct workspace tree.

Currently supports **bash** and **zsh**.

## Commands

### `ralph init`

Scaffolds the `.ralph/` directory in the current project. Idempotent --
safe to run multiple times.

```bash
ralph init
```

**What it does:**

1. Creates `.ralph/` directory with subdirectories `tasks/`, `skills/`, `workspaces/`
2. Generates `.ralph/ralph.yaml` with sensible defaults (skipped if it exists)
3. Creates `.ralph/progress.txt` (shared progress log)
4. Creates `.ralph/state/` with `archive/` and `workspaces.json`
5. Installs `.claude/commands/finish.md` (the `/finish` skill for PRD creation)
6. Adds `.ralph/workspaces/` and `.ralph/state/` to `.gitignore`
7. Prints shell integration instructions

**Generated structure:**

```
.ralph/                     # committed to git
  ralph.yaml                # project configuration (edit this)
  progress.txt              # shared progress log
  tasks/                    # task markdown files
  skills/                   # project-specific skills for the agent
  workspaces/               # gitignored -- workspace directories

.ralph/state/               # gitignored -- local working state
  workspaces.json           # registry of all workspaces
  prd.json                  # base PRD (when not using workspaces)
  archive/                  # completed PRDs

.claude/commands/           # Claude CLI skills
  finish.md                 # /finish skill -- structures conversation into PRD
```

---

### `ralph workspaces new <name>`

Creates a new workspace with an isolated git worktree and branch.

```bash
ralph workspaces new login-page
ralph workspaces new dark-mode --project-config /path/to/config.yaml
```

The branch name is derived from `repo.branch_prefix` + workspace name
(e.g., `ralph/login-page`). After creation, your shell changes directory
into the workspace's `tree/` folder and `ralph prd new` is launched
automatically if no PRD exists.

---

### `ralph workspaces list`

Lists all registered workspaces, showing the current one.

```bash
ralph workspaces list
```

Example output:

```
* login-page [current]
  dark-mode
  base
```

---

### `ralph workspaces switch <name>`

Switches to an existing workspace (or `base` for the main repo).

```bash
ralph workspaces switch dark-mode
ralph workspaces switch base
```

---

### `ralph workspaces remove <name>`

Removes a workspace: deletes the git worktree, branch, and registry entry.

```bash
ralph workspaces remove login-page
```

---

### `ralph switch`

Interactive workspace picker. Shows all workspaces and lets you select
one with arrow keys.

```bash
ralph switch
```

Requires shell integration (`eval "$(ralph shell-init)"`).

---

### `ralph status`

Shows workspace and story progress.

```bash
ralph status
ralph status --short
```

**Full output** shows workspace name, branch, and story/test progress.
**Short output** (`--short`) prints a single line suitable for shell
prompts: `workspace-name N/M` or `base`.

---

### `ralph shell-init`

Prints the shell function for workspace integration. Not called directly --
use `eval "$(ralph shell-init)"` in your shell config.

```bash
eval "$(ralph shell-init)"
```

---

### `ralph validate`

Validates the project configuration.

```bash
ralph validate
ralph validate --project-config /path/to/config.yaml
```

**What it checks:**

- Required fields (`project`, `repo.default_base`)
- `repo.branch_pattern` is valid regex (if set)
- `quality_checks` are defined (warns if empty)

---

### `ralph prd new`

Starts an interactive Claude session to create a PRD.

```bash
ralph prd new
ralph prd new --workspace login-page
```

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--workspace` | auto-detect | Workspace name |

- Discuss the feature, refine scope, answer clarifying questions
- Type `/finish` when ready -- Claude writes the PRD
- Then run `ralph run` to execute the loop

---

### `ralph run`

Runs the autonomous execution loop.

```bash
ralph run
ralph run --max-iterations 10
ralph run --workspace login-page
```

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--max-iterations` | `20` | Maximum loop iterations |
| `--workspace` | auto-detect | Workspace name |

For each iteration, Ralph reads the PRD, finds the next unfinished story,
invokes Claude to implement it, and checks if all stories are complete.

---

### `ralph chat`

Opens a free-form interactive Claude session in the workspace context.

```bash
ralph chat
ralph chat --continue
ralph chat --workspace login-page
```

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--workspace` | auto-detect | Workspace name |
| `--continue` | `false` | Resume the most recent conversation |

---

### `ralph rebase`

Rebases the workspace branch onto the latest base branch. If conflicts
occur, Claude is invoked to resolve them.

```bash
ralph rebase
ralph rebase develop
ralph rebase --workspace login-page
```

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--workspace` | auto-detect | Workspace name |

---

### `ralph done`

Squash-merges the workspace branch into the base branch, archives the PRD,
and removes the workspace.

```bash
ralph done
ralph done --workspace login-page
```

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--workspace` | auto-detect | Workspace name |

---

## Configuration

Ralph is configured per-project via a YAML file at `.ralph/ralph.yaml`
(or passed explicitly with `--project-config`).

### Config Discovery

When `--project-config` is not provided, Ralph walks up from the current
working directory looking for `.ralph/ralph.yaml`. This means you can run
commands from any subdirectory within your project.

### Full Config Reference

```yaml
# Human-readable project name
project: MyProject

repo:
  # Branch that workspaces are created from
  default_base: main

  # Prefix prepended to workspace names to form branch names
  # e.g., "ralph/" + "login-page" = "ralph/login-page"
  branch_prefix: "ralph/"

  # Regex that generated branch names must match
  branch_pattern: "^ralph/[a-zA-Z0-9._-]+$"

# Paths relative to the repo root
paths:
  tasks_dir: ".ralph/tasks"
  skills_dir: ".ralph/skills"

# Commands that must pass before committing
# Each command runs via `sh -c` in the workspace working directory
quality_checks:
  - "npm test"
  - "npm run lint"
  - "npm run typecheck"

# Files/patterns to copy from repo root into workspace worktrees
copy_to_worktree:
  - ".env"
  - "scripts/**"
```

### Required Fields

| Field | Description |
|-------|-------------|
| `project` | Project name |
| `repo.default_base` | Base branch (e.g., `main`) |

All other fields are optional.

## PRD Format

The PRD (Product Requirements Document) is a JSON file that drives the
execution loop. It is generated by `ralph prd new` and consumed by each
loop iteration.

```json
{
  "project": "MyProject",
  "branchName": "ralph/add-dark-mode",
  "description": "Add dark mode support",
  "userStories": [
    {
      "id": "US-001",
      "title": "Add theme toggle to settings",
      "description": "As a user, I want a theme toggle so I can switch to dark mode",
      "acceptanceCriteria": [
        "Toggle renders in settings page",
        "Toggling switches theme",
        "All quality checks pass"
      ],
      "priority": 1,
      "passes": false,
      "notes": ""
    }
  ]
}
```

### Story Sizing

Each story must be completable in one Claude context window (one
iteration). Right-sized examples:

- Add a database column and migration
- Add a UI component to an existing page
- Update a server action with new logic

Split larger work into multiple stories ordered by dependency.

## Architecture

```
cmd/ralph/main.go              Entry point + CLI dispatch
internal/
  config/                      YAML config types + loading
  shell/                       Subprocess runner (exec.Command wrapper)
  prd/                         PRD JSON types + read/write
  gitops/                      Git operations (worktrees, branches)
  workspace/                   Workspace management (create, remove, resolve)
  claude/                      Claude CLI invocation + output parsing
  prompts/                     Embedded prompt templates (go:embed)
  loop/                        The Ralph execution loop
  commands/                    One file per CLI command
```

### Design Decisions

- **Minimal dependencies**: `gopkg.in/yaml.v3` for config, `lipgloss` and
  `huh` for terminal UI. Everything else is stdlib.
- **Shell out to CLIs**: `git` and `claude` are invoked as subprocesses.
  No API client libraries.
- **Prompts embedded in binary**: Markdown templates are compiled into the
  binary via `go:embed`. No external prompt files needed at runtime.
- **Workspace isolation**: Each feature runs in a separate git worktree
  managed as a workspace, keeping the main branch clean and enabling
  parallel agents.

## Development

```bash
# Build
just build

# Run tests
just test

# Build + test + vet
just check
```
