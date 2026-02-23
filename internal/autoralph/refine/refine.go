package refine

import (
	"context"
	"fmt"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/approve"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/eventlog"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/knowledge"
)

// maxTurnsRefine limits the number of agentic turns for issue refinement.
const maxTurnsRefine = 15

// Poster posts a comment on a Linear issue and returns its ID.
type Poster interface {
	PostComment(ctx context.Context, linearIssueID, body string) (string, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// GitPuller pulls the default base branch before AI invocation.
type GitPuller interface {
	PullDefaultBase(ctx context.Context, repoPath, ralphConfigPath string) error
}

// Config holds the dependencies for the refine action.
type Config struct {
	Invoker      invoker.EventInvoker
	Poster       Poster
	Projects     ProjectGetter
	GitPuller    GitPuller
	OnBuildEvent func(issueID, detail string)
	OverrideDir  string
}

// NewAction returns an orchestrator ActionFunc that performs AI issue refinement.
// It renders the refine_issue.md prompt with the issue's title and description,
// invokes the AI, posts the response as a Linear comment, and logs the activity.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		if cfg.GitPuller != nil {
			if pullErr := cfg.GitPuller.PullDefaultBase(context.Background(), project.LocalPath, project.RalphConfigPath); pullErr != nil {
				_ = database.LogActivity(issue.ID, "warning", "", "", fmt.Sprintf("git pull --ff-only failed: %v", pullErr))
			}
		}

		_ = database.LogActivity(issue.ID, "ai_invocation", "", "", "Invoking AI to refine issue...")

		prompt, err := ai.RenderRefineIssue(ai.RefineIssueData{
			Title:         issue.Title,
			Description:   issue.Description,
			KnowledgePath: knowledge.Dir(project.LocalPath),
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering refine prompt: %w", err)
		}

		handler := eventlog.New(database, issue.ID, nil, cfg.OnBuildEvent, nil)
		response, err := cfg.Invoker.InvokeWithEvents(context.Background(), prompt, project.LocalPath, maxTurnsRefine, handler)
		if err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		cleaned := approve.StripTypeMarker(response)
		body := cleaned
		if approve.ResponseNeedsApproval(response) {
			body += approve.ApprovalHint
		}

		commentID, err := cfg.Poster.PostComment(context.Background(), issue.LinearIssueID, body)
		if err != nil {
			return fmt.Errorf("posting comment: %w", err)
		}

		issue.LastCommentID = commentID
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("updating last comment ID: %w", err)
		}

		if err := database.LogActivity(
			issue.ID,
			"ai_refinement",
			"",
			"",
			fmt.Sprintf("Posted refinement comment to Linear: %s", truncate(response, 200)),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
