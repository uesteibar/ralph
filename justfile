# Ralph — development tasks

# Default: show available recipes
default:
    @just --list

# Build the ralph binary
build:
    go build -o ralph ./cmd/ralph

# Build the ralph binary (alias)
ralph: build

# Build the autoralph binary
autoralph:
    go build -o autoralph ./cmd/autoralph

# Build web assets (React SPA)
web-build:
    cd web && npm run build

# Run Vite dev server for web UI
dev-web:
    cd web && npm run dev

# Run autoralph in dev mode (proxies to Vite dev server)
dev-autoralph: autoralph
    ./autoralph serve --dev

# Run all tests
test:
    go test -count=1 ./...

# Run all tests with verbose output
test-verbose:
    go test -v ./...

# Run tests for a specific package (e.g., just test-pkg config)
test-pkg pkg:
    go test -v ./internal/{{ pkg }}/...

# Run go vet on all packages
vet:
    go vet ./...

# Run shellcheck on installer scripts
shellcheck:
    shellcheck install-ralph.sh install-autoralph.sh

# Install Playwright browsers for E2E tests
e2e-setup:
    cd test/e2e/playwright && npm install && npx playwright install chromium

# Run E2E tests (Go playground tests)
e2e:
    go test -v -count=1 ./test/e2e/...

# Run build + test + vet
check: build autoralph test vet

# Format all Go files
fmt:
    gofmt -w .

# Install ralph to $GOPATH/bin
install:
    go install ./cmd/ralph

# Install autoralph to $GOPATH/bin (installs web deps + builds assets first)
install-autoralph:
    cd web && npm install && npm run build
    go install ./cmd/autoralph

# Clean build artifacts
clean:
    rm -f ralph autoralph
    go clean ./...

# Build documentation site
docs-build:
    mdbook build docs/

# Serve documentation locally with live reload
docs-serve:
    mdbook serve docs/

# Show help output from the built binary
help: build
    ./ralph help

# Run a quick smoke test (build + help + vet)
smoke: build vet
    ./ralph help
    @echo "Smoke test passed."
