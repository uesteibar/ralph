package linear

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"time"
	"sync"
	"testing"
)

// Deterministic UUID constants for test data.
// These satisfy UUID format validation while remaining traceable in test output.
const (
	TestTeamID     = "b80bc599-0000-0000-0000-000000000001"
	TestAssigneeID = "fa0cbfe1-0000-0000-0000-000000000001"

	StateBacklogID  = "00000000-0000-4000-8000-00000backlog"
	StateTodoID     = "00000000-0000-4000-8000-0000000todo00"
	StateProgressID = "00000000-0000-4000-8000-000progress00"
	StateDoneID     = "00000000-0000-4000-8000-0000000done00"
	StateCanceledID = "00000000-0000-4000-8000-000canceled00"
)

var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IssueUUID generates a deterministic UUID from a human-readable identifier.
// e.g., IssueUUID("avatars") → "a1b2c3d4-... " (consistent across runs).
func IssueUUID(name string) string {
	h := sha256.Sum256([]byte("issue:" + name))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// CommentUUID generates a deterministic UUID from a human-readable comment name.
func CommentUUID(name string) string {
	h := sha256.Sum256([]byte("comment:" + name))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func isValidUUID(s string) bool {
	return uuidRegexp.MatchString(strings.ToLower(s))
}

// Issue is a mock Linear issue.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	StateID     string
	StateName   string
	StateType   string
}

// Comment is a mock Linear comment.
type Comment struct {
	ID        string
	ParentID  string // Non-empty for threaded replies.
	Body      string
	UserName  string
	CreatedAt string
}

// WorkflowState is a mock Linear workflow state.
type WorkflowState struct {
	ID   string
	Name string
	Type string
}

// Team is a mock Linear team.
type Team struct {
	ID   string
	Key  string
	Name string
}

// User is a mock Linear user.
type User struct {
	ID          string
	Name        string
	DisplayName string
	Email       string
}

// Mock is an in-memory mock of the Linear GraphQL API.
// It enforces UUID format on ID fields to match real Linear API behavior.
type Mock struct {
	mu       sync.Mutex
	issues   map[string]*Issue
	comments map[string][]Comment // issueID → comments
	states   []WorkflowState
	teams    []Team
	users    []User
	nextID   int

	// Tracking
	ReceivedComments []PostedComment
	StateUpdates     []StateUpdate
}

// PostedComment records a comment that was posted via the API.
type PostedComment struct {
	IssueID string
	Body    string
}

// StateUpdate records a state change that was requested via the API.
type StateUpdate struct {
	IssueID string
	StateID string
}

// New creates a new empty mock with default workflow states using UUID-format IDs.
func New() *Mock {
	return &Mock{
		issues:   make(map[string]*Issue),
		comments: make(map[string][]Comment),
		states: []WorkflowState{
			{ID: StateBacklogID, Name: "Backlog", Type: "backlog"},
			{ID: StateTodoID, Name: "Todo", Type: "unstarted"},
			{ID: StateProgressID, Name: "In Progress", Type: "started"},
			{ID: StateDoneID, Name: "Done", Type: "completed"},
			{ID: StateCanceledID, Name: "Canceled", Type: "canceled"},
		},
		nextID: 1000,
	}
}

// Server starts an httptest.Server serving the mock and registers cleanup.
func (m *Mock) Server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// Handler returns an http.Handler for the mock GraphQL endpoint.
func (m *Mock) Handler() http.Handler {
	return http.HandlerFunc(m.handleGraphQL)
}

// AddIssue adds an issue to the mock. Returns the issue for chaining.
func (m *Mock) AddIssue(issue Issue) *Issue {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := issue
	m.issues[issue.ID] = &stored
	return &stored
}

// AddComment adds a comment to an issue.
func (m *Mock) AddComment(issueID string, c Comment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments[issueID] = append(m.comments[issueID], c)
}

// SimulateApproval adds an "@autoralph approved" comment to the issue.
// Uses a far-future timestamp so approval sorts after any AI-posted comments.
func (m *Mock) SimulateApproval(issueID, commentID string) {
	m.AddComment(issueID, Comment{
		ID:        commentID,
		Body:      "@autoralph approved",
		UserName:  "test-user",
		CreatedAt: "2099-01-01T00:00:00Z",
	})
}

// AddTeam adds a team to the mock for teams query resolution.
func (m *Mock) AddTeam(team Team) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teams = append(m.teams, team)
}

// AddUser adds a user to the mock for users query resolution.
func (m *Mock) AddUser(user User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users = append(m.users, user)
}

// GetIssue returns an issue by ID, or nil if not found.
func (m *Mock) GetIssue(id string) *Issue {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.issues[id]
}

// GetComments returns comments for an issue.
func (m *Mock) GetComments(issueID string) []Comment {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Comment(nil), m.comments[issueID]...)
}

func (m *Mock) genUUID() string {
	m.nextID++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", m.nextID)
}

func (m *Mock) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeGQLError(w, "invalid request body")
		return
	}

	q := req.Query
	switch {
	case strings.Contains(q, "issues(filter"):
		m.handleFetchIssues(w, req.Variables)
	case strings.Contains(q, "issue(id"):
		m.handleFetchComments(w, req.Variables)
	case strings.Contains(q, "commentCreate"):
		m.handlePostComment(w, req.Variables)
	case strings.Contains(q, "issueUpdate"):
		m.handleUpdateState(w, req.Variables)
	case strings.Contains(q, "team(id"):
		m.handleFetchStates(w, req.Variables)
	case strings.Contains(q, "teams"):
		m.handleFetchTeams(w)
	case strings.Contains(q, "users"):
		m.handleFetchUsers(w)
	default:
		writeGQLError(w, "unrecognized query")
	}
}

func (m *Mock) handleFetchIssues(w http.ResponseWriter, vars map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamID, _ := vars["teamID"].(string)
	assigneeID, _ := vars["assigneeID"].(string)

	// Validate UUID format like real Linear API.
	if teamID != "" && !isValidUUID(teamID) {
		writeGQLValidationError(w, "team", "eq")
		return
	}
	if assigneeID != "" && !isValidUUID(assigneeID) {
		writeGQLValidationError(w, "assignee", "eq")
		return
	}

	var nodes []map[string]any
	for _, iss := range m.issues {
		if iss.StateType == "canceled" || iss.StateType == "completed" {
			continue
		}
		nodes = append(nodes, map[string]any{
			"id":          iss.ID,
			"identifier":  iss.Identifier,
			"title":       iss.Title,
			"description": iss.Description,
			"state": map[string]any{
				"id":   iss.StateID,
				"name": iss.StateName,
				"type": iss.StateType,
			},
		})
	}

	writeGQLData(w, map[string]any{
		"issues": map[string]any{"nodes": nodes},
	})
}

func (m *Mock) handleFetchComments(w http.ResponseWriter, vars map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	issueID, _ := vars["issueID"].(string)
	if issueID != "" && !isValidUUID(issueID) {
		writeGQLValidationError(w, "id", "id")
		return
	}
	comments := m.comments[issueID]

	// Separate top-level and child comments.
	topLevel := make([]Comment, 0)
	childrenOf := make(map[string][]Comment) // parentID → children
	for _, c := range comments {
		if c.ParentID == "" {
			topLevel = append(topLevel, c)
		} else {
			childrenOf[c.ParentID] = append(childrenOf[c.ParentID], c)
		}
	}

	var nodes []map[string]any
	for _, c := range topLevel {
		node := map[string]any{
			"id":        c.ID,
			"parentId":  c.ParentID,
			"body":      c.Body,
			"user":      map[string]any{"name": c.UserName},
			"createdAt": c.CreatedAt,
		}
		// Include children.
		var childNodes []map[string]any
		for _, child := range childrenOf[c.ID] {
			childNodes = append(childNodes, map[string]any{
				"id":        child.ID,
				"parentId":  child.ParentID,
				"body":      child.Body,
				"user":      map[string]any{"name": child.UserName},
				"createdAt": child.CreatedAt,
			})
		}
		node["children"] = map[string]any{"nodes": childNodes}
		nodes = append(nodes, node)
	}

	writeGQLData(w, map[string]any{
		"issue": map[string]any{
			"comments": map[string]any{"nodes": nodes},
		},
	})
}

func (m *Mock) handlePostComment(w http.ResponseWriter, vars map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	issueID, _ := vars["issueID"].(string)
	parentID, _ := vars["parentID"].(string)
	body, _ := vars["body"].(string)

	// For replies, parentID is set; for top-level, issueID is set.
	if issueID != "" && !isValidUUID(issueID) {
		writeGQLValidationError(w, "issueId", "issueId")
		return
	}

	c := Comment{
		ID:        m.genUUID(),
		ParentID:  parentID,
		Body:      body,
		UserName:  "autoralph",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// For replies, find the issue that owns the parent comment.
	targetIssue := issueID
	if parentID != "" && targetIssue == "" {
		for iss, comments := range m.comments {
			for _, existing := range comments {
				if existing.ID == parentID {
					targetIssue = iss
					break
				}
			}
		}
	}
	m.comments[targetIssue] = append(m.comments[targetIssue], c)
	m.ReceivedComments = append(m.ReceivedComments, PostedComment{
		IssueID: targetIssue,
		Body:    body,
	})

	writeGQLData(w, map[string]any{
		"commentCreate": map[string]any{
			"comment": map[string]any{
				"id":        c.ID,
				"parentId":  c.ParentID,
				"body":      c.Body,
				"user":      map[string]any{"name": c.UserName},
				"createdAt": c.CreatedAt,
			},
		},
	})
}

func (m *Mock) handleUpdateState(w http.ResponseWriter, vars map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	issueID, _ := vars["issueID"].(string)
	stateID, _ := vars["stateID"].(string)

	if issueID != "" && !isValidUUID(issueID) {
		writeGQLValidationError(w, "id", "id")
		return
	}

	m.StateUpdates = append(m.StateUpdates, StateUpdate{
		IssueID: issueID,
		StateID: stateID,
	})

	if iss, ok := m.issues[issueID]; ok {
		iss.StateID = stateID
		for _, s := range m.states {
			if s.ID == stateID {
				iss.StateName = s.Name
				iss.StateType = s.Type
				break
			}
		}
	}

	writeGQLData(w, map[string]any{
		"issueUpdate": map[string]any{
			"success": true,
			"issue":   map[string]any{"id": issueID},
		},
	})
}

func (m *Mock) handleFetchStates(w http.ResponseWriter, vars map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamID, _ := vars["teamID"].(string)
	if teamID != "" && !isValidUUID(teamID) {
		writeGQLValidationError(w, "id", "id")
		return
	}

	var nodes []map[string]any
	for _, s := range m.states {
		nodes = append(nodes, map[string]any{
			"id":   s.ID,
			"name": s.Name,
			"type": s.Type,
		})
	}

	writeGQLData(w, map[string]any{
		"team": map[string]any{
			"states": map[string]any{"nodes": nodes},
		},
	})
}

func (m *Mock) handleFetchTeams(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var nodes []map[string]any
	for _, t := range m.teams {
		nodes = append(nodes, map[string]any{
			"id":   t.ID,
			"key":  t.Key,
			"name": t.Name,
		})
	}

	writeGQLData(w, map[string]any{
		"teams": map[string]any{"nodes": nodes},
	})
}

func (m *Mock) handleFetchUsers(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var nodes []map[string]any
	for _, u := range m.users {
		nodes = append(nodes, map[string]any{
			"id":          u.ID,
			"name":        u.Name,
			"displayName": u.DisplayName,
			"email":       u.Email,
		})
	}

	writeGQLData(w, map[string]any{
		"users": map[string]any{"nodes": nodes},
	})
}

func writeGQLData(w http.ResponseWriter, data any) {
	raw, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"data": json.RawMessage(raw),
	})
}

func writeGQLError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"message": msg}},
	})
}

// writeGQLValidationError returns a realistic Linear-style validation error
// matching the real API's INVALID_INPUT error shape with isUuid constraints.
func writeGQLValidationError(w http.ResponseWriter, property, field string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{
			"message": "Argument Validation Error",
			"extensions": map[string]any{
				"code": "INVALID_INPUT",
				"validationErrors": []map[string]any{{
					"property": property,
					"children": []map[string]any{{
						"property": "id",
						"constraints": map[string]any{
							"isUuid": field + " must be a UUID",
						},
					}},
				}},
			},
		}},
	})
}
