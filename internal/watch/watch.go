// Package watch is the list of ticketline.pt hub pages the scraper tracks:
// the watches table, which web-ui-api manages, and how a hub URL becomes a
// slug. The scraper reads the table at the start of every run
// (see Resolve); TICKETLINE_HUB_PAGES only seeds it.
package watch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
)

// Watch is one tracked hub page.
type Watch struct {
	// Slug is the hub page's "/evento/{slug}" slug, which ends in its
	// ticketline.pt event ID.
	Slug string `json:"slug"`
	// Label is an optional name for the dashboard; empty shows the slug.
	Label string `json:"label"`
	// Enabled watches are scraped. Disabling keeps the watch and its events.
	Enabled bool `json:"enabled"`
	// AddedAt is when the watch was created, UTC.
	AddedAt string `json:"added_at"`
}

// ErrDuplicate is returned by Add for a slug that is already watched.
var ErrDuplicate = errors.New("hub is already watched")

const timestampLayout = "2006-01-02T15:04:05Z"

// Slug extracts the bare slug from a hub page URL, "/evento/{slug}" path or
// bare slug. It is lenient (any host, any path: the last segment wins), for
// reading TICKETLINE_HUB_PAGES. Hub pages are always fetched against
// ticketline.pt, so the host never matters there. Use Parse for input from
// a user.
func Slug(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		raw = u.Path
	}
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}

var slugShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// Parse validates a hub page given by a user and returns its slug. It
// accepts a ticketline.pt URL (https://www.ticketline.pt/evento/{slug}), a
// "/evento/{slug}" path or a bare slug, and rejects any other host, any
// other path, and a slug without a ticketline event ID (the scraper could
// never fetch it).
func Parse(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("a hub URL or slug is required")
	}
	path := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", errors.New("not a valid URL")
		}
		host := strings.ToLower(u.Hostname())
		if (u.Scheme != "https" && u.Scheme != "http") || (host != "www.ticketline.pt" && host != "ticketline.pt") {
			return "", errors.New("only ticketline.pt hub pages can be watched")
		}
		path = u.Path
	}
	path = strings.Trim(path, "/")
	slug := path
	if rest, ok := strings.CutPrefix(path, "evento/"); ok {
		slug = rest
	}
	if strings.Contains(slug, "/") || !slugShape.MatchString(slug) {
		return "", errors.New("expected a hub page like https://www.ticketline.pt/evento/<name>-<id>")
	}
	if parse.ExtractIDFromSlug(slug) == 0 {
		return "", errors.New("the hub slug must end in its ticketline.pt event ID, like <name>-98164")
	}
	return slug, nil
}

// List returns every watch, oldest first.
func List(ctx context.Context, db *sql.DB) ([]Watch, error) {
	rows, err := db.QueryContext(ctx, `SELECT slug, label, enabled, added_at FROM watches ORDER BY added_at, slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Watch{}
	for rows.Next() {
		var w Watch
		if err := rows.Scan(&w.Slug, &w.Label, &w.Enabled, &w.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Add inserts an enabled watch. It returns ErrDuplicate if slug exists.
func Add(ctx context.Context, db *sql.DB, slug, label string, now time.Time) (Watch, error) {
	existing, err := List(ctx, db)
	if err != nil {
		return Watch{}, err
	}
	// Two slugs with one ticketline id are the same hub (the run log is
	// keyed by id), whatever their spelling.
	id := parse.ExtractIDFromSlug(slug)
	for _, e := range existing {
		if strings.EqualFold(e.Slug, slug) || (id != 0 && parse.ExtractIDFromSlug(e.Slug) == id) {
			return Watch{}, ErrDuplicate
		}
	}

	w := Watch{Slug: slug, Label: label, Enabled: true, AddedAt: now.UTC().Format(timestampLayout)}
	res, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO watches (slug, label, enabled, added_at) VALUES (?, ?, 1, ?)`,
		w.Slug, w.Label, w.AddedAt)
	if err != nil {
		return Watch{}, err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return Watch{}, ErrDuplicate
	}
	return w, nil
}

// Update changes the enabled flag and/or the label (nil leaves it). It
// returns sql.ErrNoRows if slug is not a watch.
func Update(ctx context.Context, db *sql.DB, slug string, enabled *bool, label *string) (Watch, error) {
	if enabled != nil {
		if _, err := db.ExecContext(ctx, `UPDATE watches SET enabled = ? WHERE slug = ?`, *enabled, slug); err != nil {
			return Watch{}, err
		}
	}
	if label != nil {
		if _, err := db.ExecContext(ctx, `UPDATE watches SET label = ? WHERE slug = ?`, *label, slug); err != nil {
			return Watch{}, err
		}
	}
	var w Watch
	err := db.QueryRowContext(ctx, `SELECT slug, label, enabled, added_at FROM watches WHERE slug = ?`, slug).
		Scan(&w.Slug, &w.Label, &w.Enabled, &w.AddedAt)
	return w, err
}

// Disabled returns the slugs of disabled watches; nil without a table.
func Disabled(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	ws, err := List(ctx, db)
	if err != nil {
		if isNoTable(err) {
			return nil, nil
		}
		return nil, err
	}
	var off map[string]bool
	for _, w := range ws {
		if !w.Enabled {
			if off == nil {
				off = map[string]bool{}
			}
			off[w.Slug] = true
		}
	}
	return off, nil
}

func isNoTable(err error) bool { return strings.Contains(err.Error(), "no such table") }

// Resolve returns the hub slugs to scrape this run: the enabled watches,
// plus any hub of seed (TICKETLINE_HUB_PAGES) that is not a watch yet.
//
// The seed is inserted as watches (INSERT OR IGNORE semantics, so a retry or
// a concurrent run does nothing twice), so the dashboard lists the hubs
// already being scraped. It is applied on every run, not only to an empty
// table: a watch added on the dashboard before the first seed must not drop
// the configured hubs. A seeded hub that was disabled stays disabled, and
// a hub that cannot be inserted is still scraped (it is not known to be
// disabled). Invalid seed entries (no event ID) are returned unchanged and
// not inserted, so the run fails on them as it always has.
//
// Without the table (web-ui-api has not migrated yet) the result is seed.
// A database error is returned rather than falling back to seed, which
// could scrape hubs that were disabled. The result may be empty; the caller
// decides that is an error.
func Resolve(ctx context.Context, db *sql.DB, seed []string, now time.Time) ([]string, error) {
	ws, err := List(ctx, db)
	if err != nil {
		if isNoTable(err) {
			log.Printf("No watches table yet; using TICKETLINE_HUB_PAGES (%d hub(s))", len(seed))
			return seed, nil
		}
		return nil, fmt.Errorf("read watches: %w", err)
	}

	known := map[string]bool{}
	var out []string
	for _, w := range ws {
		known[w.Slug] = true
		if w.Enabled {
			out = append(out, w.Slug)
		}
	}

	var added int
	for _, slug := range seed {
		if known[slug] {
			continue
		}
		known[slug] = true
		out = append(out, slug)
		if _, err := Parse(slug); err != nil {
			continue
		}
		switch _, err := Add(ctx, db, slug, "", now); {
		case err == nil:
			added++
		case errors.Is(err, ErrDuplicate):
			// The same hub under another spelling is already a watch;
			// if it was disabled, do not scrape it through the seed.
			out = out[:len(out)-1]
		default:
			log.Printf("WARN could not seed watch %s: %v", slug, err)
		}
	}
	if added > 0 {
		log.Printf("Seeded %d watch(es) from TICKETLINE_HUB_PAGES", added)
	}
	return out, nil
}
