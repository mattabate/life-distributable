package threads

// Asks: the things an agent needs from the owner, as first-class rows instead
// of a flag on the thread. An agent raises one with `lifectl ask add` while it
// works; the hub links it to the run and, at finish, to the reply message, so
// the app can jump to where it was raised. The owner's reply marks it
// answered (still on the board); the agent closes it explicitly, or the owner
// swipes Done / Dismiss. Never deleted.

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"life/hub/internal/notify"
	"life/hub/internal/spend"
	"life/hub/internal/store"
)

// AsksSchema: asks and their event log. Append an ALTER to add a column
// (store/migrate.go).
var AsksSchema = []string{`CREATE TABLE IF NOT EXISTS asks (
	id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	thread_id TEXT NOT NULL, run_id TEXT, message_id INTEGER, goal_id TEXT,
	title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT 'other',       -- decision | access | physical | read | error | other
	state TEXT NOT NULL DEFAULT 'open',       -- open | answered | done | dismissed | superseded
	check_hint TEXT NOT NULL DEFAULT '',      -- how a verifier could tell it is done ('' = only owner/agent can)
	resolved_at TEXT, resolved_by TEXT, resolution TEXT NOT NULL DEFAULT '', superseded_by TEXT);
CREATE INDEX IF NOT EXISTS asks_state ON asks(state, created_at);
CREATE INDEX IF NOT EXISTS asks_thread ON asks(thread_id, id);
CREATE TABLE IF NOT EXISTS ask_events (
	id INTEGER PRIMARY KEY, ask_id TEXT NOT NULL, ts TEXT NOT NULL,
	actor TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, note TEXT NOT NULL DEFAULT '');`,
	// surface: mobile | web | any — where the ask gets done (surface.go). The
	// phone's board only counts mobile+any; `web` items live behind one folded
	// row there and are the owner's turn in the console instead.
	`ALTER TABLE asks ADD COLUMN surface TEXT NOT NULL DEFAULT 'any'`,
	// class: WHY the owner is asked — unblock | read | install | practice |
	// step. Kind is the verb (what answering means); class is who is waiting.
	// A session stopped on a billing account (unblock) and a daily reminder
	// (practice) both carry kind `physical`, and without class the Sessions
	// page would wear one red pill for both. Backfilled at boot
	// (backfillAskClass).
	`ALTER TABLE asks ADD COLUMN class TEXT NOT NULL DEFAULT ''`,
	// said: the sentence this card's push SPOKE, word for word — `--say` when
	// the session wrote one, else the spokenFallback the hub made. Passed only
	// to APNs, it would be dropped on the floor and the only copy of what the
	// owner heard would live in a transcript; kept here, the card can be
	// reread. Rows written before this column are '' and show no spoken line.
	`ALTER TABLE asks ADD COLUMN said TEXT NOT NULL DEFAULT ''`,
	// Every ask is an `items` row (src ask)
	// and every move an `item_events` row; `asks`/`ask_events` are frozen at
	// this copy — history, never written again.
	`INSERT OR IGNORE INTO items (id,src,created_at,updated_at,title,detail,kind,state,thread_id,run_id,message_id,goal_id,class,surface,check_hint,said,resolved_at,resolved_by,resolution,superseded_by)
	SELECT id,'ask',created_at,updated_at,title,detail,kind,state,thread_id,run_id,message_id,goal_id,class,surface,check_hint,said,resolved_at,resolved_by,resolution,superseded_by FROM asks ORDER BY created_at;
INSERT INTO item_events (item_id,ts,actor,from_state,to_state,note) SELECT ask_id,ts,actor,from_state,to_state,note FROM ask_events ORDER BY id;
UPDATE items SET ` + store.ItemStampSet + ` WHERE src='ask';`,
}

// StepThread: the hub's idle session that hosts the cards of steps no
// session is behind (the calendar's fallback thread).
const StepThread = "calendar"

// The rows an Ask is read from: every ask, and every step of the owner's
// that has fired — a fired cal row IS its card now, where it used to mint an
// ask on its day. A step that fired before that change carries the
// ask it minted (ask_id), and that ask is the card instead. askThread is the
// session a card sits on: a sessionless step's is StepThread, as its ask's was.
const (
	askThread = `COALESCE(NULLIF(a.thread_id,''),'` + StepThread + `')`
	askIs     = `(a.src='ask' OR (a.src='cal' AND a.kind<>'agent' AND a.fired_at IS NOT NULL AND a.state<>'scheduled' AND COALESCE(a.ask_id,'')=''))`
	askFrom   = `items a LEFT JOIN threads t ON t.id=` + askThread
)

// Ask classes. The Sessions page counts only the first three — those are the
// ones where something is BLOCKED on the owner (an agent, a build, a finding
// they have not seen). A `practice` (their daily homework) and a `step` (a
// dated thing they do) are the calendar's: they never make a session "your turn"
// and never turn a thread needs_you.
const (
	ClassUnblock  = "unblock"
	ClassRead     = "read"
	ClassInstall  = "install"
	ClassPractice = "practice"
	ClassStep     = "step"
)

var askClasses = map[string]bool{ClassUnblock: true, ClassRead: true, ClassInstall: true, ClassPractice: true, ClassStep: true}

// ClassFor is the class an ask takes from its kind when nobody says
// otherwise: read → read, install → install, everything else unblocks.
func ClassFor(kind string) string {
	switch kind {
	case "read":
		return ClassRead
	case "install":
		return ClassInstall
	}
	return ClassUnblock
}

// OwnerClass: the ask is a thing the OWNER does on their own clock (a practice
// or a step) — nothing waits on it, so it is the calendar's row, not a session's.
func OwnerClass(class string) bool { return class == ClassPractice || class == ClassStep }

type Ask struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ThreadID    string    `json:"thread_id"`
	ThreadTitle string    `json:"thread_title,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	MessageID   int64     `json:"message_id,omitempty"`
	GoalID      string    `json:"goal_id,omitempty"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail"`
	Kind        string    `json:"kind"`
	// Class: why the owner is asked — unblock | read | install | practice |
	// step (askClasses). Kind says what answering means; Class says whether
	// anything is waiting on them. Both surfaces count a session's "for you"
	// from it: a practice or a step is the calendar's, never a session's.
	Class string `json:"class"`
	// Surface: where this gets done — mobile | web | any (surface.go). The
	// phone's board hides `web` behind one folded row; the console shows
	// everything, because that is where a `web` ask is actually acted on.
	Surface   string `json:"surface"`
	State     string `json:"state"`
	CheckHint string `json:"check_hint"`
	// Said: what was actually read out when this card was raised — the
	// session's `--say`, or the hub's spokenFallback when it wrote none.
	// Stored so the card the owner opens contains the words they heard; both
	// surfaces draw it under the detail, quiet and marked as speech, never as
	// the first line (the card still leads with the one thing). Empty on old
	// rows and on any card that never pushed.
	Said string `json:"said,omitempty"`
	// WaitingToSpeak: this card's line is queued for the owner's ears and not
	// yet heard (Voice.CardWaiting). Both surfaces turn Play into "Waiting to
	// speak", and a double tap drops the line, never the card (POST
	// /voice/hush).
	WaitingToSpeak bool `json:"waiting_to_speak,omitempty"`
	// Speaking: this card's line is in the owner's ears right now
	// (Voice.CardSpeaking). Play reads "Speaking" instead, and a double tap
	// stops it.
	Speaking bool `json:"speaking,omitempty"`
	// CalID/CalDay: the calendar item that minted this ask, if any. A dated
	// step ("renew the passport on the 27th") is a CALENDAR entry that happens
	// to need the owner, not a session checking in. Both surfaces use these to badge the card with its date and drop the
	// owning session's name and running cost, which are noise on a reminder.
	CalID        string     `json:"cal_id,omitempty"`
	CalDay       string     `json:"cal_day,omitempty"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy   string     `json:"resolved_by,omitempty"`
	Resolution   string     `json:"resolution,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`
	// ThreadRunning: the session that raised this is running RIGHT NOW — on
	// this ask or on something else — so the card can say so while the owner
	// answers it. A card whose session is still running need not be acted on
	// yet, so every card wears it, the console sinks a running
	// session's bundle under the idle ones, and a reply to a running session
	// is a steer, read at its next step, not a wake.
	ThreadRunning bool `json:"thread_running"`
	// Outcomes: the ways the owner can answer THIS card, in the order a
	// surface draws them — the card's row AND the composer's chips, the same
	// order on both: the decisive ones first, "Reply" (value "") last.
	// Computed from Kind (store/close.go AskOutcomes) so the vocabulary lives
	// in one place and both surfaces draw the same chips. A finding is
	// acknowledged ("Got it"), a decision is decided, an access ask is
	// granted — the words differ because the act differs, and a wrong word
	// makes the owner doubt whether the card needed them at all.
	Outcomes []Outcome `json:"outcomes"`
	// Verb: what the card wants from the owner in one word ("decide", "grant",
	// "restart"…) — the card's head line and the Sessions cell's corner on
	// both surfaces (store.AskVerb).
	Verb string `json:"verb"`
	// Open / Closed / Folded / Lane: where the card stands with the owner
	// (store.AskStanding) — the one answer to "is this still theirs", which both
	// clients used to work out from `state` with lists that disagreed.
	Open   bool   `json:"open"`
	Closed bool   `json:"closed"`
	Folded string `json:"folded,omitempty"`
	Lane   string `json:"lane"`
	Reopen bool   `json:"reopen,omitempty"`
	// Target: which device an install card updates — "phone" (the OTA link
	// is the button) or "mac" (the button asks the hub to swap the staged
	// desktop build in, 2026-09-30). Empty on every other kind.
	Target string `json:"target,omitempty"`
	// ResumesAt: a session-limit card (an error whose detail names the reset,
	// spend.ResetAt) — when the hub starts the turn again by itself
	// (ResumePaused). Both surfaces draw it as the paused card: amber, the
	// wait as a bar toward this time, Resume now. Nil on every other card.
	ResumesAt *time.Time `json:"resumes_at,omitempty"`
	// Window: when it is owed — now (a card), or a step's on | by its day, or
	// soon (store.ItemStampSet, the item's own `win`).
	Window string `json:"window"`
}

// Step: this card is one of the owner's calendar steps itself (a fired cal row), not
// an ask — it closes through the calendar (Manager.ResolveCal).
func (a Ask) Step() bool { return a.CalID != "" && a.CalID == a.ID }

// stand stamps the card's standing from its own fields.
func (a *Ask) stand() {
	chore := a.Class == ClassStep && store.Chore("owner", a.CalDay, a.ThreadID)
	s := store.AskStanding(a.Kind, a.Class, a.State, a.Resolution, a.ResolvedBy, chore)
	a.Open, a.Closed, a.Folded, a.Lane, a.Reopen = s.Open, s.Closed, s.Folded, s.Lane, s.Reopen
}

// Outcome is one answer chip: the prompts `outcome` it posts ("" = words only,
// the card stays open; done | wont close it) and the label a surface shows.
// The shape is the hub's one card vocabulary (store.Outcome): proposals and
// recs carry the same `outcomes`.
type Outcome = store.Outcome

// OutcomesFor returns the answer chips for a card kind — the one vocabulary,
// store.AskOutcomes (store/close.go). Changing a label there changes every
// card on both surfaces at once; the phone's swipe Done keeps posting `done`
// whatever the label says.
func OutcomesFor(kind string) []Outcome { return store.AskOutcomes(kind) }

// Active = still on the owner's board.
func (a Ask) Active() bool { return a.State == "open" || a.State == "answered" }

// Blocks = something is waiting on the owner (a session, a build, a
// finding) — the ask counts on Sessions and turns its thread needs_you. A
// practice or a step does neither: it is the owner's own dated work, listed
// by the calendar.
func (a Ask) Blocks() bool { return !OwnerClass(a.Class) }

// "error" is the hub's own kind, not one an agent raises: a run that died
// (retry.go). It reads and behaves differently everywhere — one line, no
// Open link, a Restart button — because it is not work the owner has to do.
//
// "install" is the app-update cell: title, description and one Install
// button. Both surfaces draw it teal with one Install button instead of the red Respond furniture. AddAskOn
// retags any "Install app build N" title to it, so old-habit sessions still
// get the cell.
var askKinds = map[string]bool{"decision": true, "access": true, "physical": true, "read": true, "install": true, "error": true, "other": true}

// SetAskKind retags a card's kind (`lifectl ask <id> kind physical`) — the
// escape hatch for a card wearing the wrong vocabulary: a "Move your DNS — 8
// steps" card filed as `access` offers Granted / Won't grant, but nobody is
// granting anything — the owner is being asked to DO the steps, which is
// `physical` ("I did this"). The chips follow
// the kind, so retagging redraws both surfaces at once. `error` is the hub's
// own kind and carries the Restart machinery: nothing retags to or from it.
func (m *Manager) SetAskKind(id, kind string) (Ask, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return Ask{}, err
	}
	if !askKinds[kind] || kind == "error" {
		return Ask{}, fmt.Errorf("kind must be decision|access|physical|read|install|other")
	}
	if a.Kind == "error" {
		return Ask{}, errors.New("an error card keeps its kind")
	}
	// A step's card kind is its dated-ask kind (the row's own kind says it
	// is a step).
	col := "kind"
	if a.Step() {
		col = "ask_kind"
	}
	if _, err := m.db.Exec(`UPDATE items SET `+col+`=?, updated_at=? WHERE id=?`, kind, ts(time.Now()), id); err != nil {
		return Ask{}, err
	}
	store.StampItem(m.db, id)
	return m.GetAsk(id)
}

// RewordAsk rewrites an open card's words in place — title, the message it
// leads with (`said`; a dated step's `say` until it fires), its steps — so a
// card the owner's later words made stale reads true again instead of being
// closed and raised anew. Quiet: no push, no wake; the row keeps its state and
// the trail records the rewrite. "" leaves a field as it is. A closed card
// keeps its words (they are what the owner answered), and an error card is the
// hub's, not an agent's.
func (m *Manager) RewordAsk(id, title, detail, say, by string) (Ask, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return Ask{}, errors.New("no such ask")
	}
	if a.State != "open" && a.State != "answered" {
		return Ask{}, errors.New("a closed card keeps its words")
	}
	if a.Kind == "error" {
		return Ask{}, errors.New("an error card is the hub's")
	}
	title, detail, say = strings.TrimSpace(title), strings.TrimSpace(detail), strings.TrimSpace(say)
	if title == "" && detail == "" && say == "" {
		return Ask{}, errors.New("nothing to change: give --title, --say or --detail")
	}
	now := ts(time.Now())
	sets, args, changed := []string{"updated_at=?"}, []any{now}, []string{}
	if title != "" {
		if len(title) > 200 {
			title = title[:200]
		}
		sets, args, changed = append(sets, "title=?"), append(args, title), append(changed, "title")
	}
	if detail != "" {
		sets, args, changed = append(sets, "detail=?"), append(args, detail), append(changed, "detail")
	}
	if say != "" {
		if a.Step() && a.State != "open" {
			sets, args = append(sets, "say=?"), append(args, say)
		} else {
			// The card leads with `said` on both surfaces and Play speaks it;
			// a fired step is a card like any other.
			sets, args = append(sets, "said=?, say=?"), append(args, say, say)
		}
		changed = append(changed, "message")
	}
	args = append(args, id)
	tx, err := m.db.Begin()
	if err != nil {
		return Ask{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE items SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
		return Ask{}, err
	}
	if err := store.StampItem(tx, id); err != nil {
		return Ask{}, err
	}
	if _, err := tx.Exec(`INSERT INTO item_events (item_id, ts, actor, from_state, to_state, note) VALUES (?,?,?,?,?,?)`,
		id, now, by, a.State, a.State, "reworded: "+strings.Join(changed, ", ")); err != nil {
		return Ask{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ask{}, err
	}
	log.Printf("ask %s: reworded (%s) by %s", id, strings.Join(changed, ", "), by)
	return m.GetAsk(id)
}

func newAskID() string { return store.NewID("ask") }

func normTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "."))), " ")
}

// AskSeen reports whether this thread has ever carried an ask with this
// title, in any state. A recurring hub check (the reconcile tick) asks first
// so a finding the owner has already read stays closed: AddAsk only supersedes the
// ACTIVE twin, so without this a dismissed card would come back every pass.
func (m *Manager) AskSeen(threadID, title string) bool {
	rows, err := m.db.Query(`SELECT title FROM items WHERE src='ask' AND thread_id=?`, threadID)
	if err != nil {
		return false
	}
	defer rows.Close()
	want := normTitle(title)
	for rows.Next() {
		var t string
		rows.Scan(&t)
		if normTitle(t) == want {
			return true
		}
	}
	return false
}

// AddAsk raises an ask on a thread. runID may be "" (raised outside a run,
// e.g. by hand). An open ask with the same title on the same thread is
// superseded rather than duplicated. Its surface is inferred from what it says
// (surface.go); AddAskOn takes it explicitly.
func (m *Manager) AddAsk(threadID, runID, title, detail, kind, check string) (Ask, error) {
	return m.AddAskOn(threadID, runID, title, detail, kind, check, "")
}

// AddAskOn is AddAsk with the surface named: "mobile" (done on the phone),
// "web" (done at the Mac), "any", or "" to infer it.
func (m *Manager) AddAskOn(threadID, runID, title, detail, kind, check, surface string) (Ask, error) {
	return m.AddAskAs(threadID, runID, title, detail, kind, check, surface, "")
}

// AddAskAs is AddAskOn with the class named ("" = ClassFor(kind)). Only the
// calendar names one: a fired `homework` item is a `practice`, a fired
// `owner` item a `step` — the same `physical` kind either way, so the class is
// the only thing that tells a Sessions row it has nothing to count.
func (m *Manager) AddAskAs(threadID, runID, title, detail, kind, check, surface, class string) (Ask, error) {
	return m.addAsk(threadID, runID, title, detail, kind, check, surface, class, "")
}

// AddAskAsSaid is AddAskAs with its spoken message: a dated ask carries the
// `--say` its session wrote to the day it fires.
func (m *Manager) AddAskAsSaid(threadID, runID, title, detail, kind, check, surface, class, say string) (Ask, error) {
	return m.addAsk(threadID, runID, title, detail, kind, check, surface, class, say)
}

// AddAskSaid is AddAskOn with the sentence its push SPEAKS (`lifectl ask add
// --say`): heard instead of "To read — <title>", so it stands on its own —
// what the owner asked for, what was done, what is needed — and never points
// at "this session" or "my last message", which mean nothing heard out of
// context. "" = the label form. It
// is only said, never stored: the card is its title and detail.
func (m *Manager) AddAskSaid(threadID, runID, title, detail, kind, check, surface, say string) (Ask, error) {
	return m.addAsk(threadID, runID, title, detail, kind, check, surface, "", say)
}

// spokenFallback is what a card with no `--say` speaks: notify.Spoken over
// the card's title and its session's name with their markdown taken out.
func spokenFallback(kind, class, title, thread string) string {
	if t := installTarget(kind, title); t != "" && kind == "install" {
		return notify.InstallSpoken(t, askBuild(kind, title))
	}
	return notify.Spoken(kind, class, plainTitle(title), plainTitle(thread))
}

func (m *Manager) addAsk(threadID, runID, title, detail, kind, check, surface, class, say string) (Ask, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Ask{}, errors.New("title required")
	}
	if len(title) > 200 {
		title = title[:200]
	}
	if kind == "" {
		kind = "other"
	}
	// An "Install app build N" ask IS an install whatever the session called
	// it — the teal cell must show up every time, not only when the raising
	// session knew the new kind.
	if installBuild(title) > 0 && kind != "read" {
		kind = "install"
	}
	if !askKinds[kind] {
		return Ask{}, fmt.Errorf("kind must be decision|access|physical|read|install|other")
	}
	if class == "" {
		class = ClassFor(kind)
	}
	if !askClasses[class] {
		return Ask{}, fmt.Errorf("class must be unblock|read|install|practice|step")
	}
	t, err := m.Get(threadID)
	if err != nil {
		return Ask{}, err
	}
	now := time.Now()
	a := Ask{ID: newAskID(), CreatedAt: now, UpdatedAt: now, ThreadID: threadID, RunID: runID, GoalID: t.GoalID,
		Title: title, Detail: ListifyDetail(detail), Kind: kind, Class: class, State: "open", CheckHint: strings.TrimSpace(check),
		Surface: normSurface(surface, kind, title, detail)}
	// supersede same-titled active asks on this thread — and, for
	// "Install app build N" asks, every active install ask with N or lower on
	// ANY thread: `make ship` serves one .ipa (the newest), so only the newest
	// install card may ever show (per target: phone and desktop each have
	// their own). EQUAL counts too: two sessions shipping the same tree
	// minutes apart both number it by the commit count, and the later .ipa
	// overwrites the first on the OTA route, so two "build N" cards would be
	// one install shown twice.
	newBuild, newTarget := askBuild(kind, title), installTarget(kind, title)
	rows, err := m.db.Query(`SELECT id, thread_id, title, kind, state FROM items WHERE src='ask' AND (thread_id=? OR ?) AND state IN ('open','answered')`, threadID, newBuild > 0)
	if err != nil {
		return Ask{}, err
	}
	var old []string
	var oldState []string
	var oldThreads []string // whose board state has to be recomputed after
	for rows.Next() {
		var id, tid, tt, kd, st string
		rows.Scan(&id, &tid, &tt, &kd, &st)
		ob := askBuild(kd, tt)
		switch {
		case tid == threadID && normTitle(tt) == normTitle(title):
		// The phone's card and the Mac's card are two installs, not one:
		// a desktop build never retires a phone build or the other way round.
		case newBuild > 0 && ob > 0 && ob <= newBuild && installTarget(kd, tt) == newTarget:
		default:
			continue
		}
		old = append(old, id)
		oldState = append(oldState, st)
		oldThreads = append(oldThreads, tid)
	}
	rows.Close()
	if _, err := m.db.Exec(`INSERT INTO items (id,src,created_at,updated_at,thread_id,run_id,message_id,goal_id,title,detail,kind,class,state,check_hint,surface,source) VALUES (?,'ask',?,?,?,?,NULL,?,?,?,?,?,'open',?,?,?)`,
		a.ID, ts(now), ts(now), threadID, nullable(runID), nullable(t.GoalID), a.Title, a.Detail, a.Kind, a.Class, a.CheckHint, a.Surface, "claude:thread:"+threadID); err != nil {
		return Ask{}, err
	}
	m.moveAsk(askMove{ask: a.ID, to: "open", by: "claude:thread:" + threadID})
	// A superseded card is a closed card: say what replaced it in words, so a
	// stale one still sitting in an old session reads as history, not as a
	// second thing to do (an old install card still offering Install while
	// the phone runs a newer build).
	repl := "replaced by a newer card on this session"
	if newBuild > 0 {
		repl = fmt.Sprintf("replaced by build %d", newBuild)
	}
	for i, id := range old {
		m.moveAsk(askMove{ask: id, from: oldState[i], to: "superseded", by: "claude:thread:" + threadID, note: repl, event: "re-raised as " + a.ID, supersededBy: a.ID})
	}
	// Superseding is a close, so the threads that lost an ask must re-derive
	// their status like any other close does — the install-build rule closes
	// asks on OTHER threads too, and without this that thread is stranded on
	// status=needs_you with nothing behind it: a ghost card in the console's
	// "Your turn" group that no ask list counts and no button can clear.
	for _, tid := range append(oldThreads, threadID) {
		m.syncThreadStatus(tid)
	}
	// An install card below a newer one (open, or already tapped) is born
	// superseded and never buzzes: only the newest may show.
	if newBuild > 0 {
		if _, newest := m.keepNewestInstall(newTarget); newBuild < newest {
			return m.GetAsk(a.ID)
		}
	}
	m.announce(a, t, say)
	log.Printf("ask %s raised on thread %s: %s", a.ID, threadID, title)
	return m.GetAsk(a.ID)
}

// RaiseStep puts one of the owner's steps in front of them on the day it
// fires — the calendar has just written the row `open` (the step is its own
// card; it used to mint a second row, an ask, and keep the two in step by
// hand). class is the card's why (practice | step, or a dated ask's
// own); the surface is settled from what it says, as an ask's is; then it is
// announced exactly as a raised ask is.
func (m *Manager) RaiseStep(id, class, say string) (Ask, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return Ask{}, err
	}
	if class == "" {
		class = ClassFor(a.Kind)
	}
	if !askClasses[class] {
		return Ask{}, fmt.Errorf("class must be unblock|read|install|practice|step")
	}
	var surf string
	m.db.QueryRow(`SELECT surface FROM items WHERE id=?`, id).Scan(&surf)
	a.Class, a.Surface = class, normSurface(surf, a.Kind, a.Title, a.Detail)
	if _, err := m.db.Exec(`UPDATE items SET class=?, surface=? WHERE id=?`, a.Class, a.Surface, id); err != nil {
		return Ask{}, err
	}
	store.StampItem(m.db, id)
	t, err := m.Get(a.ThreadID)
	if err != nil {
		return Ask{}, err
	}
	m.announce(a, t, say)
	m.syncThreadStatus(a.ThreadID)
	log.Printf("step %s raised on thread %s: %s", id, a.ThreadID, a.Title)
	return m.GetAsk(id)
}

// announce: the card is in front of the owner — its session's unread, its
// needs_you, its push — the one tail every card shares, a raised ask and a
// fired step alike.
func (m *Manager) announce(a Ask, t Thread, say string) {
	now, threadID, kind, class, title := time.Now(), a.ThreadID, a.Kind, a.Class, a.Title
	// Surface it NOW — mid-tool-chain, not at the end of the turn: a card is
	// not the wrap-up message. It is a thing the agent hands the owner the
	// moment it has it; their answer goes back as a steering message into the turn still in flight (runner.deliver), so
	// nothing is gained by holding the buzz until the reply lands.
	//
	// Two things are still turn-end business, deliberately:
	//   - status: a running session stays `running`; settleStatus/syncThreadStatus
	//     turn it into `needs_you` when the turn ends with the card still open.
	//   - message_id: finishTurn links the card to the reply for anything that
	//     is not placed in the chain (Steps.Segments places the rest).
	// Only a card that needs the owner pushes as NEEDS YOU — a `read` card is
	// blue on both surfaces and waits to be looked at (otherwise every
	// calendar reminder minting a read ask would buzz as NEEDS YOU). A read
	// card goes out as the quieter "To read" push instead, so it is still
	// read out. The card's words come first: that is what gets spoken.
	// A practice or a step (the owner's own dated work) still pushes ONCE —
	// that push is the reminder — but never turns the thread needs_you:
	// nothing there is waiting on them.
	m.db.Exec(`UPDATE threads SET unread=unread+1, updated_at=? WHERE id=?`, ts(now), threadID)
	if t.Status != "running" && a.Blocks() {
		m.db.Exec(`UPDATE threads SET status='needs_you' WHERE id=? AND status != 'archived'`, threadID)
	}
	line := fmt.Sprintf("%s\n%s", plainTitle(title), plainTitle(t.Title))
	// The session's own spoken sentence, when it wrote one (notify/voice.go);
	// else one the hub can make from what it has — never the label form.
	// The thread goes with it so the list can say WHICH session is the voice
	// being heard; the ask's id so the line is spoken by
	// the next hub if this one stops before it is heard. notify.Card picks
	// the lane (card, To read, NEEDS YOU) — the same funnel a proposal and a
	// calendar nag take.
	if strings.TrimSpace(say) == "" {
		// A chore's card is a practice like homework's, but it is spoken as a
		// chore: "reminder to <title>" (the lane is stamped before a step is
		// raised).
		spokenAs, lane := class, ""
		if m.db.QueryRow(`SELECT lane FROM items WHERE id=?`, a.ID).Scan(&lane); lane == "chores" {
			spokenAs = "chore"
		}
		say = spokenFallback(kind, spokenAs, title, t.Title)
	}
	if said, _ := notify.Card(m.Notifier, kind, line, say, threadID, a.ID); said != "" {
		// Keep the words the owner heard ON the card they open. This
		// is the only place the spoken sentence exists in full: `--say` is a
		// flag, the fallback is computed here, and APNs keeps nothing. Written
		// on the card lane alone, so `said` means "this was spoken", never
		// "this would have been spoken" — a card raised with no notifier
		// attached stays blank. m.GetAsk below re-reads the row, so the Ask
		// returned to the caller already carries it.
		m.db.Exec(`UPDATE items SET said=? WHERE id=?`, said, a.ID)
	}
}

// askCols: an Ask's columns over askFrom. A fired step reads as the card its
// ask used to be: raised when it fired, its card kind, its own id and day as
// the calendar link. An ask minted by a step (the old shape) finds that step
// by a sub-select, not a join: two calendar rows pointing at one ask (a
// repeat re-armed by hand) would otherwise duplicate the ask on the board.
var askCols = `a.id,CASE WHEN a.src='cal' THEN a.fired_at ELSE a.created_at END,a.updated_at,` + askThread + `,t.title,a.run_id,a.message_id,a.goal_id,a.title,a.detail,` +
	store.CardKind("a") + `,a.state,a.check_hint,a.resolved_at,a.resolved_by,a.resolution,a.superseded_by,a.surface,t.status,a.class,a.said,
	CASE WHEN a.src='cal' THEN a.id ELSE (SELECT c.id FROM items c WHERE c.ask_id=a.id AND c.src='cal' ORDER BY c.day LIMIT 1) END,
	CASE WHEN a.src='cal' THEN a.day ELSE (SELECT c.day FROM items c WHERE c.ask_id=a.id AND c.src='cal' ORDER BY c.day LIMIT 1) END,a.win`

func scanAsk(r scanner) (Ask, error) {
	var a Ask
	var c, u string
	var tt, run, goal, rAt, rBy, sup, surf, tStatus, class, said, calID, calDay sql.NullString
	var mid sql.NullInt64
	if err := r.Scan(&a.ID, &c, &u, &a.ThreadID, &tt, &run, &mid, &goal, &a.Title, &a.Detail, &a.Kind, &a.State, &a.CheckHint, &rAt, &rBy, &a.Resolution, &sup, &surf, &tStatus, &class, &said, &calID, &calDay, &a.Window); err != nil {
		return a, err
	}
	a.Said = said.String
	a.CalID, a.CalDay = calID.String, calDay.String
	a.ThreadRunning = tStatus.String == "running"
	a.Outcomes, a.Verb = OutcomesFor(a.Kind), store.AskVerb(a.Kind)
	a.Target = installTarget(a.Kind, a.Title)
	if a.Class = class.String; a.Class == "" {
		a.Class = ClassFor(a.Kind)
	}
	if a.Surface = surf.String; a.Surface == "" {
		a.Surface = "any"
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
	a.ThreadTitle, a.RunID, a.GoalID, a.ResolvedBy, a.SupersededBy = tt.String, run.String, goal.String, rBy.String, sup.String
	a.MessageID = mid.Int64
	a.Detail = ListifyDetail(a.Detail) // old rows written as one paragraph still render as lists
	if a.Kind == "error" {
		if at, ok := spend.ResetAt(a.Detail, a.CreatedAt); ok {
			a.ResumesAt, a.Verb = &at, "paused"
		}
	}
	if rAt.Valid {
		x, _ := time.Parse(time.RFC3339Nano, rAt.String)
		a.ResolvedAt = &x
	}
	a.stand()
	return a, nil
}

func (m *Manager) GetAsk(id string) (Ask, error) {
	a, err := scanAsk(m.db.QueryRow(`SELECT `+askCols+` FROM `+askFrom+` WHERE a.id=? AND `+askIs, id))
	a.WaitingToSpeak = err == nil && m.Voice.CardWaiting(a.ID)
	a.Speaking = err == nil && m.Voice.CardSpeaking(a.ID, time.Now())
	return a, err
}

// WroteSince: whether the owner wrote into the session after `t`. A card's
// line that is still waiting when they do has nothing left to say: they have
// moved the session on, and a held line speaking after their reply would be
// stale.
func (m *Manager) WroteSince(thread string, t time.Time) bool {
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=? AND role='owner' AND ts>?`, thread, store.TS(t)).Scan(&n)
	return n > 0
}

// Row projects one ask into the shared read-model row (store.Dated, Phase 5) —
// the ONE place an ask becomes a thing the board ranks or the agenda dates.
// Everything the two readers decide from lives on the row: Surface (who
// answers it, and where), CalID and Day (a dated step lists as the calendar's
// work, not its session's), AskKind (`error` sinks under a crashed run). The
// whole Ask rides in Obj for the JSON.
func Row(a Ask) store.Dated {
	return store.Dated{
		ID: a.ID, Kind: "ask", Ref: "ask:" + a.ID, AskID: a.ID,
		AskKind: a.Kind, AskClass: a.Class, Title: a.Title, Detail: a.Detail, State: a.State,
		Day: a.CalDay, ThreadID: a.ThreadID, ThreadTitle: a.ThreadTitle, GoalID: a.GoalID,
		Actor: "claude:thread:" + a.ThreadID, RunID: a.RunID,
		Surface: a.Surface, CalID: a.CalID, Window: a.Window, Obj: a,
	}
}

// OpenLimit caps each table's share of store.Items.
const OpenLimit = 500

// Open: the asks still in play — open (the owner's move) or answered (the agent's) —
// as rows, oldest first; this table's share of store.Items.
func (m *Manager) Open() ([]store.Dated, error) {
	if m == nil {
		return nil, nil
	}
	asks, err := m.ListAsks("active", "", OpenLimit)
	if err != nil {
		return nil, err
	}
	out := make([]store.Dated, 0, len(asks))
	for _, a := range asks {
		out = append(out, Row(a))
	}
	return out, nil
}

// Dated: the asks the owner closed, as record rows at the minute they closed
// them — the calendar's past half: steps that were done. One of theirs is an
// ask `resolved_by` owner, or one the hub's verifier confirmed against a
// check_hint (the phone reporting the build they installed): the deed was
// theirs either way, so the row's actor is `owner`.
// Asks a session closed are its own bookkeeping and stay off the calendar,
// as does an ask a calendar item minted — the item is already on its day,
// closed. An install ask that reached `done` is the owner's however it got
// there: the app closes it on their tap and the hub closes it when the
// phone reports the build, and either way they installed it — the row is the
// placeholder for the install at the minute it happened. Instants are UTC
// and days local, so the SQL window is a day wide each side and the exact
// test is on the row.
func (m *Manager) Dated(from, to string) ([]store.Dated, error) {
	lo, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return nil, errors.New("from must be YYYY-MM-DD")
	}
	hi, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return nil, errors.New("to must be YYYY-MM-DD")
	}
	rows, err := m.db.Query(`SELECT `+askCols+` FROM `+askFrom+`
		WHERE a.src='ask' AND a.state IN ('done','dismissed') AND a.resolved_at>=? AND a.resolved_at<?
		AND (a.resolved_by='owner' OR (a.resolved_by='hub' AND a.check_hint!='' AND a.state='done')
		     OR (a.kind='install' AND a.state='done'))
		ORDER BY a.resolved_at`, ts(lo.AddDate(0, 0, -1)), ts(hi.AddDate(0, 0, 2)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Dated{}
	for rows.Next() {
		a, err := scanAsk(rows)
		if err != nil {
			return nil, err
		}
		if a.ResolvedAt == nil || a.CalID != "" {
			continue
		}
		d := DidRow(a)
		if d.Day < from || d.Day > to {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DidRow: one closed ask as the record of the owner answering it. The verb is
// what closing that KIND of ask meant — a read card is read whether they
// tapped done or dismiss (both surfaces close one either way), a decision
// decided, an access ask granted, a physical step done, a build installed;
// a non-read ask they dismissed was skipped, and keeps the ✕.
func DidRow(a Ask) store.Dated {
	d := Row(a)
	d.Key, d.Did, d.Actor = "did:ask:"+a.ID, true, "owner"
	if a.ResolvedAt != nil {
		d.Day, d.At = store.Day(*a.ResolvedAt), store.Clock(*a.ResolvedAt)
	}
	d.Verb = store.AskDidVerb(a.Kind, a.State)
	switch {
	case a.Kind == "read":
		d.State = "done"
	case d.Verb == "Installed":
		// "Installed: build 880" — the title's imperative ("Install app build
		// 880 (tap the link)") reads wrong after the fact.
		if n := askBuild(a.Kind, a.Title); n > 0 {
			d.Title = fmt.Sprintf("build %d", n)
		}
	}
	return d
}

// ListAsks: state "" or "active" = open+answered (the board); "all"; or one
// state. threadID "" = every thread. Oldest first (the board is a queue).
func (m *Manager) ListAsks(state, threadID string, limit int) ([]Ask, error) {
	where := []string{askIs}
	var args []any
	switch state {
	case "", "active":
		where = append(where, "a.state IN ('open','answered')")
	case "all":
	default:
		where = append(where, "a.state=?")
		args = append(args, state)
	}
	if threadID != "" {
		where = append(where, askThread+"=?")
		args = append(args, threadID)
	}
	if limit <= 0 {
		limit = 200
	}
	args = append(args, limit)
	rows, err := m.db.Query(`SELECT `+askCols+` FROM `+askFrom+` WHERE `+strings.Join(where, " AND ")+` ORDER BY CASE WHEN a.src='cal' THEN a.fired_at ELSE a.created_at END LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, now := []Ask{}, time.Now()
	for rows.Next() {
		a, err := scanAsk(rows)
		if err != nil {
			return nil, err
		}
		a.WaitingToSpeak = m.Voice.CardWaiting(a.ID)
		a.Speaking = m.Voice.CardSpeaking(a.ID, now)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ResolveAsk moves an ask to done|dismissed|open (reopen). by is "owner",
// "verifier" or "claude:thread:<id>". When the owner marks one done from the app
// the thread gets a decision message so the agent can carry on (it usually
// has follow-up work); dismiss is just recorded — the agent sees it in its
// next prompt header.
func (m *Manager) ResolveAsk(id, state, by, note string) (Ask, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return a, errors.New("no such ask")
	}
	if state != "done" && state != "dismissed" && state != "open" {
		return a, errors.New("state must be done|dismissed|open")
	}
	if a.Step() {
		// A fired step IS its card (step 11): closing the card closes the step,
		// through the calendar, which owns what a step's close means.
		if m.ResolveCal == nil {
			return a, errors.New("no calendar")
		}
		if err := m.ResolveCal(a.ID, state, by, note, true); err != nil {
			return a, err
		}
		if b, err := m.GetAsk(id); err == nil {
			return b, nil
		}
		// Reopened, the step is scheduled again and no card until it fires.
		a.State = state
		return a, nil
	}
	if a.State == "superseded" {
		return a, errors.New("ask was superseded by " + a.SupersededBy)
	}
	if a.State == state {
		return a, nil
	}
	if err := m.setAskState(a, state, by, note); err != nil {
		return a, err
	}
	// The owner answering a card is a prompt that REFERENCES the ask
	// (prompts.go), never a sentence the hub composes and then has to
	// recognise again. A dismissal reaches the session too — "the task is
	// done here" and "the owner is not going to do this" are both things the
	// agent must know.
	// …except a `read` card. Acknowledging an answer says nothing the session
	// can act on, and waking it costs a turn: read/dismiss never starts the
	// session up — only reply does. Not even a note relays: the note is the
	// resolution on the card, and the button that carries one ("Read it") is
	// still just the owner saying they read it. Words they TYPE arrive as
	// their own prompt through Queue, which is the only thing that wakes a
	// session for a read.
	if by == "owner" && a.Kind == "read" {
		return m.GetAsk(id)
	}
	if by == "owner" && (state == "done" || state == "dismissed") {
		outcome := "done"
		text := "Done: " + a.Title
		if state == "dismissed" {
			outcome, text = "wont", "Won't do: "+a.Title
		}
		if note != "" {
			text += "\n" + note
		}
		if _, err := m.Queue(Prompt{Author: "owner", Target: a.ThreadID, InReplyTo: "ask:" + a.ID, Outcome: outcome, Text: text}); err != nil {
			log.Printf("ask %s: prompt to session: %v", id, err)
		}
	}
	return m.GetAsk(id)
}

// setAskState writes the state change and nothing else: the row, the event, the
// thread's derived status. The ONE writer — ResolveAsk (the owner's swipe, the agent's
// close, the verifier) and Queue (a prompt that carries `outcome`, which is the
// same claim arriving through the one inbound lane) both come through here, so
// "the owner says this is done" is recorded identically whichever surface said it.
//
// The row and its event land in one transaction (a state with no event, or an
// event for a state that never landed, was possible when both errors were
// dropped); a failed write is returned and the thread status is left alone.
func (m *Manager) setAskState(a Ask, state, by, note string) error {
	if err := m.moveAsk(askMove{ask: a.ID, from: a.State, to: state, by: by, note: note}); err != nil {
		log.Printf("ask %s: %s → %s by %s: %v", a.ID, a.State, state, by, err)
		return err
	}
	m.syncThreadStatus(a.ThreadID)
	log.Printf("ask %s: %s → %s by %s", a.ID, a.State, state, by)
	return nil
}

// askMove is one state change of one ask. from "" = the ask is being raised
// (its row was just inserted: only the event is written). event, when set, is
// the event's note where it differs from the row's resolution (a supersede
// says "re-raised as <id>" on the trail and what replaced it on the row).
type askMove struct {
	ask, from, to, by, note, event, supersededBy string
}

// moveAsk is the ONE writer of an ask's state and of ask_events: the row and
// its event land in one transaction (a state with no event, or an event for a
// state that never landed, was possible when both errors were dropped). What
// the row keeps depends on where it goes: `answered` is still the owner's card (no
// resolution), `open` clears the last close off the row — the trail keeps it
// — and every other state is a close with its doer and words.
func (m *Manager) moveAsk(mv askMove) error {
	now := ts(time.Now())
	note := strings.TrimSpace(mv.note)
	event := note
	if mv.event != "" {
		event = mv.event
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	switch {
	case mv.from == "":
	case mv.to == "open":
		_, err = tx.Exec(`UPDATE items SET state='open', updated_at=?, resolved_at=NULL, resolved_by=NULL, resolution='' WHERE id=? AND src='ask'`, now, mv.ask)
	case mv.to == "answered":
		_, err = tx.Exec(`UPDATE items SET state='answered', updated_at=? WHERE id=? AND src='ask'`, now, mv.ask)
	case mv.supersededBy != "":
		_, err = tx.Exec(`UPDATE items SET state=?, superseded_by=?, resolution=?, updated_at=?, resolved_at=?, resolved_by=? WHERE id=? AND src='ask'`, mv.to, mv.supersededBy, note, now, now, mv.by, mv.ask)
	default:
		_, err = tx.Exec(`UPDATE items SET state=?, updated_at=?, resolved_at=?, resolved_by=?, resolution=? WHERE id=? AND src='ask'`, mv.to, now, now, mv.by, note, mv.ask)
	}
	if err != nil {
		return err
	}
	if err := store.StampItem(tx, mv.ask); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO item_events (item_id, ts, actor, from_state, to_state, note) VALUES (?,?,?,?,?,?)`, mv.ask, now, mv.by, mv.from, mv.to, event); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimAsk records what the owner SAID about an ask when they respond to its
// card: done, or won't do. It is a claim, not a verification (the check_hint
// verifier keeps that role), and it happens when they say it — not when the
// prompt is delivered — so a card answered "tomorrow at 9" still leaves the
// board now.
// Quiet: no prompt is queued (the caller's prompt is the message) — which is
// why the calendar closes a fired step's ask through here, not ResolveAsk.
func (m *Manager) ClaimAsk(askID, outcome, note string) {
	state := "done"
	if outcome == "wont" {
		state = "dismissed"
	}
	a, err := m.GetAsk(askID)
	if err != nil || a.State == state || a.State == "superseded" {
		return
	}
	if a.Step() {
		if m.ResolveCal != nil {
			m.ResolveCal(a.ID, state, "owner", note, false)
		}
		return
	}
	m.setAskState(a, state, "owner", note)
}

// activeAsks of a thread, oldest first.
func (m *Manager) activeAsks(threadID string) []Ask {
	as, _ := m.ListAsks("active", threadID, 100)
	return as
}

// blocked: the ONE test for "this session is waiting on the owner" — an active ask
// that Blocks. Every writer of status=needs_you goes through it.
func (m *Manager) blocked(threadID string) bool {
	for _, a := range m.activeAsks(threadID) {
		if a.Blocks() {
			return true
		}
	}
	return false
}

// syncThreadStatus: needs_you is derived — a thread that is not running is
// needs_you iff it has active asks that BLOCK (Ask.Blocks: not a practice or
// a step, which are the calendar's rows); otherwise idle (scheduled) or done.
func (m *Manager) syncThreadStatus(threadID string) {
	var status, sched string
	if err := m.db.QueryRow(`SELECT status, schedule FROM threads WHERE id=?`, threadID).Scan(&status, &sched); err != nil {
		return
	}
	if status == "running" || status == "archived" {
		return
	}
	want := "done"
	if sched != "" {
		want = "idle"
	}
	if m.blocked(threadID) {
		want = "needs_you"
	}
	if want != status {
		m.db.Exec(`UPDATE threads SET status=?, updated_at=? WHERE id=?`, want, ts(time.Now()), threadID)
	}
}

// replyTargetRe: COMPAT, one place only. Both surfaces now send `in_reply_to`
// with the response, but older app builds compose a message beginning
// "Re ask-xxxx" — and an old build keeps running until the owner installs a
// new one. ReplyRef turns that prefix into a typed reference AT THE DOOR
// (Send), so nothing downstream ever reads the owner's words to work out
// what they are answering. Delete it when no build that
// composes the prefix can still be installed.
var replyTargetRe = regexp.MustCompile(`^\s*Re\s+(ask-[0-9a-f]{4,8})\b`)

// ReplyRef returns "ask:<id>" when a message was composed from one ask's
// card, else "".
func ReplyRef(text string) string {
	if m := replyTargetRe.FindStringSubmatch(text); m != nil {
		return "ask:" + m[1]
	}
	return ""
}

// markAnswered: the owner wrote in the thread — every open ask there is now
// answered (still on the board until the agent or the owner closes it),
// unless the message answers ONE thing (ref "ask:<id>", a reply from that
// card), then only that one.
func (m *Manager) markAnswered(threadID, ref string) {
	kind, target := store.SplitRef(ref)
	if kind != "" && kind != "ask" {
		// A proposal or a rec was answered; that says nothing about the asks.
		return
	}
	for _, a := range m.activeAsks(threadID) {
		if a.State != "open" || (target != "" && a.ID != target) || a.Step() {
			continue
		}
		// A message to the SESSION says nothing about a card that only asked
		// the owner to read something. A read closes when they answer that
		// card — through it, not past it.
		if a.Kind == "read" && target == "" {
			continue
		}
		// …nor about the owner's own dated work: one unrelated question must
		// not flip every fired step to answered. A step closes through its
		// card: I did this / Won't do.
		if OwnerClass(a.Class) && target == "" {
			continue
		}
		m.moveAsk(askMove{ask: a.ID, from: "open", to: "answered", by: "owner", note: "replied in thread"})
	}
}

// reopenUnclosed: `answered` means the ball is with the agent, and the board
// leaves such a card out (attention.BuildRows). That holds only while a turn
// is reading the owner's words: a turn that ENDED without closing the card
// has handed it back — asksHeader tells the agent to leave open what their
// message did not resolve, "they are theirs to close" — so it is `open`
// again and back on the owner's board. Without this an unrelated message hides a card for good.
func (m *Manager) reopenUnclosed(threadID string) {
	for _, a := range m.activeAsks(threadID) {
		if a.State != "answered" || a.Step() {
			continue
		}
		m.moveAsk(askMove{ask: a.ID, from: "answered", to: "open", by: "hub", note: "the turn ended without closing it"})
	}
}

// asksHeader: prompt block listing the thread's active asks so the agent
// closes what the owner's message resolved and never re-asks what is pending.
func (m *Manager) asksHeader(threadID string) string {
	as := m.activeAsks(threadID)
	if len(as) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Open asks on this thread — things you asked the owner for. Assess EACH one against their message: close it only if what they wrote or did actually resolves it — `lifectl ask <id> done \"what happened\"` (or `dismiss` if it no longer applies). A message about something else, or a Done on a different ask, resolves nothing: leave those open — they are theirs to close. Do not re-raise an ask that is listed here.")
	for _, a := range as {
		st := "open"
		if a.State == "answered" {
			st = "open — the owner has written in this thread since it was raised; that alone does not resolve it"
		}
		fmt.Fprintf(&b, "\n  %s (%s, %s): %s", a.ID, a.Kind, st, a.Title)
		if a.Detail != "" {
			fmt.Fprintf(&b, " — %s", firstLine(a.Detail, 200))
		}
	}
	b.WriteString("]\n\n")
	return b.String()
}

// migrateLegacyNeedsYou: threads that were needs_you under the old
// flag-on-thread model get one ask per NEEDS YOU line of their latest
// needs_you message, so nothing already on the board is lost. Idempotent
// (only threads with no asks at all).
func (m *Manager) migrateLegacyNeedsYou() {
	rows, err := m.db.Query(`SELECT id FROM threads WHERE status='needs_you' AND NOT EXISTS (SELECT 1 FROM items a WHERE a.src='ask' AND a.thread_id=threads.id)`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		var mid int64
		var text, run sql.NullString
		err := m.db.QueryRow(`SELECT id, text, run_id FROM thread_messages WHERE thread_id=? AND kind IN ('needs_you','error') ORDER BY id DESC LIMIT 1`, id).Scan(&mid, &text, &run)
		if err != nil {
			continue
		}
		n := 0
		for _, mt := range needsYouRe.FindAllStringSubmatch(text.String, -1) {
			line := strings.TrimSpace(mt[3]) // [1]=day [2]=at, both unused here
			if a, err := m.AddAsk(id, "legacy", firstLine(line, 120), line, "other", ""); err == nil {
				m.db.Exec(`UPDATE items SET message_id=?, run_id=? WHERE id=?`, mid, nullable(run.String), a.ID)
				n++
			}
		}
		if n == 0 {
			if a, err := m.AddAsk(id, "legacy", "Session needs you", firstLine(text.String, 300), "read", ""); err == nil {
				m.db.Exec(`UPDATE items SET message_id=?, run_id=? WHERE id=?`, mid, nullable(run.String), a.ID)
			}
		}
		log.Printf("thread %s: migrated legacy needs_you into asks", id)
	}
}

// backfillAskRun: cards raised inside a turn that never recorded which run
// they were raised in. Their client posted run_id:"" (LIFE_RUN_ID missing from
// a claude process that predates it), and with no run they are invisible in
// the chat on both surfaces: /steps cuts the tool chain at cards it can place
// in a run, and finishTurn links message_id BY run_id, so they end up with
// neither anchor. server.addAsk now fills the run in at the moment the card is
// raised; this repairs the rows that were written before it did.
//
// A run of the same thread that was in flight when the card was raised is the
// only candidate, so this is idempotent and cannot move a card that already
// names its run.
func (m *Manager) backfillAskRun() {
	res, err := m.db.Exec(`UPDATE items SET run_id = (
		SELECT r.id FROM thread_runs r
		WHERE r.thread_id = items.thread_id AND r.started_at <= items.created_at
		  AND (r.finished_at IS NULL OR r.finished_at >= items.created_at)
		ORDER BY r.started_at DESC LIMIT 1)
		WHERE src='ask' AND COALESCE(run_id,'') = '' AND message_id IS NULL AND EXISTS (
		SELECT 1 FROM thread_runs r
		WHERE r.thread_id = items.thread_id AND r.started_at <= items.created_at
		  AND (r.finished_at IS NULL OR r.finished_at >= items.created_at))`)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("asks: tied %d card(s) with no run to the run they were raised in", n)
	}
}

// backfillAskInstall: active "Install app build N" cards written before the
// install kind existed say "physical" and would keep drawing as the red
// Respond cell. One idempotent retag; closed cards are history and stay put.
func (m *Manager) backfillAskInstall() {
	res, err := m.db.Exec(`UPDATE items SET kind='install' WHERE src='ask' AND (title LIKE 'Install app build %' OR title LIKE 'Install desktop build %') AND kind NOT IN ('install','read') AND state IN ('open','answered')`)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("asks: retagged %d active install card(s) to kind=install", n)
	}
}

// backfillAskSaid re-speaks the read cards a plain reply minted in the old
// shape, whose `said` was spokenFallback over a title cut at 80 runes, so the
// spoken line stopped mid-sentence. A row whose said
// AND title end "…" while its detail holds the whole text gets the line
// cardFromReply now makes: the reply itself. Idempotent: a rebuilt line ends
// "…" only when the text is over spokenReplyMax, and then the title is left
// alone too — so the rebuilt row is re-selected but rewritten identically.
func (m *Manager) backfillAskSaid() {
	rows, err := m.db.Query(`SELECT a.id, a.detail, COALESCE(t.title,'') FROM items a LEFT JOIN threads t ON t.id=a.thread_id WHERE a.src='ask' AND a.kind='read' AND a.said LIKE '%…' AND a.title LIKE '%…' AND a.detail != ''`)
	if err != nil {
		return
	}
	type fix struct{ id, say string }
	var fixes []fix
	for rows.Next() {
		var id, detail, thread string
		rows.Scan(&id, &detail, &thread)
		if say := spokenFallback("read", "", spokenReply(stripEndSentinel(detail)), thread); say != "" {
			fixes = append(fixes, fix{id, say})
		}
	}
	rows.Close()
	for _, f := range fixes {
		m.db.Exec(`UPDATE items SET said=? WHERE id=? AND said != ?`, f.say, f.id, f.say)
	}
	m.reshapeOpenReplyCards()
}

// reshapeOpenReplyCards gives the OPEN read cards a plain reply minted in the
// old shape the one card shape: the message it leads with is not repeated
// under it, and a typed `[end]` is not a word on the card. A row whose said
// ends with its detail's first paragraph (plain) loses that paragraph; any
// other row only loses the sentinel. Idempotent: a reshaped row matches
// neither.
func (m *Manager) reshapeOpenReplyCards() {
	rows, err := m.db.Query(`SELECT id, detail, said FROM items WHERE src='ask' AND kind='read' AND state IN ('open','answered') AND detail != '' AND said != ''`)
	if err != nil {
		return
	}
	type fix struct{ id, detail string }
	var fixes []fix
	for rows.Next() {
		var id, detail, said string
		rows.Scan(&id, &detail, &said)
		d := stripEndSentinel(detail)
		para, rest := firstParagraph(d)
		if msg := spokenReply(para); msg != "" && !strings.HasSuffix(msg, "…") && strings.HasSuffix(said, msg) {
			d = rest
		}
		if d != detail {
			fixes = append(fixes, fix{id, d})
		}
	}
	rows.Close()
	for _, f := range fixes {
		m.db.Exec(`UPDATE items SET detail=? WHERE id=?`, f.detail, f.id)
	}
}

// backfillAskClass pairs with the `class` column: once, every
// row still at ” takes its class — a calendar-minted ask from the item that
// minted it (homework → practice, owner → step, unless the item dated an ask
// of the session's own kind), everything else from its kind — and the
// threads whose only open asks were the owner's own dated work stop being
// needs_you.
// Idempotent: after the first boot no row is ”.
func (m *Manager) backfillAskClass() {
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='ask' AND class=''`).Scan(&n)
	if n == 0 {
		return
	}
	var threads []string
	rows, _ := m.db.Query(`SELECT DISTINCT thread_id FROM items WHERE src='ask' AND class='' AND state IN ('open','answered')`)
	if rows != nil {
		for rows.Next() {
			var id string
			rows.Scan(&id)
			threads = append(threads, id)
		}
		rows.Close()
	}
	m.db.Exec(`UPDATE items SET class='practice' WHERE src='ask' AND class='' AND id IN (SELECT ask_id FROM items WHERE src='cal' AND kind='homework' AND ask_id IS NOT NULL AND ask_kind='')`)
	m.db.Exec(`UPDATE items SET class='step' WHERE src='ask' AND class='' AND id IN (SELECT ask_id FROM items WHERE src='cal' AND kind='owner' AND ask_id IS NOT NULL AND ask_kind='')`)
	m.db.Exec(`UPDATE items SET class=CASE kind WHEN 'read' THEN 'read' WHEN 'install' THEN 'install' ELSE 'unblock' END WHERE src='ask' AND class=''`)
	for _, id := range threads {
		m.syncThreadStatus(id)
	}
	log.Printf("asks: %d row(s) given a class", n)
}

// reconcileNeedsYou clears threads stranded on status=needs_you with no
// active ask behind them. They exist because superseding closed an ask on a
// thread other than the one raising it — one .ipa means one install card, so a
// new build's card closes every older one wherever it lives — and only the
// raising thread re-derived its status. The result was a permanent "YOUR TURN"
// on a session whose only ask says "superseded", pointing at nothing.
//
// RaiseAsk now syncs every thread it touched, so this is for the rows that
// went stale before that fix. It runs after migrateLegacyNeedsYou, which mints
// asks for threads that have none at all — those are a different case and must
// not be cleared, so this only looks at threads that DO have ask rows.
func (m *Manager) reconcileNeedsYou() {
	rows, err := m.db.Query(`SELECT id FROM threads WHERE status='needs_you'
		AND EXISTS (SELECT 1 FROM items a WHERE a.src='ask' AND a.thread_id=threads.id)
		AND NOT EXISTS (SELECT 1 FROM items a WHERE a.src='ask' AND a.thread_id=threads.id AND a.state IN ('open','answered') AND a.class NOT IN ('practice','step'))`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		m.syncThreadStatus(id)
		log.Printf("thread %s: cleared stale needs_you (no blocking ask)", id)
	}
}

// VerifierDue gates the ask-verifier job: worth a run only when an active
// ask says how to check it and something has happened since the last run
// (a message, a finished run, a new observation), or when the last run is
// more than 6h old (floor, so long-idle asks still get looked at). No
// chatting and no agents working → nothing to verify.
func (m *Manager) VerifierDue(last time.Time) bool {
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src IN ('ask','cal') AND state IN ('open','answered') AND check_hint != ''`).Scan(&n)
	if n == 0 {
		return false
	}
	if last.IsZero() || time.Since(last) > 6*time.Hour {
		return true
	}
	since := ts(last)
	var msgs, runs, obs int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE ts > ?`, since).Scan(&msgs)
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE finished_at > ?`, since).Scan(&runs)
	m.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE ingested_at > ?`, since).Scan(&obs)
	return msgs+runs+obs > 0
}

var installTitle = regexp.MustCompile(`(?i)^install app build (\d+)`)

// installMacTitle: the desktop app's install card, "Install desktop build N":
// tapping Install builds it in the background and restarts it. Same kind,
// same teal cell, same pills, same newest-only rule — per target, since the
// phone and the Mac each run their own build.
var installMacTitle = regexp.MustCompile(`(?i)^install (?:desktop|mac) build (\d+)`)
var anyBuild = regexp.MustCompile(`(?i)\bbuild (\d+)\b`)
var macWord = regexp.MustCompile(`(?i)\b(desktop|mac)\b`)

// installBuild returns N for an "Install app build N" / "Install desktop
// build N" title, else 0. This is the strict form: it is what retags a card
// to kind install, so it must not fire on a read or physical card that merely
// mentions a build.
func installBuild(title string) int {
	mm := installTitle.FindStringSubmatch(title)
	if mm == nil {
		mm = installMacTitle.FindStringSubmatch(title)
	}
	if mm == nil {
		return 0
	}
	n, _ := strconv.Atoi(mm[1])
	return n
}

// installTarget: which device an install card is for — "phone" or "mac" —
// and "" for anything that is not an install. The strict Mac title, else a
// kind=install card that says desktop/Mac anywhere in its title; every other
// install is the phone's, as it always was.
func installTarget(kind, title string) string {
	return InstallTarget(kind, title)
}

// InstallTarget is installTarget for the board: which device a card updates,
// so each app lists only the build it can install itself (2026-10-01).
func InstallTarget(kind, title string) string {
	if installMacTitle.MatchString(title) {
		return "mac"
	}
	if askBuild(kind, title) == 0 {
		return ""
	}
	if kind == "install" && !installTitle.MatchString(title) && macWord.MatchString(title) {
		return "mac"
	}
	return "phone"
}

// Mac build: the desktop app reports the build it runs on every launch
// (POST /api/v1/app/mac) and it is kept as a setting, the way the phone's is
// a row in devices — the Mac reconciler reads it every minute.
const macBuildKey = "mac_build"

func (m *Manager) SetMacBuild(n int) error {
	return m.db.SetSetting(macBuildKey, strconv.Itoa(n))
}

func (m *Manager) MacBuild() int {
	n, _ := strconv.Atoi(m.db.Setting(macBuildKey))
	return n
}

// askBuild is installBuild for a card ALREADY tagged kind=install: a session
// may word its title any way it likes, so the first "build N" anywhere in it
// counts. Every rule that closes install cards — the newest-only supersede,
// the phone-reports-build-N reconciler, the "Installed: build N" label —
// keys on this, not on the title's exact wording, or a card titled "Install
// build N: …" would sit on the board long after the phone ran a newer build.
func askBuild(kind, title string) int {
	if n := installBuild(title); n > 0 {
		return n
	}
	if kind != "install" {
		return 0
	}
	mm := anyBuild.FindStringSubmatch(title)
	if mm == nil {
		return 0
	}
	n, _ := strconv.Atoi(mm[1])
	return n
}

// ReconcileInstalls keeps "Install app build N" asks honest against what the
// phone actually runs (maxBuild = newest build any device registered with).
// The rule: tapping Install must clear the card at once, and
// it only comes back on failure. The app marks the ask done (by "app") on
// the tap; here we (1) close every still-active install ask with N <=
// maxBuild (installed, or superseded by a newer build that is), and (2)
// reopen an app-tapped one when 10 min have passed and the phone still
// reports < N, so a failed install shows on the board instead of being
// swallowed. Runs on every device registration (app launch) and on a timer.
// Returns the highest build the owner tapped in the last 10 min that the phone
// has not reported yet (0 = none): the caller sends a silent wake push so
// the freshly installed build registers itself and closes the card.
func (m *Manager) ReconcileInstalls(maxBuild int) (pending int) {
	return m.reconcileInstalls("phone", maxBuild, "phone reports build %d",
		"[Install did not take: tapped %s ago, phone still reports build %d. Tap Install again, accept the iOS prompt, wait for the icon to finish.]")
}

// ReconcileMacInstalls is ReconcileInstalls for the desktop app's cards,
// against the build the Mac last reported (MacBuild). Runs on every report
// (the app's launch) and on the same minute timer. A clicked card whose
// build has not come up in 10 min reopens with the reason in its detail.
func (m *Manager) ReconcileMacInstalls(macBuild int) (pending int) {
	return m.reconcileInstalls("mac", macBuild, "the desktop app reports build %d",
		"[Install did not take: clicked %s ago, the desktop app still reports build %d. Click Install again; if the app never comes back, open it from /Applications.]")
}

func (m *Manager) reconcileInstalls(target string, maxBuild int, doneNote, backNote string) (pending int) {
	rs, newest := m.keepNewestInstall(target)
	for _, r := range rs {
		n := askBuild(r.kind, r.title)
		if n == 0 || n < newest {
			continue // an older card stays closed, whatever the device reports
		}
		switch {
		case r.state != "done" && n <= maxBuild:
			m.ResolveAsk(r.id, "done", "hub", fmt.Sprintf(doneNote, maxBuild))
		case r.state == "done" && r.by == "app" && n > maxBuild:
			t, err := time.Parse(time.RFC3339Nano, r.at)
			if err != nil {
				continue
			}
			if time.Since(t) < 10*time.Minute {
				if time.Since(t) > 45*time.Second && n > pending { // give the download a head start
					pending = n
				}
				continue
			}
			m.ResolveAsk(r.id, "open", "hub", "")
			// Mark it so it is not reopened again and again; the detail says why it is back.
			m.db.Exec(`UPDATE items SET resolved_by='hub', detail = ? || char(10) || detail WHERE id=?`,
				fmt.Sprintf(backNote, time.Since(t).Round(time.Minute), maxBuild), r.id)
		}
	}
	return pending
}

type installRow struct{ id, tid, title, kind, state, by, at string }

// keepNewestInstall enforces the one-install-card rule on the whole table,
// per target (phone / mac): only the highest-numbered install card ever
// raised for that device may be active, so every open/answered card below it
// is superseded, on any thread. It returns the live install rows
// (open/answered/done) of that target and that highest number, so
// ReconcileInstalls never reopens an older card either. AddAsk's supersede
// alone only saw OPEN cards: a tapped (done) older card slipped past a newer
// raise and the reconciler reopened it — two install cards side by side.
func (m *Manager) keepNewestInstall(target string) (rs []installRow, newest int) {
	rows, err := m.db.Query(`SELECT id, thread_id, title, kind, state, COALESCE(resolved_by,''), COALESCE(resolved_at,'') FROM items WHERE src='ask' AND (kind='install' OR title LIKE 'Install app build %' OR title LIKE 'Install desktop build %') AND state IN ('open','answered','done')`)
	if err != nil {
		return nil, 0
	}
	for rows.Next() {
		var r installRow
		rows.Scan(&r.id, &r.tid, &r.title, &r.kind, &r.state, &r.by, &r.at)
		if installTarget(r.kind, r.title) == target {
			rs = append(rs, r)
		}
	}
	rows.Close()
	newestID := ""
	for _, r := range rs {
		if n := askBuild(r.kind, r.title); n > newest {
			newest, newestID = n, r.id
		}
	}
	for i, r := range rs {
		if n := askBuild(r.kind, r.title); n > 0 && n < newest && r.state != "done" {
			m.moveAsk(askMove{ask: r.id, from: r.state, to: "superseded", by: "hub", note: fmt.Sprintf("replaced by build %d", newest), event: "superseded by " + newestID, supersededBy: newestID})
			m.syncThreadStatus(r.tid)
			rs[i].state = "superseded"
		}
	}
	return rs, newest
}

// inlineStep matches an enumerated step buried mid-paragraph ("... 1) foo 2) bar",
// "... (1) foo", "... Step 2: foo"). Only " N) " / " N. " with N ≤ 2 digits so
// decimals and years are left alone.
var inlineStep = regexp.MustCompile(`(?:^|\s)\(?(\d{1,2})[).:]\s+`)

// ListifyDetail turns a wall-of-text detail that carries inline "1) … 2) …"
// steps into one step per line so the app's StyledText renders a numbered
// list (a 5-step ask as one paragraph is unreadable on the phone). Text that already has line breaks, or no
// enumeration, is returned trimmed and unchanged.
func ListifyDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "\n") {
		return s
	}
	locs := inlineStep.FindAllStringSubmatchIndex(s, -1)
	if len(locs) < 2 {
		return s
	}
	// require the steps to count up from 1 in order — otherwise it is prose.
	for i, l := range locs {
		if s[l[2]:l[3]] != strconv.Itoa(i+1) {
			return s
		}
	}
	var b strings.Builder
	prev := 0
	for i, l := range locs {
		if head := strings.TrimSpace(s[prev:l[0]]); head != "" {
			b.WriteString(head)
			b.WriteString("\n")
		}
		b.WriteString(strconv.Itoa(i+1) + ". ")
		prev = l[1]
	}
	b.WriteString(strings.TrimSpace(s[prev:]))
	return b.String()
}
