package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// LoadKnownIDs populates the scraper's in-memory set of seen event IDs, so
// discovery can skip events it has already published.
func (s *Scraper) LoadKnownIDs(ctx context.Context) error {
	ids, err := s.seen.Load(ctx)
	if err != nil {
		return err
	}
	s.knownIDs = ids
	log.Printf("Loaded %d known event IDs", len(s.knownIDs))
	return nil
}

// dbSeenStore reads seen IDs from web-ui-api's events table.
type dbSeenStore struct {
	db *sql.DB
}

func (d dbSeenStore) Load(ctx context.Context) (map[int64]bool, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id FROM events")
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan event id: %w", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return ids, nil
}

// Mark is a no-op: web-ui-api writes the events table when it consumes the
// published message.
func (dbSeenStore) Mark(context.Context, int64) error { return nil }
