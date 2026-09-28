package store

import (
	"fmt"
	"strings"
	"time"
)

// Schema ownership (docs/reviews/2026-08-26-structure.md §2): every package
// declares its own tables and columns in one ordered `Schema []string` and
// hands it to Migrate from its constructor. Migrate runs each statement it
// has not recorded in `schema_migrations` and records it, so adding a column
// is one appended ALTER — nothing else to touch, and `make check` proves a
// fresh database ends up with the same tables and indices as the live one
// (server_test TestFreshSchema).
//
// Statements are keyed by their text, per package: order inside the list is
// the order they run on a fresh database, and a statement edited in place is
// simply a new one. Databases from before this migrator carry every column
// already, unrecorded — a "duplicate column" / "already exists" error is
// therefore recorded as applied, not raised. Anything else is an error.
const migrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	pkg TEXT NOT NULL, stmt TEXT NOT NULL, applied_at TEXT NOT NULL,
	PRIMARY KEY (pkg, stmt))`

// Migrate applies pkg's statements that are not yet recorded. It returns the
// ones that ran for real — not the ones tolerated as already present — so a
// caller can pair a one-time backfill with the column it introduced.
func (db *DB) Migrate(pkg string, stmts []string) ([]string, error) {
	if _, err := db.Exec(migrationsTable); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	var fresh []string
	for i, stmt := range stmts {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE pkg=? AND stmt=?`, pkg, stmt).Scan(&n); err != nil {
			return fresh, err
		}
		if n > 0 {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			if !alreadyApplied(err) {
				return fresh, fmt.Errorf("%s schema %d: %w", pkg, i, err)
			}
		} else {
			fresh = append(fresh, stmt)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (pkg, stmt, applied_at) VALUES (?,?,?)`, pkg, stmt, TS(time.Now())); err != nil {
			return fresh, err
		}
	}
	return fresh, nil
}

func alreadyApplied(err error) bool {
	s := err.Error()
	return strings.Contains(s, "duplicate column") || strings.Contains(s, "already exists")
}
