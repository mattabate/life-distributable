package calendar

import (
	"testing"
	"time"

	"life/hub/internal/threads"
)

// Only the item's current wake is its run (#211). A reply that points at a
// future repeating agent item is words about it; the wake of a time the item
// was moved off is obsolete. A wake due on insert is delivered inside Queue
// and still closes its item.
func TestOnlyTheCurrentWakeClosesAnAgentItem(t *testing.T) {
	c, th, _ := newTest(t)
	tr, err := th.Create("Receipts", "life", "", "p", "", "receipts", nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := c.Add(Item{Title: "File the receipts", Day: day(ahead(3, 0, 0)), At: "09:00", Kind: "agent", Repeat: "weekly", ThreadID: tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := th.Queue(threads.Prompt{Author: "owner", Target: "new-or:" + tr.ID, InReplyTo: "cal:" + it.ID,
		Text: "Please include the receipts when this runs."}); err != nil || p.State != "delivered" {
		t.Fatalf("reply: %+v %v", p, err)
	}
	got, _ := c.Get(it.ID)
	if got.State != "scheduled" || got.PromptID != it.PromptID {
		t.Fatalf("a reply closed a future agent item: %+v", got)
	}
	old, _ := th.GetPrompt(it.PromptID)
	moved, err := c.Update(it.ID, map[string]string{"day": day(ahead(5, 0, 0)), "scope": "one"})
	if err != nil || moved.PromptID == "" || moved.PromptID == old.ID {
		t.Fatalf("move: %+v %v", moved, err)
	}
	c.OnDelivered(old, tr.ID)
	if got, _ := c.Get(it.ID); got.State != "scheduled" || got.PromptID != moved.PromptID {
		t.Fatalf("an obsolete wake closed a moved item: %+v", got)
	}
	cur, _ := th.GetPrompt(moved.PromptID)
	c.OnDelivered(cur, tr.ID)
	if got, _ := c.Get(it.ID); got.State != "done" || got.Resolution != "agent run started" {
		t.Fatalf("the current wake did not close it: %+v", got)
	}
	now, _ := c.Add(Item{Title: "Run now", Day: day(time.Now()), At: "00:01", Kind: "agent", ThreadID: tr.ID})
	if got, _ := c.Get(now.ID); got.State != "done" {
		t.Fatalf("a wake delivered inside Queue did not close its item: %+v", got)
	}
}
