package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Invoker invokes an AI model with a prompt and returns the response.
// Dir sets the working directory for the AI process.
type Invoker interface {
	Invoke(ctx context.Context, prompt, dir string) (string, error)
}

// WorkspaceCreator creates a Ralph workspace. Wraps workspace.CreateWorkspace
// to allow testing without git operations.
type WorkspaceCreator interface {
	Create(ctx context.Context, repoPath string, ws workspace.Workspace, base string, copyPatterns []string) error
}

// ConfigLoader loads a Ralph config from a file path.
type ConfigLoader interface {
	Load(path string) (*config.Config, error)
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

// PRDReader reads a PRD from disk (written by the AI).
type PRDReader interface {
	Read(path string) (*prd.PRD, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// Config holds the dependencies for the build setup action.
type Config struct {
	Invoker     Invoker
	Workspace   WorkspaceCreator
	ConfigLoad  ConfigLoader
	Linear      LinearStateUpdater
	PRDRead     PRDReader
	Projects    ProjectGetter
	OverrideDir string
}

// NewAction returns an orchestrator ActionFunc that sets up a workspace and
// generates a PRD for an approved issue. The action is idempotent: it reuses
// an existing workspace and PRD from a previous failed attempt instead of
// recreating them. Steps:
// 1. Load Ralph config from the project's ralph_config_path
// 2. Create workspace if it doesn't already exist
// 3. Invoke AI to write PRD if it doesn't already exist
// 4. Read the PRD from disk for metadata
// 5. Store workspace_name and branch_name in the DB
// 6. Update Linear issue state to "In Progress" (non-fatal)
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		ralphConfigPath := filepath.Join(project.LocalPath, project.RalphConfigPath)
		ralphCfg, err := cfg.ConfigLoad.Load(ralphConfigPath)
		if err != nil {
			return fmt.Errorf("loading ralph config: %w", err)
		}

		wsName := sanitizeName(issue.Identifier)
		// Skip the ralph branch pattern validation — autoralph uses its own prefix.
		branch, err := workspace.DeriveBranch(project.BranchPrefix, wsName, "")
		if err != nil {
			return fmt.Errorf("deriving branch name: %w", err)
		}

		// Reuse workspace from a previous attempt if it already exists.
		treePath := workspace.TreePath(project.LocalPath, wsName)
		if _, err := os.Stat(treePath); os.IsNotExist(err) {
			ws := workspace.Workspace{
				Name:      wsName,
				Branch:    branch,
				CreatedAt: time.Now().UTC(),
			}
			if err := cfg.Workspace.Create(
				context.Background(),
				project.LocalPath,
				ws,
				ralphCfg.Repo.DefaultBase,
				ralphCfg.CopyToWorktree,
			); err != nil {
				return fmt.Errorf("creating workspace: %w", err)
			}
		}

		prdPath := workspace.PRDPathForWorkspace(project.LocalPath, wsName)

		// Reuse existing PRD if one was already generated.
		if _, err := os.Stat(prdPath); os.IsNotExist(err) {
			_ = database.LogActivity(issue.ID, "build_event", "", "", "Creating PRD...")

			prompt, err := ai.RenderGeneratePRD(ai.GeneratePRDData{
				PlanText:      issue.PlanText,
				ProjectName:   ralphCfg.Project,
				PRDPath:       prdPath,
				BranchName:    branch,
				KnowledgePath: knowledge.Dir(project.LocalPath),
			}, cfg.OverrideDir)
			if err != nil {
				return fmt.Errorf("rendering PRD prompt: %w", err)
			}

			if _, err := cfg.Invoker.Invoke(context.Background(), prompt, project.LocalPath); err != nil {
				return fmt.Errorf("invoking AI for PRD generation: %w", err)
			}
		}

		generatedPRD, err := cfg.PRDRead.Read(prdPath)
		if err != nil {
			return fmt.Errorf("reading generated PRD: %w", err)
		}

		issue.WorkspaceName = wsName
		issue.BranchName = branch
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("storing workspace info: %w", err)
		}

		// Linear state update is non-fatal — the important thing is the
		// DB transition to BUILDING so the worker can pick it up.
		if err := updateLinearState(cfg.Linear, issue.LinearIssueID, project.LinearTeamID, "In Progress"); err != nil {
			_ = database.LogActivity(issue.ID, "warning", "", "", "Failed to update Linear state: "+err.Error())
		}

		if err := database.LogActivity(
			issue.ID,
			"workspace_created",
			"",
			"",
			fmt.Sprintf("Workspace %q on branch %q with PRD (%d stories)", wsName, branch, len(generatedPRD.UserStories)),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// updateLinearState fetches workflow states for the team and updates the issue
// to the state matching the given name.
func updateLinearState(client LinearStateUpdater, issueID, teamID, stateName string) error {
	states, err := client.FetchWorkflowStates(context.Background(), teamID)
	if err != nil {
		return fmt.Errorf("fetching workflow states: %w", err)
	}

	for _, s := range states {
		if s.Name == stateName {
			return client.UpdateIssueState(context.Background(), issueID, s.ID)
		}
	}

	return fmt.Errorf("workflow state %q not found for team %s", stateName, teamID)
}

// sanitizeName converts a Linear issue identifier (e.g., "PROJ-42") to a
// valid workspace name by lowercasing it. Linear identifiers already match
// the workspace name pattern (alphanumeric + hyphens).
func sanitizeName(identifier string) string {
	result := make([]byte, 0, len(identifier))
	for _, b := range []byte(identifier) {
		switch {
		case b >= 'A' && b <= 'Z':
			result = append(result, b+32) // lowercase
		case b >= 'a' && b <= 'z', b >= '0' && b <= '9', b == '-', b == '.', b == '_':
			result = append(result, b)
		default:
			result = append(result, '-')
		}
	}
	return string(result)
}
