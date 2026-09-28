package spend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuota(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("anthropic-beta") == "" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"five_hour":{"utilization":40,"resets_at":"2026-08-21T14:00:00Z"},
		"seven_day":{"utilization":25.5,"resets_at":"2026-08-25T00:00:00Z"},
		"seven_day_opus":null,"nimbus_quill":{"utilization":0,"resets_at":null},"seven_day_fable":{"utilization":80,"resets_at":"2026-08-25T00:00:00Z"},
		"extra_usage":{"is_enabled":false}}`))
	}))
	defer srv.Close()
	q := &QuotaFetcher{Token: func(context.Context) (string, error) { return "tok", nil }, Client: srv.Client(), URL: srv.URL}
	us := []Usage{
		{Model: "claude-fable-5", TS: now.Add(-1 * time.Hour), Output: 1_000_000},        // $50, inside both
		{Model: "claude-haiku-4-5", TS: now.Add(-2 * 24 * time.Hour), Output: 1_000_000}, // $5, 7d only
		{Model: "claude-fable-5", TS: now.Add(-10 * 24 * time.Hour), Output: 1_000_000},  // outside
	}
	got := q.Quota(context.Background(), us, now)
	if !got.Available || got.Error != "" {
		t.Fatalf("not available: %s", got.Error)
	}
	if len(got.Windows) != 3 || got.Windows[0].Key != "five_hour" || got.Windows[1].Key != "seven_day" {
		t.Fatalf("windows: %+v", got.Windows)
	}
	fh := got.Windows[0]
	if fh.SpentUSD != 50 || fh.Messages != 1 || fh.Label != "All models · 5 hours" || fh.HeadroomUSD != 75 {
		t.Fatalf("five_hour: %+v", fh)
	}
	sd := got.Windows[1]
	if sd.SpentUSD != 55 || len(sd.ByModel) != 2 || sd.ByModel[0].Key != "claude-fable-5" {
		t.Fatalf("seven_day: %+v", sd)
	}
	if got.Windows[2].Label != "Fable · 7 days" || got.Windows[2].ScopeModel != "fable" {
		t.Fatalf("scoped window: %+v", got.Windows[2])
	}
	// The two 7-day bars look alike; each must say whose spend fills it.
	if !strings.Contains(sd.Note, "every model") || !strings.Contains(got.Windows[2].Note, "only Fable") {
		t.Fatalf("notes: %q / %q", sd.Note, got.Windows[2].Note)
	}
	// Burn: the 5h window is 40% full and all of its spend landed an hour
	// ago, i.e. outside recentSpan, so it falls back to the average pace
	// (40% over the 3h elapsed) and projects 100% three more hours out.
	if fh.BurnPctHour < 13 || fh.BurnPctHour > 14 {
		t.Fatalf("burn: %+v", fh.BurnPctHour)
	}
	if want := now.Add(4*time.Hour + 30*time.Minute); fh.FullAt.Sub(want).Abs() > time.Minute {
		t.Fatalf("full_at: %v want %v", fh.FullAt, want)
	}
	if string(got.Extra) == "" {
		t.Fatal("extra missing")
	}

	// Token failure → unavailable with reason, no panic.
	q2 := &QuotaFetcher{Token: func(context.Context) (string, error) { return "", context.DeadlineExceeded }, Client: srv.Client(), URL: srv.URL}
	if r := q2.Quota(context.Background(), us, now); r.Available || r.Error == "" {
		t.Fatalf("expected unavailable: %+v", r)
	}
}

// Anthropic reports a scoped bucket it is not metering as resets_at:null.
// Marshalled as Go's year-1 zero, the console rendered it in Eastern time and
// said the Fable limit "resets Dec 31 7:03 PM". Unknown must
// reach both surfaces as null so they can leave the clause out.
func TestUnknownTimesMarshalAsNull(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"five_hour":{"utilization":13,"resets_at":"2026-09-02T14:00:00Z"},
		"seven_day_fable":{"utilization":0,"resets_at":null}}`))
	}))
	defer srv.Close()
	q := &QuotaFetcher{Token: func(context.Context) (string, error) { return "tok", nil }, Client: srv.Client(), URL: srv.URL}
	got := q.Quota(context.Background(), []Usage{{Model: "claude-fable-5", TS: now.Add(-time.Hour), Output: 1_000_000}}, now)
	var fable Window
	for _, w := range got.Windows {
		if w.Key == "seven_day_fable" {
			fable = w
		}
	}
	if fable.Key == "" || !fable.ResetsAt.IsZero() {
		t.Fatalf("fable window: %+v", fable)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, "0001-01-01") {
		t.Fatalf("year-1 zero on the wire: %s", s)
	}
	var back Quota
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	for _, w := range back.Windows {
		switch w.Key {
		case "seven_day_fable":
			if !w.ResetsAt.IsZero() || !w.StartsAt.IsZero() || !w.FullAt.IsZero() {
				t.Fatalf("null did not decode back to unknown: %+v", w)
			}
			// The trailing-window fallback still counts the local dollars, so
			// the bar is not silently empty just because the reset is unknown.
			if w.SpentUSD != 50 {
				t.Fatalf("fable spend: %v", w.SpentUSD)
			}
		case "five_hour":
			if w.ResetsAt.IsZero() || w.StartsAt.IsZero() {
				t.Fatalf("known times lost in the round trip: %+v", w)
			}
		}
	}
}

// Past windowTTL the page gets the reading the hub has NOW and the fetch runs
// behind it; only a reading older than staleTTL (the sampler has stopped) or
// none at all makes a request wait on Anthropic.
func TestRawServesStaleWhileRefreshing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		fmt.Fprintf(w, `{"five_hour":{"utilization":%d,"resets_at":"2026-08-21T14:00:00Z"}}`, n*10)
	}))
	defer srv.Close()
	q := &QuotaFetcher{Token: func(context.Context) (string, error) { return "tok", nil }, Client: srv.Client(), URL: srv.URL}
	util := func() float64 {
		raw, err := q.raw(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return windowsFrom(raw)[0].Utilization
	}
	age := func(d time.Duration) {
		q.mu.Lock()
		q.cachedAt = time.Now().Add(-d)
		q.mu.Unlock()
	}
	if util() != 10 || hits.Load() != 1 {
		t.Fatalf("first fetch: hits %d", hits.Load())
	}
	// Past the TTL: the old reading comes back at once…
	age(2 * windowTTL)
	if got := util(); got != 10 {
		t.Fatalf("stale call waited for the fetch: %v", got)
	}
	// …and the new one lands shortly after.
	deadline := time.Now().Add(5 * time.Second)
	for util() != 20 {
		if time.Now().After(deadline) {
			t.Fatal("background refresh never landed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Too old to show: wait for a real one.
	age(2 * staleTTL)
	if got := util(); got != 30 || hits.Load() != 3 {
		t.Fatalf("very stale reading served: %v, hits %d", got, hits.Load())
	}
}

// The burn line is the point of the page: it must react to what is happening
// now, stay silent when it cannot know, and never project a full meter.
func TestFillBurn(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	start := now.Add(-4 * time.Hour)
	spend := []Usage{
		{Model: "claude-opus-5", TS: now.Add(-3 * time.Hour), Output: 1_000_000},   // $75
		{Model: "claude-opus-5", TS: now.Add(-15 * time.Minute), Output: 333_333},  // $25, recent
		{Model: "claude-fable-5", TS: now.Add(-15 * time.Minute), Output: 100_000}, // other family
	}

	// Recent burst: $25 of the window's $100 (so 12.5 of its 50%) inside the
	// 45-minute recent span → 16.7%/hr, and the remaining 50% fills in 3h —
	// far sooner than the 12.5%/hr average since the window opened.
	w := Window{Key: "five_hour", Utilization: 50, StartsAt: start}
	fillLocal(&w, spend[:2], now, nil)
	if w.BurnPctHour < 16 || w.BurnPctHour > 17.5 || w.FullAt.Sub(now.Add(3*time.Hour)).Abs() > time.Minute {
		t.Fatalf("burst: %.1f%%/hr full_at %v", w.BurnPctHour, w.FullAt)
	}

	// A scoped meter counts only its own family, so fable spend does not make
	// an opus meter look busy; with no opus spend it falls back to the window
	// average (25% over 4h).
	s := Window{Key: "seven_day_opus", ScopeModel: "opus", Utilization: 25, StartsAt: start}
	fillLocal(&s, spend[2:], now, nil)
	if s.SpentUSD != 0 || s.BurnPctHour < 6 || s.BurnPctHour > 6.5 {
		t.Fatalf("scoped: spent %.2f burn %.2f", s.SpentUSD, s.BurnPctHour)
	}

	// Already full → no projection to make.
	f := Window{Key: "seven_day_fable", ScopeModel: "fable", Utilization: 100, StartsAt: start}
	fillLocal(&f, spend, now, nil)
	if !f.FullAt.IsZero() {
		t.Fatalf("full meter projected %v", f.FullAt)
	}

	// A family locked out by its own meter cannot fill a shared one any more,
	// so its last burst must not set the shared meter's pace: without this the
	// 7-day bar reads "full in a day" off spend that can never repeat. Its
	// spend still counts toward what the meter has used.
	l := Window{Key: "seven_day", Utilization: 50, StartsAt: start}
	fillLocal(&l, spend, now, map[string]bool{"fable": true})
	free := Window{Key: "seven_day", Utilization: 50, StartsAt: start}
	fillLocal(&free, spend, now, nil)
	if l.SpentUSD != free.SpentUSD || l.BurnPctHour >= free.BurnPctHour {
		t.Fatalf("locked family still setting pace: spent %.2f/%.2f burn %.2f/%.2f", l.SpentUSD, free.SpentUSD, l.BurnPctHour, free.BurnPctHour)
	}

	// Idle window: nothing spent, 0% used → nothing to say.
	z := Window{Key: "seven_day", Utilization: 0, StartsAt: start}
	fillLocal(&z, nil, now, nil)
	if z.BurnPctHour != 0 || !z.FullAt.IsZero() {
		t.Fatalf("idle: %+v", z)
	}
}

// A lull just before the owner opens the page must not read as "you will never
// fill this": the trailing-day pace keeps forecasting through the gap.
func TestFillTypical(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	start := now.Add(-4 * time.Hour)
	// $100 in the window, all of it 3 hours ago — the last 45 min are silent.
	spend := []Usage{{Model: "claude-opus-5", TS: now.Add(-3 * time.Hour), Output: 1_333_333}}

	w := Window{Key: "five_hour", Utilization: 50, StartsAt: start}
	fillLocal(&w, spend, now, nil)
	// 50% is worth $100, so $100 over the trailing day is 50%/24h ≈ 2.08%/hr,
	// and the remaining 50% takes ~24h more.
	if w.TypicalPctHour < 2.0 || w.TypicalPctHour > 2.2 {
		t.Fatalf("typical %.2f%%/hr", w.TypicalPctHour)
	}
	if w.FullAtTypical.Sub(now.Add(24*time.Hour)).Abs() > time.Minute {
		t.Fatalf("full at typical %v", w.FullAtTypical)
	}
	// Locked families cannot repeat their spend, so they set no forecast.
	l := Window{Key: "seven_day", Utilization: 50, StartsAt: start}
	fillLocal(&l, []Usage{{Model: "claude-fable-5", TS: now.Add(-time.Hour), Output: 1_000_000}}, now, map[string]bool{"fable": true})
	if l.TypicalPctHour != 0 || !l.FullAtTypical.IsZero() {
		t.Fatalf("locked family forecast: %.2f %v", l.TypicalPctHour, l.FullAtTypical)
	}
	// A full meter has nowhere to go.
	f := Window{Key: "seven_day_fable", ScopeModel: "fable", Utilization: 100, StartsAt: start}
	fillLocal(&f, spend, now, nil)
	if f.TypicalPctHour != 0 || !f.FullAtTypical.IsZero() {
		t.Fatalf("full meter forecast: %+v", f)
	}
}

// The measured line is the only number on the card that is not an estimate,
// so it must be silent rather than wrong.
func TestFillMeasured(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	reset := now.Add(48 * time.Hour)
	w := Window{Key: "seven_day", Utilization: 58, ResetsAt: reset}
	past := []Sample{
		{Key: "seven_day", TS: now.Add(-4 * time.Hour), Percent: 52, ResetsAt: reset},
		{Key: "seven_day", TS: now.Add(-time.Hour), Percent: 56, ResetsAt: reset},
		{Key: "five_hour", TS: now.Add(-6 * time.Hour), Percent: 90, ResetsAt: reset}, // other meter
		{Key: "seven_day", TS: now.Add(-20 * time.Hour), Percent: 3, ResetsAt: reset}, // older than measuredSpan
	}
	fillMeasured(&w, past, now)
	if w.MeasuredDelta != 6 || w.MeasuredHours != 4 || w.MeasuredPctHour != 1.5 {
		t.Fatalf("measured %+v", w)
	}

	// A reading from the previous generation of the window would read as the
	// meter running backwards; drop it and say nothing.
	r := Window{Key: "seven_day", Utilization: 4, ResetsAt: reset}
	fillMeasured(&r, []Sample{{Key: "seven_day", TS: now.Add(-3 * time.Hour), Percent: 97, ResetsAt: reset.Add(-7 * 24 * time.Hour)}}, now)
	if r.MeasuredHours != 0 || r.MeasuredPctHour != 0 {
		t.Fatalf("stale generation used: %+v", r)
	}

	// Anthropic recomputes resets_at per request, so two readings of the same
	// generation are microseconds — and often a whole second — apart. Exact
	// equality rejected all of them and the seven-day cards showed no measured
	// line at all.
	j := Window{Key: "seven_day", Utilization: 58, ResetsAt: reset}
	fillMeasured(&j, []Sample{
		{Key: "seven_day", TS: now.Add(-4 * time.Hour), Percent: 52, ResetsAt: reset.Add(-187 * time.Millisecond)},
	}, now)
	if j.MeasuredDelta != 6 || j.MeasuredHours != 4 {
		t.Fatalf("jittered resets_at rejected: %+v", j)
	}

	// A reading stored before resets_at was known used to be exempt from the
	// generation check entirely, so it could anchor a measurement several
	// resets back: the five-hour card read "+2% in the last 6.8 h" on a window
	// 1.8 h old. The window's own start settles it.
	start := now.Add(-2 * time.Hour)
	u := Window{Key: "five_hour", Utilization: 3, ResetsAt: start.Add(5 * time.Hour), StartsAt: start}
	fillMeasured(&u, []Sample{
		{Key: "five_hour", TS: now.Add(-7 * time.Hour), Percent: 0}, // resets_at unknown, and older than this window
		{Key: "five_hour", TS: now.Add(-time.Hour), Percent: 1},     // unknown too, but inside it
	}, now)
	if u.MeasuredDelta != 2 || u.MeasuredHours != 1 {
		t.Fatalf("measurement spans a reset: %+v", u)
	}

	// Nothing stored yet, or only minutes of it: no rate.
	n := Window{Key: "seven_day", Utilization: 58, ResetsAt: reset}
	fillMeasured(&n, []Sample{{Key: "seven_day", TS: now.Add(-5 * time.Minute), Percent: 58, ResetsAt: reset}}, now)
	if n.MeasuredHours != 0 {
		t.Fatalf("too-short span used: %+v", n)
	}
}

// Both surfaces print these verbatim, so the rules live in one table here.
func TestWindowWords(t *testing.T) {
	now := time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC) // noon Eastern
	cases := []struct {
		w                   Window
		tone, outlook, foot string
	}{
		// The foot is the clock only (2026-09-25): time left, share of the
		// window gone, reset time. Dollars and the measured delta stay in the
		// JSON for the chat facts.
		{Window{Key: "seven_day_fable", Utilization: 100, SpentUSD: 1234.4, ResetsAt: now.Add(26 * time.Hour)},
			"bad", "locked", "1 d 2 h left · 85% of the week gone · resets Sep 15 2:00 PM"},
		{Window{Key: "five_hour", Utilization: 40, SpentUSD: 20, HeadroomUSD: 30, TypicalPctHour: 10, ResetsAt: now.Add(2*time.Hour + 12*time.Minute),
			MeasuredHours: 4, MeasuredDelta: 6},
			"", "≈71% by reset", "2 h 12 min left · 56% of the 5 hours gone · resets 2:12 PM"},
		// Over iff the meter is past the tick (2026-09-27), whatever the last day ran.
		{Window{Key: "seven_day", Utilization: 75, TypicalPctHour: 0.1, ResetsAt: now.Add(48 * time.Hour)},
			"warn", "over by reset", "2 d left · 71% of the week gone · resets Sep 16 12:00 PM"},
		{Window{Key: "seven_day", Utilization: 60, TypicalPctHour: 5, ResetsAt: now.Add(48 * time.Hour)},
			"", "≈84% by reset", "2 d left · 71% of the week gone · resets Sep 16 12:00 PM"},
		{Window{Key: "seven_day", Utilization: 5, SpentUSD: 3},
			"", "idle", "reset time unknown"},
	}
	for i := range cases {
		c := &cases[i]
		fillWords(&c.w, now)
		if c.w.Tone != c.tone || c.w.Outlook != c.outlook || c.w.Foot != c.foot {
			t.Errorf("%s: got %q %q %q", c.w.Key, c.w.Tone, c.w.Outlook, c.w.Foot)
		}
	}
	// The tick on the bar: 48 h left of 168 is 71.4% gone; unknown reset = 0.
	if e := cases[2].w.ElapsedPct; e < 71.3 || e > 71.5 {
		t.Errorf("elapsed: got %v", e)
	}
	if cases[4].w.ElapsedPct != 0 {
		t.Errorf("elapsed with no reset: got %v", cases[4].w.ElapsedPct)
	}
	for d, want := range map[time.Duration]string{
		30 * time.Second: "0 min", 12 * time.Minute: "12 min", 3*time.Hour + 48*time.Minute: "3 h 48 min",
		3 * time.Hour: "3 h", 4*24*time.Hour + 2*time.Hour + 59*time.Minute: "4 d 2 h", 2 * 24 * time.Hour: "2 d",
	} {
		if got := leftWords(d); got != want {
			t.Errorf("leftWords(%v) = %q, want %q", d, got, want)
		}
	}
}
