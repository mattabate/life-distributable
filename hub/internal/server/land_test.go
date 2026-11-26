package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"life/hub/internal/actions"
)

func landGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// noExec stands in for the shell: these tests check what would run, not run it.
type noExec struct{}

func (noExec) Exec(context.Context, string, json.RawMessage) (string, error) { return "", nil }

// newTestExec: newTest with a queue that can carry out an approved action.
func newTestExec(t *testing.T) *Server {
	s, db := newTestDB(t)
	s.acts = actions.New(db, noExec{}, nil)
	return s
}

func postAs(s *Server, path, from, body string) (int, string) {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", "application/json")
	if from != "" {
		r.Header.Set("X-Life-Thread", from)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// A session's commits land on main through one action, decided at once on
// the standing approval for a local commit.
func TestLandRunsOnStandingApproval(t *testing.T) {
	s := newTestExec(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.MkdirAll(repo, 0o755)
	landGit(t, repo, "init", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a"), []byte("1"), 0o644)
	landGit(t, repo, "add", "a")
	landGit(t, repo, "commit", "-m", "base")
	s.cfg.Root = repo
	// A gate landing is a real card, so Propose's preflight wants its script
	// on disk (untracked here: the diffs never see it).
	os.MkdirAll(filepath.Join(repo, "ops"), 0o755)
	os.WriteFile(filepath.Join(repo, "ops", "land-incoming.sh"), []byte("#!/bin/sh\n"), 0o755)

	clone := filepath.Join(root, "clone")
	landGit(t, root, "clone", repo, clone)
	landGit(t, clone, "checkout", "-b", "work")
	os.WriteFile(filepath.Join(clone, "b"), []byte("2"), 0o644)
	landGit(t, clone, "add", "b")
	landGit(t, clone, "commit", "-m", "the work")
	sha := landGit(t, clone, "rev-parse", "HEAD")
	bundle := filepath.Join(root, "x.bundle")
	landGit(t, clone, "bundle", "create", bundle, "origin/main..work")
	raw, _ := os.ReadFile(bundle)

	say := ""
	post := func(from, branch, b64 string) (int, string) {
		b, _ := json.Marshal(map[string]string{"branch": branch, "bundle_b64": b64, "say": say})
		return postAs(s, "/api/v1/land", from, string(b))
	}
	good := base64.StdEncoding.EncodeToString(raw)
	if code, _ := post("th-1", "work", good); code != 409 {
		t.Fatalf("off: %d", code)
	}
	s.cfg.SessionUser = "life-agent"
	for _, c := range []struct {
		from, branch, b64 string
		want              int
	}{
		{"", "work", good, 403},
		// The thread goes into refs/incoming/<from>: a colon, a slash or a
		// space would change the refspec (#148).
		{"th-1:refs/heads/main", "work", good, 403},
		{"../main", "work", good, 403},
		{"th 1", "work", good, 403},
		{"th-1", "--upload-pack=x", good, 400},
		{"th-1", "work", "!!", 400},
		{"th-1", "work", base64.StdEncoding.EncodeToString([]byte("not a bundle")), 400},
	} {
		if code, body := post(c.from, c.branch, c.b64); code != c.want {
			t.Errorf("%+v: %d %s", c, code, body)
		}
	}
	code, body := post("th-1", "work", good)
	if code != 201 {
		t.Fatal(code, body)
	}
	var a actions.Action
	json.Unmarshal([]byte(body), &a)
	// Filed already approved on the standing yes: no card, nothing spoken.
	if !a.Gated || a.Kind != "commit" || a.DecidedVia != "standing" || a.State == "proposed" || a.Said != "" ||
		!strings.Contains(string(a.ExecPayload), "bash ops/land-incoming.sh "+sha) || !strings.Contains(a.Detail, "the work") {
		t.Fatalf("%+v", a)
	}
	// A landing that touches the gate's own files is the owner's card, not
	// the standing yes, and its card is the session's plain words.
	landGit(t, clone, "checkout", "-b", "gate")
	os.MkdirAll(filepath.Join(clone, "ops"), 0o755)
	os.WriteFile(filepath.Join(clone, "ops", "schedule.json"), []byte("{}"), 0o644)
	landGit(t, clone, "add", "ops/schedule.json")
	landGit(t, clone, "commit", "-m", "loosen the tool list")
	gb := filepath.Join(root, "g.bundle")
	landGit(t, clone, "bundle", "create", gb, "origin/main..gate")
	graw, _ := os.ReadFile(gb)
	gate := base64.StdEncoding.EncodeToString(graw)
	if code, body := post("th-2", "gate", gate); code != 400 || !strings.Contains(body, "--say") {
		t.Fatalf("gate land without say: %d %s", code, body)
	}
	say = "I need your approval to let scheduled jobs build, because the sweep needs it."
	code, body = post("th-2", "gate", gate)
	var g actions.Action
	json.Unmarshal([]byte(body), &g)
	if code != 201 || g.State != "proposed" || g.DecidedVia == "standing" || g.Title != "Change the approval gate" ||
		g.Detail != "loosen the tool list\n\nthe work" { // the commits' words alone: no sha, files or stat
		t.Fatalf("gate land: %d %+v", code, g)
	}
	say = ""
	// Main moved underneath: the branch must rebase first.
	os.WriteFile(filepath.Join(repo, "c"), []byte("3"), 0o644)
	landGit(t, repo, "add", "c")
	landGit(t, repo, "commit", "-m", "other")
	if code, body := post("th-1", "work", good); code != 409 {
		t.Fatal(code, body)
	}
}

// An app build from a session runs as the owner's user on the standing yes;
// its why is one short line, never a second command.
func TestAppShip(t *testing.T) {
	s := newTestExec(t)
	th, _ := s.thr.Create("Work", "life", "", "Work.", "", "", nil)
	if code, _ := postAs(s, "/api/v1/app/ship", th.ID, `{"target":"phone","why":"fixes x"}`); code != 409 {
		t.Fatalf("off: %d", code)
	}
	s.cfg.SessionUser = "life-agent"
	for _, c := range []struct {
		from, body string
		want       int
	}{
		{"", `{"target":"phone","why":"fixes x"}`, 403},
		{th.ID, `{"target":"ipad","why":"fixes x"}`, 400},
		{th.ID, `{"target":"phone","why":""}`, 400},
		{th.ID, `{"target":"phone","why":"fixes x\nrm -rf ~"}`, 400},
	} {
		if code, body := postAs(s, "/api/v1/app/ship", c.from, c.body); code != c.want {
			t.Errorf("%+v: %d %s", c, code, body)
		}
	}
	var p struct{ Cmd, Dir string }
	for target, want := range map[string]string{"phone": "make -f Makefile ship", "mac": "bash ops/install-mac.sh"} {
		code, body := postAs(s, "/api/v1/app/ship", th.ID, `{"target":"`+target+`","why":"fixes the card's $(id) box"}`)
		var a actions.Action
		json.Unmarshal([]byte(body), &a)
		json.Unmarshal(a.ExecPayload, &p)
		if code != 201 || a.DecidedVia != "standing" || p.Cmd != want || p.Dir != s.cfg.Root {
			t.Fatalf("%s: %d %+v %+v", target, code, a, p)
		}
	}
}

// With session_user set, a session's shell proposal outside the main checkout
// always waits for the owner.
func TestSessionProposalGatesOtherDirs(t *testing.T) {
	s := newTest(t)
	s.cfg.Root = "/repo"
	free := false
	prop := func(dir string) bool {
		pay, _ := json.Marshal(map[string]string{"cmd": "ls", "dir": dir})
		p := actions.Proposal{ExecType: "shell", ExecPayload: pay, Gated: &free}
		s.sessionProposal(&p)
		return p.Gated != nil && *p.Gated
	}
	if prop("/agent/th-1") {
		t.Fatal("off: gated")
	}
	s.cfg.SessionUser = "life-agent"
	for dir, want := range map[string]bool{"/repo": false, "/repo/": false, "": false,
		"/repo/data/tmp": true, "/repo/../agent": true, "/agent/th-1": true} {
		if got := prop(dir); got != want {
			t.Errorf("%q: %v", dir, got)
		}
	}
}

// The hub's own decider check is on the gate list wherever it lives: a session
// that made deciderOK return true would approve anything with the hub token
// alone, so a landing that edits it must be the owner's card. The same holds
// for every file that CALLS deciderOK (dropping the call skips the check as
// surely as editing it), and for the token check, the route wrapper and the
// open-route list.
func TestGatePathsCoverTheDeciderCheck(t *testing.T) {
	gated := map[string]bool{}
	for _, p := range gatePaths {
		gated[p] = true
	}
	files, _ := filepath.Glob("*.go")
	found := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, mark := range []string{"deciderOK(", "func (s *Server) auth(", "func (s *Server) authorized(", "func (s *Server) wrap(", "var openRoutes", "var ownerOnlyRoutes"} {
			if strings.Contains(string(b), mark) {
				found[mark] = true
				if !gated["hub/internal/server/"+f] {
					t.Errorf("%s holds %q but is not in gatePaths", f, mark)
				}
			}
		}
	}
	if len(found) != 6 {
		t.Fatalf("a gate mark is no longer in package server: found %v", found)
	}
}

// GNU make reads GNUmakefile, then makefile, before Makefile: a landing that
// added one with a build: target would run as the owner on the next build,
// the gate untouched (#225, reported by @junhwanahn). Every make the hub or an
// owner-side script runs pins -f Makefile, and the other two names are on the
// gate list besides.
func TestOwnerSideMakePinsMakefile(t *testing.T) {
	gated := "\n" + strings.Join(gatePaths, "\n") + "\n"
	for _, p := range []string{"Makefile", "GNUmakefile", "makefile"} {
		if !strings.Contains(gated, "\n"+p+"\n") {
			t.Errorf("%s is not in gatePaths", p)
		}
	}
	cmd := string(landProposal("t", "abc", "x", "", "/r", true, "").ExecPayload)
	if !strings.Contains(cmd, "make -f Makefile build") || strings.Contains(cmd, "make build") {
		t.Errorf("the landing's build does not pin -f Makefile: %s", cmd)
	}
	// A sub-make does not inherit -f, so the Makefile's own $(MAKE) pins it too.
	runsMake := regexp.MustCompile(`^\s*@?(\w+\(\)\s*\{\s*)?(as_session env \S+ )?(make|\$\(MAKE\))\s|(\|\||&&|;)\s*(make|\$\(MAKE\))\s`)
	for _, f := range []string{"ops/hub.sh", "ops/land-incoming.sh", "Makefile"} {
		b, err := os.ReadFile("../../../" + f)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i, l := range strings.Split(string(b), "\n") {
			code := strings.SplitN(l, " #", 2)[0]
			if strings.HasPrefix(strings.TrimSpace(l), "#") || !runsMake.MatchString(code) {
				continue
			}
			n++
			if !strings.Contains(code, "-f Makefile") {
				t.Errorf("%s:%d runs make without -f Makefile: %s", f, i+1, strings.TrimSpace(l))
			}
		}
		if n == 0 {
			t.Errorf("%s no longer runs make: drop it from this test", f)
		}
	}
}

// The Mac's disk ignores case and git pathspecs do not: GNUMakefile is the
// file make opens as GNUmakefile, and OPS/hub.sh is ops/hub.sh once checked
// out, so a gate path matches in any case (#225).
func TestGatePathsMatchAnyCase(t *testing.T) {
	repo := t.TempDir()
	landGit(t, repo, "init", "-b", "main")
	landGit(t, repo, "commit", "--allow-empty", "-m", "base")
	blob := landGit(t, repo, "hash-object", "-w", "--stdin")
	gated := []string{"GNUMakefile", "MAKEFILE", "OPS/hub.sh", "Hub/Internal/Actions/x.go", "ops/Session-User-x.sh"}
	for _, p := range append(gated, "README") {
		landGit(t, repo, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+p)
	}
	landGit(t, repo, "commit", "-m", "mixed case")
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	got := "\n" + gateTouched(git, "main~1", "main") + "\n"
	for _, p := range gated {
		if !strings.Contains(got, "\n"+p+"\n") {
			t.Errorf("%s changed and the gate did not see it", p)
		}
	}
	if strings.Contains(got, "\nREADME\n") {
		t.Error("README read as a gate file")
	}

	// GIT_LITERAL_PATHSPECS in the hub's environment must not blind the gate.
	t.Setenv("GIT_LITERAL_PATHSPECS", "1")
	if lit := "\n" + gateTouched(git, "main~1", "main") + "\n"; !strings.Contains(lit, "\nGNUMakefile\n") {
		t.Errorf("with GIT_LITERAL_PATHSPECS=1 the gate saw %q", lit)
	}
}

// git's icase folds ASCII only and the disk folds more: ops/hub.ſh (a long s)
// opens as ops/hub.sh. Any name outside ASCII is the owner's card; an ASCII
// one that misses every gate path is not.
func TestGateTouchedNonASCIIName(t *testing.T) {
	repo := t.TempDir()
	landGit(t, repo, "init", "-b", "main")
	landGit(t, repo, "commit", "--allow-empty", "-m", "base")
	blob := landGit(t, repo, "hash-object", "-w", "--stdin")
	landGit(t, repo, "update-index", "--add", "--cacheinfo", "100644,"+blob+",README")
	landGit(t, repo, "commit", "-m", "ascii")
	landGit(t, repo, "update-index", "--add", "--cacheinfo", "100644,"+blob+",ops/hub.ſh")
	landGit(t, repo, "commit", "-m", "long s")
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	if got := gateTouched(git, "main~2", "main~1"); got != "" {
		t.Errorf("an ASCII README read as the gate: %q", got)
	}
	if gateTouched(git, "main~1", "main") == "" {
		t.Error("ops/hub.ſh landed with no card")
	}
}

// A route marked open in routes.go is served with no token only if the gate's
// own list names it.
func TestOpenRouteNeedsTheGateList(t *testing.T) {
	s := newTest(t)
	called := false
	h := s.wrap(route{"POST /api/v1/actions/{id}/approve", func(http.ResponseWriter, *http.Request) { called = true }, true})
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", "/api/v1/actions/x/approve", nil))
	if called || w.Code != 401 {
		t.Fatalf("an unlisted open route ran with no token: %d", w.Code)
	}
	for _, r := range s.routes() {
		if r.open && !openRoutes[r.pattern] {
			t.Errorf("%s is marked open but openRoutes does not list it", r.pattern)
		}
	}
}

// A failed gate diff is the gate touched, never "no gate files".
func TestGateTouchedFailsClosed(t *testing.T) {
	broken := func(...string) (string, error) { return "", errors.New("timeout") }
	if gateTouched(broken, "main", "abc") == "" {
		t.Fatal("a failed diff read as no gate files touched")
	}
	clean := func(...string) (string, error) { return "", nil }
	if gateTouched(clean, "main", "abc") != "" {
		t.Fatal("a clean diff read as the gate touched")
	}
}
