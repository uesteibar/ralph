# E2E Testing Skill for autoralph

Use this skill to run end-to-end tests that verify autoralph changes across the full stack: Go backend, React frontend, mock API servers, and Playwright browser tests.

## Prerequisites

Before running E2E tests, verify these tools are installed:

```bash
# Go compiler
go version
# Expected: go version go1.25.6 (or later)

# Node.js + npm
node --version && npm --version
# Expected: node v20+ and npm 10+

# just (task runner)
just --version
# Expected: just 1.x
```

## Step 1: Build autoralph and web assets

Build the Go binary and React SPA. Both must be current before running E2E tests.

```bash
# Build web assets (React SPA)
just web-build
# Expected: "vite build" completes, web/dist/ directory created

# Build autoralph binary
just autoralph
# Expected: ./autoralph binary created in project root
```

## Step 2: Install Playwright browsers

Install Playwright and its browser binaries (only needed once, or after Playwright version changes).

```bash
just e2e-setup
# Expected: Chromium browser downloaded and installed
```

This runs `cd test/e2e/playwright && npm install && npx playwright install chromium`.

## Step 3: Run Go playground tests

The Go playground tests start mock servers (Linear GraphQL + GitHub REST), a SQLite database, and an autoralph HTTP server — all in-process via `test/e2e/playground.go`.

```bash
just e2e
# Expected: All tests pass. Output shows PASS for each test in test/e2e/
```

This runs `go test -v -count=1 ./test/e2e/...`.

**Expected test output pattern:**
```
=== RUN   TestPlayground_SmokeTest
--- PASS: TestPlayground_SmokeTest
=== RUN   TestPlayground_StopsCleanly
--- PASS: TestPlayground_StopsCleanly
=== RUN   TestPlayground_MockServersAvailable
--- PASS: TestPlayground_MockServersAvailable
=== RUN   TestPlayground_ProjectSeeded
--- PASS: TestPlayground_ProjectSeeded
=== RUN   TestPlayground_SeedIssue
--- PASS: TestPlayground_SeedIssue
...
PASS
ok      github.com/uesteibar/ralph/test/e2e
```

## Step 4: Run Playwright browser tests

Playwright tests verify the web UI via a real browser. They require autoralph to be running on `127.0.0.1:7749`.

```bash
# Start autoralph in the background (requires built binary from Step 1)
./autoralph serve &
AUTORALPH_PID=$!

# Run Playwright tests
cd test/e2e/playwright && npx playwright test

# Stop autoralph
kill $AUTORALPH_PID
```

**Expected output pattern:**
```
Running 3 tests using 1 worker

  ✓ Autoralph Smoke Tests > API status endpoint returns ok
  ✓ Autoralph Smoke Tests > Dashboard page loads
  ✓ Autoralph Smoke Tests > Unknown API route returns 404

  3 passed
```

Playwright config is at `test/e2e/playwright/playwright.config.ts`. Tests are in `test/e2e/playwright/tests/`.

## Step 5: CLI verification tests

Verify the autoralph binary starts correctly and responds to basic commands.

```bash
# Version check
./autoralph --version
# Expected: prints version string (e.g., "autoralph dev" or "autoralph v0.1.0")

# Start server and verify health
./autoralph serve &
AUTORALPH_PID=$!
sleep 1

# Health check
curl -s http://127.0.0.1:7749/api/status
# Expected: {"status":"ok","uptime_seconds":...,"active_builds":0}

# Projects list (empty unless configured)
curl -s http://127.0.0.1:7749/api/projects
# Expected: [] (empty array) or list of configured projects

# Issues list
curl -s http://127.0.0.1:7749/api/issues
# Expected: [] (empty array) or list of issues

# Unknown API route returns 404
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:7749/api/nonexistent
# Expected: 404

# Stop server
kill $AUTORALPH_PID
```

## Step 6: Scenario tests (issue lifecycle)

Run the Go playground tests which exercise issue lifecycle scenarios programmatically.

The playground infrastructure (`test/e2e/playground.go`) supports:

### Seed a test issue and verify state
```go
pg := e2e.StartPlayground(t)
issue := pg.SeedIssue("TEST-1", "Add user avatars", "queued")

// Verify via API
resp, _ := http.Get(pg.BaseURL() + "/api/issues/" + issue.ID)
// Expected: issue with state "queued"
```

### Simulate approval via Linear mock
```go
pg.Linear.SimulateApproval("linear-issue-id")
// Adds "@autoralph approved" comment to the mock Linear issue
```

### Simulate PR merge via GitHub mock
```go
pg.GitHub.SimulateMerge("owner", "repo", 1)
// Marks PR #1 as merged in the mock GitHub server
```

### Simulate review feedback via GitHub mock
```go
pg.GitHub.SimulateChangesRequested("owner", "repo", 1)
// Adds a "changes_requested" review to PR #1
```

### Full lifecycle test pattern
```go
func TestFullLifecycle(t *testing.T) {
    pg := e2e.StartPlayground(t)

    // 1. Seed issue as QUEUED
    issue := pg.SeedIssue("TEST-1", "Add user avatars", "queued")

    // 2. Verify via API
    resp, _ := http.Get(pg.BaseURL() + "/api/issues/" + issue.ID)
    // assert state == "queued"

    // 3. Add issue to Linear mock for ingestion
    pg.Linear.AddIssue(mocklinear.Issue{
        ID: "linear-1", Identifier: "TEST-1", Title: "Add user avatars",
        StateID: "state-todo", StateName: "Todo", StateType: "unstarted",
    })

    // 4. Simulate approval
    pg.Linear.SimulateApproval("linear-1")

    // 5. Add PR to GitHub mock
    pg.GitHub.AddPR("test-owner", "test-repo", mockgithub.PR{
        Number: 1, Head: "autoralph/test-1", Base: "main", State: "open",
    })

    // 6. Simulate merge
    pg.GitHub.SimulateMerge("test-owner", "test-repo", 1)

    // 7. Verify completion via API
    // GET /api/issues/{id} → state should be "completed"
}
```

### Mock servers accept `--linear-url` and `--github-url` flags
```bash
# When running autoralph against mock servers:
./autoralph serve --linear-url http://localhost:MOCK_PORT --github-url http://localhost:MOCK_PORT

# Or via environment variables:
AUTORALPH_LINEAR_URL=http://localhost:MOCK_PORT \
AUTORALPH_GITHUB_URL=http://localhost:MOCK_PORT \
./autoralph serve
```

## Step 7: Teardown

The Go playground tests handle teardown automatically via `t.Cleanup()`:
- HTTP server is closed
- Database connection is closed
- Temp directories (project fixture, DB) are removed
- Mock servers (httptest-based, in-process) are closed

For manual teardown after CLI tests:
```bash
# Kill any lingering autoralph processes
pkill -f "autoralph serve" || true

# Remove test artifacts
rm -f autoralph
rm -rf /tmp/autoralph-test-*
```

## Troubleshooting

### Test fails with "port already in use"
Another autoralph instance or process is using the port.
```bash
# Find and kill the process
lsof -i :7749
kill <PID>
```
The Go playground tests use random ports (`127.0.0.1:0`) so they don't conflict, but Playwright tests expect port 7749.

### Playwright test fails with "Target page, context or browser has been closed"
The autoralph server crashed or wasn't started before running Playwright.
```bash
# Ensure autoralph is running
curl -s http://127.0.0.1:7749/api/status
# If it fails, restart: ./autoralph serve &
```

### Go test fails with "playground server did not become healthy within 5s"
The server failed to start. Check for:
- Port conflicts (unlikely with `:0` random ports)
- Database issues — ensure SQLite (modernc.org/sqlite) compiles correctly
- Missing web/dist/ — run `just web-build` first

### "npm install" or "npx playwright install" fails
Network issues or version mismatch.
```bash
# Clear npm cache and retry
cd test/e2e/playwright && rm -rf node_modules && npm install
npx playwright install chromium
```

### Mock server tests fail
Ensure go-github compatibility: the GitHub mock serves routes under `/api/v3/` prefix because `go-github` adds this prefix when using `WithEnterpriseURLs`.

### Tests pass locally but fail in CI
- Ensure `just web-build` runs before `just e2e` (web assets must be embedded)
- Ensure Playwright browsers are installed (`just e2e-setup`)
- Check that no other process occupies port 7749

### "go build ./cmd/autoralph" fails
Dependencies may be out of sync.
```bash
go mod tidy
just autoralph
```

## Quick reference: justfile targets

| Target | Command | Description |
|---|---|---|
| `e2e-setup` | `cd test/e2e/playwright && npm install && npx playwright install chromium` | Install Playwright browsers |
| `e2e` | `go test -v -count=1 ./test/e2e/...` | Run Go playground E2E tests |
| `web-build` | `cd web && npm run build` | Build React SPA |
| `autoralph` | `go build -o autoralph ./cmd/autoralph` | Build autoralph binary |
| `test` | `go test -count=1 ./...` | Run all unit + E2E tests |
| `vet` | `go vet ./...` | Run Go vet on all packages |

## File map

| Path | Purpose |
|---|---|
| `test/e2e/playground.go` | Go playground orchestration (StartPlayground, SeedIssue) |
| `test/e2e/playground_test.go` | Playground smoke and integration tests |
| `test/e2e/playground_helpers_test.go` | Mock factory helpers (mocklinearIssue, mockgithubPR) |
| `test/e2e/mocks/linear/linear.go` | Mock Linear GraphQL server |
| `test/e2e/mocks/github/github.go` | Mock GitHub REST server |
| `test/e2e/fixtures/test-project/` | Minimal Go project fixture with .ralph/ralph.yaml |
| `test/e2e/playwright/playwright.config.ts` | Playwright configuration |
| `test/e2e/playwright/helpers.ts` | Playwright helper functions |
| `test/e2e/playwright/tests/smoke.spec.ts` | Playwright smoke tests |
| `cmd/autoralph/main.go` | CLI entry point (--linear-url, --github-url flags) |
| `justfile` | Build and test targets |
