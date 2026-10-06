package actions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"life/hub/internal/store"
)

type fakeExec struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeExec) Exec(_ context.Context, typ string, p json.RawMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, typ)
	return "ran " + typ, nil
}

type fakeNfy struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeNfy) NeedsYou(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, "needs:"+s)
	return nil
}

func setup(t *testing.T) (*Queue, *fakeExec, *fakeNfy) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ex, nf := &fakeExec{}, &fakeNfy{}
	return New(db, ex, nf), ex, nf
}

func waitState(t *testing.T, q *Queue, id, want string) Action {
	for i := 0; i < 100; i++ {
		a, _ := q.Get(id)
		if a.State == want {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	a, _ := q.Get(id)
	t.Fatalf("state %s, want %s", a.State, want)
	return a
}

func TestUngatedRunsImmediately(t *testing.T) {
	q, ex, nf := setup(t)
	a, err := q.Propose(Proposal{Kind: "other", Title: "tidy", ExecType: "shell", ExecPayload: json.RawMessage(`{"cmd":"true"}`)})
	if err != nil || a.Gated || a.State != "approved" {
		t.Fatal(a, err)
	}
	a = waitState(t, q, a.ID, "done")
	if a.Result != "ran shell" || len(ex.calls) != 1 || len(nf.msgs) != 0 {
		t.Fatal(a, ex.calls, nf.msgs)
	}
}

// A proposal from before the merge (step 9) reads back the same from items:
// its card and decision moved, its run record stayed, and its trail's event
// words became state moves.
func TestPreMergeActionMovesToItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate("actions", Schema[:len(Schema)-1]); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO actions (id,created_at,updated_at,project,kind,title,gated,exec_type,exec_payload,state,decided_at,decided_via,result,note,source,said)
		VALUES ('20260901-120000-aa','2026-09-01T12:00:00Z','2026-09-01T12:05:00Z','life','commit','Merge it',1,'none','{}','done','2026-09-01T12:04:00Z','app','ok','go','lifectl','Please approve the merge')`)
	for _, e := range [][2]string{{"proposed", "lifectl"}, {"approved", "app"}, {"ran", "hub"}} {
		db.Exec(`INSERT INTO action_events (action_id,ts,event,actor) VALUES ('20260901-120000-aa','2026-09-01T12:00:00Z',?,?)`, e[0], e[1])
	}
	q := New(db, &fakeExec{}, &fakeNfy{})
	a, err := q.Get("20260901-120000-aa")
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "done" || a.DecidedVia != "app" || a.Note != "go" || a.Result != "ok" || !a.Gated || a.Said != "Please approve the merge" || a.Window != "now" {
		t.Fatalf("moved action: %+v", a)
	}
	rows, _ := db.Query(`SELECT actor||':'||from_state||'>'||to_state FROM item_events WHERE item_id=? ORDER BY id`, a.ID)
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		trail = append(trail, s)
	}
	if got := strings.Join(trail, " "); got != "lifectl:>proposed app:proposed>approved hub:approved>done" {
		t.Fatalf("trail %s", got)
	}
	if evs, _ := q.Events(a.ID); len(evs) != 3 {
		t.Fatalf("the run's own trail stays: %+v", evs)
	}
}

func TestRestartFailsOrphanedRuns(t *testing.T) {
	q, ex, nf := setup(t)
	a, err := q.Propose(Proposal{Kind: "other", Title: "tidy", ExecType: "shell", ExecPayload: json.RawMessage(`{"cmd":"true"}`)})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, q, a.ID, "done")
	// The hub died while it ran: the row is left 'running'.
	if _, err := q.db.Exec(`UPDATE items SET state='running' WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	q = New(q.db, ex, nf)
	got, _ := q.Get(a.ID)
	if got.State != "failed" || got.Error != "hub restarted mid-run" {
		t.Fatalf("after restart: %+v", got)
	}
	evs, _ := q.Events(a.ID)
	if len(evs) == 0 || evs[len(evs)-1].Event != "failed" {
		t.Fatalf("events: %+v", evs)
	}
	// A second run of the same id finds nothing to claim.
	q.run(a.ID)
	if got, _ := q.Get(a.ID); got.State != "failed" {
		t.Fatalf("rerun moved it: %s", got.State)
	}
}

func TestGatedKindsAlwaysGated(t *testing.T) {
	q, ex, nf := setup(t)
	f := false
	a, err := q.Propose(Proposal{Kind: "money", Title: "move $500", Gated: &f, ExecType: "shell", ExecPayload: json.RawMessage(`{"cmd":"true"}`)})
	if err != nil || !a.Gated || a.State != "proposed" {
		t.Fatal(a, err)
	}
	if len(nf.msgs) != 1 || nf.msgs[0][:6] != "needs:" {
		t.Fatal(nf.msgs)
	}
	time.Sleep(50 * time.Millisecond)
	if len(ex.calls) != 0 {
		t.Fatal("executed without approval")
	}
	if _, err := q.Decide(a.ID, false, "app", ""); err != nil {
		t.Fatal(err)
	}
	a, _ = q.Get(a.ID)
	if a.State != "denied" || a.DecidedVia != "app" {
		t.Fatal(a)
	}
	if _, err := q.Decide(a.ID, true, "app", ""); err == nil {
		t.Fatal("re-deciding must fail")
	}
	if len(ex.calls) != 0 {
		t.Fatal("denied action executed")
	}
}

// A gated proposal is the ONE thing that notifies (NEEDS YOU); approval and
// completion are records on the action, never a second ping.
func TestApproveRunsAndNotifies(t *testing.T) {
	q, ex, nf := setup(t)
	a, _ := q.Propose(Proposal{Kind: "contact", Title: "text Sam", ExecType: "shell", ExecPayload: json.RawMessage(`{"cmd":"true"}`)})
	if _, err := q.Decide(a.ID, true, "app", ""); err != nil {
		t.Fatal(err)
	}
	a = waitState(t, q, a.ID, "done")
	if len(ex.calls) != 1 || ex.calls[0] != "shell" {
		t.Fatal(ex.calls)
	}
	time.Sleep(30 * time.Millisecond)
	nf.mu.Lock()
	defer nf.mu.Unlock()
	if len(nf.msgs) != 1 || !strings.HasPrefix(nf.msgs[0], "needs:Please approve: text Sam") {
		t.Fatal(nf.msgs)
	}
	open, _ := q.List("open", 10)
	if len(open) != 0 {
		t.Fatal(open)
	}
}

// cardNfy is a notifier with the card lane: the push SPEAKS a sentence
// instead of reading the "Needs you" label (notify/voice.go).
type cardNfy struct {
	fakeNfy
	said []string
}

func (c *cardNfy) Card(kind, line, say, thread, card string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.said = append(c.said, say)
	return nil
}

// What an approval card SPOKE is kept on the card. The session's own
// sentence when it wrote one, the hub's when it did not — and the row keeps
// it, so the card the owner opens holds the words they heard.
func TestApprovalKeepsWhatItSpoke(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	nf := &cardNfy{}
	q := New(db, &fakeExec{}, nf)

	a, err := q.Propose(Proposal{Kind: "money", Title: "Move $500 to the brokerage"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Hey, Move $500 to the brokerage. It's waiting for your approval."
	if a.Said != want {
		t.Errorf("said %q, want %q", a.Said, want)
	}
	if got, _ := q.Get(a.ID); got.Said != want {
		t.Errorf("said did not survive the round trip: %q", got.Said)
	}
	nf.mu.Lock()
	spoken := append([]string(nil), nf.said...)
	nf.mu.Unlock()
	if len(spoken) != 1 || spoken[0] != want {
		t.Fatalf("spoken %v, want the one sentence", spoken)
	}

	// The proposer's own sentence wins, as `--say` does on an ask.
	say := "Hey Alex, the Upwork bid is ready to send. Approve it and it goes."
	b, err := q.Propose(Proposal{Kind: "contact", Title: "Send the Upwork bid", Say: say})
	if err != nil || b.Said != say {
		t.Fatalf("said %q (%v), want the proposer's line", b.Said, err)
	}

	// A notifier without the card lane says the label form and stores
	// nothing — `said` means spoken, never "would have been spoken".
	plain, _, pnf := setup(t)
	c, _ := plain.Propose(Proposal{Kind: "delete", Title: "Drop the preview rows"})
	if c.Said != "" {
		t.Errorf("said %q on the label lane, want empty", c.Said)
	}
	pnf.mu.Lock()
	defer pnf.mu.Unlock()
	if len(pnf.msgs) != 1 {
		t.Fatal(pnf.msgs)
	}
}

// A relay — one session's words for another — is gated whatever the proposer
// says, its card is written by the hub from the payload (the owner approves
// exactly what is sent, to exactly whom), and only their approval delivers it.
func TestRelayIsAlwaysAnApprovedCard(t *testing.T) {
	q, ex, _ := setup(t)
	relay := Proposal{ThreadID: "th-a", Title: "harmless tidy", Detail: "it owns that repo", ExecType: "relay",
		ExecPayload: json.RawMessage(`{"to":"th-b","text":"Rename it Crossword Constructor."}`)}
	if _, err := q.Propose(relay); err == nil {
		t.Fatal("relay accepted on a hub with no relay wired")
	}
	var mu sync.Mutex
	var sent []string
	q.RelayTarget = func(id string) (string, error) {
		if id != "th-b" {
			return "", errors.New("no session " + id)
		}
		return "Crossword repo", nil
	}
	q.Relay = func(from, to, text, actionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, from+">"+to+":"+text+":"+actionID)
		return "delivered", nil
	}
	for name, bad := range map[string]Proposal{
		"no sender":    {ExecType: "relay", ExecPayload: relay.ExecPayload},
		"to itself":    {ThreadID: "th-b", ExecType: "relay", ExecPayload: relay.ExecPayload},
		"no words":     {ThreadID: "th-a", ExecType: "relay", ExecPayload: json.RawMessage(`{"to":"th-b"}`)},
		"no such peer": {ThreadID: "th-a", ExecType: "relay", ExecPayload: json.RawMessage(`{"to":"th-x","text":"hi"}`)},
		"a transcript": {ThreadID: "th-a", ExecType: "relay", ExecPayload: json.RawMessage(`{"to":"th-b","text":"` + strings.Repeat("x", relayMax+1) + `"}`)},
	} {
		if _, err := q.Propose(bad); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	f := false
	relay.Gated = &f
	a, err := q.Propose(relay)
	if err != nil || !a.Gated || a.State != "proposed" {
		t.Fatal(a, err)
	}
	if a.Title != "Send to the session “Crossword repo”" || !strings.HasPrefix(a.Detail, "Rename it Crossword Constructor.") || !strings.Contains(a.Detail, "Why: it owns that repo") || strings.Contains(a.Title+a.Detail, "harmless tidy") {
		t.Fatalf("the card is the hub's, from the payload: %q / %q", a.Title, a.Detail)
	}
	time.Sleep(30 * time.Millisecond)
	if len(sent) != 0 {
		t.Fatal("delivered before the owner approved")
	}
	if _, err := q.Decide(a.ID, true, "app", ""); err != nil {
		t.Fatal(err)
	}
	a = waitState(t, q, a.ID, "done")
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "th-a>th-b:Rename it Crossword Constructor.:"+a.ID || a.Result != "delivered" || len(ex.calls) != 0 {
		t.Fatal(sent, a.Result, ex.calls)
	}
}

// An action carries who
// proposed it and the run/reply it came from, and every state change is an
// audit line — the trail asks already had.
func TestEventsAndBacklink(t *testing.T) {
	q, _, _ := setup(t)
	a, err := q.Propose(Proposal{Kind: "money", Title: "move $500", ThreadID: "thread-1", RunID: "run-9", MessageID: 42, Source: "claude:thread:thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(a.ID)
	if got.RunID != "run-9" || got.MessageID != 42 || got.Source != "claude:thread:thread-1" {
		t.Fatalf("backlink lost: %+v", got)
	}
	if _, err := q.Decide(a.ID, true, "app", "go ahead"); err != nil {
		t.Fatal(err)
	}
	waitState(t, q, a.ID, "done")
	ev, err := q.Events(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 3 || ev[0].Event != "proposed" || ev[0].Actor != "claude:thread:thread-1" ||
		ev[1].Event != "approved" || ev[1].Actor != "app" || ev[1].Note != "go ahead" ||
		ev[2].Event != "ran" || ev[2].Actor != "hub" {
		t.Fatalf("events: %+v", ev)
	}
	// A proposal with a thread but no source is filed under that thread.
	b, _ := q.Propose(Proposal{Kind: "delete", Title: "x", ThreadID: "thread-2"})
	if got, _ := q.Get(b.ID); got.Source != "claude:thread:thread-2" {
		t.Fatalf("source not derived: %+v", got)
	}
	q.Decide(b.ID, false, "cli", "")
	if ev, _ := q.Events(b.ID); len(ev) != 2 || ev[1].Event != "denied" || ev[1].Actor != "cli" {
		t.Fatalf("denied trail: %+v", ev)
	}
	// Ungated: proposed + auto-approved + ran, with the run's result.
	c, _ := q.Propose(Proposal{Kind: "other", Title: "tidy", ExecType: "shell", ExecPayload: json.RawMessage(`{"cmd":"true"}`), Source: "claude:job:daily-sweep"})
	waitState(t, q, c.ID, "done")
	if ev, _ := q.Events(c.ID); len(ev) != 3 || ev[1].Actor != "auto" || ev[2].Note != "ran shell" {
		t.Fatalf("ungated trail: %+v", ev)
	}
	if ev, err := q.Events("nope"); err != nil || len(ev) != 0 {
		t.Fatalf("unknown id: %v %v", ev, err)
	}
}

// Dated places an action on its decided day, else its created day, with the
// decider as actor — the agenda's row for it.
func TestDated(t *testing.T) {
	q, _, _ := setup(t)
	a, _ := q.Propose(Proposal{Kind: "money", Title: "move $500", Source: "claude:thread:t-1"})
	b, _ := q.Propose(Proposal{Kind: "delete", Title: "drop dupe", Source: "claude:thread:t-1"})
	q.Decide(b.ID, true, "app", "")
	today := store.Day(time.Now())
	rows, err := q.Dated(today, today)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %+v", err, rows)
	}
	for _, r := range rows {
		switch r.ID {
		case a.ID:
			if r.Kind != "action" || r.State != "proposed" || r.Actor != "claude:thread:t-1" || r.Day != today || r.Did || r.Verb != "" {
				t.Fatalf("proposed row: %+v", r)
			}
		case b.ID:
			// Decided from the app, it is the record of the OWNER'S approval — even
			// once it has run.
			if r.State != "approved" && r.State != "done" || r.Actor != "app" || !r.Did || r.Verb != "Approved" {
				t.Fatalf("decided row: %+v", r)
			}
		default:
			t.Fatalf("stray row: %+v", r)
		}
	}
	if rows, _ := q.Dated("2020-01-01", "2020-01-02"); len(rows) != 0 {
		t.Fatalf("window not honoured: %+v", rows)
	}
	if _, err := q.Dated("nope", today); err == nil {
		t.Fatal("bad day accepted")
	}
	// The verb is the decider's deed: the gate letting one through on its own
	// is the hub's "Ran"; a denial or a failure says so whoever decided.
	now := time.Now()
	for _, x := range []struct {
		via, state, verb string
	}{
		{"auto", "done", "Ran"}, {"auto", "failed", "Failed"}, {"auto", "running", "Running"},
		{"app", "denied", "Denied"}, {"cli", "failed", "Failed"}, {"web", "done", "Approved"},
	} {
		r := Row(Action{ID: "x", State: x.state, DecidedAt: &now, DecidedVia: x.via})
		if !r.Did || r.Verb != x.verb {
			t.Fatalf("%s/%s: %+v", x.via, x.state, r)
		}
	}
}

func TestValidation(t *testing.T) {
	q, _, _ := setup(t)
	if _, err := q.Propose(Proposal{}); err == nil {
		t.Fatal("empty title accepted")
	}
	if _, err := q.Propose(Proposal{Title: "x", ExecType: "rm -rf"}); err == nil {
		t.Fatal("bad exec accepted")
	}
}

func TestDecisionRelayedToThread(t *testing.T) {
	q, _, _ := setup(t)
	var got []string
	q.OnDecided = func(a Action, approved bool, note string) {
		got = append(got, a.ThreadID, a.Title, note, a.Note)
		if !approved {
			t.Fatal("expected approved")
		}
	}
	a, err := q.Propose(Proposal{Kind: "delete", Title: "drop dupe", ThreadID: "thread-1"})
	if err != nil || a.ThreadID != "thread-1" {
		t.Fatal(a, err)
	}
	if _, err := q.Decide(a.ID, true, "app", " only gd-2026-03 "); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0] != "thread-1" || got[1] != "drop dupe" || got[2] != "only gd-2026-03" || got[3] != "only gd-2026-03" {
		t.Fatal(got)
	}
	// no thread → still relayed (main starts a session for it)
	got = nil
	q.OnDecided = func(a Action, approved bool, note string) { got = append(got, a.ThreadID) }
	b, _ := q.Propose(Proposal{Kind: "delete", Title: "x"})
	q.Decide(b.ID, false, "cli", "")
	if len(got) != 1 || got[0] != "" {
		t.Fatal(got)
	}
	// … and when the callback starts one, the decision hands it back, so the
	// app can land in it as it does for a session's own proposal.
	q.OnDecided = func(a Action, approved bool, note string) { q.SetThread(a.ID, "thread-new") }
	c, _ := q.Propose(Proposal{Kind: "delete", Title: "y", Source: "claude:job:sweep", RunID: "7"})
	d, err := q.Decide(c.ID, true, "app", "")
	if err != nil || d.ThreadID != "thread-new" || d.RunID != "7" {
		t.Fatalf("%+v %v", d, err)
	}
}
