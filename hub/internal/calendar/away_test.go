package calendar

import (
	"strings"
	"testing"
	"time"
)

// An away span fires nothing, every session's preamble names it, and a step
// filed on one of its days hears so.
func TestAwaySpanIsSeenAndFiresNothing(t *testing.T) {
	c, th, n := newTest(t)
	now := time.Date(2030, 1, 10, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	if _, err := c.AddAway(Away{Title: "Trip", From: "2030-06-13", Until: "2030-06-07"}); err == nil {
		t.Fatal("until before from was taken")
	}
	hm, err := c.AddAway(Away{Title: "Trip", From: "2030-06-07", Until: "2030-06-13", Source: "claude:thread:calendar"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddAway(Away{Title: "Conference", From: "2030-05-29"}); err != nil {
		t.Fatal(err)
	}
	// Past the preamble's ~9 months: listed by GET /away, not in the line.
	if _, err := c.AddAway(Away{Title: "Reunion", From: "2031-05-29"}); err != nil {
		t.Fatal(err)
	}
	line := c.AwayLine()
	if !strings.Contains(line, "Conference 2030-05-29; Trip 2030-06-07..2030-06-13") || strings.Contains(line, "Reunion") {
		t.Fatalf("preamble line: %q", line)
	}
	if th.AwayLine == nil || th.AwayLine() != line {
		t.Fatal("threads not wired to the away line")
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
