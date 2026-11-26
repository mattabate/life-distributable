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
// for it and prints how it ended. A landing that changes the approval gate
// (gatePaths) is the owner's card instead, in the session's own words (say).
// Off (409) unless session_user is set.
var landBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func (s *Server) land(w http.ResponseWriter, r *http.Request) {
	if s.cfg.SessionUser == "" {
		jsonErr(w, 409, "the land lane is off (no session_user): commit in the main checkout")
		return
	}
	from := strings.TrimSpace(r.Header.Get("X-Life-Thread"))
	// The thread names refs/incoming/<from> in the fetch refspec, so it is a
	// thread id or nothing, as appShip and appWalk hold it (#148).
	if from == "" || !shipThread.MatchString(from) {
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
		out, err := gitCmd(ctx, repo, args...).CombinedOutput()
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
	hub, _ := git("--no-literal-pathspecs", "diff", "--name-only", "main", sha, "--", ":(icase)hub")
	gate := gateTouched(git, "main", sha)
	// A gate landing is the owner's card, and the session's spoken words are
	// that card (what it wants to change and why): no words, no card.
	in.Say = strings.TrimSpace(in.Say)
	if gate != "" && (in.Say == "" || len(in.Say) > 700 || strings.ContainsFunc(in.Say, func(c rune) bool { return c < 0x20 || c == 0x7f })) {
		jsonErr(w, 400, "this changes the approval gate ("+strings.ReplaceAll(gate, "\n", ", ")+
			"): land again with ops/land.sh --say \"I need your approval to change <what, in plain words>, because <why>.\" (one line, at most 700 characters, no file names or shas)")
		return
	}
	a, err := s.acts.Propose(landProposal(from, sha, log, in.Say, repo, hub != "", gate))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

// landProposal: the one action that puts sha on main. ops/land-incoming.sh
// runs make test on sha in a throwaway worktree first and fast-forwards main
// only when it passes, so a red landing leaves main untouched and never
// restarts the hub (#208). When hub/ changed, the hub is rebuilt and
// restarted after the action finishes (the restart waits, or the hub would
// kill its own run). gate lists the gatePaths files the landing changes:
// non-empty = a proposed card for the owner, never the standing yes. The
// detail is the commits' own words and nothing else: no shas, no file list,
// no diff stat, no gate file names.
func landProposal(thread, sha, log, say, repo string, hub bool, gate string) actions.Proposal {
	cmd := "bash ops/land-incoming.sh " + sha
	if hub {
		cmd += " && make -f Makefile build && (nohup sh -c 'sleep 3; bash ops/hub.sh restart' >/dev/null 2>&1 &)"
	}
	payload, _ := json.Marshal(map[string]string{"cmd": cmd, "dir": repo})
	gated := true
	p := actions.Proposal{Kind: "commit", Gated: &gated, ThreadID: thread, Source: "claude:thread:" + thread,
		Title:    fmt.Sprintf("Land %d commit(s) on main", strings.Count(log, "\n")+1),
		Detail:   landWords(log),
		Say:      say,
		ExecType: "shell", ExecPayload: payload, Standing: standingNote}
	if gate = strings.TrimSpace(gate); gate != "" {
		// The title is the action and the session's say is the what and why;
		// the commits' words are the detail.
		p.Standing = ""
		p.Title = "Change the approval gate"
	}
	return p
}

// landWords: `git log --oneline` with each line's short sha cut, a blank
// line between commits.
func landWords(log string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
		if _, rest, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			out = append(out, rest)
		}
	}
	return strings.Join(out, "\n\n")
}

// gatePaths: the files that decide what a session may do without the owner,
// and the ones the hub runs as the owner on every landing or build. A landing
// that touches one is the owner's card (approved with the decider code), not
// the standing yes, or a session fooled by a page or a mail could loosen the
// gate and the hub would rebuild itself as the owner. Kept short on purpose:
// a path joins only if it is the gate or runs as the owner on every landing.
var gatePaths = []string{
	"hub/internal/actions",                 // the gate, the decider code
	"hub/internal/server/actions.go",       // deciderOK: where approve/deny checks the code
	"hub/internal/server/prompts.go",       // deciderOK's call for an approval sent as a reply
	"hub/internal/server/land.go",          // this lane and this list
	"hub/internal/server/sessionlimits.go", // the owner-only routes, the open-route list
	"hub/internal/server/sessiontoken.go",  // auth: who the hub takes for the owner or a session
	"hub/internal/sched",                   // the tool-grant check
	"hub/internal/threads/runner.go",       // how a session is started
	"hub/cmd/hub/main.go",                  // the free-shell list, the wiring
	"ops/schedule.json",                    // the tool list
	"Makefile",                             // make test/build run as the owner
	// GNU make reads GNUmakefile, then makefile, before Makefile: every
	// owner-side make pins -f Makefile, and these stay the owner's card too
	// (#225).
	"GNUmakefile",
	"makefile",
	"ops/land-incoming.sh",
	"ops/hub.sh",
	"ops/ota.sh",
	"ops/install-mac.sh",
	"ops/decider-set.sh",
	":(glob)ops/session-user-*",
}

// gateTouched: the gatePaths files a range changes. A git error is not "none":
// a diff that failed or timed out reads as the gate touched, so the landing is
// the owner's card.
//
// Every gate path matches in any case (icase): the Mac's disk ignores case, so
// GNUMakefile is what make opens as GNUmakefile and OPS/hub.sh overwrites
// ops/hub.sh at checkout (#225). --no-literal-pathspecs: GIT_LITERAL_PATHSPECS
// in the hub's environment would read every spec here as a literal name, so
// the gate would match nothing.
//
// A name outside ASCII is the owner's card too: git's icase folds ASCII only,
// and the disk folds more (hub.ſh, a long s, opens as hub.sh), so no pathspec
// can say which of them the Mac reads as a gate file.
func gateTouched(git func(...string) (string, error), rng ...string) string {
	args := append(append([]string{"--no-literal-pathspecs", "diff", "--name-only"}, rng...), "--")
	for _, p := range gatePaths {
		if magic, ok := strings.CutPrefix(p, ":("); ok {
			args = append(args, ":(icase,"+magic)
		} else {
			args = append(args, ":(icase)"+p)
		}
	}
	out, err := git(args...)
	if err != nil {
		return "(the check of which gate files it changes failed)"
	}
	// --no-renames: a rename lists both names, so a non-ASCII old name counts.
	names, err := git(append(append([]string{"--no-literal-pathspecs", "diff", "--name-only", "--no-renames", "-z"}, rng...), "--")...)
	if err != nil {
		return "(the check of which gate files it changes failed)"
	}
	for _, r := range names {
		if r > 0x7f {
			return strings.TrimSpace(out + "\n(a file name outside ASCII, which the Mac may open as a gate file)")
		}
	}
	return out
}

// gitCmd: git in repo with no GIT_* variable from the hub's environment
// (GIT_DIR, GIT_*_PATHSPECS, GIT_CONFIG_*…), so no GIT_* variable can point
// the landing's diffs elsewhere or change how they match.
func gitCmd(ctx context.Context, repo string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			c.Env = append(c.Env, kv)
		}
	}
	if c.Env == nil {
		c.Env = []string{}
	}
	return c
}

// standingNote: why a landing or an app build ran with no card. Each route
// files its row with Proposal.Standing = standingNote (decided_via
// "standing"): approved as it is filed, never a proposed card first, so
// nothing asks the owner to approve it. Every other proposal still waits for
// the owner. One exception: a landing that touches gatePaths is the owner's
// card.
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
		title, cmd = "Build the phone app: "+in.Why, "make -f Makefile ship"
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
