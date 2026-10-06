package threads

// Runner: how a thread talks to Claude.
//
// One claude process per thread (a tmux session, so it survives hub
// restarts), started in stream-json input mode with its stdin fed from a
// file the hub appends to (`lifectl feed <in>`). The owner's messages are
// appended to that file the moment they arrive:
//
//   - while a turn is in flight claude picks the message up at its next tool
//     boundary (mid-turn steering: verified with cmd/steerprobe);
//   - between turns it starts the next turn in the same process (no resume
//     cost).
//
// Each `result` line claude prints closes one turn: the reply is stored, asks
// settle, status updates. The process stays alive for a while after its last
// turn so a follow-up is instant, then the hub writes an EOF sentinel, the
// feeder exits, claude exits, and the next message starts a fresh process
// with `--resume`. A run row (thread_runs) = one process; turn ids
// (<run>, <run>-t2, …) link messages to the events they produced.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"life/hub/internal/spend"
	"life/hub/internal/store"
)

// eofSentinel, written to the in-file, makes `lifectl feed` close claude's
// stdin (it is never forwarded).
const eofSentinel = `{"type":"eof"}`

const (
	// idleTTL: how long a process with no turn in flight is kept alive.
	idleTTL = 15 * time.Minute
	// stuckTTL: a turn that produced a result, then nothing, for this long
	// after a steered message was assumed still pending is treated as done
	// (the message was folded into the turn that just ended).
	stuckTTL = 3 * time.Minute
	// holdTTL: a result folded because a background task was still pending
	// (heldResult) becomes the reply anyway after this long with no output —
	// the task's notification never came, and the text should not be lost.
	holdTTL = 15 * time.Minute
)

type run struct {
	id, thread, out, in, trigger string
	model, effort                string
	off                          int64
	busy, eof, turns             int
	lastBoundary, lastOutput     time.Time
	lastResult                   time.Time
	costSeen                     float64
	// liveMsg: ids of the recent assistant messages whose usage was counted
	// into the run's live token total, space-separated, newest last. The
	// stream repeats one message's usage once per content block (thinking,
	// then text), and parallel subagents interleave their blocks, so one id
	// recurs non-consecutively — remembering only the last id counted a
	// subagent-heavy turn at ~2.5x its bill.
	liveMsg string
	// tasks: background tasks the CLI says are pending (its
	// background_tasks_changed events). held: JSON of a heldResult — the
	// last result envelope that ended a turn while tasks were pending, kept
	// out of thread_messages until the process's real last word (see
	// heldResult).
	tasks int
	held  string
}

// heldResult is a result envelope the hub folded instead of storing as a
// reply: progress prose ("waiting for the build…") must not become a stack
// of reply bubbles above one "turn ended" line. When a session leaves a background task running
// (background Bash, Monitor, a subagent) and ends its completion on prose,
// `claude -p` closes the turn with a result line, keeps the process alive,
// and re-enters a NEW turn when the task notifies — so one piece of work
// produced a result envelope per wait, and the hub stored each as a reply.
// Now a result that lands while bg_tasks > 0 is held: its text stays a
// `text` event in the fold (it was streamed there already), its usage is
// carried forward, and the next result on the process supersedes it. Only
// the result that lands with no task pending — or the held one, when the
// process exits or goes quiet for holdTTL — becomes the reply, with the
// whole span's cost and tokens on it.
type heldResult struct {
	Env resultEnvelope `json:"env"`
	Tok Tokens         `json:"tok"` // usage of every folded sub-turn, this one included
	At  time.Time      `json:"at"`  // when the hub saw the result line
	// Turn: the turn the folded result ended (`r.turnID()` at the hold). A
	// held reply is written LATER — when the owner's next message opens a turn
	// of its own — and by then the run's turn counter has moved on; stamping
	// the reply with the current turn put it under the NEW turn's message, so
	// its read card drew below every tool call that message went on to make.
	// The read card sticks where it appears in the tool chain.
	Turn string `json:"turn,omitempty"`
}

// heldTurn: the turn a folded result ended. A held row from before `Turn`
// was stamped falls back to the turn before the one a message opened after
// the hold; a steered message never opens one, so that case stays put.
func (m *Manager) heldTurn(r run, h heldResult) string {
	if h.Turn != "" {
		return h.Turn
	}
	if r.turns > 1 {
		var opened int
		m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE run_id=? AND role!='claude' AND delivered_at > ?
			AND id=(SELECT MIN(id) FROM thread_messages WHERE run_id=? AND role!='claude')`, r.turnID(), ts(h.At), r.turnID()).Scan(&opened)
		if opened > 0 {
			prev := r
			prev.turns--
			return prev.turnID()
		}
	}
	return r.turnID()
}

// backfillHeldReplyTurn repairs reply rows written before heldResult.Turn:
// a claude row stamped with a turn that a message OPENED before it (the row
// sits after that message and the turn's tool calls come after the row),
// whose previous turn has no reply of its own, is that previous turn's
// reply. Two such rows can chain (each turn's reply stamped one turn late),
// so it repeats until nothing moves. Idempotent: a repaired row's old turn
// then has its own reply, or is opened by no message.
func (m *Manager) backfillHeldReplyTurn() {
	const prev = `CASE WHEN CAST(substr(run_id, instr(run_id, '-t') + 2) AS INTEGER) = 2
		THEN substr(run_id, 1, instr(run_id, '-t') - 1)
		ELSE substr(run_id, 1, instr(run_id, '-t') + 1) || (CAST(substr(run_id, instr(run_id, '-t') + 2) AS INTEGER) - 1) END`
	for i := 0; i < 10; i++ {
		res, err := m.db.Exec(`UPDATE thread_messages SET run_id = ` + prev + ` WHERE id IN (
			SELECT c.id FROM thread_messages c
			WHERE c.role = 'claude' AND c.run_id LIKE '%-t%'
			AND EXISTS (SELECT 1 FROM thread_events e WHERE e.thread_id = c.thread_id AND e.run_id = c.run_id AND e.kind = 'tool_use' AND e.ts > c.ts)
			AND EXISTS (SELECT 1 FROM thread_messages u WHERE u.thread_id = c.thread_id AND u.run_id = c.run_id AND u.role != 'claude' AND u.ts < c.ts)
			AND NOT EXISTS (SELECT 1 FROM thread_messages p WHERE p.thread_id = c.thread_id AND p.role = 'claude'
				AND p.run_id = (SELECT ` + prev + ` FROM thread_messages x WHERE x.id = c.id)))`)
		if err != nil {
			log.Printf("threads: held reply turn backfill: %v", err)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return
		}
		log.Printf("threads: %d reply row(s) moved to the turn they ended", n)
	}
}

// heldResult decodes the run's held envelope, if any.
func (r run) heldResult() (heldResult, bool) {
	var h heldResult
	if r.held == "" || json.Unmarshal([]byte(r.held), &h) != nil {
		return heldResult{}, false
	}
	return h, true
}

func addTokens(a, b Tokens) Tokens {
	t := Tokens{In: a.In + b.In, Out: a.Out + b.Out, CacheRead: a.CacheRead + b.CacheRead, CacheWrite: a.CacheWrite + b.CacheWrite}
	t.sum()
	return t
}

func (r run) turnID() string {
	if r.turns <= 1 {
		return r.id
	}
	return fmt.Sprintf("%s-t%d", r.id, r.turns)
}

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (m *Manager) liveRun(threadID string) (run, bool) {
	row := m.db.QueryRow(`SELECT `+runCols+` FROM thread_runs WHERE thread_id=? AND finished_at IS NULL ORDER BY started_at DESC LIMIT 1`, threadID)
	r, err := scanRun(row)
	return r, err == nil
}

// LiveRun is the id of the run this thread has in flight, or "" when nothing
// is running. It is what a card raised mid-turn is tied to when whoever raised
// it could not name the run itself (server.addAsk).
func (m *Manager) LiveRun(threadID string) string {
	if r, ok := m.liveRun(threadID); ok {
		return r.id
	}
	return ""
}

// RunningThreads is the set of session ids with a turn in flight right now —
// one query for a whole list, so a page of recs can wear a "running" pill per
// card (recs.Rec.ThreadRunning) without asking per row.
func (m *Manager) RunningThreads() map[string]bool {
	out := map[string]bool{}
	rows, err := m.db.Query(`SELECT id FROM threads WHERE status='running'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// LiveModel is the model id the session is running on right now — the live
// run's --model, else the newest run that named one — so a rec filed from a
// session can be stamped with the model that wrote it without the CLI
// knowing (recs.Rec.Model). Empty when the thread has never run with an
// explicit model.
func (m *Manager) LiveModel(threadID string) string {
	if r, ok := m.liveRun(threadID); ok && r.model != "" {
		return r.model
	}
	var model string
	m.db.QueryRow(`SELECT model FROM thread_runs WHERE thread_id=? AND model!='' ORDER BY started_at DESC LIMIT 1`, threadID).Scan(&model)
	return model
}

// Models is LiveModel for every thread at once — the model each session is
// running on (a live run first, else its newest run that named one), keyed by
// thread id. One query, so the sessions list can print it on every card
// without a lookup per row. Threads that never ran with an
// explicit model are absent.
func (m *Manager) Models() map[string]string {
	out := map[string]string{}
	// Oldest first, live runs last: the last write per thread wins, so a live
	// run beats a finished one and a newer run beats an older.
	rows, err := m.db.Query(`SELECT thread_id, model FROM thread_runs WHERE model!='' ORDER BY (finished_at IS NULL), started_at`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, model string
		if rows.Scan(&id, &model) == nil {
			out[id] = model
		}
	}
	return out
}

// runCols: what scanRun reads, in order.
const runCols = `id, thread_id, out_file, in_file, trigger, out_offset, busy, eof, turns, last_boundary, last_output, last_result, cost_seen, model, live_msg, effort, bg_tasks, held`

func scanRun(s scanner) (run, error) {
	var r run
	var lb, lo, lr string
	if err := s.Scan(&r.id, &r.thread, &r.out, &r.in, &r.trigger, &r.off, &r.busy, &r.eof, &r.turns, &lb, &lo, &lr, &r.costSeen, &r.model, &r.liveMsg, &r.effort, &r.tasks, &r.held); err != nil {
		return r, err
	}
	r.lastBoundary, r.lastOutput, r.lastResult = parseTS(lb), parseTS(lo), parseTS(lr)
	return r, nil
}

func (m *Manager) run(id, role, kind, text string, attachments []string) error {
	author := "hub"
	if role == "owner" {
		author = "owner"
	}
	return m.runRef(id, author, role, kind, text, attachments, nil, "")
}

// runRef is run with the thing this message answers carried as data
// (prompts.go): ref is "<type>:<id>", outcome is what the author claims
// happened to it. Both ride on the message row, so the wake framing is
// rendered from the referenced object and nothing has to read the owner's words to
// work out what they meant. via is the surface the message came through
// ("console" | ""): it rides on the row too, and the prompt renders a pointer
// to that surface's own log from it (consoleStamp).
func (m *Manager) runRef(id, author, role, kind, text string, attachments []string, replies []Reply, via string) error {
	// The old columns keep the first card, the new one keeps them all.
	var ref, outcome string
	if len(replies) > 0 {
		ref, outcome = replies[0].Ref, replies[0].Outcome
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	if t.Status == "archived" {
		return errors.New("thread is archived")
	}
	// Attachments: resolve every ref up front so a bad one fails the send
	// (nothing recorded).
	for _, ref := range attachments {
		if m.BlobPath == nil {
			return errors.New("attachments not supported")
		}
		if _, err := m.BlobPath(ref); err != nil {
			return fmt.Errorf("attachment %q: %w", ref, err)
		}
	}
	if attachments == nil {
		attachments = []string{}
	}
	attJSON, _ := json.Marshal(attachments)
	now := time.Now()
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, attachments, queued, in_reply_to, outcome, replies, author, via) VALUES (?,?,?,?,?,?,1,?,?,?,?,?)`, id, ts(now), role, kind, text, string(attJSON), ref, outcome, encodeReplies(replies), author, via)
	m.db.Exec(`UPDATE threads SET updated_at=? WHERE id=?`, ts(now), id)
	if role == "owner" {
		// The owner's OWN words mark the open asks here answered — still on
		// the board; the agent judges each one and closes what the message
		// resolved. A message that answers ONE thing (in_reply_to) touches only
		// that ask: otherwise marking one of two asks done could flip the other
		// to answered too.
		// Words with no outcome answer an ask without closing it (the ball is
		// back with the agent); an outcome is a close and was already recorded
		// by claimAsk when it was said.
		if kind == "message" {
			m.markAnswered(id, ref)
		}
		for _, r := range replies {
			if r.Outcome == "" && strings.HasPrefix(r.Ref, "ask:") {
				m.markAnswered(id, r.Ref)
			}
		}
		// Title the thread from the owner's words right away (not only after the
		// turn ends) so the Working card reads as the problem, not a preamble.
		go m.autoTitle(id)
	}
	if r, ok := m.liveRun(id); ok {
		if r.in != "" && r.eof == 0 {
			// An idle process on the wrong rung (a sonnet check-in's process,
			// now the owner is talking) is closed; the message stays queued and
			// finishProcess starts the next one on the right model.
			if r.busy == 0 && r.model != "" {
				if want := m.resolve(t, kind); want.Model != "" && want.Model != r.model {
					log.Printf("thread %s: %s wake wants %s, process is on %s: closing it", id, kind, want.Model, r.model)
					m.sendEOF(r)
					return nil
				}
			}
			// Live process: hand the message straight to claude's stdin — it
			// is steered into the current turn or starts the next one.
			return m.deliver(t, r)
		}
		if t.Status == "running" {
			// Legacy run (no stdin) or one already winding down: stays
			// queued and becomes the next turn when the process exits.
			return nil
		}
	}
	return m.launch(t)
}

type queuedMsg struct {
	id   int64
	kind string
	text string
	atts []string
	// replies: what this message answers ("ask:<id>" + "done", one per
	// card), carried from the prompt that delivered it.
	replies []Reply
	// ts + via: when it was sent and through which surface; a console message
	// gets a stamp pointing at the console's log around that minute.
	ts  time.Time
	via string
}

func (m *Manager) queued(threadID string) ([]queuedMsg, error) {
	rows, err := m.db.Query(`SELECT id, kind, text, attachments, in_reply_to, outcome, replies, ts, via FROM thread_messages WHERE thread_id=? AND queued=1 ORDER BY id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queuedMsg
	for rows.Next() {
		var x queuedMsg
		var att, ref, outcome, replies, sent string
		rows.Scan(&x.id, &x.kind, &x.text, &att, &ref, &outcome, &replies, &sent, &x.via)
		json.Unmarshal([]byte(att), &x.atts)
		x.replies = decodeReplies(replies, ref, outcome)
		x.ts = parseTS(sent)
		out = append(out, x)
	}
	return out, nil
}

// consoleStamp: the one line a message from the web console carries instead
// of its run-up. The console records itself (web/trail.js → console/trail and
// console/motion observations); the session that wants to know what the owner
// was looking at, highlighting or circling queries that log for the minute the
// message was sent. Nothing from the log is pasted here: the prompt costs one
// line, the history costs a command.
func consoleStamp(sent time.Time) string {
	// Local time: the hub, the console and trail.py all print the laptop's zone.
	local := sent.In(time.Local)
	at, day, zone := local.Format("15:04"), local.Format("2006-01-02"), local.Format("MST")
	return "[Sent from the web console at " + at + " " + zone + " on " + day + ". What the owner was doing there before this — pages, clicks, highlights, circles — is a log you query, not this prompt: `ops/py.sh trail.py at " + at + "` (the words, 10 minutes up to the message), `ops/py.sh trail.py motion --at " + at + "` (the mouse path, the 2 minutes before it; `--points` for every sample). The console microphone may have been on: the message holds only what they said while this box was on their screen, and everything they said on every page is `ops/py.sh trail.py said --at " + at + " --minutes 30` — read it when the message seems to start mid-thought. All take `--date " + day + "` if today has moved on.]"
}

// prompt renders queued messages as one user turn. midTurn=true when it is
// being steered into a turn already in flight.
func (m *Manager) prompt(t Thread, qs []queuedMsg, midTurn, fresh bool) (string, error) {
	var pieces []string
	for _, q := range qs {
		var b strings.Builder
		if q.kind == "checkin" && t.ClaudeSessionID != "" {
			b.WriteString("[scheduled check-in]\n")
		}
		// What this message answers is DATA on the row (prompts.go), so the
		// framing is rendered from the referenced ask/proposal — never from
		// the owner's words, and never about anything but the one thing named.
		if head := m.refHeaders(q.replies, q.kind); head != "" {
			b.WriteString(head)
		} else if q.kind == "decision" {
			b.WriteString(decisionHeader)
		}
		b.WriteString(q.text)
		if len(q.atts) > 0 {
			// Pictures and documents are told apart by the blob's extension
			// (PutBlob keeps the upload's): a shared PDF is read page by page,
			// not looked at as a picture.
			paths := make([]string, len(q.atts))
			// A document's copy under its own name in the intake in-box
			// (obs.PutIntake): the path the session works from, never a hash.
			intake := make([]string, len(q.atts))
			docs := 0
			for i, ref := range q.atts {
				p, err := m.BlobPath(ref)
				if err != nil {
					return "", fmt.Errorf("attachment %q: %w", ref, err)
				}
				paths[i] = p
				if !isImageBlob(ref) {
					docs++
					if m.IntakePath != nil {
						intake[i] = m.IntakePath(ref)
					}
				}
			}
			const intakeNote = "each file is also saved under its own name in data/imports/intake/, the in-box — work from that path, and rename it `loaded_…` once it is imported"
			switch {
			case docs == 0:
				fmt.Fprintf(&b, "\n\n[The owner attached %d image(s) from their phone — look at each with the Read tool before answering; they are also stored as app/photo observations (lifectl obs):", len(q.atts))
			case docs == len(q.atts):
				fmt.Fprintf(&b, "\n\n[The owner attached %d file(s) from their phone — read each with the Read tool before answering (a PDF: pages=\"1-5\" and on, up to 20 a call; %s); they are also stored as app/upload observations (lifectl obs):", len(q.atts), intakeNote)
			default:
				fmt.Fprintf(&b, "\n\n[The owner attached %d image(s) and %d file(s) from their phone — look at / read each with the Read tool before answering (a PDF: pages=\"1-5\" and on; %s); they are also stored as app/photo and app/upload observations (lifectl obs):", len(q.atts)-docs, docs, intakeNote)
			}
			for i, p := range paths {
				fmt.Fprintf(&b, "\n  %d. %s", i+1, p)
				if intake[i] != "" {
					fmt.Fprintf(&b, " — saved as %s", intake[i])
				}
			}
			b.WriteString("]")
			// A snap with no words typed is still wordless, whatever preamble
			// and place bullets the surface put above it (stripSnapPreamble).
			if stripSnapPreamble(q.text) == "" {
				what := "photo"
				if docs > 0 {
					what = "file"
				}
				fmt.Fprintf(&b, "\n\n[This message is a %s with NO instruction. That usually means the owner sent it by mistake or stopped typing — do not act on it and do not exit quietly: raise ONE ask (kind decision, title \"You sent a %s with no message — what did you want?\", detail = one line describing what the %s shows) so this thread surfaces on their board and they can pick it back up.]", what, what, what)
			}
		}
		if q.via == "console" && q.kind == "message" {
			sent := q.ts
			if sent.IsZero() {
				sent = time.Now()
			}
			b.WriteString("\n\n" + consoleStamp(sent))
		}
		pieces = append(pieces, b.String())
	}
	prompt := strings.Join(pieces, "\n\n---\n\n")
	if midTurn {
		head := "[The owner sent this while you are working on the current turn. Take it into account now — it may change what you should do; adjust course rather than finishing the old plan first.]\n\n"
		if len(pieces) > 1 {
			head = fmt.Sprintf("[The owner sent %d messages while you are working on the current turn, in this order. Take them into account now — they may change what you should do.]\n\n", len(pieces))
		}
		prompt = head + prompt
	}
	if fresh {
		pre := m.systemPreamble(t)
		if t.ClaudeSessionID != "" {
			pre += "\n\n" + m.recap(t) // a lean wake: no transcript, a recap instead
		}
		prompt = pre + "\n\n" + prompt
	} else if !midTurn {
		prompt += heardReminder
	}
	// Check-ins may be answering nothing; the owner's messages and decisions
	// usually resolve something — either way the agent sees what is pending.
	// A steer lands in a turn that already read who else is live and what is
	// open for the owner everywhere (boardHeader).
	peers := ""
	if !midTurn {
		peers = m.boardHeader(t.ID) + m.peersHeader(t.ID)
	}
	return m.asksHeader(t.ID) + peers + prompt, nil
}

// recapTurns / recapChars: how much of the conversation a lean wake is given.
// Six messages of ≤900 characters is a couple of thousand tokens; resuming
// the same thread was ~110k.
const (
	recapTurns = 6
	recapChars = 900
)

// recap: what a lean wake gets instead of the transcript — who this thread is
// for, that its long-term memory is the goal notes, and the last few turns in
// brief. Deliberately short: the point of the lean wake is not to re-read the
// conversation. Anything durable belongs in the goal notes, which the preamble
// above already tells the session to read and to write.
func (m *Manager) recap(t Thread) string {
	// A reply row is stored empty — its words are on the
	// card(s) it raised — so a session's own last turn is quoted from those.
	// One SQLite connection (store.Open): read the rows out before asking
	// for their cards, or the nested query waits on itself forever.
	type row struct {
		id               int64
		role, kind, text string
	}
	var recent []row
	rows, err := m.db.Query(`SELECT id, role, kind, text FROM thread_messages
		WHERE thread_id=? AND queued=0 AND (text<>'' OR role='claude') ORDER BY id DESC LIMIT ?`, t.ID, recapTurns*3)
	if err == nil {
		for rows.Next() {
			var x row
			rows.Scan(&x.id, &x.role, &x.kind, &x.text)
			recent = append(recent, x)
		}
		rows.Close()
	}
	var turns []string
	for _, x := range recent {
		if len(turns) >= recapTurns {
			break
		}
		who, text := "the owner", x.text
		switch {
		case x.kind == "checkin":
			who = "the hub (scheduled check-in)"
		case x.kind == "relay":
			who = "another session (the owner approved the hand-off)"
		case x.role == "claude":
			who = "you, last time"
			if text == "" {
				text = m.cardsOf(x.id)
			}
		}
		if text == "" {
			continue
		}
		turns = append([]string{"- " + who + ": " + strings.TrimSpace(truncate(text, recapChars))}, turns...)
	}
	b := &strings.Builder{}
	b.WriteString("[LEAN WAKE. This is a scheduled wake of an ongoing thread, and you are starting with a CLEAN context on purpose: resuming the whole conversation costs the owner more than the work itself. Ignore anything above about keeping full memory of this conversation — the hub keeps it, you do not. Your memory is: this thread's goal (read `lifectl goal <id>` first — the digest plus every decision newer than it — and write what you learn back), the files and data on this machine, and the recap below. The check-in text was written in the past: where the goal's newer notes or the live data contradict it, they win.")
	if len(turns) > 0 {
		fmt.Fprintf(b, "\n\nThe last %d messages of this thread, oldest first, truncated:\n%s", len(turns), strings.Join(turns, "\n"))
	}
	b.WriteString("\n\nDo not try to reconstruct the rest — if you truly need an older detail, it is in the goal notes or in the repo. Work the check-in below, then stop.]")
	return b.String()
}

// cardsOf: the cards a reply raised, as one line each — what the reply said,
// now that the row itself carries no words.
func (m *Manager) cardsOf(messageID int64) string {
	rows, err := m.db.Query(`SELECT title, detail, kind, said FROM items WHERE src='ask' AND message_id=? ORDER BY created_at`, messageID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var title, detail, kind, said string
		rows.Scan(&title, &detail, &kind, &said)
		line := title
		if msg := saidMessage(said); msg != "" {
			// The message is the card: the title is only its
			// first sentence, cut.
			line = msg
		}
		if detail != "" {
			line += " — " + detail
		}
		if kind != "read" {
			line = "[" + kind + " card] " + line
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n  ")
}

// saidMessage is a spoken line less its "Hey <name>, about <thread>." opener.
func saidMessage(said string) string {
	s := strings.TrimSpace(sayGreeting.ReplaceAllString(strings.TrimSpace(said), ""))
	if strings.HasPrefix(strings.ToLower(s), "about ") {
		if i := strings.Index(s, ". "); i > 0 {
			s = s[i+2:]
		}
	}
	return strings.TrimSpace(s)
}

func userLine(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	return string(b) + "\n"
}

// deliver appends the queued messages to a live process's stdin file.
// Caller holds m.mu.
func (m *Manager) deliver(t Thread, r run) error {
	qs, err := m.queued(t.ID)
	if err != nil || len(qs) == 0 {
		return err
	}
	midTurn := r.busy == 1
	p, err := m.prompt(t, qs, midTurn, false)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(r.in, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(userLine(p))
	f.Close()
	if err != nil {
		return err
	}
	now := time.Now()
	steered := 0
	if midTurn {
		steered = 1
	} else {
		// A new turn starts with this message: it is the boundary.
		r.turns++
		m.db.Exec(`UPDATE thread_runs SET turns=?, last_boundary=? WHERE id=?`, r.turns, ts(now), r.id)
	}
	for _, q := range qs {
		m.db.Exec(`UPDATE thread_messages SET queued=0, run_id=?, delivered_at=?, steered=? WHERE id=?`, r.turnID(), ts(now), steered, q.id)
	}
	m.db.Exec(`UPDATE thread_runs SET busy=1, last_output=? WHERE id=?`, ts(now), r.id)
	m.db.Exec(`UPDATE threads SET status='running', updated_at=?, last_run_at=? WHERE id=?`, ts(now), ts(now), t.ID)
	if midTurn {
		log.Printf("thread %s: %d message(s) steered into the running turn", t.ID, len(qs))
	} else {
		log.Printf("thread %s: next turn on the live process (turn %d)", t.ID, r.turns)
	}
	return nil
}

// launch starts a claude process for the thread with every queued message as
// its first turn. Caller holds m.mu.
func (m *Manager) launch(t Thread) error {
	dir, ok := m.ProjectDir(t.Project)
	if !ok {
		return fmt.Errorf("unknown project %q", t.Project)
	}
	qs, err := m.queued(t.ID)
	if err != nil || len(qs) == 0 {
		return err
	}
	// Model, effort, the CLI's own budget and whether this wake is LEAN come
	// from the policy for the kind of wake that starts this process
	// (spend/policy.go).
	res := m.resolve(t, qs[0].kind)
	// A lean wake starts a process with no transcript at all: the prompt
	// carries the preamble, the goal notes pointer and a recap instead of
	// `--resume` re-reading (and re-paying for) the whole conversation.
	lean := res.Fresh && t.ClaudeSessionID != ""
	prompt, err := m.prompt(t, qs, false, t.ClaudeSessionID == "" || lean)
	if err != nil {
		return err
	}
	now := time.Now()
	runID := store.NewID(now.Format("20060102-150405"))
	base := filepath.Join(m.RunsDir, t.ID+"-"+runID)
	if err := os.WriteFile(base+".prompt", []byte(prompt), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(base+".in", []byte(userLine(prompt)), 0o600); err != nil {
		return err
	}
	// stream-json both ways (needs --verbose in -p mode): one JSON line per
	// step out, so Poll can show tool calls / thinking live and settle each
	// turn on its result line; user messages in, so the owner can steer.
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits"}
	if len(m.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(m.AllowedTools, ","))
	}
	if t.ClaudeSessionID != "" && !lean {
		args = append(args, "--resume", t.ClaudeSessionID)
	}
	model := res.Model
	args = append(args, res.Args(false)...)
	// LIFE_THREAD_ID lets lifectl propose tag actions with this session, so
	// the owner's approve/deny (+ note) is routed back here as a message.
	// LIFE_RUN_ID lets `lifectl ask add` tie the ask to this process, so each
	// turn's asks get pointed at the reply they were raised in.
	// CLAUDE_CODE_AUTO_COMPACT_WINDOW caps context growth on these resumed
	// threads; it is set here rather than in ~/.claude/settings.json so the
	// owner's own interactive sessions keep the CLI default.
	// The CLI updates itself by reinstalling its npm package, and for the
	// seconds that takes the bin link is gone: a launch in that window died
	// with "bash: …/bin/claude: No such file or directory" and put an error
	// card on the board. Wait for the binary inside the tmux shell — never under
	// m.mu — and only then start; past 90 s the old error is the right one.
	cmd := waitForBin(m.ClaudeBin) + shellQuote(m.feedBin()) + " feed " + shellQuote(base+".in") + " | LIFE_THREAD_ID=" + shellQuote(t.ID) + " LIFE_RUN_ID=" + shellQuote(runID)
	if m.AutoCompactWindow > 0 {
		cmd += " CLAUDE_CODE_AUTO_COMPACT_WINDOW=" + strconv.Itoa(m.AutoCompactWindow)
	}
	cmd += " " + shellQuote(m.ClaudeBin)
	for _, a := range args {
		cmd += " " + shellQuote(a)
	}
	cmd += fmt.Sprintf(" > %s 2>%s.err; touch %s.done", shellQuote(base+".out"), shellQuote(base+".out"), shellQuote(base+".out"))
	tmuxName := fmt.Sprintf("%s-th-%s-%s", m.Prefix, t.ID, runID)
	if out, err := m.Run("tmux", "new-session", "-d", "-s", tmuxName, "-c", dir, cmd); err != nil {
		return fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)))
	}
	// The run row, its delivered messages and the running status land together.
	// Dropped errors here left a live process with no thread_runs row: nothing
	// polled it, its reply never landed, and the messages stayed queued for a
	// second process. If the record cannot be written the process is stopped.
	if err := m.recordStart(runID, t.ID, qs, now, base, model, res.Effort); err != nil {
		m.Run("tmux", "kill-session", "-t", tmuxName)
		return fmt.Errorf("record run: %w", err)
	}
	return nil
}

func (m *Manager) recordStart(runID, threadID string, qs []queuedMsg, now time.Time, base, model, effort string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, out_file, in_file, busy, turns, last_boundary, last_output, model, effort) VALUES (?,?,?,?,?,?,1,1,?,?,?,?)`, runID, threadID, ts(now), qs[0].kind, base+".out", base+".in", ts(now), ts(now), model, effort); err != nil {
		return err
	}
	for _, q := range qs {
		if _, err := tx.Exec(`UPDATE thread_messages SET queued=0, run_id=?, delivered_at=? WHERE id=?`, runID, ts(now), q.id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE threads SET status='running', updated_at=?, last_run_at=? WHERE id=?`, ts(now), ts(now), threadID); err != nil {
		return err
	}
	return tx.Commit()
}

// binWaitSeconds: how long a launch waits for a missing claude binary before
// running anyway (and failing loudly). An npm reinstall is a few seconds.
const binWaitSeconds = 90

// waitForBin: a shell prefix that polls for the CLI once a second until it is
// executable or the wait runs out, then falls through to the command.
func waitForBin(bin string) string {
	return fmt.Sprintf("i=0; while [ ! -x %s ] && [ $i -lt %d ]; do sleep 1; i=$((i+1)); done; ", shellQuote(bin), binWaitSeconds)
}

// feedBin: the lifectl next to the hub binary (launchd's PATH has neither).
func (m *Manager) feedBin() string {
	if m.FeedBin != "" {
		return m.FeedBin
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "lifectl"); fileExists(p) {
			return p
		}
	}
	return "lifectl"
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// Stop kills a running thread's process and returns it to idle. The
// conversation itself is kept (Claude's session id is unchanged), so the
// thread can be resumed with a new message. Cost of the killed turn is lost
// (claude never printed its result line).
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	if t.Status != "running" {
		return errors.New("thread is not running")
	}
	return m.killRuns(id, "Stopped by you.", "idle")
}

// Archive ends a thread: kills any running session, clears its schedule so the
// scheduler never wakes it again, and hides it from the default list. The
// conversation is kept and can be resumed by setting status back to idle.
func (m *Manager) Archive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	if t.Status == "archived" {
		return nil
	}
	// A SESSION WITH A CARD FOR THE OWNER IS NEVER ARCHIVED: an approval
	// raised a second before the archive would vanish with it, and Your turn
	// would no longer show it on Sessions. Answer or dismiss the card first;
	// a session with nothing for the owner already leaves Sessions.
	if n := m.cardsForOwner(id); n > 0 {
		return fmt.Errorf("it has %d card(s) waiting on you — answer or dismiss them first", n)
	}
	if _, live := m.liveRun(id); live {
		note := "Archived by you."
		if t.Status == "running" {
			note = "Stopped and archived by you."
		}
		if err := m.killRuns(id, note, "archived"); err != nil {
			return err
		}
	}
	now := time.Now()
	_, err = m.db.Exec(`UPDATE threads SET status='archived', schedule='', updated_at=? WHERE id=?`, ts(now), id)
	// Its future goes with it: the standing row (its schedule) and any
	// one-shot aimed at it. A "new-or:" prompt is aimed elsewhere and keeps.
	m.db.Exec(`UPDATE prompts SET state='cancelled', error='session archived' WHERE target=? AND state='queued'`, id)
	log.Printf("thread %s: archived", id)
	return err
}

// cardsForOwner counts what the Sessions board would list for this thread:
// open asks that block (not the owner's calendar steps) plus proposed approvals.
func (m *Manager) cardsForOwner(id string) int {
	n := 0
	for _, a := range m.activeAsks(id) {
		if a.State == "open" && a.Blocks() && a.CalID == "" {
			n++
		}
	}
	var acts int
	m.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='action' AND thread_id=? AND state='proposed'`, id).Scan(&acts)
	return n + acts
}

// killRuns kills every live process of a thread and sets its status.
// Caller holds m.mu.
func (m *Manager) killRuns(id, note, status string) error {
	rows, err := m.db.Query(`SELECT id FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, id)
	if err != nil {
		return err
	}
	var runs []string
	for rows.Next() {
		var r string
		rows.Scan(&r)
		runs = append(runs, r)
	}
	rows.Close()
	now := time.Now()
	for _, runID := range runs {
		// Ignore "no such session": the process may already be gone.
		m.Run("tmux", "kill-session", "-t", fmt.Sprintf("%s-th-%s-%s", m.Prefix, id, runID))
		m.db.Exec(`UPDATE thread_runs SET finished_at=?, ok=0, busy=0 WHERE id=?`, ts(now), runID)
	}
	// Anything not yet handed over stays queued for the next process.
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES (?,?,?,?,?)`, id, ts(now), "system", "error", note)
	_, err = m.db.Exec(`UPDATE threads SET status=?, updated_at=? WHERE id=?`, status, ts(now), id)
	log.Printf("thread %s: stopped (%d process(es) killed)", id, len(runs))
	return err
}

// Poll streams new output of live processes into events, settles finished
// turns, reaps idle processes and settles exited ones. Call every few seconds.
func (m *Manager) Poll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.db.Query(`SELECT ` + runCols + ` FROM thread_runs WHERE finished_at IS NULL`)
	if err != nil {
		return
	}
	var pending []run
	for rows.Next() {
		if r, err := scanRun(rows); err == nil {
			pending = append(pending, r)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("threads poll: %v", err) // settle what was read; the rest next poll
	}
	rows.Close()
	now := time.Now()
	for _, r := range pending {
		r = m.tail(r)
		if _, err := os.Stat(r.out + ".done"); err == nil {
			if h, ok := r.heldResult(); ok {
				// The process's last word was a folded result: it is the reply.
				r = m.finishTurn(r, h)
			}
			m.finishProcess(r)
			continue
		}
		if r.in == "" {
			continue // legacy one-shot run: ends with its .done
		}
		switch {
		case r.held != "" && now.Sub(r.lastOutput) > holdTTL:
			// A background task that never notified: the folded result lands
			// as the reply rather than staying invisible.
			if h, ok := r.heldResult(); ok {
				log.Printf("thread %s: held result settled by timeout (background task never returned)", r.thread)
				m.finishTurn(r, h)
			}
		case r.busy == 1 && !r.lastResult.IsZero() && now.Sub(r.lastOutput) > stuckTTL && r.lastResult.After(m.lastDelivered(r)):
			// A result came after the last message and nothing since: the
			// steered message was folded into that turn.
			log.Printf("thread %s: turn settled by timeout (steered message folded)", r.thread)
			m.db.Exec(`UPDATE thread_runs SET busy=0 WHERE id=?`, r.id)
			m.reopenUnclosed(r.thread)
			m.settleStatus(r.thread)
		case r.busy == 0 && r.eof == 0 && now.Sub(r.lastOutput) > idleTTL:
			m.sendEOF(r)
		}
	}
}

// ofProcess matches a process's messages: its first turn is run_id = r.id,
// later turns r.id-t<n> (turnID). A range, not LIKE 'id%' — SQLite's LIKE is
// case-insensitive and cannot use the thread_messages_run index.
const ofProcess = `(run_id = ?1 OR (run_id >= ?2 AND run_id < ?3))`

func processArgs(r run) []any { return []any{r.id, r.id + "-t", r.id + "-u"} }

func (m *Manager) lastDelivered(r run) time.Time {
	var s string
	m.db.QueryRow(`SELECT COALESCE(MAX(delivered_at), '') FROM thread_messages WHERE `+ofProcess, processArgs(r)...).Scan(&s)
	return parseTS(s)
}

// deliveredAfter: ids of this process's messages handed over after t.
func (m *Manager) deliveredAfter(r run, t time.Time) []int64 {
	rows, err := m.db.Query(`SELECT id, delivered_at FROM thread_messages WHERE `+ofProcess+` AND delivered_at != ''`, processArgs(r)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		var d string
		rows.Scan(&id, &d)
		if parseTS(d).After(t) {
			out = append(out, id)
		}
	}
	return out
}

// sendEOF asks the feeder to close claude's stdin; the process exits after
// its current (none expected) turn. The next message starts a fresh process.
func (m *Manager) sendEOF(r run) {
	if f, err := os.OpenFile(r.in, os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		f.WriteString(eofSentinel + "\n")
		f.Close()
	}
	m.db.Exec(`UPDATE thread_runs SET eof=1 WHERE id=?`, r.id)
	log.Printf("thread %s: process %s idle, closing", r.thread, r.id)
}

// tail parses stream-json lines appended to the run's output since its
// offset: steps become events, tool results move the steering boundary,
// result lines close turns. Returns the run with offset/timestamps advanced.
func (m *Manager) tail(r run) run {
	f, err := os.Open(r.out)
	if err != nil {
		return r
	}
	defer f.Close()
	if _, err := f.Seek(r.off, 0); err != nil {
		return r
	}
	buf, err := readAll(f)
	if err != nil || len(buf) == 0 {
		return r
	}
	end := strings.LastIndexByte(string(buf), '\n')
	_, doneErr := os.Stat(r.out + ".done")
	done := doneErr == nil
	if done {
		end = len(buf) // process exited: nothing more is coming, take it all
	}
	if end < 0 {
		return r
	}
	now := time.Now()
	var dirty bool
	var live Tokens     // this pass's share of the turn in flight
	var liveUSD float64 // …priced as it is counted, per message's own model
	for _, line := range strings.Split(string(buf[:end]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dirty = true
		var head struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
		}
		json.Unmarshal([]byte(line), &head)
		switch {
		case head.Type == "system" && head.Subtype == "init":
			if head.SessionID != "" {
				m.db.Exec(`UPDATE threads SET claude_session_id=? WHERE id=?`, head.SessionID, r.thread)
			}
			// A new turn on the process. While a result is held, that is
			// either the CLI re-entering on a task notification (the wait
			// goes on; keep folding) or the owner's message (steered to stdin
			// after the folded result): that message gets the held reply
			// first, and finishTurn hands the message to a turn of its own.
			if h, ok := r.heldResult(); ok && len(m.deliveredAfter(r, h.At)) > 0 {
				r = m.finishTurn(r, h)
				live, liveUSD = Tokens{}, 0
			}
			// The turn claude just opened has a starter, never a steer: a
			// message handed over as the previous turn was ending was stamped
			// steered on delivery and re-stamped `<run>-tN` by finishTurn, and
			// this init is the proof claude did NOT fold it in but started on
			// it. The phone drew "steered in mid-turn" under a message with its
			// own step block.
			m.db.Exec(`UPDATE thread_messages SET steered=0 WHERE steered=1 AND id=(SELECT MIN(id) FROM thread_messages WHERE run_id=? AND role!='claude')`, r.turnID())
		case head.Type == "system" && head.Subtype == "background_tasks_changed":
			var ev struct {
				Tasks []json.RawMessage `json:"tasks"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil {
				r.tasks = len(ev.Tasks)
			}
		case head.Type == "result" || (head.Type == "" && strings.Contains(line, `"result"`)):
			var env resultEnvelope
			if json.Unmarshal([]byte(line), &env) == nil {
				h := heldResult{Env: env, Tok: env.tokens(), At: now, Turn: r.turnID()}
				if prev, ok := r.heldResult(); ok {
					h.Tok = addTokens(prev.Tok, h.Tok) // the folded sub-turns' usage rides on the reply
				}
				if r.tasks > 0 && !done && !env.IsError {
					// A background task is still pending: the CLI will re-enter
					// a turn when it notifies, so this is a progress line, not
					// the reply. Its text is already a fold event; hold the
					// envelope and keep the live estimate running.
					b, _ := json.Marshal(h)
					r.held = string(b)
					log.Printf("thread %s: result folded, %d background task(s) pending", r.thread, r.tasks)
				} else {
					r = m.finishTurn(r, h)
					live, liveUSD = Tokens{}, 0 // settled for real; drop the estimate
				}
			}
		default:
			if id, u, usd, ok := parseUsage(line, r.model); ok && !seenMsg(r.liveMsg, id) {
				live.In += u.In
				live.Out += u.Out
				live.CacheRead += u.CacheRead
				live.CacheWrite += u.CacheWrite
				liveUSD += usd
				r.liveMsg = rememberMsg(r.liveMsg, id)
			}
			evs := parseEvents(line)
			for _, ev := range evs {
				res, _ := m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`, r.thread, r.turnID(), ts(now), ev.Kind, ev.Title, ev.Body, ev.Summary)
				if ev.Kind == "tool_result" {
					r.lastBoundary = now
				}
				if ev.describe != "" && res != nil && m.Describe != nil {
					if id, err := res.LastInsertId(); err == nil {
						go m.describeAsync(id, r.thread, ev.describe)
					}
				}
			}
			if len(evs) > 0 && r.busy == 0 {
				// Output with no turn open: claude started a turn from a
				// message we had assumed folded into the previous one.
				r.busy = 1
				m.db.Exec(`UPDATE threads SET status='running', updated_at=? WHERE id=?`, ts(now), r.thread)
			}
		}
	}
	r.off += int64(end) + 1
	if dirty {
		r.lastOutput = now
	}
	m.db.Exec(`UPDATE thread_runs SET out_offset=?, busy=?, last_boundary=?, last_output=?, live_msg=?, bg_tasks=?, held=?,
		live_tok_in=live_tok_in+?, live_tok_out=live_tok_out+?, live_tok_cache_read=live_tok_cache_read+?, live_tok_cache_write=live_tok_cache_write+? , live_cost_usd=live_cost_usd+? WHERE id=?`,
		r.off, r.busy, ts(r.lastBoundary), ts(r.lastOutput), r.liveMsg, r.tasks, r.held, live.In, live.Out, live.CacheRead, live.CacheWrite, liveUSD, r.id)
	return r
}

// No tool-call backstop lives here any more. The hub used to steer a turn at
// 300 calls and kill it at 450; the steer landed in the middle of a review
// pass, so the whole mechanism came out — a turn is
// over when the session says it is. What still bounds an unattended wake is
// its dollar cap (spend.Rule.MaxBudgetUSD) and the watchdog for a process
// that has stopped producing output, neither of which counts steps.

func readAll(f *os.File) ([]byte, error) {
	var out []byte
	b := make([]byte, 64<<10)
	for {
		n, err := f.Read(b)
		out = append(out, b[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
	}
}

const eventBodyMax = 4000

// parseEvents turns one stream-json line into zero or more events. Shapes
// (claude -p --output-format stream-json --verbose):
//
//	{"type":"assistant","message":{"content":[{"type":"text"|"thinking"|"tool_use",...}]}}
//	{"type":"user","message":{"content":[{"type":"tool_result","content":...}]}}
//	{"type":"system"|"result"|"rate_limit_event",...}   (handled by tail / ignored)
func parseEvents(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || (msg.Type != "assistant" && msg.Type != "user") {
		return nil
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Content  json.RawMessage `json:"content"`
		IsError  bool            `json:"is_error"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		return nil // plain-string content (user prompt echo): nothing to show
	}
	var out []Event
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if s := strings.TrimSpace(b.Text); s != "" {
				out = append(out, Event{Kind: "text", Body: truncate(s, eventBodyMax)})
			}
		case "thinking":
			if s := strings.TrimSpace(b.Thinking); s != "" {
				out = append(out, Event{Kind: "thinking", Body: truncate(s, eventBodyMax)})
			}
		case "tool_use":
			title, body := describeTool(b.Name, b.Input)
			summary, describe := toolSummary(b.Name, b.Input)
			out = append(out, Event{Kind: "tool_use", Title: title, Body: body, Summary: summary, describe: describe})
		case "tool_result":
			title := "result"
			if b.IsError {
				title = "error"
			}
			out = append(out, Event{Kind: "tool_result", Title: title, Body: truncate(resultText(b.Content), eventBodyMax)})
		}
	}
	return out
}

// parseUsage reads the token usage off one streamed assistant message, so a
// running session's total moves while it works instead of standing at the
// last settled turn (cost "at the present time"). Returns the
// message id for dedupe: the CLI emits one assistant line per content block
// and every one of them repeats that message's usage.
//
// Output tokens on these lines are what the block had produced so far, so the
// live figure runs a little low until the result line settles the turn with
// the real usage — input and cache, which dominate the count, are exact.
//
// usd is those tokens priced at the model named on the message itself, not at
// the run's model: a chat that dropped from Opus to Sonnet mid-run must not
// have the cheaper half billed at the dearer rate.
// liveMsgKeep: how many recent message ids liveMsg remembers — well past how
// far apart parallel subagents interleave one message's blocks.
const liveMsgKeep = 256

func seenMsg(ids, id string) bool {
	for _, s := range strings.Fields(ids) {
		if s == id {
			return true
		}
	}
	return false
}

func rememberMsg(ids, id string) string {
	f := append(strings.Fields(ids), id)
	if len(f) > liveMsgKeep {
		f = f[len(f)-liveMsgKeep:]
	}
	return strings.Join(f, " ")
}

func parseUsage(line, fallbackModel string) (id string, t Tokens, usd float64, ok bool) {
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				Input      int `json:"input_tokens"`
				Output     int `json:"output_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				// The CLI splits cache writes by TTL when it knows it; a 1h
				// entry costs 2x input, a 5m one 1.25x.
				CacheCreation struct {
					H1 int `json:"ephemeral_1h_input_tokens"`
					M5 int `json:"ephemeral_5m_input_tokens"`
				} `json:"cache_creation"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.Type != "assistant" || msg.Message.ID == "" {
		return "", Tokens{}, 0, false
	}
	u := msg.Message.Usage
	t = Tokens{In: u.Input, Out: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
	t.sum()
	model := msg.Message.Model
	if model == "" {
		model = fallbackModel
	}
	su := spend.Usage{Model: model, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
		CacheWrite1h: u.CacheCreation.H1, CacheWrite5m: u.CacheCreation.M5}
	if su.CacheWrite1h+su.CacheWrite5m == 0 {
		su.CacheWrite5m = u.CacheWrite // no split reported: assume the short TTL
	}
	usd, _ = su.Cost()
	return msg.Message.ID, t, usd, t.Total > 0
}

// describeTool: a one-line title the owner can scan ("Bash · lifectl goals") plus
// the full input as body.
func describeTool(name string, input json.RawMessage) (string, string) {
	var in map[string]any
	json.Unmarshal(input, &in)
	str := func(k string) string {
		if v, ok := in[k].(string); ok {
			return v
		}
		return ""
	}
	var arg string
	switch name {
	case "Bash":
		arg = str("command")
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		arg = str("file_path")
	case "Grep", "Glob":
		arg = str("pattern")
	case "WebFetch":
		arg = str("url")
	case "WebSearch":
		arg = str("query")
	case "Agent", "Task":
		arg = str("description")
	default:
		if p := str("prompt"); p != "" {
			arg = p
		}
	}
	title := name
	if arg = firstLine(arg, 120); arg != "" {
		title = name + " · " + arg
	}
	// The body is what the chat prints under the call once it is opened
	// (EventRow, evRowHTML). It used to be the input as JSON, so a Read opened
	// to `{"file_path": "…"}` under a title that already said the path. Now it is only what the title cannot say: a
	// shell command whole (the title cuts it at 120), an edit as its old and
	// new lines, a written file's text; nothing for a call the title covers
	// (Read, Grep, Glob, a fetch, a search); `key: value` lines for the rest.
	var body string
	switch name {
	case "Bash":
		body = str("command")
	case "Edit":
		body = editLines(str("old_string"), str("new_string"))
	case "MultiEdit":
		var parts []string
		if edits, ok := in["edits"].([]any); ok {
			for _, e := range edits {
				if m, ok := e.(map[string]any); ok {
					o, _ := m["old_string"].(string)
					n, _ := m["new_string"].(string)
					parts = append(parts, editLines(o, n))
				}
			}
		}
		body = strings.Join(parts, "\n\n")
	case "Write":
		body = str("content")
	case "NotebookEdit":
		body = str("new_source")
	case "Read", "Grep", "Glob", "WebFetch", "WebSearch", "TodoWrite":
		body = ""
	default:
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var lines []string
		for _, k := range keys {
			v, ok := in[k].(string)
			if !ok {
				if raw, err := json.Marshal(in[k]); err == nil {
					v = string(raw)
				}
			}
			lines = append(lines, k+": "+firstLine(v, 200))
		}
		body = strings.Join(lines, "\n")
	}
	return title, truncate(body, eventBodyMax)
}

// editLines: an Edit as a reader sees a diff — the old lines behind "- ",
// the new ones behind "+ ".
func editLines(old, new string) string {
	mark := func(prefix, s string) []string {
		if s == "" {
			return nil
		}
		var out []string
		for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			out = append(out, prefix+l)
		}
		return out
	}
	return strings.Join(append(mark("- ", old), mark("+ ", new)...), "\n")
}

// resultText flattens a tool_result content (string or [{type:text,text}]).
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b []string
		for _, p := range parts {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				b = append(b, p.Text)
			} else if p.Type == "image" {
				b = append(b, "[image]")
			}
		}
		return strings.TrimSpace(strings.Join(b, "\n"))
	}
	return ""
}

// Tolerates a leading list marker ("- NEEDS YOU:", "2. NEEDS YOU:", "• NEEDS
// YOU:") and bold: sessions are told to write bullets, and one that wrapped
// its alert in a bullet silently never reached the board.
// An optional date ("NEEDS YOU <YYYY-MM-DD>:", "NEEDS YOU on <YYYY-MM-DD> 09:00:")
// parks it on the calendar for that morning instead of the board today.
var needsYouRe = regexp.MustCompile(`(?m)^\s*(?:[-*•]|\d+[.)])?\s*\**NEEDS[ _-]YOU(?:\s+(?:on\s+)?(\d{4}-\d{2}-\d{2})(?:\s+(\d{1,2}:\d{2}))?)?\**\s*:\**\s*(.+)$`)

type resultEnvelope struct {
	Type      string  `json:"type"`
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	Cost      float64 `json:"total_cost_usd"` // cumulative over the process
	SessionID string  `json:"session_id"`
	// Usage is this turn alone (unlike Cost), summed over every API call the
	// turn made — the authoritative count that replaces the live estimate.
	Usage struct {
		Input      int `json:"input_tokens"`
		Output     int `json:"output_tokens"`
		CacheRead  int `json:"cache_read_input_tokens"`
		CacheWrite int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (e resultEnvelope) tokens() Tokens {
	t := Tokens{In: e.Usage.Input, Out: e.Usage.Output, CacheRead: e.Usage.CacheRead, CacheWrite: e.Usage.CacheWrite}
	t.sum()
	return t
}

// EndSentinel is the whole reply of a turn whose card was the reply: the
// preamble tells the session to end on this one token alone, finishTurn
// stores "" for it, and both surfaces draw the empty row as "turn ended · $"
// It is a token and not an empty message because `claude -p`
// (2.1.183+) re-prompts once on a reply with no visible text — "[Your
// previous response had no visible output. Please continue…]" — which is one
// more completion and a wrap-up anyway.
const EndSentinel = "[end]"

// replyCardTitleMax is the longest title a reply-minted read card gets — the
// preamble's own ceiling for an ask title.
const replyCardTitleMax = 80

// replyLabel strips the wrap-up labels older preambles asked for ("Did:",
// "Done:", "Next:") off the front of a headline — the card is the report, so
// the label is noise on a title.
var replyLabel = regexp.MustCompile(`(?i)^(?:did|done|next|result|summary)\s*[:—-]\s*`)

// heardReminder rides under every follow-up to a live process. The preamble is
// read once, when the process starts, so a session older than the `--say` rule
// never learned it and its cards went out in the label form. One line a turn
// is the price of reaching them.
const heardReminder = "\n\n[Your reply IS what the owner hears: `Say: Hey <name>, …` — one spoken message, the whole answer, 2-5 plain sentences that stand alone (name the thing, never \"this session\"; no ids, URLs, paths or markdown; ≤700 chars) — then a blank line and, ONLY if they have steps to carry out, a numbered list. Nothing else: no title line, no prose body. `lifectl ask add`: the same message in `--say`, steps only in `--detail`.]"

// sayLine is the spoken message a closing reply carries for its own card: a
// first (or last) line "Say: Hey <name>, …". Without one, a reply-minted card
// is spoken in the label form ("To read. This thread is completed…") instead
// of as one continuous message to the owner.
var sayLine = regexp.MustCompile(`(?i)^\s*\**say\**\s*:\**\s*(.+?)\s*$`)

// splitSay takes the "Say: …" message off a reply: the message is what the
// push speaks AND what the card leads with; the rest — the owner's steps,
// when there are any — is the card's detail: a reply is something to hear
// plus, possibly, a list of discrete steps. An opening Say runs on to the first blank line,
// so a message longer than one line is still one message; a closing Say is
// its one line. A "say:" inside the body stays where it is.
func splitSay(text string) (rest, say string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	first, last := -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first >= 0 {
		if m := sayLine.FindStringSubmatch(lines[first]); m != nil {
			end := first + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
				end++
			}
			say := strings.Join(append([]string{m[1]}, lines[first+1:end]...), " ")
			say = strings.Join(strings.Fields(say), " ")
			return strings.TrimSpace(strings.Join(lines[end:], "\n")), say
		}
	}
	if last > first {
		if m := sayLine.FindStringSubmatch(lines[last]); m != nil {
			return strings.TrimSpace(strings.Join(lines[:last], "\n")), m[1]
		}
	}
	return text, ""
}

// sayGreeting is the opening a spoken message wears and a card title does
// not: "Hey Sam," / "Hi Sam." / "Hey,". A greeting word and its punctuation
// are both required, so "Okay the build is up." keeps its first word.
var sayGreeting = regexp.MustCompile(`(?i)^(?:hey|hi|hello|ok|okay)(?:\s+[\p{L}'’-]+)?\s*[,.!—-]+\s*`)

// sayTitle is the card title a spoken message implies: its first sentence,
// less the greeting, capitalised, cut at a word past replyCardTitleMax. The
// card itself leads with the whole message (both surfaces hide a title the
// message already contains); this title is for the rows that show one — the
// Sessions list cell, the board, a push's label form.
func sayTitle(say string) string {
	say = mdLink.ReplaceAllString(say, "$1") // a title is never cut inside a link
	s := strings.TrimSpace(sayGreeting.ReplaceAllString(strings.TrimSpace(say), ""))
	if s == "" {
		s = strings.TrimSpace(say)
	}
	// First sentence: up to ". ", "! " or "? " (a decimal "$17.71" has no space after its dot).
	if i := strings.IndexAny(s, ".!?"); i >= 0 {
		for i >= 0 && i < len(s)-1 && s[i+1] != ' ' {
			j := strings.IndexAny(s[i+1:], ".!?")
			if j < 0 {
				i = -1
				break
			}
			i += 1 + j
		}
		if i >= 0 {
			s = s[:i]
		}
	}
	s = strings.TrimSpace(strings.TrimRight(s, ".:"))
	if r := []rune(s); len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
		s = string(r)
	}
	if utf8.RuneCountInString(s) > replyCardTitleMax {
		runes := []rune(s)
		cut := replyCardTitleMax - 1
		for j := cut; j > replyCardTitleMax/2; j-- {
			if runes[j] == ' ' {
				cut = j
				break
			}
		}
		// A bare URL the cut lands in goes whole: half a URL is a link to a 404.
		head := string(runes[:cut])
		if k := strings.LastIndexAny(head, " \t"); strings.Contains(head[k+1:], "://") {
			head = head[:k+1]
		}
		s = strings.TrimSpace(head) + "…"
	}
	return s
}

// unlinkSay moves the markdown links out of a spoken message: the message
// keeps each link's text, and the links themselves lead the detail (unless
// the detail already carries them), where they render and stay whole. A link
// left in the message was read aloud as its URL and, cut at the title's 80
// runes, drew as half a URL — a live link to a 404.
func unlinkSay(say, detail string) (string, string) {
	var lead []string
	for _, l := range mdLink.FindAllString(say, -1) {
		if !strings.Contains(detail, l) {
			lead = append(lead, "- "+l)
		}
	}
	say = strings.Join(strings.Fields(mdLink.ReplaceAllString(say, "$1")), " ")
	if len(lead) > 0 {
		detail = strings.TrimSpace(strings.Join(lead, "\n") + "\n\n" + detail)
	}
	return say, detail
}

// cardFromReply is the read card a closing reply becomes, in ONE shape: the
// message IS the reply — it is spoken, the card leads with it, its first
// sentence is the title, and whatever follows (the owner's steps) is the
// detail. A reply that forgot its "Say:" line is read as if it had one: its
// first paragraph, plain, is the message, opened "Hey <name>, about
// <session>." (thread), and the paragraphs after it are the detail.
//
// There is no second "plain reply" shape (first line cut as the title, the
// push speaking only that cut title): one path, not two. A `[end]` typed under the text is the sentinel,
// not a word: it comes off the card and the line.
func cardFromReply(text, thread string) (title, detail, say string) {
	text = stripEndSentinel(text)
	body, say := splitSay(text)
	if say != "" {
		say, body = unlinkSay(say, strings.TrimSpace(body))
		return sayTitle(say), body, say
	}
	para, rest := firstParagraph(text)
	msg := spokenReply(para)
	if msg == "" { // the first paragraph was a code block or a table
		msg, rest = spokenReply(text), text
	}
	if msg == "" {
		return "Session reply", text, spokenFallback("read", "", "Session reply", thread)
	}
	if strings.HasSuffix(msg, "…") { // too long to say whole: keep every word on the card
		rest = text
	}
	return sayTitle(msg), rest, spokenFallback("read", "", msg, thread)
}

// firstParagraph splits text at its first blank line (a fenced block counts
// as a paragraph of its own).
func firstParagraph(text string) (para, rest string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	fence := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			fence = !fence
		}
		if t == "" && !fence {
			return strings.Join(lines[:i], "\n"), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return strings.Join(lines, "\n"), ""
}

// endSentinelLine is the sentinel on a line of its own, anywhere in a reply
// (a turn that wrote its text AND `[end]` under it).
var endSentinelLine = regexp.MustCompile("(?m)^[ \t]*`*\\[end\\]`*[ \t]*$")

func stripEndSentinel(text string) string {
	return strings.TrimSpace(endSentinelLine.ReplaceAllString(text, ""))
}

// spokenReplyMax is the longest line a plain reply is read as — the ≤700
// characters sessions are told to keep a spoken message under (notify/voice.go
// bounds it at 1500). Past it the line is cut at the last sentence end, else
// a word, and ends "…".
const spokenReplyMax = 700

// fencedBlock is a ``` … ``` block: pasted JSON, a file, a command — nothing
// to read aloud.
var fencedBlock = regexp.MustCompile("(?s)```.*?(```|$)")

// spokenReply is a plain reply as one spoken paragraph: markdown stripped
// line by line (plainTitle), fenced blocks and table rows dropped, an old
// "Did:" label off the front, whitespace collapsed, capped at spokenReplyMax.
func spokenReply(text string) string {
	text = fencedBlock.ReplaceAllString(text, " ")
	var parts []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "|") {
			continue
		}
		if l = plainTitle(l); l != "" {
			parts = append(parts, l)
		}
	}
	s := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	s = replyLabel.ReplaceAllString(s, "")
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
		s = string(r)
	}
	if len(r) <= spokenReplyMax {
		return s
	}
	cut := 0
	for j := spokenReplyMax - 1; j > spokenReplyMax/2; j-- {
		if strings.ContainsRune(".!?", r[j]) && (j+1 == len(r) || r[j+1] == ' ') {
			return strings.TrimSpace(string(r[:j+1]))
		}
		if cut == 0 && r[j] == ' ' {
			cut = j
		}
	}
	if cut == 0 {
		cut = spokenReplyMax - 1
	}
	return strings.TrimSpace(string(r[:cut])) + "…"
}

// isEndSentinel says whether a reply is the sentinel and nothing else —
// surrounding whitespace and backticks tolerated, since a model told to
// "reply `[end]`" sometimes types the backticks too.
func isEndSentinel(text string) bool {
	return strings.Trim(strings.TrimSpace(text), "`") == EndSentinel
}

// finishTurn settles one turn of a live process from its result line:
// stores the reply, links asks, updates status/cost, and decides whether
// another turn is already on its way (a message delivered after the last
// tool boundary was not seen by the turn that just ended). h.Tok carries the
// usage of any sub-turns folded into this one (heldResult). Caller holds m.mu.
func (m *Manager) finishTurn(r run, h heldResult) run {
	env := h.Env
	// A result that was held belongs to the turn it ended, which may not be
	// the run's current one any more (heldResult.Turn).
	threadID, turn := r.thread, m.heldTurn(r, h)
	r.held = ""
	now := time.Now()
	ok := 1
	kind, role, text := "message", "claude", strings.TrimSpace(env.Result)
	// The card is the reply: a turn that raised a card ends
	// on EndSentinel alone, and the hub stores that as an empty reply. The
	// streamed text event still holds the token, so the de-dup below matches
	// on what was streamed, not on what is stored.
	streamed := text
	if isEndSentinel(text) {
		text = ""
	}
	if env.IsError {
		ok = 0
		kind = "error"
		errb, _ := os.ReadFile(r.out + ".err")
		text = strings.TrimSpace(env.Result + "\n" + string(errb))
		if text == "" {
			text = "turn failed"
		}
		if spend.IsRungError(text) && m.relaunchBelow(r, text) {
			r.busy = 0
			return r
		}
		// A 500/529 from the API is the provider's blip, not the owner's problem:
		// replay the turn instead of raising a card (retry.go).
		if m.retryTransient(r, text) {
			r.busy = 0
			return r
		}
	}
	if env.SessionID != "" {
		m.db.Exec(`UPDATE threads SET claude_session_id=? WHERE id=?`, env.SessionID, threadID)
	}
	cost := env.Cost - r.costSeen
	if cost < 0 {
		cost = env.Cost
	}
	r.costSeen = env.Cost
	// Asks raised during this turn (`lifectl ask add`, tied by LIFE_RUN_ID,
	// not yet linked to a reply). Fallback: NEEDS YOU lines in the reply
	// still become asks when the agent made none itself — belt and braces so
	// an alert is never lost — but the data structure is the source of truth.
	raised, _ := m.ListAsks("active", threadID, 100)
	var fresh []Ask
	for _, a := range raised {
		if a.RunID == r.id && a.MessageID == 0 {
			fresh = append(fresh, a)
		}
	}
	if len(fresh) == 0 {
		for _, n := range needsYouRe.FindAllStringSubmatch(text, -1) {
			day, at, line := n[1], n[2], strings.TrimSpace(n[3])
			if day != "" && m.DateAsk != nil {
				if err := m.DateAsk(threadID, firstLine(line, 120), line, "other", "", day, at, ""); err == nil {
					continue // becomes an ask on the day; nothing on the board now
				}
			}
			if a, err := m.AddAsk(threadID, r.id, firstLine(line, 120), line, "other", ""); err == nil {
				fresh = append(fresh, a)
			}
		}
	}
	// THERE IS NO WHITE CELL: anything the owner should read or do arrives as
	// a card. A reply's words never become a text bubble: with no card
	// raised this turn they ARE the read card (first line the title, the rest
	// the detail — the cheapest read card there is, no extra completion), and
	// beside a card they are a restatement of it,
	// kept only as a fold line in the tool chain. Either way the message row is
	// stored empty and both surfaces draw it as "turn ended · $". An error keeps
	// its text: that row is red, and the error card carries it too.
	folded := false
	if ok == 1 && text != "" {
		if len(fresh) == 0 {
			var tname string
			m.db.QueryRow(`SELECT title FROM threads WHERE id=?`, threadID).Scan(&tname)
			title, detail, say := cardFromReply(text, tname)
			if a, err := m.AddAskSaid(threadID, r.id, title, detail, "read", "", "", say); err == nil {
				fresh = append(fresh, a)
			}
		} else {
			folded = true
		}
		if len(fresh) > 0 {
			text = ""
		}
	}
	if ok == 0 {
		if a, err := m.AddAsk(threadID, r.id, failureTitle(text), text, "error", ""); err == nil {
			fresh = append(fresh, a)
		}
	}
	// A reply that raised only read-asks is an answer, not a stopped session
	// (nothing to do, the agent is not blocked, so it is not red): both surfaces
	// draw `read` blue and `needs_you` red.
	if len(fresh) > 0 {
		kind = "read"
		for _, a := range fresh {
			if a.Kind != "read" {
				kind = "needs_you"
				break
			}
		}
	}
	// A turn that died (API 500, plan limit, crash) is an error, not a reply
	// the owner is meant to answer: both surfaces colour `error` red, and the
	// sessions list says "Session failed" instead of printing the CLI's
	// paragraph as if it were the session's answer.
	if ok == 0 {
		kind = "error"
	}
	unreadBump := 0
	if len(fresh) > 0 || ok == 0 {
		unreadBump = 1
	}
	// The final reply is also the last streamed text block; keep it once —
	// on the message row, or on the read card minted from it. Text that rode
	// beside a card has no other home, so its fold line stays.
	if !folded {
		m.db.Exec(`DELETE FROM thread_events WHERE id = (SELECT MAX(id) FROM thread_events WHERE run_id=? AND kind='text') AND body=?`, turn, truncate(streamed, eventBodyMax))
	}
	tok := h.Tok
	res, _ := m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, cost_usd, tok_in, tok_out, tok_cache_read, tok_cache_write, run_id) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		threadID, ts(now), role, kind, text, cost, tok.In, tok.Out, tok.CacheRead, tok.CacheWrite, turn)
	if res != nil {
		if mid, err := res.LastInsertId(); err == nil {
			// Asks of this turn point at the reply they were raised in (the app
			// jumps there from the board).
			m.db.Exec(`UPDATE items SET message_id=? WHERE src='ask' AND run_id=? AND message_id IS NULL`, mid, r.id)
		}
	}
	// Another turn already underway? A message handed over after the last
	// tool boundary was not steered into this turn; claude starts on it next.
	// (Its `steered` stamp stays for now: whether claude folded it into this
	// turn or opens the next one on it is only known when the next turn's
	// init line arrives — see Poll — or the run settles by timeout.)
	r.busy = 0
	if late := m.deliveredAfter(r, r.lastBoundary); len(late) > 0 {
		r.busy = 1
		r.turns++
		for _, id := range late {
			m.db.Exec(`UPDATE thread_messages SET run_id=? WHERE id=?`, r.turnID(), id)
		}
	}
	r.lastBoundary, r.lastResult = now, now
	// The turn is settled, so its live estimate is spent: zero it here and the
	// authoritative usage lands on the thread instead, or the two double up.
	r.liveMsg = ""
	m.db.Exec(`UPDATE thread_runs SET busy=?, turns=?, cost_seen=?, last_boundary=?, last_result=?, ok=?, live_tok_in=0, live_tok_out=0, live_tok_cache_read=0, live_tok_cache_write=0, live_cost_usd=0, live_msg='', held='' WHERE id=?`, r.busy, r.turns, r.costSeen, ts(now), ts(now), ok, r.id)
	m.db.Exec(`UPDATE threads SET updated_at=?, unread=unread+?, cost_usd=cost_usd+?, tok_in=tok_in+?, tok_out=tok_out+?, tok_cache_read=tok_cache_read+?, tok_cache_write=tok_cache_write+? WHERE id=?`,
		ts(now), unreadBump, cost, tok.In, tok.Out, tok.CacheRead, tok.CacheWrite, threadID)
	if r.busy == 1 {
		m.db.Exec(`UPDATE threads SET status='running' WHERE id=?`, threadID)
	} else {
		// A good turn that left an answered card standing has handed it back. A
		// failed one read nothing: its Restart replays the same words.
		if ok == 1 {
			m.reopenUnclosed(threadID)
		}
		m.settleStatus(threadID)
	}
	t, _ := m.Get(threadID)
	// No push here. Every card buzzes the phone when it is RAISED (AddAskOn),
	// which is usually mid-tool-chain, minutes before this reply lands.
	// Pushing again at the end would buzz twice for one card.
	log.Printf("thread %s: turn %s finished ok=%d status=%s busy=%d", threadID, turn, ok, t.Status, r.busy)
	if ok == 1 {
		delete(m.retries, threadID) // a good turn refills the auto-restart budget
		go m.autoTitle(threadID)
	}
	return r
}

// relaunchBelow: the turn died because its rung is unusable — the model's
// plan bucket is full, or the installed CLI does not know the model id.
// Never stall on a limit —
// close that rung, hand the turn's messages back to the queue, kill the
// process and start a new one; launch picks the next rung. Caller holds m.mu.
// Returns false when there is nowhere lower to go — no Model hook, or the
// ladder hands back the same model (the bottom rung, or a pinned id that is
// not on the ladder) — so the caller reports the failure the ordinary way
// instead of relaunching into the same error forever.
func (m *Manager) relaunchBelow(r run, text string) bool {
	if m.Model == nil {
		return false
	}
	if m.ModelExhausted != nil {
		m.ModelExhausted(r.model)
	}
	t, err := m.Get(r.thread)
	if err != nil {
		return false
	}
	next := m.resolve(t, r.trigger).Model
	if next == r.model {
		return false
	}
	now := time.Now()
	m.Run("tmux", "kill-session", "-t", fmt.Sprintf("%s-th-%s-%s", m.Prefix, r.thread, r.id))
	m.db.Exec(`UPDATE thread_runs SET finished_at=?, ok=0, busy=0, cost_seen=? WHERE id=?`, ts(now), r.costSeen, r.id)
	// Every message this process has seen since its last settled turn goes
	// back on the queue (turn ids are <run>, <run>-t2, …).
	m.db.Exec(`UPDATE thread_messages SET queued=1, run_id='', delivered_at='', steered=0 WHERE run_id=? AND role != 'claude' AND role != 'system'`, r.turnID())
	why := "plan limit on " + orDefault(r.model)
	if spend.IsUnsupportedModelError(text) && !spend.IsLimitError(text) {
		why = "the CLI does not support " + orDefault(r.model)
	}
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, run_id) VALUES (?,?,?,?,?,?)`, r.thread, ts(now), "system", "message",
		fmt.Sprintf("%s (%s); restarting on %s", why, firstLine(text, 120), orDefault(next)), r.turnID())
	log.Printf("thread %s: %s, relaunching on %q", r.thread, why, next)
	if err := m.launch(t); err != nil {
		log.Printf("thread %s: relaunch: %v", r.thread, err)
		m.db.Exec(`UPDATE threads SET status='idle', updated_at=? WHERE id=?`, ts(now), r.thread)
	}
	return true
}

// failureTitle: what the owner reads on the board when a session stops badly.
// The raw first line of a CLI error is often unreadable ("You've reached your
// Fable 5 limit. Switch to another model, or manage usage credits at
// claude.ai/settings/usage?from=cc_cli_limit_message, to continue."), so the
// cases we recognise get plain English.
// An API error is the commonest of all and the worst as a title — the CLI
// spells out "API Error: 500 Internal server error. This is a server-side
// issue, usually temporary — try again in a moment." and the card wore all
// three lines of it. The code alone says it; the paragraph
// is still the detail.
func failureTitle(text string) string {
	switch {
	// A session limit names its reset, and the hub resumes the turn then
	// (ResumePaused), so the title is when it is back.
	case spend.IsSessionLimit(text):
		now := time.Now()
		if at, ok := spend.ResetAt(text, now); ok {
			return "Session limit · back at " + spend.ClockWords(at, now)
		}
		return "Session limit"
	case spend.IsLimitError(text):
		return "Session paused: plan limit reached on every model"
	case spend.IsUnsupportedModelError(text):
		return "Session stopped: the Claude CLI is too old for this model"
	case strings.Contains(text, "went to sleep"):
		return "Session interrupted: the Mac slept mid-answer"
	case apiErrorCode(text) != "":
		code := apiErrorCode(text)
		return fmt.Sprintf("Session stopped: Claude API %s (%s)", code, apiErrorWhat(code))
	default:
		return "Session failed: " + firstLine(text, 80)
	}
}

func orDefault(model string) string {
	if model == "" {
		return "default model"
	}
	return model
}

// settleStatus: board rule for a thread with no turn in flight. A session
// only surfaces to the owner when it elects to (an active ask); otherwise it
// finishes quietly: `done` if nothing will wake it again, `idle` if it has
// a schedule. Caller holds m.mu.
func (m *Manager) settleStatus(threadID string) {
	var busy int
	if m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL AND busy=1`, threadID).Scan(&busy); busy > 0 {
		// Another process (started while this one wound down) has a turn
		// in flight: it settles the status when that turn ends.
		m.db.Exec(`UPDATE threads SET status='running' WHERE id=? AND status != 'archived'`, threadID)
		return
	}
	status := "done"
	if m.hasStanding(threadID) {
		status = "idle"
	}
	// Same rule as syncThreadStatus: only a card that BLOCKS makes it the
	// owner's turn. A practice or step is the calendar's row — counting it here
	// would leave the `calendar` thread red YOUR TURN on both surfaces with
	// nothing under Your turn.
	if m.blocked(threadID) {
		status = "needs_you"
	}
	m.db.Exec(`UPDATE threads SET status=? WHERE id=? AND status != 'archived'`, status, threadID)
}

// finishProcess settles an exited process. A turn still open means claude
// died (or was killed) mid-turn: that is an error the owner should see. Anything
// queued meanwhile starts a fresh process. Caller holds m.mu.
func (m *Manager) finishProcess(r run) {
	now := time.Now()
	if r.busy == 1 {
		errb, _ := os.ReadFile(r.out + ".err")
		text := strings.TrimSpace(string(errb))
		if text == "" {
			raw, _ := os.ReadFile(r.out)
			text = "run ended without a result: " + truncate(strings.TrimSpace(string(raw)), 500)
		}
		// A plan limit can also kill the process outright (the CLI prints the
		// limit message on stderr and exits without a result envelope). That
		// path used to skip the ladder entirely and dump the raw CLI text —
		// marketing URL and all — on the owner's board. Step down a rung instead.
		if spend.IsRungError(text) && m.relaunchBelow(r, text) {
			return
		}
		if m.retryTransient(r, text) {
			return
		}
		if a, err := m.AddAsk(r.thread, r.id, failureTitle(text), text, "error", ""); err == nil {
			res, _ := m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, run_id) VALUES (?,?,?,?,?,?)`, r.thread, ts(now), "claude", "error", text, r.turnID())
			if res != nil {
				if mid, err := res.LastInsertId(); err == nil {
					m.db.Exec(`UPDATE items SET message_id=? WHERE id=?`, mid, a.ID)
				}
			}
		}
		// AddAsk already pushed the card ("[session] Session failed: …").
	}
	m.db.Exec(`UPDATE thread_runs SET finished_at=?, busy=0, ok=COALESCE(ok, 1) WHERE id=?`, ts(now), r.id)
	m.settleStatus(r.thread)
	log.Printf("thread %s: process %s exited", r.thread, r.id)
	if t, err := m.Get(r.thread); err == nil && t.Status != "archived" {
		if err := m.launch(t); err != nil {
			log.Printf("thread %s: follow-up launch: %v", r.thread, err)
		}
	}
}

// toolSummary: what the tool call does in plain English, for the owner to follow
// along without reading shell. Bash carries its own description (Claude
// Code asks the model for one); when it is missing, `describe` is the
// command for Describe (a Haiku one-liner, filled in asynchronously).
func toolSummary(name string, input json.RawMessage) (summary, describe string) {
	var in map[string]any
	json.Unmarshal(input, &in)
	str := func(k string) string {
		if v, ok := in[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	q := func(s string) string { return `"` + firstLine(s, 60) + `"` }
	where := func() string {
		if p := str("path"); p != "" {
			return " in " + shortPath(p)
		}
		return ""
	}
	switch name {
	case "Bash":
		if d := str("description"); d != "" {
			return firstLine(d, 140), ""
		}
		return "", str("command")
	case "Read":
		return "Read " + shortPath(str("file_path")), ""
	case "Edit", "MultiEdit":
		return "Edit " + shortPath(str("file_path")), ""
	case "Write":
		return "Write " + shortPath(str("file_path")), ""
	case "NotebookEdit":
		return "Edit notebook " + shortPath(str("notebook_path")), ""
	case "Grep":
		return "Search for " + q(str("pattern")) + where(), ""
	case "Glob":
		return "Find files matching " + q(str("pattern")) + where(), ""
	case "WebFetch":
		return "Fetch " + firstLine(str("url"), 100), ""
	case "WebSearch":
		return "Search the web for " + q(str("query")), ""
	case "Agent", "Task":
		return "Delegate: " + firstLine(str("description"), 120), ""
	case "TodoWrite":
		return "Update the to-do list", ""
	case "Skill":
		return "Use skill " + str("skill"), ""
	case "ToolSearch":
		return "Look up tools for " + q(str("query")), ""
	case "Monitor":
		return "Wait for: " + firstLine(str("command"), 100), ""
	case "StructuredOutput":
		return "Return the result", ""
	}
	if d := str("description"); d != "" {
		return firstLine(d, 140), ""
	}
	return name, ""
}

// shortPath: ~ for home, repo-relative when under ~/life.
func shortPath(p string) string {
	if p == "" {
		return "file"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(filepath.Join(home, "life"), p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
		if strings.HasPrefix(p, home) {
			return "~" + strings.TrimPrefix(p, home)
		}
	}
	return p
}

// describeAsync fills a tool_use event's summary with a Haiku one-liner
// for a shell command that came without a description.
func (m *Manager) describeAsync(eventID int64, threadID, command string) {
	var project string
	m.db.QueryRow(`SELECT project FROM threads WHERE id=?`, threadID).Scan(&project)
	dir, _ := m.ProjectDir(project)
	out, err := m.Describe(dir, "Describe what this shell command does in one short plain-English line (imperative, at most 12 words, no quotes, no trailing period, no markdown). Reply with only that line.\n\n"+truncate(command, 1500))
	if err != nil {
		return
	}
	s := strings.Trim(firstLine(out, 140), `"'“” .`)
	if s == "" {
		return
	}
	m.db.Exec(`UPDATE thread_events SET summary=? WHERE id=?`, s, eventID)
}
