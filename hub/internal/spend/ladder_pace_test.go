package spend

import (
	"context"
	"testing"
	"time"
)

// Fable at 87% with 57 minutes to the weekly reset must not run opus.
// With no history the 7-day cutoff ramps from 85 to 98 over the last day.
func TestWeeklyCutoffRampsNearReset(t *testing.T) {
	now := time.Date(2026, 9, 22, 23, 3, 0, 0, time.UTC)
	ctx := context.Background()
	for _, c := range []struct {
		left time.Duration
		util float64
		want string
	}{
		{57 * time.Minute, 87, "claude-fable-5-1"},
		{57 * time.Minute, 98, "claude-opus-5-5"},
		{12 * time.Hour, 91, "claude-fable-5-1"}, // cutoff 91.5
		{12 * time.Hour, 92, "claude-opus-5-5"},
		{3 * 24 * time.Hour, 86, "claude-opus-5-5"},
	} {
		at := now.Add(c.left).Format(time.RFC3339)
		p := pickerWith(`{"seven_day_fable":{"utilization":` + jsonNum(c.util) + `,"resets_at":"` + at + `"}}`)
		if got := p.pick(ctx, now); got != c.want {
			t.Errorf("%v left at %v%%: got %s want %s", c.left, c.util, got, c.want)
		}
	}
	// The 5-hour window keeps its flat 85.
	at := now.Add(10 * time.Minute).Format(time.RFC3339)
	if got := pickerWith(`{"five_hour":{"utilization":90,"resets_at":"`+at+`"}}`).pick(ctx, now); got != "claude-opus-5-5" {
		t.Fatalf("five_hour at 90: %s", got)
	}
}

// With history, the cutoff is 98 minus what past weeks spent in the same
// hours: a Monday that always runs heavy is not "ahead of pace".
func TestWeeklyCutoffFollowsHistory(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) // Monday noon
	reset := now.Add(36 * time.Hour)
	start := reset.Add(-week)
	msg := func(ts time.Time, out int) Usage {
		return Usage{TS: ts, Model: "claude-fable-5-1", Output: out}
	}
	one, _ := msg(now, 1_000_000).Cost() // $ of one message
	var us []Usage
	// This week so far: 20 messages → 60%, so one message is 3%.
	for i := 0; i < 20; i++ {
		us = append(us, msg(start.Add(time.Duration(i)*time.Hour), 1_000_000))
	}
	// Each of the last 4 weeks: 5 messages in the same Monday-noon → reset
	// span (15% needed), plus spend outside it that must not count.
	for j := 1; j <= 4; j++ {
		off := time.Duration(j) * week
		for i := 0; i < 5; i++ {
			us = append(us, msg(now.Add(-off).Add(time.Duration(i)*time.Hour), 1_000_000))
		}
		us = append(us, msg(start.Add(-off).Add(time.Hour), 9_000_000))
	}
	p := pickerWith(`{"seven_day_fable":{"utilization":60,"resets_at":"` + reset.Format(time.RFC3339) + `"}}`)
	p.Usages = func() ([]Usage, error) { return us, nil }
	w := Window{Key: "seven_day_fable", ScopeModel: "fable", Utilization: 60, ResetsAt: reset}
	n, ok := paceNeed(w, us, now)
	if !ok || n < 14.9 || n > 15.1 {
		t.Fatalf("need = %v %v, want 15 (one msg $%v)", n, ok, one)
	}
	if th := p.windowThreshold(w, 0, us, now); th < 82.9 || th > 83.1 {
		t.Fatalf("fable cutoff %v, want 83", th)
	}
	if got := p.pick(context.Background(), now); got != "claude-fable-5-1" {
		t.Fatalf("60%% under an 83 cutoff: %s", got)
	}
	// Past the pace line: 84% with the same history steps down.
	p = pickerWith(`{"seven_day_fable":{"utilization":84,"resets_at":"` + reset.Format(time.RFC3339) + `"}}`)
	p.Usages = func() ([]Usage, error) { return us, nil }
	if got := p.pick(context.Background(), now); got != "claude-opus-5-5" {
		t.Fatalf("84%% past the pace line: %s", got)
	}
}
