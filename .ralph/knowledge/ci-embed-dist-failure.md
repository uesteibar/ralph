## Tags: ci, embed, web, dist, go-test

# CI `go` check fails with `pattern all:dist: no matching files found`

## Problem
The `web/embed.go` file uses `//go:embed all:dist` to embed the built React SPA.
The `web/dist` directory is gitignored and only created by `cd web && npm run build`.

If `web/dist` doesn't exist, all Go packages that transitively depend on the `web`
package will fail with `[setup failed]`.

## How CI handles this
The CI workflow (`.github/workflows/ci.yml`) builds web assets first:
```yaml
- name: Build web assets
  run: cd web && npm ci && npm run build
```

The local `just test` recipe does NOT build web assets — it runs `go test -count=1 ./...` directly.

## Fix for local development
Run `cd web && npm ci && npm run build` before `just test`, or use `just install-autoralph`
which does both.

## Common cause of CI failure
If the branch is behind main and main has changes to shared files (especially `cmd/autoralph/main.go`),
rebasing onto main resolves the issue.
