// Sessions, headless jobs, spend, the scheduler and its runs.
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"life/hub/internal/budget"
	"life/hub/internal/spend"
)

// usages is every transcript under claude_projects_dir, from the incremental
// cache: a stat walk + the changed files, served stale past 30 s while it
// refreshes behind the request (2026-08-28: the full reparse was 2.5–5.6 s
// and the Spend page paid it on every cache miss).
func (s *Server) usages() ([]spend.Usage, error) { return s.usage.Usages() }

// UseUsage hands the server the one transcript cache: main builds it with its
// on-disk store and the model picker reads the same one, so a cold quota
// request does not walk the tree once for each (2026-09-25).
func (s *Server) UseUsage(c *spend.Cache) { s.usage = c }

// WarmSpend parses the transcripts once at boot, in the background, so the
// first Spend request after a restart does not wait for the walk.
func (s *Server) WarmSpend() { s.usage.Prime() }

func (s *Server) resolveProject(cwd string) string {
	best := ""
	bestLen := 0
	for _, p := range s.cfg.AllProjects() {
		if (cwd == p.Dir || strings.HasPrefix(cwd, p.Dir+"/")) && len(p.Dir) > bestLen {
			best, bestLen = p.Name, len(p.Dir)
		}
	}
	return best
}

func (s *Server) spendSummary(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	// `hours` wins over `days`: the Spend tab's shortest range is 5 hours, to
	// match the five_hour plan meter, and no whole number of days says that.
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if days <= 0 || days > 3650 {
		days = 30
	}
	if hours > 24*3650 {
		hours = 0
	}
	us, err := s.usages()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	now := time.Now()
	var sum spend.Summary
	if hours > 0 {
		sum = spend.SummarizeWindow(us, time.Duration(hours)*time.Hour, now, s.resolveProject)
	} else {
		sum = spend.Summarize(us, days, now, s.resolveProject)
	}
	// The hub's own split of the same window: thread turns by what woke them,
	// job runs by name — "what do the check-ins cost?" as a number.
	since := now.Add(-time.Duration(sum.WindowHours * float64(time.Hour)))
	sum.ByTrigger = s.thr.CostByTrigger(since)
	sum.Jobs = s.sch.CostByJob(since)
	writeJSON(w, 200, sum)
}

// UseQuota swaps in a shared QuotaFetcher (the model ladder uses the same one).
func (s *Server) UseQuota(q *spend.QuotaFetcher) { s.quota = q }

// UsePicker lets the Spend tab report which rung new sessions will run on.
func (s *Server) UsePicker(p *spend.Picker) { s.picker = p }

// UseBudget wires the daily budget guard (GET/POST /spend/budget…).
func (s *Server) UseBudget(g *budget.Guard) { s.budget = g }

func (s *Server) spendBudget(w http.ResponseWriter, r *http.Request) {
	if s.budget == nil {
		writeJSON(w, 200, budget.State{Level: "off", Unattended: true, Note: "no budget guard configured"})
		return
	}
	writeJSON(w, 200, s.budget.State())
}

func (s *Server) spendBudgetClear(w http.ResponseWriter, r *http.Request) {
	if s.budget == nil {
		jsonErr(w, 409, "no budget guard configured")
		return
	}
	writeJSON(w, 200, s.budget.Clear())
}

func (s *Server) spendQuota(w http.ResponseWriter, r *http.Request) {
	us, err := s.usages()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	now := time.Now()
	q := s.quota.Quota(r.Context(), us, now)
	if s.picker != nil {
		q.NextModel, q.NextReason = s.picker.Explain(r.Context(), now)
		// The fable day cap outranks the plan meters: past it the next
		// session starts a rung down whatever the buckets say, so the card
		// must say so too, or the Spend tab promises fable and the hub runs
		// opus.
		if floor := s.budget.Floor(); floor != "" {
			if m := s.picker.NotAbove(q.NextModel, floor); m != q.NextModel {
				st := s.budget.State()
				q.NextModel = m
				q.NextReason = fmt.Sprintf("fable $%.0f of $%.0f today — back at midnight ET", st.FableSpentUSD, st.FableCapUSD)
			}
		}
	}
	writeJSON(w, 200, q)
}

// modelChoices: every rung of the ladder, top first, so the Spend page can
// show which one is used, which are blocked, and let the owner pin an open one.
func (s *Server) modelChoices() []string {
	if s.picker != nil && len(s.picker.Ladder) > 0 {
		return s.picker.Ladder
	}
	return spend.DefaultLadder
}

// modelRung is one step of the Spend page's ladder: open, or closed with the
// one bucket (or the fable day cap) that shuts it.
type modelRung struct {
	Model string `json:"model"`
	Open  bool   `json:"open"`
	Why   string `json:"why"`
}

// modelSetting: the Spend page's ladder. `default_model` is what a new
// session will be PINNED to; `starts_on`/`reason` are what the picker would
// actually hand it right now, which can be a rung lower when a bucket is full;
// `rungs` says for every rung whether a session could start there now.
func (s *Server) modelSetting(ctx context.Context) map[string]any {
	set := s.thr.DefaultModel()
	choices := s.modelChoices()
	starts, reason := "", ""
	rungs := []modelRung{}
	if s.picker == nil {
		for _, m := range choices {
			rungs = append(rungs, modelRung{Model: m, Open: true})
		}
	} else {
		now := time.Now()
		// From the pinned rung when there is one: threads.go stamps `set` into
		// the new thread and the picker only steps DOWN from it, so explaining
		// the top-of-ladder pick claimed the ladder was overruling the toggle
		// when it was not.
		starts, reason = s.picker.ExplainFrom(ctx, now, set)
		if starts == "" {
			starts = choices[0]
		}
		// The fable day cap starts every session a rung down whatever the
		// buckets say (see spendQuota).
		floor, capWhy := "", ""
		if s.budget != nil {
			if floor = s.budget.Floor(); floor != "" {
				st := s.budget.State()
				capWhy = fmt.Sprintf("fable $%.0f of $%.0f today — back at midnight ET", st.FableSpentUSD, st.FableCapUSD)
				if m := s.picker.NotAbove(starts, floor); m != starts {
					starts, reason = m, capWhy
				}
			}
		}
		for _, m := range choices {
			got, why := s.picker.ExplainFrom(ctx, now, m)
			r := modelRung{Model: m, Open: got == m}
			if !r.Open {
				r.Why, _, _ = strings.Cut(why, "; ") // its own bucket, not the ones below it
			} else if floor != "" && s.picker.NotAbove(m, floor) != m {
				r.Open, r.Why = false, capWhy
			}
			rungs = append(rungs, r)
		}
	}
	shown := set
	if shown == "" {
		// Never set: show the truth rather than a guess — the toggle sits on
		// whatever a new session would start on today. Nothing is written
		// until the owner taps.
		shown = starts
	}
	return map[string]any{"default_model": shown, "explicit": set != "", "starts_on": starts, "reason": reason,
		"options": choices, "rungs": rungs}
}

func (s *Server) spendModel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.modelSetting(r.Context()))
}

func (s *Server) setSpendModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DefaultModel string `json:"default_model"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if in.DefaultModel != "" {
		ok := false
		for _, m := range s.modelChoices() {
			ok = ok || m == in.DefaultModel
		}
		if !ok {
			jsonErr(w, 400, "default_model must be one of "+strings.Join(s.modelChoices(), ", "))
			return
		}
	}
	if err := s.thr.SetDefaultModel(in.DefaultModel); err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	log.Printf("spend: default model → %q", in.DefaultModel)
	writeJSON(w, 200, s.modelSetting(r.Context()))
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	ss, err := s.sess.List()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ss)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	p, ok := s.cfg.Project(in.Project)
	if !ok {
		jsonErr(w, 404, "unknown project")
		return
	}
	sess, created, err := s.sess.StartRemoteControl(p.Name, p.Dir)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	code := 200
	if created {
		code = 201
	}
	writeJSON(w, code, sess)
}

func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	if err := s.sess.Kill(r.PathValue("name")); err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.sch.Table())
}

func (s *Server) scheduleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.sch.Reload(); err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, s.sch.Table())
}

// scheduleRun fires a job now and returns immediately with the run id.
func (s *Server) scheduleRun(w http.ResponseWriter, r *http.Request) {
	job := r.PathValue("job")
	found := false
	for _, j := range s.sch.Table().Jobs {
		if j.Name == job {
			found = true
		}
	}
	if !found {
		jsonErr(w, 404, "no such job")
		return
	}
	go func() {
		if _, err := s.sch.Run(job, "api"); err != nil {
			log.Printf("sched: %s: %v", job, err)
		}
	}()
	writeJSON(w, 202, map[string]string{"job": job, "status": "started"})
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rs, err := s.sch.Runs(r.URL.Query().Get("job"), limit)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rs)
}

// One job run, parsed for reading: what a proposal from a scheduled job
// opens on (it has no session).
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, 400, "run id is a number")
		return
	}
	d, err := s.sch.GetRun(id)
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, d)
}
