package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/shell"
)

var (
	// Styles for progress output
	arrowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))            // cyan
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true) // blue bold
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // gray
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // light gray
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // dim gray
)

const completeSignal = "<promise>COMPLETE</promise>"

// InvokeOpts configures a Claude CLI invocation.
type InvokeOpts struct {
	// Prompt is piped to Claude's stdin.
	Prompt string

	// Dir is the working directory for the Claude process.
	Dir string

	// Print runs Claude in non-interactive --print mode and captures output.
	Print bool

	// Interactive connects stdin/stdout for a live session.
	Interactive bool

	// MaxTurns limits the number of agentic turns (--max-turns flag).
	MaxTurns int

	// Verbose enables debug logging (passes --verbose to Claude CLI).
	Verbose bool
}

// Invoke runs the Claude CLI with the given options.
// In Print mode it streams progress and returns Claude's output.
// In Interactive mode it blocks until the session ends and returns empty string.
func Invoke(ctx context.Context, opts InvokeOpts) (string, error) {
	r := &shell.Runner{Dir: opts.Dir}

	if opts.Interactive {
		args := buildArgs(opts)
		return "", r.RunInteractive(ctx, "claude", args...)
	}

	// Use stream-json for real-time progress
	return runWithStreamJSON(ctx, opts)
}

// streamEvent represents a JSON event from Claude CLI stream-json output.
type streamEvent struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype,omitempty"`
	Result     string `json:"result,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	NumTurns   int    `json:"num_turns,omitempty"`
	Message    struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text,omitempty"`
			Name  string         `json:"name,omitempty"`
			Input map[string]any `json:"input,omitempty"`
		} `json:"content,omitempty"`
	} `json:"message,omitempty"`
}

// runWithStreamJSON runs Claude with --output-format stream-json and displays progress.
func runWithStreamJSON(ctx context.Context, opts InvokeOpts) (string, error) {
	args := []string{
		"--dangerously-skip-permissions",
		"--print",
		"--output-format", "stream-json",
		"--verbose",
	}

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
	}

	// Get absolute working dir for relative path calculation
	workDir := opts.Dir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.Dir
	cmd.Stdin = strings.NewReader(opts.Prompt)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting claude: %w", err)
	}

	var result string
	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var numTurns int
	var durationMS int

	for scanner.Scan() {
		line := scanner.Text()
		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "assistant":
			for _, content := range event.Message.Content {
				if content.Type == "tool_use" {
					printToolUse(content.Name, content.Input, workDir)
				} else if content.Type == "text" && content.Text != "" {
					printText(content.Text)
				}
			}
		case "result":
			result = event.Result
			numTurns = event.NumTurns
			durationMS = event.DurationMS
		}
	}

	if numTurns > 0 {
		printDone(numTurns, durationMS)
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return result, &shell.ExitError{
				Code: exitErr.ExitCode(),
				Cmd:  "claude",
			}
		}
		return result, fmt.Errorf("running claude: %w", err)
	}

	return result, nil
}

func printToolUse(name string, input map[string]any, workDir string) {
	detail := ""
	switch name {
	case "Read":
		if fp, ok := input["file_path"].(string); ok {
			detail = relativePath(fp, workDir)
		}
	case "Edit":
		if fp, ok := input["file_path"].(string); ok {
			detail = relativePath(fp, workDir)
		}
	case "Write":
		if fp, ok := input["file_path"].(string); ok {
			detail = relativePath(fp, workDir)
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			// Truncate long commands
			if len(cmd) > 60 {
				cmd = cmd[:57] + "..."
			}
			detail = cmd
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			detail = fmt.Sprintf("%q", pattern)
			if path, ok := input["path"].(string); ok {
				detail += " in " + relativePath(path, workDir)
			}
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			detail = pattern
		}
	case "Task":
		if desc, ok := input["description"].(string); ok {
			detail = desc
		}
	}

	arrow := arrowStyle.Render("→")
	tool := toolStyle.Render(name)
	if detail != "" {
		path := pathStyle.Render(detail)
		fmt.Fprintf(os.Stderr, "  %s %s %s\n", arrow, tool, path)
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", arrow, tool)
	}
}

func relativePath(path, workDir string) string {
	if workDir == "" {
		return path
	}
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return path
	}
	return rel
}

func printText(text string) {
	// Print Claude's text responses with indentation
	lines := strings.Split(strings.TrimSpace(text), "\n")
	fmt.Fprintln(os.Stderr)
	for _, line := range lines {
		styled := textStyle.Render(line)
		fmt.Fprintf(os.Stderr, "  %s\n", styled)
	}
	fmt.Fprintln(os.Stderr)
}

func printDone(numTurns int, durationMS int) {
	durationSec := durationMS / 1000
	check := successStyle.Render("✓")
	info := dimStyle.Render(fmt.Sprintf("(%d turns, %ds)", numTurns, durationSec))
	fmt.Fprintf(os.Stderr, "  %s Done %s\n", check, info)
}

// ContainsComplete checks whether Claude's output contains the completion signal.
func ContainsComplete(output string) bool {
	return strings.Contains(output, completeSignal)
}

func buildArgs(opts InvokeOpts) []string {
	var args []string

	args = append(args, "--dangerously-skip-permissions")

	if opts.Print {
		args = append(args, "--print")
	}

	if opts.Verbose {
		args = append(args, "--verbose")
	}

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
	}

	if opts.Prompt != "" && !opts.Print {
		args = append(args, "--system-prompt", opts.Prompt)
	}

	return args
}
