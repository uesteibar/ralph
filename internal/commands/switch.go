package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/uesteibar/ralph/internal/workspace"
)

// switchPickerFn is a package-level variable for the interactive picker,
// allowing tests to substitute a mock implementation.
var switchPickerFn = runSwitchPicker

// Switch handles workspace switching. With a positional argument it switches
// directly; without one it shows an interactive picker. Outputs the selected
// workspace name on the first line and tree/ path on the second line to stdout.
func Switch(args []string) error {
	fs := flag.NewFlagSet("switch", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Check shell integration.
	if os.Getenv("RALPH_SHELL_INIT") == "" {
		return fmt.Errorf("Shell integration required. Add to your shell config:\n\n  eval \"$(ralph shell-init)\"\n\nThen restart your shell.")
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	repoPath := cfg.Repo.Path

	// Print workspace header for current context.
	curWC, _ := resolveWorkContextFromFlags("", repoPath)
	printWorkspaceHeader(curWC, repoPath)

	remaining := fs.Args()

	var selected string
	if len(remaining) > 0 {
		selected = remaining[0]
		if err := switchDirect(repoPath, selected); err != nil {
			return err
		}
	} else {
		var err error
		selected, err = switchInteractive(repoPath)
		if err != nil {
			return err
		}
	}

	// Resolve path for the selected workspace.
	var targetPath string
	if selected == "base" {
		targetPath = repoPath
	} else {
		targetPath = workspace.TreePath(repoPath, selected)
	}

	fmt.Fprintf(os.Stderr, "Switched to workspace %s\n", selected)
	// Output name then path — the shell function reads both.
	fmt.Println(selected)
	fmt.Println(targetPath)

	return nil
}

// switchDirect validates that the named workspace exists.
func switchDirect(repoPath, name string) error {
	if name == "base" {
		return nil
	}

	_, err := workspace.RegistryGet(repoPath, name)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			return fmt.Errorf("Workspace %q not found. Run ralph workspaces list to see available.", name)
		}
		if strings.Contains(errMsg, "directory is missing") {
			return fmt.Errorf("Workspace %q directory is missing. Remove it: ralph workspaces remove %s", name, name)
		}
		return err
	}
	return nil
}

// switchInteractive shows the huh picker and returns the selected name.
func switchInteractive(repoPath string) (string, error) {
	entries, err := workspace.RegistryListWithMissing(repoPath)
	if err != nil {
		return "", fmt.Errorf("reading workspace registry: %w", err)
	}

	options := []huh.Option[string]{
		huh.NewOption("base", "base"),
	}
	for _, e := range entries {
		if e.Missing {
			continue
		}
		options = append(options, huh.NewOption(e.Name, e.Name))
	}

	selected, err := switchPickerFn(options)
	if err != nil {
		return "", fmt.Errorf("selection cancelled")
	}
	return selected, nil
}

// runSwitchPicker runs the interactive huh select picker rendered to stderr.
func runSwitchPicker(options []huh.Option[string]) (string, error) {
	var selected string
	sel := huh.NewSelect[string]().
		Title("Select workspace").
		Options(options...).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).WithOutput(os.Stderr)
	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}
