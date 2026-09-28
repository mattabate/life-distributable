// Package server wires the HTTP API. Every route is listed in
// shared/api.md — keep them in sync (TestContractCoverage enforces it).
package server

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/budget"
	"life/hub/internal/calendar"
	"life/hub/internal/config"
	"life/hub/internal/goals"
	"life/hub/internal/notify"
	"life/hub/internal/obs"
	"life/hub/internal/recs"
	"life/hub/internal/sched"
	"life/hub/internal/sessions"
	"life/hub/internal/spend"
	"life/hub/internal/syncruns"
	"life/hub/internal/threads"
	"life/hub/internal/usage"
)

// The console, served under /static/. Globbed per file type rather than `web`
// so web/test/ (the Node tests, ops/webcheck.sh) never ends up in the binary —
// a bare `web/*.js` still picks up a newly added top-level script, which a
// hand-kept file list did not (composer.js 404'd on 2026-08-29).
//
//go:embed web/index.html web/*.css web/*.js web/views
var webFS embed.FS

// consoleBuild identifies the console files inside THIS binary.
//
// The console is a hash-routed single page, so a tab left open overnight
// never fetches index.html again. Clicking from Money to an account only
// changes the fragment; the JS running in that tab is whatever
// the hub was serving when the tab was opened, for as long as the tab lives.
// The hub restarted with new files under it and the page could not know.
//
// So every response carries this stamp, the console remembers the first one it
// saw, and a poll that comes back with a different one means the console has
// been rebuilt underneath the tab: it reloads itself (app.js). It is a hash of
// the FILES, not a build time, so restarting the hub with no console change
// never yanks a page out from under a reader mid-read.
var consoleBuild = func() string {
	h := sha256.New()
	_ = fs.WalkDir(webFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := webFS.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}()

type Server struct {
	cfg   *config.Config
	token string
	sess  *sessions.Manager
	acts  *actions.Queue
	sch   *sched.Scheduler
	goals *goals.Store
	obs   *obs.Store
	thr   *threads.Manager
	// Push: APNs sender (nil when the hub has no APNs key configured).
	Push *notify.APNs
	// Cal: the plan layer (dated steps, agent runs, reminders).
	Cal  *calendar.Calendar
	Recs *recs.Store
	// Usage: the opt-in weekly heartbeat (Settings turns it off).
	Usage *usage.Heartbeat
	// Runs: the sync_runs log every clock syncer's runs land in; Sources
	// health reads a connector's last run and failing streak from it.
	Runs *syncruns.Log

	mux *http.ServeMux
	// StatusExtra lets main inject environment facts (profile expiry…).
	StatusExtra func() map[string]any

	// usage: the transcripts under claude_projects_dir, parsed incrementally
	// (spend.Cache) — the Spend tab and the plan meters read from it.
	usage  *spend.Cache
	quota  *spend.QuotaFetcher
	picker *spend.Picker
	budget *budget.Guard

	headMu sync.Mutex
	headAt time.Time
	headN  int
	// "has app/ changed since <commit>", cached per published commit
	appChangedFor string
	appChangedAt  time.Time
	appChangedN   bool

	// stopping closes when the hub starts shutting down (Draining).
	stopping chan struct{}
	stopOnce sync.Once
}

// Draining: main hands this to http.Server.RegisterOnShutdown. Parked
// /changes requests answer at once instead of holding Shutdown to its 5 s
// deadline — which they did on every restart, so each one was a ~6 s outage
// and a send in that window read "Failed to fetch" (2026-09-18: 19 restarts
// in 4 h of parallel sessions shipping hub changes).
func (s *Server) Draining() { s.stopOnce.Do(func() { close(s.stopping) }) }

func New(cfg *config.Config, token string, sess *sessions.Manager, acts *actions.Queue, sch *sched.Scheduler, gs *goals.Store, os_ *obs.Store, thr *threads.Manager) *Server {
	s := &Server{cfg: cfg, token: token, sess: sess, acts: acts, sch: sch, goals: gs, obs: os_, thr: thr, mux: http.NewServeMux(), usage: spend.NewCache(cfg.ClaudeProjectsDir), quota: spend.NewQuotaFetcher(), stopping: make(chan struct{})}
	s.register()
	if os_ != nil {
		s.registerKinds()
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// Set before anything can write: a 304 out of the ETag layer keeps the
	// headers already on the map, so even a fully-cached poll carries it.
	w.Header().Set("X-Hub-Build", consoleBuild)
	sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
	gw, done := maybeGzip(sw, r)
	// ETag sits above gzip: it hashes the plain JSON, so the tag is the same
	// whether or not the client asked for compression.
	cw, finish := maybeETag(gw, r)
	s.mux.ServeHTTP(cw, r)
	finish()
	done()
	if took := time.Since(start); logRequest(r.Method, r.URL.Path, sw.code, took) {
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.code, took.Round(time.Millisecond))
	}
}

// quietGET: a successful read faster than this is not logged. The console and
// the phone poll the same reads all day, which was ~1,500 lines an hour of
// "GET /api/v1/board 40ms" and a 138 MB hub.log (2026-09-14 cleanup); slow
// reads, errors and every write still land. The change feed is slow on
// purpose (it parks up to 30 s), so a successful one is never logged.
const quietGET = 250 * time.Millisecond

func logRequest(method, path string, code int, took time.Duration) bool {
	ok := code < 300 || code == http.StatusNotModified
	if method != http.MethodGet || !ok {
		return true
	}
	return took >= quietGET && path != "/api/v1/changes"
}

// statusWriter records the status the client actually got (a 304 from the
// ETag layer included) for the request log. Flush and Hijack pass through so
// the layers above it keep working.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("server: underlying writer cannot hijack")
}

// auth accepts "Authorization: Bearer <token>" or the "life_token" cookie
// (set by the dashboard once from ?token=). Constant-time compare.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie("life_token"); err == nil {
			got = c.Value
		}
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now()})
}

// index serves the dashboard. ?token=X on first visit sets the cookie so
// the phone's browser can bookmark the bare URL afterwards.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if t := r.URL.Query().Get("token"); t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1 {
		http.SetCookie(w, &http.Cookie{Name: "life_token", Value: t, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized: open /?token=<hub token> once", http.StatusUnauthorized)
		return
	}
	b, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The document that names every script: it must never come out of a cache
	// on a reload, or the reload that fixes a stale tab loads the stale list.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.cfg.AllProjects())
}

var startedAt = time.Now()

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	open, _ := s.acts.List("open", 500)
	pending := 0
	for _, a := range open {
		if a.State == "proposed" {
			pending++
		}
	}
	out := map[string]any{
		"ok": true, "time": time.Now(), "uptime_s": int(time.Since(startedAt).Seconds()),
		"pending_actions": pending, "jobs": len(s.sch.Table().Jobs),
	}
	if s.StatusExtra != nil {
		for k, v := range s.StatusExtra() {
			out[k] = v
		}
	}
	if o := s.OTACurrent(); o != nil {
		out["ota"] = o
	}
	if s.Push != nil {
		out["devices"] = s.Push.Devices()
	}
	writeJSON(w, 200, out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
