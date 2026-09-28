package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// route: one endpoint. `open` skips bearer auth — only for what a phone, a
// browser tab or an OAuth provider reaches without a token (health, the
// console shell, OTA behind its own token, OAuth callbacks).
type route struct {
	pattern string
	h       http.HandlerFunc
	open    bool
}

// routes is the ONE list: New registers from it and the contract test
// (TestContractCoverage) checks it against shared/api.md. Adding an endpoint
// is one line here plus its `### METHOD /path` section in api.md
// (hub/CLAUDE.md "Recipe: add an endpoint").
func (s *Server) routes() []route {
	return []route{
		{"GET /healthz", s.healthz, true},
		{"GET /", s.index, true},
		{"GET /api/v1/projects", s.projects, false},
		{"GET /api/v1/spend/summary", s.spendSummary, false},
		{"GET /api/v1/spend/quota", s.spendQuota, false},
		{"GET /api/v1/spend/model", s.spendModel, false},
		{"PUT /api/v1/spend/model", s.setSpendModel, false},
		{"GET /api/v1/spend/budget", s.spendBudget, false},
		{"POST /api/v1/spend/budget/clear", s.spendBudgetClear, false},
		{"GET /api/v1/usage", s.getUsage, false},
		{"PUT /api/v1/usage", s.setUsage, false},
		{"GET /api/v1/sessions", s.listSessions, false},
		{"POST /api/v1/sessions", s.startSession, false},
		{"DELETE /api/v1/sessions/{name}", s.killSession, false},
		{"GET /api/v1/actions", s.listActions, false},
		{"POST /api/v1/actions", s.proposeAction, false},
		{"GET /api/v1/actions/{id}", s.getAction, false},
		{"POST /api/v1/actions/{id}/approve", s.decideAction(true), false},
		{"POST /api/v1/actions/{id}/deny", s.decideAction(false), false},
		{"POST /api/v1/actions/{id}/dismiss", s.moveAction("dismissed"), false},
		{"POST /api/v1/actions/{id}/reopen", s.moveAction("proposed"), false},
		{"GET /api/v1/decider", s.deciderStatus, false},
		{"GET /api/v1/schedule", s.schedule, false},
		{"POST /api/v1/schedule/reload", s.scheduleReload, false},
		{"POST /api/v1/schedule/{job}/run", s.scheduleRun, false},
		{"GET /api/v1/runs", s.runs, false},
		{"GET /api/v1/runs/{id}", s.run, false},
		{"GET /api/v1/status", s.status, false},
		{"GET /api/v1/board", s.board, false},
		{"GET /api/v1/goals", s.listGoals, false},
		{"POST /api/v1/goals", s.createGoal, false},
		{"GET /api/v1/goals/{id}", s.getGoal, false},
		{"PATCH /api/v1/goals/{id}", s.patchGoal, false},
		{"GET /api/v1/goals/{id}/notes", s.goalNotes, false},
		{"POST /api/v1/goals/{id}/notes", s.addGoalNote, false},
		{"GET /api/v1/observations", s.listObs, false},
		{"POST /api/v1/observations", s.postObs, false},
		{"POST /api/v1/observations/batch", s.postObsBatch, false},
		{"GET /api/v1/observations/counts", s.obsCounts, false},
		{"POST /api/v1/voice", s.postVoice, false},
		{"POST /api/v1/voice/hush", s.postVoiceHush, false},
		{"GET /api/v1/sources", s.sources, false},
		{"GET /api/v1/blobs/{ref...}", s.getBlob, false},
		{"POST /api/v1/blobs", s.postBlob, false},
		{"GET /api/v1/threads", s.listThreads, false},
		{"POST /api/v1/threads", s.createThread, false},
		{"GET /api/v1/threads/{id}", s.getThread, false},
		{"PATCH /api/v1/threads/{id}", s.patchThread, false},
		{"GET /api/v1/threads/{id}/messages", s.threadMessages, false},
		{"POST /api/v1/threads/{id}/messages", s.sendThread, false},
		{"GET /api/v1/threads/{id}/events", s.threadEvents, false},
		{"GET /api/v1/threads/{id}/steps", s.threadSteps, false},
		{"GET /api/v1/changes", s.changes, false},
		{"POST /api/v1/threads/{id}/checkin", s.checkinThread, false},
		{"POST /api/v1/threads/{id}/stop", s.stopThread, false},
		{"POST /api/v1/threads/{id}/archive", s.archiveThread, false},
		{"POST /api/v1/threads/{id}/read", s.readThread, false},
		{"GET /api/v1/prompts", s.listPrompts, false},
		{"POST /api/v1/prompts", s.addPrompt, false},
		{"POST /api/v1/prompts/{id}/cancel", s.cancelPrompt, false},
		{"GET /api/v1/asks", s.listAsks, false},
		{"POST /api/v1/asks", s.addAsk, false},
		{"GET /api/v1/asks/{id}", s.getAsk, false},
		{"POST /api/v1/asks/{id}/resolve", s.resolveAsk, false},
		{"POST /api/v1/asks/{id}/retry", s.retryAsk, false},
		{"POST /api/v1/asks/{id}/surface", s.setAskSurface, false},
		{"POST /api/v1/asks/{id}/kind", s.setAskKind, false},
		{"GET /api/v1/calendar", s.calendarView, false},
		{"POST /api/v1/calendar", s.calendarAdd, false},
		{"GET /api/v1/calendar/{id}", s.calendarGet, false},
		{"PATCH /api/v1/calendar/{id}", s.calendarUpdate, false},
		{"POST /api/v1/calendar/{id}/resolve", s.calendarResolve, false},
		{"GET /api/v1/recs", s.recsList, false},
		{"POST /api/v1/recs", s.recsAdd, false},
		{"GET /api/v1/recs/stats", s.recsStats, false},
		{"GET /api/v1/recs/{id}", s.recsGet, false},
		{"GET /api/v1/recs/{id}/starter", s.recsStarter, false},
		{"POST /api/v1/recs/{id}/decide", s.recsDecide, false},
		{"POST /api/v1/recs/{id}/reply", s.recsReply, false},
		{"POST /api/v1/recs/{id}/score", s.recsScore, false},
		{"POST /api/v1/recs/{id}/link", s.recsLink, false},
		{"POST /api/v1/devices", s.registerDevice, false},
		{"POST /api/v1/devices/test", s.testPush, false},
		{"POST /api/v1/app/install", s.appInstall, false},
		{"GET /api/v1/app/install/status", s.appInstallStatus, false},
		{"GET /ota/{token}/{file}", s.ota, true},
	}
}

// Routes: the patterns, for the contract test and anything that lists them.
func (s *Server) Routes() []string {
	var out []string
	for _, r := range s.routes() {
		out = append(out, r.pattern)
	}
	return out
}

// register wires every route into the mux, plus the static console files.
func (s *Server) register() {
	for _, r := range s.routes() {
		h := r.h
		if !r.open {
			h = s.auth(h)
		}
		s.mux.HandleFunc(r.pattern, h)
	}
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", staticCache(http.FileServerFS(sub), sub)))
}

// staticCache makes the console's own files cacheable AND always current.
//
// They had neither before. `embed.FS` reports the zero time as every file's
// ModTime, so `http.ServeContent` sets no `Last-Modified`; condWriter only
// tags JSON, so there was no `ETag` either; and nothing set `Cache-Control`.
// A response with no validator and no freshness is at the mercy of whatever
// each browser guesses, which is the wrong place for the answer to "is the
// owner looking at the JS I just shipped?".
//
// `no-cache` is not "do not cache" — it is "cache it, but revalidate every
// time". With the content hash as the ETag that revalidation is a ~200-byte
// 304 on the Tailscale link, and a changed file can never be missed.
// http.ServeContent does the comparison itself once the header is set.
func staticCache(h http.Handler, sub fs.FS) http.Handler {
	tags := map[string]string{}
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := sub.Open(p)
		if err != nil {
			return nil
		}
		defer b.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, b); err != nil {
			return nil
		}
		tags[p] = `"` + hex.EncodeToString(sum.Sum(nil))[:16] + `"`
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := tags[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.ServeHTTP(w, r)
	})
}
