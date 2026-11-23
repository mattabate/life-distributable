package threads

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// A session's to-do list. Every message reaches the session at once, the
// owner's or a teammate's; the session keeps one task in hand and files a NEW
// task here in one step, where the owner can see it under the session's name.
//
// It is not Coming up. A prompt with a time (prompts.go) waits for the world;
// a to-do is work that can run now, so the list is empty whenever the session
// is idle. A turn that ends with an open item is followed at once by one that
// takes it (todoWake). An item that truly waits on something outside says
// what (`waiting`), stays shown, and wakes nothing.

// TodosSchema: the list's own DDL (store/migrate.go).
var TodosSchema = []string{`CREATE TABLE IF NOT EXISTS thread_todos (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	thread_id TEXT NOT NULL, created_at TEXT NOT NULL,
	text TEXT NOT NULL,
	asked_by TEXT NOT NULL DEFAULT '',      -- who the task came from: the owner, a teammate's name, '' = the session
	state TEXT NOT NULL DEFAULT 'open',     -- open | waiting | done | dropped
	note TEXT NOT NULL DEFAULT '',          -- waiting: on what; done/dropped: what happened
	closed_at TEXT NOT NULL DEFAULT '',
	wakes INTEGER NOT NULL DEFAULT 0        -- turns the hub opened for it while open (todoWakeMax)
)`,
	`CREATE INDEX IF NOT EXISTS thread_todos_thread ON thread_todos(thread_id, state)`,
}

// Todo is one row of a session's list.
type Todo struct {
	ID        int64      `json:"id"`
	ThreadID  string     `json:"thread_id"`
	CreatedAt time.Time  `json:"created_at"`
	Text      string     `json:"text"`
	AskedBy   string     `json:"asked_by"`
	State     string     `json:"state"`
	Note      string     `json:"note"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// ErrNoTodo: the list has no such item.
var ErrNoTodo = errors.New("no such to-do")

// todoWakeMax: how many turns in a row the hub opens for one open item. An
// item still open after that stays on the list, in the owner's sight, and the
// next turn of any kind shows it to the session again (todoBlock); the cap is
// only against a session that ends each turn without touching it.
const todoWakeMax = 2

// todoWakeRun: how many turns in a row the hub opens on a session's list with
// nothing from outside in between (the owner, a teammate, a check-in). A
// session that files a new item every turn cannot keep itself running past it.
const todoWakeRun = 4

// todoBy: who a task came from, in one spelling: the owner is "owner", as on
// every other row the hub keeps.
func todoBy(by string) string {
	by = strings.TrimSpace(by)
	if l := strings.ToLower(by); l == "owner" || l == "the owner" {
		return "owner"
	}
	return by
}

// TodoAdd files a task on a session's list.
func (m *Manager) TodoAdd(threadID, text, by string) (Todo, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Todo{}, errors.New("a to-do needs its words")
	}
	if _, err := m.Get(threadID); err != nil {
		return Todo{}, err
	}
	now := time.Now()
	res, err := m.db.Exec(`INSERT INTO thread_todos (thread_id, created_at, text, asked_by) VALUES (?,?,?,?)`, threadID, ts(now), text, todoBy(by))
	if err != nil {
		return Todo{}, err
	}
	id, _ := res.LastInsertId()
	return Todo{ID: id, ThreadID: threadID, CreatedAt: now, Text: text, AskedBy: todoBy(by), State: "open"}, nil
}

// TodoSet moves an item: done or dropped (note = what happened), waiting
// (note = the outside thing it waits on, required), or open again. A close
// with no note keeps the one it had (what it waited on), and only a real
// change of state gives an item its wakes back.
func (m *Manager) TodoSet(threadID string, id int64, state, note string) error {
	note = strings.TrimSpace(note)
	closed := ""
	switch state {
	case "done", "dropped":
		closed = ts(time.Now())
	case "waiting":
		if note == "" {
			return errors.New("a waiting to-do says what it waits on; work that can run now stays open")
		}
		if selfWait(note) {
			return errors.New("a to-do never waits on the person's own land, build, commit, push or tests: those run now, in this turn, and the item closes when they are done (`todo done`); wait only on the owner, a teammate or the world")
		}
	case "open":
	default:
		return fmt.Errorf("state %q: open, waiting, done or dropped", state)
	}
	keep := 0
	if closed != "" && note == "" {
		keep = 1
	}
	res, err := m.db.Exec(`UPDATE thread_todos SET note=CASE WHEN ?1=1 THEN note ELSE ?2 END, closed_at=?3,
		wakes=CASE WHEN state=?4 THEN wakes ELSE 0 END, state=?4 WHERE id=?5 AND thread_id=?6`, keep, note, closed, state, id, threadID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoTodo
	}
	return nil
}

// selfWait: a wait note that names the person's own next step, "the land",
// "build finishing", "my commit", rather than something outside. A note that
// names the owner, a card or a gate is their move, so it passes.
// A note naming a teammate, a maintainer, CI or a review is a wait outside.
var selfWaitRe = regexp.MustCompile(`(?i)^((waiting|wait)\s+)?((on|for|until)\s+)?((my|our|the|a)\s+)?(own\s+)?(land(ing)?|build|commit|push|make( check| build)?|ship|tests?|deploy)\b`)
var outsideRe = regexp.MustCompile(`(?i)\b(owner|their|them|cards?|gates?|approv\w*|teammates?|maintainers?|upstream|vendors?|review\w*|ci|notifications?)\b`)

func selfWait(note string) bool {
	return selfWaitRe.MatchString(note) && !outsideRe.MatchString(note)
}

// Todos: a session's list, open first, then waiting, oldest first in each.
// all=true adds what was closed, newest last.
func (m *Manager) Todos(threadID string, all bool) ([]Todo, error) {
	q := `SELECT id, thread_id, created_at, text, asked_by, state, note, closed_at FROM thread_todos WHERE thread_id=?`
	if !all {
		q += ` AND state IN ('open','waiting')`
	}
	q += ` ORDER BY CASE state WHEN 'open' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END, id`
	rows, err := m.db.Query(q, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		var t Todo
		var created, closed string
		if err := rows.Scan(&t.ID, &t.ThreadID, &created, &t.Text, &t.AskedBy, &t.State, &t.Note, &closed); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTS(created)
		if closed != "" {
			c := parseTS(closed)
			t.ClosedAt = &c
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// todosByThread: every session's open and waiting items in one read, for the
// list of threads.
func (m *Manager) todosByThread() map[string][]Todo {
	rows, err := m.db.Query(`SELECT id, thread_id, created_at, text, asked_by, state, note FROM thread_todos WHERE state IN ('open','waiting')
		ORDER BY CASE state WHEN 'open' THEN 0 ELSE 1 END, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][]Todo{}
	for rows.Next() {
		var t Todo
		var created string
		if rows.Scan(&t.ID, &t.ThreadID, &created, &t.Text, &t.AskedBy, &t.State, &t.Note) != nil {
			continue
		}
		t.CreatedAt = parseTS(created)
		out[t.ThreadID] = append(out[t.ThreadID], t)
	}
	return out
}

// todoLines renders a list for the session that owns it.
func todoLines(list []Todo) string {
	var b strings.Builder
	for _, t := range list {
		fmt.Fprintf(&b, "  %d. %s", t.ID, strings.ReplaceAll(t.Text, "\n", " "))
		if t.AskedBy != "" {
			by := t.AskedBy
			if by == "owner" {
				by = "the owner"
			}
			b.WriteString(" (from " + by + ")")
		}
		if t.State == "waiting" {
			b.WriteString(" (WAITING on " + t.Note + ")")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// todoBlock: the session's own list at the head of a turn, "" when it is
// empty. A waiting item is shown so the turn that brings what it waited for
// reopens it.
func (m *Manager) todoBlock(threadID string) string {
	list, err := m.Todos(threadID, false)
	if err != nil || len(list) == 0 {
		return ""
	}
	return "[Your to-do list. The owner sees it under your name. Open items are work that can run now: take them in order after the task in hand, in this same run, and close each as you go (`lifectl todo done <n> \"what happened\"`). A waiting one whose wait has ended: `lifectl todo open <n>`.]\n" + todoLines(list) + "\n"
}

// todoWake: called as a turn ends with nothing else waiting to open the next
// one. With an item open on the session's list it records the message that
// opens the next turn on it and reports true; the caller delivers it like any
// held message. Caller holds m.mu.
func (m *Manager) todoWake(threadID string) bool {
	var run int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=?1 AND kind='todo'
		AND id > COALESCE((SELECT MAX(id) FROM thread_messages WHERE thread_id=?1 AND (role='owner' OR kind IN ('checkin','relay'))),0)`, threadID).Scan(&run)
	if run >= todoWakeRun {
		log.Printf("thread %s: %d turns in a row opened on its to-do list; the rest waits for the next message", threadID, run)
		return false
	}
	var open, spent int
	if err := m.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(wakes>=?),0) FROM thread_todos WHERE thread_id=? AND state='open'`, todoWakeMax, threadID).Scan(&open, &spent); err != nil || open == 0 {
		return false
	}
	if spent == open {
		log.Printf("thread %s: %d to-do(s) still open after %d wakes each; left on the list", threadID, open, todoWakeMax)
		return false
	}
	text := "[The hub, not the owner: your turn ended with work on your to-do list that can run now. A session does not rest with an open item. Take the first one now and work down the list in this same run. For each: do it and `lifectl todo done <n> \"what happened\"`; or, only if it truly waits on something outside you, `lifectl todo wait <n> \"<what it waits on>\"`; or `lifectl todo drop <n> \"why\"`. Tell the owner only what they need to hear; nothing for them is a bare `[end]`.]"
	if _, err := m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, attachments, queued, author) VALUES (?,?,?,?,?,'[]',1,'hub')`, threadID, ts(time.Now()), "system", "todo", text); err != nil {
		log.Printf("thread %s: to-do wake: %v", threadID, err)
		return false
	}
	m.db.Exec(`UPDATE thread_todos SET wakes=wakes+1 WHERE thread_id=? AND state='open'`, threadID)
	log.Printf("thread %s: turn ended with %d open to-do(s): opening the next turn on them", threadID, open)
	return true
}

// todoTurn: whether the turn that just ended is followed by one on the list.
// Not after a capped run (a check-in's): the list must not relaunch it with no
// cap, and the items stay in the owner's sight for the next ordinary turn.
// Caller holds m.mu.
func (m *Manager) todoTurn(r run) bool {
	t, err := m.Get(r.thread)
	if err != nil || m.capUSD(t, r.trigger) > 0 {
		return false
	}
	return m.todoWake(r.thread)
}
