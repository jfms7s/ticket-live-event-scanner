package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestSchemaContainsBothTables(t *testing.T) {
	if !strings.Contains(Schema, "CREATE TABLE IF NOT EXISTS events") {
		t.Error("Schema missing events table definition")
	}
	if !strings.Contains(Schema, "CREATE TABLE IF NOT EXISTS notifications") {
		t.Error("Schema missing notifications table definition")
	}
}

func TestSchemaContainsEventColumns(t *testing.T) {
	requiredColumns := []string{
		"id",
		"slug",
		"title",
		"venue",
		"category",
		"event_date",
		"url",
		"image_url",
		"discovered_at",
	}
	for _, col := range requiredColumns {
		if !strings.Contains(Schema, col) {
			t.Errorf("Schema missing events column: %s", col)
		}
	}
}

func TestSchemaContainsNotificationColumns(t *testing.T) {
	requiredColumns := []string{
		"id",
		"event_id",
		"status",
		"telegram_message_id",
		"attempted_at",
		"confirmed_at",
		"error",
		"triggered_by",
	}
	for _, col := range requiredColumns {
		if !strings.Contains(Schema, col) {
			t.Errorf("Schema missing notifications column: %s", col)
		}
	}
}

func TestSchemaContainsIndex(t *testing.T) {
	if !strings.Contains(Schema, "CREATE INDEX IF NOT EXISTS idx_notifications_event_id") {
		t.Error("Schema missing idx_notifications_event_id index")
	}
}

// openSQLite opens a temp-file SQLite database through the same driver the
// tests use elsewhere (modernc.org/sqlite, imported by this package).
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/store.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func columnNames(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	return cols
}

// TestMigrateAddsColumnsToLegacyEvents runs Migrate on an events table
// created before the purchased and invite columns existed (as production
// has it), twice, and checks the columns arrive and old rows survive.
func TestMigrateAddsColumnsToLegacyEvents(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)

	if _, err := db.ExecContext(ctx, `CREATE TABLE events (
		id            INTEGER PRIMARY KEY,
		slug          TEXT NOT NULL,
		title         TEXT NOT NULL,
		venue         TEXT,
		category      TEXT,
		event_date    DATE,
		url           TEXT NOT NULL,
		image_url     TEXT,
		discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create legacy events: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO events (id, slug, title, url) VALUES (1, 's', 't', 'u')`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	cols := columnNames(t, db, "events")
	for _, c := range []string{"purchased", "invite_status", "invite_error"} {
		if !cols[c] {
			t.Errorf("events missing column %q after Migrate", c)
		}
	}

	var purchased bool
	var inviteStatus, inviteError sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT purchased, invite_status, invite_error FROM events WHERE id = 1`).
		Scan(&purchased, &inviteStatus, &inviteError); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if purchased || inviteStatus.Valid || inviteError.Valid {
		t.Errorf("legacy row = (%v, %v, %v), want (false, NULL, NULL)", purchased, inviteStatus, inviteError)
	}

	if _, err := db.ExecContext(ctx, `UPDATE events SET invite_status = 'bogus' WHERE id = 1`); err == nil {
		t.Error("invite_status accepted a value outside pending/sent/failed")
	}
}

// TestMigrateFreshDatabase checks Schema alone yields the invite columns.
func TestMigrateFreshDatabase(t *testing.T) {
	db := openSQLite(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	cols := columnNames(t, db, "events")
	for _, c := range []string{"purchased", "invite_status", "invite_error"} {
		if !cols[c] {
			t.Errorf("events missing column %q", c)
		}
	}
}

// TestMigrateAddsHiddenColumn checks that a database migrated before soft
// hide existed gains events.hidden, defaulting existing rows to visible.
func TestMigrateAddsHiddenColumn(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)

	if _, err := db.ExecContext(ctx, `CREATE TABLE events (
		id INTEGER PRIMARY KEY, slug TEXT NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL,
		discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		purchased BOOLEAN NOT NULL DEFAULT 0,
		invite_status TEXT CHECK (invite_status IN ('pending','sent','failed')),
		invite_error TEXT
	)`); err != nil {
		t.Fatalf("create events: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO events (id, slug, title, url) VALUES (1, 's', 't', 'u')`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	var hidden bool
	if err := db.QueryRowContext(ctx, `SELECT hidden FROM events WHERE id = 1`).Scan(&hidden); err != nil {
		t.Fatalf("read hidden: %v", err)
	}
	if hidden {
		t.Error("existing row hidden = true after Migrate, want false")
	}

	fresh := openSQLite(t)
	if err := Migrate(ctx, fresh); err != nil {
		t.Fatalf("Migrate fresh: %v", err)
	}
	if !columnNames(t, fresh, "events")["hidden"] {
		t.Error("fresh schema has no events.hidden column")
	}
}
