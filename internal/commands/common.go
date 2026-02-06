package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
)

var headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

// AddProjectConfigFlag adds the --project-config flag to a FlagSet.
func AddProjectConfigFlag(fs *flag.FlagSet) *string {
	return fs.String("project-config", "", "Path to project config YAML (default: discover .ralph/ralph.yaml)")
}

// AddWorkspaceFlag adds the --workspace flag to a FlagSet.
func AddWorkspaceFlag(fs *flag.FlagSet) *string {
	return fs.String("workspace", "", "Workspace name")
}

// resolveWorkContextFromFlags resolves workspace context from the --workspace
// flag value, RALPH_WORKSPACE env var, cwd, and repo path.
func resolveWorkContextFromFlags(workspaceFlag string, repoPath string) (workspace.WorkContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return workspace.WorkContext{}, fmt.Errorf("getting current directory: %w", err)
	}
	envWS := os.Getenv("RALPH_WORKSPACE")
	return workspace.ResolveWorkContext(workspaceFlag, envWS, cwd, repoPath)
}

// ResolveConfig loads the project config from the explicit flag value or by discovery.
func ResolveConfig(flagValue string) (*config.Config, error) {
	return config.Resolve(flagValue)
}

// RequirePositionalInt extracts a required integer positional argument.
func RequirePositionalInt(args []string, name string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("missing required argument: <%s>", name)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %q (must be a number)", name, args[0])
	}
	return n, nil
}

// archiveStagedPRD moves the current .ralph/state/prd.json to the archive
// directory if it exists, using the branch name and current date.
func archiveStagedPRD(cfg *config.Config) {
	stagedPath := cfg.StatePRDPath()
	data, err := os.ReadFile(stagedPath)
	if err != nil {
		return
	}

	var p prd.PRD
	if err := json.Unmarshal(data, &p); err != nil || p.BranchName == "" {
		return
	}

	sanitized := sanitizeBranchForArchive(p.BranchName)
	archiveDir := filepath.Join(cfg.StateArchiveDir(),
		time.Now().Format("2006-01-02")+"-"+sanitized)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create archive dir: %v\n", err)
		return
	}

	if err := os.WriteFile(filepath.Join(archiveDir, "prd.json"), data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not archive PRD: %v\n", err)
		return
	}

	os.Remove(stagedPath)
	fmt.Fprintf(os.Stderr, "archived previous PRD to %s\n", archiveDir)
}

func sanitizeBranchForArchive(branch string) string {
	result := make([]byte, 0, len(branch))
	for i := 0; i < len(branch); i++ {
		c := branch[i]
		if c == '/' {
			result = append(result, '_', '_')
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}

// printWorkspaceHeader prints a muted workspace context header to stderr.
// Format: [workspace: login-page | ralph/login-page] or [workspace: base]
func printWorkspaceHeader(wc workspace.WorkContext, repoPath string) {
	var header string
	if wc.Name == "base" {
		header = "[workspace: base]"
	} else {
		ws, err := workspace.RegistryGet(repoPath, wc.Name)
		if err == nil {
			header = fmt.Sprintf("[workspace: %s | %s]", wc.Name, ws.Branch)
		} else {
			header = fmt.Sprintf("[workspace: %s]", wc.Name)
		}
	}
	fmt.Fprintln(os.Stderr, headerStyle.Render(header))
}

// CheckLegacyWorktrees prints a migration warning to stderr if the legacy
// .ralph/worktrees/ directory exists. Tries to discover the repo path from config;
// silently does nothing if config is not available.
func CheckLegacyWorktrees() {
	cfg, err := config.Resolve("")
	if err != nil {
		return
	}
	checkLegacyWorktreesInDir(cfg.Repo.Path)
}

// checkLegacyWorktreesInDir prints a migration warning for a specific repo path.
func checkLegacyWorktreesInDir(repoPath string) {
	legacyDir := filepath.Join(repoPath, ".ralph", "worktrees")
	if info, err := os.Stat(legacyDir); err == nil && info.IsDir() {
		fmt.Fprintln(os.Stderr, "Legacy worktrees directory at .ralph/worktrees/ is no longer used. Consider removing it.")
	}
}
