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
