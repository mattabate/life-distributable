package threads

import (
	"sort"
	"sync"
	"time"
)

// Voice: which sessions are being HEARD right now. A card is spoken the moment
// it is raised, so the Sessions list must say which session the voice belongs
// to. notify marks the session when its push goes out (apns.push, in the slot
// it is spoken), for as long as the line takes to say. The board reads it into
// a "speaking" pill on both surfaces, and Version folds it in so the list
// repaints the second it starts or ends. In memory only: a restart loses at
// most a word that was already fading.
type Voice struct {
	mu    sync.Mutex
	until map[string]time.Time
	// held: sessions whose push is queued for its turn — behind a replay, a
	// pitch round or another card — counted per push.
	held map[string]int
	// cards: the same holds by card, so the card itself can say so — its Play
	// button reads "Waiting to speak" and a double tap drops the line.
	cards map[string]int
}

// Wait says `thread` has a line queued for `card` that is not being heard
// yet: the row reads "waiting to speak" until the returned release runs, so
// the owner can see what is queued before it starts speaking. Release is
// idempotent.
func (v *Voice) Wait(thread, card string) (release func()) {
	if v == nil || thread == "" {
		return func() {}
	}
	v.mu.Lock()
	if v.held == nil {
		v.held, v.cards = map[string]int{}, map[string]int{}
	}
	v.held[thread]++
	if card != "" {
		v.cards[card]++
	}
	v.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if v.held[thread]--; v.held[thread] <= 0 {
				delete(v.held, thread)
			}
			if card == "" {
				return
			}
			if v.cards[card]--; v.cards[card] <= 0 {
				delete(v.cards, card)
			}
		})
	}
}

// WaitingCards: the cards with a line queued at the moment, sorted.
func (v *Voice) WaitingCards() []string {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for id := range v.cards {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// CardWaiting: whether one card's line is queued.
func (v *Voice) CardWaiting(card string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cards[card] > 0
}

// Waiting: the sessions with a line queued at the moment, sorted.
func (v *Voice) Waiting() []string {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for id := range v.held {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// IsWaiting: whether one session has a line queued.
func (v *Voice) IsWaiting(thread string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.held[thread] > 0
}

// Mark says `thread` is heard until `until`; a time already past ends it.
func (v *Voice) Mark(thread string, until time.Time) {
	if v == nil || thread == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.until == nil {
		v.until = map[string]time.Time{}
	}
	if !until.After(time.Now()) {
		delete(v.until, thread)
		return
	}
	v.until[thread] = until
}

// Live: the sessions still being heard at `now`, sorted (a stable version
// input). Expired ones are dropped as they are passed.
func (v *Voice) Live(now time.Time) []string {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for id, t := range v.until {
		if t.After(now) {
			out = append(out, id)
		} else {
			delete(v.until, id)
		}
	}
	sort.Strings(out)
	return out
}

// Is: whether one session is being heard at `now`.
func (v *Voice) Is(thread string, now time.Time) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	t, ok := v.until[thread]
	return ok && t.After(now)
}

// Busy: anything is being heard at `now` — a pushed card or a card the owner
// is replaying (POST /voice). One voice at a time, never over audio from the
// app: notify holds every push while this is true.
func (v *Voice) Busy(now time.Time) bool {
	return len(v.Live(now)) > 0
}

// Replay is the key for a card replayed outside any session (a rec): it holds
// the floor without marking a row.
const Replay = "~replay"
