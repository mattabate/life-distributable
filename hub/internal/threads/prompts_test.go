package threads

import (
	"os"
	"strings"
	"testing"
	"time"
)

// selfPrompt: an agent coming back to its own work later — author is the
// session, target the same session (what `lifectl prompt` sends).
func selfPrompt(m *Manager, threadID, text string, at time.Time) (Prompt, error) {
	return m.Queue(Prompt{Author: "claude:thread:" + threadID, Target: threadID, Text: text, NotBefore: at})
}

// A prompt with a future time waits in the queue until the tick reaches it,
// and lands as a message on the thread it named (an agent checking back on
// itself in 30 minutes).
func TestSelfPromptWaitsForItsTime(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "Ship the build.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "Kicked off the build.")
	before, _ := m.Messages(th.ID, 50)

	p, err := selfPrompt(m, th.ID, "Check whether the build finished.", time.Now().Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "queued" {
		t.Fatalf("%+v", p)
	}
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != len(before) {
		t.Fatal("a future prompt must not wake the session yet")
	}
	// not yet due
	m.DuePrompts(time.Now())
	if p, _ = m.GetPrompt(p.ID); p.State != "queued" {
		t.Fatalf("%+v", p)
	}
	// the tick passes its time
	m.DuePrompts(time.Now().Add(31 * time.Minute))
	if p, _ = m.GetPrompt(p.ID); p.State != "delivered" || p.DeliveredThread != th.ID {
		t.Fatalf("%+v", p)
	}
	msgs, _ := m.Messages(th.ID, 50)
	last := msgs[len(msgs)-1]
	if last.Kind != "checkin" || last.Text != "Check whether the build finished." {
		t.Fatalf("%+v", last)
	}
	th, _ = m.Get(th.ID)
	if th.Status != "running" {
		t.Fatalf("%+v", th)
	}
	// a cancelled prompt never fires
	q, _ := selfPrompt(m, th.ID, "and again", time.Now().Add(time.Hour))
	if _, err := m.CancelPrompt(q.ID); err != nil {
		t.Fatal(err)
	}
	m.DuePrompts(time.Now().Add(2 * time.Hour))
	if q, _ = m.GetPrompt(q.ID); q.State != "cancelled" {
		t.Fatalf("%+v", q)
	}
}

// Agents do not talk to each other, but they may check back in on themselves.
func TestAgentMayOnlyPromptItselfOrANewSession(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("", "life", "", "Thread A.", "", "", nil)
	b, _ := m.Create("", "life", "", "Thread B.", "", "", nil)
	author := "claude:thread:" + a.ID

	if _, err := m.Queue(Prompt{Author: author, Target: b.ID, Text: "do my errand"}); err == nil {
		t.Fatal("one session prompted another")
	}
	if _, err := m.Queue(Prompt{Author: author, Target: "new-or:" + b.ID, Text: "do my errand"}); err == nil {
		t.Fatal("new-or is not a back door into another session")
	}
	if _, err := m.Queue(Prompt{Author: author, Target: a.ID, Text: "check on myself", NotBefore: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// spawning a fresh session is allowed: nobody's context is invaded
	p, err := m.Queue(Prompt{Author: author, Target: "new", Title: "Nightly price sync", Text: "Sync prices."})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "delivered" || p.DeliveredThread == "" || p.DeliveredThread == a.ID {
		t.Fatalf("%+v", p)
	}
	spawned, _ := m.Get(p.DeliveredThread)
	if spawned.Title != "Nightly price sync" {
		t.Fatalf("%+v", spawned)
	}
	// the owner is under no such rule
	if _, err := m.Queue(Prompt{Author: "owner", Target: b.ID, Text: "hey"}); err != nil {
		t.Fatal(err)
	}
}

// A blue opening brief may come from the owner, the hub or another session,
// and the row must say which. Every owner/system row names its author.
func TestEveryMessageSaysWhoSentIt(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("Parent work", "life", "", "Look into the steps.", "", "", nil)
	complete(t, m, a.ID, "Found it.")
	p, err := m.Queue(Prompt{Author: "claude:thread:" + a.ID, Target: "new", Title: "Fix steps", Text: "Fix the steps."})
	if err != nil {
		t.Fatal(err)
	}
	self, _ := selfPrompt(m, a.ID, "Check the fix landed.", time.Now())
	m.DuePrompts(time.Now().Add(time.Minute))
	if self, _ = m.GetPrompt(self.ID); self.State != "delivered" {
		t.Fatalf("%+v", self)
	}
	if _, err := m.Queue(Prompt{Author: "hub", Target: "new", Title: "Calendar run", Text: "Run the check."}); err != nil {
		t.Fatal(err)
	}
	who := func(id string) (out []string) {
		msgs, _ := m.Messages(id, 50)
		for _, x := range msgs {
			out = append(out, x.Role+"="+x.Author+"|"+x.AuthorTitle)
		}
		return out
	}
	if got := strings.Join(who(a.ID), " "); got != "owner=owner| claude=| system=claude:thread:"+a.ID+"|Parent work" {
		t.Fatal(got)
	}
	if got := who(p.DeliveredThread); got[0] != "owner=claude:thread:"+a.ID+"|Parent work" {
		t.Fatal(got)
	}
	threads, _ := m.List(true)
	found := false
	for _, th := range threads {
		if th.Title == "Calendar run" {
			found = true
			if got := who(th.ID); got[0] != "owner=hub|" {
				t.Fatal(got)
			}
		}
	}
	if !found {
		t.Fatal("no hub-started session")
	}
}

// A reference is data, and the hub refuses one it could not render.
func TestPromptReferenceIsValidated(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Thread.", "", "", nil)
	bad := []Prompt{
		{Author: "owner", Target: th.ID, Text: "x", InReplyTo: "ask-1a2b"},                      // no type
		{Author: "owner", Target: th.ID, Text: "x", InReplyTo: "invoice:9"},                     // unknown type
		{Author: "owner", Target: th.ID, Text: "x", Outcome: "done"},                            // outcome about nothing
		{Author: "owner", Target: th.ID, Text: "x", InReplyTo: "action:act-1", Outcome: "done"}, // done is about an ask
		{Author: "owner", Target: th.ID, Text: "x", InReplyTo: "ask:a", Outcome: "shipped"},     // not a word we know
		{Author: "owner", Target: th.ID},                                                        // nothing to say
		{Author: "owner", Text: "x"},                                                            // no target
	}
	for i, p := range bad {
		if _, err := m.Queue(p); err == nil {
			t.Fatalf("accepted bad prompt %d: %+v", i, p)
		}
	}
}

// Dismissing an ask used to be recorded and nothing else: the session waited
// forever on an answer that was never coming. Now "won't do" is a prompt.
func TestDismissReachesTheAgent(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Set up the sync.", "", "", nil)
	complete(t, m, th.ID, "Waiting on the token.")
	a, _ := m.AddAsk(th.ID, "", "Send me the setup token.", "", "access", "")
	if _, err := m.ResolveAsk(a.ID, "dismissed", "owner", "not doing this one"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := m.Messages(th.ID, 50)
	last := msgs[len(msgs)-1]
	if last.Role != "owner" || last.Kind != "decision" || !strings.Contains(last.Text, "Won't do: Send me") || !strings.Contains(last.Text, "not doing this one") {
		t.Fatalf("%+v", last)
	}
	pb, _ := os.ReadFile(promptFile(t, m, th.ID))
	if !strings.Contains(string(pb), "WON'T DO") || !strings.Contains(string(pb), a.ID) {
		t.Fatal(string(pb))
	}
}

// One Respond, three things on it: an outcome (which closes the card the
// moment it is said), words, and a time (which does not delay the close).
func TestRespondClosesTheCardAndCanBeSentLater(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Plan the tranches.", "", "", nil)
	complete(t, m, th.ID, "Two questions for you.")
	a, _ := m.AddAsk(th.ID, "", "Pick a risk tolerance", "", "decision", "")
	b, _ := m.AddAsk(th.ID, "", "Confirm the date", "", "decision", "")

	// words only: the ask is answered (ball back with the agent) but stays open
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, InReplyTo: "ask:" + a.ID, Text: "moderate"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(a.ID); got.State != "answered" {
		t.Fatalf("%+v", got)
	}
	if got, _ := m.GetAsk(b.ID); got.State != "open" {
		t.Fatalf("a response to one card touched another: %+v", got)
	}
	// "done", aimed at tomorrow morning: the card leaves the board NOW, the
	// words wait for the tick
	p, err := m.Queue(Prompt{Author: "owner", Target: th.ID, InReplyTo: "ask:" + b.ID, Outcome: "done",
		Text: "the 4th", NotBefore: time.Now().Add(8 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := m.GetAsk(b.ID)
	if got.State != "done" || got.ResolvedBy != "owner" || got.Resolution != "the 4th" {
		t.Fatalf("%+v", got)
	}
	if p.State != "queued" {
		t.Fatalf("%+v", p)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if strings.Contains(msgs[len(msgs)-1].Text, "the 4th") {
		t.Fatal("a prompt for tomorrow must not wake the session tonight")
	}
	m.DuePrompts(time.Now().Add(9 * time.Hour))
	msgs, _ = m.Messages(th.ID, 50)
	// …and the row says what it answered and how, so the chat can show the
	// pick under the owner's words instead of the words alone.
	if last := msgs[len(msgs)-1]; last.Text != "the 4th" || last.Kind != "decision" || last.InReplyTo != "ask:"+b.ID || last.Outcome != "done" {
		t.Fatalf("%+v", last)
	}
	// "won't do" is the same row with a different word
	c, _ := m.AddAsk(th.ID, "", "Send me the token", "", "access", "")
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, InReplyTo: "ask:" + c.ID, Outcome: "wont"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(c.ID); got.State != "dismissed" {
		t.Fatalf("%+v", got)
	}
}

// A read card has two answers and both close it: "Read it" (silent — nothing
// wakes) and a reply (the words reach the session). And a message to the
// SESSION leaves it alone: only a reply starts the session up, and only a
// reply to the card itself closes it.
func TestReadCardClosesOnReplyAndNeverWakesTheSession(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Look at the attachments.", "", "", nil)
	complete(t, m, th.ID, "Here is what I found.")
	read, _ := m.AddAsk(th.ID, "", "Attachments went out at 3x", "", "read", "")
	decide, _ := m.AddAsk(th.ID, "", "Pick a risk tolerance", "", "decision", "")
	before, _ := m.Messages(th.ID, 50)

	// A message to the session is not an answer to the read card.
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "unrelated: did the build finish?"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(read.ID); got.State != "open" {
		t.Fatalf("a reply to the session must not touch a read card: %+v", got)
	}
	if got, _ := m.GetAsk(decide.ID); got.State != "answered" {
		t.Fatalf("the owner's words still answer the cards that asked them something: %+v", got)
	}

	// The one-tap close: the card is done, the session hears nothing.
	n := len(before) + 1 // the unrelated message above
	if _, err := m.ResolveAsk(read.ID, "done", "owner", "Read it"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(read.ID); got.State != "done" || got.Resolution != "Read it" {
		t.Fatalf("%+v", got)
	}
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != n {
		t.Fatalf("reading a card must not wake the session: %+v", msgs[n:])
	}

	// Answering the sheet with the outcome and no words: same, through prompts.
	r2, _ := m.AddAsk(th.ID, "", "The real spin-up floor is 4-18s", "", "read", "")
	p, err := m.Queue(Prompt{Author: "owner", Target: th.ID, InReplyTo: "ask:" + r2.ID, Outcome: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(r2.ID); got.State != "done" {
		t.Fatalf("%+v", got)
	}
	if p.State != "cancelled" || p.Error == "" {
		t.Fatalf("a wordless read answer is recorded, not delivered: %+v", p)
	}
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != n {
		t.Fatalf("a wordless read answer must not wake the session: %+v", msgs[n:])
	}

	// Words typed INTO the card: it closes, and they are delivered.
	r3, _ := m.AddAsk(th.ID, "", "The cache hit rate is 33%", "", "read", "")
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, InReplyTo: "ask:" + r3.ID, Text: "nice — keep watching it"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetAsk(r3.ID); got.State != "done" {
		t.Fatalf("replying to a read card closes it: %+v", got)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if last := msgs[len(msgs)-1]; last.Text != "nice — keep watching it" || last.InReplyTo != "ask:"+r3.ID {
		t.Fatalf("%+v", last)
	}
}

// The framing is rendered from the referenced row at wake time, never parsed
// out of the owner's words.
func TestRefHeaderNamesTheReferencedRow(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Plan the tranches.", "", "", nil)
	a, _ := m.AddAsk(th.ID, "", "Pick a risk tolerance", "", "decision", "")

	h := m.refHeader("ask:"+a.ID, "done", "decision")
	if !strings.Contains(h, a.ID) || !strings.Contains(h, "Pick a risk tolerance") || !strings.Contains(h, "DONE") {
		t.Fatal(h)
	}
	if !strings.Contains(h, "says nothing about any other ask") {
		t.Fatal("the header must not speak for the thread's other asks: " + h)
	}
	// a reference the row no longer backs degrades to the owner's words, not a guess
	if g := m.refHeader("ask:ask-gone", "done", "decision"); !strings.Contains(g, "no longer on file") {
		t.Fatal(g)
	}
	if m.refHeader("", "", "decision") != "" || m.refHeader("nonsense", "", "decision") != "" {
		t.Fatal("no reference, no header")
	}
	// an action header uses the title the hub looks up, not anything typed
	m.ActionTitle = func(id string) string { return "Sell 3 shares" }
	if h := m.refHeader("action:act-1", "denied", "decision"); !strings.Contains(h, `act-1 "Sell 3 shares"`) || !strings.Contains(h, "DENIED") || !strings.Contains(h, "Do not do it") {
		t.Fatal(h)
	}
	// A rec answer: its three verdicts are outcomes about a rec and nothing
	// else; the frame is the rec package's when wired, a one-liner otherwise.
	th2, _ := m.Create("", "life", "", "Thread.", "", "", nil)
	if _, err := m.Queue(Prompt{Author: "owner", Target: th2.ID, InReplyTo: "ask:a", Outcome: "accepted"}); err == nil {
		t.Fatal("accepted is about a rec, not an ask")
	}
	if _, err := m.Queue(Prompt{Author: "owner", Target: th2.ID, InReplyTo: "rec:rec-1", Outcome: "deferred"}); err != nil {
		t.Fatal(err)
	}
	if h := m.refHeader("rec:rec-1", "declined", "decision"); !strings.Contains(h, "DECLINED recommendation rec-1") {
		t.Fatal(h)
	}
	m.RecHeader = func(id, outcome string) string { return "[from the ledger: " + id + " " + outcome + "]\n" }
	if h := m.refHeader("rec:rec-1", "accepted", "decision"); h != "[from the ledger: rec-1 accepted]\n" {
		t.Fatal(h)
	}
}

// A `cal:` reference means two opposite things and the MESSAGE's kind is what
// tells them apart: the hub saying a step is due, or the owner answering about
// that step. If the ref's kind shadows the message's, the decision branch is
// unreachable and closing a step reaches the session as "coming due. Do it
// now, end to end".
func TestAnswerToAStepIsNeverRenderedAsDue(t *testing.T) {
	m, _, _ := setup(t)

	due := m.refHeader("cal:cal-1", "", "checkin")
	if !strings.Contains(due, "coming due") {
		t.Fatal("the hub's own wake still says the item is due: " + due)
	}
	for _, outcome := range []string{"", "done", "wont"} {
		h := m.refHeader("cal:cal-1", outcome, "decision")
		if strings.Contains(h, "coming due") || strings.Contains(h, "Do it now") {
			t.Fatalf("outcome %q: the owner's answer was rendered as a due wake: %s", outcome, h)
		}
		if !strings.Contains(h, "their answer about that one step, not a new task") {
			t.Fatalf("outcome %q: %s", outcome, h)
		}
	}
	if h := m.refHeader("cal:cal-1", "wont", "decision"); !strings.Contains(h, "they are not doing this") {
		t.Fatal("won't-do must say the work is off: " + h)
	}
	// Wired up, the frame is the calendar package's, read from the item's row.
	m.CalHeader = func(id, outcome string) string { return "[step " + id + " " + outcome + "]\n" }
	if h := m.refHeader("cal:cal-1", "done", "decision"); h != "[step cal-1 done]\n" {
		t.Fatal("CalHeader is wired but was not used: " + h)
	}
	if h := m.refHeader("cal:cal-1", "", "checkin"); !strings.Contains(h, "coming due") {
		t.Fatal("CalHeader must not swallow the hub's due wake: " + h)
	}
}

// A prompt aimed at a session that is gone: "new-or:" starts a fresh one and
// spells the reference out (the new session was not there for it); a plain
// target fails loudly instead of inventing a session.
func TestPromptForAGoneSession(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Old work.", "", "", nil)
	complete(t, m, th.ID, "Done.")
	a, _ := m.AddAsk(th.ID, "", "Pick a rebalance date", "", "decision", "")
	// Archived with the card still open — Archive refuses that, so the row
	// is written directly.
	if _, err := m.db.Exec(`UPDATE threads SET status='archived' WHERE id=?`, th.ID); err != nil {
		t.Fatal(err)
	}
	p, err := m.Queue(Prompt{Author: "owner", Target: "new-or:" + th.ID, Text: "the 4th", InReplyTo: "ask:" + a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p.DeliveredThread == "" || p.DeliveredThread == th.ID {
		t.Fatalf("%+v", p)
	}
	msgs, _ := m.Messages(p.DeliveredThread, 10)
	if len(msgs) == 0 || !strings.Contains(msgs[0].Text, a.ID) || !strings.Contains(msgs[0].Text, "the 4th") {
		t.Fatalf("%+v", msgs)
	}
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "the 4th"}); err == nil {
		t.Fatal("a prompt into an archived session should fail")
	}
	ps, _ := m.ListPrompts("failed", "", 10)
	if len(ps) != 1 {
		t.Fatalf("%+v", ps)
	}
}

// One message answers SEVERAL cards, rather than one prompt (and one wake)
// per card. Each
// card is closed by its own rule, the message row carries every reference,
// and the session's frame names each one.
func TestOneMessageAnswersSeveralCards(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Work.", "", "", nil)
	complete(t, m, th.ID, "Three things for you.")
	read, _ := m.AddAsk(th.ID, "", "All three yes", "", "read", "")
	decide, _ := m.AddAsk(th.ID, "", "Pick a rebalance date", "", "decision", "")
	var decided []string
	m.DecideRec = func(id, outcome, note string) error {
		decided = append(decided, id+" "+outcome+" "+note)
		return nil
	}
	m.RecHeader = func(id, outcome string) string { return "[rec " + id + " " + outcome + "]\n" }
	before, _ := m.Messages(th.ID, 50)

	p, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "Thursday, and go ahead.",
		Replies: []Reply{{Ref: "ask:" + read.ID}, {Ref: "ask:" + decide.ID, Outcome: "done"}, {Ref: "rec:rec-1", Outcome: "accepted"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "delivered" || p.InReplyTo != "ask:"+read.ID || p.Outcome != "" || len(p.Replies) != 3 {
		t.Fatalf("first reply on the old columns, all of them on replies: %+v", p)
	}
	if got, _ := m.GetAsk(read.ID); got.State != "done" {
		t.Fatalf("the read card closes: %+v", got)
	}
	if got, _ := m.GetAsk(decide.ID); got.State != "done" {
		t.Fatalf("done claims the decision: %+v", got)
	}
	if len(decided) != 1 || decided[0] != "rec-1 accepted Thursday, and go ahead." {
		t.Fatalf("the rec is decided once, with the owner's words: %v", decided)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if len(msgs) != len(before)+1 {
		t.Fatalf("one message: %+v", msgs[len(before):])
	}
	last := msgs[len(msgs)-1]
	if last.InReplyTo != "ask:"+read.ID || len(last.Replies) != 3 || last.Replies[2].Ref != "rec:rec-1" || last.Replies[2].Outcome != "accepted" {
		t.Fatalf("%+v", last)
	}
	prompt, _ := os.ReadFile(promptFile(t, m, th.ID))
	s := string(prompt)
	for _, want := range []string{"answered 3 cards with ONE message", read.ID, decide.ID, "[rec rec-1 accepted]", "Thursday, and go ahead."} {
		if !strings.Contains(s, want) {
			t.Fatalf("frame lacks %q:\n%s", want, s)
		}
	}

	// The same ref twice collapses to one; an outcome with no ref is refused;
	// a rec verdict on an ask is still refused.
	q, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "again", InReplyTo: "rec:rec-1", Outcome: "accepted",
		Replies: []Reply{{Ref: "rec:rec-1", Outcome: "accepted"}}})
	if err != nil || len(q.Replies) != 1 {
		t.Fatalf("%v %+v", err, q)
	}
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "x", Replies: []Reply{{Outcome: "done"}}}); err == nil {
		t.Fatal("an outcome without a reference")
	}
	if _, err := m.Queue(Prompt{Author: "owner", Target: th.ID, Text: "x", Replies: []Reply{{Ref: "ask:" + read.ID, Outcome: "accepted"}}}); err == nil {
		t.Fatal("accepted is about a rec, not an ask")
	}

	// Wordless: every card a read → recorded, never delivered. A read plus a
	// rec verdict with no words → the session still hears the verdict.
	r2, _ := m.AddAsk(th.ID, "", "Note one", "", "read", "")
	r3, _ := m.AddAsk(th.ID, "", "Note two", "", "read", "")
	n := len(msgs) + 1 // the "again" message above
	p, err = m.Queue(Prompt{Author: "owner", Target: th.ID, Replies: []Reply{{Ref: "ask:" + r2.ID, Outcome: "done"}, {Ref: "ask:" + r3.ID, Outcome: "done"}}})
	if err != nil || p.State != "cancelled" {
		t.Fatalf("%v %+v", err, p)
	}
	if got, _ := m.GetAsk(r3.ID); got.State != "done" {
		t.Fatalf("%+v", got)
	}
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != n {
		t.Fatalf("two wordless reads must not wake the session: %+v", msgs[n:])
	}
	r4, _ := m.AddAsk(th.ID, "", "Note three", "", "read", "")
	p, err = m.Queue(Prompt{Author: "owner", Target: th.ID, Replies: []Reply{{Ref: "ask:" + r4.ID, Outcome: "done"}, {Ref: "rec:rec-2", Outcome: "declined"}}})
	if err != nil || p.State != "delivered" {
		t.Fatalf("a wordless verdict is work for the session: %v %+v", err, p)
	}
	if got, _ := m.GetAsk(r4.ID); got.State != "done" {
		t.Fatalf("%+v", got)
	}
	// (decided[1] is the "again" send above — the real hook skips a rec
	// already in that state, this stub records every call.)
	if len(decided) != 3 || !strings.HasPrefix(decided[2], "rec-2 declined") {
		t.Fatalf("%v", decided)
	}
}
