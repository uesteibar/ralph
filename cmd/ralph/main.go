package main

import (
	"fmt"
	"os"

	"github.com/uesteibar/ralph/internal/commands"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Ralph — autonomous coding agent loop

Usage:
  ralph init                                     Scaffold .ralph/ directory and config
  ralph validate [--project-config path]         Validate project configuration
  ralph prd new [--project-config path] [--workspace name]   Create a PRD interactively
  ralph run [--project-config path] [--max-iterations n] [--workspace name] [--no-tui]   Run the agent loop
  ralph chat [--project-config path] [--continue] [--workspace name]   Ad-hoc Claude session
  ralph switch [name] [--project-config path]    Switch workspace (interactive picker if no name)
  ralph rebase [branch] [--project-config path] [--workspace name]   Rebase onto base branch
  ralph done [--project-config path] [--workspace name]   Squash-merge and clean up
  ralph status [--project-config path] [--short] Show workspace and story progress
  ralph overview [--project-config path]         Show progress across all workspaces
  ralph workspaces new <name> [--project-config path]   Create a new workspace
  ralph workspaces list [--project-config path]  List all workspaces
  ralph workspaces switch <name>                 Switch to a workspace
  ralph workspaces remove <name>                 Remove a workspace
  ralph shell-init                               Print shell integration (eval in .bashrc/.zshrc)

Flags:
  --project-config    Path to project config YAML (default: discover .ralph/ralph.yaml)
  --max-iterations    Maximum loop iterations for run command (default: 20)
  --workspace         Workspace name to run in (resolves workDir and prdPath)
  --short             Short output for shell prompt embedding (status command only)
  --no-tui            Disable TUI and use plain-text output (run command only)
  --continue          Resume the most recent conversation (chat command only)
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	rest := os.Args[2:]

	// Check for legacy worktrees directory on all commands except init and shell-init.
	if subcmd != "init" && subcmd != "shell-init" && subcmd != "help" && subcmd != "-h" && subcmd != "--help" {
		commands.CheckLegacyWorktrees()
	}

	var err error
	switch subcmd {
	case "init":
		err = commands.Init(rest, os.Stdin)
	case "validate":
		err = commands.Validate(rest)
	case "run":
		err = commands.Run(rest)
	case "prd":
		err = commands.PRD(rest)
	case "chat":
		err = commands.Chat(rest)
	case "status":
		err = commands.Status(rest)
	case "overview":
		err = commands.Overview(rest)
	case "switch":
		err = commands.Switch(rest)
	case "rebase":
		err = commands.Rebase(rest)
	case "done":
		err = commands.Done(rest)
	case "workspaces":
		err = commands.Workspaces(rest)
	case "shell-init":
		err = commands.ShellInit(rest)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", subcmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ralph %s: %v\n", subcmd, err)
		os.Exit(1)
	}
}
