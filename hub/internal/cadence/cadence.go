// Package cadence is the one grammar for "when does this recur" in the hub
// (docs/reviews/2026-08-28-one-clock.md). It is a leaf: only `time`, so
// sched, threads and calendar can all sit above it.
//
// Two shapes, because the hub already spoke both and neither is wrong:
//
//   - clock kinds, the schedule.json / thread-schedule grammar:
//     `daily@HH:MM`, `weekly@Mon HH:MM`, `every@<duration>`; `manual` or
//     `""` means never.
//   - stride kinds, the calendar repeat grammar, stepped from an anchor day:
//     `daily`, `weekly`, `monthly`, `yearly`, `every<N>d` (1..365).
//
// Times use the location of the `now` (or anchor) passed in, as every caller
// did before — the hub runs in Eastern.
package cadence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinEvery is the shortest `every@` the hub can honour: standing rows fire
// from a one-minute tick. A shorter stride would be accepted and shown as
// written ("every 30s"), but last+30s is already past at the next tick, so
// the target would wake on every tick, one billed run a minute.
const MinEvery = time.Minute

// Kind classifies a cadence: "never", "clock" or "stride". Invalid → "".
func Kind(s string) string {
	if s == "" || s == "manual" {
		return "never"
	}
	if _, _, err := parseClock(s); err == nil {
		return "clock"
	}
	if stride(s) {
		return "stride"
	}
	return ""
}

// Valid is true for every cadence Next can advance, including "never".
func Valid(s string) bool { return Kind(s) != "" }

// Parse validates a clock cadence and returns its error for a form field.
func Parse(s string) error {
	if Kind(s) == "" {
		return fmt.Errorf("bad cadence %q", s)
	}
	return nil
}

// Next is the first occurrence strictly after now. `last` is the previous
// occurrence (zero when there was none): an `every@` cadence is last+d (or
// now itself when there is no last — it has never run, so it is due), and a
// stride cadence steps from last until it is past now. Zero time for
// "never" and for anything that does not parse: a caller that gets zero
// must not wedge on it.
func Next(cad string, last, now time.Time) time.Time {
	loc := now.Location()
	if kind, arg, err := parseClock(cad); err == nil {
		switch kind {
		case "never":
			return time.Time{}
		case "daily", "weekly":
			wd := -1
			hm := arg
			if kind == "weekly" {
				f := strings.Fields(arg)
				wd = weekday(f[0])
				hm = f[1]
			}
			h, m, _ := hhmm(hm)
			y, mo, d := now.In(loc).Date()
			for i := 0; i < 8; i++ {
				t := time.Date(y, mo, d+i, h, m, 0, 0, loc)
				if t.After(now) && (wd < 0 || int(t.Weekday()) == wd) {
					return t
				}
			}
		case "every":
			d, _ := time.ParseDuration(arg)
			if last.IsZero() {
				return now
			}
			return last.Add(d)
		}
		return time.Time{}
	}
	if !stride(cad) || last.IsZero() {
		return time.Time{}
	}
	t := Step(cad, last)
	for i := 0; !t.IsZero() && !t.After(now) && i < 100000; i++ {
		t = StepOn(cad, t, last.Day())
	}
	return t
}

// Step is exactly one stride from anchor, for stride cadences only (a clock
// cadence has no anchor to step from — use Next). Zero for anything else.
// This is what a repeating calendar item wants: the next occurrence is
// counted from the item's own day, and the caller decides about backlog.
func Step(cad string, anchor time.Time) time.Time { return StepOn(cad, anchor, anchor.Day()) }

// StepOn is Step for a chain whose occurrences belong on day-of-month dom:
// a monthly or yearly stride lands on dom, or on the month's last day when
// the month is shorter. A chain that began on Jan 31 passes 31 from its
// Feb 28 row, so the next is Mar 31, not Mar 28. dom only shapes monthly
// and yearly; < 1 means the anchor's own day.
func StepOn(cad string, anchor time.Time, dom int) time.Time {
	if dom < 1 {
		dom = anchor.Day()
	}
	switch cad {
	case "daily":
		return anchor.AddDate(0, 0, 1)
	case "weekly":
		return anchor.AddDate(0, 0, 7)
	case "monthly":
		return addMonths(anchor, 1, dom)
	case "yearly":
		return addMonths(anchor, 12, dom)
	}
	if n := StrideDays(cad); n > 0 {
		return anchor.AddDate(0, 0, n)
	}
	return time.Time{}
}

// addMonths is t plus n calendar months on day dom, held to the last day of
// the month it lands in. time.AddDate normalises instead: Jan 31 + 1 month is
// "Feb 31", i.e. Mar 3, so a monthly chain on the 29th-31st would skip
// February and then live on the 1st-3rd for good, and a yearly one on Feb 29
// would move to Mar 1.
func addMonths(t time.Time, n, dom int) time.Time {
	y, m, _ := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	if last := first.AddDate(0, 1, -1).Day(); dom > last {
		dom = last
	}
	return first.AddDate(0, 0, dom-1)
}

// MonthEnd: t is the last day of its month (a clamped occurrence may be).
func MonthEnd(t time.Time) bool { return t.AddDate(0, 0, 1).Day() == 1 }

// StrideDays is N for `every<N>d` (1..365), else 0.
func StrideDays(cad string) int {
	if !strings.HasPrefix(cad, "every") || !strings.HasSuffix(cad, "d") || strings.Contains(cad, "@") {
		return 0
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(cad, "every"), "d")
	if len(digits) < 1 || len(digits) > 3 {
		return 0
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || n > 365 {
		return 0
	}
	return n
}

func stride(s string) bool {
	switch s {
	case "daily", "weekly", "monthly", "yearly":
		return true
	}
	return StrideDays(s) > 0
}

// Human is the cadence in words for a card or a list row.
func Human(cad string) string {
	if kind, arg, err := parseClock(cad); err == nil {
		switch kind {
		case "never":
			return "not scheduled"
		case "daily":
			return "daily at " + arg
		case "weekly":
			return "weekly on " + arg
		case "every":
			return "every " + arg
		}
	}
	if n := StrideDays(cad); n > 0 {
		if n == 1 {
			return "daily"
		}
		return fmt.Sprintf("every %d days", n)
	}
	return cad
}

func parseClock(w string) (kind, arg string, err error) {
	if w == "manual" || w == "" {
		return "never", "", nil
	}
	kind, arg, ok := strings.Cut(w, "@")
	if !ok {
		return "", "", fmt.Errorf("bad cadence %q", w)
	}
	switch kind {
	case "daily":
		if _, _, ok := hhmm(arg); !ok {
			return "", "", fmt.Errorf("bad time %q", arg)
		}
	case "weekly":
		f := strings.Fields(arg)
		if len(f) != 2 || weekday(f[0]) < 0 {
			return "", "", fmt.Errorf("bad weekly %q (want 'Mon 09:00')", arg)
		}
		if _, _, ok := hhmm(f[1]); !ok {
			return "", "", fmt.Errorf("bad time %q", f[1])
		}
	case "every":
		d, err := time.ParseDuration(arg)
		if err != nil {
			return "", "", err
		}
		if d <= 0 {
			return "", "", fmt.Errorf("bad every %q", arg)
		}
		if d < MinEvery {
			return "", "", fmt.Errorf("every %q is shorter than %v: wakes are checked once a minute", arg, MinEvery)
		}
	default:
		return "", "", fmt.Errorf("bad cadence %q", w)
	}
	return kind, arg, nil
}

// hhmm reads H:MM or HH:MM and nothing else. Sscanf("%d:%d") stopped after
// the minutes, so "07:00pm" was 07:00 (twelve hours early, shown as written),
// and "19:00:30" or "9:00 PT" passed too.
func hhmm(s string) (int, int, bool) {
	hs, ms, ok := strings.Cut(s, ":")
	if !ok || len(hs) < 1 || len(hs) > 2 || len(ms) != 2 || !digits(hs) || !digits(ms) {
		return 0, 0, false
	}
	h, _ := strconv.Atoi(hs)
	m, _ := strconv.Atoi(ms)
	if h > 23 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func digits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func weekday(s string) int {
	for i, n := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
		if strings.EqualFold(n, s) {
			return i
		}
	}
	return -1
}
