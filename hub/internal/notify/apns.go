package notify

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"life/hub/internal/store"
)

// APNs pushes to the Life app on the owner's iPhone (needs a paid Apple
// Developer team). Token-based auth: an ES256 JWT signed with the .p8
// key from developer.apple.com → Keys (APNs enabled). Stdlib only — Go's
// http.Client negotiates HTTP/2 over TLS, which APNs requires.
//
// Device tokens arrive from the app (POST /api/v1/devices) and live in the
// devices table; every registered token gets every push, and tokens APNs
// reports as Unregistered/BadDeviceToken are dropped.
//
// Host: development-signed builds (ops/install-phone.sh, aps-environment=
// development) only receive from the SANDBOX host; ad-hoc/App Store builds
// (ops/ota.sh, aps-environment=production) only from the production host.
// The app reports its build's aps-environment when it registers (devices.env),
// so each token is pushed through the matching host; tokens without one use
// the hub's apns_production default.
type APNs struct {
	db      *store.DB
	keyID   string
	teamID  string
	bundle  string
	defHost string
	key     *ecdsa.PrivateKey
	client  *http.Client

	mu       sync.Mutex
	jwt      string
	jwtIssue time.Time

	// free: when the spoken queue is next clear (see slot).
	qmu  sync.Mutex
	free time.Time
	// after runs a queued push later; time.AfterFunc outside tests.
	after func(time.Duration, func())
	// deliverFn stands in for deliver in tests (records the payload).
	deliverFn func(payload []byte) error
	// Voice is told which session is being heard while its push is spoken
	// (threads.Voice; the Sessions list's "speaking" pill). nil = none.
	Voice Voice
	// Floor reports that something else has the owner's ears — a card being heard or
	// replayed, a perfect-pitch round — and every push waits until it is
	// false (onFloor). nil = never held.
	Floor func() bool
	// Open says whether a queued card still wants the owner (queue.go Resume).
	Open Open
	// Badge is the number the app icon wears (server.BadgeTotal); every alert
	// carries it and SyncBadge re-sends it when it moves. nil or -1 = leave
	// the icon as it is.
	Badge     func() int
	bmu       sync.Mutex
	lastBadge int // the last number SyncBadge sent; -1 = none yet
	fmu       sync.Mutex
	fq        sync.Mutex
	held      int

	// Ears: whether anything is on the owner's ears to speak a push into — the
	// phone's last word on its route (ears.go).
	emu       sync.Mutex
	phoneEars earsSeen
	// hushed: cards whose line the owner dropped (Hush); the card stays open.
	hushed map[string]bool
}

// How long a push waits for the floor, and how often it looks. Past the wait
// it is spoken anyway: the card is still on the board, but a hold that never
// ends (a replay the page never closed) must not swallow every card.
const (
	floorPoll = 500 * time.Millisecond
	floorWait = 45 * time.Minute
)

// onFloor runs send once nothing else is being heard: only one session
// speaks at a time, and none plays over audio coming from the app. Held pushes go one at a time, in turn: each marks its own
// session speaking as it goes out, which holds the next. `wait` marks the
// session "waiting to speak" while it is held (a push that already waited
// for its slot arrives holding one); send ends it when the line is heard.
func (a *APNs) onFloor(send func(release func()) error, wait func() func(), rel func()) error {
	if a.Floor == nil || a.floorFree() {
		if rel == nil {
			rel = func() {}
		}
		return send(rel)
	}
	if rel == nil {
		rel = wait()
	}
	a.fmu.Lock()
	a.held++
	a.fmu.Unlock()
	go func() {
		a.fq.Lock()
		defer a.fq.Unlock()
		start := time.Now()
		for time.Since(start) < floorWait && a.Floor() {
			time.Sleep(floorPoll)
		}
		log.Printf("apns: push held %.0fs for the voice floor", time.Since(start).Seconds())
		if err := send(rel); err != nil {
			log.Printf("apns: floor-held push: %v", err)
		}
		a.fmu.Lock()
		a.held--
		a.fmu.Unlock()
	}()
	return nil
}

// floorFree: nothing is held ahead of this push and nothing is being heard.
func (a *APNs) floorFree() bool {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	return a.held == 0 && !a.Floor()
}

// APNsConfig: all fields required except Production.
type APNsConfig struct {
	KeyFile    string // .p8 path (secret, ops/secrets/)
	KeyID      string // 10-char key id
	TeamID     string
	BundleID   string
	Production bool
}

func NewAPNs(db *store.DB, c APNsConfig) (*APNs, error) {
	raw, err := os.ReadFile(c.KeyFile)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return nil, errors.New("apns: key file is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse key: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: key is not ECDSA (expected an APNs .p8)")
	}
	if c.KeyID == "" || c.TeamID == "" || c.BundleID == "" {
		return nil, errors.New("apns: key id, team id and bundle id are required")
	}
	host := hostSandbox
	if c.Production {
		host = hostProduction
	}
	// The devices table (env, build columns included) is store.Schema's.
	return &APNs{db: db, keyID: c.KeyID, teamID: c.TeamID, bundle: c.BundleID, defHost: host, key: ec,
		client: &http.Client{Timeout: 15 * time.Second}, lastBadge: -1,
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) }}, nil
}

// speakTime is how long Siri takes to read one push aloud: the chime, "Life",
// then title and body at roughly 14 characters a second, then the pause where
// it listens for a reply.
func speakTime(text string) time.Duration {
	return 6*time.Second + time.Duration(len(text))*time.Second/14
}

// slot queues pushes behind one another so they don't cut each other off.
// With Announce Notifications on, Siri reads each push into the owner's
// headphones; two that land together interrupt each other or collapse into "2 notifications from Life".
// Each push books the time it takes to say, and the next one waits for it.
// Returns how long this push must wait (0 = send now). In memory only; a
// restart drops every queued push, and what it dropped is spoken again from
// voice_queue when the hub is back (queue.go).
func (a *APNs) slot(text string, now time.Time) time.Duration {
	a.qmu.Lock()
	defer a.qmu.Unlock()
	at := now
	if a.free.After(at) {
		at = a.free
	}
	a.free = at.Add(speakTime(text))
	return at.Sub(now)
}

const (
	hostSandbox    = "https://api.sandbox.push.apple.com"
	hostProduction = "https://api.push.apple.com"
)

// hostFor maps a device's reported aps-environment to the APNs host.
func (a *APNs) hostFor(env string) string {
	switch env {
	case "production":
		return hostProduction
	case "development", "sandbox":
		return hostSandbox
	}
	return a.defHost
}

// token returns a provider JWT, reused for up to 50 minutes (Apple: refresh
// no more often than every 20 min, no less often than every hour).
func (a *APNs) token() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Since(a.jwtIssue) < 50*time.Minute {
		return a.jwt, nil
	}
	now := time.Now()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "ES256", "kid": a.keyID}) + "." +
		enc(map[string]any{"iss": a.teamID, "iat": now.Unix()})
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, sum[:])
	if err != nil {
		return "", err
	}
	// JWS wants the raw fixed-width r||s, not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.jwt = signing + "." + base64.RawURLEncoding.EncodeToString(sig)
	// Round(0): wall-clock age. The monotonic clock stops while the Mac sleeps,
	// so a JWT minted before a sleep stayed "under 50 minutes" and every push
	// got 403 ExpiredProviderToken (2026-10-03 00:21 on).
	a.jwtIssue = now.Round(0)
	return a.jwt, nil
}

// Register stores (or refreshes) a device token from the app. env is the
// build's aps-environment ("development"/"production"; "" keeps the hub
// default) — the same phone flips between them as LAN and OTA builds are
// installed over each other, and the token is re-posted on every launch.
// build = the app's CFBundleVersion (0 when an older app did not send it);
// "Install app build N" asks are reconciled against it.
func (a *APNs) Register(token, device, env string, build int) error {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return errors.New("empty token")
	}
	env = strings.TrimSpace(strings.ToLower(env))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := a.db.Exec(`INSERT INTO devices (token, device, env, build, created_at, updated_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(token) DO UPDATE SET device=excluded.device, env=excluded.env, build=excluded.build, updated_at=excluded.updated_at`, token, device, env, build, now, now)
	return err
}

// MaxBuild is the newest app build any registered phone has reported (0 = none).
func (a *APNs) MaxBuild() int {
	var n int
	a.db.QueryRow(`SELECT COALESCE(MAX(build),0) FROM devices`).Scan(&n)
	return n
}

type device struct {
	token, env string
	build      int
}

func (a *APNs) tokens() ([]device, error) {
	rows, err := a.db.Query(`SELECT token, env, build FROM devices ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []device
	for rows.Next() {
		var d device
		rows.Scan(&d.token, &d.env, &d.build)
		out = append(out, d)
	}
	return out, rows.Err()
}

// Devices lists registered tokens (prefix only) with their environment and
// build, so status/lifectl can tell which build (LAN dev / OTA ad-hoc) the phone runs.
func (a *APNs) Devices() []map[string]any {
	toks, _ := a.tokens()
	out := []map[string]any{}
	for _, d := range toks {
		out = append(out, map[string]any{"token": d.token[:min(8, len(d.token))] + "…", "env": d.env, "build": d.build})
	}
	return out
}

// Push sends one alert to every registered device. kind is "needs_you",
// "read" or "fyi"; it rides along in the payload so the app can route the tap
// (all land on the Sessions board today) and keep a read card's banner off
// the screen the owner is already looking at. Sent at once when the spoken queue is
// clear (returns the first delivery error after trying all tokens); otherwise
// sent when its slot comes up and delivery errors are logged.
func (a *APNs) Push(kind, title, body string) error {
	return a.push(kind, title, body, "", "")
}

// Test sends the test alert. `only` is accepted for older clients ("phone" or
// "mac"); the phone is the only voice, so "mac" sends nothing.
func (a *APNs) Test(only string) error {
	if only == "mac" {
		return errors.New("apns: the Mac does not speak; the phone is the only voice")
	}
	return a.push("fyi", "Life", "Test push from the hub", "", "")
}

// push sends one alert in its spoken slot. The phone is the voice: the push
// wakes the app (content-available) and the APP speaks the line into
// headphones (Speak.swift). `thread` is the session whose card it is ("" for
// a test or a label push): the phone marks it "speaking" through the
// `app/speech` report it posts once it has found headphones and begun
// (server/obs.go); until then a held push reads "waiting to speak", but only
// when the phone last reported headphones (ears.go). `card` is the card's id
// ("" for a test or a label push): its row in voice_queue is written here and
// stamped heard when the phone reports the sound, so a hub that stops with the
// line still queued speaks it when it comes back (queue.go).
func (a *APNs) push(kind, title, body, thread, card string) error {
	if a.deliverFn == nil {
		toks, err := a.tokens()
		if err != nil {
			return err
		}
		if len(toks) == 0 {
			return errors.New("apns: no registered devices")
		}
	}
	a.enqueue(card, thread, kind, title, body)
	// The whole spoken line goes out, up to voiceMax (the sanity bound —
	// sessions target ≤700; voice.go). The truncation is the emergency, never
	// the plan.
	if r := []rune(body); len(r) > voiceMax {
		body = string(r[:voiceMax-1]) + "…"
	}
	wait := func() func() {
		if a.Voice == nil || thread == "" || !a.hasEars() {
			return func() {}
		}
		return a.waiting(thread, card)
	}
	send := func(release func()) error {
		release()
		// Answered while it waited its turn: nothing left to say.
		if !a.cardOpen(card) {
			log.Printf("apns: %s was answered before its turn; not spoken", card)
			return nil
		}
		return a.sendPayload(apsPayload(kind, title, body, false, thread, a.badge()))
	}
	if d := a.slot(title+" "+body, time.Now()); d > 0 {
		rel := wait()
		a.after(d, func() {
			if err := a.onFloor(send, wait, rel); err != nil {
				log.Printf("apns: queued push: %v", err)
			}
		})
		return nil
	}
	return a.onFloor(send, wait, nil)
}

// apsPayload is the alert the phone gets. No "sound": a ding on the phone's
// own speaker is what a card must never be; the spoken line is the sound, and it
// goes to headphones only.
//
// When the Mac speaks (spoken) the phone shows the card at its level: needs-you
// time-sensitive, a read active. When the PHONE is the voice the push is
// PASSIVE and wakes the app (content-available), and the app says the whole
// line (Speak.swift). Passive because Siri's Announce Notifications never
// says a long one: since iOS 18 it reads a short push and, past an
// undocumented length of a sentence or two, says only "Life sent a long
// notification. Read it?" (roughly 30 words). Title, subtitle and body are one text to it; a Time
// Sensitive or communication notification changes whether it is announced,
// not how much of it (Messages themselves stop at "a sentence or two"), and a
// service extension can only rewrite the text it cuts. Siri announces
// time-sensitive, communication and — with Life set to all notifications —
// active pushes; a passive one lights no screen, plays no sound and is not
// announced (WWDC21 10091), so the app's voice is the only one and the line is
// heard once, whole, even with Announce Notifications left on; the card is
// still in the list.
//
// `thread` is the session whose card it is: the phone hands it back on its
// `app/speech` report, which is what marks the session "speaking" on the
// board (PhoneSpeakTime; server/obs.go).
//
// `badge` is the icon's number (APNs.Badge); -1 leaves the icon alone.
func apsPayload(kind, title, body string, spoken bool, thread string, badge int) []byte {
	alert := map[string]any{"body": body}
	if title != "" {
		alert["title"] = title
	}
	level := "time-sensitive"
	if kind == "read" {
		level = "active"
	}
	aps := map[string]any{"alert": alert, "interruption-level": level}
	if badge >= 0 {
		aps["badge"] = badge
	}
	if !spoken {
		aps["interruption-level"] = "passive"
		aps["content-available"] = 1
	}
	// `at`: when it was sent, so the phone can leave a push iOS delivered
	// late unspoken (Speak.swift staleAfter): a line said minutes late can
	// pull shared headphones away from another device mid-use.
	doc := map[string]any{"aps": aps, "kind": kind, "open": "sessions", "at": time.Now().Unix()}
	if thread != "" {
		doc["thread"] = thread
	}
	payload, _ := json.Marshal(doc)
	return payload
}

// badge reads the icon's number now; -1 when there is no reader or it failed.
func (a *APNs) badge() int {
	if a.Badge == nil {
		return -1
	}
	n := a.Badge()
	if n >= 0 {
		a.bmu.Lock()
		a.lastBadge = n
		a.bmu.Unlock()
	}
	return n
}

// SyncBadge keeps the app icon's number true between cards: when the nav
// total moves (the owner answered an ask in the console, a calendar item
// came due) the phone gets a badge-only push — no alert, no sound, nothing
// announced. Runs on the hub clock; a number already sent is not sent again.
func (a *APNs) SyncBadge() error {
	if a.Badge == nil {
		return nil
	}
	n := a.Badge()
	if n < 0 {
		return nil
	}
	a.bmu.Lock()
	same := n == a.lastBadge
	a.lastBadge = n
	a.bmu.Unlock()
	if same {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"aps": map[string]any{"badge": n}, "kind": "badge"})
	return a.sendPayload(payload)
}

// PhoneSpeakTime is how long the app's own synthesizer takes to say a line:
// about two and a half words a second plus a moment to start (the same
// reckoning Speak.swift sends with a replay's POST /voice).
func PhoneSpeakTime(line string) time.Duration {
	words := len(strings.Fields(line))
	return 2*time.Second + time.Duration(float64(words)/2.5*float64(time.Second))
}

// sendPayload is deliver, or the test's stand-in.
func (a *APNs) sendPayload(payload []byte) error {
	if a.deliverFn != nil {
		return a.deliverFn(payload)
	}
	return a.deliver(payload)
}

// deliver sends one alert payload to every registered device.
func (a *APNs) deliver(payload []byte) error {
	toks, err := a.tokens()
	if err != nil {
		return err
	}
	jwt, err := a.token()
	if err != nil {
		return err
	}
	var first error
	for _, d := range toks {
		if err := a.send(jwt, d, payload); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Wake sends a silent background push (content-available) to every device
// whose reported build is below `build`: iOS launches the app in the
// background, it re-registers with its CFBundleVersion, and a just-installed
// build closes its own "Install app build N" ask without the owner opening
// the app. Best effort —
// iOS throttles background pushes.
func (a *APNs) Wake(build int) error {
	toks, err := a.tokens()
	if err != nil {
		return err
	}
	jwt, err := a.token()
	if err != nil {
		return err
	}
	payload := []byte(`{"aps":{"content-available":1},"kind":"wake"}`)
	var first error
	for _, d := range toks {
		if d.build >= build {
			continue
		}
		if err := a.sendType(jwt, d, payload, "background"); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (a *APNs) send(jwt string, d device, payload []byte) error {
	return a.sendType(jwt, d, payload, "alert")
}

func (a *APNs) sendType(jwt string, d device, payload []byte, pushType string) error {
	token := d.token
	req, err := http.NewRequestWithContext(context.Background(), "POST", a.hostFor(d.env)+"/3/device/"+token, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", a.bundle)
	req.Header.Set("apns-push-type", pushType)
	priority := "10"
	if pushType == "background" {
		priority = "5" // Apple requires 5 for background pushes
	}
	req.Header.Set("apns-priority", priority)
	req.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var e struct{ Reason string }
	json.Unmarshal(b, &e)
	// Only Unregistered means the token is dead. BadDeviceToken also fires
	// on a sandbox/production mismatch — keep those.
	if e.Reason == "Unregistered" {
		a.db.Exec(`DELETE FROM devices WHERE token=?`, token)
		log.Printf("apns: dropped token %s…: %s", token[:8], e.Reason)
	}
	return fmt.Errorf("apns %d %s", resp.StatusCode, e.Reason)
}

// Notifier adapter (sched.NeedsYou / sched.ToRead). The titles are what Siri
// says first, so they are the verb: a card that waits on the owner, a card to read.
func (a *APNs) NeedsYou(text string) error { return a.Push("needs_you", "Needs you", text) }
func (a *APNs) ToRead(text string) error   { return a.Push("read", "To read", text) }
