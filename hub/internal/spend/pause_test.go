package spend

import (
	"testing"
	"time"
)

func TestResetAt(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	from := time.Date(2026, 9, 30, 2, 16, 0, 0, ny)
	cases := []struct {
		text string
		want time.Time
	}{
		// The CLI's own line.
		{"You've hit your session limit · resets 2:40am (America/New_York)", time.Date(2026, 9, 30, 2, 40, 0, 0, ny)},
		// A clock already past today is tomorrow's.
		{"You've hit your session limit · resets 1am (America/New_York)", time.Date(2026, 10, 1, 1, 0, 0, 0, ny)},
		{"You've hit your weekly limit · resets Oct 3, 9am (America/New_York)", time.Date(2026, 10, 3, 9, 0, 0, 0, ny)},
		{"You've hit your limit · resets 3:05 PM (America/New_York)", time.Date(2026, 9, 30, 15, 5, 0, 0, ny)},
	}
	for _, c := range cases {
		got, ok := ResetAt(c.text, from)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%q: got %v %v, want %v", c.text, got, ok, c.want)
		}
	}
	if IsSessionLimit("You've reached your Fable 5 limit. Switch to another model") {
		t.Error("a rung limit names no reset: it is the ladder's, not a pause")
	}
	if IsLimitError("You've hit your session limit · resets 2:40am (America/New_York)") {
		t.Error("a session limit must not step the ladder down: every rung shares it")
	}
}
