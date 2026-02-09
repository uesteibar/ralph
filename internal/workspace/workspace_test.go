package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Name Validation ---

func TestValidateName_ValidNames(t *testing.T) {
	valid := []string{"my-feature", "login_page", "v1.0", "FooBar", "a", "my.feature-1_2"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateName(name); err != nil {
				t.Errorf("ValidateName(%q) = %v, want nil", name, err)
			}
		})
	}
}

func TestValidateName_InvalidNames(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
	}{
		{"", "must not be empty"},
		{"has spaces", "must match"},
		{"has/slash", "must match"},
		{"special@char", "must match"},
		{"new\nline", "must match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if err == nil {
				t.Fatalf("ValidateName(%q) = nil, want error", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- Path Helpers ---

func TestWorkspacePath(t *testing.T) {
	got := WorkspacePath("/repo", "my-ws")
	want := filepath.Join("/repo", ".ralph", "workspaces", "my-ws")
	if got != want {
		t.Errorf("WorkspacePath = %q, want %q", got, want)
	}
}

func TestTreePath(t *testing.T) {
	got := TreePath("/repo", "my-ws")
	want := filepath.Join("/repo", ".ralph", "workspaces", "my-ws", "tree")
	if got != want {
		t.Errorf("TreePath = %q, want %q", got, want)
	}
}

func TestPRDPathForWorkspace(t *testing.T) {
	got := PRDPathForWorkspace("/repo", "my-ws")
	want := filepath.Join("/repo", ".ralph", "workspaces", "my-ws", "prd.json")
	if got != want {
		t.Errorf("PRDPathForWorkspace = %q, want %q", got, want)
	}
}

func TestProgressPathForWorkspace(t *testing.T) {
	got := ProgressPathForWorkspace("/repo", "my-ws")
	want := filepath.Join("/repo", ".ralph", "workspaces", "my-ws", "progress.txt")
	if got != want {
		t.Errorf("ProgressPathForWorkspace = %q, want %q", got, want)
	}
}

// --- Branch Derivation ---

func TestDeriveBranch_Simple(t *testing.T) {
	branch, err := DeriveBranch("ralph/", "login-page", "")
	if err != nil {
		t.Fatalf("DeriveBranch error: %v", err)
	}
	if branch != "ralph/login-page" {
		t.Errorf("branch = %q, want %q", branch, "ralph/login-page")
	}
}

func TestDeriveBranch_MatchesPattern(t *testing.T) {
	branch, err := DeriveBranch("ralph/", "login-page", `^ralph/[a-zA-Z0-9._-]+$`)
	if err != nil {
		t.Fatalf("DeriveBranch error: %v", err)
	}
	if branch != "ralph/login-page" {
		t.Errorf("branch = %q, want %q", branch, "ralph/login-page")
	}
}

func TestDeriveBranch_PatternMismatch(t *testing.T) {
	_, err := DeriveBranch("feature/", "login-page", `^ralph/[a-zA-Z0-9._-]+$`)
	if err == nil {
		t.Fatal("expected error for pattern mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error = %q, want to contain 'does not match'", err.Error())
	}
}

func TestDeriveBranch_InvalidPattern(t *testing.T) {
	_, err := DeriveBranch("ralph/", "test", "[invalid")
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}
	if !strings.Contains(err.Error(), "invalid branch_pattern") {
		t.Errorf("error = %q, want to contain 'invalid branch_pattern'", err.Error())
	}
}

// --- DetectCurrent ---

func TestDetectCurrent_InsideWorkspaceTree(t *testing.T) {
	tests := []struct {
		cwd      string
		wantName string
		wantOk   bool
	}{
		{"/repo/.ralph/workspaces/my-ws/tree", "my-ws", true},
		{"/repo/.ralph/workspaces/my-ws/tree/src/deep", "my-ws", true},
		{"/repo/.ralph/workspaces/login-page/tree", "login-page", true},
		{"/repo/.ralph/workspaces/v1.0/tree/", "v1.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.cwd, func(t *testing.T) {
			name, ok := DetectCurrent(tt.cwd)
			if ok != tt.wantOk || name != tt.wantName {
				t.Errorf("DetectCurrent(%q) = (%q, %v), want (%q, %v)",
					tt.cwd, name, ok, tt.wantName, tt.wantOk)
			}
		})
	}
}

func TestDetectCurrent_NotInsideWorkspace(t *testing.T) {
	tests := []string{
		"/repo",
		"/repo/.ralph/state",
		"/repo/.ralph/workspaces/my-ws",       // not in tree/ subdir
		"/repo/.ralph/workspaces/my-ws/other",  // wrong subdir
		"/repo/.ralph/workspaces//tree",        // empty name
	}
	for _, cwd := range tests {
		t.Run(cwd, func(t *testing.T) {
			name, ok := DetectCurrent(cwd)
			if ok {
				t.Errorf("DetectCurrent(%q) = (%q, true), want (_, false)", cwd, name)
			}
		})
	}
}

// --- Registry CRUD ---

func TestRegistry_CreateAndList(t *testing.T) {
	dir := t.TempDir()
	repoPath := dir

	// List on missing file returns empty
	list, err := RegistryList(repoPath)
	if err != nil {
		t.Fatalf("RegistryList error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("RegistryList on empty = %d entries, want 0", len(list))
	}

	// Create
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ws := Workspace{Name: "my-ws", Branch: "ralph/my-ws", CreatedAt: now}
	if err := RegistryCreate(repoPath, ws); err != nil {
		t.Fatalf("RegistryCreate error: %v", err)
	}

	// List after create
	list, err = RegistryList(repoPath)
	if err != nil {
		t.Fatalf("RegistryList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("RegistryList = %d entries, want 1", len(list))
	}
	if list[0].Name != "my-ws" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "my-ws")
	}
	if list[0].Branch != "ralph/my-ws" {
		t.Errorf("list[0].Branch = %q, want %q", list[0].Branch, "ralph/my-ws")
	}
}

func TestRegistry_CreateDuplicate_Error(t *testing.T) {
	dir := t.TempDir()
	ws := Workspace{Name: "dup", Branch: "ralph/dup", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}
	err := RegistryCreate(dir, ws)
	if err == nil {
		t.Fatal("expected error for duplicate create")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want to contain 'already exists'", err.Error())
	}
}

func TestRegistry_Get_Found(t *testing.T) {
	dir := t.TempDir()
	ws := Workspace{Name: "get-me", Branch: "ralph/get-me", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	// Create the workspace directory so Get doesn't report missing
	if err := os.MkdirAll(WorkspacePath(dir, "get-me"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := RegistryGet(dir, "get-me")
	if err != nil {
		t.Fatalf("RegistryGet error: %v", err)
	}
	if got.Name != "get-me" {
		t.Errorf("Name = %q, want %q", got.Name, "get-me")
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := RegistryGet(dir, "nope")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestRegistry_Get_MissingDirectory(t *testing.T) {
	dir := t.TempDir()
	ws := Workspace{Name: "missing-dir", Branch: "ralph/missing-dir", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}
	// Do NOT create the workspace directory

	_, err := RegistryGet(dir, "missing-dir")
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want to contain 'missing'", err.Error())
	}
}

func TestRegistry_Remove(t *testing.T) {
	dir := t.TempDir()
	ws1 := Workspace{Name: "keep", Branch: "ralph/keep", CreatedAt: time.Now()}
	ws2 := Workspace{Name: "remove-me", Branch: "ralph/remove-me", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws1); err != nil {
		t.Fatal(err)
	}
	if err := RegistryCreate(dir, ws2); err != nil {
		t.Fatal(err)
	}

	if err := RegistryRemove(dir, "remove-me"); err != nil {
		t.Fatalf("RegistryRemove error: %v", err)
	}

	list, err := RegistryList(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d entries, want 1", len(list))
	}
	if list[0].Name != "keep" {
		t.Errorf("remaining entry = %q, want %q", list[0].Name, "keep")
	}
}

func TestRegistry_Remove_NotFound(t *testing.T) {
	dir := t.TempDir()
	err := RegistryRemove(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing nonexistent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestRegistry_MissingFile_ReturnsEmptyList(t *testing.T) {
	dir := t.TempDir()
	list, err := RegistryList(dir)
	if err != nil {
		t.Fatalf("RegistryList error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list = %d entries, want 0", len(list))
	}
}

func TestRegistry_CreateCreatesFileOnFirstWrite(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, ".ralph", "state", "workspaces.json")

	// File should not exist yet
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Fatal("registry file should not exist initially")
	}

	ws := Workspace{Name: "first", Branch: "ralph/first", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws); err != nil {
		t.Fatal(err)
	}

	// File should exist now
	if _, err := os.Stat(regPath); err != nil {
		t.Errorf("registry file should exist after first write, got: %v", err)
	}
}

func TestRegistryListWithMissing_DetectsMissingDir(t *testing.T) {
	dir := t.TempDir()
	ws1 := Workspace{Name: "present", Branch: "ralph/present", CreatedAt: time.Now()}
	ws2 := Workspace{Name: "gone", Branch: "ralph/gone", CreatedAt: time.Now()}
	if err := RegistryCreate(dir, ws1); err != nil {
		t.Fatal(err)
	}
	if err := RegistryCreate(dir, ws2); err != nil {
		t.Fatal(err)
	}

	// Create directory only for "present"
	if err := os.MkdirAll(WorkspacePath(dir, "present"), 0755); err != nil {
		t.Fatal(err)
	}

	entries, err := RegistryListWithMissing(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	for _, e := range entries {
		switch e.Name {
		case "present":
			if e.Missing {
				t.Error("present workspace should not be missing")
			}
		case "gone":
			if !e.Missing {
				t.Error("gone workspace should be missing")
			}
		}
	}
}

// --- Workspace JSON ---

func TestWorkspaceJSON_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	ws := Workspace{Name: "test-ws", Branch: "ralph/test-ws", CreatedAt: now}

	if err := WriteWorkspaceJSON(dir, "test-ws", ws); err != nil {
		t.Fatalf("WriteWorkspaceJSON error: %v", err)
	}

	got, err := ReadWorkspaceJSON(dir, "test-ws")
	if err != nil {
		t.Fatalf("ReadWorkspaceJSON error: %v", err)
	}
	if got.Name != ws.Name {
		t.Errorf("Name = %q, want %q", got.Name, ws.Name)
	}
	if got.Branch != ws.Branch {
		t.Errorf("Branch = %q, want %q", got.Branch, ws.Branch)
	}
	if !got.CreatedAt.Equal(ws.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, ws.CreatedAt)
	}
}

func TestWorkspaceJSON_ReadMissing_Error(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadWorkspaceJSON(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing workspace.json")
	}
}

// --- ResolveWorkContext ---

func TestResolveWorkContext_FlagWins(t *testing.T) {
	repo := "/repo"
	wc, err := ResolveWorkContext("flag-ws", "env-ws", "/repo/.ralph/workspaces/cwd-ws/tree", repo)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if wc.Name != "flag-ws" {
		t.Errorf("Name = %q, want %q", wc.Name, "flag-ws")
	}
	if wc.WorkDir != TreePath(repo, "flag-ws") {
		t.Errorf("WorkDir = %q, want %q", wc.WorkDir, TreePath(repo, "flag-ws"))
	}
	if wc.PRDPath != PRDPathForWorkspace(repo, "flag-ws") {
		t.Errorf("PRDPath = %q, want %q", wc.PRDPath, PRDPathForWorkspace(repo, "flag-ws"))
	}
	if wc.ProgressPath != ProgressPathForWorkspace(repo, "flag-ws") {
		t.Errorf("ProgressPath = %q, want %q", wc.ProgressPath, ProgressPathForWorkspace(repo, "flag-ws"))
	}
}

func TestResolveWorkContext_EnvVarSecondPriority(t *testing.T) {
	repo := "/repo"
	wc, err := ResolveWorkContext("", "env-ws", "/some/other/dir", repo)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if wc.Name != "env-ws" {
		t.Errorf("Name = %q, want %q", wc.Name, "env-ws")
	}
}

func TestResolveWorkContext_CwdDetectionThirdPriority(t *testing.T) {
	repo := "/repo"
	wc, err := ResolveWorkContext("", "", "/repo/.ralph/workspaces/cwd-ws/tree/src", repo)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if wc.Name != "cwd-ws" {
		t.Errorf("Name = %q, want %q", wc.Name, "cwd-ws")
	}
}

func TestResolveWorkContext_BaseFallback(t *testing.T) {
	repo := "/repo"
	wc, err := ResolveWorkContext("", "", "/repo", repo)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if wc.Name != "base" {
		t.Errorf("Name = %q, want %q", wc.Name, "base")
	}
	if wc.WorkDir != repo {
		t.Errorf("WorkDir = %q, want %q", wc.WorkDir, repo)
	}
	if wc.PRDPath != filepath.Join(repo, ".ralph", "state", "prd.json") {
		t.Errorf("PRDPath = %q, want default state path", wc.PRDPath)
	}
	wantProgress := filepath.Join(repo, ".ralph", "progress.txt")
	if wc.ProgressPath != wantProgress {
		t.Errorf("ProgressPath = %q, want %q", wc.ProgressPath, wantProgress)
	}
}

func TestResolveWorkContext_InvalidName_Error(t *testing.T) {
	_, err := ResolveWorkContext("bad name!", "", "/dir", "/repo")
	if err == nil {
		t.Fatal("expected error for invalid workspace name")
	}
}
