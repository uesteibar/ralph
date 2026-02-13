package approve

import (
	"regexp"
	"strings"
)

// typeMarkerPattern matches the HTML comment marker that the AI is instructed
// to place at the start of its response. The marker indicates whether the
// response is a plan (requiring the approval hint) or clarifying questions
// (no approval hint needed).
var typeMarkerPattern = regexp.MustCompile(`(?m)^\s*<!--\s*type:\s*(plan|questions)\s*-->\s*\n?`)

// ResponseNeedsApproval returns true if the AI response is a plan that should
// include the approval hint. It returns false only when the response explicitly
// contains the questions marker. When no marker is found, the safe default is
// to return true (append the approval hint).
func ResponseNeedsApproval(response string) bool {
	match := typeMarkerPattern.FindStringSubmatch(response)
	if match == nil {
		return true // safe default
	}
	return match[1] != "questions"
}

// StripTypeMarker removes the first type marker comment from the response so
// that users do not see the internal classification hint.
func StripTypeMarker(response string) string {
	loc := typeMarkerPattern.FindStringIndex(response)
	if loc == nil {
		return response
	}
	return strings.TrimLeft(response[:loc[0]]+response[loc[1]:], "\n ")
}
