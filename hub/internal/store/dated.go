package store

// Dated: ONE row shape for everything that can want the owner's attention or sit
// on a day — asks, proposals, calendar items, deferred recs. Two readers
// project it: the board (`attention.Build` — what is waiting, bundled by
// session) and the agenda (`calendar.Agenda` — the same things, placed on
// their day). Separate queries over the same tables drift: the console and the
// phone once printed different counts for one pile of asks.
//
// It lives here, not in calendar or attention, because it must be importable
// by every package that owns one of these objects, and the dependencies
// already run threads/actions/recs → store while calendar → threads and
// recs → calendar. `store` is the leaf; anything else is a cycle.
//
// The rule for adding a field: it belongs here only if a READER needs it to
// decide what to draw. Everything else stays on the object, which rides along
// in Obj.
type Dated struct {
	// ID / Kind: the object's own id, and what it is — `ask`, `action`, `rec`,
	// `run`, `job`, or a calendar item's own kind (`owner`, `agent`, `note`).
	ID   string
	Kind string
	// Ref: the typed pointer both surfaces route on — `ask:<id>` |
	// `action:<id>` | `rec:<id>` | `cal:<id>` | `thread:<id>` | `job:<name>`.
	// The kind it names picks the page; the id's own prefix never does.
	Ref string
	// Key: this ROW's identity on the agenda, which is not always the object's
	// — a standing check-in projects one row per occurrence, so its key
	// carries the time. Empty means Ref is the key.
	Key string
	// AskID: the open ask this row can be resolved through, if any (an item's
	// minted ask, or the ask itself). It is what the console's Done/Dismiss
	// buttons post to.
	AskID string
	// AskKind: an ask's own kind (read | decision | physical | access |
	// error). `Kind` says what table the row came from; this says what
	// answering it means. Empty on everything that is not an ask.
	AskKind string
	// AskClass: why the owner is asked — unblock | read | install | practice | step
	// (threads.askClasses). The board counts a session's "for you" from it:
	// a practice or a step is the calendar's work and never a session's turn.
	AskClass string
	Title    string
	Detail   string
	// State: the object's own state — open/answered for an ask, proposed/
	// approved/denied for a proposal, scheduled/fired/done for an item.
	State string
	// Day / At: the local day (YYYY-MM-DD) and time (HH:MM) it belongs on, or
	// "" for work with no date. An ask's day is its calendar item's day.
	Day string
	At  string
	// Soon: a calendar to-do of the owner's with no due day — Day/At are
	// then the minute it was added, or closed.
	Soon bool
	// Window: when the item behind the row is owed — now | on | by | soon (the
	// items table's `win`, ItemStampSet). "" on a row that is no item (a run,
	// a job).
	Window string
	// Repeat: the recurrence that projected this row, if any.
	Repeat string
	// ThreadID / ThreadTitle: the session that owns it. "" means no session —
	// a scheduled job's proposal, which the board bundles under its own head.
	ThreadID    string
	ThreadTitle string
	GoalID      string
	// Actor: who raised or decided it — an item's source, an ask's session,
	// an action's decided_via, a rec's source. On a did row (below) it is
	// the doer: `owner`, one of the owner's surfaces (`app` | `web` | `cli`), `hub`,
	// `auto`, or a session.
	Actor string
	// Did / Verb: the row is a RECORD of something that happened, placed at
	// the minute it happened: ahead of now the calendar is prospective events,
	// behind it a portfolio of steps done. An ask the owner closed, a rec they
	// accepted or declined, an action decided, a step closed with words. Verb
	// is the one word a reader prefixes the title with — Read, Did, Decided,
	// Granted, Skipped, Accepted, Declined, Approved, Denied, Ran, Failed —
	// so "Read: SimpleFIN is $15.99/yr" is a thing they did, not a thing to do.
	// A did row is closed by definition; the reader's lane is its Actor's.
	Did  bool
	Verb string
	// Source / RunID: who proposed it (claude:thread:<id>, claude:job:<name>,
	// lifectl, app) and the run that did, so a row can open the run behind it.
	Source string
	RunID  string
	// Surface: where the work gets done — mobile | web | any. The phone folds
	// `web` rows away uncounted; the console shows everything.
	Surface string
	// (No `Held` field: a card is answerable the moment it is raised, and the
	// owner's answer steers the live turn. See attention.buildMobile.)
	//
	// CalID: the calendar item that minted this ask. A dated step is a
	// calendar entry that happens to need the owner, not a session checking
	// in, so both surfaces list it apart from its session.
	CalID string
	// Obj: the full object this row projects (threads.Ask, actions.Action, …),
	// untyped because `store` is below all of them. The board's JSON still
	// carries whole Asks and Actions — the clients decode those, and Phase 5
	// changed what the hub *reads*, never what either surface prints.
	Obj any
}
