package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
)

const seedReadme = `# Knowledge Base

This directory contains learnings, patterns, and insights discovered by Ralph's AI agents during development.

## File Naming Convention

Use descriptive kebab-case names with a topic prefix:

- ` + "`testing-flaky-ci.md`" + `
- ` + "`go-error-handling.md`" + `
- ` + "`react-state-patterns.md`" + `

## Tagging Format

Add a Tags line at the top of each file to enable search:

` + "```" + `
## Tags: testing, ci, go
` + "```" + `

## Usage

AI agents search this directory for relevant context before starting work and write new files when they discover patterns, fix recurring mistakes, or learn from feedback.
`

// Dir returns the knowledge base directory path: <repoPath>/.ralph/knowledge/
func Dir(repoPath string) string {
	return filepath.Join(repoPath, ".ralph", "knowledge")
}

// EnsureDir creates the knowledge base directory if it doesn't exist.
func EnsureDir(repoPath string) error {
	dir := Dir(repoPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating knowledge directory %s: %w", dir, err)
	}
	return nil
}

// SeedReadme writes a README.md into the knowledge directory if one doesn't
// already exist. It is safe to call multiple times.
func SeedReadme(repoPath string) error {
	readmePath := filepath.Join(Dir(repoPath), "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		return nil // already exists
	}
	if err := os.WriteFile(readmePath, []byte(seedReadme), 0644); err != nil {
		return fmt.Errorf("writing knowledge README: %w", err)
	}
	return nil
}
