package budget

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/sched"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

func guard(t *testing.T) (*Guard, *store.DB, *[]string) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Migrate("threads", threads.Schema)
	db.Migrate("sched", sched.Schema)
	g, err := New(db, 100, 10, 250, "claude-opus-5")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-08-26 15:00 ET = 19:00Z; the ET day began at 04:00Z.
	g.Now = func() time.Time { return time.Date(2026, 8, 26, 19, 0, 0, 0, time.UTC) }
	var notes []string
	g.Notify = func(s string) { notes = append(notes, s) }
	return g, db, &notes
}

func spend(db *store.DB, thread, ts string, usd float64) {
	db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, cost_usd) VALUES (?,?,'claude','message','x',?)`, thread, ts, usd)
}

func TestLadder(t *testing.T) {
	g, db, notes := guard(t)
	// Yesterday's spend (before 04:00Z) does not count.
	spend(db, "th-a", "2026-08-26T03:59:00Z", 500)
	if st := g.State(); st.Level != "ok" || st.SpentUSD != 0 || !st.Unattended {
		t.Fatalf("%+v", st)
	}
	spend(db, "th-a", "2026-08-26T10:00:00Z", 79)
	if st := g.State(); st.Level != "ok" || len(*notes) != 0 {
		t.Fatalf("%+v %v", st, *notes)
	}
	spend(db, "th-b", "2026-08-26T11:00:00Z", 2)
	if st := g.State(); st.Level != "warn" || !st.Unattended || len(*notes) != 1 || !strings.Contains((*notes)[0], "$81 of $100") {
		t.Fatalf("%+v %v", st, *notes)
	}
	g.State() // the warning is sent once
	if len(*notes) != 1 {
		t.Fatal(*notes)
	}
	if ok, _ := g.Allow("checkin", "th-b"); !ok {
		t.Fatal("warn still allows")
	}
	// A turn in flight and a job run count too.
	db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, out_file, live_cost_usd) VALUES ('r1','th-b','2026-08-26T12:00:00Z','checkin','',15)`)
	db.Exec(`INSERT INTO runs (job, started_at, cost_usd) VALUES ('daily-sweep','2026-08-26T12:30:00Z',5)`)
	st := g.State()
	if st.Level != "refuse" || st.Unattended || st.SpentUSD != 101 || len(*notes) != 2 {
		t.Fatalf("%+v %v", st, *notes)
	}
	if ok, why := g.Allow("job", "daily-sweep"); ok || !strings.Contains(why, "paused until midnight ET") {
		t.Fatal(ok, why)
	}
	// The owner clears it: unattended runs again for the rest of the day.
	if st := g.Clear(); st.Level != "cleared" || !st.Unattended {
		t.Fatalf("%+v", st)
	}
	if ok, _ := g.Allow("job", "daily-sweep"); !ok {
		t.Fatal("cleared should allow")
	}
	spend(db, "th-a", "2026-08-26T13:00:00Z", 20)
	if st := g.State(); st.Level != "cleared" || st.LockedAt == "" || len(*notes) != 3 {
		t.Fatalf("%+v %v", st, *notes)
	}
	// The per-thread cap is its own rule: th-b spent $17 today.
	if ok, why := g.Allow("checkin", "th-b"); ok || !strings.Contains(why, "cap $10") {
		t.Fatal(ok, why)
	}
	if ok, _ := g.Allow("checkin", "th-c"); !ok {
		t.Fatal("fresh thread allowed")
	}
	// Tomorrow starts clean.
	g.Now = func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) }
	if st := g.State(); st.Level != "ok" || st.SpentUSD != 15 { // only the live run is still counted
		t.Fatalf("%+v", st)
	}
}

// The fable day cap: the work never stops, it drops a rung.
func TestFableDayCap(t *testing.T) {
	g, db, notes := guard(t)
	g.DailyUSD = 0 // the pause ladder is off, as in ops/hub.json
	run := func(id, model, started string, seen, live float64) {
		db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, out_file, model, cost_seen, live_cost_usd) VALUES (?,'th-a',?,'message','',?,?,?)`,
			id, started, model, seen, live)
	}
	// Yesterday's fable, and today's opus, are not this day's fable spend.
	run("r0", "claude-fable-5", "2026-08-26T03:00:00Z", 400, 0)
	run("r1", "claude-opus-5", "2026-08-26T10:00:00Z", 300, 0)
	run("r2", "claude-fable-5", "2026-08-26T11:00:00Z", 200, 0)
	if f := g.Floor(); f != "" {
		t.Fatalf("under the cap: %q", f)
	}
	if st := g.State(); st.FableSpentUSD != 200 || st.FableFloor != "" || !strings.Contains(st.Note, "fable $200 of $250") {
		t.Fatalf("%+v", st)
	}
	// The turn in flight counts, so the cap bites before the run settles.
	run("r3", "claude-fable-5", "2026-08-26T12:00:00Z", 0, 60)
	if f := g.Floor(); f != "claude-opus-5" {
		t.Fatalf("over the cap: %q", f)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "Fable at $260 today (cap $250)") || !strings.Contains((*notes)[0], "run on opus") {
		t.Fatalf("%v", *notes)
	}
	g.Floor()
	g.State()
	if len(*notes) != 1 { // one FYI a day
		t.Fatalf("%v", *notes)
	}
	// Nothing is paused: unattended wakes still run, one rung down.
	if ok, _ := g.Allow("job", "daily-sweep"); !ok {
		t.Fatal("the fable cap must not stop work")
	}
	if st := g.State(); st.FableFloor != "claude-opus-5" || !strings.Contains(st.Note, "run on opus until midnight ET") {
		t.Fatalf("%+v", st)
	}
	// Tomorrow starts at fable again.
	g.Now = func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) }
	if f := g.Floor(); f != "" {
		t.Fatalf("new day: %q", f)
	}
	// Off is off.
	g.FableDailyUSD = 0
	g.Now = func() time.Time { return time.Date(2026, 8, 26, 19, 0, 0, 0, time.UTC) }
	if f := g.Floor(); f != "" {
		t.Fatalf("cap off: %q", f)
	}
	if st := g.State(); st.Note != "" {
		t.Fatalf("%+v", st)
	}
}

func TestOff(t *testing.T) {
	g, db, _ := guard(t)
	g.DailyUSD = 0
	spend(db, "th-a", "2026-08-26T10:00:00Z", 1000)
	if st := g.State(); st.Level != "off" || !st.Unattended || st.SpentUSD != 1000 {
		t.Fatalf("%+v", st)
	}
	if ok, _ := g.Allow("job", "x"); !ok {
		t.Fatal("off allows")
	}
	var nilGuard *Guard
	if ok, _ := nilGuard.Allow("job", "x"); !ok {
		t.Fatal("nil allows")
	}
}
