package claude

import (
	"context"
	"strconv"
	"strings"

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

	// Verbose streams Claude's output to stdout in real-time while also
	// capturing it for return. Only applies when Print is true.
	Verbose bool
}

// Invoke runs the Claude CLI with the given options.
// In Print mode it returns Claude's output. In Interactive mode it blocks
// until the session ends and returns empty string.
func Invoke(ctx context.Context, opts InvokeOpts) (string, error) {
	r := &shell.Runner{Dir: opts.Dir}

	args := buildArgs(opts)

	if opts.Interactive {
		return "", r.RunInteractive(ctx, "claude", args...)
	}

	if opts.Verbose {
		return r.RunWithStdinStreaming(ctx, opts.Prompt, "claude", args...)
	}

	return r.RunWithStdin(ctx, opts.Prompt, "claude", args...)
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

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
	}

	if opts.Prompt != "" && !opts.Print {
		args = append(args, "--system-prompt", opts.Prompt)
	}

	return args
}
