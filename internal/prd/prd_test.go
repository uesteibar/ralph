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

func TestQAVerification_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := &PRD{
		Project:     "TestProject",
		BranchName:  "ralph/test",
		Description: "Test with QA verification",
		UserStories: []Story{
			{ID: "US-001", Title: "First", Passes: true},
		},
		QAVerification: &QAVerification{
			Status:   "passed",
			Attempts: 2,
		},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if loaded.QAVerification == nil {
		t.Fatal("QAVerification should not be nil")
	}
	if loaded.QAVerification.Status != "passed" {
		t.Errorf("QAVerification.Status = %q, want %q", loaded.QAVerification.Status, "passed")
	}
	if loaded.QAVerification.Attempts != 2 {
		t.Errorf("QAVerification.Attempts = %d, want 2", loaded.QAVerification.Attempts)
	}
}

func TestRead_PRDWithoutQAVerification_ParsesCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

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
	if loaded.QAVerification != nil {
		t.Errorf("QAVerification should be nil for legacy PRD, got %+v", loaded.QAVerification)
	}
}

func TestQAVerificationStatus_Nil_ReturnsPending(t *testing.T) {
	p := &PRD{}
	if got := QAVerificationStatus(p); got != "pending" {
		t.Errorf("QAVerificationStatus = %q, want %q", got, "pending")
	}
}

func TestQAVerificationStatus_Passed(t *testing.T) {
	p := &PRD{
		QAVerification: &QAVerification{Status: "passed", Attempts: 1},
	}
	if got := QAVerificationStatus(p); got != "passed" {
		t.Errorf("QAVerificationStatus = %q, want %q", got, "passed")
	}
}

func TestQAVerificationStatus_Failed(t *testing.T) {
	p := &PRD{
		QAVerification: &QAVerification{Status: "failed", Attempts: 3},
	}
	if got := QAVerificationStatus(p); got != "failed" {
		t.Errorf("QAVerificationStatus = %q, want %q", got, "failed")
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

func TestQAFinding_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	original := &PRD{
		Project:     "TestProject",
		BranchName:  "ralph/test",
		Description: "Test with QA findings",
		UserStories: []Story{{ID: "US-001", Passes: true}},
		QAVerification: &QAVerification{
			Status:   "failed",
			Attempts: 1,
			Findings: []QAFinding{
				{ID: "QA-001", Title: "Login fails", Description: "Login page returns 500", Severity: "error", TestScript: "test-login.sh", Status: "found"},
				{ID: "QA-002", Title: "Slow query", Description: "Query takes >5s", Severity: "warning", TestScript: "test-perf.sh", Status: "addressed"},
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

	if loaded.QAVerification == nil {
		t.Fatal("QAVerification should not be nil")
	}
	if len(loaded.QAVerification.Findings) != 2 {
		t.Fatalf("Findings count = %d, want 2", len(loaded.QAVerification.Findings))
	}
	f := loaded.QAVerification.Findings[0]
	if f.ID != "QA-001" || f.Title != "Login fails" || f.Severity != "error" || f.Status != "found" {
		t.Errorf("First finding = %+v, unexpected", f)
	}
}

func TestNextUnfixedFinding_ReturnsFirstFound(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{
			{ID: "QA-001", Status: "addressed"},
			{ID: "QA-002", Status: "found"},
			{ID: "QA-003", Status: "found"},
		},
	}
	f := NextUnfixedFinding(qa)
	if f == nil || f.ID != "QA-002" {
		t.Errorf("NextUnfixedFinding = %v, want QA-002", f)
	}
}

func TestNextUnfixedFinding_AllAddressed_ReturnsNil(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{
			{ID: "QA-001", Status: "addressed"},
		},
	}
	if f := NextUnfixedFinding(qa); f != nil {
		t.Errorf("expected nil, got %v", f)
	}
}

func TestNextUnfixedFinding_NilQA_ReturnsNil(t *testing.T) {
	if f := NextUnfixedFinding(nil); f != nil {
		t.Errorf("expected nil, got %v", f)
	}
}

func TestMarkFindingAddressed_ExistingFinding(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{
			{ID: "QA-001", Status: "found"},
			{ID: "QA-002", Status: "found"},
		},
	}
	if !MarkFindingAddressed(qa, "QA-001") {
		t.Error("expected true for existing finding")
	}
	if qa.Findings[0].Status != "addressed" {
		t.Errorf("Status = %q, want addressed", qa.Findings[0].Status)
	}
	if qa.Findings[1].Status != "found" {
		t.Error("QA-002 should still be found")
	}
}

func TestMarkFindingAddressed_NonexistentFinding(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{{ID: "QA-001", Status: "found"}},
	}
	if MarkFindingAddressed(qa, "QA-999") {
		t.Error("expected false for nonexistent finding")
	}
}

func TestMarkFindingAddressed_NilQA(t *testing.T) {
	if MarkFindingAddressed(nil, "QA-001") {
		t.Error("expected false for nil QA")
	}
}

func TestRemoveFinding_ExistingFinding(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{
			{ID: "QA-001"},
			{ID: "QA-002"},
			{ID: "QA-003"},
		},
	}
	if !RemoveFinding(qa, "QA-002") {
		t.Error("expected true for existing finding")
	}
	if len(qa.Findings) != 2 {
		t.Fatalf("Findings count = %d, want 2", len(qa.Findings))
	}
	if qa.Findings[0].ID != "QA-001" || qa.Findings[1].ID != "QA-003" {
		t.Errorf("Remaining = [%s, %s], want [QA-001, QA-003]", qa.Findings[0].ID, qa.Findings[1].ID)
	}
}

func TestRemoveFinding_NonexistentFinding(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{{ID: "QA-001"}},
	}
	if RemoveFinding(qa, "QA-999") {
		t.Error("expected false for nonexistent finding")
	}
}

func TestRemoveFinding_NilQA(t *testing.T) {
	if RemoveFinding(nil, "QA-001") {
		t.Error("expected false for nil QA")
	}
}

func TestHasUnfixedFindings_WithFound(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{{ID: "QA-001", Status: "found"}},
	}
	if !HasUnfixedFindings(qa) {
		t.Error("expected true with found findings")
	}
}

func TestHasUnfixedFindings_AllAddressed(t *testing.T) {
	qa := &QAVerification{
		Findings: []QAFinding{{ID: "QA-001", Status: "addressed"}},
	}
	if HasUnfixedFindings(qa) {
		t.Error("expected false when all addressed")
	}
}

func TestHasUnfixedFindings_Empty(t *testing.T) {
	qa := &QAVerification{}
	if HasUnfixedFindings(qa) {
		t.Error("expected false with no findings")
	}
}
