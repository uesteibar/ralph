# Commands

CLI command reference for Ralph. This page is auto-generated from `ralph --help` output.

> To regenerate: `just docs-gen-cli`

## `init`

Scaffold .ralph/ directory and config

```
ralph init
```

## `validate`

Validate project configuration

```
ralph validate [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `run`

Run the agent loop

```
ralph run [--project-config path] [--max-iterations n] [--workspace name] [--no-tui]
```

**Flags:**

```
  -max-iterations int
    	Maximum loop iterations (default 20)
  -no-tui
    	Disable TUI and use plain-text output
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -verbose
    	Enable verbose debug logging
  -workspace string
    	Workspace name
```

## `chat`

Ad-hoc Claude session

```
ralph chat [--project-config path] [--continue] [--workspace name]
```

**Flags:**

```
  -continue
    	Resume the most recent conversation
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -workspace string
    	Workspace name
```

## `switch`

Switch workspace (interactive picker if no name)

```
ralph switch [name] [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `rebase`

Rebase onto base branch

```
ralph rebase [branch] [--project-config path] [--workspace name]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -workspace string
    	Workspace name
```

## `new`

Create a new workspace (alias for `ralph workspaces new`)

```
ralph new <name> [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `eject`

Export prompt templates to .ralph/prompts/ for customization

```
ralph eject [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `tui`

Multi-workspace overview TUI

```
ralph tui [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `attach`

Attach to a running daemon's viewer

```
ralph attach [--project-config path] [--workspace name] [--no-tui]
```

**Flags:**

```
  -no-tui
    	Disable TUI and use plain-text output
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -workspace string
    	Workspace name
```

## `stop`

Stop a running daemon

```
ralph stop [<name>] [--project-config path] [--workspace name]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -workspace string
    	Workspace name
```

## `done`

Squash-merge and clean up

```
ralph done [--project-config path] [--workspace name]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -workspace string
    	Workspace name
```

## `status`

Show workspace and story progress

```
ralph status [--project-config path] [--short]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
  -short
    	Short output for shell prompt embedding
```

## `overview`

Show progress across all workspaces

```
ralph overview [--project-config path]
```

**Flags:**

```
  -project-config string
    	Path to project config YAML (default: discover .ralph/ralph.yaml)
```

## `workspaces`

Manage workspaces (new, list, switch, remove, prune)

```
ralph workspaces <subcommand> [args...]
```

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `new <name>` | Create a new workspace |
| `list` | List all workspaces |
| `switch <name>` | Switch to a workspace |
| `remove <name>` | Remove a workspace |
| `prune` | Remove all done workspaces |

## `check`

Run command with compact output, log full output

```
ralph check [--tail N] <command> [args...]
```

## `shell-init`

Print shell integration (eval in .bashrc/.zshrc)

```
ralph shell-init
```

