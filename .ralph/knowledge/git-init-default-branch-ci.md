## Tags: ci, git, test, defaultBranch

# Tests using `git init` must specify `-b main` for CI compatibility

## Problem
Tests that create git repos with `git init` and then reference `main` as the branch name
(e.g., `git push origin main`) fail in CI because `init.defaultBranch` is not configured
on the Ubuntu runner. Without this setting, git defaults to `master`, causing
`error: src refspec main does not match any`.

## Fix
Always use `git init -b main` (or `git init --initial-branch=main`) when the test
expects the branch to be named `main`. This works regardless of the system's
`init.defaultBranch` configuration.

## Example
```go
// BAD: depends on global git config
{"git", "init", "--bare"}

// GOOD: explicit branch name
{"git", "init", "--bare", "-b", "main"}
```
