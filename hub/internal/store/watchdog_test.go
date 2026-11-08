package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The class of bug, reproduced: hold the writer's only connection and issue
// a second write. That is a permanent self-deadlock, and before the
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

	// Now wedge the writer: a transaction left open holds its only
	// connection, and a write outside it waits forever. (A nested read no
	// longer can: reads have their own pool.)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	go func() {
		// This is the line that deadlocks; it never returns.
		_, _ = db.Exec(`INSERT INTO t VALUES (3)`)
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

// Reads never queue behind the writer: with a write transaction open, a read
// still answers at once, and with the last committed state; a read with a
// cursor open still lets another read and a write through.
func TestReadsDoNotWaitForTheWriter(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER); INSERT INTO t VALUES (1),(2)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO t VALUES (3)`); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n)
		done <- n
	}()
	select {
	case n := <-done:
		if n != 2 {
			t.Fatalf("read saw %d rows mid-transaction, want the committed 2", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a read waited for the writer's transaction")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("read beside an open cursor: n=%d err=%v", n, err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (4)`); err != nil {
		t.Fatalf("write beside an open cursor: %v", err)
	}
	if _, err := db.Query(`INSERT INTO t VALUES (5)`); err == nil {
		t.Fatal("a write sent through Query ran; the readers are query_only")
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
