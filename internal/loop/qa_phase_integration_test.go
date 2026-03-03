package loop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/prd"
)

// --------------------------------------------------------------------------
// IT-002: Loop exits after stories without running QA
// --------------------------------------------------------------------------

func TestIT002_LoopExitsAfterStoriesWithoutRunningQA(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	// Step 1: Set up a PRD with one story
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

	// Step 2: Mock invokeClaudeFn to mark the story as passing
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invocations := 0
	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		testPRD.UserStories[0].Passes = true
		prd.Write(prdPath, testPRD)
		return "", nil
	}

	handler := &recordingHandler{}

	// Step 3: Call loop.Run()
	err := Run(context.Background(), Config{
		MaxIterations: 5,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"go test ./..."},
		EventHandler:  handler,
	})

	// Step 4: Verify loop exits successfully after the story passes
	if err != nil {
		t.Errorf("Run returned error: %v", err)
	}

	// Step 5: Verify no QA events were emitted
	for _, e := range handler.events {
		switch e.(type) {
		case events.QAVerifyStarted:
			t.Error("unexpected QAVerifyStarted event — Run() should not emit QA events")
		case events.QAFixStarted:
			t.Error("unexpected QAFixStarted event — Run() should not emit QA events")
		case events.QAComplete:
			t.Error("unexpected QAComplete event — Run() should not emit QA events")
		}
	}

	// Step 6: Verify only one Claude invocation occurred (for the story, not for QA)
	if invocations != 1 {
		t.Errorf("expected 1 invocation (story only), got %d", invocations)
	}

	// Verify StoryStarted was emitted
	foundStory := false
	for _, e := range handler.events {
		if ss, ok := e.(events.StoryStarted); ok {
			foundStory = true
			if ss.StoryID != "US-001" {
				t.Errorf("expected story ID US-001, got %s", ss.StoryID)
			}
		}
	}
	if !foundStory {
		t.Error("expected StoryStarted event to be emitted")
	}
}

// --------------------------------------------------------------------------
// IT-003: RunFull completes stories then runs QA verification
// --------------------------------------------------------------------------

func TestIT003_RunFullCompletesStoriesThenRunsQA(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	// Step 1: Set up a PRD with one story
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

	// Step 2: Mock invokeClaudeFn: mark story passing on first call, QA succeeds on second
	invocations := 0
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		invocations++
		if invocations == 1 {
			// Story invocation — mark story as passed
			testPRD.UserStories[0].Passes = true
			prd.Write(prdPath, testPRD)
			return "", nil
		}
		// QA verify invocation — just succeed
		return "", nil
	}

	// Step 3: Mock quality checks to pass
	defer mockQualityChecks(func(call int, cmd string) error {
		return nil
	})()

	handler := &recordingHandler{}

	// Step 4: Call loop.RunFull()
	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 3,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
		EventHandler:  handler,
	})

	// Step 5: Verify events emitted in order: StoryStarted → QAVerifyStarted → QAComplete{Passed: true}
	if err != nil {
		t.Fatalf("RunFull returned error: %v", err)
	}

	var eventTypes []string
	for _, e := range handler.events {
		switch ev := e.(type) {
		case events.StoryStarted:
			eventTypes = append(eventTypes, "StoryStarted")
		case events.QAVerifyStarted:
			eventTypes = append(eventTypes, "QAVerifyStarted")
		case events.QAComplete:
			eventTypes = append(eventTypes, fmt.Sprintf("QAComplete{Passed:%v}", ev.Passed))
		}
	}

	// Verify key events are present and in order
	var hasStory, hasVerify, hasComplete bool
	var storyIdx, verifyIdx, completeIdx int
	for i, et := range eventTypes {
		switch {
		case et == "StoryStarted":
			hasStory = true
			storyIdx = i
		case et == "QAVerifyStarted":
			hasVerify = true
			verifyIdx = i
		case et == "QAComplete{Passed:true}":
			hasComplete = true
			completeIdx = i
		}
	}

	if !hasStory {
		t.Error("expected StoryStarted event")
	}
	if !hasVerify {
		t.Error("expected QAVerifyStarted event")
	}
	if !hasComplete {
		t.Error("expected QAComplete{Passed:true} event")
	}
	if hasStory && hasVerify && storyIdx >= verifyIdx {
		t.Error("expected StoryStarted before QAVerifyStarted")
	}
	if hasVerify && hasComplete && verifyIdx >= completeIdx {
		t.Error("expected QAVerifyStarted before QAComplete")
	}

	// Step 6: Verify loop returns nil
	// (already checked above)

	// Verify PRD qaVerification status is "passed"
	p, _ := prd.Read(prdPath)
	if prd.QAVerificationStatus(p) != "passed" {
		t.Errorf("expected QA status passed, got %s", prd.QAVerificationStatus(p))
	}
}

// --------------------------------------------------------------------------
// IT-004: RunFull triggers QA fix loop when quality checks fail after QA verify
// --------------------------------------------------------------------------

func TestIT004_RunFullTriggersQAFixLoop(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	// Step 1: Set up a PRD with one passing story
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

	// Step 2 & 3: Mock QA verify and fix invocations to succeed
	qaFixInvocations := 0
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", nil
	}

	// Step 4: Mock quality checks: fail on first run, pass on second
	qualityCheckCallCount := 0
	defer mockQualityChecks(func(call int, cmd string) error {
		qualityCheckCallCount++
		if qualityCheckCallCount <= 1 {
			return fmt.Errorf("check failed")
		}
		return nil
	})()

	handler := &recordingHandler{}

	// Step 5: Call loop.RunFull()
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

	// Step 6: Verify events: QAVerifyStarted → QAFixStarted → QAVerifyStarted → QAComplete{Passed: true}
	var qaVerifyCount, qaFixCount int
	var qaComplete *events.QAComplete
	for _, e := range handler.events {
		switch ev := e.(type) {
		case events.QAVerifyStarted:
			qaVerifyCount++
		case events.QAFixStarted:
			qaFixCount++
			qaFixInvocations++
		case events.QAComplete:
			qaComplete = &ev
		}
	}

	if qaVerifyCount != 2 {
		t.Errorf("expected 2 QAVerifyStarted events, got %d", qaVerifyCount)
	}
	if qaFixCount != 1 {
		t.Errorf("expected 1 QAFixStarted event, got %d", qaFixCount)
	}
	if qaComplete == nil {
		t.Fatal("expected QAComplete event")
	}
	if !qaComplete.Passed {
		t.Error("expected QAComplete.Passed to be true after fix")
	}

	// Verify QA fix was invoked exactly once
	if qaFixInvocations != 1 {
		t.Errorf("expected QA fix invoked once, got %d", qaFixInvocations)
	}
}

// --------------------------------------------------------------------------
// IT-005: RunFull pauses after max QA attempts
// --------------------------------------------------------------------------

func TestIT005_RunFullPausesAfterMaxAttempts(t *testing.T) {
	defer mockGitClean()()

	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.json")
	progressPath := filepath.Join(dir, "progress.txt")

	// Step 1: Set up a PRD with one passing story
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

	// Step 2: Mock quality checks to always fail
	origInvokeFn := invokeClaudeFn
	defer func() { invokeClaudeFn = origInvokeFn }()

	invokeClaudeFn = func(ctx context.Context, opts invokeOpts) (string, error) {
		return "", nil
	}

	defer mockQualityChecks(func(call int, cmd string) error {
		return fmt.Errorf("check failed")
	})()

	handler := &recordingHandler{}

	// Step 3: Call loop.RunFull() with max attempts = 2
	err := RunFull(context.Background(), Config{
		MaxIterations: 5,
		MaxQAAttempts: 2,
		WorkDir:       dir,
		PRDPath:       prdPath,
		ProgressPath:  progressPath,
		QualityChecks: []string{"just test"},
		EventHandler:  handler,
	})

	// Step 4: Verify QAVerifyStarted emitted twice, QAFixStarted emitted once
	// (With MaxQAAttempts=2: attempt 1 verify+fail, fix, attempt 2 verify+fail)
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

	// Step 5: Verify QAComplete{Passed: false} emitted
	if qaComplete == nil {
		t.Fatal("expected QAComplete event")
	}
	if qaComplete.Passed {
		t.Error("expected QAComplete.Passed to be false")
	}

	// Step 6: Verify function returns an error
	if err == nil {
		t.Fatal("expected error when max QA attempts exceeded")
	}
	if !strings.Contains(err.Error(), "QA verification failed after 2 attempts") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify PRD qaVerification status is "failed"
	p, _ := prd.Read(prdPath)
	if prd.QAVerificationStatus(p) != "failed" {
		t.Errorf("expected QA status failed, got %s", prd.QAVerificationStatus(p))
	}
}
