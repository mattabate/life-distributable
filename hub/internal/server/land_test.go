package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

	post := func(from, branch, b64 string) (int, string) {
		b, _ := json.Marshal(map[string]string{"branch": branch, "bundle_b64": b64})
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
		!strings.Contains(string(a.ExecPayload), "git merge --ff-only "+sha) || !strings.Contains(a.Detail, "the work") {
		t.Fatalf("%+v", a)
	}
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
	for target, want := range map[string]string{"phone": "make ship", "mac": "bash ops/install-mac.sh"} {
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
