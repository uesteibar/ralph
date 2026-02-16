package feedback

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/workspace"
)

// EventInvoker invokes an AI model with a prompt and an event handler for
// streaming tool-use events. Dir sets the working directory for the AI process.
type EventInvoker interface {
	InvokeWithEvents(ctx context.Context, prompt, dir string, handler events.EventHandler) (string, error)
}

// CommentFetcher fetches review comments from a GitHub PR.
type CommentFetcher interface {
	FetchPRComments(ctx context.Context, owner, repo string, prNumber int) ([]github.Comment, error)
}

// ReviewReplier replies to a review comment on a GitHub PR.
type ReviewReplier interface {
	PostReviewReply(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) (github.Comment, error)
}

// GitOps abstracts git operations for the feedback action.
type GitOps interface {
	Commit(ctx context.Context, workDir, message string) error
	PushBranch(ctx context.Context, workDir, branch string) error
	HeadSHA(ctx context.Context, workDir string) (string, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// ConfigLoader loads a Ralph config from a file path.
type ConfigLoader interface {
	Load(path string) (*config.Config, error)
}

// Config holds the dependencies for the feedback action.
type Config struct {
	Invoker      EventInvoker
	Comments     CommentFetcher
	Replier      ReviewReplier
	Git          GitOps
	Projects     ProjectGetter
	ConfigLoad   ConfigLoader
	EventHandler events.EventHandler
	OnBuildEvent func(issueID, detail string)
	OverrideDir  string
}

// IsAddressingFeedback returns true if the issue is in the addressing_feedback state.
func IsAddressingFeedback(issue db.Issue) bool {
	return orchestrator.IssueState(issue.State) == orchestrator.StateAddressingFeedback
}

// NewAction returns an orchestrator ActionFunc that addresses PR review feedback.
// It fetches review comments, invokes AI with the address_feedback.md prompt,
// commits and pushes changes, then replies to each comment on GitHub.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		ctx := context.Background()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		if err := database.LogActivity(issue.ID, "feedback_start", "", "", fmt.Sprintf("Addressing feedback for %s", issue.Identifier)); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		// Load quality checks from ralph.yaml if a ConfigLoader is provided.
		var qualityChecks []string
		if cfg.ConfigLoad != nil {
			ralphConfigPath := filepath.Join(project.LocalPath, project.RalphConfigPath)
			ralphCfg, err := cfg.ConfigLoad.Load(ralphConfigPath)
			if err != nil {
				return fmt.Errorf("loading ralph config: %w", err)
			}
			qualityChecks = ralphCfg.QualityChecks
		}

		comments, err := cfg.Comments.FetchPRComments(ctx, project.GithubOwner, project.GithubRepo, issue.PRNumber)
		if err != nil {
			return fmt.Errorf("fetching PR comments: %w", err)
		}

		// Filter to top-level comments only (skip replies)
		topLevel := filterTopLevel(comments)
		if len(topLevel) == 0 {
			return nil
		}

		// Build AI prompt data from review comments
		var aiComments []ai.AddressFeedbackComment
		for _, c := range topLevel {
			aiComments = append(aiComments, ai.AddressFeedbackComment{
				Path:   c.Path,
				Author: c.User,
				Body:   c.Body,
			})
		}

		prompt, err := ai.RenderAddressFeedback(ai.AddressFeedbackData{
			Comments:      aiComments,
			QualityChecks: qualityChecks,
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering feedback prompt: %w", err)
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		handler := newBuildEventHandler(database, issue.ID, cfg.EventHandler, cfg.OnBuildEvent)
		aiResponse, err := cfg.Invoker.InvokeWithEvents(ctx, prompt, treePath, handler)
		if err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		// Try to commit and push. If nothing changed (e.g., AI only
		// provided explanations), skip commit/push gracefully.
		committed := false
		if err := cfg.Git.Commit(ctx, treePath, "Address review feedback"); err != nil {
			if !isNothingToCommit(err) {
				return fmt.Errorf("committing changes: %w", err)
			}
		} else {
			if err := cfg.Git.PushBranch(ctx, treePath, issue.BranchName); err != nil {
				return fmt.Errorf("pushing changes: %w", err)
			}
			committed = true
		}

		// Build reply message per comment from AI response.
		var replyRef string
		if committed {
			sha, shaErr := cfg.Git.HeadSHA(ctx, treePath)
			if shaErr == nil {
				replyRef = sha
			} else {
				replyRef = "latest commit"
			}
		}

		for _, c := range topLevel {
			replyMsg := buildReplyForComment(aiResponse, c, replyRef)
			if _, err := cfg.Replier.PostReviewReply(ctx,
				project.GithubOwner, project.GithubRepo,
				issue.PRNumber, c.ID, replyMsg,
			); err != nil {
				return fmt.Errorf("replying to comment %d: %w", c.ID, err)
			}
		}

		detail := fmt.Sprintf("Addressed %d comments", len(topLevel))
		if replyRef != "" {
			detail += " in " + replyRef
		}
		if err := database.LogActivity(issue.ID, "feedback_finish", "", "", detail); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// buildEventHandler wraps events from an AI invocation, stores them in the
// activity log as build_event type, and forwards to an optional upstream handler.
type buildEventHandler struct {
	db           *db.DB
	issueID      string
	upstream     events.EventHandler
	onBuildEvent func(issueID, detail string)
}

func newBuildEventHandler(database *db.DB, issueID string, upstream events.EventHandler, onBuildEvent func(issueID, detail string)) *buildEventHandler {
	return &buildEventHandler{
		db:           database,
		issueID:      issueID,
		upstream:     upstream,
		onBuildEvent: onBuildEvent,
	}
}

func (h *buildEventHandler) Handle(e events.Event) {
	detail := formatEventDetail(e)
	if detail != "" {
		_ = h.db.LogActivity(h.issueID, "build_event", "", "", detail)

		if h.onBuildEvent != nil {
			h.onBuildEvent(h.issueID, detail)
		}
	}

	if h.upstream != nil {
		h.upstream.Handle(e)
	}
}

// formatEventDetail converts an event to a human-readable string for the
// activity log. Returns empty string for events that shouldn't be logged.
func formatEventDetail(e events.Event) string {
	switch ev := e.(type) {
	case events.ToolUse:
		if ev.Detail != "" {
			return fmt.Sprintf("→ %s %s", ev.Name, ev.Detail)
		}
		return fmt.Sprintf("→ %s", ev.Name)
	case events.InvocationDone:
		return fmt.Sprintf("Invocation done: %d turns in %dms", ev.NumTurns, ev.DurationMS)
	default:
		return ""
	}
}

// filterTopLevel returns only comments that are not replies (InReplyTo == 0).
func filterTopLevel(comments []github.Comment) []github.Comment {
	var result []github.Comment
	for _, c := range comments {
		if c.InReplyTo == 0 {
			result = append(result, c)
		}
	}
	return result
}

// isNothingToCommit returns true when a git commit error indicates there was
// nothing to commit (no staged changes).
func isNothingToCommit(err error) bool {
	return strings.Contains(err.Error(), "nothing to commit") ||
		strings.Contains(err.Error(), "exited with code 1")
}

// buildReplyForComment constructs a reply message for a review comment.
// If code was committed, it references the commit SHA. Otherwise it extracts
// the AI's explanation from its response.
func buildReplyForComment(aiResponse string, c github.Comment, commitRef string) string {
	if commitRef != "" {
		return fmt.Sprintf("Addressed in %s", commitRef)
	}
	// No commit — extract AI's response for this file from the output.
	if c.Path != "" {
		if section := extractSection(aiResponse, c.Path); section != "" {
			return section
		}
	}
	return "Reviewed — no code changes needed."
}

// extractSection pulls the **Response:** content for a given file path from
// the AI's structured output.
func extractSection(response, path string) string {
	// Look for "### <path>" section in the AI output
	marker := "### " + path
	idx := strings.Index(response, marker)
	if idx < 0 {
		return ""
	}
	rest := response[idx+len(marker):]

	// Find **Response:** line
	respMarker := "**Response:**"
	rIdx := strings.Index(rest, respMarker)
	if rIdx < 0 {
		return ""
	}
	after := strings.TrimSpace(rest[rIdx+len(respMarker):])

	// Take until next "###" section or end
	nextSection := strings.Index(after, "\n###")
	if nextSection >= 0 {
		after = after[:nextSection]
	}
	return strings.TrimSpace(after)
}
