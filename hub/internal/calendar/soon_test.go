package calendar

import (
	"errors"
	"testing"
	"time"
)

// A soon item (filed at a moment, doable any time after it; shown as "do this
// soon", never as overdue): an `owner` item with no day. It never fires, nags, counts as due or enters Overdue
// however long it sits; it lists under `soon` and in the cell of the minute it
// was added; it closes with the owner's words and no session, and Reopen brings it
// back as the same to-do.
func TestSoonItemIsNeverOverdueAndClosesWithWords(t *testing.T) {
	c, th, n := newTest(t)
	now := time.Date(2026, 9, 20, 16, 21, 0, 0, time.Local)
	c.Now = func() time.Time { return now }

	if _, err := c.Add(Item{Title: "Book the string quartet", Kind: "owner"}); err == nil {
		t.Fatal("a forgotten day must still be an error: soon is explicit")
	}
	for _, bad := range []Item{
		{Title: "x", Kind: "agent", Soon: true},
		{Title: "x", Kind: "homework", Soon: true},
		{Title: "x", Kind: "owner", Soon: true, At: "09:00"},
		{Title: "x", Kind: "owner", Soon: true, Repeat: "weekly"},
	} {
		if _, err := c.Add(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	it, err := c.Add(Item{Title: "Book the string quartet", Kind: "owner", Soon: true, Source: "claude:thread:x"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Day != "" || !it.Soon || it.NagMin != 0 || it.ThreadID != "" {
		t.Fatalf("soon item: %+v", it)
	}

	// Ten days on: nothing fired, nothing pushed, nothing due, nothing overdue.
	now = now.AddDate(0, 0, 10)
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "scheduled" || got.AskID != "" || len(n.msgs) != 0 {
		t.Fatalf("a soon item fired or nagged: %+v %v", got, n.msgs)
	}
	if due, _ := c.DueCount(); due != 0 {
		t.Fatalf("due count %d, want 0", due)
	}
	if od, _ := c.Overdue(); len(od) != 0 {
		t.Fatalf("overdue: %+v", od)
	}

	// The agenda: under `soon` whatever the window, and in the cell of the
	// minute it was added when that day is inside it.
	v, err := c.Agenda("2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Anytime) != 1 || len(v.Soon) != 0 || len(v.Overdue) != 0 || len(v.Days) != 0 {
		t.Fatalf("view: %+v", v)
	}
	e := v.Anytime[0]
	if !e.Soon || e.Overdue || e.Day != "2026-09-20" || e.At != "16:21" || e.KindLabel != "anytime" || e.Lane != "mine" || !e.Item || e.Closed {
		t.Fatalf("soon entry: %+v", e)
	}
	v, _ = c.Agenda("2026-09-20", "2026-09-20")
	if len(v.Days) != 1 || len(v.Days[0].Entries) != 1 || !v.Days[0].Entries[0].Soon || v.Days[0].Entries[0].Overdue {
		t.Fatalf("added-day cell: %+v", v.Days)
	}

	// The owner's words close it; no session is behind it, so none hears them.
	if _, err := c.Resolve(it.ID, "done", "owner", ""); !errors.Is(err, ErrNoteRequired) {
		t.Fatalf("closed without words: %v", err)
	}
	now = now.Add(3 * time.Hour) // 19:21 on the 30th
	if got, err = c.Resolve(it.ID, "done", "owner", "booked, $1,800"); err != nil || got.State != "done" || got.Resolution != "booked, $1,800" || got.ResolvedBy != "owner" {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	if ps, _ := th.ListPrompts("queued", "", 10); len(ps) != 0 {
		t.Fatalf("a session was woken for a to-do that has none: %+v", ps)
	}
	// Closed, it is a record at the minute it was done, with Reopen (item).
	v, _ = c.Agenda("2026-09-20", "2026-09-30")
	if len(v.Anytime) != 0 || len(v.Days) != 1 || v.Days[0].Day != "2026-09-30" {
		t.Fatalf("closed view: %+v", v)
	}
	if e = v.Days[0].Entries[0]; !e.Did || e.Verb != "Did" || e.At != "19:21" || !e.Item || !e.Closed {
		t.Fatalf("record: %+v", e)
	}
	if got, err = c.Resolve(it.ID, "scheduled", "owner", ""); err != nil || got.State != "scheduled" || got.Day != "" {
		t.Fatalf("reopen: %+v %v", got, err)
	}
}

// Converting a fired step takes it off its session (a converted to-do is an
// any-time calendar row, not the session's card): the ask it raised closes by
// the hub — no prompt, no "Skipped" record in the owner's name — the item lets go of the thread, stops
// nagging and leaves Overdue; a day given later makes it a dated step again.
func TestStepBecomesSoonAndLeavesItsSession(t *testing.T) {
	c, th, n := newTest(t)
	now := time.Date(2026, 9, 20, 18, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	host, err := th.Create("To-dos", "life", "", "p", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	queued := func() int { ps, _ := th.ListPrompts("queued", "", 50); return len(ps) }
	base := queued()
	if _, err := c.Add(Item{Title: "x", Kind: "owner", Soon: true, ThreadID: host.ID}); err != nil {
		t.Fatal(err)
	} else if got, _ := c.Agenda("", ""); len(got.Anytime) != 1 || got.Anytime[0].ThreadID != "" {
		t.Fatalf("a filed to-do kept a session: %+v", got.Anytime)
	}
	it, err := c.Add(Item{Title: "Buy new sneakers", Day: "2026-09-20", At: "19:00", Kind: "owner", ThreadID: host.ID})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(90 * time.Minute)
	c.Tick()
	fired, _ := c.Get(it.ID)
	if fired.State != "fired" || fired.AskID == "" {
		t.Fatalf("not fired: %+v", fired)
	}
	now = now.AddDate(0, 0, 1)
	if od, _ := c.Overdue(); len(od) != 1 {
		t.Fatalf("expected it overdue first: %+v", od)
	}
	soon, err := c.Update(it.ID, map[string]string{"day": ""})
	if err != nil {
		t.Fatal(err)
	}
	if soon.Day != "" || soon.At != "" || soon.State != "scheduled" || soon.AskID != "" || soon.ThreadID != "" || soon.NagMin != 0 {
		t.Fatalf("converted: %+v", soon)
	}
	// The step was its card: back on the schedule, it is off the session.
	if a, err := th.GetAsk(fired.AskID); err == nil {
		t.Fatalf("its card is still the session's: %+v", a)
	}
	if queued() != base {
		t.Fatal("the session was woken about it")
	}
	pushed := len(n.msgs)
	now = now.AddDate(0, 0, 3)
	c.Tick()
	if od, _ := c.Overdue(); len(od) != 0 || len(n.msgs) != pushed {
		t.Fatalf("still overdue or nagged: %+v %v", od, n.msgs)
	}
	if due, _ := c.DueCount(); due != 0 {
		t.Fatalf("due count %d", due)
	}
	v, _ := c.Agenda("", "")
	if len(v.Anytime) != 2 || len(v.Soon) != 0 {
		t.Fatalf("the item is the row, its ask is not a second one: %+v", v)
	}
	// The owner's words close it on the hub, like a to-do filed soon.
	if got, err := c.Resolve(it.ID, "done", "owner", "bought"); err != nil || got.State != "done" {
		t.Fatalf("close: %+v %v", got, err)
	}
	if queued() != base {
		t.Fatal("a session was woken")
	}

	// Repeating, agent and homework items cannot be soon; a to-do given a day
	// fires and nags again.
	rep, _ := c.Add(Item{Title: "Juice", Day: "2026-10-01", Kind: "owner", Repeat: "daily"})
	if _, err := c.Update(rep.ID, map[string]string{"day": ""}); err == nil {
		t.Fatal("a repeating item became soon")
	}
	ag, _ := c.Add(Item{Title: "Run", Day: "2026-10-01", Kind: "agent"})
	if _, err := c.Update(ag.ID, map[string]string{"day": ""}); err == nil {
		t.Fatal("an agent item became soon")
	}
	todo, _ := c.Add(Item{Title: "Mac mini", Kind: "owner", Soon: true})
	dated, err := c.Update(todo.ID, map[string]string{"day": "2026-10-02"})
	if err != nil || dated.Day != "2026-10-02" || dated.NagMin != c.DefaultNag {
		t.Fatalf("dated again: %+v %v", dated, err)
	}
}
