package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	mocklinear "github.com/uesteibar/ralph/test/e2e/mocks/linear"
)

func TestPlayground_SmokeTest(t *testing.T) {
	pg := StartPlayground(t)

	// Health check.
	resp, err := http.Get(pg.BaseURL() + "/api/status")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decoding status: %v", err)
	}
	if status["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", status["status"])
	}
}

func TestPlayground_StopsCleanly(t *testing.T) {
	pg := StartPlayground(t)
	addr := pg.Addr

	// Verify server is running.
	resp, err := http.Get("http://" + addr + "/api/status")
	if err != nil {
		t.Fatalf("server not running: %v", err)
	}
	resp.Body.Close()

	// Close server explicitly (simulates clean shutdown).
	pg.Server.Close()

	// Verify server is no longer accepting connections.
	_, err = http.Get("http://" + addr + "/api/status")
	if err == nil {
		t.Error("expected connection error after server close")
	}
}

func TestPlayground_MockServersAvailable(t *testing.T) {
	pg := StartPlayground(t)

	// Linear mock should respond.
	resp, err := http.Post(pg.LinearURL, "application/json",
		nil)
	if err != nil {
		t.Fatalf("Linear mock not reachable: %v", err)
	}
	resp.Body.Close()

	// GitHub mock should respond (with 404 for unknown path, but no connection error).
	resp, err = http.Get(pg.GitHubURL + "/api/v3/repos/test/test/pulls")
	if err != nil {
		t.Fatalf("GitHub mock not reachable: %v", err)
	}
	resp.Body.Close()
}

func TestPlayground_ProjectSeeded(t *testing.T) {
	pg := StartPlayground(t)

	// Check projects via API.
	resp, err := http.Get(pg.BaseURL() + "/api/projects")
	if err != nil {
		t.Fatalf("fetching projects: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var projects []map[string]any
	if err := json.Unmarshal(body, &projects); err != nil {
		t.Fatalf("decoding projects: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0]["name"] != "test-project" {
		t.Errorf("expected project name 'test-project', got %v", projects[0]["name"])
	}
}

func TestPlayground_SeedIssue(t *testing.T) {
	pg := StartPlayground(t)

	issue := pg.SeedIssue("TEST-1", "Add user avatars", "queued")

	// Verify via API.
	resp, err := http.Get(pg.BaseURL() + "/api/issues/" + issue.ID)
	if err != nil {
		t.Fatalf("fetching issue: %v", err)
	}
	defer resp.Body.Close()

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding issue: %v", err)
	}

	if got["identifier"] != "TEST-1" {
		t.Errorf("expected identifier 'TEST-1', got %v", got["identifier"])
	}
	if got["title"] != "Add user avatars" {
		t.Errorf("expected title 'Add user avatars', got %v", got["title"])
	}
	if got["state"] != "queued" {
		t.Errorf("expected state 'queued', got %v", got["state"])
	}
}

func TestPlayground_NoProject(t *testing.T) {
	pg := StartPlayground(t, func(cfg *PlaygroundConfig) {
		cfg.SeedProject = false
	})

	resp, err := http.Get(pg.BaseURL() + "/api/projects")
	if err != nil {
		t.Fatalf("fetching projects: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var projects []map[string]any
	json.Unmarshal(body, &projects)

	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestPlayground_LinearMockAddIssue(t *testing.T) {
	pg := StartPlayground(t)

	pg.Linear.AddIssue(mocklinearIssue(mocklinear.IssueUUID("issue-1"), "TEST-1", "Test issue"))

	// Verify the issue is retrievable via the Linear mock API (using an HTTP client directly).
	// Use a valid UUID for the team filter (the mock validates UUID format).
	reqBody := `{"query":"query($teamID: ID!, $assigneeID: ID!) { issues(filter: { team: { id: { eq: $teamID } } }) { nodes { id identifier title } } }","variables":{"teamID":"` + mocklinear.TestTeamID + `","assigneeID":"` + mocklinear.TestAssigneeID + `"}}`
	resp, err := http.Post(pg.LinearURL, "application/json",
		stringReader(reqBody))
	if err != nil {
		t.Fatalf("calling Linear mock: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var gqlResp map[string]any
	json.Unmarshal(body, &gqlResp)
	data, _ := gqlResp["data"].(json.RawMessage)
	if data == nil {
		dataAny, _ := gqlResp["data"]
		dataBytes, _ := json.Marshal(dataAny)
		data = json.RawMessage(dataBytes)
	}

	var dataMap map[string]any
	json.Unmarshal(data, &dataMap)
	issues, _ := dataMap["issues"].(map[string]any)
	nodes, _ := issues["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 issue from Linear mock")
	}
}

func TestPlayground_GitHubMockCreatePR(t *testing.T) {
	pg := StartPlayground(t)

	pg.GitHub.AddPR("test-owner", "test-repo", mockgithubPR(1, "feat", "main"))

	pr := pg.GitHub.GetPR("test-owner", "test-repo", 1)
	if pr == nil {
		t.Fatal("expected PR to exist")
	}
	if pr.Number != 1 {
		t.Errorf("expected PR #1, got #%d", pr.Number)
	}
}
