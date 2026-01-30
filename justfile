# Ralph — development tasks

# Default: show available recipes
default:
    @just --list

# Build the ralph binary
build:
    go build -o ralph ./cmd/ralph

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

# Run build + test + vet
check: build test vet

# Format all Go files
fmt:
    gofmt -w .

# Install the binary to $GOPATH/bin
install:
    go install ./cmd/ralph

# Clean build artifacts
clean:
    rm -f ralph
    go clean ./...

# Show help output from the built binary
help: build
    ./ralph help

# Run a quick smoke test (build + help + vet)
smoke: build vet
    ./ralph help
    @echo "Smoke test passed."
