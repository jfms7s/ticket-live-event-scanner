package discovery

import (
	"context"
	"fmt"
	"log"
)

// LoadKnownIDs populates the scraper's in-memory set of known event IDs
// from the database, so discovery can skip events it has already seen.
func (s *Scraper) LoadKnownIDs(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM events")
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan event id: %w", err)
		}
		s.knownIDs[id] = true
	}

	// Check for errors BEFORE logging success
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}

	log.Printf("Loaded %d known event IDs from database", len(s.knownIDs))
	return nil
}
