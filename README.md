# Ralph

An autonomous coding agent loop. Ralph takes a feature description, breaks
it into right-sized user stories, and implements them one by one -- running
quality checks and committing after each step -- until the feature is done.

You work from your terminal. Ralph works in an isolated git worktree.

## How It Works

1. You describe a feature interactively with Claude
2. When you're happy, type `/finish` -- Claude generates a structured PRD
   with user stories and integration tests
3. Ralph creates an isolated worktree and loops through stories:
   - Pick next unfinished story
   - Implement and test it
   - Run quality checks
   - Commit
   - Repeat until all stories are done
4. QA verification phase:
   - QA agent runs integration tests defined in the PRD
   - If tests fail, QA fix agent resolves the issues
   - Loop continues until all integration tests pass

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           SETUP (once per project)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ralph init              Create .ralph/ directory and config             │
│         │                                                                   │
│         ▼                                                                   │
│   $ $EDITOR .ralph/ralph.yaml    Configure quality checks, branch pattern   │
│         │                                                                   │
│         ▼                                                                   │
│   $ ralph validate          Verify configuration is valid                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         FEATURE WORKFLOW (per feature)                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ralph prd new           Start interactive Claude session                │
│         │                                                                   │
│         │  ┌────────────────────────────────────────┐                       │
│         │  │  Discuss feature with Claude           │                       │
│         │  │  Refine scope, answer questions        │                       │
│         │  │  Type /finish when ready               │                       │
│         │  └────────────────────────────────────────┘                       │
│         │                                                                   │
│         ▼                                                                   │
│   .ralph/state/prd.json     PRD saved with user stories + integration tests │
│         │                                                                   │
│         ▼                                                                   │
│   $ ralph run               Start the autonomous loop                       │
│         │                                                                   │
│         │  ┌────────────────────────────────────────────────────────────┐   │
│         │  │  Creates isolated git worktree                             │   │
│         │  │           │                                                │   │
│         │  │           ▼                                                │   │
│         │  │  ┌─────────────────────────────┐                           │   │
│         │  │  │  Pick next unfinished story │◄──┐                       │   │
│         │  │  └─────────────────────────────┘   │                       │   │
│         │  │           │                        │                       │   │
│         │  │           ▼                        │                       │   │
│         │  │  ┌─────────────────────────────┐   │                       │   │
│         │  │  │  Claude implements + tests  │   │                       │   │
│         │  │  └─────────────────────────────┘   │                       │   │
│         │  │           │                        │                       │   │
│         │  │           ▼                        │                       │   │
│         │  │  ┌─────────────────────────────┐   │                       │   │
│         │  │  │  Run quality checks         │   │                       │   │
│         │  │  └─────────────────────────────┘   │                       │   │
│         │  │           │                        │                       │   │
│         │  │           ▼                        │                       │   │
│         │  │  ┌─────────────────────────────┐   │                       │   │
│         │  │  │  Commit changes             │   │                       │   │
│         │  │  └─────────────────────────────┘   │                       │   │
│         │  │           │                        │                       │   │
│         │  │           ▼                        │                       │   │
│         │  │     More stories? ─── yes ─────────┘                       │   │
│         │  │           │                                                │   │
│         │  │          no                                                │   │
│         │  │           │                                                │   │
│         │  │           ▼                                                │   │
│         │  │  ┌─────────────────────────────────────────────────────┐   │   │
│         │  │  │              QA VERIFICATION PHASE                  │   │   │
│         │  │  ├─────────────────────────────────────────────────────┤   │   │
│         │  │  │                                                     │   │   │
│         │  │  │  ┌───────────────────────────────┐                  │   │   │
│         │  │  │  │  QA agent runs integration    │◄─────────┐       │   │   │
│         │  │  │  │  tests from PRD               │          │       │   │   │
│         │  │  │  └───────────────────────────────┘          │       │   │   │
│         │  │  │           │                                 │       │   │   │
│         │  │  │           ▼                                 │       │   │   │
│         │  │  │     All tests pass?                         │       │   │   │
│         │  │  │        │       │                            │       │   │   │
│         │  │  │       yes      no                           │       │   │   │
│         │  │  │        │       │                            │       │   │   │
│         │  │  │        │       ▼                            │       │   │   │
│         │  │  │        │  ┌───────────────────────────┐     │       │   │   │
│         │  │  │        │  │  QA fix agent resolves    │     │       │   │   │
│         │  │  │        │  │  failing tests            │─────┘       │   │   │
│         │  │  │        │  └───────────────────────────┘             │   │   │
│         │  │  │        │                                            │   │   │
│         │  │  │        ▼                                            │   │   │
│         │  │  │     COMPLETE                                        │   │   │
│         │  │  │                                                     │   │   │
│         │  │  └─────────────────────────────────────────────────────┘   │   │
│         │  │                                                            │   │
│         │  └────────────────────────────────────────────────────────────┘   │
│         │                                                                   │
│         ▼                                                                   │
│   Feature branch ready with all commits                                     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              OPTIONAL COMMANDS                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ralph switch            Jump into a worktree (select interactively)     │
│                             └─► Opens shell in worktree, exit to return     │
│                                                                             │
│   $ ralph chat              Ad-hoc Claude session in worktree context       │
│                             └─► Debug, explore, or make manual changes      │
│                                                                             │
│   $ ralph rebase            Rebase worktree branch onto latest main         │
│                             └─► Keeps feature branch up to date             │
│                                                                             │
│   $ ralph done              Squash-merge feature branch into base branch    │
│                             └─► Optionally cleans up worktree after merge   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

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

# 3. Edit the generated config (set quality checks, etc.)
$EDITOR .ralph/ralph.yaml

# 4. Validate the config
ralph validate

# 5. Create a PRD interactively (use /finish when ready)
ralph prd new

# 6. Run the loop from the staged PRD
ralph run
```

## Commands

### `ralph init`

Scaffolds the `.ralph/` directory in the current project. Idempotent --
safe to run multiple times. Existing files are skipped, skills are
always updated.

```bash
ralph init
```

**What it does:**

1. Creates `.ralph/` directory with subdirectories `tasks/` and `skills/`
2. Generates `.ralph/ralph.yaml` with sensible defaults (skipped if it exists)
3. Creates `.ralph/progress.txt` (shared progress log, committed to git)
4. Creates `.ralph/state/` with `archive/` (gitignored staging area for PRDs)
5. Installs `.claude/commands/finish.md` (the `/finish` skill for PRD creation)
6. Adds `.ralph/worktrees/` and `.ralph/state/` to `.gitignore`

**Generated structure:**

```
.ralph/                     # committed to git
  ralph.yaml                # project configuration (edit this)
  progress.txt              # shared progress log, persists across all runs
  tasks/                    # PRD markdown files
  skills/                   # project-specific skills for the agent

.ralph/state/               # gitignored -- local working state
  prd.json                  # current PRD (staging area)
  archive/                  # completed PRDs
    2026-01-15-ralph__dark-mode/
      prd.json

.claude/commands/           # Claude CLI skills
  finish.md                 # /finish skill -- structures conversation into PRD
```

After running `init`, edit `.ralph/ralph.yaml` to configure your quality
checks and other settings.

---

### `ralph validate`

Validates the project configuration and reports any issues.

```bash
ralph validate
ralph validate --project-config /path/to/config.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |

**What it checks:**

- Required fields (`project`, `repo.default_base`)
- `repo.branch_pattern` is valid regex (if set)
- `quality_checks` are defined (warns if empty)

---

### `ralph prd new`

Starts an interactive conversation with Claude to create a PRD from
scratch. This is the starting point for a new feature.

```bash
ralph prd new
ralph prd new --project-config /path/to/config.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |

**Behavior:**

- Opens an interactive Claude session in your terminal
- Discuss the feature, refine scope, answer clarifying questions
- When you're happy with the plan, type `/finish`
- Claude structures the conversation into a PRD and writes it to
  `.ralph/state/prd.json`
- Then run `ralph run` to execute the loop

---

### `ralph run`

Runs the execution loop from a staged PRD.

```bash
ralph run
ralph run --max-iterations 10
ralph run --project-config /path/to/config.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |
| `--max-iterations` | `20` | Maximum number of loop iterations |

**Step-by-step behavior:**

1. **Read staged PRD** -- Loads `.ralph/state/prd.json` (created by
   `ralph prd new`).

2. **Validate** -- Checks that the PRD has a branch name and at least one
   user story. Validates the branch name against the configured pattern.

3. **Create worktree** -- Creates an isolated git worktree for the PRD's
   branch name.

4. **Copy .ralph/** -- Copies config, skills, and tasks into the worktree.

5. **Write PRD** -- Writes `prd.json` into the worktree's `.ralph/state/`
   directory.

6. **Run the loop** -- For each iteration (up to `--max-iterations`):
   - Reads `prd.json` and finds the highest-priority story with
     `passes: false`
   - Renders a prompt instructing Claude to implement that single story
   - Invokes Claude in `--print` mode
   - Claude implements the story, runs quality checks, commits, and
     updates `prd.json`
   - If Claude signals `COMPLETE`, all stories are done
   - Waits 2 seconds between iterations (context cooldown)

**Exit conditions:**

- All stories pass (success)
- Max iterations reached without completing all stories (error)

---

### `ralph chat`

Opens a free-form interactive Claude session in the project context.
Useful for ad-hoc coding, debugging, or exploring the codebase.

```bash
ralph chat
ralph chat --project-config /path/to/config.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |

**Behavior:**

- Must be run from inside a worktree (use `ralph run` first)
- Claude is given the project name, config, progress log, and recent
  git history as context
- stdin/stdout are connected directly to your terminal

---

### `ralph switch`

Interactively select a worktree and open a new shell session inside it.
Type `exit` to return to where you were.

```bash
ralph switch
ralph switch --project-config /path/to/config.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--project-config` | auto-discover | Path to project config YAML |

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
  # Branch that worktrees are created from
  default_base: main

  # Regex that generated branch names must match
  branch_pattern: "^ralph/[a-zA-Z0-9._-]+$"

# Paths relative to the repo root
paths:
  tasks_dir: ".ralph/tasks"
  skills_dir: ".ralph/skills"

# Commands that must pass before committing
# Each command runs via `sh -c` in the repo root
quality_checks:
  - "npm test"
  - "npm run lint"
  - "npm run typecheck"
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
  claude/                      Claude CLI invocation + output parsing
  prompts/                     Embedded prompt templates (go:embed)
  loop/                        The Ralph execution loop
  commands/                    One file per CLI command
```

### Design Decisions

- **Single dependency**: `gopkg.in/yaml.v3`. Everything else is stdlib.
- **Shell out to CLIs**: `git` and `claude` are invoked as subprocesses.
  No API client libraries.
- **Prompts embedded in binary**: Markdown templates are compiled into the
  binary via `go:embed`. No external prompt files needed at runtime.
- **Worktree isolation**: Each feature runs in a separate git worktree,
  keeping the main branch clean and enabling parallel agents.

## Development

```bash
# Build
just build

# Run tests
just test

# Build + test + vet
just check
```
