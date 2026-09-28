package server

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"life/hub/internal/obs"
	"life/hub/internal/syncruns"
)

// Data-source inventory: every connected source and how it is stored. A
// static catalog of where data comes from, joined with live facts
// from the observations table (counts, first/last) and each syncer's run log,
// so the page never claims access from memory. Anything with no rows and no
// runs is left out.

type SourceKind struct {
	Kind  string    `json:"kind"`
	N     int       `json:"n"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
	Note  string    `json:"note,omitempty"` // what ONE row of this kind is
}

// kindNotes says what a single row means, for kinds where the row count would
// otherwise read as a thing count.
var kindNotes = map[string]string{
	"health/steps":     "one row per day — the day's total",
	"health/sleep":     "one row per night, dated by the morning you woke up: minutes in bed, asleep, and per stage",
	"health/workout":   "one row per workout Health recorded",
	"health/weight_kg": "one row per day — the day's average of every weigh-in",
}

// SourceAccount is one thing a source is connected to. `via` is the lane it
// arrives through and the console groups a section's accounts by it.
type SourceAccount struct {
	Label  string     `json:"label"`
	Via    string     `json:"via,omitempty"` // API | phone
	Detail string     `json:"detail,omitempty"`
	URL    string     `json:"url,omitempty"`
	N      int        `json:"n,omitempty"`
	Last   *time.Time `json:"last,omitempty"`
}

type Source struct {
	ID      string     `json:"id"`      // obs source value
	Title   string     `json:"title"`   // "Apple Health"
	From    string     `json:"from"`    // how it gets in
	Storage string     `json:"storage"` // where it lives
	Status  string     `json:"status"`  // connected | failing | live | manual
	Last    *time.Time `json:"last,omitempty"`
	// Whether the connection WORKS, which "connected" and `last` cannot say
	// (2026-09-01): a credential can be present and every call still be refused,
	// and `last` is the newest row of any kind.
	Error    string          `json:"error,omitempty"`         // the newest failure, if it is still failing
	LastOK   *time.Time      `json:"last_ok,omitempty"`       // the last tick that actually worked
	Since    *time.Time      `json:"failing_since,omitempty"` // when the current run of failures began
	Fails    int             `json:"fails,omitempty"`         // how many ticks it has wasted since
	Total    int             `json:"total"`
	Kinds    []SourceKind    `json:"kinds"`
	Accounts []SourceAccount `json:"accounts"`
}

type SourceGroup struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Blurb   string   `json:"blurb"`
	Note    string   `json:"note,omitempty"` // live facts about the group's lanes
	Sources []Source `json:"sources"`
}

type SourcesView struct {
	Groups []SourceGroup `json:"groups"`
}

const obsStore = "observations table in data/life.db (SQLite, append-only)"

// catalog: the static part. A catalog entry with no rows and no runs is not
// shown. Sources that land rows but are not in the catalog are listed under
// "Other" so nothing the hub holds is invisible.
var sourceCatalog = []SourceGroup{
	{ID: "health", Title: "Health", Blurb: "Apple Health syncs itself from the phone.", Sources: []Source{
		{ID: "health", Title: "Apple Health", From: "The app reads HealthKit whenever it is open (Sources › Apple Health) — everything Health already holds on the first sync, then the last few days on every launch", Storage: obsStore + ": one row per metric per day (steps, distance, energy, exercise, heart rate, resting HR, HRV, VO2max, weight, body fat), one per night of sleep, one per workout"},
	}},
	{ID: "phone", Title: "From the phone", Blurb: "What you send the app directly.", Sources: []Source{
		{ID: "app", Title: "Life app", From: "Photos, screenshots, voice notes, meals and notes sent to a session", Storage: obsStore + ": photo, voice, meal, note; files as blobs"},
	}},
}

// hidden: observation sources the hub writes about itself, not data sources.
var hiddenSources = map[string]bool{"spend": true}

// applyRuns: a row whose syncer (a clock ck.Sync task named like the source)
// has runs says whether its newest one worked, and when it last did.
func applyRuns(src *Source, runs map[string]syncruns.Health) {
	h, ok := runs[src.ID]
	if !ok {
		return
	}
	src.Status = "connected"
	src.LastOK = when(h.LastOK)
	if h.Error != "" {
		src.Status, src.Error, src.Since, src.Fails = "failing", h.Error, when(h.Since), h.Fails
	}
}

func when(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func later(dst **time.Time, t time.Time) {
	if t.After(time.Now()) { // future-dated rows read as now
		t = time.Now()
	}
	if *dst == nil || t.After(**dst) {
		tt := t
		*dst = &tt
	}
}

// fill joins the live counts onto a source: kinds, total, newest row.
func fill(src *Source, cs []obs.Count) {
	if src.Kinds == nil {
		src.Kinds = []SourceKind{}
	}
	for _, c := range cs {
		src.Kinds = append(src.Kinds, SourceKind{Kind: c.Kind, N: c.N, First: c.First, Last: c.Last, Note: kindNotes[c.Source+"/"+c.Kind]})
		src.Total += c.N
		later(&src.Last, c.Last)
	}
	sort.Slice(src.Kinds, func(i, j int) bool { return src.Kinds[i].N > src.Kinds[j].N })
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	counts, err := s.obs.Counts()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	bySource := map[string][]obs.Count{}
	for _, c := range counts {
		bySource[c.Source] = append(bySource[c.Source], c)
	}

	var runs map[string]syncruns.Health
	if s.Runs != nil {
		if runs, err = s.Runs.Health(); err != nil {
			log.Printf("sources: sync_runs: %v", err) // the page still answers, without run health
		}
	}

	view := SourcesView{}
	known := map[string]bool{}
	for _, g := range sourceCatalog {
		out := SourceGroup{ID: g.ID, Title: g.Title, Blurb: g.Blurb, Sources: []Source{}}
		for _, src := range g.Sources {
			known[src.ID] = true
			src.Accounts = []SourceAccount{}
			fill(&src, bySource[src.ID])
			src.Status = "live"
			if src.Total > 0 {
				src.Accounts = append(src.Accounts, SourceAccount{Label: "iPhone (Life app)", Via: "phone", Last: src.Last})
			}
			applyRuns(&src, runs)
			if src.Total == 0 && src.Status != "connected" && src.Status != "failing" {
				continue // not connected, nothing kept: not on the page
			}
			out.Sources = append(out.Sources, src)
		}
		view.Groups = append(view.Groups, out)
	}
	other := SourceGroup{ID: "other", Title: "Other", Blurb: "Everything else that has landed rows.", Sources: []Source{}}
	for id, cs := range bySource {
		if known[id] || hiddenSources[id] {
			continue
		}
		src := Source{ID: id, Title: id, From: "posted to /api/v1/observations", Storage: obsStore, Status: "live", Accounts: []SourceAccount{}}
		fill(&src, cs)
		applyRuns(&src, runs)
		other.Sources = append(other.Sources, src)
	}
	if len(other.Sources) > 0 {
		view.Groups = append(view.Groups, other)
	}
	sortSources(view.Groups)
	writeJSON(w, 200, view)
}

// The page is a list to look something up in, not a ranking. So: rows are alphabetical by title, and so are
// the lines under a row WHEN THEY CARRY NO COUNT; where a count is shown
// biggest-first is a real order and stays.
func sortSources(groups []SourceGroup) {
	for gi := range groups {
		g := &groups[gi]
		sort.Slice(g.Sources, func(i, j int) bool {
			return strings.ToLower(g.Sources[i].Title) < strings.ToLower(g.Sources[j].Title)
		})
		for si := range g.Sources {
			acc := g.Sources[si].Accounts
			ranked := false
			for _, a := range acc {
				if a.N > 0 {
					ranked = true
					break
				}
			}
			if ranked {
				continue
			}
			sort.Slice(acc, func(i, j int) bool {
				return strings.ToLower(acc[i].Label) < strings.ToLower(acc[j].Label)
			})
		}
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmtN(n) + " " + word + "s"
}

func fmtN(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	s := fmt.Sprint(n)
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	return s + "," + strings.Join(out, ",")
}
