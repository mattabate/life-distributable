package calendar

import (
	"testing"
	"time"
)

// An agent item whose wake was cancelled (the chat's cancel on a queued
// prompt) skips this run: before, the row sat scheduled forever (Tick leaves
// a clocked item alone, backfill only takes rows with no prompt, OnDelivered
// never runs) and a repeating one never minted its next
// (life-distributable#87, eduelias).
func TestAgentItemWithCancelledWakeSkipsThisRun(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(1, 7, 0)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Weekly review.", "", "", nil)
	it, err := c.Add(Item{Title: "Weekly review", Day: day(ahead(2, 0, 0)), At: "09:00", Kind: "agent", Repeat: "weekly", ThreadID: tr.ID, Detail: "x", Source: "owner"})
	if err != nil || it.PromptID == "" {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := th.CancelPrompt(it.PromptID); err != nil {
		t.Fatal(err)
	}
	c.Tick() // before its day: the tick does not look ahead
	now = ahead(2, 8, 0)
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "dismissed" || got.Resolution != "wake cancelled" {
		t.Fatalf("a cancelled wake skips the run: %+v", got)
	}
	next, _ := c.List(day(ahead(3, 0, 0)), day(ahead(30, 0, 0)), "scheduled")
	if len(next) != 1 || next[0].Day != day(ahead(9, 0, 0)) || next[0].PrevID != it.ID || next[0].PromptID == "" {
		t.Fatalf("the chain goes on: %+v", next)
	}
	if p, _ := th.GetPrompt(next[0].PromptID); p.State != "queued" {
		t.Fatalf("next wake: %+v", p)
	}
	// A moved item's wake is replaced, not cancelled for good: it stays.
	moved, _ := c.Update(next[0].ID, map[string]string{"at": "17:00"})
	c.Tick()
	if g, _ := c.Get(moved.ID); g.State != "scheduled" {
		t.Fatalf("a moved item keeps its run: %+v", g)
	}
}

// Two chains of one title on different weekdays (weekly Gym Monday and
// Friday) both go on: the forked-chain guard only looks along the item's own
// chain (life-distributable#88, eduelias). Monthly the same, by day of month.
func TestTwoWeekdayChainsOfOneTitleBothContinue(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local) // a Monday
	c.Now = func() time.Time { return now }
	mon, err := c.Add(Item{Title: "Gym", Day: "2026-11-02", Kind: "note", Repeat: "weekly", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(Item{Title: "Gym", Day: "2026-11-06", Kind: "note", Repeat: "weekly", Source: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(mon.ID, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	next, _ := c.List("2026-11-09", "2026-11-09", "scheduled")
	if len(next) != 1 || next[0].PrevID != mon.ID {
		t.Fatalf("Monday's chain must go on beside Friday's: %+v", next)
	}
	first, _ := c.Add(Item{Title: "Rent", Day: "2026-11-03", Kind: "note", Repeat: "monthly", Source: "owner"})
	if _, err := c.Add(Item{Title: "Rent", Day: "2026-11-15", Kind: "note", Repeat: "monthly", Source: "owner"}); err != nil {
		t.Fatal(err)
	}
	c.Resolve(first.ID, "done", "owner", "")
	if next, _ := c.List("2026-12-03", "2026-12-03", "scheduled"); len(next) != 1 || next[0].PrevID != first.ID {
		t.Fatalf("the 3rd's chain must go on beside the 15th's: %+v", next)
	}
}

// Two chains of one title on different weekdays both project past their real
// rows on the agenda, and both are due ahead: the chain is told apart the way
// spawnNext's guard tells it (life-distributable#88 follow-up, eduelias).
func TestTwoWeekdayChainsBothProject(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local) // a Sunday
	c.Now = func() time.Time { return now }
	for _, day := range []string{"2026-11-02", "2026-11-06"} { // Monday, Friday
		if _, err := c.Add(Item{Title: "Gym", Day: day, Kind: "homework", Repeat: "weekly", Source: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := c.Agenda("2026-11-01", "2026-11-22")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, d := range v.Days {
		for _, e := range d.Entries {
			if e.Title == "Gym" && e.Coming {
				got[d.Day]++
			}
		}
	}
	for _, day := range []string{"2026-11-09", "2026-11-13", "2026-11-16", "2026-11-20"} {
		if got[day] != 1 {
			t.Fatalf("%s: want one coming Gym, got %v", day, got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("coming Gym rows: %v", got)
	}
	ahead, err := c.DueAhead()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range ahead {
		if it.Title == "Gym" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("both chains' next Gym are due ahead, got %d: %+v", n, ahead)
	}
}

// A daily on-the-day step at 21:00 whose Saturday row only fired at 07:00
// Sunday (the Mac slept through it, the window still open) mints Sunday's,
// not Monday's: Sunday's own window has not even begun
// (life-distributable#89, eduelias). A by-the-day item still never lands
// before tomorrow.
func TestOnTheDayChainKeepsTodayAfterALateFire(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 11, 1, 7, 0, 0, 0, time.Local) // Sunday
	c.Now = func() time.Time { return now }
	sat, err := c.Add(Item{Title: "Journal", Day: "2026-10-31", At: "21:00", Kind: "owner", Repeat: "daily", Due: "on", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	by, err := c.Add(Item{Title: "Stretch", Day: "2026-10-31", At: "21:00", Kind: "owner", Repeat: "daily", Due: "by", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	if got, _ := c.Get(sat.ID); got.State != "fired" {
		t.Fatalf("Saturday's window is still open: %+v", got)
	}
	rows, _ := c.List("2026-11-01", "2026-11-30", "scheduled")
	days := map[string]string{}
	for _, r := range rows {
		days[r.PrevID] = r.Day
	}
	if days[sat.ID] != "2026-11-01" {
		t.Fatalf("on-the-day chain must mint Sunday: %+v", rows)
	}
	if days[by.ID] != "2026-11-02" {
		t.Fatalf("a by-the-day chain still starts tomorrow: %+v", rows)
	}
}
