package store

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// The hub keeps ONE SQLite connection (SetMaxOpenConns(1)), which makes
// writes serial and the code simple. The cost is that an open `*sql.Rows`
// IS the connection: any code that issues a second query while still
// iterating the first deadlocks against itself, and because the pool never
// times out, it deadlocks FOREVER — every later request queues behind it and
// the whole hub stops answering while still passing /healthz (which touches
// no DB) and still showing `state = running` to launchd.
//
// That happened on 2026-08-29: people.enrichProfiles wrote an observation
// inside its own row loop, and the hub sat wedged with 99 goroutines parked
// on database/sql.(*DB).conn until it was killed by hand. The bug is fixed,
// but it is a whole CLASS of bug — every `.Query(` site is one careless edit
// away from it — so this watchdog bounds the damage.
//
// It proves liveness the only way that cannot lie: it asks the pool for a
// connection like any other caller. A healthy pool always hands one back
// between queries. If it cannot within probeTimeout, several times running,
// the pool is wedged — so dump every goroutine (the stack naming the culprit
// is the whole diagnosis) and exit, letting launchd restart a working hub.
// A permanent silent outage becomes a ~10-second one that explains itself.
type Watchdog struct {
	DB *DB
	// ProbeEvery: how often to ask the pool for a connection.
	ProbeEvery time.Duration
	// ProbeTimeout: how long one probe waits before it counts as a strike.
	ProbeTimeout time.Duration
	// Strikes before acting. Generous on purpose: a legitimate long
	// transaction (the people rebuild writes thousands of rows in one) must
	// never be mistaken for a deadlock.
	Strikes int
	// DumpDir receives db-wedge-<timestamp>.txt.
	DumpDir string
	// Exit is called after the dump. Swappable for tests; nil = os.Exit(1).
	Exit func()
	// Now is swappable for tests.
	Now func() time.Time
}

func NewWatchdog(db *DB, dumpDir string) *Watchdog {
	return &Watchdog{DB: db, ProbeEvery: 30 * time.Second, ProbeTimeout: 60 * time.Second,
		Strikes: 3, DumpDir: dumpDir, Now: time.Now}
}

// Start runs the watchdog until ctx is done.
func (w *Watchdog) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(w.ProbeEvery)
		defer t.Stop()
		strikes := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if w.probe(ctx) {
					if strikes > 0 {
						log.Printf("db watchdog: pool recovered after %d strike(s)", strikes)
					}
					strikes = 0
					continue
				}
				strikes++
				st := w.DB.Stats()
				log.Printf("db watchdog: STRIKE %d/%d — no connection within %s (in use %d, waiting %d, total waited %s)",
					strikes, w.Strikes, w.ProbeTimeout, st.InUse, st.WaitCount, st.WaitDuration.Round(time.Second))
				if strikes >= w.Strikes {
					w.trip()
					return
				}
			}
		}
	}()
}

// probe reports whether the pool handed back a connection in time.
func (w *Watchdog) probe(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, w.ProbeTimeout)
	defer cancel()
	var one int
	// Deliberately the pool's own path (not a cached conn): this is the same
	// queue every handler waits in.
	return w.DB.QueryRowContext(c, `SELECT 1`).Scan(&one) == nil && one == 1
}

// trip records why the hub was stuck, then ends the process.
func (w *Watchdog) trip() {
	st := w.DB.Stats()
	path := w.dump(fmt.Sprintf("db pool wedged: in use %d, waiting %d, total waited %s\n\n",
		st.InUse, st.WaitCount, st.WaitDuration.Round(time.Second)))
	log.Printf("db watchdog: POOL WEDGED — a query is holding the only connection and never released it "+
		"(almost always a second query issued while iterating rows). Goroutine dump: %s. Exiting so launchd restarts.", path)
	if w.Exit != nil {
		w.Exit()
		return
	}
	os.Exit(1)
}

// dump writes every goroutine stack; the one NOT parked in database/sql
// .(*DB).conn is the code that holds the connection.
func (w *Watchdog) dump(header string) string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	if w.DumpDir == "" {
		log.Print(header + string(buf))
		return "(stderr)"
	}
	_ = os.MkdirAll(w.DumpDir, 0o755)
	path := filepath.Join(w.DumpDir, "db-wedge-"+w.Now().Format("20060102-150405")+".txt")
	if err := os.WriteFile(path, append([]byte(header), buf...), 0o644); err != nil {
		log.Printf("db watchdog: could not write dump: %v", err)
		log.Print(header + string(buf))
		return "(stderr)"
	}
	return path
}
