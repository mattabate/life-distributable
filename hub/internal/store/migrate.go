package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	var recorded int
	db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE pkg=?`, pkg).Scan(&recorded)
	snapped := recorded == 0 // a package's first run only creates its tables
	for i, stmt := range stmts {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE pkg=? AND stmt=?`, pkg, stmt).Scan(&n); err != nil {
			return fresh, err
		}
		if n > 0 {
			continue
		}
		if !snapped {
			snapped = true
			db.preMigrateSnapshot(pkg)
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

// preMigrateFile: the copy taken before an existing package's new
// statements run, in a snapshots/ directory next to the database.
const preMigrateFile = "pre-migrate.db"

// preMigrateKeep: a snapshot younger than this is an unfinished upgrade's —
// a hub in a crash loop restarts within seconds — and is kept.
const preMigrateKeep = 15 * time.Minute

// preMigrated: the open databases that took their snapshot this process.
var preMigrated sync.Map

// preMigrateSnapshot: a bad backfill UPDATE in a new statement could only be
// undone from the last nightly backup, up to a day of writes behind. So
// before an already-migrated package runs anything new, the database is
// copied with VACUUM INTO (never a file copy: WAL), one file replaced by the
// next upgrade. A failure is logged and the migration goes ahead.
//
// Once per process, and never over a snapshot from the last few minutes
// (#79): each package's Migrate used to retake it, so the copy held the
// first packages' new statements, and a crash loop overwrote the good copy
// with a half-migrated one. The copy lands under a temporary name and is
// renamed, so a failed VACUUM never removes the last one.
func (db *DB) preMigrateSnapshot(pkg string) {
	if _, done := preMigrated.LoadOrStore(db.DB, true); done {
		return
	}
	var seq int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil || path == "" {
		return // in-memory
	}
	dst := filepath.Join(filepath.Dir(path), "snapshots", preMigrateFile)
	if fi, err := os.Stat(dst); err == nil && time.Since(fi.ModTime()) < preMigrateKeep {
		log.Printf("store: %s has new statements; keeping %s from %s (an unfinished upgrade's)", pkg, dst, fi.ModTime().Format(time.RFC3339))
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Printf("store: pre-migrate snapshot (%s): %v", pkg, err)
		return
	}
	tmp := dst + ".tmp"
	os.Remove(tmp)
	start := time.Now()
	if _, err := db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		os.Remove(tmp)
		log.Printf("store: pre-migrate snapshot (%s): %v", pkg, err)
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		log.Printf("store: pre-migrate snapshot (%s): %v", pkg, err)
		return
	}
	log.Printf("store: %s has new statements; snapshot %s (%s)", pkg, dst, time.Since(start).Round(time.Millisecond))
}

func alreadyApplied(err error) bool {
	s := err.Error()
	return strings.Contains(s, "duplicate column") || strings.Contains(s, "already exists")
}
