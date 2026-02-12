package poller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
)

// ProjectInfo holds the data the poller needs for each configured project.
type ProjectInfo struct {
	ProjectID        string
	LinearTeamID     string
	LinearAssigneeID string
	LinearProjectID  string
	LinearLabel      string
	LinearClient     *linear.Client
}

// Poller polls Linear for assigned issues and ingests new ones into the DB.
type Poller struct {
	db       *db.DB
	projects []ProjectInfo
	interval time.Duration
	logger   *slog.Logger
}

// New creates a new Poller.
func New(database *db.DB, projects []ProjectInfo, interval time.Duration, logger *slog.Logger) *Poller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		db:       database,
		projects: projects,
		interval: interval,
		logger:   logger,
	}
}

// Run starts the poll loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.logger.Info("poller started", "interval", p.interval, "projects", len(p.projects))

	// Run an immediate poll before entering the ticker loop.
	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("poller stopped")
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

// pollProject fetches assigned issues from Linear for a single project
// and ingests any that are not already in the DB.
func (p *Poller) pollProject(ctx context.Context, proj ProjectInfo) {
	issues, err := proj.LinearClient.FetchAssignedIssues(ctx, proj.LinearTeamID, proj.LinearAssigneeID, proj.LinearProjectID, proj.LinearLabel)
	if err != nil {
		p.logger.Warn("poll failed", "project_id", proj.ProjectID, "error", err)
		return
	}

	var newCount int
	for _, li := range issues {
		_, err := p.db.GetIssueByLinearIDAndProject(li.ID, proj.ProjectID)
		if err == nil {
			continue // already in this project
		}
		if !errors.Is(err, sql.ErrNoRows) {
			p.logger.Warn("checking existing issue", "linear_issue_id", li.ID, "error", err)
			continue
		}

		issue, err := p.db.CreateIssue(db.Issue{
			ProjectID:     proj.ProjectID,
			LinearIssueID: li.ID,
			Identifier:    li.Identifier,
			Title:         li.Title,
			Description:   li.Description,
			State:         string(orchestrator.StateQueued),
		})
		if err != nil {
			p.logger.Warn("creating issue", "linear_issue_id", li.ID, "error", err)
			continue
		}

		if err := p.db.LogActivity(issue.ID, "ingested", "", string(orchestrator.StateQueued),
			fmt.Sprintf("Issue %s ingested from Linear", li.Identifier)); err != nil {
			p.logger.Warn("logging activity", "issue_id", issue.ID, "error", err)
		}

		newCount++
	}

	p.logger.Info("poll cycle", "project_id", proj.ProjectID, "found", len(issues), "new", newCount)
}
