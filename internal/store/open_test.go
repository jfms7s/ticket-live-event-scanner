package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func scalar(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	if !s.Valid {
		return "<NULL>"
	}
	return s.String
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "x.db"), AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// A wrong or unmounted volume must not look like a fresh install: the
// scraper would find every session new and announce them all again.
func TestOpenRefusesAnEmptyDatabaseUnlessAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	if _, err := Open(context.Background(), Options{Path: path}); !errors.Is(err, ErrEmptyDatabase) {
		t.Fatalf("err = %v, want ErrEmptyDatabase", err)
	}
	// A fresh install says so.
	db, err := Open(context.Background(), Options{Path: path, AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO events (id, slug, title, url) VALUES (1, 's', 't', 'u')`)
	db.Close()
	// Once it has data it is not "empty" any more, and restarts need no flag.
	db, err = Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("restart of a database with data: %v", err)
	}
	db.Close()
}

func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open(context.Background(), Options{AllowEmpty: true}); err == nil {
		t.Fatal("Open accepted an empty path")
	}
}

func TestOpenCreatesAWorkingWALDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := Open(ctx, Options{Path: path, AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := scalar(t, db, `PRAGMA journal_mode`); got != "wal" {
		t.Errorf("journal_mode = %s, want wal", got)
	}
	if got := scalar(t, db, `PRAGMA foreign_keys`); got != "1" {
		t.Errorf("foreign_keys = %s, want 1", got)
	}
	if got := scalar(t, db, `PRAGMA busy_timeout`); got != "5000" {
		t.Errorf("busy_timeout = %s, want 5000", got)
	}
	// A second process (web-ui-api --scrape-once) opens the same file.
	db2, err := Open(ctx, Options{Path: path, AllowEmpty: true})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db2.Close()
	mustExec(t, db, `INSERT INTO watches (slug) VALUES ('a-1')`)
	if got := scalar(t, db2, `SELECT COUNT(*) FROM watches`); got != "1" {
		t.Errorf("second handle sees %s watches, want 1", got)
	}
}

// The schema stores dates as the text that was written, whatever the driver.
func TestSchemaReturnsDatesAsWrittenText(t *testing.T) {
	db := newDB(t)
	mustExec(t, db, `INSERT INTO events (id, slug, title, url, event_date, discovered_at) VALUES (1, 's', 't', 'u', '2026-08-25', '2026-08-17T09:00:00Z')`)
	var ev, disc string
	if err := db.QueryRow(`SELECT event_date, discovered_at FROM events`).Scan(&ev, &disc); err != nil {
		t.Fatal(err)
	}
	if ev != "2026-08-25" || disc != "2026-08-17T09:00:00Z" {
		t.Errorf("scanned %q and %q, want the stored text", ev, disc)
	}
}

// The production database predates the outbox. Migrating it adds the
// columns without touching a row, and nothing already there becomes due: a
// row left pending by the earlier release (it was waiting for a result that
// will never come) must not suddenly be sent.
func TestMigrateAddsTheOutboxToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	mustExec(t, db, `CREATE TABLE events (id INTEGER PRIMARY KEY, slug TEXT NOT NULL, title TEXT NOT NULL, venue TEXT, category TEXT,
		event_date TEXT, url TEXT NOT NULL, image_url TEXT, discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		purchased BOOLEAN NOT NULL DEFAULT 0, invite_status TEXT, invite_error TEXT, hidden BOOLEAN NOT NULL DEFAULT 0, hub_slug TEXT)`)
	mustExec(t, db, `CREATE TABLE notifications (id INTEGER PRIMARY KEY AUTOINCREMENT, event_id INTEGER NOT NULL REFERENCES events(id),
		status TEXT NOT NULL CHECK (status IN ('pending','sent','failed')), telegram_message_id TEXT,
		attempted_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, confirmed_at TEXT, error TEXT,
		triggered_by TEXT NOT NULL DEFAULT 'scraper', delivery_seq INTEGER)`)
	mustExec(t, db, `INSERT INTO events (id, slug, title, url) VALUES (1, 'a', 'A', 'u'), (2, 'b', 'B', 'u')`)
	mustExec(t, db, `INSERT INTO notifications (event_id, status, telegram_message_id) VALUES (1, 'sent', '77'), (2, 'pending', NULL)`)

	for i := 0; i < 2; i++ { // twice: Migrate is safe to repeat
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	for q, want := range map[string]string{
		`SELECT COUNT(*) FROM notifications`:                                        "2",
		`SELECT channel FROM notifications WHERE event_id = 2`:                      "telegram",
		`SELECT attempts FROM notifications WHERE event_id = 2`:                     "0",
		`SELECT next_attempt_at FROM notifications WHERE event_id = 2`:              "<NULL>",
		`SELECT status FROM notifications WHERE event_id = 2`:                       "pending",
		`SELECT telegram_message_id FROM notifications WHERE event_id = 1`:          "77",
		`SELECT COUNT(*) FROM notifications WHERE next_attempt_at IS NOT NULL`:      "0",
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'idx_notifications_dedup'`: "1",
	} {
		if got := scalar(t, db, q); got != want {
			t.Errorf("%s = %q, want %q", q, got, want)
		}
	}
	// Rows from before the outbox have no dedup key, so they never collide.
	mustExec(t, db, `INSERT INTO notifications (event_id, status) VALUES (1, 'sent')`)
	// New ones with a key are unique per (event, channel, key).
	mustExec(t, db, `INSERT INTO notifications (event_id, status, channel, dedup_key) VALUES (1, 'pending', 'telegram', 'k')`)
	if _, err := db.Exec(`INSERT INTO notifications (event_id, status, channel, dedup_key) VALUES (1, 'pending', 'telegram', 'k')`); err == nil {
		t.Error("the same (event, channel, dedup key) was accepted twice")
	}
	mustExec(t, db, `INSERT INTO notifications (event_id, status, channel, dedup_key) VALUES (1, 'pending', 'email', 'k')`)
}
