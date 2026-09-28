// Package syncruns is the one run log every syncer writes (review 2026-09-26
// §3): a row per clock run of a task registered with clock.Sync — when it
// started, when it ended, whether it worked and what it said if not. It is
// written by one hook in main.go (clock.Done), never by a syncer itself, so a
// new connector gets its "last run / failing since" on the Sources page by
// being registered, with no status table of its own.
//
// The older per-connector tables (simplefin_sync, audience_sync) stay: they
// carry what this log cannot — SimpleFin's since= window, one row per
// audience connector inside the one "audience" task.
package syncruns

import (
	"database/sql"
	"time"

	"life/hub/internal/store"
)

var Schema = []string{
	`CREATE TABLE IF NOT EXISTS sync_runs (id INTEGER PRIMARY KEY, name TEXT NOT NULL, started TEXT NOT NULL, ended TEXT NOT NULL, ok INTEGER NOT NULL, error TEXT NOT NULL DEFAULT '');`,
	`CREATE INDEX IF NOT EXISTS sync_runs_name ON sync_runs(name, id);`,
}

type Log struct{ db *store.DB }

func New(db *store.DB) (*Log, error) {
	if _, err := db.Migrate("syncruns", Schema); err != nil {
		return nil, err
	}
	return &Log{db: db}, nil
}

// Record appends one run. err nil = the run worked.
func (l *Log) Record(name string, start, end time.Time, err error) error {
	ok, msg := 1, ""
	if err != nil {
		ok, msg = 0, err.Error()
	}
	_, e := l.db.Exec(`INSERT INTO sync_runs (name, started, ended, ok, error) VALUES (?,?,?,?,?)`,
		name, store.TS(start), store.TS(end), ok, msg)
	return e
}

// Health is one syncer's standing as its runs tell it.
type Health struct {
	LastRun time.Time // end of the newest run, ok or not
	LastOK  time.Time // end of the newest run that worked (zero: none yet)
	Error   string    // the newest run's error; empty when it worked
	Since   time.Time // start of the first run in the current failing streak
	Fails   int       // runs in that streak
}

// Health reads every syncer's standing, keyed by task name.
func (l *Log) Health() (map[string]Health, error) {
	out := map[string]Health{}
	rows, err := l.db.Query(`SELECT name, MAX(ended), COALESCE(MAX(CASE WHEN ok=1 THEN ended END),'') FROM sync_runs GROUP BY name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, last, lastOK string
		if err := rows.Scan(&name, &last, &lastOK); err != nil {
			rows.Close()
			return nil, err
		}
		out[name] = Health{LastRun: parse(last), LastOK: parse(lastOK)}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The failing streak: every failed run after the newest good one. The
	// newest failure's text is the one shown.
	rows, err = l.db.Query(`SELECT r.name, COUNT(*), MIN(r.started),
		(SELECT e.error FROM sync_runs e WHERE e.name=r.name ORDER BY e.id DESC LIMIT 1)
		FROM sync_runs r
		WHERE r.ok=0 AND r.id > COALESCE((SELECT MAX(o.id) FROM sync_runs o WHERE o.name=r.name AND o.ok=1), 0)
		GROUP BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, since string
		var n int
		var msg sql.NullString
		if err := rows.Scan(&name, &n, &since, &msg); err != nil {
			return nil, err
		}
		h := out[name]
		h.Fails, h.Since, h.Error = n, parse(since), msg.String
		out[name] = h
	}
	return out, rows.Err()
}

func parse(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
