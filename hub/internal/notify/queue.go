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
		ON CONFLICT(card) DO UPDATE SET thread=excluded.thread, kind=excluded.kind, title=excluded.title, body=excluded.body, queued_at=excluded.queued_at, heard_at='', resumed_at=''`,
		card, thread, kind, title, body, now); err != nil {
		log.Printf("voice: queue %s: %v", card, err)
	}
	// Rows past a week are dropped here as well as at Resume, so a hub that
	// stays up for weeks does not grow the table (#232).
	a.db.Exec(`DELETE FROM voice_queue WHERE queued_at < ?`, store.TS(time.Now().Add(-7*24*time.Hour)))
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

// LineCard: the card the phone's line speaks for — the session's unheard row
// with those words first, else its newest unheard row (read before Heard
// stamps it).
func (a *APNs) LineCard(thread, line string) string {
	if a == nil || a.db == nil || thread == "" {
		return ""
	}
	var card string
	if a.db.QueryRow(`SELECT card FROM voice_queue WHERE thread=? AND body=? AND heard_at='' ORDER BY queued_at DESC LIMIT 1`, thread, line).Scan(&card) == nil {
		return card
	}
	a.db.QueryRow(`SELECT card FROM voice_queue WHERE thread=? AND heard_at='' ORDER BY queued_at DESC LIMIT 1`, thread).Scan(&card)
	return card
}

// Resume speaks again what the last hub left unheard: rows queued within
// resumeWindow, never heard, never resumed, whose card is still open, oldest
// first — each through the ordinary push path, so they queue behind one
// another, wait for the floor and for the owner's microphone as any push does. Rows
// older than a week are dropped. Called once by main after the voice is wired.
// Opens the boot gate (Booting) once every resumed line is handed out.
func (a *APNs) Resume() {
	a.bootMu.Lock()
	a.resuming = true
	a.bootMu.Unlock()
	defer a.openBoot()
	if a.db == nil {
		return
	}
	now := time.Now()
	a.db.Exec(`DELETE FROM voice_queue WHERE queued_at < ?`, store.TS(now.Add(-7*24*time.Hour)))
	rows, err := a.db.Query(`SELECT card, thread, kind, title, body, queued_at FROM voice_queue WHERE heard_at='' AND resumed_at='' AND queued_at >= ? ORDER BY queued_at`, store.TS(now.Add(-resumeWindow)))
	if err != nil {
		log.Printf("voice: resume: %v", err)
		return
	}
	type row struct{ card, thread, kind, title, body, queued string }
	var todo []row
	for rows.Next() {
		var r row
		rows.Scan(&r.card, &r.thread, &r.kind, &r.title, &r.body, &r.queued)
		todo = append(todo, r)
	}
	rows.Close()
	a.resumeMacLine(now)
	if len(todo) > 0 {
		a.awaitEar()
	}
	n := 0
	// resumed_at is stamped after the push: enqueue clears it, so a card
	// raised again after one resume is resumed again if a restart cuts it.
	// And only when the push went out: a failed one (a transient APNs error,
	// no device registered yet) said nothing, so the next hub tries again
	// (#110). push re-queues the row as new; its window runs from the first
	// queueing, so a line that never goes out is let go after resumeWindow
	// rather than retried on every restart.
	for _, r := range todo {
		if a.Open != nil && !a.Open(r.card) {
			a.db.Exec(`UPDATE voice_queue SET resumed_at=? WHERE card=?`, store.TS(now), r.card)
			log.Printf("voice: %s was queued unheard but is closed; not re-spoken", r.card)
			continue
		}
		// Read again just before its push: during the ear wait (or the line
		// before it) the row may have been heard, or raised again by enqueue
		// (a new queued_at), and that push says it; this one would say it twice.
		var still int
		if a.db.QueryRow(`SELECT 1 FROM voice_queue WHERE card=? AND heard_at='' AND resumed_at='' AND queued_at=?`, r.card, r.queued).Scan(&still) != nil {
			continue
		}
		n++
		log.Printf("voice: %s was queued unheard when the hub stopped; speaking it again", r.card)
		err := a.pushAs(true, r.kind, r.title, r.body, r.thread, r.card)
		a.db.Exec(`UPDATE voice_queue SET queued_at=? WHERE card=?`, r.queued, r.card)
		if err != nil {
			log.Printf("voice: resume %s: %v; the next hub tries again", r.card, err)
			continue
		}
		a.db.Exec(`UPDATE voice_queue SET resumed_at=? WHERE card=?`, store.TS(now), r.card)
	}
	if n > 0 {
		log.Printf("voice: %d card(s) left unheard by the last hub, re-spoken", n)
	}
}

// awaitEar: at startup a Mac's ear has not polled yet, so a resumed line
// would skip a Mac with headphones in (and, with no push key, go nowhere).
// Resume waits for the first poll, or earTrust at most (ear.boot in tests;
// below zero = no wait).
func (a *APNs) awaitEar() {
	a.ear.mu.Lock()
	wait := a.ear.boot
	a.ear.mu.Unlock()
	if wait < 0 {
		return
	}
	if wait == 0 {
		wait = earTrust
	}
	end := time.Now().Add(wait)
	for !a.earAlive() && time.Now().Before(end) {
		time.Sleep(50 * time.Millisecond)
	}
	// One poll seen: one more poll interval, so the other ear on the hub's
	// Mac (the helper or the app) is seen too, never past `end`.
	if a.earAlive() {
		if gap := min(earPollGap, time.Until(end)); gap > 0 {
			time.Sleep(gap)
		}
	}
}

// earPollGap: about one ear poll interval (ops/ear.swift and MacEar poll
// every 2 s; the second ear is usually seen within one).
const earPollGap = time.Second

// bootSafety bounds the boot gate: if Resume never starts (startup stopped
// short of it), new pushes go on after this. Once Resume has begun, its own
// deferred openBoot opens the gate on every return, and the timer does
// nothing: firing mid-Resume would let held pushes ahead of resumed rows.
const bootSafety = earTrust + 30*time.Second

// Booting closes the boot gate: until Resume ends (or bootSafety, if Resume
// never starts), each new push waits whole (pushAs), in arrival order. main
// calls it before `go Resume()`; without it nothing waits.
func (a *APNs) Booting() {
	a.bootMu.Lock()
	a.booting = true
	a.bootMu.Unlock()
	time.AfterFunc(bootSafety, a.bootTimeout)
}

// bootTimeout: the safety timer's end, a no-op once Resume has begun.
func (a *APNs) bootTimeout() {
	a.bootMu.Lock()
	started := a.resuming
	a.bootMu.Unlock()
	if !started {
		a.openBoot()
	}
}

// holdForBoot queues `job` while the gate is closed (true = held).
func (a *APNs) holdForBoot(job func()) bool {
	a.bootMu.Lock()
	defer a.bootMu.Unlock()
	if !a.booting {
		return false
	}
	a.bootJobs = append(a.bootJobs, job)
	return true
}

// openBoot runs the held choices in arrival order, then opens the gate. The
// gate stays closed while they run, so a push arriving meanwhile goes after
// them; a second call (the safety timer) waits for the first and finds it open.
func (a *APNs) openBoot() {
	a.bootRun.Lock()
	defer a.bootRun.Unlock()
	for {
		a.bootMu.Lock()
		jobs := a.bootJobs
		a.bootJobs = nil
		if len(jobs) == 0 {
			a.booting = false
			a.bootMu.Unlock()
			return
		}
		a.bootMu.Unlock()
		for _, j := range jobs {
			j()
		}
	}
}

// earBegunKey (a settings row) names the card whose line a Mac began and
// has not ended: one line is on a Mac at a time (macLine), so one row.
const earBegunKey = "voice:mac-begun"

// macBegun records (card) or clears ("") the line a Mac is saying.
func (a *APNs) macBegun(card string) {
	if a.db != nil {
		a.db.SetSetting(earBegunKey, card)
	}
}

// resumeMacLine: the last hub stopped while a Mac was saying a card's line.
// It is heard (from its first word), so it is not said again, but the phone
// never got its banner: send that now, if the line began within
// macBegunWindow and the card is still open. No phone: nothing.
func (a *APNs) resumeMacLine(now time.Time) {
	card := a.db.Setting(earBegunKey)
	if card == "" {
		return
	}
	a.macBegun("")
	var kind, title, body, thread string
	err := a.db.QueryRow(`SELECT kind, title, body, thread FROM voice_queue WHERE card=? AND heard_at >= ?`,
		card, store.TS(now.Add(-macBegunWindow))).Scan(&kind, &title, &body, &thread)
	if err != nil || !a.Phone() || a.Open != nil && !a.Open(card) {
		return
	}
	log.Printf("voice: the last hub stopped mid-line on a Mac (%s); the phone gets its banner", card)
	if err := a.sendCard(apsPayload(kind, title, body, true, thread, a.badge()), card); err != nil {
		log.Printf("voice: banner for %s: %v", card, err)
	}
}

// macBegunWindow: how recent a line cut by a restart must be for its banner.
const macBegunWindow = 5 * time.Minute
