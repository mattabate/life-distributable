package sched

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/store"
)

// nfy stands in for the phone (Push) and for the ask board (Ask): the two
// places a job's output can reach the owner.
type nfy struct{ msgs []string }

func (n *nfy) NeedsYou(s string) error { n.msgs = append(n.msgs, "needs:"+s); return nil }
func (n *nfy) ask(job, s string)       { n.msgs = append(n.msgs, "ask:"+job+": "+s) }

func setup(t *testing.T, table string) (*Scheduler, *actions.Queue, *nfy) {
	dir := t.TempDir()
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	p := filepath.Join(dir, "schedule.json")
	os.WriteFile(p, []byte(table), 0o644)
	n := &nfy{}
	q := actions.New(db, nil, n)
	s, err := New(db, p, q, "/bin/false", func(string) (string, bool) { return dir, true })
	if err != nil {
		t.Fatal(err)
	}
	s.Push = n
	s.Ask = n.ask
	return s, q, n
}

// An unattended session may not be handed a general-purpose interpreter, a
// write handle on the database, or an HTTP client that can reach any host
// (fewer tools, and more of them narrow CLIs). This guards the LIVE
// ops/schedule.json and the fallbacks in LoadTable, which is where the grant
// would quietly come back: `"default_tools": []` means "use the default",
// so the default has to be safe too.
func TestNoUnrestrictedShellInAnyToolGrant(t *testing.T) {
	banned := []string{"python3", "sqlite3", "curl", "bash", "sh", "zsh", "osascript", "perl", "ruby", "node", "ssh", "nc", "scp", "wget"}

	check := func(label string, tools []string) {
		if len(tools) == 0 {
			t.Fatalf("%s: empty grant — LoadTable would substitute the fallback, test it instead", label)
		}
		for _, tool := range tools {
			cmd := strings.TrimPrefix(tool, "Bash(")
			cmd = strings.TrimSuffix(cmd, ")")
			if !strings.HasPrefix(tool, "Bash(") {
				continue // Read/Edit/WebFetch… are tools, not shell commands
			}
			// `bash ops/merge.sh` runs one named repo script, not a shell
			// (a granted repo script): judge the script, not the interpreter.
			if rest, ok := strings.CutPrefix(cmd, "bash "); ok && strings.HasPrefix(rest, "ops/") && strings.HasSuffix(strings.TrimSuffix(rest, ":*"), ".sh") {
				cmd = rest
			}
			word := cmd
			if i := strings.IndexAny(word, " :"); i >= 0 {
				word = word[:i]
			}
			word = filepath.Base(word) // ops/db.sh -> db.sh, /Users/.../py.sh -> py.sh
			for _, b := range banned {
				if word == b {
					t.Errorf("%s grants %q — %s is an unrestricted shell or an exfiltration path; use lifectl api / ops/db.sh / ops/py.sh", label, tool, b)
				}
			}
		}
	}

	// The fallbacks, used whenever schedule.json omits or empties a key.
	var empty Table
	b, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "schedule.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	fallback, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	check("fallback prompt_tools", fallback.PromptTools)
	check("fallback default_tools", fallback.DefaultTools)

	// The live file the hub actually runs.
	live, err := LoadTable("../../../ops/schedule.json")
	if err != nil {
		t.Fatalf("ops/schedule.json unreadable: %v", err)
	}
	check("live prompt_tools", live.PromptTools)
	// A job's own narrower grant is held to the same rule.
	for _, j := range live.Jobs {
		if len(j.AllowedTools) > 0 {
			check("job "+j.Name, j.AllowedTools)
		}
	}
}

func TestWhenParsing(t *testing.T) {
	for _, ok := range []string{"daily@07:30", "weekly@Mon 09:00", "every@6h", "manual"} {
		if _, _, err := parseWhen(ok); err != nil {
			t.Error(ok, err)
		}
	}
	for _, bad := range []string{"daily@25:00", "weekly@Funday 09:00", "every@soon", "hourly"} {
		if _, _, err := parseWhen(bad); err == nil {
			t.Error("accepted", bad)
		}
	}
}

// fakeClock stands in for threads.Manager: the standing rows by target.
type fakeClock struct {
	rows map[string]string // target → repeat
	sets []string
}

func (c *fakeClock) SetStanding(author, target, repeat, text string, last time.Time) error {
	c.sets = append(c.sets, target+"="+repeat)
	if repeat == "" {
		delete(c.rows, target)
	} else {
		c.rows[target] = repeat
	}
	return nil
}
func (c *fakeClock) StandingRepeat(target string) (string, bool) {
	r, ok := c.rows[target]
	return r, ok
}
func (c *fakeClock) StandingTargets(prefix string) []string {
	var out []string
	for k := range c.rows {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// The scheduler has no clock of its own: SetClock/Reload reconcile the table
// into standing rows (one per enabled clock job), an unchanged cadence is
// left alone, and a job that leaves the table loses its row.
func TestReconcileTableIntoStandingRows(t *testing.T) {
	s, _, _ := setup(t, `{"jobs":[
		{"name":"sweep","project":"life","when":"daily@07:00","prompt":"p"},
		{"name":"hourly","project":"life","when":"every@1h","prompt":"p"},
		{"name":"off","project":"life","when":"daily@09:00","prompt":"p","enabled":false},
		{"name":"byhand","project":"life","when":"manual","prompt":"p"}]}`)
	c := &fakeClock{rows: map[string]string{"job:stale": "every@2h", "job:hourly": "every@1h"}}
	s.SetClock(c)
	if c.rows["job:sweep"] != "daily@07:00" || c.rows["job:hourly"] != "every@1h" {
		t.Fatalf("rows: %v", c.rows)
	}
	if _, ok := c.rows["job:stale"]; ok {
		t.Fatal("job not in the table kept its row")
	}
	if _, ok := c.rows["job:off"]; ok {
		t.Fatal("disabled job got a row")
	}
	if _, ok := c.rows["job:byhand"]; ok {
		t.Fatal("manual job got a row")
	}
	for _, set := range c.sets {
		if set == "job:hourly=every@1h" {
			t.Fatal("unchanged cadence was rewritten (its not_before would reset)")
		}
	}
	// Reload with the same table: nothing to do.
	c.sets = nil
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(c.sets) != 0 {
		t.Fatal(c.sets)
	}
}

// Fire is the clock's entry: held (gate shut) writes nothing so the next tick
// asks again; skipped (budget) records a skipped run carrying the wake's id;
// started runs the job with the wake's id on its run row.
func TestFireAnswersTheClock(t *testing.T) {
	s, _, _ := setup(t, `{"jobs":[{"name":"gated","project":"life","when":"every@1h","prompt":"p","gate":"asks"},{"name":"j","project":"life","when":"every@1h","prompt":"p"}]}`)
	ran := make(chan string, 1)
	s.RunClaude = func(_ context.Context, _, _ string, _ []string) (Output, error) {
		ran <- "ran"
		return ParseOutput(`{"summary":"ok","findings":[],"needs_you":[]}`, "")
	}
	s.Gate = func(gate string, last time.Time) bool { return false }
	if st, _ := s.Fire("gated", "p-1"); st != "held" {
		t.Fatal(st)
	}
	if held, _ := s.Held("gated"); !held {
		t.Fatal("Held disagrees with Fire")
	}
	if held, _ := s.Held("j"); held {
		t.Fatal("an ungated idle job is not held")
	}
	if st, _ := s.Fire("nope", "p-1"); st != "gone" {
		t.Fatal(st)
	}
	if rs, _ := s.Runs("", 10); len(rs) != 0 {
		t.Fatal("held wrote a run", rs)
	}
	s.Allow = func(kind, id string) (bool, string) { return false, "budget paused" }
	if st, why := s.Fire("j", "p-2"); st != "skipped" || why != "budget paused" {
		t.Fatal(st, why)
	}
	rs, _ := s.Runs("j", 10)
	if len(rs) != 1 || rs[0].PromptID != "p-2" || !strings.Contains(rs[0].Error, "budget paused") {
		t.Fatalf("%+v", rs)
	}
	s.Allow = nil
	if st, _ := s.Fire("j", "p-3"); st != "started" {
		t.Fatal(st)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not run")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rs, _ = s.Runs("j", 10)
		if len(rs) == 2 && rs[0].FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run not recorded: %+v", rs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rs[0].PromptID != "p-3" || rs[0].OK == nil || !*rs[0].OK {
		t.Fatalf("%+v", rs[0])
	}
}

func TestRunRoutesOutput(t *testing.T) {
	s, q, n := setup(t, `{"digest_at":"18:00","jobs":[{"name":"sweep","project":"life","when":"daily@07:00","prompt":"look around"}]}`)
	s.RunClaude = func(_ context.Context, dir, prompt string, tools []string) (Output, error) {
		if len(tools) == 0 {
			t.Fatal("no default tools")
		}
		if !strings.Contains(prompt, "look around") || !strings.Contains(prompt, "NEVER move money") {
			t.Fatal("prompt not wrapped")
		}
		return ParseOutput(`Here you go: {"summary":"all quiet","findings":[{"kind":"money","title":"pay bill","detail":"x"},{"kind":"other","title":"tidy","detail":"y"}],"needs_you":["renew passport"]}`, "")
	}
	id, err := s.Run("sweep", "test")
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	open, _ := q.List("open", 10)
	all, _ := q.List("", 10)
	if len(all) != 2 {
		t.Fatalf("want 2 actions, got %d", len(all))
	}
	// money is gated → still proposed; other auto-approved (exec none → done quickly)
	var gated int
	for _, a := range open {
		if a.State == "proposed" && a.Kind == "money" {
			gated++
			// The proposal points back at its run: that is what it opens on.
			if a.RunID != "1" || a.Source != "claude:job:sweep" || a.ThreadID != "" {
				t.Fatalf("backlink: %+v", a)
			}
		}
	}
	if gated != 1 {
		t.Fatal(open)
	}
	// needs_you became an ask (the Ask hook); the gated proposal pushed once;
	// the summary went nowhere but the run record (no digest).
	var asks, needs int
	for _, m := range n.msgs {
		if m == "ask:sweep: renew passport" {
			asks++
		}
		if strings.HasPrefix(m, "needs:") {
			needs++
		}
	}
	if asks != 1 || needs != 1 {
		t.Fatal(n.msgs)
	}
	runs, _ := s.Runs("", 10)
	if len(runs) != 1 || runs[0].OK == nil || !*runs[0].OK || runs[0].Summary != "all quiet" {
		t.Fatalf("%+v", runs)
	}
}

// A failed run is a run row with ok=0 and the error; it pings nobody — the
// runs page shows it, and a job that keeps failing is a thing a session
// notices, not a notification.
func TestRunFailureIsARecordNotAPing(t *testing.T) {
	s, _, n := setup(t, `{"jobs":[{"name":"j","project":"life","when":"manual","prompt":"p"}]}`)
	s.RunClaude = func(context.Context, string, string, []string) (Output, error) {
		return Output{}, context.DeadlineExceeded
	}
	if _, err := s.Run("j", "test"); err == nil {
		t.Fatal("expected error")
	}
	if len(n.msgs) != 0 {
		t.Fatal("failure must not notify", n.msgs)
	}
	runs, _ := s.Runs("j", 1)
	if len(runs) != 1 || runs[0].OK == nil || *runs[0].OK || !strings.Contains(runs[0].Error, "deadline") {
		t.Fatalf("%+v", runs)
	}
	if _, err := s.Run("nope", "test"); err == nil {
		t.Fatal("unknown job accepted")
	}
}
