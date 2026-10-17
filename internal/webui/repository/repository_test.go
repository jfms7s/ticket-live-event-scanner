package repository

import (
	"context"
	"database/sql"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
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

// setupTestSchema applies the production schema (store.Migrate), so tests
// see the same columns as a migrated database.
func setupTestSchema(ctx context.Context, db *sql.DB) error {
	return store.Migrate(ctx, db)
}
