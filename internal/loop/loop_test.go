package loop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/prd"
)

func TestRun_InvokesQAVerificationWhenAllStoriesPass(t *testing.T) {
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
