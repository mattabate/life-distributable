package spend

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Model ladder: every session runs the top model until its
// plan bucket is used up, then the next rung, then the next — a session must
// never stall on "limit reached". The picker consults the same usage buckets
// the Spend tab shows; a rung is closed when a bucket named after it (e.g.
// seven_day_fable, five_hour_opus) is at 100%, or when a run on it just
// died with a limit error (the usage endpoint lags a minute or so). If the
// shared all-model buckets are full, nothing helps: the bottom rung still
// runs, so the session fails loudly rather than silently sitting.

// DefaultLadder. No 1M-context variant: long-context input is billed higher
// and threads rarely need it — the CLI compacts instead. Fable 5.1 on top
// (same base price as 5, cheaper cache reads, better benchmarks), Opus 5.5
// as the fallback (cheaper than Opus 5, $4/$20 against $5/$25), Sonnet 5.5
// at the bottom.
var DefaultLadder = []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5"}

// successors: a rung the ladder retired → the id that replaced it. A thread
// pins its model at birth (threads.DefaultModel), so threads made on a
// retired rung would resume on the old id forever — off the ladder, so no
// step-down and no fable day cap, and fable 5 reads its cache at 4× fable
// 5.1's price.
var successors = map[string]string{
	"claude-fable-5":  "claude-fable-5-1",
	"claude-opus-5":   "claude-opus-5-5",
	"claude-sonnet-5": "claude-sonnet-5-5",
}

// Current: the model a pinned rung runs on today — its successor when the
// ladder retired it, else itself.
func Current(model string) string {
	if s, ok := successors[model]; ok {
		return s
	}
	return model
}

// DefaultStepDown (never hit the limits, but use as much fable as
// possible): rung i closes once the shared 5h/7d
// bucket — or a bucket named after its family — reaches StepDown[i]%, not
// 100%. Fable runs until 85%, opus until 95%, sonnet takes the last 5%, so
// the bar never actually fills. The bottom rung never closes.
var DefaultStepDown = []float64{85, 95}

// exhaustedTTL: how long a rung stays closed after a run on it hit a limit
// when the usage endpoint does not (yet) confirm it. Five-hour buckets roll
// continuously, so re-trying hourly is cheap.
const exhaustedTTL = time.Hour

// LimitErrorRe matches what the CLI prints when a plan bucket is used up.
// 2026-08-23: the live message is "You've reached your Fable 5 limit. Switch
// to another model, …" — none of the original alternatives matched it, so
// the reactive step-down never fired and every thread died instead.
// Only consulted on turns that already failed, so loose is safe.
var LimitErrorRe = regexp.MustCompile(`(?i)(rate.?limit|usage limit|hit your limit|reached your [^.]{0,40}limit|limit reached|limit has been reached|switch to another model|out of extra usage|out of credits|credit balance|cc_cli_limit)`)

// IsLimitError reports whether a failed turn's text is a plan/rate limit.
func IsLimitError(text string) bool { return LimitErrorRe.MatchString(text) }

// UnsupportedModelRe matches the API's answer when the installed CLI is too
// old for a model id: "API Error: 400 Claude Code 2.1.276 does not support
// this model; version 2.1.280 or newer is required. [claude-code:unrecognized_model]".
// A model put on a rung before the installed CLI knows it would otherwise
// kill every turn that steps down to it.
var UnsupportedModelRe = regexp.MustCompile(`(?i)(unrecognized_model|does not support this model)`)

// IsUnsupportedModelError reports whether a failed turn died on a model id
// the CLI rejects. The ladder treats it like a limit: close the rung, step down.
func IsUnsupportedModelError(text string) bool { return UnsupportedModelRe.MatchString(text) }

// IsRungError: a failure the next rung down fixes — a full plan bucket or a
// model id this CLI does not know.
func IsRungError(text string) bool { return IsLimitError(text) || IsUnsupportedModelError(text) }

type Picker struct {
	Q      *QuotaFetcher
	Ladder []string
	// StepDown[i]: utilization % at which rung i closes (missing = 100).
	StepDown []float64

	// Usages: local transcript spend, for the weekly pace line (nil = the
	// last-day ramp only). main.go hands it the spend cache.
	Usages func() ([]Usage, error)

	mu     sync.Mutex
	closed map[string]time.Time // model → when it was seen exhausted
}

func (p *Picker) usages() []Usage {
	if p.Usages == nil {
		return nil
	}
	us, _ := p.Usages()
	return us
}

func NewPicker(q *QuotaFetcher, ladder []string, stepDown []float64) *Picker {
	if len(ladder) == 0 {
		ladder = DefaultLadder
	}
	if len(stepDown) == 0 {
		stepDown = DefaultStepDown
	}
	return &Picker{Q: q, Ladder: ladder, StepDown: stepDown, closed: map[string]time.Time{}}
}

// Below: the first rung under `family` (Below("fable") = "claude-opus-5-5" on
// the default ladder), or "" when the ladder has nothing under it. The fable
// day cap in internal/budget starts every process there once the day's fable
// allowance is gone, so the model to fall to is read off the ladder rather
// than written down twice.
func (p *Picker) Below(family string) string {
	seen := false
	for _, m := range p.Ladder {
		if Family(m) == family {
			seen = true
			continue
		}
		if seen {
			return m
		}
	}
	return ""
}

// NotAbove: `start` (or "" for the top rung) clamped to no higher than
// `floor`. A rung the ladder does not list is left alone — an explicit model
// id from the policy means what it says.
func (p *Picker) NotAbove(start, floor string) string {
	start = Current(start)
	if floor == "" {
		return start
	}
	idx := func(m string) int {
		for i, x := range p.Ladder {
			if x == m {
				return i
			}
		}
		return -1
	}
	fi := idx(floor)
	if fi < 0 {
		return start
	}
	si := 0 // "" = the top rung
	if start != "" {
		if si = idx(start); si < 0 {
			return start
		}
	}
	if si < fi {
		return floor
	}
	return start
}

// threshold for rung i; the bottom rung is always open.
func (p *Picker) threshold(i int) float64 {
	if i == len(p.Ladder)-1 {
		return 101
	}
	if i < len(p.StepDown) && p.StepDown[i] > 0 {
		return p.StepDown[i]
	}
	return 100
}

// MarkExhausted closes a rung for exhaustedTTL (a run on it hit a limit).
func (p *Picker) MarkExhausted(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed[model] = time.Now()
}

// Pick returns the model for the next run. Never empty.
func (p *Picker) Pick(ctx context.Context) string {
	return p.pick(ctx, time.Now())
}

// PickFrom walks the ladder from `start` instead of the top (the model
// policy's rung for a check-in or a build thread); a start that is not on
// the ladder (haiku, say) is used as is, and "" means the top rung.
func (p *Picker) PickFrom(ctx context.Context, start string) string {
	start = Current(start)
	if start == "" {
		return p.pick(ctx, time.Now())
	}
	from := -1
	for i, m := range p.Ladder {
		if m == start {
			from = i
		}
	}
	if from < 0 {
		return start
	}
	return p.pickFrom(ctx, time.Now(), from)
}

// Explain returns the model a new session would use and, when that is not
// the top rung, a one-line reason ("fable 7 days at 100%, resets in 3d").
// The Spend tab shows it so "why am I on opus?" is never a mystery.
func (p *Picker) Explain(ctx context.Context, now time.Time) (string, string) {
	return p.ExplainFrom(ctx, now, "")
}

// ExplainFrom is Explain for a session pinned to `start` — the Spend page's
// toggle, which threads.go stamps into every new thread. The ladder only ever
// steps DOWN from that rung, so explaining the top-of-ladder pick instead
// would name a model the pinned session never ran on. "" means the top rung.
func (p *Picker) ExplainFrom(ctx context.Context, now time.Time, start string) (string, string) {
	from := 0
	if start != "" {
		from = -1
		for i, m := range p.Ladder {
			if m == start {
				from = i
			}
		}
		if from < 0 {
			return start, "" // not on the ladder: an explicit id means what it says
		}
	}
	model := p.pickFrom(ctx, now, from)
	if len(p.Ladder) == 0 || model == p.Ladder[from] {
		return model, ""
	}
	var reasons []string
	us := p.usages()
	if p.Q != nil {
		if raw, err := p.Q.raw(ctx); err == nil {
			for i := from; i < len(p.Ladder); i++ {
				m := p.Ladder[i]
				if m == model {
					break
				}
				for _, w := range windowsFrom(raw) {
					th := p.windowThreshold(w, i, us, now)
					if w.Utilization < th || (!w.ResetsAt.IsZero() && !w.ResetsAt.After(now)) {
						continue
					}
					fam := Family(m)
					if w.Key != fiveHourKey && w.Key != sevenDayKey && !strings.Contains(w.Key, fam) {
						continue
					}
					// "Fable · 7 days" already names the family; on a shared
					// window name the model being skipped, not "All models".
					r := fmt.Sprintf("%s at %.0f%%", w.Label, w.Utilization)
					if w.ScopeModel == "" {
						r = fmt.Sprintf("%s %s at %.0f%%", fam, windowBase(w.Key), w.Utilization)
					}
					r += fmt.Sprintf(" (cutoff %.0f%%)", th)
					if !w.ResetsAt.IsZero() {
						r += ", resets " + humanUntil(w.ResetsAt.Sub(now))
					}
					reasons = append(reasons, r)
					break
				}
			}
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "a run on a higher rung just hit its limit")
	}
	return model, strings.Join(reasons, "; ")
}

func humanUntil(d time.Duration) string {
	switch {
	case d <= 0:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	return fmt.Sprintf("in %dd", int(d.Hours()/24))
}

func (p *Picker) pick(ctx context.Context, now time.Time) string {
	return p.pickFrom(ctx, now, 0)
}

func (p *Picker) pickFrom(ctx context.Context, now time.Time, from int) string {
	var live []Window // live windows only
	us := p.usages()
	if p.Q != nil {
		if raw, err := p.Q.raw(ctx); err == nil {
			for _, w := range windowsFrom(raw) {
				if w.ResetsAt.IsZero() || w.ResetsAt.After(now) {
					live = append(live, w)
				}
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, m := range p.Ladder {
		if i < from {
			continue
		}
		if t, ok := p.closed[m]; ok && now.Sub(t) < exhaustedTTL {
			continue
		}
		if bucketFull(m, live, func(w Window) float64 { return p.windowThreshold(w, i, us, now) }) {
			continue
		}
		return m
	}
	return p.Ladder[len(p.Ladder)-1]
}

// The weekly pace line. A flat 85% on a 7-day bucket spends the reserve on
// nothing when the reset is near (a rung sitting at 87% an hour before the
// reset should run), and says nothing about whether Monday's pace is the
// owner's normal Monday. So a 7-day
// window closes a rung when what is used plus what history says the rest of
// the window will use crosses rampCeiling:
//
//	threshold = rampCeiling − need × f
//
// need = the mean spend over the last paceWeeks weeks in the SAME hours of
// the week (now → reset, shifted back a week at a time — the hours the owner was
// actually working the agent), at this window's own $→% rate. f scales the
// reserve per rung so the order holds (fable 1, opus (100−95)/(100−85) = ⅓):
// a typical 15-point need reproduces the flat 85/95. With no usable history
// the flat threshold ramps up to rampCeiling over the window's last day.
// The 5-hour windows keep the flat threshold.
const (
	rampWindow  = 24 * time.Hour
	rampCeiling = 98.0
	paceFloor   = 30.0 // a noisy estimate never shuts a rung for the week
	paceWeeks   = 4
	week        = 7 * 24 * time.Hour
)

// windowThreshold: the utilization at which window w closes rung i.
func (p *Picker) windowThreshold(w Window, i int, us []Usage, now time.Time) float64 {
	th := p.threshold(i)
	if th >= rampCeiling || w.ResetsAt.IsZero() || !strings.HasPrefix(w.Key, sevenDayKey) {
		return th
	}
	if n, ok := paceNeed(w, us, now); ok {
		f := 1.0
		if top := 100 - p.threshold(0); top > 0 {
			f = (100 - th) / top
		}
		return max(paceFloor, min(rampCeiling, rampCeiling-n*f))
	}
	left := w.ResetsAt.Sub(now)
	if left >= rampWindow {
		return th
	}
	return rampCeiling - (rampCeiling-th)*float64(max(left, 0))/float64(rampWindow)
}

// paceNeed: the % of window w the rest of it should use, going by the same
// hours of the week in past weeks. ok=false without a $→% rate (too little
// spent yet to divide by) or a single fully-covered past week.
func paceNeed(w Window, us []Usage, now time.Time) (float64, bool) {
	left := w.ResetsAt.Sub(now)
	if left <= 0 {
		return 0, true
	}
	start := w.ResetsAt.Add(-week)
	var spent float64
	past := make([]float64, paceWeeks+1)
	earliest := now
	for _, u := range us {
		if w.ScopeModel != "" && Family(u.Model) != w.ScopeModel {
			continue
		}
		if u.TS.Before(earliest) {
			earliest = u.TS
		}
		usd, _ := u.Cost()
		if !u.TS.Before(start) && u.TS.Before(now) {
			spent += usd
			continue
		}
		for j := 1; j <= paceWeeks; j++ {
			off := time.Duration(j) * week
			if !u.TS.Before(now.Add(-off)) && u.TS.Before(w.ResetsAt.Add(-off)) {
				past[j] += usd
			}
		}
	}
	if w.Utilization < 5 || spent < 5 {
		return 0, false
	}
	var sum float64
	n := 0
	for j := 1; j <= paceWeeks; j++ {
		if earliest.After(start.Add(-time.Duration(j) * week)) {
			break // that week is not fully on disk
		}
		sum += past[j]
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n) / (spent / w.Utilization), true
}

// bucketFull: a model is blocked when a bucket whose key names its family
// (claude-opus-5-5 ↔ seven_day_opus) or a shared bucket (five_hour, seven_day)
// is at or above the rung's threshold. The bottom rung's threshold is >100,
// so it is never blocked and the ladder always resolves to something.
func bucketFull(model string, live []Window, threshold func(Window) float64) bool {
	fam := Family(model)
	for _, w := range live {
		k, u := w.Key, w.Utilization
		if u < threshold(w) {
			continue
		}
		if k == fiveHourKey || k == sevenDayKey {
			return true
		}
		if fam != "" && strings.Contains(k, fam) {
			return true
		}
	}
	return false
}

// Family: "claude-fable-5[1m]" → "fable".
func Family(model string) string {
	s := strings.TrimPrefix(model, "claude-")
	for _, f := range []string{"fable", "mythos", "opus", "sonnet", "haiku"} {
		if strings.HasPrefix(s, f) {
			return f
		}
	}
	if i := strings.IndexAny(s, "-["); i > 0 {
		return s[:i]
	}
	return s
}
