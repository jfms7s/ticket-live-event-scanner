package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Retention is how long a record is kept: a week of hourly runs plus the
// retries, enough for the weekly digest.
const Retention = 8 * 24 * time.Hour

const timestampLayout = "2006-01-02T15:04:05Z"

// SQLStore keeps the run log in the scraper_runs table. It is a Sink for
// Write and a KV for Reader.
type SQLStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s SQLStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Put stores the record and drops the ones older than Retention.
func (s SQLStore) Put(ctx context.Context, key string, value []byte) error {
	now := s.now().UTC()
	if _, err := s.DB.ExecContext(ctx,
		`INSERT OR REPLACE INTO scraper_runs (key, value, created_at) VALUES (?, ?, ?)`,
		key, string(value), now.Format(timestampLayout)); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM scraper_runs WHERE created_at < ?`,
		now.Add(-Retention).Format(timestampLayout)); err != nil {
		return fmt.Errorf("expire old run records: %w", err)
	}
	return nil
}

// Keys lists the keys of the records still within Retention.
func (s SQLStore) Keys(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key FROM scraper_runs WHERE created_at >= ?`,
		s.now().UTC().Add(-Retention).Format(timestampLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Get returns the record at key.
func (s SQLStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM scraper_runs WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(v), true, nil
}
