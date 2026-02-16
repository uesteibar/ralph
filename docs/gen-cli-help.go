//go:build ignore

// gen-cli-help.go generates docs/src/ralph/commands.md from the ralph binary's
// help output. Run via: go run docs/gen-cli-help.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// command describes a Ralph CLI command for documentation.
type command struct {
	Name        string
	Description string
	Usage       string
	// SkipHelp means don't run --help (output is not useful).
	SkipHelp bool
}

var commands = []command{
	{Name: "init", Description: "Scaffold .ralph/ directory and config", Usage: "ralph init"},
	{Name: "validate", Description: "Validate project configuration", Usage: "ralph validate [--project-config path]"},
	{Name: "run", Description: "Run the agent loop", Usage: "ralph run [--project-config path] [--max-iterations n] [--workspace name] [--no-tui]"},
	{Name: "chat", Description: "Ad-hoc Claude session", Usage: "ralph chat [--project-config path] [--continue] [--workspace name]"},
	{Name: "switch", Description: "Switch workspace (interactive picker if no name)", Usage: "ralph switch [name] [--project-config path]"},
	{Name: "rebase", Description: "Rebase onto base branch", Usage: "ralph rebase [branch] [--project-config path] [--workspace name]"},
	{Name: "new", Description: "Create a new workspace (alias for `ralph workspaces new`)", Usage: "ralph new <name> [--project-config path]"},
	{Name: "eject", Description: "Export prompt templates to .ralph/prompts/ for customization", Usage: "ralph eject [--project-config path]"},
	{Name: "tui", Description: "Multi-workspace overview TUI", Usage: "ralph tui [--project-config path]"},
	{Name: "attach", Description: "Attach to a running daemon's viewer", Usage: "ralph attach [--project-config path] [--workspace name] [--no-tui]"},
	{Name: "stop", Description: "Stop a running daemon", Usage: "ralph stop [<name>] [--project-config path] [--workspace name]"},
	{Name: "done", Description: "Squash-merge and clean up", Usage: "ralph done [--project-config path] [--workspace name]"},
	{Name: "status", Description: "Show workspace and story progress", Usage: "ralph status [--project-config path] [--short]"},
	{Name: "overview", Description: "Show progress across all workspaces", Usage: "ralph overview [--project-config path]"},
	{Name: "workspaces", Description: "Manage workspaces (new, list, switch, remove, prune)", Usage: "ralph workspaces <subcommand> [args...]", SkipHelp: true},
	{Name: "check", Description: "Run command with compact output, log full output", Usage: "ralph check [--tail N] <command> [args...]", SkipHelp: true},
	{Name: "shell-init", Description: "Print shell integration (eval in .bashrc/.zshrc)", Usage: "ralph shell-init", SkipHelp: true},
}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	docsDir := filepath.Dir(thisFile)
	ralphBin := filepath.Join(docsDir, "..", "ralph")

	// Verify the binary exists.
	if _, err := os.Stat(ralphBin); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ralph binary not found at %s — run 'go build -o ralph ./cmd/ralph' first\n", ralphBin)
		os.Exit(1)
	}

	var sb strings.Builder
	sb.WriteString("# Commands\n\n")
	sb.WriteString("CLI command reference for Ralph. This page is auto-generated from `ralph --help` output.\n\n")
	sb.WriteString("> To regenerate: `just docs-gen-cli`\n\n")

	for _, cmd := range commands {
		fmt.Fprintf(&sb, "## `%s`\n\n", cmd.Name)
		fmt.Fprintf(&sb, "%s\n\n", cmd.Description)
		fmt.Fprintf(&sb, "```\n%s\n```\n\n", cmd.Usage)

		if !cmd.SkipHelp {
			flags := getFlags(ralphBin, cmd.Name)
			if flags != "" {
				sb.WriteString("**Flags:**\n\n")
				sb.WriteString("```\n")
				sb.WriteString(flags)
				sb.WriteString("```\n\n")
			}
		}

		if cmd.Name == "workspaces" {
			sb.WriteString("**Subcommands:**\n\n")
			sb.WriteString("| Subcommand | Description |\n")
			sb.WriteString("|------------|-------------|\n")
			sb.WriteString("| `new <name>` | Create a new workspace |\n")
			sb.WriteString("| `list` | List all workspaces |\n")
			sb.WriteString("| `switch <name>` | Switch to a workspace |\n")
			sb.WriteString("| `remove <name>` | Remove a workspace |\n")
			sb.WriteString("| `prune` | Remove all done workspaces |\n\n")
		}
	}

	outPath := filepath.Join(docsDir, "src", "ralph", "commands.md")
	if err := os.WriteFile(outPath, []byte(sb.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s\n", outPath)
}

// getFlags runs "ralph <cmd> --help" and extracts flag lines.
func getFlags(ralphBin, cmd string) string {
	args := []string{cmd, "--help"}
	// For "new", it's an alias for "workspaces new", so help uses the workspaces new flags.
	if cmd == "new" {
		args = []string{"new", "--help", "placeholder"}
	}

	out, _ := exec.Command(ralphBin, args...).CombinedOutput()
	output := string(out)

	// The flag.Parse --help output looks like:
	//   Usage of <cmd>:
	//     -flag-name type
	//       description
	var lines []string
	inFlags := false
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Usage of ") {
			inFlags = true
			continue
		}
		if inFlags && line != "" {
			// Skip error lines like "ralph attach: flag: help requested".
			if strings.Contains(line, ": flag:") {
				continue
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
