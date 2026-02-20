# Getting Started

## Prerequisites

- **Go 1.25+** (build from source)
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** (`claude` CLI) installed and authenticated
- **git** (for worktree operations)
- **bash** or **zsh** (for shell integration)

To build from source, you also need **just** (task runner). For full development (web, docs, E2E), use [mise](https://mise.jdx.dev/) — the project includes a `mise.toml` with all tool versions; run `mise install` in the repo root.

## Installation

### From install script

```bash
curl -fsSL https://raw.githubusercontent.com/uesteibar/ralph/main/install-ralph.sh | sh
```

### From source (go install)

```bash
go install github.com/uesteibar/ralph/cmd/ralph@latest
```

### From source (clone and build)

```bash
git clone https://github.com/uesteibar/ralph.git
cd ralph
mise install    # Install Go, just, etc. (recommended)
just install   # Install ralph to $GOPATH/bin
```

## Quick Start

```bash
# 1. Navigate to your project
cd ~/code/my-project

# 2. Initialize Ralph (creates .ralph/ directory and config)
ralph init

# 3. Set up shell integration (add to ~/.bashrc or ~/.zshrc)
eval "$(ralph shell-init)"

# 4. Review and edit the generated config
$EDITOR .ralph/ralph.yaml

# 5. Create a workspace and start building
ralph new login-page
#    └─ Creates workspace, cd's into it, starts PRD session with Claude

# 6. Discuss the feature with Claude, then type /finish

# 7. Run the autonomous loop
ralph run

# 8. When done, squash-merge and clean up
ralph done
```

### What happens in each step

1. **`ralph init`** scaffolds the `.ralph/` directory with a config file, progress log, and prompt templates. It optionally uses Claude to auto-detect your project's quality check commands.

2. **`ralph new login-page`** creates an isolated workspace (git worktree + branch), switches you into it, and launches an interactive PRD creation session with Claude.

3. **During the PRD session**, you discuss the feature with Claude. When the plan is solid, type **`/finish`** and Claude writes a structured PRD with user stories and integration tests.

4. **`ralph run`** starts the autonomous execution loop. Ralph picks up stories one by one, implements them, runs quality checks, and commits. After all stories pass, a QA phase verifies integration tests.

5. **`ralph done`** squash-merges the workspace branch into your base branch, archives the PRD, and cleans up the workspace.
