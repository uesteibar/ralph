package approve

import (
	"context"
	"fmt"
	"regexp"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
)

// approvalPattern matches "I approve this" (case-insensitive) anywhere in the comment.
// This explicit phrase works even when the user and autoralph share the same Linear account.
var approvalPattern = regexp.MustCompile(`(?i)\bI approve this\b`)

// ApprovalHint is appended to refinement and iteration comments to tell the
// user how to approve the plan.
const ApprovalHint = "\n\n---\n_To approve this plan, reply with **I approve this**. To request changes, reply with your feedback._"

// Invoker invokes an AI model with a prompt and returns the response.
// Dir sets the working directory for the AI process.
type Invoker interface {
	Invoke(ctx context.Context, prompt, dir string) (string, error)
}

// CommentClient fetches and posts comments on Linear issues.
type CommentClient interface {
	FetchIssueComments(ctx context.Context, issueID string) ([]linear.Comment, error)
	PostComment(ctx context.Context, issueID, body string) (linear.Comment, error)
	PostReply(ctx context.Context, parentID, body string) (linear.Comment, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// Config holds the dependencies for the approve transition actions.
type Config struct {
	Invoker       Invoker
	Comments      CommentClient
	Projects      ProjectGetter
	OverrideDir   string
}

// HasNewComments returns true if there are comments newer than the issue's
// LastCommentID. This is used as a condition for both the approval and
// iteration transitions.
func HasNewComments(comments CommentClient) func(issue db.Issue) bool {
	return func(issue db.Issue) bool {
		cs, err := comments.FetchIssueComments(context.Background(), issue.LinearIssueID)
		if err != nil || len(cs) == 0 {
			return false
		}
		if issue.LastCommentID == "" {
			return true
		}
		last := cs[len(cs)-1]
		return last.ID != issue.LastCommentID
	}
}

// IsApproval returns true if any new comment (after LastCommentID) contains
// an approval mention like '@<bot-username> approved' (case-insensitive).
func IsApproval(comments CommentClient) func(issue db.Issue) bool {
	return func(issue db.Issue) bool {
		cs, err := comments.FetchIssueComments(context.Background(), issue.LinearIssueID)
		if err != nil || len(cs) == 0 {
			return false
		}
		newComments := commentsAfter(cs, issue.LastCommentID)
		for _, c := range newComments {
			if containsApproval(c.Body) {
				return true
			}
		}
		return false
	}
}

// IsIteration returns true if there are new comments but none contain the
// approval marker.
func IsIteration(comments CommentClient) func(issue db.Issue) bool {
	return func(issue db.Issue) bool {
		cs, err := comments.FetchIssueComments(context.Background(), issue.LinearIssueID)
		if err != nil || len(cs) == 0 {
			return false
		}
		newComments := commentsAfter(cs, issue.LastCommentID)
		if len(newComments) == 0 {
			return false
		}
		for _, c := range newComments {
			if containsApproval(c.Body) {
				return false
			}
		}
		return true
	}
}

// NewApprovalAction returns an ActionFunc that stores the plan text and updates
// the LastCommentID when an approval is detected.
func NewApprovalAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		cs, err := cfg.Comments.FetchIssueComments(context.Background(), issue.LinearIssueID)
		if err != nil {
			return fmt.Errorf("fetching comments for approval: %w", err)
		}

		// Find who approved.
		newComments := commentsAfter(cs, issue.LastCommentID)
		approver := ""
		for _, c := range newComments {
			if containsApproval(c.Body) {
				approver = c.UserName
				break
			}
		}

		// Find the last AI-posted plan in the thread (the most recent
		// autoralph comment before the approval).
		planText := extractPlanText(cs)
		issue.PlanText = planText
		issue.LastCommentID = cs[len(cs)-1].ID

		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("storing plan text: %w", err)
		}

		if err := database.LogActivity(
			issue.ID,
			"approval_detected",
			"",
			"",
			fmt.Sprintf("Plan approved by %s. Stored plan text (%d chars)", approver, len(planText)),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// NewIterationAction returns an ActionFunc that invokes AI with the full
// comment thread and posts the response. When the user's comment is a threaded
// reply, the response is posted in the same thread.
func NewIterationAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		cs, err := cfg.Comments.FetchIssueComments(context.Background(), issue.LinearIssueID)
		if err != nil {
			return fmt.Errorf("fetching comments for iteration: %w", err)
		}

		newComments := commentsAfter(cs, issue.LastCommentID)

		// Log that we detected a reply.
		replyAuthor := ""
		if len(newComments) > 0 {
			replyAuthor = newComments[0].UserName
		}
		_ = database.LogActivity(issue.ID, "reply_received", "", "", fmt.Sprintf("Reply from %s — invoking AI", replyAuthor))

		var aiComments []ai.RefineIssueComment
		for _, c := range cs {
			aiComments = append(aiComments, ai.RefineIssueComment{
				Author:    c.UserName,
				CreatedAt: c.CreatedAt,
				Body:      c.Body,
			})
		}

		prompt, err := ai.RenderRefineIssue(ai.RefineIssueData{
			Title:       issue.Title,
			Description: issue.Description,
			Comments:    aiComments,
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering refine prompt: %w", err)
		}

		response, err := cfg.Invoker.Invoke(context.Background(), prompt, project.LocalPath)
		if err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		responseWithHint := response + ApprovalHint

		// Reply in thread if the user's comment was a threaded reply,
		// otherwise post a top-level comment.
		threadParent := findThreadParent(newComments)
		var posted linear.Comment
		if threadParent != "" {
			posted, err = cfg.Comments.PostReply(context.Background(), threadParent, responseWithHint)
		} else {
			posted, err = cfg.Comments.PostComment(context.Background(), issue.LinearIssueID, responseWithHint)
		}
		if err != nil {
			return fmt.Errorf("posting reply: %w", err)
		}

		issue.LastCommentID = posted.ID
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("updating last comment ID: %w", err)
		}

		if err := database.LogActivity(
			issue.ID,
			"plan_iteration",
			"",
			"",
			fmt.Sprintf("Replied to %s: %s", replyAuthor, truncate(response, 200)),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// findThreadParent returns the parent comment ID to reply to. If any new
// comment is a threaded reply, we reply in the same thread. Returns "" for
// top-level comments.
func findThreadParent(newComments []linear.Comment) string {
	for _, c := range newComments {
		if c.ParentID != "" {
			return c.ParentID
		}
	}
	return ""
}

// commentsAfter returns comments that appear after the one with lastID.
// If lastID is empty, returns all comments.
func commentsAfter(comments []linear.Comment, lastID string) []linear.Comment {
	if lastID == "" {
		return comments
	}
	for i, c := range comments {
		if c.ID == lastID {
			return comments[i+1:]
		}
	}
	// lastID not found — treat all as new (defensive)
	return comments
}

// containsApproval checks if text contains an approval mention (case-insensitive).
// Matches "@<username> approved" where username is any non-whitespace sequence.
func containsApproval(text string) bool {
	return approvalPattern.MatchString(text)
}

// extractPlanText finds the most recent autoralph comment (the bot's last
// response) before the approval comment. This represents the plan that was
// approved. Falls back to concatenating all bot responses if no clear pattern.
func extractPlanText(comments []linear.Comment) string {
	// Walk backwards to find the approval comment, then the AI response before it.
	approvalIdx := -1
	for i := len(comments) - 1; i >= 0; i-- {
		if containsApproval(comments[i].Body) {
			approvalIdx = i
			break
		}
	}

	if approvalIdx <= 0 {
		// No approval found or it's the first comment — return empty
		return ""
	}

	// The comment immediately before the approval is the plan
	return comments[approvalIdx-1].Body
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
