package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

type Project struct {
	ID                 string
	Name               string
	LocalPath          string
	CredentialsProfile string
	GithubOwner        string
	GithubRepo         string
	LinearTeamID       string
	LinearAssigneeID   string
	LinearProjectID    string
	LinearLabel        string
	RalphConfigPath    string
	MaxIterations      int
	BranchPrefix       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Issue struct {
	ID               string
	ProjectID        string
	LinearIssueID    string
	Identifier       string
	Title            string
	Description      string
	State            string
	PlanText         string
	WorkspaceName    string
	BranchName       string
	PRNumber         int
	PRURL            string
	ErrorMessage     string
	LastCommentID    string
	LastReviewID     string
	LastCheckSHA     string
	CheckFixAttempts int
	InputTokens      int
	OutputTokens     int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ActivityEntry struct {
	ID        string
	IssueID   string
	EventType string
	FromState string
	ToState   string
	Detail    string
	CreatedAt time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	local_path TEXT NOT NULL,
	credentials_profile TEXT NOT NULL DEFAULT '',
	github_owner TEXT NOT NULL DEFAULT '',
	github_repo TEXT NOT NULL DEFAULT '',
	linear_team_id TEXT NOT NULL DEFAULT '',
	linear_assignee_id TEXT NOT NULL DEFAULT '',
	linear_project_id TEXT NOT NULL DEFAULT '',
	linear_label TEXT NOT NULL DEFAULT '',
	ralph_config_path TEXT NOT NULL DEFAULT '.ralph/ralph.yaml',
	max_iterations INTEGER NOT NULL DEFAULT 20,
	branch_prefix TEXT NOT NULL DEFAULT 'autoralph/',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS issues (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id),
	linear_issue_id TEXT NOT NULL DEFAULT '',
	identifier TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'queued',
	plan_text TEXT NOT NULL DEFAULT '',
	workspace_name TEXT NOT NULL DEFAULT '',
	branch_name TEXT NOT NULL DEFAULT '',
	pr_number INTEGER NOT NULL DEFAULT 0,
	pr_url TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	last_comment_id TEXT NOT NULL DEFAULT '',
	last_review_id TEXT NOT NULL DEFAULT '',
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS activity_log (
	id TEXT PRIMARY KEY,
	issue_id TEXT NOT NULL REFERENCES issues(id),
	event_type TEXT NOT NULL,
	from_state TEXT NOT NULL DEFAULT '',
	to_state TEXT NOT NULL DEFAULT '',
	detail TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	dir := filepath.Join(home, ".autoralph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}
	return filepath.Join(dir, "autoralph.db"), nil
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating directory %s: %w", dir, err)
	}

	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running schema migration: %w", err)
	}

	// Migrations for existing databases: add columns that may not exist yet.
	// ALTER TABLE ADD COLUMN errors are silently ignored (column already exists).
	conn.Exec(`ALTER TABLE projects ADD COLUMN linear_project_id TEXT NOT NULL DEFAULT ''`)
	conn.Exec(`ALTER TABLE projects ADD COLUMN linear_label TEXT NOT NULL DEFAULT ''`)
	conn.Exec(`ALTER TABLE issues ADD COLUMN last_check_sha TEXT NOT NULL DEFAULT ''`)
	conn.Exec(`ALTER TABLE issues ADD COLUMN check_fix_attempts INTEGER NOT NULL DEFAULT 0`)
	conn.Exec(`ALTER TABLE issues ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`)
	conn.Exec(`ALTER TABLE issues ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`)

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// Tx runs fn within a database transaction. If fn returns an error, the
// transaction is rolled back; otherwise it is committed.
func (db *DB) Tx(fn func(tx *Tx) error) error {
	sqlTx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := fn(&Tx{tx: sqlTx}); err != nil {
		sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

// Tx wraps a sql.Tx for use within transactional operations.
type Tx struct {
	tx *sql.Tx
}
