package store

import "database/sql"

// ItemsSchema: THE table of everything that reaches the owner. Before it, an
// ask, a dated step of theirs, a rec and a proposal were four tables with three state
// vocabularies and four hand-synced pairs — a due step minted a second row
// (an ask) on its day, and the two were kept in step by hand. Now each is one
// `items` row: `src` says which object it is (ask | cal | rec | action) and
// the id is the object's own, so `src:id` is its ref. What is not contact
// stays in a side table under the same id: a rec's scoring ledger (`recs`),
// an action's run record (`actions` + `action_events`). The `asks` and
// `cal_items` tables are frozen since the copy — history, never written.
//
// The columns are the union of what the four carried, one name each:
//   - kind / state: the object's own words (an ask's decision|read|…, a cal
//     row's owner|homework|agent|note; open|answered|…, proposed|…). A cal
//     row that has fired is `open` — it IS the card now (step 11).
//   - verb · win · lane: what it wants from the owner (do | decide | read |
//     install | grant | accept | approve), when (now | on | by | soon) and
//     whose lane it waits in (mine | chores | homework | scheduled | recs).
//     Never written by hand: ItemStampSet derives all three from the row's
//     own columns after every write, so they cannot drift.
//   - resolved_*: the close (a rec's decided_*, a proposal's decided_*).
//
// Every package that writes items migrates this list first (Migrate is keyed
// by pkg + text, so running it from four constructors applies it once).
var ItemsSchema = []string{`CREATE TABLE IF NOT EXISTS items (
	id TEXT PRIMARY KEY,
	src TEXT NOT NULL,                  -- ask | cal | rec | action: the object, and its side row
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT '',      -- the object's own kind word
	state TEXT NOT NULL,                -- the object's own state word (a fired cal row is open)
	verb TEXT NOT NULL DEFAULT '',      -- do | decide | read | install | grant | accept | approve ('' = not theirs)
	win TEXT NOT NULL DEFAULT '',       -- now | on | by | soon
	lane TEXT NOT NULL DEFAULT '',      -- mine | chores | homework | scheduled | recs
	thread_id TEXT, run_id TEXT, message_id INTEGER, goal_id TEXT,
	source TEXT NOT NULL DEFAULT '',    -- who filed it: owner | claude:thread:<id> | job:<name> | hub:…
	class TEXT NOT NULL DEFAULT '',     -- an ask's why: unblock | read | install | practice | step
	surface TEXT NOT NULL DEFAULT '',   -- mobile | web | any ('' = infer)
	check_hint TEXT NOT NULL DEFAULT '',
	said TEXT NOT NULL DEFAULT '',      -- the sentence its push spoke
	day TEXT NOT NULL DEFAULT '', at TEXT NOT NULL DEFAULT '', repeat TEXT NOT NULL DEFAULT '',
	due TEXT NOT NULL DEFAULT '',       -- a dated step's on | by
	ask_kind TEXT NOT NULL DEFAULT '',  -- a dated ask's card kind
	say TEXT NOT NULL DEFAULT '',       -- a dated ask's --say, spoken the day it fires
	nag_min INTEGER NOT NULL DEFAULT 0, nag_count INTEGER NOT NULL DEFAULT 0, last_nag_at TEXT,
	fired_at TEXT, ask_id TEXT,         -- ask_id: the ask a step minted before 2026-09-27 (none since)
	prompt_id TEXT NOT NULL DEFAULT '', prev_id TEXT,
	resolved_at TEXT, resolved_by TEXT, resolution TEXT NOT NULL DEFAULT '', superseded_by TEXT);
CREATE INDEX IF NOT EXISTS items_src ON items(src, state, created_at);
CREATE INDEX IF NOT EXISTS items_thread ON items(thread_id, id);
CREATE INDEX IF NOT EXISTS items_day ON items(day, state);
CREATE INDEX IF NOT EXISTS items_ask ON items(ask_id);
CREATE TABLE IF NOT EXISTS item_events (
	id INTEGER PRIMARY KEY, item_id TEXT NOT NULL, ts TEXT NOT NULL,
	actor TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, note TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS item_events_item ON item_events(item_id, id);`,
}

// ItemLog: the items table's state column and its one event log. Every state
// write of any item goes through ItemLog.Move.
var ItemLog = Log{Table: "items", Events: "item_events", Key: "item_id", State: "state"}

// CardKind: the card kind a row answers to, as SQL over the columns of table
// alias p ("" = unqualified) — an ask's own kind; a cal row's dated-ask kind,
// else read for a note and physical for the owner's step; empty for an agent wake,
// which is nobody's card.
func CardKind(p string) string {
	if p != "" {
		p += "."
	}
	return `CASE WHEN ` + p + `src<>'cal' THEN ` + p + `kind WHEN ` + p + `ask_kind<>'' THEN ` + p + `ask_kind WHEN ` + p + `kind='note' THEN 'read' WHEN ` + p + `kind='agent' THEN '' ELSE 'physical' END`
}

// ItemStampSet derives verb, win and lane from the row's own columns (the SET
// half of an UPDATE). A rec is accepted and waits in its own lane, soon; a
// proposal is approved, now. A cal row is on its day, or by it (`due`), or
// soon with no day; a dated step no session is behind is a chore (Chore). An
// ask is now, in the owner's lane — homework's when it is a practice, the chores'
// when a chore minted it.
var ItemStampSet = `verb = CASE src WHEN 'rec' THEN 'accept' WHEN 'action' THEN 'approve' ELSE
		CASE ` + CardKind("") + ` WHEN '' THEN '' WHEN 'read' THEN 'read' WHEN 'install' THEN 'install'
			WHEN 'decision' THEN 'decide' WHEN 'access' THEN 'grant' ELSE 'do' END END,
	win = CASE src WHEN 'rec' THEN 'soon' WHEN 'cal' THEN
		CASE WHEN day='' THEN 'soon' WHEN due IN ('on','by') THEN due ELSE 'on' END ELSE 'now' END,
	lane = CASE src WHEN 'rec' THEN 'recs' WHEN 'action' THEN 'mine' WHEN 'cal' THEN
		CASE kind WHEN 'agent' THEN 'scheduled' WHEN 'homework' THEN 'homework'
			WHEN 'owner' THEN CASE WHEN day<>'' AND COALESCE(thread_id,'') IN ('','calendar') THEN 'chores' ELSE 'mine' END
			ELSE 'mine' END
		ELSE CASE WHEN class='practice' THEN 'homework'
			WHEN class='step' AND COALESCE(thread_id,'') IN ('','calendar') THEN 'chores' ELSE 'mine' END END`

// StampItem re-derives one row's verb, win and lane — for a write that did
// not go through ItemLog.Move (which stamps on its own).
func StampItem(db interface {
	Exec(string, ...any) (sql.Result, error)
}, id string) error {
	_, err := db.Exec(`UPDATE items SET `+ItemStampSet+` WHERE id=?`, id)
	return err
}

// Items: the ONE read of everything that waits on the owner — open or
// answered asks, proposed actions, proposed or deferred recs, and their own open calendar items (owner | homework | note).
// One table since 2026-09-27; each source still projects its own rows (an
// ask's chips, a rec's ledger) into one list of Dated rows. The board
// (attention.Service) and the agenda (calendar.Agenda's anytime asks) read
// it instead of each calling ListAsks / List("proposed") / Stats on their own.
// Nothing writes through it, and a reader that wants one kind filters on Kind.
//
// ItemSource is implemented by threads.Manager, actions.Queue, recs.Store and
// calendar.Calendar (each `Open`), so `store` — the leaf every one of them
// imports — can gather them without importing any.
type ItemSource interface {
	Open() ([]Dated, error)
}

// Items gathers the sources in order, each in its own order; the first error
// stops it. A nil source is skipped (a hub without a queue, a test without a
// calendar).
func Items(srcs ...ItemSource) ([]Dated, error) {
	var out []Dated
	for _, s := range srcs {
		if s == nil {
			continue
		}
		rows, err := s.Open()
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ItemsOf: the rows of one Kind, order kept.
func ItemsOf(rows []Dated, kind string) []Dated {
	var out []Dated
	for _, r := range rows {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}
