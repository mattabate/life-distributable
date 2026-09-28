package notify

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A card's push marks nothing "speaking" here: the phone may find nothing on
// the owner's ears and stay silent (headphones not in) — the phone's own `app/speech`
// report marks it (server/obs.go). A push with no session marks nothing.
func TestPushMarksTheSessionSpeaking(t *testing.T) {
	marks := map[string]time.Time{}
	var mu sync.Mutex
	mark := func(id string) (time.Time, bool) { mu.Lock(); defer mu.Unlock(); u, ok := marks[id]; return u, ok }
	v := markFn(func(id string, until time.Time) { mu.Lock(); marks[id] = until; mu.Unlock() })
	a := &APNs{after: func(_ time.Duration, f func()) { f() }, Voice: v}
	a.deliverFn = func([]byte) error { return nil }
	if err := a.PushCard("read", "line", "Hey Alex, the sessions list says who is speaking now.", "t-1", ""); err != nil {
		t.Fatal(err)
	}
	if until, ok := mark("t-1"); ok {
		t.Fatalf("phone voice: marked until %v before the phone said a word", until)
	}
	if err := a.Push("read", "To read", "x"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(marks) != 0 {
		t.Fatalf("a push marked %v", marks)
	}
}

type markFn func(string, time.Time)

func (f markFn) Mark(id string, until time.Time) { f(id, until) }
func (f markFn) Wait(string, string) func()      { return func() {} }

// A push held behind another voice (the floor) reads "waiting to speak", not
// "speaking" — but only when the phone last reported headphones; the wait ends
// when the line goes out to the phone, and "speaking" is the phone's word once
// it has begun.
func TestHeldPushWaitsToSpeak(t *testing.T) {
	v := &fakeVoice{marks: map[string]time.Time{}, held: map[string]int{}}
	var busy atomic.Bool
	busy.Store(true)
	sent := make(chan bool, 2)
	a := &APNs{after: func(_ time.Duration, f func()) { f() }, Voice: v, Floor: busy.Load}
	a.deliverFn = func([]byte) error { sent <- true; return nil }
	// Nothing on the owner's ears (the phone last said Speaker): the held push
	// is a banner to come, not a voice — no pill.
	a.PhoneRoute(false)
	if err := a.PushCard("read", "line", "Hey Alex, nobody will hear this one.", "t-0", ""); err != nil {
		t.Fatal(err)
	}
	if v.state("t-0") != "" {
		t.Fatalf("no headphones anywhere: %q, want no pill", v.state("t-0"))
	}
	// The phone last spoke into headphones: the held line waits to speak.
	a.PhoneRoute(true)
	if err := a.PushCard("read", "line", "Hey Alex, a card waiting for the floor.", "t-1", ""); err != nil {
		t.Fatal(err)
	}
	if v.state("t-1") != "waiting" {
		t.Fatalf("held behind the floor: %q, want waiting", v.state("t-1"))
	}
	busy.Store(false)
	<-sent
	<-sent
	deadline := time.Now().Add(2 * time.Second)
	for v.state("t-1") == "waiting" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if v.state("t-1") != "" {
		t.Fatalf("sent: %q, want neither waiting nor speaking until the phone reports", v.state("t-1"))
	}
}

// A double tap on "Waiting to speak": the line goes as if answered, the card
// stays open, and raising the card again speaks it as new.
func TestHushDropsTheLineNotTheCard(t *testing.T) {
	v := &fakeVoice{marks: map[string]time.Time{}, held: map[string]int{}}
	var busy atomic.Bool
	busy.Store(true)
	sent := make(chan struct{}, 2)
	a := &APNs{after: func(_ time.Duration, f func()) { f() }, Voice: v, Floor: busy.Load,
		Open: func(string) bool { return true }}
	a.deliverFn = func([]byte) error { sent <- struct{}{}; return nil }
	a.PhoneRoute(true)
	if err := a.PushCard("read", "line", "Hey Alex, hush me.", "t-1", "ask-1"); err != nil {
		t.Fatal(err)
	}
	if v.state("t-1") != "waiting" {
		t.Fatalf("held behind the floor: %q, want waiting", v.state("t-1"))
	}
	a.Hush("ask-1")
	deadline := time.Now().Add(3 * closedPoll)
	for v.state("t-1") == "waiting" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if v.state("t-1") != "" {
		t.Fatalf("hushed: %q, want no pill", v.state("t-1"))
	}
	if !a.Open("ask-1") {
		t.Fatal("the hush closed the card")
	}
	busy.Store(false)
	select {
	case <-sent:
		t.Fatal("a hushed line was sent after the floor freed")
	case <-time.After(2 * time.Second):
	}
	if err := a.PushCard("read", "line", "Hey Alex, raised again.", "t-1", "ask-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("a card raised again after a hush stayed silent")
	}
}

type fakeVoice struct {
	mu    sync.Mutex
	marks map[string]time.Time
	held  map[string]int
}

func (v *fakeVoice) Mark(id string, until time.Time) { v.mu.Lock(); v.marks[id] = until; v.mu.Unlock() }
func (v *fakeVoice) Wait(id, _ string) func() {
	v.mu.Lock()
	v.held[id]++
	v.mu.Unlock()
	return func() { v.mu.Lock(); v.held[id]--; v.mu.Unlock() }
}
func (v *fakeVoice) state(id string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.marks[id].After(time.Now()) {
		return "speaking"
	}
	if v.held[id] > 0 {
		return "waiting"
	}
	return ""
}
