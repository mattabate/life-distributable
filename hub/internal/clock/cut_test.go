package clock

import (
	"sync/atomic"
	"testing"
	"time"
)

// A syncer whose run a restart cut (a start recorded, no end) is due on the
// first tick after boot, not an interval later. A housekeeping task waits as
// before.
func TestACutSyncerRunsAgainAtBoot(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 20, 20, 3, 0, time.UTC)
	started, ended := map[string]time.Time{}, map[string]time.Time{}
	boot := func(at time.Time, fn func() error) *Clock {
		c := New()
		c.Now = fixed(at)
		c.Last = func(name string) time.Time { return started[name] }
		c.Ran = func(name string, when time.Time) { started[name] = when }
		c.Ended = func(name string, start time.Time) { ended[name] = start }
		c.Cut = func(name string) bool { return !ended[name].Equal(started[name]) }
		c.Sync("feed", 6*time.Hour, false, fn)
		c.Every("sweep", 6*time.Hour, false, fn)
		return c
	}
	// Both started at 20:20:03 and the process ended under them.
	started["feed"], started["sweep"] = t0, t0
	var n atomic.Int32
	c := boot(t0.Add(20*time.Second), func() error { n.Add(1); return nil })
	if c.TickAt(t0.Add(20*time.Second)) != 1 {
		t.Fatal("the cut syncer, and only it, should start on the first tick")
	}
	c.Wait()
	// It finished this time: the next boot waits out the interval.
	c = boot(t0.Add(time.Hour), func() error { n.Add(1); return nil })
	if c.TickAt(t0.Add(time.Hour)) != 0 || n.Load() != 1 {
		t.Fatalf("a finished run ran again: runs=%d", n.Load())
	}
}
