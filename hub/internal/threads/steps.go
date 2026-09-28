package threads

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// StepCount: how many steps belong under ONE message of the chat — the
// headline of a run block ("450 tool calls"), computed by the hub instead of
// by whoever happens to have the events loaded.
//
// Why the hub owns it: a single turn regularly runs into the hundreds of
// events (past a thousand at times), so any client that counts what it has
// fetched is one page size away from being wrong. The events themselves are
// ours — every one Claude streams is written to thread_events at the moment
// it happens and nothing deletes them — so a count is a SQL question, and a
// client fetches BODIES only for the block someone actually opens.
//
// FirstID/LastID bound that block, so opening it is one ranged read
// (`events?since=first-1&before=last+1`) no matter how long the run is.
type StepCount struct {
	MessageID int64  `json:"message_id"`
	RunID     string `json:"run_id"`
	Tools     int    `json:"tools"`
	Thoughts  int    `json:"thoughts"`
	Steps     int    `json:"steps"`
	FirstID   int64  `json:"first_id"`
	LastID    int64  `json:"last_id"`
	// Segments: the block CUT at the cards the agent raised while it ran.
	// Empty when it raised none — then the whole block is one fold, as before.
	//
	// Session cards appear in the middle of the tool chain, so the owner can
	// approve or act on them while the agent continues to work.
	// A card is not the wrap-up message; it is a thing the agent hands over at
	// the step it had it, and the tool chain goes on underneath. So the hub
	// splits the chain here, once, and both surfaces draw the same cut: each
	// segment is its own fold with its OWN count, and `ref` names the card
	// drawn between it and the next one.
	//
	// The counts have to come from here for the same reason the block's do: a
	// client that counted the events it happens to hold would print a number
	// that moves as the run streams (the block above a steering message
	// counting down).
	Segments []Segment `json:"segments,omitempty"`
}

// Segment: one run of steps inside a block, and the card that closes it.
type Segment struct {
	// Ref: the card drawn AFTER these steps — `ask:<id>` or `action:<id>`.
	// "" on the last segment: the steps after the last card, still running.
	Ref      string `json:"ref,omitempty"`
	Tools    int    `json:"tools"`
	Thoughts int    `json:"thoughts"`
	Steps    int    `json:"steps"`
	FirstID  int64  `json:"first_id"`
	LastID   int64  `json:"last_id"`
}

func (s *Segment) add(kind string, id int64) {
	switch kind {
	case "tool_use":
		s.Tools++
	case "thinking":
		s.Thoughts++
	}
	s.Steps++
	if s.FirstID == 0 || id < s.FirstID {
		s.FirstID = id
	}
	if id > s.LastID {
		s.LastID = id
	}
}

// Steps returns one row per starter message (the message that opened a run,
// or steered into one), in chat order.
//
// The split is the same rule both surfaces draw: a steering message shares
// its run_id with the message that started the turn, so each starter takes
// the events stamped between it and the next starter of that run, and the
// first starter also takes anything earlier. That is what steering means —
// the chain breaks where the message actually landed.
func (m *Manager) Steps(id string) ([]StepCount, error) {
	if !validID.MatchString(id) {
		return nil, errors.New("bad id")
	}
	type starter struct {
		id  int64
		ts  time.Time
		run string
	}
	rows, err := m.db.Query(`SELECT id, ts, run_id FROM thread_messages
		WHERE thread_id=? AND COALESCE(run_id,'') != '' AND role != 'claude' AND kind != 'error' ORDER BY ts, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byRun := map[string][]starter{}
	var order []starter
	for rows.Next() {
		var s starter
		var t string
		if err := rows.Scan(&s.id, &t, &s.run); err != nil {
			return nil, err
		}
		s.ts, _ = time.Parse(time.RFC3339Nano, t)
		byRun[s.run] = append(byRun[s.run], s)
		order = append(order, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Where each card the agent raised mid-run cuts its block. A card's run_id
	// is the process's (`r.id`); a starter's is the TURN's (`r.id`, `r.id-t2`,
	// …), so the match is on the base run, and the card then lands on the last
	// starter of that run at or before it — the same "where did this actually
	// happen" rule the events follow.
	//
	// A card raised AFTER the turn's reply landed is not mid-chain: it is the
	// reply. finishTurn mints a read card from a text reply (`replyCard`)
	// right after it writes the reply row, and that card belongs under the
	// "turn ended" line, placed by message_id — not cut into a block that,
	// for a turn with no tool calls, has nothing to draw (a "1 to read" with
	// no cell).
	replies, err := m.replyTimes(id)
	if err != nil {
		return nil, err
	}
	cuts := map[int64][]card{}
	for _, c := range m.cards(id) {
		var pick *starter
		for i, s := range order {
			if !sameRun(s.run, c.run) || s.ts.After(c.at) {
				continue
			}
			pick = &order[i]
		}
		if pick == nil {
			continue // raised outside every run: placed by message_id instead
		}
		landed := false
		for _, r := range replies {
			if sameRun(r.run, c.run) && !r.ts.Before(pick.ts) && !r.ts.After(c.at) {
				landed = true
				break
			}
		}
		if landed {
			continue // the turn had already replied: the card sits under that reply
		}
		cuts[pick.id] = append(cuts[pick.id], c)
	}

	// Only the skinny columns: a count must never pay for the bodies (the
	// events of this thread carry the tool output, megabytes of it).
	evs, err := m.db.Query(`SELECT id, run_id, ts, kind FROM thread_events WHERE thread_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer evs.Close()
	out := map[int64]*StepCount{}
	for _, s := range order {
		c := &StepCount{MessageID: s.id, RunID: s.run}
		// One segment per card, plus the tail after the last one.
		for _, k := range cuts[s.id] {
			c.Segments = append(c.Segments, Segment{Ref: k.ref})
		}
		if len(c.Segments) > 0 {
			c.Segments = append(c.Segments, Segment{})
		}
		out[s.id] = c
	}
	for evs.Next() {
		var eid int64
		var run, t, kind string
		if err := evs.Scan(&eid, &run, &t, &kind); err != nil {
			return nil, err
		}
		ss := byRun[run]
		if len(ss) == 0 {
			continue // an orphan run: no message of this thread opened it
		}
		ets, _ := time.Parse(time.RFC3339Nano, t)
		// The last starter at or before this event; the first takes anything
		// stamped earlier than itself.
		pick := ss[0]
		for _, s := range ss[1:] {
			if !s.ts.After(ets) {
				pick = s
			}
		}
		c := out[pick.id]
		switch kind {
		case "tool_use":
			c.Tools++
		case "thinking":
			c.Thoughts++
		}
		c.Steps++
		if c.FirstID == 0 || eid < c.FirstID {
			c.FirstID = eid
		}
		if eid > c.LastID {
			c.LastID = eid
		}
		// …and into the segment it ran in: everything stamped before the first
		// card is segment 0, up to the tail after the last card.
		if len(c.Segments) > 0 {
			seg := 0
			for _, k := range cuts[pick.id] {
				if !k.at.After(ets) {
					seg++
				}
			}
			c.Segments[seg].add(kind, eid)
		}
	}
	if err := evs.Err(); err != nil {
		return nil, err
	}
	list := make([]StepCount, 0, len(out))
	for _, c := range out {
		// A card raised after the last step leaves an empty tail; drop it so
		// no surface draws a fold with nothing in it.
		for len(c.Segments) > 0 && c.Segments[len(c.Segments)-1].Steps == 0 && c.Segments[len(c.Segments)-1].Ref == "" {
			c.Segments = c.Segments[:len(c.Segments)-1]
		}
		list = append(list, *c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].MessageID < list[j].MessageID })
	return list, nil
}

// reply: when a turn's claude row landed, and in which run.
type reply struct {
	ts  time.Time
	run string
}

// replyTimes: every reply row of the thread, oldest first. A card stamped at
// or after one of these (same run, after the starter it would cut) was raised
// by the turn's ending, not mid-chain.
func (m *Manager) replyTimes(threadID string) ([]reply, error) {
	rows, err := m.db.Query(`SELECT ts, run_id FROM thread_messages
		WHERE thread_id=? AND role='claude' AND COALESCE(run_id,'') != '' ORDER BY ts, id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reply
	for rows.Next() {
		var t string
		var r reply
		if err := rows.Scan(&t, &r.run); err != nil {
			return nil, err
		}
		r.ts, _ = time.Parse(time.RFC3339Nano, t)
		out = append(out, r)
	}
	return out, rows.Err()
}

// card: one thing raised mid-run that a surface draws as a cell of the chat.
type card struct {
	ref string
	at  time.Time
	run string
}

// sameRun: a starter's run_id is the turn's (`<run>`, `<run>-t2`, …) while a
// card records the process's. One process, one chain.
func sameRun(msgRun, cardRun string) bool {
	return cardRun != "" && (msgRun == cardRun || strings.HasPrefix(msgRun, cardRun+"-t"))
}

// cards: every ask and proposal this thread raised inside a run, oldest first.
// State is deliberately not filtered (only a superseded ask, which no surface
// draws, is dropped): a card the owner answers mid-chain must not JUMP when
// they answer it. A client that does not draw one — a dismissed ask leaves the
// transcript entirely — folds that segment into the next instead.
//
// Asks and proposals are both `items` rows, so one query reads
// them; a fired step was never raised in a run and has no run_id.
func (m *Manager) cards(threadID string) []card {
	var out []card
	rows, err := m.db.Query(`SELECT src, id, created_at, COALESCE(run_id,'') FROM items
		WHERE src IN ('ask','action') AND thread_id=? AND state != 'superseded' AND COALESCE(run_id,'') != ''`, threadID)
	if err == nil {
		for rows.Next() {
			var src, id, at, run string
			if rows.Scan(&src, &id, &at, &run) == nil {
				t, _ := time.Parse(time.RFC3339Nano, at)
				out = append(out, card{ref: src + ":" + id, at: t, run: run})
			}
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}
