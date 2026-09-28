package notify

import (
	"log"
	"sync"
	"time"

	"life/hub/internal/store"
)

// Ears: a push is spoken on its own only into headphones — the phone's when
// the app finds them as its route (Speak.swift); on a speaker it is a silent
// banner. So a held push reads "waiting to speak" only when the phone has
// ears. The phone is known only by what it last reported on a push
// (server/obs.go phoneSpeech → PhoneRoute), trusted for phoneEarsTrust.
//
// A wrong "no" costs a missing pill while a line waits, never a line: the
// phone still checks its own route when the push lands and speaks if it can.

type earsSeen struct {
	on bool
	at time.Time
}

// phoneEarsTrust: how long the phone's last word on its route stands. It
// reports only when a push reaches it, so a later change goes unseen.
const phoneEarsTrust = 15 * time.Minute

// PhoneRoute is the phone's report of where a push's line went: true when it
// spoke into headphones, false when it found only the speaker or the route
// left them.
func (a *APNs) PhoneRoute(ears bool) {
	if a == nil {
		return
	}
	a.emu.Lock()
	a.phoneEars = earsSeen{ears, time.Now()}
	a.emu.Unlock()
}

// waiting marks a session "waiting to speak" for `card`. The mark ends by
// itself the moment the card closes (an answered card needs no voice), and
// the line goes unsaid when its turn comes (cardOpen). Release is idempotent.
func (a *APNs) waiting(thread, card string) func() {
	if a.Voice == nil || thread == "" {
		return func() {}
	}
	var once sync.Once
	r := a.Voice.Wait(thread, card)
	done := make(chan struct{})
	release := func() { once.Do(func() { close(done); r() }) }
	if card != "" && a.Open != nil {
		go func() {
			t := time.NewTicker(closedPoll)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if !a.cardOpen(card) {
						release()
						return
					}
				}
			}
		}()
	}
	return release
}

// closedPoll: how often a waiting line looks whether its card was answered.
const closedPoll = time.Second

// cardOpen: whether the card a line speaks for still wants the owner. A line
// with no card (a test push) always does; a hushed one never.
func (a *APNs) cardOpen(card string) bool {
	if card == "" {
		return true
	}
	a.emu.Lock()
	hushed := a.hushed[card]
	a.emu.Unlock()
	return !hushed && (a.Open == nil || a.Open(card))
}

// Hush drops a card's line and leaves the card open: dismissing the audio is
// not dismissing the card. Every wait on it reads the card as answered — the
// "waiting to speak" goes within closedPoll — and the row is stamped heard so
// no restart says it again. A card raised again later (enqueue) speaks as new.
func (a *APNs) Hush(card string) {
	if a == nil || card == "" {
		return
	}
	a.emu.Lock()
	if a.hushed == nil {
		a.hushed = map[string]bool{}
	}
	a.hushed[card] = true
	a.emu.Unlock()
	if a.db != nil {
		now := store.TS(time.Now())
		a.db.Exec(`UPDATE voice_queue SET heard_at=?, resumed_at=? WHERE card=? AND heard_at=''`, now, now, card)
	}
	log.Printf("voice: %s hushed; the card stays open", card)
}

// hasEars: whether a push now would be heard.
func (a *APNs) hasEars() bool {
	a.emu.Lock()
	phone := a.phoneEars
	a.emu.Unlock()
	return phone.on && time.Since(phone.at) < phoneEarsTrust
}
