// Package recs is the hub's recommendation ledger: "needs action" and
// "recommendations" are separate concepts, and recommendations are tracked
// over time while agents serve new ones.
//
// Why it is its own object, and not an ask, a calendar item or a goal note:
//
//   - An ask NOTIFIES. It is the owner's turn, now, and it closes when the
//     deed is done. A recommendation must NOT notify — it is a standing list
//     they pull from when in the mood to spend money / pick a tool / change a
//     habit. Push vs pull is the whole distinction.
//   - A calendar item is a date. Half of what gets recommended has no date
//     ("try Phantombuster"), and the half that does (buy the tranche on the
//     27th) wants the date to be a CONSEQUENCE of accepting, not the record.
//   - A goal note is prose. It cannot be counted, filtered, or scored.
//
// And the thing none of them do: every other object in this hub closes when
// the ACTION is taken. A recommendation closes when the EFFECT is measured.
// That is why it carries two dates (act_by, review_on) and two resolutions
// (status = what the owner decided, outcome = whether it worked). The pair is
// the feedback loop: over time `Stats` answers "how often is the agent right",
// "what is its advice costing per month", and "what has the owner already
// said no to" — so a future session recommends from a track record instead of from
// scratch.
//
// Rows are never deleted; status and outcome only advance. Accepting one is
// what mints the downstream objects (calendar items, a proposal, an ask);
// their ids are recorded in links, so the record stays attached to the deed.
package recs

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Schema: the recommendation ledger. Append an ALTER to add a column
// (store/migrate.go).
var Schema = []string{`CREATE TABLE IF NOT EXISTS recs (
	id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
	goal_id TEXT, thread_id TEXT,
	source TEXT NOT NULL DEFAULT '',        -- owner | claude:thread:<id> | job:<name>
	domain TEXT NOT NULL DEFAULT 'other',   -- money | health | audience | tools | home | other
	kind TEXT NOT NULL DEFAULT 'other',     -- buy | subscribe | trade | try | stop | habit | process | other
	cost_cents INTEGER NOT NULL DEFAULT 0,
	cost_period TEXT NOT NULL DEFAULT '',   -- '' = one-off | monthly | yearly
	effort TEXT NOT NULL DEFAULT 'low',     -- low | med | high (of the owner's time)
	confidence INTEGER NOT NULL DEFAULT 50, -- 0..100, the agent's own prior
	because TEXT NOT NULL DEFAULT '',       -- the evidence that produced it
	expect TEXT NOT NULL DEFAULT '',        -- what should change, and how we would know
	act_by TEXT NOT NULL DEFAULT '',        -- YYYY-MM-DD after which it is stale
	review_on TEXT NOT NULL DEFAULT '',     -- YYYY-MM-DD to come back and score it
	status TEXT NOT NULL DEFAULT 'proposed',-- proposed | deferred | accepted | declined | done | superseded | expired
	decided_at TEXT, decided_by TEXT, decision_note TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',       -- '' | worked | mixed | failed | unclear
	outcome_at TEXT, outcome_note TEXT NOT NULL DEFAULT '',
	prev_id TEXT,                           -- the rec this supersedes
	links TEXT NOT NULL DEFAULT '',         -- comma list of cal-/ask-/act- ids it minted
	model TEXT NOT NULL DEFAULT '');        -- the model id that filed it (claude-opus-5, …); '' = the owner
CREATE INDEX IF NOT EXISTS recs_status ON recs(status, created_at);
CREATE INDEX IF NOT EXISTS recs_review ON recs(review_on, status);`,
	// model: a required field. The track record is per model as well as per domain, so a rec filed by a
	// session must say which model wrote it; the hub fills it from the
	// session's live run when the body omits it.
	`ALTER TABLE recs ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
	// rec_events (2026-09-26, review-primitives step 4): every decision on a
	// rec, append-only, written by setRec alone. Decide overwrote decided_*,
	// so a rec accepted, put back and declined kept only the decline.
	`CREATE TABLE IF NOT EXISTS rec_events (
	id INTEGER PRIMARY KEY, rec_id TEXT NOT NULL, ts TEXT NOT NULL,
	actor TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, note TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS rec_events_rec ON rec_events(rec_id, id);`,
	// items (2026-09-27, inner-design step 9): a rec's accept stage — title,
	// detail, status, the decision — moves to the one table of what reaches
	// the owner (store.ItemsSchema), trail and all. `recs` stays as the scoring
	// ledger under the same id (domain, cost, confidence, dates, outcome,
	// links, model); its status/decided_* columns and rec_events are frozen
	// from here on.
	`INSERT OR IGNORE INTO items (id,src,created_at,updated_at,title,detail,kind,state,goal_id,thread_id,source,prev_id,resolved_at,resolved_by,resolution)
	SELECT id,'rec',created_at,updated_at,title,detail,kind,status,goal_id,thread_id,source,prev_id,decided_at,decided_by,decision_note FROM recs ORDER BY created_at;
INSERT INTO item_events (item_id,ts,actor,from_state,to_state,note) SELECT rec_id,ts,actor,from_state,to_state,note FROM rec_events ORDER BY id;
UPDATE items SET ` + store.ItemStampSet + ` WHERE src='rec';`,
}

// setRec is the ONE writer of a rec's status (and of its score): move is the
// item's own write, ledger the side table's (same transaction), and the
// decision — before → after, by whom, with what words — is appended to
// item_events. Every call is a decision, so it is logged even when the status
// stays (a re-decided note, a score). moved = the row changed.
func (s *Store) setRec(id, by, note string, move store.Stmt, ledger ...store.Stmt) (bool, error) {
	return store.ItemLog.MoveWith(s.db, id, by, strings.TrimSpace(note), true, s.Now(), move, ledger...)
}

// stmt: one statement, for setRec.
func stmt(q string, args ...any) store.Stmt { return store.Stmt{Q: q, Args: args} }

// backfill gives every rec a session filed before the column existed the
// model of the run that was live when it was filed (thread_runs is another
// package's table in the same file, so this is best-effort: on a database
// without it the update simply fails and nothing changes).
const backfill = `UPDATE recs SET model = COALESCE((SELECT r.model FROM thread_runs r
	WHERE r.thread_id = recs.thread_id AND r.model != '' AND r.started_at <= recs.created_at
	ORDER BY r.started_at DESC LIMIT 1), '')
	WHERE model = '' AND thread_id IS NOT NULL AND thread_id != ''`

// Rec: one thing the agent told the owner to do.
type Rec struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	GoalID    string    `json:"goal_id,omitempty"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Source    string    `json:"source"`
	Domain    string    `json:"domain"`
	Kind      string    `json:"kind"`
	// CostCents is what saying yes costs, in cents; CostPeriod makes it
	// recurring. Both zero/empty = free.
	CostCents    int        `json:"cost_cents"`
	CostPeriod   string     `json:"cost_period,omitempty"`
	Effort       string     `json:"effort"`
	Confidence   int        `json:"confidence"`
	Because      string     `json:"because,omitempty"`
	Expect       string     `json:"expect,omitempty"`
	ActBy        string     `json:"act_by,omitempty"`
	ReviewOn     string     `json:"review_on,omitempty"`
	Status       string     `json:"status"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecidedBy    string     `json:"decided_by,omitempty"`
	DecisionNote string     `json:"decision_note,omitempty"`
	Outcome      string     `json:"outcome,omitempty"`
	OutcomeAt    *time.Time `json:"outcome_at,omitempty"`
	OutcomeNote  string     `json:"outcome_note,omitempty"`
	PrevID       string     `json:"prev_id,omitempty"`
	Links        string     `json:"links,omitempty"`
	// Model is the model id of the session that filed it (`claude-opus-5`,
	// `claude-fable-5[1m]`), so the ledger can say whose advice worked. Empty
	// only when the owner filed it themselves.
	Model string `json:"model,omitempty"`
	// MessageID is where the rec sits in its session's chat: the first reply
	// the filing session wrote after filing it (a filed rec is a cell in the
	// chat). Not a column — resolved on
	// `List{Thread}` from thread_messages, so a rec filed mid-run has no home
	// until the reply lands (0 = draw it at the end of the chat). A card in
	// the chat is still pull: it is inside the session, and it notifies nobody.
	MessageID int64 `json:"message_id,omitempty"`
	// ThreadRunning: the session that filed it is running RIGHT NOW, shown on
	// the card so the owner knows not to follow up yet. Not a column — the server stamps
	// it from the threads table on every read (server.recsList / recsGet), so
	// the list and the page carry the same live answer.
	ThreadRunning bool `json:"thread_running,omitempty"`
	// Answers: the buttons on the card — Accept · Decline · Reply, the
	// Recs page's own row (Later went 2026-09-09) — worded here like an ask's
	// `outcomes` so both surfaces draw one row (2026-09-18). Dismiss is not
	// one: it marks the rec expired with the note store.RecDismissed, silently.
	Answers []store.Outcome `json:"outcomes"`
	// open / closed / folded / lane: where the rec stands with the owner
	// (store.RecStanding) — open while proposed, neither open nor closed while
	// parked, "dismissed" folded — so neither client keeps its own list of
	// open statuses.
	Waiting bool   `json:"open"`
	Closed  bool   `json:"closed"`
	Folded  string `json:"folded,omitempty"`
	Lane    string `json:"lane"`
	// Window: when it is owed — always soon: a rec is pulled whenever the owner
	// comes looking, never pushed (the item's own `win`).
	Window string `json:"window"`
}

// FiledBySession reports whether an agent session filed the rec (as opposed
// to the owner, or a job) — the case where Model is required.
func (r Rec) FiledBySession() bool { return strings.HasPrefix(r.Source, "claude:thread:") }

// MonthlyCents: the recurring cost of saying yes, normalised to a month.
// One-off costs are not recurring and count zero here.
func (r Rec) MonthlyCents() int {
	switch r.CostPeriod {
	case "monthly":
		return r.CostCents
	case "yearly":
		return r.CostCents / 12
	}
	return 0
}

// Open reports whether the rec is still waiting on the owner's decision.
func (r Rec) Open() bool { return r.Status == "proposed" }

// SourceThread is the session that filed the rec — the agent the owner's
// decision goes back to by default (a new session is the option). thread_id is set when a session
// files one through lifectl; older rows carry it only inside source.
func SourceThread(r Rec) string {
	if r.ThreadID != "" {
		return r.ThreadID
	}
	if id, ok := strings.CutPrefix(r.Source, "claude:thread:"); ok {
		return id
	}
	return ""
}

// Starter is the message a NEW session opens with when the owner sends their
// decision somewhere other than the session that filed the rec. Opener is the
// first line — the session title and the board preview — and is their own
// note whenever they typed one, because their words make a better title than
// ours; Context is the block underneath, which they never have to write and
// which has to say what is going on and where to find it, since this session
// has no history with the rec.
type Starter struct {
	Opener  string `json:"opener"`
	Context string `json:"context"`
}

// Text is the whole first message: the owner's line, then the hub's block.
func (s Starter) Text() string { return s.Opener + "\n\n" + s.Context }

// Related is everything the rec points at but does not hold: the calendar
// items, proposals and sessions it minted or cites, the recs it builds on or
// supersedes, what else is on the calendar for the goal, and the session
// that filed it with its last reply. The rec alone sent a new session off to
// rediscover all of it (`lifectl cal`, `lifectl recs all`, the goal notes,
// the filing thread's history), so as much usable information as possible
// rides along. The hub resolves it from the other stores
// (server.recRelated); this package only lays it out. Every line is the
// CURRENT state, so "cal-… · scheduled" means it is still to do.
type Related struct {
	// Linked: one line per id in `links` or named in the text — calendar
	// items, recs, asks, proposals, sessions — each with its state today.
	Linked []string
	// Calendar: open items on the same goal that the rec does not name,
	// soonest first — the plan it has to fit around.
	Calendar []string
	// Source: the filing session in one line (title, status, cadence).
	Source string
	// SourceSaid: that session's last reply, cut short — the argument as
	// the owner read it, which the record's fields compress.
	SourceSaid string
}

func (rel Related) empty() bool {
	return len(rel.Linked) == 0 && len(rel.Calendar) == 0 && rel.Source == "" && rel.SourceSaid == ""
}

// section renders the whole of it for a new session's brief.
func (rel Related) section(goalID string) string {
	if rel.empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nAround it, as of now (the hub looked these up so you do not have to):\n")
	if len(rel.Linked) > 0 {
		b.WriteString("What it points at:\n")
		for _, l := range rel.Linked {
			b.WriteString("- " + l + "\n")
		}
	}
	if len(rel.Calendar) > 0 {
		if goalID != "" {
			fmt.Fprintf(&b, "Also on their calendar for %s, soonest first:\n", goalID)
		} else {
			b.WriteString("Also on their calendar, soonest first:\n")
		}
		for _, l := range rel.Calendar {
			b.WriteString("- " + l + "\n")
		}
	}
	if rel.Source != "" {
		b.WriteString("Filed from: " + rel.Source + "\n")
	}
	if rel.SourceSaid != "" {
		b.WriteString("That session's last reply:\n" + indent(rel.SourceSaid) + "\n")
	}
	return b.String()
}

// links is the short form for the session that filed the rec: it knows its
// own argument, but not what has happened to the things it minted since.
func (rel Related) links() string {
	if len(rel.Linked) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nWhat it points at, as of now:\n")
	for _, l := range rel.Linked {
		b.WriteString("- " + l + "\n")
	}
	return b.String()
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// StarterFor builds that message from the record itself: same text on the
// phone and in the console, because it is composed here and not twice.
// rel is what the record points at, resolved by the hub (see Related).
func StarterFor(r Rec, rel Related) Starter {
	declined := r.Status == "declined"
	deferred := r.Status == "deferred"
	var b strings.Builder
	if declined {
		b.WriteString("[The owner declined a recommendation in the life app and sent their answer here. Everything in this block is filled in by the hub — the line above it is theirs, typed at the moment they decided.]\n")
	} else if deferred {
		b.WriteString("[The owner deferred a recommendation in the life app — neither yes nor no — and sent their answer here. Everything in this block is filled in by the hub — the line above it is theirs, typed at the moment they decided.]\n")
	} else {
		b.WriteString("[The owner accepted a recommendation in the life app and sent it here to be carried out. Everything in this block is filled in by the hub — the line above it is theirs, typed at the moment they accepted.]\n")
	}
	b.WriteString(recBlock(r))
	b.WriteString(rel.section(r.GoalID))
	if declined {
		fmt.Fprintf(&b, "\nThey said no. Do not do it and do not re-file it. Their line above is the reason, and it is the valuable half: write it to the goal it belongs to so a future session recommends from it instead of re-deriving the same idea next month (`lifectl goal <id> note …`). If their line asks for something different instead, that is the work.\n")
		return Starter{Opener: opener(r, "I declined your recommendation: "+r.Title), Context: b.String()}
	}
	if deferred {
		fmt.Fprintf(&b, "\nThey said not now: check back on %s. The hub already handles the wait — the rec leaves their list and returns to it that day, and a calendar agent item on that day re-serves it with whatever changed. Do not do it, do not argue it and do not re-file it. If their line above asks for something before then (a number to watch, a cheaper route to find), that is the work; otherwise write the reason for the wait to the goal (`lifectl goal <id> note …`) and reply in a line.\n", r.ReviewOn)
		return Starter{Opener: opener(r, "Check back with me on "+r.ReviewOn+": "+r.Title), Context: b.String()}
	}
	b.WriteString("\nYour job is to carry it out. They have already said yes: do not re-argue it, do not ask whether they want it, and do not re-file it as a new rec. Mint the calendar items, proposals and asks that make it real and record their ids with `lifectl rec " + r.ID + " link <ids>`. If their line above says they have already done the thing themselves, then the work is writing down what they did and setting up the check that proves it worked.")
	if r.ReviewOn != "" {
		fmt.Fprintf(&b, " It is due to be scored on %s (`lifectl rec %s score …`); put that check on the calendar rather than leaving it to a future review to notice.", r.ReviewOn, r.ID)
	}
	b.WriteString("\n")
	return Starter{Opener: opener(r, "I accepted your recommendation: "+r.Title), Context: b.String()}
}

// opener: the owner's note if they typed one, otherwise the hub's default line.
func opener(r Rec, fallback string) string {
	if n := strings.TrimSpace(r.DecisionNote); n != "" {
		return n
	}
	return fallback
}

// ReplyStarter and ReplyRelay are the fourth button: reply without deciding.
// The owner's line travels exactly as a decision's does — the filing session
// gets the short form, a new one the whole record — but there is no verdict
// in it: the rec stays where it is, and the session's job is to answer them,
// not to act.
func ReplyStarter(r Rec, note string, rel Related) Starter {
	var b strings.Builder
	b.WriteString("[The owner replied to a recommendation in the life app WITHOUT deciding on it, and sent their line here. Everything in this block is filled in by the hub — the line above it is theirs.]\n")
	b.WriteString(recBlock(r))
	b.WriteString(rel.section(r.GoalID))
	b.WriteString("\n" + replyStanding(r) + " Their line above is a question, a correction or context — answer it here in a few lines and raise a read ask if they need to see the answer. Do not carry the rec out, do not decide it for them and do not re-file it. If their line changes the picture, supersede it (`lifectl rec add … --supersedes " + r.ID + "`) or write the fact to the goal; if it asks for work, that is the work.\n")
	r.DecisionNote = note
	return Starter{Opener: opener(r, "About your recommendation: "+r.Title), Context: b.String()}
}

func ReplyRelay(r Rec, note string, rel Related) string {
	return ReplyRelayHeader(r, rel) + strings.TrimSpace(note) + "\n"
}

// ReplyRelayHeader is ReplyRelay without the owner's words: the bracketed
// frame the filing session sees ABOVE a prompt that answers `rec:<id>` with no
// outcome (threads/prompts.go refHeader → Manager.RecHeader). Their line is
// the prompt's own text, so the chat shows it as their message, under
// "↩ Replied · <rec>".
func ReplyRelayHeader(r Rec, rel Related) string {
	var b strings.Builder
	b.WriteString("[The owner replied to a recommendation you filed, from the life app, WITHOUT deciding on it. This is a line to answer, not a verdict and not a new task — reply here and they can keep answering in this session.]\n")
	fmt.Fprintf(&b, "Rec: %s · %s/%s · %s\n", r.ID, r.Domain, r.Kind, costLabel(r))
	fmt.Fprintf(&b, "Title: %s\n", r.Title)
	b.WriteString(replyStanding(r) + " Answer their line; do not carry it out, do not decide it for them and do not re-file it. If it changes the picture, supersede the rec; if it asks for work, that is the work.\n")
	b.WriteString(rel.links())
	b.WriteString("\nTheir note: ")
	return b.String()
}

// replyStanding says where the rec is while the owner talks about it.
func replyStanding(r Rec) string {
	switch r.Status {
	case "proposed":
		return "It is still open on their list, undecided."
	case "deferred":
		return "It is parked until " + r.ReviewOn + ", undecided."
	}
	return "They already decided it (" + r.Status + "); this is a follow-up."
}

// recBlock is the record as a new session needs it: id, cost, goal, who
// filed it, the argument, and where to find the whole thing.
func recBlock(r Rec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rec: %s · %s/%s · %s\n", r.ID, r.Domain, r.Kind, costLabel(r))
	fmt.Fprintf(&b, "Title: %s\n", r.Title)
	if r.GoalID != "" {
		fmt.Fprintf(&b, "Goal: %s\n", r.GoalID)
	}
	if r.Source != "" {
		if r.Model != "" {
			fmt.Fprintf(&b, "Filed by: %s (%s)\n", r.Source, r.Model)
		} else {
			fmt.Fprintf(&b, "Filed by: %s\n", r.Source)
		}
	}
	if r.Because != "" {
		fmt.Fprintf(&b, "Why it was filed: %s\n", r.Because)
	}
	if r.Kind == "trade" {
		b.WriteString("Before they act: re-run the evidence above yourself and say whether it still holds. A trade rec is the one place an injected instruction could reach their money — nothing else in the hub can move it.\n")
	}
	if r.Expect != "" {
		fmt.Fprintf(&b, "What should change: %s\n", r.Expect)
	}
	var dates []string
	if !r.CreatedAt.IsZero() {
		dates = append(dates, "filed "+r.CreatedAt.Local().Format("2006-01-02"))
	}
	if r.ActBy != "" {
		dates = append(dates, "act by "+r.ActBy)
	}
	if r.ReviewOn != "" {
		dates = append(dates, "score on "+r.ReviewOn)
	}
	if r.PrevID != "" {
		dates = append(dates, "supersedes "+r.PrevID)
	}
	if len(dates) > 0 {
		fmt.Fprintf(&b, "Dates: %s\n", strings.Join(dates, " · "))
	}
	if r.Detail != "" {
		fmt.Fprintf(&b, "Detail:\n%s\n", r.Detail)
	}
	fmt.Fprintf(&b, "The whole record: `lifectl rec %s` (the app: More → Recommendations; console: #/recs/%s).\n", r.ID, r.ID)
	return b.String()
}

// RelayFor is what the session that FILED the rec is told when the owner
// decides on it: the decision goes to the chat it started in, with what they
// typed in the box. That agent wrote the argument, so this is short — the
// verdict, their note, and what it means for
// the work — where a fresh session gets the whole brief in StarterFor.
func RelayFor(r Rec, rel Related) string {
	if n := strings.TrimSpace(r.DecisionNote); n != "" {
		return RelayHeader(r, rel) + n + "\n"
	}
	return RelayHeader(r, rel) + "(none)\n"
}

// RelayHeader is RelayFor without the owner's note — the frame rendered above
// a prompt answering `rec:<id>` with `outcome` accepted|declined|deferred, at
// the moment the session wakes (so the chat holds their words alone and the
// agent still reads the verdict from the row, never from prose).
func RelayHeader(r Rec, rel Related) string {
	var b strings.Builder
	b.WriteString("[The owner decided on a recommendation you filed, from the life app. This is their answer, not a new task — reply here and they can keep answering in this session.]\n")
	fmt.Fprintf(&b, "Rec: %s · %s/%s · %s\n", r.ID, r.Domain, r.Kind, costLabel(r))
	fmt.Fprintf(&b, "Title: %s\n", r.Title)
	if r.Status == "declined" {
		b.WriteString("Declined. Do not do it and do not serve it again. Their note below is the reason — write it to the goal so a future session recommends from it instead of re-deriving the same idea; if it asks for something different instead, that is the work.\n")
	} else if r.Status == "deferred" {
		fmt.Fprintf(&b, "Deferred until %s — neither yes nor no. The hub handles the wait: the rec leaves their list and comes back that day, and a calendar agent item re-serves it then. Do not do it and do not re-file it; if their note asks for something before then, that is the work, otherwise write the reason for the wait to the goal and reply in a line.\n", r.ReviewOn)
	} else {
		fmt.Fprintf(&b, "Accepted. Carry it out now: mint the calendar items, proposals and asks that make it real and record their ids with `lifectl rec %s link <ids>`. Do not re-argue it and do not re-file it. If their note says they already did the thing themselves, the work is writing down what they did and setting up the check that proves it worked.\n", r.ID)
		if r.ReviewOn != "" {
			fmt.Fprintf(&b, "Due to be scored on %s (`lifectl rec %s score …`); put that check on the calendar rather than leaving it to a future review to notice.\n", r.ReviewOn, r.ID)
		}
	}
	b.WriteString(rel.links())
	b.WriteString("\nTheir note (blank = they added none): ")
	return b.String()
}

// costLabel: what saying yes costs, in the words the card shows.
func costLabel(r Rec) string {
	if r.CostCents <= 0 {
		return "no cost"
	}
	switch r.CostPeriod {
	case "monthly":
		return "$" + usd(r.CostCents) + "/mo"
	case "yearly":
		return "$" + usd(r.CostCents) + "/yr"
	}
	return "$" + usd(r.CostCents) + " once"
}

// citesEvidence: does the argument point at something the owner can re-run or
// look up themselves? No code path can move their money, so the only way an
// injected instruction reaches it is a trade rec they carry out by hand. A
// citation is the check — they re-run the command and
// sees the same number before placing anything. Deliberately shallow: it
// cannot judge whether the argument is true, only whether it is checkable.
func citesEvidence(because string) bool {
	b := strings.ToLower(because)
	for _, cite := range []string{"lifectl", "obs ", "obs_id", "observation", "statement", "1099", "docs/"} {
		if strings.Contains(b, cite) {
			return true
		}
	}
	return false
}

var (
	domains  = map[string]bool{"money": true, "health": true, "audience": true, "tools": true, "home": true, "other": true}
	kinds    = map[string]bool{"buy": true, "subscribe": true, "trade": true, "try": true, "stop": true, "habit": true, "process": true, "other": true}
	efforts  = map[string]bool{"low": true, "med": true, "high": true}
	periods  = map[string]bool{"": true, "monthly": true, "yearly": true}
	statuses = map[string]bool{"proposed": true, "deferred": true, "accepted": true, "declined": true, "done": true, "superseded": true, "expired": true}
	outcomes = map[string]bool{"worked": true, "mixed": true, "failed": true, "unclear": true}
)

type Store struct {
	db  *store.DB
	Now func() time.Time
}

func New(db *store.DB) (*Store, error) {
	if _, err := db.Migrate("items", store.ItemsSchema); err != nil {
		return nil, err
	}
	if _, err := db.Migrate("recs", Schema); err != nil {
		return nil, err
	}
	if res, err := db.Exec(backfill); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("recs: backfilled model on %d rows from thread_runs", n)
		}
	}
	return &Store{db: db, Now: time.Now}, nil
}

func ts(t time.Time) string { return store.TS(t) }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func newID() string { return store.NewID("rec") }

func day(s, field string) error {
	if s == "" {
		return nil
	}
	if _, err := time.ParseInLocation("2006-01-02", s, time.Local); err != nil {
		return fmt.Errorf("%s must be YYYY-MM-DD", field)
	}
	return nil
}

// Add records a recommendation. It deliberately notifies nobody: a rec waits
// on the list until the owner goes looking, unlike an ask.
func (s *Store) Add(r Rec) (Rec, error) {
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		return Rec{}, errors.New("title required")
	}
	if len(r.Title) > 200 {
		r.Title = r.Title[:200]
	}
	if r.Domain == "" {
		r.Domain = "other"
	}
	if r.Kind == "" {
		r.Kind = "other"
	}
	if r.Effort == "" {
		r.Effort = "low"
	}
	if !domains[r.Domain] {
		return Rec{}, errors.New("domain must be money|health|audience|tools|home|other")
	}
	if !kinds[r.Kind] {
		return Rec{}, errors.New("kind must be buy|subscribe|trade|try|stop|habit|process|other")
	}
	if !efforts[r.Effort] {
		return Rec{}, errors.New("effort must be low|med|high")
	}
	if !periods[r.CostPeriod] {
		return Rec{}, errors.New("cost_period must be monthly|yearly or empty")
	}
	if r.CostCents < 0 {
		return Rec{}, errors.New("cost_cents must not be negative")
	}
	if r.Confidence == 0 {
		r.Confidence = 50
	}
	if r.Confidence < 0 || r.Confidence > 100 {
		return Rec{}, errors.New("confidence must be 0..100")
	}
	if err := day(r.ActBy, "act_by"); err != nil {
		return Rec{}, err
	}
	if err := day(r.ReviewOn, "review_on"); err != nil {
		return Rec{}, err
	}
	if r.PrevID != "" {
		if _, err := s.Get(r.PrevID); err != nil {
			return Rec{}, fmt.Errorf("unknown prev_id %q", r.PrevID)
		}
	}
	if r.Source == "" {
		r.Source = "owner"
	}
	if r.Kind == "trade" && r.FiledBySession() && !citesEvidence(r.Because) {
		return Rec{}, errors.New("a trade rec must cite evidence you can re-run before you act: put a `lifectl …` command, an observation id (`obs 39771`) or a statement/document reference in --because (money cannot be moved by code, so the only path to your money is a rec you act on by hand)")
	}
	r.Model = strings.TrimSpace(r.Model)
	if r.FiledBySession() && r.Model == "" {
		return Rec{}, errors.New("model required: a session that files a rec must say which model wrote it (the hub reads it from the session's run; pass --model if it cannot)")
	}
	now := s.Now()
	r.ID, r.CreatedAt, r.UpdatedAt, r.Status = newID(), now, now, "proposed"
	// The item is the card; the ledger row keeps the filing whole (its own
	// title and status are the record as filed, never updated).
	_, err := s.setRec(r.ID, r.Source, "",
		stmt(`INSERT INTO items (id,src,created_at,updated_at,title,detail,kind,state,goal_id,thread_id,source,prev_id)
		VALUES (?,'rec',?,?,?,?,?,'proposed',?,?,?,?)`,
			r.ID, ts(now), ts(now), r.Title, strings.TrimSpace(r.Detail), r.Kind, nullable(r.GoalID), nullable(r.ThreadID), r.Source, nullable(r.PrevID)),
		stmt(`INSERT INTO recs (id,created_at,updated_at,title,detail,goal_id,thread_id,source,domain,kind,
		cost_cents,cost_period,effort,confidence,because,expect,act_by,review_on,status,prev_id,model)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'proposed',?,?)`,
			r.ID, ts(now), ts(now), r.Title, strings.TrimSpace(r.Detail), nullable(r.GoalID), nullable(r.ThreadID), r.Source,
			r.Domain, r.Kind, r.CostCents, r.CostPeriod, r.Effort, r.Confidence, strings.TrimSpace(r.Because),
			strings.TrimSpace(r.Expect), r.ActBy, r.ReviewOn, nullable(r.PrevID), r.Model))
	if err != nil {
		return Rec{}, err
	}
	// Superseding one closes the old row, so the list never shows both.
	if r.PrevID != "" {
		s.setRec(r.PrevID, r.Source, "superseded by "+r.ID, stmt(`UPDATE items SET state='superseded', updated_at=? WHERE id=? AND src='rec' AND state IN ('proposed','accepted')`, ts(now), r.PrevID))
	}
	log.Printf("recs: %s %s/%s: %s", r.ID, r.Domain, r.Kind, r.Title)
	return s.Get(r.ID)
}

// cols / joined: a rec is its item (i: the card and its decision) joined to
// its ledger row (l: what it costs, what it expects, how it scored).
const cols = `i.id,i.created_at,i.updated_at,i.title,i.detail,i.goal_id,i.thread_id,i.source,l.domain,l.kind,l.cost_cents,l.cost_period,
	l.effort,l.confidence,l.because,l.expect,l.act_by,l.review_on,i.state,i.resolved_at,i.resolved_by,i.resolution,
	l.outcome,l.outcome_at,l.outcome_note,i.prev_id,l.links,l.model,i.win`

const joined = ` FROM items i JOIN recs l ON l.id=i.id WHERE i.src='rec' AND `

type scanner interface{ Scan(...any) error }

func scanRec(sc scanner) (Rec, error) {
	var r Rec
	var cr, up string
	var goal, thread, decAt, decBy, outAt, prev sql.NullString
	if err := sc.Scan(&r.ID, &cr, &up, &r.Title, &r.Detail, &goal, &thread, &r.Source, &r.Domain, &r.Kind,
		&r.CostCents, &r.CostPeriod, &r.Effort, &r.Confidence, &r.Because, &r.Expect, &r.ActBy, &r.ReviewOn,
		&r.Status, &decAt, &decBy, &r.DecisionNote, &r.Outcome, &outAt, &r.OutcomeNote, &prev, &r.Links, &r.Model, &r.Window); err != nil {
		return r, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, cr)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, up)
	r.GoalID, r.ThreadID, r.DecidedBy, r.PrevID = goal.String, thread.String, decBy.String, prev.String
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
	r.DecidedAt, r.OutcomeAt = pt(decAt), pt(outAt)
	r.Answers = store.RecOutcomes()
	st := store.RecStanding(r.Status, r.DecisionNote)
	r.Waiting, r.Closed, r.Folded, r.Lane = st.Open, st.Closed, st.Folded, st.Lane
	return r, nil
}

func (s *Store) Get(id string) (Rec, error) {
	return scanRec(s.db.QueryRow(`SELECT `+cols+joined+`i.id=?`, id))
}

// Filter selects rows for List. Empty fields mean "any"; Status accepts
// "open" (= proposed) and "all".
type Filter struct {
	Status   string
	Domain   string
	GoalID   string
	Kind     string
	DueBy    string // review_on <= this day, and not yet scored
	MaxCents int    // <= 0 = no cap
	Limit    int
	Model    string // exact model id, e.g. claude-opus-5
	// Thread: the recs one session filed (thread_id, or the session named in
	// `source`), each placed in that chat (Rec.MessageID).
	Thread string
}

func (s *Store) List(f Filter) ([]Rec, error) {
	where, args := []string{"1=1"}, []any{}
	switch f.Status {
	case "", "open":
		where = append(where, "i.state='proposed'")
	case "all":
	default:
		if !statuses[f.Status] {
			return nil, errors.New("unknown status " + f.Status)
		}
		where, args = append(where, "i.state=?"), append(args, f.Status)
	}
	if f.Domain != "" {
		where, args = append(where, "l.domain=?"), append(args, f.Domain)
	}
	if f.Kind != "" {
		where, args = append(where, "l.kind=?"), append(args, f.Kind)
	}
	if f.GoalID != "" {
		where, args = append(where, "i.goal_id=?"), append(args, f.GoalID)
	}
	if f.Model != "" {
		where, args = append(where, "l.model=?"), append(args, f.Model)
	}
	if f.DueBy != "" {
		// Only something the owner said yes to can be scored: a deferred rec also
		// carries a review_on, but that is its wake-up date, not a verdict due.
		where, args = append(where, "l.review_on!='' AND l.review_on<=? AND l.outcome='' AND i.state IN ('accepted','done')"), append(args, f.DueBy)
	}
	if f.MaxCents > 0 {
		where, args = append(where, "l.cost_cents<=?"), append(args, f.MaxCents)
	}
	if f.Thread != "" {
		where, args = append(where, "(i.thread_id=? OR i.source=?)"), append(args, f.Thread, "claude:thread:"+f.Thread)
	}
	q := `SELECT ` + cols + joined + strings.Join(where, " AND ") + ` ORDER BY i.created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rec{}
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if f.Thread != "" {
		s.placeInChat(out)
	}
	return out, nil
}

// placeInChat gives each rec the message it is drawn under: the first reply
// (a `claude` row — the turn's answer, or its error) the session wrote at or
// after the rec was filed. A rec is filed by the run in flight, so its reply
// is the one that says "rec filed"; an older rec gets the reply that followed
// it. thread_messages is another package's table in the same file, so this
// is best-effort: no table, no placement, and the surfaces draw it at the end.
func (s *Store) placeInChat(list []Rec) {
	for i := range list {
		if list[i].ThreadID == "" && SourceThread(list[i]) == "" {
			continue
		}
		var mid sql.NullInt64
		if err := s.db.QueryRow(`SELECT MIN(id) FROM thread_messages WHERE thread_id=? AND role='claude' AND ts>=?`,
			SourceThread(list[i]), ts(list[i].CreatedAt)).Scan(&mid); err == nil && mid.Valid {
			list[i].MessageID = mid.Int64
		}
	}
}

// Decide records the owner's answer: accepted | declined | done | expired, or
// "proposed" to put it back on the list. Their note is the valuable half — a
// decline with a reason is what stops the same idea coming back next month.
func (s *Store) Decide(id, status, by, note string) (Rec, error) {
	r, err := s.Get(id)
	if err != nil {
		return r, errors.New("no such rec")
	}
	if status == "deferred" {
		return r, errors.New("deferred needs a date: use Defer")
	}
	if !statuses[status] || status == "superseded" {
		return r, errors.New("status must be proposed|accepted|declined|done|expired (deferred takes a date)")
	}
	now := s.Now()
	if status == "proposed" {
		// A deferred rec's review_on was its wake-up date, not a scoring date;
		// putting it back on the list by hand drops it too.
		reviewOn := r.ReviewOn
		if r.Status == "deferred" {
			reviewOn = ""
		}
		// The row forgets the decision; item_events keeps it.
		_, err = s.setRec(id, by, note,
			stmt(`UPDATE items SET state='proposed', updated_at=?, resolved_at=NULL, resolved_by=NULL, resolution='' WHERE id=? AND src='rec'`, ts(now), id),
			stmt(`UPDATE recs SET review_on=?, updated_at=? WHERE id=?`, reviewOn, ts(now), id))
	} else {
		// Accepting starts the clock on the answer: something the agent must come
		// back and score. Default 30 days out if the rec did not name a date. A
		// deferred rec's review_on was its wake-up day, not a scoring date, so an
		// accept straight from "later" gets the default too. Same write as the
		// status (it was a second UPDATE with its error dropped: an accepted rec
		// could end up with no review date, or a deferred one's wake-up date).
		reviewOn := r.ReviewOn
		if status == "accepted" && (r.ReviewOn == "" || r.Status == "deferred") {
			reviewOn = now.AddDate(0, 0, 30).Format("2006-01-02")
		}
		_, err = s.setRec(id, by, note,
			stmt(`UPDATE items SET state=?, updated_at=?, resolved_at=?, resolved_by=?, resolution=? WHERE id=? AND src='rec'`,
				status, ts(now), ts(now), by, strings.TrimSpace(note), id),
			stmt(`UPDATE recs SET review_on=?, updated_at=? WHERE id=?`, reviewOn, ts(now), id))
	}
	if err != nil {
		return r, err
	}
	return s.Get(id)
}

// Defer is the third answer: not a decline, but "check back in with me on a
// different day". Neither yes nor no: the rec leaves the open list until
// `until`, when Wake puts it back exactly as it was filed. review_on carries
// the wake-up date — it is the one "come back to this on" column, and a
// deferred rec has no verdict to score. The note is kept like any decision's,
// and copied into the detail on wake so the reason for the wait survives it.
func (s *Store) Defer(id, until, by, note string) (Rec, error) {
	r, err := s.Get(id)
	if err != nil {
		return r, errors.New("no such rec")
	}
	if r.Status != "proposed" && r.Status != "deferred" {
		return r, errors.New("only a proposed rec can be deferred (this one is " + r.Status + ")")
	}
	day, err := time.ParseInLocation("2006-01-02", until, time.Local)
	if err != nil {
		return r, errors.New("until must be YYYY-MM-DD")
	}
	now := s.Now()
	if !day.After(now) {
		return r, errors.New("until must be a future day")
	}
	// Deferring again would overwrite the first note; keep it on the record
	// the way Wake does, so a second "later" reads as a second thought.
	detail := r.Detail
	if r.Status == "deferred" && (r.ReviewOn != until || strings.TrimSpace(r.DecisionNote) != strings.TrimSpace(note)) {
		detail = appendLine(detail, deferralLine(r))
	}
	_, err = s.setRec(id, by, "until "+until+" "+strings.TrimSpace(note),
		stmt(`UPDATE items SET state='deferred', updated_at=?, resolved_at=?, resolved_by=?, resolution=?, detail=? WHERE id=? AND src='rec'`,
			ts(now), ts(now), by, strings.TrimSpace(note), detail, id),
		stmt(`UPDATE recs SET review_on=?, updated_at=? WHERE id=?`, until, ts(now), id))
	if err != nil {
		return r, err
	}
	return s.Get(id)
}

// Reply keeps a line the owner sent without deciding. It is on the record the way a
// decision note is — appended to the detail, where Wake writes a deferral, so
// the next reader sees it — and changes nothing else: status, dates and the
// verdict slot are untouched. Any status takes one; a follow-up on a decided
// rec is still worth keeping.
func (s *Store) Reply(id, by, note string) (Rec, error) {
	r, err := s.Get(id)
	if err != nil {
		return r, errors.New("no such rec")
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return r, errors.New("a reply needs a note")
	}
	if by == "" {
		by = "owner"
	}
	now := s.Now()
	detail := strings.TrimSpace(r.Detail)
	if detail != "" {
		detail += "\n\n"
	}
	detail += fmt.Sprintf("_%s replied on %s: %s_", by, now.Format("2006-01-02"), note)
	if _, err := s.db.Exec(`UPDATE items SET detail=?, updated_at=? WHERE id=? AND src='rec'`, detail, ts(now), id); err != nil {
		return r, err
	}
	return s.Get(id)
}

// Dated: what the agenda shows for recs in [from, to] — the deferred ones on
// their review_on ("check back on X" on the day the owner named), EVERY one on the minute it was filed, and the record of every
// decision and score. Actor is the rec's source.
func (s *Store) Dated(from, to string) ([]store.Dated, error) {
	rows, err := s.db.Query(`SELECT `+cols+joined+`i.state='deferred' AND l.review_on>=? AND l.review_on<=? ORDER BY l.review_on, i.created_at`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Dated{}
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, Row(r))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filed, err := s.filed(from, to)
	if err != nil {
		return nil, err
	}
	out = append(out, filed...)
	did, err := s.did(from, to)
	if err != nil {
		return nil, err
	}
	return append(out, did...), nil
}

// filed: EVERY rec on the minute it was made. Without it an undecided rec had
// no day: a rec only reached the agenda once the owner had touched it
// (deferred, on the day they named; decided, as the deed), so the purple lane
// was empty on exactly the weeks where the recs were piling up.
//
// Two bars per rec: a dark one at the minute it was filed, a light one when
// it was accepted, declined or completed. So the filed row is minted for
// every status, decided ones included, and `did()` adds the light bar at
// decided_at. The filed row's State is the rec's CURRENT status, not the
// filing's — so the dark bar reads ✓ faded once accepted, ✕ struck once
// declined or expired, and full colour only while it is still the owner's to
// answer. It keeps its dark
// shade (it is still the minute it was filed) and is not a `did` row.
// Recs are pull, so seeing them is the whole point
// (docs/design/recommendations.md).
func (s *Store) filed(from, to string) ([]store.Dated, error) {
	a, b, err := instantWindow(from, to)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+cols+joined+`i.created_at>=? AND i.created_at<? ORDER BY i.created_at`, a, b)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Dated{}
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		d := Row(r)
		// Its own key: a rec can be both filed and (later) checked back on,
		// and two rows sharing an id is a row the console cannot open.
		d.Key, d.Verb, d.State = "filed:rec:"+r.ID, "Filed", r.Status
		d.Day, d.At = store.Day(r.CreatedAt), store.Clock(r.CreatedAt)
		if d.Day >= from && d.Day <= to {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// instantWindow: [from, to] local days as the UTC-stamp bounds to query an
// instant column with — a day wide each side, because the stamps are UTC and
// the days are Eastern; the exact day test is done on the rows.
func instantWindow(from, to string) (string, string, error) {
	lo, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return "", "", errors.New("from must be YYYY-MM-DD")
	}
	hi, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return "", "", errors.New("to must be YYYY-MM-DD")
	}
	return ts(lo.AddDate(0, 0, -1)), ts(hi.AddDate(0, 0, 2)), nil
}

// did: the record half of the agenda — long-lived recs can be done at any
// time, and doing one belongs on the calendar. A rec accepted or declined
// lands at the minute it was decided — the owner's deed when decided_by is
// `owner`, a session's when a thread took it; the same rec scored later lands
// again at its outcome, as the word it was given. Instants are UTC, local days;
// the SQL window is a day wide each side and the exact test is here.
func (s *Store) did(from, to string) ([]store.Dated, error) {
	a, b, err := instantWindow(from, to)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+cols+joined+`i.state IN ('accepted','declined','done')
		AND ((i.resolved_at IS NOT NULL AND i.resolved_at>=? AND i.resolved_at<?) OR (l.outcome_at IS NOT NULL AND l.outcome_at>=? AND l.outcome_at<?))
		ORDER BY i.resolved_at`, a, b, a, b)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Dated{}
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		for _, d := range DidRows(r) {
			if d.Day >= from && d.Day <= to {
				out = append(out, d)
			}
		}
	}
	return out, rows.Err()
}

// DidRows: the record rows one rec leaves behind — its decision, and its
// score if it has one. Each keys on the rec plus the deed, so a rec decided
// and scored on one day is two rows that both open the rec's page.
func DidRows(r Rec) []store.Dated {
	var out []store.Dated
	if r.DecidedAt != nil && r.DecidedBy != "" {
		verb := store.RecDidVerb(r.Status)
		if verb != "" {
			d := Row(r)
			d.Key, d.Did, d.Verb, d.Actor = "did:rec:"+r.ID, true, verb, r.DecidedBy
			d.Day, d.At = store.Day(*r.DecidedAt), store.Clock(*r.DecidedAt)
			out = append(out, d)
		}
	}
	if r.OutcomeAt != nil && r.Outcome != "" {
		d := Row(r)
		// Score records no scorer of its own; the decider is the best guess.
		d.Key, d.Did, d.Actor = "did:rec:"+r.ID+":outcome", true, r.DecidedBy
		if d.Actor == "" {
			d.Actor = "owner"
		}
		d.Verb = store.RecScoreVerb(r.Outcome)
		d.State = "done"
		d.Day, d.At = store.Day(*r.OutcomeAt), store.Clock(*r.OutcomeAt)
		out = append(out, d)
	}
	return out
}

// Row projects one rec into the shared read-model row (store.Dated, Phase 5).
// Its day is the day the owner asked to be asked again; a rec is never "your turn" —
// recs are pulled, never pushed (docs/design/recommendations.md) — so it
// carries no surface and is never held.
func Row(r Rec) store.Dated {
	return store.Dated{
		// No Detail: the agenda row is "Check back: <title>" and opens the rec's
		// own page for the rest (Phase 2b).
		ID: r.ID, Kind: "rec", Ref: "rec:" + r.ID, Title: r.Title, State: r.Status,
		Day: r.ReviewOn, ThreadID: r.ThreadID, GoalID: r.GoalID, Actor: r.Source, Window: r.Window, Obj: r,
	}
}

// Open: the recs still the owner's to answer — proposed, or parked until a
// day they named — as rows, newest first; this table's share of store.Items.
func (s *Store) Open() ([]store.Dated, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT ` + cols + joined + `i.state IN ('proposed','deferred') ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Dated
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, Row(r))
	}
	return out, rows.Err()
}

// Wake puts deferred recs whose day has come back on the open list — status
// proposed, undecided, as if newly filed — so the owner sees it the next
// time they come looking, which is still pull. The deferral itself is written into the
// detail first: "check back in September" is context the next reader needs.
// Returns how many it woke. Run with Expire.
func (s *Store) Wake() (int, error) {
	today := s.Now().Format("2006-01-02")
	rows, err := s.db.Query(`SELECT `+cols+joined+`i.state='deferred' AND l.review_on!='' AND l.review_on<=?`, today)
	if err != nil {
		return 0, err
	}
	var due []Rec
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, r := range due {
		detail := appendLine(r.Detail, deferralLine(r))
		if _, err := s.setRec(r.ID, "hub", "woke: its deferral ran out",
			stmt(`UPDATE items SET state='proposed', updated_at=?, resolved_at=NULL, resolved_by=NULL, resolution='', detail=? WHERE id=? AND src='rec'`,
				ts(s.Now()), detail, r.ID),
			stmt(`UPDATE recs SET review_on='', updated_at=? WHERE id=?`, ts(s.Now()), r.ID)); err != nil {
			return 0, err
		}
	}
	if len(due) > 0 {
		log.Printf("recs: woke %d deferred", len(due))
	}
	return len(due), nil
}

// deferralLine: one italic line saying when and why a rec was parked, written
// into detail by Wake (and by a second Defer) so the reason survives the wait.
func deferralLine(r Rec) string {
	when := ""
	if r.DecidedAt != nil {
		when = " on " + r.DecidedAt.Local().Format("2006-01-02")
	}
	line := fmt.Sprintf("Deferred%s until %s (%s)", when, r.ReviewOn, r.DecidedBy)
	if n := strings.TrimSpace(r.DecisionNote); n != "" {
		line += ": " + n
	}
	return "_" + line + "_"
}

func appendLine(detail, line string) string {
	detail = strings.TrimSpace(detail)
	if detail != "" {
		detail += "\n\n"
	}
	return detail + line
}

// Score writes what actually happened, which is the only reason this table
// exists. Called by the review loop (or the owner), long after the deed.
func (s *Store) Score(id, outcome, by, note string) (Rec, error) {
	r, err := s.Get(id)
	if err != nil {
		return r, errors.New("no such rec")
	}
	if !outcomes[outcome] {
		return r, errors.New("outcome must be worked|mixed|failed|unclear")
	}
	now := s.Now()
	scorer := by
	if scorer == "" {
		scorer = "hub"
	}
	// The score is the ledger's; the item only records that it happened (and,
	// with no decider on record, who scored it).
	if _, err := s.setRec(id, scorer, "scored "+outcome+": "+strings.TrimSpace(note),
		stmt(`UPDATE items SET updated_at=?, resolved_by=CASE WHEN ?<>'' THEN COALESCE(resolved_by,?) ELSE resolved_by END WHERE id=? AND src='rec'`, ts(now), by, by, id),
		stmt(`UPDATE recs SET outcome=?, outcome_at=?, outcome_note=?, updated_at=? WHERE id=?`,
			outcome, ts(now), strings.TrimSpace(note), ts(now), id)); err != nil {
		return r, err
	}
	return s.Get(id)
}

// Link attaches the ids a rec minted when it was accepted (cal-…, ask-…,
// act-…), so the record and the deed stay joined.
func (s *Store) Link(id string, refs ...string) (Rec, error) {
	r, err := s.Get(id)
	if err != nil {
		return r, errors.New("no such rec")
	}
	seen := map[string]bool{}
	all := []string{}
	for _, v := range append(strings.Split(r.Links, ","), refs...) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		all = append(all, v)
	}
	if _, err := s.db.Exec(`UPDATE recs SET links=?, updated_at=? WHERE id=?`, strings.Join(all, ","), ts(s.Now()), id); err != nil {
		return r, err
	}
	if _, err := s.db.Exec(`UPDATE items SET updated_at=? WHERE id=? AND src='rec'`, ts(s.Now()), id); err != nil {
		return r, err
	}
	return s.Get(id)
}

// Expire moves still-undecided recs past their act_by out of the list, so it
// stays a list of live ideas. Returns how many it closed. Run daily.
func (s *Store) Expire() (int, error) {
	today := s.Now().Format("2006-01-02")
	// One row at a time, through the one writer, so each expiry is on its
	// rec's trail; the guard is the old bulk WHERE, per row.
	rows, err := s.db.Query(`SELECT i.id`+joined+`i.state='proposed' AND l.act_by!='' AND l.act_by<?`, today)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		moved, err := s.setRec(id, "hub", "act_by passed with no decision",
			stmt(`UPDATE items SET state='expired', updated_at=?, resolved_at=?, resolved_by='hub',
		resolution='act_by passed with no decision' WHERE id=? AND src='rec' AND state='proposed'
		AND EXISTS (SELECT 1 FROM recs WHERE id=items.id AND act_by!='' AND act_by<?)`,
				ts(s.Now()), ts(s.Now()), id, today))
		if err != nil {
			return n, err
		}
		if moved {
			n++
		}
	}
	if n > 0 {
		log.Printf("recs: expired %d past act_by", n)
	}
	return int(n), nil
}

// DomainStat: the track record within one domain.
type DomainStat struct {
	Domain     string `json:"domain"`
	Total      int    `json:"total"`
	Accepted   int    `json:"accepted"`
	Declined   int    `json:"declined"`
	Worked     int    `json:"worked"`
	Failed     int    `json:"failed"`
	Scored     int    `json:"scored"`
	MonthlyUSD string `json:"monthly_usd"`
}

// ModelStat: the track record of one model — which of them is worth listening
// to. Recs the owner filed themselves fall under model "owner".
type ModelStat struct {
	Model    string `json:"model"`
	Total    int    `json:"total"`
	Accepted int    `json:"accepted"`
	Declined int    `json:"declined"`
	Worked   int    `json:"worked"`
	Failed   int    `json:"failed"`
	Scored   int    `json:"scored"`
}

// Stats is the point of the whole table: is the agent worth listening to, and
// what is its advice costing. AcceptRate is over decided recs (a rec the owner has
// not looked at yet is not a no); HitRate is over scored ones.
type Stats struct {
	Total       int          `json:"total"`
	Proposed    int          `json:"proposed"`
	Deferred    int          `json:"deferred"` // waiting for a day the owner named; not a decision, not open
	Accepted    int          `json:"accepted"`
	Declined    int          `json:"declined"`
	Expired     int          `json:"expired"`
	Superseded  int          `json:"superseded"`
	Scored      int          `json:"scored"`
	Worked      int          `json:"worked"`
	Mixed       int          `json:"mixed"`
	Failed      int          `json:"failed"`
	Unclear     int          `json:"unclear"`
	AcceptRate  float64      `json:"accept_rate"`
	HitRate     float64      `json:"hit_rate"`
	DueForRev   int          `json:"due_for_review"`
	MonthlyUSD  string       `json:"monthly_usd"` // recurring cost of everything the owner said yes to
	OneOffUSD   string       `json:"one_off_usd"` // one-off spend the owner said yes to — kind=trade excluded
	TradedUSD   string       `json:"traded_usd"`  // accepted trades: money moved inside the owner's accounts, not spent
	ByDomain    []DomainStat `json:"by_domain"`
	ByModel     []ModelStat  `json:"by_model"`
	OldestOpen  string       `json:"oldest_open,omitempty"`
	LastScored  string       `json:"last_scored,omitempty"`
	AsOfDay     string       `json:"as_of"`
	UnscoredDue []string     `json:"unscored_due,omitempty"`
}

func usd(cents int) string { return fmt.Sprintf("%.2f", float64(cents)/100) }

func (s *Store) Stats() (Stats, error) {
	all, err := s.List(Filter{Status: "all"})
	if err != nil {
		return Stats{}, err
	}
	today := s.Now().Format("2006-01-02")
	st := Stats{Total: len(all), AsOfDay: today}
	byDom := map[string]*DomainStat{}
	byModel := map[string]*ModelStat{}
	monthly, oneOff, traded := 0, 0, 0
	domMonthly := map[string]int{}
	for _, r := range all {
		d := byDom[r.Domain]
		if d == nil {
			d = &DomainStat{Domain: r.Domain}
			byDom[r.Domain] = d
		}
		d.Total++
		mk := r.Model
		if mk == "" {
			mk = "owner"
			if r.Source != "owner" {
				mk = "unknown"
			}
		}
		mo := byModel[mk]
		if mo == nil {
			mo = &ModelStat{Model: mk}
			byModel[mk] = mo
		}
		mo.Total++
		switch r.Status {
		case "proposed":
			st.Proposed++
			// Local, not UTC: in the evening a UTC stamp can read as tomorrow
			// and "oldest open" lands in the future.
			if d := r.CreatedAt.Local().Format("2006-01-02"); st.OldestOpen == "" || d < st.OldestOpen {
				st.OldestOpen = d
			}
		case "deferred":
			st.Deferred++
		case "accepted", "done":
			st.Accepted++
			d.Accepted++
			mo.Accepted++
			monthly += r.MonthlyCents()
			domMonthly[r.Domain] += r.MonthlyCents()
			// A trade's cost is what moved, not what left: trades stay out of
			// "spend".
			if r.CostPeriod == "" && r.Kind == "trade" {
				traded += r.CostCents
			} else if r.CostPeriod == "" {
				oneOff += r.CostCents
			}
		case "declined":
			st.Declined++
			d.Declined++
			mo.Declined++
		case "expired":
			st.Expired++
		case "superseded":
			st.Superseded++
		}
		if r.Outcome != "" {
			st.Scored++
			d.Scored++
			mo.Scored++
			switch r.Outcome {
			case "worked":
				st.Worked++
				d.Worked++
				mo.Worked++
			case "mixed":
				st.Mixed++
			case "failed":
				st.Failed++
				d.Failed++
				mo.Failed++
			case "unclear":
				st.Unclear++
			}
			if r.OutcomeAt != nil {
				if ds := r.OutcomeAt.Local().Format("2006-01-02"); ds > st.LastScored {
					st.LastScored = ds
				}
			}
		} else if r.ReviewOn != "" && r.ReviewOn <= today && (r.Status == "accepted" || r.Status == "done") {
			st.DueForRev++
			st.UnscoredDue = append(st.UnscoredDue, r.ID)
		}
	}
	if decided := st.Accepted + st.Declined; decided > 0 {
		st.AcceptRate = float64(st.Accepted) / float64(decided)
	}
	// Mixed counts as half a hit: most advice is neither clean win nor loss.
	if judged := st.Worked + st.Mixed + st.Failed; judged > 0 {
		st.HitRate = (float64(st.Worked) + 0.5*float64(st.Mixed)) / float64(judged)
	}
	st.MonthlyUSD, st.OneOffUSD, st.TradedUSD = usd(monthly), usd(oneOff), usd(traded)
	for name, d := range byDom {
		d.MonthlyUSD = usd(domMonthly[name])
		st.ByDomain = append(st.ByDomain, *d)
	}
	sort.Slice(st.ByDomain, func(i, j int) bool { return st.ByDomain[i].Total > st.ByDomain[j].Total })
	for _, mo := range byModel {
		st.ByModel = append(st.ByModel, *mo)
	}
	sort.Slice(st.ByModel, func(i, j int) bool {
		if st.ByModel[i].Total != st.ByModel[j].Total {
			return st.ByModel[i].Total > st.ByModel[j].Total
		}
		return st.ByModel[i].Model < st.ByModel[j].Model
	})
	return st, nil
}
