package threads

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A 500 from the API is the provider's blip: the hub replays the turn itself,
// twice, and the owner is told nothing. Only the third failure becomes a card —
// and it is an `error` card, titled with the code rather than the CLI's
// paragraph.
func TestTransientAPIErrorRetriesThenCards(t *testing.T) {
	m, n, cmds := setup(t)
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	fail := func() {
		out := pendingOut(t, m, th.ID)
		appendOut(t, out, `{"type":"result","subtype":"error","is_error":true,"result":"API Error: 500 Internal server error. This is a server-side issue, usually temporary — try again in a moment.","total_cost_usd":0.01,"session_id":"sess-1"}
`)
		m.Poll()
	}
	for i := 1; i <= maxAutoRetries; i++ {
		fail()
		if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 0 || len(n.msgs) != 0 {
			t.Fatalf("attempt %d: asks=%v notes=%v", i, asks, n.msgs)
		}
		if th, _ := m.Get(th.ID); th.Status != "running" {
			t.Fatalf("attempt %d: %+v", i, th)
		}
		// The opening message went back on the queue and into the new process.
		if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "Do a long thing.") {
			t.Fatalf("attempt %d: in=%q", i, string(b))
		}
	}
	// Killed and relaunched once per retry, on top of the first launch.
	if len(*cmds) != 1+2*maxAutoRetries {
		t.Fatalf("%v", *cmds)
	}
	fail()
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Kind != "error" || asks[0].Title != "Session stopped: Claude API 500 (server error)" {
		t.Fatalf("%+v", asks)
	}
	if !strings.Contains(asks[0].Detail, "server-side issue") {
		t.Fatalf("detail lost: %q", asks[0].Detail)
	}
	if th, _ := m.Get(th.ID); th.Status != "needs_you" {
		t.Fatalf("%+v", th)
	}
}

// The CLI's self-update reinstalls its npm package, and the bin link is gone
// for the seconds that takes; a session launched in that gap died on "bash:
// …/bin/claude: No such file or directory". The tmux
// command waits for the binary before running it, outside the hub's lock.
func TestLaunchWaitsForTheCLIBinary(t *testing.T) {
	m, _, cmds := setup(t)
	m.Create("", "life", "", "Hello.", "", "", nil)
	if len(*cmds) != 1 {
		t.Fatalf("%v", *cmds)
	}
	cmd := (*cmds)[0]
	wait := "while [ ! -x '/usr/local/bin/claude' ] && [ $i -lt 90 ]; do sleep 1;"
	if !strings.Contains(cmd, wait) {
		t.Fatalf("no wait for the binary: %s", cmd)
	}
	if strings.Index(cmd, wait) > strings.Index(cmd, " feed ") {
		t.Fatalf("wait must come before the run: %s", cmd)
	}
}

// Restart on the card: the failed turn's own message runs again and the card
// leaves the board (the board is the owner's to-do list).
func TestRetryAskReplaysTheTurn(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	os.WriteFile(out+".err", []byte("claude: killed by signal"), 0o644)
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Kind != "error" {
		t.Fatalf("%+v", asks)
	}
	if _, err := m.RetryAsk(asks[0].ID); err != nil {
		t.Fatal(err)
	}
	a, _ := m.GetAsk(asks[0].ID)
	if a.State != "done" || a.ResolvedBy != "app" {
		t.Fatalf("%+v", a)
	}
	if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "Do a long thing.") {
		t.Fatalf("in=%q", string(b))
	}
	if th, _ := m.Get(th.ID); th.Status != "running" {
		t.Fatalf("%+v", th)
	}
	// Nothing to restart twice: the card is closed.
	if _, err := m.RetryAsk(asks[0].ID); err == nil {
		t.Fatal("second restart should fail")
	}
}

// An error result leaves its process alive and idle. Restart on that card
// closes it and replays the turn on a fresh process once it has exited, never
// a second process resuming the same Claude session beside it; and a process
// that exits while a newer one has a turn in flight starts no third
// (life-distributable#57).
func TestRestartNeverRunsTwoProcessesOnOneSession(t *testing.T) {
	launches := func(cmds []string) int {
		k := 0
		for _, c := range cmds {
			if strings.Contains(c, "new-session") {
				k++
			}
		}
		return k
	}
	live := func(m *Manager, id string) int {
		var k int
		m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL AND eof=0`, id).Scan(&k)
		return k
	}
	t.Run("restart on an idle failed process", func(t *testing.T) {
		m, _, cmds := setup(t)
		th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
		out := pendingOut(t, m, th.ID)
		appendOut(t, out, `{"type":"result","subtype":"error","is_error":true,"result":"API Error: 400 invalid_request_error: tool name too long","total_cost_usd":0.01,"session_id":"sess-1"}
`)
		m.Poll()
		asks, _ := m.ListAsks("active", th.ID, 10)
		if len(asks) != 1 || asks[0].Kind != "error" || live(m, th.ID) != 1 {
			t.Fatalf("precondition: asks=%+v live=%d", asks, live(m, th.ID))
		}
		if _, err := m.RetryAsk(asks[0].ID); err != nil {
			t.Fatal(err)
		}
		if n := launches(*cmds); n != 1 || live(m, th.ID) != 0 {
			t.Fatalf("second process beside the idle one: launches=%d live=%d", n, live(m, th.ID))
		}
		os.WriteFile(out+".done", nil, 0o644)
		m.Poll()
		if n := launches(*cmds); n != 2 || live(m, th.ID) != 1 {
			t.Fatalf("no replay once it exited: launches=%d live=%d", n, live(m, th.ID))
		}
		if b, _ := os.ReadFile(inFile(t, m, th.ID)); !strings.Contains(string(b), "Do a long thing.") {
			t.Fatalf("in=%q", b)
		}
	})
	t.Run("old process exits under a busy one", func(t *testing.T) {
		m, _, cmds := setup(t)
		th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
		outA := pendingOut(t, m, th.ID)
		appendOut(t, outA, `{"type":"result","subtype":"success","is_error":false,"result":"Done.","total_cost_usd":0.01,"session_id":"sess-1"}
`)
		m.Poll()
		m.db.Exec(`UPDATE thread_runs SET last_output=? WHERE thread_id=?`, ts(time.Now().Add(-idleTTL-time.Minute)), th.ID)
		m.Poll() // A is told to close
		if err := m.Send(th.ID, "one more"); err != nil {
			t.Fatal(err)
		}
		if n := launches(*cmds); n != 2 {
			t.Fatalf("precondition: B not launched: launches=%d", n)
		}
		if err := m.run(th.ID, "system", "relay", "Over to you.", nil); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(outA+".done", nil, 0o644)
		m.Poll()
		if n := launches(*cmds); n != 2 || live(m, th.ID) != 1 {
			t.Fatalf("third process while B is busy: launches=%d live=%d", n, live(m, th.ID))
		}
	})
}

// Only the hub's own error cards get a Restart; a read/decision ask must not.
func TestRetryAskRefusesOrdinaryAsks(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	a, err := m.AddAsk(th.ID, "", "Send me the token", "", "access", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RetryAsk(a.ID); err == nil {
		t.Fatal("an access ask is not restartable")
	}
}

// A dead Claude login: Restart could only fail again (the card vanished and
// came back). The card says so, and once any turn gets through the hub
// restarts it by itself.
func TestAuthFailureResumesOnceLoginWorks(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	os.WriteFile(out+".err", []byte("Failed to authenticate: OAuth session expired and could not be refreshed"), 0o644)
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Title != "Session stopped: Claude login expired · resumes once it works" {
		t.Fatalf("%+v", asks)
	}
	m.ResumeAuthFailed()
	a, _ := m.GetAsk(asks[0].ID)
	if a.State != "done" || a.ResolvedBy != "hub" {
		t.Fatalf("%+v", a)
	}
	if th, _ := m.Get(th.ID); th.Status != "running" {
		t.Fatalf("%+v", th)
	}
}

func TestFailureTitleSaysTheCode(t *testing.T) {
	for text, want := range map[string]string{
		"API Error: 529 Overloaded. This is a server-side issue, usually temporary — try again in a moment.": "Session stopped: Claude API 529 (overloaded)",
		// 429 is not here: a rate limit is the model ladder's, checked first.
		"API Error: 503 upstream unavailable":                      "Session stopped: Claude API 503 (server error)",
		"API Error: 400 invalid_request_error: tool name too long": "Session stopped: Claude API 400 (error)",
		"claude: boom": "Session failed: claude: boom",
	} {
		if got := failureTitle(text); got != want {
			t.Fatalf("%q → %q, want %q", text, got, want)
		}
	}
	// A 400 is our own bad request: it would fail the same way twice.
	if transientFailure("API Error: 400 invalid_request_error") {
		t.Fatal("400 is not transient")
	}
	if !transientFailure("API Error: 529 Overloaded") {
		t.Fatal("529 is transient")
	}
}
