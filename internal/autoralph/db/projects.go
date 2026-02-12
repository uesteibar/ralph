package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (db *DB) CreateProject(p Project) (Project, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err := db.conn.Exec(`
		INSERT INTO projects (id, name, local_path, credentials_profile, github_owner, github_repo,
			linear_team_id, linear_assignee_id, ralph_config_path, max_iterations, branch_prefix,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.LocalPath, p.CredentialsProfile, p.GithubOwner, p.GithubRepo,
		p.LinearTeamID, p.LinearAssigneeID, p.RalphConfigPath, p.MaxIterations, p.BranchPrefix,
		p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return Project{}, fmt.Errorf("creating project: %w", err)
	}
	return p, nil
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, local_path, credentials_profile, github_owner, github_repo,
			linear_team_id, linear_assignee_id, ralph_config_path, max_iterations, branch_prefix,
			created_at, updated_at
		FROM projects ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (db *DB) GetProject(id string) (Project, error) {
	row := db.conn.QueryRow(`
		SELECT id, name, local_path, credentials_profile, github_owner, github_repo,
			linear_team_id, linear_assignee_id, ralph_config_path, max_iterations, branch_prefix,
			created_at, updated_at
		FROM projects WHERE id = ?`, id)

	p, err := scanProjectRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Project{}, fmt.Errorf("project not found: %s", id)
		}
		return Project{}, fmt.Errorf("getting project: %w", err)
	}
	return p, nil
}

func (db *DB) GetProjectByName(name string) (Project, error) {
	row := db.conn.QueryRow(`
		SELECT id, name, local_path, credentials_profile, github_owner, github_repo,
			linear_team_id, linear_assignee_id, ralph_config_path, max_iterations, branch_prefix,
			created_at, updated_at
		FROM projects WHERE name = ?`, name)

	p, err := scanProjectRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Project{}, fmt.Errorf("project not found: %s", name)
		}
		return Project{}, fmt.Errorf("getting project by name: %w", err)
	}
	return p, nil
}

func (db *DB) UpdateProject(p Project) error {
	p.UpdatedAt = time.Now().UTC()
	result, err := db.conn.Exec(`
		UPDATE projects SET name = ?, local_path = ?, credentials_profile = ?,
			github_owner = ?, github_repo = ?, linear_team_id = ?, linear_assignee_id = ?,
			ralph_config_path = ?, max_iterations = ?, branch_prefix = ?, updated_at = ?
		WHERE id = ?`,
		p.Name, p.LocalPath, p.CredentialsProfile, p.GithubOwner, p.GithubRepo,
		p.LinearTeamID, p.LinearAssigneeID, p.RalphConfigPath, p.MaxIterations,
		p.BranchPrefix, p.UpdatedAt.Format(time.RFC3339), p.ID,
	)
	if err != nil {
		return fmt.Errorf("updating project: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project not found: %s", p.ID)
	}
	return nil
}

func (db *DB) DeleteProject(id string) error {
	result, err := db.conn.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project not found: %s", id)
	}
	return nil
}

func scanProject(rows *sql.Rows) (Project, error) {
	var p Project
	var createdAt, updatedAt string
	err := rows.Scan(&p.ID, &p.Name, &p.LocalPath, &p.CredentialsProfile,
		&p.GithubOwner, &p.GithubRepo, &p.LinearTeamID, &p.LinearAssigneeID,
		&p.RalphConfigPath, &p.MaxIterations, &p.BranchPrefix,
		&createdAt, &updatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("scanning project: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return p, nil
}

// IssueCountByProject holds the count of active issues for a project.
type IssueCountByProject struct {
	ProjectID string
	Count     int
}

// CountActiveIssuesByProject returns the number of non-completed, non-failed
// issues for each project that has at least one such issue.
func (db *DB) CountActiveIssuesByProject() (map[string]int, error) {
	rows, err := db.conn.Query(`
		SELECT project_id, COUNT(*) as cnt
		FROM issues
		WHERE state NOT IN ('completed', 'failed', 'dismissed')
		GROUP BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("counting active issues: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var projectID string
		var count int
		if err := rows.Scan(&projectID, &count); err != nil {
			return nil, fmt.Errorf("scanning issue count: %w", err)
		}
		counts[projectID] = count
	}
	return counts, rows.Err()
}

// CountIssuesByStateForProject returns a map of state→count for a given project.
func (db *DB) CountIssuesByStateForProject(projectID string) (map[string]int, error) {
	rows, err := db.conn.Query(`
		SELECT state, COUNT(*) as cnt
		FROM issues
		WHERE project_id = ?
		GROUP BY state`, projectID)
	if err != nil {
		return nil, fmt.Errorf("counting issues by state: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scanning state count: %w", err)
		}
		counts[state] = count
	}
	return counts, rows.Err()
}

func scanProjectRow(row *sql.Row) (Project, error) {
	var p Project
	var createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.LocalPath, &p.CredentialsProfile,
		&p.GithubOwner, &p.GithubRepo, &p.LinearTeamID, &p.LinearAssigneeID,
		&p.RalphConfigPath, &p.MaxIterations, &p.BranchPrefix,
		&createdAt, &updatedAt)
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return p, nil
}
