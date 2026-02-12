package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockLinear returns an httptest.Server that handles GraphQL requests.
// The handler function receives the parsed query and variables, and returns
// the data payload (or an error response).
func mockLinear(t *testing.T, handler func(query string, vars map[string]any) (any, []graphqlError)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got == "" {
			t.Error("missing Authorization header")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", got)
		}

		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}

		data, errs := handler(req.Query, req.Variables)

		resp := map[string]any{}
		if data != nil {
			raw, _ := json.Marshal(data)
			resp["data"] = json.RawMessage(raw)
		}
		if len(errs) > 0 {
			resp["errors"] = errs
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestClient_FetchAssignedIssues_Success(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["teamID"] != "team-1" || vars["assigneeID"] != "user-1" {
			t.Errorf("unexpected variables: %v", vars)
		}
		return map[string]any{
			"issues": map[string]any{
				"nodes": []map[string]any{
					{
						"id":          "issue-1",
						"identifier":  "ENG-42",
						"title":       "Add avatars",
						"description": "User avatars needed",
						"state":       map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
					},
					{
						"id":          "issue-2",
						"identifier":  "ENG-43",
						"title":       "Fix login",
						"description": "",
						"state":       map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
					},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	issues, err := c.FetchAssignedIssues(context.Background(), "team-1", "user-1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	if issues[0].ID != "issue-1" || issues[0].Identifier != "ENG-42" || issues[0].Title != "Add avatars" {
		t.Errorf("issue 0 mismatch: %+v", issues[0])
	}
	if issues[0].State.Name != "Todo" || issues[0].State.Type != "unstarted" {
		t.Errorf("issue 0 state mismatch: %+v", issues[0].State)
	}
	if issues[1].ID != "issue-2" {
		t.Errorf("issue 1 mismatch: %+v", issues[1])
	}
}

func TestClient_FetchAssignedIssues_EmptyResult(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return map[string]any{
			"issues": map[string]any{
				"nodes": []map[string]any{},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	issues, err := c.FetchAssignedIssues(context.Background(), "team-1", "user-1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d", len(issues))
	}
}

func TestClient_FetchIssueComments_Success(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["issueID"] != "issue-1" {
			t.Errorf("unexpected issueID: %v", vars["issueID"])
		}
		return map[string]any{
			"issue": map[string]any{
				"comments": map[string]any{
					"nodes": []map[string]any{
						{
							"id":        "comment-1",
							"body":      "Looks good",
							"user":      map[string]any{"name": "Alice"},
							"createdAt": "2026-02-11T10:00:00Z",
						},
						{
							"id":        "comment-2",
							"body":      "@autoralph approved",
							"user":      map[string]any{"name": "Bob"},
							"createdAt": "2026-02-11T11:00:00Z",
						},
					},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	comments, err := c.FetchIssueComments(context.Background(), "issue-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != "comment-1" || comments[0].Body != "Looks good" || comments[0].UserName != "Alice" {
		t.Errorf("comment 0 mismatch: %+v", comments[0])
	}
	if comments[1].Body != "@autoralph approved" || comments[1].UserName != "Bob" {
		t.Errorf("comment 1 mismatch: %+v", comments[1])
	}
}

func TestClient_PostComment_UsesStringTypeForIssueID(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		// Linear's commentCreate input expects String for issueId, not ID.
		if !contains(query, "$issueID: String!") {
			t.Errorf("PostComment query should declare $issueID as String!, got query:\n%s", query)
		}
		return map[string]any{
			"commentCreate": map[string]any{
				"comment": map[string]any{
					"id": "c1", "body": "x", "user": map[string]any{"name": "bot"}, "createdAt": "2026-01-01T00:00:00Z",
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	_, err := c.PostComment(context.Background(), "issue-1", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_PostComment_Success(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["issueID"] != "issue-1" || vars["body"] != "Hello world" {
			t.Errorf("unexpected variables: %v", vars)
		}
		return map[string]any{
			"commentCreate": map[string]any{
				"comment": map[string]any{
					"id":        "comment-new",
					"body":      "Hello world",
					"user":      map[string]any{"name": "autoralph"},
					"createdAt": "2026-02-11T12:00:00Z",
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	comment, err := c.PostComment(context.Background(), "issue-1", "Hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if comment.ID != "comment-new" || comment.Body != "Hello world" || comment.UserName != "autoralph" {
		t.Errorf("comment mismatch: %+v", comment)
	}
}

func TestClient_UpdateIssueState_Success(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["issueID"] != "issue-1" || vars["stateID"] != "state-done" {
			t.Errorf("unexpected variables: %v", vars)
		}
		return map[string]any{
			"issueUpdate": map[string]any{
				"success": true,
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	err := c.UpdateIssueState(context.Background(), "issue-1", "state-done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_UpdateIssueState_Unsuccessful(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return map[string]any{
			"issueUpdate": map[string]any{
				"success": false,
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	err := c.UpdateIssueState(context.Background(), "issue-1", "bad-state")
	if err == nil {
		t.Fatal("expected error for unsuccessful update")
	}
}

func TestClient_FetchWorkflowStates_Success(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["teamID"] != "team-1" {
			t.Errorf("unexpected teamID: %v", vars["teamID"])
		}
		return map[string]any{
			"team": map[string]any{
				"states": map[string]any{
					"nodes": []map[string]any{
						{"id": "s1", "name": "Backlog", "type": "backlog"},
						{"id": "s2", "name": "Todo", "type": "unstarted"},
						{"id": "s3", "name": "In Progress", "type": "started"},
						{"id": "s4", "name": "Done", "type": "completed"},
						{"id": "s5", "name": "Canceled", "type": "canceled"},
					},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	states, err := c.FetchWorkflowStates(context.Background(), "team-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(states) != 5 {
		t.Fatalf("expected 5 states, got %d", len(states))
	}
	if states[0].Name != "Backlog" || states[0].Type != "backlog" {
		t.Errorf("state 0 mismatch: %+v", states[0])
	}
	if states[3].Name != "Done" || states[3].Type != "completed" {
		t.Errorf("state 3 mismatch: %+v", states[3])
	}
}

func TestClient_GraphQLError(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return nil, []graphqlError{
			{Message: "Variable '$teamID' is invalid"},
			{Message: "Authentication required"},
		}
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	_, err := c.FetchAssignedIssues(context.Background(), "bad", "bad", "", "")
	if err == nil {
		t.Fatal("expected error for GraphQL errors")
	}
	errMsg := err.Error()
	if !contains(errMsg, "Variable '$teamID' is invalid") || !contains(errMsg, "Authentication required") {
		t.Errorf("error should contain both GraphQL error messages, got: %s", errMsg)
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid token"}`))
	}))
	defer srv.Close()

	c := New("bad-key", WithEndpoint(srv.URL))
	_, err := c.FetchAssignedIssues(context.Background(), "team-1", "user-1", "", "")
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
	errMsg := err.Error()
	if !contains(errMsg, "HTTP 401") {
		t.Errorf("error should contain HTTP status, got: %s", errMsg)
	}
}

func TestClient_HTTPServerError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL), WithRetryBackoff(time.Millisecond, time.Millisecond))
	_, err := c.FetchWorkflowStates(context.Background(), "team-1")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !contains(err.Error(), "HTTP 500") {
		t.Errorf("error should contain HTTP 500, got: %s", err.Error())
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts (with retries), got %d", calls)
	}
}

func TestClient_HTTPServerError_RetriesAndSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("transient error"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"team": map[string]any{
					"states": map[string]any{
						"nodes": []map[string]any{
							{"id": "s1", "name": "Todo", "type": "unstarted"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL), WithRetryBackoff(time.Millisecond, time.Millisecond))
	states, err := c.FetchWorkflowStates(context.Background(), "team-1")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(states) != 1 || states[0].Name != "Todo" {
		t.Errorf("unexpected states: %+v", states)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestClient_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issues": map[string]any{"nodes": []any{}},
			},
		})
	}))
	defer srv.Close()

	c := New("lin_api_supersecret", WithEndpoint(srv.URL))
	c.FetchAssignedIssues(context.Background(), "t", "u", "", "")

	if gotAuth != "lin_api_supersecret" {
		t.Errorf("expected Authorization header 'lin_api_supersecret', got %q", gotAuth)
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow server — but context should cancel first.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := New("test-key", WithEndpoint(srv.URL))
	_, err := c.FetchAssignedIssues(ctx, "team-1", "user-1", "", "")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClient_FetchAssignedIssues_WithProjectID(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if vars["projectID"] != "proj-1" {
			t.Errorf("expected projectID variable, got: %v", vars)
		}
		if !contains(query, "project: { id: { eq: $projectID } }") {
			t.Errorf("expected project filter in query, got:\n%s", query)
		}
		return map[string]any{
			"issues": map[string]any{
				"nodes": []map[string]any{
					{
						"id":          "issue-1",
						"identifier":  "ENG-42",
						"title":       "Filtered issue",
						"description": "",
						"state":       map[string]any{"id": "s1", "name": "Todo", "type": "unstarted"},
					},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	issues, err := c.FetchAssignedIssues(context.Background(), "team-1", "user-1", "proj-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Title != "Filtered issue" {
		t.Errorf("expected 'Filtered issue', got %q", issues[0].Title)
	}
}

func TestClient_FetchAssignedIssues_WithoutProjectID_OmitsFilter(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		if _, ok := vars["projectID"]; ok {
			t.Errorf("projectID should not be in vars when empty, got: %v", vars)
		}
		if contains(query, "project:") {
			t.Errorf("query should not contain project filter when empty, got:\n%s", query)
		}
		return map[string]any{
			"issues": map[string]any{
				"nodes": []map[string]any{},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	_, err := c.FetchAssignedIssues(context.Background(), "team-1", "user-1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ResolveProjectID_UUIDPassthrough(t *testing.T) {
	c := New("test-key", WithEndpoint("http://unused"))
	id, err := c.ResolveProjectID(context.Background(), "a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("expected UUID passthrough, got %q", id)
	}
}

func TestClient_ResolveProjectID_EmptyPassthrough(t *testing.T) {
	c := New("test-key", WithEndpoint("http://unused"))
	id, err := c.ResolveProjectID(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty passthrough, got %q", id)
	}
}

func TestClient_ResolveProjectID_ByName(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return map[string]any{
			"projects": map[string]any{
				"nodes": []map[string]any{
					{"id": "proj-uuid-1", "slugId": "my-project", "name": "My Project"},
					{"id": "proj-uuid-2", "slugId": "other", "name": "Other"},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))

	// By name
	id, err := c.ResolveProjectID(context.Background(), "My Project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "proj-uuid-1" {
		t.Errorf("expected proj-uuid-1, got %q", id)
	}
}

func TestClient_ResolveProjectID_BySlug(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return map[string]any{
			"projects": map[string]any{
				"nodes": []map[string]any{
					{"id": "proj-uuid-1", "slugId": "my-project", "name": "My Project"},
				},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	id, err := c.ResolveProjectID(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "proj-uuid-1" {
		t.Errorf("expected proj-uuid-1, got %q", id)
	}
}

func TestClient_ResolveProjectID_NotFound(t *testing.T) {
	srv := mockLinear(t, func(query string, vars map[string]any) (any, []graphqlError) {
		return map[string]any{
			"projects": map[string]any{
				"nodes": []map[string]any{},
			},
		}, nil
	})
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL))
	_, err := c.ResolveProjectID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
	if !contains(err.Error(), "project not found") {
		t.Errorf("expected 'project not found' error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
