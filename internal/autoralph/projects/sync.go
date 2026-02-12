package projects

import (
	"fmt"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/db"
)

// Sync synchronizes a list of validated project configs to the SQLite
// projects table. Existing projects (matched by name) are updated;
// new projects are created.
func Sync(database *db.DB, configs []ProjectConfig) error {
	for _, cfg := range configs {
		existing, err := database.GetProjectByName(cfg.Name)
		if err != nil {
			if !strings.Contains(err.Error(), "project not found") {
				return fmt.Errorf("looking up project %q: %w", cfg.Name, err)
			}
			// New project — create it.
			_, err := database.CreateProject(db.Project{
				Name:               cfg.Name,
				LocalPath:          cfg.LocalPath,
				CredentialsProfile: cfg.CredentialsProfile,
				GithubOwner:        cfg.Github.Owner,
				GithubRepo:         cfg.Github.Repo,
				LinearTeamID:       cfg.Linear.TeamID,
				LinearAssigneeID:   cfg.Linear.AssigneeID,
				RalphConfigPath:    cfg.RalphConfigPath,
				MaxIterations:      cfg.MaxIterations,
				BranchPrefix:       cfg.BranchPrefix,
			})
			if err != nil {
				return fmt.Errorf("creating project %q: %w", cfg.Name, err)
			}
			continue
		}

		// Existing project — update it.
		existing.LocalPath = cfg.LocalPath
		existing.CredentialsProfile = cfg.CredentialsProfile
		existing.GithubOwner = cfg.Github.Owner
		existing.GithubRepo = cfg.Github.Repo
		existing.LinearTeamID = cfg.Linear.TeamID
		existing.LinearAssigneeID = cfg.Linear.AssigneeID
		existing.RalphConfigPath = cfg.RalphConfigPath
		existing.MaxIterations = cfg.MaxIterations
		existing.BranchPrefix = cfg.BranchPrefix

		if err := database.UpdateProject(existing); err != nil {
			return fmt.Errorf("updating project %q: %w", cfg.Name, err)
		}
	}
	return nil
}
