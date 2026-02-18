package ccusage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParse_UsageLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []UsageGroup
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "single section with one line",
			input: "Claude Code Usage Statistics\n────────────────────────────────────────────────────────────\n5-hour         [███████████░░░░░░░░░]  56%  resets in 3h 8m\n",
			want: []UsageGroup{
				{
					GroupLabel: "Claude Code Usage Statistics",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 56, ResetTime: "3h 8m"},
					},
				},
			},
		},
		{
			name: "single section with multiple lines",
			input: `Claude Code Usage Statistics
────────────────────────────────────────────────────────────
5-hour         [███████████░░░░░░░░░]  56%  resets in 3h 8m
7-day          [████████████████░░░░]  83%  resets in 22h 8m
7-day Sonnet   [██░░░░░░░░░░░░░░░░░░]  10%  resets in 22h 8m
`,
			want: []UsageGroup{
				{
					GroupLabel: "Claude Code Usage Statistics",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 56, ResetTime: "3h 8m"},
						{Label: "7-day", Percentage: 83, ResetTime: "22h 8m"},
						{Label: "7-day Sonnet", Percentage: 10, ResetTime: "22h 8m"},
					},
				},
			},
		},
		{
			name: "multiple sections",
			input: `Claude Code Usage Statistics
────────────────────────────────────────────────────────────
5-hour         [███████████░░░░░░░░░]  56%  resets in 3h 8m
7-day          [████████████████░░░░]  83%  resets in 22h 8m
7-day Sonnet   [██░░░░░░░░░░░░░░░░░░]  10%  resets in 22h 8m


Codex Usage Limits (Plan: Free)
────────────────────────────────────────────────────────────
7-day          [███░░░░░░░░░░░░░░░░░]  18%  resets in 20h 59m
`,
			want: []UsageGroup{
				{
					GroupLabel: "Claude Code Usage Statistics",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 56, ResetTime: "3h 8m"},
						{Label: "7-day", Percentage: 83, ResetTime: "22h 8m"},
						{Label: "7-day Sonnet", Percentage: 10, ResetTime: "22h 8m"},
					},
				},
				{
					GroupLabel: "Codex Usage Limits (Plan: Free)",
					Lines: []UsageLine{
						{Label: "7-day", Percentage: 18, ResetTime: "20h 59m"},
					},
				},
			},
		},
		{
			name:  "zero percent",
			input: "Stats\n──────\n5-hour         [░░░░░░░░░░░░░░░░░░░░]  0%  resets in 5h 0m\n",
			want: []UsageGroup{
				{
					GroupLabel: "Stats",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 0, ResetTime: "5h 0m"},
					},
				},
			},
		},
		{
			name:  "100 percent",
			input: "Stats\n──────\n5-hour         [████████████████████]  100%  resets in 1m\n",
			want: []UsageGroup{
				{
					GroupLabel: "Stats",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 100, ResetTime: "1m"},
					},
				},
			},
		},
		{
			name:  "usage lines without section header",
			input: "5-hour         [##########..........]  50%  resets in 3h 13m\n",
			want: []UsageGroup{
				{
					GroupLabel: "",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 50, ResetTime: "3h 13m"},
					},
				},
			},
		},
		{
			name:  "separator only lines ignored",
			input: "────────────────────────────────────────\n",
			want:  nil,
		},
		{
			name:  "dashes as separator",
			input: "---\n",
			want:  nil,
		},
		{
			name:  "equals as separator",
			input: "====\n",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.input)
			assertUsageGroupsEqual(t, tt.want, got)
		})
	}
}

func TestPoller_Current_ReturnsNilInitially(t *testing.T) {
	p := NewPoller("nonexistent-binary-xxxxx", time.Minute, slog.Default())
	if got := p.Current(); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestPoller_Start_BinaryNotFound_ReturnsImmediately(t *testing.T) {
	p := NewPoller("nonexistent-binary-xxxxx", time.Minute, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Start returned immediately — correct behavior.
	case <-ctx.Done():
		t.Fatal("Start did not return immediately when binary not found")
	}

	if got := p.Current(); got != nil {
		t.Errorf("expected nil after binary-not-found, got %v", got)
	}
}

func TestPoller_Start_PollsAndCachesResult(t *testing.T) {
	script := writeMockScript(t, `Claude Code Usage Statistics
────────────────────────────────────────────────────────────
5-hour         [##########..........]  50%  resets in 3h 13m
`)

	p := NewPoller(script, time.Hour, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	// Wait for the initial poll to populate results.
	deadline := time.After(5 * time.Second)
	for {
		if result := p.Current(); result != nil {
			cancel()
			<-done
			assertUsageGroupsEqual(t, []UsageGroup{
				{
					GroupLabel: "Claude Code Usage Statistics",
					Lines: []UsageLine{
						{Label: "5-hour", Percentage: 50, ResetTime: "3h 13m"},
					},
				},
			}, result)
			return
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for poller to populate results")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestPoller_Start_ExecutionFailure_KeepsLastResult(t *testing.T) {
	dir := t.TempDir()

	// First: a script that succeeds.
	goodScript := writeScriptAt(t, dir, "good", `Claude Code Usage Statistics
──────
5-hour         [##########..........]  50%  resets in 3h 13m
`)

	p := NewPoller(goodScript, time.Hour, slog.Default())

	// Manually poll to seed a good result.
	p.poll()
	if got := p.Current(); got == nil {
		t.Fatal("expected non-nil result after successful poll")
	}

	// Now replace binary with a failing one.
	p.binary = writeFailScript(t, dir, "bad")
	p.poll()

	// Should still return the previous good result.
	got := p.Current()
	if got == nil {
		t.Fatal("expected previous result to be retained after failure")
	}
	if got[0].Lines[0].Percentage != 50 {
		t.Errorf("expected percentage 50, got %d", got[0].Lines[0].Percentage)
	}
}

func TestPoller_Start_RespectsContextCancellation(t *testing.T) {
	script := writeMockScript(t, "Stats\n──────\n5-hour         [##]  10%  resets in 1h\n")

	p := NewPoller(script, time.Hour, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	// Wait for the initial poll to complete, then cancel.
	deadline := time.After(10 * time.Second)
	for {
		if p.Current() != nil {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for initial poll")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	cancel()

	select {
	case <-done:
		// Exited cleanly.
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not exit after context cancellation")
	}
}

// writeMockScript creates a temporary executable script that prints the given output.
func writeMockScript(t *testing.T, output string) string {
	t.Helper()
	return writeScriptAt(t, t.TempDir(), "mock-ccstats", output)
}

func writeFailScript(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fail script: %v", err)
	}
	return path
}

func writeScriptAt(t *testing.T, dir, name, output string) string {
	t.Helper()
	var script string
	var ext string

	if runtime.GOOS == "windows" {
		ext = ".bat"
		script = fmt.Sprintf("@echo off\necho %s\n", output)
	} else {
		ext = ""
		script = fmt.Sprintf("#!/bin/sh\ncat <<'CCSTATS_EOF'\n%sCCSTATS_EOF\n", output)
	}

	path := filepath.Join(dir, name+ext)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing mock script: %v", err)
	}
	return path
}

func assertUsageGroupsEqual(t *testing.T, want, got []UsageGroup) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("group count: want %d, got %d\nwant: %+v\ngot:  %+v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i].GroupLabel != got[i].GroupLabel {
			t.Errorf("group[%d].GroupLabel: want %q, got %q", i, want[i].GroupLabel, got[i].GroupLabel)
		}
		if len(want[i].Lines) != len(got[i].Lines) {
			t.Fatalf("group[%d].Lines count: want %d, got %d\nwant: %+v\ngot:  %+v",
				i, len(want[i].Lines), len(got[i].Lines), want[i].Lines, got[i].Lines)
		}
		for j := range want[i].Lines {
			wl := want[i].Lines[j]
			gl := got[i].Lines[j]
			if wl.Label != gl.Label {
				t.Errorf("group[%d].line[%d].Label: want %q, got %q", i, j, wl.Label, gl.Label)
			}
			if wl.Percentage != gl.Percentage {
				t.Errorf("group[%d].line[%d].Percentage: want %d, got %d", i, j, wl.Percentage, gl.Percentage)
			}
			if wl.ResetTime != gl.ResetTime {
				t.Errorf("group[%d].line[%d].ResetTime: want %q, got %q", i, j, wl.ResetTime, gl.ResetTime)
			}
		}
	}
}
