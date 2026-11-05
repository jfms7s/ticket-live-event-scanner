package discovery

import (
	"context"
	"fmt"
	"log"
)

// LoadKnownIDs populates the scraper's in-memory set of known event IDs, so
// discovery can skip sessions it has already queued, and the set of
// recently rejected ones.
//
// The events table is the record of what was announced: a session is
// stored in the same transaction that queues its notification, so a stored
// event (hidden ones included, the dashboard's Remove only hides) is a
// session that was queued, and there is no second list to disagree with it.
func (s *Scraper) LoadKnownIDs(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM events")
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan event id: %w", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}
	s.knownIDs = ids
	log.Printf("Loaded %d known event IDs", len(s.knownIDs))

	// The skipped set only saves requests, so a table that can't be read
	// means re-fetching those sessions, not failing the run.
	skipped, err := s.skipped.Load(ctx)
	if err != nil {
		log.Printf("WARN %v; rejected sessions will be fetched again", err)
		skipped = map[int64]bool{}
	}
	s.skipIDs = skipped
	if len(skipped) > 0 {
		log.Printf("Loaded %d recently rejected event IDs", len(skipped))
	}
	return nil
}
