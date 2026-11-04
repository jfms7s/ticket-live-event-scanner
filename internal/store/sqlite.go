package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

// sqliteDSN is the connection string for the database file at path:
//   - WAL, so the readers (HTTP handlers) do not block the writer and a
//     second process (web-ui-api --scrape-once) can open the same file;
//   - busy_timeout, so two writers wait for each other instead of failing;
//   - foreign keys on, as sqld may enforce them and the code is written
//     for it;
//   - immediate transactions, so a write transaction takes the write lock up
//     front (and waits for it) instead of failing when it upgrades from a
//     read lock.
func sqliteDSN(path string, foreignKeys bool) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	if foreignKeys {
		q.Add("_pragma", "foreign_keys(1)")
	}
	q.Set("_txlock", "immediate")
	return "file:" + path + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

// OpenSQLite opens (creating it if need be) the SQLite database at path.
func OpenSQLite(ctx context.Context, path string) (*sql.DB, error) {
	return openSQLiteDB(ctx, path, true)
}

func openSQLiteDB(ctx context.Context, path string, foreignKeys bool) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("open sqlite: empty path")
	}
	db, err := sql.Open("sqlite", sqliteDSN(path, foreignKeys))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// A handful of connections: reads run beside the one writer (WAL).
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return db, nil
}
