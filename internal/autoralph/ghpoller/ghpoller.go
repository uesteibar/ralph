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

// CheckRunFetcher fetches check run status from GitHub.
type CheckRunFetcher interface {
	FetchCheckRuns(ctx context.Context, owner, repo, ref string) ([]github.CheckRun, error)
}

// PRFetcher fetches pull request details from GitHub.
type PRFetcher interface {
	FetchPR(ctx context.Context, owner, repo string, prNumber int) (github.PR, error)
}

// TimelineFetcher fetches PR timeline events from GitHub.
type TimelineFetcher interface {
	FetchTimeline(ctx context.Context, owner, repo string, prNumber int) ([]github.TimelineEvent, error)
}

// GitHubClient combines all GitHub interaction interfaces needed by the poller.
type GitHubClient interface {
	ReviewFetcher
	MergeChecker
	CheckRunFetcher
	PRFetcher
	TimelineFetcher
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

// pollProject checks GitHub for each issue in IN_REVIEW, ADDRESSING_FEEDBACK, FIXING_CHECKS, or FAILED.
// Failed issues with a PR number are included so we can detect merges that happened
// while an action was running, recovering them to "completed" instead of leaving them stuck.
func (p *Poller) pollProject(ctx context.Context, proj ProjectInfo) {
	issues, err := p.db.ListIssues(db.IssueFilter{
		ProjectID: proj.ProjectID,
		States: []string{
			string(orchestrator.StateInReview),
			string(orchestrator.StateAddressingFeedback),
			string(orchestrator.StateFixingChecks),
			string(orchestrator.StateFailed),
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

// checkIssue checks merge status, check runs, and reviews for a single issue.
// Check status is evaluated before review status so check failures take priority.
func (p *Poller) checkIssue(ctx context.Context, proj ProjectInfo, issue db.Issue) {
	// Check merge first — if merged, nothing else matters.
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

	// Failed issues are only checked for merge status — don't evaluate checks or reviews.
	if issue.State == string(orchestrator.StateFailed) {
		return
	}

	// Fetch PR head SHA for check run evaluation.
	pr, err := proj.GitHub.FetchPR(ctx, proj.GithubOwner, proj.GithubRepo, issue.PRNumber)
	if err != nil {
		p.logger.Warn("fetching PR", "issue_id", issue.ID, "pr", issue.PRNumber, "error", err)
		return
	}

	headSHA := pr.HeadSHA
	shaChanged := headSHA != issue.LastCheckSHA

	// Reset CheckFixAttempts when head SHA changes externally
	// (someone pushed a fix manually) and issue is not currently fixing checks.
	if shaChanged && issue.State != string(orchestrator.StateFixingChecks) {
		issue.CheckFixAttempts = 0
	}

	// Evaluate check runs on the current head SHA.
	if headSHA != "" {
		checkRuns, err := proj.GitHub.FetchCheckRuns(ctx, proj.GithubOwner, proj.GithubRepo, headSHA)
		if err != nil {
			p.logger.Warn("fetching check runs", "issue_id", issue.ID, "ref", headSHA, "error", err)
			return
		}

		if len(checkRuns) > 0 {
			allCompleted, hasFailed := evaluateCheckRuns(checkRuns)

			// Only record the SHA once all checks have finished. Saving it
			// while checks are still running would make shaChanged false on
			// the next poll, causing us to miss failures that surface later.
			if allCompleted {
				issue.LastCheckSHA = headSHA
			}

			if allCompleted && hasFailed && shaChanged {
				// Checks failed on a new SHA — transition to fixing_checks.
				p.transitionIssue(issue, string(orchestrator.StateFixingChecks), "checks_failed",
					fmt.Sprintf("Check runs failed on PR #%d (SHA %s)", issue.PRNumber, headSHA[:minLen(len(headSHA), 7)]))
				return
			}

			// Persist SHA tracking when checks completed and SHA changed
			// (covers the all-checks-passed case).
			if allCompleted && shaChanged {
				if err := p.db.UpdateIssue(issue); err != nil {
					p.logger.Warn("updating last_check_sha", "issue_id", issue.ID, "error", err)
				}
			}
		}
	}

	// For issues in fixing_checks, don't evaluate reviews — just wait for checks.
	if issue.State == string(orchestrator.StateFixingChecks) {
		return
	}

	// Compute delegated trusted reviewers from timeline when TrustedUserID is set.
	var delegated map[int64]bool
	if proj.TrustedUserID != 0 {
		events, err := proj.GitHub.FetchTimeline(ctx, proj.GithubOwner, proj.GithubRepo, issue.PRNumber)
		if err != nil {
			p.logger.Warn("fetching PR timeline", "issue_id", issue.ID, "pr", issue.PRNumber, "error", err)
			// Fall back to direct user ID check only (empty delegated map).
		} else {
			delegated = trustedReviewerIDs(events, proj.TrustedUserID)
		}
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
			if !isTrustedReviewer(r.UserID, proj.TrustedUserID, delegated) && (r.State == "CHANGES_REQUESTED" || r.State == "COMMENTED") {
				p.db.LogActivity(issue.ID, "untrusted_feedback_skipped", issue.State, issue.State,
					fmt.Sprintf("Skipped feedback from untrusted user %s (ID %d) on PR #%d", r.User, r.UserID, issue.PRNumber))
			}
		}
	}

	// Check if any new review has actionable feedback (changes requested or comments).
	if hasFeedback(newReviews, proj.TrustedUserID, delegated) {
		p.transitionIssue(issue, string(orchestrator.StateAddressingFeedback), "changes_requested",
			fmt.Sprintf("Feedback received on PR #%d", issue.PRNumber))
		return
	}

	// No state transition needed, but still update the last review ID.
	if err := p.db.UpdateIssue(issue); err != nil {
		p.logger.Warn("updating last_review_id", "issue_id", issue.ID, "error", err)
	}
}

// evaluateCheckRuns returns whether all check runs are completed, and whether
// at least one has a "failure" conclusion.
func evaluateCheckRuns(checkRuns []github.CheckRun) (allCompleted, hasFailed bool) {
	allCompleted = true
	for _, cr := range checkRuns {
		if cr.Status != "completed" {
			allCompleted = false
		}
		if cr.Conclusion == "failure" {
			hasFailed = true
		}
	}
	return
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// trustedReviewerIDs computes the net set of reviewer IDs that the trusted user
// has explicitly requested reviews from. review_requested adds a reviewer,
// review_request_removed removes them. Only events where RequesterID matches
// trustedUserID are considered.
func trustedReviewerIDs(events []github.TimelineEvent, trustedUserID int64) map[int64]bool {
	trusted := make(map[int64]bool)
	for _, ev := range events {
		if ev.RequesterID != trustedUserID {
			continue
		}
		switch ev.Event {
		case "review_requested":
			trusted[ev.ReviewerID] = true
		case "review_request_removed":
			delete(trusted, ev.ReviewerID)
		}
	}
	return trusted
}

// isTrustedReviewer returns true if the reviewer should be trusted given
// the current trust configuration. When trustedUserID is 0, all non-bot
// reviewers are trusted (backward compatible).
func isTrustedReviewer(userID, trustedUserID int64, delegated map[int64]bool) bool {
	if trustedUserID == 0 {
		return true
	}
	return userID == trustedUserID || delegated[userID]
}

// hasFeedback returns true if any review has actionable feedback — either
// an explicit "CHANGES_REQUESTED" or a "COMMENTED" review (which means the
// reviewer left inline comments without formally requesting changes).
// Bot reviews (e.g. from autoralph itself replying to comments) are ignored
// so they don't trigger a new feedback cycle.
// When trustedUserID is non-zero, only reviews from that user ID or from
// delegated trusted reviewers are considered.
// When trustedUserID is 0, all non-bot reviews are trusted (backward compatible).
func hasFeedback(reviews []github.Review, trustedUserID int64, delegated map[int64]bool) bool {
	for _, r := range reviews {
		if isBot(r.User) {
			continue
		}
		if !isTrustedReviewer(r.UserID, trustedUserID, delegated) {
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
