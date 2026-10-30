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
		// Below the one-minute tick: last+30s is past at every tick, so it
		// would wake its target once a minute while reading "every 30s".
		"every@30s": "", "every@59s": "", "every@1ns": "", "every@1m": "clock", "every@1m30s": "clock",
	}
	for in, want := range cases {
		if got := Kind(in); got != want {
			t.Errorf("Kind(%q) = %q, want %q", in, got, want)
		}
	}
}

// A clock time is the whole field: "07:00pm" must not read as 07:00, and
// "19:00:30" or "9:00 PT" must not pass.
func TestClockTimeIsTheWholeField(t *testing.T) {
	for _, in := range []string{"daily@07:00pm", "daily@7:00am", "daily@19:00:30", "daily@19:00 ", "daily@ 19:00", "daily@+7:00",
		"daily@19:0x", "daily@1900", "daily@:30", "daily@19:", "daily@123:00", "daily@19:000", "weekly@Mon 09:00pm", "weekly@Mon +9:00"} {
		if got := Kind(in); got != "" {
			t.Errorf("Kind(%q) = %q, want refused", in, got)
		}
	}
	for _, in := range []string{"daily@07:30", "daily@7:30", "daily@00:00", "daily@23:59", "weekly@Wed 09:30", "weekly@mon 9:05"} {
		if got := Kind(in); got != "clock" {
			t.Errorf("Kind(%q) = %q, want clock", in, got)
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

// Clock times across a DST change fire once a day: a missing local time is
// skipped, a repeated one fires at its first occurrence only.
func TestNextClockAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	// 2026-11-01: 01:30 happens twice (EDT, then EST an hour later).
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).In(loc) // 01:30 EDT
	if n := Next("daily@01:30", time.Time{}, first.Add(-time.Minute)); !n.Equal(first) {
		t.Fatalf("fall back: due %v, want the first 01:30 %v", n, first)
	}
	if n := Next("daily@01:30", first, first); n.Day() != 2 {
		t.Fatalf("fall back: after the first 01:30 the next is %v, want Nov 2", n)
	}
	// 2027-03-14: 02:30 never happens; the day still gets one run.
	before := time.Date(2027, 3, 14, 1, 0, 0, 0, loc)
	n := Next("daily@02:30", time.Time{}, before)
	if n.Day() != 14 {
		t.Fatalf("spring forward: next %v, want one run on Mar 14", n)
	}
	if m := Next("daily@02:30", n, n); m.Day() != 15 {
		t.Fatalf("spring forward: after %v the next is %v, want Mar 15", n, m)
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
	if s := Step("monthly", d); s.Month() != 2 || s.Day() != 28 || s.Hour() != 8 {
		t.Error("monthly from Jan 31 is Feb 28, not Go's Mar 3", s)
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

// A monthly chain on the 29th-31st never skips a month and comes back to its
// day after a short one; a yearly one on Feb 29 is Feb 28 in common years.
func TestMonthlyChainKeepsItsDay(t *testing.T) {
	loc := time.Local
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 9, 0, 0, 0, loc) }
	cur, want := day(2027, 1, 31), []string{"2027-02-28", "2027-03-31", "2027-04-30", "2027-05-31"}
	for _, w := range want {
		cur = StepOn("monthly", cur, 31)
		if got := cur.Format("2006-01-02"); got != w {
			t.Fatalf("monthly on the 31st: got %s, want %s", got, w)
		}
	}
	if s := Step("monthly", day(2027, 1, 29)); s.Format("2006-01-02") != "2027-02-28" {
		t.Error("Jan 29 + 1 month", s)
	}
	if s := StepOn("monthly", day(2027, 2, 28), 29); s.Format("2006-01-02") != "2027-03-29" {
		t.Error("a 29th chain returns to the 29th after February", s)
	}
	if s := Step("yearly", day(2028, 2, 29)); s.Format("2006-01-02") != "2029-02-28" {
		t.Error("yearly from Feb 29", s)
	}
	if s := StepOn("yearly", day(2031, 2, 28), 29); s.Format("2006-01-02") != "2032-02-29" {
		t.Error("yearly Feb 29 chain in a leap year", s)
	}
	if s := StepOn("weekly", day(2027, 1, 31), 31); s.Format("2006-01-02") != "2027-02-07" {
		t.Error("dom does not touch day strides", s)
	}
	if n := Next("monthly", day(2027, 1, 31), day(2027, 3, 1)); n.Format("2006-01-02") != "2027-03-31" {
		t.Error("Next keeps last's day across February", n)
	}
	if !MonthEnd(day(2027, 2, 28)) || MonthEnd(day(2028, 2, 28)) || !MonthEnd(day(2027, 4, 30)) {
		t.Error("MonthEnd")
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
