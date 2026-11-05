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
