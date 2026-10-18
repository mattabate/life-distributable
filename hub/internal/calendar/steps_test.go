package calendar

import (
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

// Steps: one item, one fire and one ask; each step closed on its own; the
// item's fate follows. Doing part of a routine is not skipping it.
func TestStepsCloseOneByOne(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	it, err := c.Add(Item{Title: "Do your morning routine", Kind: "owner", Day: "2026-10-18", At: "09:30", Repeat: "daily", Due: "on",
		Steps: []store.Step{{Title: "Drink a glass of juice"}, {Title: " Wash your face "}, {Title: ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Steps) != 2 || it.Steps[1].Title != "Wash your face" || it.Steps[0].State != "open" || it.Steps[0].Mark != "todo" {
		t.Fatalf("steps = %+v", it.Steps)
	}
	now = now.Add(time.Hour)
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "fired" || got.AskID == "" {
		t.Fatalf("one item fires once: %+v", got)
	}
	if a, _ := th.GetAsk(got.AskID); len(a.Steps) != 2 {
		t.Fatalf("the card carries the steps: %+v", a.Steps)
	}
	got, err = c.CloseStep(it.ID, 1, "done", "owner")
	if err != nil || got.State != "fired" || got.Steps[0].State != "done" {
		t.Fatalf("one step done keeps the item open: %v %+v", err, got)
	}
	v, _ := c.Agenda("2026-10-18", "2026-10-18")
	var part string
	for _, e := range v.Days[0].Entries {
		if e.ID == it.ID {
			part = e.Part
			if len(e.Steps) != 2 {
				t.Fatalf("entry steps = %+v", e.Steps)
			}
		}
	}
	if part != "1 of 2" {
		t.Fatalf("part = %q", part)
	}
	got, _ = c.CloseStep(it.ID, 2, "wont", "owner")
	if got.State != "done" || !strings.Contains(got.Resolution, "skipped: Wash your face") {
		t.Fatalf("some skipped = done with the skipped part: %+v", got)
	}
	if a, _ := th.GetAsk(got.AskID); a.Active() {
		t.Fatal("the card closes with the item")
	}
	// The step's undo reopens the item, the other step keeps its mark.
	got, _ = c.CloseStep(it.ID, 2, "open", "owner")
	if got.State != "scheduled" || got.Steps[0].State != "done" || got.Steps[1].State != "open" {
		t.Fatalf("step reopen: %+v", got)
	}
	// The whole item skipped: the open step is skipped, the done one stays.
	got, _ = c.Resolve(it.ID, "dismissed", "hub", "")
	if got.Steps[0].State != "done" || got.Steps[1].State != "skipped" {
		t.Fatalf("item close: %+v", got.Steps)
	}
	// The repeat's next row has the same steps, all open.
	var next string
	c.db.QueryRow(`SELECT id FROM items WHERE prev_id=?`, it.ID).Scan(&next)
	nx, err := c.Get(next)
	if err != nil || len(nx.Steps) != 2 || nx.Steps[0].State != "open" || nx.Day != "2026-10-19" {
		t.Fatalf("next: %v %+v", err, nx)
	}
	var events int
	c.db.QueryRow(`SELECT COUNT(*) FROM item_step_events WHERE step_id=?`, store.StepID(it.ID, 2)).Scan(&events)
	if events < 4 {
		t.Fatalf("step events = %d", events)
	}
}

// All skipped = skipped; an unclosed step at the miss time closes as missed,
// and the item is done if any step was.
func TestStepsFateAndMiss(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	two := []store.Step{{Title: "a"}, {Title: "b"}}
	skip, _ := c.Add(Item{Title: "Routine A", Kind: "owner", Day: "2026-10-18", At: "09:30", Due: "on", Steps: two})
	c.CloseStep(skip.ID, 1, "wont", "owner")
	got, _ := c.CloseStep(skip.ID, 2, "wont", "owner")
	if got.State != "dismissed" {
		t.Fatalf("all skipped = skipped: %+v", got)
	}
	if _, err := c.SetSteps(got.ID, []string{"x"}); err == nil {
		t.Fatal("a closed item's steps stay")
	}
	half, _ := c.Add(Item{Title: "Routine B", Kind: "owner", Day: "2026-10-18", At: "09:30", Due: "on", Steps: two})
	c.CloseStep(half.ID, 1, "done", "owner")
	none, _ := c.Add(Item{Title: "Routine C", Kind: "owner", Day: "2026-10-18", At: "09:30", Due: "on", Steps: two})
	now = time.Date(2026, 10, 19, 9, 31, 0, 0, time.Local)
	c.Tick()
	got, _ = c.Get(half.ID)
	if got.State != "done" || got.ResolvedBy != "hub" || got.Steps[1].State != "missed" || !strings.Contains(got.Resolution, "missed: b") {
		t.Fatalf("half done at the miss: %+v", got)
	}
	got, _ = c.Get(none.ID)
	if got.State != "dismissed" || got.Resolution != "missed" || got.Steps[0].State != "missed" {
		t.Fatalf("none done at the miss: %+v", got)
	}
}
