package actions

import (
	"encoding/json"
	"strings"
	"testing"
)

// A gated card that names an ops/ script its session cannot run, with no
// command on it, is refused before it reaches the owner: approved, it would
// go back to a session that cannot carry it out. One naming what the
// session may run, or carrying the command itself, goes through.
func TestProposeRefusesACardItsSessionCannotCarryOut(t *testing.T) {
	q, _, _ := setup(t)
	q.SessionTools = []string{"Bash(lifectl:*)", "Bash(bash ops/land.sh:*)", "Bash(ops/py.sh:*)"}
	q.Checkout = "/srv/life"
	q.CloneRoot = "/srv/clones"
	_, err := q.Propose(Proposal{Title: "File the statement", Kind: "share",
		Detail: "ops/upload.sh put /srv/life/data/imports/intake/x.pdf docs/x.pdf (never overwrites)"})
	if err == nil || !strings.Contains(err.Error(), "ops/upload.sh put") || !strings.Contains(err.Error(), "--exec shell") {
		t.Fatalf("want a refusal naming the put: %v", err)
	}
	for _, d := range []string{"Then I run bash ops/land.sh and report.", "Runs /srv/life/ops/py.sh lots.py f.json",
		"Runs /srv/clones/sess-1/ops/py.sh lots.py f.json", "Post 3 comments on the issues."} {
		if _, err := q.Propose(Proposal{Title: "Land it", Kind: "commit", Detail: d}); err != nil {
			t.Fatalf("%q: %v", d, err)
		}
	}
	if _, err := q.Propose(Proposal{Title: "File it", Kind: "share", ExecType: "shell",
		ExecPayload: json.RawMessage(`{"cmd":"bash ops/upload.sh put a b","dir":"/srv/life"}`), Detail: "bash ops/upload.sh put a b"}); err != nil {
		t.Fatal(err)
	}
	// Ungated: nothing waits on an Approve, so nothing to refuse.
	if _, err := q.Propose(Proposal{Title: "Note", Kind: "other", Detail: "ops/upload.sh put a b"}); err != nil {
		t.Fatal(err)
	}
}
