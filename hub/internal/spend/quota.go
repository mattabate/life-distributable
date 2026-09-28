package spend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"life/hub/internal/format"
	"life/hub/internal/httpx"
)

// Plan quota: what the Claude Code CLI's /usage shows. Consumer Max/Pro
// plans have no billing API; the CLI reads
// GET https://api.anthropic.com/api/oauth/usage with the OAuth access token
// it keeps in the macOS Keychain item "Claude Code-credentials". We do the
// same, read-only, and never persist the token.

const (
	usageURL      = "https://api.anthropic.com/api/oauth/usage"
	keychainItem  = "Claude Code-credentials"
	oauthBeta     = "oauth-2025-04-20"
	windowTTL     = 60 * time.Second
	fiveHourKey   = "five_hour"
	sevenDayKey   = "seven_day"
	maxWindowDays = 31
)

// Window is one rate-limit bucket as reported by Anthropic, plus what our
// local transcripts say was spent inside it.
type Window struct {
	Key         string    `json:"key"`          // five_hour, seven_day, seven_day_opus, …
	Label       string    `json:"label"`        // "7 days · all models", "7 days · Fable only"
	Note        string    `json:"note"`         // one line: which spend counts toward this limit
	Utilization float64   `json:"utilization"`  // percent 0..100 (can exceed 100)
	ResetsAt    time.Time `json:"resets_at"`    // zero if unknown
	StartsAt    time.Time `json:"starts_at"`    // resets_at - window length (derived)
	SpentUSD    float64   `json:"spent_usd"`    // list-price $ of local usage inside the window
	Messages    int       `json:"messages"`     // local assistant messages inside the window
	HeadroomUSD float64   `json:"headroom_usd"` // est. list-price $ left: spent*(100-util)/util; 0 if unknown
	ByModel     []Bucket  `json:"by_model"`     // local usage inside the window, by model

	// Which models this meter gates, and when it is projected to fill. The
	// page exists to answer "when do I run out", and a bar
	// that does not say what it gates cannot answer it.
	ScopeModel  string    `json:"scope_model,omitempty"` // "fable" when the limit is scoped to one family; "" = all models
	BurnPctHour float64   `json:"burn_pct_per_hour"`     // recent fill rate in percent/hour (0 if unknown)
	FullAt      time.Time `json:"full_at"`               // projected 100% at that rate; zero if unknown or not filling

	// Measured movement: Anthropic's own number now minus its own number a
	// while ago, from the readings we store every 10 min. The $-derived pace
	// above is an estimate; this is the meter actually moving — without
	// kept readings, a bar stuck at one number cannot say whether it is wrong.
	MeasuredPctHour float64 `json:"measured_pct_per_hour"` // Δ% / Δhours over the samples we have
	MeasuredDelta   float64 `json:"measured_delta_pct"`    // Δ% itself ("+6% in the last 4 h")
	MeasuredHours   float64 `json:"measured_span_hours"`   // how long that Δ spans; 0 = not enough history yet

	// Pace over the trailing 24 h rather than the last 45 min. A lull right
	// before the owner looks made "at this pace" read 0.0%/hr — i.e. "you will
	// never fill this" — seconds after an hour of hard use.
	TypicalPctHour float64   `json:"typical_pct_per_hour"`
	FullAtTypical  time.Time `json:"full_at_typical"`

	// How much of the window's clock has run, 0..100 (0 when the reset is
	// unknown). Both surfaces draw it as a tick on the bar so "35% of the
	// budget, 65% of the week" is one glance.
	ElapsedPct float64 `json:"elapsed_pct"`

	// The words and colour both surfaces draw, computed once here so the
	// console and the phone never word or colour the same reading differently. Tone: "bad" | "warn" | "". Outlook: "locked" | "over by reset"
	// | "≈N% by reset" | "idle". Foot: "4 d 2 h left · 41% of the week gone ·
	// resets Sep 30 12:00 AM" — no dollars or measured delta; they stay in the JSON for the chat facts.
	Tone    string `json:"tone"`
	Outlook string `json:"outlook"`
	Foot    string `json:"foot"`
}

// MarshalJSON sends an unknown time as null instead of Go's year-1 zero.
// Anthropic returns `resets_at: null` for a scoped bucket it is not metering
// (e.g. seven_day_fable); the console rendered that zero in Eastern time as
// a Fable limit that "resets Dec 31 7:03 PM" — a date no code had computed. Every consumer already treats a
// missing time as unknown, so null is the honest wire value.
func (w Window) MarshalJSON() ([]byte, error) {
	type alias Window // no MarshalJSON, so this does not recurse
	nz := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	return json.Marshal(struct {
		alias
		ResetsAt      *time.Time `json:"resets_at"`
		StartsAt      *time.Time `json:"starts_at"`
		FullAt        *time.Time `json:"full_at"`
		FullAtTypical *time.Time `json:"full_at_typical"`
	}{alias(w), nz(w.ResetsAt), nz(w.StartsAt), nz(w.FullAt), nz(w.FullAtTypical)})
}

// Sample is one stored reading of one meter: what Anthropic said, and when.
type Sample struct {
	Key      string    `json:"key"`
	TS       time.Time `json:"ts"`
	Percent  float64   `json:"percent"`
	ResetsAt time.Time `json:"resets_at"`
}

// Quota is the response of GET /api/v1/spend/quota.
type Quota struct {
	GeneratedAt time.Time       `json:"generated_at"`
	FetchedAt   time.Time       `json:"fetched_at"`
	Available   bool            `json:"available"`
	Error       string          `json:"error,omitempty"` // why not available
	Windows     []Window        `json:"windows"`
	NextModel   string          `json:"next_model,omitempty"`  // what a new session will run on
	NextReason  string          `json:"next_reason,omitempty"` // why, when it is not the top rung
	Extra       json.RawMessage `json:"extra,omitempty"`       // extra_usage object verbatim, if present
}

// TokenSource returns the OAuth access token. Swappable for tests.
type TokenSource func(ctx context.Context) (string, error)

// KeychainToken reads Claude Code's credentials from the login Keychain.
func KeychainToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", keychainItem, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain %q: %w", keychainItem, err)
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &creds); err != nil {
		return "", fmt.Errorf("keychain item is not Claude Code JSON: %w", err)
	}
	if creds.OAuth.AccessToken == "" {
		return "", errors.New("no accessToken in Keychain item (not logged in via claude.ai?)")
	}
	if creds.OAuth.ExpiresAt > 0 && time.UnixMilli(creds.OAuth.ExpiresAt).Before(time.Now()) {
		return "", errors.New("access token expired; run any `claude` command to refresh it")
	}
	return creds.OAuth.AccessToken, nil
}

// QuotaFetcher fetches and caches the plan windows.
type QuotaFetcher struct {
	Token  TokenSource
	Client *http.Client
	URL    string

	// Record stores one fetch's readings (cmd/hub writes them to the
	// observations table); Past reads them back. Both optional: without them
	// the windows just have no measured pace.
	Record func(at time.Time, ws []Window)
	Past   func(since time.Time) []Sample

	mu           sync.Mutex // guards the fields below
	cachedAt     time.Time
	cachedRaw    map[string]json.RawMessage
	cachedErr    error
	lastRecorded time.Time
	refreshing   bool
}

// staleTTL is how old a reading may be and still be shown at once while a
// fresh one is fetched behind the request. The hub samples every 10 min
// anyway, so a reading older than this means the sampler is not running and
// the request should wait for a real one.
const staleTTL = 15 * time.Minute

func NewQuotaFetcher() *QuotaFetcher {
	return &QuotaFetcher{Token: KeychainToken, Client: &http.Client{Timeout: 15 * time.Second}, URL: usageURL}
}

// raw returns the decoded top-level usage object, cached for windowTTL.
func (q *QuotaFetcher) raw(ctx context.Context) (map[string]json.RawMessage, error) {
	raw, _, err := q.rawAt(ctx)
	return raw, err
}

// rawAt is raw plus when that reading was fetched. Past windowTTL a reading
// younger than staleTTL is returned as it is and refreshed behind the
// request (one refresh at a time): the Spend page and the model picker never
// wait on Anthropic unless the hub has nothing at all to show.
func (q *QuotaFetcher) rawAt(ctx context.Context) (map[string]json.RawMessage, time.Time, error) {
	q.mu.Lock()
	age := time.Since(q.cachedAt)
	if age < windowTTL && (q.cachedRaw != nil || q.cachedErr != nil) {
		defer q.mu.Unlock()
		return q.cachedRaw, q.cachedAt, q.cachedErr
	}
	if q.cachedRaw != nil && age < staleTTL {
		if !q.refreshing {
			q.refreshing = true
			go q.refresh(context.Background())
		}
		defer q.mu.Unlock()
		return q.cachedRaw, q.cachedAt, nil
	}
	q.mu.Unlock()
	return q.refresh(ctx)
}

// refresh fetches once and installs the result.
func (q *QuotaFetcher) refresh(ctx context.Context) (map[string]json.RawMessage, time.Time, error) {
	// Detach from the caller's context: the app cancels in-flight requests
	// when a view goes away, and caching that "context canceled" for the TTL
	// made the Spend tab say "Plan usage unavailable".
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	raw, err := q.fetch(fctx)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.refreshing = false
	if err != nil && q.cachedRaw != nil {
		// Keep serving the last good answer over a transient failure.
		return q.cachedRaw, q.cachedAt, nil
	}
	q.cachedRaw, q.cachedErr = raw, err
	q.cachedAt = time.Now()
	return q.cachedRaw, q.cachedAt, q.cachedErr
}

// claimRecord reports whether a reading fetched at `at` is newer than the last
// one stored, and marks it stored.
func (q *QuotaFetcher) claimRecord(at time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if at.IsZero() || !at.After(q.lastRecorded) {
		return false
	}
	q.lastRecorded = at
	return true
}

func (q *QuotaFetcher) fetch(ctx context.Context) (map[string]json.RawMessage, error) {
	tok, err := q.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", q.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "life-hub/1 (claude-code usage mirror)")
	body, err := httpx.Bytes(q.Client, req, 1<<20)
	var se *httpx.StatusError
	if errors.As(err, &se) {
		return nil, fmt.Errorf("usage endpoint HTTP %d: %s", se.Code, truncate(se.Body, 200))
	}
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("usage endpoint: bad JSON: %w", err)
	}
	return m, nil
}

// windowsFrom returns every usable bucket in a raw usage payload:
//   - the top-level {utilization, resets_at} objects (five_hour, seven_day…)
//   - the per-model buckets in the "limits" array (this is the only place a scoped limit like "Fable weekly 100%" is reported; the
//     legacy top-level seven_day_<model> keys are all null now, so a hub
//     that reads only those thinks everything is fine while every Fable run
//     dies.)
//
// Both the Spend tab and the model ladder go through here so they can never
// disagree about which rung is closed.
func windowsFrom(raw map[string]json.RawMessage) []Window {
	out := []Window{}
	seen := map[string]bool{}
	for key, v := range raw {
		if w, ok := parseWindow(key, v); ok {
			out = append(out, w)
			seen[w.Key] = true
		}
	}
	for _, w := range scopedWindows(raw["limits"]) {
		if !seen[w.Key] {
			out = append(out, w)
			seen[w.Key] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return windowRank(out[i].Key) < windowRank(out[j].Key) })
	return out
}

// limitEntry is one element of the "limits" array.
type limitEntry struct {
	Kind     string   `json:"kind"`     // session | weekly_all | weekly_scoped
	Group    string   `json:"group"`    // session | weekly
	Percent  *float64 `json:"percent"`  // 0..100
	Severity string   `json:"severity"` // normal | warning | critical
	ResetsAt string   `json:"resets_at"`
	IsActive bool     `json:"is_active"`
	Scope    *struct {
		Model *struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Surface json.RawMessage `json:"surface"`
	} `json:"scope"`
}

// scopedWindows turns model-scoped entries of "limits" into Windows keyed
// like the legacy buckets (weekly + Fable → "seven_day_fable") so the
// ladder's family matching and the Spend tab both understand them.
func scopedWindows(raw json.RawMessage) []Window {
	if len(raw) == 0 {
		return nil
	}
	var entries []limitEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	var out []Window
	for _, e := range entries {
		if e.Percent == nil || e.Scope == nil || e.Scope.Model == nil {
			continue // unscoped: already covered by the top-level buckets
		}
		fam := Family(strings.ToLower(strings.ReplaceAll(e.Scope.Model.DisplayName, " ", "-")))
		if fam == "" {
			continue
		}
		base := sevenDayKey
		if e.Group == "session" || strings.HasPrefix(e.Kind, "session") {
			base = fiveHourKey
		}
		w := Window{Key: base + "_" + fam, ScopeModel: fam, Utilization: *e.Percent, ByModel: []Bucket{}}
		if t, err := time.Parse(time.RFC3339, e.ResetsAt); err == nil {
			w.ResetsAt = t
		}
		w.Label, w.Note = windowLabel(w.Key), windowNote(w.Key)
		if d := windowLength(w.Key); d > 0 && !w.ResetsAt.IsZero() {
			w.StartsAt = w.ResetsAt.Add(-d)
		}
		out = append(out, w)
	}
	return out
}

// Quota joins the remote windows with local usage. us must be the full
// usage list (it filters by window itself).
func (q *QuotaFetcher) Quota(ctx context.Context, us []Usage, now time.Time) Quota {
	out := Quota{GeneratedAt: now, Windows: []Window{}}
	raw, fetchedAt, err := q.rawAt(ctx)
	out.FetchedAt = fetchedAt
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Available = true
	if x, ok := raw["extra_usage"]; ok && string(x) != "null" {
		out.Extra = x
	}
	ws := windowsFrom(raw)
	locked := map[string]bool{}
	for _, w := range ws {
		if w.ScopeModel != "" && w.Utilization >= 100 {
			locked[w.ScopeModel] = true
		}
	}
	// Keep this reading: one row per meter per fetch, so tomorrow's session can
	// see whether the bar moved instead of guessing from list-price dollars.
	if q.Record != nil && q.claimRecord(out.FetchedAt) {
		q.Record(out.FetchedAt, ws)
	}
	var past []Sample
	if q.Past != nil {
		past = q.Past(now.Add(-measuredSpan))
	}
	for _, w := range ws {
		fillLocal(&w, us, now, locked)
		fillMeasured(&w, past, now)
		fillWords(&w, now)
		out.Windows = append(out.Windows, w)
	}
	return out
}

// parseWindow accepts any object shaped {utilization, resets_at}; unknown
// keys (new model-specific buckets) still show up, labelled from the key.
func parseWindow(key string, v json.RawMessage) (Window, bool) {
	var o struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	if err := json.Unmarshal(v, &o); err != nil || o.Utilization == nil {
		return Window{}, false
	}
	w := Window{Key: key, ScopeModel: scopeFamily(key), Utilization: *o.Utilization, ByModel: []Bucket{}}
	if o.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339, o.ResetsAt); err == nil {
			w.ResetsAt = t
		}
	}
	// Internal/feature-flag buckets show up as {utilization:0, resets_at:null}
	// with an opaque key; they carry no information, so drop them.
	if w.ResetsAt.IsZero() && w.Utilization == 0 && windowLength(key) == 0 {
		return Window{}, false
	}
	w.Label, w.Note = windowLabel(key), windowNote(key)
	if d := windowLength(key); d > 0 && !w.ResetsAt.IsZero() {
		w.StartsAt = w.ResetsAt.Add(-d)
	}
	return w, true
}

// scopeFamily returns the model family a bucket key is scoped to, or "".
func scopeFamily(key string) string {
	for _, base := range []string{fiveHourKey + "_", sevenDayKey + "_", "one_day_"} {
		if rest := strings.TrimPrefix(key, base); rest != key && rest != "" {
			if f := Family(rest); f == rest {
				return f
			}
		}
	}
	return ""
}

func windowLength(key string) time.Duration {
	switch {
	case strings.HasPrefix(key, "five_hour"):
		return 5 * time.Hour
	case strings.HasPrefix(key, "seven_day"):
		return 7 * 24 * time.Hour
	case strings.HasPrefix(key, "one_day"):
		return 24 * time.Hour
	case strings.HasPrefix(key, "monthly"):
		return maxWindowDays * 24 * time.Hour
	}
	return 0
}

// windowBase is the window's length in words, without any model scope.
func windowBase(key string) string {
	base, _ := windowParts(key)
	return base
}

// windowParts splits a bucket key into its length ("7 days") and the model it
// is scoped to ("fable", "" when the bucket covers every model). known is
// false for a bucket shape we do not recognise.
func windowParts(key string) (base, scope string) {
	switch {
	case strings.HasPrefix(key, "five_hour"):
		base, scope = "5 hours", strings.TrimPrefix(key, "five_hour")
	case strings.HasPrefix(key, "seven_day"):
		base, scope = "7 days", strings.TrimPrefix(key, "seven_day")
	case strings.HasPrefix(key, "one_day"):
		base, scope = "24 hours", strings.TrimPrefix(key, "one_day")
	default:
		return strings.ReplaceAll(key, "_", " "), ""
	}
	return base, strings.Trim(scope, "_")
}

func windowLabel(key string) string {
	base, rest := windowParts(key)
	if rest == "" {
		// An unlabelled "7 days" next to "Fable · 7 days"
		// reads as if one contains the other; say whose spend fills it.
		if base != strings.ReplaceAll(key, "_", " ") {
			return "All models · " + base
		}
		return base
	}
	// A scoped bucket is really a per-model meter, so lead with the model:
	// "Fable · 7 days" reads as one, "7 days · Fable" reads as a subsection
	// of the 7-day bar.
	return strings.ToUpper(rest[:1]) + strings.ReplaceAll(rest[1:], "_", " ") + " · " + base
}

// windowNote says in one line whose spend fills a bucket. The two 7-day bars
// look identical otherwise — same units, same reset — and the all-models
// one reads as a subtotal of the Fable one.
func windowNote(key string) string {
	base, rest := windowParts(key)
	if base == strings.ReplaceAll(key, "_", " ") && rest == "" {
		return ""
	}
	if rest == "" {
		return "your total for the window — every model counts toward it"
	}
	name := strings.ToUpper(rest[:1]) + strings.ReplaceAll(rest[1:], "_", " ")
	return "only " + name + " counts toward it — a separate cap from the all-models bar"
}

func windowRank(key string) int {
	switch key {
	case fiveHourKey:
		return 0
	case sevenDayKey:
		return 1
	}
	return 2
}

// fillLocal sums local transcript usage inside [StartsAt, now]. For the
// five_hour bucket Anthropic's resets_at is the end of the *current* window,
// so resets_at-5h is the right start; if resets_at is missing we fall back
// to a trailing window of the same length.
// lockedFams is the set of families whose own scoped meter is already full;
// they cannot add to a shared meter any more, so they are left out of its
// burn rate.
func fillLocal(w *Window, us []Usage, now time.Time, lockedFams map[string]bool) {
	start := w.StartsAt
	if start.IsZero() {
		if d := windowLength(w.Key); d > 0 {
			start = now.Add(-d)
		} else {
			return
		}
	}
	// A scoped bucket (seven_day_fable) is a limit on ONE family: counting
	// every model's spend in it would misreport what filled it.
	fam := w.ScopeModel
	byModel := map[string]*Bucket{}
	for _, u := range us {
		if u.TS.Before(start) || u.TS.After(now) {
			continue
		}
		if fam != "" && Family(u.Model) != fam {
			continue
		}
		usd, _ := u.Cost()
		b := byModel[u.Model]
		if b == nil {
			b = &Bucket{Key: u.Model}
			byModel[u.Model] = b
		}
		b.USD += usd
		b.Messages++
		b.Input += u.Input
		b.Output += u.Output
		b.CacheRead += u.CacheRead
		b.CacheWrite += u.CacheWrite5m + u.CacheWrite1h
		w.SpentUSD += usd
		w.Messages++
	}
	w.ByModel = flatten(byModel, func(a, b *Bucket) bool { return a.USD > b.USD })
	if w.Utilization > 0 && w.Utilization < 100 {
		w.HeadroomUSD = w.SpentUSD * (100 - w.Utilization) / w.Utilization
	}
	fillBurn(w, us, now, start, lockedFams)
	fillTypical(w, us, now, lockedFams)
}

// typicalSpan is "the pace I have actually been running at lately". A day is
// the shortest span that averages over the owner sleeping, a meeting, and an
// evening of ten parallel threads.
const typicalSpan = 24 * time.Hour

// fillTypical is fillBurn's slow twin: the same $→% exchange rate, but over
// the trailing day instead of the last 45 minutes. The 45-minute number is
// honest about *this instant* and useless as a forecast — a lull makes it
// read 0.0%/hr, i.e. "you will never fill this", right after an hour of hard
// use.
func fillTypical(w *Window, us []Usage, now time.Time, lockedFams map[string]bool) {
	if w.SpentUSD <= 0 || w.Utilization <= 0 || w.Utilization >= 100 {
		return
	}
	from := now.Add(-typicalSpan)
	var recent float64
	for _, u := range us {
		if u.TS.Before(from) || u.TS.After(now) {
			continue
		}
		fam := Family(u.Model)
		if w.ScopeModel != "" && fam != w.ScopeModel {
			continue
		}
		if w.ScopeModel == "" && lockedFams[fam] {
			continue // locked out: that spend cannot repeat
		}
		usd, _ := u.Cost()
		recent += usd
	}
	if recent <= 0 {
		return
	}
	w.TypicalPctHour = recent / w.SpentUSD * w.Utilization / typicalSpan.Hours()
	if h := (100 - w.Utilization) / w.TypicalPctHour; h < maxWindowDays*24 {
		w.FullAtTypical = now.Add(time.Duration(h * float64(time.Hour)))
	}
}

// measuredSpan is how far back we look for stored readings, and measuredMin
// the shortest Δt worth dividing by (a 1%-resolution number over 5 minutes is
// noise, not a rate).
const (
	measuredSpan = 8 * time.Hour
	measuredMin  = 20 * time.Minute
)

// fillMeasured derives the one number that is not an estimate: how much
// Anthropic's own percentage moved between the oldest reading we kept and
// right now. Readings from a previous generation of the window (different
// resets_at) are dropped — otherwise a reset reads as the meter running
// backwards.
func fillMeasured(w *Window, past []Sample, now time.Time) {
	oldest := Sample{Key: w.Key, TS: now, Percent: w.Utilization}
	for _, s := range past {
		if s.Key != w.Key || s.TS.After(now) || s.TS.Before(now.Add(-measuredSpan)) {
			continue
		}
		if !sameGeneration(w, s) {
			continue
		}
		if s.TS.Before(oldest.TS) {
			oldest = s
		}
	}
	span := now.Sub(oldest.TS)
	if span < measuredMin {
		return
	}
	delta := w.Utilization - oldest.Percent
	if delta < 0 {
		return // window rolled over between the readings
	}
	w.MeasuredHours, w.MeasuredDelta = span.Hours(), delta
	w.MeasuredPctHour = delta / span.Hours()
}

// resetsSlack absorbs the jitter in Anthropic's resets_at. It is recomputed
// per request, so two readings of the SAME generation come back as
// 23:59:59.813122Z and 00:00:00.173095Z — never Equal. Consecutive
// generations are a whole window apart (5 h, or 7 d), so a minute of slack
// cannot confuse one for the next.
const resetsSlack = time.Minute

// sameGeneration reports whether a stored reading belongs to the window that
// is running right now. It has to: the meter restarts at 0, so anchoring on a
// reading from the previous generation turns "how far did the bar actually
// move" into a number spanning a reset. The five-hour card said "+2% in the
// last 6.8 h" while also saying "resets in 3 hr, 13 min" — a window only 1.8 h
// old — because one reading stored with an unknown resets_at was exempted from
// the check and anchored the whole measurement 6.8 h back.
//
// Comparing the timestamp against the window's own start is what actually
// decides it; resets_at is the backstop for when the start is unknown. That
// also revives the seven-day cards, which had gone silent: exact-equality on a
// jittering resets_at was rejecting every same-generation reading they had.
func sameGeneration(w *Window, s Sample) bool {
	// A reading taken before this window opened is a previous generation by
	// definition — including the ones stored before resets_at was known.
	if !w.StartsAt.IsZero() && s.TS.Before(w.StartsAt) {
		return false
	}
	if w.ResetsAt.IsZero() || s.ResetsAt.IsZero() {
		// Unverifiable. Only the start check above can vouch for it, and with
		// no start either we say nothing rather than risk spanning a reset.
		return !w.StartsAt.IsZero()
	}
	d := s.ResetsAt.Sub(w.ResetsAt)
	return d < resetsSlack && d > -resetsSlack
}

// recentSpan is how far back "the rate I am going right now" looks. Short
// enough to react to a burst of parallel threads, long enough that one idle
// gap does not read as "not filling".
const recentSpan = 45 * time.Minute

// fillBurn estimates how fast the meter is filling and when it hits 100%.
// Anthropic gives us a percentage but no rate, so we convert local list-price
// spend into percent: the window's own spend is worth its own utilization, so
// $1 ≈ utilization/spent percent. Recent spend at that exchange rate is the
// current burn; if there is none we fall back to the window's average pace.
//
// A family that has hit its own scoped limit is excluded from the recent spend
// on a shared meter: it is locked out, so the burst it made just before the
// lock is not a pace anything can keep up (with Fable locked the 7-day bar
// would otherwise say "full in 23 hr", counting Fable spend that could
// never happen again).
func fillBurn(w *Window, us []Usage, now, start time.Time, lockedFams map[string]bool) {
	elapsed := now.Sub(start).Hours()
	if elapsed <= 0 || w.Utilization <= 0 {
		return
	}
	rate := w.Utilization / elapsed // average since the window opened
	if w.SpentUSD > 0 {
		from := now.Add(-recentSpan)
		if from.Before(start) {
			from = start
		}
		if hrs := now.Sub(from).Hours(); hrs > 0 {
			var recent float64
			for _, u := range us {
				if u.TS.Before(from) || u.TS.After(now) {
					continue
				}
				fam := Family(u.Model)
				if w.ScopeModel != "" && fam != w.ScopeModel {
					continue
				}
				if w.ScopeModel == "" && lockedFams[fam] {
					continue
				}
				usd, _ := u.Cost()
				recent += usd
			}
			if recent > 0 {
				rate = recent / w.SpentUSD * w.Utilization / hrs
			}
		}
	}
	w.BurnPctHour = rate
	left := 100 - w.Utilization
	if rate <= 0 || left <= 0 {
		return
	}
	if h := left / rate; h < maxWindowDays*24 {
		w.FullAt = now.Add(time.Duration(h * float64(time.Hour)))
	}
}

// eastern is the wall clock the reset/fill times are written in — the hub's, not
// the laptop's or the phone's.
var eastern = func() *time.Location {
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		return loc
	}
	return time.Local
}()

// fillWords sets the tone, outlook and foot line of a window from its numbers.
//
// The outlook is the tick on the bar: over by reset iff the
// meter is ahead of the share of the window gone, else the window's own
// average pace carried to the reset. Not the trailing day — a heavy weekend
// said "over" at 60% with 70% of the week gone, and weekdays run lighter.
func fillWords(w *Window, now time.Time) {
	if !w.ResetsAt.IsZero() {
		if d := windowLength(w.Key); d > 0 {
			gone := 100 * (1 - w.ResetsAt.Sub(now).Hours()/d.Hours())
			w.ElapsedPct = math.Max(0, math.Min(100, gone))
		}
	}
	over := false
	switch {
	case w.Utilization >= 100:
		w.Outlook = "locked"
	case w.ElapsedPct > 0 && w.Utilization > 0:
		if w.Utilization > w.ElapsedPct {
			over = true
			w.Outlook = "over by reset"
		} else {
			w.Outlook = fmt.Sprintf("≈%.0f%% by reset", 100*w.Utilization/w.ElapsedPct)
		}
	default:
		w.Outlook = "idle"
	}
	switch {
	case w.Utilization >= 100:
		w.Tone = "bad"
	case w.Utilization >= 85 || over:
		w.Tone = "warn"
	}
	// The foot is the clock, not the money: how long until
	// the reset, how much of the window has run, and when. Read against the
	// big percentage it answers "am I ahead of the week or behind it".
	if w.ResetsAt.IsZero() {
		w.Foot = "reset time unknown"
		return
	}
	parts := []string{leftWords(w.ResetsAt.Sub(now)) + " left"}
	if windowLength(w.Key) > 0 {
		parts = append(parts, fmt.Sprintf("%.0f%% of %s gone", w.ElapsedPct, spanName(w.Key)))
	}
	parts = append(parts, "resets "+clockWhen(w.ResetsAt, now))
	w.Foot = strings.Join(parts, " · ")
}

// spanName is the window in the words people use for it: "the week", "the day",
// "the 5 hours".
func spanName(key string) string {
	switch windowBase(key) {
	case "7 days":
		return "the week"
	case "24 hours":
		return "the day"
	case "5 hours":
		return "the 5 hours"
	}
	return "the window"
}

// leftWords is "4 d 2 h", "3 h 48 min", "12 min" — the two largest units,
// zeros dropped, never less than a minute.
func leftWords(d time.Duration) string {
	if d < time.Minute {
		return "0 min"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	mins := int(d/time.Minute) % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d d %d h", days, hours)
	case days > 0:
		return fmt.Sprintf("%d d", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%d h %d min", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d h", hours)
	}
	return fmt.Sprintf("%d min", mins)
}

// clockWhen is "3:33 AM" today and "Sep 15 3:33 AM" otherwise, Eastern.
func clockWhen(t, now time.Time) string {
	t, now = t.In(eastern), now.In(eastern)
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return t.Format("3:04 PM")
	}
	return t.Format("Jan 2 3:04 PM")
}

func truncate(s string, n int) string { return format.Truncate(s, n) }
