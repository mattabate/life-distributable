package threads

import (
	"testing"
	"time"
)

// A reply beside an approval raised this turn is folded, not a second card;
// an approval from an earlier turn that is still waiting does not fold the
// next turn's reply.
func TestReplyBesideAnApprovalIsNotASecondCard(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("Batch", "life", "", "Post the issues.", "", "", nil)
	now := ts(time.Now())
	if _, err := m.db.Exec(`INSERT INTO items (id, src, kind, state, title, thread_id, created_at, updated_at, source) VALUES ('20261029-230000-aaaaaaaa','action','share','proposed','Post 22 issues',?,?,?,'claude')`, th.ID, now, now); err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "Say: Hey, batch seven is ready to post. Approve and I'll post them.")
	if a := m.activeAsks(th.ID); len(a) != 0 {
		t.Fatalf("a read card beside the approval: %+v", a)
	}
	var said int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=? AND role='claude' AND text LIKE '%ready to post%'`, th.ID).Scan(&said)
	if said != 0 {
		t.Fatal("the folded reply still shows as a message")
	}

	time.Sleep(2 * time.Millisecond)
	if err := m.Send(th.ID, "How many builders were there?"); err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "Say: Hey, twelve builders.")
	if a := m.activeAsks(th.ID); len(a) != 1 || a[0].Kind != "read" {
		t.Fatalf("the next turn's answer should be a read card: %+v", a)
	}
}
