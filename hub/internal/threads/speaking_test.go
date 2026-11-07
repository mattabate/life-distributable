package threads

import (
	"testing"
	"time"
)

// A marked session is heard until its time, then not; a mark in the past
// ends it early (playback finished). Nil-safe throughout.
func TestVoiceMarksAndExpires(t *testing.T) {
	var none *Voice
	none.Mark("x", time.Now().Add(time.Minute))
	if none.Is("x", time.Now()) || none.Live(time.Now()) != nil {
		t.Fatal("a nil voice hears nothing")
	}
	v := &Voice{}
	now := time.Now()
	v.Mark("b", now.Add(20*time.Second))
	v.Mark("a", now.Add(10*time.Second))
	if got := v.Live(now); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("live %v", got)
	}
	if got := v.Live(now.Add(15 * time.Second)); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after a lapses: %v", got)
	}
	v.Mark("b", now) // playback finished
	if v.Is("b", now) || len(v.Live(now)) != 0 {
		t.Fatal("a mark in the past ends the voice")
	}
}

// A card raised before the owner's last message is stale for the voice; one
// raised after it is not (a held line must not speak after their reply).
func TestWroteSince(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do the thing.", "", "", nil)
	raised := time.Now().Add(time.Minute)
	if m.WroteSince(th.ID, raised) {
		t.Fatal("the starter came before the card")
	}
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES (?,?,?,?,?)`,
		th.ID, ts(raised.Add(time.Second)), "claude", "read", "a later card")
	if m.WroteSince(th.ID, raised) {
		t.Fatal("the agent's own words are not the owner's")
	}
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text) VALUES (?,?,?,?,?)`,
		th.ID, ts(raised.Add(2*time.Second)), "owner", "message", "my reply")
	if !m.WroteSince(th.ID, raised) || m.WroteSince(th.ID, raised.Add(3*time.Second)) {
		t.Fatal("the reply after the card makes it stale, and only that card")
	}
}

// A held line drops once the owner answers the silent sibling raised with it
// (two install cards, one spoken line); a sibling with its own line, an
// agent's close, or another run's card leaves it to be said.
func TestSiblingAnswered(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Ship it.", "", "", nil)
	first, _ := m.AddAskSaid(th.ID, "run-1", "Install on the phone", "", "install", "", "", "Hey, install build 3 on the phone and the desktop.")
	second, _ := m.AddAskSaid(th.ID, "run-1", "Install on the desktop", "", "install", "", "", "")
	other, _ := m.AddAskSaid(th.ID, "run-2", "Read this", "", "read", "", "", "")
	if m.SiblingAnswered(first) {
		t.Fatal("nothing answered yet")
	}
	m.ResolveAsk(other.ID, "done", "app", "")
	if m.SiblingAnswered(first) {
		t.Fatal("another run's card is not a sibling")
	}
	m.ResolveAsk(second.ID, "done", "claude:thread:"+th.ID, "")
	if m.SiblingAnswered(first) {
		t.Fatal("the agent's own close is not the owner's answer")
	}
	if _, err := m.ResolveAsk(second.ID, "open", "app", "reopened"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveAsk(second.ID, "done", "app", "clicked Install"); err != nil {
		t.Fatal(err)
	}
	if !m.SiblingAnswered(first) {
		t.Fatal("the owner's Install on the silent sibling makes the shared line stale")
	}
	if m.SiblingAnswered(second) {
		t.Fatal("the first card has its own line and is open: it does not silence the second")
	}
}

// A card's own line reads "speaking" while its session is heard, and not once
// the mark ends or a double tap hushes it.
func TestVoiceCardSpeaking(t *testing.T) {
	var none *Voice
	none.MarkCard("t", "ask-1", time.Now().Add(time.Minute))
	if none.CardSpeaking("ask-1", time.Now()) {
		t.Fatal("a nil voice hears nothing")
	}
	v := &Voice{}
	now := time.Now()
	v.MarkCard("t", "ask-1", now.Add(10*time.Second))
	if !v.CardSpeaking("ask-1", now) || v.CardSpeaking("ask-2", now) || !v.Is("t", now) {
		t.Fatal("the marked card speaks, no other")
	}
	if v.CardSpeaking("ask-1", now.Add(11*time.Second)) {
		t.Fatal("the mark lapses")
	}
	v.MarkCard("t", "", now) // the voice stopped
	if v.CardSpeaking("ask-1", now) || v.Is("t", now) {
		t.Fatal("an ended mark speaks nothing")
	}
	v.MarkCard("t", "ask-1", now.Add(10*time.Second))
	v.EndCard("ask-1") // hushed
	if v.CardSpeaking("ask-1", now) || v.Is("t", now) {
		t.Fatal("a hushed card stops speaking at once")
	}
}
