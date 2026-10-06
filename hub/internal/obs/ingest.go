package obs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Ingest is the one way a row enters the observations table (review
// 2026-09-26 §2). The phone's single and batch POSTs, the mic's speech rows,
// the screen shots, the console trail and every syncer on the clock call it
// (Insert and InsertBatch are thin wrappers), so what a kind's registration
// says — revisable, what to do once a row lands — holds for every path, and
// no writer can forget a step the others take.
//
// A call is one transaction: every item is checked first, so one bad row
// (no source/kind, a payload that is not JSON, a blob_ref naming no stored
// blob) writes nothing; then the rows go in; then, after the commit, the
// hooks of the kinds that received new rows run once each with those rows.
//
// Rules every row gets here, not in its writer:
//   - tz is an IANA name. Abbreviations ("EDT", "EST"), "UTC" and empty mean
//     the household's zone, America/New_York; a region name the phone sends
//     ("Europe/Paris" on a trip) is kept.
//   - blob first, then refs: a file goes to PutBlob (or POST /blobs) and the
//     row names the ref it got back. A ref with no file behind it is refused.
//   - uniq_key dedupes within (source, kind); a revisable kind appends a
//     "~rN" revision for a re-sent key with a different payload.
//   - Supersedes set by the writer (mail's parser bump) is stored as given.
//
// Future writers land the same way. The Mac recorder, car (internal/car,
// 2026-09-28), is source `car`, kinds `speech` (a transcribed chunk,
// revisable), `session` (revisable), `marker`, `activity` (app, window,
// click, scroll… — never car's `typed`/`key` keystrokes) and `screen` (a
// JPEG, blob first), each keyed `<device>:<session>[:<n>]`, so a re-read
// window is skipped, not doubled, whichever route (the local syncer, or POST
// /observations/batch from a laptop once the hub moves) the rows came in by.
func (s *Store) Ingest(items []Observation) (Result, error) {
	res := Result{Rows: make([]Observation, len(items))}
	for i, o := range items {
		o, err := s.normalize(o)
		if err != nil {
			return Result{}, fmt.Errorf("item %d: %w", i, err)
		}
		res.Rows[i] = o
	}
	if len(items) == 0 {
		return res, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	for i := range res.Rows {
		o, stored, err := s.insertTx(tx, res.Rows[i])
		if err != nil {
			return Result{}, fmt.Errorf("item %d: %w", i, err)
		}
		if stored {
			res.Inserted++
		} else {
			o.ID = 0
			res.Skipped++
		}
		res.Rows[i] = o
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	s.fire(res.Rows)
	return res, nil
}

// Result: what one Ingest call did. Rows are the items as stored, in the
// order given, normalised (ts, tz, schema_version, a revision's key); ID is
// 0 on a row that was already stored.
type Result struct {
	Inserted int           `json:"inserted"`
	Skipped  int           `json:"skipped"`
	Rows     []Observation `json:"-"`
}

// Kind registers what the store knows about one source/kind. Source or Kind
// empty matches any: {Kind: "workout"} is a workout from any source,
// {Source: "health"} every Health kind.
type Kind struct {
	Source, Kind string
	// Revisable: the uniq_key names a PERIOD that keeps filling in (a day's
	// steps), not an immutable object; see revisable below.
	Revisable bool
	// OnInsert runs after the commit, once per Ingest call, with the rows of
	// this kind that call newly stored (never with duplicates). A failure is
	// the hook's to log: the rows are the record and are already stored.
	OnInsert func(rows []Observation)
}

func (k Kind) matches(source, kind string) bool {
	return (k.Source == "" || k.Source == source) && (k.Kind == "" || k.Kind == kind)
}

// Register adds a kind registration. The revisable defaults are registered
// by New; a package that reacts to new rows (workouts, the phone's speech
// report) registers its hook where it is wired up.
func (s *Store) Register(k Kind) {
	s.mu.Lock()
	s.kinds = append(s.kinds, k)
	s.mu.Unlock()
}

func (s *Store) registered() []Kind {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Kind(nil), s.kinds...)
}

func (s *Store) isRevisable(source, kind string) bool {
	for _, k := range s.registered() {
		if k.Revisable && k.matches(source, kind) {
			return true
		}
	}
	return false
}

func (s *Store) fire(rows []Observation) {
	for _, k := range s.registered() {
		if k.OnInsert == nil {
			continue
		}
		var mine []Observation
		for _, o := range rows {
			if o.ID != 0 && k.matches(o.Source, o.Kind) {
				mine = append(mine, o)
			}
		}
		if len(mine) > 0 {
			k.OnInsert(mine)
		}
	}
}

// Zone is the tz every row without a region name of its own is stamped with.
const Zone = "America/New_York"

// CanonicalTZ: an IANA region name is kept; anything else — "", the
// abbreviations time.Zone() hands back ("EDT", "EST"), "UTC", "Local" —
// is the household's zone. Before 2026-09-26 the table held 33,993
// America/New_York rows beside 25,928 "EDT", 6,915 "EST" and 2,682 "UTC",
// the last mostly syncers whose timestamps happened to be in UTC.
func CanonicalTZ(tz string) string {
	if strings.Contains(tz, "/") {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	return Zone
}

func (s *Store) normalize(o Observation) (Observation, error) {
	if o.Source == "" || o.Kind == "" {
		return o, errors.New("source and kind required")
	}
	if o.TS.IsZero() {
		o.TS = time.Now()
	}
	o.TZ = CanonicalTZ(o.TZ)
	if len(o.Payload) == 0 {
		o.Payload = json.RawMessage("{}")
	}
	if !json.Valid(o.Payload) {
		return o, errors.New("payload must be JSON")
	}
	if o.SchemaVersion == 0 {
		o.SchemaVersion = 1
	}
	if o.BlobRef != "" {
		if _, err := s.BlobPath(o.BlobRef); err != nil {
			return o, fmt.Errorf("blob_ref %q: store the blob first (%v)", o.BlobRef, err)
		}
	}
	o.IngestedAt = time.Now().UTC()
	return o, nil
}

// execer: the transaction every statement of one Ingest runs in. The hub
// has one SQLite connection, so a query outside the tx would wait forever.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Store) insertTx(x execer, o Observation) (Observation, bool, error) {
	r, err := x.Exec(`INSERT OR IGNORE INTO observations (source,kind,ts,tz,payload,blob_ref,ingested_at,schema_version,uniq_key,supersedes) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		o.Source, o.Kind, store.TS(o.TS), o.TZ, string(o.Payload), nullable(o.BlobRef), store.TS(o.IngestedAt), o.SchemaVersion, nullable(o.UniqKey), nullableID(o.Supersedes))
	if err != nil {
		return o, false, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		if o.UniqKey != "" && s.isRevisable(o.Source, o.Kind) {
			return revise(x, o)
		}
		return o, false, nil // same (source, kind, uniq_key) is already stored
	}
	o.ID, _ = r.LastInsertId()
	return o, true, nil
}

// revise: o's key is already stored. If the newest reading for that key says
// something else, append o as its revision; if it says the same, it is a
// duplicate. Payloads compare as JSON values — the phone's dictionaries come
// out in a different key order on every sync.
func revise(x execer, o Observation) (Observation, bool, error) {
	const sameKey = `source=?1 AND kind=?2 AND (uniq_key=?3 OR (uniq_key>?3||'~r' AND uniq_key<?3||'~s'))`
	var latest int64
	var n int
	var payload string
	if err := x.QueryRow(`SELECT id, payload, (SELECT COUNT(*) FROM observations WHERE `+sameKey+`)
		FROM observations WHERE `+sameKey+` ORDER BY id DESC LIMIT 1`, o.Source, o.Kind, o.UniqKey).Scan(&latest, &payload, &n); err != nil {
		return o, false, err
	}
	var was, now any
	if json.Unmarshal([]byte(payload), &was) == nil && json.Unmarshal(o.Payload, &now) == nil && reflect.DeepEqual(was, now) {
		return o, false, nil
	}
	o.UniqKey = fmt.Sprintf("%s~r%d", o.UniqKey, n)
	o.Supersedes = latest
	r, err := x.Exec(`INSERT INTO observations (source,kind,ts,tz,payload,blob_ref,ingested_at,schema_version,uniq_key,supersedes) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		o.Source, o.Kind, store.TS(o.TS), o.TZ, string(o.Payload), nullable(o.BlobRef), store.TS(o.IngestedAt), o.SchemaVersion, o.UniqKey, latest)
	if err != nil {
		return o, false, err
	}
	o.ID, _ = r.LastInsertId()
	return o, true, nil
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// ErrDuplicate: the row carries a uniq_key that (source, kind) already has.
// Not a failure — the caller re-sent something the store already holds.
var ErrDuplicate = errors.New("observation already stored")

// Insert stores one observation through Ingest; ErrDuplicate (with the row
// as normalised) when its key is already stored.
func (s *Store) Insert(o Observation) (Observation, error) {
	r, err := s.Ingest([]Observation{o})
	if err != nil {
		return o, err
	}
	if r.Inserted == 0 {
		return r.Rows[0], ErrDuplicate
	}
	return r.Rows[0], nil
}

// InsertBatch stores many observations through Ingest, skipping the ones
// whose uniq_key is already present. One bad row fails the whole call and
// writes nothing, so a connector bug cannot half-write a window.
func (s *Store) InsertBatch(items []Observation) (inserted, skipped int, err error) {
	r, err := s.Ingest(items)
	return r.Inserted, r.Skipped, err
}

// KeyIDs maps each uniq_key stored for (source, kind) to its row id — what a
// writer needs to name the row its new one supersedes.
func (s *Store) KeyIDs(source, kind string) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT uniq_key, id FROM observations WHERE source=? AND kind=? AND uniq_key IS NOT NULL`, source, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		out[k] = id
	}
	return out, rows.Err()
}
