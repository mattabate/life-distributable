package calendar

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"life/hub/internal/cadence"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

type stubNfy struct{ msgs []string }

func (s *stubNfy) NeedsYou(m string) error { s.msgs = append(s.msgs, m); return nil }

// ahead: a local time `days` from the real today at hh:mm. The clock's tick
// (threads.Queue/DuePrompts) reads the real clock, so a test that queues a
// wake must date it in the real future or it is delivered on the spot.
func ahead(days, hh, mm int) time.Time {
	d := time.Now().AddDate(0, 0, days)
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, time.Local)
}

func day(t time.Time) string { return t.Format("2006-01-02") }

func newTest(t *testing.T) (*Calendar, *threads.Manager, *stubNfy) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	th, err := threads.New(db, "/bin/false", filepath.Join(t.TempDir(), "runs"), "life", func(p string) (string, bool) { return "/tmp", p == "life" }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	th.Summarize = nil
	th.Run = func(string, ...string) ([]byte, error) { return nil, nil }
	c, err := New(db, th)
	if err != nil {
		t.Fatal(err)
	}
	n := &stubNfy{}
	c.Nfy = n
	return c, th, n
}

func TestOwnerItemFiresAskThenNags(t *testing.T) {
	c, th, n := newTest(t)
	now := time.Date(2026, 9, 3, 7, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	it, err := c.Add(Item{Title: "Buy SPY tranche 3", Day: "2026-09-03", Kind: "owner", Repeat: "monthly", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	if got, _ := c.Get(it.ID); got.State != "scheduled" {
		t.Fatalf("fired before 08:00: %+v", got)
	}
	now = now.Add(2 * time.Hour) // 09:00
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "fired" || got.AskID == "" || got.ThreadID != "calendar" {
		t.Fatalf("expected fired with ask on fallback thread: %+v", got)
	}
	a, err := th.GetAsk(got.AskID)
	if err != nil || a.Kind != "physical" || a.Title != "Buy SPY tranche 3" {
		t.Fatalf("ask: %+v %v", a, err)
	}
	// recurrence spawned once
	next, _ := c.List("2026-10-01", "2026-10-31", "")
	if len(next) != 1 || next[0].Day != "2026-10-03" || next[0].PrevID != it.ID {
		t.Fatalf("next occurrence: %+v", next)
	}
	c.Tick()
	if again, _ := c.List("2026-10-01", "2026-10-31", ""); len(again) != 1 {
		t.Fatalf("duplicate recurrence: %d", len(again))
	}
	// nag after 6h, not before
	now = now.Add(3 * time.Hour)
	c.Tick()
	if len(n.msgs) != 0 {
		t.Fatalf("nagged too early: %v", n.msgs)
	}
	now = now.Add(4 * time.Hour) // 16:00
	c.Tick()
	if len(n.msgs) != 1 {
		t.Fatalf("expected one nag: %v", n.msgs)
	}
	// The owner closes the ask → item done on the next tick
	th.ResolveAsk(got.AskID, "done", "owner", "bought")
	c.Tick()
	if got, _ = c.Get(it.ID); got.State != "done" || got.ResolvedBy != "owner" {
		t.Fatalf("expected done via ask: %+v", got)
	}
	now = now.Add(12 * time.Hour)
	c.Tick()
	if len(n.msgs) != 1 {
		t.Fatalf("nagged after done: %v", n.msgs)
	}
}

// An ask a calendar item raised carries the item and the day it was due, so
// both boards can file it as a calendar entry instead of as its session
// checking in. An ask raised directly carries neither.
func TestCalendarRaisedAskCarriesItsDay(t *testing.T) {
	c, th, _ := newTest(t)
	c.Now = func() time.Time { return time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local) }
	it, err := c.Add(Item{Title: "Buy SPY tranche 3", Day: "2026-09-03", Kind: "owner", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	got, _ := c.Get(it.ID)
	a, err := th.GetAsk(got.AskID)
	if err != nil {
		t.Fatal(err)
	}
	if a.CalID != it.ID || a.CalDay != "2026-09-03" {
		t.Fatalf("expected cal link %s/2026-09-03, got %q/%q", it.ID, a.CalID, a.CalDay)
	}
	// ListAsks takes the same path as the board.
	list, err := th.ListAsks("active", "", 50)
	if err != nil || len(list) == 0 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	for _, x := range list {
		if x.ID == a.ID && x.CalID != it.ID {
			t.Fatalf("cal link lost in ListAsks: %+v", x)
		}
	}
	direct, err := th.AddAsk(got.ThreadID, "", "Pick a risk tolerance", "", "decision", "")
	if err != nil {
		t.Fatal(err)
	}
	if direct.CalID != "" || direct.CalDay != "" {
		t.Fatalf("a session's own ask must not look dated: %+v", direct)
	}
}

func TestResolveClosesAskAndAgenda(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	tr, err := th.Create("Finance agent", "life", "make-more-money", "p", "weekly@Wed 09:00", "check balances", nil)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := c.Add(Item{Title: "Export Basic Capital", Day: "2026-09-03", At: "08:30", Kind: "owner", ThreadID: tr.ID})
	c.Tick()
	it, _ = c.Get(it.ID)
	if it.State != "fired" || it.ThreadID != tr.ID {
		t.Fatalf("%+v", it)
	}
	if _, err := c.Resolve(it.ID, "done", "owner", "exported"); err != nil {
		t.Fatal(err)
	}
	if a, _ := th.GetAsk(it.AskID); a.State != "done" {
		t.Fatalf("ask not closed: %+v", a)
	}
	// note kind → its own read card, open until the owner has read it (step 11)
	nt, _ := c.Add(Item{Title: "OPT-20 vests", Day: "2026-09-03", Kind: "note"})
	c.Tick()
	nt, _ = c.Get(nt.ID)
	if nt.State != "fired" || nt.AskID != nt.ID {
		t.Fatalf("note: %+v", nt)
	}
	if a, err := th.GetAsk(nt.ID); err != nil || a.Kind != "read" || !a.Active() {
		t.Fatalf("note card: %+v %v", a, err)
	}
	if _, err := th.ResolveAsk(nt.ID, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if nt, _ = c.Get(nt.ID); nt.State != "done" {
		t.Fatalf("read note: %+v", nt)
	}
	// The thread's next run is projected from the REAL clock (threads.Create
	// stamps NotBefore with time.Now), so the window runs to the next
	// Wednesday from today — a fixed "2026-09-10" went stale on 2026-09-10.
	wed := cadence.Next("weekly@Wed 09:00", time.Time{}, time.Now()).In(time.Local).Format("2006-01-02")
	v, err := c.Agenda("2026-09-03", wed)
	if err != nil {
		t.Fatal(err)
	}
	var runs, items int
	for _, d := range v.Days {
		for _, e := range d.Entries {
			switch e.Kind {
			case "run":
				runs++
				if d.Day != wed || e.At != "09:00" {
					t.Fatalf("projected run wrong: %s %+v", d.Day, e)
				}
			case "owner", "note":
				items++
			}
		}
	}
	if runs != 1 || items != 2 {
		t.Fatalf("agenda: runs=%d items=%d %+v", runs, items, v.Days)
	}
	if len(v.Anytime) != 0 {
		t.Fatalf("calendar-raised asks must not show as anytime: %+v", v.Anytime)
	}
	th.AddAsk(tr.ID, "", "Send the YouTube API key", "", "access", "")
	v, _ = c.Agenda("2026-09-03", "2026-09-10")
	if len(v.Anytime) != 1 || v.Anytime[0].Kind != "ask" {
		t.Fatalf("anytime: %+v", v.Anytime)
	}
}

// The owner closing their own step is a message, not a button: no note →
// refused; with a note → ONE prompt to the item's session carrying their
// words, and the item and its ask
// close through it, quietly. Reopen needs no words. Agents still close
// directly.
func TestOwnerStepClosesWithWords(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	tr, err := th.Create("Groceries", "life", "eat-well", "p", "", "restock", nil)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := c.Add(Item{Title: "Warehouse store run", Day: "2026-09-03", At: "08:30", Kind: "owner", Repeat: "monthly", ThreadID: tr.ID})
	c.Tick()
	it, _ = c.Get(it.ID)
	if it.State != "fired" || it.AskID == "" {
		t.Fatalf("%+v", it)
	}
	if _, err := c.Resolve(it.ID, "done", "owner", "  "); !errors.Is(err, ErrNoteRequired) {
		t.Fatalf("closed with nothing said: %v", err)
	}
	if got, _ := c.Get(it.ID); got.State != "fired" {
		t.Fatalf("refused close still wrote: %+v", got)
	}
	got, err := c.Resolve(it.ID, "done", "owner", "Went Saturday, freezer is full")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "done" || got.ResolvedBy != "owner" || got.Resolution != "Went Saturday, freezer is full" {
		t.Fatalf("%+v", got)
	}
	if a, _ := th.GetAsk(it.AskID); a.State != "done" {
		t.Fatalf("ask not closed: %+v", a)
	}
	ps, _ := th.ListPrompts("", tr.ID, 10)
	var owners []threads.Prompt
	for _, p := range ps {
		if p.Author == "owner" {
			owners = append(owners, p)
		}
	}
	if len(owners) != 1 || owners[0].InReplyTo != "cal:"+it.ID || owners[0].Outcome != "done" || owners[0].Text != "Went Saturday, freezer is full" {
		t.Fatalf("want exactly one prompt carrying the owner's words: %+v", owners)
	}
	if h := c.header(it.ID, "done"); !strings.Contains(h, "marked DONE") || !strings.Contains(h, `"Warehouse store run"`) {
		t.Fatalf("header: %q", h)
	}
	// Undo needs no words, and the monthly successor is still one.
	if re, err := c.Resolve(it.ID, "scheduled", "owner", ""); err != nil || re.State != "scheduled" || re.AskID != "" {
		t.Fatalf("reopen: %+v %v", re, err)
	}
	// Won't do → wont, dismissed.
	if _, err := c.Resolve(it.ID, "dismissed", "owner", "Skipping this month"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(it.ID); got.State != "dismissed" {
		t.Fatalf("%+v", got)
	}
	ps, _ = th.ListPrompts("", tr.ID, 10)
	var wont int
	for _, p := range ps {
		if p.Author == "owner" && p.Outcome == "wont" && p.InReplyTo == "cal:"+it.ID {
			wont++
		}
	}
	if wont != 1 {
		t.Fatalf("want one wont prompt: %+v", ps)
	}
	// An agent closing the owner's step needs no words — the gate is theirs.
	ag, _ := c.Add(Item{Title: "Renew parking", Day: "2026-09-03", Kind: "owner", ThreadID: tr.ID})
	if got, err := c.Resolve(ag.ID, "done", "claude:thread:"+tr.ID, ""); err != nil || got.State != "done" {
		t.Fatalf("agent close: %+v %v", got, err)
	}
}

// Talking about one of the owner's steps is not doing it: a prompt at
// `cal:<id>` with no outcome — the Respond sheet's "Reply" — reaches the
// session and leaves the item exactly where it was. Delivery closes an AGENT
// item, because only an agent item has a wake; otherwise a question about a
// step would mark it done.
func TestReplyToHisStepLeavesItOpen(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Now()
	tr, err := th.Create("Security", "life", "", "p", "", "decider", nil)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := c.Add(Item{Title: "Arm the decider code", Day: day(now), Kind: "owner", ThreadID: tr.ID})
	p, err := th.Queue(threads.Prompt{Author: "owner", Target: "new-or:" + tr.ID, InReplyTo: "cal:" + it.ID,
		Text: "is this still a valid calendar event for today?"})
	if err != nil || p.State != "delivered" {
		t.Fatalf("prompt: %+v %v", p, err)
	}
	got, _ := c.Get(it.ID)
	if got.State != "scheduled" || got.ResolvedBy != "" || got.Resolution != "" {
		t.Fatalf("the owner's step closed itself on a reply: %+v", got)
	}
}

// One clock (2026-08-28): an agent item IS a prompt row — queued for its
// fire time, referencing the item — and the clock's tick, not the
// calendar's, delivers it. Delivery closes the item and records the session.
func TestAgentItemStartsThread(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(7, 8, 30)
	c.Now = func() time.Time { return now }
	it, _ := c.Add(Item{Title: "Re-export Basic Capital statements", Day: day(now), Kind: "agent", Detail: "run lifectl statements import", GoalID: "make-more-money"})
	if it.PromptID == "" {
		t.Fatalf("no wake: %+v", it)
	}
	p, err := th.GetPrompt(it.PromptID)
	if err != nil || p.State != "queued" || p.Target != "new" || p.InReplyTo != "cal:"+it.ID || p.GoalID != "make-more-money" ||
		!p.NotBefore.Equal(ahead(7, 8, 0)) || !strings.Contains(p.Text, "run lifectl statements import") {
		t.Fatalf("wake: %+v %v", p, err)
	}
	c.Tick() // the calendar's tick leaves it to the clock
	if got, _ := c.Get(it.ID); got.State != "scheduled" {
		t.Fatalf("the tick fired a clocked item: %+v", got)
	}
	th.DuePrompts(now)
	it, _ = c.Get(it.ID)
	if it.State != "done" || it.ThreadID == "" || it.Resolution != "agent run started" {
		t.Fatalf("%+v", it)
	}
	tr, err := th.Get(it.ThreadID)
	if err != nil || tr.GoalID != "make-more-money" {
		t.Fatalf("thread: %+v %v", tr, err)
	}
	if p, _ := th.GetPrompt(it.PromptID); p.State != "delivered" || p.DeliveredThread != it.ThreadID {
		t.Fatalf("prompt: %+v", p)
	}
	// Closing or moving an item takes its wake with it.
	later, _ := c.Add(Item{Title: "Later", Day: day(ahead(14, 0, 0)), Kind: "agent", Detail: "x"})
	first := later.PromptID
	moved, _ := c.Update(later.ID, map[string]string{"at": "17:00"})
	if moved.PromptID == first {
		t.Fatalf("moved item kept its old wake: %+v", moved)
	}
	if p, _ := th.GetPrompt(first); p.State != "cancelled" {
		t.Fatalf("old wake: %+v", p)
	}
	if p, _ := th.GetPrompt(moved.PromptID); !p.NotBefore.Equal(ahead(14, 17, 0)) {
		t.Fatalf("new wake: %+v", p)
	}
	c.Resolve(later.ID, "dismissed", "owner", "")
	if p, _ := th.GetPrompt(moved.PromptID); p.State != "cancelled" {
		t.Fatalf("dismissed item's wake: %+v", p)
	}
	// Items from before the clock get a wake on the first tick.
	old, _ := c.Add(Item{Title: "Old", Day: day(ahead(15, 0, 0)), Kind: "agent", Detail: "x"})
	c.db.Exec(`UPDATE items SET prompt_id='' WHERE id=?`, old.ID)
	c.backfilled = false
	c.Tick()
	if got, _ := c.Get(old.ID); got.PromptID == "" || got.State != "scheduled" {
		t.Fatalf("backfill: %+v", got)
	}
}

// A dated ask (lifectl ask add --on / "NEEDS YOU <date>:") is an owner item
// carrying the ask it should mint: same kind, same check hint, same thread —
// just not until the day it can actually be done.
func TestDatedAskKeepsItsKindAndCheckHint(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Tranche plan.", "", "", nil)
	it, err := c.Add(Item{Title: "Buy tranche 2 at Broker B", Detail: "SPY $725 / SNOW $363 / TWLO $363",
		Day: "2026-08-27", Kind: "owner", ThreadID: tr.ID, Source: "claude:thread:" + tr.ID,
		AskKind: "decision", CheckHint: "a Broker B buy for SPY on 08-27"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(it.ID); got.AskKind != "decision" || got.CheckHint == "" {
		t.Fatalf("round-trip: %+v", got)
	}
	c.Tick()
	got, _ := c.Get(it.ID)
	a, err := th.GetAsk(got.AskID)
	if err != nil || a.Kind != "decision" || a.CheckHint != "a Broker B buy for SPY on 08-27" || a.ThreadID != tr.ID {
		t.Fatalf("minted ask: %+v %v", a, err)
	}
	// unset ask_kind keeps the old default
	it2, _ := c.Add(Item{Title: "Renew the PAT", Day: "2026-08-27", Kind: "owner", Source: "owner"})
	c.Tick()
	g2, _ := c.Get(it2.ID)
	if a2, _ := th.GetAsk(g2.AskID); a2.Kind != "physical" {
		t.Fatalf("default kind: %+v", a2)
	}
}

// every2d: the cadence the named repeats cannot say (a check-in every two
// days). Each firing spawns the next occurrence.
func TestEveryNDaysRepeatSpawnsNext(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(7, 20, 0)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Water check-in.", "", "", nil)
	it, err := c.Add(Item{Title: "Water check-in", Detail: "how many yesterday, how many today",
		Day: day(now), At: "20:00", Kind: "agent", Repeat: "every2d", ThreadID: tr.ID, Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	th.DuePrompts(now)
	next, _ := c.List(day(ahead(8, 0, 0)), day(ahead(13, 0, 0)), "")
	if len(next) != 1 || next[0].Day != day(ahead(9, 0, 0)) || next[0].At != "20:00" ||
		next[0].Repeat != "every2d" || next[0].PrevID != it.ID {
		t.Fatalf("expected one occurrence on %s: %+v", day(ahead(9, 0, 0)), next)
	}
	for _, bad := range []string{"every0d", "every400d", "every2days", "2d"} {
		if _, err := c.Add(Item{Title: "bad", Day: day(ahead(13, 0, 0)), Kind: "note", Repeat: bad}); err == nil {
			t.Fatalf("%s should be rejected", bad)
		}
	}
	// The next occurrence can be moved without dismissing it (dismissing a
	// repeating item spawns the one after, so it never actually moved).
	up, err := c.Update(next[0].ID, map[string]string{"at": "23:30", "detail": "later in the day"})
	if err != nil || up.At != "23:30" || up.Detail != "later in the day" || up.Repeat != "every2d" {
		t.Fatalf("update: %+v %v", up, err)
	}
	if _, err := c.Update(next[0].ID, map[string]string{"at": "half eleven"}); err == nil {
		t.Fatal("bad at should be rejected")
	}
	if again, _ := c.Get(next[0].ID); again.At != "23:30" {
		t.Fatalf("a rejected edit must not partially apply: %+v", again)
	}
	if _, err := c.Update(it.ID, map[string]string{"at": "09:00"}); err == nil {
		t.Fatal("a fired item is not editable")
	}
}

// Phase 0 (docs/reviews/2026-08-26-structure.md §3): an agent item whose
// session is gone must not error and retry every minute forever — a fresh
// session takes the run and the item records it.
func TestAgentItemWithDeadThreadStartsFreshOne(t *testing.T) {
	c, th, _ := newTest(t)
	now := ahead(7, 8, 30)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("", "life", "", "Will be archived.", "", "", nil)
	it, err := c.Add(Item{Title: "Check back on: X token", Day: day(now), Kind: "agent", ThreadID: tr.ID, Source: "hub:rec:rec-1"})
	if err != nil {
		t.Fatal(err)
	}
	th.Archive(tr.ID)
	c.Tick()
	th.DuePrompts(now)
	it, _ = c.Get(it.ID)
	if it.State != "done" || it.ThreadID == "" || it.ThreadID == tr.ID {
		t.Fatalf("expected a fresh session to take the run: %+v", it)
	}
}

// A repeating item that sat unfired for days spawns one next occurrence
// after today, not a backlog.
func TestSpawnNextNeverLandsInThePast(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, time.Local) // 10 days late
	c.Now = func() time.Time { return now }
	it, _ := c.Add(Item{Title: "Water check-in", Day: "2026-09-05", Kind: "note", Repeat: "every2d", Source: "owner"})
	c.db.Exec(`UPDATE items SET day='2026-08-26' WHERE id=?`, it.ID)
	c.Tick()
	next, _ := c.List("2026-08-27", "2026-09-30", "scheduled")
	if len(next) != 1 || next[0].Day != "2026-09-07" {
		t.Fatalf("expected exactly one next occurrence on 2026-09-07: %+v", next)
	}
}

// One item per thing per day, and closing a stray duplicate occurrence of a
// chain never forks a second chain (there should only ever be one).
func TestNoDuplicateItemsOrForkedChains(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	first, err := c.Add(Item{Title: "Venmo export", Day: "2026-10-01", Kind: "note", Repeat: "monthly", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(Item{Title: "venmo export", Day: "2026-10-01", Kind: "note", Source: "claude:thread:x"}); err == nil {
		t.Fatal("same title, kind and day should be refused as a duplicate")
	}
	if _, err := c.Add(Item{Title: "Venmo export", Day: "2026-10-01", Kind: "owner", Source: "owner"}); err != nil {
		t.Fatalf("a different kind is a different item: %v", err)
	}
	stray, err := c.Add(Item{Title: "Venmo export", Day: "2026-11-01", Kind: "note", Repeat: "monthly", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(stray.ID, "dismissed", "claude:thread:x", "duplicate"); err != nil {
		t.Fatal(err)
	}
	open, _ := c.List("2026-09-01", "2027-12-31", "scheduled")
	var n int
	for _, it := range open {
		if it.Kind == "note" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dismissing the stray must leave only %s: %+v", first.ID, open)
	}
}

// Reopening a fired item dismisses the ask it minted rather than orphaning
// it on the board; the item mints a new one when it fires again.
func TestReopenClosesTheMintedAsk(t *testing.T) {
	c, th, _ := newTest(t)
	c.Now = func() time.Time { return time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local) }
	it, _ := c.Add(Item{Title: "Buy SPY tranche 3", Day: "2026-09-03", Kind: "owner", Source: "owner"})
	c.Tick()
	fired, _ := c.Get(it.ID)
	if _, err := c.Resolve(it.ID, "scheduled", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if a, _ := th.GetAsk(fired.AskID); a.Active() {
		t.Fatalf("ask left open after reopen: %+v", a)
	}
	if got, _ := c.Get(it.ID); got.State != "scheduled" || got.AskID != "" {
		t.Fatalf("%+v", got)
	}
}

// fakeSource: a DatedSource that hands the agenda one action on the window's
// first day and one deferred rec on its last, or fails outright.
type fakeSource struct {
	fail   bool
	thread string // the session the did row belongs to
}

func (f fakeSource) Dated(from, to string) ([]Dated, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return []Dated{
		{ID: "20260903-0900-abcd", Kind: "action", Title: "Move $500 to savings", State: "approved", Day: from, ThreadID: "t-1", Actor: "app"},
		{ID: "rec-1234", Kind: "rec", Title: "Try the X token", State: "deferred", Day: to, GoalID: "g", Actor: "claude:thread:t-2"},
		{ID: "outside", Kind: "action", Title: "not in window", State: "done", Day: "2099-09-20", Actor: "app"},
		{ID: "ask-did", Kind: "ask", Ref: "ask:ask-did", Key: "did:ask:ask-did", Title: "Read the gold verdict", State: "done", Day: from, At: "21:04",
			ThreadID: f.thread, Actor: "owner", Did: true, Verb: "Read"},
	}, nil
}

// Phase 2 (docs/reviews/2026-08-26-structure.md §5): the agenda is the one
// read model over everything dated. Actions and deferred recs arrive through
// DatedSource on their day with a typed ref; a failing source is skipped, and
// every entry kind carries actor + ref.
func TestAgendaCarriesEveryDatedObject(t *testing.T) {
	c, th, _ := newTest(t)
	// Relative dates: a standing schedule's next fire comes from the real
	// clock, so a pinned window date-rots (this one died on 2026-09-04).
	now, next := ahead(7, 7, 0), ahead(8, 0, 0)
	c.Now = func() time.Time { return now }
	it, _ := c.Add(Item{Title: "Buy SPY tranche 3", Day: day(now), Kind: "owner", Source: "claude:thread:t-9"})
	tr, _ := th.Create("Finance agent", "life", "", "p", "daily@09:00", "check", nil)
	c.Sources = []DatedSource{fakeSource{fail: true}, fakeSource{thread: tr.ID}}
	th.AddAsk(tr.ID, "", "Send the key", "", "access", "")
	v, err := c.Agenda(day(now), day(next))
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]Entry{}
	for _, d := range v.Days {
		for _, e := range d.Entries {
			if e.Ref == "" || e.Actor == "" {
				t.Fatalf("entry without actor/ref: %+v", e)
			}
			byRef[e.Ref] = e
			if e.Day != d.Day {
				t.Fatalf("entry on the wrong day: %s vs %+v", d.Day, e)
			}
		}
	}
	if a := byRef["action:20260903-0900-abcd"]; a.Day != day(now) || a.Kind != "action" || a.State != "approved" || a.Actor != "app" || a.ThreadID != "t-1" || a.ID != "action:20260903-0900-abcd" {
		t.Fatalf("action entry: %+v", a)
	}
	if r := byRef["rec:rec-1234"]; r.Day != day(next) || r.Kind != "rec" || r.State != "deferred" || r.GoalID != "g" || r.Actor != "claude:thread:t-2" {
		t.Fatalf("rec entry: %+v", r)
	}
	if _, ok := byRef["action:outside"]; ok {
		t.Fatal("a row outside the window leaked in")
	}
	// A did row keeps its record fields and is named for its session, so a
	// reader can fold an evening's answers into one box titled by the work.
	if d := byRef["ask:ask-did"]; !d.Did || d.Verb != "Read" || d.ID != "did:ask:ask-did" || d.At != "21:04" || d.Actor != "owner" || d.ThreadTitle != "Finance agent" {
		t.Fatalf("did entry: %+v", d)
	}
	if e := byRef["thread:"+tr.ID]; e.ThreadTitle != "Finance agent" {
		t.Fatalf("run entry without its session's title: %+v", e)
	}
	if e := byRef["cal:"+it.ID]; e.Actor != "claude:thread:t-9" || e.Kind != "owner" {
		t.Fatalf("item entry: %+v", e)
	}
	if e := byRef["thread:"+tr.ID]; e.Kind != "run" || e.Actor != "claude:thread:"+tr.ID || e.At != "09:00" || e.Day != day(next) {
		t.Fatalf("run entry: %+v", e)
	}
	if len(v.Anytime) != 1 || v.Anytime[0].Ref != "ask:"+v.Anytime[0].AskID || v.Anytime[0].Actor != "claude:thread:"+tr.ID {
		t.Fatalf("anytime ask: %+v", v.Anytime)
	}
}

// The agenda is the queued prompts, placed (one clock): a standing row shows
// once per occurrence in the window, a job's daily row as `job`, an every@
// row not at all, a one-shot on its day (as its session when it has one),
// and an agent item's own wake never twice — the item is already there.
func TestAgendaIsTheQueuedPromptsPlaced(t *testing.T) {
	c, th, _ := newTest(t)
	now, next := ahead(7, 7, 0), ahead(8, 0, 0)
	c.Now = func() time.Time { return now }
	tr, _ := th.Create("Finance agent", "life", "make-more-money", "p", "daily@09:00", "check balances", nil)
	th.SetStanding("hub", "job:sweep", "daily@07:30", "Sweep the ledger", time.Time{})
	th.SetStanding("hub", "job:hourly", "every@1h", "", time.Time{})
	th.Queue(threads.Prompt{Author: "owner", Target: tr.ID, Text: "Tomorrow: sell the lot", NotBefore: ahead(8, 6, 0)})
	th.Queue(threads.Prompt{Author: "owner", Target: "new", Title: "Fresh look", Text: "Start over", NotBefore: ahead(8, 12, 0)})
	it, _ := c.Add(Item{Title: "Re-export", Day: day(next), At: "10:00", Kind: "agent", Detail: "x", ThreadID: tr.ID})
	v, err := c.Agenda(day(now), day(next))
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]Entry{}
	for _, d := range v.Days {
		for _, e := range d.Entries {
			keys[e.ID] = e
		}
	}
	for _, d := range []string{now.Format("20060102"), next.Format("20060102")} {
		if e := keys["run:"+tr.ID+":"+d+"T0900"]; e.Kind != "run" || e.Ref != "thread:"+tr.ID || e.Detail != "check balances" || e.Repeat != "daily@09:00" {
			t.Fatalf("run %s: %+v", d, e)
		}
		if e := keys["job:sweep:"+d+"T0730"]; e.Kind != "job" || e.Ref != "job:sweep" || e.Detail != "Sweep the ledger" || e.Actor != "claude:job:sweep" {
			t.Fatalf("job %s: %+v", d, e)
		}
	}
	for k, e := range keys {
		if strings.HasPrefix(k, "job:hourly") {
			t.Fatalf("every@ row on the agenda: %+v", e)
		}
		if e.Ref == "prompt:"+it.PromptID || e.ID == "prompt:"+it.PromptID {
			t.Fatalf("an agent item's wake shown twice: %+v", e)
		}
	}
	var msg, fresh Entry
	for _, e := range keys {
		if e.Kind == "run" && e.Detail == "Tomorrow: sell the lot" {
			msg = e
		}
		if e.Kind == "run" && e.Title == "Fresh look" {
			fresh = e
		}
	}
	if msg.Day != day(next) || msg.At != "06:00" || msg.Ref != "thread:"+tr.ID || msg.Actor != "owner" || msg.ThreadID != tr.ID {
		t.Fatalf("one-shot to a session: %+v", msg)
	}
	if fresh.Day != day(next) || fresh.At != "12:00" || !strings.HasPrefix(fresh.Ref, "prompt:p-") || fresh.ThreadID != "" {
		t.Fatalf("one-shot to a new session: %+v", fresh)
	}
	if e := keys[it.ID]; e.Kind != "agent" || e.Day != day(next) {
		t.Fatalf("item: %+v", e)
	}
	// Schedule off = no rows.
	th.SetSchedule(tr.ID, "", "")
	v, _ = c.Agenda(day(now), day(next))
	for _, d := range v.Days {
		for _, e := range d.Entries {
			if e.Ref == "thread:"+tr.ID && e.Kind == "run" && e.Repeat != "" {
				t.Fatalf("schedule off still projected: %+v", e)
			}
		}
	}
}

// A dated ask keeps its explicit surface to the day it fires.
func TestDatedAskKeepsItsSurface(t *testing.T) {
	c, th, _ := newTest(t)
	c.Now = func() time.Time { return time.Date(2026, 8, 27, 9, 0, 0, 0, time.Local) }
	it, err := c.Add(Item{Title: "Approve the PR", Day: "2026-08-27", Kind: "owner", Source: "owner", AskKind: "read", Surface: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	got, _ := c.Get(it.ID)
	if a, _ := th.GetAsk(got.AskID); a.Surface != "web" {
		t.Fatalf("surface dropped on fire: %+v", a)
	}
	if _, ok := c.OpenBySource("hub:rec:none"); ok {
		t.Fatal("found an item that does not exist")
	}
}

// The tab's number is the owner's steps for the day, not the tick's progress:
// an `owner` item dated today counts before 08:00 fires it, stays counted once
// fired, and drops when closed. Agent runs and notes never count, and
// tomorrow's step waits for tomorrow.
// Yesterday's unfinished steps stack in the tray even when the agenda being
// drawn starts today — listing only items from before `from` would hide the
// viewed day's own leftovers.
func TestOverdueStacksYesterdaysStepsWhateverTheWindow(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	fired, _ := c.Add(Item{Title: "Download the X archive", Day: "2026-09-02", At: "18:00", Kind: "owner", Source: "owner"})
	c.db.Exec(`UPDATE items SET state='open' WHERE id=?`, fired.ID)
	never, _ := c.Add(Item{Title: "Drink the juice", Day: "2026-09-02", At: "19:30", Kind: "owner", Source: "owner"})
	done, _ := c.Add(Item{Title: "Water the plants", Day: "2026-09-02", Kind: "owner", Source: "owner"})
	c.Resolve(done.ID, "done", "owner", "watered")
	c.Add(Item{Title: "Water check-in", Day: "2026-09-02", Kind: "agent", Detail: "count", Source: "owner"})
	c.Add(Item{Title: "Sell 5 GLD", Day: "2026-09-03", Kind: "owner", Source: "owner"})
	// Earlier today counts too: a 09:00 step at 10:00 is in the tray, a step
	// later today is not, and today's all-day step waits — the day is not over.
	c.Add(Item{Title: "Morning stretch", Day: "2026-09-03", At: "09:00", Kind: "owner", Source: "owner"})
	c.Add(Item{Title: "Juice with dinner", Day: "2026-09-03", At: "19:30", Kind: "owner", Source: "owner"})

	// The window the phone asks for in Day mode: a week either side of Sep 2,
	// so both leftovers are IN it — they must still be in the tray.
	v, err := c.Agenda("2026-08-26", "2026-09-09")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range v.Overdue {
		got = append(got, e.Title)
		if !e.Overdue {
			t.Fatalf("tray row not flagged overdue: %+v", e)
		}
	}
	want := []string{"Download the X archive", "Drink the juice", "Morning stretch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tray: want %v, got %v", want, got)
	}
	// It is a tray, not a filter: the rows keep their own cells too.
	var onDay int
	for _, d := range v.Days {
		if d.Day == "2026-09-02" {
			for _, e := range d.Entries {
				if e.ID == fired.ID || e.ID == never.ID {
					onDay++
					if !e.Overdue {
						t.Fatalf("row on its day not flagged overdue: %+v", e)
					}
				}
			}
		}
	}
	if onDay != 2 {
		t.Fatalf("want both rows still on Sep 2, got %d", onDay)
	}
	// An agenda that starts today (Schedule, Upcoming) carries the same tray.
	v2, _ := c.Agenda("2026-09-03", "2026-11-02")
	if len(v2.Overdue) != 3 {
		t.Fatalf("upcoming agenda tray: want 3, got %d", len(v2.Overdue))
	}
	// Closing one empties its slot in the tray.
	if _, err := c.Resolve(never.ID, "done", "owner", "drank it"); err != nil {
		t.Fatal(err)
	}
	v3, _ := c.Agenda("2026-09-03", "2026-11-02")
	if len(v3.Overdue) != 2 || v3.Overdue[0].ID != fired.ID {
		t.Fatalf("after closing one: %+v", v3.Overdue)
	}
}

func TestDueCountIsTodaysOpenSteps(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 8, 27, 2, 30, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	buy, _ := c.Add(Item{Title: "Buy tranche 2", Day: "2026-08-27", Kind: "owner", Source: "owner"})
	c.Add(Item{Title: "Check the Amazon export", Day: "2026-08-27", Kind: "agent", Detail: "check", Source: "owner"})
	c.Add(Item{Title: "Vest date", Day: "2026-08-27", Kind: "note", Source: "owner"})
	c.Add(Item{Title: "Buy tranche 3", Day: "2026-09-03", Kind: "owner", Source: "owner"})
	if n, _ := c.DueCount(); n != 1 {
		t.Fatalf("before the tick fires: want 1, got %d", n)
	}
	now = now.Add(7 * time.Hour) // 09:30, fired
	c.Tick()
	if got, _ := c.Get(buy.ID); got.State != "fired" {
		t.Fatalf("expected fired: %+v", got)
	}
	if n, _ := c.DueCount(); n != 1 {
		t.Fatalf("after firing: want 1, got %d", n)
	}
	if _, err := c.Resolve(buy.ID, "done", "owner", "bought"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.DueCount(); n != 0 {
		t.Fatalf("after done: want 0, got %d", n)
	}
}

// The drag is honest about state and chains: dragging an event changes its
// time, and a recurring one asks whether it is this one or all future ones.
func TestDragMovesFiredAndRepeatingItems(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }

	// A fired step moves: back to scheduled, its ask off the board as
	// "rescheduled", and the tick fires it again at the new moment.
	juice, _ := c.Add(Item{Title: "Drink the juice", Day: "2026-09-03", At: "07:30", Kind: "owner", Source: "owner"})
	c.Tick()
	f, _ := c.Get(juice.ID)
	if f.State != "fired" || f.AskID == "" {
		t.Fatalf("setup: %+v", f)
	}
	askID := f.AskID
	// New words on the open card move nothing (a second card's content
	// is folded into the fired step); its title stays locked.
	if w, err := c.Update(juice.ID, map[string]string{"detail": "The copy to paste"}); err != nil || w.State != "fired" || w.AskID != askID || w.Detail != "The copy to paste" {
		t.Fatalf("a fired item takes new detail and stays fired: %+v %v", w, err)
	}
	if _, err := c.Update(juice.ID, map[string]string{"title": "Drink water"}); err == nil {
		t.Fatal("a fired item's title must stay locked")
	}
	moved, err := c.Update(juice.ID, map[string]string{"at": "19:00"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.State != "scheduled" || moved.AskID != "" || moved.At != "19:00" || moved.NagCount != 0 {
		t.Fatalf("expected rescheduled: %+v", moved)
	}
	// The step is its card, so a step back on the schedule is off the board.
	if a, err := th.GetAsk(askID); err == nil {
		t.Fatalf("card should leave the board as rescheduled: %+v", a)
	}
	var why string
	c.db.QueryRow(`SELECT note FROM item_events WHERE item_id=? ORDER BY id DESC LIMIT 1`, juice.ID).Scan(&why)
	if why != "rescheduled to 2026-09-03 19:00" {
		t.Fatalf("trail: %q", why)
	}
	c.Tick() // 09:00 < 19:00 — must not refire
	if g, _ := c.Get(juice.ID); g.State != "scheduled" {
		t.Fatalf("refired early: %+v", g)
	}
	now = time.Date(2026, 9, 3, 19, 0, 0, 0, time.Local)
	c.Tick()
	if g, _ := c.Get(juice.ID); g.State != "fired" || g.AskID == "" {
		t.Fatalf("expected a fresh fire at the new time: %+v", g)
	}

	// A fired item only MOVES — its words are not editable in the same gesture.
	if _, err := c.Update(juice.ID, map[string]string{"at": "20:00", "title": "x"}); err == nil {
		t.Fatal("editing a fired item's title should refuse")
	}
	// Done and dismissed still refuse a move outright.
	c.Resolve(juice.ID, "done", "verifier", "drunk")
	if _, err := c.Update(juice.ID, map[string]string{"at": "21:00"}); err == nil {
		t.Fatal("moving a done item should refuse")
	}

	// scope=one detaches an occurrence: the chain continues at the ORIGINAL
	// time, and the moved row stops repeating.
	plants, _ := c.Add(Item{Title: "Water the plants", Day: "2026-09-04", At: "08:00", Kind: "owner", Repeat: "daily", Source: "owner"})
	one, err := c.Update(plants.ID, map[string]string{"at": "10:15", "scope": "one"})
	if err != nil {
		t.Fatal(err)
	}
	if one.At != "10:15" || one.Repeat != "" {
		t.Fatalf("expected a detached occurrence: %+v", one)
	}
	kids, _ := c.List("2026-09-05", "2026-09-05", "")
	if len(kids) != 1 || kids[0].PrevID != plants.ID || kids[0].At != "08:00" || kids[0].Repeat != "daily" {
		t.Fatalf("the chain should continue from the original shape: %+v", kids)
	}
	// Closing the detached row later cannot fork the chain.
	if _, err := c.Resolve(plants.ID, "done", "verifier", "watered"); err != nil {
		t.Fatal(err)
	}
	if again, _ := c.List("2026-09-05", "2026-09-06", ""); len(again) != 1 {
		t.Fatalf("the chain forked: %+v", again)
	}

	// scope=future on a FIRED repeating item reaches the already-spawned next
	// occurrence too — fire() mints it at fire time with the old clock.
	pouch, _ := c.Add(Item{Title: "Water check", Day: "2026-09-03", At: "07:00", Kind: "owner", Repeat: "daily", Source: "owner"})
	c.Tick() // 19:00 — fires, spawns 09-04 at 07:00
	if p, _ := c.Get(pouch.ID); p.State != "fired" {
		t.Fatalf("setup: %+v", p)
	}
	if _, err := c.Update(pouch.ID, map[string]string{"at": "21:00", "scope": "future"}); err != nil {
		t.Fatal(err)
	}
	next, _ := c.List("2026-09-04", "2026-09-04", "scheduled")
	if len(next) != 1 || next[0].At != "21:00" || next[0].Repeat != "daily" {
		t.Fatalf("the chain should carry the new time: %+v", next)
	}
	// And an unknown scope is refused, never guessed.
	if bad, err := c.Update(pouch.ID, map[string]string{"at": "22:00", "scope": "bogus"}); err == nil {
		t.Fatalf("bogus scope accepted: %+v", bad)
	}
}

// CloseBySource: a practice homework closes itself when a full round
// finishes — today's scheduled item AND a fired one never answered, with
// the round as the note; the daily chain goes on.
func TestCloseBySource(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 12, 7, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	src := "hub:pitch:daily"
	// Due "by": an on-the-day round from yesterday would be closed as missed
	// by the tick before it could fire (TestOnTheDayChoreIsMissedNotOverdue).
	old, err := c.Add(Item{Title: "Perfect pitch: one round", Day: "2026-09-11", Kind: "homework", Repeat: "daily", Source: src, NagMin: 1440, Due: "by"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick() // 09-11 is past: fires; the chain skips to tomorrow (spawnNext never lands on today)
	if got, _ := c.Get(old.ID); got.State != "fired" || got.AskID == "" {
		t.Fatalf("yesterday's should have fired: %+v", got)
	}
	if n := c.CloseBySource("hub:other", "hub", "x"); n != 0 {
		t.Fatalf("closed a stranger's: %d", n)
	}
	if n := c.CloseBySource(src, "hub", "round pr-1: 18/20 (90%) on C D E G A"); n != 1 {
		t.Fatalf("closed %d, want yesterday's fired one", n)
	}
	got, _ := c.Get(old.ID)
	if got.State != "done" || got.ResolvedBy != "hub" || !strings.Contains(got.Resolution, "18/20") {
		t.Fatalf("yesterday's: %+v", got)
	}
	if a, _ := th.GetAsk(got.AskID); a.State != "done" {
		t.Fatalf("its ask stays open: %+v", a)
	}
	// Tomorrow's stays scheduled — a round today is not tomorrow's homework.
	next, _ := c.List("2026-09-13", "2026-09-13", "scheduled")
	if len(next) != 1 || next[0].Source != src {
		t.Fatalf("tomorrow missing: %+v", next)
	}
	// A round before the day's item fires closes it too, and the chain
	// goes on from it.
	now = time.Date(2026, 9, 13, 7, 0, 0, 0, time.Local)
	if n := c.CloseBySource(src, "hub", "round pr-2: 19/20 (95%) on C D E G A"); n != 1 {
		t.Fatalf("closed %d, want today's scheduled one", n)
	}
	if after, _ := c.List("2026-09-14", "2026-09-14", "scheduled"); len(after) != 1 {
		t.Fatalf("the chain should go on: %+v", after)
	}
	if open, _ := c.List("2026-09-01", "2026-09-13", "scheduled"); len(open) != 0 {
		t.Fatalf("still open: %+v", open)
	}
}

// A `homework` item is a PRACTICE of the owner's from Learn: it counts on the
// tab, fires a physical ask of class `practice` on the hub's calendar thread
// — never on a session, whatever thread the item names — pushes once and
// never nags, the chain keeps the kind, and when the next occurrence fires
// the earlier one still open is closed as `missed` (one missed on Tuesday is
// not done twice on Wednesday). It draws in its own light-blue lane and
// closes with a TICK rather than words.
func TestHomeworkIsHisStepInItsOwnLane(t *testing.T) {
	c, th, _ := newTest(t)
	now := time.Date(2026, 9, 12, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	host, _ := th.CreateIdle("pitch-host", "Perfect Pitch daily homework", "life")
	it, err := c.Add(Item{Title: "Perfect pitch: one round", Day: "2026-09-12", Kind: "homework", Repeat: "daily", Source: "hub:pitch:daily", ThreadID: host.ID})
	if err != nil {
		t.Fatal(err)
	}
	if it.NagMin != 0 {
		t.Fatalf("a practice is one reminder, no nag: %+v", it)
	}
	if n, _ := c.DueCount(); n != 1 {
		t.Fatalf("homework counts on the tab: %d", n)
	}
	c.Tick()
	got, _ := c.Get(it.ID)
	if got.State != "fired" || got.AskID == "" {
		t.Fatalf("homework fires an ask: %+v", got)
	}
	a, _ := th.GetAsk(got.AskID)
	if a.Kind != "physical" || a.Class != threads.ClassPractice || !threads.OwnerClass(a.Class) {
		t.Fatalf("its ask is a physical practice: %+v", a)
	}
	if a.ThreadID != c.FallbackThread || got.ThreadID != c.FallbackThread {
		t.Fatalf("a practice has no host session — it lands on the calendar thread: ask on %q, item on %q", a.ThreadID, got.ThreadID)
	}
	if h, _ := th.Get(host.ID); h.Status == "needs_you" {
		t.Fatalf("the session it named is not the owner's turn: %+v", h)
	}
	if next, _ := c.List("2026-09-13", "2026-09-13", "scheduled"); len(next) != 1 || next[0].Kind != "homework" || next[0].NagMin != 0 {
		t.Fatalf("the chain keeps the kind and the no-nag: %+v", next)
	}
	// The next morning: today's fires, yesterday's is missed — not stacked.
	now = time.Date(2026, 9, 13, 9, 0, 0, 0, time.Local)
	c.Tick()
	if od, _ := c.Overdue(); len(od) != 0 {
		t.Fatalf("a missed practice does not stack in the inbox: %+v", od)
	}
	old, _ := c.Get(it.ID)
	if old.State != "dismissed" || old.ResolvedBy != "hub" || old.Resolution != "missed" {
		t.Fatalf("yesterday's is missed: %+v", old)
	}
	if a, _ := th.GetAsk(got.AskID); a.State != "dismissed" {
		t.Fatalf("and its ask closed with it: %+v", a)
	}
	todays, _ := c.List("2026-09-13", "2026-09-13", "fired")
	if len(todays) != 1 {
		t.Fatalf("today's is the one open: %+v", todays)
	}
	it = todays[0]
	// Homework is a completion: the owner ticks it with nothing said (an `owner` step
	// still needs words), and the tick closes its ask with it.
	done, err := c.Resolve(it.ID, "done", "owner", "")
	if err != nil || done.State != "done" || done.ResolvedBy != "owner" {
		t.Fatalf("the owner's homework closes with a tick: %v %+v", err, done)
	}
	if a, _ := th.GetAsk(it.AskID); a.State == "open" {
		t.Fatalf("the tick closes its ask: %+v", a)
	}
	if _, err := c.Resolve(it.ID, "scheduled", "owner", ""); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// An `owner` item is a STEP: its ask stays on its session (the owner's words
	// about it go there), class `step`, and the session is still not "your turn".
	step, _ := c.Add(Item{Title: "Book the dentist", Day: "2026-09-13", Kind: "owner", ThreadID: host.ID})
	c.Tick()
	fired, _ := c.Get(step.ID)
	if sa, _ := th.GetAsk(fired.AskID); sa.Class != threads.ClassStep || sa.ThreadID != host.ID {
		t.Fatalf("a step's ask is a step on its own session: %+v", sa)
	}
	if h, _ := th.Get(host.ID); h.Status == "needs_you" {
		t.Fatalf("a step does not make its session the owner's turn: %+v", h)
	}
	task, _ := c.Add(Item{Title: "Draft the letter", Day: "2026-09-13", Kind: "owner", ThreadID: host.ID})
	if _, err := c.Resolve(task.ID, "done", "owner", ""); !errors.Is(err, ErrNoteRequired) {
		t.Fatalf("an owner step a session waits on still closes with words: %v", err)
	}
	if _, err := c.Add(Item{Title: "bad", Day: "2026-09-12", Kind: "lesson"}); err == nil || !strings.Contains(err.Error(), "homework") {
		t.Fatalf("the kind list names homework: %v", err)
	}
}

// An agent run is never all day: the hub fires it at one minute, so that minute is stamped
// on the row — 08:00, then 30 minutes past every other agent run that day —
// on add, on any edit that would leave it timeless, and once at boot for the
// rows from before. The owner's own steps keep "all day" (any time today).
func TestAgentRunAlwaysHasAMinute(t *testing.T) {
	c, _, _ := newTest(t)
	d := day(ahead(3, 0, 0))
	a, err := c.Add(Item{Title: "Score rec-1", Day: d, Kind: "agent"})
	if err != nil || a.At != "08:00" {
		t.Fatalf("first agent run of the day is 08:00: %+v %v", a, err)
	}
	b, _ := c.Add(Item{Title: "Score rec-2", Day: d, Kind: "agent"})
	if b.At != "08:30" {
		t.Fatalf("second is staggered: %+v", b)
	}
	if x, _ := c.Add(Item{Title: "Step count", Day: d, At: "23:30", Kind: "agent"}); x.At != "23:30" {
		t.Fatalf("a chosen time is kept: %+v", x)
	}
	// A drop on the all-day band (at: "") does not make it timeless.
	if m, err := c.Update(a.ID, map[string]string{"at": ""}); err != nil || m.At != "08:00" {
		t.Fatalf("an all-day drop keeps a minute (its own slot is free): %+v %v", m, err)
	}
	// The owner's step stays all day; retagged to agent it gains the next free slot.
	step, _ := c.Add(Item{Title: "Export the statement", Day: d, Kind: "owner"})
	if step.At != "" {
		t.Fatalf("the owner's step may be all day: %+v", step)
	}
	if r, err := c.Update(step.ID, map[string]string{"kind": "agent"}); err != nil || r.At != "09:00" || r.PromptID == "" {
		t.Fatalf("retagged to agent it gets a minute and a wake: %+v %v", r, err)
	}
	// Rows from before the rule are stamped on the first tick, and their wake
	// moves with them.
	c.db.Exec(`INSERT INTO items (id,src,created_at,updated_at,title,detail,kind,day,at,repeat,source,state,nag_min,ask_kind,check_hint,surface)
		VALUES ('cal-old1','cal','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z','Verify the purge ran','','agent',?,'','','owner','scheduled',0,'','','')`, d)
	c.db.Exec(`INSERT INTO items (id,src,created_at,updated_at,title,detail,kind,day,at,repeat,source,state,nag_min,ask_kind,check_hint,surface)
		VALUES ('cal-old2','cal','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z','Skipped check','','agent',?,'','','owner','dismissed',0,'','','')`, d)
	c.Tick()
	old, _ := c.Get("cal-old1")
	if old.At != "09:30" || old.PromptID == "" {
		t.Fatalf("a timeless agent row from before is stamped and queued: %+v", old)
	}
	if closed, _ := c.Get("cal-old2"); closed.At != "08:00" || closed.PromptID != "" {
		t.Fatalf("a closed one takes the hour it always fired at, no wake: %+v", closed)
	}
	n, _ := c.List(d, d, "")
	if len(n) != 6 {
		t.Fatalf("six rows on the day: %d", len(n))
	}
	for _, it := range n {
		if it.Kind == "agent" && it.At == "" {
			t.Fatalf("no agent row is timeless: %+v", it)
		}
	}
}

// The next occurrence of a repeating dated ask keeps what shapes its card —
// before 2026-09-14 spawnNext dropped surface, ask kind and check hint, so
// only the first occurrence was a decision on the web.
func TestNextOccurrenceKeepsItsAskShape(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	it, err := c.Add(Item{Title: "Approve the weekly buy", Day: "2026-09-03", Kind: "owner", Repeat: "weekly",
		Surface: "web", AskKind: "decision", CheckHint: "a buy on the day"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	next, _ := c.List("2026-09-10", "2026-09-10", "")
	if len(next) != 1 || next[0].PrevID != it.ID || next[0].Surface != "web" || next[0].AskKind != "decision" || next[0].CheckHint != "a buy on the day" {
		t.Fatalf("next occurrence lost its shape: %+v", next)
	}
}

type blockingNfy struct{ release chan struct{} }

func (b *blockingNfy) NeedsYou(string) error { <-b.release; return nil }

// A nag is a push; the tick must not hold the calendar lock while it waits on
// one, or a Resolve from either surface waits on the network.
func TestTickDoesNotHoldTheLockAcrossAPush(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	nag, _ := c.Add(Item{Title: "Call the dentist", Day: "2026-09-03", Kind: "owner"})
	other, _ := c.Add(Item{Title: "Renew the PAT", Day: "2026-09-04", Kind: "owner"})
	c.Tick()
	if got, _ := c.Get(nag.ID); got.State != "fired" {
		t.Fatalf("not fired: %+v", got)
	}
	b := &blockingNfy{release: make(chan struct{})}
	c.Nfy = b
	now = now.Add(7 * time.Hour)
	done := make(chan struct{})
	go func() { c.Tick(); close(done) }()
	time.Sleep(50 * time.Millisecond)
	resolved := make(chan error, 1)
	go func() { _, err := c.Resolve(other.ID, "dismissed", "hub", "not needed"); resolved <- err }()
	select {
	case err := <-resolved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		close(b.release)
		t.Fatal("Resolve waited on the tick's push")
	}
	close(b.release)
	<-done
	if got, _ := c.Get(nag.ID); got.NagCount != 1 {
		t.Fatalf("nag counted once: %+v", got)
	}
}

// OnDelivered (the clock, no calendar lock) and a Resolve of the same
// repeating agent item race to close it; exactly one next occurrence exists.
func TestDeliveredAndResolvedSpawnOneNext(t *testing.T) {
	for i := 0; i < 20; i++ {
		c, _, _ := newTest(t)
		it, err := c.Add(Item{Title: "Step count", Day: day(ahead(2, 0, 0)), At: "20:00", Kind: "agent", Repeat: "daily"})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		fin := make(chan struct{}, 2)
		go func() {
			<-start
			c.OnDelivered(threads.Prompt{ID: "p-x", InReplyTo: "cal:" + it.ID}, "calendar")
			fin <- struct{}{}
		}()
		go func() {
			<-start
			c.Resolve(it.ID, "done", "hub", "ran")
			fin <- struct{}{}
		}()
		close(start)
		<-fin
		<-fin
		var n int
		c.db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='cal' AND state='scheduled' AND lower(title)='step count' AND id<>?`, it.ID).Scan(&n)
		if n != 1 {
			t.Fatalf("run %d: %d next occurrences", i, n)
		}
	}
}

// The console and the phone each worked out a row's lane, closed look,
// drag rule, "why not" sentence and kind word — and drifted (a Reopen on one
// surface only, "agent" vs "agent runs it"). The hub stamps them once now.
func TestEntryIsStampedOnceForBothSurfaces(t *testing.T) {
	st := func(e Entry) Entry { stampEntry(&e); return e }
	lane := func(e Entry) string { return st(e).Lane }
	for k, want := range map[string]string{"owner": "mine", "ask": "mine", "note": "mine", "homework": "homework",
		"agent": "scheduled", "run": "agents", "job": "agents", "rec": "recs", "whatever-comes-next": "agents"} {
		if got := lane(Entry{Kind: k, State: "scheduled", Ref: k + ":x"}); got != want {
			t.Errorf("kind %s: lane %q, want %q", k, got, want)
		}
	}
	// A one-shot prompt wake is a scheduled run; a cadence check-in is not.
	if lane(Entry{Kind: "run", Ref: "prompt:p-1"}) != "scheduled" || lane(Entry{Kind: "run", Ref: "thread:t", Repeat: "daily@08:00"}) != "agents" {
		t.Error("run lanes")
	}
	// A record is its doer's — except homework (blue) and a rec (purple).
	if lane(Entry{Kind: "ask", Did: true, Actor: "owner", Ref: "ask:a"}) != "mine" ||
		lane(Entry{Kind: "action", Did: true, Actor: "auto", State: "done"}) != "agents" ||
		lane(Entry{Kind: "owner", Did: true, Actor: "hub", Ref: "cal:c"}) != "mine" ||
		lane(Entry{Kind: "agent", Did: true, Actor: "hub", Ref: "cal:c"}) != "scheduled" ||
		lane(Entry{Kind: "agent", Did: true, Actor: "owner", Ref: "cal:c", State: "dismissed"}) != "scheduled" ||
		lane(Entry{Kind: "homework", Did: true, Actor: "hub", Ref: "cal:c"}) != "homework" ||
		lane(Entry{Kind: "rec", Did: true, Actor: "owner", State: "accepted"}) != "recs" {
		t.Error("did lanes")
	}
	if lane(Entry{Kind: "action", State: "proposed"}) != "mine" || lane(Entry{Kind: "action", State: "approved"}) != "agents" {
		t.Error("action lanes")
	}
	// A chore: a dated step of the owner's no session is behind (step 10) —
	// its own lane open or closed; a session's step and a "soon" to-do stay
	// the owner's.
	if lane(Entry{Kind: "owner", Day: "2026-09-27", Ref: "cal:c", State: "scheduled"}) != "chores" ||
		lane(Entry{Kind: "owner", Day: "2026-09-27", Ref: "cal:c", ThreadID: "calendar", Did: true, Actor: "owner"}) != "chores" ||
		lane(Entry{Kind: "owner", Day: "2026-09-27", Ref: "cal:c", ThreadID: "t-1", State: "fired"}) != "mine" ||
		lane(Entry{Kind: "owner", Day: "2026-09-27", Ref: "cal:c", Soon: true}) != "mine" {
		t.Error("chore lanes")
	}
	// Closed is one rule.
	for _, e := range []Entry{{State: "done"}, {State: "accepted"}, {State: "declined"}, {Did: true}, {Kind: "action", State: "approved"}} {
		if !st(e).Closed {
			t.Errorf("not closed: %+v", e)
		}
	}
	if st(Entry{Kind: "action", State: "proposed"}).Closed || st(Entry{Kind: "owner", State: "fired", Ref: "cal:c"}).Closed {
		t.Error("open rows drawn closed")
	}
	// What moves, and the sentence the rest show.
	item := Entry{Kind: "owner", State: "fired", Ref: "cal:c"}
	if e := st(item); !e.Item || e.Move != "item" || e.Why != "" {
		t.Errorf("fired item: %+v", e)
	}
	if e := st(Entry{Kind: "owner", State: "done", Did: true, Actor: "owner", Ref: "cal:c"}); !e.Item || e.Move != "" || !strings.Contains(e.Why, "reopen it first") {
		t.Errorf("closed item: %+v", e)
	}
	if e := st(Entry{Kind: "run", Ref: "thread:t", ThreadID: "t", Repeat: "weekly@Mon 09:00"}); e.Move != "run" {
		t.Errorf("cadence run: %+v", e)
	}
	if e := st(Entry{Kind: "run", Ref: "thread:t", ThreadID: "t", Repeat: "every@6h"}); e.Move != "" || e.Why == "" {
		t.Errorf("every@ run: %+v", e)
	}
	if e := st(Entry{Kind: "job", Ref: "job:x", Repeat: "daily@09:00"}); e.Move != "" || !strings.Contains(e.Why, "ops/schedule.json") || e.Item {
		t.Errorf("job: %+v", e)
	}
	// One word per kind, on both surfaces.
	for want, e := range map[string]Entry{
		"your step": {Kind: "owner"}, "homework": {Kind: "homework"}, "one-off run": {Kind: "agent"},
		"reminder": {Kind: "note"}, "recurring run": {Kind: "run", Ref: "thread:t"}, "app update": {Kind: "ask", AskKind: "install"},
		"check back": {Kind: "rec", State: "deferred"}, "rec": {Kind: "rec", At: "09:00", Verb: "Filed"}, "action": {Kind: "action"},
	} {
		if got := st(e).KindLabel; got != want {
			t.Errorf("%+v: kind label %q, want %q", e, got, want)
		}
	}
}
