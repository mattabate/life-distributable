package calendar

import (
	"testing"
	"time"

	"life/hub/internal/threads"
)

// Due (an assignment due every Saturday may be done on Thursday, but must be
// done by Saturday): weekly homework is owed the whole week before its day; daily homework and every
// `owner` step (a weekly chore included) on their day only; a step whose minute
// has passed is Overdue, not Due.
func TestDueTrayCoversTheRepeatsWeek(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local) // Thursday
	c.Now = func() time.Time { return now }

	add := func(it Item) string {
		t.Helper()
		got, err := c.Add(it)
		if err != nil {
			t.Fatal(err)
		}
		return got.ID
	}
	weekly := add(Item{Title: "Harmony course: watch the next video", Kind: "homework", Day: "2026-09-26", Repeat: "weekly", Source: "hub:learn:lt-1"})
	add(Item{Title: "Water the plants", Kind: "homework", Day: "2026-09-26", Repeat: "weekly", Due: "on"}) // a practice: its day only
	add(Item{Title: "Perfect pitch", Kind: "homework", Day: "2026-09-25", Repeat: "daily"})
	today := add(Item{Title: "Call the florist", Kind: "owner", Day: "2026-09-24"})
	add(Item{Title: "Dentist", Kind: "owner", Day: "2026-09-25"})
	add(Item{Title: "Water the plants", Kind: "owner", Day: "2026-09-26", Repeat: "weekly", Due: "on"})
	add(Item{Title: "Juice", Kind: "homework", Day: "2026-09-24", At: "09:30", Due: "on"}) // earlier today, still Due
	add(Item{Title: "Morning step", Kind: "owner", Day: "2026-09-24", At: "09:00"})
	add(Item{Title: "Next week's video", Kind: "homework", Day: "2026-10-03", Repeat: "weekly"})

	v, err := c.Agenda("2026-09-20", "2026-09-26")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range v.Due {
		ids = append(ids, e.ID)
	}
	if len(ids) != 2 || ids[0] != today {
		t.Fatalf("due = %v, want [%s juice]", ids, today)
	}
	// A later day's step is Do soon, not Due.
	if len(v.Soon) != 1 || v.Soon[0].ID != weekly {
		t.Fatalf("soon = %+v, want [%s]", v.Soon, weekly)
	}
	if len(v.Overdue) != 1 || v.Overdue[0].Title != "Morning step" {
		t.Fatalf("overdue = %+v", v.Overdue)
	}
}

// An on-the-day chore (watering the plants early does not cover the rest of
// the week): it fires once with no nag, closes with a tick when no session
// is behind it, is never Overdue, and midnight closes it as missed — the
// chain goes on. A step a session waits on still needs the owner's words.
func TestOnTheDayChoreIsMissedNotOverdue(t *testing.T) {
	c, th, n := newTest(t)
	now := time.Date(2026, 9, 30, 8, 0, 0, 0, time.Local) // Wednesday
	c.Now = func() time.Time { return now }
	it, err := c.Add(Item{Title: "Water the plants", Kind: "owner", Day: "2026-09-30", At: "09:00", Repeat: "weekly", Due: "on"})
	if err != nil {
		t.Fatal(err)
	}
	if it.NagMin != 0 || it.Due != "on" {
		t.Fatalf("a chore is one push, on its day: %+v", it)
	}
	if by, _ := c.Add(Item{Title: "Sweep Venmo", Kind: "owner", Day: "2026-09-30", Repeat: "monthly"}); by.Due != "by" || by.NagMin == 0 {
		t.Fatalf("default is by, nagged: %+v", by)
	}
	now = now.Add(2 * time.Hour) // 10:00: fired, past its minute, still Due
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "fired" {
		t.Fatalf("not fired: %+v", got)
	}
	if a, _ := th.GetAsk(got.AskID); a.Class != threads.ClassPractice {
		t.Fatalf("a sessionless chore is a practice: %+v", a)
	}
	v, _ := c.Agenda("2026-09-27", "2026-10-03")
	for _, e := range v.Overdue {
		if e.ID == it.ID {
			t.Fatal("an on-the-day chore is never Overdue")
		}
	}
	var tick bool
	for _, d := range v.Days {
		for _, e := range d.Entries {
			if e.ID == it.ID {
				tick = e.Tick && e.Due == "on"
			}
		}
	}
	if !tick {
		t.Fatal("entry should say due=on, tick=true")
	}
	// Thursday: missed, the next Wednesday is up, nothing nagged.
	now = time.Date(2026, 10, 1, 0, 5, 0, 0, time.Local)
	c.Tick()
	if got, _ = c.Get(it.ID); got.State != "dismissed" || got.Resolution != "missed" || got.ResolvedBy != "hub" {
		t.Fatalf("expected missed at midnight: %+v", got)
	}
	if a, _ := th.GetAsk(got.AskID); a.Active() {
		t.Fatalf("its ask should close too: %+v", a)
	}
	next, _ := c.List("2026-10-07", "2026-10-07", "scheduled")
	if len(next) != 1 || next[0].Due != "on" {
		t.Fatalf("chain: %+v", next)
	}
	if len(n.msgs) != 0 {
		t.Fatalf("a chore never nags: %v", n.msgs)
	}
	// A tick closes the next one with no words; a session's step needs them.
	if _, err := c.Resolve(next[0].ID, "done", "owner", ""); err != nil {
		t.Fatalf("sessionless chore should tick closed: %v", err)
	}
	host, _ := th.CreateIdle("host", "Host", "life")
	step, _ := c.Add(Item{Title: "Send the stove photo", Kind: "owner", Day: "2026-10-01", ThreadID: host.ID})
	if _, err := c.Resolve(step.ID, "done", "owner", ""); err != ErrNoteRequired {
		t.Fatalf("a session's step needs words: %v", err)
	}
}
