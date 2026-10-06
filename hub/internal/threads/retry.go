package threads

// Errors are their own kind of card. A run that died on the provider's 500
// is not work for the owner — it is a turn that wants starting
// again — and the old card was the worst possible shape for it: the raw error
// as a three-line title, and an "Open link" button that opened the URL out of
// the error text.
//
// Two halves live here:
//   - the hub retries a transient API failure itself, twice, before the owner is
//     told anything (the same move the model ladder makes on a plan limit);
//   - what survives that becomes an ask of kind `error`, whose one button is
//     Restart — this file's RetryAsk puts the failed turn's own messages back
//     on the queue and starts a fresh process on them.

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"life/hub/internal/spend"
)

// apiErrorRe: the status code in "API Error: 529 Overloaded. This is a
// server-side issue…" — what the card says instead of the paragraph.
var apiErrorRe = regexp.MustCompile(`API Error:\s*(\d{3})`)

// transientRe: failures that are the provider's or the network's, not the
// run's, and that a second attempt usually clears. 4xx other than 429 is our
// own bad request and repeats identically, so it is not here.
var transientRe = regexp.MustCompile(`API Error:\s*(?:429|5\d\d)|Overloaded|overloaded_error|Connection error|fetch failed`)

// maxAutoRetries: how many times the hub restarts a turn on its own before it
// puts the error on the board. Two covers a blip; more would hide a session
// that is failing for a reason.
const maxAutoRetries = 2

func apiErrorCode(text string) string {
	if mm := apiErrorRe.FindStringSubmatch(text); mm != nil {
		return mm[1]
	}
	return ""
}

// apiErrorWhat: the code in plain words, for the card's one line.
func apiErrorWhat(code string) string {
	switch code {
	case "429":
		return "rate limited"
	case "529":
		return "overloaded"
	case "500", "502", "503", "504":
		return "server error"
	default:
		return "error"
	}
}

// transientFailure: worth restarting without asking. A plan limit is not one
// (the model ladder owns that) — it is checked first by both callers anyway.
func transientFailure(text string) bool {
	return !spend.IsLimitError(text) && transientRe.MatchString(text)
}

// lastTurnID: the turn a finished run died on. Messages carry the turn id
// (<run>, <run>-t2, …), and only the last turn's messages may be replayed —
// re-queueing the whole run would re-send messages that were already answered.
func (m *Manager) lastTurnID(runID string) string {
	var turn string
	m.db.QueryRow(`SELECT run_id FROM thread_messages WHERE (run_id=? OR run_id LIKE ?) AND role != 'claude' ORDER BY id DESC LIMIT 1`, runID, runID+"-t%").Scan(&turn)
	if turn == "" {
		return runID
	}
	return turn
}

// requeueTurn hands a turn's input back to the queue so the next launch
// replays it: the owner's messages and scheduled check-ins (a `system` note the
// hub wrote about the failure itself is not input and stays put). Returns how
// many messages went back. Caller holds m.mu.
func (m *Manager) requeueTurn(turnID string) int {
	res, err := m.db.Exec(`UPDATE thread_messages SET queued=1, run_id='', delivered_at='', steered=0 WHERE run_id=? AND (role='owner' OR kind='checkin')`, turnID)
	if err != nil || res == nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// resumeNote is what a restart says when the failed turn has no message of
// its own to replay (it was itself a resumed turn, or a check-in already
// consumed). Without it launch() would find an empty queue and do nothing.
func (m *Manager) resumeNote(threadID, text string) {
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, queued) VALUES (?,?,?,?,?,1)`,
		threadID, ts(time.Now()), "system", "message",
		fmt.Sprintf("[restart] The previous turn stopped with an error (%s). Pick up where it left off; if the work was already finished, say so in one line.", firstLine(text, 120)))
}

// retryTransient: the turn died on something the provider will probably not
// do twice. Put its messages back on the queue, kill the process, start a new
// one — no ask, no push, one system line in the thread so the restart is
// visible in the history. Returns false when the budget is spent (the caller
// then reports the failure the ordinary way). Caller holds m.mu.
func (m *Manager) retryTransient(r run, text string) bool {
	if !transientFailure(text) || m.retries[r.thread] >= maxAutoRetries {
		return false
	}
	if m.retries == nil {
		m.retries = map[string]int{}
	}
	m.retries[r.thread]++
	now := time.Now()
	m.Run("tmux", "kill-session", "-t", fmt.Sprintf("%s-th-%s-%s", m.Prefix, r.thread, r.id))
	m.db.Exec(`UPDATE thread_runs SET finished_at=?, ok=0, busy=0, cost_seen=? WHERE id=?`, ts(now), r.costSeen, r.id)
	turn := r.turnID()
	if m.requeueTurn(turn) == 0 {
		m.resumeNote(r.thread, text)
	}
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, run_id) VALUES (?,?,?,?,?,?)`, r.thread, ts(now), "system", "message",
		fmt.Sprintf("%s; restarting (attempt %d of %d)", firstLine(text, 120), m.retries[r.thread], maxAutoRetries), turn)
	log.Printf("thread %s: transient failure %q, retry %d", r.thread, firstLine(text, 80), m.retries[r.thread])
	t, err := m.Get(r.thread)
	if err != nil {
		return false
	}
	if err := m.launch(t); err != nil {
		log.Printf("thread %s: retry launch: %v", r.thread, err)
		m.db.Exec(`UPDATE threads SET status='idle', updated_at=? WHERE id=?`, ts(now), r.thread)
	}
	return true
}

// backfillErrorAsks: the failure cards already on the owner's board were written
// as `read` asks with the CLI's paragraph for a title. Retag and retitle the
// live ones so the change is visible on the cards that prompted it, not only
// on the next crash. Idempotent; run at startup.
func (m *Manager) backfillErrorAsks() {
	rows, err := m.db.Query(`SELECT id, detail FROM items WHERE src='ask' AND state IN ('open','answered') AND kind != 'error'
		AND (title LIKE 'Session failed:%' OR title LIKE 'Session paused:%' OR title LIKE 'Session interrupted:%' OR title LIKE 'Session stopped:%')`)
	if err != nil {
		return
	}
	type row struct{ id, detail string }
	var todo []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.detail)
		todo = append(todo, r)
	}
	rows.Close()
	for _, r := range todo {
		if strings.TrimSpace(r.detail) == "" {
			m.db.Exec(`UPDATE items SET kind='error' WHERE id=?`, r.id)
			continue
		}
		m.db.Exec(`UPDATE items SET kind='error', title=? WHERE id=?`, failureTitle(r.detail), r.id)
	}
	if len(todo) > 0 {
		log.Printf("asks: %d failure card(s) retagged as errors", len(todo))
	}
}

// RetryAsk is the Restart button on an error card: replay the turn that died.
// The card closes on the spot (by "app", like a tapped Install — the board
// is the owner's to-do list, so a tapped card must leave) and the session starts
// again on its own messages.
func (m *Manager) RetryAsk(id string) (Thread, error) {
	return m.retryAsk(id, "app", "restarted the session")
}

// ResumePaused: a session-limit card whose reset has come (Ask.ResumesAt,
// a minute's grace) restarts its session by itself — the owner should never
// have to type "are we back? can you keep going?". Called on the
// hub's minute clock; a turn that hits the limit again raises a new card.
func (m *Manager) ResumePaused(now time.Time) {
	open, err := m.ListAsks("active", "", 500)
	if err != nil {
		return
	}
	for _, a := range open {
		if a.Kind != "error" || !a.Open || a.ResumesAt == nil || now.Before(a.ResumesAt.Add(time.Minute)) {
			continue
		}
		if _, err := m.retryAsk(a.ID, "hub", "resumed at the reset"); err != nil {
			log.Printf("ask %s: resume at reset: %v", a.ID, err)
			continue
		}
		log.Printf("ask %s: session %s resumed at its limit reset", a.ID, a.ThreadID)
	}
}

func (m *Manager) retryAsk(id, by, note string) (Thread, error) {
	a, err := m.GetAsk(id)
	if err != nil {
		return Thread{}, errors.New("no such ask")
	}
	if a.Kind != "error" {
		return Thread{}, errors.New("only an error card can be restarted")
	}
	if !a.Active() {
		return Thread{}, errors.New("this card is already closed")
	}
	t, err := m.Get(a.ThreadID)
	if err != nil {
		return Thread{}, err
	}
	if t.Status == "archived" {
		return Thread{}, errors.New("session is archived")
	}
	if r, ok := m.liveRun(a.ThreadID); ok && r.busy == 1 {
		return Thread{}, errors.New("this session is running again already")
	}
	if _, err := m.ResolveAsk(id, "done", by, note); err != nil {
		return Thread{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// The owner restarting by hand clears the automatic budget: the next transient
	// failure gets its own two attempts.
	delete(m.retries, a.ThreadID)
	if a.RunID == "" || m.requeueTurn(m.lastTurnID(a.RunID)) == 0 {
		m.resumeNote(a.ThreadID, a.Detail)
	}
	if err := m.launch(t); err != nil {
		return Thread{}, err
	}
	log.Printf("ask %s: session %s restarted by the owner", id, a.ThreadID)
	return m.Get(a.ThreadID)
}
