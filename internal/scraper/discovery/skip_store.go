package discovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
)

// errSkip marks a per-session data problem (an unparseable date, a missing
// title, URL or slug). Such a session is skipped with a warning and does
// not fail the run: one bad listing must not turn an hourly scrape into
// three (the CronJob retries a failed run). Transport and structural
// problems still fail the run.
var errSkip = errors.New("skipped")

// skipTTL is how long a rejected session is left alone before it is fetched
// and parsed again.
const skipTTL = 24 * time.Hour

// skipStore remembers sessions the parser rejected, so later runs don't
// fetch them every hour. Entries expire after skipTTL.
type skipStore interface {
	// Load returns the IDs of sessions rejected recently.
	Load(ctx context.Context) (map[int64]bool, error)
	// Mark records that id was rejected, and why.
	Mark(ctx context.Context, id int64, reason string) error
}

// tableSkipStore keeps the skipped set in the skipped_sessions table.
type tableSkipStore struct {
	db  *sql.DB
	now func() time.Time
}

func newSkipStore(db *sql.DB) skipStore { return tableSkipStore{db: db, now: time.Now} }

const skipTimeLayout = "2006-01-02T15:04:05Z"

func (t tableSkipStore) Load(ctx context.Context) (map[int64]bool, error) {
	rows, err := t.db.QueryContext(ctx, `SELECT event_id FROM skipped_sessions WHERE until > ?`,
		t.now().UTC().Format(skipTimeLayout))
	if err != nil {
		return nil, fmt.Errorf("skipped sessions: %w", err)
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("skipped sessions: %w", err)
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (t tableSkipStore) Mark(ctx context.Context, id int64, reason string) error {
	until := t.now().UTC().Add(skipTTL).Format(skipTimeLayout)
	if _, err := t.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO skipped_sessions (event_id, reason, until) VALUES (?, ?, ?)`, id, reason, until); err != nil {
		return fmt.Errorf("mark event %d skipped: %w", id, err)
	}
	return nil
}

// skip records a rejected session: counted, logged and remembered. A
// failure to remember only costs a re-fetch next run, so it is logged and
// not returned.
func (s *Scraper) skip(ctx context.Context, id int64, err error) {
	s.Stats.Skipped++
	s.skipIDs[id] = true
	log.Printf("WARN skipping event %d: %v", id, err)
	if markErr := s.skipped.Mark(ctx, id, err.Error()); markErr != nil {
		log.Printf("WARN %v; it will be fetched again next run", markErr)
	}
}
