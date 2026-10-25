package calendar

import (
	"strings"
	"testing"
	"time"
)

// An away span fires nothing and a step filed on one of its days hears so;
// the preamble only says to check (no dates in it).
func TestAwaySpanIsSeenAndFiresNothing(t *testing.T) {
	c, _, n := newTest(t)
	now := time.Date(2030, 1, 10, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	if _, err := c.AddAway(Away{Title: "Trip", From: "2030-06-13", Until: "2030-06-07"}); err == nil {
		t.Fatal("until before from was taken")
	}
	hm, err := c.AddAway(Away{Title: "Trip", From: "2030-06-07", Until: "2030-06-13", Source: "claude:thread:calendar"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.AwayOn("2030-06-10"); !strings.Contains(got, "Trip 2030-06-07..2030-06-13") {
		t.Fatalf("AwayOn inside: %q", got)
	}
	if got := c.AwayOn("2030-06-14"); got != "" {
		t.Fatalf("AwayOn after: %q", got)
	}
	v, err := c.Agenda("2030-06-01", "2030-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Away) != 1 || v.Away[0].ID != hm.ID {
		t.Fatalf("agenda away: %+v", v.Away)
	}
	for _, d := range v.Days {
		if len(d.Entries) > 0 {
			t.Fatalf("an away span became an entry: %+v", d)
		}
	}
	now = time.Date(2030, 6, 8, 12, 0, 0, 0, time.Local)
	c.Tick()
	if len(n.msgs) != 0 {
		t.Fatalf("an away span pushed: %q", n.msgs)
	}
	if _, err := c.EndAway(hm.ID, "dates moved"); err != nil {
		t.Fatal(err)
	}
	if got := c.AwayOn("2030-06-10"); got != "" {
		t.Fatalf("ended span still away: %q", got)
	}
	if _, err := c.EndAway(hm.ID, "again"); err == nil {
		t.Fatal("ended twice")
	}
}
