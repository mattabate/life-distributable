package threads

import (
	"os"
	"strings"
	"testing"
)

// A message written to the live process is never written again, even when
// the database refuses its delivery stamp (#210): the row is held out of the
// queue and stamped once the database takes it.
func TestASentMessageIsNotReplayedWhenItsStampFails(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	in := inFile(t, m, th.ID)
	if _, err := m.db.Exec(`CREATE TRIGGER refuse_stamp BEFORE UPDATE OF queued ON thread_messages
		WHEN NEW.queued=0 AND OLD.text='send me once' BEGIN SELECT RAISE(FAIL, 'stamp refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(th.ID, "send me once"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`DROP TRIGGER refuse_stamp`); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(th.ID, "and a second"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(in)
	if n := strings.Count(string(b), "send me once"); n != 1 || !strings.Contains(string(b), "and a second") {
		t.Fatalf("the first message is in the process input %d times, want 1: %s", n, b)
	}
	var queued int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=? AND queued=1`, th.ID).Scan(&queued)
	if queued != 0 || len(m.sent) != 0 {
		t.Fatalf("still queued: %d rows, %d held", queued, len(m.sent))
	}
}
