package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupTestDB creates a file-based SQLite database for testing.
// Using file-based DB instead of :memory: to avoid connection pool issues
func setupTestDB(t *testing.T) *sql.DB {
	// Use file-based SQLite database for unit tests with URI=true for better control
	tempDir := t.TempDir()
	dbPath := "file:" + tempDir + "/test.db?cache=shared"

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Limit connection pool to avoid database locking issues in tests
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Set reasonable timeouts
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create tables directly for testing
	if err := setupTestSchema(ctx, db); err != nil {
		t.Fatalf("Failed to setup test schema: %v", err)
	}

	return db
}

// setupTestSchema creates the necessary tables for testing
func setupTestSchema(ctx context.Context, db *sql.DB) error {
	// Create tables separately since Exec doesn't handle multiple statements well
	tables := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id            INTEGER PRIMARY KEY,
			slug          TEXT NOT NULL,
			title         TEXT NOT NULL,
			venue         TEXT,
			category      TEXT,
			event_date    DATE,
			url           TEXT NOT NULL,
			image_url     TEXT,
			discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			purchased     BOOLEAN NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id            INTEGER NOT NULL REFERENCES events(id),
			status              TEXT NOT NULL CHECK (status IN ('pending','sent','failed')),
			telegram_message_id TEXT,
			attempted_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			confirmed_at        TIMESTAMP,
			error               TEXT,
			triggered_by        TEXT NOT NULL DEFAULT 'scraper'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_event_id ON notifications(event_id)`,
	}

	for _, table := range tables {
		if _, err := db.ExecContext(ctx, table); err != nil {
			return err
		}
	}
	return nil
}
