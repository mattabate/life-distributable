package notify

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"life/hub/internal/store"
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

// A card's push that hits a passing failure (APNs 503, a network blip) is
// sent again a moment later while the card is open; a 400 is final, and a
// card closed meanwhile is not retried.
func TestCardPushRetriesTransientFailures(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rig := func(t *testing.T, answer func(n int32) int, open Open) (*APNs, *atomic.Int32) {
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(answer(n.Add(1)))
			w.Write([]byte(`{"reason":"x"}`))
		}))
		t.Cleanup(srv.Close)
		db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		a := &APNs{db: db, keyID: "k", teamID: "t", bundle: "b", defHost: srv.URL, key: key,
			client: srv.Client(), retryWaits: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}, Open: open}
		if err := a.Register("abcdef0123456789", "phone", "", 1); err != nil {
			t.Fatal(err)
		}
		return a, &n
	}
	settle := func(n *atomic.Int32, want int32) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for n.Load() < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond) // and nothing more after it
		if got := n.Load(); got != want {
			t.Fatalf("sends = %d, want %d", got, want)
		}
	}
	always := func(string) bool { return true }

	a, n := rig(t, func(i int32) int { return map[bool]int{true: 503, false: 200}[i == 1] }, always)
	if err := a.sendCard([]byte(`{}`), "ask-1"); err == nil {
		t.Fatal("the first send's 503 is still reported")
	}
	settle(n, 2)

	a, n = rig(t, func(int32) int { return 400 }, always)
	a.sendCard([]byte(`{}`), "ask-1")
	settle(n, 1)

	var closed atomic.Bool
	a, n = rig(t, func(int32) int { closed.Store(true); return 503 }, func(string) bool { return !closed.Load() })
	a.sendCard([]byte(`{}`), "ask-1")
	settle(n, 1)
}

// A provider JWT APNs answers 403 ExpiredProviderToken to is dropped and the
// card's retry goes out under a newly signed one.
func TestRefusedProviderTokenIsResigned(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("authorization"))
		first := len(auths) == 1
		mu.Unlock()
		if first {
			w.WriteHeader(403)
			w.Write([]byte(`{"reason":"ExpiredProviderToken"}`))
		}
	}))
	t.Cleanup(srv.Close)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	a := &APNs{db: db, keyID: "k", teamID: "t", bundle: "b", defHost: srv.URL, key: key,
		client: srv.Client(), retryWaits: []time.Duration{10 * time.Millisecond}, Open: func(string) bool { return true }}
	if err := a.Register("abcdef0123456789", "phone", "", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.sendCard([]byte(`{}`), "ask-1"); err == nil {
		t.Fatal("the first send's 403 is still reported")
	}
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(auths)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 2 {
		t.Fatalf("sends = %d, want 2 (the refused one, then a retry)", len(auths))
	}
	if auths[0] == auths[1] {
		t.Fatal("the retry reused the JWT APNs refused")
	}
}

// Register takes only a hex device token: a malformed one would never be
// dropped (BadDeviceToken rows are kept), so it is refused at the door and
// no row is written.
func TestRegisterRefusesANonHexToken(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	a := &APNs{db: db}
	for _, bad := range []string{"<abcd 1234>", "abc", "abcdef012", "abcd/../x0", "abcdefgh"} {
		if err := a.Register(bad, "phone", "", 1); err == nil {
			t.Errorf("Register(%q) took it", bad)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows written for refused tokens", n)
	}
	if err := a.Register("  ABCDEF0123456789\n", "phone", "", 1); err != nil {
		t.Fatalf("a padded upper-case hex token was refused: %v", err)
	}
	var tok string
	db.QueryRow(`SELECT token FROM devices`).Scan(&tok)
	if tok != "abcdef0123456789" {
		t.Fatalf("stored %q, want the trimmed lower-case token", tok)
	}
}
