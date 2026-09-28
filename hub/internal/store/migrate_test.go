package store

import (
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

	fresh, err := db.Migrate("widgets", []string{create})
	if err != nil || len(fresh) != 1 {
		t.Fatalf("first: fresh=%v err=%v", fresh, err)
	}
	// Appending a statement runs only that one.
	fresh, err = db.Migrate("widgets", []string{create, addB})
	if err != nil || len(fresh) != 1 || fresh[0] != addB {
		t.Fatalf("append: fresh=%v err=%v", fresh, err)
	}
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
