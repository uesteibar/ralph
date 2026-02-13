package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/workspace"
)

// Check handles the `ralph check <command>` subcommand.
func Check(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	code := checkRun(args, cwd, os.Stdout)
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// checkRun is the internal implementation for testability. It returns the exit code.
func checkRun(args []string, cwd string, w io.Writer) int {
	tail := 20

	// Parse --tail flag manually before the command args.
	cmdArgs := args
	if len(args) >= 2 && args[0] == "--tail" {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(w, "invalid --tail value: %s\n", args[1])
			return 1
		}
		tail = n
		cmdArgs = args[2:]
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintln(w, "usage: ralph check [--tail N] <command> [args...]")
		return 1
	}

	cmdStr := strings.Join(cmdArgs, " ")

	// Run the command via sh -c.
	start := time.Now()
	cmd := exec.Command("sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	// Determine log directory.
	logDir := resolveLogDir(cwd)
	if mkErr := os.MkdirAll(logDir, 0755); mkErr != nil {
		fmt.Fprintf(w, "error creating log directory: %v\n", mkErr)
		return 1
	}

	// Write full output to log file.
	logFile := filepath.Join(logDir, "check-"+sanitizeCmd(cmdStr)+".log")
	if wErr := os.WriteFile(logFile, output, 0644); wErr != nil {
		fmt.Fprintf(w, "error writing log file: %v\n", wErr)
		return 1
	}

	durationStr := formatDuration(duration)

	if err == nil {
		// Success.
		fmt.Fprintf(w, "PASS: %s (%s)\n", cmdStr, durationStr)
		fmt.Fprintf(w, "Full log: %s\n", logFile)
		return 0
	}

	// Failure — extract exit code.
	exitCode := 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}

	fmt.Fprintf(w, "FAIL: %s (%s)\n", cmdStr, durationStr)
	fmt.Fprintf(w, "--- last %d lines ---\n", tail)
	lines := strings.Split(string(output), "\n")
	// Remove trailing empty line from split.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "Full log: %s\n", logFile)
	return exitCode
}

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizeCmd replaces spaces and special characters with underscores for log filenames.
func sanitizeCmd(cmd string) string {
	return sanitizeRe.ReplaceAllString(cmd, "_")
}

// resolveLogDir determines the log directory based on workspace context.
func resolveLogDir(cwd string) string {
	if name, ok := workspace.DetectCurrent(cwd); ok {
		// Inside a workspace tree — find the workspace root.
		normalized := filepath.ToSlash(cwd)
		marker := ".ralph/workspaces/" + name + "/tree"
		idx := strings.Index(normalized, marker)
		if idx >= 0 {
			repoRoot := filepath.FromSlash(normalized[:idx])
			return filepath.Join(repoRoot, ".ralph", "workspaces", name, "logs")
		}
	}

	// Base context — use .ralph/logs/ relative to cwd.
	return filepath.Join(cwd, ".ralph", "logs")
}

// formatDuration formats a duration as seconds with two decimal places.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}
