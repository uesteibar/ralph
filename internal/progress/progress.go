package progress

import "strings"

// DefaultMaxEntries is the default number of progress entries to keep.
const DefaultMaxEntries = 5

// CapProgressEntries keeps the header, Codebase Patterns section, and the
// last maxEntries progress entries. Entries are delimited by "---" lines.
// The first two "---"-delimited sections (header and Codebase Patterns) are
// always preserved. Returns the original content unmodified if there are
// maxEntries or fewer entries.
func CapProgressEntries(content string, maxEntries int) string {
	if content == "" {
		return ""
	}

	// Split on "---" delimiter lines. The progress file format is:
	//   [header] --- [codebase patterns] --- [entry1] --- [entry2] --- ...
	sections := splitSections(content)

	// First two sections are structural (header + codebase patterns).
	// Everything after is a progress entry.
	const headerSections = 2
	if len(sections) <= headerSections {
		return content
	}

	entries := sections[headerSections:]
	if len(entries) <= maxEntries {
		return content
	}

	// Keep only the last maxEntries entries
	kept := entries
	if maxEntries > 0 {
		kept = entries[len(entries)-maxEntries:]
	} else {
		kept = nil
	}

	// Reassemble: header sections + kept entries
	var b strings.Builder
	for i := 0; i < headerSections; i++ {
		b.WriteString(sections[i])
		b.WriteString("---\n")
	}
	for _, entry := range kept {
		b.WriteString(entry)
		b.WriteString("---\n")
	}
	return b.String()
}

// splitSections splits the progress file content by "---" delimiter lines.
// Each returned section includes trailing content up to (but not including)
// the next "---" line.
func splitSections(content string) []string {
	var sections []string
	var current strings.Builder

	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "---" {
			sections = append(sections, current.String())
			current.Reset()
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	// If there's trailing content after the last ---, include it as a section
	trailing := current.String()
	if strings.TrimSpace(trailing) != "" {
		sections = append(sections, trailing)
	}

	return sections
}
