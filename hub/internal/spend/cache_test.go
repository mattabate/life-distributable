package spend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const extra = `{"type":"assistant","requestId":"r3","sessionId":"s1","cwd":"/Users/owner/life","timestamp":"2026-08-20T11:00:00Z","message":{"id":"m3","model":"claude-fable-5","usage":{"input_tokens":10,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`

// A refresh must cost a stat walk plus the files that changed — never the
// whole 600 MB tree again (the 2.5–5.6 s Spend page, 2026-08-28).
func TestCacheReparsesOnlyWhatChanged(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(a), 0o755)
	os.WriteFile(a, []byte(sample), 0o644)
	c := NewCache(dir)

	us, err := c.Usages()
	if err != nil || len(us) != 2 || c.parses != 1 {
		t.Fatalf("first parse: %d usages, %d parses, %v", len(us), c.parses, err)
	}
	// Nothing changed: the walk parses nothing.
	if us, _ = c.refresh(); len(us) != 2 || c.parses != 1 {
		t.Fatalf("unchanged tree reparsed: %d parses", c.parses)
	}
	// An appended transcript (size changes) is parsed again, and only it.
	b := filepath.Join(dir, "y", "b.jsonl")
	os.MkdirAll(filepath.Dir(b), 0o755)
	os.WriteFile(b, []byte(sample), 0o644)
	if us, _ = c.refresh(); len(us) != 4 || c.parses != 2 {
		t.Fatalf("new file: %d usages, %d parses", len(us), c.parses)
	}
	f, _ := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(extra)
	f.Close()
	if us, _ = c.refresh(); len(us) != 5 || c.parses != 3 {
		t.Fatalf("appended file: %d usages, %d parses", len(us), c.parses)
	}
	// Sorted by time across files, oldest first.
	for i := 1; i < len(us); i++ {
		if us[i].TS.Before(us[i-1].TS) {
			t.Fatalf("not sorted at %d: %v < %v", i, us[i].TS, us[i-1].TS)
		}
	}
	// A deleted transcript (the nightly purge) drops out without a parse.
	os.Remove(b)
	if us, _ = c.refresh(); len(us) != 3 || c.parses != 3 {
		t.Fatalf("removed file: %d usages, %d parses", len(us), c.parses)
	}
}

// Past MaxAge the caller gets the snapshot it has NOW; the walk happens behind
// it. Only the very first call waits.
func TestCacheServesStaleWhileRefreshing(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(a), 0o755)
	os.WriteFile(a, []byte(sample), 0o644)
	c := &Cache{Root: dir, MaxAge: time.Nanosecond}
	if us, _ := c.Usages(); len(us) != 2 {
		t.Fatalf("first: %d", len(us))
	}
	f, _ := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(extra)
	f.Close()
	// Stale answer now…
	if us, _ := c.Usages(); len(us) != 2 {
		t.Fatalf("stale call blocked or refreshed inline: %d", len(us))
	}
	// …fresh one shortly after.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if us, _ := c.Usages(); len(us) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh never landed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A restart must not walk the tree again (2026-09-25: 2.1 GB, 5–34 s cold,
// 31 restarts in a night, and + New session waited on it). With Store set the
// parsed tree is written out and read back: the first call after a start is
// served from the file and parses nothing; what changed since is caught up
// behind it.
func TestCacheStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(a), 0o755)
	os.WriteFile(a, []byte(sample), 0o644)
	store := filepath.Join(t.TempDir(), "cache", "spend.gob")

	c1 := &Cache{Root: dir, Store: store}
	if us, err := c1.Usages(); err != nil || len(us) != 2 || c1.parses != 1 {
		t.Fatalf("first parse: %d usages, %d parses, %v", len(us), c1.parses, err)
	}
	waitFor(t, "the store to be written", func() bool { _, err := os.Stat(store); return err == nil })

	// The hub restarts: a fresh Cache over the same file parses nothing.
	c2 := &Cache{Root: dir, Store: store, MaxAge: time.Nanosecond}
	if us, err := c2.Usages(); err != nil || len(us) != 2 || c2.parses != 0 {
		t.Fatalf("after restart: %d usages, %d parses, %v", len(us), c2.parses, err)
	}
	// A transcript that grew while the hub was down lands behind that call,
	// and only it is parsed.
	f, _ := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(extra)
	f.Close()
	waitFor(t, "the changed file to be caught up", func() bool { us, _ := c2.Usages(); return len(us) == 3 })
	if c2.parses != 1 {
		t.Fatalf("catch-up parsed %d files, want 1", c2.parses)
	}

	// A store that is not ours (another shape, a truncated write) is ignored
	// and the walk starts from nothing.
	os.WriteFile(store, []byte("not a gob"), 0o644)
	c3 := &Cache{Root: dir, Store: store}
	if us, err := c3.Usages(); err != nil || len(us) != 3 || c3.parses != 1 {
		t.Fatalf("bad store: %d usages, %d parses, %v", len(us), c3.parses, err)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 5 s for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An empty tree is never cached: the transcript written right after the hub
// starts shows on the next request, not in 30 s.
func TestCacheEmptyTreeIsNotASnapshot(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if us, err := c.Usages(); err != nil || len(us) != 0 {
		t.Fatalf("empty: %d %v", len(us), err)
	}
	a := filepath.Join(dir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(a), 0o755)
	os.WriteFile(a, []byte(sample), 0o644)
	if us, _ := c.Usages(); len(us) != 2 {
		t.Fatalf("file written after an empty parse is invisible: %d", len(us))
	}
}
