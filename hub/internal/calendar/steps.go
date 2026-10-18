package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"life/hub/internal/store"
)

// Steps: an item's steps live in item_steps under it (store.Step). The item
// stays the one thing that fires, pushes, speaks and draws a row; each step is
// closed on its own (CloseStep), and the item's fate follows them
// (store.StepsFate) - all did is done, some skipped is done with the skipped
// part on its row, all skipped is skipped. An item closed or missed as a whole
// closes its open steps the same way (closeOpenSteps); a repeat's next row
// gets the same titles, all open.

// cleanSteps: the titles, trimmed, blanks dropped, each up to 200 bytes.
func cleanSteps(steps []store.Step) []string {
	var out []string
	for _, s := range steps {
		t := strings.TrimSpace(s.Title)
		if t == "" {
			continue
		}
		if len(t) > 200 {
			t = t[:200]
		}
		out = append(out, t)
	}
	return out
}

// putSteps writes an item's step list, all open, each insert an event.
func (c *Calendar) putSteps(id string, titles []string, now time.Time) error {
	for i, t := range titles {
		sid := store.StepID(id, i+1)
		if _, err := store.StepLog.Move(c.db, sid, "hub", "", true, now,
			`INSERT INTO item_steps (id,item_id,n,title,state,created_at,updated_at) VALUES (?,?,?,?,'open',?,?)`,
			sid, id, i+1, t, ts(now), ts(now)); err != nil {
			return err
		}
	}
	return nil
}

// SetSteps replaces an open item's step list (empty = no steps). Refused once
// any step is closed: that record stays.
func (c *Calendar) SetSteps(id string, titles []string) (Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return Item{}, errors.New("no such item")
	}
	if it.State != "scheduled" && it.State != "fired" {
		return Item{}, errors.New("only an open item's steps can be set")
	}
	for _, s := range it.Steps {
		if s.State != "open" {
			return Item{}, fmt.Errorf("step %d is already %s: close or reopen steps one by one (`lifectl cal %s step <n> ...`)", s.N, s.State, id)
		}
	}
	var in []store.Step
	for _, t := range titles {
		in = append(in, store.Step{Title: t})
	}
	if _, err := c.db.Exec(`DELETE FROM item_steps WHERE item_id=?`, id); err != nil {
		return Item{}, err
	}
	if err := c.putSteps(id, cleanSteps(in), c.Now()); err != nil {
		return Item{}, err
	}
	return c.Get(id)
}

// stepState: the state a step outcome closes it with.
var stepState = map[string]string{"done": "done", "wont": "skipped", "open": "open"}

// CloseStep closes (done | wont) or reopens (open) one step. The last open
// step closed closes the item with the steps' fate; a step reopened on a
// closed item reopens the item (its other steps keep their marks). A step
// changed on an item already closed re-derives the item's fate in place.
func (c *Calendar) CloseStep(id string, n int, outcome, by string) (Item, error) {
	to, ok := stepState[outcome]
	if !ok {
		return Item{}, errors.New("outcome must be done|wont|open")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return Item{}, errors.New("no such item")
	}
	if n < 1 || n > len(it.Steps) {
		return Item{}, fmt.Errorf("%s has no step %d", id, n)
	}
	if it.Steps[n-1].State == to {
		return it, nil
	}
	now := c.Now()
	sid := store.StepID(id, n)
	if to == "open" {
		_, err = store.StepLog.Move(c.db, sid, by, "", false, now,
			`UPDATE item_steps SET state='open', updated_at=?, resolved_at=NULL, resolved_by=NULL WHERE id=?`, ts(now), sid)
	} else {
		_, err = store.StepLog.Move(c.db, sid, by, "", false, now,
			`UPDATE item_steps SET state=?, updated_at=?, resolved_at=?, resolved_by=? WHERE id=?`, to, ts(now), ts(now), by, sid)
	}
	if err != nil {
		return Item{}, err
	}
	steps := store.LoadSteps(c.db, id)[id]
	closed := it.State == "done" || it.State == "dismissed"
	switch state, note, done := store.StepsFate(steps); {
	case to == "open" && closed:
		c.resolveLocked(it, "scheduled", by, "step reopened: "+steps[n-1].Title, false)
	case done && !closed:
		c.resolveLocked(it, state, by, note, false)
	case done && closed:
		c.setItem(id, by, note, `UPDATE items SET state=?, updated_at=?, resolution=? WHERE id=?`, state, ts(now), note, id)
	}
	return c.Get(id)
}

// closeOpenSteps: the item closed as a whole closes its still-open steps the
// same way (Did it = every step did; Skip = every step skipped; missed =
// missed). Ones already closed keep their own mark.
func (c *Calendar) closeOpenSteps(id, to, by string, now time.Time) {
	c.db.Exec(`INSERT INTO item_step_events (step_id, ts, actor, from_state, to_state, note)
		SELECT id, ?, ?, 'open', ?, 'item closed' FROM item_steps WHERE item_id=? AND state='open'`, ts(now), by, to, id)
	c.db.Exec(`UPDATE item_steps SET state=?, updated_at=?, resolved_at=?, resolved_by=? WHERE item_id=? AND state='open'`, to, ts(now), ts(now), by, id)
}

// reopenSteps: an item reopened with every step closed (the undo of its
// close) reopens them all; one reopened by its own step keeps the others.
func (c *Calendar) reopenSteps(id, by string, now time.Time) {
	var open int
	c.db.QueryRow(`SELECT COUNT(*) FROM item_steps WHERE item_id=? AND state='open'`, id).Scan(&open)
	if open > 0 {
		return
	}
	c.db.Exec(`INSERT INTO item_step_events (step_id, ts, actor, from_state, to_state, note)
		SELECT id, ?, ?, state, 'open', 'item reopened' FROM item_steps WHERE item_id=?`, ts(now), by, id)
	c.db.Exec(`UPDATE item_steps SET state='open', updated_at=?, resolved_at=NULL, resolved_by=NULL WHERE item_id=?`, ts(now), id)
}

// stampSteps puts each row's steps on it - one read for the whole view. A
// coming row shows its chain's titles, all open.
func (c *Calendar) stampSteps(v *View) {
	var ids []string
	each := func(f func(e *Entry)) {
		for _, es := range [][]Entry{v.Anytime, v.Overdue, v.Due, v.Soon} {
			for i := range es {
				f(&es[i])
			}
		}
		for _, d := range v.Days {
			for i := range d.Entries {
				f(&d.Entries[i])
			}
		}
	}
	item := func(e *Entry) string {
		if strings.HasPrefix(e.Ref, "cal:") {
			return strings.TrimPrefix(e.Ref, "cal:")
		}
		return ""
	}
	each(func(e *Entry) {
		if id := item(e); id != "" {
			ids = append(ids, id)
		}
	})
	if len(ids) == 0 {
		return
	}
	got := store.LoadSteps(c.db, ids...)
	each(func(e *Entry) {
		steps := got[item(e)]
		if len(steps) == 0 {
			return
		}
		if e.Coming {
			fresh := make([]store.Step, len(steps))
			for i, s := range steps {
				fresh[i] = store.Step{N: s.N, Title: s.Title, State: "open", Mark: "todo"}
			}
			steps = fresh
		}
		e.Steps, e.Part = steps, store.StepsPart(steps)
	})
}
