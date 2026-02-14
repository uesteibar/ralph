package prd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func samplePRD() *PRD {
	return &PRD{
		Project:     "TestProject",
		BranchName:  "ralph/test-feature",
		Description: "A test feature",
		UserStories: []Story{
			{ID: "US-001", Title: "First", Priority: 2, Passes: false},
			{ID: "US-002", Title: "Second", Priority: 1, Passes: false},
			{ID: "US-003", Title: "Third", Priority: 3, Passes: true},
		},
	}
}

func TestWriteAndRead_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := samplePRD()
	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if loaded.Project != original.Project {
		t.Errorf("Project = %q, want %q", loaded.Project, original.Project)
	}
	if loaded.BranchName != original.BranchName {
		t.Errorf("BranchName = %q, want %q", loaded.BranchName, original.BranchName)
	}
	if len(loaded.UserStories) != len(original.UserStories) {
		t.Fatalf("UserStories count = %d, want %d", len(loaded.UserStories), len(original.UserStories))
	}
	for i, s := range loaded.UserStories {
		if s.ID != original.UserStories[i].ID {
			t.Errorf("Story[%d].ID = %q, want %q", i, s.ID, original.UserStories[i].ID)
		}
		if s.Passes != original.UserStories[i].Passes {
			t.Errorf("Story[%d].Passes = %v, want %v", i, s.Passes, original.UserStories[i].Passes)
		}
	}
}

func TestNextUnfinished_ReturnsByPriority(t *testing.T) {
	p := samplePRD()
	next := NextUnfinished(p)
	if next == nil {
		t.Fatal("expected a story, got nil")
	}
	// US-002 has priority 1, US-001 has priority 2
	if next.ID != "US-002" {
		t.Errorf("NextUnfinished = %q, want %q (lowest priority number)", next.ID, "US-002")
	}
}

func TestNextUnfinished_AllPassing_ReturnsNil(t *testing.T) {
	p := &PRD{
		UserStories: []Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}
	if got := NextUnfinished(p); got != nil {
		t.Errorf("expected nil, got story %q", got.ID)
	}
}

func TestNextUnfinished_Empty_ReturnsNil(t *testing.T) {
	p := &PRD{}
	if got := NextUnfinished(p); got != nil {
		t.Errorf("expected nil for empty PRD, got %v", got)
	}
}

func TestAllPass_MixedStates(t *testing.T) {
	p := samplePRD()
	if AllPass(p) {
		t.Error("AllPass should be false when some stories are not passing")
	}
}

func TestAllPass_AllTrue(t *testing.T) {
	p := &PRD{
		UserStories: []Story{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}
	if !AllPass(p) {
		t.Error("AllPass should be true when all stories pass")
	}
}

func TestMarkPassing_ExistingStory(t *testing.T) {
	p := samplePRD()
	if !MarkPassing(p, "US-001") {
		t.Error("MarkPassing should return true for existing story")
	}
	if !p.UserStories[0].Passes {
		t.Error("Story US-001 should now have Passes=true")
	}
}

func TestMarkPassing_NonexistentStory(t *testing.T) {
	p := samplePRD()
	if MarkPassing(p, "US-999") {
		t.Error("MarkPassing should return false for nonexistent story")
	}
}

func TestRead_NonexistentFile_ReturnsError(t *testing.T) {
	_, err := Read("/nonexistent/prd.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestRead_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIntegrationTests_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := &PRD{
		Project:     "TestProject",
		BranchName:  "ralph/test",
		Description: "Test with integration tests",
		UserStories: []Story{
			{ID: "US-001", Title: "First", Passes: true},
		},
		IntegrationTests: []IntegrationTest{
			{
				ID:          "IT-001",
				Description: "Test login flow",
				Steps:       []string{"Open login page", "Enter credentials", "Click submit"},
				Passes:      false,
				Failure:     "Button not found",
				Notes:       "Needs UI update",
			},
			{
				ID:          "IT-002",
				Description: "Test logout",
				Steps:       []string{"Click logout"},
				Passes:      true,
				Failure:     "",
				Notes:       "",
			},
		},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(loaded.IntegrationTests) != 2 {
		t.Fatalf("IntegrationTests count = %d, want 2", len(loaded.IntegrationTests))
	}

	it := loaded.IntegrationTests[0]
	if it.ID != "IT-001" {
		t.Errorf("IT[0].ID = %q, want %q", it.ID, "IT-001")
	}
	if it.Description != "Test login flow" {
		t.Errorf("IT[0].Description = %q, want %q", it.Description, "Test login flow")
	}
	if len(it.Steps) != 3 {
		t.Errorf("IT[0].Steps count = %d, want 3", len(it.Steps))
	}
	if it.Steps[0] != "Open login page" {
		t.Errorf("IT[0].Steps[0] = %q, want %q", it.Steps[0], "Open login page")
	}
	if it.Passes != false {
		t.Errorf("IT[0].Passes = %v, want false", it.Passes)
	}
	if it.Failure != "Button not found" {
		t.Errorf("IT[0].Failure = %q, want %q", it.Failure, "Button not found")
	}
	if it.Notes != "Needs UI update" {
		t.Errorf("IT[0].Notes = %q, want %q", it.Notes, "Needs UI update")
	}

	it2 := loaded.IntegrationTests[1]
	if it2.Passes != true {
		t.Errorf("IT[1].Passes = %v, want true", it2.Passes)
	}
}

func TestRead_PRDWithoutIntegrationTests_ParsesCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	// Write a PRD JSON without integrationTests field
	jsonData := `{
  "project": "OldProject",
  "branchName": "ralph/old-feature",
  "description": "Legacy PRD",
  "userStories": [
    {"id": "US-001", "title": "Story", "passes": true}
  ]
}`
	if err := os.WriteFile(path, []byte(jsonData), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if loaded.Project != "OldProject" {
		t.Errorf("Project = %q, want %q", loaded.Project, "OldProject")
	}
	if len(loaded.UserStories) != 1 {
		t.Errorf("UserStories count = %d, want 1", len(loaded.UserStories))
	}
	// IntegrationTests should be nil or empty
	if len(loaded.IntegrationTests) != 0 {
		t.Errorf("IntegrationTests count = %d, want 0 for legacy PRD", len(loaded.IntegrationTests))
	}
}

func TestAllIntegrationTestsPass_Empty_ReturnsTrue(t *testing.T) {
	p := &PRD{}
	if !AllIntegrationTestsPass(p) {
		t.Error("AllIntegrationTestsPass should be true when there are no integration tests")
	}
}

func TestAllIntegrationTestsPass_AllTrue(t *testing.T) {
	p := &PRD{
		IntegrationTests: []IntegrationTest{
			{ID: "IT-001", Passes: true},
			{ID: "IT-002", Passes: true},
		},
	}
	if !AllIntegrationTestsPass(p) {
		t.Error("AllIntegrationTestsPass should be true when all tests pass")
	}
}

func TestAllIntegrationTestsPass_SomeFailing(t *testing.T) {
	p := &PRD{
		IntegrationTests: []IntegrationTest{
			{ID: "IT-001", Passes: true},
			{ID: "IT-002", Passes: false},
		},
	}
	if AllIntegrationTestsPass(p) {
		t.Error("AllIntegrationTestsPass should be false when some tests fail")
	}
}

func TestAllIntegrationTestsPass_AllFailing(t *testing.T) {
	p := &PRD{
		IntegrationTests: []IntegrationTest{
			{ID: "IT-001", Passes: false},
			{ID: "IT-002", Passes: false},
		},
	}
	if AllIntegrationTestsPass(p) {
		t.Error("AllIntegrationTestsPass should be false when all tests fail")
	}
}

func TestFailedIntegrationTests_ReturnsOnlyFailing(t *testing.T) {
	p := &PRD{
		IntegrationTests: []IntegrationTest{
			{ID: "IT-001", Description: "Test 1", Passes: true},
			{ID: "IT-002", Description: "Test 2", Passes: false},
			{ID: "IT-003", Description: "Test 3", Passes: false},
		},
	}

	failed := FailedIntegrationTests(p)

	if len(failed) != 2 {
		t.Fatalf("expected 2 failed tests, got %d", len(failed))
	}
	if failed[0].ID != "IT-002" {
		t.Errorf("failed[0].ID = %q, want %q", failed[0].ID, "IT-002")
	}
	if failed[1].ID != "IT-003" {
		t.Errorf("failed[1].ID = %q, want %q", failed[1].ID, "IT-003")
	}
}

func TestFailedIntegrationTests_Empty_ReturnsNil(t *testing.T) {
	p := &PRD{}
	failed := FailedIntegrationTests(p)
	if failed != nil {
		t.Errorf("expected nil for empty PRD, got %v", failed)
	}
}

func TestOverviewFields_Roundtrip_String(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := &PRD{
		Project:              "TestProject",
		BranchName:           "ralph/test",
		Description:          "Test with overviews",
		FeatureOverview:      json.RawMessage(`"This feature adds X to improve Y"`),
		ArchitectureOverview: json.RawMessage(`"We will use a layered architecture with Z"`),
		UserStories:          []Story{{ID: "US-001", Title: "First", Passes: true}},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if got := RawJSONToString(loaded.FeatureOverview); got != "This feature adds X to improve Y" {
		t.Errorf("FeatureOverview = %q, want %q", got, "This feature adds X to improve Y")
	}
	if got := RawJSONToString(loaded.ArchitectureOverview); got != "We will use a layered architecture with Z" {
		t.Errorf("ArchitectureOverview = %q, want %q", got, "We will use a layered architecture with Z")
	}
}

func TestOverviewFields_Roundtrip_Object(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	featureObj := json.RawMessage(`{"problem":"Users need X","approach":"We will do Y"}`)
	archObj := json.RawMessage(`{"approach":"Layered architecture","otherOptions":["Option A"]}`)

	original := &PRD{
		Project:              "TestProject",
		BranchName:           "ralph/test",
		Description:          "Test with object overviews",
		FeatureOverview:      featureObj,
		ArchitectureOverview: archObj,
		UserStories:          []Story{{ID: "US-001", Title: "First", Passes: true}},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := RawJSONToString(loaded.FeatureOverview)
	if got == "" {
		t.Fatal("FeatureOverview should not be empty")
	}
	// Object should contain the key
	if !strings.Contains(got, "problem") {
		t.Errorf("FeatureOverview = %q, should contain 'problem'", got)
	}
	if !strings.Contains(got, "Users need X") {
		t.Errorf("FeatureOverview = %q, should contain 'Users need X'", got)
	}
}

func TestRead_PRDWithoutOverviewFields_ParsesCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	jsonData := `{
  "project": "OldProject",
  "branchName": "ralph/old-feature",
  "description": "Legacy PRD without overviews",
  "userStories": [
    {"id": "US-001", "title": "Story", "passes": true}
  ]
}`
	if err := os.WriteFile(path, []byte(jsonData), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if loaded.Project != "OldProject" {
		t.Errorf("Project = %q, want %q", loaded.Project, "OldProject")
	}
	if RawJSONToString(loaded.FeatureOverview) != "" {
		t.Errorf("FeatureOverview = %q, want empty string", RawJSONToString(loaded.FeatureOverview))
	}
	if RawJSONToString(loaded.ArchitectureOverview) != "" {
		t.Errorf("ArchitectureOverview = %q, want empty string", RawJSONToString(loaded.ArchitectureOverview))
	}
	if len(loaded.UserStories) != 1 {
		t.Errorf("UserStories count = %d, want 1", len(loaded.UserStories))
	}
}

func TestFailedIntegrationTests_AllPassing_ReturnsNil(t *testing.T) {
	p := &PRD{
		IntegrationTests: []IntegrationTest{
			{ID: "IT-001", Passes: true},
			{ID: "IT-002", Passes: true},
		},
	}
	failed := FailedIntegrationTests(p)
	if failed != nil {
		t.Errorf("expected nil when all tests pass, got %v", failed)
	}
}
