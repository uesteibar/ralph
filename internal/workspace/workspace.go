package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Workspace represents a named workspace with metadata.
type Workspace struct {
	Name      string    `json:"name"`
	Branch    string    `json:"branch"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkContext holds the resolved context for the current working environment.
type WorkContext struct {
	Name         string // workspace name or "base"
	WorkDir      string // tree/ directory or repo root
	PRDPath      string // workspace/prd.json or .ralph/state/prd.json
	ProgressPath string // workspace/progress.txt or .ralph/progress.txt
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidateName checks that a workspace name matches the allowed pattern.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name must not be empty")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid workspace name %q: must match ^[a-zA-Z0-9._-]+$", name)
	}
	return nil
}

// WorkspacePath returns the workspace directory: <repoPath>/.ralph/workspaces/<name>/
func WorkspacePath(repoPath, name string) string {
	return filepath.Join(repoPath, ".ralph", "workspaces", name)
}

// TreePath returns the worktree directory: <repoPath>/.ralph/workspaces/<name>/tree/
func TreePath(repoPath, name string) string {
	return filepath.Join(repoPath, ".ralph", "workspaces", name, "tree")
}

// PRDPathForWorkspace returns the PRD path: <repoPath>/.ralph/workspaces/<name>/prd.json
func PRDPathForWorkspace(repoPath, name string) string {
	return filepath.Join(repoPath, ".ralph", "workspaces", name, "prd.json")
}

// ProgressPathForWorkspace returns the progress path: <repoPath>/.ralph/workspaces/<name>/progress.txt
func ProgressPathForWorkspace(repoPath, name string) string {
	return filepath.Join(repoPath, ".ralph", "workspaces", name, "progress.txt")
}

// DeriveBranch returns prefix + name and validates against branchPattern if set.
func DeriveBranch(prefix, name, branchPattern string) (string, error) {
	branch := prefix + name
	if branchPattern != "" {
		re, err := regexp.Compile(branchPattern)
		if err != nil {
			return "", fmt.Errorf("invalid branch_pattern %q: %w", branchPattern, err)
		}
		if !re.MatchString(branch) {
			return "", fmt.Errorf("derived branch %q does not match branch_pattern %q", branch, branchPattern)
		}
	}
	return branch, nil
}

// DetectCurrent parses cwd for .ralph/workspaces/<name>/tree path segment.
// Returns (name, true) if inside a workspace tree, or ("", false) otherwise.
func DetectCurrent(cwd string) (string, bool) {
	// Normalize path separators
	normalized := filepath.ToSlash(cwd)

	// Look for the pattern .ralph/workspaces/<name>/tree
	const marker = ".ralph/workspaces/"
	_, rest, found := strings.Cut(normalized, marker)
	if !found {
		return "", false
	}
	// rest should be "<name>/tree" or "<name>/tree/..."
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return "", false
	}
	name := parts[0]
	if parts[1] != "tree" {
		return "", false
	}
	if name == "" {
		return "", false
	}
	return name, true
}

// registryEntry is the JSON structure stored in workspaces.json.
type registryEntry struct {
	Name      string    `json:"name"`
	Branch    string    `json:"branch"`
	CreatedAt time.Time `json:"createdAt"`
	Missing   bool      `json:"missing,omitempty"`
}

func registryPath(repoPath string) string {
	return filepath.Join(repoPath, ".ralph", "state", "workspaces.json")
}

func readRegistry(repoPath string) ([]registryEntry, error) {
	data, err := os.ReadFile(registryPath(repoPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading workspaces registry: %w", err)
	}
	var entries []registryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing workspaces registry: %w", err)
	}
	return entries, nil
}

func writeRegistry(repoPath string, entries []registryEntry) error {
	dir := filepath.Dir(registryPath(repoPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating registry directory: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling workspaces registry: %w", err)
	}
	return os.WriteFile(registryPath(repoPath), data, 0644)
}

// RegistryCreate adds a workspace to the registry.
func RegistryCreate(repoPath string, ws Workspace) error {
	entries, err := readRegistry(repoPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == ws.Name {
			return fmt.Errorf("workspace %q already exists in registry", ws.Name)
		}
	}
	entries = append(entries, registryEntry{
		Name:      ws.Name,
		Branch:    ws.Branch,
		CreatedAt: ws.CreatedAt,
	})
	return writeRegistry(repoPath, entries)
}

// RegistryList returns all registered workspaces, detecting missing directories.
func RegistryList(repoPath string) ([]Workspace, error) {
	entries, err := readRegistry(repoPath)
	if err != nil {
		return nil, err
	}
	var result []Workspace
	for _, e := range entries {
		ws := Workspace{
			Name:      e.Name,
			Branch:    e.Branch,
			CreatedAt: e.CreatedAt,
		}
		result = append(result, ws)
	}
	return result, nil
}

// WorkspaceEntry represents a workspace with its missing status for list output.
type WorkspaceEntry struct {
	Name    string
	Branch  string
	Missing bool
}

// RegistryListWithMissing returns all registered workspaces with a missing flag
// for workspaces whose directories no longer exist.
func RegistryListWithMissing(repoPath string) ([]WorkspaceEntry, error) {
	entries, err := readRegistry(repoPath)
	if err != nil {
		return nil, err
	}
	var result []WorkspaceEntry
	for _, e := range entries {
		entry := WorkspaceEntry{
			Name:   e.Name,
			Branch: e.Branch,
		}
		wsDir := WorkspacePath(repoPath, e.Name)
		if _, statErr := os.Stat(wsDir); os.IsNotExist(statErr) {
			entry.Missing = true
		}
		result = append(result, entry)
	}
	return result, nil
}

// RegistryGet returns a single workspace from the registry.
func RegistryGet(repoPath, name string) (*Workspace, error) {
	entries, err := readRegistry(repoPath)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name {
			ws := Workspace{
				Name:      e.Name,
				Branch:    e.Branch,
				CreatedAt: e.CreatedAt,
			}
			// Detect missing directory
			wsDir := WorkspacePath(repoPath, name)
			if _, statErr := os.Stat(wsDir); os.IsNotExist(statErr) {
				return nil, fmt.Errorf("workspace %q directory is missing", name)
			}
			return &ws, nil
		}
	}
	return nil, fmt.Errorf("workspace %q not found", name)
}

// RegistryRemove removes a workspace from the registry by name.
func RegistryRemove(repoPath, name string) error {
	entries, err := readRegistry(repoPath)
	if err != nil {
		return err
	}
	found := false
	var remaining []registryEntry
	for _, e := range entries {
		if e.Name == name {
			found = true
			continue
		}
		remaining = append(remaining, e)
	}
	if !found {
		return fmt.Errorf("workspace %q not found in registry", name)
	}
	if remaining == nil {
		remaining = []registryEntry{}
	}
	return writeRegistry(repoPath, remaining)
}

// ReadWorkspaceJSON reads the workspace.json file from a workspace directory.
func ReadWorkspaceJSON(repoPath, name string) (*Workspace, error) {
	path := filepath.Join(WorkspacePath(repoPath, name), "workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading workspace.json: %w", err)
	}
	var ws Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parsing workspace.json: %w", err)
	}
	return &ws, nil
}

// WriteWorkspaceJSON writes the workspace.json file to a workspace directory.
func WriteWorkspaceJSON(repoPath, name string, ws Workspace) error {
	dir := WorkspacePath(repoPath, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling workspace.json: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "workspace.json"), data, 0644)
}

// ResolveWorkContext resolves the current work context using the following priority:
// 1) --workspace flag (workspaceFlag)
// 2) RALPH_WORKSPACE env var (envVar)
// 3) cwd inside workspace tree (detected from cwd)
// 4) "base" with repo root paths
func ResolveWorkContext(workspaceFlag, envVar, cwd, repoPath string) (WorkContext, error) {
	// Priority 1: explicit flag
	if workspaceFlag != "" {
		return workContextForWorkspace(repoPath, workspaceFlag)
	}

	// Priority 2: env var
	if envVar != "" {
		return workContextForWorkspace(repoPath, envVar)
	}

	// Priority 3: detect from cwd
	if name, ok := DetectCurrent(cwd); ok {
		return workContextForWorkspace(repoPath, name)
	}

	// Priority 4: base
	return WorkContext{
		Name:         "base",
		WorkDir:      repoPath,
		PRDPath:      filepath.Join(repoPath, ".ralph", "state", "prd.json"),
		ProgressPath: "",
	}, nil
}

func workContextForWorkspace(repoPath, name string) (WorkContext, error) {
	if err := ValidateName(name); err != nil {
		return WorkContext{}, err
	}
	return WorkContext{
		Name:         name,
		WorkDir:      TreePath(repoPath, name),
		PRDPath:      PRDPathForWorkspace(repoPath, name),
		ProgressPath: ProgressPathForWorkspace(repoPath, name),
	}, nil
}
