package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (db *DB) LogActivity(issueID, eventType, fromState, toState, detail string) error {
	id := uuid.New().String()
	_, err := db.conn.Exec(`
		INSERT INTO activity_log (id, issue_id, event_type, from_state, to_state, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, issueID, eventType, fromState, toState, detail, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("logging activity: %w", err)
	}
	return nil
}

func (tx *Tx) LogActivity(issueID, eventType, fromState, toState, detail string) error {
	id := uuid.New().String()
	_, err := tx.tx.Exec(`
		INSERT INTO activity_log (id, issue_id, event_type, from_state, to_state, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, issueID, eventType, fromState, toState, detail, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("logging activity: %w", err)
	}
	return nil
}

func (db *DB) ListActivity(issueID string, limit, offset int) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, event_type, from_state, to_state, detail, created_at
		FROM activity_log WHERE issue_id = ?
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?`, issueID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.FromState, &e.ToState, &e.Detail, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanning activity: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (db *DB) ListBuildActivity(issueID string, limit, offset int) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, event_type, from_state, to_state, detail, created_at
		FROM activity_log WHERE issue_id = ? AND event_type = 'build_event'
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?`, issueID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing build activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.FromState, &e.ToState, &e.Detail, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanning build activity: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (db *DB) ListTimelineActivity(issueID string, limit, offset int) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, event_type, from_state, to_state, detail, created_at
		FROM activity_log WHERE issue_id = ? AND event_type != 'build_event'
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?`, issueID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing timeline activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.FromState, &e.ToState, &e.Detail, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanning timeline activity: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListRecentActivity returns the most recent activity entries across all issues.
func (db *DB) ListRecentActivity(limit int) ([]ActivityEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, event_type, from_state, to_state, detail, created_at
		FROM activity_log
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing recent activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.FromState, &e.ToState, &e.Detail, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanning activity: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
