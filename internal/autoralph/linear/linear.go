package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/retry"
)

var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Issue represents a Linear issue.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	State       WorkflowState
}

// Comment represents a Linear comment on an issue.
type Comment struct {
	ID        string
	ParentID  string // Non-empty for threaded replies.
	Body      string
	UserName  string
	CreatedAt string
}

// WorkflowState represents a Linear workflow state.
type WorkflowState struct {
	ID   string
	Name string
	Type string
}

// Client is a typed Linear API client using GraphQL over net/http.
type Client struct {
	apiKey       string
	httpClient   *http.Client
	endpoint     string
	retryBackoff []time.Duration
}

// New creates a new Linear GraphQL client.
// Use WithEndpoint to override the default Linear API URL (useful for testing).
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		endpoint:   "https://api.linear.app/graphql",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Option configures a Client.
type Option func(*Client)

// WithEndpoint overrides the GraphQL endpoint URL.
func WithEndpoint(url string) Option {
	return func(c *Client) { c.endpoint = url }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithRetryBackoff overrides the default retry backoff delays.
func WithRetryBackoff(delays ...time.Duration) Option {
	return func(c *Client) { c.retryBackoff = delays }
}

// graphqlRequest is the JSON body sent to the GraphQL endpoint.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphqlResponse is the top-level JSON wrapper from the GraphQL endpoint.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphqlError  `json:"errors,omitempty"`
}

// graphqlError represents a single GraphQL error.
type graphqlError struct {
	Message    string         `json:"message"`
	Extensions graphqlErrExt  `json:"extensions,omitempty"`
}

type graphqlErrExt struct {
	Code             string                   `json:"code,omitempty"`
	ValidationErrors []map[string]interface{} `json:"validationErrors,omitempty"`
}

func (e graphqlError) detail() string {
	if e.Extensions.Code == "" {
		return e.Message
	}
	msg := e.Message + " [" + e.Extensions.Code + "]"
	for _, ve := range e.Extensions.ValidationErrors {
		if prop, ok := ve["property"].(string); ok {
			if children, ok := ve["children"].([]interface{}); ok {
				for _, child := range children {
					if cm, ok := child.(map[string]interface{}); ok {
						if constraints, ok := cm["constraints"].(map[string]interface{}); ok {
							for _, v := range constraints {
								msg += fmt.Sprintf("; %s: %v", prop, v)
							}
						}
					}
				}
			}
		}
	}
	return msg
}

// execute sends a GraphQL request and returns the raw data payload.
// It retries on transient errors (HTTP 5xx, network errors) with exponential backoff.
func (c *Client) execute(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	var opts []retry.Option
	if len(c.retryBackoff) > 0 {
		opts = append(opts, retry.WithBackoff(c.retryBackoff...))
	}
	return retry.DoVal(ctx, func() (json.RawMessage, error) {
		return c.executeOnce(ctx, query, vars)
	}, opts...)
}

func (c *Client) executeOnce(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(graphqlRequest{Query: query, Variables: vars})
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("marshaling request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("creating request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("linear API returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, retry.Permanent(fmt.Errorf("linear API returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200)))
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, retry.Permanent(fmt.Errorf("decoding response: %w", err))
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.detail()
		}
		return nil, retry.Permanent(fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; ")))
	}

	return gqlResp.Data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// FetchAssignedIssues returns issues assigned to assigneeID in the given team.
func (c *Client) FetchAssignedIssues(ctx context.Context, teamID, assigneeID string) ([]Issue, error) {
	const query = `query($teamID: ID!, $assigneeID: ID!) {
  issues(filter: {
    team: { id: { eq: $teamID } }
    assignee: { id: { eq: $assigneeID } }
    state: { type: { nin: ["canceled", "completed"] } }
  }) {
    nodes {
      id
      identifier
      title
      description
      state { id name type }
    }
  }
}`
	vars := map[string]any{"teamID": teamID, "assigneeID": assigneeID}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return nil, fmt.Errorf("fetching assigned issues: %w", err)
	}

	var result struct {
		Issues struct {
			Nodes []issueNode `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding assigned issues: %w", err)
	}

	issues := make([]Issue, len(result.Issues.Nodes))
	for i, n := range result.Issues.Nodes {
		issues[i] = n.toIssue()
	}
	return issues, nil
}

// FetchIssueComments returns comments on the given issue, including threaded
// replies. The result is flattened into a single chronologically-sorted slice.
// Replies have a non-empty ParentID.
func (c *Client) FetchIssueComments(ctx context.Context, issueID string) ([]Comment, error) {
	const query = `query($issueID: String!) {
  issue(id: $issueID) {
    comments {
      nodes {
        id
        parentId
        body
        user { name }
        createdAt
        children {
          nodes {
            id
            parentId
            body
            user { name }
            createdAt
          }
        }
      }
    }
  }
}`
	vars := map[string]any{"issueID": issueID}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return nil, fmt.Errorf("fetching issue comments: %w", err)
	}

	var result struct {
		Issue struct {
			Comments struct {
				Nodes []commentNode `json:"nodes"`
			} `json:"comments"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding issue comments: %w", err)
	}

	// Flatten: for each top-level comment, append it and then its children.
	var comments []Comment
	for _, n := range result.Issue.Comments.Nodes {
		comments = append(comments, n.toComment())
		if n.Children != nil {
			for _, child := range n.Children.Nodes {
				comments = append(comments, child.toComment())
			}
		}
	}

	// Sort chronologically.
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt < comments[j].CreatedAt
	})
	return comments, nil
}

// PostComment creates a comment on the given issue and returns it.
func (c *Client) PostComment(ctx context.Context, issueID, body string) (Comment, error) {
	const query = `mutation($issueID: String!, $body: String!) {
  commentCreate(input: { issueId: $issueID, body: $body }) {
    comment {
      id
      body
      user { name }
      createdAt
    }
  }
}`
	vars := map[string]any{"issueID": issueID, "body": body}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return Comment{}, fmt.Errorf("posting comment: %w", err)
	}

	var result struct {
		CommentCreate struct {
			Comment commentNode `json:"comment"`
		} `json:"commentCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Comment{}, fmt.Errorf("decoding posted comment: %w", err)
	}

	return result.CommentCreate.Comment.toComment(), nil
}

// PostReply creates a threaded reply under the given parent comment.
func (c *Client) PostReply(ctx context.Context, parentID, body string) (Comment, error) {
	const query = `mutation($parentID: String!, $body: String!) {
  commentCreate(input: { parentId: $parentID, body: $body }) {
    comment {
      id
      parentId
      body
      user { name }
      createdAt
    }
  }
}`
	vars := map[string]any{"parentID": parentID, "body": body}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return Comment{}, fmt.Errorf("posting reply: %w", err)
	}

	var result struct {
		CommentCreate struct {
			Comment commentNode `json:"comment"`
		} `json:"commentCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Comment{}, fmt.Errorf("decoding posted reply: %w", err)
	}

	return result.CommentCreate.Comment.toComment(), nil
}

// UpdateIssueState transitions the issue to the given workflow state.
func (c *Client) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	const query = `mutation($issueID: String!, $stateID: String!) {
  issueUpdate(id: $issueID, input: { stateId: $stateID }) {
    success
    issue { id }
  }
}`
	vars := map[string]any{"issueID": issueID, "stateID": stateID}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return fmt.Errorf("updating issue state: %w", err)
	}

	var result struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decoding issue update response: %w", err)
	}

	if !result.IssueUpdate.Success {
		return fmt.Errorf("linear reported issue update as unsuccessful")
	}
	return nil
}

// FetchWorkflowStates returns the workflow states for the given team.
func (c *Client) FetchWorkflowStates(ctx context.Context, teamID string) ([]WorkflowState, error) {
	const query = `query($teamID: String!) {
  team(id: $teamID) {
    states {
      nodes {
        id
        name
        type
      }
    }
  }
}`
	vars := map[string]any{"teamID": teamID}
	data, err := c.execute(ctx, query, vars)
	if err != nil {
		return nil, fmt.Errorf("fetching workflow states: %w", err)
	}

	var result struct {
		Team struct {
			States struct {
				Nodes []workflowStateNode `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding workflow states: %w", err)
	}

	states := make([]WorkflowState, len(result.Team.States.Nodes))
	for i, n := range result.Team.States.Nodes {
		states[i] = WorkflowState{ID: n.ID, Name: n.Name, Type: n.Type}
	}
	return states, nil
}

// ResolveTeamID resolves a team identifier (UUID, key, or name) to a team UUID.
// If the identifier is already a UUID, it is returned as-is.
func (c *Client) ResolveTeamID(ctx context.Context, identifier string) (string, error) {
	if isUUID(identifier) {
		return identifier, nil
	}

	const query = `query {
  teams {
    nodes { id key name }
  }
}`
	data, err := c.execute(ctx, query, nil)
	if err != nil {
		return "", fmt.Errorf("fetching teams: %w", err)
	}

	var result struct {
		Teams struct {
			Nodes []struct {
				ID   string `json:"id"`
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding teams: %w", err)
	}

	lower := strings.ToLower(identifier)
	for _, t := range result.Teams.Nodes {
		if strings.ToLower(t.Key) == lower || strings.ToLower(t.Name) == lower {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("team not found: %q (tried matching key and name)", identifier)
}

// ResolveUserID resolves a user identifier (UUID, displayName, name, or email) to a user UUID.
// If the identifier is already a UUID, it is returned as-is.
func (c *Client) ResolveUserID(ctx context.Context, identifier string) (string, error) {
	if isUUID(identifier) {
		return identifier, nil
	}

	const query = `query {
  users {
    nodes { id name displayName email }
  }
}`
	data, err := c.execute(ctx, query, nil)
	if err != nil {
		return "", fmt.Errorf("fetching users: %w", err)
	}

	var result struct {
		Users struct {
			Nodes []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Email       string `json:"email"`
			} `json:"nodes"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding users: %w", err)
	}

	lower := strings.ToLower(identifier)
	for _, u := range result.Users.Nodes {
		if strings.ToLower(u.DisplayName) == lower ||
			strings.ToLower(u.Name) == lower ||
			strings.ToLower(u.Email) == lower {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("user not found: %q (tried matching displayName, name, and email)", identifier)
}

func isUUID(s string) bool {
	return uuidRegexp.MatchString(strings.ToLower(s))
}

// issueNode is the GraphQL response shape for an issue.
type issueNode struct {
	ID          string            `json:"id"`
	Identifier  string            `json:"identifier"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	State       workflowStateNode `json:"state"`
}

func (n issueNode) toIssue() Issue {
	return Issue{
		ID:          n.ID,
		Identifier:  n.Identifier,
		Title:       n.Title,
		Description: n.Description,
		State:       WorkflowState{ID: n.State.ID, Name: n.State.Name, Type: n.State.Type},
	}
}

// commentNode is the GraphQL response shape for a comment.
type commentNode struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Body     string `json:"body"`
	User     struct {
		Name string `json:"name"`
	} `json:"user"`
	CreatedAt string `json:"createdAt"`
	Children  *struct {
		Nodes []commentNode `json:"nodes"`
	} `json:"children,omitempty"`
}

func (n commentNode) toComment() Comment {
	return Comment{
		ID:        n.ID,
		ParentID:  n.ParentID,
		Body:      n.Body,
		UserName:  n.User.Name,
		CreatedAt: n.CreatedAt,
	}
}

// workflowStateNode is the GraphQL response shape for a workflow state.
type workflowStateNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
