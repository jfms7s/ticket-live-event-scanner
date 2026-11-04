package watch

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var now = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func TestSlugIsLenient(t *testing.T) {
	for in, want := range map[string]string{
		"auchan-live-academia-maia-98164":                                     "auchan-live-academia-maia-98164",
		"https://www.ticketline.pt/evento/auchan-live-academia-maia-98164":    "auchan-live-academia-maia-98164",
		"https://www.ticketline.pt/evento/auchan-live-academia-aveiro-98167/": "auchan-live-academia-aveiro-98167",
		"/evento/auchan-live-academia-maia-98164":                             "auchan-live-academia-maia-98164",
		"":    "",
		"   ": "",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAcceptsTicketlineHubs(t *testing.T) {
	for _, in := range []string{
		"https://www.ticketline.pt/evento/fado-hub-500",
		"http://ticketline.pt/evento/fado-hub-500/",
		"/evento/fado-hub-500",
		"evento/fado-hub-500",
		"fado-hub-500",
		"  fado-hub-500  ",
	} {
		got, err := Parse(in)
		if err != nil || got != "fado-hub-500" {
			t.Errorf("Parse(%q) = %q, %v; want fado-hub-500", in, got, err)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "",
		"other host":     "https://evil.example/evento/fado-hub-500",
		"lookalike host": "https://ticketline.pt.evil.example/evento/fado-hub-500",
		"userinfo host":  "https://www.ticketline.pt@evil.example/evento/fado-hub-500",
		"other scheme":   "ftp://www.ticketline.pt/evento/fado-hub-500",
		"no id":          "fado-hub",
		"zero id":        "fado-hub-0",
		"other path":     "https://www.ticketline.pt/agenda/fado-hub-500",
		"nested path":    "https://www.ticketline.pt/evento/a/fado-hub-500",
		"query in slug":  "fado-hub-500?x=1",
		"space in slug":  "fado hub-500",
		"leading hyphen": "-fado-500",
		"only digits":    "500",
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("%s: Parse(%q) = %q, want an error", name, in, got)
		}
	}
}

func testDB(t *testing.T, withTable bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if withTable {
		if _, err := db.Exec(`CREATE TABLE watches (slug TEXT PRIMARY KEY, label TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT 1, added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestAddListUpdate(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()

	w, err := Add(ctx, db, "a-1", "Maia", now)
	if err != nil || !w.Enabled || w.Label != "Maia" || w.AddedAt != "2026-11-04T12:00:00Z" {
		t.Fatalf("Add = %+v, %v", w, err)
	}
	if _, err := Add(ctx, db, "a-1", "again", now); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate Add error = %v, want ErrDuplicate", err)
	}

	off := false
	w, err = Update(ctx, db, "a-1", &off, nil)
	if err != nil || w.Enabled || w.Label != "Maia" {
		t.Errorf("Update(disable) = %+v, %v", w, err)
	}
	label := "Maia hub"
	w, err = Update(ctx, db, "a-1", nil, &label)
	if err != nil || w.Enabled || w.Label != "Maia hub" {
		t.Errorf("Update(label) = %+v, %v", w, err)
	}
	if _, err := Update(ctx, db, "missing-9", &off, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Update(missing) error = %v, want sql.ErrNoRows", err)
	}

	got, err := Disabled(ctx, db)
	if err != nil || !reflect.DeepEqual(got, map[string]bool{"a-1": true}) {
		t.Errorf("Disabled = %v, %v", got, err)
	}
}

func TestResolveSeedsAnEmptyTable(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	got, err := Resolve(ctx, db, []string{"a-1", "b-2"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"a-1", "b-2"}) {
		t.Fatalf("Resolve = %v, %v", got, err)
	}
	ws, _ := List(ctx, db)
	if len(ws) != 2 || !ws[0].Enabled {
		t.Errorf("seeded watches = %+v, want both enabled", ws)
	}
	// A second run changes nothing.
	if _, err := Resolve(ctx, db, []string{"a-1", "b-2"}, now); err != nil {
		t.Fatal(err)
	}
	if ws, _ := List(ctx, db); len(ws) != 2 {
		t.Errorf("after a second Resolve there are %d watches, want 2", len(ws))
	}
}

// A watch added on the dashboard before the scraper's first seed must not
// drop the configured hubs.
func TestResolveSeedsAroundAnExistingWatch(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	Add(ctx, db, "user-hub-55", "", now)

	got, err := Resolve(ctx, db, []string{"a-1", "b-2"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"user-hub-55", "a-1", "b-2"}) {
		t.Fatalf("Resolve = %v, %v; want the user's hub and both configured hubs", got, err)
	}
	if ws, _ := List(ctx, db); len(ws) != 3 {
		t.Errorf("watches = %+v, want 3", ws)
	}
}

func TestResolveKeepsADisabledSeededHubDisabled(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	Resolve(ctx, db, []string{"a-1", "b-2"}, now)
	off := false
	Update(ctx, db, "b-2", &off, nil)

	got, err := Resolve(ctx, db, []string{"a-1", "b-2"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"a-1"}) {
		t.Errorf("Resolve = %v, %v; want only a-1: the env var must not re-enable b-2", got, err)
	}
}

func TestResolveAllDisabledIsEmpty(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	Resolve(ctx, db, []string{"a-1"}, now)
	off := false
	Update(ctx, db, "a-1", &off, nil)
	got, err := Resolve(ctx, db, []string{"a-1"}, now)
	if err != nil || len(got) != 0 {
		t.Errorf("Resolve = %v, %v; want none", got, err)
	}
}

// A seed that cannot be written must not drop hubs: they are not known to be
// disabled.
func TestResolveStillScrapesTheSeedWhenItCannotBeWritten(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	if _, err := db.Exec(`CREATE TRIGGER no_ins BEFORE INSERT ON watches BEGIN SELECT RAISE(ABORT, 'readonly'); END`); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(ctx, db, []string{"a-1"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"a-1"}) {
		t.Errorf("Resolve = %v, %v; want the seed", got, err)
	}
}

// An entry without an event id is returned (so the run fails on it, loudly,
// as before) but not stored as a watch.
func TestResolveDoesNotStoreAnInvalidSeedEntry(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	got, err := Resolve(ctx, db, []string{"no-id", "a-1"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"no-id", "a-1"}) {
		t.Fatalf("Resolve = %v, %v", got, err)
	}
	if ws, _ := List(ctx, db); len(ws) != 1 || ws[0].Slug != "a-1" {
		t.Errorf("watches = %+v, want only a-1", ws)
	}
}

func TestAddRejectsTheSameHubIDUnderAnotherSlug(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	Add(ctx, db, "maia-98164", "", now)
	for _, slug := range []string{"maia-98164", "MAIA-98164", "other-name-98164"} {
		if _, err := Add(ctx, db, slug, "", now); !errors.Is(err, ErrDuplicate) {
			t.Errorf("Add(%q) error = %v, want ErrDuplicate", slug, err)
		}
	}
}

func TestResolveFallsBackWithoutTheTable(t *testing.T) {
	db, ctx := testDB(t, false), context.Background()
	got, err := Resolve(ctx, db, []string{"a-1"}, now)
	if err != nil || !reflect.DeepEqual(got, []string{"a-1"}) {
		t.Errorf("Resolve = %v, %v; want the env seed", got, err)
	}
	if off, err := Disabled(ctx, db); err != nil || off != nil {
		t.Errorf("Disabled without a table = %v, %v; want nil, nil", off, err)
	}
}

func TestResolveFailsOnADatabaseError(t *testing.T) {
	db, ctx := testDB(t, true), context.Background()
	db.Close()
	if got, err := Resolve(ctx, db, []string{"a-1"}, now); err == nil {
		t.Errorf("Resolve on a closed database = %v, nil; want an error, not the env hubs", got)
	}
}
