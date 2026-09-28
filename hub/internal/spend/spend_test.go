package spend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sample = `{"type":"user","message":{"role":"user","content":"hi"},"sessionId":"s1","cwd":"/Users/owner/life","timestamp":"2026-08-20T10:00:00Z"}
{"type":"assistant","requestId":"r1","sessionId":"s1","cwd":"/Users/owner/life","timestamp":"2026-08-20T10:00:01Z","message":{"id":"m1","model":"claude-fable-5","usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500,"cache_creation":{"ephemeral_1h_input_tokens":500,"ephemeral_5m_input_tokens":0}}}}
{"type":"assistant","requestId":"r1","sessionId":"s1","cwd":"/Users/owner/life","timestamp":"2026-08-20T10:00:02Z","message":{"id":"m1","model":"claude-fable-5","usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500,"cache_creation":{"ephemeral_1h_input_tokens":500,"ephemeral_5m_input_tokens":0}}}}
{"type":"assistant","requestId":"r2","sessionId":"s1","cwd":"/Users/owner/other","timestamp":"2026-08-19T10:00:00Z","message":{"id":"m2","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`

func TestParseDedupAndCost(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x", "a.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(sample), 0o644)
	us, err := ParseAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 {
		t.Fatalf("want 2 deduped usages, got %d", len(us))
	}
	// m1 fable: 100*10 + 1000*10*0.1 + 500*10*2 + 200*50 = 1000+1000+10000+10000 = 22000 per 1e6
	usd, known := us[1].Cost()
	if !known || abs(usd-0.022) > 1e-9 {
		t.Fatalf("cost = %v known=%v", usd, known)
	}
	// Fable 5.1: same base, cache reads at $0.25/MTok flat, not Input*0.1 —
	// the same record costs 1000 + 250 + 10000 + 10000 = 21250 per 1e6.
	u51 := us[1]
	u51.Model = "claude-fable-5-1"
	usd, known = u51.Cost()
	if !known || abs(usd-0.02125) > 1e-9 {
		t.Fatalf("fable 5.1 cost = %v known=%v", usd, known)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	s := Summarize(us, 30, now, func(cwd string) string {
		if cwd == "/Users/owner/life" {
			return "life"
		}
		return ""
	})
	if s.Messages != 2 || len(s.ByProject) != 2 || s.ByProject[0].Key != "life" {
		t.Fatalf("summary: %+v", s)
	}
	if abs(s.TodayUSD-0.022) > 1e-9 {
		t.Fatalf("today = %v", s.TodayUSD)
	}
	// s1 has one cheap message in ~/other and the expensive one in ~/life:
	// the session is labelled by where its spend went, not by its first cwd.
	if len(s.Sessions) != 1 || s.Sessions[0].Project != "life" {
		t.Fatalf("sessions: %+v", s.Sessions)
	}
	if len(s.UnknownModels) != 0 {
		t.Fatalf("unknown models: %v", s.UnknownModels)
	}
}

// The day chart is a stacked bar: every by_day bucket
// carries its own split by model, dearest first, and the summary names which
// model owns which colour slot — by the model, never by its rank in the window.
func TestSummaryDayModelsAndPalette(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	us := []Usage{
		{SessionID: "a", Cwd: "/x", Model: "claude-haiku-4-5", TS: at(1), Output: 1_000_000},      // $5 today
		{SessionID: "a", Cwd: "/x", Model: "claude-opus-5-5", TS: at(2), Output: 1_000_000},       // $20 today
		{SessionID: "b", Cwd: "/x", Model: "claude-fable-5-1", TS: at(30), Output: 1_000_000},     // $50 yesterday
		{SessionID: "b", Cwd: "/x", Model: "claude-opus-5-5-20260101", TS: at(31), Output: 1_000}, // a dated variant, same slot family
	}
	s := Summarize(us, 7, now, func(string) string { return "p" })
	if len(s.ByDay) != 2 {
		t.Fatalf("by day: %+v", s.ByDay)
	}
	today := s.ByDay[1]
	if len(today.Models) != 2 || today.Models[0].Key != "claude-opus-5-5" || today.Models[1].Key != "claude-haiku-4-5" {
		t.Fatalf("today's split should be opus then haiku: %+v", today.Models)
	}
	if abs(today.Models[0].USD+today.Models[1].USD-today.USD) > 1e-9 {
		t.Fatalf("the split must add up to the day: %+v", today)
	}
	if len(s.ByModel) == 0 || s.ByModel[0].Models != nil {
		t.Fatalf("only by_day buckets carry a split: %+v", s.ByModel)
	}
	// Slots are fixed by the model: opus 5.5 is slot 0 and fable 5.1 slot 1
	// whether or not the window holds a fable 5, so the range picker never
	// repaints a bar. The dated opus lands on the same family and loses the
	// slot to the bare id, so it draws grey rather than stealing a colour.
	if s.Palette[0] != "claude-opus-5-5" || s.Palette[1] != "claude-fable-5-1" || s.Palette[2] != "" || s.Palette[4] != "claude-haiku-4-5" {
		t.Fatalf("palette: %q", s.Palette)
	}
	if got := paletteFor([]string{"claude-fable-5-1[1m]", "claude-fable-5"}); got[1] != "claude-fable-5-1[1m]" || got[2] != "claude-fable-5" {
		t.Fatalf("prefix slots: %q", got)
	}
	if p := Summarize(nil, 7, now, func(string) string { return "" }).Palette; len(p) != len(palette) {
		t.Fatalf("an empty window still names its slots: %q", p)
	}
}

// The day chart is all-time: `history` holds every day
// since the first transcript whatever the window, each with its split by
// model, and the palette names a model seen only before the window so its
// bars paint in the same colours as this week's.
func TestSummaryHistoryIsAllTime(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	us := []Usage{
		{SessionID: "a", Cwd: "/x", Model: "claude-opus-5-5", TS: at(1), Output: 1_000_000},
		{SessionID: "old", Cwd: "/x", Model: "claude-fable-5", TS: at(24 * 30), Output: 1_000_000},
		{SessionID: "old", Cwd: "/x", Model: "claude-sonnet-5", TS: at(24 * 30), Output: 1_000_000},
	}
	s := Summarize(us, 7, now, func(string) string { return "p" })
	if len(s.ByDay) != 1 || s.Messages != 1 {
		t.Fatalf("the window is still the window: %+v", s.ByDay)
	}
	if len(s.History) != 2 || s.History[0].Key != "2026-08-23" || s.History[1].Key != "2026-09-22" {
		t.Fatalf("history should hold both days, oldest first: %+v", s.History)
	}
	if old := s.History[0]; len(old.Models) != 2 || old.Messages != 2 || old.Models[0].Key != "claude-fable-5" {
		t.Fatalf("an old day carries its split, dearest first: %+v", old)
	}
	if s.Palette[2] != "claude-fable-5" || s.Palette[3] != "claude-sonnet-5" || s.Palette[0] != "claude-opus-5-5" {
		t.Fatalf("the palette covers models seen only before the window: %q", s.Palette)
	}
	if h := Summarize(nil, 7, now, func(string) string { return "" }).History; h == nil || len(h) != 0 {
		t.Fatalf("no transcripts: an empty list, never null: %v", h)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
