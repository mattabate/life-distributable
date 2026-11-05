package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"life/hub/internal/actions"
)

// With session_user set, sessions run as a user that cannot write the main
// checkout. Their commits arrive here as a git bundle and wait under
// refs/incoming/<session>; the hub fast-forwards main as the owner's user, one
// action per landing however many commits it holds. A local commit is not
// gated (CLAUDE.md), so the action is decided at once on that standing
// approval (decided_via "standing") and runs with no card; ops/land.sh waits
// for it and prints how it ended. Off (409) unless session_user is set.
var landBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func (s *Server) land(w http.ResponseWriter, r *http.Request) {
	if s.cfg.SessionUser == "" {
		jsonErr(w, 409, "the land lane is off (no session_user): commit in the main checkout")
		return
	}
	from := strings.TrimSpace(r.Header.Get("X-Life-Thread"))
	if from == "" {
		jsonErr(w, 403, "only a session lands through this route")
		return
	}
	var in struct {
		Branch    string `json:"branch"`
		BundleB64 string `json:"bundle_b64"`
		Say       string `json:"say"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json: "+err.Error())
		return
	}
	bundle, err := base64.StdEncoding.DecodeString(in.BundleB64)
	if err != nil || len(bundle) == 0 || !landBranch.MatchString(in.Branch) {
		jsonErr(w, 400, "need {branch, bundle_b64}")
		return
	}
	f, err := os.CreateTemp("", "life-land-*.bundle")
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(bundle)
	f.Close()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	repo := s.cfg.Root
	git := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	ref := "refs/incoming/" + from
	if out, err := git("fetch", "--no-tags", "--force", f.Name(), "refs/heads/"+in.Branch+":"+ref); err != nil {
		jsonErr(w, 400, "bundle refused: "+out)
		return
	}
	sha, err := git("rev-parse", ref)
	if err != nil {
		jsonErr(w, 500, sha)
		return
	}
	if _, err := git("merge-base", "--is-ancestor", "main", sha); err != nil {
		jsonErr(w, 409, "main moved: rebase on main and land again")
		return
	}
	log, _ := git("log", "--oneline", "main.."+sha)
	if log == "" {
		jsonErr(w, 409, "nothing to land: main already has it")
		return
	}
	stat, _ := git("diff", "--stat", "main", sha)
	hub, _ := git("diff", "--name-only", "main", sha, "--", "hub")
	a, err := s.acts.Propose(landProposal(from, sha, log, stat, in.Say, repo, hub != ""))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

// landProposal: the one action that fast-forwards main to sha. When hub/
// changed, the hub is rebuilt and restarted after the action finishes (the
// restart waits, or the hub would kill its own run).
func landProposal(thread, sha, log, stat, say, repo string, hub bool) actions.Proposal {
	cmd := "git merge --ff-only " + sha
	if hub {
		cmd += " && make build && (nohup sh -c 'sleep 3; bash ops/hub.sh restart' >/dev/null 2>&1 &)"
	}
	payload, _ := json.Marshal(map[string]string{"cmd": cmd, "dir": repo})
	gated := true
	return actions.Proposal{Kind: "commit", Gated: &gated, ThreadID: thread, Source: "claude:thread:" + thread,
		Title:    fmt.Sprintf("Land %d commit(s) on main", strings.Count(log, "\n")+1),
		Detail:   log + "\n\n" + stat,
		Say:      say,
		ExecType: "shell", ExecPayload: payload, Standing: standingNote}
}

// standingNote: why a landing or an app build ran with no card. Each route
// files its row with Proposal.Standing = standingNote (decided_via
// "standing"): approved as it is filed, never a proposed card first, so
// nothing asks the owner to approve it. Every other proposal still waits for
// the owner.
const standingNote = "standing approval: a local commit and an app build need no card"

var shipThread = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// appShip builds the phone or desktop app from main as the owner's user: the
// session user cannot read the signing setup (app/local.xcconfig, the
// keychain). phone runs `make ship`, whose output ends with the install link
// the session puts in its one install ask; mac runs ops/install-mac.sh. why is
// what the build fixes, the action's title.
func (s *Server) appShip(w http.ResponseWriter, r *http.Request) {
	if s.cfg.SessionUser == "" {
		jsonErr(w, 409, "the ship lane is off (no session_user): run make ship or make mac")
		return
	}
	from := strings.TrimSpace(r.Header.Get("X-Life-Thread"))
	if from == "" || !shipThread.MatchString(from) {
		jsonErr(w, 403, "only a session ships through this route")
		return
	}
	var in struct {
		Target string `json:"target"`
		Why    string `json:"why"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json: "+err.Error())
		return
	}
	in.Why = strings.TrimSpace(in.Why)
	if in.Why == "" || len(in.Why) > 200 || strings.ContainsFunc(in.Why, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		jsonErr(w, 400, "need why: one line, at most 200 characters, what the build fixes")
		return
	}
	var cmd, title string
	switch in.Target {
	case "phone":
		title, cmd = "Build the phone app: "+in.Why, "make ship"
	case "mac":
		title, cmd = "Build the desktop app: "+in.Why, "bash ops/install-mac.sh"
	default:
		jsonErr(w, 400, "target: phone or mac")
		return
	}
	payload, _ := json.Marshal(map[string]string{"cmd": cmd, "dir": s.cfg.Root})
	gated := true
	a, err := s.acts.Propose(actions.Proposal{Kind: "other", Gated: &gated, ThreadID: from, Source: "claude:thread:" + from,
		Title: title, Detail: in.Why, ExecType: "shell", ExecPayload: payload, Standing: standingNote})
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
}
