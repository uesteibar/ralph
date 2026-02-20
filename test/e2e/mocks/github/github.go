package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// PR is a mock GitHub pull request.
type PR struct {
	Number  int
	HTMLURL string
	Title   string
	Body    string
	Head    string
	Base    string
	State   string
	Merged  bool
	HeadSHA string
}

// Review is a mock GitHub pull request review.
type Review struct {
	ID     int64
	State  string
	Body   string
	User   string
	UserID int64
}

// Comment is a mock GitHub pull request review comment.
type Comment struct {
	ID        int64
	Body      string
	Path      string
	User      string
	InReplyTo int64
}

// IssueComment is a mock GitHub issue comment (general PR comment).
type IssueComment struct {
	ID   int64
	Body string
	User string
}

// CreatedPR records a PR creation request received by the mock.
type CreatedPR struct {
	Owner string
	Repo  string
	Head  string
	Base  string
	Title string
	Body  string
}

// PostedComment records a comment posted via the mock API.
type PostedComment struct {
	Owner    string
	Repo     string
	PRNumber int
	Body     string
}

// PostedReply records a review reply posted via the mock API.
type PostedReply struct {
	Owner     string
	Repo      string
	PRNumber  int
	CommentID int64
	Body      string
}

// EditedPR records a PR edit request received by the mock.
type EditedPR struct {
	Owner    string
	Repo     string
	PRNumber int
	Title    string
	Body     string
}

// Mock is an in-memory mock of the GitHub REST API, compatible with go-github client.
// Routes are served under /api/v3/ prefix to match go-github's WithEnterpriseURLs.
type Mock struct {
	mu            sync.Mutex
	prs           map[string][]PR           // "owner/repo" → PRs
	reviews       map[string][]Review       // "owner/repo/prNumber" → reviews
	comments      map[string][]Comment      // "owner/repo/prNumber" → review comments
	issueComments map[string][]IssueComment // "owner/repo/prNumber" → issue comments
	nextPR        int
	nextCommentID int64

	// Tracking for verification
	CreatedPRs     []CreatedPR
	EditedPRs      []EditedPR
	PostedComments []PostedComment
	PostedReplies  []PostedReply
}

// New creates a new empty GitHub mock.
func New() *Mock {
	return &Mock{
		prs:           make(map[string][]PR),
		reviews:       make(map[string][]Review),
		comments:      make(map[string][]Comment),
		issueComments: make(map[string][]IssueComment),
		nextPR:        1,
		nextCommentID: 1000,
	}
}

// Server starts an httptest.Server serving the mock and registers cleanup.
func (m *Mock) Server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// Handler returns an http.Handler for the mock GitHub REST API.
func (m *Mock) Handler() http.Handler {
	mux := http.NewServeMux()
	// go-github WithEnterpriseURLs adds /api/v3/ prefix.
	mux.HandleFunc("/api/v3/repos/", m.handleRepos)
	return mux
}

// AddPR adds a pull request to the mock. Returns the PR for chaining.
func (m *Mock) AddPR(owner, repo string, pr PR) *PR {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + repo
	m.prs[key] = append(m.prs[key], pr)
	return &m.prs[key][len(m.prs[key])-1]
}

// AddReview adds a review to a PR.
func (m *Mock) AddReview(owner, repo string, prNumber int, r Review) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + repo + "/" + strconv.Itoa(prNumber)
	m.reviews[key] = append(m.reviews[key], r)
}

// AddComment adds a review comment to a PR.
func (m *Mock) AddComment(owner, repo string, prNumber int, c Comment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + repo + "/" + strconv.Itoa(prNumber)
	m.comments[key] = append(m.comments[key], c)
}

// SimulateMerge marks a PR as merged.
func (m *Mock) SimulateMerge(owner, repo string, prNumber int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + repo
	for i, pr := range m.prs[key] {
		if pr.Number == prNumber {
			m.prs[key][i].Merged = true
			m.prs[key][i].State = "closed"
			return
		}
	}
}

// SimulateChangesRequested adds a "CHANGES_REQUESTED" review to a PR.
func (m *Mock) SimulateChangesRequested(owner, repo string, prNumber int, reviewID int64, body string) {
	m.AddReview(owner, repo, prNumber, Review{
		ID:    reviewID,
		State: "CHANGES_REQUESTED",
		Body:  body,
		User:  "test-reviewer",
	})
}

// GetPR returns a PR by number, or nil if not found.
func (m *Mock) GetPR(owner, repo string, prNumber int) *PR {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + repo
	for i, pr := range m.prs[key] {
		if pr.Number == prNumber {
			return &m.prs[key][i]
		}
	}
	return nil
}

func (m *Mock) handleRepos(w http.ResponseWriter, r *http.Request) {
	// Parse: /api/v3/repos/{owner}/{repo}/...
	path := strings.TrimPrefix(r.URL.Path, "/api/v3/repos/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}
	owner := parts[0]
	repo := parts[1]
	rest := parts[2]

	switch {
	case rest == "pulls" && r.Method == http.MethodPost:
		m.handleCreatePR(w, r, owner, repo)
	case rest == "pulls" && r.Method == http.MethodGet:
		m.handleListPRs(w, r, owner, repo)
	case strings.HasPrefix(rest, "pulls/") && strings.HasSuffix(rest, "/merge") && r.Method == http.MethodGet:
		m.handleIsMerged(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "pulls/") && strings.HasSuffix(rest, "/reviews") && r.Method == http.MethodGet:
		m.handleListReviews(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "pulls/") && strings.Contains(rest, "/comments") && r.Method == http.MethodGet:
		m.handleListComments(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "pulls/") && strings.Contains(rest, "/comments") && r.Method == http.MethodPost:
		m.handleCreateReply(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "pulls/") && !strings.Contains(rest[len("pulls/"):], "/") && r.Method == http.MethodPatch:
		m.handleEditPR(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "pulls/") && !strings.Contains(rest[len("pulls/"):], "/") && r.Method == http.MethodGet:
		m.handleGetPR(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "issues/") && strings.Contains(rest, "/comments") && r.Method == http.MethodPost:
		m.handleCreateIssueComment(w, r, owner, repo, rest)
	case strings.HasPrefix(rest, "commits/") && strings.HasSuffix(rest, "/check-runs") && r.Method == http.MethodGet:
		m.handleListCheckRuns(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *Mock) handleCreatePR(w http.ResponseWriter, r *http.Request, owner, repo string) {
	var body struct {
		Title *string `json:"title"`
		Head  *string `json:"head"`
		Base  *string `json:"base"`
		Body  *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prNum := m.nextPR
	m.nextPR++

	pr := PR{
		Number:  prNum,
		HTMLURL: "https://github.com/" + owner + "/" + repo + "/pull/" + strconv.Itoa(prNum),
		Title:   deref(body.Title),
		Body:    deref(body.Body),
		Head:    deref(body.Head),
		Base:    deref(body.Base),
		State:   "open",
	}

	key := owner + "/" + repo
	m.prs[key] = append(m.prs[key], pr)
	m.CreatedPRs = append(m.CreatedPRs, CreatedPR{
		Owner: owner,
		Repo:  repo,
		Head:  pr.Head,
		Base:  pr.Base,
		Title: pr.Title,
		Body:  pr.Body,
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(prToJSON(pr))
}

func (m *Mock) handleListPRs(w http.ResponseWriter, r *http.Request, owner, repo string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	head := r.URL.Query().Get("head")
	base := r.URL.Query().Get("base")
	state := r.URL.Query().Get("state")

	key := owner + "/" + repo
	var result []map[string]any
	for _, pr := range m.prs[key] {
		if state != "" && pr.State != state {
			continue
		}
		if head != "" && owner+":"+pr.Head != head {
			continue
		}
		if base != "" && pr.Base != base {
			continue
		}
		result = append(result, prToJSON(pr))
	}

	json.NewEncoder(w).Encode(result)
}

func (m *Mock) handleIsMerged(w http.ResponseWriter, _ *http.Request, owner, repo, rest string) {
	// rest = "pulls/{number}/merge"
	prNum := extractPRNumber(rest)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := owner + "/" + repo
	for _, pr := range m.prs[key] {
		if pr.Number == prNum && pr.Merged {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (m *Mock) handleListReviews(w http.ResponseWriter, _ *http.Request, owner, repo, rest string) {
	// rest = "pulls/{number}/reviews"
	prNum := extractPRNumber(rest)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := owner + "/" + repo + "/" + strconv.Itoa(prNum)
	reviews := m.reviews[key]

	var result []map[string]any
	for _, rev := range reviews {
		result = append(result, map[string]any{
			"id":    rev.ID,
			"state": rev.State,
			"body":  rev.Body,
			"user":  map[string]any{"login": rev.User, "id": rev.UserID},
		})
	}

	if result == nil {
		result = []map[string]any{}
	}
	json.NewEncoder(w).Encode(result)
}

func (m *Mock) handleListComments(w http.ResponseWriter, _ *http.Request, owner, repo, rest string) {
	// rest = "pulls/{number}/comments"
	prNum := extractPRNumber(rest)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := owner + "/" + repo + "/" + strconv.Itoa(prNum)
	comments := m.comments[key]

	var result []map[string]any
	for _, c := range comments {
		entry := map[string]any{
			"id":   c.ID,
			"body": c.Body,
			"path": c.Path,
			"user": map[string]any{"login": c.User},
		}
		if c.InReplyTo != 0 {
			entry["in_reply_to_id"] = c.InReplyTo
		}
		result = append(result, entry)
	}

	if result == nil {
		result = []map[string]any{}
	}
	json.NewEncoder(w).Encode(result)
}

func (m *Mock) handleCreateReply(w http.ResponseWriter, r *http.Request, owner, repo, rest string) {
	prNum := extractPRNumber(rest)

	var body struct {
		Body      string `json:"body"`
		InReplyTo int64  `json:"in_reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextCommentID++
	c := Comment{
		ID:        m.nextCommentID,
		Body:      body.Body,
		User:      "autoralph",
		InReplyTo: body.InReplyTo,
	}

	key := owner + "/" + repo + "/" + strconv.Itoa(prNum)
	m.comments[key] = append(m.comments[key], c)
	m.PostedReplies = append(m.PostedReplies, PostedReply{
		Owner:     owner,
		Repo:      repo,
		PRNumber:  prNum,
		CommentID: body.InReplyTo,
		Body:      body.Body,
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":   c.ID,
		"body": c.Body,
		"user": map[string]any{"login": c.User},
	})
}

func (m *Mock) handleGetPR(w http.ResponseWriter, _ *http.Request, owner, repo, rest string) {
	prNum := extractPRNumber(rest)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := owner + "/" + repo
	for _, pr := range m.prs[key] {
		if pr.Number == prNum {
			json.NewEncoder(w).Encode(prToJSON(pr))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (m *Mock) handleEditPR(w http.ResponseWriter, r *http.Request, owner, repo, rest string) {
	prNum := extractPRNumber(rest)

	var body struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := owner + "/" + repo
	for i, pr := range m.prs[key] {
		if pr.Number == prNum {
			if body.Title != nil {
				m.prs[key][i].Title = *body.Title
			}
			if body.Body != nil {
				m.prs[key][i].Body = *body.Body
			}
			m.EditedPRs = append(m.EditedPRs, EditedPR{
				Owner:    owner,
				Repo:     repo,
				PRNumber: prNum,
				Title:    m.prs[key][i].Title,
				Body:     m.prs[key][i].Body,
			})
			json.NewEncoder(w).Encode(prToJSON(m.prs[key][i]))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (m *Mock) handleListCheckRuns(w http.ResponseWriter, _ *http.Request) {
	// Return an empty list of check runs by default.
	json.NewEncoder(w).Encode(map[string]any{
		"total_count": 0,
		"check_runs":  []any{},
	})
}

func (m *Mock) handleCreateIssueComment(w http.ResponseWriter, r *http.Request, owner, repo, rest string) {
	// rest = "issues/{number}/comments"
	prNum := extractIssueNumber(rest)

	var body struct {
		Body *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextCommentID++
	ic := IssueComment{
		ID:   m.nextCommentID,
		Body: deref(body.Body),
		User: "autoralph",
	}

	key := owner + "/" + repo + "/" + strconv.Itoa(prNum)
	m.issueComments[key] = append(m.issueComments[key], ic)
	m.PostedComments = append(m.PostedComments, PostedComment{
		Owner:    owner,
		Repo:     repo,
		PRNumber: prNum,
		Body:     ic.Body,
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":   ic.ID,
		"body": ic.Body,
		"user": map[string]any{"login": ic.User},
	})
}

func extractPRNumber(rest string) int {
	// rest = "pulls/{number}/..." — extract number
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 {
		n, _ := strconv.Atoi(parts[1])
		return n
	}
	return 0
}

func extractIssueNumber(rest string) int {
	// rest = "issues/{number}/..." — extract number
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 {
		n, _ := strconv.Atoi(parts[1])
		return n
	}
	return 0
}

func prToJSON(pr PR) map[string]any {
	headSHA := pr.HeadSHA
	if headSHA == "" {
		headSHA = "0000000000000000000000000000000000000000"
	}
	return map[string]any{
		"number":   pr.Number,
		"html_url": pr.HTMLURL,
		"title":    pr.Title,
		"body":     pr.Body,
		"head":     map[string]any{"ref": pr.Head, "sha": headSHA},
		"base":     map[string]any{"ref": pr.Base},
		"state":    pr.State,
		"merged":   pr.Merged,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
