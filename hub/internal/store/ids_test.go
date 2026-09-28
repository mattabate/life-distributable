package store

import (
	"testing"
	"time"
)

func TestNewIDAndTS(t *testing.T) {
	a, b := NewID("ask"), NewID("ask")
	if len(a) != 12 || a[:4] != "ask-" || a == b {
		t.Fatalf("%s %s", a, b)
	}
	// Fixed width: string order is time order inside a second.
	t1 := time.Date(2026, 8, 26, 5, 0, 0, 100, time.UTC)
	t2 := t1.Add(50 * time.Nanosecond)
	if TS(t1) >= TS(t2) || len(TS(t1)) != len("2026-08-26T05:00:00.000000100Z") {
		t.Fatalf("%s %s", TS(t1), TS(t2))
	}
	if TS(time.Time{}) != "" {
		t.Fatal("zero time must be empty")
	}
	// Day counts in Eastern: 23:30 UTC on the 26th is still the 26th in NY,
	// and 01:00 UTC on the 27th is too.
	if Day(time.Date(2026, 8, 26, 23, 30, 0, 0, time.UTC)) != "2026-08-26" {
		t.Fatal(Day(time.Date(2026, 8, 26, 23, 30, 0, 0, time.UTC)))
	}
	if Day(time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)) != "2026-08-26" {
		t.Fatal("Day is not Eastern")
	}
}

// Every id shape the hub has ever minted resolves to its kind: the 2-byte
// ids from before Phase 0, today's 4-byte ones, thread slugs (with either
// suffix), the stamped proposal/run form, and the prompt queue's p-.
func TestKindOfAndRef(t *testing.T) {
	cases := map[string]string{
		"ask-b755":                         "ask",
		"ask-1a2b3c4d":                     "ask",
		"cal-b755":                         "cal",
		"cal-1a2b3c4d":                     "cal",
		"rec-1a2b3c4d":                     "rec",
		"p-0a1b2c3d":                       "prompt",
		"per-0a1b2c3d":                     "person", // people/build.go personID
		"20260828-150405-0a1b2c3d":         "action",
		"20260828-150405-0a1b":             "action",
		"build-sections-2-and-3-of-x-6bae": "thread",
		"hub":                              "thread",
		"thread-1a2b3c4d":                  "thread",
		NewID("a-slug-with-dashes"):        "thread",
	}
	for id, want := range cases {
		if got := KindOf(id); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", id, got, want)
		}
	}
	if Ref("rec-1a2b3c4d") != "rec:rec-1a2b3c4d" {
		t.Fatal(Ref("rec-1a2b3c4d"))
	}
	if k, id := SplitRef("action:20260828-150405-0a1b2c3d"); k != "action" || id != "20260828-150405-0a1b2c3d" {
		t.Fatal(k, id)
	}
	if k, id := SplitRef("no-colon"); k != "" || id != "" {
		t.Fatal(k, id)
	}
	// The stamped form is NewID with the stamp as prefix — same text as the
	// three copies of it the packages used to carry.
	if !stampedRe.MatchString(NewID("20260828-150405")) {
		t.Fatal(NewID("20260828-150405"))
	}
}
