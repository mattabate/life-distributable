package calendar

import (
	"strings"
	"testing"
	"time"

	"life/hub/internal/threads"
)

// A waiting item: its session looks once on the day; an empty look moves it
// a week out with a fresh look and raises nothing on the owner (a waiting
// item is not an action for them); a look nobody answered is moved the same
// way the morning after; a session that finds it came closes it with what
// came.
func TestWaitingItemLooksThenWaitsAgainQuietly(t *testing.T) {
	c, th, n := newTest(t)
	now := ahead(7, 8, 30)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Vendor quotes.", "", "", nil)
	if _, err := c.Add(Item{Title: "Vendor A owes a quote", Day: day(now), Kind: "waiting", Detail: "mail from:vendor-a.example"}); err == nil {
		t.Fatal("a waiting item with no session must be refused")
	}
	if _, err := c.Add(Item{Title: "Vendor A owes a quote", Day: day(now), Kind: "waiting", ThreadID: tr.ID}); err == nil {
		t.Fatal("a waiting item with no detail must be refused")
	}
	it, err := c.Add(Item{Title: "Vendor A owes a quote", Day: day(now), Kind: "waiting", ThreadID: tr.ID, Detail: "mail from:vendor-a.example"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := th.GetPrompt(it.PromptID)
	if err != nil || p.State != "queued" || !strings.Contains(p.Text, "lifectl cal "+it.ID+" raise") || !strings.Contains(p.Text, "no card on the owner") || !strings.Contains(p.Text, "mail from:vendor-a.example") {
		t.Fatalf("look: %+v %v", p, err)
	}
	if rows, _ := c.Open(); len(rows) != 0 {
		t.Fatalf("a waiting item is not the owner's: %+v", rows)
	}
	c.Tick()
	th.DuePrompts(now)
	if got, _ := c.Get(it.ID); got.State != "scheduled" || got.ThreadID != tr.ID {
		t.Fatalf("the look must not close it: %+v", got)
	}

	got, err := c.Raise(it.ID, "claude:thread:"+tr.ID, "inbox empty")
	if err != nil || got.State != "scheduled" || got.Day != day(ahead(14, 0, 0)) || !strings.Contains(got.Detail, "inbox empty") || got.PromptID == it.PromptID {
		t.Fatalf("empty look: %+v %v", got, err)
	}
	if _, err := th.GetAsk(it.ID); err == nil {
		t.Fatal("an empty look raised a card on the owner")
	}
	if p, _ := th.GetPrompt(got.PromptID); p.State != "queued" || !p.NotBefore.Equal(ahead(14, 8, 0)) {
		t.Fatalf("fresh look: %+v", p)
	}

	// The look is delivered and the session never answers: the hub moves it.
	now = ahead(15, 9, 0)
	th.DuePrompts(now)
	c.Tick()
	if got, _ = c.Get(it.ID); got.State != "scheduled" || got.Day != day(ahead(22, 0, 0)) {
		t.Fatalf("unanswered look: %+v", got)
	}
	if rows, _ := c.Open(); len(rows) != 0 || len(n.msgs) != 0 {
		t.Fatalf("nothing reaches the owner: %+v %v", rows, n.msgs)
	}

	// It came: the session closes it, and the look goes with it.
	came, _ := c.Add(Item{Title: "Vendor B owes a quote", Day: day(ahead(20, 0, 0)), Kind: "waiting", ThreadID: tr.ID, Detail: "mail"})
	if done, err := c.Resolve(came.ID, "done", "claude:thread:"+tr.ID, "quote came: $100"); err != nil || done.State != "done" {
		t.Fatalf("came: %+v %v", done, err)
	}
	if p, _ := th.GetPrompt(came.PromptID); p.State != "cancelled" {
		t.Fatalf("closed item's look: %+v", p)
	}
	if _, err := c.Raise(came.ID, "hub", ""); err != nil {
		t.Fatalf("raise of a closed item is a no-op: %v", err)
	}
}

// A card an older hub raised (Nudge · Wait 7 days · Drop) goes back to
// waiting on the next tick: off the owner's board, a week out, a fresh look
// queued.
func TestOldWaitingCardGoesBackToWaiting(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(3, 9, 0)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Forum.", "", "", nil)
	it, err := c.Add(Item{Title: "Forum staff owe an approval", Day: day(ahead(2, 0, 0)), Kind: "waiting", ThreadID: tr.ID, Detail: "mail from:forum.example", Source: "claude:thread:" + tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	c.cancelWake(it)
	c.db.Exec(`UPDATE items SET state='open', fired_at=?, ask_kind='waiting' WHERE id=?`, ts(now), it.ID)
	if _, err := th.RaiseStep(it.ID, threads.ClassUnblock, it.Say); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(it.ID); got.State != "fired" || got.AskKind != "waiting" {
		t.Fatalf("fixture: %+v", got)
	}
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "scheduled" || got.AskKind != "" || got.Day != day(ahead(10, 0, 0)) {
		t.Fatalf("not back to waiting: %+v", got)
	}
	if a, err := th.GetAsk(it.ID); err == nil && a.Active() {
		t.Fatalf("its card is still on the owner's board: %+v", a)
	}
	if p, _ := th.GetPrompt(got.PromptID); p.State != "queued" {
		t.Fatalf("fresh look: %+v", p)
	}
}

// A look cancelled while its item stays scheduled is no look: on its day the
// tick queues a fresh one instead of leaving the row scheduled forever,
// unchecked.
func TestWaitingItemWithCancelledLookGetsAFreshOne(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(1, 7, 0)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Quotes.", "", "", nil)
	it, err := c.Add(Item{Title: "Vendor B owes a quote", Day: day(ahead(2, 0, 0)), Kind: "waiting", ThreadID: tr.ID, Detail: "mail from:vendor-b.example", Source: "claude:thread:" + tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := th.CancelPrompt(it.PromptID); err != nil {
		t.Fatal(err)
	}
	c.Tick() // before its day: nothing to do yet
	if got, _ := c.Get(it.ID); got.PromptID != it.PromptID {
		t.Fatalf("a look is owed only once its day comes: %+v", got)
	}
	now = ahead(3, 9, 0)
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "scheduled" || got.PromptID == "" || got.PromptID == it.PromptID {
		t.Fatalf("cancelled look must be replaced: %+v", got)
	}
	if p, _ := th.GetPrompt(got.PromptID); p.State != "queued" || !strings.Contains(p.Text, "lifectl cal "+it.ID+" raise") {
		t.Fatalf("fresh look: %+v", p)
	}
	th.DuePrompts(now)
	now = ahead(4, 9, 0)
	c.Tick()
	if got, _ = c.Get(it.ID); got.State != "scheduled" || got.Day != day(ahead(11, 0, 0)) {
		t.Fatalf("the fresh look, unanswered, moves it a week out: %+v", got)
	}
}
