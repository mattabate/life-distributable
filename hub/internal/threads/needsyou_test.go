package threads

import (
	"strings"
	"testing"

	"life/hub/internal/notify"
)

// A thread whose only ask was superseded is not the owner's turn any more.
// The install rule closes older cards on OTHER threads (one .ipa, one card),
// and before this those threads kept status=needs_you forever: a "YOUR TURN"
// in the console pointing at a card that says "superseded".
func TestSupersedeClearsNeedsYouOnEveryThread(t *testing.T) {
	m, _, _ := setup(t)
	t1, _ := m.Create("", "life", "", "Money page.", "", "", nil)
	t2, _ := m.Create("", "life", "", "Recs list.", "", "", nil)
	// Create starts a turn; both sessions have finished theirs by the time
	// their cards are on the board.
	m.db.Exec(`UPDATE threads SET status='idle' WHERE id IN (?,?)`, t1.ID, t2.ID)
	if _, err := m.AddAsk(t1.ID, "", "Install app build 310 (tap the link)", "itms://x", "physical", ""); err != nil {
		t.Fatal(err)
	}
	th1, _ := m.Get(t1.ID)
	if th1.Status != "needs_you" {
		t.Fatalf("raising an ask must put the thread on the board: %+v", th1)
	}
	// A newer build's card on another thread closes the older one…
	if _, err := m.AddAsk(t2.ID, "", "Install app build 311 (tap the link)", "itms://x", "physical", ""); err != nil {
		t.Fatal(err)
	}
	// …and t1 must leave the board with it.
	th1, _ = m.Get(t1.ID)
	if th1.Status == "needs_you" {
		t.Errorf("t1 still needs_you after its only ask was superseded: %+v", th1)
	}
	th2, _ := m.Get(t2.ID)
	if th2.Status != "needs_you" {
		t.Errorf("t2 owns the live card and must be on the board: %+v", th2)
	}
	if as, _ := m.ListAsks("active", t1.ID, 10); len(as) != 0 {
		t.Errorf("t1 has no active ask left, got %+v", as)
	}
}

// reconcileNeedsYou repairs rows that went stale before the fix above: the
// flag is written straight into the table here, the way the old code left it.
func TestReconcileClearsStaleNeedsYou(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Stranded.", "", "", nil)
	m.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, th.ID)
	a, _ := m.AddAsk(th.ID, "", "Install app build 300 (tap the link)", "", "physical", "")
	m.db.Exec(`UPDATE items SET state='superseded', superseded_by='ask-ffff' WHERE id=?`, a.ID)
	m.db.Exec(`UPDATE threads SET status='needs_you' WHERE id=?`, th.ID)

	m.reconcileNeedsYou()

	th, _ = m.Get(th.ID)
	if th.Status == "needs_you" {
		t.Errorf("stale needs_you survived the reconcile: %+v", th)
	}
	// A thread with a live ask is untouched by the same pass.
	other, _ := m.Create("", "life", "", "Real.", "", "", nil)
	m.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, other.ID)
	m.AddAsk(other.ID, "", "Paste the analytics key", "", "access", "")
	m.reconcileNeedsYou()
	other, _ = m.Get(other.ID)
	if other.Status != "needs_you" {
		t.Errorf("reconcile cleared a thread that really does need the owner: %+v", other)
	}
}

type cardNfy struct {
	nfy
	said []string
}

func (n *cardNfy) Card(kind, line, say, thread, card string) error {
	n.said = append(n.said, say)
	return nil
}

// What was read out is kept ON the card, so the owner can reread it instead
// of digging through a session's transcript. Both the sentence a session
// wrote and the one the hub falls back to are stored, word for word as
// spoken — and a card that never pushed keeps nothing, so `said` always
// means "this was said out loud".
func TestSpokenLineIsStoredOnTheCard(t *testing.T) {
	m, _, _ := setup(t)
	n := &cardNfy{}
	m.Notifier = n
	th, _ := m.Create("Newsletter launch", "life", "", "Working.", "", "", nil)

	say := "Hey Alex, I cut the launch post down to four versions. Drafts are on the card."
	a, err := m.AddAskSaid(th.ID, "run1", "Four tighter versions of the post", "", "read", "", "", say)
	if err != nil {
		t.Fatal(err)
	}
	if a.Said != say {
		t.Errorf("said not stored: %q, want %q", a.Said, say)
	}
	if len(n.said) != 1 || n.said[0] != say {
		t.Fatalf("spoken %v, want the same sentence", n.said)
	}
	// Re-read from the database, not just the value addAsk returned.
	if got, _ := m.GetAsk(a.ID); got.Said != say {
		t.Errorf("said did not survive the round trip: %q", got.Said)
	}
	// No --say: the hub's own sentence is what was heard, so that is what is kept.
	b, _ := m.AddAsk(th.ID, "run1", "Paste the YouTube key", "", "access", "")
	want := notify.Spoken("access", ClassUnblock, "Paste the YouTube key", "Newsletter launch")
	if !strings.Contains(want, "Paste the YouTube key") {
		t.Fatalf("fallback does not carry the card's title: %q", want)
	}
	if b.Said != want {
		t.Errorf("fallback not stored: %q, want %q", b.Said, want)
	}
	if len(n.said) != 2 || n.said[1] != want {
		t.Errorf("spoken %v, want %q", n.said, want)
	}

	// A manager with no notifier says nothing, so it records nothing.
	m2, _, _ := setup(t)
	m2.Notifier = nil
	th2, _ := m2.Create("", "life", "", "Quiet.", "", "", nil)
	c, _ := m2.AddAskSaid(th2.ID, "", "Nothing was spoken", "", "read", "", "", say)
	if c.Said != "" {
		t.Errorf("said recorded with nothing to speak it: %q", c.Said)
	}
}

type readNfy struct {
	nfy
	reads []string
}

func (n *readNfy) ToRead(s string) error { n.reads = append(n.reads, s); return nil }

// A read card is spoken too, on its own quieter lane — never as NEEDS YOU:
// a note from a session is still read out.
func TestReadCardPushesAsToRead(t *testing.T) {
	m, _, _ := setup(t)
	n := &readNfy{}
	m.Notifier = n
	th, _ := m.Create("", "life", "", "Working.", "", "", nil)
	m.AddAsk(th.ID, "run1", "The backup job is 33% faster", "", "read", "")
	m.AddAsk(th.ID, "run1", "Paste the YouTube key", "", "access", "")
	if len(n.reads) != 1 || !strings.HasPrefix(n.reads[0], "The backup job is 33% faster\n") {
		t.Fatalf("read card: %v", n.reads)
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "Paste the YouTube key") {
		t.Fatalf("needs-you lane: %v", n.msgs)
	}
}

// A card raised mid-run reaches the owner NOW — the push goes out at the
// moment the agent hands it over, not when the turn wraps up. Their answer
// steers the turn still in flight, so there is nothing to wait for. Two things stay turn-end business:
// the session keeps status=running (settleStatus turns it into needs_you when
// the turn ends with the card open), and a `read` card never buzzes.
func TestCardsSurfaceMidRun(t *testing.T) {
	m, n, _ := setup(t)
	th, _ := m.Create("", "life", "", "Working.", "", "", nil)
	m.db.Exec(`UPDATE threads SET status='running', unread=0 WHERE id=?`, th.ID)
	access, _ := m.AddAsk(th.ID, "run1", "Paste the YouTube key", "", "access", "")
	m.AddAsk(th.ID, "run1", "The backup job is 33% faster", "", "read", "")

	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "Paste the YouTube key") {
		t.Fatalf("the access card must push the moment it is raised, the read card never: %v", n.msgs)
	}
	th, _ = m.Get(th.ID)
	if th.Status != "running" {
		t.Errorf("a card does not stop the session that raised it: %+v", th)
	}
	if th.Unread != 2 {
		t.Errorf("both cards are unread the moment they exist: %+v", th)
	}
	// It is answerable right now: the answer is a steering message, and the
	// board carries it like any other open card.
	if a, _ := m.GetAsk(access.ID); a.State != "open" || !a.ThreadRunning {
		t.Errorf("mid-run card: %+v", a)
	}
	if as, _ := m.ListAsks("active", th.ID, 10); len(as) != 2 {
		t.Errorf("both cards are on the board while the session works, got %+v", as)
	}
}

// Every card carries the ways it can be answered, per kind, decisive first
// and "Reply" last — the hub owns the words AND the order, so the card
// row and the composer strip on either surface cannot differ. A read is
// acknowledged, not "done"; an error has Restart, not Respond.
func TestAskOutcomesFollowTheKind(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Working.", "", "", nil)
	read, _ := m.AddAsk(th.ID, "run1", "The backup job is 33% faster", "", "read", "")
	access, _ := m.AddAsk(th.ID, "run1", "Paste the YouTube key", "", "access", "")
	read, _ = m.GetAsk(read.ID)
	access, _ = m.GetAsk(access.ID)
	// One outcome and no Dismiss: a read card is read or replied to. Reply is
	// the composer, not a second outcome: with or without words the card closes.
	if len(read.Outcomes) != 1 || read.Outcomes[0] != (Outcome{Value: "done", Label: "Read it"}) {
		t.Errorf("a read card is read, and that is the only outcome: %+v", read.Outcomes)
	}
	if len(access.Outcomes) != 3 || access.Outcomes[0] != (Outcome{Value: "done", Label: "Granted"}) || access.Outcomes[1].Value != "wont" || access.Outcomes[2] != (Outcome{Value: "", Label: "Reply"}) {
		t.Errorf("an access card is granted or refused: %+v", access.Outcomes)
	}
	if got := OutcomesFor("error"); got == nil || len(got) != 0 {
		t.Errorf("error cards have Restart, not answers: %v", got)
	}
	if got := OutcomesFor("bogus"); len(got) != 3 || got[0].Label != "I did this" {
		t.Errorf("an unknown kind falls back to other's: %v", got)
	}
}

// A card filed under the wrong kind wears the wrong verbs ("Granted" on a
// do-these-8-DNS-steps card filed as access), so the kind
// is retaggable after the fact and the chips follow it. Error cards are the
// hub's own — Restart hangs off them — so they keep their kind, both ways.
func TestSetAskKind(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Working.", "", "", nil)
	a, _ := m.AddAsk(th.ID, "run1", "Move example.com to Cloudflare — 8 steps", "", "access", "")
	a, err := m.SetAskKind(a.ID, "physical")
	if err != nil || a.Kind != "physical" || a.Outcomes[0] != (Outcome{Value: "done", Label: "I did this"}) {
		t.Fatalf("retag: %+v %v", a, err)
	}
	if _, err := m.SetAskKind(a.ID, "urgent"); err == nil {
		t.Error("an unknown kind must be refused, not stored")
	}
	if _, err := m.SetAskKind(a.ID, "error"); err == nil {
		t.Error("nothing retags to error")
	}
	e, _ := m.AddAsk(th.ID, "run1", "Session stopped: API 529", "", "error", "")
	if _, err := m.SetAskKind(e.ID, "read"); err == nil {
		t.Error("an error card keeps its kind")
	}
}

// A practice or a step is the calendar's row, never a session's turn — and
// that holds for EVERY writer of the status, not just syncThreadStatus. The
// end-of-turn settle counted any open card, so the `calendar` thread wore a
// red YOUR TURN on both surfaces with nothing under Your turn — a turn marker
// with nothing behind it reads as something badly wrong under the hood.
func TestOwnersWorkNeverMakesItTheirTurn(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Calendar.", "", "", nil)
	m.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, th.ID)
	m.db.Exec(`UPDATE thread_runs SET busy=0 WHERE thread_id=?`, th.ID) // its opening turn has ended
	m.AddAskAs(th.ID, "", "Perfect pitch: one round (20 notes)", "", "physical", "", "", ClassPractice)
	if th, _ = m.Get(th.ID); th.Status == "needs_you" {
		t.Fatalf("raising a practice put the session on Your turn: %+v", th)
	}
	m.mu.Lock()
	m.settleStatus(th.ID)
	m.mu.Unlock()
	if th, _ = m.Get(th.ID); th.Status == "needs_you" {
		t.Errorf("a turn ending with only a practice open settled to needs_you: %+v", th)
	}
	// The row the old settle left behind is repaired at boot.
	m.db.Exec(`UPDATE threads SET status='needs_you' WHERE id=?`, th.ID)
	m.reconcileNeedsYou()
	if th, _ = m.Get(th.ID); th.Status == "needs_you" {
		t.Errorf("reconcile kept a needs_you whose only card is a practice: %+v", th)
	}
	// A blocking card still does it, through the same settle.
	m.AddAsk(th.ID, "", "Paste the key", "", "access", "")
	m.mu.Lock()
	m.settleStatus(th.ID)
	m.mu.Unlock()
	if th, _ = m.Get(th.ID); th.Status != "needs_you" {
		t.Errorf("a blocking card must settle to needs_you: %+v", th)
	}
}
