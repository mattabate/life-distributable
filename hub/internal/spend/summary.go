package spend

import (
	"os"
	"sort"
	"strings"
	"time"
)

type Bucket struct {
	Key        string  `json:"key"`
	USD        float64 `json:"usd"`
	Messages   int     `json:"messages"`
	Input      int     `json:"input_tokens"`
	Output     int     `json:"output_tokens"`
	CacheRead  int     `json:"cache_read_tokens"`
	CacheWrite int     `json:"cache_write_tokens"`
	// Models: the same dollars split by model, dearest first. Filled on the
	// by_day buckets only (the day chart is a stacked bar and its tooltip says "$200 to Fable, $300 to Fable 5.1"); nil elsewhere.
	Models []Bucket `json:"models,omitempty"`
}

// palette is the fixed colour-slot order for the stacked day chart: slot i
// draws in the clients' series colour i (`--s1…--s8` on the console,
// MixSeries on the phone). A model's slot is a property of the MODEL, never
// of its rank in the window, so changing the range never repaints a bar
// (dataviz: colour follows the entity). Longest prefix wins, like pricing.go,
// so a dated or `[1m]` variant lands on its family's slot; anything past the
// eight, or unlisted, draws grey.
var palette = []string{
	"claude-opus-5-5", "claude-fable-5-1", "claude-fable-5", "claude-sonnet-5",
	"claude-haiku-4-5", "claude-opus-5", "claude-mythos-5-1", "claude-mythos-5",
}

// paletteFor maps the models seen in a window onto the slots: index = slot,
// value = the model key as the buckets spell it ("" for an empty slot). Two
// keys on one slot (a dated variant beside the bare id) keep the first.
func paletteFor(models []string) []string {
	out := make([]string, len(palette))
	sort.Strings(models)
	for _, m := range models {
		best, bestLen := -1, 0
		for i, p := range palette {
			if strings.HasPrefix(m, p) && len(p) > bestLen {
				best, bestLen = i, len(p)
			}
		}
		if best >= 0 && out[best] == "" {
			out[best] = m
		}
	}
	return out
}

type Session struct {
	SessionID string    `json:"session_id"`
	Project   string    `json:"project"`
	Cwd       string    `json:"cwd"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	USD       float64   `json:"usd"`
	Messages  int       `json:"messages"`
	Models    []string  `json:"models"`
}

type Summary struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Days is the calendar-day window; 0 when the window is shorter than a
	// day (the Spend tab's 5-hour range), which WindowHours always states.
	Days          int      `json:"days"`
	WindowHours   float64  `json:"window_hours"`
	TotalUSD      float64  `json:"total_usd"`
	TodayUSD      float64  `json:"today_usd"`
	Messages      int      `json:"messages"`
	UnknownModels []string `json:"unknown_models"`
	ByDay         []Bucket `json:"by_day"`
	// History: every day since the first transcript, oldest first, each with
	// its split by model — the by-day chart's series, which is all-time
	// whatever the range picker says; the tiles above it keep the window.
	History   []Bucket `json:"history"`
	ByProject []Bucket `json:"by_project"`
	ByModel   []Bucket `json:"by_model"`
	// Palette: model key per colour slot (see paletteFor); "" = unused slot.
	Palette []string `json:"palette"`
	// ByTrigger: the hub's own thread turns in the window by what woke them
	// (message | checkin | decision), settled dollars plus turns in flight;
	// Jobs: ops/schedule.json runs by job name. Both from the hub's tables,
	// not the transcripts, so they are a subset of TotalUSD with the same
	// window. Filled by the server (2026-08-26).
	ByTrigger []Bucket  `json:"by_trigger"`
	Jobs      []Bucket  `json:"jobs"`
	Sessions  []Session `json:"sessions"`
}

// ProjectResolver maps a cwd to a project name; "" means unattributed.
type ProjectResolver func(cwd string) string

func Summarize(us []Usage, days int, now time.Time, resolve ProjectResolver) Summary {
	return summarize(us, now.AddDate(0, 0, -days), days, float64(days)*24, now, resolve)
}

// SummarizeWindow covers an arbitrary trailing window instead of whole days —
// the Spend tab's 5-hour range, which lines up with the five_hour plan meter.
// ByDay is still filled, but over a sub-day window it is one bucket, so the
// caller should not draw it as a day chart.
func SummarizeWindow(us []Usage, d time.Duration, now time.Time, resolve ProjectResolver) Summary {
	return summarize(us, now.Add(-d), 0, d.Hours(), now, resolve)
}

func summarize(us []Usage, cutoff time.Time, days int, windowHours float64, now time.Time, resolve ProjectResolver) Summary {
	loc := now.Location()
	today := now.In(loc).Format("2006-01-02")
	s := Summary{GeneratedAt: now, Days: days, WindowHours: windowHours, UnknownModels: []string{}, ByDay: []Bucket{}, History: []Bucket{}, ByProject: []Bucket{}, ByModel: []Bucket{}, Palette: paletteFor(nil), Sessions: []Session{}}
	byDay, byProj, byModel := map[string]*Bucket{}, map[string]*Bucket{}, map[string]*Bucket{}
	dayModel := map[string]map[string]*Bucket{} // day -> model -> that day's share
	// The all-time series behind the day chart: every usage, cutoff or not.
	histDay, histModel := map[string]*Bucket{}, map[string]map[string]*Bucket{}
	histModels := map[string]bool{}
	sess := map[string]*Session{}
	sessProj := map[string]map[string]float64{} // session -> project -> usd
	unknown := map[string]bool{}
	add := func(m map[string]*Bucket, k string, u Usage, usd float64) {
		b := m[k]
		if b == nil {
			b = &Bucket{Key: k}
			m[k] = b
		}
		b.USD += usd
		b.Messages++
		b.Input += u.Input
		b.Output += u.Output
		b.CacheRead += u.CacheRead
		b.CacheWrite += u.CacheWrite5m + u.CacheWrite1h
	}
	for _, u := range us {
		day := u.TS.In(loc).Format("2006-01-02")
		usd, known := u.Cost()
		add(histDay, day, u, usd)
		if histModel[day] == nil {
			histModel[day] = map[string]*Bucket{}
		}
		add(histModel[day], u.Model, u, usd)
		histModels[u.Model] = true
		if u.TS.Before(cutoff) {
			continue
		}
		if !known {
			unknown[u.Model] = true
		}
		proj := resolve(u.Cwd)
		if proj == "" {
			proj = "other: " + shorten(u.Cwd)
		}
		s.TotalUSD += usd
		s.Messages++
		if day == today {
			s.TodayUSD += usd
		}
		add(byDay, day, u, usd)
		add(byProj, proj, u, usd)
		add(byModel, u.Model, u, usd)
		if dayModel[day] == nil {
			dayModel[day] = map[string]*Bucket{}
		}
		add(dayModel[day], u.Model, u, usd)
		ss := sess[u.SessionID]
		if ss == nil {
			ss = &Session{SessionID: u.SessionID, Project: proj, Cwd: u.Cwd, Start: u.TS}
			sess[u.SessionID] = ss
		}
		ss.USD += usd
		ss.Messages++
		if sessProj[u.SessionID] == nil {
			sessProj[u.SessionID] = map[string]float64{}
		}
		sessProj[u.SessionID][proj] += usd
		if u.TS.After(ss.End) {
			ss.End = u.TS
		}
		if !contains(ss.Models, u.Model) {
			ss.Models = append(ss.Models, u.Model)
		}
	}
	// History and the palette cover every model ever seen, so a bar from
	// before the window paints in the same colours as the window's.
	s.History = flatten(histDay, func(a, b *Bucket) bool { return a.Key < b.Key })
	for i := range s.History {
		s.History[i].Models = flatten(histModel[s.History[i].Key], func(a, b *Bucket) bool { return a.USD > b.USD })
	}
	models := make([]string, 0, len(histModels))
	for m := range histModels {
		models = append(models, m)
	}
	s.Palette = paletteFor(models)
	if len(byDay) == 0 {
		return s
	}
	s.ByDay = flatten(byDay, func(a, b *Bucket) bool { return a.Key < b.Key })
	for i := range s.ByDay {
		s.ByDay[i].Models = flatten(dayModel[s.ByDay[i].Key], func(a, b *Bucket) bool { return a.USD > b.USD })
	}
	s.ByProject = flatten(byProj, func(a, b *Bucket) bool { return a.USD > b.USD })
	s.ByModel = flatten(byModel, func(a, b *Bucket) bool { return a.USD > b.USD })
	for id, v := range sess {
		// A session can change cwd mid-way (e.g. started in ~, worked in
		// ~/life); label it by the project that carries most of its spend so
		// the session list agrees with the by-project buckets.
		best, bestUSD := v.Project, -1.0
		for p, usd := range sessProj[id] {
			if usd > bestUSD || (usd == bestUSD && p < best) {
				best, bestUSD = p, usd
			}
		}
		v.Project = best
		s.Sessions = append(s.Sessions, *v)
	}
	sort.Slice(s.Sessions, func(i, j int) bool { return s.Sessions[i].End.After(s.Sessions[j].End) })
	for m := range unknown {
		s.UnknownModels = append(s.UnknownModels, m)
	}
	sort.Strings(s.UnknownModels)
	return s
}

func flatten(m map[string]*Bucket, less func(a, b *Bucket) bool) []Bucket {
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return less(&out[i], &out[j]) })
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func shorten(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
