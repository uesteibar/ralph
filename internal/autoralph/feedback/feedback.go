package feedback

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/workspace"
)

// maxTurnsFeedback limits the number of agentic turns for addressing feedback.
const maxTurnsFeedback = 30

// CommentFetcher fetches line-specific review comments from a GitHub PR.
type CommentFetcher interface {
	FetchPRComments(ctx context.Context, owner, repo string, prNumber int) ([]github.Comment, error)
}

// ReviewFetcher fetches PR reviews (including body text) from GitHub.
type ReviewFetcher interface {
	FetchPRReviews(ctx context.Context, owner, repo string, prNumber int) ([]github.Review, error)
}

// IssueCommentFetcher fetches general PR/issue comments from GitHub.
type IssueCommentFetcher interface {
	FetchPRIssueComments(ctx context.Context, owner, repo string, prNumber int) ([]github.Comment, error)
}

// ReviewReplier replies to a review comment on a GitHub PR.
type ReviewReplier interface {
	PostReviewReply(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) (github.Comment, error)
}

// PRCommenter posts general comments on a pull request.
type PRCommenter interface {
	PostPRComment(ctx context.Context, owner, repo string, prNumber int, body string) (github.Comment, error)
}

// CommentReactor adds emoji reactions to GitHub PR review comments.
type CommentReactor interface {
	ReactToReviewComment(ctx context.Context, owner, repo string, commentID int64, reaction string) error
}

// IssueCommentReactor adds emoji reactions to general PR/issue comments.
type IssueCommentReactor interface {
	ReactToIssueComment(ctx context.Context, owner, repo string, commentID int64, reaction string) error
}

// BranchPuller pulls the latest remote branch state into the local worktree.
type BranchPuller interface {
	PullBranch(ctx context.Context, workDir, branch string) error
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
	Invoker       invoker.EventInvoker
	Comments      CommentFetcher
	Reviews       ReviewFetcher       // optional: fetches review bodies
	IssueComments IssueCommentFetcher // optional: fetches general PR comments
	Replier       ReviewReplier       // for inline (line-specific) replies
	PRCommenter   PRCommenter         // optional: for general PR comment replies
	Git           GitOps
	BranchPuller  BranchPuller
	Projects      ProjectGetter
	ConfigLoad    ConfigLoader
	Reactor       CommentReactor       // for line comment reactions
	IssueReactor  IssueCommentReactor  // optional: for issue comment reactions
	EventHandler  events.EventHandler
	OnBuildEvent  func(issueID, detail string)
	OverrideDir   string
	TrustedUser   string // GitHub username of the trusted reviewer (empty = no trust annotation)
}

// commentSource identifies where a feedback item originated.
type commentSource int

const (
	sourceLineComment  commentSource = iota // line-specific review comment
	sourceReviewBody                        // review submission body text
	sourceIssueComment                      // general PR/issue comment
)

// replyItem holds a reply attached to a parent feedback comment.
type replyItem struct {
	author string
	body   string
}

// feedbackItem is a normalized piece of feedback from any source.
type feedbackItem struct {
	id        int64
	path      string // empty for non-line feedback
	author    string
	body      string
	source    commentSource
	replies   []replyItem
	isTrusted bool
}

func (f feedbackItem) isInline() bool {
	return f.source == sourceLineComment
}

// IsAddressingFeedback returns true if the issue is in the addressing_feedback state.
func IsAddressingFeedback(issue db.Issue) bool {
	return orchestrator.IssueState(issue.State) == orchestrator.StateAddressingFeedback
}

// NewAction returns an orchestrator ActionFunc that addresses PR review feedback.
// It fetches feedback from three sources (line comments, review bodies, and
// general PR comments), invokes AI with the address_feedback.md prompt,
// commits and pushes changes, then replies via the appropriate channel.
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

		// Gather feedback from all sources (line comments, review bodies, issue comments).
		items, err := gatherFeedback(ctx, cfg, project.GithubOwner, project.GithubRepo, issue.PRNumber)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}

		// Annotate trusted user if configured.
		annotateTrust(items, cfg.TrustedUser)

		// React 👀 to each feedback item before invoking AI.
		reactToFeedback(ctx, cfg, project.GithubOwner, project.GithubRepo, items)

		// Build AI prompt data from all feedback items.
		var aiComments []ai.AddressFeedbackComment
		for _, item := range items {
			var replies []ai.CommentReply
			for _, r := range item.replies {
				replies = append(replies, ai.CommentReply{
					Author: r.author,
					Body:   r.body,
				})
			}
			aiComments = append(aiComments, ai.AddressFeedbackComment{
				Path:      item.path,
				Author:    item.author,
				Body:      item.body,
				Replies:   replies,
				IsTrusted: item.isTrusted,
			})
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		if err := cfg.BranchPuller.PullBranch(ctx, treePath, issue.BranchName); err != nil {
			return fmt.Errorf("pulling branch: %w", err)
		}

		prompt, err := ai.RenderAddressFeedback(ai.AddressFeedbackData{
			Comments:       aiComments,
			QualityChecks:  qualityChecks,
			KnowledgePath:  knowledge.Dir(treePath),
			HasTrustedUser: cfg.TrustedUser != "",
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering feedback prompt: %w", err)
		}

		handler := eventlog.New(database, issue.ID, cfg.EventHandler, cfg.OnBuildEvent)
		aiResponse, err := cfg.Invoker.InvokeWithEvents(ctx, prompt, treePath, maxTurnsFeedback, handler)
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

		// Build reply reference from commit SHA.
		var replyRef string
		if committed {
			sha, shaErr := cfg.Git.HeadSHA(ctx, treePath)
			if shaErr == nil {
				replyRef = sha
			} else {
				replyRef = "latest commit"
			}
		}

		// Reply to each feedback item via the appropriate channel.
		if err := replyToFeedback(ctx, cfg, project.GithubOwner, project.GithubRepo, issue.PRNumber, items, aiResponse, replyRef); err != nil {
			return err
		}

		detail := fmt.Sprintf("Addressed %d comments", len(items))
		if replyRef != "" {
			detail += " in " + replyRef
		}
		if err := database.LogActivity(issue.ID, "feedback_finish", "", "", detail); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// gatherFeedback collects feedback from all configured sources into a
// normalized list of feedbackItems.
func gatherFeedback(ctx context.Context, cfg Config, owner, repo string, prNumber int) ([]feedbackItem, error) {
	var items []feedbackItem

	// 1. Line-specific review comments (grouped with reply threads).
	comments, err := cfg.Comments.FetchPRComments(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching PR comments: %w", err)
	}
	items = append(items, groupThreads(comments)...)

	// 2. Review submission bodies (optional).
	if cfg.Reviews != nil {
		reviews, err := cfg.Reviews.FetchPRReviews(ctx, owner, repo, prNumber)
		if err != nil {
			return nil, fmt.Errorf("fetching PR reviews: %w", err)
		}
		for _, r := range reviews {
			if r.Body == "" || isBot(r.User) {
				continue
			}
			if r.State != "CHANGES_REQUESTED" && r.State != "COMMENTED" {
				continue
			}
			items = append(items, feedbackItem{
				id:     r.ID,
				author: r.User,
				body:   r.Body,
				source: sourceReviewBody,
			})
		}
	}

	// 3. General PR/issue comments (optional).
	if cfg.IssueComments != nil {
		issueComments, err := cfg.IssueComments.FetchPRIssueComments(ctx, owner, repo, prNumber)
		if err != nil {
			return nil, fmt.Errorf("fetching PR issue comments: %w", err)
		}
		for _, c := range issueComments {
			if isBot(c.User) {
				continue
			}
			items = append(items, feedbackItem{
				id:     c.ID,
				author: c.User,
				body:   c.Body,
				source: sourceIssueComment,
			})
		}
	}

	return items, nil
}

// reactToFeedback adds 👀 reactions to feedback items before AI invocation.
func reactToFeedback(ctx context.Context, cfg Config, owner, repo string, items []feedbackItem) {
	for _, item := range items {
		switch item.source {
		case sourceLineComment:
			if cfg.Reactor != nil {
				if err := cfg.Reactor.ReactToReviewComment(ctx, owner, repo, item.id, "eyes"); err != nil {
					slog.Warn("reacting to review comment", "comment_id", item.id, "error", err)
				}
			}
		case sourceIssueComment:
			if cfg.IssueReactor != nil {
				if err := cfg.IssueReactor.ReactToIssueComment(ctx, owner, repo, item.id, "eyes"); err != nil {
					slog.Warn("reacting to issue comment", "comment_id", item.id, "error", err)
				}
			}
		// sourceReviewBody: no reaction API for review submissions
		}
	}
}

// replyToFeedback sends replies to each feedback item via the appropriate channel:
// line comments get individual review replies, non-inline items are consolidated
// into a single PR comment joined by horizontal rule separators.
func replyToFeedback(ctx context.Context, cfg Config, owner, repo string, prNumber int, items []feedbackItem, aiResponse, replyRef string) error {
	var nonInlineReplies []string
	for _, item := range items {
		replyMsg := buildReplyForComment(aiResponse, item.path, replyRef)
		if item.isInline() {
			if _, err := cfg.Replier.PostReviewReply(ctx, owner, repo, prNumber, item.id, replyMsg); err != nil {
				return fmt.Errorf("replying to comment %d: %w", item.id, err)
			}
		} else {
			nonInlineReplies = append(nonInlineReplies, replyMsg)
		}
	}
	if len(nonInlineReplies) > 0 && cfg.PRCommenter != nil {
		consolidated := strings.Join(nonInlineReplies, "\n\n---\n\n")
		if _, err := cfg.PRCommenter.PostPRComment(ctx, owner, repo, prNumber, consolidated); err != nil {
			return fmt.Errorf("posting consolidated PR comment: %w", err)
		}
	}
	return nil
}

// groupThreads groups comments into top-level feedback items with their replies
// attached. A comment with InReplyTo == 0 is a top-level comment; otherwise it
// is a reply that gets attached to its parent. Only top-level comments become
// actionable feedbackItems.
func groupThreads(comments []github.Comment) []feedbackItem {
	// Index top-level comments by ID.
	indexByID := make(map[int64]int) // comment ID -> index in result
	var result []feedbackItem
	for _, c := range comments {
		if c.InReplyTo == 0 {
			indexByID[c.ID] = len(result)
			result = append(result, feedbackItem{
				id:     c.ID,
				path:   c.Path,
				author: c.User,
				body:   c.Body,
				source: sourceLineComment,
			})
		}
	}
	// Attach replies to their parent.
	for _, c := range comments {
		if c.InReplyTo != 0 {
			if idx, ok := indexByID[c.InReplyTo]; ok {
				result[idx].replies = append(result[idx].replies, replyItem{
					author: c.User,
					body:   c.Body,
				})
			}
		}
	}
	return result
}

// annotateTrust marks feedback items whose author matches trustedUser as trusted.
// When trustedUser is empty, no annotations are applied.
func annotateTrust(items []feedbackItem, trustedUser string) {
	if trustedUser == "" {
		return
	}
	for i := range items {
		if strings.EqualFold(items[i].author, trustedUser) {
			items[i].isTrusted = true
		}
	}
}

// isBot returns true if the username looks like a GitHub App bot (e.g. "my-app[bot]").
func isBot(user string) bool {
	return strings.HasSuffix(user, "[bot]")
}

// isNothingToCommit returns true when a git commit error indicates there was
// nothing to commit (no staged changes).
func isNothingToCommit(err error) bool {
	return strings.Contains(err.Error(), "nothing to commit") ||
		strings.Contains(err.Error(), "exited with code 1")
}

// buildReplyForComment constructs a reply message for a feedback item.
// It always tries to extract the AI's explanation from its structured output.
// When code was committed, the commit SHA is included alongside the explanation.
func buildReplyForComment(aiResponse, path, commitRef string) string {
	// Extract the AI's explanation for this item.
	// For general (non-line) feedback, look up "General feedback" section.
	lookupKey := path
	if lookupKey == "" {
		lookupKey = "General feedback"
	}
	section := extractSection(aiResponse, lookupKey)

	if commitRef != "" {
		if section != "" {
			return fmt.Sprintf("Addressed in %s\n\n%s", commitRef, section)
		}
		return fmt.Sprintf("Addressed in %s", commitRef)
	}

	if section != "" {
		return section
	}

	// Fallback: return the full AI response (trimmed) so the reviewer
	// gets something meaningful instead of a canned message.
	if trimmed := strings.TrimSpace(aiResponse); trimmed != "" {
		if len(trimmed) > 1000 {
			trimmed = trimmed[:1000] + "…"
		}
		return trimmed
	}
	return "Reviewed — no code changes needed."
}

// extractSection pulls the **Response:** content for a given file path from
// the AI's structured output.
func extractSection(response, path string) string {
	// Look for "### <path>" section in the AI output
	marker := "### " + path
	_, rest, found := strings.Cut(response, marker)
	if !found {
		return ""
	}

	// Find **Response:** line
	respMarker := "**Response:**"
	_, after, found := strings.Cut(rest, respMarker)
	if !found {
		return ""
	}
	after = strings.TrimSpace(after)

	// Take until next "###" section or end
	nextSection := strings.Index(after, "\n###")
	if nextSection >= 0 {
		after = after[:nextSection]
	}
	return strings.TrimSpace(after)
}
