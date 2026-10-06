package calendar

import (
	"testing"
	"time"
)

type cardNfy struct {
	stubNfy
	said []string
}

func (n *cardNfy) Card(kind, line, say, thread, card string) error {
	n.said = append(n.said, say)
	return nil
}

// A chore is spoken as one sentence ("Hey, reminder to …"); homework and a
// session's step keep "a reminder.", and an item's own --say is never
// rewritten.
func TestAChoreSpeaksReminderTo(t *testing.T) {
	c, th, _ := newTest(t)
	n := &cardNfy{}
	th.Notifier = n
	now := time.Date(2026, 10, 5, 8, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	host, _ := th.CreateIdle("host", "Host", "life")
	for _, it := range []Item{
		{Title: "Take your supplements", Kind: "owner", Day: "2026-10-05", At: "13:00", Repeat: "daily"},
		{Title: "Perfect pitch: one round", Kind: "homework", Day: "2026-10-05", At: "13:00", Repeat: "daily"},
		{Title: "Send the stove photo", Kind: "owner", Day: "2026-10-05", At: "13:00", ThreadID: host.ID},
		{Title: "Wash your face", Kind: "owner", Day: "2026-10-05", At: "13:00", Say: "Hey, face time."},
	} {
		if _, err := c.Add(it); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(5*time.Hour + time.Minute)
	c.Tick()
	want := map[string]bool{
		"Hey, reminder to take your supplements.":    true,
		"Hey, a reminder. Perfect pitch: one round.": true,
		"Hey, a reminder. Send the stove photo.":     true,
		"Hey, face time.":                            true,
	}
	if len(n.said) != len(want) {
		t.Fatalf("said %q", n.said)
	}
	for _, s := range n.said {
		if !want[s] {
			t.Errorf("said %q", s)
		}
	}
}
