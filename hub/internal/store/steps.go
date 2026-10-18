package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Steps: one calendar item can hold several steps, each closed on its own (a
// morning routine where one part is done and another skipped is not a skipped
// routine). One item, one push, one row; its steps are rows of `item_steps`
// under it, in order, each with its own state and its own event log
// (StepLog). The table belongs to the calendar (calendar.Schema creates it);
// it is read here so a card (threads) and a calendar row draw the same list.
//
// A step's state: open | done | skipped | missed. The item's fate follows
// them (StepsFate).
type Step struct {
	N     int    `json:"n"` // 1-based, the order they are done in
	Title string `json:"title"`
	State string `json:"state"` // open | done | skipped | missed
	// Mark: the glyph in the calendar's one vocabulary (CalMark): done,
	// wont (skipped or missed), todo.
	Mark       string     `json:"mark"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
}

// UnmarshalJSON: a step posted as its bare title (`steps: ["Stretch",
// "Wash your face"]` on POST /calendar) or as the object.
func (s *Step) UnmarshalJSON(b []byte) error {
	var title string
	if json.Unmarshal(b, &title) == nil {
		*s = Step{Title: title}
		return nil
	}
	type plain Step
	return json.Unmarshal(b, (*plain)(s))
}

// StepLog: the steps table's state column and its event log. A step's id is
// `<item id>.<n>`.
var StepLog = Log{Table: "item_steps", Events: "item_step_events", Key: "step_id", State: "state"}

// StepID: a step's row id.
func StepID(item string, n int) string { return fmt.Sprintf("%s.%d", item, n) }

func stepMark(state string) string {
	switch state {
	case "done":
		return "done"
	case "skipped", "missed":
		return "wont"
	}
	return "todo"
}

// LoadSteps: the steps of each of ids, in order (ids with none are absent).
// A database without the table (a package tested on its own) has none.
func LoadSteps(db *DB, ids ...string) map[string][]Step {
	out := map[string][]Step{}
	if len(ids) == 0 {
		return out
	}
	q := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Query(`SELECT item_id, n, title, state, COALESCE(resolved_at,''), COALESCE(resolved_by,'') FROM item_steps WHERE item_id IN (`+q+`) ORDER BY item_id, n`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, at, by string
		var s Step
		if rows.Scan(&id, &s.N, &s.Title, &s.State, &at, &by) != nil {
			continue
		}
		s.Mark, s.ResolvedBy = stepMark(s.State), by
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			s.ResolvedAt = &t
		}
		out[id] = append(out[id], s)
	}
	return out
}

// StepTitles: the step list as its titles (what a repeat's next row carries).
func StepTitles(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Title
	}
	return out
}

// StepsFate: what the item's steps say about the item once none is open -
// all skipped (or missed) is `dismissed`, anything done is `done` - and the
// words that keep the skipped part visible on the closed row ("did: ...;
// skipped: ..."). closed=false while a step is still open.
func StepsFate(steps []Step) (state, note string, closed bool) {
	if len(steps) == 0 {
		return "", "", false
	}
	by := map[string][]string{}
	for _, s := range steps {
		if s.State == "open" {
			return "", "", false
		}
		by[s.State] = append(by[s.State], s.Title)
	}
	var parts []string
	for _, st := range []string{"done", "skipped", "missed"} {
		if len(by[st]) > 0 {
			word := map[string]string{"done": "did", "skipped": "skipped", "missed": "missed"}[st]
			parts = append(parts, word+": "+strings.Join(by[st], ", "))
		}
	}
	state = "dismissed"
	if len(by["done"]) > 0 {
		state = "done"
	}
	return state, strings.Join(parts, "; "), true
}

// StepsPart: "N of M" done, for a row whose steps are partly closed - the
// skipped part visible on the bar itself (both grids print it after the
// title). Empty with no steps, none closed, or every one done.
func StepsPart(steps []Step) string {
	done, closed := 0, 0
	for _, s := range steps {
		if s.State != "open" {
			closed++
		}
		if s.State == "done" {
			done++
		}
	}
	if len(steps) == 0 || closed == 0 || done == len(steps) {
		return ""
	}
	return fmt.Sprintf("%d of %d", done, len(steps))
}
