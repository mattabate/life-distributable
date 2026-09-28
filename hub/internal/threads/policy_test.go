package threads

import (
	"os"
	"strings"
	"testing"

	"life/hub/internal/spend"
)

// The model policy: a check-in
// launches on the SESSION'S OWN rung with the CLI's own budget cap — the
// same rung a message takes (judgment = top of the ladder, build = opus),
// never a cheaper one; sonnet only when the session is pinned to it. No
// live process ever gets --max-turns. Nothing caps a turn's tool calls, on
// any launch: the backstop that did was removed.
func TestPolicyLaunchArgs(t *testing.T) {
	m, _, cmds := setup(t)
	m.Policy = spend.DefaultPolicy()
	th, _ := m.Create("", "life", "", "Look at the ledger.", "", "", nil)
	if c := (*cmds)[0]; strings.Contains(c, "'--model'") || strings.Contains(c, "'--effort'") || strings.Contains(c, "'--max-turns'") {
		t.Fatal(c)
	}
	complete(t, m, th.ID, "Looked.")
	if err := m.CheckIn(th.ID, "checkin"); err != nil {
		t.Fatal(err)
	}
	// An unpinned session's check-in: the ladder top (no --model), no
	// effort pin, the cap.
	if c := (*cmds)[1]; strings.Contains(c, "'--model'") || strings.Contains(c, "'--effort'") || !strings.Contains(c, "'--max-budget-usd' '8.00'") || strings.Contains(c, "'--max-turns'") {
		t.Fatal(c)
	}
	var model, effort string
	m.db.QueryRow(`SELECT model, effort FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&model, &effort)
	if model != "" || effort != "" {
		t.Fatal(model, effort)
	}
	complete(t, m, th.ID, "Nothing due.")
	// The rote lane: a session pinned to sonnet checks in on sonnet.
	if _, err := m.Update(th.ID, map[string]string{"model_class": "claude-sonnet-5"}); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckIn(th.ID, "checkin"); err != nil {
		t.Fatal(err)
	}
	if c := (*cmds)[2]; !strings.Contains(c, "'--model' 'claude-sonnet-5'") || strings.Contains(c, "'--effort'") {
		t.Fatal(c)
	}
	complete(t, m, th.ID, "Exported.")
	// class by hand: build threads start on opus
	if _, err := m.Update(th.ID, map[string]string{"model_class": "bogus"}); err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatal(err)
	}
	if th2, err := m.Update(th.ID, map[string]string{"model_class": "build"}); err != nil || th2.ModelClass != "build" {
		t.Fatal(err, th2)
	}
	if err := m.Send(th.ID, "ship it"); err != nil {
		t.Fatal(err)
	}
	if c := (*cmds)[3]; !strings.Contains(c, "'--model' 'claude-opus-5-5'") || strings.Contains(c, "'--effort'") || strings.Contains(c, "'--max-turns'") {
		t.Fatal(c)
	}
	complete(t, m, th.ID, "Shipped.")
	// …and its check-ins start on opus too, not a rung lower.
	if err := m.CheckIn(th.ID, "checkin"); err != nil {
		t.Fatal(err)
	}
	if c := (*cmds)[4]; !strings.Contains(c, "'--model' 'claude-opus-5-5'") || !strings.Contains(c, "'--max-budget-usd' '8.00'") {
		t.Fatal(c)
	}
}

// Lean wakes: a scheduled check-in starts a process with NO --resume — it
// pays for a preamble and a short recap instead of the whole transcript —
// while the owner's own message still resumes.
func TestLeanCheckIn(t *testing.T) {
	m, _, cmds := setup(t)
	m.Policy = spend.DefaultPolicy()
	m.Notifier = &cardNfy{} // the recap quotes the spoken message, as live
	th, _ := m.Create("Ledger watch", "life", "make-more-money", "Watch the ledger.", "weekly@Mon 09:00", "", nil)
	complete(t, m, th.ID, "Watching. Nothing moved.")
	if err := m.CheckIn(th.ID, "checkin"); err != nil {
		t.Fatal(err)
	}
	c := (*cmds)[1]
	if strings.Contains(c, "--resume") {
		t.Fatalf("a lean check-in must not resume: %s", c)
	}
	p, err := os.ReadFile(strings.TrimSuffix(pendingOut(t, m, th.ID), ".out") + ".prompt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"LEAN WAKE", "make-more-money", "you, last time: Watching. Nothing moved", "the owner: Watch the ledger."} {
		if !strings.Contains(string(p), want) {
			t.Fatalf("recap missing %q:\n%s", want, p)
		}
	}
	complete(t, m, th.ID, "Still nothing.")
	// The owner talking to the same thread keeps their conversation.
	if err := m.Send(th.ID, "what did you find?"); err != nil {
		t.Fatal(err)
	}
	if c := (*cmds)[2]; !strings.Contains(c, "'--resume' 'sess-123'") {
		t.Fatalf("the owner's message must resume: %s", c)
	}
	if p, _ := os.ReadFile(strings.TrimSuffix(pendingOut(t, m, th.ID), ".out") + ".prompt"); strings.Contains(string(p), "LEAN WAKE") {
		t.Fatalf("no recap on a resumed turn:\n%s", p)
	}
}

// A message arriving at an idle process on the wrong rung (a check-in's,
// when a configured policy puts check-ins on sonnet — the default no longer
// does) must not run there: the hub closes that process and the
// message waits for the relaunch on the right rung — never two processes on
// one session.
func TestMessageRelaunchesOffCheckInModel(t *testing.T) {
	m, _, cmds := setup(t)
	m.Policy = (&spend.Policy{Triggers: map[string]spend.Rule{"checkin": {Model: "claude-sonnet-5", Fresh: true}}}).Merge(spend.DefaultPolicy())
	m.Model = func(start string) string {
		if start == "" {
			return "claude-fable-5"
		}
		return start
	}
	th, _ := m.Create("", "life", "", "Watch the ledger.", "", "", nil)
	if !strings.Contains((*cmds)[0], "'--model' 'claude-fable-5'") {
		t.Fatal((*cmds)[0])
	}
	complete(t, m, th.ID, "Watching.")
	if err := m.CheckIn(th.ID, "checkin"); err != nil {
		t.Fatal(err)
	}
	out := pendingOut(t, m, th.ID)
	in := inFile(t, m, th.ID)
	// the check-in's turn ends, the sonnet process stays alive (idle)
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-2"}
{"type":"result","subtype":"success","is_error":false,"result":"Nothing due.","total_cost_usd":0.02,"session_id":"sess-2"}
`)
	m.Poll()
	n := len(*cmds)
	if err := m.Send(th.ID, "actually, buy 3 shares"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(in); !strings.HasSuffix(string(b), eofSentinel+"\n") || strings.Contains(string(b), "buy 3 shares") {
		t.Fatalf("in-file: %q", string(b))
	}
	if len(*cmds) != n {
		t.Fatalf("launched early: %v", (*cmds)[n:])
	}
	msgs, _ := m.Messages(th.ID, 1)
	if !msgs[0].Queued {
		t.Fatalf("%+v", msgs[0])
	}
	// the sonnet process exits → the queued message launches on fable
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	if len(*cmds) != n+1 || !strings.Contains((*cmds)[n], "'--model' 'claude-fable-5'") || !strings.Contains((*cmds)[n], "'--resume' 'sess-2'") {
		t.Fatalf("%v", (*cmds)[n:])
	}
	if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "buy 3 shares") {
		t.Fatal(string(b))
	}
	msgs, _ = m.Messages(th.ID, 1)
	if msgs[0].Queued {
		t.Fatalf("%+v", msgs[0])
	}
}

// No tool-call cap, at any number: an old backstop steered a review turn
// into stopping mid-pass, and deep explorations are allowed. A long turn runs:
// nothing is written to its stdin, no session is killed, no system card is
// filed, and the run is still live at the end of it.
func TestNoToolCallCap(t *testing.T) {
	m, n, cmds := setup(t)
	m.Policy = spend.DefaultPolicy()
	th, _ := m.Create("", "life", "", "Audit everything.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	in := inFile(t, m, th.ID)
	before, _ := os.ReadFile(in)
	nCmds := len(*cmds)
	call := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/x"}}]}}` + "\n"
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-3"}`+"\n"+strings.Repeat(call, 600))
	m.Poll()
	if b, _ := os.ReadFile(in); string(b) != string(before) {
		t.Fatalf("the hub steered a long turn: %s", string(b))
	}
	for _, c := range (*cmds)[nCmds:] {
		if strings.HasPrefix(c, "tmux kill-session") {
			t.Fatalf("the hub killed a long turn: %v", *cmds)
		}
	}
	if msgs, _ := m.Messages(th.ID, 5); len(msgs) > 0 {
		for _, msg := range msgs {
			if msg.Role == "system" {
				t.Fatalf("system card on a long turn: %+v", msg)
			}
		}
	}
	if len(n.msgs) != 0 {
		t.Fatalf("pushed on a long turn: %v", n.msgs)
	}
	var live int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&live)
	if live != 1 {
		t.Fatalf("live=%d, the turn should still be running", live)
	}
}
