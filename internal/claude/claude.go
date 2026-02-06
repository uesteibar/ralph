package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/shell"
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

	// Continue resumes the most recent conversation (passes --continue to Claude CLI).
	Continue bool

	// EventHandler receives structured events during stream processing.
	// If nil, events are silently discarded.
	EventHandler events.EventHandler
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
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "assistant":
			for _, content := range ev.Message.Content {
				if content.Type == "tool_use" {
					emitEvent(opts.EventHandler, events.ToolUse{
						Name:   content.Name,
						Detail: toolDetail(content.Name, content.Input, workDir),
					})
				} else if content.Type == "text" && content.Text != "" {
					emitEvent(opts.EventHandler, events.AgentText{Text: content.Text})
				}
			}
		case "result":
			result = ev.Result
			numTurns = ev.NumTurns
			durationMS = ev.DurationMS
		}
	}

	if numTurns > 0 {
		emitEvent(opts.EventHandler, events.InvocationDone{
			NumTurns:   numTurns,
			DurationMS: durationMS,
		})
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

	// Check for usage limit in the result text. The CLI exits with code 0
	// when hitting the subscription cap, so we detect it from the output.
	if ulErr := parseUsageLimit(result); ulErr != nil {
		return result, ulErr
	}

	return result, nil
}

// emitEvent sends an event to the handler if non-nil.
func emitEvent(h events.EventHandler, e events.Event) {
	if h != nil {
		h.Handle(e)
	}
}

// toolDetail extracts a human-readable detail string from tool input.
func toolDetail(name string, input map[string]any, workDir string) string {
	switch name {
	case "Read":
		if fp, ok := input["file_path"].(string); ok {
			return relativePath(fp, workDir)
		}
	case "Edit":
		if fp, ok := input["file_path"].(string); ok {
			return relativePath(fp, workDir)
		}
	case "Write":
		if fp, ok := input["file_path"].(string); ok {
			return relativePath(fp, workDir)
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 60 {
				cmd = cmd[:57] + "..."
			}
			return cmd
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			detail := fmt.Sprintf("%q", pattern)
			if path, ok := input["path"].(string); ok {
				detail += " in " + relativePath(path, workDir)
			}
			return detail
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	case "Task":
		if desc, ok := input["description"].(string); ok {
			return desc
		}
	}
	return ""
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

// ContainsComplete checks whether Claude's output contains the completion signal.
func ContainsComplete(output string) bool {
	return strings.Contains(output, completeSignal)
}

// UsageLimitError indicates Claude CLI exited because the subscription usage cap was reached.
type UsageLimitError struct {
	ResetAt time.Time
	Message string
}

func (e *UsageLimitError) Error() string {
	return fmt.Sprintf("usage limit reached (resets %s): %s", e.ResetAt.Format(time.RFC3339), e.Message)
}

// IsUsageLimitError returns true if the output text contains a Claude usage limit message.
func IsUsageLimitError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "you've hit your limit") ||
		strings.Contains(lower, "usage limit reached")
}

// resets Jan 2, 2026, 3pm (UTC)
// resets January 2, 2026, 3:04pm (UTC)
var resetsPattern = regexp.MustCompile(`resets\s+(\w+\s+\d{1,2},\s+\d{4},\s+\d{1,2}(?::\d{2})?(?:am|pm))\s+\(([^)]+)\)`)

// Your limit will reset at 1pm (Etc/GMT+5)
// Your limit will reset at 3:30pm (UTC)
var resetAtPattern = regexp.MustCompile(`reset at\s+(\d{1,2}(?::\d{2})?(?:am|pm))\s+\(([^)]+)\)`)

// parseUsageLimit checks output for a usage limit message and parses the reset time.
// Returns nil if the output does not contain a usage limit message.
func parseUsageLimit(output string) *UsageLimitError {
	if !IsUsageLimitError(output) {
		return nil
	}

	resetAt := parseResetTime(output)

	line := extractLimitLine(output)
	return &UsageLimitError{
		ResetAt: resetAt,
		Message: line,
	}
}

func parseResetTime(output string) time.Time {
	// Try "resets Jan 2, 2026, 3pm (UTC)" pattern
	if m := resetsPattern.FindStringSubmatch(output); m != nil {
		if t, err := parseDateTime(m[1], m[2]); err == nil {
			return t
		}
	}

	// Try "reset at 1pm (Etc/GMT+5)" pattern — time only, assume today or next occurrence
	if m := resetAtPattern.FindStringSubmatch(output); m != nil {
		if t, err := parseTimeOnly(m[1], m[2]); err == nil {
			return t
		}
	}

	// Fallback: could not parse, return 30 minutes from now
	return time.Now().Add(30 * time.Minute)
}

func parseDateTime(datetime, tzName string) (time.Time, error) {
	loc, err := loadLocation(tzName)
	if err != nil {
		return time.Time{}, err
	}

	// Try with minutes: "Jan 2, 2026, 3:04pm"
	if t, err := time.ParseInLocation("Jan 2, 2006, 3:04pm", datetime, loc); err == nil {
		return t, nil
	}
	// Try full month with minutes: "January 2, 2026, 3:04pm"
	if t, err := time.ParseInLocation("January 2, 2006, 3:04pm", datetime, loc); err == nil {
		return t, nil
	}
	// Try without minutes: "Jan 2, 2026, 3pm"
	if t, err := time.ParseInLocation("Jan 2, 2006, 3pm", datetime, loc); err == nil {
		return t, nil
	}
	// Try full month without minutes: "January 2, 2026, 3pm"
	if t, err := time.ParseInLocation("January 2, 2006, 3pm", datetime, loc); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("cannot parse datetime %q", datetime)
}

func parseTimeOnly(timeStr, tzName string) (time.Time, error) {
	loc, err := loadLocation(tzName)
	if err != nil {
		return time.Time{}, err
	}

	now := time.Now().In(loc)
	var parsed time.Time

	// Try with minutes: "3:04pm"
	if t, err := time.ParseInLocation("3:04pm", timeStr, loc); err == nil {
		parsed = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	} else if t, err := time.ParseInLocation("3pm", timeStr, loc); err == nil {
		// Without minutes: "3pm"
		parsed = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), 0, 0, 0, loc)
	} else {
		return time.Time{}, fmt.Errorf("cannot parse time %q", timeStr)
	}

	// If the parsed time is in the past, it means tomorrow
	if parsed.Before(now) {
		parsed = parsed.Add(24 * time.Hour)
	}

	return parsed, nil
}

func loadLocation(tzName string) (*time.Location, error) {
	if strings.EqualFold(tzName, "UTC") {
		return time.UTC, nil
	}
	return time.LoadLocation(tzName)
}

func extractLimitLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "hit your limit") || strings.Contains(lower, "usage limit") {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(output)
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

	if opts.Continue {
		args = append(args, "--continue")
	}

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
	}

	if opts.Prompt != "" && !opts.Print {
		args = append(args, "--system-prompt", opts.Prompt)
	}

	return args
}
