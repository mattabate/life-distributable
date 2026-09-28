package calendar

import (
	"strings"
	"testing"
	"time"

	"life/hub/internal/threads"
)

// A row says what its session is doing right now, in the board's words — so
// the step the owner just answered from does not fall silent while the
// session works on their words.
func TestAgendaRowSaysWhatItsSessionIsDoing(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 26, 1, 40, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	tr, err := th.Create("Twitter follower growth", "life", "", "p", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Create leaves the session running on its first prompt; this test
	// starts from a session that has ended its turn.
	if _, err := c.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}
	it, err := c.Add(Item{Title: "Download your X archive", Day: day(now), At: "19:00", Kind: "owner", ThreadID: tr.ID, Source: "claude:thread:" + tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	row := func() Entry {
		v, err := c.Agenda(day(now), day(now))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range v.Days {
			for _, e := range d.Entries {
				if e.Ref == "cal:"+it.ID {
					return e
				}
			}
		}
		t.Fatal("item row missing")
		return Entry{}
	}
	words := func(e Entry) string {
		var w []string
		for _, p := range e.Live {
			w = append(w, p.Word+"/"+p.Tone)
		}
		return strings.Join(w, " ")
	}
	// A quiet session: no capsule at all — a row never says "idle".
	if got := words(row()); got != "" {
		t.Fatalf("quiet session wears %q", got)
	}
	// The owner's answer from the step's box is delivered the moment it is queued
	// (Queue → sendPrompt), so by the time the page redraws the session is
	// running: the pulsing "running" pill, the board's own.
	if _, err := th.Queue(threads.Prompt{Author: "owner", Target: "new-or:" + tr.ID, Text: "Done. It is in life_intake.", InReplyTo: "cal:" + it.ID}); err != nil {
		t.Fatal(err)
	}
	if got := words(row()); got != "running/running" {
		t.Fatalf("answered step: %q", got)
	}
	// Its card being spoken leads (ListBrief reads the wall clock for this).
	th.Voice.Mark(tr.ID, time.Now().Add(5*time.Second))
	if got := words(row()); got != "speaking/speaking running/running" {
		t.Fatalf("speaking: %q", got)
	}
	// The same words on a row drawn away from its day.
	if _, err := c.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}
	th.Voice.Mark(tr.ID, time.Now())
	if got := words(row()); got != "" {
		t.Fatalf("ended: %q", got)
	}
}
