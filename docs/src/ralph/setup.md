# Setup

How to set up Ralph in an existing codebase.

## Initialize Your Project

Run `ralph init` from your project root:

```bash
cd ~/code/my-project
ralph init
```

This scaffolds the `.ralph/` directory with configuration and prompt templates.

### Interactive Prompts

During init, Ralph asks two questions:

1. **Git tracking choice:**
   - *Track in git* (recommended for teams) — gitignores only ephemeral directories
   - *Keep local* — gitignores the entire `.ralph/` directory

2. **LLM analysis:** optionally uses Claude to auto-detect quality check commands for your project (test runners, linters, type checkers, etc.)

### What It Creates

| Path | Purpose |
|------|---------|
| `.ralph/ralph.yaml` | Project configuration (edit this!) |
| `.ralph/tasks/` | Task markdown files |
| `.ralph/skills/` | Project-specific skills for the agent |
| `.ralph/workspaces/` | Workspace directories (gitignored) |
| `.ralph/state/workspaces.json` | Workspace registry (gitignored) |
| `.ralph/state/archive/` | Completed PRDs (gitignored) |
| `.claude/commands/finish.md` | The `/finish` skill for PRD generation |
| `.claude/CLAUDE.md` | Project rules for the agent |

## Shell Integration

Ralph requires a thin shell wrapper for workspace switching. It lets commands like `ralph new`, `ralph switch`, and `ralph done` change your working directory and track the current workspace via the `RALPH_WORKSPACE` environment variable.

Add this to your `~/.bashrc` or `~/.zshrc`:

```bash
eval "$(ralph shell-init)"
```

### What It Does

- Wraps the `ralph` binary in a shell function
- Intercepts workspace commands (`new`, `switch`, `done`, `workspaces *`) to automatically `cd` into the correct directory
- Sets/unsets `RALPH_WORKSPACE` to track the active workspace
- After `ralph new`, automatically starts an interactive PRD creation session if no PRD exists yet
- All other commands pass through to the binary unchanged

Currently supports **bash** and **zsh**.

### Shell Prompt Integration

You can embed workspace status in your shell prompt:

```bash
PS1='$(ralph status --short 2>/dev/null) $ '
# Shows: "login-page 3/5 $ " or "base $ "
```

## Edit Your Configuration

After init, review and customize `.ralph/ralph.yaml`:

```bash
$EDITOR .ralph/ralph.yaml
```

At minimum, verify:

- `project` is set to your project name
- `repo.default_base` points to your main branch (e.g., `main`, `develop`)
- `quality_checks` lists the commands the agent should run before committing

See the [Configuration](configuration.md) chapter for the full reference.

## Verify Setup

Run the config validator to check for issues:

```bash
ralph validate
```

This checks required fields, validates regex patterns, and warns about missing quality checks.

## Start Building

With setup complete, create your first workspace:

```bash
ralph new my-first-feature
```

This creates an isolated workspace and launches the PRD creation session. See the [Workflow](workflow.md) chapter for the full development cycle.
