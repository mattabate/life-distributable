package notify

import (
	"log"
	"time"

	"life/hub/internal/store"
)

// The spoken queue is durable in one respect: what has been queued and not
// yet heard. Everything that makes a push wait — its slot behind the last
// line, the floor (a replay, a pitch round), the owner's microphone, the Mac
// helper's render — lives in memory and in child processes, and `ops/hub.sh
// restart` (launchd kickstart -k) ends all of it at once. Cards raised while
// the owner dictated, just before a restart, lost their "waiting to speak"
// pill with the old process and were never said. A dropped push is not
// harmless because the card is the record — the card is, the hearing is not.
//
// So every card's push writes a voice_queue row (store.Schema) as it is
// queued, the row is stamped heard when the sound itself is reported — the
// Mac helper's "playing" (speak.go), the phone's `spoke` report (server/obs.go
// → Heard) — and Resume, at startup, speaks again every row of the last
// resumeWindow that was never heard and whose card is still open. Once: a card
// the phone keeps rightly silent (nothing on the owner's ears) is not re-pushed on
// every restart after that.
const resumeWindow = 30 * time.Minute

// Open reports whether a card (an ask, a proposal or a nagging calendar item
// id — whatever went through Card) still wants the owner; nil = every queued card
// does. Set by main from threads, actions and calendar.
type Open func(card string) bool

func (a *APNs) enqueue(card, thread, kind, title, body string) {
	if card == "" {
		return
	}
	a.emu.Lock()
	delete(a.hushed, card) // raised again: a new line
	a.emu.Unlock()
	if a.db == nil {
		return
	}
	now := store.TS(time.Now())
	if _, err := a.db.Exec(`INSERT INTO voice_queue (card, thread, kind, title, body, queued_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(card) DO UPDATE SET thread=excluded.thread, kind=excluded.kind, title=excluded.title, body=excluded.body, queued_at=excluded.queued_at, heard_at=''`,
		card, thread, kind, title, body, now); err != nil {
		log.Printf("voice: queue %s: %v", card, err)
	}
}

// heardCard: the Mac's helper reported the card's line playing.
func (a *APNs) heardCard(card string) {
	if a.db == nil || card == "" {
		return
	}
	a.db.Exec(`UPDATE voice_queue SET heard_at=? WHERE card=? AND heard_at=''`, store.TS(time.Now()), card)
}

// Heard is the phone's word (Speak.swift `spoke`, server/obs.go): the line it
// began saying for that session. The row with those words is stamped; when
// none matches word for word (the app trims what it says), the session's
// newest unheard row is — one push, one line.
func (a *APNs) Heard(thread, line string) {
	if a.db == nil || thread == "" {
		return
	}
	now := store.TS(time.Now())
	res, err := a.db.Exec(`UPDATE voice_queue SET heard_at=? WHERE thread=? AND body=? AND heard_at=''`, now, thread, line)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return
	}
	a.db.Exec(`UPDATE voice_queue SET heard_at=? WHERE card=(SELECT card FROM voice_queue WHERE thread=? AND heard_at='' ORDER BY queued_at DESC LIMIT 1)`, now, thread)
}

// Resume speaks again what the last hub left unheard: rows queued within
// resumeWindow, never heard, never resumed, whose card is still open, oldest
// first — each through the ordinary push path, so they queue behind one
// another, wait for the floor and for the owner's microphone as any push does. Rows
// older than a week are dropped. Called once by main after the voice is wired.
func (a *APNs) Resume() {
	if a.db == nil {
		return
	}
	now := time.Now()
	a.db.Exec(`DELETE FROM voice_queue WHERE queued_at < ?`, store.TS(now.Add(-7*24*time.Hour)))
	rows, err := a.db.Query(`SELECT card, thread, kind, title, body FROM voice_queue WHERE heard_at='' AND resumed_at='' AND queued_at >= ? ORDER BY queued_at`, store.TS(now.Add(-resumeWindow)))
	if err != nil {
		log.Printf("voice: resume: %v", err)
		return
	}
	type row struct{ card, thread, kind, title, body string }
	var todo []row
	for rows.Next() {
		var r row
		rows.Scan(&r.card, &r.thread, &r.kind, &r.title, &r.body)
		todo = append(todo, r)
	}
	rows.Close()
	n := 0
	for _, r := range todo {
		a.db.Exec(`UPDATE voice_queue SET resumed_at=? WHERE card=?`, store.TS(now), r.card)
		if a.Open != nil && !a.Open(r.card) {
			log.Printf("voice: %s was queued unheard but is closed; not re-spoken", r.card)
			continue
		}
		n++
		log.Printf("voice: %s was queued unheard when the hub stopped; speaking it again", r.card)
		if err := a.push(r.kind, r.title, r.body, r.thread, r.card); err != nil {
			log.Printf("voice: resume %s: %v", r.card, err)
		}
	}
	if n > 0 {
		log.Printf("voice: %d card(s) left unheard by the last hub, re-spoken", n)
	}
}
