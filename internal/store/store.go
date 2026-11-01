// Package store holds the Turso/libSQL connection helper and schema
// shared by every service that talks to the database (web-ui-api writes
// it, scraper reads known event IDs from it), per docs/design.md §7.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// Schema is applied with CREATE TABLE IF NOT EXISTS, so it is safe to run
// on every service startup that needs it (currently just web-ui-api).
const Schema = `
CREATE TABLE IF NOT EXISTS events (
    id            INTEGER PRIMARY KEY,
    slug          TEXT NOT NULL,
    title         TEXT NOT NULL,
    venue         TEXT,
    category      TEXT,
    event_date    DATE,
    url           TEXT NOT NULL,
    image_url     TEXT,
    discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    purchased     BOOLEAN NOT NULL DEFAULT 0,
    invite_status TEXT CHECK (invite_status IN ('pending','sent','failed')),
    invite_error  TEXT,
    hidden        BOOLEAN NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS notifications (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id            INTEGER NOT NULL REFERENCES events(id),
    status              TEXT NOT NULL CHECK (status IN ('pending','sent','failed')),
    telegram_message_id TEXT,
    attempted_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confirmed_at        TIMESTAMP,
    error               TEXT,
    triggered_by        TEXT NOT NULL DEFAULT 'scraper',
    delivery_seq        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_notifications_event_id ON notifications(event_id);
CREATE INDEX IF NOT EXISTS idx_events_discovered_at ON events(discovered_at);
`

// Connect opens a libSQL connection to Turso. dbURL is expected in the
// form "libsql://<db>-<org>.turso.io"; authToken is appended as the
// driver's ?authToken query parameter.
func Connect(dbURL, authToken string) (*sql.DB, error) {
	dsn := dbURL
	if authToken != "" {
		sep := "?"
		if strings.Contains(dbURL, "?") {
			sep = "&"
		}
		dsn = fmt.Sprintf("%s%sauthToken=%s", dbURL, sep, authToken)
	}

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open turso connection: connection failed (check TURSO_DATABASE_URL/TURSO_AUTH_TOKEN)")
	}
	return db, nil
}

// addedEventColumns are events columns added after the table already
// existed in production, where CREATE TABLE IF NOT EXISTS in Schema is a
// no-op. Migrate adds each one with ALTER TABLE. Every definition must be
// valid for ADD COLUMN on a table with rows (nullable, or NOT NULL with a
// default) and must match its line in Schema.
var addedEventColumns = []string{
	"purchased BOOLEAN NOT NULL DEFAULT 0",
	"invite_status TEXT CHECK (invite_status IN ('pending','sent','failed'))",
	"invite_error TEXT",
	// hidden is the dashboard's Remove: the row stays as the record that
	// the event was already notified.
	"hidden BOOLEAN NOT NULL DEFAULT 0",
}

// addedNotificationColumns are notifications columns added after the
// table already existed in production; same rules as addedEventColumns.
var addedNotificationColumns = []string{
	// delivery_seq is the EVENTS stream sequence of the events.discovered
	// message a row was made for (NULL on rows from before it existed).
	"delivery_seq INTEGER",
}

// postColumnIndexes reference added columns, so Migrate creates them after
// the ADD COLUMNs rather than in Schema, which runs first and would fail on
// an older table that lacks the column. SQLite treats NULLs as distinct in
// a UNIQUE index, so rows without delivery_seq never conflict.
const postColumnIndexes = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_event_delivery_seq ON notifications(event_id, delivery_seq);
`

// addColumn runs ALTER TABLE ... ADD COLUMN, treating "duplicate column
// name" as already applied, since SQLite has no ADD COLUMN IF NOT EXISTS.
func addColumn(ctx context.Context, db *sql.DB, table, def string) error {
	if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+def); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			name, _, _ := strings.Cut(def, " ")
			return fmt.Errorf("add %s.%s column: %w", table, name, err)
		}
	}
	return nil
}

// Migrate applies Schema, then adds any columns an older database lacks
// and the indexes on them. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, Schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}

	for _, col := range addedEventColumns {
		if err := addColumn(ctx, db, "events", col); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}

	for _, col := range addedNotificationColumns {
		if err := addColumn(ctx, db, "notifications", col); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, postColumnIndexes); err != nil {
		return fmt.Errorf("migrate indexes: %w", err)
	}

	return nil
}
