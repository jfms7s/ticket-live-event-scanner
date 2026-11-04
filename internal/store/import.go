package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Options say where the database is. With Path set it is the SQLite file at
// Path; if that file is new and LegacyURL is set, its contents are copied
// once from the libSQL server (sqld) there (see Open). Without Path the
// database is the libSQL server at LegacyURL, as before.
type Options struct {
	Path        string
	LegacyURL   string
	LegacyToken string
	// AllowEmpty lets the process start on a SQLite file that is new and
	// empty with no legacy server to fill it from: a fresh install. Without
	// it that start is refused, because it is also what a missing or
	// misnamed Secret looks like, and a scraper on an empty database finds
	// every session "new" and notifies them all again.
	AllowEmpty bool
}

// importTimeout bounds filling the file from sqld, so a hung server stops
// the start with an error instead of holding it (and the write lock) open.
var importTimeout = 3 * time.Minute

// Open opens the database, creates or upgrades its schema and, for a new
// SQLite file with a legacy server configured, imports the server's data.
func Open(ctx context.Context, o Options) (*sql.DB, error) {
	if o.Path == "" {
		db, err := Connect(o.LegacyURL, o.LegacyToken)
		if err != nil {
			return nil, err
		}
		if err := Migrate(ctx, db); err != nil {
			db.Close()
			return nil, err
		}
		return db, nil
	}

	db, err := OpenSQLite(ctx, o.Path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if err := fillOrRefuse(ctx, o, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// fillOrRefuse decides what a SQLite file with no import marker needs: the
// import from sqld, nothing (it already has data), or a refusal (it is empty
// and nothing says that is intended).
func fillOrRefuse(ctx context.Context, o Options, db *sql.DB) error {
	var marked int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meta WHERE key = ?`, importedKey).Scan(&marked); err != nil {
		return fmt.Errorf("check import marker: %w", err)
	}
	if marked > 0 {
		return nil
	}

	if o.LegacyURL != "" {
		// The copy runs on its own handle with foreign keys off: it copies
		// the server's rows as they are, and a pooled connection would
		// enforce keys the server may not have.
		imp, err := openSQLiteDB(ctx, o.Path, false)
		if err != nil {
			return err
		}
		defer imp.Close()
		ictx, cancel := context.WithTimeout(ctx, importTimeout)
		defer cancel()
		return importLegacyOnce(ictx, o, imp)
	}

	empty, err := isEmpty(ctx, db)
	if err != nil {
		return err
	}
	if empty && !o.AllowEmpty {
		return ErrEmptyDatabase
	}
	return nil
}

// ErrEmptyDatabase is returned when the SQLite file is empty, was never
// imported into and nothing allows starting empty.
var ErrEmptyDatabase = errors.New("the SQLite database is empty and no sqld to import from is configured (TURSO_DATABASE_URL, TURSO_AUTH_TOKEN): check the Secret; set DATABASE_ALLOW_EMPTY=true only for a fresh install")

// importedKey is the meta row that says the file was filled from sqld. It is
// written in the transaction that copies the data, so it exists exactly when
// the copy is complete.
const importedKey = "imported_from_libsql"

// importTables are copied in this order (events first: notifications refer
// to them).
var importTables = []struct {
	name string
	pk   string
}{
	{"events", "id"},
	{"notifications", "id"},
	{"watches", "slug"},
}

// connectLegacy opens the sqld; tests replace it.
var connectLegacy = Connect

// ErrNotEmpty means a new-looking database already has data but was never
// imported into, so importing now could mix two histories.
var ErrNotEmpty = errors.New("the SQLite file already has data but was not imported from sqld; refusing to import over it")

// importLegacyOnce copies the legacy server's tables into dst when dst has
// never been imported into and is empty. It fails (the process must not
// start) when the server cannot be read or the copy does not verify: an
// empty database next to a scraper that finds every session "new" would
// notify them all again.
func importLegacyOnce(ctx context.Context, o Options, dst *sql.DB) error {
	var done int
	if err := dst.QueryRowContext(ctx, `SELECT COUNT(*) FROM meta WHERE key = ?`, importedKey).Scan(&done); err != nil {
		return fmt.Errorf("check import marker: %w", err)
	}
	if done > 0 {
		return nil
	}

	empty, err := isEmpty(ctx, dst)
	if err != nil {
		return err
	}
	if !empty {
		return ErrNotEmpty
	}

	src, err := connectLegacy(o.LegacyURL, o.LegacyToken)
	if err != nil {
		return fmt.Errorf("import from sqld: %w", err)
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return fmt.Errorf("import from sqld: server unreachable: %w", err)
	}

	summary, err := copyAll(ctx, src, dst)
	if err != nil {
		return fmt.Errorf("import from sqld: %w", err)
	}
	log.Printf("Imported the database from sqld into SQLite: %s", summary)
	log.Printf("WARN the SQLite file is now the database. Remove TURSO_DATABASE_URL and TURSO_AUTH_TOKEN from this deployment: if the volume were ever lost with them still set, the next start would copy the sqld's frozen snapshot over newer state.")
	return nil
}

func isEmpty(ctx context.Context, db *sql.DB) (bool, error) {
	for _, t := range importTables {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t.name).Scan(&n); err != nil {
			return false, fmt.Errorf("count %s: %w", t.name, err)
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}

// copyAll copies every table in one transaction on dst, checks the copy
// against what was read (row count and a hash of every value) before it
// commits, and records the marker in the same transaction. Verification is
// of every value as text (CAST AS TEXT): a REAL or BLOB that the text round
// trip would change is not detected, and this schema has none.
func copyAll(ctx context.Context, src, dst *sql.DB) (string, error) {
	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	// The copy is faithful, including rows that break a foreign key (the
	// caller's handle does not enforce them); the check after is reported.
	var parts []string
	for _, t := range importTables {
		cols, err := sharedColumns(ctx, src, tx, t.name)
		if errors.Is(err, errNoSourceTable) {
			parts = append(parts, t.name+"=absent")
			continue
		}
		if err != nil {
			return "", err
		}
		n, srcSum, err := copyTable(ctx, src, tx, t.name, t.pk, cols)
		if err != nil {
			return "", fmt.Errorf("copy %s: %w", t.name, err)
		}
		gotN, gotSum, err := tableDigest(ctx, tx, t.name, t.pk, cols)
		if err != nil {
			return "", fmt.Errorf("verify %s: %w", t.name, err)
		}
		if gotN != n || gotSum != srcSum {
			return "", fmt.Errorf("verify %s: copied %d rows (%s), read %d rows (%s)", t.name, gotN, gotSum[:12], n, srcSum[:12])
		}
		parts = append(parts, fmt.Sprintf("%s=%d (sha256 %s)", t.name, n, srcSum[:12]))
	}

	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return "", fmt.Errorf("foreign key check: %w", err)
	}
	orphans := 0
	for rows.Next() {
		orphans++
	}
	rows.Close()
	if orphans > 0 {
		log.Printf("WARN the imported data has %d row(s) that break a foreign key (copied as they were)", orphans)
	}

	summary := strings.Join(parts, ", ")
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, importedKey, summary); err != nil {
		return "", fmt.Errorf("write import marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return summary, nil
}

var errNoSourceTable = errors.New("no such table in the source")

// sharedColumns lists the columns table has in both databases, in the
// destination's order. A column only the destination has takes its default;
// one only the source has is not copied (and not verified).
func sharedColumns(ctx context.Context, src *sql.DB, dst interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string) ([]string, error) {
	srcList, err := tableColumns(ctx, src, table)
	if err != nil {
		return nil, err
	}
	if len(srcList) == 0 {
		return nil, errNoSourceTable
	}
	srcCols := map[string]bool{}
	for _, c := range srcList {
		srcCols[c] = true
	}
	dstCols, err := tableColumns(ctx, dst, table)
	if err != nil {
		return nil, err
	}
	var shared []string
	for _, c := range dstCols {
		if srcCols[c] {
			shared = append(shared, c)
		}
	}
	if len(shared) == 0 {
		return nil, fmt.Errorf("table %s shares no column with the source", table)
	}
	return shared, nil
}

// tableColumns returns the column names of table in order; empty for no such
// table.
func tableColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, fmt.Errorf("columns of %s: %w", table, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("columns of %s: %w", table, err)
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// selectText reads every value as text (NULL stays NULL), so both drivers
// give the same thing whatever the declared column types are.
func selectText(table, pk string, cols []string) string {
	casts := make([]string, len(cols))
	for i, c := range cols {
		casts[i] = "CAST(" + c + " AS TEXT)"
	}
	return "SELECT " + strings.Join(casts, ", ") + " FROM " + table + " ORDER BY " + pk
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// copyTable inserts every row of src's table into tx and returns the row
// count and the hash of what it read.
func copyTable(ctx context.Context, src *sql.DB, tx *sql.Tx, table, pk string, cols []string) (int, string, error) {
	rows, err := src.QueryContext(ctx, selectText(table, pk, cols))
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()

	insert, err := tx.PrepareContext(ctx, "INSERT INTO "+table+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")+")")
	if err != nil {
		return 0, "", err
	}
	defer insert.Close()

	h := sha256.New()
	n := 0
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return 0, "", err
		}
		args := make([]any, len(cols))
		for i, v := range vals {
			if v.Valid {
				args[i] = v.String
			}
		}
		if _, err := insert.ExecContext(ctx, args...); err != nil {
			return 0, "", fmt.Errorf("row %d: %w", n+1, err)
		}
		hashRow(h, vals)
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// tableDigest returns the row count and hash of the same text view of
// table, as copyTable computes it for the source.
func tableDigest(ctx context.Context, q queryer, table, pk string, cols []string) (int, string, error) {
	rows, err := q.QueryContext(ctx, selectText(table, pk, cols))
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	h := sha256.New()
	n := 0
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return 0, "", err
		}
		hashRow(h, vals)
		n++
	}
	return n, hex.EncodeToString(h.Sum(nil)), rows.Err()
}

// hashRow feeds a row into h with lengths and NULL markers, so no two
// different rows hash alike by concatenation accident.
func hashRow(h interface{ Write([]byte) (int, error) }, vals []sql.NullString) {
	for _, v := range vals {
		if !v.Valid {
			h.Write([]byte("N;"))
			continue
		}
		fmt.Fprintf(h, "S%d:%s;", len(v.String), v.String)
	}
	h.Write([]byte("\n"))
}
