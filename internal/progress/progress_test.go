package progress

import (
	"fmt"
	"strings"
	"testing"
)

func TestCapProgressEntries_KeepsLastNEntries(t *testing.T) {
	content := buildProgress(10)

	result := CapProgressEntries(content, 5)

	// Should contain entries S6-S10 (last 5)
	for i := 6; i <= 10; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d", i)
		if !strings.Contains(result, marker) {
			t.Errorf("expected result to contain %q", marker)
		}
	}

	// Should NOT contain entries S1-S5
	for i := 1; i <= 5; i++ {
		// Use trailing newline to avoid "S1" matching "S10"
		marker := fmt.Sprintf("## 2026-02-20 - S%d\n", i)
		if strings.Contains(result, marker) {
			t.Errorf("expected result to NOT contain %q", marker)
		}
	}
}

func TestCapProgressEntries_PreservesHeaderAndCodebasePatterns(t *testing.T) {
	content := buildProgress(10)

	result := CapProgressEntries(content, 5)

	if !strings.Contains(result, "# Ralph Progress Log") {
		t.Error("expected result to contain header")
	}
	if !strings.Contains(result, "## Codebase Patterns") {
		t.Error("expected result to contain Codebase Patterns section")
	}
	if !strings.Contains(result, "Pattern one") {
		t.Error("expected result to contain Codebase Patterns content")
	}
}

func TestCapProgressEntries_ReturnsAllWhenFewerThanMax(t *testing.T) {
	content := buildProgress(3)

	result := CapProgressEntries(content, 5)

	for i := 1; i <= 3; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d", i)
		if !strings.Contains(result, marker) {
			t.Errorf("expected result to contain %q", marker)
		}
	}
}

func TestCapProgressEntries_ReturnsAllWhenExactlyMax(t *testing.T) {
	content := buildProgress(5)

	result := CapProgressEntries(content, 5)

	for i := 1; i <= 5; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d", i)
		if !strings.Contains(result, marker) {
			t.Errorf("expected result to contain %q", marker)
		}
	}
}

func TestCapProgressEntries_EmptyContent(t *testing.T) {
	result := CapProgressEntries("", 5)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCapProgressEntries_HeaderOnlyNoEntries(t *testing.T) {
	content := "# Ralph Progress Log\nStarted: 2026-02-20\n---\n\n## Codebase Patterns\n\n---\n"

	result := CapProgressEntries(content, 5)

	if result != content {
		t.Errorf("expected unchanged content when no entries exist\ngot:  %q\nwant: %q", result, content)
	}
}

func TestCapProgressEntries_ZeroMaxEntries(t *testing.T) {
	content := buildProgress(3)

	result := CapProgressEntries(content, 0)

	if !strings.Contains(result, "# Ralph Progress Log") {
		t.Error("expected header to be preserved")
	}
	for i := 1; i <= 3; i++ {
		marker := fmt.Sprintf("## 2026-02-20 - S%d", i)
		if strings.Contains(result, marker) {
			t.Errorf("expected result to NOT contain %q with maxEntries=0", marker)
		}
	}
}

func TestCapProgressEntries_DefaultMaxEntries(t *testing.T) {
	if DefaultMaxEntries != 5 {
		t.Errorf("expected DefaultMaxEntries=5, got %d", DefaultMaxEntries)
	}
}

// --- helpers ---

func buildProgress(numEntries int) string {
	var b strings.Builder
	b.WriteString("# Ralph Progress Log\nStarted: 2026-02-20T15:33:09+01:00\n---\n\n")
	b.WriteString("## Codebase Patterns\n\n- Pattern one\n- Pattern two\n\n---\n\n")
	for i := 1; i <= numEntries; i++ {
		fmt.Fprintf(&b, "## 2026-02-20 - S%d\n", i)
		fmt.Fprintf(&b, "- Implemented story S%d\n", i)
		fmt.Fprintf(&b, "- Files changed: file%d.go\n", i)
		b.WriteString("---\n\n")
	}
	return b.String()
}
