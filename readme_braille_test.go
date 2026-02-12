package main

import (
	"os"
	"strings"
	"testing"
)

func TestReadme_ContainsBrailleArt(t *testing.T) {
	content, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	t.Run("first line is heading", func(t *testing.T) {
		if lines[0] != "# Ralph" {
			t.Errorf("first line should be '# Ralph', got %q", lines[0])
		}
	})

	t.Run("contains fenced code block with Braille art", func(t *testing.T) {
		if !strings.Contains(text, "```\n⠀⠀⠀⠀⠀⠀⣀⣤⣶⡶⢛⠟⡿⠻⢻⢿⢶⢦⣄⡀") {
			t.Error("README should contain the Braille art inside a fenced code block")
		}
	})

	t.Run("Braille art has 26 lines", func(t *testing.T) {
		firstLine := "⠀⠀⠀⠀⠀⠀⣀⣤⣶⡶⢛⠟⡿⠻⢻⢿⢶⢦⣄⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀"
		lastLine := "⠀⠀⠀⠀⠀⠉⠛⠲⠤⠤⢤⣤⣄⣀⣀⣀⣀⡸⠇⠀⠀⠀⠉⠉⠉⠉⠉⠉⠁⠀"

		firstIdx := -1
		lastIdx := -1
		for i, line := range lines {
			if strings.TrimRight(line, " \t\r") == firstLine {
				firstIdx = i
			}
			if strings.TrimRight(line, " \t\r") == lastLine {
				lastIdx = i
			}
		}

		if firstIdx == -1 {
			t.Fatal("could not find first line of Braille art")
		}
		if lastIdx == -1 {
			t.Fatal("could not find last line of Braille art")
		}

		artLines := lastIdx - firstIdx + 1
		if artLines != 26 {
			t.Errorf("Braille art should have 26 lines, got %d", artLines)
		}
	})

	t.Run("art appears between heading and disclaimer", func(t *testing.T) {
		headingIdx := -1
		artIdx := -1
		disclaimerIdx := -1

		for i, line := range lines {
			if line == "# Ralph" && headingIdx == -1 {
				headingIdx = i
			}
			if strings.Contains(line, "⠀⠀⠀⠀⠀⠀⣀⣤⣶⡶⢛⠟⡿⠻⢻⢿⢶⢦⣄⡀") && artIdx == -1 {
				artIdx = i
			}
			if strings.HasPrefix(line, "> **Note:**") && disclaimerIdx == -1 {
				disclaimerIdx = i
			}
		}

		if headingIdx == -1 {
			t.Fatal("could not find '# Ralph' heading")
		}
		if artIdx == -1 {
			t.Fatal("could not find Braille art")
		}
		if disclaimerIdx == -1 {
			t.Fatal("could not find '> **Note:**' disclaimer")
		}

		if artIdx <= headingIdx {
			t.Errorf("Braille art (line %d) should appear after heading (line %d)", artIdx, headingIdx)
		}
		if disclaimerIdx <= artIdx {
			t.Errorf("disclaimer (line %d) should appear after Braille art (line %d)", disclaimerIdx, artIdx)
		}
	})

	t.Run("rest of README content is preserved", func(t *testing.T) {
		expectedSections := []string{
			"## Table of Contents",
			"## How It Works",
			"## Prerequisites",
			"## Installation",
			"## Quick Start",
			"## Commands",
			"## Configuration",
			"## Architecture",
			"## Development",
		}
		for _, section := range expectedSections {
			if !strings.Contains(text, section) {
				t.Errorf("README should still contain %q", section)
			}
		}
	})
}
