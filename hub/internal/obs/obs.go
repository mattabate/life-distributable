// Package obs: the append-only observations table + content-addressed blob
// store (DESIGN.md "Data model"). Adding a new input = new source/kind
// string; nothing here ever updates, and the ONE delete is
// PruneQuotaHeartbeats — a fixed WHERE on the hub's own 10-minute plan-quota
// readings, never a parameter another kind could be passed to.
package obs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"life/hub/internal/store"
)

type Observation struct {
	ID            int64           `json:"id"`
	Source        string          `json:"source"` // app | healthkit | polar | simplefin | claude:<job> …
	Kind          string          `json:"kind"`   // photo | note | meal | workout-survey | weight …
	TS            time.Time       `json:"ts"`
	TZ            string          `json:"tz"`
	Payload       json.RawMessage `json:"payload"`
	BlobRef       string          `json:"blob_ref,omitempty"`
	IngestedAt    time.Time       `json:"ingested_at"`
	SchemaVersion int             `json:"schema_version"`
	// UniqKey: optional identity within (source, kind) — "steps:2026-08-20", a
	// HealthKit sample UUID. Re-sending a row with a key already stored is a
	// no-op, so a connector can re-read a window (late-arriving samples) without
	// duplicating what it already sent.
	UniqKey string `json:"uniq_key,omitempty"`
	// Supersedes: the id of the row this one replaces — a later reading of a
	// revisable key (see revisable). Readers (List, Counts) skip a row once
	// something supersedes it; the old row itself is never touched.
	Supersedes int64 `json:"supersedes,omitempty"`
}

// revisable: sources whose uniq_key names a PERIOD that keeps filling in, not
// an immutable object. The phone's Health sync keys a day's total as
// "steps:2026-09-12"; plain INSERT OR IGNORE kept the first partial sync of
// each day forever (09-12 read 27 steps, 09-13 19 — every day 08-24..09-13
// held whatever the first sync after midnight saw). For these sources a
// re-sent key with a DIFFERENT payload appends a revision row
// ("steps:2026-09-12~r1", supersedes = the row it replaces); the same payload
// is still a duplicate. Everything else keeps first-write-wins.
//
// An entry is a whole source ("health") or one source/kind. The audience
// per-day snapshots joined 2026-09-14: PostHog re-sent the same 30 days on every
// 6-hour sync with no key at all, so `posthog/site-day` held 12,074 rows of
// which 8,579 were byte-identical copies (`ops/db.sh "select count(*),
// count(distinct payload) from observations where source='posthog' and
// kind='site-day'"`), and a 1,000-row read of "the last 31 days" reached back
// only ~20. Keyed and revisable, a day gets a new row only when its numbers move.
// New registers these; see Kind and Register (ingest.go).
var revisable = []Kind{
	{Source: "health", Revisable: true},
	{Source: "posthog", Kind: "site-day", Revisable: true},
	{Source: "posthog", Kind: "site-totals", Revisable: true},
	{Source: "youtube", Kind: "channel-stats", Revisable: true},
}

// BaseKey strips a revision suffix: "steps:2026-09-12~r2" → "steps:2026-09-12".
func BaseKey(k string) string {
	if i := strings.LastIndex(k, "~r"); i >= 0 {
		return k[:i]
	}
	return k
}

type Store struct {
	db      *store.DB
	BlobDir string

	mu    sync.RWMutex
	kinds []Kind // Register
}

func New(db *store.DB, blobDir string) (*Store, error) {
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{db: db, BlobDir: blobDir}
	for _, k := range revisable {
		s.Register(k)
	}
	return s, nil
}

// PutBlob stores bytes under sha256/<hash>.<ext>; idempotent.
func (s *Store) PutBlob(r io.Reader, ext string) (ref string, size int64, err error) {
	tmp, err := os.CreateTemp(s.BlobDir, ".upload-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	tmp.Close()
	if err != nil {
		return "", 0, err
	}
	if size == 0 {
		return "", 0, errors.New("empty blob")
	}
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" {
		ext = "bin"
	}
	sum := hex.EncodeToString(h.Sum(nil))
	ref = fmt.Sprintf("sha256/%s/%s.%s", sum[:2], sum, ext)
	dst := filepath.Join(s.BlobDir, filepath.FromSlash(ref))
	if _, err := os.Stat(dst); err == nil {
		return ref, size, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	return ref, size, os.Rename(tmp.Name(), dst)
}

func (s *Store) BlobPath(ref string) (string, error) {
	if !strings.HasPrefix(ref, "sha256/") || strings.Contains(ref, "..") {
		return "", errors.New("bad blob ref")
	}
	p := filepath.Join(s.BlobDir, filepath.FromSlash(ref))
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

// PruneQuotaHeartbeats deletes the hub's own plan-quota readings (source
// `spend`, kind `quota` — written every 10 minutes by cmd/hub's quotaHistory)
// older than `before`, and returns how many went. They are a heartbeat, not
// evidence: the Spend page reads the last 8 h of them and nothing else reads
// them at all (docs/reviews/2026-08-28-followups.md §3). Both identifiers are
// literals in the statement on purpose — every other kind in this table is
// something a finance or audience session may cite years from now, so there
// is no general prune here and none may be added; a delete of anything else
// goes through `lifectl propose --kind delete`.
func (s *Store) PruneQuotaHeartbeats(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM observations WHERE source='spend' AND kind='quota' AND ts < ?`, store.TS(before))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Keys returns the uniq_keys already stored for (source, kind) — what a
// connector needs to know which remote objects it can skip fetching.
func (s *Store) Keys(source, kind string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT uniq_key FROM observations WHERE source=? AND kind=? AND uniq_key IS NOT NULL`, source, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

type Query struct {
	Source, Kind string
	// Account matches payload.account exactly (a finance row's account name
	// or slug): one account's transactions without a client-side filter.
	Account      string
	Since, Until time.Time
	Limit        int
	// LatestBy: payload fields ("project_id", "date") that name one thing; List
	// then returns only the newest row (highest id) per distinct combination.
	// This is how a reader asks "what does each day say now" of a table that
	// may hold many copies of a day — in SQL, so a row limit can never decide
	// how many days come back.
	LatestBy []string
	// AfterID pages FORWARD through the table: only rows with a larger id,
	// oldest first. ids only grow, so a reader that keeps the last id it saw
	// (ops/trail.py) never misses a row or reads one twice — which paging by
	// ts could, with rows written late for an earlier ts or on one instant.
	// Forward asks for that order from the start (after_id=0).
	AfterID int64
	Forward bool
}

var fieldName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (s *Store) List(q Query) ([]Observation, error) {
	// a superseded reading is history, not the current value
	where := []string{"NOT EXISTS (SELECT 1 FROM observations r WHERE r.supersedes=o.id)"}
	var args []any
	if q.Source != "" {
		where = append(where, "source=?")
		args = append(args, q.Source)
	}
	if q.Kind != "" {
		where = append(where, "kind=?")
		args = append(args, q.Kind)
	}
	if q.Account != "" {
		where = append(where, "json_extract(payload,'$.account')=?")
		args = append(args, q.Account)
	}
	if !q.Since.IsZero() {
		where = append(where, "ts>=?")
		args = append(args, store.TS(q.Since))
	}
	if !q.Until.IsZero() {
		where = append(where, "ts<?")
		args = append(args, store.TS(q.Until))
	}
	order := "ts DESC, id DESC"
	if q.AfterID > 0 || q.Forward {
		where = append(where, "id>?")
		args = append(args, q.AfterID)
		order = "id ASC"
	}
	// uniq_key comes back too: it is the row's identity within (source, kind),
	// and a reader that derives a table from these rows (internal/workouts)
	// needs it to give the same input the same derived id every rebuild.
	cond := strings.Join(where, " AND ")
	if len(q.LatestBy) > 0 {
		groups := make([]string, 0, len(q.LatestBy))
		for _, f := range q.LatestBy {
			if !fieldName.MatchString(f) {
				return nil, fmt.Errorf("obs: bad LatestBy field %q", f)
			}
			groups = append(groups, "json_extract(o.payload,'$."+f+"')")
		}
		cond = `o.id IN (SELECT MAX(o.id) FROM observations o WHERE ` + cond + ` GROUP BY ` + strings.Join(groups, ", ") + `)`
	}
	sql := `SELECT id,source,kind,ts,tz,payload,blob_ref,ingested_at,schema_version,uniq_key,supersedes FROM observations o WHERE ` + cond
	// Asking for MORE than the ceiling used to hand back 100 rows: the guard
	// read `<= 0 || > 1000` and fell through to the same default, so a caller
	// saying "give me everything" got a page. Found 2026-08-29 on the website
	// section — `Limit: 20000` over a 133-day PostHog history measured 6 days
	// and printed 22 visitors — and it was cutting the GitHub traffic history
	// and the X follower curve the same way. Too big now CLAMPS; only an unset
	// limit means "just a page".
	const maxRows = 50000
	switch {
	case q.Limit <= 0:
		q.Limit = 100
	case q.Limit > maxRows:
		q.Limit = maxRows
	}
	sql += fmt.Sprintf(" ORDER BY %s LIMIT %d", order, q.Limit)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Observation{}
	for rows.Next() {
		var o Observation
		var ts, ing, payload string
		var blob, uniq *string
		var sup *int64
		if err := rows.Scan(&o.ID, &o.Source, &o.Kind, &ts, &o.TZ, &payload, &blob, &ing, &o.SchemaVersion, &uniq, &sup); err != nil {
			return nil, err
		}
		o.TS, _ = time.Parse(time.RFC3339Nano, ts)
		o.IngestedAt, _ = time.Parse(time.RFC3339Nano, ing)
		o.Payload = json.RawMessage(payload)
		if blob != nil {
			o.BlobRef = *blob
		}
		if uniq != nil {
			o.UniqKey = *uniq
		}
		if sup != nil {
			o.Supersedes = *sup
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Counts by source/kind — the "what data do we have" view.
type Count struct {
	Source string    `json:"source"`
	Kind   string    `json:"kind"`
	N      int       `json:"n"`
	First  time.Time `json:"first"`
	Last   time.Time `json:"last"`
}

func (s *Store) Counts() ([]Count, error) {
	rows, err := s.db.Query(`SELECT source, kind, COUNT(*), MIN(ts), MAX(ts) FROM observations o WHERE NOT EXISTS (SELECT 1 FROM observations r WHERE r.supersedes=o.id) GROUP BY source, kind ORDER BY MAX(ts) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Count{}
	for rows.Next() {
		var c Count
		var f, l string
		if err := rows.Scan(&c.Source, &c.Kind, &c.N, &f, &l); err != nil {
			return nil, err
		}
		c.First, _ = time.Parse(time.RFC3339Nano, f)
		c.Last, _ = time.Parse(time.RFC3339Nano, l)
		out = append(out, c)
	}
	return out, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
