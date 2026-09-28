// Package store owns data/life.db (SQLite, WAL). Append-only by convention:
// observations are never updated; actions only advance state. Schema changes
// are additive: each package appends to its own Schema list (migrate.go).
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ *sql.DB }

// Schema: the observations table (obs is a thin package over it) and the
// APNs device registry. Every other table lives with the package that owns
// it — see hub/CLAUDE.md "Recipe: add a table/column".
var Schema = []string{
	`CREATE TABLE IF NOT EXISTS observations (
		id INTEGER PRIMARY KEY,
		source TEXT NOT NULL, kind TEXT NOT NULL,
		ts TEXT NOT NULL, tz TEXT NOT NULL DEFAULT '',
		payload TEXT NOT NULL DEFAULT '{}', blob_ref TEXT,
		ingested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		schema_version INTEGER NOT NULL DEFAULT 1)`,
	`CREATE INDEX IF NOT EXISTS obs_source_kind_ts ON observations(source, kind, ts)`,
	// uniq_key: an optional caller-supplied identity for a row
	// ("steps:2026-08-20", a HealthKit sample UUID), so a connector that
	// re-reads a window it already sent inserts nothing instead of
	// duplicating it. Still append-only — dupes are never written, existing
	// rows are never touched. Empty/absent = no dedupe.
	`ALTER TABLE observations ADD COLUMN uniq_key TEXT`,
	`CREATE UNIQUE INDEX IF NOT EXISTS obs_uniq ON observations(source, kind, uniq_key) WHERE uniq_key IS NOT NULL`,
	// supersedes: a later reading of a key that keeps filling in (a Health
	// day's total) is a NEW row pointing at the one it replaces; readers skip
	// a row something supersedes. The old row is never updated (obs.revisable).
	`ALTER TABLE observations ADD COLUMN supersedes INTEGER`,
	`CREATE INDEX IF NOT EXISTS obs_supersedes ON observations(supersedes) WHERE supersedes IS NOT NULL`,
	// ingested_at is read as a range ("what arrived since the last digest",
	// threads/asks.go; "rows an import run wrote", statements/documents.go),
	// which scanned all ~55k rows. Written with store.TS since 2026-09-14.
	`CREATE INDEX IF NOT EXISTS obs_ingested_at ON observations(ingested_at)`,
	// APNs device tokens registered by the app (notify/apns.go owns the rows;
	// the table is here so the hub can read it before APNs is configured).
	`CREATE TABLE IF NOT EXISTS devices (
		token TEXT PRIMARY KEY, device TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`ALTER TABLE devices ADD COLUMN env TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE devices ADD COLUMN build INTEGER NOT NULL DEFAULT 0`,
	// settings: one row per hub-wide switch the owner flips from a surface
	// (today: `default_model`, the rung a new session is pinned to),
	// plus the hub clock's `clock:<task>` last-start stamps, so a restart
	// does not reset every 6h/24h countdown.
	// The one table here that is NOT append-only, deliberately: a switch is a
	// current position, not a record. What it changed IS append-only — every
	// thread carries the model it was created with.
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL)`,
	// voice_queue: every card's spoken line from the moment its push is
	// queued until it is HEARD (notify/queue.go owns the rows; here beside
	// devices for the same reason). The queue itself — slot timers, the wait
	// for the floor, the wait for the owner's microphone, the Mac's helper —
	// is in memory, and a hub restart drops all of it, so cards raised just
	// before a restart would die unspoken. A row with no heard_at is re-spoken when the hub comes back (once).
	`CREATE TABLE IF NOT EXISTS voice_queue (
		card TEXT PRIMARY KEY, thread TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', body TEXT NOT NULL,
		queued_at TEXT NOT NULL, heard_at TEXT NOT NULL DEFAULT '',
		resumed_at TEXT NOT NULL DEFAULT '')`,
}

// Setting reads a hub-wide switch; never set = "".
func (d *DB) Setting(key string) string {
	var v string
	d.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

// SetSetting writes one ("" clears it back to the hub's own default).
func (d *DB) SetSetting(key, value string) error {
	if value == "" {
		_, err := d.Exec(`DELETE FROM settings WHERE key=?`, key)
		return err
	}
	_, err := d.Exec(`INSERT INTO settings (key,value,updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, TS(time.Now()))
	return err
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite + WAL: one writer, keep it simple
	d := &DB{db}
	if _, err := d.Migrate("store", Schema); err != nil {
		return nil, err
	}
	return d, nil
}
