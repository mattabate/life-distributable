package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real bug, reproduced: hold an open cursor and issue a second query.
// With one connection that is a permanent self-deadlock, and before the
// watchdog existed it took the whole hub down until someone killed it.
func TestWatchdogCatchesNestedQueryDeadlock(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER); INSERT INTO t VALUES (1),(2)`); err != nil {
		t.Fatal(err)
	}

	tripped := make(chan struct{})
	w := NewWatchdog(db, dir)
	w.ProbeEvery, w.ProbeTimeout, w.Strikes = 40*time.Millisecond, 120*time.Millisecond, 2
	w.Exit = func() { close(tripped) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Healthy first: the watchdog must not trip on a working pool.
	select {
	case <-tripped:
		t.Fatal("watchdog tripped on a healthy pool")
	case <-time.After(300 * time.Millisecond):
	}

	// Now wedge it exactly as enrichProfiles did.
	rows, err := db.Query(`SELECT id FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next() // cursor open == the only connection is checked out
	go func() {
		// This is the line that deadlocks; it never returns.
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n)
	}()

	select {
	case <-tripped:
	case <-time.After(4 * time.Second):
		t.Fatal("watchdog did not trip on a wedged pool")
	}

	// It must leave behind the diagnosis, naming the stuck call.
	ents, _ := os.ReadDir(dir)
	var dump string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "db-wedge-") {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			dump = string(b)
		}
	}
	if dump == "" {
		t.Fatal("no db-wedge dump written")
	}
	if !strings.Contains(dump, "db pool wedged") || !strings.Contains(dump, "database/sql") {
		t.Fatalf("dump missing the evidence: %.200s", dump)
	}
}

// A slow-but-progressing pool must not be killed: the watchdog exists to end
// deadlocks, not to punish a long transaction.
func TestWatchdogToleratesSlowQueries(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tripped := make(chan struct{})
	w := NewWatchdog(db, dir)
	w.ProbeEvery, w.ProbeTimeout, w.Strikes = 30*time.Millisecond, 500*time.Millisecond, 3
	w.Exit = func() { close(tripped) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	// Busy but never deadlocked: each query returns the connection.
	deadline := time.After(1200 * time.Millisecond)
	for {
		select {
		case <-tripped:
			t.Fatal("watchdog tripped on a busy but healthy pool")
		case <-deadline:
			return
		default:
			var n int
			if err := db.QueryRow(`SELECT 1`).Scan(&n); err != nil {
				t.Fatal(err)
			}
		}
	}
}
