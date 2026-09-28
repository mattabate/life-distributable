package threads

import (
	"os"
	"strings"
	"testing"
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
