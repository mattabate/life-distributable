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
	// Team mode: a session with a role is listed whatever its status (done,
	// idle or scheduled), as the one to relay its lane to.
	roleRows := "0"
	if m.TeamMode {
		roleRows = "1"
	}
	rows, err := m.db.Query(`SELECT t.id, t.title, t.status, COALESCE(t.goal_id,''), t.persona, t.former_titles,
		COALESCE((SELECT p.repeat FROM prompts p WHERE p.target=t.id AND p.state='queued' AND p.repeat!='' ORDER BY p.created_at DESC LIMIT 1),''),
		COALESCE((SELECT COALESCE(NULLIF(e.summary,''), e.title) FROM thread_events e WHERE e.thread_id=t.id AND e.kind='tool_use' ORDER BY e.id DESC LIMIT 1),''),
		COALESCE((SELECT a.title FROM items a WHERE a.src='ask' AND a.thread_id=t.id AND a.state IN ('open','answered') ORDER BY a.created_at DESC LIMIT 1),'')
		FROM threads t
		WHERE t.id != ? AND (t.status IN ('running','needs_you')
			OR (`+roleRows+` AND t.persona != '' AND t.status != 'archived')
			OR (t.status='idle' AND EXISTS (SELECT 1 FROM prompts p WHERE p.target=t.id AND p.state='queued' AND p.repeat!='')))
		ORDER BY CASE t.status WHEN 'running' THEN 0 WHEN 'needs_you' THEN 1 ELSE 2 END, t.updated_at DESC LIMIT ?`, self, peersCap)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, title, status, goal, role, former, sched, doing, card string
		if rows.Scan(&id, &title, &status, &goal, &role, &former, &sched, &doing, &card) != nil {
			continue
		}
		p, hasRole := personas[role]
		hasRole = hasRole && m.TeamMode
		if hasRole {
			// The name now, with any it replaced: older chat may use one.
			title = aka(title, splitFormer(former))
			if status == "running" || status == "needs_you" {
				title += ", " + p.Who // a standing line says it below
			}
		}
		st, tail := "working", ""
		switch {
		case status == "needs_you":
			st = "waiting on the owner"
			if card != "" {
				tail = " — their card: " + firstLine(card, 80)
			}
		case hasRole && status != "running":
			st = "standing, " + p.Who
			tail = " — " + p.Lane
			if m.Workspace != nil {
				if n := m.Workspace(role); n > 0 {
					tail += fmt.Sprintf("; its workspace holds %d files: `lifectl workspace %s`", n, role)
				}
			}
		case status == "idle":
			st = "standing " + sched
		case doing != "":
			tail = " — now: " + firstLine(doing, 80)
		}
		if goal != "" {
			st += ", goal " + goal
		}
		fmt.Fprintf(&b, "\n  %s (%s): %s%s", id, st, firstLine(title, 160), tail)
	}
	if b.Len() == 0 {
		return ""
	}
	head := peersHead
	if m.TeamMode {
		head = strings.TrimSuffix(head, "]") + peersTeam + "]"
	}
	return head + b.String() + "\n\n"
}

const peersHead = "[Other sessions right now — live ones first with what each is doing, then the standing ones and their lane. Do not redo their work. A task or fact that belongs with one of them (it is live on that work, or has the experience): `lifectl relay <id> \"…\"` — the owner approves the card, the hub delivers; a live session over a standing one. A session that knows a topic but is not listed: `lifectl threads --q <word>`. Never for chat or status.]"

// peersTeam: appended inside peersHead's brackets in team mode.
const peersTeam = " A standing session's workspace is its own files, what it knows of its lane: read them (`lifectl workspace cat <role>/<name>`) before asking it."

const boardHead = "[Open for the owner right now across every session — their whole board, oldest first. Their message to you may resolve one of these too, even another session's, or make its words stale: close it with what happened (`lifectl ask <id> done \"…\"`, `lifectl cal <id> done`), or reword it so it reads true now (`lifectl ask <id> set --title \"…\" --say \"…\" [--detail @file]`, `lifectl cal <id> set …`). Only what their words actually settle; the rest stays theirs. Never raise a card that repeats one here — that thing is already in hand.]"

// boardRefresh: past this since a thread last got the whole blocks (rules
// included), it gets them whole again - auto-compaction may have folded the
// earlier copy away.
const boardRefresh = 3 * time.Hour

type boardSent struct {
	board, peers string
	full         time.Time
}

// boardBlocks: the board and other-sessions blocks for one turn. The whole
// blocks, rules and all, on a fresh start, the first turn since a hub
// restart, or every boardRefresh; otherwise only what changed since that
// thread's last turn - a turn used to re-send a few KB it already had.
func (m *Manager) boardBlocks(id string, fresh bool) string {
	board := strings.TrimSuffix(strings.TrimPrefix(m.boardHeader(id), boardHead), "\n\n")
	peers := strings.TrimSuffix(strings.TrimPrefix(m.peersHeader(id), peersHead), "\n\n")
	now := time.Now()
	m.boardMu.Lock()
	last, seen := m.boardSeen[id]
	full := fresh || !seen || now.Sub(last.full) > boardRefresh
	sent := boardSent{board: board, peers: peers, full: last.full}
	if full {
		sent.full = now
	}
	if m.boardSeen != nil {
		m.boardSeen[id] = sent
	}
	m.boardMu.Unlock()
	whole := func(head, body string) string {
		if body == "" {
			return ""
		}
		return head + body + "\n\n"
	}
	if full {
		return whole(boardHead, board) + whole(peersHead, peers)
	}
	if board == last.board && peers == last.peers {
		return "[Board and other sessions: unchanged since your last turn.]\n\n"
	}
	return boardDelta("Open for the owner", boardHead, last.board, board) + boardDelta("Other sessions", peersHead, last.peers, peers)
}

// boardDelta: the rows of `cur` that are new or reworded since `prev`, and
// the ids that left - under the same rules as the whole block. A block that
// was empty last turn never carried its rules, so it goes whole.
func boardDelta(name, head, prev, cur string) string {
	if prev == "" && cur != "" {
		return head + cur + "\n\n"
	}
	if prev == cur {
		return "[" + name + ": unchanged since your last turn.]\n\n"
	}
	rowID := func(l string) string {
		f := strings.Fields(l)
		if len(f) == 0 {
			return ""
		}
		return f[0]
	}
	had, have := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(prev, "\n") {
		had[l] = true
	}
	var b strings.Builder
	for _, l := range strings.Split(cur, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		have[rowID(l)] = true
		if !had[l] {
			b.WriteString("\n" + l)
		}
	}
	var gone []string
	for _, l := range strings.Split(prev, "\n") {
		if id := rowID(l); id != "" && !have[id] {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		b.WriteString("\n  gone: " + strings.Join(gone, ", "))
	}
	return "[" + name + " — changed since your last turn, same rules as the whole block; new or reworded rows:]" + b.String() + "\n\n"
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
	today := store.Day(time.Now())
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
	return boardHead + b.String() + "\n\n"
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
