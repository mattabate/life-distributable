package clock

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

// A restart must not push a 6h sync out another 6h: the hub restarted every
// hour or two on 09-13 and SimpleFin went 17 h without a run.
func TestRestartKeepsTheLastRunsSchedule(t *testing.T) {
	t0 := time.Date(2026, 9, 13, 8, 17, 0, 0, time.UTC)
	stamps := map[string]time.Time{}
	boot := func(at time.Time) (*Clock, *atomic.Int32) {
		c := New()
		c.Now = fixed(at)
		c.Last = func(name string) time.Time { return stamps[name] }
		c.Ran = func(name string, when time.Time) { stamps[name] = when }
		var n atomic.Int32
		c.Every("finance", 6*time.Hour, false, func() error { n.Add(1); return nil })
		return c, &n
	}
	c, n := boot(t0)
	c.TickAt(t0.Add(6 * time.Hour)) // first run 14:17
	c.Wait()
	if n.Load() != 1 || !stamps["finance"].Equal(t0.Add(6*time.Hour)) {
		t.Fatalf("runs=%d stamp=%v", n.Load(), stamps["finance"])
	}
	// restart at 18:00, due again at 20:17 — not at midnight
	c, n = boot(t0.Add(9*time.Hour + 43*time.Minute))
	if c.TickAt(t0.Add(11*time.Hour)) != 0 {
		t.Fatal("ran before its interval was up")
	}
	c.TickAt(t0.Add(12 * time.Hour))
	c.Wait()
	if n.Load() != 1 {
		t.Fatalf("restart pushed the run out: runs=%d", n.Load())
	}
	// a stamp far in the past is due on the first tick after boot
	stamps["finance"] = t0.Add(-48 * time.Hour)
	c, n = boot(t0)
	if c.TickAt(t0) != 1 {
		t.Fatal("overdue task did not run on the first tick")
	}
	c.Wait()
}

func TestAtBootRunsOnTheFirstTickAndNotAgainUntilDue(t *testing.T) {
	t0 := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	c := New()
	c.Now = fixed(t0)
	var boot, later atomic.Int32
	c.Every("quota", 10*time.Minute, true, func() error { boot.Add(1); return nil })
	c.Every("prompts", time.Minute, false, func() error { later.Add(1); return nil })

	if n := c.TickAt(t0); n != 1 {
		t.Fatalf("first tick started %d, want 1 (the at-boot task only)", n)
	}
	c.Wait()
	if boot.Load() != 1 || later.Load() != 0 {
		t.Fatalf("boot=%d later=%d after the first tick", boot.Load(), later.Load())
	}
	// One minute on: prompts is due, quota is not.
	c.Now = fixed(t0.Add(time.Minute))
	if n := c.TickAt(t0.Add(time.Minute)); n != 1 {
		t.Fatalf("second tick started %d, want 1", n)
	}
	c.Wait()
	if later.Load() != 1 || boot.Load() != 1 {
		t.Fatalf("boot=%d later=%d after the second tick", boot.Load(), later.Load())
	}
	// A 1-minute task on a 1-minute ticker fires on EVERY tick, even when the
	// tick lands a few ms late (the next due time comes from the tick, not
	// from when the run finished).
	c.TickAt(t0.Add(2*time.Minute + 5*time.Millisecond))
	c.Wait()
	if later.Load() != 2 {
		t.Fatalf("prompts ran %d times over three ticks, want 2", later.Load())
	}
	st := c.Status()
	if len(st) != 2 || st[0].Name != "quota" || st[1].Name != "prompts" {
		t.Fatalf("status order: %+v", st)
	}
	if st[0].Every != "10m" || st[1].Every != "1m" {
		t.Fatalf("every: %s %s", st[0].Every, st[1].Every)
	}
	if st[0].Runs != 1 || st[1].Runs != 2 || st[0].LastStart == nil || st[0].LastEnd == nil {
		t.Fatalf("runs/stamps: %+v", st)
	}
	if !st[0].NextDue.Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("quota next due %s, want %s", st[0].NextDue, t0.Add(10*time.Minute))
	}
}

func TestATaskNeverOverlapsItself(t *testing.T) {
	t0 := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	c := New()
	c.Now = fixed(t0)
	release := make(chan struct{})
	var runs atomic.Int32
	c.Every("slow", time.Minute, true, func() error { runs.Add(1); <-release; return nil })
	c.TickAt(t0)
	// Still running on the next two turns: neither starts a second copy.
	for i := 1; i <= 2; i++ {
		if n := c.TickAt(t0.Add(time.Duration(i) * time.Minute)); n != 0 {
			t.Fatalf("tick %d started %d while the task was running", i, n)
		}
	}
	if st := c.Status(); !st[0].Running || st[0].Runs != 1 {
		t.Fatalf("status while running: %+v", st[0])
	}
	close(release)
	c.Wait()
	if st := c.Status(); st[0].Running || st[0].LastEnd == nil {
		t.Fatalf("status after: %+v", st[0])
	}
	// Overdue once it finishes: the next tick starts it.
	if n := c.TickAt(t0.Add(3 * time.Minute)); n != 1 {
		t.Fatalf("overdue tick started %d, want 1", n)
	}
	c.Wait()
	if runs.Load() != 2 {
		t.Fatalf("runs=%d, want 2", runs.Load())
	}
}

func TestAnErrorIsKeptOnTheRowAndClearedByTheNextSuccess(t *testing.T) {
	t0 := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	c := New()
	c.Now = fixed(t0)
	var fail atomic.Bool
	fail.Store(true)
	c.Every("finance", 6*time.Hour, true, func() error {
		if fail.Load() {
			return errors.New("simplefin: 502")
		}
		return nil
	})
	c.TickAt(t0)
	c.Wait()
	st := c.Status()[0]
	if st.LastError != "simplefin: 502" || st.Errors != 1 || st.Runs != 1 {
		t.Fatalf("after a failed run: %+v", st)
	}
	fail.Store(false)
	c.TickAt(t0.Add(6 * time.Hour))
	c.Wait()
	st = c.Status()[0]
	if st.LastError != "" || st.Errors != 1 || st.Runs != 2 || st.Every != "6h" {
		t.Fatalf("after a good run: %+v", st)
	}
}

// Only syncers report their runs, and a skipped run (no credential) is not a
// sync: Sources would otherwise call an unconfigured connector healthy.
func TestSyncReportsRunsToDone(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	c := New()
	c.Now = fixed(t0)
	var mu sync.Mutex
	got := map[string]string{}
	c.Done = func(name string, start, end time.Time, err error) {
		mu.Lock()
		defer mu.Unlock()
		got[name] = "ok"
		if err != nil {
			got[name] = err.Error()
		}
	}
	c.Every("calendar", time.Minute, true, func() error { return nil })
	c.Sync("mail", time.Hour, true, func() error { return errors.New("401") })
	c.Sync("device", time.Hour, true, func() error { return ErrSkip })
	c.Sync("gcal", time.Hour, true, func() error { return nil })
	c.TickAt(t0)
	c.Wait()
	if len(got) != 2 || got["mail"] != "401" || got["gcal"] != "ok" {
		t.Fatalf("reported %v", got)
	}
	for _, tk := range c.Status() {
		if tk.Name == "device" && (tk.LastError != "" || tk.Errors != 0) {
			t.Fatalf("a skip is not an error: %+v", tk)
		}
		if tk.Syncer != (tk.Name != "calendar") {
			t.Fatalf("syncer flag: %+v", tk)
		}
	}
}
