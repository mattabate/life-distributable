package calendar

import (
	"strings"
	"testing"
	"time"

	"life/hub/internal/threads"
)

// A waiting item: its session looks once on the day; an empty look is ONE
// card on the owner; Wait 7 days puts it back a week with a fresh look; a
// look nobody answered is raised the morning after; Drop closes it; a
// session that finds it came closes it with what came.
func TestWaitingItemLooksThenRaisesOneCard(t *testing.T) {
	c, th, _ := newTest(t)
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
	if err != nil || p.State != "queued" || !strings.Contains(p.Text, "lifectl cal "+it.ID+" raise") || !strings.Contains(p.Text, "mail from:vendor-a.example") {
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
	if err != nil || got.State != "fired" || got.AskKind != "waiting" || !strings.Contains(got.Detail, "inbox empty") {
		t.Fatalf("raise: %+v %v", got, err)
	}
	a, err := th.GetAsk(it.ID)
	if err != nil || a.Class != threads.ClassUnblock || len(a.Outcomes) != 4 || a.Outcomes[0].Value != "nudge" || a.Outcomes[1].Value != "extend" || a.Outcomes[2].Label != "Drop" {
		t.Fatalf("card: %+v %v", a, err)
	}
	if again, _ := c.Raise(it.ID, "hub", "x"); again.State != "fired" {
		t.Fatalf("second raise: %+v", again)
	}

	if _, err := th.Queue(threads.Prompt{Author: "owner", Target: tr.ID, InReplyTo: "cal:" + it.ID, Outcome: "extend"}); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Get(it.ID)
	if got.State != "scheduled" || got.Day != day(ahead(14, 0, 0)) || got.PromptID == it.PromptID {
		t.Fatalf("extend: %+v", got)
	}
	if p, _ := th.GetPrompt(got.PromptID); p.State != "queued" || !p.NotBefore.Equal(ahead(14, 8, 0)) {
		t.Fatalf("fresh look: %+v", p)
	}

	// The look is delivered and the session never answers: the hub raises it.
	now = ahead(15, 9, 0)
	th.DuePrompts(now)
	c.Tick()
	if got, _ = c.Get(it.ID); got.State != "fired" || got.AskKind != "waiting" {
		t.Fatalf("unanswered look: %+v", got)
	}
	if _, err := th.Queue(threads.Prompt{Author: "owner", Target: tr.ID, InReplyTo: "cal:" + it.ID, Outcome: "wont"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = c.Get(it.ID); got.State != "dismissed" {
		t.Fatalf("drop: %+v", got)
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

// A look cancelled while its item stays scheduled is no look: on its day the
// tick queues a fresh one instead of leaving the row scheduled forever,
// unchecked and never raised.
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
	if got, _ = c.Get(it.ID); got.State != "fired" || got.AskKind != "waiting" {
		t.Fatalf("the fresh look, unanswered, raises the card: %+v", got)
	}
}
