package notify

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

func earTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// earTest: an APNs whose phone pushes land on the channel, with a db for the
// Audio switches.
func earTest(t *testing.T) (*APNs, chan []byte) {
	sent := make(chan []byte, 4)
	a := &APNs{db: earTestDB(t), host: "mini", after: func(_ time.Duration, f func()) { f() }}
	a.deliverFn = func(p []byte) error { sent <- p; return nil }
	return a, sent
}

// waitLine polls as `device` (its voice helper on the hub's Mac, the app on
// another) until the hub hands it a line.
func waitLine(t *testing.T, a *APNs, device, route string) *EarLine {
	t.Helper()
	for range 300 { // Resume waits a poll interval past the first poll
		if v := a.EarPoll(device, route, "", 0, device == a.host); v.Line != nil {
			return v.Line
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was never handed a line", device)
	return nil
}

func passive(t *testing.T, p []byte) bool {
	t.Helper()
	var got struct {
		Aps struct {
			Level string `json:"interruption-level"`
		} `json:"aps"`
	}
	if err := json.Unmarshal(p, &got); err != nil {
		t.Fatal(err)
	}
	return got.Aps.Level == "passive"
}

// Who says a line: a Mac with headphones in, then a Mac on its speakers only
// while the owner is at it, never a Mac whose Audio switch is off.
func TestEarPickOrder(t *testing.T) {
	a, _ := earTest(t)
	a.EarPoll("MacBook-Pro.local", "speakers", "MacBook Pro Speakers", 3, false)
	a.EarPoll("mini", "speakers", "", 900, true) // nobody at the hub's Mac
	if d, ok := a.earPick(true); ok {
		t.Fatalf("no headphones anywhere, picked %s", d)
	}
	if d, ok := a.earPick(false); !ok || d != "macbook-pro" {
		t.Fatalf("speakers rung picked %q %v, want the MacBook the owner is at", d, ok)
	}
	a.EarPoll("mini", "headphones", "AirPods Pro", 900, true)
	if d, ok := a.earPick(true); !ok || d != "mini" {
		t.Fatalf("headphones rung picked %q %v, want mini", d, ok)
	}
	a.SetAudio("mini", false)
	if d, ok := a.earPick(true); ok {
		t.Fatalf("a Mac with Audio off was picked: %s", d)
	}
	a.SetAudio("macbook-pro", false)
	if a.macEars() {
		t.Fatal("every Mac's Audio is off, yet one would speak")
	}
	macs := a.Macs()
	if len(macs) != 2 || macs[0].Device != "mini" || !macs[0].Hub || macs[0].Audio || macs[0].Ears != "headphones" || macs[0].Output != "AirPods Pro" {
		t.Fatalf("Devices rows: %+v", macs)
	}
	if !macs[1].Here || macs[1].Ears != "speakers" {
		t.Fatalf("the MacBook's row: %+v", macs[1])
	}
}

// On a Mac whose voice helper polls, the desktop app's poll is not its ear.
func TestHelperPollOutranksTheApp(t *testing.T) {
	a, _ := earTest(t)
	a.EarPoll("mini", "speakers", "", 0, true)
	a.EarPoll("mini", "headphones", "", 0, false)
	if _, ok := a.earPick(true); ok {
		t.Fatal("the app's headphones overrode the helper's speakers")
	}
}

// The phone with headphones beats a Mac on its speakers: the push is the
// phone's own line, and no Mac is handed it.
func TestPhoneHeadphonesBeatMacSpeakers(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "speakers", "", 0, true)
	a.PhoneRoute(true)
	if err := a.PushCard("read", "", "Hey, one line.", "", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-sent:
		if !passive(t, p) {
			t.Fatal("the phone's own line must be passive (the app says it)")
		}
	case <-time.After(time.Second):
		t.Fatal("no push to the phone")
	}
	if v := a.EarPoll("mini", "speakers", "", 0, true); v.Line != nil {
		t.Fatal("a Mac was handed the line the phone took")
	}
}

// A Mac says the line: playing, then spoke; the card is heard and the phone
// gets a plain banner (not a line to say again).
func TestMacSaysTheLineThenThePhoneGetsABanner(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "headphones", "", 0, true)
	if err := a.PushCard("read", "", "Hey, the build is out.", "t-1", "ask-1"); err != nil {
		t.Fatal(err)
	}
	l := waitLine(t, a, "mini", "headphones")
	if l.Text != "Hey, the build is out." || l.Card != "ask-1" {
		t.Fatalf("line %+v", l)
	}
	a.EarReport("mini", l.ID, "playing")
	a.EarReport("mini", l.ID, "spoke")
	select {
	case p := <-sent:
		if passive(t, p) {
			t.Fatal("after the Mac said it, the phone must not say it again")
		}
	case <-time.After(time.Second):
		t.Fatal("no banner after the Mac's line")
	}
	var heard string
	a.db.QueryRow(`SELECT heard_at FROM voice_queue WHERE card='ask-1'`).Scan(&heard)
	if heard == "" {
		t.Fatal("the Mac's spoke did not mark the card heard")
	}
}

// A line picked for the headphones stops when they leave, even though macOS
// falls back to the speakers and the owner is at the Mac.
func TestHeadphonesTurnedSpeakersStopTheLine(t *testing.T) {
	a, _ := earTest(t)
	a.EarPoll("mini", "headphones", "AirPods Pro", 0, true)
	a.PushCard("read", "", "Hey, private line.", "", "ask-ears")
	l := waitLine(t, a, "mini", "headphones")
	a.EarReport("mini", l.ID, "playing")
	if v := a.EarPoll("mini", "headphones", "AirPods Pro", 0, true); !v.Ears {
		t.Fatal("the headphones are still in, yet the line must stop")
	}
	if v := a.EarPoll("mini", "speakers", "Mac mini Speakers", 0, true); v.Ears {
		t.Fatal("the headphones left mid-line and the Mac goes on out loud")
	}
}

// The one begun line the phone says again: its headphones left mid-line.
func TestHeadphonesLeftLineGoesBackToThePhone(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "headphones", "", 0, true)
	a.PushCard("read", "", "Hey, half said.", "t-1", "ask-half")
	l := waitLine(t, a, "mini", "headphones")
	a.EarReport("mini", l.ID, "playing")
	a.EarReport("mini", l.ID, "silent")
	select {
	case p := <-sent:
		if !passive(t, p) {
			t.Fatal("the rest of the line was never heard; the phone must say it")
		}
	case <-time.After(time.Second):
		t.Fatal("no push after the headphones left")
	}
	var heard string
	a.db.QueryRow(`SELECT heard_at FROM voice_queue WHERE card='ask-half'`).Scan(&heard)
	if heard != "" {
		t.Fatal("the line the phone now says is still marked heard")
	}
}

// A speakers line, once begun, goes on when the owner steps away from the
// Mac (being at it is checked at the pick only), and a begun speakers line
// that ends silent is heard: the phone gets a banner, not the line again.
func TestSpeakersLineHoldsWhenTheOwnerStepsAway(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "speakers", "", 0, true)
	a.PushCard("read", "", "Hey, out loud.", "t-1", "ask-loud")
	l := waitLine(t, a, "mini", "speakers")
	a.EarReport("mini", l.ID, "playing")
	if v := a.EarPoll("mini", "speakers", "", 900, true); !v.Ears || v.Line == nil {
		t.Fatal("idle passing five minutes mid-line stopped the line")
	}
	a.EarReport("mini", l.ID, "silent")
	select {
	case p := <-sent:
		if passive(t, p) {
			t.Fatal("a begun speakers line was sent to the phone to say again")
		}
	case <-time.After(time.Second):
		t.Fatal("no banner after the begun line")
	}
	var heard string
	a.db.QueryRow(`SELECT heard_at FROM voice_queue WHERE card='ask-loud'`).Scan(&heard)
	if heard == "" {
		t.Fatal("a begun speakers line was un-heard")
	}
}

// A line that arrives while a Mac says another waits its turn even when no
// Mac would take it now: the phone never says it before the line ahead.
func TestLineBehindAMacLineWaitsItsTurn(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "speakers", "", 0, true)
	a.PushCard("read", "", "Hey, first.", "", "ask-first")
	l := waitLine(t, a, "mini", "speakers")
	a.EarReport("mini", l.ID, "playing")
	a.EarPoll("mini", "speakers", "", 900, true) // no Mac qualifies for a new line
	a.PushCard("read", "", "Hey, second.", "", "ask-second")
	select {
	case <-sent:
		t.Fatal("the second line reached the phone while the first was being said")
	case <-time.After(100 * time.Millisecond):
	}
	a.EarReport("mini", l.ID, "spoke")
	for i, want := range []bool{false, true} {
		select {
		case p := <-sent:
			if passive(t, p) != want {
				t.Fatalf("push %d: passive %v, want %v", i+1, !want, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("push %d never came", i+1)
		}
	}
}

// A begun line whose spoke report is lost is heard: the card is marked at the
// first word, and the phone gets a banner, not the line again.
func TestLostSpokeAfterPlayingIsHeard(t *testing.T) {
	a, sent := earTest(t)
	a.ear.said = 50 * time.Millisecond
	a.EarPoll("mini", "headphones", "", 0, true)
	a.PushCard("read", "", "Hey, my end got lost.", "t-1", "ask-lost")
	l := waitLine(t, a, "mini", "headphones")
	a.EarReport("mini", l.ID, "playing")
	var heard string
	for range 100 {
		a.db.QueryRow(`SELECT heard_at FROM voice_queue WHERE card='ask-lost'`).Scan(&heard)
		if heard != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if heard == "" {
		t.Fatal("the first word did not mark the card heard")
	}
	select {
	case p := <-sent:
		if passive(t, p) {
			t.Fatal("a line the Mac began was sent to the phone to say again")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no banner after the begun line")
	}
}

// Lines come out on the Mac in the order their cards arrived.
func TestMacLinesKeepArrivalOrder(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "headphones", "", 0, true)
	want := []string{"Hey, one.", "Hey, two.", "Hey, three.", "Hey, four.", "Hey, five."}
	for i, w := range want {
		a.PushCard("read", "", w, "", "ask-order-"+string(rune('a'+i)))
	}
	for _, w := range want {
		l := waitLine(t, a, "mini", "headphones")
		if l.Text != w {
			t.Fatalf("said %q, want %q", l.Text, w)
		}
		a.EarReport("mini", l.ID, "playing")
		a.EarReport("mini", l.ID, "spoke")
		select {
		case <-sent:
		case <-time.After(time.Second):
			t.Fatalf("no banner after %q", w)
		}
		for range 100 { // the next line is handed out once this one is withdrawn
			if v := a.EarPoll("mini", "headphones", "", 0, true); v.Line == nil || v.Line.ID != l.ID {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// At startup Resume waits for a Mac's ear to poll before it picks a voice: a
// line the last hub left goes to the Mac with headphones in, not the phone.
func TestResumeWaitsForTheFirstEarPoll(t *testing.T) {
	a, sent := earTest(t)
	a.ear.boot = 2 * time.Second
	a.enqueue("ask-left", "t-1", "read", "", "Hey, left by the last hub.")
	done := make(chan struct{})
	go func() { a.Resume(); close(done) }()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-sent:
		t.Fatal("Resume sent the line to the phone before any ear polled")
	default:
	}
	a.EarPoll("mini", "headphones", "", 0, true)
	l := waitLine(t, a, "mini", "headphones")
	if l.Card != "ask-left" {
		t.Fatalf("line %+v", l)
	}
	a.EarReport("mini", l.ID, "playing")
	a.EarReport("mini", l.ID, "spoke")
	<-done
	// No ear at all: Resume gives up waiting after boot and the phone says it.
	b, bsent := earTest(t)
	b.ear.boot = 50 * time.Millisecond
	b.enqueue("ask-alone", "t-1", "read", "", "Hey, no Mac awake.")
	b.Resume()
	select {
	case p := <-bsent:
		if !passive(t, p) {
			t.Fatal("with no ear the phone must say the line")
		}
	case <-time.After(time.Second):
		t.Fatal("Resume never fell back to the phone")
	}
}

// A row heard, or raised again (its own push says it), while Resume waits
// for an ear is not said again by Resume.
func TestResumeSkipsRowsHeardOrRaisedDuringTheWait(t *testing.T) {
	a, sent := earTest(t)
	a.ear.boot = 300 * time.Millisecond
	a.enqueue("ask-heard", "t-1", "read", "", "Hey, heard meanwhile.")
	a.enqueue("ask-again", "t-2", "read", "", "Hey, raised again.")
	done := make(chan struct{})
	go func() { a.Resume(); close(done) }()
	time.Sleep(50 * time.Millisecond)
	a.heardCard("ask-heard")
	a.enqueue("ask-again", "t-2", "read", "", "Hey, raised again.")
	<-done
	select {
	case p := <-sent:
		t.Fatalf("Resume pushed a row it no longer owned: %s", p)
	default:
	}
	var resumed string
	a.db.QueryRow(`SELECT resumed_at FROM voice_queue WHERE card='ask-again'`).Scan(&resumed)
	if resumed != "" {
		t.Fatal("the raised-again row was stamped resumed")
	}
}

// At startup a new card waits behind the lines Resume holds, and for the
// Mac's ear: it is said on the Mac after them, never on the phone first.
// With no ear at all, the gate opens when Resume gives up, in order.
func TestBootGateHoldsNewPushesBehindResume(t *testing.T) {
	a, sent := earTest(t)
	a.ear.boot = 2 * time.Second
	a.Booting()
	a.enqueue("ask-left", "t-1", "read", "", "Hey, left by the last hub.")
	done := make(chan struct{})
	go func() { a.Resume(); close(done) }()
	time.Sleep(50 * time.Millisecond)
	if err := a.PushCard("read", "", "Hey, new since the restart.", "t-2", "ask-new"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	select {
	case <-sent:
		t.Fatal("a push reached the phone before any ear polled")
	default:
	}
	last := ""
	for _, want := range []string{"ask-left", "ask-new"} {
		l := waitLine(t, a, "mini", "headphones")
		for l.ID == last { // the said line until the hub takes it back
			time.Sleep(10 * time.Millisecond)
			l = waitLine(t, a, "mini", "headphones")
		}
		last = l.ID
		if l.Card != want {
			t.Fatalf("line %s, want %s", l.Card, want)
		}
		a.EarReport("mini", l.ID, "playing")
		a.EarReport("mini", l.ID, "spoke")
	}
	<-done

	b, bsent := earTest(t)
	b.ear.boot = 50 * time.Millisecond
	b.Booting()
	b.enqueue("ask-old", "t-1", "read", "", "Hey, the old line.")
	bdone := make(chan struct{})
	go func() { b.Resume(); close(bdone) }()
	time.Sleep(10 * time.Millisecond)
	b.PushCard("read", "", "Hey, the new line.", "t-2", "ask-young")
	for _, want := range []string{"the old line", "the new line"} {
		select {
		case p := <-bsent:
			if !passive(t, p) || !strings.Contains(string(p), want) {
				t.Fatalf("want %q said on the phone, got %s", want, p)
			}
		case <-time.After(time.Second):
			t.Fatalf("%q never went out", want)
		}
	}
	<-bdone
}

// The safety timer firing while Resume is still at work does not open the
// gate: held pushes go after every resumed row.
func TestBootTimeoutMidResumeKeepsHeldPushesLast(t *testing.T) {
	a, sent := earTest(t)
	a.ear.boot = 300 * time.Millisecond
	a.Booting()
	a.enqueue("ask-one", "t-1", "read", "", "Hey, resumed one.")
	time.Sleep(time.Millisecond)
	a.enqueue("ask-two", "t-1", "read", "", "Hey, resumed two.")
	done := make(chan struct{})
	go func() { a.Resume(); close(done) }()
	time.Sleep(50 * time.Millisecond)
	a.PushCard("read", "", "Hey, the held one.", "t-2", "ask-held")
	a.bootTimeout() // the 40 s timer, firing mid-Resume
	for _, want := range []string{"resumed one", "resumed two", "the held one"} {
		select {
		case p := <-sent:
			if !strings.Contains(string(p), want) {
				t.Fatalf("want %q next, got %s", want, p)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q never went out", want)
		}
	}
	<-done
}

// A test push has no card to resume: during boot it skips the gate and
// answers its real error.
func TestTestPushSkipsTheBootGate(t *testing.T) {
	a := &APNs{db: earTestDB(t), host: "mini", after: func(_ time.Duration, f func()) { f() }}
	a.Booting()
	if err := a.Test(""); err == nil {
		t.Fatal("with no ear and no push key a test during boot answered OK")
	}
}

// A hub restarted mid-line on a Mac: the card is heard from its first word,
// so the next hub does not say it again, but the phone gets its banner. With
// no phone, nothing.
func TestRestartMidMacLineSendsTheBanner(t *testing.T) {
	for _, phone := range []bool{true, false} {
		db := earTestDB(t)
		first := &APNs{db: db, host: "mini", after: func(_ time.Duration, f func()) { f() }}
		first.EarPoll("mini", "headphones", "", 0, true)
		first.PushCard("read", "", "Hey, cut by a restart.", "t-1", "ask-cut")
		l := waitLine(t, first, "mini", "headphones")
		first.EarReport("mini", l.ID, "playing")
		for range 100 {
			if db.Setting(earBegunKey) != "" {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if db.Setting(earBegunKey) != "ask-cut" {
			t.Fatal("the begun line was not recorded")
		}
		// The hub stops here; the next one comes up.
		sent := make(chan []byte, 4)
		next := &APNs{db: db, host: "mini", after: func(_ time.Duration, f func()) { f() }}
		next.ear.boot = -1
		if phone {
			next.deliverFn = func(p []byte) error { sent <- p; return nil }
		}
		next.Resume()
		select {
		case p := <-sent:
			if passive(t, p) {
				t.Fatal("the begun line was sent to the phone to say again")
			}
		case <-time.After(200 * time.Millisecond):
			if phone {
				t.Fatal("no banner for the line the restart cut")
			}
		}
		if db.Setting(earBegunKey) != "" {
			t.Fatal("the begun-line row outlived the resume")
		}
		first.StopLine(l.ID)
	}
}

// A line no Mac starts within the claim goes to the phone to say.
func TestUnclaimedLineGoesToThePhone(t *testing.T) {
	a, sent := earTest(t)
	a.ear.claim = 50 * time.Millisecond
	a.EarPoll("mini", "headphones", "", 0, true)
	a.PushCard("read", "", "Hey, nobody claimed me.", "", "")
	select {
	case p := <-sent:
		if !passive(t, p) {
			t.Fatal("an unsaid line goes to the phone to say")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the unclaimed line never reached the phone")
	}
}

// Stop and the Audio switch cut the line being said; the card stays open and
// nothing follows on the phone.
func TestStopAndAudioOffCutTheLine(t *testing.T) {
	for _, how := range []string{"stop", "audio"} {
		a, sent := earTest(t)
		a.EarPoll("mini", "headphones", "", 0, true)
		a.PushCard("read", "", "Hey, cut me.", "", "ask-"+how)
		l := waitLine(t, a, "mini", "headphones")
		a.EarReport("mini", l.ID, "playing")
		if !a.Macs()[0].Saying {
			t.Fatalf("%s: the hub's tile does not show it saying", how)
		}
		if how == "stop" {
			if a.StopLine("ear-gone") {
				t.Fatal("a Stop for a line that ended cut the one being said")
			}
			if !a.StopLine(a.Macs()[0].Line) {
				t.Fatal("Stop found no line")
			}
		} else {
			a.SetAudio("mini", false)
		}
		for range 100 {
			if a.EarPoll("mini", "headphones", "", 0, true).Line == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if v := a.EarPoll("mini", "headphones", "", 0, true); v.Line != nil {
			t.Fatalf("%s: the line is still handed out", how)
		}
		select {
		case <-sent:
			t.Fatalf("%s: a cut line went on to the phone", how)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// The Mac test line never replaces a card's line being said.
func TestMacTestWaitsForTheCardLine(t *testing.T) {
	a, _ := earTest(t)
	a.EarPoll("mini", "headphones", "", 0, true)
	a.PushCard("read", "", "Hey, a card line.", "", "ask-held")
	l := waitLine(t, a, "mini", "headphones")
	a.EarReport("mini", l.ID, "playing")
	if err := a.Test("mac"); err == nil {
		t.Fatal("the test was taken while a card's line was being said")
	}
	if got := waitLine(t, a, "mini", "headphones"); got.ID != l.ID {
		t.Fatalf("the card's line was replaced by %+v", got)
	}
}

// A line queued behind another goes to the phone at once when, by its turn,
// no Mac can say it (no 15 s wait on a Mac that left).
func TestQueuedLineFallsToThePhoneWhenTheMacLeft(t *testing.T) {
	a, sent := earTest(t)
	a.EarPoll("mini", "headphones", "", 0, true)
	a.macLine.Lock() // a line before this one is being said
	a.PushCard("read", "", "Hey, second line.", "", "ask-second")
	a.SetAudio("mini", false)
	a.macLine.Unlock()
	select {
	case p := <-sent:
		if !passive(t, p) {
			t.Fatal("the phone must say the line no Mac could")
		}
	case <-time.After(time.Second):
		t.Fatal("the line waited on a Mac that can no longer say it")
	}
}

// With no push key the hub still says lines on a Mac, and pushes nothing.
func TestVoiceOnlyLaneSaysOnAMac(t *testing.T) {
	a := NewVoiceOnly(earTestDB(t))
	a.after = func(_ time.Duration, f func()) { f() }
	if a.Phone() {
		t.Fatal("no key, yet Phone()")
	}
	if err := a.PushCard("read", "", "Hey, nobody's here.", "", ""); err == nil {
		t.Fatal("no ear and no push answered no error")
	}
	a.EarPoll("mbp", "headphones", "", 0, false)
	if err := a.PushCard("read", "", "Hey, on the Mac.", "", ""); err != nil {
		t.Fatal(err)
	}
	l := waitLine(t, a, "mbp", "headphones")
	a.EarReport("mbp", l.ID, "silent") // nothing to fall back to: no panic, no send
	if err := a.Test("phone"); err == nil {
		t.Fatal("a phone test with no key answered no error")
	}
}
