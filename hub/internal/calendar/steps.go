package calendar

import (
	"errors"
	"fmt"
	"log"
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

// cleanSteps: the titles cleaned like an item's (cleanTitle: one line, whole
// characters, up to 200 bytes), blanks dropped.
func cleanSteps(steps []store.Step) []string {
	var out []string
	for _, s := range steps {
		t := cleanTitle(s.Title)
		if t == "" {
			continue
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
	return c.closeStepLocked(it, n, to, by, "", false)
}

// closeStepLocked moves one step (c.mu held). quiet: the item's ask, if this
// closes it, goes through ClaimAsk - the caller's message is the news.
func (c *Calendar) closeStepLocked(it Item, n int, to, by, words string, quiet bool) (Item, error) {
	id := it.ID
	if n < 1 || n > len(it.Steps) {
		return Item{}, fmt.Errorf("%s has no step %d", id, n)
	}
	if it.Steps[n-1].State == to {
		return it, nil
	}
	now := c.Now()
	sid := store.StepID(id, n)
	var err error
	if to == "open" {
		_, err = store.StepLog.Move(c.db, sid, by, words, false, now,
			`UPDATE item_steps SET state='open', updated_at=?, resolved_at=NULL, resolved_by=NULL WHERE id=?`, ts(now), sid)
	} else {
		_, err = store.StepLog.Move(c.db, sid, by, words, false, now,
			`UPDATE item_steps SET state=?, updated_at=?, resolved_at=?, resolved_by=? WHERE id=?`, to, ts(now), ts(now), by, sid)
	}
	if err != nil {
		return Item{}, err
	}
	steps := store.LoadSteps(c.db, id)[id]
	closed := it.State == "done" || it.State == "dismissed"
	switch state, note, done := store.StepsFate(steps); {
	case to == "open" && closed:
		c.resolveLocked(it, "scheduled", by, "step reopened: "+steps[n-1].Title, quiet)
	case done && !closed:
		c.resolveLocked(it, state, by, note, quiet)
	case done && closed:
		c.setItem(id, by, note, `UPDATE items SET state=?, updated_at=?, resolution=? WHERE id=?`, state, ts(now), note, id)
	}
	return c.Get(id)
}

// splitStepID: "cal-1a2b3c4d.2" -> the item and the step's number.
func splitStepID(sid string) (string, int, bool) {
	i := strings.LastIndex(sid, ".")
	if i <= 0 {
		return "", 0, false
	}
	var n int
	if _, err := fmt.Sscanf(sid[i+1:], "%d", &n); err != nil || n < 1 {
		return "", 0, false
	}
	return sid[:i], n, true
}

// claimStep is threads.Manager.ClaimStep: one step marked in a message the
// owner SENT. The composer stacks a `step:<item>.<n>` reply per Did it / Skip;
// nothing moves until Send, then each closes here with the owner's words on
// its event, and the item closes quietly when its last step does.
func (c *Calendar) claimStep(sid, outcome, words string) {
	to, ok := stepState[outcome]
	id, n, okID := splitStepID(sid)
	if !ok || to == "open" || !okID {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	it, err := c.Get(id)
	if err != nil {
		return
	}
	if _, err := c.closeStepLocked(it, n, to, "owner", words, true); err != nil {
		log.Printf("calendar: step %s %s: %v", sid, outcome, err)
	}
}

// stepHeader is threads.Manager.StepHeader: the frame above the owner's words
// for one step marked in the message.
func (c *Calendar) stepHeader(sid, outcome string) string {
	id, n, ok := splitStepID(sid)
	if !ok {
		return ""
	}
	it, err := c.Get(id)
	if err != nil || n > len(it.Steps) {
		return ""
	}
	verdict := map[string]string{"done": "DID IT", "wont": "SKIPPED"}[outcome]
	if verdict == "" {
		verdict = "wrote about"
	}
	return fmt.Sprintf("[The owner marked step %d of calendar item %s %q (%s) - %q - %s (already recorded by the hub). Only that step; the item's other steps are as the card shows them.]\n",
		n, it.ID, it.Title, it.Day, it.Steps[n-1].Title, verdict)
}

// PlanStep skips (wont) or takes back (open) one step on chosen days of a
// repeat. A repeat has one row ahead at a time, so a step skipped on a later
// day of it is a row of item_step_plans on the chain's newest row; spawnNext
// carries the plans to the row it mints and applies that day's. The series
// itself is untouched: no rewrite now, no "add it back later". A day that
// already has its row is closed on that row (closeStepLocked). The newest plan
// for a (day, step) wins.
func (c *Calendar) PlanStep(id string, n int, days []string, outcome, by, note string) (Item, error) {
	to, ok := map[string]string{"wont": "skipped", "open": "open"}[outcome]
	if !ok {
		return Item{}, errors.New("a step ahead is skipped (wont) or taken back (open)")
	}
	if len(days) == 0 {
		return Item{}, errors.New("name the days")
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
	chain := c.chainFrom(it)
	last := chain[len(chain)-1]
	for _, d := range days {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return Item{}, fmt.Errorf("day %q must be YYYY-MM-DD", d)
		}
		if d < it.Day {
			return Item{}, fmt.Errorf("%s is before %s's day (%s)", d, id, it.Day)
		}
		var row *Item
		for i := range chain {
			if chain[i].Day == d {
				row = &chain[i]
			}
		}
		if row != nil {
			if _, err := c.closeStepLocked(*row, n, to, by, note, false); err != nil {
				return Item{}, err
			}
			continue
		}
		if last.Repeat == "" || d < last.Day {
			return Item{}, fmt.Errorf("%s is not a day of %s", d, id)
		}
		on, dom := last.Day, c.chainDay(last)
		for on != "" && on < d {
			on = nextDay(on, last.Repeat, dom)
		}
		if on != d {
			return Item{}, fmt.Errorf("%s is not a day of this %s repeat (the next one after it is %s)", d, last.Repeat, on)
		}
		if to == "skipped" {
			plan := map[int]string{n: "skipped"}
			for k, v := range c.plans(last.ID)[d] {
				if k != n {
					plan[k] = v
				}
			}
			skipped := 0
			for k := 1; k <= len(last.Steps); k++ {
				if plan[k] == "skipped" {
					skipped++
				}
			}
			if skipped == len(last.Steps) {
				return Item{}, fmt.Errorf("that skips every step on %s: skip the item on its day instead, or move it with `lifectl cal %s set --scope one`", d, last.ID)
			}
		}
		if _, err := c.db.Exec(`INSERT INTO item_step_plans (item_id, day, n, state, actor, note, created_at) VALUES (?,?,?,?,?,?,?)`,
			last.ID, d, n, to, by, strings.TrimSpace(note), ts(c.Now())); err != nil {
			return Item{}, err
		}
	}
	store.StampItem(c.db, last.ID)
	return c.Get(it.ID)
}

// chainFrom: it, then each next occurrence minted after it, in order.
func (c *Calendar) chainFrom(it Item) []Item {
	out := []Item{it}
	for i := 0; i < 400; i++ {
		var next string
		if c.db.QueryRow(`SELECT id FROM items WHERE src='cal' AND prev_id=? ORDER BY created_at LIMIT 1`, out[len(out)-1].ID).Scan(&next) != nil {
			break
		}
		nx, err := c.Get(next)
		if err != nil {
			break
		}
		out = append(out, nx)
	}
	return out
}

// plans: the newest planned state of each step, by day, on a row.
func (c *Calendar) plans(id string) map[string]map[int]string {
	return c.plansOf(id)[id]
}

func (c *Calendar) plansOf(ids ...string) map[string]map[string]map[int]string {
	out := map[string]map[string]map[int]string{}
	if len(ids) == 0 {
		return out
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := c.db.Query(`SELECT item_id, day, n, state FROM item_step_plans WHERE item_id IN (?`+strings.Repeat(",?", len(ids)-1)+`) ORDER BY id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, day, state string
		var n int
		if rows.Scan(&id, &day, &n, &state) != nil {
			continue
		}
		if out[id] == nil {
			out[id] = map[string]map[int]string{}
		}
		if out[id][day] == nil {
			out[id][day] = map[int]string{}
		}
		out[id][day][n] = state
	}
	return out
}

// carryPlans: the row spawnNext just minted takes the chain's plans from its
// own day on, and that day's skips are on its steps from the start (never
// every one: PlanStep refuses that).
func (c *Calendar) carryPlans(from string, next Item) {
	c.db.Exec(`UPDATE item_step_plans SET item_id=? WHERE item_id=? AND day>=?`, next.ID, from, next.Day)
	plan := c.plans(next.ID)[next.Day]
	if len(plan) == 0 {
		return
	}
	var notes = map[int]string{}
	rows, err := c.db.Query(`SELECT n, actor, note FROM item_step_plans WHERE item_id=? AND day=? ORDER BY id`, next.ID, next.Day)
	if err == nil {
		for rows.Next() {
			var n int
			var actor, note string
			if rows.Scan(&n, &actor, &note) == nil {
				notes[n] = strings.TrimSpace("planned by " + actor + ": " + note)
			}
		}
		rows.Close()
	}
	steps := store.LoadSteps(c.db, next.ID)[next.ID]
	skipped := 0
	for _, s := range steps {
		if plan[s.N] == "skipped" {
			skipped++
		}
	}
	if skipped == 0 || skipped == len(steps) {
		return
	}
	now := c.Now()
	for _, s := range steps {
		if plan[s.N] != "skipped" {
			continue
		}
		sid := store.StepID(next.ID, s.N)
		store.StepLog.Move(c.db, sid, "hub", strings.TrimSuffix(notes[s.N], ":"), false, now,
			`UPDATE item_steps SET state='skipped', updated_at=?, resolved_at=?, resolved_by='hub' WHERE id=?`, ts(now), ts(now), sid)
	}
	store.StampItem(c.db, next.ID)
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
	planned := c.plansOf(ids...)
	each(func(e *Entry) {
		steps := got[item(e)]
		if len(steps) == 0 {
			return
		}
		if e.Coming {
			// A coming row is the chain's titles, open but for the steps
			// planned skipped on its day (PlanStep).
			plan := planned[item(e)][e.Day]
			fresh := make([]store.Step, len(steps))
			for i, s := range steps {
				fresh[i] = store.Step{N: s.N, Title: s.Title, State: "open", Mark: "todo"}
				if plan[s.N] == "skipped" {
					fresh[i].State, fresh[i].Mark = "skipped", "wont"
				}
			}
			steps = fresh
		}
		e.Steps, e.Part = steps, store.StepsPart(steps)
	})
}
