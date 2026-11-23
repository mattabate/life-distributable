package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Mac's ear: a card's line is said on a Mac as well as the phone. Each Mac
// that speaks runs an ear, which polls `GET /voice/ear` every 2 s with its
// output route (headphones, speakers or none) and the seconds since its last
// keyboard or mouse input: the voice helper (ops/ear.swift → ops/bin/ear,
// launchd com.life.ear, the hub's Mac) or the desktop app while it is open
// (MacEar.swift, any other Mac). When a line is due the hub picks ONE Mac and
// only that Mac's polls carry it; the ear says it and reports `playing`,
// `spoke` or `silent` on `POST /voice/ear`. A line no Mac claims within
// earClaim, or one it reports silent, goes to the phone as before.
//
// Who says it (earPick, apns.go push):
//  1. a Mac with its Audio switch on and headphones in;
//  2. the phone, when its last word was headphones (ears.go);
//  3. a Mac with Audio on, on its own speakers, while the owner is at it
//     (input within hereIdle): a hub Mac nobody sits at stays quiet;
//  4. else the phone's push, as with no Mac at all.
// A Mac's Audio switch (AudioKey, Configuration's Devices card) off = that Mac
// says nothing; its tile's Stop cuts the line it is saying (StopLine).

// EarLine is one line handed to a Mac's ear.
type EarLine struct {
	ID     string `json:"id"`
	Card   string `json:"card,omitempty"`
	Thread string `json:"thread_id,omitempty"`
	Text   string `json:"text"`
	At     string `json:"at"`
	// Device: the Mac whose ear was handed the line; its polls alone show it.
	Device string `json:"-"`
	// Rung: why that Mac was picked, "headphones" or "speakers". While it
	// holds the line its ears are that rung's: AirPods that drop mid-line
	// (macOS falls back to the speakers) stop the line, never play it aloud.
	Rung string `json:"-"`
}

// earOK: whether a Mac's ear, as last seen, may go on with a line picked on
// `rung` ("" = no line held: headphones, or speakers). A headphones line
// needs them still in; a speakers line any output (the owner being at the
// Mac is checked when it is picked, not mid-line). Its Audio switch is apart.
func earOK(rung string, s earSeen) bool {
	if rung == "headphones" {
		return s.route == "headphones"
	}
	return s.route == "headphones" || s.route == "speakers"
}

// EarView is GET/POST /voice/ear's answer.
type EarView struct {
	// Ears: this Mac may say a line now (its route and its Audio switch).
	Ears bool `json:"ears"`
	// Line: the line waiting for this Mac's ear, if any.
	Line *EarLine `json:"line,omitempty"`
	// Voice: the system voice to say it in (`say -v`).
	Voice string `json:"voice,omitempty"`
}

// SpokenVoice is the macOS voice a Mac's ear says lines in.
const SpokenVoice = "Samantha"

const (
	// earTrust: how long an ear's last poll stands as its word on its route
	// (it polls every 2 s; five missed polls = it is gone).
	earTrust = 10 * time.Second
	// earClaim: how long a handed line waits for the ear's first report
	// before the phone takes it.
	earClaim = 15 * time.Second
	// earSayCap: how long past its reckoned length a line the ear says is
	// playing may run before it is called unsaid.
	earSayCap = 30 * time.Second
	// hereIdle: how long since the last input a Mac still counts as where
	// the owner is.
	hereIdle = 5 * time.Minute
	// earDefaultDevice names the hub's Mac when its host name gives none.
	earDefaultDevice = "mac"
	// earDeviceMax: the longest device name an ear may send.
	earDeviceMax = 40
	// phoneDevice is the phone's name on the Devices card.
	phoneDevice = "phone"
)

// earNoteKey is the settings row that remembers a Mac's ear ever polled
// (`<route> <RFC3339 of the poll>`), so a Mac asleep keeps its tile.
const earNoteKey = "voice:ear:"

// earNoteEvery: how often a polling ear's row is refreshed.
const earNoteEvery = time.Minute

// AudioKey is the settings row of a Mac's Audio switch: "off" = it says
// nothing; no row = on.
func AudioKey(device string) string { return "voice:audio:" + device }

// earSeen is one Mac's last poll.
type earSeen struct {
	route  string // headphones | speakers | none
	output string // the output's name as the ear read it ("" = not said)
	at     time.Time
	// idle: seconds since the last input on that Mac; <0 = not said.
	idle  float64
	noted time.Time // when its earNoteKey row was last written
}

// here: the owner is at that Mac — its ear polled within earTrust and the
// Mac saw input within hereIdle.
func (s earSeen) here(now time.Time) bool {
	return now.Sub(s.at) < earTrust && s.idle >= 0 && time.Duration(s.idle*float64(time.Second)) < hereIdle
}

// ear is the APNs side of the lane.
type ear struct {
	mu   sync.Mutex
	seen map[string]earSeen // each Mac's last poll, by device
	// helper: the last poll from each Mac's voice helper. While one polls,
	// that Mac's desktop app is not its ear, so the two never both say a line.
	helper map[string]time.Time
	line   *EarLine
	report chan string
	stop   context.CancelFunc // cuts the line being said (StopLine)
	claim  time.Duration      // earClaim; tests shorten it
	said   time.Duration      // a begun line's wait for its end (speakTime+earSayCap); tests shorten it
	boot   time.Duration      // Resume's wait for a first poll (awaitEar); 0 = earTrust, <0 = none
}

// deviceName: a Mac as the ears tell them apart — its host name's first
// label, lower case.
func deviceName(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	if i := strings.IndexByte(d, '.'); i >= 0 {
		d = d[:i]
	}
	return d
}

// EarDevice: a Mac's name as the Devices card and the Audio switch key it
// ("MacBook-Air.local" → "macbook-air"). ok false: empty, "phone" (the
// phone's tile), longer than earDeviceMax, or not only a-z, 0-9 and "-".
func EarDevice(d string) (string, bool) {
	d = deviceName(d)
	if d == "" || d == "phone" || len(d) > earDeviceMax {
		return "", false
	}
	for _, c := range d {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "", false
		}
	}
	return d, true
}

// hostDevice: this hub's own Mac, as its ear names itself.
func hostDevice() string {
	h, _ := os.Hostname()
	if h = deviceName(h); h == "" {
		h = earDefaultDevice
	}
	return h
}

// AudioOn: whether a Mac's Audio switch is on (the default).
func (a *APNs) AudioOn(device string) bool {
	if a == nil || a.db == nil {
		return true
	}
	return a.db.Setting(AudioKey(deviceName(device))) != "off"
}

// SetAudio turns a Mac's Audio switch on or off. Off while that Mac says a
// line cuts it.
func (a *APNs) SetAudio(device string, on bool) error {
	device, ok := EarDevice(device)
	if !ok {
		return fmt.Errorf("not a Mac's device name")
	}
	if a == nil || a.db == nil {
		return nil
	}
	v := ""
	if !on {
		v = "off"
	}
	if err := a.db.SetSetting(AudioKey(device), v); err != nil {
		return err
	}
	if !on {
		a.ear.mu.Lock()
		cut := ""
		if a.ear.line != nil && a.ear.line.Device == device {
			cut = a.ear.line.ID
		}
		a.ear.mu.Unlock()
		a.StopLine(cut)
	}
	return nil
}

// EarPoll is one ear's poll: `device` names its Mac, `route` is its output
// now, `output` the output's name, `idle` the seconds since its last input
// (<0: not said), `helper` whether the voice helper sent it (else the desktop
// app). The answer says whether this Mac may say a line now and carries the
// line waiting for it, if any.
func (a *APNs) EarPoll(device, route, output string, idle float64, helper bool) EarView {
	var ok bool
	if device, ok = EarDevice(device); a == nil || !ok {
		return EarView{}
	}
	switch route {
	case "headphones", "speakers":
	default:
		route = "none"
	}
	audio := a.AudioOn(device)
	on := route != "none" && audio
	now := time.Now()
	a.ear.mu.Lock()
	if a.ear.seen == nil {
		a.ear.seen = map[string]earSeen{}
		a.ear.helper = map[string]time.Time{}
	}
	if helper {
		a.ear.helper[device] = now
	} else if now.Sub(a.ear.helper[device]) < earTrust {
		a.ear.mu.Unlock()
		return EarView{Ears: on}
	}
	prev := a.ear.seen[device]
	s := earSeen{route: route, output: strings.TrimSpace(output), at: now, idle: idle, noted: prev.noted}
	note := a.db != nil && (prev.route != route || now.Sub(prev.noted) >= earNoteEvery)
	if note {
		s.noted = now
	}
	a.ear.seen[device] = s
	v := a.earView(device, audio, s)
	a.ear.mu.Unlock()
	if note {
		a.db.SetSetting(earNoteKey+device, route+" "+now.UTC().Format(time.RFC3339))
	}
	return v
}

// EarReport is an ear's word on the line it was handed: `playing` (the first
// word is out), `spoke` (said to its end), `silent` (no ears, or they left
// mid-line). An id that is not the pending line's, or a report from a Mac the
// line was not handed to, changes nothing.
func (a *APNs) EarReport(device, id, outcome string) EarView {
	var ok bool
	if device, ok = EarDevice(device); a == nil || !ok {
		return EarView{}
	}
	a.ear.mu.Lock()
	defer a.ear.mu.Unlock()
	if a.ear.line != nil && a.ear.line.ID == id && a.ear.line.Device == device && a.ear.report != nil {
		select {
		case a.ear.report <- outcome:
		default:
		}
	}
	s := a.ear.seen[device]
	return a.earView(device, time.Since(s.at) < earTrust && a.AudioOn(device), s)
}

// earView: the answer for one Mac's ear, under ear.mu. Only the Mac a line
// was handed to sees it; the others poll on with none (a line is one voice).
// Ears: its Audio switch is on and its route may say a line — for the Mac
// holding one, a line of the rung it was picked on (earOK).
func (a *APNs) earView(device string, audio bool, s earSeen) EarView {
	rung := ""
	var line *EarLine
	if a.ear.line != nil && a.ear.line.Device == device {
		l := *a.ear.line
		line, rung = &l, l.Rung
	}
	return EarView{Ears: audio && earOK(rung, s), Line: line, Voice: SpokenVoice}
}

// StopLine cuts line `id` if a Mac is saying it now (the Devices card's Stop
// names the line its tile showed). Its card stays open and the line is not
// said again (Hush). False = no such line: it ended, and the next card's line
// is never the one stopped.
func (a *APNs) StopLine(id string) bool {
	if a == nil || id == "" {
		return false
	}
	a.ear.mu.Lock()
	l, stop := a.ear.line, a.ear.stop
	a.ear.mu.Unlock()
	if l == nil || l.ID != id {
		return false
	}
	if l.Card != "" {
		a.Hush(l.Card)
	}
	if stop != nil {
		stop()
	}
	log.Printf("ear: the line on %s was stopped (%s)", l.Device, l.ID)
	return true
}

// earPick: the Mac the next line goes to. headphones = only a Mac with
// headphones in; otherwise a Mac on its speakers while the owner is at it.
// Each with its Audio switch on and a poll within earTrust; the latest poll
// between equals. ok false = no such Mac now.
func (a *APNs) earPick(headphones bool) (device string, ok bool) {
	now := time.Now()
	a.ear.mu.Lock()
	type cand struct {
		d string
		s earSeen
	}
	var cs []cand
	for d, s := range a.ear.seen {
		if now.Sub(s.at) >= earTrust {
			continue
		}
		if headphones && s.route == "headphones" || !headphones && s.route == "speakers" && s.here(now) {
			cs = append(cs, cand{d, s})
		}
	}
	a.ear.mu.Unlock()
	var best earSeen
	for _, c := range cs {
		if !a.AudioOn(c.d) {
			continue
		}
		if !ok || c.s.at.After(best.at) {
			device, best, ok = c.d, c.s, true
		}
	}
	return device, ok
}

// earVoice: the Mac a card's line goes to now — one with headphones in, else
// (unless the phone's last word was headphones) one on its speakers while
// the owner is at it. ok false = the phone's.
func (a *APNs) earVoice() (device, rung string, ok bool) {
	if device, ok = a.earPick(true); ok {
		return device, "headphones", true
	}
	if a.hasPhoneEars() {
		return "", "", false
	}
	device, ok = a.earPick(false)
	return device, "speakers", ok
}

// macEnqueue queues a card's line for the Mac: one drain says them in
// arrival order, each under macLine (a mutex alone wakes waiters in any
// order).
func (a *APNs) macEnqueue(job func()) {
	a.macMu.Lock()
	a.macJobs = append(a.macJobs, job)
	start := !a.macRun
	a.macRun = true
	a.macMu.Unlock()
	if start {
		go a.macDrain()
	}
}

func (a *APNs) macDrain() {
	for {
		a.macMu.Lock()
		if len(a.macJobs) == 0 {
			a.macRun = false
			a.macMu.Unlock()
			return
		}
		job := a.macJobs[0]
		a.macJobs = a.macJobs[1:]
		a.macMu.Unlock()
		a.macLine.Lock()
		job()
		a.macLine.Unlock()
	}
}

// macBusy: a card's line is being said on a Mac or waits its turn.
func (a *APNs) macBusy() bool {
	a.macMu.Lock()
	defer a.macMu.Unlock()
	return a.macRun
}

// macEars: some Mac would say a line now (either rung of earPick).
func (a *APNs) macEars() bool {
	if _, ok := a.earPick(true); ok {
		return true
	}
	_, ok := a.earPick(false)
	return ok
}

// earAlive: some Mac's ear polled within earTrust (a line may go to a Mac
// even with no phone registered).
func (a *APNs) earAlive() bool {
	now := time.Now()
	a.ear.mu.Lock()
	defer a.ear.mu.Unlock()
	for _, s := range a.ear.seen {
		if now.Sub(s.at) < earTrust {
			return true
		}
	}
	return false
}

// What became of a line handed to a Mac (earSay).
const (
	earSpoke  = "spoke"  // said to its end
	earUnsaid = "unsaid" // no ears, never claimed, or ran past its cap: the phone takes it
	earCut    = "cut"    // withdrawn: the card answered or hushed, the Stop, the Audio switch
)

// earSay hands one line to `device`'s ear, picked on `rung`, and follows it:
// heard(true) when the ear reports playing; earSpoke when it reports spoke,
// or when a line it began runs past its cap with no word (its spoke report
// was lost: the line was heard, so the phone must not say it again);
// earUnsaid when it reports silent or nobody claims it within earClaim;
// earCut when ctx ends or StopLine cuts it. Either way the line is then
// withdrawn and the ear's next poll finds none, which stops its voice.
func (a *APNs) earSay(ctx context.Context, card, thread, device, rung, text string, heard func(playing bool)) string {
	var b [8]byte
	rand.Read(b[:])
	id := "ear-" + hex.EncodeToString(b[:])
	ch := make(chan string, 8)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.ear.mu.Lock()
	a.ear.line = &EarLine{ID: id, Card: card, Thread: thread, Text: text, At: time.Now().UTC().Format(time.RFC3339), Device: device, Rung: rung}
	a.ear.report, a.ear.stop = ch, cancel
	claim, said := a.ear.claim, a.ear.said
	a.ear.mu.Unlock()
	if claim == 0 {
		claim = earClaim
	}
	if said == 0 {
		said = speakTime(text) + earSayCap
	}
	defer func() {
		a.ear.mu.Lock()
		if a.ear.line != nil && a.ear.line.ID == id {
			a.ear.line, a.ear.report, a.ear.stop = nil, nil, nil
		}
		a.ear.mu.Unlock()
	}()
	log.Printf("ear: handing %d chars to %s (%s)", len(text), device, id)
	deadline := time.NewTimer(claim)
	defer deadline.Stop()
	playing := false
	for {
		select {
		case <-ctx.Done():
			log.Printf("ear: %s withdrawn from %s", id, device)
			return earCut
		case o := <-ch:
			switch o {
			case "playing":
				if !playing {
					playing = true
					heard(true)
				}
				deadline.Reset(said)
			case "spoke":
				log.Printf("ear: %s said the line (%s)", device, id)
				return earSpoke
			case "silent":
				log.Printf("ear: %s found no ears for the line (%s)", device, id)
				return earUnsaid
			}
		case <-deadline.C:
			if playing {
				log.Printf("ear: %s began the line and sent no end (%s); taken as said", device, id)
				return earSpoke
			}
			log.Printf("ear: %s never began the line (%s); unsaid", device, id)
			return earUnsaid
		}
	}
}

// sayOnMac says a card's line on a Mac (push's send, one line at a time) and
// then sends the phone its push: a plain banner when the Mac said it, the
// line for the phone to say when the Mac left it unsaid, nothing when it was
// cut (answered, hushed, stopped). The Mac is picked once the line before
// has ended: if none qualifies by then, the phone takes the line at once.
// `release` ends the "waiting to speak" the line arrived with; the session
// reads "waiting to speak" until the first word and "speaking" while it plays.
// The card is heard at the first word, so a hub restarted mid-line does not
// say it again (Resume); a Mac that then reports silent gives it back.
// Called by macDrain, holding macLine.
func (a *APNs) sayOnMac(kind, title, body, thread, card string, release func()) {
	if !a.cardOpen(card) {
		release()
		return
	}
	device, rung, ok := a.earVoice()
	if !ok {
		release()
		if a.Phone() {
			if err := a.sendCard(apsPayload(kind, title, body, false, thread, a.badge()), card); err != nil {
				log.Printf("apns: push when no Mac could say it: %v", err)
			}
		}
		return
	}
	hold := release
	if a.Voice != nil && thread != "" {
		w := a.waiting(thread, card)
		hold = func() { w(); release() }
	}
	text := body
	if title != "" {
		text = title + ". " + body
	}
	// The card answered or hushed mid-line stops the Mac's voice.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		t := time.NewTicker(closedPoll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !a.cardOpen(card) {
					cancel()
					return
				}
			}
		}
	}()
	var mu sync.Mutex
	played := false
	got := a.earSay(ctx, card, thread, device, rung, text, func(bool) {
		a.heardCard(card)
		if card != "" {
			a.macBegun(card) // a restart from here on owes the phone its banner
		}
		mu.Lock()
		defer mu.Unlock()
		played = true
		if a.Voice != nil && thread != "" {
			a.markCard(thread, card, time.Now().Add(speakTime(text)+earSayCap))
		}
		if hold != nil {
			hold()
			hold = nil
		}
	})
	mu.Lock()
	if hold != nil {
		hold()
		hold = nil
	}
	if a.Voice != nil && thread != "" {
		a.markCard(thread, "", time.Now())
	}
	began := played
	mu.Unlock()
	if began {
		a.macBegun("")
	}
	// A begun line is heard, so the phone gets a banner. The exception is a
	// line whose headphones left mid-line: its rest was never heard, so the
	// phone says it.
	if got == earUnsaid && began && rung != "headphones" {
		got = earSpoke
	}
	switch {
	case got == earSpoke:
		a.heardCard(card)
	case got == earCut || !a.cardOpen(card):
		return
	case began && card != "" && a.db != nil:
		// The headphones left mid-line: the phone says it again.
		a.db.Exec(`UPDATE voice_queue SET heard_at='' WHERE card=?`, card)
	}
	if !a.Phone() {
		return
	}
	if err := a.sendCard(apsPayload(kind, title, body, got == earSpoke, thread, a.badge()), card); err != nil {
		log.Printf("apns: push after the Mac's line: %v", err)
	}
}

// MacEars is one device as Configuration's Devices card lists it: a Mac with
// its Audio switch and what its ear reports now, or the phone.
type MacEars struct {
	Device string `json:"device"`
	// Kind: "mac" or "phone".
	Kind string `json:"kind"`
	// Hub: the hub's own Mac.
	Hub bool `json:"hub,omitempty"`
	// Audio: a Mac's Audio switch (AudioKey).
	Audio bool `json:"audio"`
	// Ears: the output now — headphones, speakers, none — or "" when its ear
	// has not polled lately (not running, or the Mac asleep; the phone with no
	// push lately).
	Ears string `json:"ears"`
	// Output: the output's name behind Ears, while the poll is fresh.
	Output string `json:"output,omitempty"`
	// Here: the owner is at this Mac (input within hereIdle).
	Here bool `json:"here,omitempty"`
	// Saying: this Mac's ear holds a line now (the tile's Stop).
	Saying bool `json:"saying,omitempty"`
	// Line: the id of the line it is saying, which its Stop names.
	Line string `json:"line,omitempty"`
	// At: the last poll (a Mac) or route report (the phone), RFC3339.
	At string `json:"at,omitempty"`
}

// Macs lists the devices: every Mac whose ear ever polled (the earNoteKey
// rows) plus this hub's own, the hub's first then by name, and the phone last
// when one is registered. Never nil.
func (a *APNs) Macs() []MacEars {
	out := []MacEars{}
	if a == nil {
		return out
	}
	seen := map[string]MacEars{}
	if a.db != nil {
		rows, err := a.db.Query(`SELECT key, value FROM settings WHERE key LIKE ?`, earNoteKey+"%")
		if err == nil {
			for rows.Next() {
				var k, v string
				if rows.Scan(&k, &v) != nil {
					continue
				}
				d := strings.TrimPrefix(k, earNoteKey)
				m := MacEars{Device: d}
				if _, at, ok := strings.Cut(v, " "); ok {
					m.At = at
				}
				seen[d] = m
			}
			rows.Close()
		}
	}
	now := time.Now()
	a.ear.mu.Lock()
	for d, s := range a.ear.seen {
		m := seen[d]
		m.Device = d
		m.At = s.at.UTC().Format(time.RFC3339)
		if now.Sub(s.at) < earTrust {
			m.Ears, m.Output = s.route, s.output
		}
		m.Here = s.here(now)
		seen[d] = m
	}
	saying, line := "", ""
	if a.ear.line != nil {
		saying, line = a.ear.line.Device, a.ear.line.ID
	}
	a.ear.mu.Unlock()
	host := a.host
	if host == "" {
		host = hostDevice()
	}
	m := seen[host]
	m.Device, m.Hub = host, true
	seen[host] = m
	for d, m := range seen {
		m.Kind = "mac"
		m.Audio = a.AudioOn(d)
		if m.Saying = d == saying; m.Saying {
			m.Line = line
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hub != out[j].Hub {
			return out[i].Hub
		}
		return out[i].Device < out[j].Device
	})
	if p, ok := a.phoneRow(); ok {
		out = append(out, p)
	}
	return out
}

// phoneRow: the phone as the Devices card lists it — its last word on its
// route (PhoneRoute) while fresh.
func (a *APNs) phoneRow() (MacEars, bool) {
	if a.db == nil {
		return MacEars{}, false
	}
	var at string
	if a.db.QueryRow(`SELECT updated_at FROM devices ORDER BY updated_at DESC LIMIT 1`).Scan(&at) != nil {
		return MacEars{}, false
	}
	m := MacEars{Device: phoneDevice, Kind: "phone", Audio: true, At: at}
	a.emu.Lock()
	if !a.phoneEars.at.IsZero() && time.Since(a.phoneEars.at) < phoneEarsTrust {
		m.Ears = "speakers"
		if a.phoneEars.on {
			m.Ears = "headphones"
		}
		m.At = a.phoneEars.at.UTC().Format(time.RFC3339)
	}
	a.emu.Unlock()
	return m, true
}
