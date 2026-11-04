package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// productionLikeSource builds what sqld holds today: the tables as they were
// created before hubs and watches existed (DATE and TIMESTAMP columns, no
// hub_slug, no watches table), filled with rows that exercise NULLs, the
// hidden flag, a text with separators and non-ASCII, and AUTOINCREMENT ids.
func productionLikeSource(t *testing.T) *sql.DB {
	t.Helper()
	db := openSQLite(t)
	mustExec(t, db, `CREATE TABLE events (
		id INTEGER PRIMARY KEY, slug TEXT NOT NULL, title TEXT NOT NULL, venue TEXT, category TEXT,
		event_date DATE, url TEXT NOT NULL, image_url TEXT,
		discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		purchased BOOLEAN NOT NULL DEFAULT 0,
		invite_status TEXT CHECK (invite_status IN ('pending','sent','failed')), invite_error TEXT,
		hidden BOOLEAN NOT NULL DEFAULT 0)`)
	mustExec(t, db, `CREATE TABLE notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT, event_id INTEGER NOT NULL REFERENCES events(id),
		status TEXT NOT NULL CHECK (status IN ('pending','sent','failed')), telegram_message_id TEXT,
		attempted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, confirmed_at TIMESTAMP, error TEXT,
		triggered_by TEXT NOT NULL DEFAULT 'scraper', delivery_seq INTEGER)`)
	mustExec(t, db, `INSERT INTO events (id, slug, title, venue, event_date, url, discovered_at, purchased, invite_status, hidden) VALUES
		(98164, 'maia-98164', 'Académia; "Maia"', 'Fórum', '2026-08-25', 'https://x/1', '2026-08-17T09:00:00Z', 1, 'sent', 0),
		(98167, 'aveiro-98167', 'Aveiro', NULL, '2026-09-01T21:30', 'https://x/2', '2026-08-18T09:00:00Z', 0, NULL, 1),
		(109582, 'sushi-109582', '', '', NULL, 'https://x/3', '2026-11-04T16:00:00Z', 0, NULL, 0)`)
	mustExec(t, db, `INSERT INTO notifications (id, event_id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by, delivery_seq) VALUES
		(1, 98164, 'sent', '77', '2026-08-17T09:00:01Z', '2026-08-17T09:00:02Z', NULL, 'scraper', 10),
		(5, 98167, 'failed', NULL, '2026-08-18T09:00:01Z', NULL, 'boom', 'manual-retry', NULL)`)
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", strings.Fields(q)[0], err)
	}
}

func newDest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "dest.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
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

func TestImportCopiesEveryRowFaithfully(t *testing.T) {
	ctx := context.Background()
	src, dst := productionLikeSource(t), newDest(t)

	summary, err := copyAll(ctx, src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "events=3") || !strings.Contains(summary, "notifications=2") || !strings.Contains(summary, "watches=absent") {
		t.Errorf("summary = %q", summary)
	}

	for q, want := range map[string]string{
		`SELECT COUNT(*) FROM events`:                                 "3",
		`SELECT COUNT(*) FROM notifications`:                          "2",
		`SELECT title FROM events WHERE id = 98164`:                   `Académia; "Maia"`,
		`SELECT event_date FROM events WHERE id = 98164`:              "2026-08-25",
		`SELECT event_date FROM events WHERE id = 98167`:              "2026-09-01T21:30",
		`SELECT event_date FROM events WHERE id = 109582`:             "<NULL>",
		`SELECT venue FROM events WHERE id = 109582`:                  "",
		`SELECT venue FROM events WHERE id = 98167`:                   "<NULL>",
		`SELECT hidden FROM events WHERE id = 98167`:                  "1",
		`SELECT purchased FROM events WHERE id = 98164`:               "1",
		`SELECT invite_status FROM events WHERE id = 98164`:           "sent",
		`SELECT hub_slug FROM events WHERE id = 98164`:                "<NULL>",
		`SELECT discovered_at FROM events WHERE id = 98164`:           "2026-08-17T09:00:00Z",
		`SELECT typeof(purchased) FROM events WHERE id = 98164`:       "integer",
		`SELECT typeof(id) FROM events WHERE id = 98164`:              "integer",
		`SELECT error FROM notifications WHERE id = 5`:                "boom",
		`SELECT delivery_seq FROM notifications WHERE id = 1`:         "10",
		`SELECT typeof(delivery_seq) FROM notifications WHERE id = 1`: "integer",
		`SELECT confirmed_at FROM notifications WHERE id = 5`:         "<NULL>",
		`SELECT value FROM meta WHERE key = 'imported_from_libsql'`:   summary,
	} {
		if got := scalar(t, dst, q); got != want {
			t.Errorf("%s = %q, want %q", q, got, want)
		}
	}

	// AUTOINCREMENT carries on after the highest copied id.
	mustExec(t, dst, `INSERT INTO notifications (event_id, status) VALUES (98164, 'pending')`)
	if got := scalar(t, dst, `SELECT MAX(id) FROM notifications`); got != "6" {
		t.Errorf("next notification id = %s, want 6", got)
	}
}

// If what lands in the destination differs from what was read, nothing is
// kept: the process must not start on a database that is not the server's.
func TestImportRollsBackWhenTheCopyDoesNotVerify(t *testing.T) {
	ctx := context.Background()
	src, dst := productionLikeSource(t), newDest(t)
	mustExec(t, dst, `CREATE TRIGGER corrupt AFTER INSERT ON events BEGIN UPDATE events SET title = title || '!' WHERE id = NEW.id; END`)

	if _, err := copyAll(ctx, src, dst); err == nil || !strings.Contains(err.Error(), "verify events") {
		t.Fatalf("err = %v, want a verification failure on events", err)
	}
	if got := scalar(t, dst, `SELECT COUNT(*) FROM events`); got != "0" {
		t.Errorf("%s event rows were left behind", got)
	}
	if got := scalar(t, dst, `SELECT COUNT(*) FROM meta`); got != "0" {
		t.Error("the import marker was written for a failed copy")
	}
}

func TestImportOnceRules(t *testing.T) {
	ctx := context.Background()
	withSource := func(src *sql.DB, err error) func() {
		old := connectLegacy
		connectLegacy = func(string, string) (*sql.DB, error) { return src, err }
		return func() { connectLegacy = old }
	}
	opts := Options{LegacyURL: "libsql://x", LegacyToken: "t"}

	t.Run("fills a new file once, then never again", func(t *testing.T) {
		defer withSource(productionLikeSource(t), nil)()
		dst := newDest(t)
		if err := importLegacyOnce(ctx, opts, dst); err != nil {
			t.Fatal(err)
		}
		mustExec(t, dst, `DELETE FROM notifications`)
		mustExec(t, dst, `DELETE FROM events`)
		// A second start must not import again, even if the server is gone.
		defer withSource(nil, errors.New("sqld is down"))()
		if err := importLegacyOnce(ctx, opts, dst); err != nil {
			t.Fatalf("second start: %v", err)
		}
		if got := scalar(t, dst, `SELECT COUNT(*) FROM events`); got != "0" {
			t.Errorf("a second import put %s events back", got)
		}
	})

	t.Run("an unreachable server stops the start", func(t *testing.T) {
		defer withSource(nil, errors.New("sqld is down"))()
		dst := newDest(t)
		if err := importLegacyOnce(ctx, opts, dst); err == nil {
			t.Fatal("started with an empty database although the sqld could not be read")
		}
		if got := scalar(t, dst, `SELECT COUNT(*) FROM meta`); got != "0" {
			t.Error("marker written without a copy")
		}
	})

	t.Run("a file with data and no marker is not imported over", func(t *testing.T) {
		defer withSource(productionLikeSource(t), nil)()
		dst := newDest(t)
		mustExec(t, dst, `INSERT INTO events (id, slug, title, url) VALUES (1, 's', 't', 'u')`)
		if err := importLegacyOnce(ctx, opts, dst); !errors.Is(err, ErrNotEmpty) {
			t.Fatalf("err = %v, want ErrNotEmpty", err)
		}
	})
}

// A source that already has the newer tables and columns copies them too.
func TestImportCopiesWatchesAndHubSlugWhenTheSourceHasThem(t *testing.T) {
	ctx := context.Background()
	src := openSQLite(t)
	if err := Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}
	mustExec(t, src, `INSERT INTO events (id, slug, title, url, hub_slug) VALUES (1, 's', 't', 'u', 'maia-98164')`)
	mustExec(t, src, `INSERT INTO watches (slug, label, enabled, added_at) VALUES ('maia-98164', 'Maia', 0, '2026-11-04T12:00:00Z')`)
	dst := newDest(t)

	if _, err := copyAll(ctx, src, dst); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, dst, `SELECT hub_slug FROM events WHERE id = 1`); got != "maia-98164" {
		t.Errorf("hub_slug = %q", got)
	}
	if got := scalar(t, dst, `SELECT enabled || '|' || label FROM watches WHERE slug = 'maia-98164'`); got != "0|Maia" {
		t.Errorf("watch = %q, want the disabled flag kept", got)
	}
}

// The schema stores dates as the text that was written, whatever the driver.
func TestFreshSchemaReturnsDatesAsWrittenText(t *testing.T) {
	db := newDest(t)
	mustExec(t, db, `INSERT INTO events (id, slug, title, url, event_date, discovered_at) VALUES (1, 's', 't', 'u', '2026-08-25', '2026-08-17T09:00:00Z')`)
	if got := scalar(t, db, `SELECT event_date FROM events`); got != "2026-08-25" {
		t.Errorf("event_date = %q", got)
	}
	var ev, disc string
	if err := db.QueryRow(`SELECT event_date, discovered_at FROM events`).Scan(&ev, &disc); err != nil {
		t.Fatal(err)
	}
	if ev != "2026-08-25" || disc != "2026-08-17T09:00:00Z" {
		t.Errorf("scanned %q and %q, want the stored text", ev, disc)
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

// A missing or misnamed Secret must not look like a fresh install: the
// scraper would find every session new.
func TestOpenRefusesAnEmptyDatabaseWithNothingToImportFrom(t *testing.T) {
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

func TestOpenImportsThroughItsOwnHandleAndKeepsForeignKeysOnAfterwards(t *testing.T) {
	defer func(old func(string, string) (*sql.DB, error)) { connectLegacy = old }(connectLegacy)
	src := productionLikeSource(t)
	// An orphan: a notification whose event the server does not have.
	mustExec(t, src, `INSERT INTO notifications (id, event_id, status) VALUES (9, 424242, 'sent')`)
	connectLegacy = func(string, string) (*sql.DB, error) { return src, nil }

	path := filepath.Join(t.TempDir(), "x.db")
	db, err := Open(context.Background(), Options{Path: path, LegacyURL: "libsql://x"})
	if err != nil {
		t.Fatalf("an orphan row stopped the import: %v", err)
	}
	defer db.Close()
	if got := scalar(t, db, `SELECT COUNT(*) FROM notifications`); got != "3" {
		t.Errorf("notifications = %s, want all 3 including the orphan", got)
	}
	if got := scalar(t, db, `PRAGMA foreign_keys`); got != "1" {
		t.Errorf("foreign_keys = %s after the import, want 1", got)
	}
}

// A legacy server that accepts the connection and never answers stops the
// start with an error once the import's time is up.
func TestOpenGivesUpOnAHungLegacyServer(t *testing.T) {
	defer func(old func(string, string) (*sql.DB, error), d time.Duration) {
		connectLegacy, importTimeout = old, d
	}(connectLegacy, importTimeout)
	importTimeout = 1500 * time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	hang := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	hang.Listener.Close()
	hang.Listener = ln
	hang.Start()
	defer func() {
		close(release) // lets the handlers return before Close waits for them
		hang.Close()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "x.db"), LegacyURL: hang.URL})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Open succeeded against a server that never answers")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Open is still waiting for the hung server after 10 s")
	}
}
