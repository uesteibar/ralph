package commands

import (
	"flag"
	"testing"
)

func TestChat_ContinueFlagParsing(t *testing.T) {
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
			name:     "--continue sets to true",
			args:     []string{"--continue"},
			expected: true,
		},
		{
			name:     "--continue=true sets to true",
			args:     []string{"--continue=true"},
			expected: true,
		},
		{
			name:     "--continue=false sets to false",
			args:     []string{"--continue=false"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("chat", flag.ContinueOnError)
			continueFlag := fs.Bool("continue", false, "Resume the most recent conversation")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if *continueFlag != tt.expected {
				t.Errorf("expected continue=%v, got %v", tt.expected, *continueFlag)
			}
		})
	}
}
