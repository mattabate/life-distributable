package threads

import (
	"fmt"
	"log"
	"strings"
	"time"

	"life/hub/internal/store"
)

// changedCap / changedChars: how much of the newer history a run is handed.
// Fifteen notes of ≤600 characters is a few thousand tokens — far less than
// one wrong card costs the owner in trust.
const (
	changedCap   = 15
	changedChars = 600
)

// changedSince: the block a scheduled or dated run is delivered under — every
// decision/context/intent note on its goal newer than its instructions or the
// goal's digest, whichever is older.
//
// A queued run is frozen instructions: a run written weeks ago can wake and
// ask for something that was automated, done or declined since, because its
// lean wake reads an old digest plus a few of its own newest notes. So the
// hub, not the session's diligence, puts the newer facts in front of the run.
func (m *Manager) changedSince(p Prompt, threadID string, newOnly bool) string {
	goal := p.GoalID
	if goal == "" && !newOnly {
		if t, err := m.Get(threadID); err == nil {
			goal = t.GoalID
		}
	}
	if goal == "" {
		return ""
	}
	written := m.instructionsWritten(p)
	var digestAt string
	m.db.QueryRow(`SELECT COALESCE(digest_at,'') FROM goals WHERE id=?`, goal).Scan(&digestAt)
	since := ts(written)
	if digestAt < since {
		since = digestAt
	}
	rows, err := m.db.Query(`SELECT id, created_at, kind, text FROM goal_notes
		WHERE goal_id=? AND kind IN ('decision','context','intent') AND created_at > ?
		ORDER BY id DESC`, goal, since)
	if err != nil {
		return "" // no goals table (tests of other packages) — nothing to add
	}
	var lines []string
	total := 0
	for rows.Next() {
		var id int64
		var at, kind, text string
		if rows.Scan(&id, &at, &kind, &text) != nil {
			continue
		}
		total++
		if len(lines) < changedCap {
			one := strings.Join(strings.Fields(text), " ")
			lines = append(lines, fmt.Sprintf("- #%d %s %s: %s", id, firstN(at, 10), kind, truncate(one, changedChars)))
		}
	}
	if err := rows.Err(); err != nil {
		// A partial read would print a wrong count as if complete.
		log.Printf("changed since for %s: %v", goal, err)
		rows.Close()
		return ""
	}
	rows.Close()
	if total == 0 {
		return ""
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "[CHANGED SINCE THESE INSTRUCTIONS. The instructions below were written %s; goal `%s`'s digest is dated %s. These %d decision/context/intent notes are newer than one or both, newest first. Instructions go stale — a thing gets automated, done or declined, a number is revised. Before acting on any premise below, and before raising any ask or rec, check it against these notes and the live data. Where a note contradicts the instructions, the note wins: skip the stale step, and rewrite the instructions in this run (`lifectl cal <id> set --detail @file` for a calendar item, `lifectl thread <id> schedule <cadence> \"…\"` for a check-in). Full text of one: `ops/db.sh \"select text from goal_notes where id=N\"`.\n",
		firstN(ts(written), 10), goal, orNone(firstN(digestAt, 10)), total)
	b.WriteString(strings.Join(lines, "\n"))
	if total > len(lines) {
		fmt.Fprintf(b, "\n- … %d older: `ops/db.sh \"select id,created_at,kind,substr(text,1,300) from goal_notes where goal_id='%s' and kind in ('decision','context','intent') and created_at>'%s' order by id desc\"`", total-len(lines), goal, since)
	}
	b.WriteString("]")
	return b.String()
}

// instructionsWritten: when the text being delivered was set. A standing
// check-in's child is minted at fire time, so its parent row dates the text
// (re-set on every schedule change). A calendar item's wake row is re-minted
// whenever the item is edited or moved, so the item's own creation is the
// conservative date: it can only show more.
func (m *Manager) instructionsWritten(p Prompt) time.Time {
	if kind, id := store.SplitRef(p.InReplyTo); kind == "cal" {
		var c string
		if m.db.QueryRow(`SELECT created_at FROM items WHERE id=? AND src='cal'`, id).Scan(&c) == nil {
			if t := parseTS(c); !t.IsZero() {
				return t
			}
		}
	}
	if p.Parent != "" {
		if parent, err := m.GetPrompt(p.Parent); err == nil {
			return parent.CreatedAt
		}
	}
	return p.CreatedAt
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "never (no digest)"
	}
	return s
}
