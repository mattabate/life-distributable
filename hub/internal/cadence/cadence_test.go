package cadence

import (
	"testing"
	"time"
)

func TestKinds(t *testing.T) {
	cases := map[string]string{
		"": "never", "manual": "never",
		"daily@07:30": "clock", "weekly@Mon 09:00": "clock", "every@2h": "clock",
		"daily": "stride", "weekly": "stride", "monthly": "stride", "yearly": "stride", "every2d": "stride", "every365d": "stride",
		"daily@25:00": "", "weekly@Funday 09:00": "", "every@": "", "every@-1h": "", "every0d": "", "every366d": "", "hourly": "", "every2h": "",
	}
	for in, want := range cases {
		if got := Kind(in); got != want {
			t.Errorf("Kind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextClock(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 8, 20, 7, 29, 30, 0, loc) // Thu
	if n := Next("daily@07:30", time.Time{}, now); !n.Equal(time.Date(2026, 8, 20, 7, 30, 0, 0, loc)) {
		t.Error("daily today", n)
	}
	if n := Next("daily@07:00", time.Time{}, now); !n.Equal(time.Date(2026, 8, 21, 7, 0, 0, 0, loc)) {
		t.Error("daily tomorrow", n)
	}
	if n := Next("weekly@Thu 07:30", time.Time{}, now); n.Weekday() != time.Thursday || n.Day() != 20 {
		t.Error("weekly this week", n)
	}
	if n := Next("weekly@Wed 07:30", time.Time{}, now); n.Weekday() != time.Wednesday || n.Day() != 26 {
		t.Error("weekly next week", n)
	}
	if n := Next("every@6h", time.Time{}, now); !n.Equal(now) {
		t.Error("every with no last is due now", n)
	}
	if n := Next("every@6h", now.Add(-2*time.Hour), now); !n.Equal(now.Add(4 * time.Hour)) {
		t.Error("every from last", n)
	}
	if n := Next("manual", time.Time{}, now); !n.IsZero() {
		t.Error("manual never", n)
	}
	if n := Next("nonsense", time.Time{}, now); !n.IsZero() {
		t.Error("invalid never", n)
	}
	// the occurrence is strictly after now, so a row that just fired at
	// 07:30 advances to tomorrow, not to itself
	at := time.Date(2026, 8, 20, 7, 30, 0, 0, loc)
	if n := Next("daily@07:30", at, at); !n.Equal(at.AddDate(0, 0, 1)) {
		t.Error("advance past itself", n)
	}
}

func TestStep(t *testing.T) {
	loc := time.Local
	d := time.Date(2026, 1, 31, 8, 0, 0, 0, loc)
	if s := Step("daily", d); s.Day() != 1 || s.Month() != 2 {
		t.Error("daily", s)
	}
	if s := Step("weekly", d); s.Day() != 7 || s.Month() != 2 {
		t.Error("weekly", s)
	}
	if s := Step("monthly", d); s.Month() != 3 || s.Day() != 3 {
		t.Error("monthly (Go normalises Jan 31 + 1mo to Mar 3)", s)
	}
	if s := Step("yearly", d); s.Year() != 2027 {
		t.Error("yearly", s)
	}
	if s := Step("every2d", d); s.Day() != 2 || s.Month() != 2 {
		t.Error("every2d", s)
	}
	if s := Step("daily@08:00", d); !s.IsZero() {
		t.Error("clock cadences have no stride", s)
	}
}

func TestNextStrideSkipsBacklog(t *testing.T) {
	loc := time.Local
	anchor := time.Date(2026, 8, 1, 8, 0, 0, 0, loc)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, loc)
	if n := Next("every2d", anchor, now); !n.Equal(time.Date(2026, 8, 13, 8, 0, 0, 0, loc)) {
		t.Error("one next date, not five", n)
	}
	if n := Next("daily", time.Time{}, now); !n.IsZero() {
		t.Error("a stride with no anchor cannot advance", n)
	}
}

func TestHuman(t *testing.T) {
	for in, want := range map[string]string{
		"": "not scheduled", "daily@08:30": "daily at 08:30", "weekly@Sun 17:00": "weekly on Sun 17:00",
		"every@2h": "every 2h", "every2d": "every 2 days", "every1d": "daily", "weekly": "weekly",
	} {
		if got := Human(in); got != want {
			t.Errorf("Human(%q) = %q, want %q", in, got, want)
		}
	}
}
