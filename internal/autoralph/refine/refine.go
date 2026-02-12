package refine

import (
	"context"
	"fmt"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/approve"
	"github.com/uesteibar/ralph/internal/autoralph/db"
)

// Invoker invokes an AI model with a prompt and returns the response.
// Dir sets the working directory for the AI process.
type Invoker interface {
	Invoke(ctx context.Context, prompt, dir string) (string, error)
}

// Poster posts a comment on a Linear issue and returns its ID.
type Poster interface {
	PostComment(ctx context.Context, linearIssueID, body string) (string, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// Config holds the dependencies for the refine action.
type Config struct {
	Invoker     Invoker
	Poster      Poster
	Projects    ProjectGetter
	OverrideDir string
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

		_ = database.LogActivity(issue.ID, "ai_invocation", "", "", "Invoking AI to refine issue...")

		prompt, err := ai.RenderRefineIssue(ai.RefineIssueData{
			Title:       issue.Title,
			Description: issue.Description,
		}, cfg.OverrideDir)
		if err != nil {
			return fmt.Errorf("rendering refine prompt: %w", err)
		}

		response, err := cfg.Invoker.Invoke(context.Background(), prompt, project.LocalPath)
		if err != nil {
			return fmt.Errorf("invoking AI: %w", err)
		}

		commentID, err := cfg.Poster.PostComment(context.Background(), issue.LinearIssueID, response+approve.ApprovalHint)
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
