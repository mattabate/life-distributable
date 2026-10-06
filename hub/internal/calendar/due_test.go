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

// A repeat lists its next occurrence only, and the window says what a miss is
// (a missed daily drill is gone, a missed weekly assignment builds up work
// debt): the later row waits until the
// one before it closes or slips; a by-the-day assignment that slips stays owed
// when the next fires, the chain goes on past it, and one video pays one week.
func TestARepeatListsItsNextOnlyAndByTheDayIsDebt(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 10, 4, 3, 25, 0, 0, time.Local) // Sunday
	c.Now = func() time.Time { return now }
	const src = "hub:learn:lt-1"
	moved, err := c.Add(Item{Title: "Course: watch the next video", Kind: "homework", Day: "2026-10-04", At: "20:30", Repeat: "weekly", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	next, err := c.Add(Item{Title: "Course: watch the next video", Kind: "homework", Day: "2026-10-07", At: "20:30", Repeat: "weekly", Source: src, PrevID: moved.ID})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(es []Entry) string {
		s := ""
		for _, e := range es {
			s += e.ID + " "
		}
		return s
	}
	v, _ := c.Agenda("2026-10-04", "2026-10-10")
	if ids(v.Due) != moved.ID+" " || len(v.Soon) != 0 {
		t.Fatalf("due today, so the next one waits: due=%s soon=%s", ids(v.Due), ids(v.Soon))
	}
	// Past its minute: it is debt, and the next one is up.
	now = time.Date(2026, 10, 4, 21, 0, 0, 0, time.Local)
	c.Tick()
	v, _ = c.Agenda("2026-10-04", "2026-10-10")
	if ids(v.Overdue) != moved.ID+" " || len(v.Due) != 0 || ids(v.Soon) != next.ID+" " {
		t.Fatalf("slipped: overdue=%s due=%s soon=%s", ids(v.Overdue), ids(v.Due), ids(v.Soon))
	}
	// Wednesday's fires: Sunday's is still owed, and the chain goes on.
	now = time.Date(2026, 10, 7, 21, 0, 0, 0, time.Local)
	c.Tick()
	if od, _ := c.Overdue(); len(od) != 2 {
		t.Fatalf("a missed by-the-day assignment is debt, not missed: %+v", od)
	}
	if nx, _ := c.List("2026-10-14", "2026-10-14", "scheduled"); len(nx) != 1 {
		t.Fatalf("debt does not stop the chain: %+v", nx)
	}
	// One video pays one assignment, the oldest.
	if n := c.CloseBySource(src, "hub", "done: part 5"); n != 1 {
		t.Fatalf("closed %d, want 1", n)
	}
	if a, _ := c.Get(moved.ID); a.State != "done" {
		t.Fatalf("the oldest closes: %+v", a)
	}
	if b, _ := c.Get(next.ID); b.State != "fired" {
		t.Fatalf("the other stays owed: %+v", b)
	}
	// Watched ahead of its day, inside its week: that one closes too.
	c.CloseBySource(src, "hub", "done: part 6")
	now = time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	if n := c.CloseBySource(src, "hub", "done: part 7"); n != 1 {
		t.Fatalf("an assignment is owed from the start of its week: closed %d", n)
	}
}

// A repeat shows in the weeks ahead: the table holds one open occurrence, the agenda draws the later
// ones on the days they will fall — read-only, on their day only, never a
// second row on a day the chain already has one, and never in a tray.
func TestARepeatShowsInTheWeeksAhead(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 10, 5, 1, 30, 0, 0, time.Local) // Monday
	c.Now = func() time.Time { return now }
	// The one the owner pushed, alone (its repeat is off), and the chain's next.
	c.Add(Item{Title: "Course: watch the next video", Kind: "homework", Day: "2026-10-05", At: "20:30", Source: "hub:learn:lt-1"})
	sat, err := c.Add(Item{Title: "Course: watch the next video", Kind: "homework", Day: "2026-10-10", Repeat: "weekly", Source: "hub:learn:lt-1"})
	if err != nil {
		t.Fatal(err)
	}
	c.Add(Item{Title: "Perfect pitch", Kind: "homework", Day: "2026-10-05", Repeat: "daily", Due: "on"})
	c.Add(Item{Title: "One-off", Kind: "owner", Day: "2026-10-06"})

	on := func(v View, day, title string) []Entry {
		var out []Entry
		for _, d := range v.Days {
			for _, e := range d.Entries {
				if d.Day == day && e.Title == title {
					out = append(out, e)
				}
			}
		}
		return out
	}
	v, err := c.Agenda("2026-10-04", "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}
	// Saturday the 10th is the real row; the 17th and 24th are coming.
	if got := on(v, "2026-10-10", sat.Title); len(got) != 1 || got[0].Coming || !got[0].Item || got[0].Move != "item" {
		t.Fatalf("the open occurrence is itself, once: %+v", got)
	}
	for _, day := range []string{"2026-10-17", "2026-10-24"} {
		got := on(v, day, sat.Title)
		if len(got) != 1 {
			t.Fatalf("%s: want one coming row, got %+v", day, got)
		}
		e := got[0]
		if !e.Coming || e.Item || e.Open || e.Tick || e.Move != "" || len(e.Outcomes) != 0 || e.Why == "" {
			t.Fatalf("%s: a coming row is read-only: %+v", day, e)
		}
		if e.ID != sat.ID+"@"+day || e.Ref != "cal:"+sat.ID || e.Lane != "homework" || e.Repeat != "weekly" || e.At != "" {
			t.Fatalf("%s: it wears its item's shape and points at it: %+v", day, e)
		}
	}
	// A daily one: every later day, not today's own (that is the row) and
	// nothing behind today.
	if got := on(v, "2026-10-05", "Perfect pitch"); len(got) != 1 || got[0].Coming {
		t.Fatalf("today's is the real one: %+v", got)
	}
	if got := on(v, "2026-10-04", "Perfect pitch"); len(got) != 0 {
		t.Fatalf("nothing is projected backwards: %+v", got)
	}
	for _, day := range []string{"2026-10-06", "2026-10-24"} {
		if got := on(v, day, "Perfect pitch"); len(got) != 1 || !got[0].Coming {
			t.Fatalf("%s: daily repeat: %+v", day, got)
		}
	}
	// What does not repeat does not come again, and the trays read the table.
	if got := on(v, "2026-10-13", "One-off"); len(got) != 0 {
		t.Fatalf("a one-off repeated: %+v", got)
	}
	for _, e := range append(append(append([]Entry{}, v.Due...), v.Soon...), v.Overdue...) {
		if e.Coming {
			t.Fatalf("a coming row in a tray: %+v", e)
		}
	}
	// Next week alone (the page the owner turned to) is not empty.
	v, _ = c.Agenda("2026-10-11", "2026-10-17")
	if got := on(v, "2026-10-17", sat.Title); len(got) != 1 || !got[0].Coming {
		t.Fatalf("next week: %+v", got)
	}
}
