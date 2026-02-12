package complete

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
)

// WorkspaceRemover removes a Ralph workspace.
type WorkspaceRemover interface {
	RemoveWorkspace(ctx context.Context, repoPath, name string) error
}

// LinearStateUpdater updates the workflow state of a Linear issue.
type LinearStateUpdater interface {
	FetchWorkflowStates(ctx context.Context, teamID string) ([]WorkflowState, error)
	UpdateIssueState(ctx context.Context, issueID, stateID string) error
}

// WorkflowState mirrors linear.WorkflowState to avoid importing the linear package.
type WorkflowState struct {
	ID   string
	Name string
	Type string
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// Config holds the dependencies for the completion action.
type Config struct {
	Workspace WorkspaceRemover
	Linear    LinearStateUpdater
	Projects  ProjectGetter
}

// NewAction returns a function that completes an issue after its PR is merged.
// It deletes the workspace, updates Linear state to "Done", transitions the
// issue to completed, and logs the final activity.
//
// Workspace removal and Linear update errors are non-fatal — the issue still
// transitions to completed since the PR is already merged.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		// Delete workspace (non-fatal on error).
		if issue.WorkspaceName != "" {
			if err := cfg.Workspace.RemoveWorkspace(context.Background(), project.LocalPath, issue.WorkspaceName); err != nil {
				slog.Warn("removing workspace", "issue_id", issue.ID, "workspace", issue.WorkspaceName, "error", err)
			}
		}

		// Update Linear issue state to "Done" (non-fatal on error).
		updateLinearState(cfg.Linear, issue.LinearIssueID, project.LinearTeamID, "Done")

		// Transition issue to completed.
		fromState := issue.State
		issue.State = string(orchestrator.StateCompleted)
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("updating issue state: %w", err)
		}

		if err := database.LogActivity(
			issue.ID,
			"issue_completed",
			fromState,
			string(orchestrator.StateCompleted),
			fmt.Sprintf("Issue completed. PR #%d merged.", issue.PRNumber),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// updateLinearState fetches workflow states for the team and updates the issue
// to the state matching the given name. Errors are logged but not returned.
func updateLinearState(client LinearStateUpdater, issueID, teamID, stateName string) {
	states, err := client.FetchWorkflowStates(context.Background(), teamID)
	if err != nil {
		slog.Warn("fetching workflow states for completion", "issue_id", issueID, "error", err)
		return
	}

	for _, s := range states {
		if s.Name == stateName {
			if err := client.UpdateIssueState(context.Background(), issueID, s.ID); err != nil {
				slog.Warn("updating Linear state for completion", "issue_id", issueID, "error", err)
			}
			return
		}
	}

	slog.Warn("workflow state not found for completion", "state", stateName, "team_id", teamID)
}
