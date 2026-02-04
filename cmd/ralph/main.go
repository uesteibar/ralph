package main

import (
	"fmt"
	"log"
	"os"

	"github.com/uesteibar/ralph/internal/commands"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Ralph — autonomous coding agent loop

Usage:
  ralph init
  ralph validate [--project-config path]
  ralph prd new [--project-config path]
  ralph run [--project-config path] [--max-iterations n] [--local]
  ralph chat [--project-config path] [--continue]
  ralph switch [--project-config path]
  ralph rebase [branch] [--project-config path]
  ralph done [--project-config path]

Flags:
  --project-config    Path to project config YAML (default: discover .ralph/ralph.yaml)
  --max-iterations    Maximum loop iterations for run command (default: 20)
  --local             Skip worktree creation and run loop in current directory
  --continue          Resume the most recent conversation (chat command only)
`)
}

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	rest := os.Args[2:]

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
	case "switch":
		err = commands.Switch(rest)
	case "rebase":
		err = commands.Rebase(rest)
	case "done":
		err = commands.Done(rest)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", subcmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		log.Fatalf("ralph %s: %v", subcmd, err)
	}
}
