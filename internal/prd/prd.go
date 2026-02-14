package prd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type PRD struct {
	Project               string            `json:"project"`
	BranchName            string            `json:"branchName"`
	Description           string            `json:"description"`
	FeatureOverview       json.RawMessage   `json:"featureOverview,omitempty"`
	ArchitectureOverview  json.RawMessage   `json:"architectureOverview,omitempty"`
	UserStories           []Story           `json:"userStories"`
	IntegrationTests      []IntegrationTest `json:"integrationTests,omitempty"`
}

type Story struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	Priority           int      `json:"priority"`
	Passes             bool     `json:"passes"`
	Notes              string   `json:"notes"`
}

type IntegrationTest struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	Passes      bool     `json:"passes"`
	Failure     string   `json:"failure"`
	Notes       string   `json:"notes"`
}

// Read loads a PRD from the given JSON file.
func Read(path string) (*PRD, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading PRD %s: %w", path, err)
	}

	var p PRD
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing PRD %s: %w", path, err)
	}

	return &p, nil
}

// Write persists a PRD as formatted JSON.
func Write(path string, p *PRD) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling PRD: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing PRD %s: %w", path, err)
	}

	return nil
}

// NextUnfinished returns the highest-priority story where Passes is false.
// Returns nil when all stories pass.
func NextUnfinished(p *PRD) *Story {
	pending := make([]Story, 0)
	for _, s := range p.UserStories {
		if !s.Passes {
			pending = append(pending, s)
		}
	}

	if len(pending) == 0 {
		return nil
	}

	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Priority < pending[j].Priority
	})

	return &pending[0]
}

// AllPass returns true when every story has Passes set to true.
func AllPass(p *PRD) bool {
	for _, s := range p.UserStories {
		if !s.Passes {
			return false
		}
	}
	return true
}

// AllIntegrationTestsPass returns true when every integration test has Passes set to true.
// Returns true if there are no integration tests.
func AllIntegrationTestsPass(p *PRD) bool {
	for _, t := range p.IntegrationTests {
		if !t.Passes {
			return false
		}
	}
	return true
}

// MarkPassing sets the story with the given ID to Passes=true.
func MarkPassing(p *PRD, storyID string) bool {
	for i := range p.UserStories {
		if p.UserStories[i].ID == storyID {
			p.UserStories[i].Passes = true
			return true
		}
	}
	return false
}

// FailedIntegrationTests returns all integration tests where Passes is false.
func FailedIntegrationTests(p *PRD) []IntegrationTest {
	var failed []IntegrationTest
	for _, t := range p.IntegrationTests {
		if !t.Passes {
			failed = append(failed, t)
		}
	}
	return failed
}

// RawJSONToString converts a json.RawMessage to a human-readable string.
// If the value is a JSON string, it returns the unquoted string.
// If the value is an object or array, it returns the indented JSON.
// Returns empty string for nil or empty input.
func RawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}

	// If it's a JSON string, unquote it.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}

	// For objects/arrays, return indented JSON.
	var buf bytes.Buffer
	if err := json.Indent(&buf, trimmed, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
