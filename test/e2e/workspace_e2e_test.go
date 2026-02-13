package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ralphBin is the path to the built ralph binary, set by TestMain.
var ralphBin string

// cmdReport captures a single command invocation.
type cmdReport struct {
	Command  string `json:"command"`
	Args     string `json:"args"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func TestMain(m *testing.M) {
	// Build the ralph binary into a temp directory.
	tmp, err := os.MkdirTemp("", "ralph-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	ralphBin = filepath.Join(tmp, "ralph")

	// Find the module root (two levels up from test/e2e/).
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get cwd: %v\n", err)
		os.Exit(1)
	}
	moduleRoot := filepath.Join(wd, "..", "..")

	cmd := exec.Command("go", "build", "-o", ralphBin, "./cmd/ralph")
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build ralph: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// runRalph executes the ralph binary with the given args and env, and returns
// a cmdReport. The command runs in the specified directory.
func runRalph(t *testing.T, dir string, env []string, args ...string) cmdReport {
	t.Helper()
	return runRalphWithStdin(t, dir, env, "", args...)
}

// runRalphWithStdin executes the ralph binary with the given stdin input.
func runRalphWithStdin(t *testing.T, dir string, env []string, stdin string, args ...string) cmdReport {
	t.Helper()

	cmd := exec.Command(ralphBin, args...)
	cmd.Dir = dir
	// Start with a clean environment plus essentials, then add overrides.
	baseEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	}
	cmd.Env = append(baseEnv, env...)

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return cmdReport{
		Command:  "ralph",
		Args:     strings.Join(args, " "),
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
}

// setupTestRepo copies fixtures to a temp dir, initializes a git repo with
// a bare remote, and returns the repo path.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "project")

	// Copy fixture files.
	fixtureDir := filepath.Join("..", "..", "test", "fixtures", "sample-project")
	if err := copyDir(fixtureDir, repoDir); err != nil {
		t.Fatalf("copying fixtures: %v", err)
	}

	// Initialize git repo.
	gitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitCmd("init", "-b", "main")
	gitCmd("add", "-A")
	gitCmd("commit", "-m", "initial commit")

	// Create bare remote.
	bareDir := filepath.Join(tmpDir, "remote.git")
	cmd := exec.Command("git", "init", "--bare", bareDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("creating bare remote: %v\n%s", err, out)
	}

	gitCmd("remote", "add", "origin", bareDir)
	gitCmd("push", "-u", "origin", "main")

	return repoDir
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// realPath resolves symlinks (needed for macOS /private/var temp dirs).
func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("realpath %s: %v", path, err)
	}
	return real
}

// --- Test: ralph init ---

func TestE2E_Init(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	// Provide stdin: "1" for git tracking choice, "n" for LLM analysis.
	r := runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")
	if r.ExitCode != 0 {
		t.Fatalf("ralph init failed: exit %d\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Verify .ralph/workspaces/ exists.
	workspacesDir := filepath.Join(repoDir, ".ralph", "workspaces")
	if _, err := os.Stat(workspacesDir); os.IsNotExist(err) {
		t.Fatal("expected .ralph/workspaces/ to be created")
	}

	// Verify workspaces.json exists.
	registryPath := filepath.Join(repoDir, ".ralph", "state", "workspaces.json")
	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("expected workspaces.json to exist: %v", err)
	}

	// Verify it's a valid JSON array.
	var registry []any
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("workspaces.json is not valid JSON: %v", err)
	}
	if len(registry) != 0 {
		t.Fatalf("expected empty registry, got %d entries", len(registry))
	}
}

// --- Test: ralph shell-init ---

func TestE2E_ShellInit(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Test bash output.
	r := runRalph(t, repoDir, []string{"SHELL=/bin/bash"}, "shell-init")
	if r.ExitCode != 0 {
		t.Fatalf("shell-init (bash) failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	if !strings.Contains(r.Stdout, "ralph()") {
		t.Error("expected bash output to contain ralph() function declaration")
	}
	if !strings.Contains(r.Stdout, "RALPH_SHELL_INIT") {
		t.Error("expected bash output to contain RALPH_SHELL_INIT")
	}

	// Validate bash syntax.
	bashCheck := exec.Command("bash", "-n")
	bashCheck.Stdin = strings.NewReader(r.Stdout)
	if out, err := bashCheck.CombinedOutput(); err != nil {
		t.Errorf("bash -n syntax check failed: %v\n%s", err, out)
	}

	// Test zsh output.
	rZsh := runRalph(t, repoDir, []string{"SHELL=/bin/zsh"}, "shell-init")
	if rZsh.ExitCode != 0 {
		t.Fatalf("shell-init (zsh) failed: exit %d\nstderr: %s", rZsh.ExitCode, rZsh.Stderr)
	}

	// Validate zsh syntax.
	zshCheck := exec.Command("zsh", "-n")
	zshCheck.Stdin = strings.NewReader(rZsh.Stdout)
	if out, err := zshCheck.CombinedOutput(); err != nil {
		t.Errorf("zsh -n syntax check failed: %v\n%s", err, out)
	}
}

// --- Test: workspace lifecycle ---

func TestE2E_WorkspaceLifecycle(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	// Initialize ralph.
	r := runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")
	if r.ExitCode != 0 {
		t.Fatalf("init failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	env := []string{"RALPH_SHELL_INIT=1"}

	// --- workspaces new test-feature ---
	r = runRalph(t, repoDir, env, "workspaces", "new", "test-feature")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces new test-feature failed: exit %d\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Verify stdout contains tree/ path.
	treePath := strings.TrimSpace(r.Stdout)
	if !strings.Contains(treePath, "tree") {
		t.Errorf("expected stdout to contain tree/ path, got: %s", treePath)
	}
	if _, err := os.Stat(treePath); os.IsNotExist(err) {
		t.Fatalf("tree/ path does not exist: %s", treePath)
	}

	// Verify workspace.json exists.
	wsDir := filepath.Dir(treePath) // .ralph/workspaces/test-feature/
	wsJSON := filepath.Join(wsDir, "workspace.json")
	if _, err := os.Stat(wsJSON); os.IsNotExist(err) {
		t.Fatal("expected workspace.json to exist")
	}

	// Verify tree/ has .ralph/ copied (but no .ralph/state/).
	treeRalphDir := filepath.Join(treePath, ".ralph")
	if _, err := os.Stat(treeRalphDir); os.IsNotExist(err) {
		t.Fatal("expected .ralph/ to be copied to tree/")
	}
	treeStateDir := filepath.Join(treePath, ".ralph", "state")
	if _, err := os.Stat(treeStateDir); err == nil {
		t.Fatal("expected tree/.ralph/state/ NOT to exist")
	}

	// Verify prd.json not yet created at workspace level.
	wsPRD := filepath.Join(wsDir, "prd.json")
	if _, err := os.Stat(wsPRD); err == nil {
		t.Fatal("expected prd.json NOT to exist at workspace level (not created by new)")
	}

	// --- workspaces list ---
	r = runRalph(t, repoDir, env, "workspaces", "list")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces list failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "test-feature") {
		t.Error("expected list to include test-feature")
	}
	if !strings.Contains(r.Stdout, "base") {
		t.Error("expected list to include base")
	}

	// --- create second workspace ---
	r = runRalph(t, repoDir, env, "workspaces", "new", "test-bugfix")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces new test-bugfix failed: exit %d\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Verify list shows both.
	r = runRalph(t, repoDir, env, "workspaces", "list")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces list failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "test-feature") {
		t.Error("expected list to include test-feature")
	}
	if !strings.Contains(r.Stdout, "test-bugfix") {
		t.Error("expected list to include test-bugfix")
	}

	// --- workspaces switch ---
	r = runRalph(t, repoDir, env, "workspaces", "switch", "test-feature")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces switch test-feature failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	switchPath := strings.TrimSpace(r.Stdout)
	expectedFeaturePath := filepath.Join(repoDir, ".ralph", "workspaces", "test-feature", "tree")
	if switchPath != expectedFeaturePath {
		t.Errorf("switch to test-feature: expected %s, got %s", expectedFeaturePath, switchPath)
	}

	// Switch to base.
	r = runRalph(t, repoDir, env, "workspaces", "switch", "base")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces switch base failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	basePath := strings.TrimSpace(r.Stdout)
	if basePath != repoDir {
		t.Errorf("switch to base: expected %s, got %s", repoDir, basePath)
	}

	// --- workspaces remove ---
	r = runRalph(t, repoDir, env, "workspaces", "remove", "test-bugfix")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces remove test-bugfix failed: exit %d\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Verify directory removed.
	bugfixDir := filepath.Join(repoDir, ".ralph", "workspaces", "test-bugfix")
	if _, err := os.Stat(bugfixDir); !os.IsNotExist(err) {
		t.Fatal("expected test-bugfix workspace directory to be removed")
	}

	// Verify registry cleaned (list should not show test-bugfix).
	r = runRalph(t, repoDir, env, "workspaces", "list")
	if strings.Contains(r.Stdout, "test-bugfix") {
		t.Error("expected test-bugfix to be removed from list")
	}
	if !strings.Contains(r.Stdout, "test-feature") {
		t.Error("expected test-feature to still be in list")
	}

	// Verify branch deleted.
	branchCheck := exec.Command("git", "branch", "--list", "ralph/test-bugfix")
	branchCheck.Dir = repoDir
	branchOut, _ := branchCheck.Output()
	if strings.Contains(string(branchOut), "ralph/test-bugfix") {
		t.Error("expected ralph/test-bugfix branch to be deleted")
	}
}

// --- Test: ralph status ---

func TestE2E_Status(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	// Initialize ralph.
	runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")

	env := []string{"RALPH_SHELL_INIT=1"}

	// Create a workspace.
	r := runRalph(t, repoDir, env, "workspaces", "new", "status-test")
	if r.ExitCode != 0 {
		t.Fatalf("workspaces new failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	treePath := strings.TrimSpace(r.Stdout)
	wsDir := filepath.Dir(treePath)

	// Write a PRD at workspace level.
	prdContent := `{
  "project": "test",
  "branchName": "ralph/status-test",
  "description": "Test PRD for status",
  "userStories": [
    {"id":"US-001","title":"Story A","description":"As a dev","acceptanceCriteria":["Works"],"priority":1,"passes":true,"notes":""},
    {"id":"US-002","title":"Story B","description":"As a dev","acceptanceCriteria":["Works"],"priority":2,"passes":false,"notes":""}
  ],
  "integrationTests": [
    {"id":"IT-001","description":"Test 1","steps":["step 1"],"passes":false,"failure":"","notes":""}
  ]
}`
	if err := os.WriteFile(filepath.Join(wsDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatalf("writing PRD: %v", err)
	}

	// Run status from workspace context.
	wsEnv := append(env, "RALPH_WORKSPACE=status-test")
	r = runRalph(t, repoDir, wsEnv, "status")
	if r.ExitCode != 0 {
		t.Fatalf("status failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	// Status full output goes to stdout.
	if !strings.Contains(r.Stdout, "status-test") {
		t.Error("expected status output to contain workspace name")
	}

	// Run status --short.
	r = runRalph(t, repoDir, wsEnv, "status", "--short")
	if r.ExitCode != 0 {
		t.Fatalf("status --short failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	shortOut := strings.TrimSpace(r.Stdout)
	if !strings.Contains(shortOut, "status-test") {
		t.Errorf("expected short status to contain workspace name, got: %s", shortOut)
	}
	if !strings.Contains(shortOut, "1/2") {
		t.Errorf("expected short status to contain 1/2, got: %s", shortOut)
	}
}

// --- Test: error cases ---

func TestE2E_ErrorCases(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")

	env := []string{"RALPH_SHELL_INIT=1"}

	// Create initial workspace.
	r := runRalph(t, repoDir, env, "workspaces", "new", "existing-ws")
	if r.ExitCode != 0 {
		t.Fatalf("initial workspace creation failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	// Duplicate name error.
	r = runRalph(t, repoDir, env, "workspaces", "new", "existing-ws")
	if r.ExitCode == 0 {
		t.Fatal("expected duplicate workspace creation to fail")
	}
	if !strings.Contains(r.Stderr, "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", r.Stderr)
	}

	// Invalid name 'has spaces'.
	r = runRalph(t, repoDir, env, "workspaces", "new", "has spaces")
	if r.ExitCode == 0 {
		t.Fatal("expected invalid name to fail")
	}

	// Switch to nonexistent workspace.
	r = runRalph(t, repoDir, env, "workspaces", "switch", "nonexistent")
	if r.ExitCode == 0 {
		t.Fatal("expected switch to nonexistent workspace to fail")
	}
	if !strings.Contains(r.Stderr, "not found") {
		t.Errorf("expected 'not found' error, got: %s", r.Stderr)
	}

	// Remove nonexistent workspace.
	r = runRalph(t, repoDir, env, "workspaces", "remove", "nonexistent")
	if r.ExitCode == 0 {
		t.Fatal("expected remove nonexistent workspace to fail")
	}
	if !strings.Contains(r.Stderr, "not found") {
		t.Errorf("expected 'not found' error, got: %s", r.Stderr)
	}
}

// --- Test: missing directory detection ---

func TestE2E_MissingDirectory(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")
	env := []string{"RALPH_SHELL_INIT=1"}

	// Create a workspace.
	r := runRalph(t, repoDir, env, "workspaces", "new", "missing-dir-test")
	if r.ExitCode != 0 {
		t.Fatalf("workspace creation failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	// Manually delete the workspace directory (but leave registry entry).
	wsDir := filepath.Join(repoDir, ".ralph", "workspaces", "missing-dir-test")
	// First remove the git worktree properly.
	gitCmd := exec.Command("git", "worktree", "remove", "--force", filepath.Join(wsDir, "tree"))
	gitCmd.Dir = repoDir
	gitCmd.Run() // Best-effort.
	os.RemoveAll(wsDir)

	// List should show [missing] marker.
	r = runRalph(t, repoDir, env, "workspaces", "list")
	if r.ExitCode != 0 {
		t.Fatalf("list failed: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "[missing]") {
		t.Errorf("expected [missing] marker in list output, got:\n%s", r.Stdout)
	}
}

// --- Test: RALPH_SHELL_INIT detection ---

func TestE2E_ShellInitDetection(t *testing.T) {
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init")

	// Without RALPH_SHELL_INIT, workspaces new should fail.
	// Use a clean env without RALPH_SHELL_INIT.
	r := runRalph(t, repoDir, nil, "workspaces", "new", "no-shell-init")
	if r.ExitCode == 0 {
		t.Fatal("expected error when RALPH_SHELL_INIT not set")
	}
	if !strings.Contains(r.Stderr, "Shell integration required") {
		t.Errorf("expected 'Shell integration required' error, got: %s", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "eval") {
		t.Errorf("expected eval command in error, got: %s", r.Stderr)
	}

	// With RALPH_SHELL_INIT set, it should work.
	r = runRalph(t, repoDir, []string{"RALPH_SHELL_INIT=1"}, "workspaces", "new", "no-shell-init")
	if r.ExitCode != 0 {
		t.Fatalf("expected success with RALPH_SHELL_INIT=1, got exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
}

// --- Test: comprehensive agent evaluation ---

func TestE2E_AgentEvaluation(t *testing.T) {
	// Check if claude CLI is available.
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not available, skipping agent evaluation")
	}

	// Run a full lifecycle collecting reports.
	repoDir := setupTestRepo(t)
	repoDir = realPath(t, repoDir)

	var reports []cmdReport
	collect := func(r cmdReport) cmdReport {
		reports = append(reports, r)
		return r
	}

	env := []string{"RALPH_SHELL_INIT=1"}

	// init
	collect(runRalphWithStdin(t, repoDir, nil, "1\nn\n", "init"))

	// shell-init
	collect(runRalph(t, repoDir, []string{"SHELL=/bin/bash"}, "shell-init"))

	// workspaces new
	r := collect(runRalph(t, repoDir, env, "workspaces", "new", "eval-feature"))

	// workspaces list
	collect(runRalph(t, repoDir, env, "workspaces", "list"))

	// second workspace
	collect(runRalph(t, repoDir, env, "workspaces", "new", "eval-bugfix"))
	collect(runRalph(t, repoDir, env, "workspaces", "list"))

	// switch
	collect(runRalph(t, repoDir, env, "workspaces", "switch", "eval-feature"))
	collect(runRalph(t, repoDir, env, "workspaces", "switch", "base"))

	// status
	wsDir := filepath.Dir(strings.TrimSpace(r.Stdout))
	prdContent := `{"project":"test","branchName":"ralph/eval-feature","description":"Eval PRD","userStories":[{"id":"US-001","title":"Story","description":"As a dev","acceptanceCriteria":["Works"],"priority":1,"passes":true,"notes":""}]}`
	os.WriteFile(filepath.Join(wsDir, "prd.json"), []byte(prdContent), 0644)

	wsEnv := append(env, "RALPH_WORKSPACE=eval-feature")
	collect(runRalph(t, repoDir, wsEnv, "status"))
	collect(runRalph(t, repoDir, wsEnv, "status", "--short"))

	// remove
	collect(runRalph(t, repoDir, env, "workspaces", "remove", "eval-bugfix"))
	collect(runRalph(t, repoDir, env, "workspaces", "list"))

	// error cases
	collect(runRalph(t, repoDir, env, "workspaces", "new", "eval-feature"))   // duplicate
	collect(runRalph(t, repoDir, env, "workspaces", "new", "has spaces"))     // invalid name
	collect(runRalph(t, repoDir, env, "workspaces", "switch", "nonexistent")) // switch nonexistent
	collect(runRalph(t, repoDir, env, "workspaces", "remove", "nonexistent")) // remove nonexistent

	// shell-init detection
	collect(runRalph(t, repoDir, nil, "workspaces", "new", "test-no-shell")) // no RALPH_SHELL_INIT

	// Build the report.
	reportJSON, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		t.Fatalf("marshaling reports: %v", err)
	}

	prompt := fmt.Sprintf(`You are evaluating the UX quality of a CLI tool called "ralph" for workspace management.

Below is a structured report of every command invocation made during end-to-end testing. Each entry contains the command, arguments, exit code, stdout, and stderr.

Evaluate the tool on these dimensions (rate each 1-5):

1. **Clarity**: Are error messages clear? Is output easy to understand? Do success messages confirm what happened?
2. **Consistency**: Do similar commands follow similar patterns? Are error formats consistent? Is the stdout/stderr split consistent (machine output on stdout, human output on stderr)?
3. **Discoverability**: Do error messages suggest next steps? Are hints provided? Can a user figure out what to do next?
4. **Edge case handling**: Are invalid inputs handled gracefully? Are missing resources detected? Are error messages specific to the problem?
5. **Completeness**: Does the tool cover the full workspace lifecycle? Are all operations represented?

IMPORTANT: You MUST respond ONLY in the following exact format. No other text before or after.

Clarity: N
Consistency: N
Discoverability: N
Edge case handling: N
Completeness: N
Feedback: <one paragraph of specific feedback for any dimension rated below 5>

Command Report:
%s`, string(reportJSON))

	cmd := exec.Command("claude",
		"--print",
		"--dangerously-skip-permissions",
		"--max-turns", "1",
	)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("claude stderr: %s", stderr.String())
		t.Fatalf("claude evaluation failed: %v", err)
	}

	response := stdout.String()
	t.Logf("Agent evaluation response:\n%s", response)

	// Parse ratings.
	dimensions := []string{"Clarity", "Consistency", "Discoverability", "Edge case handling", "Completeness"}
	ratings := make(map[string]int)

	for _, dim := range dimensions {
		pattern := regexp.MustCompile(fmt.Sprintf(`(?i)%s:\s*(\d)`, regexp.QuoteMeta(dim)))
		matches := pattern.FindStringSubmatch(response)
		if len(matches) < 2 {
			t.Errorf("could not parse rating for %s from response", dim)
			continue
		}
		rating, err := strconv.Atoi(matches[1])
		if err != nil {
			t.Errorf("invalid rating for %s: %s", dim, matches[1])
			continue
		}
		ratings[dim] = rating
	}

	// Assert all ratings >= 4.
	for dim, rating := range ratings {
		if rating < 4 {
			t.Errorf("%s rated %d/5 (minimum required: 4)", dim, rating)
		}
		if rating < 5 {
			t.Logf("FEEDBACK: %s rated %d/5 — review agent feedback for improvement areas", dim, rating)
		}
	}

	// Log feedback section if present.
	feedbackPattern := regexp.MustCompile(`(?is)Feedback:\s*(.+)`)
	if matches := feedbackPattern.FindStringSubmatch(response); len(matches) >= 2 {
		t.Logf("Agent feedback: %s", strings.TrimSpace(matches[1]))
	}
}
