package threads

import (
	"errors"
	"sync"
	"testing"
)

// An ask's move checks the state its caller read: a Done landing as a turn
// ends is not flipped back to open by an earlier read, and a Done tapped on
// two surfaces at once queues one prompt, not two.
func TestAskMoveChecksItsFromState(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.CreateIdle("me", "Me", "life")
	a, _ := m.AddAsk(th.ID, "", "Pick a date", "", "decision", "")
	events := func() int {
		var k int
		m.db.QueryRow(`SELECT count(*) FROM item_events WHERE item_id=?`, a.ID).Scan(&k)
		return k
	}
	m.markAnswered(th.ID, "ask:"+a.ID)
	stale, _ := m.GetAsk(a.ID)
	if stale.State != "answered" {
		t.Fatalf("%+v", stale)
	}
	if _, err := m.ResolveAsk(a.ID, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	before := events()
	if err := m.moveAsk(askMove{ask: a.ID, from: stale.State, to: "open", by: "hub", note: "the turn ended without closing it"}); !errors.Is(err, errStale) {
		t.Fatalf("a stale move landed: %v", err)
	}
	if got, _ := m.GetAsk(a.ID); got.State != "done" || got.ResolvedBy != "owner" || events() != before {
		t.Fatalf("done moved: %+v events %d→%d", got, before, events())
	}

	b, _ := m.AddAsk(th.ID, "", "Book the venue", "", "decision", "")
	if err := m.setAskState(b, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.setAskState(b, "done", "owner", ""); !errors.Is(err, errStale) {
		t.Fatalf("second move from a stale read: %v", err)
	}
	c, _ := m.AddAsk(th.ID, "", "Choose the band", "", "decision", "")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := m.ResolveAsk(c.ID, "done", "owner", ""); err != nil || got.State != "done" {
				t.Errorf("tap: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	var prompts int
	m.db.QueryRow(`SELECT count(*) FROM thread_messages WHERE thread_id=? AND text LIKE 'Done: Choose the band%'`, th.ID).Scan(&prompts)
	if prompts != 1 {
		t.Fatalf("%d Done prompts queued for one card", prompts)
	}
}
