package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

// recordingHandler captures events for test assertions.
type recordingHandler struct {
	events []events.Event
}

func (h *recordingHandler) Handle(e events.Event) {
	h.events = append(h.events, e)
}

// mockGitClean sets up the git check to always return clean (no uncommitted changes).
// Returns a cleanup function that restores the original.
func mockGitClean() func() {
	origGitFn := gitHasUncommittedChangesFn
	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		return false, nil // Always clean
	}
	return func() { gitHasUncommittedChangesFn = origGitFn }
}

func TestRun_ExitsWhenAllStoriesPass(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var invocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	if invocations != 0 {
		t.Errorf("expected 0 invocations when all stories pass, got %d", invocations)
	}
}

func TestEnsureProgressFile_CreatesFileIfNotExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "progress.txt")

	ensureProgressFile(path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected progress file to be created")
	}
}

func TestEnsureProgressFile_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.txt")

	existingContent := "existing content"
	os.WriteFile(path, []byte(existingContent), 0644)

	ensureProgressFile(path)

	content, _ := os.ReadFile(path)
	if string(content) != existingContent {
		t.Errorf("expected existing content to be preserved, got: %s", content)
	}
}

func TestRun_VerboseFlagPassedToInvoke(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedVerbose bool
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		capturedVerbose = opts.verbose
		// Mark story as passed to exit loop
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		Verbose:       true,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	if !capturedVerbose {
		t.Error("expected verbose flag to be passed through to invoke")
	}
}

func TestRun_DoesNotExitWithUncommittedChanges(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	// Simulate uncommitted changes on first check, then clean on second
	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		if gitCheckCount == 1 {
			return true, nil // First check: dirty
		}
		return false, nil // Subsequent checks: clean
	}

	var invocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have checked git status at least twice (once dirty, once clean)
	if gitCheckCount < 2 {
		t.Errorf("expected at least 2 git status checks, got %d", gitCheckCount)
	}
}

func TestRun_InvokesClaudeToCommitWhenAllStoriesPassButGitDirty(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	// Simulate persistent uncommitted changes until Claude is invoked to commit them
	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	var invocations int
	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		// Stay dirty until Claude gets invoked to commit
		return invocations == 0, nil
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		// Simulate Claude committing the changes
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// BUG: Currently Claude is never invoked when all stories pass but git is dirty
	// Expected: Claude should be invoked to review and commit the changes
	if invocations == 0 {
		t.Error("expected Claude to be invoked to commit changes when all stories pass but git is dirty")
	}

	// Should have checked git status at least twice (dirty, then clean after commit)
	if gitCheckCount < 2 {
		t.Errorf("expected at least 2 git status checks, got %d", gitCheckCount)
	}
}

func TestRun_ExitsImmediatelyWhenClaudeSignalsCompleteAfterCommit(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	gitCheckCount := 0
	var invocations int
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		// Dirty on first check, clean after Claude commits
		return invocations == 0, nil
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		// Simulate Claude committing and signaling COMPLETE
		return "<promise>COMPLETE</promise>", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Claude should be invoked exactly once to commit
	if invocations != 1 {
		t.Errorf("expected exactly 1 invocation, got %d", invocations)
	}

	// Should exit immediately after COMPLETE signal without needing another iteration
	// Git check count: 1 (initial check showing dirty) + 1 (verification after COMPLETE) = 2
	if gitCheckCount != 2 {
		t.Errorf("expected exactly 2 git status checks, got %d", gitCheckCount)
	}
}

func TestRun_ContinuesLoopWhenGitCheckFails(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	// Simulate git check error on first try, then success
	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		if gitCheckCount == 1 {
			return false, fmt.Errorf("git not available")
		}
		return false, nil // Clean on subsequent checks
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have retried after git check error
	if gitCheckCount < 2 {
		t.Errorf("expected at least 2 git status checks after error, got %d", gitCheckCount)
	}
}

func TestRun_ExitsImmediatelyWhenGitClean(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		return false, nil // Always clean
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		t.Error("should not invoke Claude when all stories pass and git is clean")
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have checked git exactly once (clean, so exit immediately)
	if gitCheckCount != 1 {
		t.Errorf("expected exactly 1 git status check, got %d", gitCheckCount)
	}
}

func TestRun_EmitsIterationStartAndStoryStartedEvents(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	handler := &recordingHandler{}
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		EventHandler:  handler,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have IterationStart event
	var hasIterationStart bool
	for _, e := range handler.events {
		if is, ok := e.(events.IterationStart); ok {
			hasIterationStart = true
			if is.Iteration != 1 {
				t.Errorf("expected first iteration, got %d", is.Iteration)
			}
			break
		}
	}
	if !hasIterationStart {
		t.Error("expected IterationStart event")
	}

	// Should have StoryStarted event
	var hasStoryStarted bool
	for _, e := range handler.events {
		if ss, ok := e.(events.StoryStarted); ok {
			hasStoryStarted = true
			if ss.StoryID != "US-001" {
				t.Errorf("expected story ID US-001, got %s", ss.StoryID)
			}
			if ss.Title != "Story 1" {
				t.Errorf("expected title 'Story 1', got %s", ss.Title)
			}
			break
		}
	}
	if !hasStoryStarted {
		t.Error("expected StoryStarted event")
	}
}

func TestRun_EmitsLogMessageOnCompletion(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		t.Error("should not invoke Claude when all stories pass")
		return "", nil
	}

	handler := &recordingHandler{}
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		EventHandler:  handler,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have a LogMessage with "done" indicating completion
	var found bool
	for _, e := range handler.events {
		if lm, ok := e.(events.LogMessage); ok {
			if lm.Level == "info" && contains(lm.Message, "done") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected info LogMessage containing 'done'")
	}
}

func TestRun_EmitsWarningLogOnClaudeError(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	callCount := 0
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		callCount++
		if callCount == 1 {
			// First call: return an error (non-fatal)
			return "", fmt.Errorf("something went wrong")
		}
		// Second call: succeed and mark story as passed
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	handler := &recordingHandler{}
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		EventHandler:  handler,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have a warning LogMessage about Claude error
	var found bool
	for _, e := range handler.events {
		if lm, ok := e.(events.LogMessage); ok {
			if lm.Level == "warning" && contains(lm.Message, "Claude returned error") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected warning LogMessage about Claude error")
	}
}

func TestRun_StoryInvocationUsesMaxTurns50(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedMaxTurns int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		capturedMaxTurns = opts.maxTurns
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	if capturedMaxTurns != 50 {
		t.Errorf("expected maxTurns=50 for story invocation, got %d", capturedMaxTurns)
	}
}

// mockFastUsageLimitWait overrides usageLimitFallbackWait for fast tests.
func mockFastUsageLimitWait() func() {
	orig := usageLimitFallbackWait
	usageLimitFallbackWait = 1 * time.Millisecond
	return func() { usageLimitFallbackWait = orig }
}

func TestInvokeWithUsageLimitWait_RetriesOnUsageLimit(t *testing.T) {
	defer mockFastUsageLimitWait()()

	var calls int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		calls++
		if calls == 1 {
			return "", &claude.UsageLimitError{
				ResetAt: time.Now().Add(-1 * time.Second), // past -> triggers fallback wait
				Message: "You've hit your limit",
			}
		}
		return "success", nil
	}

	output, err := invokeWithUsageLimitWait(context.Background(), invokeOpts{
		prompt: "test",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "success" {
		t.Errorf("expected output 'success', got %q", output)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestInvokeWithUsageLimitWait_PassesThroughNonUsageLimitErrors(t *testing.T) {
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	expectedErr := fmt.Errorf("some other error")
	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "partial", expectedErr
	}

	output, err := invokeWithUsageLimitWait(context.Background(), invokeOpts{
		prompt: "test",
	})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if output != "partial" {
		t.Errorf("expected output 'partial', got %q", output)
	}
}

func TestInvokeWithUsageLimitWait_PassesThroughSuccess(t *testing.T) {
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "done", nil
	}

	output, err := invokeWithUsageLimitWait(context.Background(), invokeOpts{
		prompt: "test",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "done" {
		t.Errorf("expected output 'done', got %q", output)
	}
}

func TestInvokeWithUsageLimitWait_RespectsContext(t *testing.T) {
	defer mockFastUsageLimitWait()()

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", &claude.UsageLimitError{
			ResetAt: time.Now().Add(1 * time.Hour), // far future
			Message: "You've hit your limit",
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := invokeWithUsageLimitWait(ctx, invokeOpts{
		prompt: "test",
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestInvokeWithUsageLimitWait_EmitsUsageLimitEvent(t *testing.T) {
	defer mockFastUsageLimitWait()()

	var calls int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	resetAt := time.Now().Add(-1 * time.Second)
	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		calls++
		if calls == 1 {
			return "", &claude.UsageLimitError{
				ResetAt: resetAt,
				Message: "You've hit your limit",
			}
		}
		return "success", nil
	}

	handler := &recordingHandler{}
	output, err := invokeWithUsageLimitWait(context.Background(), invokeOpts{
		prompt:       "test",
		eventHandler: handler,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "success" {
		t.Errorf("expected output 'success', got %q", output)
	}

	// Verify UsageLimitWait event was emitted
	var found bool
	for _, e := range handler.events {
		if _, ok := e.(events.UsageLimitWait); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected UsageLimitWait event to be emitted")
	}
}

func TestRun_UsageLimitDoesNotCountAsIteration(t *testing.T) {
	defer mockGitClean()()
	defer mockFastUsageLimitWait()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var calls int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		calls++
		// First 3 calls return usage limit, 4th succeeds
		if calls <= 3 {
			return "", &claude.UsageLimitError{
				ResetAt: time.Now().Add(-1 * time.Second),
				Message: "You've hit your limit",
			}
		}
		// Mark story as passed to exit loop
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	// With MaxIterations=2, iteration 1 hits the wrapper (3 retries + success),
	// iteration 2 sees the story now passes and exits. If usage limit retries
	// counted as iterations, we'd exhaust MaxIterations before succeeding.
	err := Run(context.Background(), Config{
		MaxIterations: 2,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v (usage limit retries should not count as iterations)", err)
	}
	if calls != 4 {
		t.Errorf("expected 4 invokeClaudeFn calls (3 rate limited + 1 success), got %d", calls)
	}
}

func TestRun_KnowledgePathPassedToStoryPrompt(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedPrompt string
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		capturedPrompt = opts.prompt
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		KnowledgePath: "/tmp/test/.ralph/knowledge",
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	if !contains(capturedPrompt, "/tmp/test/.ralph/knowledge") {
		t.Error("expected story prompt to contain knowledge path")
	}
}

func TestRun_EmitsWarningLogOnGitCheckFailure(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: true},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		if gitCheckCount == 1 {
			return false, fmt.Errorf("git not available")
		}
		return false, nil
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", nil
	}

	handler := &recordingHandler{}
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		EventHandler:  handler,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have a warning LogMessage about git check failure
	var found bool
	for _, e := range handler.events {
		if lm, ok := e.(events.LogMessage); ok {
			if lm.Level == "warning" && contains(lm.Message, "failed to check git status") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected warning LogMessage about git status check failure")
	}
}

func TestRun_WritesProgressViewFile(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	// Write a progress file with 10 entries
	writeProgressWithEntries(t, progressPath, 10)

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedPrompt string
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		capturedPrompt = opts.prompt
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Verify .progress-view file was created
	viewPath := filepath.Join(dir, ".progress-view")
	if _, err := os.Stat(viewPath); os.IsNotExist(err) {
		t.Error("expected .progress-view file to be created")
	}

	// Verify the prompt references the view file path, not the original
	if !contains(capturedPrompt, ".progress-view") {
		t.Error("expected prompt to reference .progress-view path")
	}

	// Verify the original progress file remains unmodified (still has 10 entries)
	originalContent, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("reading original progress file: %v", err)
	}
	// Count ## entries (excluding ## Codebase Patterns header)
	entryCount := 0
	for _, line := range strings.Split(string(originalContent), "\n") {
		if strings.HasPrefix(line, "## 2026") {
			entryCount++
		}
	}
	if entryCount != 10 {
		t.Errorf("expected original file to have 10 entries, got %d", entryCount)
	}
}

func TestRun_ProgressViewContainsCappedEntries(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	writeProgressWithEntries(t, progressPath, 10)

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Read the view file and verify it has only the last 5 entries
	viewPath := filepath.Join(dir, ".progress-view")
	viewContent, err := os.ReadFile(viewPath)
	if err != nil {
		t.Fatalf("reading view file: %v", err)
	}

	viewStr := string(viewContent)

	// Should contain last 5 entries (S6-S10)
	for i := 6; i <= 10; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d\n", i)
		if !strings.Contains(viewStr, marker) {
			t.Errorf("expected view to contain %q", marker)
		}
	}

	// Should NOT contain first 5 entries (S1-S5)
	for i := 1; i <= 5; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d\n", i)
		if strings.Contains(viewStr, marker) {
			t.Errorf("expected view to NOT contain %q", marker)
		}
	}
}

func writeProgressWithEntries(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Ralph Progress Log\nStarted: 2026-02-20T15:33:09+01:00\n---\n\n")
	b.WriteString("## Codebase Patterns\n\n- Pattern one\n\n---\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "## 2026-02-20 - S%d\n", i)
		fmt.Fprintf(&b, "- Implemented story S%d\n", i)
		b.WriteString("---\n\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("writing progress file: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
