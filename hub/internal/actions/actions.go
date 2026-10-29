// Package actions is the approval queue — the mechanical enforcement of the
// confirmation gate in DESIGN.md "Operating model". Anything that moves
// money, deletes, contacts people, shares data or commits the owner is
// proposed here with gated=true and does not execute until they approve it.
package actions

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"life/hub/internal/notify"
	"life/hub/internal/store"
)

// Kinds that are ALWAYS gated regardless of what the proposer says.
var gatedKinds = map[string]bool{"money": true, "delete": true, "contact": true, "share": true, "commit": true}

// A shell or claude run is the owner's to approve whatever kind it says,
// unless the command is one plain line on the free list (FreeShell). A
// kind=other shell proposal used to run at once: the hub's own bash, outside
// every session's --allowedTools, so `lifectl propose --exec shell` was one
// line past the python3/curl/sqlite3 ban. The kind is the proposer's word;
// the command is what runs.
var shellMeta = "`$;&|<>(){}\n\r\\"

// freeShell: cmd is exactly a free entry or one followed by plain arguments,
// with nothing the shell would read as a second command, a redirect or an
// expansion.
func freeShell(free []string, cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, shellMeta) {
		return false
	}
	for _, f := range free {
		if f != "" && (cmd == f || strings.HasPrefix(cmd, f+" ")) {
			return true
		}
	}
	return false
}

type Action struct {
	ID          string          `json:"id"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Project     string          `json:"project"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail"`
	Gated       bool            `json:"gated"`
	ExecType    string          `json:"exec_type"`
	ExecPayload json.RawMessage `json:"exec_payload"`
	State       string          `json:"state"`
	DecidedAt   *time.Time      `json:"decided_at,omitempty"`
	DecidedVia  string          `json:"decided_via,omitempty"`
	Result      string          `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	// ThreadID is the session that proposed it (lifectl fills it from
	// LIFE_THREAD_ID). The owner's decision goes back into that thread as a
	// message, with their optional note, so the two can go back and forth.
	ThreadID string `json:"thread_id,omitempty"`
	Note     string `json:"note,omitempty"`
	// RunID / MessageID: the run and the reply that proposed it (a backlink
	// like an ask's); Source: who proposed — claude:thread:<id>,
	// claude:job:<name>, lifectl, or app.
	RunID     string `json:"run_id,omitempty"`
	MessageID int64  `json:"message_id,omitempty"`
	Source    string `json:"source,omitempty"`
	// Said: the sentence the push actually spoke when this proposal
	// landed — the proposer's `--say`, or the one the hub wrote when it sent
	// none. Kept ON the card for the same reason an ask keeps it: the
	// approval card is answered from what was heard, so the words heard
	// belong on it. Written on the push path alone, so it always means "this
	// was spoken", never "this would have been" — ungated rows (which never
	// push) are empty.
	Said string `json:"said,omitempty"`
	// Outcomes: the buttons on the card, worded here like an ask's
	// (store/close.go ActionOutcomes) so both surfaces draw one row. A
	// session's proposal is answered through its chat bar —
	// Approve · Deny · Reply each arm it; a job's
	// proposal has no session to talk to, so it is Approve · Deny, decided
	// from the card. Dismiss is not an outcome: it says nothing to anyone.
	Outcomes []store.Outcome `json:"outcomes"`
	// Open / Closed / Folded / Lane: where the proposal stands with the owner
	// (store.ActionStanding) — both clients draw these instead of reading
	// `state` against lists of their own.
	Open   bool   `json:"open"`
	Closed bool   `json:"closed"`
	Folded string `json:"folded,omitempty"`
	Lane   string `json:"lane"`
	Reopen bool   `json:"reopen,omitempty"`
	// Window: when it is owed — now: a proposal waits on the owner the moment it
	// lands (the item's own `win`).
	Window string `json:"window"`
}

// stamp fills what the row's own fields decide: its buttons and its standing.
func (a *Action) stamp() {
	a.Outcomes = store.ActionOutcomes(a.ThreadID != "")
	s := store.ActionStanding(a.State, a.DecidedVia)
	a.Open, a.Closed, a.Folded, a.Lane, a.Reopen = s.Open, s.Closed, s.Folded, s.Lane, s.Reopen
}

// Event: one line of an action's audit trail (proposed → approved/denied →
// ran/failed). Actor is who did it: the proposer's source, decided_via, or
// `hub` for the executor.
type Event struct {
	ID       int64  `json:"id"`
	ActionID string `json:"action_id"`
	TS       string `json:"ts"`
	Event    string `json:"event"`
	Actor    string `json:"actor,omitempty"`
	Note     string `json:"note,omitempty"`
}

type Proposal struct {
	ThreadID    string          `json:"thread_id"`
	RunID       string          `json:"run_id"`
	MessageID   int64           `json:"message_id"`
	Source      string          `json:"source"`
	Project     string          `json:"project"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail"`
	Gated       *bool           `json:"gated"`
	ExecType    string          `json:"exec_type"`
	ExecPayload json.RawMessage `json:"exec_payload"`
	// Say: what the push should speak when the card lands — one
	// conversational sentence written by the proposing session (`lifectl
	// propose --say`, `lifectl relay --say`), exactly as an ask carries one
	// (notify/voice.go). Empty = the hub speaks spokenApproval instead.
	Say string `json:"say"`
}

// Executor runs an approved action's exec. Implementations: shell, claude.
type Executor interface {
	Exec(ctx context.Context, typ string, payload json.RawMessage) (result string, err error)
}

// Notifier is told when a gated proposal needs the owner (nil = no notifications).
// One method on purpose: the hub has one notification, the NEEDS YOU push;
// completions and FYIs are records (the action row, the run row), not pings.
// A notifier that also has `Card(kind, line, say, thread, card string) error` (the
// scheduler does) takes that lane instead — the push then SPEAKS the card's
// own sentence and the hub keeps it on the row (Action.Said).
type Notifier interface {
	NeedsYou(string) error
}

// spokenApproval: what the push says when a proposal lands and the session
// that made it wrote no `--say`. "Please approve: <title>" is a label read by
// a robot; notify.Spoken makes one sentence, the ask first, no id in it.
func spokenApproval(title string) string {
	return notify.Spoken("approval", "", plainSay(title), "")
}

// plainSay: a title as it should be HEARD — markdown marks and the curly
// quotes a relay card wraps a session title in are noise out loud.
func plainSay(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '`', '“', '”', '"', '#':
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

type Queue struct {
	db   *store.DB
	exec Executor
	nfy  Notifier
	mu   sync.Mutex
	// FreeShell: the commands a shell proposal may run without the owner's
	// card (freeShell). main sets read-only ones; nil = every shell run is
	// gated.
	FreeShell []string
	// OnDecided fires after the owner approves/denies a gated action (main relays
	// it into the proposing thread, or starts one). nil = no callback.
	OnDecided func(a Action, approved bool, note string)
	// RelayTarget / Relay: the `relay` exec type — one session's words handed
	// to another session, which only ever happens on an approved card.
	// RelayTarget names the session a proposal addresses (an error = it
	// cannot take one); Relay delivers once the owner approves. Hooks, so
	// this package never reads the threads tables. nil = relay is refused.
	RelayTarget func(threadID string) (title string, err error)
	Relay       func(from, to, text, actionID string) (result string, err error)
	// onUncertain / untold: who hears that an action ended `uncertain`, and
	// the ones that did before anyone was listening: closeOrphans runs inside
	// New, before main has the sessions to tell (TellUncertain).
	onUncertain func(a Action)
	untold      []string
}

// Uncertain: a run cut off from outside (the hub restarted under it, or the
// timeout or a signal killed it) may have done all, part or none of its work,
// so it is not `failed` (a clean non-zero exit, nothing to doubt). Running the
// same command again after one may copy or insert twice.
const Uncertain = "uncertain"

// UncertainNote: what the proposing session is told, once, when its action
// ends uncertain.
func UncertainNote(a Action) string {
	why := a.Error
	if why == "" {
		why = "cut off mid-run"
	}
	return fmt.Sprintf("Your proposal %s “%s” did not finish (%s), so its outcome is unknown: it may have run fully, partly or not at all. "+
		"Check what it actually did (files, rows, git log) before you propose it again; an identical command is held for the owner either way.", a.ID, a.Title, why)
}

// TellUncertain sets who hears that an action ended uncertain and tells them
// about every one that did before this (the restart's orphans).
func (q *Queue) TellUncertain(f func(a Action)) {
	q.mu.Lock()
	q.onUncertain = f
	ids := q.untold
	q.untold = nil
	q.mu.Unlock()
	for _, id := range ids {
		q.tellUncertain(id)
	}
}

func (q *Queue) tellUncertain(id string) {
	q.mu.Lock()
	f := q.onUncertain
	if f == nil {
		q.untold = append(q.untold, id)
	}
	q.mu.Unlock()
	if f == nil {
		return
	}
	if a, err := q.Get(id); err == nil {
		f(a)
	}
}

// relayMax: a hand-off is a task, not a transcript.
const relayMax = 4000

// relayPayload: what a `relay` proposal carries.
type relayPayload struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// relayCard checks a relay proposal and writes its card. The title and detail
// are the hub's, never the proposer's: what the owner approves is exactly what is
// delivered, to exactly the session named. The proposer's own detail rides
// underneath as the reason.
func (q *Queue) relayCard(p *Proposal) error {
	if q.RelayTarget == nil || q.Relay == nil {
		return errors.New("relay is not wired on this hub")
	}
	from := strings.TrimSpace(p.ThreadID)
	if from == "" {
		return errors.New("relay: only a session can hand a task to another session (thread_id required)")
	}
	var rp relayPayload
	if err := json.Unmarshal(p.ExecPayload, &rp); err != nil {
		return errors.New("relay: need {to, text}")
	}
	rp.To, rp.Text = strings.TrimSpace(rp.To), strings.TrimSpace(rp.Text)
	switch {
	case rp.To == "" || rp.Text == "":
		return errors.New("relay: need {to, text}")
	case rp.To == from:
		return errors.New("relay: that is this session — use `lifectl prompt`")
	case len(rp.Text) > relayMax:
		return fmt.Errorf("relay: text is %d characters, the limit is %d — hand over the task, not the transcript", len(rp.Text), relayMax)
	}
	title, err := q.RelayTarget(rp.To)
	if err != nil {
		return fmt.Errorf("relay: %v", err)
	}
	why := strings.TrimSpace(p.Detail)
	p.Title = truncate("Send to the session “"+title+"”", 120)
	p.Detail = rp.Text
	if why != "" {
		p.Detail += "\n\nWhy: " + why
	}
	p.ExecPayload, _ = json.Marshal(rp)
	return nil
}

func (q *Queue) relay(a Action) (string, error) {
	var rp relayPayload
	if err := json.Unmarshal(a.ExecPayload, &rp); err != nil || rp.To == "" || rp.Text == "" {
		return "", errors.New("relay: need {to, text}")
	}
	if q.Relay == nil {
		return "", errors.New("relay is not wired on this hub")
	}
	return q.Relay(a.ThreadID, rp.To, rp.Text, a.ID)
}

// Schema: the proposal queue. Append an ALTER to add a column (store/migrate.go).
var Schema = []string{
	`CREATE TABLE IF NOT EXISTS actions (
		id TEXT PRIMARY KEY,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		project TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL,            -- money | delete | contact | share | commit | other
		title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
		gated INTEGER NOT NULL,        -- 1 = needs the owner's approval
		exec_type TEXT NOT NULL,       -- none | shell | claude | imessage
		exec_payload TEXT NOT NULL DEFAULT '{}',
		state TEXT NOT NULL,           -- proposed | approved | denied | running | done | failed
		decided_at TEXT, decided_via TEXT,
		result TEXT, error TEXT,
		notified_at TEXT)`,
	`CREATE INDEX IF NOT EXISTS actions_state ON actions(state, created_at)`,
	// thread_id: the session that proposed it; note: the owner's words with their decision.
	`ALTER TABLE actions ADD COLUMN thread_id TEXT`,
	`ALTER TABLE actions ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
	// The backlink asks already have (run + reply that
	// raised it), who proposed it, and an audit trail like ask_events.
	`ALTER TABLE actions ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE actions ADD COLUMN message_id INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE actions ADD COLUMN source TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS action_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		action_id TEXT NOT NULL, ts TEXT NOT NULL,
		event TEXT NOT NULL, actor TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX IF NOT EXISTS action_events_action ON action_events(action_id, id)`,
	// said: the words the push SPOKE for this proposal. The same column asks
	// have, for the same reason — otherwise the only copy of what was heard
	// is the APNs payload, which keeps nothing.
	`ALTER TABLE actions ADD COLUMN said TEXT NOT NULL DEFAULT ''`,
	// items: a proposal's card — title, detail, state, the decision, the
	// words it spoke — moves to the one table of what reaches the owner
	// (store.ItemsSchema). `actions` stays as the run
	// record under the same id (project, gate, exec, result, error); its
	// state/decided_*/note/said columns are frozen from here on. The trail's
	// event words become state moves (ran = done, reopened = proposed), each
	// from the state the one before it left.
	`INSERT OR IGNORE INTO items (id,src,created_at,updated_at,title,detail,kind,state,thread_id,run_id,message_id,source,said,resolved_at,resolved_by,resolution)
	SELECT id,'action',created_at,updated_at,title,detail,kind,state,thread_id,run_id,message_id,source,said,decided_at,decided_via,note FROM actions ORDER BY created_at;
INSERT INTO item_events (item_id,ts,actor,from_state,to_state,note)
	SELECT action_id,ts,actor,COALESCE(LAG(st) OVER (PARTITION BY action_id ORDER BY id),''),st,note FROM
	(SELECT id,action_id,ts,actor,note,CASE event WHEN 'ran' THEN 'done' WHEN 'reopened' THEN 'proposed' ELSE event END AS st FROM action_events) ORDER BY id;
UPDATE items SET ` + store.ItemStampSet + ` WHERE src='action';`,
}

// setState is the ONE writer of a proposal's state: move is the item's own
// write (with its guard), run the run record's (same transaction), and the
// move is appended to item_events. action_events stays the run's audit trail
// (q.event), in its own words. moved = the guard held.
func (q *Queue) setState(id, by, note string, move store.Stmt, run ...store.Stmt) (bool, error) {
	return store.ItemLog.MoveWith(q.db, id, by, note, false, time.Now(), move, run...)
}

func stmt(query string, args ...any) store.Stmt { return store.Stmt{Q: query, Args: args} }

func New(db *store.DB, exec Executor, nfy Notifier) *Queue {
	if _, err := db.Migrate("items", store.ItemsSchema); err != nil {
		log.Printf("actions: migrate items: %v", err)
	}
	if _, err := db.Migrate("actions", Schema); err != nil {
		log.Printf("actions: migrate: %v", err)
	}
	q := &Queue{db: db, exec: exec, nfy: nfy}
	q.closeOrphans()
	return q
}

// closeOrphans closes every action still 'running' at startup: its command
// was a child of the hub that died, so nothing will ever record its result.
// It ends `uncertain`, not `failed` (nobody knows how far it got), and the
// session that proposed it is told so (TellUncertain).
func (q *Queue) closeOrphans() {
	rows, err := q.db.Query(`SELECT id FROM items WHERE src='action' AND state='running'`)
	if err != nil {
		log.Printf("actions: orphans: %v", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	const why = "hub restarted mid-run"
	for _, id := range ids {
		now := ts(time.Now())
		if _, err := q.setState(id, "hub", why,
			stmt(`UPDATE items SET state=?, updated_at=? WHERE id=? AND src='action' AND state='running'`, Uncertain, now, id),
			stmt(`UPDATE actions SET error=?, updated_at=? WHERE id=?`, why, now, id)); err != nil {
			log.Printf("actions: orphan %s: %v", id, err)
			continue
		}
		q.event(id, Uncertain, "hub", why)
		q.tellUncertain(id)
	}
}

// cutOff: the run was stopped from outside (its context ran out, the
// 30-minute timeout, or a signal ended it: "signal: killed", "signal:
// terminated") rather than exiting on its own.
func cutOff(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return !ee.Exited()
	}
	return false
}

// sameCommand: the line a proposal's card gets when the very same command
// (kind, exec type, payload) ended done or uncertain in the last 7 days,
// "Same command as <id> (<state> <day>)", or "". Such a proposal is held for
// the owner whatever its kind: running it again may do the work twice.
func (q *Queue) sameCommand(kind, typ string, payload json.RawMessage) string {
	if typ == "none" || typ == "relay" {
		return ""
	}
	// Compared minified on both sides, so spacing is not a different command.
	norm := string(payload)
	var c bytes.Buffer
	if json.Compact(&c, payload) == nil {
		norm = c.String()
	}
	var id, state, at string
	err := q.db.QueryRow(`SELECT i.id, i.state, i.updated_at`+joined+`WHERE r.kind=? AND r.exec_type=?
		AND (CASE WHEN json_valid(r.exec_payload) THEN json(r.exec_payload) ELSE r.exec_payload END)=?
		AND i.state IN ('done',?) AND i.updated_at>=? ORDER BY i.updated_at DESC LIMIT 1`,
		kind, typ, norm, Uncertain, ts(time.Now().Add(-7*24*time.Hour))).Scan(&id, &state, &at)
	if err != nil {
		return ""
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return fmt.Sprintf("Same command as %s (%s %s)", id, state, store.Day(t))
}

// SetThread records the session that took an action on — the one started to
// carry out a proposal a scheduled job made outside any session — so the
// owner's later notes on it have somewhere to go.
func (q *Queue) SetThread(id, threadID string) error {
	_, err := q.db.Exec(`UPDATE items SET thread_id=?, updated_at=? WHERE id=? AND src='action'`, threadID, ts(time.Now()), id)
	return err
}

// newID: the stamped form, `YYYYMMDD-HHMMSS-<hex>` — the stamp is the prefix.
func newID() string { return store.NewID(time.Now().UTC().Format("20060102-150405")) }

var validExec = map[string]bool{"none": true, "shell": true, "claude": true, "relay": true}

// Propose records an action. Ungated actions run immediately (async).
func (q *Queue) Propose(p Proposal) (Action, error) {
	if p.ExecType == "relay" {
		if err := q.relayCard(&p); err != nil {
			return Action{}, err
		}
	}
	if strings.TrimSpace(p.Title) == "" {
		return Action{}, errors.New("title required")
	}
	if p.Kind == "" {
		p.Kind = "other"
	}
	if p.ExecType == "" {
		p.ExecType = "none"
	}
	if !validExec[p.ExecType] {
		return Action{}, fmt.Errorf("bad exec_type %q", p.ExecType)
	}
	if len(p.ExecPayload) == 0 {
		p.ExecPayload = json.RawMessage("{}")
	}
	gated := gatedKinds[p.Kind]
	if p.Gated != nil && *p.Gated {
		gated = true
	}
	if p.ExecType == "relay" {
		gated = true // never auto-runs, whatever the proposer says
	}
	switch p.ExecType {
	case "claude":
		gated = true
	case "shell":
		var sp struct {
			Cmd string `json:"cmd"`
		}
		json.Unmarshal(p.ExecPayload, &sp)
		if !freeShell(q.FreeShell, sp.Cmd) {
			gated = true
		}
	}
	// A retry of a command that already ran, or may have, is the owner's to
	// approve: the card says which run it repeats.
	if same := q.sameCommand(p.Kind, p.ExecType, p.ExecPayload); same != "" {
		p.Detail = strings.TrimRight(p.Detail, "\n") + "\n\n" + same
		gated = true
	}
	now := time.Now().UTC()
	a := Action{ID: newID(), CreatedAt: now, UpdatedAt: now, Project: p.Project, Kind: p.Kind, Title: p.Title,
		Detail: p.Detail, Gated: gated, ExecType: p.ExecType, ExecPayload: p.ExecPayload, State: "proposed", ThreadID: strings.TrimSpace(p.ThreadID),
		RunID: strings.TrimSpace(p.RunID), MessageID: p.MessageID, Source: strings.TrimSpace(p.Source)}
	if a.Source == "" && a.ThreadID != "" {
		a.Source = "claude:thread:" + a.ThreadID
	}
	if !gated {
		a.State = "approved"
		a.DecidedVia = "auto"
		a.DecidedAt = &now
	}
	a.stamp()
	// The item is the card; the run record keeps the proposal whole (its own
	// state and decision are the record as filed, never updated).
	_, err := q.setState(a.ID, a.Source, "",
		stmt(`INSERT INTO items (id,src,created_at,updated_at,title,detail,kind,state,thread_id,run_id,message_id,source,resolved_at,resolved_by)
		VALUES (?,'action',?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, ts(a.CreatedAt), ts(a.UpdatedAt), a.Title, a.Detail, a.Kind, a.State, nullStr(a.ThreadID), a.RunID, a.MessageID, a.Source, tsp(a.DecidedAt), nullStr(a.DecidedVia)),
		stmt(`INSERT INTO actions (id,created_at,updated_at,project,kind,title,detail,gated,exec_type,exec_payload,state,decided_at,decided_via,thread_id,run_id,message_id,source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, ts(a.CreatedAt), ts(a.UpdatedAt), a.Project, a.Kind, a.Title, a.Detail, b2i(a.Gated), a.ExecType, string(a.ExecPayload), a.State, tsp(a.DecidedAt), a.DecidedVia, nullStr(a.ThreadID), a.RunID, a.MessageID, a.Source))
	if err != nil {
		return Action{}, err
	}
	q.event(a.ID, "proposed", a.Source, "")
	if !gated {
		q.event(a.ID, "approved", "auto", "ungated")
	}
	if gated {
		if q.nfy != nil {
			// Spoken aloud: the verb first, no id to read out.
			msg := fmt.Sprintf("Please approve: %s\n%s", a.Title, truncate(a.Detail, 300))
			// The one push funnel (notify.Card, the lane an ask and a calendar
			// nag take too): a proposal is a card, so it speaks its own
			// sentence instead of the "Needs you" label, and the sentence is
			// kept on the row. A notifier without that lane still gets the
			// label push and stores nothing — `said` means spoken.
			say := strings.TrimSpace(p.Say)
			if say == "" {
				say = spokenApproval(a.Title)
			}
			if said, err := notify.Card(q.nfy, "approval", msg, say, a.ThreadID, a.ID); err != nil {
				log.Printf("notify: %v", err)
			} else if said != "" {
				a.Said = said
				q.db.Exec(`UPDATE items SET said=? WHERE id=?`, said, a.ID)
				q.db.Exec(`UPDATE actions SET notified_at=? WHERE id=?`, ts(time.Now()), a.ID)
			} else {
				q.db.Exec(`UPDATE actions SET notified_at=? WHERE id=?`, ts(time.Now()), a.ID)
			}
		}
	} else {
		go q.run(a.ID)
	}
	return a, nil
}

// Decide approves or denies a gated action. via = "app" | "web" | "cli".
// note is the owner's optional message to the proposing session ("yes, but only
// the first one"); stored on the action and relayed via OnDecided.
func (q *Queue) Decide(id string, approve bool, via, note string) (Action, error) {
	return q.decide(id, approve, via, note, true)
}

// DecideQuiet is Decide without the OnDecided relay: for a decision that
// arrived ON a prompt to the proposing session (threads.Manager.DecideAction),
// where the prompt itself is the relay. A row already in the asked-for state
// is left alone (nil error) — /actions/{id}/approve records first and then
// relays through the same prompt, so the second arrival must not be a fault.
func (q *Queue) DecideQuiet(id string, approve bool, via, note string) error {
	if a, err := q.Get(id); err == nil && a.State != "proposed" {
		// An approved row moves on to running/done/failed on its own.
		same := a.State == "denied"
		if approve {
			same = a.State == "approved" || a.State == "running" || a.State == "done" || a.State == "failed" || a.State == Uncertain
		}
		if same {
			return nil
		}
		return fmt.Errorf("action is %s, not proposed", a.State)
	}
	_, err := q.decide(id, approve, via, note, false)
	return err
}

func (q *Queue) decide(id string, approve bool, via, note string, relay bool) (Action, error) {
	q.mu.Lock()
	a, err := q.Get(id)
	if err != nil {
		q.mu.Unlock()
		return Action{}, err
	}
	if a.State != "proposed" {
		q.mu.Unlock()
		return a, fmt.Errorf("action is %s, not proposed", a.State)
	}
	state := "denied"
	if approve {
		state = "approved"
	}
	now := time.Now().UTC()
	note = strings.TrimSpace(note)
	_, err = q.setState(id, via, note, stmt(`UPDATE items SET state=?, resolved_at=?, resolved_by=?, resolution=?, updated_at=? WHERE id=? AND src='action'`, state, ts(now), via, note, ts(now), id))
	q.mu.Unlock()
	if err != nil {
		return Action{}, err
	}
	q.event(id, state, via, note)
	if approve {
		go q.run(id)
	}
	a, err = q.Get(id)
	if err == nil && relay && q.OnDecided != nil {
		q.OnDecided(a, approve, note)
		// A job's proposal has no thread until the callback starts one
		// (SetThread); read again so the caller gets the session to hand
		// off to, as it does for a session's own proposal.
		if a.ThreadID == "" {
			if b, gerr := q.Get(id); gerr == nil {
				a = b
			}
		}
	}
	return a, err
}

// Dismiss is the silent close every card has, for a proposal that went out
// of date unanswered: the proposal leaves the owner's board,
// nothing runs, and the session is not told — so it needs no decider code,
// which guards only what can run. Only a proposed row can be dismissed;
// Reopen puts it back exactly as it was, with the trail showing both.
func (q *Queue) Dismiss(id, via string) (Action, error) {
	return q.move(id, "proposed", "dismissed", "dismissed", via)
}

// Reopen undoes a Dismiss (the folded card's one button).
func (q *Queue) Reopen(id, via string) (Action, error) {
	return q.move(id, "dismissed", "proposed", "reopened", via)
}

func (q *Queue) move(id, from, to, event, via string) (Action, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, err := q.Get(id)
	if err != nil {
		return Action{}, err
	}
	if a.State != from {
		return a, fmt.Errorf("action is %s, not %s", a.State, from)
	}
	if _, err := q.setState(id, via, event, stmt(`UPDATE items SET state=?, updated_at=? WHERE id=? AND src='action'`, to, ts(time.Now().UTC()), id)); err != nil {
		return Action{}, err
	}
	q.event(id, event, via, "")
	return q.Get(id)
}

func (q *Queue) run(id string) {
	a, err := q.Get(id)
	if err != nil || a.State != "approved" {
		return
	}
	// Claim it: of two runs racing on one approval, only one moves it on.
	// Nothing to execute goes straight to done.
	to := "running"
	if a.ExecType == "none" {
		to = "done"
	}
	claimed, err := q.setState(id, "hub", "", stmt(`UPDATE items SET state=?, updated_at=? WHERE id=? AND src='action' AND state='approved'`, to, ts(time.Now()), id))
	if err != nil {
		log.Printf("actions: run %s: %v", id, err)
		return
	}
	if !claimed {
		return
	}
	if a.ExecType == "none" {
		q.event(id, "ran", "hub", "")
		return
	}
	if a.ExecType == "relay" {
		res, err := q.relay(a)
		q.finish(id, res, err, false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	res, err := q.exec.Exec(ctx, a.ExecType, a.ExecPayload)
	q.finish(id, res, err, cutOff(ctx, err))
}

// runTimeout: how long an approved command may run before it is killed (a
// var so a test can cut one off).
var runTimeout = 30 * time.Minute

// finish records how a run ended: done, failed (it exited with an error), or
// uncertain (cut = it was stopped from outside, so how far it got is unknown).
func (q *Queue) finish(id, res string, err error, cut bool) {
	state, e := "done", ""
	if err != nil {
		state, e = "failed", err.Error()
		if cut {
			state = Uncertain
		}
	}
	now := ts(time.Now())
	if _, dbErr := q.setState(id, "hub", truncate(e, 2000),
		stmt(`UPDATE items SET state=?, updated_at=? WHERE id=? AND src='action'`, state, now, id),
		stmt(`UPDATE actions SET result=?, error=?, updated_at=? WHERE id=?`, truncate(res, 20000), e, now, id)); dbErr != nil {
		log.Printf("actions: finish %s: %v", id, dbErr)
	}
	if err != nil {
		q.event(id, state, "hub", truncate(e, 2000))
		if cut {
			q.tellUncertain(id)
		}
	} else {
		q.event(id, "ran", "hub", truncate(res, 2000))
	}
}

// Dated: every action whose decided day — else its created day, both local
// (store.Day) — falls in [from, to], as agenda rows: the calendar is the
// "when" view over the same objects the board shows "now". Actor is who
// decided it, else who proposed it.
func (q *Queue) Dated(from, to string) ([]store.Dated, error) {
	// Decided/created instants are stored in UTC (store.TS, fixed width, so
	// string order is time order); the SQL window is padded a day each side
	// and the exact local-day test happens here.
	lo, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return nil, errors.New("from must be YYYY-MM-DD")
	}
	hi, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return nil, errors.New("to must be YYYY-MM-DD")
	}
	rows, err := q.db.Query(`SELECT `+cols+joined+`WHERE COALESCE(i.resolved_at, i.created_at) >= ? AND COALESCE(i.resolved_at, i.created_at) < ? ORDER BY i.created_at`,
		ts(lo.AddDate(0, 0, -1)), ts(hi.AddDate(0, 0, 2)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Dated{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		// The SQL window is padded a day each side (UTC instants, local days);
		// the row's own day decides whether it is really in range.
		d := Row(a)
		if d.Day < from || d.Day > to {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Row projects one proposal into the shared read-model row (store.Dated,
// Phase 5) — the ONE place an action becomes a thing the board bundles or the
// agenda places on a day. Its day is the day it was decided, else the day it
// was proposed; its actor is whoever decided it, else whoever proposed it.
func Row(a Action) store.Dated {
	// …and its TIME is that same instant, not just the day (which would stack
	// every proposal in the calendar's all-day band). A proposal happens at a
	// moment; it belongs at that moment.
	when, actor := a.CreatedAt, a.Source
	if a.DecidedAt != nil {
		when = *a.DecidedAt
		if a.DecidedVia != "" {
			actor = a.DecidedVia
		}
	}
	d := store.Dated{
		// No Detail: a proposal argues its case at length and the agenda row is
		// one line. The board's card reads the whole Action out of Obj instead.
		ID: a.ID, Kind: "action", Ref: "action:" + a.ID, Title: a.Title, State: a.State,
		Day: store.Day(when), At: store.Clock(when), ThreadID: a.ThreadID, Actor: actor, Source: a.Source, RunID: a.RunID,
		// A proposal has no surface of its own: it is answerable wherever it is
		// seen, and the phone's rule is about its *session* running, not it.
		Surface: "any", Window: a.Window, Obj: a,
	}
	// Decided, it is a record: the OWNER's deed when they approved or
	// denied it from one of their surfaces, the hub's when the gate let it through
	// on its own. The verb is what the decider did — Approved even once the
	// action has since run, because the running was the hub's part.
	if a.DecidedAt != nil && a.State != "proposed" {
		d.Did = true
		d.Verb = store.ActionDidVerb(a.State, store.Owner(a.DecidedVia))
	}
	return d
}

// Open: the proposals waiting on the owner's decision, as rows — this table's share
// of store.Items (the list order: List's).
func (q *Queue) Open() ([]store.Dated, error) {
	if q == nil {
		return nil, nil
	}
	acts, err := q.List("proposed", 500)
	if err != nil {
		return nil, err
	}
	out := make([]store.Dated, 0, len(acts))
	for _, a := range acts {
		out = append(out, Row(a))
	}
	return out, nil
}

// event appends one audit line. Failures are logged, never returned: the
// trail is a record of what happened, not a gate on it happening.
func (q *Queue) event(id, event, actor, note string) {
	if _, err := q.db.Exec(`INSERT INTO action_events (action_id, ts, event, actor, note) VALUES (?,?,?,?,?)`, id, ts(time.Now()), event, actor, note); err != nil {
		log.Printf("actions: event %s %s: %v", id, event, err)
	}
}

// Events: the audit trail of one action, oldest first.
func (q *Queue) Events(id string) ([]Event, error) {
	rows, err := q.db.Query(`SELECT id, action_id, ts, event, actor, note FROM action_events WHERE action_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ActionID, &e.TS, &e.Event, &e.Actor, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// cols / joined: a proposal is its item (i: the card and its decision)
// joined to its run record (r: what runs, and what it returned).
const cols = `i.id,i.created_at,i.updated_at,r.project,i.kind,i.title,i.detail,r.gated,r.exec_type,r.exec_payload,i.state,i.resolved_at,i.resolved_by,
	r.result,r.error,i.thread_id,i.resolution,COALESCE(i.run_id,''),COALESCE(i.message_id,0),i.source,i.said,i.win`

const joined = ` FROM items i JOIN actions r ON r.id=i.id AND i.src='action' `

func (q *Queue) Get(id string) (Action, error) {
	row := q.db.QueryRow(`SELECT `+cols+joined+`WHERE i.id=?`, id)
	return scan(row)
}

// List returns newest first; state="" = all, "open" = proposed|approved|running.
func (q *Queue) List(state string, limit int) ([]Action, error) {
	return q.ListFor(state, "", limit)
}

// ListFor narrows List to one thread's proposals ("" = every thread), so a
// chat reads its own cards instead of the whole queue.
func (q *Queue) ListFor(state, threadID string, limit int) ([]Action, error) {
	where := "WHERE 1=1"
	switch state {
	case "":
	case "open":
		where += " AND i.state IN ('proposed','approved','running')"
	default:
		where += " AND i.state = '" + strings.NewReplacer("'", "").Replace(state) + "'"
	}
	var args []any
	if threadID != "" {
		where += " AND i.thread_id = ?"
		args = append(args, threadID)
	}
	rows, err := q.db.Query(fmt.Sprintf(`SELECT `+cols+joined+`%s ORDER BY i.created_at DESC LIMIT %d`, where, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Action, error) {
	var a Action
	var created, updated string
	var decided, via, res, errS, thr, note, said sql.NullString
	var gated int
	var payload string
	if err := r.Scan(&a.ID, &created, &updated, &a.Project, &a.Kind, &a.Title, &a.Detail, &gated, &a.ExecType, &payload, &a.State, &decided, &via, &res, &errS, &thr, &note, &a.RunID, &a.MessageID, &a.Source, &said, &a.Window); err != nil {
		return Action{}, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	a.Gated = gated == 1
	a.ExecPayload = json.RawMessage(payload)
	if decided.Valid {
		t, _ := time.Parse(time.RFC3339Nano, decided.String)
		a.DecidedAt = &t
	}
	a.DecidedVia, a.Result, a.Error = via.String, res.String, errS.String
	a.ThreadID, a.Note, a.Said = thr.String, note.String, said.String
	a.stamp()
	return a, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ts(t time.Time) string { return store.TS(t) }
func tsp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// DefaultExecutor: shell (bash -c in a dir), claude (headless -p in a
// project dir). Nothing here can contact anyone: contacting others is a
// connector with its own gate.
type DefaultExecutor struct {
	ClaudeBin  string
	ProjectDir func(name string) (string, bool)
	// ClaudeArgs: --model and friends for the "claude" exec type, from the
	// model policy's `job` rule (these run unattended). nil = CLI defaults.
	ClaudeArgs func() []string
}

func (e DefaultExecutor) Exec(ctx context.Context, typ string, payload json.RawMessage) (string, error) {
	switch typ {
	case "shell":
		var p struct {
			Cmd string `json:"cmd"`
			Dir string `json:"dir"`
		}
		if err := json.Unmarshal(payload, &p); err != nil || p.Cmd == "" {
			return "", errors.New("shell: need {cmd, dir?}")
		}
		c := exec.CommandContext(ctx, "/bin/bash", "-lc", p.Cmd)
		c.Dir = p.Dir
		out, err := c.CombinedOutput()
		return string(out), err
	case "claude":
		var p struct {
			Project string `json:"project"`
			Prompt  string `json:"prompt"`
		}
		if err := json.Unmarshal(payload, &p); err != nil || p.Prompt == "" {
			return "", errors.New("claude: need {project, prompt}")
		}
		dir, ok := e.ProjectDir(p.Project)
		if !ok {
			return "", fmt.Errorf("unknown project %q", p.Project)
		}
		args := []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits"}
		if e.ClaudeArgs != nil {
			args = append(args, e.ClaudeArgs()...)
		}
		c := exec.CommandContext(ctx, e.ClaudeBin, args...)
		c.Dir = dir
		c.Stdin = strings.NewReader(p.Prompt)
		out, err := c.CombinedOutput()
		if err != nil {
			return string(out), err
		}
		var r struct {
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(out, &r) == nil {
			if r.IsError {
				return r.Result, errors.New("claude reported error")
			}
			return r.Result, nil
		}
		return string(out), nil
	}
	return "", fmt.Errorf("unknown exec type %q", typ)
}
