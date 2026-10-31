package sched

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// An unattended job connects no MCP server, nor does the gate's claude exec.
func TestJobArgsConnectNoMCP(t *testing.T) {
	got := strings.Join(jobArgs("/l", []string{"Read", "Bash(lifectl:*)"}, []string{"--model", "m"}), " ")
	for _, want := range []string{"--strict-mcp-config", `--mcp-config {"mcpServers":{}}`, "--allowedTools Read,Bash(lifectl:*)", "--model m"} {
		if !strings.Contains(got, want) {
			t.Fatalf("job args lack %q: %s", want, got)
		}
	}
	got = strings.Join(actions.ClaudeExecArgs(nil), " ")
	if !strings.Contains(got, `--strict-mcp-config --mcp-config {"mcpServers":{}}`) {
		t.Fatalf("claude exec args lack the no-MCP flags: %s", got)
	}
}

// acceptEdits auto-accepts Edit/Write under the job's directory whatever
// --allowedTools says: a read-only grant refuses every file tool, and a
// scoped Write grant keeps its path but never reaches ops/secrets or .git.
func TestJobArgsDenyUngrantedFileTools(t *testing.T) {
	got := strings.Join(jobArgs("/l", []string{"Read", "Bash(lifectl:*)"}, nil), " ")
	if !strings.Contains(got, "--disallowedTools Edit,Write,MultiEdit,NotebookEdit") {
		t.Fatalf("read-only job may still edit: %s", got)
	}
	d := strings.Join(jobDenied("/l", []string{"Read", "Write(/data/tmp/pdf/**)"}), ",")
	for _, want := range []string{"Edit", "Write(//l/ops/secrets/**)", "Write(//l/.git/**)", "NotebookEdit"} {
		if !strings.Contains(","+d+",", ","+want+",") {
			t.Fatalf("denials lack %q: %s", want, d)
		}
	}
	if strings.Contains(","+d+",", ",Write,") {
		t.Fatalf("a scoped Write grant was refused outright: %s", d)
	}
}

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
			// A launcher whole runs anything from a one-liner, no file first:
			// `xcrun python3 -c …`, `find … -exec`. Its named subcommands pass.
			// make, go and npm run stay on purpose: they run only what is in the repo first.
			if cmd == "xcrun:*" || cmd == "find:*" {
				t.Errorf("%s grants %q — a launcher runs any command; grant named subcommands", label, tool)
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

// A wake that cannot start a run says so on the record: an unknown project
// is a failed run row carrying the wake's id (Fire answers skipped, so the
// wake is no "delivered"), and a job already running holds instead of
// reading started with no row behind it.
func TestFireThatCannotStartIsOnTheRecord(t *testing.T) {
	s, _, _ := setup(t, `{"jobs":[{"name":"typo","project":"lfie","when":"every@1h","prompt":"p"},{"name":"j","project":"life","when":"every@1h","prompt":"p"}]}`)
	s.projDir = func(p string) (string, bool) { return t.TempDir(), p == "life" }
	s.RunClaude = func(context.Context, string, string, []string) (Output, error) {
		t.Fatal("ran a job that could not start")
		return Output{}, nil
	}
	st, why := s.Fire("typo", "p-1")
	if st != "skipped" || !strings.Contains(why, `unknown project "lfie"`) {
		t.Fatal(st, why)
	}
	rs, _ := s.Runs("typo", 10)
	if len(rs) != 1 || rs[0].PromptID != "p-1" || rs[0].FinishedAt == nil || rs[0].OK == nil || *rs[0].OK || !strings.Contains(rs[0].Error, "unknown project") {
		t.Fatalf("%+v", rs)
	}
	if held, _ := s.Held("typo"); held {
		t.Fatal("a job that failed to start still holds its running flag")
	}
	s.running["j"] = true // a manual run got there first
	if st, _ := s.Fire("j", "p-2"); st != "held" {
		t.Fatal(st)
	}
	if rs, _ := s.Runs("j", 10); len(rs) != 0 {
		t.Fatalf("%+v", rs)
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

// A failed run exits 1 with its envelope; the reason sits after ~1 KB of
// usage and stats. The run's error carries that reason and its cost, not
// the first 500 raw bytes; non-JSON output is still quoted.
func TestDecodeResultReadsTheReasonOfAFailedExit(t *testing.T) {
	pad := strings.Repeat(`"x":0,`, 200)
	raw := `{"type":"result","subtype":"success",` + pad + `"is_error":true,"total_cost_usd":0.42,"api_error_status":null,"result":"API Error: Can't reach the API server (ENOTFOUND)"}`
	o, err := decodeResult([]byte(raw), fmt.Errorf("exit status 1"))
	if err == nil || !strings.Contains(err.Error(), "Can't reach the API server") || strings.Contains(err.Error(), `"x"`) || o.Cost != 0.42 {
		t.Fatalf("cost %v err %v", o.Cost, err)
	}
	if strings.Contains(err.Error(), "JSON") {
		t.Fatal("an API failure must not file the no-result-block card", err)
	}
	if _, err := decodeResult([]byte("Error: spawn failed"), fmt.Errorf("exit status 1")); err == nil || !strings.Contains(err.Error(), "spawn failed") {
		t.Fatal(err)
	}
	ok := `{"is_error":false,"total_cost_usd":0.1,"result":"{\"summary\":\"s\",\"findings\":[],\"needs_you\":[]}"}`
	if o, err := decodeResult([]byte(ok), nil); err != nil || o.Summary != "s" || o.Cost != 0.1 {
		t.Fatalf("%+v %v", o, err)
	}
}

// The CLI's permission_denials reach the next run of the job. A run whose
// last run had none hears nothing.
func TestDeniedCallsReachTheNextRun(t *testing.T) {
	s, _, _ := setup(t, `{"jobs":[{"name":"j","project":"life","when":"manual","prompt":"p"}]}`)
	var prompts []string
	raw := `{"type":"result","is_error":false,"total_cost_usd":0.1,"result":"{\"summary\":\"s\",\"findings\":[],\"needs_you\":[]}",` +
		`"permission_denials":[{"tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"lifectl asks --json | python3 -c \"\nimport json\""}},` +
		`{"tool_name":"Bash","tool_use_id":"t2","tool_input":{"command":"lifectl asks --json | python3 -c \"\nimport json\""}},` +
		`{"tool_name":"WebFetch","tool_use_id":"t3","tool_input":{"url":"https://example.com"}}]}`
	outs := []string{raw, `{"is_error":false,"total_cost_usd":0.1,"result":"{\"summary\":\"s\",\"findings\":[],\"needs_you\":[]}","permission_denials":[]}`}
	s.RunClaude = func(_ context.Context, _, prompt string, _ []string) (Output, error) {
		prompts = append(prompts, prompt)
		r := outs[0]
		outs = outs[1:]
		return decodeResult([]byte(r), nil)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Run("j", "test"); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(prompts[0], "refused") {
		t.Fatal("first run has no previous run", prompts[0])
	}
	want := "\n- Bash: lifectl asks --json | python3 -c \" import json\"\n- WebFetch: {\"url\":\"https://example.com\"}"
	if !strings.HasSuffix(prompts[1], want) || strings.Count(prompts[1], "python3") != 1 {
		t.Fatalf("%q", prompts[1])
	}
	outs = []string{`{"is_error":false,"total_cost_usd":0.1,"result":"{\"summary\":\"s\",\"findings\":[],\"needs_you\":[]}"}`}
	if _, err := s.Run("j", "test"); err != nil || strings.Contains(prompts[2], "refused") {
		t.Fatalf("a run after a clean one is told nothing: %v %q", err, prompts[2])
	}
	if d := deniedOf("not json"); d != nil {
		t.Fatal(d)
	}
}

// Prose that quotes JSON before the envelope must not hide the envelope;
// nested objects inside a finding are never taken for it; the prose is
// GetRun's text.
func TestParseOutputSkipsQuotedJSONInProse(t *testing.T) {
	text := "Nothing to parse: `tool next` returned `{\"done\":true}` immediately.\n\n" +
		`{"summary": "s", "findings": [{"kind":"other","title":"t","detail":"d","exec_type":"shell","exec_payload":{"summary":"inner"}}], "needs_you": ["n"]}` +
		"\nTrailing `}` note."
	o, err := ParseOutput(text, "")
	if err != nil || o.Summary != "s" || len(o.Findings) != 1 || len(o.NeedsYou) != 1 {
		t.Fatalf("%+v %v", o, err)
	}
	if got := proseBefore(text); !strings.HasSuffix(got, "immediately.") {
		t.Fatalf("prose: %q", got)
	}
	if _, err := ParseOutput("all quiet, no block", ""); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatal("a missing envelope must still fail with a JSON error", err)
	}
}

// A tool's leftover child holds claude's output pipe after claude wrote its
// result and exited: the run still closes, keeps the result, and the child
// is killed. A timeout kills the whole group, not just claude.
func TestRunGroupDoesNotWaitOnAnOrphanedPipe(t *testing.T) {
	dir := t.TempDir()
	pidf := filepath.Join(dir, "pid")
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\nsleep 30 &\necho $! > "+pidf+"\necho '{\"result\":\"done\"}'\n"), 0o755)
	start := time.Now()
	out, err := runGroup(exec.CommandContext(context.Background(), bin), 200*time.Millisecond)
	if err != nil || !strings.Contains(string(out), `"result":"done"`) {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("waited %v on the orphan's pipe", d)
	}
	b, _ := os.ReadFile(pidf)
	var pid int
	fmt.Sscan(string(b), &pid)
	time.Sleep(100 * time.Millisecond)
	if pid == 0 || syscall.Kill(pid, 0) == nil {
		t.Fatalf("orphan %d still alive", pid)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	os.WriteFile(bin, []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	start = time.Now()
	if _, err := runGroup(exec.CommandContext(ctx, bin), 200*time.Millisecond); err == nil {
		t.Fatal("a timed-out run reported no error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timeout took %v", d)
	}
}
