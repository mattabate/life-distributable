// Devices and the app: APNs registration, test push, OTA bundle, app install.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ota serves the over-the-air app install bundle published by ops/ota.sh
// (data/ota: install.html, manifest.plist, Life.ipa). Not bearer-authed: iOS
// fetches the manifest and .ipa itself with no headers, so the secret is the
// unguessable path token (data/ota/.token). Tailscale-only like everything.
func (s *Server) ota(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(filepath.Dir(s.cfg.BlobDir), "ota")
	want, err := os.ReadFile(filepath.Join(dir, ".token"))
	if err != nil || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(string(want))), []byte(r.PathValue("token"))) != 1 {
		http.NotFound(w, r)
		return
	}
	file := r.PathValue("file")
	switch file {
	case "install.html", "manifest.plist", "Life.ipa": // not index.html: ServeFile 301s that to "./"
	default:
		http.NotFound(w, r)
		return
	}
	if file == "manifest.plist" {
		w.Header().Set("Content-Type", "application/xml")
	} else if file == "Life.ipa" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(dir, file))
}

// registerDevice stores the app's APNs token (app sends it on every launch).
func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	if !need(w, s.Push != nil, "push not configured") {
		return
	}
	var in struct {
		Token, Device, Env string
		Build              int
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if err := s.Push.Register(in.Token, in.Device, in.Env, in.Build); err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	// The phone just said what it runs: close "Install app build N" asks
	// with N <= that, reopen tapped-but-never-installed ones.
	s.thr.ReconcileInstalls(s.Push.MaxBuild())
	w.WriteHeader(204)
}

func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	if !need(w, s.Push != nil, "push not configured") {
		return
	}
	var in struct{ Only string }
	if !decodeOptional(w, r, &in, 0) { // no body = both halves
		return
	}
	if in.Only != "" && in.Only != "phone" && in.Only != "mac" {
		jsonErr(w, 400, "only must be phone or mac")
		return
	}
	if err := s.Push.Test(in.Only); err != nil {
		jsonErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(204)
}

// appInstall: rebuild the iOS app from the phone itself. Two lanes, body
// {"lane":"ota"|"lan"} (default ota): "ota" runs `make ship` (ops/ota.sh:
// ad-hoc archive published at /ota/<token>/ — the app then opens the
// itms-services link from GET .../status `ota.url`); "lan" runs
// ops/install-phone.sh (Xcode install over the Mac's Wi-Fi, phone must be on
// the LAN). Runs in tmux so it outlives the request.
var installMu sync.Mutex

func (s *Server) appInstall(w http.ResponseWriter, r *http.Request) {
	installMu.Lock()
	defer installMu.Unlock()
	var in struct{ Lane string }
	if !decodeOptional(w, r, &in, 0) {
		return
	}
	script := s.cfg.OpsPath("ota.sh")
	if in.Lane == "lan" {
		script = s.cfg.OpsPath("install-phone.sh")
	} else if in.Lane != "" && in.Lane != "ota" {
		jsonErr(w, 400, "lane must be ota or lan")
		return
	}
	if _, err := exec.Command("tmux", "has-session", "-t", "=life-app-install").CombinedOutput(); err == nil {
		jsonErr(w, 409, "an install is already running")
		return
	}
	logPath := s.cfg.OpsPath("logs", "app-install.log")
	cmd := script + " > " + logPath + " 2>&1; echo \"exit=$?\" >> " + logPath
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "life-app-install", "-c", s.cfg.Root, cmd).CombinedOutput(); err != nil {
		jsonErr(w, 500, "tmux: "+strings.TrimSpace(string(out)))
		return
	}
	writeJSON(w, 202, map[string]string{"status": "started", "lane": strings.TrimSuffix(filepath.Base(script), ".sh")})
}

func (s *Server) appInstallStatus(w http.ResponseWriter, r *http.Request) {
	running := false
	if _, err := exec.Command("tmux", "has-session", "-t", "=life-app-install").CombinedOutput(); err == nil {
		running = true
	}
	logPath := s.cfg.OpsPath("logs", "app-install.log")
	b, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	// The log outlives the run that wrote it, so the tail alone is undated and
	// a finished build's lines read as if they were the current one's.
	// log_at lets the app drop a tail older than the build it started.
	out := map[string]any{"running": running, "tail": lines, "ota": s.OTACurrent()}
	if fi, err := os.Stat(logPath); err == nil {
		out["log_at"] = fi.ModTime().UTC().Format(time.RFC3339)
	}
	writeJSON(w, 200, out)
}

// OTACurrent is the newest published OTA build (data/ota/current.json
// written by ops/ota.sh) or nil: {version, build, url, profile_expires,
// aps, built_at, commit} plus `head`. The app compares `build` with its own
// and offers the install link; `url` carries the unguessable path token.
//
// `head` is the build number the NEXT `make ship` would produce (the repo's
// commit count — both ops scripts pass it as CURRENT_PROJECT_VERSION). It is
// here because the published build routinely falls behind what is on the
// phone: ops/install-phone.sh (the LAN lane) installs a newer build without
// republishing an .ipa, so current.json keeps naming an older one and the app
// had no way to say so.
//
// `app_changed` says whether building `head` would produce a DIFFERENT app
// than the published one: the build number counts every commit in the repo, so
// a docs-only commit raises head without touching a line of Swift, and the app
// used to advertise that as newer code to build. Absent when git or the
// published commit is unavailable — the app then falls back to comparing
// numbers.
func (s *Server) OTACurrent() map[string]any {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(s.cfg.BlobDir), "ota", "current.json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	if n := s.headBuild(); n > 0 {
		m["head"] = n
	}
	if c, _ := m["commit"].(string); c != "" {
		if changed, known := s.appChangedSince(c); known {
			m["app_changed"] = changed
		}
	}
	return m
}

// appChangedSince reports whether anything under app/ differs from the commit
// the published build was cut at — committed or not, since both ops scripts
// archive the working tree. Second result is false when git could not answer.
func (s *Server) appChangedSince(commit string) (changed, known bool) {
	s.headMu.Lock()
	defer s.headMu.Unlock()
	if s.appChangedFor == commit && time.Since(s.appChangedAt) < 30*time.Second {
		return s.appChangedN, true
	}
	root := filepath.Dir(filepath.Dir(s.cfg.BlobDir))
	git := "/usr/bin/git"
	if p, err := exec.LookPath("git"); err == nil {
		git = p
	}
	diff, err := exec.Command(git, "-C", root, "diff", "--name-only", commit+"..HEAD", "--", "app").Output()
	if err != nil {
		return false, false
	}
	dirty, err := exec.Command(git, "-C", root, "status", "--porcelain", "--", "app").Output()
	if err != nil {
		return false, false
	}
	s.appChangedN = len(strings.TrimSpace(string(diff))) > 0 || len(strings.TrimSpace(string(dirty))) > 0
	s.appChangedFor, s.appChangedAt = commit, time.Now()
	return s.appChangedN, true
}

// headBuild is `git rev-list --count HEAD` for the repo root, cached briefly
// (/status is polled by the app). 0 when git is unavailable.
func (s *Server) headBuild() int {
	s.headMu.Lock()
	defer s.headMu.Unlock()
	if time.Since(s.headAt) < 30*time.Second {
		return s.headN
	}
	root := filepath.Dir(filepath.Dir(s.cfg.BlobDir)) // …/data/blobs → …/
	git := "/usr/bin/git"                             // launchd PATH is minimal
	if p, err := exec.LookPath("git"); err == nil {
		git = p
	}
	out, err := exec.Command(git, "-C", root, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return s.headN
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return s.headN
	}
	s.headN, s.headAt = n, time.Now()
	return n
}
