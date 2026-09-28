package threads

// Prompts: the ONE inbound lane. Everything a session is woken by is a prompt
// row — the owner's typed message, their response to a card, an agent asking
// itself to check back later, and (phase 2) check-ins, calendar agent items
// and jobs. A response references the thing it came from; a prompt can be
// sent now, later or on a cadence; an agent can prompt itself to check back.
//
// What that replaces: the hub used to compose a sentence ("Done: <title>
// (ask-xxxx)", "Re ask-xxxx …", "Approved: <title> (action …)") and then
// recognise its own sentence again with a regex to work out what was being
// answered. A prompt carries the reference as DATA:
//
//	in_reply_to  ask:<id> | action:<id> | rec:<id> | cal:<id> | message:<id>
//	outcome      done | wont | approved | denied | ''   (words only)
//
// The framing the model reads is rendered from the referenced row at wake time
// (refHeader, used by runner.prompt), so it can never be wrong about WHICH
// thing is being answered, and the owner's own words are never edited or parsed.
//
// Delivery still writes a thread_messages row — that is what both surfaces
// read — and the reference rides on it (in_reply_to/outcome columns).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"life/hub/internal/cadence"
	"life/hub/internal/store"
)

// PromptsSchema: the queue, plus the reference columns on thread_messages.
var PromptsSchema = []string{
	`CREATE TABLE IF NOT EXISTS prompts (
		id TEXT PRIMARY KEY, created_at TEXT NOT NULL,
		author TEXT NOT NULL,                  -- owner | hub | claude:thread:<id>
		target TEXT NOT NULL,                  -- <thread id> | new | new-or:<thread id>
		not_before TEXT NOT NULL DEFAULT '',   -- '' = now
		in_reply_to TEXT NOT NULL DEFAULT '',  -- <type>:<id>, '' = about nothing in particular
		outcome TEXT NOT NULL DEFAULT '',      -- done | wont | approved | denied | ''
		text TEXT NOT NULL DEFAULT '',
		attachments TEXT NOT NULL DEFAULT '[]',
		title TEXT NOT NULL DEFAULT '',        -- for a session this prompt starts
		goal_id TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'queued',  -- queued | delivered | cancelled | failed
		delivered_at TEXT, delivered_thread TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX IF NOT EXISTS prompts_due ON prompts(state, not_before)`,
	`ALTER TABLE thread_messages ADD COLUMN in_reply_to TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE thread_messages ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`,
	// One clock: a STANDING row has a
	// repeat cadence and stays queued, its not_before always the next
	// occurrence; each firing writes a one-shot child row (parent = the
	// standing row's id) that carries the delivery record. Thread check-ins
	// and jobs are standing rows (target <thread id> | job:<name>), so every
	// future wake of the hub is one query on this table.
	stmtPromptRepeat,
	`ALTER TABLE prompts ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
	`CREATE INDEX IF NOT EXISTS prompts_target ON prompts(target, state)`,
	// Who sent a message (owner | hub | claude:thread:<id>), copied from the
	// prompt that delivered it. Role only says how it is coloured: an opening
	// brief another session or the hub wrote is a blue "owner" row too, and
	// without this there is no telling where a blue message came from.
	stmtMessageAuthor,
	// ONE message, SEVERAL cards: the owner can answer several of an agent's
	// cards with one message. `replies` is the JSON list of
	// every {ref, outcome} the message answers; in_reply_to/outcome stay the
	// FIRST of them, so every reader of one reference keeps working.
	`ALTER TABLE prompts ADD COLUMN replies TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE thread_messages ADD COLUMN replies TEXT NOT NULL DEFAULT ''`,
}

const stmtMessageAuthor = `ALTER TABLE thread_messages ADD COLUMN author TEXT NOT NULL DEFAULT ''`

// backfillMessageAuthor runs once, with the column: every delivered prompt
// not from the owner names the owner/system row it wrote — the one in its
// session stamped within two seconds of delivery, nearest first (the insert
// and the delivered_at stamp are one call apart, about a second at worst). Rows with no such prompt keep an empty author and read by role (authorOf).
func (m *Manager) backfillMessageAuthor(fresh []string) {
	if !slices.Contains(fresh, stmtMessageAuthor) {
		return
	}
	res, err := m.db.Exec(`UPDATE thread_messages SET author = (
		SELECT p.author FROM prompts p
		WHERE p.state='delivered' AND p.author != 'owner' AND p.delivered_thread = thread_messages.thread_id
		  AND abs(julianday(p.delivered_at) - julianday(thread_messages.ts)) * 86400 < 2
		ORDER BY abs(julianday(p.delivered_at) - julianday(thread_messages.ts)) LIMIT 1)
	WHERE author = '' AND role IN ('owner','system') AND kind IN ('message','checkin') AND EXISTS (
		SELECT 1 FROM prompts p
		WHERE p.state='delivered' AND p.author != 'owner' AND p.delivered_thread = thread_messages.thread_id
		  AND abs(julianday(p.delivered_at) - julianday(thread_messages.ts)) * 86400 < 2)`)
	if err != nil {
		log.Printf("threads: backfill message author: %v", err)
		return
	}
	n, _ := res.RowsAffected()
	log.Printf("threads: backfilled the author of %d messages from their prompts", n)
}

// stmtPromptRepeat is named so New can pair the one-time backfill of the
// threads' schedule columns with the migration that made them derived.
const stmtPromptRepeat = `ALTER TABLE prompts ADD COLUMN repeat TEXT NOT NULL DEFAULT ''`

// defaultCheckIn is what a scheduled session is told when its standing row
// carries no words of its own.
const defaultCheckIn = "Scheduled check-in. Review this thread's task and what has changed since last time (use lifectl / the data). Do any work that is now due. If nothing is due and there is nothing the owner needs to know, reply with exactly `[end]`."

// Prompt: one thing to say to a session, now or later.
type Prompt struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	// Author: who is speaking. "owner", "hub", or "claude:thread:<id>" — an
	// agent, which may only ever address ITSELF or a new session: agents do
	// not talk to each other, but may check back in on themselves. Enforced
	// in Queue.
	Author string `json:"author"`
	// Target: the session id to wake, "new" for a fresh one, or
	// "new-or:<id>" — relay into that session if it still exists, else start
	// one (the rule deliver.Deliverer already implements).
	Target string `json:"target"`
	// NotBefore: when it may be delivered. Zero = now.
	NotBefore time.Time `json:"not_before,omitempty"`
	// InReplyTo: "<type>:<id>" — what this prompt answers (the first of
	// Replies when there are several).
	InReplyTo string `json:"in_reply_to,omitempty"`
	// Outcome: what the author claims happened to the referenced thing.
	Outcome string `json:"outcome,omitempty"`
	// Replies: EVERY card this prompt answers, in order, each with its own
	// outcome — one message closing a read card and accepting a rec at once.
	// One reference may be given either way; Queue keeps
	// the two in step (normalizeReplies).
	Replies []Reply `json:"replies,omitempty"`
	Text    string  `json:"text"`
	// Attachments: blob refs, as on a message.
	Attachments []string `json:"attachments,omitempty"`
	// Title/GoalID: used only when this prompt starts a session.
	Title  string `json:"title,omitempty"`
	GoalID string `json:"goal_id,omitempty"`
	// Repeat: a clock cadence (daily@HH:MM | weekly@Mon HH:MM | every@<dur>)
	// makes this a STANDING row — it stays queued and not_before is always the
	// next occurrence; each firing is a child row. '' = fires once.
	Repeat string `json:"repeat,omitempty"`
	// Parent: the standing row this firing came from.
	Parent string `json:"parent,omitempty"`
	// Via: the surface that carried this prompt (web|app|cli), for the
	// audit row an `action:` reply's decision writes. Not stored.
	Via string `json:"-"`
	// relayed: the approved `relay` action this prompt carries out — the ONE
	// way an agent's words reach another session (Relay, below). Unexported
	// and unstored: no API body or queued row can claim it.
	relayed string

	State           string     `json:"state"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	DeliveredThread string     `json:"delivered_thread,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// Reply: one card a prompt answers and what the author says about it.
type Reply struct {
	Ref     string `json:"ref"`
	Outcome string `json:"outcome,omitempty"`
	// Label: the verdict word the "↩ <label> · <card>" line prints
	// (store.ReplyLabel) — filled on read (Messages), never stored.
	Label string `json:"label,omitempty"`
}

// normalizeReplies makes InReplyTo/Outcome and Replies one list: a single
// reference given the old way becomes Replies[0]; a list given the new way
// puts its first on the old fields. A reference named both ways is one
// entry; a repeated ref keeps its first mention. Returns the first bad one.
func (p *Prompt) normalizeReplies() error {
	var out []Reply
	seen := map[string]bool{}
	add := func(r Reply) error {
		r.Ref, r.Outcome, r.Label = strings.TrimSpace(r.Ref), strings.TrimSpace(r.Outcome), ""
		if r.Ref == "" {
			return errors.New("replies: every entry names a ref")
		}
		kind, id := store.SplitRef(r.Ref)
		// A fired step IS its card, so a surface answering the card
		// names it as an ask; the answer is about the step, and says so.
		if kind == "ask" && strings.HasPrefix(id, "cal-") {
			kind, r.Ref = "cal", "cal:"+id
		}
		if !refTypes[kind] || id == "" {
			return fmt.Errorf("in_reply_to must be <ask|action|rec|cal|message>:<id>, got %q", r.Ref)
		}
		if r.Outcome != "" {
			want, ok := refOutcomes[r.Outcome]
			if !ok {
				return fmt.Errorf("outcome must be done|wont|approved|denied|accepted|declined|deferred, got %q", r.Outcome)
			}
			if !want[kind] {
				return fmt.Errorf("outcome %q only says something about a %s: reference one", r.Outcome, refOutcomeWant(r.Outcome))
			}
		}
		if seen[r.Ref] {
			return nil
		}
		seen[r.Ref] = true
		out = append(out, r)
		return nil
	}
	if p.InReplyTo != "" {
		if err := add(Reply{Ref: p.InReplyTo, Outcome: p.Outcome}); err != nil {
			return err
		}
	} else if p.Outcome != "" && len(p.Replies) == 0 {
		return fmt.Errorf("outcome %q only says something about a %s: reference one", p.Outcome, refOutcomeWant(p.Outcome))
	}
	for _, r := range p.Replies {
		if err := add(r); err != nil {
			return err
		}
	}
	p.Replies = out
	if len(out) > 0 {
		p.InReplyTo, p.Outcome = out[0].Ref, out[0].Outcome
	}
	return nil
}

// encodeReplies: the column's text — ” for none, so a one-reference row
// written before the column reads the same as one written after.
func encodeReplies(rs []Reply) string {
	if len(rs) == 0 {
		return ""
	}
	b, _ := json.Marshal(rs)
	return string(b)
}

// decodeReplies: the column back to a list, or the one reference the old
// columns name when the column is empty.
func decodeReplies(col, ref, outcome string) []Reply {
	var rs []Reply
	if col != "" {
		json.Unmarshal([]byte(col), &rs)
	}
	if len(rs) == 0 && ref != "" {
		rs = []Reply{{Ref: ref, Outcome: outcome}}
	}
	return rs
}

// Reference types a prompt may answer. A reference the hub cannot render is
// refused at Queue rather than silently becoming prose.
var refTypes = map[string]bool{"ask": true, "action": true, "rec": true, "cal": true, "message": true}

// Outcomes, by what they can be said about. "" is always allowed (words only).
// done|wont are said about an ask, or about one of the owner's own
// calendar steps: closing a `cal:` item from either surface is this prompt
// with words, never a bare button (ClaimCal).
var refOutcomes = map[string]map[string]bool{
	"done": {"ask": true, "cal": true}, "wont": {"ask": true, "cal": true},
	"approved": {"action": true}, "denied": {"action": true},
	// A rec's three answers (the fourth, "just reply", is "" with words). The
	// rec itself is decided by POST /recs/{id}/decide before the prompt is
	// queued; this only says what the prompt answers.
	"accepted": {"rec": true}, "declined": {"rec": true}, "deferred": {"rec": true},
}

// CarriesOutcome: can a prompt answering a `kind` reference carry `outcome`?
// The one outcome set — recs/flow.go asks it before sending a rec's status as
// a verdict.
func CarriesOutcome(kind, outcome string) bool { return refOutcomes[outcome][kind] }

// refOutcomeWant: the reference kinds an outcome may be said about, for the
// error line ("ask or cal").
func refOutcomeWant(outcome string) string {
	var ks []string
	for k := range refOutcomes[outcome] {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, " or ")
}

func promptID() string { return store.NewID("p") }

// Queue records a prompt and delivers it if it is due now. A future one waits
// for DuePrompts (the scheduler tick).
func (m *Manager) Queue(p Prompt) (Prompt, error) {
	p.Author = strings.TrimSpace(p.Author)
	p.Target = strings.TrimSpace(p.Target)
	p.InReplyTo = strings.TrimSpace(p.InReplyTo)
	p.Outcome = strings.TrimSpace(p.Outcome)
	p.Repeat = strings.TrimSpace(p.Repeat)
	if p.Author == "" {
		return p, errors.New("author required")
	}
	if p.Target == "" {
		return p, errors.New("target required: a session id, 'new', or 'new-or:<id>'")
	}
	if p.Repeat != "" && cadence.Kind(p.Repeat) != "clock" {
		return p, fmt.Errorf("repeat must be daily@HH:MM | weekly@Mon HH:MM | every@<duration>, got %q", p.Repeat)
	}
	// A job's clock is a standing row the hub writes from ops/schedule.json
	// (sched.Reconcile); nobody else addresses a job, so the table stays the
	// one place a job is defined.
	if strings.HasPrefix(p.Target, "job:") && (p.Author != "hub" || p.Repeat == "") {
		return p, errors.New("job:<name> is a standing target the hub sets from the schedule table")
	}
	if err := p.normalizeReplies(); err != nil {
		return p, err
	}
	// A standing row may carry no words: a check-in with none says
	// defaultCheckIn when it fires, a job's prompt is in the table.
	if strings.TrimSpace(p.Text) == "" && !p.anyOutcome() && len(p.Attachments) == 0 && p.Repeat == "" {
		return p, errors.New("nothing to say: text, an outcome or attachments")
	}
	// An agent addresses itself or a new session; another session only through
	// a relay the owner approved (Relay).
	if strings.HasPrefix(p.Author, "claude:thread:") && p.relayed == "" {
		self := strings.TrimPrefix(p.Author, "claude:thread:")
		t := p.Target
		if strings.HasPrefix(t, "new-or:") {
			t = strings.TrimPrefix(t, "new-or:")
		}
		if t != "new" && t != self {
			return p, errors.New("a session may only prompt itself or a new session; to hand a task to another session: `lifectl relay <session-id> \"…\"` (the owner approves the card, the hub delivers)")
		}
	}
	if p.Attachments == nil {
		p.Attachments = []string{}
	}
	if p.Repeat != "" && p.NotBefore.IsZero() {
		p.NotBefore = cadence.Next(p.Repeat, time.Time{}, time.Now())
	}
	if err := m.insertPrompt(&p, "queued", ""); err != nil {
		return p, err
	}
	if p.Repeat != "" {
		// A standing row never fires on insert, even when its first occurrence
		// is now: the tick fires it, and only the tick, so there is one path.
		log.Printf("prompt %s: standing %s for %s, next %s", p.ID, p.Repeat, p.Target, p.NotBefore.Format(time.RFC3339))
		return p, nil
	}
	// A READ card is answered by reading it: however the owner responds
	// THROUGH it — the one-tap close, or words — the card closes. And a
	// response with no words has nothing for the session to act on, so it is
	// recorded here and goes no further rather than spinning the session up.
	// The row is still written: it is the record of what they said, when, and
	// that it was deliberately not delivered.
	//
	// With SEVERAL cards on one message, each is closed by its
	// own rule below; the wordless short-circuit holds only when every one of
	// them was a read — a wordless "Accept" on a rec is work for the session.
	if p.Author == "owner" {
		wordless := strings.TrimSpace(p.Text) == "" && len(p.Attachments) == 0
		allRead := len(p.Replies) > 0
		for _, r := range p.Replies {
			kind, id := store.SplitRef(r.Ref)
			isRead := false
			if kind == "ask" && r.Outcome != "wont" {
				if a, err := m.GetAsk(id); err == nil && a.Kind == "read" {
					m.ClaimAsk(id, "done", firstLine(p.Text, 200))
					isRead = true
				}
			}
			// quiet: this reply, wordless, gives the session nothing to act on.
			quiet := isRead
			// "The owner's task is done here" is data on the prompt, so the
			// card leaves their board the moment they say it — even when the words are
			// aimed at a session tomorrow morning. The agent still judges the
			// rest of the thread.
			switch {
			case isRead:
			case kind == "ask" && (r.Outcome == "done" || r.Outcome == "wont"):
				m.ClaimAsk(id, r.Outcome, firstLine(p.Text, 200))
			case kind == "cal" && (r.Outcome == "done" || r.Outcome == "wont"):
				// Their words are the record on the item (the whole note, as a
				// rec's decision_note is), and the item's ask closes with it.
				if m.ClaimCal != nil {
					m.ClaimCal(id, r.Outcome, p.Text)
				}
			case kind == "rec" && (r.Outcome == "accepted" || r.Outcome == "declined"):
				// A verdict on a rec is recorded on the ledger row the moment
				// it is given, wherever the words are going (the Recs page's
				// own decide does the same and then queues this prompt — the
				// hook is a no-op for a row already in that state).
				if m.DecideRec != nil {
					if err := m.DecideRec(id, r.Outcome, p.Text); err != nil {
						log.Printf("prompt %s: rec %s %s: %v", p.ID, id, r.Outcome, err)
					}
				}
			case kind == "action" && (r.Outcome == "approved" || r.Outcome == "denied"):
				// The proposal is decided on its own row the moment it is said
				// (an approved one starts running); this prompt is the relay
				// to the session, so the queue posts none of its own.
				if m.DecideAction != nil {
					if err := m.DecideAction(id, r.Outcome, p.Text, p.Via); err != nil {
						log.Printf("prompt %s: action %s %s: %v", p.ID, id, r.Outcome, err)
					} else if r.Outcome == "approved" && m.ActionExec != nil && m.ActionExec(id) == "relay" {
						// The hub carries an approved relay out itself: a bare
						// Approve leaves the proposer nothing to do (relay.go).
						quiet = true
					}
				}
			}
			if !quiet {
				allRead = false
			}
		}
		if wordless && allRead {
			const why = "read card closed; no words to deliver"
			m.db.Exec(`UPDATE prompts SET state='cancelled', error=? WHERE id=?`, why, p.ID)
			p.State, p.Error = "cancelled", why
			return p, nil
		}
	}
	if p.NotBefore.After(time.Now()) {
		log.Printf("prompt %s: queued for %s (%s → %s)", p.ID, p.NotBefore.Format(time.RFC3339), p.Author, p.Target)
		return p, nil
	}
	return m.sendPrompt(p)
}

// insertPrompt writes the row, minting id/created_at when the caller has not,
// in the given state (a child that was refused is born failed).
func (m *Manager) insertPrompt(p *Prompt, state, errText string) error {
	if p.Attachments == nil {
		p.Attachments = []string{}
	}
	att, _ := json.Marshal(p.Attachments)
	if p.ID == "" {
		p.ID = promptID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	p.State, p.Error = state, errText
	nb := ""
	if !p.NotBefore.IsZero() {
		nb = ts(p.NotBefore)
	}
	_, err := m.db.Exec(`INSERT INTO prompts (id, created_at, author, target, not_before, in_reply_to, outcome, replies, text, attachments, title, goal_id, repeat, parent, state, error)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, ts(p.CreatedAt), p.Author, p.Target, nb, p.InReplyTo, p.Outcome, encodeReplies(p.Replies), p.Text, string(att), p.Title, p.GoalID, p.Repeat, p.Parent, state, errText)
	return err
}

// anyOutcome: at least one card on this prompt gets a verdict.
func (p Prompt) anyOutcome() bool {
	for _, r := range p.Replies {
		if r.Outcome != "" {
			return true
		}
	}
	return p.Outcome != ""
}

// GetPrompt reads one back.
func (m *Manager) GetPrompt(id string) (Prompt, error) {
	row := m.db.QueryRow(`SELECT `+promptCols+` FROM prompts WHERE id=?`, id)
	return scanPrompt(row)
}

const promptCols = `id, created_at, author, target, not_before, in_reply_to, outcome, replies, text, attachments, title, goal_id, repeat, parent, state, delivered_at, delivered_thread, error`

func scanPrompt(s scanner) (Prompt, error) {
	var p Prompt
	var created, nb, att, replies string
	var delivered *string
	if err := s.Scan(&p.ID, &created, &p.Author, &p.Target, &nb, &p.InReplyTo, &p.Outcome, &replies, &p.Text, &att,
		&p.Title, &p.GoalID, &p.Repeat, &p.Parent, &p.State, &delivered, &p.DeliveredThread, &p.Error); err != nil {
		return p, err
	}
	p.Replies = decodeReplies(replies, p.InReplyTo, p.Outcome)
	p.CreatedAt = parseTS(created)
	if nb != "" {
		p.NotBefore = parseTS(nb)
	}
	if delivered != nil && *delivered != "" {
		d := parseTS(*delivered)
		p.DeliveredAt = &d
	}
	json.Unmarshal([]byte(att), &p.Attachments)
	if p.Attachments == nil {
		p.Attachments = []string{}
	}
	return p, nil
}

// ListPrompts: state "" = all, "queued" = the ones still to come. threadID
// filters on the target (a session's own future).
func (m *Manager) ListPrompts(state, threadID string, limit int) ([]Prompt, error) {
	where, args := []string{"1=1"}, []any{}
	if state != "" {
		where = append(where, "state=?")
		args = append(args, state)
	}
	if threadID != "" {
		where = append(where, "(target=? OR target=? OR delivered_thread=?)")
		args = append(args, threadID, "new-or:"+threadID, threadID)
	}
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := m.db.Query(`SELECT `+promptCols+` FROM prompts WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Prompt{}
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CancelPrompt drops one that has not been delivered. Cancelling a session's
// standing row IS "schedule off", so the conversation gets the same card a
// PATCH would have posted.
func (m *Manager) CancelPrompt(id string) (Prompt, error) {
	p, err := m.GetPrompt(id)
	if err != nil {
		return p, errors.New("no such prompt")
	}
	if p.State != "queued" {
		return p, errors.New("prompt is already " + p.State)
	}
	m.db.Exec(`UPDATE prompts SET state='cancelled' WHERE id=?`, id)
	if p.Repeat != "" && !strings.HasPrefix(p.Target, "job:") {
		if _, err := m.Get(p.Target); err == nil {
			m.postScheduleCard(p.Target, "", "")
		}
	}
	return m.GetPrompt(id)
}

// SetStanding makes `repeat` the one standing row for target (author hub for
// a session's check-in or a job's clock; a session may also set its own).
// Any earlier standing row for the target is cancelled; repeat ” is "off".
// `last` is when the target last ran, so every@ counts from it rather than
// firing at the next tick.
func (m *Manager) SetStanding(author, target, repeat, text string, last time.Time) error {
	if repeat != "" && cadence.Kind(repeat) != "clock" {
		return fmt.Errorf("repeat must be daily@HH:MM | weekly@Mon HH:MM | every@<duration>, got %q", repeat)
	}
	m.db.Exec(`UPDATE prompts SET state='cancelled' WHERE target=? AND state='queued' AND repeat!=''`, target)
	if repeat == "" {
		return nil
	}
	_, err := m.Queue(Prompt{Author: author, Target: target, Repeat: repeat, Text: text, NotBefore: cadence.Next(repeat, last, time.Now())})
	return err
}

// Standing is the target's standing row, if it has one.
func (m *Manager) Standing(target string) (Prompt, bool) {
	row := m.db.QueryRow(`SELECT `+promptCols+` FROM prompts WHERE target=? AND state='queued' AND repeat!='' ORDER BY created_at DESC LIMIT 1`, target)
	p, err := scanPrompt(row)
	return p, err == nil
}

// StandingRepeat answers sched.Clock: the cadence on target's standing row.
func (m *Manager) StandingRepeat(target string) (string, bool) {
	p, ok := m.Standing(target)
	return p.Repeat, ok
}

// StandingTargets lists every target with a standing row under prefix
// ("job:" = the jobs the clock knows), so sched.Reconcile can drop the ones
// the table no longer has.
func (m *Manager) StandingTargets(prefix string) []string {
	rows, err := m.db.Query(`SELECT DISTINCT target FROM prompts WHERE state='queued' AND repeat!='' AND target LIKE ? ORDER BY target`, prefix+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

// hasStanding: does this session wake again on its own?
func (m *Manager) hasStanding(threadID string) bool {
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM prompts WHERE target=? AND state='queued' AND repeat!=''`, threadID).Scan(&n)
	return n > 0
}

// DuePrompts is the hub's ONE wake: every queued prompt whose time has come —
// a one-shot is delivered, a standing row fires a child and advances. Called
// from the minute tick.
func (m *Manager) DuePrompts(now time.Time) {
	rows, err := m.db.Query(`SELECT `+promptCols+` FROM prompts WHERE state='queued' AND not_before!='' AND not_before<=? ORDER BY not_before`, ts(now))
	if err != nil {
		return
	}
	var due []Prompt
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err == nil {
			due = append(due, p)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("prompts: due: %v", err) // fire what was read; the rest is due next minute
	}
	rows.Close()
	for _, p := range due {
		if p.Repeat != "" {
			m.fireStanding(p, now)
			continue
		}
		// A calendar agent item's wake obeys the daily budget guard the way
		// its tick always did: refused, it stays queued and the next tick asks
		// again, so it runs once the guard lifts (midnight ET, `lifectl budget
		// clear`) rather than being skipped.
		if kind, id := store.SplitRef(p.InReplyTo); kind == "cal" {
			// An edit to the item requeues its wake — cancelling this row and
			// delivering the new one on the spot. Sending the stale copy too
			// would put one item in two sessions.
			if cur, err := m.GetPrompt(p.ID); err != nil || cur.State != "queued" {
				continue
			}
			if m.Allow != nil {
				if ok, why := m.Allow("cal", strings.TrimPrefix(p.Target, "new-or:")); !ok {
					if !m.held[id] {
						log.Printf("calendar: %s held by the hub: %s", id, why)
						m.held[id] = true
					}
					continue
				}
				delete(m.held, id)
			}
		}
		if _, err := m.sendPrompt(p); err != nil {
			log.Printf("prompt %s: %v", p.ID, err)
		}
	}
}

// fireStanding is one occurrence of a standing row: a child row records what
// happened (delivered, or failed with why), and the parent moves to its next
// occurrence. Occurrences missed while the hub was down fire ONCE — Next is
// computed from now, not from the missed time. A target that is busy (a
// session mid-turn, a job still running or gated shut) HOLDS: nothing is
// written and the same tick comes back next minute.
func (m *Manager) fireStanding(p Prompt, now time.Time) {
	child := Prompt{Author: p.Author, Target: p.Target, Parent: p.ID, Text: p.Text, Title: p.Title, GoalID: p.GoalID,
		InReplyTo: p.InReplyTo, Outcome: p.Outcome, Replies: p.Replies, NotBefore: now, Attachments: []string{}}
	// claim moves the parent to its next occurrence BEFORE anything fires, and
	// only if it is still queued and due: the occurrence belongs to whoever
	// moved it. Advancing after the send with the error dropped meant a failed
	// write left the row due, and the next minute fired it again. undo puts a
	// claimed occurrence back when the target turns out to be busy (a hold).
	claim := func() (ok bool, undo func()) {
		next := cadence.Next(p.Repeat, now, now)
		var res sql.Result
		var err error
		if next.IsZero() {
			res, err = m.db.Exec(`UPDATE prompts SET state='failed', error=? WHERE id=? AND state='queued' AND not_before<=?`, "cadence "+p.Repeat+" has no next occurrence", p.ID, ts(now))
			undo = func() {
				m.db.Exec(`UPDATE prompts SET state='queued', error='' WHERE id=? AND state='failed'`, p.ID)
			}
		} else {
			res, err = m.db.Exec(`UPDATE prompts SET not_before=? WHERE id=? AND state='queued' AND not_before<=?`, ts(next), p.ID, ts(now))
			undo = func() {
				if _, err := m.db.Exec(`UPDATE prompts SET not_before=? WHERE id=? AND state='queued' AND not_before=?`, ts(p.NotBefore), p.ID, ts(next)); err != nil {
					log.Printf("prompt %s: put back held occurrence: %v", p.ID, err)
				}
			}
		}
		if err != nil {
			log.Printf("prompt %s: advance: %v", p.ID, err)
			return false, nil
		}
		n, _ := res.RowsAffected()
		return n == 1, undo
	}
	if name, ok := strings.CutPrefix(p.Target, "job:"); ok {
		if m.RunJob == nil {
			return // no job runner wired (tests): hold
		}
		if m.JobHeld != nil {
			if held, _ := m.JobHeld(name); held {
				return
			}
		}
		ok, undo := claim()
		if !ok {
			return
		}
		child.ID = promptID()
		switch state, why := m.RunJob(name, child.ID); state {
		case "held":
			undo()
		case "started":
			m.insertPrompt(&child, "delivered", "")
			m.db.Exec(`UPDATE prompts SET delivered_at=? WHERE id=?`, ts(now), child.ID)
		case "skipped":
			m.insertPrompt(&child, "failed", "skipped by the hub: "+why)
		default: // gone: the table no longer has this job; Reconcile normally beats us to it
			m.db.Exec(`UPDATE prompts SET state='cancelled', error=? WHERE id=? AND state IN ('queued','failed')`, why, p.ID)
		}
		return
	}
	t, err := m.Get(p.Target)
	if err != nil || t.Status == "archived" {
		m.db.Exec(`UPDATE prompts SET state='cancelled', error=? WHERE id=? AND state='queued'`, "session "+p.Target+" is gone", p.ID)
		return
	}
	if t.Status == "running" {
		return
	}
	if m.Allow != nil {
		if ok, why := m.Allow("checkin", p.Target); !ok {
			// Skipped, not deferred: the child says why, the session sees it,
			// and the next occurrence is due on the cadence as usual.
			if ok, _ := claim(); !ok {
				return
			}
			log.Printf("thread %s check-in skipped: %s", p.Target, why)
			m.insertPrompt(&child, "failed", "skipped by the hub: "+why)
			m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES (?,?,?,?,?)`, p.Target, ts(now), "system", "message", "Check-in skipped by the hub: "+why)
			return
		}
	}
	if strings.TrimSpace(child.Text) == "" {
		child.Text = defaultCheckIn
	}
	ok, undo := claim()
	if !ok {
		return
	}
	if err := m.insertPrompt(&child, "queued", ""); err != nil {
		log.Printf("prompt %s: child: %v", p.ID, err)
		undo() // nothing was sent: the same occurrence comes back next minute
		return
	}
	if _, err := m.sendPrompt(child); err != nil {
		log.Printf("thread %s check-in: %v", p.Target, err)
	}
}

// sendPrompt hands one to a session: the named one if it is still there,
// otherwise (target new / new-or) a fresh one carrying the same text.
func (m *Manager) sendPrompt(p Prompt) (Prompt, error) {
	fail := func(err error) (Prompt, error) {
		m.db.Exec(`UPDATE prompts SET state='failed', error=? WHERE id=?`, err.Error(), p.ID)
		p.State, p.Error = "failed", err.Error()
		return p, err
	}
	threadID, err := m.Reach(p.Target)
	if err != nil {
		return fail(err)
	}
	newOnly := threadID == ""
	role, kind := p.role(), p.kind()
	if kind == "checkin" {
		// Delivered with what changed since it was written; the stored message
		// carries it too, so the thread shows what the session was told.
		if block := m.changedSince(p, threadID, newOnly); block != "" {
			p.Text = block + "\n\n" + p.Text
		}
	}
	if newOnly {
		title := p.Title
		if title == "" {
			title = firstLine(p.Text, 80)
		}
		t, err := m.CreateBy(p.Author, title, "life", p.GoalID, p.renderForNew(m), "", "", p.Attachments)
		if err != nil {
			return fail(err)
		}
		threadID = t.ID
	} else if err := m.runRef(threadID, p.Author, role, kind, p.Text, p.Attachments, p.Replies, ""); err != nil {
		return fail(err)
	}
	m.db.Exec(`UPDATE prompts SET state='delivered', delivered_at=?, delivered_thread=? WHERE id=?`, ts(time.Now()), threadID, p.ID)
	p.State, p.DeliveredThread = "delivered", threadID
	now := time.Now()
	p.DeliveredAt = &now
	if m.OnDelivered != nil {
		m.OnDelivered(p, threadID)
	}
	return p, nil
}

// Reach is the ONE meaning of a delivery target (review-primitives step 6):
// "<id>" is that session or an error; "new-or:<id>" is that session if it is
// still there and not archived — it already knows the argument — else a new
// one; "new" is always a new one. threadID "" = start a new session. A
// prompt's target and deliver.ToSession (a rec decision, an approved
// proposal) both ask it.
func (m *Manager) Reach(target string) (threadID string, err error) {
	if target == "new" {
		return "", nil
	}
	id, orNew := strings.CutPrefix(target, "new-or:")
	t, err := m.Get(id)
	if err == nil && t.Status != "archived" {
		return id, nil
	}
	// A session that is gone (or ended) does not block the message when its
	// author allowed a new one; a plain target is an error.
	if orNew {
		return "", nil
	}
	return "", fmt.Errorf("session %s: %v", id, errOr(err, errors.New("archived")))
}

func errOr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// role/kind: how the delivered message is stored, so both surfaces colour it
// the way they always have. The owner answering a card is a "decision"; their
// words are a "message"; an agent's own later prompt is a check-in.
func (p Prompt) role() string {
	if p.Author == "owner" {
		return "owner"
	}
	return "system"
}

func (p Prompt) kind() string {
	switch {
	case p.Author == "owner" && p.InReplyTo != "":
		return "decision"
	case p.Author == "owner":
		return "message"
	case p.relayed != "":
		// Another session's words on the owner's approval (relay.go): a plain system
		// message on both surfaces, and the target keeps its full memory (an
		// unknown trigger resolves as `message` in the model policy).
		return "relay"
	default:
		return "checkin"
	}
}

// renderForNew: the opening message of a session a prompt starts. A fresh
// session has no memory of the referenced thing, so the reference is spelled
// out here instead of framed at wake time.
func (p Prompt) renderForNew(m *Manager) string {
	head := m.refHeaders(p.Replies, p.kind())
	if head == "" {
		return p.Text
	}
	return head + "\n" + p.Text
}

// refHeaders frames every card a message answers. One card is refHeader as
// it always was; several are each framed in turn under one line saying so —
// the words that follow the frames are the owner's answer to ALL of them at
// once, and nothing unnamed is touched.
func (m *Manager) refHeaders(rs []Reply, msgKind string) string {
	if len(rs) == 0 {
		return ""
	}
	if len(rs) == 1 {
		return m.refHeader(rs[0].Ref, rs[0].Outcome, msgKind)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[The owner answered %d cards with ONE message. Each is framed below; their words after the frames are their answer to all of them together. Anything not named here is untouched: judge it on its own.]\n", len(rs))
	for _, r := range rs {
		if h := m.refHeader(r.Ref, r.Outcome, msgKind); h != "" {
			b.WriteString(h)
		}
	}
	return b.String()
}

// refHeader: the bracketed line a session sees above a prompt that answers
// something, rendered FROM THE REFERENCED ROW. No regex, no guessing — and it
// never speaks for anything but the one thing referenced. kind is the
// message's (Prompt.kind): "decision" = the owner answering, else the hub — a
// `cal:` reference is a step of theirs coming due when the hub says it and
// their answer to that step when they do.
// msgKind is the MESSAGE's kind, refKind the REFERENCE's. Keep them distinct:
// when one shadowed the other, the cal-decision branch — CalHeader included —
// was unreachable, and every answer to a calendar step (even "I deleted
// this") reached the session as "coming due. Do it now, end to end".
func (m *Manager) refHeader(ref, outcome, msgKind string) string {
	refKind, id := store.SplitRef(ref)
	if refKind == "" {
		return ""
	}
	switch refKind {
	case "ask":
		a, err := m.GetAsk(id)
		if err != nil {
			return fmt.Sprintf("[The owner responded to %s — the ask is no longer on file. Treat their words below on their own.]\n", id)
		}
		verdict := "wrote back about it"
		switch outcome {
		case "done":
			verdict = "marked it DONE (already closed by the hub)"
		case "wont":
			verdict = "marked it WON'T DO — they are not going to do this; drop it or find another way"
		}
		return fmt.Sprintf("[The owner responded to ONE ask — %s %q (%s) — and %s. This says nothing about any other ask on this thread: judge each of those on its own and leave open anything their words do not resolve. Anything below is theirs; act on it.]\n",
			a.ID, a.Title, a.Kind, verdict)
	case "action":
		verdict := "decided on"
		switch outcome {
		case "approved":
			verdict = "APPROVED"
		case "denied":
			verdict = "DENIED"
		}
		which := id
		if m.ActionTitle != nil {
			if t := m.ActionTitle(id); t != "" {
				which = fmt.Sprintf("%s %q", id, t)
			}
		}
		what := "Carry out exactly that action now — nothing more — and report the outcome briefly."
		if m.ActionExec != nil && m.ActionExec(id) == "relay" {
			what = "The hub delivers it to that session itself: send nothing, and end with `[end]` unless their words below ask for more."
		}
		if outcome == "denied" {
			what = "Do not do it; adjust or drop the plan."
		}
		return fmt.Sprintf("[The owner %s the action you proposed — %s. %s Any words below may narrow or change what to do: follow them.]\n", verdict, which, what)
	case "rec":
		// The frame is the rec package's (RelayHeader / ReplyRelayHeader —
		// verdict, cost, what it means for the work, the links' state today),
		// looked up by the hook so this package never reads its table.
		if m.RecHeader != nil {
			if h := m.RecHeader(id, outcome); h != "" {
				return h
			}
		}
		verdict := "replied to"
		switch outcome {
		case "accepted":
			verdict = "ACCEPTED"
		case "declined":
			verdict = "DECLINED"
		case "deferred":
			verdict = "DEFERRED"
		}
		return fmt.Sprintf("[The owner %s recommendation %s from the ledger. Their words follow — this is their answer, not a new task.]\n", verdict, id)
	case "cal":
		if msgKind == "decision" {
			if m.CalHeader != nil {
				if h := m.CalHeader(id, outcome); h != "" {
					return h
				}
			}
			verdict := "wrote back about"
			switch outcome {
			case "done":
				verdict = "marked DONE (already closed by the hub)"
			case "wont":
				verdict = "marked WON'T DO (already closed by the hub) — they are not doing this; drop it or find another way"
			}
			return fmt.Sprintf("[The owner %s calendar step %s. Their words follow — this is their answer about that one step, not a new task.]\n", verdict, id)
		}
		return fmt.Sprintf("[This is calendar item %s coming due. Do it now, end to end; raise an ask only if you are blocked or the owner must read a result.]\n", id)
	case "message":
		return fmt.Sprintf("[This answers message %s earlier in this session.]\n", id)
	}
	return ""
}
