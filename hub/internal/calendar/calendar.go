// Package calendar is the hub's plan layer: everything that should happen on
// a date — a step the owner must take ("buy the SPY tranche"), an agent run
// that fires that day ("re-export the 401k statement"), or a reminder
// ("options vest") — lives in one table so the app can show a single
// forward-looking agenda, and so nothing slips: a dated item for the owner
// becomes an ask on their board when its day comes, and if they have not
// closed it the hub nags them (push) until they do.
//
// The calendar view merges three sources: cal_items (this table), the
// queued prompts (every future wake of the hub — a standing check-in or job
// projected forward, a one-shot on its day), and undated open asks
// ("anytime" work). Rows are never deleted; state only advances.
//
// One clock (docs/reviews/2026-08-28-one-clock.md): an agent item is the
// owner-facing record; its wake is a prompt row (`prompt_id`) with
// not_before = the item's fire time, so the same tick that wakes every
// session wakes it. The item closes when the prompt is DELIVERED
// (threads.OnDelivered) — never before its session has the words.
package calendar

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"life/hub/internal/cadence"
	"life/hub/internal/notify"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

// Schema: dated items. Append an ALTER to add a column (store/migrate.go).
var Schema = []string{`CREATE TABLE IF NOT EXISTS cal_items (
	id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,                 -- owner (the owner's step) | agent (run an agent) | note (remind the owner)
	day TEXT NOT NULL,                  -- YYYY-MM-DD local
	at TEXT NOT NULL DEFAULT '',        -- HH:MM local, '' = all day (fires at default_hour)
	repeat TEXT NOT NULL DEFAULT '',    -- '' | daily | weekly | monthly | yearly | every<N>d
	goal_id TEXT, thread_id TEXT,       -- thread: where the ask lands / the agent prompt goes
	source TEXT NOT NULL DEFAULT '',    -- owner | claude:thread:<id> | job:<name>
	state TEXT NOT NULL DEFAULT 'scheduled', -- scheduled | fired | done | dismissed
	nag_min INTEGER NOT NULL DEFAULT 0, -- minutes between "did this get done?" pushes after firing (0 = none)
	nag_count INTEGER NOT NULL DEFAULT 0, last_nag_at TEXT,
	fired_at TEXT, ask_id TEXT, prev_id TEXT,
	resolved_at TEXT, resolved_by TEXT, resolution TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS cal_items_day ON cal_items(day, state);`,
	// ask_kind: what the ask minted on the day should be (decision|access|
	// physical|read|other). Empty keeps the old behaviour (physical for owner,
	// read for note). Set when a session dates an ask instead of raising it
	// now — `lifectl ask add --on` (schedule a "needs you" for the day it can
	// be done).
	`ALTER TABLE cal_items ADD COLUMN ask_kind TEXT NOT NULL DEFAULT ''`,
	// check_hint carried through to the minted ask, so the hourly verifier can
	// close it on evidence exactly as it would an undated ask.
	`ALTER TABLE cal_items ADD COLUMN check_hint TEXT NOT NULL DEFAULT ''`,
	// surface carried to the minted ask (mobile|web|any; '' = infer that day),
	// so a dated `--surface web` is not re-guessed from its text when it fires.
	`ALTER TABLE cal_items ADD COLUMN surface TEXT NOT NULL DEFAULT ''`,
	// prompt_id: the queued prompt that is an agent item's wake (one clock,
	// 2026-08-28). '' on owner/note items, and on an agent item from before
	// the clock until the first tick backfills it.
	`ALTER TABLE cal_items ADD COLUMN prompt_id TEXT NOT NULL DEFAULT ''`,
	// say: a dated ask's spoken message (`lifectl ask add --on … --say`),
	// carried to the ask it mints so the card is spoken and leads with it like
	// any other (2026-09-25; until then --on dropped --say).
	`ALTER TABLE cal_items ADD COLUMN say TEXT NOT NULL DEFAULT ''`,
	// due (2026-09-26): WHEN a dated step of the owner's is owed — `on` its
	// day only (a missed one closes as missed at midnight, never Overdue:
	// watering the plants early does not count) or `by` its day (owed from the
	// day its period opens, Overdue after: a weekly assignment). '' on agent and
	// note items and on a soon to-do (no day). See `dueOf`.
	`ALTER TABLE cal_items ADD COLUMN due TEXT NOT NULL DEFAULT ''`,
	// cal_events (2026-09-26, review-primitives step 4): every state move of
	// an item, append-only, written by setItem alone. A Reopen used to null
	// the close off the row and that was the only record of it.
	`CREATE TABLE IF NOT EXISTS cal_events (
	id INTEGER PRIMARY KEY, item_id TEXT NOT NULL, ts TEXT NOT NULL,
	actor TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, note TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS cal_events_item ON cal_events(item_id, id);`,
	// items (2026-09-27, inner-design step 9): every item moves to the one
	// table of what reaches the owner (store.ItemsSchema), trail and all; cal_items
	// and cal_events are frozen from here on. A fired item is `open` there — it
	// is its own card now (step 11) — and reads back as `fired` (scanItem).
	`INSERT OR IGNORE INTO items (id,src,created_at,updated_at,title,detail,kind,state,thread_id,goal_id,source,surface,check_hint,day,at,repeat,due,ask_kind,say,nag_min,nag_count,last_nag_at,fired_at,ask_id,prompt_id,prev_id,resolved_at,resolved_by,resolution)
	SELECT id,'cal',created_at,updated_at,title,detail,kind,CASE state WHEN 'fired' THEN 'open' ELSE state END,thread_id,goal_id,source,surface,check_hint,day,at,repeat,due,ask_kind,say,nag_min,nag_count,last_nag_at,fired_at,ask_id,prompt_id,prev_id,resolved_at,resolved_by,resolution FROM cal_items ORDER BY created_at;
INSERT INTO item_events (item_id,ts,actor,from_state,to_state,note)
	SELECT item_id,ts,actor,CASE from_state WHEN 'fired' THEN 'open' ELSE from_state END,CASE to_state WHEN 'fired' THEN 'open' ELSE to_state END,note FROM cal_events ORDER BY id;
UPDATE items SET ` + store.ItemStampSet + ` WHERE src='cal';`,
}

// setItem is the ONE writer of an item's state: query is the row's own write
// (with its guard), and the move — before → after, by whom, with what words —
// is appended to item_events in the same transaction. moved = a row changed.
func (c *Calendar) setItem(id, by, note, query string, args ...any) (bool, error) {
	return store.ItemLog.Move(c.db, id, by, strings.TrimSpace(note), false, c.Now(), query, args...)
}

// edit: a plain write to an item that is not a state move (its time, its
// window, a nag); verb, window and lane are re-derived after it.
func (c *Calendar) edit(id, query string, args ...any) error {
	if _, err := c.db.Exec(query, args...); err != nil {
		return err
	}
	return store.StampItem(c.db, id)
}

// backfillDue: the rows from before the column say what they were — a practice
// (homework that is not a Learn assignment, or a daily step) is `on`, every
// other dated step of the owner's `by`. Idempotent: only rows still empty.
func (c *Calendar) backfillDue() {
	c.db.Exec(`UPDATE items SET due='on' WHERE src='cal' AND due='' AND day<>'' AND
		((kind='homework' AND source NOT LIKE 'hub:learn:%') OR (kind='owner' AND repeat='daily'))`)
	c.db.Exec(`UPDATE items SET due='by' WHERE src='cal' AND due='' AND day<>'' AND kind IN ('owner','homework')`)
	c.db.Exec(`UPDATE items SET ` + store.ItemStampSet + ` WHERE src='cal'`)
}

// dueOf: the window a new dated step of the owner's gets when its filer did not say —
// a daily one is owed on its day, anything else by it. Empty for everything else.
func dueOf(it Item) string {
	switch {
	case !ownerKind(it.Kind) || it.Day == "":
		return ""
	case it.Due == "on" || it.Due == "by":
		return it.Due
	case it.Repeat == "daily":
		return "on"
	}
	return "by"
}

// fallbackThread: the hub's idle thread that hosts the cards of steps no
// session is behind (threads.StepThread — the card reads it there too).
const fallbackThread = threads.StepThread

// sessionless: nobody is behind this step but the hub — homework always (a
// practice has no host), and a step with no thread or only the calendar's.
// Such a step closes with one tap; a step a session waits on closes with the
// owner's words to it.
func sessionless(kind, threadID string) bool {
	return kind == "homework" || threadID == "" || threadID == fallbackThread
}

// Item: one dated thing. Fire time = day+at (or day+DefaultHour when at=”).
//
// A SOON item (2026-09-21) is an `owner` item with NO day: a to-do of the
// owner's that was filed at a moment and can be done at any time after it —
// "please do this soon", never overdue. So it never fires, never nags, never
// counts as due, never enters Overdue, and still closes with the owner's
// words (Reopen always there). It belongs to NO session and raises no ask: it
// appears as any-time on the calendar, not as a to-do on the session that
// filed it. So `thread_id` is empty (`source` keeps who filed it), a step
// that turns soon gives up the ask it had raised, and the owner's words close
// it on the hub. Its place on the page is the minute it was added — and, once
// closed, the minute it was done (`Slot`).
type Item struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Title     string     `json:"title"`
	Detail    string     `json:"detail"`
	Kind      string     `json:"kind"`
	Day       string     `json:"day"`
	At        string     `json:"at"`
	Repeat    string     `json:"repeat"`
	GoalID    string     `json:"goal_id,omitempty"`
	ThreadID  string     `json:"thread_id,omitempty"`
	Source    string     `json:"source"`
	State     string     `json:"state"`
	NagMin    int        `json:"nag_min"`
	NagCount  int        `json:"nag_count"`
	LastNagAt *time.Time `json:"last_nag_at,omitempty"`
	FiredAt   *time.Time `json:"fired_at,omitempty"`
	AskKind   string     `json:"ask_kind,omitempty"`
	CheckHint string     `json:"check_hint,omitempty"`
	Surface   string     `json:"surface,omitempty"`
	Say       string     `json:"say,omitempty"`
	Due       string     `json:"due,omitempty"` // on | by (the owner's dated steps), '' otherwise
	// Window (2026-09-27, step 10): when it is owed, for every item — on | by
	// its day, or soon with none (store.ItemStampSet derives it).
	Window     string     `json:"window"`
	AskID      string     `json:"ask_id,omitempty"`
	PromptID   string     `json:"prompt_id,omitempty"`
	PrevID     string     `json:"prev_id,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	// Soon: no day — read-only on a row (day == ""); on POST it is the explicit
	// "I mean no day", so a forgotten `day` is still an error.
	Soon bool `json:"soon,omitempty"`
}

// Slot: where the row sits — its own day and time, or for a soon item the
// local minute it was added (open) or closed (done | dismissed).
func (it Item) Slot() (day, at string) {
	if it.Day != "" {
		return it.Day, it.At
	}
	t := it.CreatedAt
	if (it.State == "done" || it.State == "dismissed") && it.ResolvedAt != nil {
		t = *it.ResolvedAt
	}
	t = t.Local()
	return t.Format("2006-01-02"), t.Format("15:04")
}

// Entry: one row of the merged agenda (items + projected schedules + undated
// asks + every other object with a date: actions, deferred recs). This is
// the ONE read model over everything dated (Phase 2, 2026-08-26): the board
// is the "now" view of these objects, the agenda the "when" view.
type Entry struct {
	ID   string `json:"id"`
	Day  string `json:"day"` // '' = anytime
	At   string `json:"at,omitempty"`
	Kind string `json:"kind"` // owner | homework | agent | note | run (thread check-in) | job (scheduler) | ask (undated) | action | rec
	// AskKind (2026-09-12): the ask's own kind when Kind is "ask" or the row
	// is the record of one closing — so an `install` ask draws as the same
	// teal cell with one Install button it is everywhere else, not a generic
	// Done/Dismiss row.
	AskKind  string `json:"ask_kind,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	State    string `json:"state"` // scheduled | fired | done | dismissed | open | answered | an action state | deferred
	GoalID   string `json:"goal_id,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	AskID    string `json:"ask_id,omitempty"`
	Repeat   string `json:"repeat,omitempty"`
	Overdue  bool   `json:"overdue,omitempty"`
	// Soon (2026-09-21): a to-do of the owner's with no due day. `day`/`at` are
	// the minute it was ADDED (closed: the minute it was done); never overdue.
	Soon bool `json:"soon,omitempty"`
	// Due (2026-09-26): on | by — owed on its day only, or by it (the item's
	// own column). Tick: an open step with no session behind it, so it closes
	// with Did it / Skip and no words (stampEntry).
	Due  string `json:"due,omitempty"`
	Tick bool   `json:"tick,omitempty"`
	// Window (2026-09-27, step 10): when the item behind the row is owed —
	// now | on | by | soon, the item's own (store.Dated.Window). Absent on a
	// row that is no item (a run, a job).
	Window string `json:"window,omitempty"`
	// Actor: who raised or decided it (an item's source, an ask's session, an
	// action's decided_via, a rec's source). Ref: the typed pointer to the
	// object behind the row — ask:<id> | action:<id> | rec:<id> | cal:<id> |
	// thread:<id> | job:<name> | prompt:<id> (a one-shot wake bound for a
	// session that does not exist yet).
	Actor string `json:"actor,omitempty"`
	Ref   string `json:"ref,omitempty"`
	// Did / Verb (2026-09-09): the row is a record of something that
	// happened — an ask the owner closed, a rec decided, an action decided, a step
	// closed — at the minute it happened; Verb is the word to prefix the
	// title with ("Read: …", "Approved: …"). ThreadTitle names the session
	// behind it, so a reader can fold an evening's steps into one box titled
	// by the work they belonged to ("Migrate the blog to a new host").
	Did         bool   `json:"did,omitempty"`
	Verb        string `json:"verb,omitempty"`
	ThreadTitle string `json:"thread_title,omitempty"`
	// Live (2026-09-26): what the row's session is doing RIGHT NOW, in the
	// board's own capsules — "speaking" / "waiting to speak" / "running".
	// Stamped at read time on every row with a session, so the row the owner
	// just answered from says its session is on it instead of falling
	// silent. Absent on a quiet session — a row
	// never says "idle".
	Live []threads.Pill `json:"live,omitempty"`
	// What both surfaces used to work out for themselves, row by row, and
	// drifted on (parity pass 2026-09-14) — so the hub says it once
	// (`stampEntry`) and the console and the phone only draw it:
	//   Lane      mine | chores | homework | scheduled | agents | recs — colour + switch
	//   Open      the owner's still to resolve (a pending item or ask: its Done /
	//             Dismiss answer) — NOT !Closed: a proposed action or a
	//             deferred rec is neither
	//   Closed    ✓/✕ and muted (a LOOK, never a filter)
	//   Mark      the glyph: wont (✕ + strike) | done (✓) | todo (○) | ""
	//             (store.CalMark)
	//   Item     a cal_items row: closes via /calendar/{id}/resolve, and a
	//             closed one always carries Reopen
	//   Move      item (PATCH day/at) | run (rewrite the cadence) | "" = fixed
	//   Why       the sentence an unmovable row shows instead of dragging
	//   KindLabel the word the row's pill wears
	Lane      string `json:"lane"`
	Open      bool   `json:"open,omitempty"`
	Closed    bool   `json:"closed,omitempty"`
	Mark      string `json:"mark,omitempty"`
	Item      bool   `json:"item,omitempty"`
	Move      string `json:"move,omitempty"`
	Why       string `json:"why,omitempty"`
	KindLabel string `json:"kind_label"`
	// Outcomes (2026-09-26): the row's answer buttons, from the hub's one
	// vocabulary (store/close.go) — an open tick (homework, a chore): Did it ·
	// Skip · Send; an open step of the owner's: Done · Won't do · Reply; a pending
	// proposal: Approve · Deny (· Reply with a session). Absent otherwise.
	Outcomes []store.Outcome `json:"outcomes,omitempty"`
	// Coming (2026-10-05): a LATER occurrence of a repeating item, drawn on the
	// day it will fall (`coming`). No row exists for it yet — a repeat's next
	// occurrence is minted when the one before it fires — so it is read-only:
	// not an item, no buttons, nothing to drag; `id` is `<item id>@<day>` and
	// `ref` points at the occurrence that does exist.
	Coming bool `json:"coming,omitempty"`
}

// stampEntry fills Lane / Closed / Item / Move / Why / KindLabel from the
// row's own fields (docs/design/calendar.md, "Three colours, three switches").
func stampEntry(e *Entry) {
	refKind, _, _ := strings.Cut(e.Ref, ":")
	install := e.AskKind == "install" && (e.Kind == "ask" || e.Did)
	// A coming occurrence wears its item's lane and words, and nothing that
	// acts: there is no row behind it to close or move yet.
	isCal := refKind == "cal"
	e.Item = isCal && !e.Coming
	e.Tick = e.Item && !e.Soon && !e.Did && sessionless(e.Kind, e.ThreadID) && ownerKind(e.Kind)

	// A rec is purple its whole life; homework keeps its blue whoever closed
	// it, and a chore (a dated step no session is behind) its own lane; a
	// record is its doer's; a pending approval is the owner's; a prompt an agent wrote
	// for a minute is a scheduled run its whole life, done or dismissed (one
	// colour for one kind); anything new falls to grey.
	switch {
	case e.Kind == "rec":
		e.Lane = "recs"
	case e.Kind == "homework":
		e.Lane = "homework"
	case e.Kind == "agent":
		e.Lane = "scheduled"
	case isCal && !e.Soon && store.Chore(e.Kind, e.Day, e.ThreadID):
		e.Lane = "chores"
	case e.Did:
		// A did row whose doer is the owner (store.Owner) is red; anyone else — the
		// hub, the gate, a session — is the agents' (grey).
		if e.Kind == "owner" {
			e.Lane = "mine"
		} else {
			e.Lane = store.DoerLane(e.Actor)
		}
	case e.Kind == "action":
		if e.State == "proposed" {
			e.Lane = "mine"
		} else {
			e.Lane = "agents"
		}
	case e.Kind == "agent", e.Kind == "run" && refKind == "prompt":
		e.Lane = "scheduled"
	case e.Kind == "owner", e.Kind == "ask", e.Kind == "note":
		e.Lane = "mine"
	default:
		e.Lane = "agents"
	}

	e.Closed = e.Did || e.State == "done" || e.State == "accepted" || store.Wont(e.State) ||
		(e.Kind == "action" && e.State != "" && e.State != "proposed")
	e.Mark = store.CalMark(e.State, e.Did, e.Lane)
	e.Open = false
	switch e.State {
	case "scheduled", "fired", "open", "answered":
		e.Open = !e.Coming
	}

	e.Outcomes = nil
	switch {
	case e.Closed:
	case e.Item && (e.Tick || e.Kind == "homework"):
		e.Outcomes = store.TickOutcomes(true)
	case e.Item && e.Kind == "owner":
		e.Outcomes = store.StepOutcomes()
	case e.Item && e.Kind == "note":
		e.Outcomes = store.NoteOutcomes()
	case e.Kind == "action" && e.State == "proposed":
		e.Outcomes = store.ActionOutcomes(e.ThreadID != "")
	}

	e.Move = ""
	switch {
	case e.Item && (e.State == "scheduled" || e.State == "fired"):
		e.Move = "item"
	case e.Kind == "run" && e.ThreadID != "" && (strings.HasPrefix(e.Repeat, "daily@") || strings.HasPrefix(e.Repeat, "weekly@")):
		e.Move = "run"
	}
	e.Why = ""
	if e.Move == "" {
		switch {
		case e.Coming:
			e.Why = "A repeat: this one appears for real when the one before it comes due. Move that one and these follow."
		case e.Item && (e.State == "done" || e.State == "dismissed"):
			e.Why = "Already " + e.State + " — reopen it first."
		case e.Did:
			e.Why = "A record — it sits at the minute it happened, and nothing moves it."
		case e.Kind == "job":
			e.Why = "A job's cadence lives in ops/schedule.json — no API moves it."
		case e.Kind == "run":
			e.Why = "A one-shot wake, or a cadence the calendar cannot rewrite (every@…). Change it in the session."
		case e.Kind == "action":
			e.Why = "A proposal sits at the minute it was made or decided — nothing moves it."
		case e.Kind == "rec":
			e.Why = "A recommendation sits at the minute it was filed (dark) or answered (light) — nothing moves it."
		case install:
			e.Why = "A build waiting to be tapped — it sits in Anytime until you install it, then lands as a record at that minute."
		case e.Kind == "ask":
			e.Why = "This row has no date of its own — it sits on the day it is due for review."
		default:
			e.Why = "Not movable."
		}
	}

	switch {
	case install:
		e.KindLabel = "app update"
	case e.Soon:
		e.KindLabel = "anytime"
	case e.Kind == "owner":
		e.KindLabel = "your step"
	case e.Kind == "agent", e.Kind == "run" && refKind == "prompt":
		e.KindLabel = "one-off run"
	case e.Kind == "note":
		e.KindLabel = "reminder"
	case e.Kind == "run":
		e.KindLabel = "recurring run"
	case e.Kind == "rec" && e.At == "" && !e.Did:
		e.KindLabel = "check back"
	default:
		e.KindLabel = e.Kind
	}
}

// Dated is one agenda row another package contributes (store.Dated); a
// DatedSource is any package that can list its dated objects for a window.
// actions.Queue (every action, by decided else created day) and recs.Store
// (deferred recs, by review_on) implement it; Calendar.Sources holds them.
type Dated = store.Dated

type DatedSource interface {
	Dated(from, to string) ([]Dated, error)
}

// Row projects one calendar item into the shared read-model row (Phase 5) —
// the ONE place an item becomes an agenda row, beside threads.Row,
// actions.Row and recs.Row. An item's Kind is its own (owner | agent | note):
// the board ignores those, because a scheduled item is not waiting on the
// owner until it fires — and then it is its own card (step 11), one row.
func Row(it Item) Dated {
	d := Dated{
		ID: it.ID, Kind: it.Kind, Ref: "cal:" + it.ID, Key: it.ID, AskID: it.AskID,
		Title: it.Title, Detail: it.Detail, State: it.State, Day: it.Day, At: it.At,
		Repeat: it.Repeat, GoalID: it.GoalID, ThreadID: it.ThreadID, Actor: it.Source, Window: it.Window, Obj: it,
	}
	// A soon item sits at the minute it was added; closed, at the minute it
	// was done — it never had a plan day to stay on.
	d.Day, d.At = it.Slot()
	d.Soon = it.Day == ""
	// A closed item is a record of who closed it (2026-09-09): the actor
	// becomes the closer, the verb says what happened. It stays on ITS day —
	// the plan day is the item's identity, and Google Calendar mirrors it
	// there — unlike an ask or a rec, which have no day but the deed's.
	if (it.State == "done" || it.State == "dismissed") && it.ResolvedBy != "" {
		d.Did, d.Actor, d.Verb = true, it.ResolvedBy, store.CalDidVerb(it.Kind, it.State)
	}
	return d
}

// entryFrom: the ONE place a read-model row becomes an agenda entry. Every
// section below goes through it, so a row's kind, day, actor and pointer are
// drawn the same way whichever package contributed it.
func entryFrom(d Dated) Entry {
	// `kind:id` is the default pointer, so a DatedSource that only fills the
	// seven columns it owns still gets a row that routes.
	ref := d.Ref
	if ref == "" {
		ref = d.Kind + ":" + d.ID
	}
	id := d.Key
	if id == "" {
		id = ref
	}
	e := Entry{
		ID: id, Day: d.Day, At: d.At, Kind: d.Kind, AskKind: d.AskKind, Title: d.Title, Detail: d.Detail,
		State: d.State, GoalID: d.GoalID, ThreadID: d.ThreadID, AskID: d.AskID,
		Repeat: d.Repeat, Actor: d.Actor, Ref: ref, Soon: d.Soon, Window: d.Window,
		Did: d.Did, Verb: d.Verb, ThreadTitle: d.ThreadTitle,
	}
	if it, ok := d.Obj.(Item); ok {
		e.Due = it.Due
	}
	stampEntry(&e)
	return e
}

type Day struct {
	Day     string  `json:"day"`
	Entries []Entry `json:"entries"`
}

type View struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	Today   string  `json:"today"`
	Anytime []Entry `json:"anytime"` // the owner's undated to-dos (oldest first), then undated open asks
	Overdue []Entry `json:"overdue"` // every open owner item from a day that has passed, in or out of the window
	Due     []Entry `json:"due"`     // every open step of the owner's due today (DueAhead, day <= today)
	Soon    []Entry `json:"soon"`    // "Do soon": DueAhead's later days — a repeat's step before its day in its own week
	Days    []Day   `json:"days"`
}

// Item kinds. `homework` (2026-09-11) is a step of the OWNER's that comes from
// Learn — a daily practice round, say, more as learning goals are handed over
// — and the hub treats it exactly as an `owner` item (a physical ask on the
// day, nagged, closed with words, stacked in For you); the kind exists so
// both surfaces can draw it as its own light-blue lane instead of a red task.
// `ownerKind(kind)` is the one test for "a step the owner takes". What differs: a
// homework item is a COMPLETION, not a conversation — the owner ticks it (no
// note; Resolve), or the hub closes it by `source` when the evidence arrives
// (CloseBySource; e.g. a practice site reporting the round). Any later kind
// of homework is the same shape: kind `homework`, a `source` that names what
// closes it, and the tick as the fallback.
var kinds = map[string]bool{"owner": true, "homework": true, "agent": true, "note": true}

const kindsHelp = "kind must be owner|homework|agent|note"

func ownerKind(kind string) bool { return kind == "owner" || kind == "homework" }

// overdueItem: one rule for the red word on a row and for the tray behind it —
// a step of the owner's, still open, whose moment has gone: any day that has
// passed, or a timed step from earlier TODAY (an unanswered step stays open —
// the tray must show what is slipping as the day goes, not only from
// tomorrow morning). `scheduled` counts: an all-day item only fires at
// DefaultHour, so a step from yesterday that the tick never reached is still
// a step not taken.
// A today item with no time is not overdue — the day is not over.
func overdueItem(it Item, today, hhmm string) bool {
	// An on-the-day step is never owed after its day: midnight closes it as
	// missed (missDay), and inside its day it is Due, not Overdue.
	if !ownerKind(it.Kind) || (it.State != "scheduled" && it.State != "fired") || it.Day == "" || it.Due == "on" {
		return false
	}
	return it.Day < today || (it.Day == today && it.At != "" && it.At <= hhmm)
}

// validRepeat: ” or a stride cadence — daily/weekly/monthly/yearly or
// "every2d" (a check-in every 2 days). The
// grammar is internal/cadence's (one clock, 2026-08-28).
func validRepeat(repeat string) bool { return repeat == "" || cadence.Kind(repeat) == "stride" }

// Notifier: how the nag reaches the owner — the scheduler, through notify.Card
// (its card lane when it has one, else the NEEDS YOU push).
type Notifier = notify.Needer

type Calendar struct {
	db  *store.DB
	thr *threads.Manager
	Nfy Notifier
	// DefaultHour: when an all-day item fires (ask raised / agent run). 08:00.
	DefaultHour int
	// DefaultNag for kind=owner items: minutes between reminders once fired
	// (no answer means another notification).
	DefaultNag int
	// MaxNags caps the reminders per item.
	MaxNags int
	// FallbackThread: where asks land when the item has no thread (created on demand).
	FallbackThread string
	// Sources: the other dated objects the agenda carries (actions, deferred
	// recs). Optional; set from cmd/hub after New. A source that errors is
	// logged and skipped, never fatal to the view.
	Sources []DatedSource
	// The daily budget guard on an agent item's wake lives with the clock
	// (threads.Manager.Allow, kind "cal"): refused, the prompt stays queued
	// and the next tick asks again.
	Now func() time.Time
	mu  sync.Mutex
	// spawnMu: spawnNext's check-then-insert (see there). Never held across a
	// call that can take mu.
	spawnMu sync.Mutex
	// firing: items whose ask is being raised outside mu (fire); a second
	// tick skips them.
	firing map[string]bool
	// backfilled: the first tick gave every agent item from before the clock
	// its prompt row.
	backfilled bool
}

func New(db *store.DB, thr *threads.Manager) (*Calendar, error) {
	if _, err := db.Migrate("items", store.ItemsSchema); err != nil {
		return nil, err
	}
	if _, err := db.Migrate("calendar", Schema); err != nil {
		return nil, err
	}
	c := &Calendar{db: db, thr: thr, DefaultHour: 8, DefaultNag: 6 * 60, MaxNags: 6, FallbackThread: fallbackThread, Now: time.Now}
	c.backfillDue()
	thr.OnDelivered = c.OnDelivered
	thr.ClaimCal = c.claim
	thr.CalHeader = c.header
	thr.ResolveCal = c.resolveCard
	c.unprefixDatedAsks()
	return c, nil
}

// unprefixDatedAsks takes the "Calendar (<when>): " the fire used to stamp
// off the open cards that sessions dated (2026-09-25: a dated ask is the
// session's card, in its own words). Idempotent: a stripped row no longer
// matches.
func (c *Calendar) unprefixDatedAsks() {
	c.db.Exec(`UPDATE items SET detail = substr(detail, instr(detail, '): ') + 3)
		WHERE src='ask' AND state IN ('open','answered') AND detail LIKE 'Calendar (%): %'
		AND id IN (SELECT ask_id FROM items WHERE src='cal' AND ask_kind != '' AND ask_id IS NOT NULL)`)
}

// resolveCard is threads.Manager.ResolveCal: a fired step's card answered as
// an ask (`lifectl ask cal-… done`, a swipe). The card's `open` is the step's
// reopen. relay: the ResolveAsk road — the calendar's own rule decides who
// hears it (the owner's words to the session behind a step); else quiet, like claim.
func (c *Calendar) resolveCard(id, state, by, note string, relay bool) error {
	if state == "open" {
		state = "scheduled"
	}
	if relay {
		_, err := c.Resolve(id, state, by, note)
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return err
	}
	if it.State != state {
		c.resolveLocked(it, state, by, note, true)
	}
	return nil
}

func ts(t time.Time) string { return store.TS(t) }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func newID() string { return store.NewID("cal") }

// Add creates an item. day = YYYY-MM-DD (local), at = HH:MM or "".
func (c *Calendar) Add(it Item) (Item, error) {
	it.Title = strings.TrimSpace(it.Title)
	if it.Title == "" {
		return Item{}, errors.New("title required")
	}
	if len(it.Title) > 200 {
		it.Title = it.Title[:200]
	}
	if it.Kind == "" {
		it.Kind = "owner"
	}
	if !kinds[it.Kind] {
		return Item{}, errors.New(kindsHelp)
	}
	soon := it.Day == "" && it.Soon
	if soon {
		if it.Kind != "owner" {
			return Item{}, errors.New("only an owner item can be soon (no day): a " + it.Kind + " item needs a day")
		}
		if it.At != "" || it.Repeat != "" {
			return Item{}, errors.New("a soon item has no day, so no time and no repeat")
		}
		// No session owns a to-do (it is an any-time calendar row, not a
		// session's card); `source` still says who filed it.
		it.ThreadID = ""
	} else if _, err := time.ParseInLocation("2006-01-02", it.Day, time.Local); err != nil {
		return Item{}, errors.New("day must be YYYY-MM-DD (or soon: true for a to-do with no due day)")
	}
	if it.At != "" {
		if _, err := time.Parse("15:04", it.At); err != nil {
			return Item{}, errors.New("at must be HH:MM")
		}
	}
	if !validRepeat(it.Repeat) {
		return Item{}, errors.New("repeat must be daily|weekly|monthly|yearly|every<N>d or empty")
	}
	if it.ThreadID != "" {
		if _, err := c.thr.Get(it.ThreadID); err != nil {
			return Item{}, fmt.Errorf("unknown thread %q", it.ThreadID)
		}
	}
	if it.NagMin < 0 {
		it.NagMin = 0
	}
	if it.Kind == "agent" && it.At == "" {
		it.At = c.agentSlot(it.Day, "")
	}
	if it.Due != "" && it.Due != "on" && it.Due != "by" {
		return Item{}, errors.New("due must be on (its day only) or by (any time up to its day)")
	}
	it.Due = dueOf(it)
	if it.NagMin == 0 && ownerKind(it.Kind) {
		it.NagMin = c.DefaultNag
	}
	// A practice is one reminder, never a nag: it fires, it pushes once, and
	// midnight (or the next occurrence) closes it as missed (a practice missed
	// on Tuesday is not done twice on Wednesday). An on-the-day chore is the
	// same (Water the plants).
	if !ownerKind(it.Kind) || it.Kind == "homework" || it.Due == "on" || soon {
		it.NagMin = 0
	}
	if it.Source == "" {
		it.Source = "owner"
	}
	// One item per thing per day: the same title and kind already open on that
	// day is a duplicate, whichever session files it: only one reaches the
	// owner.
	var dup string
	if c.db.QueryRow(`SELECT id FROM items WHERE src='cal' AND state IN ('scheduled','open') AND kind=? AND day=? AND lower(title)=lower(?) LIMIT 1`,
		it.Kind, it.Day, it.Title).Scan(&dup) == nil {
		return Item{}, fmt.Errorf("duplicate of %s (same title, kind and day, still open): edit that one with `lifectl cal %s set` instead", dup, dup)
	}
	now := c.Now()
	it.ID, it.CreatedAt, it.UpdatedAt, it.State = newID(), now, now, "scheduled"
	_, err := c.setItem(it.ID, it.Source, "", `INSERT INTO items (id,src,created_at,updated_at,title,detail,kind,day,at,repeat,goal_id,thread_id,source,state,nag_min,prev_id,ask_kind,check_hint,surface,say,due)
		VALUES (?,'cal',?,?,?,?,?,?,?,?,?,?,?,'scheduled',?,?,?,?,?,?,?)`,
		it.ID, ts(now), ts(now), it.Title, strings.TrimSpace(it.Detail), it.Kind, it.Day, it.At, it.Repeat, nullable(it.GoalID), nullable(it.ThreadID), it.Source, it.NagMin, nullable(it.PrevID), it.AskKind, it.CheckHint, it.Surface, strings.TrimSpace(it.Say), it.Due)
	if err != nil {
		return Item{}, err
	}
	log.Printf("calendar: %s %s on %s %s: %s", it.ID, it.Kind, it.Day, it.At, it.Title)
	if it.Kind == "agent" {
		c.queueWake(it)
	}
	return c.Get(it.ID)
}

// agentSlot: the minute an agent item with no time of its own will run. An
// agent run is never "all day" — the hub always fires it at one minute, and
// that minute is the honest thing to draw, not "all day". Start at DefaultHour and step 30 minutes past every other open agent run on
// that day (the "space out agent runs" rule, 2026-09-03), so two sessions
// dated for the same morning do not wake on the same minute. `skip` is the
// item's own id when it is being moved.
func (c *Calendar) agentSlot(day, skip string) string {
	taken := map[string]bool{}
	rows, err := c.db.Query(`SELECT at FROM items WHERE src='cal' AND kind='agent' AND day=? AND at<>'' AND state IN ('scheduled','open') AND id<>?`, day, skip)
	if err == nil {
		for rows.Next() {
			var at string
			if rows.Scan(&at) == nil {
				taken[at] = true
			}
		}
		rows.Close()
	}
	mins := c.DefaultHour * 60
	for taken[hhmm(mins)] && mins+30 < 24*60 {
		mins += 30
	}
	return hhmm(mins)
}

func hhmm(mins int) string { return fmt.Sprintf("%02d:%02d", mins/60, mins%60) }

// queueWake makes an agent item's wake a prompt row: author hub, bound for
// the item's session if it is still there (else a fresh one titled by the
// item), due at the item's fire time, referencing the item so the session
// reads "[This is calendar item … coming due …]" and the delivery closes
// it. Any earlier wake of the item is cancelled first, so a moved item has
// one. A wake whose time has passed is delivered on the spot (threads.Queue).
func (c *Calendar) queueWake(it Item) {
	if it.PromptID != "" {
		if p, err := c.thr.GetPrompt(it.PromptID); err == nil && p.State == "queued" {
			c.thr.CancelPrompt(it.PromptID)
		}
	}
	target := "new"
	if it.ThreadID != "" {
		target = "new-or:" + it.ThreadID
	}
	text := it.Title
	if it.Detail != "" {
		text += "\n\n" + it.Detail
	}
	p, err := c.thr.Queue(threads.Prompt{Author: "hub", Target: target, NotBefore: c.fireAt(it), InReplyTo: "cal:" + it.ID,
		Text: text, Title: it.Title, GoalID: it.GoalID})
	if err != nil {
		log.Printf("calendar: %s wake: %v", it.ID, err)
		return
	}
	c.db.Exec(`UPDATE items SET prompt_id=? WHERE id=?`, p.ID, it.ID)
}

// cancelWake drops an item's queued wake (it was closed or moved).
func (c *Calendar) cancelWake(it Item) {
	if it.PromptID == "" {
		return
	}
	if p, err := c.thr.GetPrompt(it.PromptID); err == nil && p.State == "queued" {
		c.thr.CancelPrompt(it.PromptID)
	}
}

// OnDelivered is the clock telling the calendar an item's wake reached a
// session: the item is done ("agent run started"), records which session
// took it, and a repeating one spawns its next occurrence. Wired by New as
// threads.Manager.OnDelivered; a prompt about anything else is ignored. Runs
// inside threads.Queue/DuePrompts, so it takes no calendar lock (Add may be
// queueing under one): the close is a conditional write instead (only a row
// still `scheduled` flips, and only the caller that flipped it spawns the next
// occurrence), and spawnNext serialises its own check-then-insert.
func (c *Calendar) OnDelivered(p threads.Prompt, threadID string) {
	kind, id := store.SplitRef(p.InReplyTo)
	if kind != "cal" {
		return
	}
	it, err := c.Get(id)
	if err != nil || it.State != "scheduled" {
		return
	}
	// ONLY an agent item, because only an agent item has a wake (queueWake is
	// `Kind == "agent"`). A prompt pointed at one of the OWNER's steps is the
	// owner talking about it — the Respond sheet's "Reply" — and answering a
	// step is not doing it: closing the row here would mark a step done the
	// moment a note about it was delivered. The owner's step closes through
	// Resolve or ClaimCal, with words, or not at all.
	if it.Kind != "agent" {
		return
	}
	now := c.Now()
	moved, err := c.setItem(it.ID, "hub", "agent run started", `UPDATE items SET state='done', fired_at=?, updated_at=?, resolved_at=?, resolved_by='hub', resolution='agent run started', thread_id=?, prompt_id=? WHERE id=? AND state='scheduled'`,
		ts(now), ts(now), ts(now), threadID, p.ID, it.ID)
	if err != nil {
		log.Printf("calendar: close %s after delivery: %v", it.ID, err)
		return
	}
	if !moved {
		return // closed or fired by someone else meanwhile; they own the next occurrence
	}
	log.Printf("calendar: fired %s (agent) %s → %s", it.ID, it.Title, threadID)
	if it.Repeat != "" {
		c.spawnNext(it)
	}
}

// backfill gives every agent item from before the clock its prompt row —
// once, on the first tick; idempotent (prompt_id marks the ones done). Since
// 2026-09-13 it also gives every open agent item that has no minute the one
// it will run at (agentSlot) and requeues its wake — idempotent too, since a
// stamped row is no longer timeless.
func (c *Calendar) backfill() {
	c.stampAgentSlots()
	rows, err := c.db.Query(`SELECT ` + cols + ` FROM items WHERE src='cal' AND kind='agent' AND state='scheduled' AND prompt_id='' ORDER BY day, at`)
	if err != nil {
		return
	}
	var items []Item
	for rows.Next() {
		if it, err := scanItem(rows); err == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		c.queueWake(it)
	}
	if len(items) > 0 {
		log.Printf("calendar: %d agent items given their wake", len(items))
	}
}

func (c *Calendar) stampAgentSlots() {
	rows, err := c.db.Query(`SELECT ` + cols + ` FROM items WHERE src='cal' AND kind='agent' AND at='' AND state IN ('scheduled','open') ORDER BY day, created_at`)
	if err != nil {
		return
	}
	var items []Item
	for rows.Next() {
		if it, err := scanItem(rows); err == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		slot := c.agentSlot(it.Day, it.ID)
		if _, err := c.db.Exec(`UPDATE items SET at=?, updated_at=? WHERE id=?`, slot, ts(c.Now()), it.ID); err != nil {
			continue
		}
		it.At = slot
		if it.State == "scheduled" {
			c.queueWake(it)
		}
	}
	// Closed rows from before are the record: a done one ran at DefaultHour,
	// a dismissed one was planned for it — so that is the minute they get,
	// unstaggered and with no wake, and the all-day band holds only the owner's.
	if r, err := c.db.Exec(`UPDATE items SET at=? WHERE src='cal' AND kind='agent' AND at='' AND state NOT IN ('scheduled','open')`, hhmm(c.DefaultHour*60)); err == nil {
		if n, _ := r.RowsAffected(); n > 0 {
			log.Printf("calendar: %d closed timeless agent items stamped %02d:00", n, c.DefaultHour)
		}
	}
	if len(items) > 0 {
		log.Printf("calendar: %d timeless agent items stamped with the minute they run", len(items))
	}
}

// Update edits a scheduled item in place: title, detail, day, at, repeat.
// A recurrence carries its shape forward, so the only way to move a standing
// check-in used to be dismissing it — and dismissing spawns the next
// occurrence, so the chain never actually moved. Nil fields are left alone.
//
// Two rules from the drag (events drag to a new time):
//   - A FIRED item moves too: dragging it is "not now, then" — it goes back
//     to scheduled, its open ask leaves the board ("rescheduled"), and the
//     tick fires it again at the new moment. Done/dismissed still refuse
//     (reopen first), and a fired item's other fields stay locked — but
//     for `detail`: the words on the owner's open card are the item's, and
//     a session folds what a second card would have said into them, never
//     a read card stacked beside the step. A detail edit moves nothing.
//   - `scope` on a repeating item: "future" (default) moves the chain —
//     including the already-spawned next occurrence, since fire() mints it
//     at fire time — while "one" detaches this occurrence: the next one is
//     minted NOW from the original shape and the moved row stops repeating.
func (c *Calendar) Update(id string, in map[string]string) (Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return Item{}, errors.New("no such item")
	}
	moveBy, moveWhy := "owner", "" // what cal_events says if the edit moves the state
	scope := strings.TrimSpace(in["scope"])
	if scope != "" && scope != "one" && scope != "future" {
		return Item{}, errors.New("scope must be one|future")
	}
	if _, ok := in["repeat"]; ok && scope == "one" {
		return Item{}, errors.New("scope=one detaches this occurrence — it cannot also set repeat")
	}
	_, hasDay := in["day"]
	_, hasAt := in["at"]
	// Soon (no day) is a value `day` can take: `day: ""` turns an open step of
	// the owner's into a to-do — it leaves its session, and the ask it had raised
	// leaves that session's card (closed by the hub, nobody woken) — and any
	// day gives a to-do a date again. A to-do moved by its time alone (the
	// phone's drag) lands on the day it was drawn on.
	toSoon := false
	if hasDay && strings.TrimSpace(in["day"]) == "" {
		if it.Day == "" && it.AskID == "" && it.ThreadID == "" {
			delete(in, "day")
			hasDay = false
		} else {
			// (A to-do from the first hours of 2026-09-21 that still holds an
			// ask or a session is detached by asking again.)
			toSoon = true
		}
	}
	if it.Day == "" && hasAt && !hasDay {
		if strings.TrimSpace(in["at"]) == "" {
			delete(in, "at")
			hasAt = false
		} else {
			in["day"], _ = it.Slot()
			hasDay = true
		}
	}
	if toSoon {
		if strings.TrimSpace(in["at"]) != "" {
			return Item{}, errors.New("a soon item has no day, so no time")
		}
		if it.Repeat != "" {
			return Item{}, errors.New(`a repeating item cannot be soon — stop the repeat first (repeat: "")`)
		}
		in["at"], hasAt = "", true
	}
	moving := hasDay || hasAt
	_, hasDetail := in["detail"]
	resched := it.State == "fired" && moving && !toSoon
	if (it.State != "scheduled" && it.State != "fired") || (it.State == "fired" && !moving && !hasDetail) {
		return Item{}, fmt.Errorf("item is %s, not scheduled", it.State)
	}
	if it.State == "fired" {
		for _, f := range []string{"title", "repeat", "kind", "due"} {
			if _, ok := in[f]; ok {
				return Item{}, errors.New("a fired item only moves (day and at) or takes new detail; reopen it to edit the rest")
			}
		}
	}
	set, args := []string{}, []any{}
	for _, f := range []string{"title", "detail", "day", "at", "repeat", "kind", "due"} {
		v, ok := in[f]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch f {
		case "title":
			if v == "" {
				return Item{}, errors.New("title required")
			}
			if len(v) > 200 {
				v = v[:200]
			}
		case "day":
			if _, err := time.ParseInLocation("2006-01-02", v, time.Local); err != nil && !toSoon {
				return Item{}, errors.New(`day must be YYYY-MM-DD (or "" for soon)`)
			}
		case "at":
			if v != "" {
				if _, err := time.Parse("15:04", v); err != nil {
					return Item{}, errors.New("at must be HH:MM")
				}
			}
		case "repeat":
			if !validRepeat(v) {
				return Item{}, errors.New("repeat must be daily|weekly|monthly|yearly|every<N>d or empty")
			}
		case "kind":
			if !kinds[v] {
				return Item{}, errors.New(kindsHelp)
			}
		case "due":
			if v != "on" && v != "by" {
				return Item{}, errors.New("due must be on (its day only) or by (any time up to its day)")
			}
		}
		set, args = append(set, f+"=?"), append(args, v)
	}
	if len(set) == 0 {
		return it, nil
	}
	// An agent run keeps a minute whatever the edit did to it: a drop on the
	// all-day band, a Google event made all-day, or an `owner` item retagged
	// `agent` without a time (agentSlot says why).
	kindAfter, dayAfter, atAfter := it.Kind, it.Day, it.At
	if v, ok := in["kind"]; ok {
		kindAfter = strings.TrimSpace(v)
	}
	if v, ok := in["day"]; ok {
		dayAfter = strings.TrimSpace(v)
	}
	if v, ok := in["at"]; ok {
		atAfter = strings.TrimSpace(v)
	}
	if dayAfter == "" {
		repeatAfter := it.Repeat
		if v, ok := in["repeat"]; ok {
			repeatAfter = strings.TrimSpace(v)
		}
		if kindAfter != "owner" || repeatAfter != "" {
			return Item{}, errors.New("only an owner item with no repeat can be soon (no day)")
		}
		set, args = append(set, "nag_min=?"), append(args, 0)
	} else if it.Day == "" {
		// A to-do that gains a day is a dated step again: it fires and nags.
		set, args = append(set, "nag_min=?"), append(args, c.DefaultNag)
	}
	if kindAfter == "agent" && atAfter == "" {
		slot := c.agentSlot(dayAfter, id)
		if hasAt {
			for i, f := range set {
				if f == "at=?" {
					args[i] = slot
				}
			}
		} else {
			set, args = append(set, "at=?"), append(args, slot)
			hasAt = true
		}
		in["at"], moving = slot, true
	}
	now := c.Now()
	if it.Repeat != "" && moving {
		if scope == "one" {
			// The chain continues from the ORIGINAL shape (spawnNext refuses
			// a second child, so a fired item's already-minted next stands),
			// and the moved row stops repeating — closing it later cannot
			// fork the chain.
			c.spawnNext(it)
			set, args = append(set, "repeat=?"), append(args, "")
		} else if hasAt {
			// "future": fire() spawns the next occurrence at fire time, so a
			// fired item's chain already carries the OLD time — the new one
			// has to reach the scheduled child too, or "every future one"
			// would move exactly one.
			c.db.Exec(`UPDATE items SET at=?, updated_at=? WHERE src='cal' AND prev_id=? AND state='scheduled'`,
				strings.TrimSpace(in["at"]), ts(now), it.ID)
			var childID string
			if c.db.QueryRow(`SELECT id FROM items WHERE src='cal' AND prev_id=? AND state='scheduled'`, it.ID).Scan(&childID) == nil {
				if child, err := c.Get(childID); err == nil && child.Kind == "agent" {
					c.queueWake(child)
				}
			}
		}
	}
	if resched {
		when := it.Day
		if hasDay {
			when = strings.TrimSpace(in["day"])
		}
		if at := strings.TrimSpace(in["at"]); hasAt && at != "" {
			when += " " + at
		} else if !hasAt && it.At != "" {
			when += " " + it.At
		}
		if ask := it.legacyAsk(); ask != "" {
			if a, err := c.thr.GetAsk(ask); err == nil && a.Active() {
				c.thr.ResolveAsk(ask, "dismissed", "owner", "rescheduled to "+when)
			}
		}
		set = append(set, "state='scheduled'", "fired_at=NULL", "ask_id=NULL", "nag_count=0", "last_nag_at=NULL")
		moveBy, moveWhy = "owner", "rescheduled to "+when
	}
	if toSoon {
		moveBy, moveWhy = "hub", "now a do-soon to-do on the calendar"
		// The to-do is the calendar's alone now: its ask comes off the session
		// (by the hub, so no prompt is queued and no "Skipped" record is
		// written in the owner's name) and the item lets go of the session.
		if ask := it.legacyAsk(); ask != "" {
			if a, err := c.thr.GetAsk(ask); err == nil && a.Active() {
				c.thr.ResolveAsk(ask, "dismissed", "hub", "now a do-soon to-do on the calendar")
			}
		}
		set = append(set, "state='scheduled'", "fired_at=NULL", "ask_id=NULL", "thread_id=NULL", "nag_count=0", "last_nag_at=NULL")
	}
	set, args = append(set, "updated_at=?"), append(args, ts(now))
	args = append(args, id)
	// An edit is logged only when it moved the state (a fired step rescheduled
	// or made a to-do goes back to scheduled).
	if _, err := c.setItem(id, moveBy, moveWhy, `UPDATE items SET `+strings.Join(set, ",")+` WHERE id=?`, args...); err != nil {
		return Item{}, err
	}
	log.Printf("calendar: %s edited (%s)", id, strings.Join(set, ","))
	// An agent item's wake follows the edit: new time, new words. Kind is
	// editable because Google Calendar's UI has exactly one gesture for it —
	// drag the event between "Life — me" and "Life — agents" — so an item
	// that gains `agent` gets a wake and one that loses it has its cancelled.
	up, err := c.Get(id)
	if err != nil {
		return Item{}, err
	}
	switch {
	case up.Kind == "agent":
		c.queueWake(up)
	case it.Kind == "agent":
		c.cancelWake(up)
	}
	// The nag follows the kind: a step takes the default, a practice none
	// (one reminder, the next occurrence closes it — Add says why), anything
	// else never nagged. Retagging a juice reminder from `owner` to
	// `homework` must not leave it pushing "Did this get done?" all day.
	// The window follows too: a row that stopped being a dated step of the
	// owner's has none, one that became one gets the default (dueOf).
	if d := dueOf(up); d != up.Due {
		c.edit(id, `UPDATE items SET due=? WHERE id=?`, d, id)
		up.Due = d
	}
	if (up.Kind != it.Kind || up.Due != it.Due) && up.Day != "" {
		nag := 0
		if up.Kind == "owner" && up.Due == "by" {
			nag = c.DefaultNag
		}
		c.db.Exec(`UPDATE items SET nag_min=? WHERE id=?`, nag, id)
	}
	return c.Get(id)
}

// cols: an item's columns over `items` (src='cal'). A fired item is `open`
// in the one table and `fired` to every reader of the calendar's own words.
const cols = `id,created_at,updated_at,title,detail,kind,day,at,repeat,goal_id,thread_id,source,CASE state WHEN 'open' THEN 'fired' ELSE state END,nag_min,nag_count,last_nag_at,fired_at,ask_id,prev_id,resolved_at,resolved_by,resolution,ask_kind,check_hint,surface,prompt_id,say,due,win`

// dbState: a calendar state as the one table stores it (`fired` is `open`).
func dbState(s string) string {
	if s == "fired" {
		return "open"
	}
	return s
}

type scanner interface{ Scan(...any) error }

func scanItem(r scanner) (Item, error) {
	var it Item
	var cr, up string
	var goal, thread, lastNag, fired, ask, prev, rAt, rBy sql.NullString
	if err := r.Scan(&it.ID, &cr, &up, &it.Title, &it.Detail, &it.Kind, &it.Day, &it.At, &it.Repeat, &goal, &thread, &it.Source, &it.State, &it.NagMin, &it.NagCount, &lastNag, &fired, &ask, &prev, &rAt, &rBy, &it.Resolution, &it.AskKind, &it.CheckHint, &it.Surface, &it.PromptID, &it.Say, &it.Due, &it.Window); err != nil {
		return it, err
	}
	it.CreatedAt, _ = time.Parse(time.RFC3339Nano, cr)
	it.UpdatedAt, _ = time.Parse(time.RFC3339Nano, up)
	it.GoalID, it.ThreadID, it.AskID, it.PrevID, it.ResolvedBy = goal.String, thread.String, ask.String, prev.String, rBy.String
	pt := func(s sql.NullString) *time.Time {
		if !s.Valid {
			return nil
		}
		t, err := time.Parse(time.RFC3339Nano, s.String)
		if err != nil {
			return nil
		}
		return &t
	}
	it.LastNagAt, it.FiredAt, it.ResolvedAt = pt(lastNag), pt(fired), pt(rAt)
	it.Soon = it.Day == ""
	// A step that has fired IS its card (threads askIs): it is resolved
	// through its own id, the way a minted ask used to be through its.
	if it.AskID == "" && it.FiredAt != nil && it.Kind != "agent" && it.State != "scheduled" {
		it.AskID = it.ID
	}
	return it, nil
}

// legacyAsk: the separate ask a step fired before 2026-09-27 minted, if any
// — the one card the calendar still has to close alongside its row.
func (it Item) legacyAsk() string {
	if it.AskID == it.ID {
		return ""
	}
	return it.AskID
}

func (c *Calendar) Get(id string) (Item, error) {
	return scanItem(c.db.QueryRow(`SELECT `+cols+` FROM items WHERE id=? AND src='cal'`, id))
}

// List items with day in [from,to] (inclusive; "" = unbounded). state "" = all.
// A soon item has no day of its own, so it lists by its Slot — the local day
// it was added, or closed — which no SQL over a UTC stamp can say; there are
// only ever a handful, so they are read whole and placed here.
func (c *Calendar) List(from, to, state string) ([]Item, error) {
	out, err := c.listDated(from, to, state)
	if err != nil {
		return nil, err
	}
	q, args := `SELECT `+cols+` FROM items WHERE src='cal' AND day=''`, []any{}
	if state != "" && state != "all" {
		q, args = q+` AND state=?`, append(args, dbState(state))
	}
	rows, err := c.db.Query(q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := len(out)
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		if day, _ := it.Slot(); (from == "" || day >= from) && (to == "" || day <= to) {
			out = append(out, it)
		}
	}
	if len(out) > n {
		sort.SliceStable(out, func(i, j int) bool {
			di, ai := out[i].Slot()
			dj, aj := out[j].Slot()
			if di != dj {
				return di < dj
			}
			return ai < aj
		})
	}
	return out, rows.Err()
}

// Open: the owner's own open items — a step, homework or a note not yet
// closed, dated or not — as rows in day order; this table's share of
// store.Items. Agent items are not here: they wait on a session's clock, not
// on the owner.
func (c *Calendar) Open() ([]Dated, error) {
	if c == nil {
		return nil, nil
	}
	rows, err := c.db.Query(`SELECT ` + cols + ` FROM items WHERE src='cal' AND kind IN ('owner','homework','note') AND state IN ('scheduled','open') ORDER BY day, at, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dated
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, Row(it))
	}
	return out, rows.Err()
}

func (c *Calendar) listDated(from, to, state string) ([]Item, error) {
	where, args := []string{"src='cal'", "day<>''"}, []any{}
	if from != "" {
		where, args = append(where, "day>=?"), append(args, from)
	}
	if to != "" {
		where, args = append(where, "day<=?"), append(args, to)
	}
	if state != "" && state != "all" {
		where, args = append(where, "state=?"), append(args, dbState(state))
	}
	rows, err := c.db.Query(`SELECT `+cols+` FROM items WHERE `+strings.Join(where, " AND ")+` ORDER BY day, at, created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DueCount: the owner's steps whose day has come and that nothing has closed — every
// open `owner` item dated today or earlier, whether or not the tick has fired
// it yet. The Calendar tab's number on both surfaces. It used to count only
// `fired` items, so before 08:00 (DefaultHour) a "buy tranche 2" listed under
// Today showed no number on the tab. Notes and agent runs fire and close
// themselves and are the hub's work, not the owner's, so they never count. Counted over the calendar's share
// of store.Items (Open), not a query of its own.
func (c *Calendar) DueCount() (int, error) {
	rows, err := c.Open()
	if err != nil {
		return 0, err
	}
	today, n := c.Now().Format("2006-01-02"), 0
	for _, r := range rows {
		it, ok := r.Obj.(Item)
		if ok && r.Kind != "note" && it.Day != "" && it.Day <= today {
			n++
		}
	}
	return n, nil
}

// Overdue: every open `owner` item whose moment has passed — a past day, fired
// or not, or a timed step from earlier today — oldest first (`overdueItem` is
// the same rule per row). It is the "For you" pile both surfaces stack behind
// the tray: undone steps from earlier days stack up in the inbox, and an
// unanswered timed step from today stays open. Deliberately NOT
// limited to the window the agenda was asked for: a backlog that only appears
// once you scroll past it is not a backlog. Notes and agent runs close
// themselves, so they never stack.
func (c *Calendar) Overdue() ([]Item, error) {
	now := c.Now()
	rows, err := c.db.Query(`SELECT `+cols+` FROM items
		WHERE src='cal' AND kind IN ('owner','homework') AND state IN ('scheduled','open') AND day<>'' AND due<>'on'
		  AND (day<? OR (day=? AND at<>'' AND at<=?))
		ORDER BY day, at, created_at`,
		now.Format("2006-01-02"), now.Format("2006-01-02"), now.Format("15:04"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DueAhead: every open step of the owner's (owner, homework) that is due and
// has not slipped into Overdue — today's, and a repeating one's from the day
// its period opens, capped at a week (an assignment due every Saturday may be
// done on Thursday, but must be done by Saturday). Weekly homework therefore shows the whole week before its day;
// daily homework, and every `owner` step, on its day only — a weekly chore
// ("Water the plants") is a day, not a deadline, and a week of them ahead
// would bury today. Sorted by day, then minute.
//
// A repeat lists its NEXT occurrence only — never under Due and Do soon at
// once, unless earlier ones are overdue: a later open row of the same
// chain waits its turn — it keeps its cell on its day — until the one before
// it closes or slips into Overdue, where a by-the-day chain's debt stacks.
func (c *Calendar) DueAhead() ([]Item, error) {
	now := c.Now()
	today := now.Format("2006-01-02")
	day0, _ := time.ParseInLocation("2006-01-02", today, time.Local)
	rows, err := c.db.Query(`SELECT `+cols+` FROM items
		WHERE src='cal' AND kind IN ('owner','homework') AND state IN ('scheduled','open') AND day>=? AND day<=?
		  AND NOT (due<>'on' AND day=? AND at<>'' AND at<=?)
		ORDER BY day, at, created_at`,
		today, day0.AddDate(0, 0, 6).Format("2006-01-02"), today, now.Format("15:04"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	// Chains already listed, and whether a listed row repeats: an occurrence
	// moved on its own ("only this one") has lost its repeat, its next has not.
	listed := map[string]string{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		if d, err := time.ParseInLocation("2006-01-02", it.Day, time.Local); err == nil && daysBetween(day0, d) < leadDays(it, d) {
			k := chainKey(it)
			if first, ok := listed[k]; ok && (it.Repeat != "" || first != "") {
				continue
			}
			if _, ok := listed[k]; !ok {
				listed[k] = it.Repeat
			}
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

// chainKey: the rows of one repeat chain — the same kind and title, which is
// how spawnNext tells that a chain already continues.
func chainKey(it Item) string { return it.Kind + "|" + strings.ToLower(it.Title) }

// leadDays: how many days before its day a step is already due — for a
// repeating by-the-day step its period (one occurrence is issued when the
// last one closes), at most a week; 1 (its own day) otherwise. An on-the-day
// step (Water the plants) is owed on its day, not all week.
func leadDays(it Item, day time.Time) int {
	if it.Due != "by" || it.Repeat == "" {
		return 1
	}
	n := daysBetween(day, cadence.Step(it.Repeat, day))
	if n < 1 {
		return 1
	}
	if n > 7 {
		return 7
	}
	return n
}

func daysBetween(a, b time.Time) int { return int(math.Round(b.Sub(a).Hours() / 24)) }

// OpenBySource finds the scheduled item a caller minted under `source`
// ("hub:rec:<id>"), so a second deferral moves the check-in instead of
// adding another. ok=false when there is none.
func (c *Calendar) OpenBySource(source string) (Item, bool) {
	it, err := scanItem(c.db.QueryRow(`SELECT `+cols+` FROM items WHERE src='cal' AND source=? AND state='scheduled' ORDER BY created_at DESC LIMIT 1`, source))
	return it, err == nil
}

// ChainBySource: what a paced homework chain looks like right now — its
// repeat rule, the earliest still-open item's day ("" when none) and when
// the newest one closed done (Learn's due line, learn.PlanHomework).
func (c *Calendar) ChainBySource(source string) (repeat, openDay string, lastDone *time.Time) {
	var r, d sql.NullString
	if c.db.QueryRow(`SELECT repeat, day FROM items WHERE src='cal' AND source=? AND state IN ('scheduled','open') ORDER BY day LIMIT 1`, source).Scan(&r, &d) == nil {
		repeat, openDay = r.String, d.String
	}
	var at sql.NullString
	if c.db.QueryRow(`SELECT repeat, resolved_at FROM items WHERE src='cal' AND source=? AND state='done' AND resolved_at IS NOT NULL ORDER BY resolved_at DESC LIMIT 1`, source).Scan(&r, &at) == nil {
		if repeat == "" {
			repeat = r.String
		}
		if t, err := time.Parse(time.RFC3339Nano, at.String); err == nil {
			lastDone = &t
		}
	}
	return
}

// CloseBySource closes every still-open item minted under `source` (today's
// scheduled one and any fired one never answered) as done, by the hub, with
// `note` as the evidence. A daily practice homework uses it: a finished round
// IS the answer, so the day's card clears itself instead of asking the owner
// to say in words that they did the thing the hub just recorded.
// Returns how many closed.
//
// A by-the-day chain carries debt (2026-10-04), so there one piece of evidence
// pays ONE assignment, the oldest owed — a video watched with two weeks open
// leaves the other open — and it counts from the day the assignment is owed
// (its week), not only on its day.
func (c *Calendar) CloseBySource(source, by, note string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	today := c.Now().Format("2006-01-02")
	day0, _ := time.ParseInLocation("2006-01-02", today, time.Local)
	rows, err := c.db.Query(`SELECT `+cols+` FROM items WHERE src='cal' AND source=? AND state IN ('scheduled','open') AND day<>'' ORDER BY day, at, created_at`, source)
	if err != nil {
		return 0
	}
	var items []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", it.Day, time.Local)
		if err != nil || daysBetween(day0, d) >= leadDays(it, d) {
			continue // not owed yet
		}
		items = append(items, it)
	}
	rows.Close()
	if len(items) > 1 && items[0].Due == "by" {
		items = items[:1]
	}
	for _, it := range items {
		c.resolveLocked(it, "done", by, note, false)
	}
	return len(items)
}

// ErrNoteRequired: the owner closing one of their own steps with nothing said.
var ErrNoteRequired = errors.New("say what happened: a step of yours closes with a note to its session, like a rec (Done needs words; Reopen does not)")

// Resolve: done | dismissed | scheduled (reopen). Closing an item closes its
// ask; reopening one dismisses the ask it had minted (the item will mint a
// fresh one when it fires again) instead of orphaning it on the board.
//
// THE OWNER CLOSING THEIR OWN STEP IS A MESSAGE, NOT A BUTTON: a stray tap
// must not mark a step done, so Done needs words, the way a rec's box works.
// So by=owner on an `owner` item with done|dismissed REQUIRES a note, and the
// close travels as ONE prompt to the item's session (`cal:<id>`, outcome
// done|wont, the owner's words as the text): Queue calls claim, which writes the
// row — the same path a Respond sheet posting that prompt directly takes, so
// both surfaces and lifectl converge on one write. Agents, the verifier and
// the hub still close directly (the ask relay below is theirs). Reopen never
// needs words: that is the undo.
func (c *Calendar) Resolve(id, state, by, note string) (Item, error) {
	c.mu.Lock() // Tick holds the same lock: no fire/followUp mid-resolve
	it, err := c.Get(id)
	if err != nil {
		c.mu.Unlock()
		return it, errors.New("no such item")
	}
	if state != "done" && state != "dismissed" && state != "scheduled" {
		c.mu.Unlock()
		return it, errors.New("state must be done|dismissed|scheduled")
	}
	if it.State == state {
		c.mu.Unlock()
		return it, nil
	}
	// Only a step a session waits on closes with words (and a soon to-do, whose
	// words stay on its row). Homework, and any dated step no session is
	// behind, is a completion — "I did this" — ticked from the row, or closed
	// by the hub when the evidence lands (CloseBySource); it never needs a
	// message to a session.
	if by == "owner" && it.Kind == "owner" && state != "scheduled" && (it.Day == "" || !sessionless(it.Kind, it.ThreadID)) {
		note = strings.TrimSpace(note)
		if note == "" {
			c.mu.Unlock()
			return it, ErrNoteRequired
		}
		// A to-do no session is behind: the owner's words close it and stay on
		// the row. Nothing is woken to hear them.
		if it.Day == "" && it.ThreadID == "" {
			defer c.mu.Unlock()
			c.resolveLocked(it, state, by, note, true)
			return c.Get(id)
		}
		c.mu.Unlock()
		outcome := "done"
		if state == "dismissed" {
			outcome = "wont"
		}
		tid, err := c.threadFor(it)
		if err != nil {
			return it, err
		}
		// new-or: a session that has since ended must not swallow the owner's answer.
		if _, err := c.thr.Queue(threads.Prompt{Author: "owner", Target: "new-or:" + tid, InReplyTo: "cal:" + id, Outcome: outcome, Text: note}); err != nil {
			return it, err
		}
		return c.Get(id)
	}
	defer c.mu.Unlock()
	c.resolveLocked(it, state, by, note, false)
	return c.Get(id)
}

// claim is threads.Manager.ClaimCal: the owner answered a step's card (a
// prompt `cal:<id>` with done|wont), so the item — and the ask it raised —
// close with their words, when they say it, quietly (the prompt is the message).
func (c *Calendar) claim(id, outcome, note string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return
	}
	state := "done"
	if outcome == "wont" {
		state = "dismissed"
	}
	if it.State == state {
		return
	}
	c.resolveLocked(it, state, "owner", note, true)
}

// header is threads.Manager.CalHeader: the frame above the owner's answer to a
// step, from the item's own row.
func (c *Calendar) header(id, outcome string) string {
	it, err := c.Get(id)
	if err != nil {
		return ""
	}
	verdict := "wrote back about"
	switch outcome {
	case "done":
		verdict = "marked DONE (already closed by the hub)"
	case "wont":
		verdict = "marked WON'T DO (already closed by the hub) — they are not doing this; drop it or find another way"
	}
	when := it.Day
	if it.At != "" {
		when += " " + it.At
	}
	if it.Day == "" {
		when = "a do-soon to-do with no due day, added " + it.CreatedAt.Local().Format("2006-01-02")
	}
	// The step's goal and detail ride along: from a homework row the owner's
	// words can start a NEW session, and one that opened knowing only a title
	// had to rediscover what the assignment was.
	var more string
	if it.GoalID != "" {
		more += " Goal: " + it.GoalID + "."
	}
	if d := strings.TrimSpace(it.Detail); d != "" {
		if r := []rune(d); len(r) > 800 {
			d = string(r[:800]) + "…"
		}
		more += " Detail: " + d
	}
	return fmt.Sprintf("[The owner %s calendar step %s %q (%s, kind %s).%s Their words follow — this is their answer about that one step, not a new task; record what it means for the goal and carry on.]\n", verdict, it.ID, it.Title, when, it.Kind, more)
}

// resolveLocked writes a state change. quiet: close the item's ask through
// ClaimAsk (no relay — the caller's prompt is the message) rather than
// ResolveAsk, which would queue a second "Done: <title>" prompt.
func (c *Calendar) resolveLocked(it Item, state, by, note string, quiet bool) {
	id := it.ID
	now := c.Now()
	if state == "scheduled" {
		// The old ask goes quietly (ClaimAsk, no relay): a reopen is the undo
		// of a close, not news for the session — through ResolveAsk by owner
		// it queued a "Won't do" prompt and woke the session for nothing.
		if ask := it.legacyAsk(); ask != "" {
			if a, err := c.thr.GetAsk(ask); err == nil && a.Active() {
				c.thr.ClaimAsk(ask, "wont", "calendar item reopened")
			}
		}
		// The row forgets the close; cal_events keeps it (the done/dismissed
		// move is already on the trail) and appends the reopen.
		c.setItem(id, by, note, `UPDATE items SET state='scheduled', updated_at=?, fired_at=NULL, ask_id=NULL, nag_count=0, last_nag_at=NULL, resolved_at=NULL, resolved_by=NULL, resolution='' WHERE id=?`, ts(now), id)
		// A reopened agent item gets a fresh wake (its old one was delivered
		// or cancelled).
		if it.Kind == "agent" {
			if re, err := c.Get(id); err == nil {
				c.queueWake(re)
			}
		}
	} else {
		c.setItem(id, by, note, `UPDATE items SET state=?, updated_at=?, resolved_at=?, resolved_by=?, resolution=? WHERE id=?`, state, ts(now), ts(now), by, strings.TrimSpace(note), id)
		if ask := it.legacyAsk(); ask != "" {
			if a, err := c.thr.GetAsk(ask); err == nil && a.Active() {
				if quiet {
					outcome := "done"
					if state == "dismissed" {
						outcome = "wont"
					}
					c.thr.ClaimAsk(ask, outcome, note)
				} else {
					c.thr.ResolveAsk(ask, state, by, note)
				}
			}
		}
		// Closed before its wake: the wake goes with it.
		c.cancelWake(it)
		// Done/dismissed before firing: keep the recurrence alive.
		if it.State == "scheduled" && it.Repeat != "" {
			c.spawnNext(it)
		}
	}
	log.Printf("calendar: %s %s → %s by %s", id, it.State, state, by)
}

// fireAt: the instant an item becomes live.
func (c *Calendar) fireAt(it Item) time.Time {
	d, _ := time.ParseInLocation("2006-01-02", it.Day, time.Local)
	h, m := c.DefaultHour, 0
	if it.At != "" {
		fmt.Sscanf(it.At, "%d:%d", &h, &m)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, time.Local)
}

func nextDay(day, repeat string) string {
	d, _ := time.ParseInLocation("2006-01-02", day, time.Local)
	n := cadence.Step(repeat, d)
	if n.IsZero() {
		return ""
	}
	return n.Format("2006-01-02")
}

// spawnNext mints the next occurrence of a repeating item. It steps from the
// item's own day but never lands before tomorrow: an every2d item that sat
// unfired for ten days gets one next date, not five asks of backlog.
//
// Callers are Tick/Resolve (under c.mu) and OnDelivered (inside the clock,
// no c.mu), so the "does the chain already continue?" check and the insert
// hold spawnMu of their own — two of them racing minted a duplicate
// occurrence. The next occurrence carries everything that shapes its ask
// (surface, ask kind, check hint), not just its title and day.
func (c *Calendar) spawnNext(it Item) {
	nd := nextDay(it.Day, it.Repeat)
	if nd == "" {
		return
	}
	today := c.Now().Format("2006-01-02")
	for nd <= today {
		nd = nextDay(nd, it.Repeat)
	}
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	var n int
	c.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='cal' AND prev_id=?`, it.ID).Scan(&n)
	if n > 0 {
		return
	}
	// Nor when the chain already continues through another open row: closing a
	// stray duplicate occurrence must not fork a second chain (2026-08-30, a
	// session dismissed five duplicates and each one spawned its next month).
	// An EARLIER row that already fired is debt, not the chain's continuation:
	// a by-the-day homework the owner missed stays owed, it minted its
	// next when it fired, and the week after still comes.
	c.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='cal' AND kind=? AND repeat=? AND lower(title)=lower(?) AND id<>? AND (state='scheduled' OR (state='open' AND day>=?))`,
		it.Kind, it.Repeat, it.Title, it.ID, it.Day).Scan(&n)
	if n > 0 {
		return
	}
	next := Item{Title: it.Title, Detail: it.Detail, Kind: it.Kind, Day: nd, At: it.At, Repeat: it.Repeat, GoalID: it.GoalID, ThreadID: it.ThreadID, Source: it.Source, NagMin: it.NagMin, PrevID: it.ID, Due: it.Due,
		Surface: it.Surface, AskKind: it.AskKind, CheckHint: it.CheckHint}
	if _, err := c.Add(next); err != nil {
		log.Printf("calendar: next occurrence of %s: %v", it.ID, err)
	}
}

// Tick fires due owner/note items, nags on fired-but-unfinished ones, and
// mirrors ask closures back. Call every minute. An agent item with a wake is
// the clock's (threads.DuePrompts), not the tick's.
//
// The lock is taken per decision, never across a push: raising an ask pushes
// to the phone (APNs) and a nag is a push, and while Tick held c.mu through
// those an HTTP Resolve from either surface waited on the network. fire and
// followUp re-read the row under the lock and act on what is there then.
func (c *Calendar) Tick() {
	c.mu.Lock()
	if !c.backfilled {
		c.backfilled = true
		c.backfill()
	}
	now := c.Now()
	items, err := c.List("", now.Format("2006-01-02"), "")
	if err == nil {
		c.missDay(items, now)
	}
	c.mu.Unlock()
	if err != nil {
		log.Printf("calendar tick: %v", err)
		return
	}
	today := now.Format("2006-01-02")
	for _, it := range items {
		if it.Due == "on" && it.Day != "" && it.Day < today {
			continue // missDay closed it
		}
		switch it.State {
		case "scheduled":
			// A soon item has no moment to fire at: no ask, no push, no nag.
			if it.Day == "" || (it.Kind == "agent" && it.PromptID != "") {
				continue
			}
			if !now.Before(c.fireAt(it)) {
				c.fire(it, now)
			}
		case "fired":
			c.followUp(it, now)
		}
	}
}

func (c *Calendar) threadFor(it Item) (string, error) {
	if it.ThreadID != "" {
		if _, err := c.thr.Get(it.ThreadID); err == nil {
			return it.ThreadID, nil
		}
	}
	if _, err := c.thr.Get(c.FallbackThread); err == nil {
		return c.FallbackThread, nil
	}
	t, err := c.thr.CreateIdle(c.FallbackThread, "Calendar", "life")
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// fire turns a due item into its card. Called by Tick without c.mu: the row
// is re-read under the lock (Resolve or a drag may have got there first), the
// fired state lands in one conditional write — only if the row is still the
// scheduled one at the same moment — and the card is announced OUTSIDE the
// lock (it pushes). A write that finds the row changed raises nothing.
func (c *Calendar) fire(it Item, now time.Time) {
	c.mu.Lock()
	cur, err := c.Get(it.ID)
	if err != nil || cur.State != "scheduled" || cur.Day == "" || cur.Day != it.Day || cur.At != it.At || c.firing[it.ID] {
		c.mu.Unlock()
		return
	}
	it = cur
	if c.firing == nil {
		c.firing = map[string]bool{}
	}
	c.firing[it.ID] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.firing, it.ID)
		c.mu.Unlock()
	}()
	when := it.Day
	if it.At != "" {
		when += " " + it.At
	}
	switch it.Kind {
	case "agent":
		// An agent item without a wake (its Queue failed, or it predates the
		// clock and the backfill has not run): give it one. It is due, so the
		// clock delivers it now and OnDelivered closes the item.
		c.mu.Lock()
		c.queueWake(it)
		c.mu.Unlock()
		return
	case "owner", "homework", "note":
		// A practice has no host session (a session run per check-in costs
		// too much): its ask lands on the hub's idle calendar thread whatever
		// thread the item names, so nothing is ever woken to host a reminder.
		// A step keeps its session — the owner's words about it go there.
		practice := it.Kind == "homework" || (it.Kind == "owner" && sessionless(it.Kind, it.ThreadID))
		if it.Kind == "homework" {
			it.ThreadID = ""
		}
		tid, err := c.threadFor(it)
		if err != nil {
			log.Printf("calendar: fire %s: %v", it.ID, err)
			return
		}
		// The class is why the owner is asked: a fired homework is a practice, a
		// fired owner item a step — neither counts on Sessions (attention). A
		// dated ask keeps the class the session asked for; a note is read.
		class := threads.ClassStep
		if practice {
			class = threads.ClassPractice
		}
		if it.AskKind != "" || it.Kind == "note" {
			class = ""
		}
		// A fired step IS its card (inner design step 11): the row turns open
		// on its session and the card is the row — no second ask is minted,
		// and a note stays open until the owner has read it.
		c.mu.Lock()
		// Yesterday's practice closes as missed before today's is up.
		if it.Kind == "homework" {
			c.missPrevious(it, now)
		}
		moved, err := c.setItem(it.ID, "hub", "fired", `UPDATE items SET thread_id=?, state='open', fired_at=?, updated_at=? WHERE id=? AND state='scheduled' AND day=? AND at=?`,
			tid, ts(now), ts(now), it.ID, it.Day, it.At)
		if moved && it.Repeat != "" {
			c.spawnNext(it)
		}
		c.mu.Unlock()
		if !moved {
			if err != nil {
				log.Printf("calendar: fire %s: mark fired: %v", it.ID, err)
			}
			return
		}
		// Raised outside the lock: the card pushes.
		if _, err := c.thr.RaiseStep(it.ID, class, it.Say); err != nil {
			log.Printf("calendar: fire %s card: %v", it.ID, err)
		}
		log.Printf("calendar: fired %s (%s) %s on %s", it.ID, it.Kind, it.Title, when)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	log.Printf("calendar: fired %s (%s) %s", it.ID, it.Kind, it.Title)
	if it.Repeat != "" {
		c.spawnNext(it)
	}
}

// missPrevious: a practice is the LATEST occurrence only. When today's
// homework fires, the earlier one still open — the row this one chains from,
// or any same-titled homework from an earlier day (a hand-made series has no
// chain) — closes as `missed` by the hub, and its ask with it, quietly: one missed on
// Tuesday is not done twice on Wednesday. Missed is a record on the calendar, not a debt.
//
// The window says which: an on-the-day practice (a daily drill) is missed; a
// by-the-day assignment is never closed for the owner — it stays in Overdue
// beside the next one, as work debt.
func (c *Calendar) missPrevious(it Item, now time.Time) {
	if it.Due == "by" {
		return
	}
	rows, err := c.db.Query(`SELECT id, ask_id FROM items WHERE src='cal' AND kind='homework' AND due<>'by' AND state IN ('scheduled','open') AND day<? AND id<>? AND (lower(title)=lower(?) OR id=?)`,
		it.Day, it.ID, it.Title, nullable(it.PrevID))
	if err != nil {
		return
	}
	type prev struct{ id, ask string }
	var old []prev
	for rows.Next() {
		var id string
		var ask sql.NullString
		rows.Scan(&id, &ask)
		old = append(old, prev{id, ask.String})
	}
	rows.Close()
	for _, p := range old {
		c.missLocked(p.id, p.ask, "missed — the next one is up", now)
		log.Printf("calendar: %s missed (the next one, %s, is up)", p.id, it.ID)
	}
}

// missLocked closes one occurrence as missed, by the hub, and its ask with it.
// Missed is a record on the calendar, not a debt.
func (c *Calendar) missLocked(id, ask, why string, now time.Time) {
	c.setItem(id, "hub", "missed", `UPDATE items SET state='dismissed', updated_at=?, resolved_at=?, resolved_by='hub', resolution='missed' WHERE id=?`, ts(now), ts(now), id)
	if ask != "" {
		if a, err := c.thr.GetAsk(ask); err == nil && a.Active() {
			c.thr.ResolveAsk(ask, "dismissed", "hub", why)
		}
	}
}

// missDay: an on-the-day step whose day is over closes as missed (plants watered
// late one week are not good until the next). One that
// never fired (the hub was down) keeps its chain going.
func (c *Calendar) missDay(items []Item, now time.Time) {
	today := now.Format("2006-01-02")
	for _, it := range items {
		if it.Due != "on" || it.Day == "" || it.Day >= today || (it.State != "scheduled" && it.State != "fired") {
			continue
		}
		c.missLocked(it.ID, it.legacyAsk(), "missed — its day is over", now)
		if it.State == "scheduled" && it.Repeat != "" {
			c.spawnNext(it)
		}
		log.Printf("calendar: %s missed (%s is over)", it.ID, it.Day)
	}
}

// followUp: mirror the ask's fate; nag while it is still open. The row is
// re-read and the nag counted under c.mu; the push goes out after.
func (c *Calendar) followUp(it Item, now time.Time) {
	c.mu.Lock()
	msg := c.followUpLocked(it, now)
	c.mu.Unlock()
	if msg == "" {
		return
	}
	// The one push funnel (notify.Card): the nag is a NEEDS YOU card for this
	// item, so the line is queued until heard, spoken again by a hub that
	// restarted over it while the item is still fired (cmd/hub push.Open), and
	// its session is marked speaking. The words are the label form, as before.
	if _, err := notify.Card(c.Nfy, "needs_you", msg, "", it.ThreadID, it.ID); err != nil {
		log.Printf("calendar: nag %s: %v", it.ID, err)
	}
}

// followUpLocked returns the nag to send ("" = none), already counted.
func (c *Calendar) followUpLocked(it Item, now time.Time) string {
	cur, err := c.Get(it.ID)
	if err != nil || cur.State != "fired" {
		return ""
	}
	it = cur
	if ask := it.legacyAsk(); ask != "" {
		if a, err := c.thr.GetAsk(ask); err == nil && !a.Active() {
			st := "done"
			if a.State == "dismissed" {
				st = "dismissed"
			}
			by := a.ResolvedBy
			if by == "" {
				by = "ask"
			}
			if _, err := c.setItem(it.ID, by, a.Resolution, `UPDATE items SET state=?, updated_at=?, resolved_at=?, resolved_by=?, resolution=? WHERE id=? AND state='open'`, st, ts(now), ts(now), by, a.Resolution, it.ID); err != nil {
				log.Printf("calendar: mirror ask close onto %s: %v", it.ID, err)
			}
			return ""
		}
	}
	if it.NagMin <= 0 || c.Nfy == nil || it.NagCount >= c.MaxNags {
		return ""
	}
	last := it.FiredAt
	if it.LastNagAt != nil {
		last = it.LastNagAt
	}
	if last == nil || now.Sub(*last) < time.Duration(it.NagMin)*time.Minute {
		return ""
	}
	// No nagging overnight.
	if h := now.Hour(); h < 8 || h >= 22 {
		return ""
	}
	// Counted before the push: a nag that cannot be recorded is not sent, so a
	// failing write never turns into a push every minute.
	if _, err := c.db.Exec(`UPDATE items SET nag_count=nag_count+1, last_nag_at=?, updated_at=? WHERE id=?`, ts(now), ts(now), it.ID); err != nil {
		log.Printf("calendar: nag %s: count: %v", it.ID, err)
		return ""
	}
	return fmt.Sprintf("Did this get done? %s (due %s) — mark it done on the board or tell the session what happened.", it.Title, it.Day)
}

// Agenda: the merged view for [from,to].
func (c *Calendar) Agenda(from, to string) (View, error) {
	now := c.Now()
	today := now.Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = now.AddDate(0, 0, 60).Format("2006-01-02")
	}
	fromT, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return View{}, errors.New("from must be YYYY-MM-DD")
	}
	toT, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return View{}, errors.New("to must be YYYY-MM-DD")
	}
	toEnd := toT.AddDate(0, 0, 1)
	v := View{From: from, To: to, Today: today, Anytime: []Entry{}, Overdue: []Entry{}, Due: []Entry{}, Soon: []Entry{}, Days: []Day{}}
	byDay := map[string][]Entry{}
	add := func(e Entry) { byDay[e.Day] = append(byDay[e.Day], e) }

	items, err := c.List(from, to, "")
	if err != nil {
		return v, err
	}
	hhmm := c.Now().Format("15:04")
	for _, it := range items {
		e := entryFrom(Row(it))
		e.Overdue = overdueItem(it, today, hhmm)
		add(e)
	}
	// A repeat's later occurrences, on the days they will fall, so a week
	// ahead is never empty of the homework that recurs in it.
	have := map[string]bool{}
	for _, it := range items {
		have[chainKey(it)+"|"+it.Day] = true
	}
	for _, e := range c.coming(from, to, today, have) {
		add(e)
	}
	// The inbox: every open step of the owner's from a day that has passed, whether or
	// not that day is inside [from,to]. A day inside the window therefore lists
	// its row twice — once in its own cell, where it happened, and once here —
	// which is what a tray IS; a reader that draws both (the schedule list under
	// Past) skips this section.
	if old, err := c.Overdue(); err == nil {
		for _, it := range old {
			e := entryFrom(Row(it))
			e.Overdue = true
			v.Overdue = append(v.Overdue, e)
		}
	}
	// Due: what the owner owes TODAY that has not slipped yet; Do soon: the
	// same kind of step on a later day (a weekly homework all week before its
	// day), so Due's count is the tab's red number. Both whole whatever the
	// window, drawn in their day cell too.
	if ahead, err := c.DueAhead(); err == nil {
		for _, it := range ahead {
			if it.Day > today {
				v.Soon = append(v.Soon, entryFrom(Row(it)))
			} else {
				v.Due = append(v.Due, entryFrom(Row(it)))
			}
		}
	}
	// Anytime: every open to-do of the owner's with no due day, oldest first,
	// ahead of the undated asks. Listed again in the cell of the day it was
	// added when that day is inside the window.
	if rows, err := c.db.Query(`SELECT ` + cols + ` FROM items WHERE src='cal' AND day='' AND state IN ('scheduled','open') ORDER BY created_at`); err == nil {
		for rows.Next() {
			if it, err := scanItem(rows); err == nil {
				v.Anytime = append(v.Anytime, entryFrom(Row(it)))
			}
		}
		rows.Close()
	}
	// Every other dated object (actions by decided/created day, deferred recs
	// by review_on): the same rows the board shows, placed on their day.
	for _, src := range c.Sources {
		rows, err := src.Dated(from, to)
		if err != nil {
			log.Printf("calendar: agenda source %T: %v", src, err)
			continue
		}
		for _, d := range rows {
			if d.Day < from || d.Day > to {
				continue
			}
			add(entryFrom(d))
		}
	}
	// Every future wake of the hub is a queued prompt (one clock): a standing
	// row projects forward from its not_before on its cadence — a session's
	// check-in as `run`, a job as `job` (every@ rows are plumbing, omitted) —
	// and a one-shot lands on its day. Wakes that ARE calendar items are
	// already on the agenda as themselves.
	for _, w := range c.wakes(fromT, toEnd, now) {
		add(entryFrom(w))
	}
	// Undated open asks = anytime work (skip the ones a calendar item raised):
	// the asks' share of the one waiting-on-the-owner read (store.Items).
	if asks, err := store.Items(c.thr); err == nil {
		owned := map[string]bool{}
		rows, _ := c.db.Query(`SELECT ask_id FROM items WHERE src='cal' AND ask_id IS NOT NULL`)
		if rows != nil {
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					owned[id] = true
				}
			}
			if err := rows.Err(); err != nil {
				log.Printf("calendar: agenda owned asks: %v", err)
			}
			rows.Close()
		}
		for _, a := range asks {
			// A fired step is its own card: it is already on its day.
			if owned[a.ID] || strings.HasPrefix(a.ID, "cal-") {
				continue
			}
			v.Anytime = append(v.Anytime, entryFrom(a))
		}
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	titles := map[string]string{}
	for _, d := range days {
		es := byDay[d]
		// The session's title on every row that has a session and no title of
		// its own yet (asks arrive with theirs; actions, recs and items do
		// not), so a reader can name a pile of did rows by the work they were
		// part of. One lookup per distinct session, not per row.
		for i := range es {
			if es[i].ThreadID == "" || es[i].ThreadTitle != "" {
				continue
			}
			t, ok := titles[es[i].ThreadID]
			if !ok {
				c.db.QueryRow(`SELECT title FROM threads WHERE id=?`, es[i].ThreadID).Scan(&t)
				titles[es[i].ThreadID] = t
			}
			es[i].ThreadTitle = t
		}
		sort.SliceStable(es, func(i, j int) bool {
			ai, aj := es[i].At, es[j].At
			if ai == "" {
				ai = "00:00"
			}
			if aj == "" {
				aj = "00:00"
			}
			return ai < aj
		})
		v.Days = append(v.Days, Day{Day: d, Entries: es})
	}
	c.stampLive(&v)
	return v, nil
}

// coming projects every repeating item's later occurrences onto [from,to].
// The table holds ONE open occurrence per repeat — spawnNext mints the next
// when it fires or closes — so without this a week past it drew empty, and
// read as the repeat having been removed. A chain's last scheduled row is
// stepped on its own cadence, exactly as spawnNext will step it (from its
// day, never before tomorrow); a day the chain already has a real row on
// (`have`: chainKey|day) is skipped. Read-only rows (Entry.Coming), and only
// ever on their day: the Due / Do soon trays and every count read the table.
func (c *Calendar) coming(from, to, today string, have map[string]bool) []Entry {
	rows, err := c.db.Query(`SELECT ` + cols + ` FROM items WHERE src='cal' AND repeat<>'' AND day<>'' AND state='scheduled' ORDER BY day DESC, created_at DESC`)
	if err != nil {
		log.Printf("calendar: agenda coming: %v", err)
		return nil
	}
	var tails []Item
	seen := map[string]bool{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil || seen[chainKey(it)] {
			continue
		}
		seen[chainKey(it)] = true
		tails = append(tails, it)
	}
	rows.Close()
	var out []Entry
	for _, it := range tails {
		// 400 steps: a daily repeat reaches a year out, and a window further
		// than that is nobody's week.
		for nd, i := nextDay(it.Day, it.Repeat), 0; nd != "" && nd <= to && i < 400; nd, i = nextDay(nd, it.Repeat), i+1 {
			if nd < from || nd <= today || have[chainKey(it)+"|"+nd] {
				continue
			}
			n := it
			n.ID, n.Day, n.PrevID = it.ID+"@"+nd, nd, it.ID
			d := Row(n)
			d.Ref = "cal:" + it.ID
			e := entryFrom(d)
			e.Coming = true
			stampEntry(&e)
			out = append(out, e)
		}
	}
	return out
}

// stampLive puts the session's live capsules on every row that has one —
// the same words the Sessions board wears (attention.headings), minus the
// card counts, because the calendar row IS the card. One ListBrief for the
// whole view, never a lookup per row. The owner's answer from a step's box is
// delivered the moment it is posted (threads.Queue → sendPrompt: a fresh
// turn, or steered into the one in flight), so by the time the page redraws
// the session IS running and the row says so.
func (c *Calendar) stampLive(v *View) {
	ths, err := c.thr.ListBrief()
	if err != nil {
		return
	}
	pills := map[string][]threads.Pill{}
	for _, t := range ths {
		var out []threads.Pill
		if t.Speaking {
			out = append(out, threads.Pill{Word: "speaking", Tone: "speaking"})
		} else if t.WaitingToSpeak {
			out = append(out, threads.Pill{Word: "waiting to speak", Tone: "waiting"})
		}
		if t.Status == "running" {
			out = append(out, threads.StatusPill("running"))
		}
		if len(out) > 0 {
			pills[t.ID] = out
		}
	}
	live := func(id string) []threads.Pill { return pills[id] }
	stamp := func(es []Entry) {
		for i := range es {
			if es[i].ThreadID != "" && !es[i].Coming {
				es[i].Live = live(es[i].ThreadID)
			}
		}
	}
	stamp(v.Anytime)
	stamp(v.Overdue)
	stamp(v.Due)
	stamp(v.Soon)
	for _, d := range v.Days {
		stamp(d.Entries)
	}
}

// wakes projects the queued prompts onto [fromT, toEnd): one row per
// occurrence, so the key carries the time and the same standing check-in
// appears on every day it will run. A standing row's next occurrence is its
// not_before; the ones after step on its cadence. Held rows (not_before
// already past) are placed at their next occurrence from now.
func (c *Calendar) wakes(fromT, toEnd, now time.Time) []Dated {
	ps, err := c.thr.ListPrompts("queued", "", 1000)
	if err != nil {
		return nil
	}
	start := fromT
	if start.Before(now) {
		start = now
	}
	var titles map[string]threads.Thread
	thread := func(id string) (threads.Thread, bool) {
		if titles == nil {
			titles = map[string]threads.Thread{}
			if ths, err := c.thr.ListBrief(); err == nil {
				for _, t := range ths {
					titles[t.ID] = t
				}
			}
		}
		t, ok := titles[id]
		return t, ok
	}
	var out []Dated
	for _, p := range ps {
		if kind, _ := store.SplitRef(p.InReplyTo); kind == "cal" {
			continue
		}
		if p.Repeat != "" {
			if strings.HasPrefix(p.Repeat, "every@") {
				continue
			}
			row := Dated{Kind: "run", State: "scheduled", Repeat: p.Repeat, Detail: firstLine(p.Text, 200), Obj: p}
			if name, ok := strings.CutPrefix(p.Target, "job:"); ok {
				row.ID, row.Kind, row.Ref, row.Title, row.Actor = name, "job", "job:"+name, "Job: "+name, "claude:job:"+name
			} else {
				t, ok := thread(p.Target)
				if !ok {
					continue
				}
				row.ID, row.Ref, row.Title, row.GoalID, row.ThreadID, row.Actor = t.ID, "thread:"+t.ID, t.Title, t.GoalID, t.ID, "claude:thread:"+t.ID
			}
			n := p.NotBefore.In(time.Local)
			if n.Before(start) {
				n = cadence.Next(p.Repeat, time.Time{}, start.Add(-time.Minute))
			}
			for i := 0; !n.IsZero() && n.Before(toEnd) && i < 70; n, i = cadence.Next(p.Repeat, n, n), i+1 {
				if n.Before(fromT) {
					continue
				}
				r := row
				r.Key, r.Day, r.At = row.Ref+":"+n.Format("20060102T1504"), n.Format("2006-01-02"), n.Format("15:04")
				if r.Kind == "run" {
					r.Key = "run:" + r.ID + ":" + n.Format("20060102T1504")
				}
				out = append(out, r)
			}
			continue
		}
		// One-shot: the owner's message for tomorrow morning, an agent's own
		// check-back. It lands once, on its day.
		n := p.NotBefore.In(time.Local)
		if n.IsZero() || n.Before(fromT) || !n.Before(toEnd) {
			continue
		}
		row := Dated{ID: p.ID, Kind: "run", Key: "prompt:" + p.ID, Ref: "prompt:" + p.ID, State: "scheduled", Actor: p.Author,
			Title: p.Title, Detail: firstLine(p.Text, 200), GoalID: p.GoalID, Day: n.Format("2006-01-02"), At: n.Format("15:04"), Obj: p}
		if id := strings.TrimPrefix(p.Target, "new-or:"); id != "new" {
			if t, ok := thread(id); ok {
				row.Ref, row.ThreadID, row.Title = "thread:"+t.ID, t.ID, t.Title
				if row.GoalID == "" {
					row.GoalID = t.GoalID
				}
			}
		}
		if row.Title == "" {
			row.Title = firstLine(p.Text, 80)
		}
		out = append(out, row)
	}
	return out
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}
