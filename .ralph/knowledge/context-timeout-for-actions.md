# Context Timeout for Long-Running Actions

## Tags: orchestrator, actions, timeout, claude-cli, debugging

## Problem

Action functions (QA verify, QA fix, feedback, checks, PR) were using `context.Background()` without any timeout. This caused issues to get stuck indefinitely when the Claude CLI process failed to exit properly.

### Symptoms

- Issue stuck in "qa" state for hours
- Claude CLI process (PID) still running but not making progress
- No `qa_verify_finish` or similar completion events logged
- Orchestrator unable to progress because it thinks the action is still running

### Root Cause

The action functions create a context with `context.Background()` which has no timeout or cancellation mechanism:

```go
func NewVerifyAction(cfg Config) func(issue db.Issue, database *db.DB) error {
    return func(issue db.Issue, database *db.DB) error {
        ctx := context.Background()  // ❌ No timeout!
        // ... uses ctx for all operations
    }
}
```

When the Claude CLI invocation completes (AI finishes its work), the verify action continues to:
1. Read the PRD to check findings
2. Run quality checks independently (`ralph check just test`, `ralph check just vet`)
3. Log completion and return

However, if the Claude CLI process doesn't exit cleanly after step 1, the `cmd.Wait()` call in `internal/claude/claude.go:191` will hang forever waiting for the process to exit. Since there's no timeout, the action never completes.

### Investigation Details

For issue `9fe1c588-a38f-44e0-b48d-96fe45cf0eff`:
- QA verify started at 14:50:53
- AI agent completed work at 14:55:11 (wrote APPROVE summary)
- Quality checks ran successfully (logs show all tests passed)
- But the Claude CLI process (PID 40630) remained running for 19+ minutes
- No `qa_verify_finish` event was ever logged
- The action was stuck waiting for the process to exit

## Solution

Add a timeout context to all long-running action functions:

```go
func NewVerifyAction(cfg Config) func(issue db.Issue, database *db.DB) error {
    return func(issue db.Issue, database *db.DB) error {
        // Create a context with timeout to prevent indefinite hangs.
        // QA verification involves AI invocation, quality checks, and git operations,
        // which should complete within 60 minutes under normal circumstances.
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
        defer cancel()

        // ... rest of the action
    }
}
```

### Timeout Values

- **QA verify/fix**: 60 minutes (includes AI invocation + quality checks + git ops)
- **Feedback addressing**: 60 minutes (includes PR comment fetching + AI invocation + quality checks + git ops)
- **Check fixing**: 60 minutes (includes check run fetching + AI invocation + quality checks + git ops)
- **PR creation**: 30 minutes (includes git push + AI invocation for description + GitHub API calls)

These are generous timeouts that should never be hit under normal operation, but provide a safety net against indefinite hangs.

## Files Modified

- `internal/qa/verify.go`: Added timeout context and `time` import
- `internal/qa/fix.go`: Added timeout context and `time` import
- `internal/autoralph/feedback/feedback.go`: Added timeout context and `time` import
- `internal/autoralph/checks/checks.go`: Added timeout context and `time` import
- `internal/autoralph/pr/pr.go`: Added timeout context and `time` import

## Testing

All existing tests pass with the timeout context in place:
- `internal/qa`: 48 tests, all passing
- `internal/autoralph/feedback`: 45 tests, all passing
- `internal/autoralph/checks`: 32 tests, all passing
- `internal/autoralph/pr`: 27 tests, all passing

## How Context Timeout Works

When a context times out:
1. The `ctx.Done()` channel is closed
2. Commands created with `exec.CommandContext(ctx, ...)` receive a SIGKILL
3. The Claude CLI process is terminated
4. `cmd.Wait()` returns an error
5. The action function returns the error to the orchestrator
6. The orchestrator can retry or mark the issue as failed

This is much better than hanging indefinitely!

## Future Improvements

Consider adding configurable timeouts in the ralph config:
```yaml
timeouts:
  qa_verify: 60m
  qa_fix: 60m
  feedback: 60m
  checks: 60m
  pr: 30m
```

## Additional Resilience Improvement

Beyond adding timeouts, we also made the QA verify action more resilient to process exit failures:

### Problem
When the Claude CLI process hung after the AI completed its work and updated the PRD to "passed", the action would return an invocation error. This caused the orchestrator to incorrectly transition to QA_FIX even though QA had actually passed.

### Solution
Check if the PRD shows "passed" before returning an invocation error:

```go
// If invocation failed but PRD already shows "passed", the AI completed
// successfully even though the process didn't exit cleanly. We can proceed.
// Otherwise, if invocation failed and status is not "passed", return the error.
if invokeErr != nil && p.QAVerification.Status != "passed" {
    return fmt.Errorf("invoking AI: %w", invokeErr)
}
```

This makes the action resilient to:
- Process exit timeouts after successful completion
- Process hangs after writing results
- Context cancellation after AI work is done

### Testing
Added two regression tests:
- `TestNewVerifyAction_InvokeErrorButPRDShowsPassed_Succeeds`: Verifies that QA succeeds when PRD shows "passed" despite invocation error
- `TestNewVerifyAction_InvokeErrorAndPRDNotPassed_ReturnsError`: Verifies that genuine invocation failures still return errors

## Related Issues

- Issue `9fe1c588-a38f-44e0-b48d-96fe45cf0eff` (UNI-123): QA verify stuck for 19+ minutes after AI completed
