package commands

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_LocalFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no flag defaults to false",
			args:     []string{},
			expected: false,
		},
		{
			name:     "--local sets to true",
			args:     []string{"--local"},
			expected: true,
		},
		{
			name:     "--local=true sets to true",
			args:     []string{"--local=true"},
			expected: true,
		},
		{
			name:     "--local=false sets to false",
			args:     []string{"--local=false"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			local := fs.Bool("local", false, "Run loop in current directory without creating a worktree")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if *local != tt.expected {
				t.Errorf("expected local=%v, got %v", tt.expected, *local)
			}
		})
	}
}

func TestWorktreeRoot_DetectsWorktreePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
		root     string
	}{
		{
			name:     "not in worktree",
			path:     "/home/user/project",
			expected: false,
			root:     "",
		},
		{
			name:     "inside worktree",
			path:     "/home/user/project/.ralph/worktrees/my-branch",
			expected: true,
			root:     "/home/user/project/.ralph/worktrees/my-branch",
		},
		{
			name:     "deep inside worktree",
			path:     "/home/user/project/.ralph/worktrees/my-branch/src/pkg",
			expected: true,
			root:     "/home/user/project/.ralph/worktrees/my-branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, ok := worktreeRoot(tt.path)
			if ok != tt.expected {
				t.Errorf("worktreeRoot(%q): expected ok=%v, got %v", tt.path, tt.expected, ok)
			}
			if ok && root != tt.root {
				t.Errorf("worktreeRoot(%q): expected root=%q, got %q", tt.path, tt.root, root)
			}
		})
	}
}

func TestRunLocal_FailsWhenPRDDoesNotExist(t *testing.T) {
	// Create a temp directory with a valid config but no PRD
	tmpDir := t.TempDir()

	// Create .ralph/ralph.yaml
	ralphDir := filepath.Join(tmpDir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0755); err != nil {
		t.Fatalf("failed to create .ralph dir: %v", err)
	}

	configContent := `project: test-project
repo:
  path: .
  default_base: main
`
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Run with --local flag
	err = Run([]string{"--local"})

	// Should fail because PRD doesn't exist
	if err == nil {
		t.Fatal("expected error when PRD does not exist, got nil")
	}

	// Error message should mention PRD not found
	errMsg := err.Error()
	if !strings.Contains(errMsg, "PRD not found") {
		t.Errorf("expected error to mention 'PRD not found', got: %v", errMsg)
	}
}

func TestRunLocal_PRDExistenceCheck(t *testing.T) {
	// Create temp directory with config and PRD
	tmpDir := t.TempDir()

	// Create .ralph/ralph.yaml
	ralphDir := filepath.Join(tmpDir, ".ralph")
	stateDir := filepath.Join(ralphDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	configContent := `project: test-project
repo:
  path: .
  default_base: main
`
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Create a valid PRD
	prdContent := `{
  "project": "test",
  "branchName": "test/feature",
  "description": "Test PRD",
  "userStories": [
    {
      "id": "US-001",
      "title": "Test story",
      "description": "As a user, I want something",
      "acceptanceCriteria": ["Works"],
      "priority": 1,
      "passes": false,
      "notes": ""
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(stateDir, "prd.json"), []byte(prdContent), 0644); err != nil {
		t.Fatalf("failed to write PRD: %v", err)
	}

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Run with --local flag - it should NOT fail with "PRD not found"
	// It may fail later (e.g., when trying to invoke Claude) but that's OK for this test
	err = Run([]string{"--local", "--max-iterations=0"})

	// If there's an error, it should NOT be about PRD not found
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "PRD not found") {
			t.Errorf("unexpected 'PRD not found' error when PRD exists: %v", errMsg)
		}
		// Other errors are expected (e.g., claude not available)
		// This test just verifies the PRD existence check works
	}
}
