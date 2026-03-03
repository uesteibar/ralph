package loop

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

// mockQualityChecks sets up runQualityCheckFn to return results based on callFn.
// callFn receives the call count (1-based) and the command, and returns an error.
func mockQualityChecks(callFn func(call int, cmd string) error) func() {
	orig := runQualityCheckFn
	callCount := 0
	runQualityCheckFn = func(ctx context.Context, dir, command string) error {
		callCount++
		return callFn(callCount, command)
	}
	return func() { runQualityCheckFn = orig }
}

func TestRunFull_StoriesThenQAPass(t *testing.T) {
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

	invocations := 0
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		if invocations == 1 {
			// Story invocation — mark story as passed.
			testPRD.UserStories[0].Passes = true
			prd.Write(prdPath, testPRD)
			return "", nil
		}
		// QA verify invocation — just succeed.
		return "", nil
	}

	// Quality checks pass.
	defer mockQualityChecks(func(call int, cmd string) error {
		return nil
	})()

	handler := &recordingHandler{}
	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
		EventHandler:  handler,
	})

	if err != nil {
		t.Fatalf("RunFull returned error: %v", err)
	}

	// Verify events emitted in order: StoryStarted, QAVerifyStarted, QAComplete{Passed: true}
	var hasStoryStarted, hasQAVerifyStarted, hasQAComplete bool
	for _, e := range handler.events {
		switch ev := e.(type) {
		case events.StoryStarted:
			hasStoryStarted = true
		case events.QAVerifyStarted:
			hasQAVerifyStarted = true
		case events.QAComplete:
			hasQAComplete = true
			if !ev.Passed {
				t.Error("expected QAComplete.Passed to be true")
			}
		}
	}

	if !hasStoryStarted {
		t.Error("expected StoryStarted event")
	}
	if !hasQAVerifyStarted {
		t.Error("expected QAVerifyStarted event")
	}
	if !hasQAComplete {
		t.Error("expected QAComplete event")
	}

	// Verify PRD qaVerification status is "passed".
	p, _ := prd.Read(prdPath)
	if prd.QAVerificationStatus(p) != "passed" {
		t.Errorf("expected QA status passed, got %s", prd.QAVerificationStatus(p))
	}
}

func TestRunFull_QAFixLoopThenPass(t *testing.T) {
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
		return "", nil
	}

	// Quality checks: fail on first call, pass on second (after QA fix).
	qualityCheckCallCount := 0
	defer mockQualityChecks(func(call int, cmd string) error {
		qualityCheckCallCount++
		if qualityCheckCallCount <= 1 {
			return fmt.Errorf("check failed")
		}
		return nil
	})()

	handler := &recordingHandler{}
	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
		EventHandler:  handler,
	})

	if err != nil {
		t.Fatalf("RunFull returned error: %v", err)
	}

	// Verify events: QAVerifyStarted, QAFixStarted, QAVerifyStarted, QAComplete{Passed: true}
	var qaVerifyCount, qaFixCount int
	var qaCompleted bool
	for _, e := range handler.events {
		switch ev := e.(type) {
		case events.QAVerifyStarted:
			qaVerifyCount++
		case events.QAFixStarted:
			qaFixCount++
		case events.QAComplete:
			qaCompleted = true
			if !ev.Passed {
				t.Error("expected QAComplete.Passed to be true after fix succeeded")
			}
		}
	}

	if qaVerifyCount != 2 {
		t.Errorf("expected 2 QAVerifyStarted events, got %d", qaVerifyCount)
	}
	if qaFixCount != 1 {
		t.Errorf("expected 1 QAFixStarted event, got %d", qaFixCount)
	}
	if !qaCompleted {
		t.Error("expected QAComplete event")
	}
}

func TestRunFull_MaxAttemptsExceeded(t *testing.T) {
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
		return "", nil
	}

	// Quality checks always fail.
	defer mockQualityChecks(func(call int, cmd string) error {
		return fmt.Errorf("check failed")
	})()

	handler := &recordingHandler{}
	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 2,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
		EventHandler:  handler,
	})

	if err == nil {
		t.Fatal("expected error when max QA attempts exceeded")
	}

	if !contains(err.Error(), "QA verification failed after 2 attempts") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify events: QAVerifyStarted x2, QAFixStarted x1, QAComplete{Passed: false}
	var qaVerifyCount, qaFixCount int
	var qaComplete *events.QAComplete
	for _, e := range handler.events {
		switch ev := e.(type) {
		case events.QAVerifyStarted:
			qaVerifyCount++
		case events.QAFixStarted:
			qaFixCount++
		case events.QAComplete:
			qaComplete = &ev
		}
	}

	if qaVerifyCount != 2 {
		t.Errorf("expected 2 QAVerifyStarted events, got %d", qaVerifyCount)
	}
	if qaFixCount != 1 {
		t.Errorf("expected 1 QAFixStarted event (between attempt 1 and 2), got %d", qaFixCount)
	}
	if qaComplete == nil {
		t.Fatal("expected QAComplete event")
	}
	if qaComplete.Passed {
		t.Error("expected QAComplete.Passed to be false")
	}

	// Verify PRD qaVerification status is "failed".
	p, _ := prd.Read(prdPath)
	if prd.QAVerificationStatus(p) != "failed" {
		t.Errorf("expected QA status failed, got %s", prd.QAVerificationStatus(p))
	}
}

func TestRunFull_DefaultMaxQAAttempts(t *testing.T) {
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
		return "", nil
	}

	// Quality checks always fail.
	defer mockQualityChecks(func(call int, cmd string) error {
		return fmt.Errorf("check failed")
	})()

	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		// MaxQAAttempts not set, should default to 3
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
	})

	if err == nil {
		t.Fatal("expected error when max QA attempts exceeded")
	}

	if !contains(err.Error(), "QA verification failed after 3 attempts") {
		t.Errorf("expected default 3 attempts in error, got: %v", err)
	}
}

func TestRunFull_StoryFailurePropagates(t *testing.T) {
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

	// Never mark story as passing — Run() will exhaust iterations.
	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", nil
	}

	err := RunFull(context.Background(), Config{
		MaxIterations: 1,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
	})

	if err == nil {
		t.Fatal("expected error from Run() to propagate through RunFull()")
	}

	if !contains(err.Error(), "max iterations") {
		t.Errorf("expected max iterations error, got: %v", err)
	}
}

func TestRunFull_QAVerifyUsesCorrectMaxTurns(t *testing.T) {
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

	var capturedMaxTurns []int
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		capturedMaxTurns = append(capturedMaxTurns, opts.maxTurns)
		return "", nil
	}

	// Fail first check, pass second to capture both verify and fix max turns.
	checkCallCount := 0
	defer mockQualityChecks(func(call int, cmd string) error {
		checkCallCount++
		if checkCallCount <= 1 {
			return fmt.Errorf("fail")
		}
		return nil
	})()

	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
	})

	if err != nil {
		t.Fatalf("RunFull returned error: %v", err)
	}

	// Should have 3 invocations: QA verify (30), QA fix (30), QA verify again (30)
	if len(capturedMaxTurns) != 3 {
		t.Fatalf("expected 3 invocations, got %d", len(capturedMaxTurns))
	}
	if capturedMaxTurns[0] != 30 {
		t.Errorf("QA verify maxTurns: expected 30, got %d", capturedMaxTurns[0])
	}
	if capturedMaxTurns[1] != 30 {
		t.Errorf("QA fix maxTurns: expected 30, got %d", capturedMaxTurns[1])
	}
	if capturedMaxTurns[2] != 30 {
		t.Errorf("QA re-verify maxTurns: expected 30, got %d", capturedMaxTurns[2])
	}
}
