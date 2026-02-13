package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/shell"
)

// initRepo creates a bare-minimum git repo in dir with one initial commit.
func initRepo(t *testing.T, dir string) *shell.Runner {
	t.Helper()
	r := &shell.Runner{Dir: dir}
	ctx := context.Background()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		if _, err := r.Run(ctx, c[0], c[1:]...); err != nil {
			t.Fatalf("init repo %v: %v", c, err)
		}
	}

	// Create an initial commit so HEAD exists.
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
	return r
}

func TestIsWorktree_MainRepo(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	isWt, err := IsWorktree(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isWt {
		t.Fatal("expected false for main repo, got true")
	}
}

func TestIsWorktree_InsideWorktree(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	// Create a branch and worktree.
	if _, err := r.Run(ctx, "git", "branch", "feature"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(dir, "wt")
	if _, err := r.Run(ctx, "git", "worktree", "add", wtPath, "feature"); err != nil {
		t.Fatal(err)
	}

	wtRunner := &shell.Runner{Dir: wtPath}
	isWt, err := IsWorktree(ctx, wtRunner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isWt {
		t.Fatal("expected true for worktree, got false")
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	branch, err := CurrentBranch(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default branch name varies; just check it's not empty.
	if branch == "" {
		t.Fatal("expected non-empty branch name")
	}
}

func TestIsAncestor(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	// Get initial commit hash.
	initial, err := r.Run(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	initial = strings.TrimSpace(initial)

	// Add another commit.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "second"); err != nil {
		t.Fatal(err)
	}
	head, err := r.Run(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head = strings.TrimSpace(head)

	// initial is ancestor of head.
	ok, err := IsAncestor(ctx, r, initial, head)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected initial to be ancestor of head")
	}

	// head is NOT ancestor of initial.
	ok, err = IsAncestor(ctx, r, head, initial)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected head NOT to be ancestor of initial")
	}
}

func TestFetchBranch(t *testing.T) {
	// Create a "remote" repo and a clone.
	remoteDir := t.TempDir()
	initRepo(t, remoteDir)
	ctx := context.Background()

	cloneDir := t.TempDir()
	cloneRunner := &shell.Runner{Dir: cloneDir}
	// Clone needs a parent-less dir; remove the temp dir and use git clone.
	os.RemoveAll(cloneDir)
	parentRunner := &shell.Runner{Dir: filepath.Dir(cloneDir)}
	if _, err := parentRunner.Run(ctx, "git", "clone", remoteDir, cloneDir); err != nil {
		t.Fatalf("cloning: %v", err)
	}

	// Fetch should succeed for the default branch.
	branch, err := CurrentBranch(ctx, cloneRunner)
	if err != nil {
		t.Fatal(err)
	}
	if err := FetchBranch(ctx, cloneRunner, branch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartRebase_NoConflicts(t *testing.T) {
	// Create a repo with two branches that don't conflict.
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	// Create feature branch.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feature commit"); err != nil {
		t.Fatal(err)
	}

	// Go back to default branch and add non-conflicting commit.
	defaultBranch, _ := r.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	// Actually we're on feature now, go back to initial branch.
	if _, err := r.Run(ctx, "git", "checkout", "-"); err != nil {
		t.Fatal(err)
	}
	defaultBranch, _ = CurrentBranch(ctx, r)
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "base commit"); err != nil {
		t.Fatal(err)
	}

	// Checkout feature and rebase onto default branch.
	if _, err := r.Run(ctx, "git", "checkout", "feature"); err != nil {
		t.Fatal(err)
	}
	result, err := StartRebase(ctx, r, defaultBranch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
}

func TestStartRebase_WithConflicts(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	// Get default branch name.
	defaultBranch, _ := CurrentBranch(ctx, r)

	// Create feature branch with conflicting change.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feature change"); err != nil {
		t.Fatal(err)
	}

	// Go back and add conflicting change on default branch.
	if _, err := r.Run(ctx, "git", "checkout", defaultBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "base change"); err != nil {
		t.Fatal(err)
	}

	// Checkout feature and rebase.
	if _, err := r.Run(ctx, "git", "checkout", "feature"); err != nil {
		t.Fatal(err)
	}
	result, err := StartRebase(ctx, r, defaultBranch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure due to conflicts")
	}
	if !result.HasConflicts {
		t.Fatal("expected conflicts")
	}

	// Verify rebase is in progress.
	inProgress, err := HasRebaseInProgress(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inProgress {
		t.Fatal("expected rebase in progress")
	}

	// Verify conflict files.
	files, err := ConflictFiles(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "README.md" {
		t.Fatalf("expected [README.md], got %v", files)
	}

	// Abort the rebase.
	if err := AbortRebase(ctx, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no longer in progress.
	inProgress, err = HasRebaseInProgress(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inProgress {
		t.Fatal("expected rebase not in progress after abort")
	}
}

func TestContinueRebase_AfterResolvingConflicts(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	defaultBranch, _ := CurrentBranch(ctx, r)

	// Create conflicting branches.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feature"); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(ctx, "git", "checkout", defaultBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(ctx, "git", "checkout", "feature"); err != nil {
		t.Fatal(err)
	}

	// Start rebase — will conflict.
	result, err := StartRebase(ctx, r, defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasConflicts {
		t.Fatal("expected conflicts")
	}

	// Resolve the conflict.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("resolved\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "README.md"); err != nil {
		t.Fatal(err)
	}

	// Continue rebase.
	result, err = ContinueRebase(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success after resolving conflicts")
	}
}

func TestSquashMerge(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	defaultBranch, _ := CurrentBranch(ctx, r)

	// Create feature branch with multiple commits.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feat: add a"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feat: add b"); err != nil {
		t.Fatal(err)
	}

	// Squash merge back to default branch.
	err := SquashMerge(ctx, r, dir, "feature", defaultBranch, "squashed feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should now be on the default branch.
	branch, _ := CurrentBranch(ctx, r)
	if branch != defaultBranch {
		t.Fatalf("expected to be on %s, got %s", defaultBranch, branch)
	}

	// Check the log has the squash commit.
	log, err := r.Run(ctx, "git", "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "squashed feature") {
		t.Fatalf("expected squash commit message, got: %s", log)
	}

	// Verify both files exist.
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("expected %s to exist: %v", f, err)
		}
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving symlinks for %s: %v", path, err)
	}
	return resolved
}

func TestMainRepoPath_FromMainRepo(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	got, err := MainRepoPath(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %s, got %s", dir, got)
	}
}

func TestMainRepoPath_FromWorktree(t *testing.T) {
	dir := realPath(t, t.TempDir())
	r := initRepo(t, dir)
	ctx := context.Background()

	// Create a branch and worktree.
	if _, err := r.Run(ctx, "git", "branch", "feature"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(dir, "wt")
	if _, err := r.Run(ctx, "git", "worktree", "add", wtPath, "feature"); err != nil {
		t.Fatal(err)
	}

	wtRunner := &shell.Runner{Dir: wtPath}
	got, err := MainRepoPath(ctx, wtRunner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %s, got %s", dir, got)
	}
}

func TestSquashMerge_FromWorktree(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	defaultBranch, _ := CurrentBranch(ctx, r)

	// Create feature branch with a commit.
	if _, err := r.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "git", "commit", "-m", "feat: add a"); err != nil {
		t.Fatal(err)
	}

	// Go back to default branch and create a worktree for the feature branch.
	if _, err := r.Run(ctx, "git", "checkout", defaultBranch); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(dir, "worktree-feature")
	if _, err := r.Run(ctx, "git", "worktree", "add", wtPath, "feature"); err != nil {
		t.Fatal(err)
	}

	// Run SquashMerge from the worktree runner, passing the main repo path.
	// This is the scenario that previously failed because the main repo already
	// has defaultBranch checked out.
	wtRunner := &shell.Runner{Dir: wtPath}
	err := SquashMerge(ctx, wtRunner, dir, "feature", defaultBranch, "squashed from worktree")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the squash commit landed on the default branch.
	logOut, err := r.Run(ctx, "git", "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logOut, "squashed from worktree") {
		t.Fatalf("expected squash commit message, got: %s", logOut)
	}
}

func TestHasRebaseInProgress_NoRebase(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	inProgress, err := HasRebaseInProgress(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inProgress {
		t.Fatal("expected no rebase in progress")
	}
}

func TestConflictFiles_NoConflicts(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	files, err := ConflictFiles(ctx, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no conflict files, got %v", files)
	}
}

func TestCopyDotRalph_SkipsRalphYaml(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	// Create .ralph directory with ralph.yaml and other files.
	ralphDir := filepath.Join(repoDir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte("project: test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "progress.txt"), []byte("some progress"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory with a file.
	skillsDir := filepath.Join(ralphDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "test.md"), []byte("# Skill"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyDotRalph(repoDir, worktreeDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ralph.yaml must NOT be copied (would cause config.Discover to resolve
	// wrong Repo.Path inside workspace trees).
	yamlPath := filepath.Join(worktreeDir, ".ralph", "ralph.yaml")
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Fatal("expected ralph.yaml NOT to be copied into worktree")
	}

	// Other files should still be copied.
	progressPath := filepath.Join(worktreeDir, ".ralph", "progress.txt")
	if _, err := os.Stat(progressPath); err != nil {
		t.Fatalf("expected progress.txt to be copied: %v", err)
	}

	skillPath := filepath.Join(worktreeDir, ".ralph", "skills", "test.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected skills/test.md to be copied: %v", err)
	}
}

func TestCopyDotClaude_CopiesDirectory(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	// Create .claude directory with files in the repo.
	claudeDir := filepath.Join(repoDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(`{"key": "value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory with another file.
	commandsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(commandsDir, "test.md")
	if err := os.WriteFile(commandPath, []byte("# Test Command"), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy .claude to worktree.
	if err := CopyDotClaude(repoDir, worktreeDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify files were copied.
	dstSettings := filepath.Join(worktreeDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(dstSettings)
	if err != nil {
		t.Fatalf("expected settings file to exist: %v", err)
	}
	if string(data) != `{"key": "value"}` {
		t.Fatalf("expected settings content to match, got: %s", data)
	}

	dstCommand := filepath.Join(worktreeDir, ".claude", "commands", "test.md")
	data, err = os.ReadFile(dstCommand)
	if err != nil {
		t.Fatalf("expected command file to exist: %v", err)
	}
	if string(data) != "# Test Command" {
		t.Fatalf("expected command content to match, got: %s", data)
	}
}

func TestCopyDotClaude_NoErrorWhenMissing(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	// No .claude directory in repo — should not error.
	if err := CopyDotClaude(repoDir, worktreeDir); err != nil {
		t.Fatalf("unexpected error when .claude does not exist: %v", err)
	}

	// Verify nothing was created.
	claudePath := filepath.Join(worktreeDir, ".claude")
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("expected .claude not to exist in worktree, got: %v", err)
	}
}

func TestPullFFOnly_Success(t *testing.T) {
	// Create a "remote" repo and a clone, then push a new commit to the
	// remote and verify PullFFOnly brings the clone up to date.
	remoteDir := t.TempDir()
	initRepo(t, remoteDir)
	ctx := context.Background()

	cloneDir := t.TempDir()
	os.RemoveAll(cloneDir)
	parentRunner := &shell.Runner{Dir: filepath.Dir(cloneDir)}
	if _, err := parentRunner.Run(ctx, "git", "clone", remoteDir, cloneDir); err != nil {
		t.Fatalf("cloning: %v", err)
	}
	cloneRunner := &shell.Runner{Dir: cloneDir}

	branch, err := CurrentBranch(ctx, cloneRunner)
	if err != nil {
		t.Fatal(err)
	}

	// Add a commit to the remote so the clone is behind.
	remoteRunner := &shell.Runner{Dir: remoteDir}
	if err := os.WriteFile(filepath.Join(remoteDir, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRunner.Run(ctx, "git", "commit", "-m", "remote commit"); err != nil {
		t.Fatal(err)
	}

	// PullFFOnly should succeed and bring the clone up to date.
	if err := PullFFOnly(ctx, cloneRunner, branch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the new file exists in the clone.
	if _, err := os.Stat(filepath.Join(cloneDir, "new.txt")); err != nil {
		t.Fatalf("expected new.txt to exist after pull: %v", err)
	}
}

func TestPullFFOnly_FailsOnDivergedHistory(t *testing.T) {
	// Create a "remote" repo and a clone, then diverge them so ff-only fails.
	remoteDir := t.TempDir()
	initRepo(t, remoteDir)
	ctx := context.Background()

	cloneDir := t.TempDir()
	os.RemoveAll(cloneDir)
	parentRunner := &shell.Runner{Dir: filepath.Dir(cloneDir)}
	if _, err := parentRunner.Run(ctx, "git", "clone", remoteDir, cloneDir); err != nil {
		t.Fatalf("cloning: %v", err)
	}
	cloneRunner := &shell.Runner{Dir: cloneDir}

	branch, err := CurrentBranch(ctx, cloneRunner)
	if err != nil {
		t.Fatal(err)
	}

	// Add a commit to the remote.
	remoteRunner := &shell.Runner{Dir: remoteDir}
	if err := os.WriteFile(filepath.Join(remoteDir, "remote.txt"), []byte("remote"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRunner.Run(ctx, "git", "commit", "-m", "remote diverge"); err != nil {
		t.Fatal(err)
	}

	// Add a divergent commit to the clone.
	if err := os.WriteFile(filepath.Join(cloneDir, "local.txt"), []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "commit", "-m", "local diverge"); err != nil {
		t.Fatal(err)
	}

	// PullFFOnly should return an error.
	err = PullFFOnly(ctx, cloneRunner, branch)
	if err == nil {
		t.Fatal("expected error for diverged history, got nil")
	}
}

func TestForcePushBranch_Success(t *testing.T) {
	// Create a "remote" bare repo and a clone, then force push a rebased branch.
	remoteDir := t.TempDir()
	ctx := context.Background()

	// Init a bare repo to act as remote.
	remoteRunner := &shell.Runner{Dir: remoteDir}
	if _, err := remoteRunner.Run(ctx, "git", "init", "--bare"); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}

	// Clone it.
	cloneDir := t.TempDir()
	os.RemoveAll(cloneDir)
	parentRunner := &shell.Runner{Dir: filepath.Dir(cloneDir)}
	if _, err := parentRunner.Run(ctx, "git", "clone", remoteDir, cloneDir); err != nil {
		t.Fatalf("cloning: %v", err)
	}
	cloneRunner := &shell.Runner{Dir: cloneDir}
	if _, err := cloneRunner.Run(ctx, "git", "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit and push.
	if err := os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	branch, err := CurrentBranch(ctx, cloneRunner)
	if err != nil {
		t.Fatal(err)
	}
	if err := PushBranch(ctx, cloneRunner, branch); err != nil {
		t.Fatal(err)
	}

	// Create a feature branch and push it.
	if _, err := cloneRunner.Run(ctx, "git", "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "commit", "-m", "feature commit"); err != nil {
		t.Fatal(err)
	}
	if err := PushBranch(ctx, cloneRunner, "feature"); err != nil {
		t.Fatal(err)
	}

	// Amend the commit (simulating a rebase that rewrites history).
	if err := os.WriteFile(filepath.Join(cloneDir, "a.txt"), []byte("amended"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneRunner.Run(ctx, "git", "commit", "--amend", "-m", "feature amended"); err != nil {
		t.Fatal(err)
	}

	// Normal push would fail here. ForcePushBranch should succeed.
	if err := ForcePushBranch(ctx, cloneRunner, "feature"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForcePushBranch_ErrorWrapping(t *testing.T) {
	// Pushing to a non-existent remote should return a wrapped error.
	dir := t.TempDir()
	r := initRepo(t, dir)
	ctx := context.Background()

	err := ForcePushBranch(ctx, r, "nonexistent-branch")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "force pushing branch nonexistent-branch") {
		t.Fatalf("expected wrapped error with branch name, got: %v", err)
	}
}

func TestCopyGlobPatterns_LiteralPath(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create a literal file to copy.
	scriptsDir := filepath.Join(srcDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	setupPath := filepath.Join(scriptsDir, "setup.sh")
	if err := os.WriteFile(setupPath, []byte("#!/bin/bash\necho setup"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"scripts/setup.sh"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was copied with correct path structure.
	dstPath := filepath.Join(dstDir, "scripts", "setup.sh")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if string(data) != "#!/bin/bash\necho setup" {
		t.Fatalf("expected content to match, got: %s", data)
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_SingleLevelWildcard(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create configs/*.json files.
	configsDir := filepath.Join(srcDir, "configs")
	if err := os.MkdirAll(configsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configsDir, "dev.json"), []byte(`{"env":"dev"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configsDir, "prod.json"), []byte(`{"env":"prod"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Also create a non-matching file.
	if err := os.WriteFile(filepath.Join(configsDir, "readme.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"configs/*.json"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both JSON files were copied.
	for _, name := range []string{"dev.json", "prod.json"} {
		dstPath := filepath.Join(dstDir, "configs", name)
		if _, err := os.Stat(dstPath); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	// Verify non-matching file was NOT copied.
	txtPath := filepath.Join(dstDir, "configs", "readme.txt")
	if _, err := os.Stat(txtPath); !os.IsNotExist(err) {
		t.Fatalf("expected readme.txt NOT to be copied")
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_RecursiveWildcard(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create fixtures/**/*.txt files at various depths.
	fixturesDir := filepath.Join(srcDir, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(fixturesDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	deepDir := filepath.Join(subDir, "deep")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "c.txt"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}

	// Also create a non-matching file.
	if err := os.WriteFile(filepath.Join(fixturesDir, "ignore.md"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"fixtures/**/*.txt"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all three txt files were copied with correct paths.
	expected := []string{
		filepath.Join("fixtures", "a.txt"),
		filepath.Join("fixtures", "sub", "b.txt"),
		filepath.Join("fixtures", "sub", "deep", "c.txt"),
	}
	for _, relPath := range expected {
		dstPath := filepath.Join(dstDir, relPath)
		if _, err := os.Stat(dstPath); err != nil {
			t.Fatalf("expected %s to exist: %v", relPath, err)
		}
	}

	// Verify non-matching file was NOT copied.
	mdPath := filepath.Join(dstDir, "fixtures", "ignore.md")
	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Fatalf("expected ignore.md NOT to be copied")
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_DirectoryPath(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create a directory with multiple files.
	dataDir := filepath.Join(srcDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "file1.txt"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "file2.txt"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}

	subDataDir := filepath.Join(dataDir, "sub")
	if err := os.MkdirAll(subDataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDataDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	// Specify directory path (not a glob) — should copy recursively.
	patterns := []string{"data"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all files were copied with correct structure.
	expected := []string{
		filepath.Join("data", "file1.txt"),
		filepath.Join("data", "file2.txt"),
		filepath.Join("data", "sub", "nested.txt"),
	}
	for _, relPath := range expected {
		dstPath := filepath.Join(dstDir, relPath)
		if _, err := os.Stat(dstPath); err != nil {
			t.Fatalf("expected %s to exist: %v", relPath, err)
		}
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_NoMatchesWarns(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"nonexistent/*.xyz"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have warned about no matches.
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got: %v", warnings)
	}
	if !strings.Contains(warnings[0], "nonexistent/*.xyz") {
		t.Fatalf("expected warning to mention pattern, got: %s", warnings[0])
	}
}

func TestCopyGlobPatterns_EmptyPatterns(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	// Empty patterns list should do nothing and not error.
	if err := CopyGlobPatterns(srcDir, dstDir, nil, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_DirectoryWithSymlinks(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create a directory tree that contains a symlink to another directory,
	// mimicking Elixir deps/ or similar package manager layouts.
	depsDir := filepath.Join(srcDir, "deps", "mypkg", "_build")
	if err := os.MkdirAll(depsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "deps", "mypkg", "mix.exs"), []byte("defmodule Mix"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a target directory that the symlink will point to.
	symlinkTarget := t.TempDir()
	if err := os.WriteFile(filepath.Join(symlinkTarget, "plugin.beam"), []byte("beam"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the tree pointing to the external directory.
	if err := os.Symlink(symlinkTarget, filepath.Join(depsDir, "plugins")); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"deps"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the regular file was copied.
	mixPath := filepath.Join(dstDir, "deps", "mypkg", "mix.exs")
	if _, err := os.Stat(mixPath); err != nil {
		t.Fatalf("expected %s to exist: %v", mixPath, err)
	}

	// Verify the symlinked directory's contents were copied.
	pluginPath := filepath.Join(dstDir, "deps", "mypkg", "_build", "plugins", "plugin.beam")
	if _, err := os.Stat(pluginPath); err != nil {
		t.Fatalf("expected %s to exist: %v", pluginPath, err)
	}

	if len(warnings) > 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestCopyGlobPatterns_PreservesRelativePath(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create deeply nested file.
	deepPath := filepath.Join(srcDir, "a", "b", "c", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deepPath, []byte("deep content"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	patterns := []string{"a/b/c/file.txt"}
	if err := CopyGlobPatterns(srcDir, dstDir, patterns, warn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify exact path structure is preserved.
	dstPath := filepath.Join(dstDir, "a", "b", "c", "file.txt")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("expected file to exist at exact path: %v", err)
	}
	if string(data) != "deep content" {
		t.Fatalf("expected content to match, got: %s", data)
	}
}
