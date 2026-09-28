package store

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"regexp"
	"strings"
	"time"
)

// NewID mints "<prefix>-<8 hex>" — 4 random bytes. EVERY id the hub mints
// comes out of here (docs/reviews/2026-08-28-followups.md §2): the prefixed
// kinds (ask-, cal-, rec-, p-), a thread's slug (the prefix is the slug) and
// the stamped `YYYYMMDD-HHMMSS-<hex>` of proposals, jobs and runs (the prefix
// is the stamp). It used to be 2 bytes per package: with ~300 asks in a 65,536
// space a new one hit a PK collision one time in 200 (Phase 0 of
// docs/reviews/2026-08-26-structure.md). Callers still treat an INSERT failure
// as an error; at 4 bytes it will not be a collision.
func NewID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("store: crypto/rand: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// A ref is the typed pointer every row carries: `kind:<id>` (`ask:…`,
// `action:…`, `rec:…`, `cal:…`, `thread:…`, `job:…`, `prompt:…`) — the kind
// names the table, the id's own shape never has to (conventions.md "Names").
// SplitRef takes one apart; KindOf and Ref build one from a bare id, which is
// what a rec's prose or a `links` column carries. Ids minted before NewID
// existed keep their shape, so the table below is the WHOLE list of shapes:
// nothing else in the hub may switch on a prefix.
var (
	refKinds  = map[string]string{"ask": "ask", "cal": "cal", "rec": "rec", "p": "prompt", "per": "person"}
	prefixed  = regexp.MustCompile(`^([a-z]+)-[0-9a-f]{4,8}$`)
	stampedRe = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{4,8}$`)
)

// SplitRef returns the kind and id of a "<kind>:<id>" reference, or two
// empty strings when ref is not one.
func SplitRef(ref string) (kind, id string) {
	if i := strings.Index(ref, ":"); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ""
}

// KindOf is the kind a bare id's ref would carry. A stamped id is a proposal
// (runs and jobs share the shape but are never named by one); anything that
// is not a known prefix or a stamp is a thread — a slug is any word, so a
// thread called "cal" would collide, and always did.
func KindOf(id string) string {
	if m := prefixed.FindStringSubmatch(id); m != nil {
		if k, ok := refKinds[m[1]]; ok {
			return k
		}
	}
	if stampedRe.MatchString(id) {
		return "action"
	}
	return "thread"
}

// Ref is the `kind:id` form of a bare id.
func Ref(id string) string { return KindOf(id) + ":" + id }

// TS is the one timestamp encoding for text columns: UTC, RFC 3339, always
// nine fraction digits, so string order is time order. RFC3339Nano trims
// trailing zeros and breaks that inside a second; threads/asks always wrote
// this fixed form, calendar/recs/actions/goals now do too. time.Parse with
// RFC3339Nano reads both, so old rows need no rewrite.
func TS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// Day is the calendar day of t as this household counts it: Eastern, not
// UTC. "Today" after 8pm ET is still today.
func Day(t time.Time) string { return t.In(Eastern).Format("2006-01-02") }

// Clock is the wall time of t on that day, same zone — the "At" half of a
// Dated row. A row that knows its instant belongs at that instant on the
// grid; only work with no time of its own goes in the all-day band.
func Clock(t time.Time) string { return t.In(Eastern).Format("15:04") }

// Eastern is the hub's one wall clock. Every day boundary — calendar items,
// review dates, "today" — used to be time.Local, which is right only while
// the Mac's zone is Eastern; UseEastern pins time.Local at start-up so that
// stays true wherever the binary runs.
var Eastern = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("store: %s not found, falling back to the system zone", name)
		return time.Local
	}
	return loc
}

// UseEastern makes time.Local Eastern for the process. Call first thing in
// main; tests do not need it (they build times in time.Local on purpose).
func UseEastern() { time.Local = Eastern }
