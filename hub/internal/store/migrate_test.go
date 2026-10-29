package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Migrate runs what it has not recorded, records it, tolerates a column or
// index that an older database already carries, and fails loudly on anything
// else — so a package's Schema is append-only and a fresh database and the
// live one converge on the same shape.
func TestMigrateRecordsAndTolerates(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	create := `CREATE TABLE IF NOT EXISTS widgets (id TEXT PRIMARY KEY)`
	addB := `ALTER TABLE widgets ADD COLUMN b TEXT NOT NULL DEFAULT ''`
	addC := `ALTER TABLE widgets ADD COLUMN c INTEGER NOT NULL DEFAULT 0`

	snap := filepath.Join(filepath.Dir(dbPath(t, db)), "snapshots", preMigrateFile)
	fresh, err := db.Migrate("widgets", []string{create})
	if err != nil || len(fresh) != 1 {
		t.Fatalf("first: fresh=%v err=%v", fresh, err)
	}
	if _, err := os.Stat(snap); err == nil {
		t.Fatal("a package's first run (new tables only) took a snapshot")
	}
	db.Exec(`INSERT INTO widgets (id) VALUES ('w1')`)
	// Appending a statement runs only that one, after a copy of the database
	// as it was, the row in it.
	fresh, err = db.Migrate("widgets", []string{create, addB})
	if err != nil || len(fresh) != 1 || fresh[0] != addB {
		t.Fatalf("append: fresh=%v err=%v", fresh, err)
	}
	old, err := Open(snap)
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	old.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&rows)
	if _, err := old.Exec(`SELECT b FROM widgets`); err == nil || rows != 1 {
		t.Fatalf("snapshot: %d rows, column b err %v; want the pre-migrate shape with 1 row", rows, err)
	}
	old.Close()
	// A column the database already has (added by hand, or by the pre-migrator
	// code) is recorded as applied without being counted as fresh.
	if _, err := db.Exec(addC); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.Migrate("widgets", []string{create, addB, addC})
	if err != nil || len(fresh) != 0 {
		t.Fatalf("tolerate: fresh=%v err=%v", fresh, err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE pkg='widgets'`).Scan(&n)
	if n != 3 {
		t.Fatalf("recorded %d, want 3", n)
	}
	// Anything else is an error that names the package and the statement.
	if _, err := db.Migrate("widgets", []string{create, addB, addC, `ALTER TABLE nope ADD COLUMN x`}); err == nil || !strings.Contains(err.Error(), "widgets schema 3") {
		t.Fatalf("bad statement: %v", err)
	}
}

func dbPath(t *testing.T, db *DB) string {
	var seq int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}
