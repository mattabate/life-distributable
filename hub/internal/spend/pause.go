package spend

import (
	"regexp"
	"strings"
	"time"
)

// A session limit is not a rung (the card for "You've hit your session
// limit · resets 2:40am (America/New_York)"). The plan's five-hour bucket is
// shared by every model, so stepping down the ladder only fails again; the
// turn waits for the reset instead, and the CLI says when that is. PauseRe
// finds the line and its clock; ResetAt turns it into a time.
var PauseRe = regexp.MustCompile(`(?i)hit your (?:[a-z0-9 ]{0,24} )?limit[^\n]*?resets\s+(?:at\s+)?((?:[a-z]{3,9}\.?\s+\d{1,2},?\s+(?:at\s+)?)?\d{1,2}(?::\d{2})?\s*[ap]m)(?:\s*\(([^)]+)\))?`)

// ampmGap: "3:05 pm" → "3:05pm", the one form the layouts read.
var ampmGap = regexp.MustCompile(`\s+([ap]m)$`)

// IsSessionLimit: the failed turn stopped on a plan limit that names its reset.
func IsSessionLimit(text string) bool { return PauseRe.MatchString(text) }

// ResetAt: when the limit in text lifts, read against `from` (the moment the
// turn failed). A bare clock ("2:40am") is the next such time after from; a
// dated one ("Oct 3, 9am") is that day in from's year, or the next year's if
// that is already more than a day gone. The zone is the one the CLI names,
// else the Mac's.
func ResetAt(text string, from time.Time) (time.Time, bool) {
	mm := PauseRe.FindStringSubmatch(text)
	if mm == nil {
		return time.Time{}, false
	}
	loc := time.Local
	if mm[2] != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(mm[2])); err == nil {
			loc = l
		}
	}
	from = from.In(loc)
	s := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer(",", " ", ".", " ", " at ", " ").Replace(mm[1])), " "))
	s = ampmGap.ReplaceAllString(s, "$1")
	for _, layout := range []string{"3:04pm", "3pm"} {
		if c, err := time.ParseInLocation(layout, s, loc); err == nil {
			t := time.Date(from.Year(), from.Month(), from.Day(), c.Hour(), c.Minute(), 0, 0, loc)
			if !t.After(from.Add(-time.Minute)) {
				t = t.AddDate(0, 0, 1)
			}
			return t, true
		}
	}
	for _, layout := range []string{"Jan 2 3:04pm", "Jan 2 3pm", "January 2 3:04pm", "January 2 3pm"} {
		if c, err := time.ParseInLocation(layout, s, loc); err == nil {
			t := time.Date(from.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), 0, 0, loc)
			if t.Before(from.Add(-24 * time.Hour)) {
				t = t.AddDate(1, 0, 0)
			}
			return t, true
		}
	}
	return time.Time{}, false
}

// ClockWords: a reset as the owner reads it — "2:40 AM", or "Oct 3, 9:00 AM"
// when it is not within the next day.
func ClockWords(t, from time.Time) string {
	if t.Sub(from) > 20*time.Hour {
		return t.Format("Jan 2, 3:04 PM")
	}
	return t.Format("3:04 PM")
}
