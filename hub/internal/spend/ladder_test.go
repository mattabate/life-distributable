package spend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func pickerWith(raw string) *Picker {
	q := &QuotaFetcher{cachedAt: time.Now()}
	if raw != "" {
		json.Unmarshal([]byte(raw), &q.cachedRaw)
	} else {
		q.cachedErr = context.Canceled
	}
	return NewPicker(q, nil, nil)
}

// Step-down: rungs close at 60/80% of the shared bucket,
// long before the plan limit; the bottom rung stays open past 100%.
func TestPickerStepDown(t *testing.T) {
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour).Format(time.RFC3339)
	ctx := context.Background()
	for _, c := range []struct {
		util float64
		want string
	}{{84, "claude-fable-5-1"}, {85, "claude-opus-5-5"}, {94, "claude-opus-5-5"}, {95, "claude-sonnet-5"}, {100, "claude-sonnet-5"}} {
		p := pickerWith(`{"five_hour":{"utilization":` + jsonNum(c.util) + `,"resets_at":"` + later + `"}}`)
		if got := p.pick(ctx, now); got != c.want {
			t.Errorf("shared %v%%: got %s want %s", c.util, got, c.want)
		}
	}
	// The 7-day bucket counts too; a family bucket closes only its rung.
	week := now.Add(72 * time.Hour).Format(time.RFC3339)
	p := pickerWith(`{"five_hour":{"utilization":10,"resets_at":"` + later + `"},"seven_day_fable":{"utilization":90,"resets_at":"` + week + `"}}`)
	if got := p.pick(ctx, now); got != "claude-opus-5-5" {
		t.Fatalf("fable 7d at 90: %s", got)
	}
	// Configured thresholds override the defaults.
	p = pickerWith(`{"five_hour":{"utilization":65,"resets_at":"` + later + `"}}`)
	p.StepDown = []float64{60, 80}
	if got := p.pick(ctx, now); got != "claude-opus-5-5" {
		t.Fatalf("custom 60: %s", got)
	}
}

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

// The policy names the rung a wake starts on; the picker still steps down
// from there, and a model off the ladder is used as given.
func TestPickFromAndPolicy(t *testing.T) {
	ctx := context.Background()
	p := pickerWith("")
	if got := p.PickFrom(ctx, "claude-opus-5-5"); got != "claude-opus-5-5" {
		t.Fatalf("from opus: %s", got)
	}
	if got := p.PickFrom(ctx, ""); got != "claude-fable-5-1" {
		t.Fatalf("from top: %s", got)
	}
	if got := p.PickFrom(ctx, "claude-haiku-4-5-20251001"); got != "claude-haiku-4-5-20251001" {
		t.Fatalf("off-ladder: %s", got)
	}
	p.closed["claude-opus-5-5"] = time.Now()
	if got := p.PickFrom(ctx, "claude-opus-5-5"); got != "claude-sonnet-5" {
		t.Fatalf("opus closed: %s", got)
	}

	pol := DefaultPolicy()
	pol.ClassByGoal["build-a-compelling-life-agent"] = "build"
	if r := pol.Resolve("message", "", "build-a-compelling-life-agent"); r.Model != "claude-opus-5-5" || r.Effort != "" {
		t.Fatalf("build thread message: %+v", r)
	}
	if r := pol.Resolve("message", "", "make-more-money"); r.Model != "" {
		t.Fatalf("judgment thread starts at the top: %+v", r)
	}
	// A check-in starts where the session's own class does — the ladder top
	// for judgment, opus for build — lean and $8-capped, no effort pin
	// (overnight code reviews must not wake on sonnet).
	if r := pol.Resolve("checkin", "judgment", ""); r.Model != "" || r.Effort != "" || r.MaxBudgetUSD != 8 || !r.Fresh {
		t.Fatalf("checkin: %+v", r)
	}
	if r := pol.Resolve("checkin", "build", ""); r.Model != "claude-opus-5-5" {
		t.Fatalf("build checkin: %+v", r)
	}
	// Sonnet only when the session is pinned to it (a rote lane).
	if r := pol.Resolve("checkin", "claude-sonnet-5", ""); r.Model != "claude-sonnet-5" {
		t.Fatalf("pinned checkin: %+v", r)
	}
	// No launch carries --max-turns: a turn's length is not a lever.
	for _, oneShot := range []bool{true, false} {
		for k := range pol.Triggers {
			for _, a := range pol.Resolve(k, "judgment", "").Args(oneShot) {
				if a == "--max-turns" {
					t.Fatalf("%s (oneShot=%v) still caps tool calls", k, oneShot)
				}
			}
		}
	}
	if r := pol.Resolve("message", "claude-haiku-4-5-20251001", ""); r.Model != "claude-haiku-4-5-20251001" {
		t.Fatalf("explicit model as class: %+v", r)
	}
	if r := pol.Resolve("bogus", "build", ""); r.Model != "claude-opus-5-5" {
		t.Fatalf("unknown trigger falls back to message: %+v", r)
	}
	// A partial config keeps the defaults it does not name.
	cfg := (&Policy{Triggers: map[string]Rule{"checkin": {Model: "claude-haiku-4-5-20251001"}}}).Merge(DefaultPolicy())
	if cfg.Triggers["job"].Model != "claude-sonnet-5" || cfg.Triggers["checkin"].Model != "claude-haiku-4-5-20251001" || cfg.DefaultClass != "judgment" {
		t.Fatalf("merge: %+v", cfg)
	}
	if !pol.ValidClass("build") || !pol.ValidClass("auto") || pol.ValidClass("cheap") {
		t.Fatal("ValidClass")
	}
}

func TestPickerLadder(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour).Format(time.RFC3339)
	ctx := context.Background()
	// Usage endpoint down → top rung.
	if got := pickerWith("").pick(ctx, now); got != "claude-fable-5-1" {
		t.Fatalf("no quota: %s", got)
	}
	// Fable bucket full → opus.
	p := pickerWith(`{"five_hour":{"utilization":40,"resets_at":"` + later + `"},"seven_day_fable":{"utilization":100,"resets_at":"` + later + `"}}`)
	if got := p.pick(ctx, now); got != "claude-opus-5-5" {
		t.Fatalf("fable full: %s", got)
	}
	// …and opus too → sonnet.
	p = pickerWith(`{"five_hour":{"utilization":40,"resets_at":"` + later + `"},"seven_day_fable":{"utilization":100,"resets_at":"` + later + `"},"seven_day_opus":{"utilization":101,"resets_at":"` + later + `"}}`)
	if got := p.pick(ctx, now); got != "claude-sonnet-5" {
		t.Fatalf("opus full: %s", got)
	}
	// A full bucket whose reset is in the past is stale → ignored.
	p = pickerWith(`{"seven_day_fable":{"utilization":100,"resets_at":"2026-08-21T00:00:00Z"}}`)
	if got := p.pick(ctx, now); got != "claude-fable-5-1" {
		t.Fatalf("stale: %s", got)
	}
	// Shared bucket full → bottom rung (nothing better exists).
	p = pickerWith(`{"five_hour":{"utilization":100,"resets_at":"` + later + `"}}`)
	if got := p.pick(ctx, now); got != "claude-sonnet-5" {
		t.Fatalf("shared full: %s", got)
	}
	// A run that just died on a limit closes its rung for an hour even
	// before the endpoint says so.
	p = pickerWith(`{"five_hour":{"utilization":40,"resets_at":"` + later + `"}}`)
	p.closed["claude-fable-5-1"] = now
	if got := p.pick(ctx, now); got != "claude-opus-5-5" {
		t.Fatalf("marked: %s", got)
	}
	if got := p.pick(ctx, now.Add(2*time.Hour)); got != "claude-fable-5-1" {
		t.Fatalf("mark expired: %s", got)
	}
}

func TestIsUnsupportedModelError(t *testing.T) {
	live := `API Error: 400 Claude Code 2.1.276 does not support this model; version 2.1.280 or newer is required. [claude-code:unrecognized_model] {"model":"claude-opus-5-5","query_source":"sdk"}`
	if !IsUnsupportedModelError(live) || !IsRungError(live) {
		t.Fatal("live 400 not matched")
	}
	if IsLimitError(live) {
		t.Fatal("a stale CLI is not a plan limit")
	}
	if IsUnsupportedModelError("API Error: 400 invalid_request_error: prompt is too long") {
		t.Fatal("other 400s are not rung errors")
	}
}

func TestIsLimitError(t *testing.T) {
	for _, s := range []string{"You've hit your limit · resets 3pm", "Rate limit exceeded", "API Error: 429 rate_limit_error"} {
		if !IsLimitError(s) {
			t.Errorf("should match: %q", s)
		}
	}
	if IsLimitError("command not found") {
		t.Error("false positive")
	}
}

// The live payload on 2026-08-23: the shared buckets are nowhere near full,
// the legacy per-model keys are null, and the ONLY signal that Fable is
// finished is a scoped entry in "limits". A hub locked out because it read only the top-level keys and kept picking fable.
const scopedLimitPayload = `{
  "five_hour":  {"utilization": 12.0, "resets_at": "2026-08-23T09:50:00+00:00"},
  "seven_day":  {"utilization": 52.0, "resets_at": "2026-08-26T04:00:00+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": null,
  "nimbus_quill": {"utilization": 0.0, "resets_at": null},
  "limits": [
    {"kind":"session","group":"session","percent":12,"severity":"normal",
     "resets_at":"2026-08-23T09:50:00+00:00","scope":null,"is_active":false},
    {"kind":"weekly_all","group":"weekly","percent":52,"severity":"normal",
     "resets_at":"2026-08-26T04:00:00+00:00","scope":null,"is_active":false},
    {"kind":"weekly_scoped","group":"weekly","percent":100,"severity":"critical",
     "resets_at":"2026-08-26T04:00:00+00:00",
     "scope":{"model":{"id":null,"display_name":"Fable"},"surface":null},"is_active":true}
  ]
}`

func TestScopedModelLimitStepsDown(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 50, 0, 0, time.UTC)
	p := pickerWith(scopedLimitPayload)
	if got := p.pick(context.Background(), now); got != "claude-opus-5-5" {
		t.Fatalf("fable weekly is at 100%%: want claude-opus-5, got %q", got)
	}
	// The scoped bucket must also be visible to the Spend tab.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(scopedLimitPayload), &raw); err != nil {
		t.Fatal(err)
	}
	var fable *Window
	for _, w := range windowsFrom(raw) {
		if w.Key == "seven_day_fable" {
			x := w
			fable = &x
		}
	}
	if fable == nil {
		t.Fatal("no seven_day_fable window synthesized from limits[]")
	}
	if fable.Utilization != 100 || fable.Label != "Fable · 7 days" || fable.ScopeModel != "fable" || fable.ResetsAt.IsZero() || fable.StartsAt.IsZero() {
		t.Fatalf("bad scoped window: %+v", *fable)
	}
	// Unscoped entries must not duplicate the top-level buckets.
	n := 0
	for _, w := range windowsFrom(raw) {
		if w.Key == fiveHourKey || w.Key == sevenDayKey {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("shared buckets duplicated: %d", n)
	}
	// Once the scoped window has reset, fable is back on top.
	if got := p.pick(context.Background(), time.Date(2026, 8, 26, 5, 0, 0, 0, time.UTC)); got != "claude-fable-5-1" {
		t.Fatalf("after reset want fable, got %q", got)
	}
}

func TestIsLimitErrorMatchesLiveCLIMessage(t *testing.T) {
	yes := []string{
		"You've reached your Fable 5 limit. Switch to another model, or manage usage credits at claude.ai/settings/usage?from=cc_cli_limit_message, to continue",
		"You've reached your Opus limit.",
		"Claude usage limit reached. Your limit will reset at 9am.",
		"rate limit exceeded",
	}
	for _, s := range yes {
		if !IsLimitError(s) {
			t.Errorf("should be a limit error: %q", s)
		}
	}
	no := []string{
		"compilation failed: undefined: foo",
		"the statement parser hit a malformed row",
	}
	for _, s := range no {
		if IsLimitError(s) {
			t.Errorf("should NOT be a limit error: %q", s)
		}
	}
}

func TestExplainNamesTheClosedRung(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 50, 0, 0, time.UTC)
	p := pickerWith(scopedLimitPayload)
	model, why := p.Explain(context.Background(), now)
	if strings.Count(strings.ToLower(why), "fable") != 1 {
		t.Fatalf("family repeated in reason: %q", why)
	}
	if model != "claude-opus-5-5" {
		t.Fatalf("model = %q", model)
	}
	if !strings.Contains(strings.ToLower(why), "fable") || !strings.Contains(why, "100%") || !strings.Contains(why, "resets in 3d") {
		t.Fatalf("reason = %q", why)
	}
	// Top rung open → no reason to explain.
	later := now.Add(time.Hour).Format(time.RFC3339)
	p2 := pickerWith(`{"five_hour":{"utilization":10,"resets_at":"` + later + `"}}`)
	if m, why := p2.Explain(context.Background(), now); m != "claude-fable-5-1" || why != "" {
		t.Fatalf("%q %q", m, why)
	}
}

// The Spend toggle pins a rung and the ladder only ever steps DOWN from it,
// so explaining the top-of-ladder pick claimed "the ladder is handing them
// fable 5 instead" under a toggle pinned to opus 5.
func TestExplainFromPinnedRung(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 50, 0, 0, time.UTC)
	later := now.Add(time.Hour).Format(time.RFC3339)

	// Nothing is full. Unpinned, the ladder starts at fable; pinned to opus,
	// opus is what runs and there is nothing to explain.
	p := pickerWith(`{"five_hour":{"utilization":10,"resets_at":"` + later + `"}}`)
	if m, why := p.Explain(context.Background(), now); m != "claude-fable-5-1" || why != "" {
		t.Fatalf("unpinned: %q %q", m, why)
	}
	if m, why := p.ExplainFrom(context.Background(), now, "claude-opus-5-5"); m != "claude-opus-5-5" || why != "" {
		t.Fatalf("pinned to opus: %q %q", m, why)
	}

	// The pinned rung really is full: step down, and name the bucket that did
	// it — not fable's, which was never asked for.
	full := pickerWith(`{"five_hour":{"utilization":10,"resets_at":"` + later + `"},` +
		`"seven_day_fable":{"utilization":100,"resets_at":"` + later + `"},` +
		`"seven_day_opus":{"utilization":100,"resets_at":"` + later + `"}}`)
	m, why := full.ExplainFrom(context.Background(), now, "claude-opus-5-5")
	if m != "claude-sonnet-5" {
		t.Fatalf("model = %q (%q)", m, why)
	}
	if !strings.Contains(strings.ToLower(why), "opus") || strings.Contains(strings.ToLower(why), "fable") {
		t.Fatalf("reason should blame the pinned rung only: %q", why)
	}

	// A model that is not on the ladder means what it says.
	if m, why := p.ExplainFrom(context.Background(), now, "claude-haiku-4-5"); m != "claude-haiku-4-5" || why != "" {
		t.Fatalf("off-ladder: %q %q", m, why)
	}
}

// A scoped bucket reports only its own family's spend.
func TestScopedWindowCountsOnlyItsFamily(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 50, 0, 0, time.UTC)
	q := &QuotaFetcher{cachedAt: time.Now()}
	json.Unmarshal([]byte(scopedLimitPayload), &q.cachedRaw)
	us := []Usage{
		{Model: "claude-fable-5", TS: now.Add(-time.Hour), Output: 1_000_000},  // $50
		{Model: "claude-opus-5-5", TS: now.Add(-time.Hour), Output: 1_000_000}, // $20
	}
	got := q.Quota(context.Background(), us, now)
	for _, w := range got.Windows {
		switch w.Key {
		case "seven_day_fable":
			if len(w.by_modelKeys()) != 1 || w.by_modelKeys()[0] != "claude-fable-5" || w.SpentUSD != 50 {
				t.Fatalf("scoped window counted foreign models: %+v", w)
			}
		case sevenDayKey:
			if len(w.by_modelKeys()) != 2 {
				t.Fatalf("shared window should keep every model: %+v", w)
			}
		}
	}
}

// The fable day cap reads the rung to fall to off the ladder, and clamps a
// policy's starting rung to it.
func TestBelowAndNotAbove(t *testing.T) {
	p := NewPicker(nil, nil, nil) // fable → opus → sonnet
	if got := p.Below("fable"); got != "claude-opus-5-5" {
		t.Fatalf("below fable = %q", got)
	}
	if got := p.Below("sonnet"); got != "" {
		t.Fatalf("below the bottom rung = %q", got)
	}
	cases := []struct{ start, floor, want string }{
		{"", "", ""}, // no cap: the policy's own rung
		{"", "claude-opus-5-5", "claude-opus-5-5"},                                    // top rung clamped down
		{"claude-opus-5-5", "claude-opus-5-5", "claude-opus-5-5"},                     // already there
		{"claude-sonnet-5", "claude-opus-5-5", "claude-sonnet-5"},                     // never clamped UP
		{"claude-haiku-4-5-20251001", "claude-opus-5-5", "claude-haiku-4-5-20251001"}, // off-ladder id left alone
		{"", "claude-mythos-9", ""},                                                   // a floor the ladder does not list
	}
	for _, c := range cases {
		if got := p.NotAbove(c.start, c.floor); got != c.want {
			t.Fatalf("NotAbove(%q,%q) = %q, want %q", c.start, c.floor, got, c.want)
		}
	}
}

func (w Window) by_modelKeys() []string {
	out := []string{}
	for _, b := range w.ByModel {
		out = append(out, b.Key)
	}
	return out
}

func TestCurrentMapsRetiredRungs(t *testing.T) {
	for in, want := range map[string]string{
		"claude-fable-5": "claude-fable-5-1", "claude-opus-5": "claude-opus-5-5",
		"claude-fable-5-1": "claude-fable-5-1", "claude-sonnet-5": "claude-sonnet-5", "": "",
	} {
		if got := Current(in); got != want {
			t.Errorf("Current(%q) = %q, want %q", in, got, want)
		}
	}
}
