package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/shell"
)

// initRepo creates a bare-minimum git repo in dir with one initial commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	r := &shell.Runner{Dir: dir}
	ctx := context.Background()

	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		if _, err := r.Run(ctx, c[0], c[1:]...); err != nil {
			t.Fatalf("init repo %v: %v", c, err)
		}
	}

	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
}

func commitCount(t *testing.T, dir string) int {
	t.Helper()
	r := &shell.Runner{Dir: dir}
	out, err := r.Run(context.Background(), "git", "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("counting commits: %v", err)
	}
	var n int
	if _, err := strings.NewReader(strings.TrimSpace(out)).Read(nil); err != nil {
		// ignore
	}
	for _, c := range strings.TrimSpace(out) {
		n = n*10 + int(c-'0')
	}
	return n
}

func lastCommitMessage(t *testing.T, dir string) string {
	t.Helper()
	r := &shell.Runner{Dir: dir}
	out, err := r.Run(context.Background(), "git", "log", "-1", "--format=%s")
	if err != nil {
		t.Fatalf("reading last commit: %v", err)
	}
	return strings.TrimSpace(out)
}

// --- Tests ---

func TestRunPreCommit_EmptyConfig_IsNoop(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	runner := New(config.HooksConfig{}, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// No new commits should exist beyond the initial one.
	if n := commitCount(t, dir); n != 1 {
		t.Errorf("commit count = %d, want 1", n)
	}
}

func TestRunPostCommit_EmptyConfig_IsNoop(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	runner := New(config.HooksConfig{}, nil)
	err := runner.RunPostCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRunPrePR_EmptyConfig_IsNoop(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	runner := New(config.HooksConfig{}, nil)
	err := runner.RunPrePR(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRunPreCommit_NilConfig_IsNoop(t *testing.T) {
	runner := New(config.HooksConfig{}, nil)
	err := runner.RunPreCommit(context.Background(), t.TempDir())
	// No git repo needed - should bail early with no hooks.
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRunPreCommit_HookProducesFileChanges_AutoCommits(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{"echo generated > generated.txt"},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the file was created.
	data, err := os.ReadFile(filepath.Join(dir, "generated.txt"))
	if err != nil {
		t.Fatalf("expected generated.txt to exist: %v", err)
	}
	if !strings.Contains(string(data), "generated") {
		t.Errorf("file content = %q, want to contain 'generated'", string(data))
	}

	// Verify auto-commit happened (initial + auto-commit = 2).
	if n := commitCount(t, dir); n != 2 {
		t.Errorf("commit count = %d, want 2", n)
	}

	// Verify commit message contains the hook command.
	msg := lastCommitMessage(t, dir)
	if !strings.Contains(msg, "echo generated > generated.txt") {
		t.Errorf("commit message = %q, want to contain hook command", msg)
	}
}

func TestRunPostCommit_HookProducesFileChanges_AutoCommits(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PostCommit: []string{"echo post > post.txt"},
	}
	runner := New(cfg, nil)
	err := runner.RunPostCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if n := commitCount(t, dir); n != 2 {
		t.Errorf("commit count = %d, want 2", n)
	}
}

func TestRunPrePR_HookProducesFileChanges_AutoCommits(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PrePR: []string{"echo pr > pr.txt"},
	}
	runner := New(cfg, nil)
	err := runner.RunPrePR(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if n := commitCount(t, dir); n != 2 {
		t.Errorf("commit count = %d, want 2", n)
	}
}

func TestRunPreCommit_HookNoChanges_NoAutoCommit(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{"echo hello"},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// No new commits — echo to stdout doesn't modify files.
	if n := commitCount(t, dir); n != 1 {
		t.Errorf("commit count = %d, want 1", n)
	}
}

func TestRunPreCommit_HookFails_NonFatal(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{"exit 1"},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error (non-fatal), got: %v", err)
	}
}

func TestRunPreCommit_HookFails_SubsequentHooksStillRun(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{
			"exit 1",
			"echo second > second.txt",
		},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// The second hook should have run and created the file.
	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatalf("expected second.txt to exist (second hook should run after first fails): %v", err)
	}

	// Auto-commit for second hook's changes.
	if n := commitCount(t, dir); n != 2 {
		t.Errorf("commit count = %d, want 2", n)
	}
}

func TestRunPreCommit_MultipleHooks_ExecuteSequentially(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{
			"echo first > first.txt",
			"echo second > second.txt",
		},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Both files should exist.
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); err != nil {
		t.Fatalf("expected first.txt to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatalf("expected second.txt to exist: %v", err)
	}

	// Each hook that produces changes should get its own auto-commit.
	// initial + 2 auto-commits = 3.
	if n := commitCount(t, dir); n != 3 {
		t.Errorf("commit count = %d, want 3", n)
	}
}

func TestRunPreCommit_UsesGitEnvVars(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := config.HooksConfig{
		PreCommit: []string{"echo env > env.txt"},
	}
	env := []string{
		"GIT_AUTHOR_NAME=Hook Bot",
		"GIT_AUTHOR_EMAIL=hookbot@test.com",
		"GIT_COMMITTER_NAME=Hook Bot",
		"GIT_COMMITTER_EMAIL=hookbot@test.com",
	}
	runner := New(cfg, env)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the auto-commit uses the env vars.
	r := &shell.Runner{Dir: dir}
	out, err := r.Run(context.Background(), "git", "log", "-1", "--format=%an <%ae>")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	got := strings.TrimSpace(out)
	want := "Hook Bot <hookbot@test.com>"
	if got != want {
		t.Errorf("author = %q, want %q", got, want)
	}
}

func TestRunPreCommit_ExecutesViaShDashC(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// Use shell features (pipe) to verify sh -c execution.
	cfg := config.HooksConfig{
		PreCommit: []string{"echo hello | tr h H > piped.txt"},
	}
	runner := New(cfg, nil)
	err := runner.RunPreCommit(context.Background(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "piped.txt"))
	if err != nil {
		t.Fatalf("expected piped.txt to exist: %v", err)
	}
	if !strings.Contains(string(data), "Hello") {
		t.Errorf("piped.txt content = %q, want to contain 'Hello'", string(data))
	}
}
