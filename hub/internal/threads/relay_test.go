package threads

import (
	"os"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

// Tasks move between sessions only on an approved card. An approved relay
// lands in the other session as the sending agent's words — framed, never as
// the owner's — and the direct road stays shut.
func TestRelayDeliversOnlyWithAnApprovedAction(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("Site renderer", "life", "", "Render the page.", "", "", nil)
	b, _ := m.Create("Site repo", "life", "", "Sort the repo out.", "", "", nil)
	complete(t, m, b.ID, "Sorted.")

	if _, err := m.Queue(Prompt{Author: "claude:thread:" + a.ID, Target: b.ID, Text: "rename it"}); err == nil || !strings.Contains(err.Error(), "lifectl relay") {
		t.Fatalf("the direct road must stay shut and name the sanctioned one: %v", err)
	}
	if _, err := m.Relay(a.ID, b.ID, "rename it", ""); err == nil {
		t.Fatal("a relay with no approved action behind it was delivered")
	}
	res, err := m.Relay(a.ID, b.ID, "Alex wants it called Page Builder.", "act-1")
	if err != nil || !strings.Contains(res, b.ID) {
		t.Fatal(res, err)
	}
	msgs, _ := m.Messages(b.ID, 50)
	last := msgs[len(msgs)-1]
	if last.Role != "system" || last.Kind != "relay" || last.Author != "claude:thread:"+a.ID {
		t.Fatalf("%+v", last)
	}
	pb, _ := os.ReadFile(promptFile(t, m, b.ID))
	for _, want := range []string{"Handed to you from another session", `"Site renderer"`, "action act-1", "not the owner's", "lifectl relay " + a.ID, "Page Builder"} {
		if !strings.Contains(string(pb), want) {
			t.Fatalf("missing %q in:\n%s", want, pb)
		}
	}
	if strings.Contains(string(pb), "[scheduled check-in]") {
		t.Fatal("a relay is not a check-in")
	}
	ps, _ := m.ListPrompts("delivered", b.ID, 10)
	if len(ps) == 0 || ps[0].Author != "claude:thread:"+a.ID {
		t.Fatalf("the hand-off is a prompts row like every other wake: %+v", ps)
	}

	if _, err := m.RelayTarget("nope"); err == nil {
		t.Fatal("a session that does not exist took a relay")
	}
	clearCards(m, b.ID)
	if err := m.Archive(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RelayTarget(b.ID); err == nil {
		t.Fatal("an archived session took a relay")
	}
}

// Every turn starts knowing who else is live — and only them: reading the list
// wakes nobody, and a finished or archived session is not on it.
func TestPeersHeaderNamesLiveSessionsOnly(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("Duck the music", "life", "", "Build it.", "", "", nil)
	if h := m.peersHeader(a.ID); h != "" {
		t.Fatalf("alone, there is nothing to say: %q", h)
	}
	b, _ := m.Create("Site repo", "life", "", "Sort the repo out.", "", "", nil)
	c, _ := m.Create("Old errand", "life", "", "Do it.", "", "", nil)
	complete(t, m, c.ID, "[end]")
	m.Archive(c.ID)

	h := m.peersHeader(a.ID)
	if !strings.Contains(h, b.ID+" (working): Site repo") || strings.Contains(h, a.ID) || strings.Contains(h, c.ID) || !strings.Contains(h, "lifectl relay") {
		t.Fatal(h)
	}
	if err := m.Send(a.ID, "and test it"); err != nil {
		t.Fatal(err)
	}
	complete(t, m, a.ID, "Built.")
	if err := m.Send(a.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if pb, _ := os.ReadFile(promptFile(t, m, a.ID)); !strings.Contains(string(pb), "[Other sessions right now") || !strings.Contains(string(pb), b.ID) {
		t.Fatal(string(pb))
	}
}

// Every agent sees the other sessions and what they are doing, so work can
// pass to the session with the experience or the one live on it. A live
// peer says what it is doing this minute or which card it waits on, a standing
// (scheduled, idle) session is listed with its lane, and a finished one is not.
func TestPeersHeaderSaysWhatEachIsDoing(t *testing.T) {
	m, _, _ := setup(t)
	me, _ := m.Create("Cross-session coordination", "life", "", "Plan it.", "", "", nil)
	busy, _ := m.Create("Daily tweet draft", "life", "grow-my-audience", "Draft it.", "", "", nil)
	m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, summary) VALUES (?,?,?,?,?,?)`, busy.ID, "", "2026-10-02T12:00:00Z", "tool_use", "Bash", "ops/py.sh x-archive.py posts")
	waiting, _ := m.Create("Face and supplements", "life", "", "Sort it.", "", "", nil)
	complete(t, m, waiting.ID, "[end]")
	m.AddAsk(waiting.ID, "", "Hand over the Acme email", "", "physical", "")
	standing, _ := m.Create("Weekly investing check-in", "life", "make-more-money", "Check.", "weekly@Sun 17:00", "", nil)
	complete(t, m, standing.ID, "[end]")
	done, _ := m.Create("Old errand", "life", "", "Do it.", "", "", nil)
	complete(t, m, done.ID, "[end]")

	h := m.peersHeader(me.ID)
	for _, want := range []string{
		busy.ID + " (working, goal grow-my-audience): Daily tweet draft — now: ops/py.sh x-archive.py posts",
		waiting.ID + " (waiting on the owner): Face and supplements — their card: Hand over the Acme email",
		standing.ID + " (standing weekly@Sun 17:00, goal make-more-money): Weekly investing check-in",
		"lifectl threads --q <word>",
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q in:\n%s", want, h)
		}
	}
	if strings.Contains(h, done.ID) || strings.Contains(h, me.ID) {
		t.Fatal(h)
	}
	if i, j := strings.Index(h, busy.ID), strings.Index(h, standing.ID); i > j {
		t.Fatal("live sessions come before standing ones:\n" + h)
	}
	// `lifectl threads --q` finds the one with the experience, listed or not.
	if ts, _ := m.Find("investing weekly", false); len(ts) != 1 || ts[0].ID != standing.ID {
		t.Fatalf("%+v", ts)
	}
	if ts, _ := m.Find("errand", false); len(ts) != 1 || ts[0].ID != done.ID {
		t.Fatalf("%+v", ts)
	}
	// A card a session raised counts as its experience too.
	if ts, _ := m.Find("acme", false); len(ts) != 1 || ts[0].ID != waiting.ID {
		t.Fatalf("%+v", ts)
	}
}

// Every turn also sees what is open for the owner EVERYWHERE — other sessions'
// cards, proposals, their due steps — so the session their message reached can
// close or reword the one it settles. This thread's own asks are asksHeader's;
// a step for next month is not yet theirs.
func TestBoardHeaderListsWhatIsOpenForTheOwnerEverywhere(t *testing.T) {
	m, _, _ := setup(t)
	me, _ := m.Create("Cross-session coordination", "life", "", "Plan it.", "", "", nil)
	mine, _ := m.AddAsk(me.ID, "", "Pick the block's shape", "", "decision", "")
	other, _ := m.Create("Face and supplements", "life", "", "Sort it.", "", "", nil)
	theirs, _ := m.AddAsk(other.ID, "", "Hand over the Acme email", "", "physical", "")
	now := ts(time.Now())
	today := time.Now().Format("2006-01-02")
	for _, row := range [][]any{
		{"cal-soon", "cal", "owner", "scheduled", "", "Book the string quartet", "calendar"},
		{"cal-today", "cal", "owner", "scheduled", today, "Go to the dry cleaner", ""},
		{"cal-late", "cal", "homework", "scheduled", "2026-01-01", "Perfect pitch: one round", other.ID},
		{"cal-future", "cal", "owner", "scheduled", "2099-01-01", "Renew the passport", ""},
		{"cal-agent", "cal", "agent", "scheduled", today, "Run the export", other.ID},
		{"20261002-010101", "action", "contact", "proposed", "", "Email Noah the brief", other.ID},
	} {
		if _, err := m.db.Exec(`INSERT INTO items (id, src, kind, state, day, title, thread_id, created_at, updated_at, source) VALUES (?,?,?,?,?,?,?,?,?,'owner')`, row[0], row[1], row[2], row[3], row[4], row[5], row[6], now, now); err != nil {
			t.Fatal(err)
		}
		store.StampItem(m.db, row[0].(string))
	}

	h := m.boardHeader(me.ID)
	for _, want := range []string{
		theirs.ID + " (physical · Face and supplements): Hand over the Acme email",
		"cal-soon (step soon): Book the string quartet",
		"cal-today (step on " + today + " · chores): Go to the dry cleaner",
		"cal-late (practice overdue 2026-01-01 · Face and supplements): Perfect pitch: one round",
		"20261002-010101 (approval · Face and supplements): Email Noah the brief",
		"lifectl ask <id> set --title",
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q in:\n%s", want, h)
		}
	}
	for _, no := range []string{mine.ID, "cal-future", "cal-agent"} {
		if strings.Contains(h, no) {
			t.Fatalf("%s does not belong on the block:\n%s", no, h)
		}
	}
	// It rides the prompt of every turn the owner starts, beside the asks block.
	if err := m.Send(other.ID, "forwarded it"); err != nil {
		t.Fatal(err)
	}
	pb, _ := os.ReadFile(promptFile(t, m, other.ID))
	if !strings.Contains(string(pb), "[Open for the owner right now") || !strings.Contains(string(pb), mine.ID) || !strings.Contains(string(pb), "[Open asks on this thread") {
		t.Fatal(string(pb))
	}

	// Rewording: the card keeps its state and id, reads anew, and the trail says so.
	if _, err := m.RewordAsk(theirs.ID, "", "", "", "claude:thread:"+me.ID); err == nil {
		t.Fatal("nothing to change must be refused")
	}
	before, _ := m.GetAsk(theirs.ID) // the message above marked it answered; rewording keeps that
	a, err := m.RewordAsk(theirs.ID, "Forward the Acme email or drop the 9/9 doc", "1. Forward it.", "Hey Alex, forward the Acme email, or drop the doc in life intake.", "claude:thread:"+me.ID)
	if err != nil || a.Title != "Forward the Acme email or drop the 9/9 doc" || a.Detail != "1. Forward it." || !strings.HasPrefix(a.Said, "Hey Alex, forward") || a.State != before.State || a.State == "" {
		t.Fatalf("%+v %v", a, err)
	}
	var note string
	m.db.QueryRow(`SELECT note FROM item_events WHERE item_id=? ORDER BY id DESC LIMIT 1`, theirs.ID).Scan(&note)
	if note != "reworded: title, detail, message" {
		t.Fatal(note)
	}
	if _, err := m.ResolveAsk(theirs.ID, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RewordAsk(theirs.ID, "Too late", "", "", "owner"); err == nil {
		t.Fatal("a closed card keeps its words")
	}
}
