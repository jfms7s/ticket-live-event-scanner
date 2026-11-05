package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Options say where the database is.
type Options struct {
	// Path is the SQLite file (created if need be).
	Path string
	// AllowEmpty lets the process start on a file that is new and empty: a
	// fresh install. Without it that start is refused, because it is also
	// what a lost or wrong volume looks like, and a scraper on an empty
	// database finds every session "new" and notifies them all again.
	AllowEmpty bool
}

// ErrEmptyDatabase is returned when the SQLite file is empty and nothing
// allows starting empty.
var ErrEmptyDatabase = errors.New("the SQLite database is empty: check that the volume holding it is mounted; set DATABASE_ALLOW_EMPTY=true only for a fresh install")

// Open opens the SQLite database at o.Path, creates or upgrades its schema,
// and refuses an empty one unless o.AllowEmpty.
func Open(ctx context.Context, o Options) (*sql.DB, error) {
	db, err := OpenSQLite(ctx, o.Path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if !o.AllowEmpty {
		empty, err := isEmpty(ctx, db)
		if err != nil {
			db.Close()
			return nil, err
		}
		if empty {
			db.Close()
			return nil, ErrEmptyDatabase
		}
	}
	return db, nil
}

// isEmpty reports whether the tables that hold the application's state have
// no rows.
func isEmpty(ctx context.Context, db *sql.DB) (bool, error) {
	for _, table := range []string{"events", "notifications", "watches"} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			return false, fmt.Errorf("count %s: %w", table, err)
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}
