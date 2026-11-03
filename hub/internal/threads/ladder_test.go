package threads

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// Preflight asks the CLI sessions run, as their user with its sign-in, from
// an empty folder of that user's; with no session user, the hub's own CLI.
func TestPreflightAsksTheSessionCLI(t *testing.T) {
	m := &Manager{ClaudeBin: "/owner/claude"}
	if c := m.sessionCLI(context.Background(), "--version"); c.Path != "/owner/claude" || len(c.Args) != 2 {
		t.Fatalf("owner: %v", c.Args)
	}
	m.RunAs = RunAs{User: "sessions", ClaudeBin: "/session/claude"}
	c := m.sessionCLI(context.Background(), "-p", "--model", "claude-opus-5-5")
	sh := c.Args[len(c.Args)-1]
	if c.Path != "/usr/bin/sudo" || strings.Join(c.Args[1:7], " ") != "-n -H -u sessions /bin/sh -c" || c.Dir != "/" {
		t.Fatalf("session: %v in %q", c.Args, c.Dir)
	}
	for _, want := range []string{`cd "$d"`, `.claude-token`, `'/session/claude' '-p' '--model' 'claude-opus-5-5'`, `rmdir "$d"; exit $rc`} {
		if !strings.Contains(sh, want) || strings.Contains(sh, "/owner/claude") {
			t.Fatalf("missing %q: %s", want, sh)
		}
	}
}

// Model ladder: the process is launched with the picked --model; a turn
// that dies on a plan limit is re-queued and relaunched on the next rung,
// with no "Session failed" ask: never stall on a limit.
func TestLimitFallsDownLadder(t *testing.T) {
	m, n, cmds := setup(t)
	ladder := []string{"claude-fable-5[1m]", "claude-opus-5"}
	var exhausted []string
	m.Model = func(string) string { return ladder[len(exhausted)] }
	m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	if c := (*cmds)[0]; !strings.Contains(c, "'--model' 'claude-fable-5[1m]'") {
		t.Fatal(c)
	}
	out := pendingOut(t, m, th.ID)
	if err := m.Send(th.ID, "and the bank"); err != nil {
		t.Fatal(err)
	}
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"result","subtype":"error","is_error":true,"result":"You've hit your limit · resets 3pm","total_cost_usd":0.01,"session_id":"sess-1"}
`)
	m.Poll()
	if len(exhausted) != 1 || exhausted[0] != "claude-fable-5[1m]" {
		t.Fatalf("exhausted=%v", exhausted)
	}
	// killed the fable process, launched an opus one with both messages
	if len(*cmds) != 3 || !strings.HasPrefix((*cmds)[1], "tmux kill-session -t life-th-"+th.ID+"-") || !strings.Contains((*cmds)[2], "'--model' 'claude-opus-5'") {
		t.Fatalf("%v", *cmds)
	}
	in := inFile(t, m, th.ID)
	if b, _ := os.ReadFile(in); !strings.Contains(string(b), "Do a long thing.") || !strings.Contains(string(b), "and the bank") {
		t.Fatal(string(b))
	}
	msgs, _ := m.Messages(th.ID, 50)
	var sys, queued int
	for _, x := range msgs {
		if x.Role == "system" && strings.Contains(x.Text, "restarting on claude-opus-5") {
			sys++
		}
		if x.Queued {
			queued++
		}
	}
	if sys != 1 || queued != 0 {
		t.Fatalf("%+v", msgs)
	}
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 0 || len(n.msgs) != 0 {
		t.Fatalf("asks=%v notes=%v", asks, n.msgs)
	}
	if th, _ = m.Get(th.ID); th.Status != "running" {
		t.Fatalf("%+v", th)
	}
	var live int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&live)
	if live != 1 {
		t.Fatalf("live runs: %d", live)
	}
}

// A turn another session's relay opened falls down the ladder with that relay:
// left off the queue, the relaunch started nothing and the session read
// "running" with no process.
func TestLimitRelaunchKeepsTheRelay(t *testing.T) {
	m, _, cmds := setup(t)
	ladder := []string{"claude-fable-5", "claude-opus-5"}
	var exhausted []string
	m.Model = func(string) string { return ladder[len(exhausted)] }
	m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
	th, _ := m.Create("", "life", "", "Watch the money.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"result","subtype":"success","is_error":false,"result":"On it.","total_cost_usd":0.01,"session_id":"sess-1"}
`)
	m.Poll()
	if err := m.run(th.ID, "system", "relay", "From the assistant: the quarterly review is yours.", nil); err != nil {
		t.Fatal(err)
	}
	appendOut(t, out, `{"type":"result","subtype":"error","is_error":true,"result":"You've reached your Fable limit.","total_cost_usd":0,"session_id":"sess-1"}
`)
	m.Poll()
	if len(*cmds) != 3 || !strings.Contains((*cmds)[2], "'--model' 'claude-opus-5'") {
		t.Fatalf("not relaunched: %v", *cmds)
	}
	if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "the quarterly review is yours") {
		t.Fatalf("relay lost: %s", b)
	}
	var live int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&live)
	if live != 1 {
		t.Fatalf("live runs: %d", live)
	}
}

// A standing session's check-in (queued as role system, kind checkin) that
// hits a plan limit is re-run on the next rung with its own words: left off
// the queue, the relaunch started nothing, the thread sat `running` with no
// process and fireStanding held every later occurrence (#85). Both exits: a
// result envelope and a process killed outright.
func TestLimitRelaunchKeepsTheCheckIn(t *testing.T) {
	for _, viaStderr := range []bool{false, true} {
		m, n, cmds := setup(t)
		ladder := []string{"claude-fable-5", "claude-opus-5"}
		var exhausted []string
		m.Model = func(string) string { return ladder[len(exhausted)] }
		m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
		th, _ := m.Create("", "life", "", "Watch the money.", "", "", nil)
		complete(t, m, th.ID, "On it.")
		if err := m.CheckIn(th.ID, "checkin"); err != nil {
			t.Fatal(err)
		}
		before := len(*cmds)
		out := pendingOut(t, m, th.ID)
		if viaStderr {
			os.WriteFile(out+".err", []byte("You've reached your Fable 5 limit. Switch to another model, or manage usage credits at claude.ai/settings/usage?from=cc_cli_limit_message, to continue."), 0o644)
			os.WriteFile(out+".done", nil, 0o644)
		} else {
			appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"result","subtype":"error","is_error":true,"result":"You've reached your Fable limit.","total_cost_usd":0,"session_id":"sess-1"}
`)
		}
		m.Poll()
		if len(exhausted) != 1 || len(*cmds) <= before || !strings.Contains((*cmds)[len(*cmds)-1], "'--model' 'claude-opus-5'") {
			t.Fatalf("stderr=%v not relaunched: exhausted=%v %v", viaStderr, exhausted, *cmds)
		}
		if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "[scheduled check-in]") || strings.Contains(string(b), "[restart]") {
			t.Fatalf("stderr=%v check-in lost: %s", viaStderr, b)
		}
		var live int
		m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&live)
		if th, _ = m.Get(th.ID); th.Status != "running" || live != 1 {
			t.Fatalf("stderr=%v status=%s live runs=%d", viaStderr, th.Status, live)
		}
		asks, _ := m.ListAsks("active", th.ID, 10)
		for _, a := range asks {
			if a.Kind == "error" {
				t.Fatalf("stderr=%v error card: %+v", viaStderr, a)
			}
		}
		if len(n.msgs) != 0 {
			t.Fatalf("stderr=%v notes=%v", viaStderr, n.msgs)
		}
	}
}

// The safety net under #85: a relaunch whose launch starts nothing (nothing
// left on the queue) settles the status instead of leaving the thread
// `running` with no process, which held every later check-in.
func TestRelaunchThatStartsNothingSettles(t *testing.T) {
	m, _, cmds := setup(t)
	th, _ := m.Create("", "life", "", "Watch the money.", "", "", nil)
	complete(t, m, th.ID, "On it.")
	m.db.Exec(`UPDATE threads SET status='running' WHERE id=?`, th.ID)
	before := len(*cmds)
	th, _ = m.Get(th.ID)
	m.mu.Lock()
	m.relaunch(th, "relaunch")
	m.mu.Unlock()
	if len(*cmds) != before {
		t.Fatalf("launched with an empty queue: %v", (*cmds)[before:])
	}
	if th, _ = m.Get(th.ID); th.Status == "running" {
		t.Fatalf("status still running with no process")
	}
}

// A plan limit that kills the process outright (the CLI prints it on stderr
// and exits with no result envelope) must also step down the ladder, not land
// on the owner's board as a raw "Session failed" card.
func TestLimitOnStderrFallsDownLadder(t *testing.T) {
	m, n, cmds := setup(t)
	ladder := []string{"claude-fable-5", "claude-opus-5"}
	var exhausted []string
	m.Model = func(string) string { return ladder[len(exhausted)] }
	m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	os.WriteFile(out+".err", []byte("You've reached your Fable 5 limit. Switch to another model, or manage usage credits at claude.ai/settings/usage?from=cc_cli_limit_message, to continue."), 0o644)
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	if len(exhausted) != 1 || exhausted[0] != "claude-fable-5" {
		t.Fatalf("exhausted=%v", exhausted)
	}
	if len(*cmds) != 3 || !strings.Contains((*cmds)[2], "'--model' 'claude-opus-5'") {
		t.Fatalf("%v", *cmds)
	}
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 0 || len(n.msgs) != 0 {
		t.Fatalf("asks=%v notes=%v", asks, n.msgs)
	}
	if th, _ = m.Get(th.ID); th.Status != "running" {
		t.Fatalf("%+v", th)
	}
}

// A CLI that does not know a rung's model id kills every turn on it with a
// 400, dropping the owner's messages. That error
// takes the same lane as a limit: rung closed, messages requeued, next rung.
func TestUnsupportedModelFallsDownLadder(t *testing.T) {
	m, n, cmds := setup(t)
	ladder := []string{"claude-opus-5-5", "claude-sonnet-5"}
	var exhausted []string
	m.Model = func(string) string { return ladder[len(exhausted)] }
	m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
	th, _ := m.Create("", "life", "", "Triage the responses.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"result","subtype":"error","is_error":true,"result":"API Error: 400 Claude Code 2.1.276 does not support this model; version 2.1.280 or newer is required. [claude-code:unrecognized_model] {\"model\":\"claude-opus-5-5\",\"query_source\":\"sdk\"}","total_cost_usd":0,"session_id":"sess-1"}
`)
	m.Poll()
	if len(exhausted) != 1 || exhausted[0] != "claude-opus-5-5" {
		t.Fatalf("exhausted=%v", exhausted)
	}
	if len(*cmds) != 3 || !strings.Contains((*cmds)[2], "'--model' 'claude-sonnet-5'") {
		t.Fatalf("%v", *cmds)
	}
	if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "Triage the responses.") {
		t.Fatal(string(b))
	}
	msgs, _ := m.Messages(th.ID, 50)
	var sys int
	for _, x := range msgs {
		if x.Role == "system" && strings.HasPrefix(x.Text, "the CLI does not support claude-opus-5-5 (") {
			sys++
		}
	}
	if sys != 1 {
		t.Fatalf("%+v", msgs)
	}
	if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 0 || len(n.msgs) != 0 {
		t.Fatalf("asks=%v notes=%v", asks, n.msgs)
	}
}

// With nowhere lower to go (bottom rung, or a pinned id off the ladder) the
// ladder hands back the same model: that is a failure card, not a relaunch
// into the same error forever.
func TestRungErrorOnBottomRungStops(t *testing.T) {
	m, _, cmds := setup(t)
	m.Model = func(string) string { return "claude-sonnet-5" }
	m.ModelExhausted = func(string) {}
	th, _ := m.Create("", "life", "", "Do a thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"result","subtype":"error","is_error":true,"result":"API Error: 400 [claude-code:unrecognized_model]","total_cost_usd":0,"session_id":"sess-1"}
`)
	m.Poll()
	for _, c := range (*cmds)[1:] {
		if strings.Contains(c, "'--model'") {
			t.Fatalf("relaunched: %v", *cmds)
		}
	}
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Kind != "error" || asks[0].Title != "Session stopped: the Claude CLI is too old for this model" {
		t.Fatalf("%+v", asks)
	}
}

// Startup preflight: a rung the CLI rejects is closed and raised once.
func TestPreflightClosesRejectedRung(t *testing.T) {
	m, _, _ := setup(t)
	m.CreateIdle("hub", "Hub", "life")
	var exhausted []string
	m.ModelExhausted = func(model string) { exhausted = append(exhausted, model) }
	m.Probe = func(model string) (string, error) {
		switch model {
		case "claude-opus-5-5":
			return "API Error: 400 Claude Code 2.1.276 does not support this model; version 2.1.280 or newer is required.", errors.New("exit status 1")
		case "claude-sonnet-5":
			return "Connection error", errors.New("exit status 1") // not ours to close
		}
		return "OK\n", nil
	}
	ladder := []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5"}
	for i := 0; i < 2; i++ {
		if bad := m.PreflightModels(ladder); len(bad) != 1 || bad[0] != "claude-opus-5-5" {
			t.Fatalf("bad=%v", bad)
		}
	}
	if len(exhausted) != 2 || exhausted[0] != "claude-opus-5-5" {
		t.Fatalf("exhausted=%v", exhausted)
	}
	asks, _ := m.ListAsks("active", "hub", 10)
	if len(asks) != 1 || !strings.Contains(asks[0].Title, "claude-opus-5-5") {
		t.Fatalf("%+v", asks)
	}
}

// When no rung is left, the card at least reads as English.
func TestFailureTitleIsReadable(t *testing.T) {
	limit := "You've reached your Fable 5 limit. Switch to another model, or manage usage credits at claude.ai/settings/usage?from=cc_cli_limit_message, to continue."
	if got := failureTitle(limit); got != "Session paused: plan limit reached on every model" {
		t.Fatal(got)
	}
	if got := failureTitle("API Error: Your computer went to sleep mid-response. The response above may be incomplete."); got != "Session interrupted: the Mac slept mid-answer" {
		t.Fatal(got)
	}
	if got := failureTitle("claude: boom"); got != "Session failed: claude: boom" {
		t.Fatal(got)
	}
}
