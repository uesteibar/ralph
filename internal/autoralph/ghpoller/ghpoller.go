package ghpoller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
)

// ReviewFetcher fetches PR reviews from GitHub.
type ReviewFetcher interface {
	FetchPRReviews(ctx context.Context, owner, repo string, prNumber int) ([]github.Review, error)
}

// MergeChecker checks whether a PR has been merged.
type MergeChecker interface {
	IsPRMerged(ctx context.Context, owner, repo string, prNumber int) (bool, error)
}

// GitHubClient combines ReviewFetcher and MergeChecker.
type GitHubClient interface {
	ReviewFetcher
	MergeChecker
}

// ProjectInfo holds the data the GitHub poller needs for each project.
type ProjectInfo struct {
	ProjectID     string
	GithubOwner   string
	GithubRepo    string
	GitHub        GitHubClient
	TrustedUserID int64
}

// CompleteFunc is called when a PR merge is detected. It handles workspace
// cleanup, Linear state update, and issue completion.
type CompleteFunc func(issue db.Issue, database *db.DB) error

// Poller polls GitHub for PR reviews and merge status on tracked issues.
type Poller struct {
	db       *db.DB
	projects []ProjectInfo
	interval time.Duration
	logger   *slog.Logger
	complete CompleteFunc
}

// New creates a new GitHub Poller. If complete is nil, merge detection falls
// back to a simple state transition without workspace cleanup or Linear update.
func New(database *db.DB, projects []ProjectInfo, interval time.Duration, logger *slog.Logger, complete CompleteFunc) *Poller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		db:       database,
		projects: projects,
		interval: interval,
		logger:   logger,
		complete: complete,
	}
}

// Run starts the poll loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.logger.Info("github poller started", "interval", p.interval, "projects", len(p.projects))

	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("github poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// poll executes a single poll cycle across all projects.
func (p *Poller) poll(ctx context.Context) {
	for _, proj := range p.projects {
		if ctx.Err() != nil {
			return
		}
		p.pollProject(ctx, proj)
	}
}

// pollProject checks GitHub for each issue in IN_REVIEW or ADDRESSING_FEEDBACK.
func (p *Poller) pollProject(ctx context.Context, proj ProjectInfo) {
	issues, err := p.db.ListIssues(db.IssueFilter{
		ProjectID: proj.ProjectID,
		States: []string{
			string(orchestrator.StateInReview),
			string(orchestrator.StateAddressingFeedback),
		},
	})
	if err != nil {
		p.logger.Warn("listing issues for github poll", "project_id", proj.ProjectID, "error", err)
		return
	}

	for _, issue := range issues {
		if ctx.Err() != nil {
			return
		}
		if issue.PRNumber == 0 {
			continue
		}
		p.checkIssue(ctx, proj, issue)
	}
}

// checkIssue checks merge status and reviews for a single issue.
func (p *Poller) checkIssue(ctx context.Context, proj ProjectInfo, issue db.Issue) {
	// Check merge first — if merged, we don't need to check reviews.
	merged, err := proj.GitHub.IsPRMerged(ctx, proj.GithubOwner, proj.GithubRepo, issue.PRNumber)
	if err != nil {
		p.logger.Warn("checking PR merged", "issue_id", issue.ID, "pr", issue.PRNumber, "error", err)
		return
	}

	if merged {
		if p.complete != nil {
			if err := p.complete(issue, p.db); err != nil {
				p.logger.Warn("completing issue", "issue_id", issue.ID, "error", err)
			}
		} else {
			p.transitionIssue(issue, string(orchestrator.StateCompleted), "pr_merged",
				fmt.Sprintf("PR #%d merged", issue.PRNumber))
		}
		return
	}

	// Fetch reviews and check for new ones.
	reviews, err := proj.GitHub.FetchPRReviews(ctx, proj.GithubOwner, proj.GithubRepo, issue.PRNumber)
	if err != nil {
		p.logger.Warn("fetching PR reviews", "issue_id", issue.ID, "pr", issue.PRNumber, "error", err)
		return
	}

	newReviews := reviewsAfter(reviews, issue.LastReviewID)
	if len(newReviews) == 0 {
		return
	}

	// Update LastReviewID to the highest ID we've seen.
	maxID := maxReviewID(newReviews)
	issue.LastReviewID = strconv.FormatInt(maxID, 10)

	// Log skipped untrusted reviews when a trusted user ID is configured.
	if proj.TrustedUserID != 0 {
		for _, r := range newReviews {
			if isBot(r.User) {
				continue
			}
			if r.UserID != proj.TrustedUserID && (r.State == "CHANGES_REQUESTED" || r.State == "COMMENTED") {
				p.db.LogActivity(issue.ID, "untrusted_feedback_skipped", issue.State, issue.State,
					fmt.Sprintf("Skipped feedback from untrusted user %s (ID %d) on PR #%d", r.User, r.UserID, issue.PRNumber))
			}
		}
	}

	// Check if any new review has actionable feedback (changes requested or comments).
	if hasFeedback(newReviews, proj.TrustedUserID) {
		p.transitionIssue(issue, string(orchestrator.StateAddressingFeedback), "changes_requested",
			fmt.Sprintf("Feedback received on PR #%d", issue.PRNumber))
		return
	}

	// No state transition needed, but still update the last review ID.
	if err := p.db.UpdateIssue(issue); err != nil {
		p.logger.Warn("updating last_review_id", "issue_id", issue.ID, "error", err)
	}
}

// transitionIssue updates the issue state, logs activity, and persists.
func (p *Poller) transitionIssue(issue db.Issue, toState, eventType, detail string) {
	fromState := issue.State
	issue.State = toState

	if err := p.db.UpdateIssue(issue); err != nil {
		p.logger.Warn("updating issue state", "issue_id", issue.ID, "error", err)
		return
	}

	if err := p.db.LogActivity(issue.ID, eventType, fromState, toState, detail); err != nil {
		p.logger.Warn("logging activity", "issue_id", issue.ID, "error", err)
	}
}

// reviewsAfter filters reviews to only those with ID > lastSeenID.
func reviewsAfter(reviews []github.Review, lastSeenID string) []github.Review {
	if lastSeenID == "" {
		return reviews
	}
	lastID, err := strconv.ParseInt(lastSeenID, 10, 64)
	if err != nil {
		return reviews
	}
	var after []github.Review
	for _, r := range reviews {
		if r.ID > lastID {
			after = append(after, r)
		}
	}
	return after
}

// hasFeedback returns true if any review has actionable feedback — either
// an explicit "CHANGES_REQUESTED" or a "COMMENTED" review (which means the
// reviewer left inline comments without formally requesting changes).
// Bot reviews (e.g. from autoralph itself replying to comments) are ignored
// so they don't trigger a new feedback cycle.
// When trustedUserID is non-zero, only reviews from that user ID are considered.
// When trustedUserID is 0, all non-bot reviews are trusted (backward compatible).
func hasFeedback(reviews []github.Review, trustedUserID int64) bool {
	for _, r := range reviews {
		if isBot(r.User) {
			continue
		}
		if trustedUserID != 0 && r.UserID != trustedUserID {
			continue
		}
		if r.State == "CHANGES_REQUESTED" || r.State == "COMMENTED" {
			return true
		}
	}
	return false
}

// isBot returns true if the username looks like a GitHub App bot (e.g. "my-app[bot]").
func isBot(user string) bool {
	return strings.HasSuffix(user, "[bot]")
}

// maxReviewID returns the highest review ID in the slice.
func maxReviewID(reviews []github.Review) int64 {
	var max int64
	for _, r := range reviews {
		if r.ID > max {
			max = r.ID
		}
	}
	return max
}
