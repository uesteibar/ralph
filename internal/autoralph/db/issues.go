package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type IssueFilter struct {
	ProjectID string
	State     string
	States    []string
}

func (db *DB) CreateIssue(issue Issue) (Issue, error) {
	if issue.ID == "" {
		issue.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	_, err := db.conn.Exec(`
		INSERT INTO issues (id, project_id, linear_issue_id, identifier, title, description,
			state, plan_text, workspace_name, branch_name, pr_number, pr_url,
			error_message, last_comment_id, last_review_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issue.ID, issue.ProjectID, issue.LinearIssueID, issue.Identifier,
		issue.Title, issue.Description, issue.State, issue.PlanText,
		issue.WorkspaceName, issue.BranchName, issue.PRNumber, issue.PRURL,
		issue.ErrorMessage, issue.LastCommentID, issue.LastReviewID,
		issue.CreatedAt.Format(time.RFC3339), issue.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return Issue{}, fmt.Errorf("creating issue: %w", err)
	}
	return issue, nil
}

func (db *DB) ListIssues(filter IssueFilter) ([]Issue, error) {
	query := `
		SELECT id, project_id, linear_issue_id, identifier, title, description,
			state, plan_text, workspace_name, branch_name, pr_number, pr_url,
			error_message, last_comment_id, last_review_id, created_at, updated_at
		FROM issues`

	var conditions []string
	var args []any

	if filter.ProjectID != "" {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.State != "" {
		conditions = append(conditions, "state = ?")
		args = append(args, filter.State)
	}
	if len(filter.States) > 0 {
		placeholders := make([]string, len(filter.States))
		for i, s := range filter.States {
			placeholders[i] = "?"
			args = append(args, s)
		}
		conditions = append(conditions, "state IN ("+strings.Join(placeholders, ", ")+")")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	defer rows.Close()

	var issues []Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetIssueByLinearID returns the issue with the given Linear issue ID,
// or sql.ErrNoRows-wrapped error if not found.
func (db *DB) GetIssueByLinearID(linearIssueID string) (Issue, error) {
	row := db.conn.QueryRow(`
		SELECT id, project_id, linear_issue_id, identifier, title, description,
			state, plan_text, workspace_name, branch_name, pr_number, pr_url,
			error_message, last_comment_id, last_review_id, created_at, updated_at
		FROM issues WHERE linear_issue_id = ?`, linearIssueID)

	issue, err := scanIssueRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Issue{}, fmt.Errorf("issue not found for linear_issue_id: %s: %w", linearIssueID, sql.ErrNoRows)
		}
		return Issue{}, fmt.Errorf("getting issue by linear_issue_id: %w", err)
	}
	return issue, nil
}

func (db *DB) GetIssue(id string) (Issue, error) {
	row := db.conn.QueryRow(`
		SELECT id, project_id, linear_issue_id, identifier, title, description,
			state, plan_text, workspace_name, branch_name, pr_number, pr_url,
			error_message, last_comment_id, last_review_id, created_at, updated_at
		FROM issues WHERE id = ?`, id)

	issue, err := scanIssueRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Issue{}, fmt.Errorf("issue not found: %s", id)
		}
		return Issue{}, fmt.Errorf("getting issue: %w", err)
	}
	return issue, nil
}

func (db *DB) UpdateIssue(issue Issue) error {
	issue.UpdatedAt = time.Now().UTC()
	result, err := db.conn.Exec(`
		UPDATE issues SET project_id = ?, linear_issue_id = ?, identifier = ?,
			title = ?, description = ?, state = ?, plan_text = ?,
			workspace_name = ?, branch_name = ?, pr_number = ?, pr_url = ?,
			error_message = ?, last_comment_id = ?, last_review_id = ?, updated_at = ?
		WHERE id = ?`,
		issue.ProjectID, issue.LinearIssueID, issue.Identifier,
		issue.Title, issue.Description, issue.State, issue.PlanText,
		issue.WorkspaceName, issue.BranchName, issue.PRNumber, issue.PRURL,
		issue.ErrorMessage, issue.LastCommentID, issue.LastReviewID,
		issue.UpdatedAt.Format(time.RFC3339), issue.ID,
	)
	if err != nil {
		return fmt.Errorf("updating issue: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue not found: %s", issue.ID)
	}
	return nil
}

func (tx *Tx) UpdateIssue(issue Issue) error {
	issue.UpdatedAt = time.Now().UTC()
	result, err := tx.tx.Exec(`
		UPDATE issues SET project_id = ?, linear_issue_id = ?, identifier = ?,
			title = ?, description = ?, state = ?, plan_text = ?,
			workspace_name = ?, branch_name = ?, pr_number = ?, pr_url = ?,
			error_message = ?, last_comment_id = ?, last_review_id = ?, updated_at = ?
		WHERE id = ?`,
		issue.ProjectID, issue.LinearIssueID, issue.Identifier,
		issue.Title, issue.Description, issue.State, issue.PlanText,
		issue.WorkspaceName, issue.BranchName, issue.PRNumber, issue.PRURL,
		issue.ErrorMessage, issue.LastCommentID, issue.LastReviewID,
		issue.UpdatedAt.Format(time.RFC3339), issue.ID,
	)
	if err != nil {
		return fmt.Errorf("updating issue: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue not found: %s", issue.ID)
	}
	return nil
}

// GetIssue reads an issue within the transaction, preserving changes made by
// earlier writes in the same transaction.
func (tx *Tx) GetIssue(id string) (Issue, error) {
	row := tx.tx.QueryRow(`
		SELECT id, project_id, linear_issue_id, identifier, title, description,
			state, plan_text, workspace_name, branch_name, pr_number, pr_url,
			error_message, last_comment_id, last_review_id, created_at, updated_at
		FROM issues WHERE id = ?`, id)

	issue, err := scanIssueRow(row)
	if err != nil {
		return Issue{}, fmt.Errorf("getting issue in tx: %w", err)
	}
	return issue, nil
}

func (db *DB) DeleteIssue(id string) error {
	// Delete activity_log entries first (FK constraint).
	if _, err := db.conn.Exec(`DELETE FROM activity_log WHERE issue_id = ?`, id); err != nil {
		return fmt.Errorf("deleting activity for issue: %w", err)
	}
	result, err := db.conn.Exec(`DELETE FROM issues WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting issue: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue not found: %s", id)
	}
	return nil
}

func scanIssue(rows *sql.Rows) (Issue, error) {
	var issue Issue
	var createdAt, updatedAt string
	err := rows.Scan(&issue.ID, &issue.ProjectID, &issue.LinearIssueID,
		&issue.Identifier, &issue.Title, &issue.Description, &issue.State,
		&issue.PlanText, &issue.WorkspaceName, &issue.BranchName,
		&issue.PRNumber, &issue.PRURL, &issue.ErrorMessage,
		&issue.LastCommentID, &issue.LastReviewID, &createdAt, &updatedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("scanning issue: %w", err)
	}
	issue.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	issue.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return issue, nil
}

func scanIssueRow(row *sql.Row) (Issue, error) {
	var issue Issue
	var createdAt, updatedAt string
	err := row.Scan(&issue.ID, &issue.ProjectID, &issue.LinearIssueID,
		&issue.Identifier, &issue.Title, &issue.Description, &issue.State,
		&issue.PlanText, &issue.WorkspaceName, &issue.BranchName,
		&issue.PRNumber, &issue.PRURL, &issue.ErrorMessage,
		&issue.LastCommentID, &issue.LastReviewID, &createdAt, &updatedAt)
	if err != nil {
		return Issue{}, err
	}
	issue.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	issue.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return issue, nil
}
