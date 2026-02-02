package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/config"
)

// invokeClaudeFn is the function used to invoke Claude CLI. It can be
// overridden in tests to avoid calling the real CLI.
var invokeClaudeFn = claude.Invoke

const qualityCheckPrompt = `Analyze this codebase and detect the quality check commands that should be run (tests, linting, type checking, formatting, etc.).

Output ONLY a YAML list of shell commands, one per line. No explanation, no code fences, no surrounding text. Example format:
- "npm test"
- "npm run lint"
`

const defaultConfigTemplate = `project: %s

repo:
  default_base: main
  branch_pattern: "^ralph/[a-zA-Z0-9._-]+$"

paths:
  tasks_dir: ".ralph/tasks"
  skills_dir: ".ralph/skills"

quality_checks:
  - "npm test"
  # - "npm run lint"
`

const finishSkillContent = `Take the plan we have discussed and agreed upon in this conversation and structure it into a PRD JSON file.

## Output Format

Write a valid JSON file to ` + "`.ralph/state/prd.json`" + ` with this exact schema:

` + "```json" + `
{
  "project": "<project name from .ralph/ralph.yaml>",
  "branchName": "ralph/<feature-name-kebab-case>",
  "description": "<one-line description of the feature>",
  "userStories": [
    {
      "id": "US-001",
      "title": "<short story title>",
      "description": "As a <user>, I want <feature> so that <benefit>",
      "acceptanceCriteria": [
        "Specific verifiable criterion",
        "All quality checks pass"
      ],
      "priority": 1,
      "passes": false,
      "notes": ""
    }
  ]
}
` + "```" + `

## Story Rules

- Each story must be completable in ONE context window (one Ralph iteration)
- Order by dependency: schema/data first, then backend logic, then UI
- Acceptance criteria must be specific and verifiable
- Include "All quality checks pass" in every story's acceptance criteria
- All stories start with ` + "`passes: false`" + `
- Priority determines execution order (1 = first)

## After Writing

1. Read back the file to confirm it is valid JSON
2. Tell the user the PRD is ready and suggest: ` + "`ralph run`" + `
`

// Init scaffolds the .ralph/ directory in the current project.
// It is idempotent: re-running it skips existing files and ensures
// all directories and skills are up to date.
// The in parameter provides stdin for interactive prompts.
func Init(args []string, in io.Reader) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	scanner := bufio.NewScanner(in)

	ralphDir := filepath.Join(cwd, ".ralph")
	stateDir := filepath.Join(ralphDir, "state")
	configPath := filepath.Join(ralphDir, "ralph.yaml")
	progressPath := filepath.Join(ralphDir, "progress.txt")
	finishSkillPath := filepath.Join(cwd, ".claude", "commands", "finish.md")

	var created, skipped []string

	// --- Git tracking prompt ---
	gitTrackChoice := promptGitTracking(scanner)

	// --- LLM analysis prompt ---
	useLLM := promptLLMAnalysis(scanner)

	// Create directory structure (MkdirAll is idempotent)
	dirs := []string{
		ralphDir,
		filepath.Join(ralphDir, "tasks"),
		filepath.Join(ralphDir, "skills"),
		stateDir,
		filepath.Join(stateDir, "archive"),
		filepath.Join(cwd, ".claude", "commands"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Write config (only if it doesn't exist)
	if _, err := os.Stat(configPath); err != nil {
		projectName := filepath.Base(cwd)

		var qualityChecks []string
		if useLLM {
			detected, llmErr := detectQualityChecks(cwd)
			if llmErr != nil {
				fmt.Printf("Warning: Claude analysis failed (%v), using default template.\n", llmErr)
			} else {
				qualityChecks = detected
			}
		}

		var content string
		if len(qualityChecks) > 0 {
			content = buildConfigWithChecks(projectName, qualityChecks)
		} else {
			content = fmt.Sprintf(defaultConfigTemplate, projectName)
		}

		var cfg config.Config
		if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
			return fmt.Errorf("generated config is invalid (bug): %w", err)
		}

		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		created = append(created, ".ralph/ralph.yaml")
	} else {
		skipped = append(skipped, ".ralph/ralph.yaml (already exists)")
	}

	// Write progress.txt (only if it doesn't exist)
	if _, err := os.Stat(progressPath); err != nil {
		if err := os.WriteFile(progressPath, []byte("# Ralph Progress Log\n\n## Codebase Patterns\n\n---\n"), 0644); err != nil {
			return fmt.Errorf("writing progress.txt: %w", err)
		}
		created = append(created, ".ralph/progress.txt")
	} else {
		skipped = append(skipped, ".ralph/progress.txt (already exists)")
	}

	// Write .gitkeep files (idempotent)
	for _, dir := range []string{"tasks", "skills"} {
		keepPath := filepath.Join(ralphDir, dir, ".gitkeep")
		if err := os.WriteFile(keepPath, []byte(""), 0644); err != nil {
			return fmt.Errorf("writing .gitkeep: %w", err)
		}
	}

	// Install /finish Claude skill (always write to keep up to date)
	if err := os.WriteFile(finishSkillPath, []byte(finishSkillContent), 0644); err != nil {
		return fmt.Errorf("writing finish skill: %w", err)
	}
	created = append(created, ".claude/commands/finish.md")

	// Ensure appropriate paths are in .gitignore based on user's choice
	ensureGitignoreEntries(cwd, gitTrackChoice)

	log.Printf("[init] initialized .ralph/ in %s", cwd)
	fmt.Println("Ralph initialized.")
	if len(created) > 0 {
		fmt.Println()
		fmt.Println("Created:")
		for _, c := range created {
			fmt.Printf("  %s\n", c)
		}
	}
	if len(skipped) > 0 {
		fmt.Println()
		fmt.Println("Skipped:")
		for _, s := range skipped {
			fmt.Printf("  %s\n", s)
		}
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit .ralph/ralph.yaml (set quality checks, project details)")
	fmt.Println("  2. ralph prd new        (interactive PRD creation)")
	fmt.Println("  3. ralph run            (execute the loop from staged PRD)")

	return nil
}

// promptGitTracking asks the user how to handle .ralph/ in git.
// Returns 1 for "track in git" or 2 for "keep local".
func promptGitTracking(scanner *bufio.Scanner) int {
	fmt.Println("How should .ralph/ be handled in git?")
	fmt.Println()
	fmt.Println("  1) Track in git — share config with your team")
	fmt.Println("  2) Keep local  — gitignore the entire .ralph/ directory")
	fmt.Println()

	for {
		fmt.Print("Choose [1/2]: ")
		if !scanner.Scan() {
			return 1
		}
		choice := trimSpace(scanner.Text())
		if choice == "1" {
			return 1
		}
		if choice == "2" {
			return 2
		}
		fmt.Println("Please enter 1 or 2.")
	}
}

// promptLLMAnalysis asks whether to use Claude for quality check detection.
// Returns true if the user accepts (default yes).
func promptLLMAnalysis(scanner *bufio.Scanner) bool {
	fmt.Print("Use Claude to detect quality checks? [Y/n] ")
	if !scanner.Scan() {
		return true
	}
	answer := trimSpace(scanner.Text())
	if answer == "" || answer == "Y" || answer == "y" || answer == "yes" || answer == "Yes" {
		return true
	}
	return false
}

func ensureGitignoreEntries(dir string, gitTrackChoice int) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	content, _ := os.ReadFile(gitignorePath)

	var entries []string
	if gitTrackChoice == 2 {
		entries = []string{".ralph/"}
	} else {
		entries = []string{".ralph/worktrees/", ".ralph/state/"}
	}

	var toAdd []string
	existing := string(content)
	for _, entry := range entries {
		if !containsLine(existing, entry) {
			toAdd = append(toAdd, entry)
		}
	}

	if len(toAdd) == 0 {
		return
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Add a newline separator if file doesn't end with one
	if len(content) > 0 && content[len(content)-1] != '\n' {
		f.WriteString("\n")
	}

	f.WriteString("\n# Ralph\n")
	for _, entry := range toAdd {
		f.WriteString(entry + "\n")
	}
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if trimSpace(l) == line {
			return true
		}
	}
	return false
}

// String helpers to avoid importing strings (keeping deps minimal within commands).
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// detectQualityChecks invokes Claude CLI to analyze the codebase and return
// a list of quality check commands.
func detectQualityChecks(dir string) ([]string, error) {
	fmt.Println("Analyzing the codebase...")

	ctx := context.Background()
	output, err := invokeClaudeFn(ctx, claude.InvokeOpts{
		Prompt:   qualityCheckPrompt,
		Dir:      dir,
		Print:    true,
		MaxTurns: 3,
	})
	if err != nil {
		return nil, err
	}
	return parseQualityChecks(output)
}

// parseQualityChecks parses Claude's YAML list output into a string slice.
func parseQualityChecks(output string) ([]string, error) {
	var checks []string
	if err := yaml.Unmarshal([]byte(output), &checks); err != nil {
		return nil, fmt.Errorf("parsing Claude output as YAML list: %w", err)
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("Claude returned no quality checks")
	}
	return checks, nil
}

// buildConfigWithChecks generates ralph.yaml content with the given quality checks.
func buildConfigWithChecks(projectName string, checks []string) string {
	cfg := config.Config{
		Project: projectName,
		Repo: config.RepoConfig{
			DefaultBase:   "main",
			BranchPattern: "^ralph/[a-zA-Z0-9._-]+$",
		},
		Paths: config.PathsConfig{
			TasksDir:  ".ralph/tasks",
			SkillsDir: ".ralph/skills",
		},
		QualityChecks: checks,
	}
	out, _ := yaml.Marshal(&cfg)
	return string(out)
}
