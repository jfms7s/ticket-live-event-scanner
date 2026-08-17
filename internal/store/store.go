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
    discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id            INTEGER NOT NULL REFERENCES events(id),
    status              TEXT NOT NULL CHECK (status IN ('pending','sent','failed')),
    telegram_message_id TEXT,
    attempted_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confirmed_at        TIMESTAMP,
    error               TEXT,
    triggered_by        TEXT NOT NULL DEFAULT 'scraper'
);

CREATE INDEX IF NOT EXISTS idx_notifications_event_id ON notifications(event_id);
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

// Migrate applies Schema. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, Schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}
