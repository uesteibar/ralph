package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClient_CreatePullRequest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		assertAuth(t, r, "Bearer ghp_test123")

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["head"] != "feat-branch" || body["base"] != "main" {
			t.Errorf("unexpected body: %v", body)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"number":   42,
			"html_url": "https://github.com/octocat/hello/pull/42",
			"title":    "Add feature",
			"state":    "open",
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	pr, err := c.CreatePullRequest(context.Background(), "octocat", "hello", "feat-branch", "main", "Add feature", "Description here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.Number != 42 {
		t.Errorf("expected PR number 42, got %d", pr.Number)
	}
	if pr.HTMLURL != "https://github.com/octocat/hello/pull/42" {
		t.Errorf("unexpected HTMLURL: %s", pr.HTMLURL)
	}
	if pr.Title != "Add feature" {
		t.Errorf("unexpected title: %s", pr.Title)
	}
	if pr.State != "open" {
		t.Errorf("unexpected state: %s", pr.State)
	}
}

func TestClient_FetchPRReviews_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls/42/reviews" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		assertAuth(t, r, "Bearer ghp_test123")

		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":    1,
				"state": "APPROVED",
				"body":  "Looks great!",
				"user":  map[string]any{"login": "reviewer1", "id": 1001},
			},
			{
				"id":    2,
				"state": "CHANGES_REQUESTED",
				"body":  "Needs work",
				"user":  map[string]any{"login": "reviewer2", "id": 1002},
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	reviews, err := c.FetchPRReviews(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(reviews))
	}
	if reviews[0].ID != 1 || reviews[0].State != "APPROVED" || reviews[0].Body != "Looks great!" || reviews[0].User != "reviewer1" || reviews[0].UserID != 1001 {
		t.Errorf("review 0 mismatch: %+v", reviews[0])
	}
	if reviews[1].ID != 2 || reviews[1].State != "CHANGES_REQUESTED" || reviews[1].User != "reviewer2" || reviews[1].UserID != 1002 {
		t.Errorf("review 1 mismatch: %+v", reviews[1])
	}
}

func TestClient_FetchPRReviews_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	reviews, err := c.FetchPRReviews(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("expected 0 reviews, got %d", len(reviews))
	}
}

func TestClient_FetchPRComments_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls/42/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":   10,
				"body": "Fix this line",
				"path": "main.go",
				"user": map[string]any{"login": "reviewer1"},
				"pull_request_review_id": 1,
			},
			{
				"id":             20,
				"body":           "Done",
				"path":           "main.go",
				"user":           map[string]any{"login": "author1"},
				"in_reply_to_id": 10,
				"pull_request_review_id": 1,
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	comments, err := c.FetchPRComments(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != 10 || comments[0].Body != "Fix this line" || comments[0].Path != "main.go" || comments[0].User != "reviewer1" {
		t.Errorf("comment 0 mismatch: %+v", comments[0])
	}
	if comments[1].ID != 20 || comments[1].InReplyTo != 10 || comments[1].User != "author1" {
		t.Errorf("comment 1 mismatch: %+v", comments[1])
	}
}

func TestClient_PostPRComment_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/issues/42/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["body"] != "Nice work!" {
			t.Errorf("unexpected body: %v", body)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":   100,
			"body": "Nice work!",
			"user": map[string]any{"login": "autoralph"},
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	comment, err := c.PostPRComment(context.Background(), "octocat", "hello", 42, "Nice work!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if comment.ID != 100 || comment.Body != "Nice work!" || comment.User != "autoralph" {
		t.Errorf("comment mismatch: %+v", comment)
	}
}

func TestClient_PostReviewReply_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls/42/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["body"] != "Fixed in abc123" {
			t.Errorf("unexpected body: %v", body)
		}
		// go-github sends in_reply_to as a number
		if inReplyTo, ok := body["in_reply_to"].(float64); !ok || int64(inReplyTo) != 10 {
			t.Errorf("unexpected in_reply_to: %v", body["in_reply_to"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":             30,
			"body":           "Fixed in abc123",
			"user":           map[string]any{"login": "autoralph"},
			"in_reply_to_id": 10,
			"path":           "main.go",
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	comment, err := c.PostReviewReply(context.Background(), "octocat", "hello", 42, 10, "Fixed in abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if comment.ID != 30 || comment.Body != "Fixed in abc123" || comment.User != "autoralph" {
		t.Errorf("comment mismatch: %+v", comment)
	}
}

func TestClient_IsPRMerged_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls/42/merge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	merged, err := c.IsPRMerged(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !merged {
		t.Error("expected merged=true")
	}
}

func TestClient_IsPRMerged_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	merged, err := c.IsPRMerged(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged {
		t.Error("expected merged=false")
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"message": "Bad credentials",
		})
	}))
	defer srv.Close()

	c := mustNew(t,"bad-token", WithBaseURL(srv.URL+"/"))
	_, err := c.FetchPRReviews(context.Background(), "octocat", "hello", 42)
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := mustNew(t,"ghp_test123", WithBaseURL(srv.URL+"/"))
	_, err := c.FetchPRReviews(ctx, "octocat", "hello", 42)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClient_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_supersecret", WithBaseURL(srv.URL+"/"))
	c.FetchPRReviews(context.Background(), "o", "r", 1)

	if gotAuth != "Bearer ghp_supersecret" {
		t.Errorf("expected Authorization 'Bearer ghp_supersecret', got %q", gotAuth)
	}
}

func TestClient_ServerError_RetriesAndSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"message": "server error"})
			return
		}
		w.WriteHeader(http.StatusNoContent) // merged
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test", WithBaseURL(srv.URL+"/"), WithRetryBackoff(time.Millisecond, time.Millisecond))
	merged, err := c.IsPRMerged(context.Background(), "o", "r", 1)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if !merged {
		t.Error("expected merged=true")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestClient_FindOpenPR_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number":   42,
				"html_url": "https://github.com/o/r/pull/42",
				"title":    "My PR",
				"state":    "open",
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test", WithBaseURL(srv.URL+"/"))
	pr, err := c.FindOpenPR(context.Background(), "o", "r", "feat-branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil {
		t.Fatal("expected a PR, got nil")
	}
	if pr.Number != 42 {
		t.Errorf("expected PR #42, got #%d", pr.Number)
	}
}

func TestClient_FindOpenPR_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := mustNew(t,"ghp_test", WithBaseURL(srv.URL+"/"))
	pr, err := c.FindOpenPR(context.Background(), "o", "r", "feat-branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Fatalf("expected nil PR, got: %+v", pr)
	}
}

func TestNew_WithAppAuth_BadKeyPath_Error(t *testing.T) {
	_, err := New("", WithAppAuth(AppCredentials{
		ClientID:       "Iv23liABC",
		InstallationID: 12345,
		PrivateKeyPath: "/nonexistent/key.pem",
	}))
	if err == nil {
		t.Fatal("expected error for bad key path, got nil")
	}
}

func TestNew_WithAppAuth_BadKeyContent_Error(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "bad.pem")
	os.WriteFile(keyFile, []byte("not a valid PEM key"), 0600)

	_, err := New("", WithAppAuth(AppCredentials{
		ClientID:       "Iv23liABC",
		InstallationID: 12345,
		PrivateKeyPath: keyFile,
	}))
	if err == nil {
		t.Fatal("expected error for bad PEM content, got nil")
	}
}

func TestNew_WithAppAuth_UsesInstallationToken(t *testing.T) {
	// Generate a test RSA key pair.
	key := generateTestKey(t)

	keyFile := filepath.Join(t.TempDir(), "test.pem")
	os.WriteFile(keyFile, key, 0600)

	// Mock server that handles both the token exchange and the API call.
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/12345/access_tokens" {
			// Token exchange endpoint.
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_installtoken123",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
			return
		}
		// Regular API call — capture the auth header.
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c, err := New("", WithAppAuth(AppCredentials{
		ClientID:       "Iv23liABC",
		InstallationID: 12345,
		PrivateKeyPath: keyFile,
	}), WithBaseURL(srv.URL+"/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.FetchPRReviews(context.Background(), "o", "r", 1)
	if err != nil {
		t.Fatalf("FetchPRReviews: %v", err)
	}

	if gotAuth != "token ghs_installtoken123" {
		t.Errorf("expected auth with installation token, got %q", gotAuth)
	}
}

func generateTestKey(t *testing.T) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	buf := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(k),
	})
	return buf
}

func TestClient_FetchCheckRuns_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/commits/abc123/check-runs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		assertAuth(t, r, "Bearer ghp_test123")

		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"check_runs": []map[string]any{
				{
					"id":         1001,
					"name":       "build",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/octocat/hello/runs/1001",
				},
				{
					"id":         1002,
					"name":       "test",
					"status":     "completed",
					"conclusion": "failure",
					"html_url":   "https://github.com/octocat/hello/runs/1002",
				},
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test123", WithBaseURL(srv.URL+"/"))
	runs, err := c.FetchCheckRuns(context.Background(), "octocat", "hello", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runs) != 2 {
		t.Fatalf("expected 2 check runs, got %d", len(runs))
	}
	if runs[0].ID != 1001 || runs[0].Name != "build" || runs[0].Status != "completed" || runs[0].Conclusion != "success" {
		t.Errorf("check run 0 mismatch: %+v", runs[0])
	}
	if runs[1].ID != 1002 || runs[1].Name != "test" || runs[1].Conclusion != "failure" {
		t.Errorf("check run 1 mismatch: %+v", runs[1])
	}
	if runs[0].HTMLURL != "https://github.com/octocat/hello/runs/1001" {
		t.Errorf("unexpected HTMLURL: %s", runs[0].HTMLURL)
	}
}

func TestClient_FetchCheckRuns_Pagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			w.Header().Set("Link", `<`+r.URL.Path+`?page=2>; rel="next"`)
			json.NewEncoder(w).Encode(map[string]any{
				"total_count": 2,
				"check_runs": []map[string]any{
					{"id": 1, "name": "build", "status": "completed", "conclusion": "success"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"check_runs": []map[string]any{
				{"id": 2, "name": "test", "status": "completed", "conclusion": "failure"},
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test", WithBaseURL(srv.URL+"/"))
	runs, err := c.FetchCheckRuns(context.Background(), "o", "r", "sha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 check runs across pages, got %d", len(runs))
	}
	if runs[0].Name != "build" || runs[1].Name != "test" {
		t.Errorf("unexpected check run names: %s, %s", runs[0].Name, runs[1].Name)
	}
}

func TestClient_FetchCheckRuns_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 0,
			"check_runs":  []map[string]any{},
		})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test", WithBaseURL(srv.URL+"/"))
	runs, err := c.FetchCheckRuns(context.Background(), "o", "r", "sha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 check runs, got %d", len(runs))
	}
}

func TestClient_FetchCheckRunLog_Success(t *testing.T) {
	logContent := "Step 1: Build\nStep 2: Test\nFAILED: assertion error"

	// Log download server (simulates the redirect target).
	logSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(logContent))
	}))
	defer logSrv.Close()

	// GitHub API server returns a redirect to the log download URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/repos/octocat/hello/actions/jobs/1002/logs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Location", logSrv.URL+"/download")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test", WithBaseURL(srv.URL+"/"))
	log, err := c.FetchCheckRunLog(context.Background(), "octocat", "hello", 1002)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(log) != logContent {
		t.Errorf("unexpected log content: %q", string(log))
	}
}

func TestClient_FetchCheckRunLog_NotFound_ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test", WithBaseURL(srv.URL+"/"))
	log, err := c.FetchCheckRunLog(context.Background(), "o", "r", 999)
	if err != nil {
		t.Fatalf("expected no error for missing log, got: %v", err)
	}
	if log != nil {
		t.Errorf("expected nil log, got %d bytes", len(log))
	}
}

func TestClient_FetchPR_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/octocat/hello/pulls/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		assertAuth(t, r, "Bearer ghp_test123")

		json.NewEncoder(w).Encode(map[string]any{
			"number":   42,
			"html_url": "https://github.com/octocat/hello/pull/42",
			"title":    "Add feature",
			"state":    "open",
			"head": map[string]any{
				"sha": "abc123def456",
				"ref": "feat-branch",
			},
		})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test123", WithBaseURL(srv.URL+"/"))
	pr, err := c.FetchPR(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.Number != 42 {
		t.Errorf("expected PR number 42, got %d", pr.Number)
	}
	if pr.HeadSHA != "abc123def456" {
		t.Errorf("expected HeadSHA abc123def456, got %s", pr.HeadSHA)
	}
	if pr.HTMLURL != "https://github.com/octocat/hello/pull/42" {
		t.Errorf("unexpected HTMLURL: %s", pr.HTMLURL)
	}
	if pr.Title != "Add feature" {
		t.Errorf("unexpected title: %s", pr.Title)
	}
	if pr.State != "open" {
		t.Errorf("unexpected state: %s", pr.State)
	}
}

func TestClient_FetchPR_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
	}))
	defer srv.Close()

	c := mustNew(t, "ghp_test", WithBaseURL(srv.URL+"/"))
	_, err := c.FetchPR(context.Background(), "o", "r", 999)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

func TestClient_FetchCheckRuns_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"message": "Bad credentials"})
	}))
	defer srv.Close()

	c := mustNew(t, "bad-token", WithBaseURL(srv.URL+"/"))
	_, err := c.FetchCheckRuns(context.Background(), "o", "r", "sha")
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func mustNew(t *testing.T, token string, opts ...Option) *Client {
	t.Helper()
	c, err := New(token, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func assertAuth(t *testing.T, r *http.Request, expected string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != expected {
		t.Errorf("expected Authorization %q, got %q", expected, got)
	}
}
