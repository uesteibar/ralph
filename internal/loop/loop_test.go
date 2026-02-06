package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestRun_InvokesQAVerificationWhenAllStoriesPass(t *testing.T) {
	defer mockGitClean()()

	// Create temp dir with PRD where all stories pass
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	// Track invocations
	var qaInvocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			qaInvocations++
			// Simulate QA agent marking test as passed
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	if qaInvocations != 1 {
		t.Errorf("expected 1 QA verification invocation, got %d", qaInvocations)
	}
}

func TestRun_SkipsQAVerificationWhenNoIntegrationTests(t *testing.T) {
	defer mockGitClean()()

	// Create temp dir with PRD where all stories pass but no integration tests
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
		// No integration tests
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var qaInvocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			qaInvocations++
		}
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

	if qaInvocations != 0 {
		t.Errorf("expected 0 QA verification invocations when no integration tests, got %d", qaInvocations)
	}
}

func TestRun_QAVerificationReceivesCorrectContext(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedOpts invokeOpts
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			capturedOpts = opts
			// Mark test as passed to exit loop
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
		return "", nil
	}

	qualityChecks := []string{"go test ./...", "go vet ./..."}
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: qualityChecks,
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	if capturedOpts.dir != dir {
		t.Errorf("expected WorkDir %s, got %s", dir, capturedOpts.dir)
	}

	// Verify prompt contains expected paths
	if capturedOpts.prompt == "" {
		t.Error("expected non-empty prompt")
	}
}

func TestRun_ReturnsSuccessAfterQAVerificationCompletes(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			// Simulate QA agent marking test as passed
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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
		t.Errorf("expected no error after QA verification, got: %v", err)
	}

	// Verify PRD shows all tests passing
	finalPRD, _ := prd.Read(prdPath)
	if !prd.AllIntegrationTestsPass(finalPRD) {
		t.Error("expected all integration tests to pass after QA verification")
	}
}

func TestRun_ContinuesLoopIfQAVerificationDoesNotPassAllTests(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var iterations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		iterations++
		// Don't mark test as passed - should hit max iterations
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err == nil {
		t.Error("expected error when max iterations reached")
	}

	// Should invoke QA verification multiple times (up to max iterations)
	if iterations < 2 {
		t.Errorf("expected multiple QA verification attempts, got %d", iterations)
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

func TestRun_InvokesQAFixWhenIntegrationTestsFail(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var qaVerificationCount, qaFixCount int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			qaVerificationCount++
			// QA verification does not fix the test
			return "", nil
		}
		if opts.isQAFix {
			qaFixCount++
			// Fix agent marks test as passed
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	if qaVerificationCount < 1 {
		t.Errorf("expected at least 1 QA verification invocation, got %d", qaVerificationCount)
	}
	if qaFixCount < 1 {
		t.Errorf("expected at least 1 QA fix invocation, got %d", qaFixCount)
	}
}

func TestRun_QAFixReceivesFailedTests(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: true},
			{ID: "IT-002", Description: "Test 2", Passes: false, Failure: "Element not found"},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var capturedFixPrompt string
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAFix {
			capturedFixPrompt = opts.prompt
			// Fix agent marks test as passed
			testPRD.IntegrationTests[1].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	// Verify the fix prompt contains the failed test details
	if capturedFixPrompt == "" {
		t.Error("expected QA fix prompt to be captured")
	}
	if !contains(capturedFixPrompt, "IT-002") {
		t.Error("expected fix prompt to contain failed test ID IT-002")
	}
	if !contains(capturedFixPrompt, "Element not found") {
		t.Error("expected fix prompt to contain failure message")
	}
}

func TestRun_FixCycleContinuesUntilAllTestsPass(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
			{ID: "IT-002", Description: "Test 2", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var fixInvocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAFix {
			fixInvocations++
			// Fix one test per invocation
			if fixInvocations == 1 {
				testPRD.IntegrationTests[0].Passes = true
			} else if fixInvocations == 2 {
				testPRD.IntegrationTests[1].Passes = true
			}
			prd.Write(prdPath, testPRD)
		}
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 10,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Should have invoked fix at least twice (once per failing test)
	if fixInvocations < 2 {
		t.Errorf("expected at least 2 fix invocations, got %d", fixInvocations)
	}

	// Final state should have all tests passing
	finalPRD, _ := prd.Read(prdPath)
	if !prd.AllIntegrationTestsPass(finalPRD) {
		t.Error("expected all integration tests to pass after fix cycle")
	}
}

func TestRun_FixCycleRespectsMaxIterations(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var totalInvocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		totalInvocations++
		// Never fix the test - should hit max iterations
		return "", nil
	}

	err := Run(context.Background(), Config{
		MaxIterations: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
	})

	if err == nil {
		t.Error("expected error when max iterations reached with failing tests")
	}

	// Should have invoked multiple times (verification + fix per iteration)
	if totalInvocations < 3 {
		t.Errorf("expected at least 3 invocations with 3 iterations, got %d", totalInvocations)
	}
}

func TestRun_CompleteSignalRejectedWhenIntegrationTestsFail(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	testPRD := &prd.PRD{
		Project:     "test",
		BranchName:  "test/branch",
		Description: "Test project",
		UserStories: []prd.Story{
			{ID: "US-001", Title: "Story 1", Passes: false}, // Start with story not passing
		},
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var storyInvocations int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if !opts.isQAVerification && !opts.isQAFix {
			storyInvocations++
			// Story agent marks story as passed and sends COMPLETE signal
			// but integration tests still fail - COMPLETE should be rejected
			testPRD.UserStories[0].Passes = true
			prd.Write(prdPath, testPRD)
			return "<promise>COMPLETE</promise>", nil
		}
		if opts.isQAFix {
			// Fix agent marks test as passed
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	// Should have invoked the story agent once (COMPLETE rejected because integration tests fail)
	// then continued to QA phase
	if storyInvocations != 1 {
		t.Errorf("expected 1 story invocation (COMPLETE rejected, then QA phase), got %d", storyInvocations)
	}
}

func TestRun_CompleteSignalAcceptedWhenBothStoriesAndIntegrationTestsPass(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: true},
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

	// Should complete immediately without any invocations since all stories
	// and integration tests already pass
	if invocations != 0 {
		t.Errorf("expected 0 invocations when all stories and tests pass, got %d", invocations)
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

func TestRun_VerboseFlagPassedToQAVerification(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var qaVerbose bool
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			qaVerbose = opts.verbose
			// Mark test as passed to exit loop
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	if !qaVerbose {
		t.Error("expected verbose flag to be passed through to QA verification")
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
		// No integration tests - would normally exit immediately
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

func TestRun_GitCheckOnQAVerificationExit(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	// First check dirty (after QA marks test as passing), second clean
	gitCheckCount := 0
	origGitFn := gitHasUncommittedChangesFn
	defer func() { gitHasUncommittedChangesFn = origGitFn }()

	gitHasUncommittedChangesFn = func(ctx context.Context, dir string) (bool, error) {
		gitCheckCount++
		if gitCheckCount == 1 {
			return true, nil // Dirty after QA verification
		}
		return false, nil // Clean on retry
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			// QA verification marks test as passed
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	// Should have checked git at least twice
	if gitCheckCount < 2 {
		t.Errorf("expected at least 2 git checks (dirty then clean), got %d", gitCheckCount)
	}
}

func TestRun_VerboseFlagPassedToQAFix(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	var fixVerbose bool
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAFix {
			fixVerbose = opts.verbose
			// Mark test as passed to exit loop
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	if !fixVerbose {
		t.Error("expected verbose flag to be passed through to QA fix")
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
				ResetAt: time.Now().Add(-1 * time.Second), // past → triggers fallback wait
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

func TestRun_EmitsQAPhaseStartedEvent(t *testing.T) {
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
		IntegrationTests: []prd.IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: false},
		},
	}

	if err := prd.Write(prdPath, testPRD); err != nil {
		t.Fatalf("writing test PRD: %v", err)
	}

	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		if opts.isQAVerification {
			testPRD.IntegrationTests[0].Passes = true
			prd.Write(prdPath, testPRD)
		}
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

	// Should have QAPhaseStarted event with "verification" phase
	var hasQAPhase bool
	for _, e := range handler.events {
		if qa, ok := e.(events.QAPhaseStarted); ok {
			hasQAPhase = true
			if qa.Phase != "verification" {
				t.Errorf("expected phase 'verification', got %s", qa.Phase)
			}
			break
		}
	}
	if !hasQAPhase {
		t.Error("expected QAPhaseStarted event")
	}
}
