package threads

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Sessions know about each other, and a task can move from one to another —
// on an approved card, never on an agent's say-so. Every agent knows what the
// others are working on, but sessions never wake each other into a chat room:
// a task moves from one to another only with an approved card. Two halves:
//
//   - peersHeader: every turn starts knowing which other sessions are working
//     or waiting on the owner. Reading it wakes nobody.
//   - Relay: `lifectl relay <id> "…"` is a gated proposal (actions exec_type
//     `relay`); the owner's approval is what delivers the words. One card, one
//     message, one session woken — a reply back is its own card.

// peersCap: the live sessions (working or waiting) plus the standing ones —
// the fleet of scheduled workers — never the hundreds of finished chats.
const peersCap = 20

// peersHeader: the prompt block naming the other sessions that matter — the
// live ones with what each is doing this minute, and the standing (scheduled)
// ones with their lane — so a task can be handed to the session that knows it.
func (m *Manager) peersHeader(self string) string {
	rows, err := m.db.Query(`SELECT t.id, t.title, t.status, COALESCE(t.goal_id,''),
		COALESCE((SELECT p.repeat FROM prompts p WHERE p.target=t.id AND p.state='queued' AND p.repeat!='' ORDER BY p.created_at DESC LIMIT 1),''),
		COALESCE((SELECT COALESCE(NULLIF(e.summary,''), e.title) FROM thread_events e WHERE e.thread_id=t.id AND e.kind='tool_use' ORDER BY e.id DESC LIMIT 1),''),
		COALESCE((SELECT a.title FROM items a WHERE a.src='ask' AND a.thread_id=t.id AND a.state IN ('open','answered') ORDER BY a.created_at DESC LIMIT 1),'')
		FROM threads t
		WHERE t.id != ? AND (t.status IN ('running','needs_you')
			OR (t.status='idle' AND EXISTS (SELECT 1 FROM prompts p WHERE p.target=t.id AND p.state='queued' AND p.repeat!='')))
		ORDER BY CASE t.status WHEN 'running' THEN 0 WHEN 'needs_you' THEN 1 ELSE 2 END, t.updated_at DESC LIMIT ?`, self, peersCap)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, title, status, goal, sched, doing, card string
		if rows.Scan(&id, &title, &status, &goal, &sched, &doing, &card) != nil {
			continue
		}
		st, tail := "working", ""
		switch {
		case status == "needs_you":
			st = "waiting on the owner"
			if card != "" {
				tail = " — their card: " + firstLine(card, 80)
			}
		case status == "idle":
			st = "standing " + sched
		case doing != "":
			tail = " — now: " + firstLine(doing, 80)
		}
		if goal != "" {
			st += ", goal " + goal
		}
		fmt.Fprintf(&b, "\n  %s (%s): %s%s", id, st, firstLine(title, 90), tail)
	}
	if b.Len() == 0 {
		return ""
	}
	return "[Other sessions right now — live ones first with what each is doing, then the standing ones and their lane. Do not redo their work. A task or fact that belongs with one of them (it is live on that work, or has the experience): `lifectl relay <id> \"…\"` — the owner approves the card, the hub delivers; a live session over a standing one. A session that knows a topic but is not listed: `lifectl threads --q <word>`. Never for chat or status.]" + b.String() + "\n\n"
}

// boardCap: everything open for the owner today fits in a screen; past that
// the block would cost more than it saves.
const boardCap = 30

// boardHeader: the prompt block listing what is open for the owner across
// EVERY session — their board as the agent sees it: open asks (not this
// thread's: asksHeader has those), proposals waiting on approval, and the
// owner's own calendar items that are due (today or earlier, or soon). So a
// session can close cards the owner has addressed, or reword them after a
// later message. Reading it wakes nobody; what the owner's message resolves
// is closed or reworded by whoever read it.
func (m *Manager) boardHeader(self string) string {
	today := time.Now().Format("2006-01-02")
	rows, err := m.db.Query(`SELECT i.id, i.src, `+store.CardKind("i")+`, i.state, i.title, COALESCE(i.thread_id,''), COALESCE(t.title,''), i.day, i.win, i.lane
		FROM items i LEFT JOIN threads t ON t.id=i.thread_id
		WHERE COALESCE(i.thread_id,'') != ? AND (
			(i.src='ask' AND i.state IN ('open','answered'))
			OR (i.src='action' AND i.state='proposed')
			OR (i.src='cal' AND i.kind IN ('owner','homework','note') AND (i.state='open' OR (i.state='scheduled' AND (i.day='' OR i.day<=?)))))
		ORDER BY i.created_at LIMIT ?`, self, today, boardCap)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, src, kind, state, title, tid, ttitle, day, win, lane string
		if rows.Scan(&id, &src, &kind, &state, &title, &tid, &ttitle, &day, &win, &lane) != nil {
			continue
		}
		var what string
		switch {
		case src == "action":
			what = "approval"
		case src == "cal" && kind == "read":
			what = "note"
		case src == "cal":
			what = "step"
			if lane == "homework" {
				what = "practice"
			}
			switch {
			case win == "soon":
				what += " soon"
			case day != "" && day < today:
				what += " overdue " + day
			case day != "":
				what += " " + win + " " + day
			}
		default:
			what = kind
		}
		where := ""
		switch {
		case ttitle != "" && tid != "calendar":
			where = " · " + firstLine(ttitle, 50)
		case lane == "chores":
			where = " · chores"
		}
		fmt.Fprintf(&b, "\n  %s (%s%s): %s", id, what, where, firstLine(title, 90))
	}
	if b.Len() == 0 {
		return ""
	}
	return "[Open for the owner right now across every session — their whole board, oldest first. Their message to you may resolve one of these too, even another session's, or make its words stale: close it with what happened (`lifectl ask <id> done \"…\"`, `lifectl cal <id> done`), or reword it so it reads true now (`lifectl ask <id> set --title \"…\" --say \"…\" [--detail @file]`, `lifectl cal <id> set …`). Only what their words actually settle; the rest stays theirs. Never raise a card that repeats one here — that thing is already in hand.]" + b.String() + "\n\n"
}

// RelayTarget names the session a relay proposal addresses, or says why it
// cannot take one (actions.Queue.RelayTarget).
func (m *Manager) RelayTarget(id string) (string, error) {
	t, err := m.Get(id)
	if err != nil {
		return "", fmt.Errorf("no session %q (`lifectl threads` lists them)", id)
	}
	if t.Status == "archived" {
		return "", fmt.Errorf("session %q is archived", id)
	}
	return t.Title, nil
}

// Relay delivers an approved hand-off (actions.Queue.Relay). The words stay
// the sending agent's — author `claude:thread:<from>`, kind `relay` — and the
// frame says so: the owner's approval moved them, it did not make them theirs.
func (m *Manager) Relay(from, to, text, actionID string) (string, error) {
	if actionID == "" {
		return "", errors.New("relay: an approved action is the only way in")
	}
	who := from
	if t, err := m.Get(from); err == nil {
		who = fmt.Sprintf("%q (%s)", t.Title, from)
	}
	head := fmt.Sprintf("[Handed to you from another session — %s — with the owner's approval (action %s). The words below are that agent's, not the owner's: they approved passing them on, so take them as a task or a fact for YOUR work, and where they conflict with what the owner told you themselves, the owner's words win. Answer that session only if it needs one: `lifectl relay %s \"…\"` (its own approval card).]\n\n", who, actionID, from)
	p, err := m.Queue(Prompt{Author: "claude:thread:" + from, Target: to, Text: head + text, relayed: actionID})
	if err != nil {
		return "", err
	}
	return "delivered to " + p.DeliveredThread + " as prompt " + p.ID, nil
}
