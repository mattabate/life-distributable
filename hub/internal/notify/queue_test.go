package notify

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"life/hub/internal/store"
)

// Cards raised just before a hub restart must not die unspoken: a card's line is kept until it is HEARD, and the next hub
// speaks what the last one left in its queue — once, and only while the card
// is still open.
func TestUnheardCardIsSpokenAgainAfterARestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	body := func(p []byte) string {
		var got struct {
			Aps struct {
				Alert map[string]string `json:"alert"`
			} `json:"aps"`
		}
		json.Unmarshal(p, &got)
		return got.Aps.Alert["body"]
	}
	hub := func() *APNs {
		a := &APNs{db: db, after: func(_ time.Duration, f func()) { f() }}
		a.deliverFn = func(p []byte) error { sent = append(sent, body(p)); return nil }
		return a
	}
	// The first hub: the phone is the voice (no Mac), the push goes out, and
	// the phone never reports it spoken — the hub stops.
	first := hub()
	if err := first.PushCard("read", "line", "Hey Alex, one card.", "t-1", "ask-1"); err != nil {
		t.Fatal(err)
	}
	if err := first.PushCard("needs_you", "line", "Hey Alex, a second card you already closed.", "t-1", "ask-2"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d, want 2", len(sent))
	}
	// The next hub: the open card is spoken again, the closed one is not.
	sent = nil
	second := hub()
	second.Open = func(card string) bool { return card == "ask-1" }
	second.Resume()
	if len(sent) != 1 || sent[0] != "Hey Alex, one card." {
		t.Fatalf("after the restart: %q, want the one open card", sent)
	}
	// Heard now (the phone's report, word for word): the third hub says nothing;
	// nor would it for the closed card, resumed once already.
	second.Heard("t-1", "Hey Alex, one card.")
	sent = nil
	third := hub()
	third.Resume()
	if len(sent) != 0 {
		t.Fatalf("a heard card was spoken again: %q", sent)
	}
	// A phone report whose words differ from the row's (the app trims what
	// it says) still closes the session's newest unheard line.
	fourth := hub()
	if err := fourth.PushCard("read", "line", "Hey Alex, a third card.", "t-1", "ask-3"); err != nil {
		t.Fatal(err)
	}
	fourth.Heard("t-1", "Hey Alex, a third card")
	sent = nil
	fifth := hub()
	fifth.Resume()
	if len(sent) != 0 {
		t.Fatalf("a card the phone spoke was spoken again: %q", sent)
	}
	// A card left unheard for longer than resumeWindow stays quiet: a
	// restart an hour later must not read out a stale board.
	db.Exec(`UPDATE voice_queue SET heard_at='', resumed_at='', queued_at=? WHERE card='ask-2'`, store.TS(time.Now().Add(-2*resumeWindow)))
	sent = nil
	hub().Resume()
	if len(sent) != 0 {
		t.Fatalf("a stale card was spoken: %q", sent)
	}
}
