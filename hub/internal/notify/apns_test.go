package notify

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A push waits while the floor is held (a replay, a pitch round) and goes out
// once it frees; with the floor free it goes at once.
func TestPushWaitsForTheVoiceFloor(t *testing.T) {
	sent := make(chan struct{}, 2)
	var held atomic.Bool
	held.Store(true)
	a := &APNs{after: func(_ time.Duration, f func()) { f() }, Floor: held.Load}
	a.deliverFn = func([]byte) error { sent <- struct{}{}; return nil }
	if err := a.PushCard("read", "t", "One.", "th-1", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("a push went out over a held floor")
	case <-time.After(3 * floorPoll):
	}
	held.Store(false)
	select {
	case <-sent:
	case <-time.After(5 * floorPoll):
		t.Fatal("the held push never went out once the floor freed")
	}
}

// The phone's own push is passive and wakes the app: Siri never reads a long
// notification, the app does. When the Mac speaks the card keeps its level.
func TestPhoneVoicePushIsPassiveAndWakesTheApp(t *testing.T) {
	var got struct {
		Aps struct {
			Alert            map[string]string `json:"alert"`
			Level            string            `json:"interruption-level"`
			ContentAvailable int               `json:"content-available"`
		} `json:"aps"`
		Kind   string `json:"kind"`
		Thread string `json:"thread"`
		At     int64  `json:"at"`
	}
	json.Unmarshal(apsPayload("needs_you", "", "Hey Alex, the build is out.", false, "th-1", -1), &got)
	if got.Aps.Level != "passive" || got.Aps.ContentAvailable != 1 || got.Aps.Alert["body"] != "Hey Alex, the build is out." || got.Kind != "needs_you" || got.Thread != "th-1" {
		t.Fatalf("phone voice payload: %+v", got)
	}
	// Stamped with its send time, so a push iOS delivers minutes late is
	// left unspoken.
	if age := time.Since(time.Unix(got.At, 0)); age < 0 || age > time.Minute {
		t.Fatalf("push sent at %v, want now", got.At)
	}
	if _, has := got.Aps.Alert["title"]; has {
		t.Fatalf("an empty title must be left out: %+v", got.Aps.Alert)
	}
	got.Aps.ContentAvailable = 0 // Unmarshal leaves an absent key as it was
	json.Unmarshal(apsPayload("needs_you", "Needs you", "Install app build 1226", true, "", -1), &got)
	if got.Aps.Level != "time-sensitive" || got.Aps.ContentAvailable != 0 || got.Aps.Alert["title"] != "Needs you" {
		t.Fatalf("Mac-spoken needs-you payload: %+v", got)
	}
	if strings.Contains(string(apsPayload("needs_you", "Needs you", "x", true, "", -1)), `"thread"`) {
		t.Fatal("a push with no session carries no thread")
	}
	if p := string(apsPayload("needs_you", "Needs you", "x", true, "", -1)); strings.Contains(p, `"badge"`) {
		t.Fatalf("no badge reader, no badge: %s", p)
	}
	if p := string(apsPayload("needs_you", "Needs you", "x", true, "", 0)); !strings.Contains(p, `"badge":0`) {
		t.Fatalf("a zero badge clears the icon: %s", p)
	}
	json.Unmarshal(apsPayload("read", "To read", "x", true, "", -1), &got)
	if got.Aps.Level != "active" {
		t.Fatalf("Mac-spoken read payload: %+v", got)
	}
	// The phone's own reckoning of a line: a beat to start, then words.
	if d := PhoneSpeakTime("one two three four five"); d != 4*time.Second {
		t.Fatalf("PhoneSpeakTime(5 words) = %v, want 4s", d)
	}
}

// A 700-character line goes out whole; only past voiceMax is it cut.
func TestPushKeepsTheWholeSpokenLine(t *testing.T) {
	var sent []byte
	a := &APNs{after: func(_ time.Duration, f func()) { f() }} // a queued push goes now
	a.deliverFn = func(p []byte) error { sent = p; return nil }
	line := strings.Repeat("word ", 140) // 700 chars
	if err := a.push("read", "", line, "", ""); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Aps struct {
			Alert map[string]string `json:"alert"`
		} `json:"aps"`
	}
	json.Unmarshal(sent, &got)
	if got.Aps.Alert["body"] != line {
		t.Fatalf("body cut to %d chars", len(got.Aps.Alert["body"]))
	}
	a.push("read", "", strings.Repeat("é", voiceMax+100), "", "")
	json.Unmarshal(sent, &got)
	if r := []rune(got.Aps.Alert["body"]); len(r) != voiceMax || r[len(r)-1] != '…' {
		t.Fatalf("an over-long body is cut at %d runes ending in …, got %d", voiceMax, len(r))
	}
}

// The icon follows the nav total: a badge-only push when it moves, nothing
// when it holds, nothing when the board read fails.
func TestSyncBadgeSendsOnlyWhenTheTotalMoves(t *testing.T) {
	n := 3
	var sent []string
	a := &APNs{lastBadge: -1, Badge: func() int { return n },
		deliverFn: func(p []byte) error { sent = append(sent, string(p)); return nil }}
	a.SyncBadge()
	a.SyncBadge()
	n = -1
	a.SyncBadge()
	n = 0
	a.SyncBadge()
	if len(sent) != 2 || !strings.Contains(sent[0], `"badge":3`) || !strings.Contains(sent[1], `"badge":0`) || strings.Contains(sent[0], `"alert"`) {
		t.Fatalf("badge pushes: %q", sent)
	}
}

// Pushes are spoken one at a time: each books the time Siri needs to say it,
// and the next waits behind it — however many land at once.
func TestSlotQueuesPushesBehindOneAnother(t *testing.T) {
	a := &APNs{}
	now := time.Date(2026, 9, 19, 22, 0, 0, 0, time.UTC)
	first, second := "Needs you Install app build 1064", "To read Yes, 45% is a real difference"
	if w := a.slot(first, now); w != 0 {
		t.Fatalf("an empty queue sends now, waited %v", w)
	}
	if w := a.slot(second, now); w != speakTime(first) {
		t.Fatalf("second push waited %v, want the first one's %v", w, speakTime(first))
	}
	if w := a.slot("third", now); w != speakTime(first)+speakTime(second) {
		t.Fatalf("third push waited %v, want %v", w, speakTime(first)+speakTime(second))
	}
	// Once everything has been said the queue is clear again.
	later := now.Add(speakTime(first) + speakTime(second) + speakTime("third"))
	if w := a.slot("fourth", later); w != 0 {
		t.Fatalf("a drained queue sends now, waited %v", w)
	}
	if speakTime("") < 5*time.Second || speakTime(string(make([]byte, 400))) > 40*time.Second {
		t.Fatalf("speakTime out of range: %v … %v", speakTime(""), speakTime(string(make([]byte, 400))))
	}
}
