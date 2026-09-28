// Package budget: two guards over the hub's own Claude spend, both reset at
// Eastern midnight and both counting only what the HUB launched — settled
// thread turns, turns in flight, job runs — never the owner's own terminal
// sessions.
//
//  1. The FABLE DAY CAP: a daily allowance of fable spend. Nothing stops:
//     once the day's fable spend passes the cap every new process starts at
//     opus instead, and tomorrow starts at fable again. It degrades the
//     model, never the work.
//  2. The daily points ladder (80% warn / 100% + 120% pause unattended
//     wakes). Off by default — pausing work to save budget for later in the
//     day is rarely wanted — so `daily_budget_usd` is 0 in ops/hub.json and
//     every level below is dormant. The code stays because
//     the switch is one number, and `lifectl budget clear` still lifts a
//     pause for the rest of a day if it is ever turned back on.
package budget

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"life/hub/internal/store"
)

var Schema = []string{`CREATE TABLE IF NOT EXISTS budget_days (
	day TEXT PRIMARY KEY,
	warned_at TEXT NOT NULL DEFAULT '', refused_at TEXT NOT NULL DEFAULT '',
	locked_at TEXT NOT NULL DEFAULT '', cleared_at TEXT NOT NULL DEFAULT '')`,
	// fable_at: when the day's fable cap was first crossed (one FYI a day).
	`ALTER TABLE budget_days ADD COLUMN fable_at TEXT NOT NULL DEFAULT ''`}

type Guard struct {
	db *store.DB
	// DailyUSD: 100 points. 0 = the ladder is off (State says so).
	DailyUSD float64
	// FableDailyUSD: the day's fable allowance. Past it, Floor() names the
	// rung every new process starts on instead. 0 = off.
	FableDailyUSD float64
	// FableFloor: that rung, e.g. "claude-opus-5-5" (main reads it off the
	// ladder, so the ladder stays the one place models are named).
	FableFloor string
	// ThreadDailyUSD: a scheduled thread that has spent this much today is
	// not woken again today. 0 = off.
	ThreadDailyUSD float64
	Now            func() time.Time
	Loc            *time.Location
	// Notify carries the one-per-level FYI (digest when quiet). nil = log only.
	Notify func(text string)

	mu sync.Mutex
}

// State is GET /api/v1/spend/budget.
type State struct {
	Day          string  `json:"day"`
	BudgetUSD    float64 `json:"budget_usd"`
	SpentUSD     float64 `json:"spent_usd"`
	Percent      float64 `json:"percent"`
	Level        string  `json:"level"` // off | ok | warn | refuse | locked | cleared
	Unattended   bool    `json:"unattended"`
	ThreadCapUSD float64 `json:"thread_cap_usd"`
	// The fable day cap: the allowance, what fable has earned today, and the
	// rung in force ("" = still starting at fable).
	FableCapUSD   float64 `json:"fable_cap_usd"`
	FableSpentUSD float64 `json:"fable_spent_usd"`
	FableFloor    string  `json:"fable_floor,omitempty"`
	LockedAt      string  `json:"locked_at,omitempty"`
	ClearedAt     string  `json:"cleared_at,omitempty"`
	// Note: one line for a card or a log ("$212 of $200 today — unattended
	// wakes paused until midnight ET").
	Note string `json:"note,omitempty"`
}

func New(db *store.DB, dailyUSD, threadDailyUSD, fableDailyUSD float64, fableFloor string) (*Guard, error) {
	if _, err := db.Migrate("budget", Schema); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.Local
	}
	return &Guard{db: db, DailyUSD: dailyUSD, ThreadDailyUSD: threadDailyUSD,
		FableDailyUSD: fableDailyUSD, FableFloor: fableFloor, Now: time.Now, Loc: loc}, nil
}

// day and the UTC timestamp prefix its first second sorts at (ts columns
// are RFC3339Nano UTC, so a plain string compare works).
func (g *Guard) day() (string, string) {
	now := g.Now().In(g.Loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, g.Loc)
	return now.Format("2006-01-02"), start.UTC().Format("2006-01-02T15:04:05")
}

func (g *Guard) sum(q string, args ...any) float64 {
	var v float64
	if err := g.db.QueryRow(q, args...).Scan(&v); err != nil {
		return 0
	}
	return v
}

// SpentToday: the hub's own Claude spend since Eastern midnight.
func (g *Guard) SpentToday() float64 {
	_, since := g.day()
	return g.sum(`SELECT COALESCE(SUM(cost_usd),0) FROM thread_messages WHERE role='claude' AND ts >= ?`, since) +
		g.sum(`SELECT COALESCE(SUM(live_cost_usd),0) FROM thread_runs WHERE finished_at IS NULL`) +
		g.sum(`SELECT COALESCE(SUM(cost_usd),0) FROM runs WHERE started_at >= ?`, since)
}

// FableSpentToday: what fable has earned today. Read off thread_runs, the
// only place the model that ran the work is recorded (a run carries the
// --model it was launched with, settled cost_seen plus the live estimate of
// the turn in flight). Job runs are not here — the policy already runs them
// on sonnet — and a long run counts on the day it started.
func (g *Guard) FableSpentToday() float64 {
	_, since := g.day()
	return g.sum(`SELECT COALESCE(SUM(cost_seen + live_cost_usd),0) FROM thread_runs
		WHERE started_at >= ? AND model LIKE 'claude-fable%'`, since)
}

// Floor: the rung every new process must start on for the rest of today, or
// "" for "start where the policy says". Non-empty once the day's fable spend
// passes the cap — the work still runs, one model down.
// Crossing is logged and FYI'd once a day.
func (g *Guard) Floor() string {
	if g == nil || g.FableDailyUSD <= 0 || g.FableFloor == "" {
		return ""
	}
	spent := g.FableSpentToday()
	if spent < g.FableDailyUSD {
		return ""
	}
	g.mu.Lock()
	day, _ := g.day()
	var at string
	g.db.QueryRow(`SELECT fable_at FROM budget_days WHERE day=?`, day).Scan(&at)
	first := at == ""
	if first {
		g.db.Exec(`INSERT OR IGNORE INTO budget_days (day) VALUES (?)`, day)
		g.db.Exec(`UPDATE budget_days SET fable_at=? WHERE day=?`, g.Now().UTC().Format(time.RFC3339Nano), day)
	}
	g.mu.Unlock()
	if first {
		text := fmt.Sprintf("Fable at $%.0f today (cap $%.0f) — sessions run on %s until midnight ET, then fable again",
			spent, g.FableDailyUSD, short(g.FableFloor))
		log.Printf("budget: %s", text)
		if g.Notify != nil {
			g.Notify(text)
		}
	}
	return g.FableFloor
}

// short: "claude-opus-5" → "opus", for a line read on a phone.
func short(model string) string {
	s := strings.TrimPrefix(model, "claude-")
	if i := strings.IndexAny(s, "-["); i > 0 {
		return s[:i]
	}
	return s
}

// ThreadSpentToday: one thread's share of that.
func (g *Guard) ThreadSpentToday(threadID string) float64 {
	_, since := g.day()
	return g.sum(`SELECT COALESCE(SUM(cost_usd),0) FROM thread_messages WHERE thread_id=? AND role='claude' AND ts >= ?`, threadID, since) +
		g.sum(`SELECT COALESCE(SUM(live_cost_usd),0) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, threadID)
}

// State reads the meter and records the first crossing of each level.
func (g *Guard) State() State {
	floor, fable := g.Floor(), g.FableSpentToday() // Floor takes the lock itself
	g.mu.Lock()
	defer g.mu.Unlock()
	day, _ := g.day()
	st := State{Day: day, BudgetUSD: g.DailyUSD, ThreadCapUSD: g.ThreadDailyUSD, Level: "off", Unattended: true,
		FableCapUSD: g.FableDailyUSD, FableSpentUSD: fable, FableFloor: floor}
	st.SpentUSD = g.SpentToday()
	if g.DailyUSD <= 0 {
		st.Note = fableNote(fable, g.FableDailyUSD, floor)
		return st
	}
	st.Percent = 100 * st.SpentUSD / g.DailyUSD
	var warned, refused, locked, cleared string
	g.db.QueryRow(`SELECT warned_at, refused_at, locked_at, cleared_at FROM budget_days WHERE day=?`, day).Scan(&warned, &refused, &locked, &cleared)
	now := g.Now().UTC().Format(time.RFC3339Nano)
	mark := func(col, cur, text string) string {
		if cur != "" {
			return cur
		}
		g.db.Exec(`INSERT OR IGNORE INTO budget_days (day) VALUES (?)`, day)
		g.db.Exec(`UPDATE budget_days SET `+col+`=? WHERE day=?`, now, day)
		log.Printf("budget: %s", text)
		if g.Notify != nil {
			g.Notify(text)
		}
		return now
	}
	money := fmt.Sprintf("$%.0f of $%.0f today", st.SpentUSD, g.DailyUSD)
	switch {
	case st.Percent >= 120:
		locked = mark("locked_at", locked, "Budget locked: "+money+" (120%) — unattended wakes paused until midnight ET; `lifectl budget clear` to resume")
		st.Level, st.Unattended = "locked", false
	case st.Percent >= 100:
		refused = mark("refused_at", refused, "Budget spent: "+money+" — unattended wakes paused until midnight ET; `lifectl budget clear` to resume")
		st.Level, st.Unattended = "refuse", false
	case st.Percent >= 80:
		warned = mark("warned_at", warned, "Budget warning: "+money+" (80%) — unattended wakes pause at 100%")
		st.Level = "warn"
	default:
		st.Level = "ok"
	}
	_ = warned
	_ = refused
	st.LockedAt = locked
	if cleared != "" {
		st.ClearedAt = cleared
		if !st.Unattended {
			st.Level, st.Unattended = "cleared", true
		}
	}
	switch st.Level {
	case "refuse", "locked":
		st.Note = money + " — unattended wakes paused until midnight ET"
	case "cleared":
		st.Note = money + " — paused, then cleared by you for today"
	case "warn":
		st.Note = money + " — unattended wakes pause at $" + fmt.Sprintf("%.0f", g.DailyUSD)
	default:
		st.Note = money
	}
	if n := fableNote(fable, g.FableDailyUSD, floor); n != "" {
		st.Note += " · " + n
	}
	return st
}

// fableNote: one line for a card, a log or `lifectl budget`.
func fableNote(spent, cap float64, floor string) string {
	if cap <= 0 {
		return ""
	}
	s := fmt.Sprintf("fable $%.0f of $%.0f today", spent, cap)
	if floor != "" {
		return s + " — sessions run on " + short(floor) + " until midnight ET"
	}
	return s
}

// Clear lifts today's pause (the owner's call, `lifectl budget clear`).
func (g *Guard) Clear() State {
	g.mu.Lock()
	day, _ := g.day()
	now := g.Now().UTC().Format(time.RFC3339Nano)
	g.db.Exec(`INSERT OR IGNORE INTO budget_days (day) VALUES (?)`, day)
	g.db.Exec(`UPDATE budget_days SET cleared_at=? WHERE day=?`, now, day)
	g.mu.Unlock()
	log.Printf("budget: cleared for %s by the owner", day)
	return g.State()
}

// Allow answers "may this unattended wake run now?" for a check-in
// (kind=checkin, id=thread), a job (kind=job, id=name) or a calendar agent
// item (kind=cal, id=item). The reason is for the log/digest when not.
func (g *Guard) Allow(kind, id string) (bool, string) {
	if g == nil {
		return true, ""
	}
	if st := g.State(); !st.Unattended {
		return false, st.Note
	}
	if kind == "checkin" && g.ThreadDailyUSD > 0 {
		if spent := g.ThreadSpentToday(id); spent >= g.ThreadDailyUSD {
			return false, fmt.Sprintf("thread spent $%.2f today (cap $%.0f) — next wake tomorrow", spent, g.ThreadDailyUSD)
		}
	}
	return true, ""
}
