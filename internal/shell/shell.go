package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExitError wraps a non-zero exit from a subprocess.
type ExitError struct {
	Code   int
	Stderr string
	Cmd    string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s exited with code %d: %s", e.Cmd, e.Code, e.Stderr)
}

// Runner executes shell commands with a shared working directory and environment.
type Runner struct {
	Dir string
	Env []string
}

// Run executes a command and returns its stdout. Stderr is captured and
// included in the error on non-zero exit.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), &ExitError{
				Code:   exitErr.ExitCode(),
				Stderr: strings.TrimSpace(stderr.String()),
				Cmd:    name + " " + strings.Join(args, " "),
			}
		}
		return "", fmt.Errorf("running %s: %w", name, err)
	}

	return stdout.String(), nil
}

// RunInteractive executes a command with stdin/stdout/stderr connected to the
// terminal. Used for interactive sessions (e.g., claude chat).
func (r *Runner) RunInteractive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExitError{
				Code: exitErr.ExitCode(),
				Cmd:  name + " " + strings.Join(args, " "),
			}
		}
		return fmt.Errorf("running %s: %w", name, err)
	}
	return nil
}

// RunWithStdin executes a command, piping the given string to stdin, and
// returns stdout.
func (r *Runner) RunWithStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ()
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), &ExitError{
				Code:   exitErr.ExitCode(),
				Stderr: strings.TrimSpace(stderr.String()),
				Cmd:    name + " " + strings.Join(args, " "),
			}
		}
		return "", fmt.Errorf("running %s: %w", name, err)
	}

	return stdout.String(), nil
}

func (r *Runner) environ() []string {
	if len(r.Env) == 0 {
		return nil // inherit parent
	}
	return append(os.Environ(), r.Env...)
}
