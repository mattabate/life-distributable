package threads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inFile(t *testing.T, m *Manager, threadID string) string {
	var in string
	m.db.QueryRow(`SELECT in_file FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, threadID).Scan(&in)
	if in == "" {
		t.Fatal("no live run")
	}
	return in
}

func appendOut(t *testing.T, out, s string) {
	f, err := os.OpenFile(out, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

// The owner can message a working session: the message is written to claude's
// stdin at once (steered into the running turn); between turns the same
// process serves the next turn; tool calls streamed by claude land in events
// with plain-English summaries while the run is still going.
func TestSteerAndTurns(t *testing.T) {
	t.Setenv("HOME", "/Users/owner") // shortPath makes ~/life paths repo-relative
	m, _, cmds := setup(t)
	m.FeedBin = "/opt/bin/lifectl"
	th, _ := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	in := inFile(t, m, th.ID)
	if c := (*cmds)[0]; !strings.Contains(c, "'/opt/bin/lifectl' feed '"+in+"' | LIFE_THREAD_ID=") || !strings.Contains(c, "'--input-format' 'stream-json'") {
		t.Fatal(c)
	}
	// the opening prompt is the first stdin line
	if b, _ := os.ReadFile(in); !strings.HasPrefix(string(b), `{"message":{"content":"`) || !strings.Contains(string(b), "Do a long thing.") || strings.Count(string(b), "\n") != 1 {
		t.Fatal(string(b))
	}
	// claude streams: init, a thinking block, a tool call (with description), its result
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Let me look at the goals first."},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"lifectl goals","description":"List Alex's goals"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"make-more-money  active\n"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu2","name":"Read","input":{"file_path":"/Users/owner/life/DESIGN.md"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Work`)
	m.Poll()
	evs, _ := m.Events(th.ID, 0, 0, 100)
	if len(evs) != 4 || evs[0].Kind != "thinking" || evs[1].Title != "Bash · lifectl goals" || evs[1].Summary != "List Alex's goals" || evs[2].Kind != "tool_result" || evs[3].Summary != "Read DESIGN.md" {
		t.Fatalf("%+v", evs)
	}
	if th, _ = m.Get(th.ID); th.Status != "running" || th.ClaudeSessionID != "sess-1" {
		t.Fatalf("%+v", th)
	}
	// The owner sends two messages while it works: handed to stdin immediately,
	// marked steered, same turn, no new process.
	if err := m.Send(th.ID, "also check Robinhood"); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(th.ID, "and the bank"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if len(msgs) != 3 || !msgs[1].Steered || !msgs[2].Steered || msgs[1].Queued || msgs[1].RunID != msgs[0].RunID || len(*cmds) != 1 {
		t.Fatalf("%+v %v", msgs, *cmds)
	}
	b, _ := os.ReadFile(in)
	if strings.Count(string(b), "\n") != 3 || !strings.Contains(string(b), "while you are working on the current turn") || !strings.Contains(string(b), "and the bank") {
		t.Fatal(string(b))
	}
	// claude saw them at a tool boundary, then finished the turn.
	appendOut(t, out, `ing on it."}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"..."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Checked all three.","total_cost_usd":0.05,"session_id":"sess-1"}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ = m.Messages(th.ID, 50)
	// There is no white cell: the reply's words are a read card
	// linked to an empty `read` row, and a read card on the board is a thread
	// the owner owes a look — needs_you, like any other card.
	if th.Status != "needs_you" || th.CostUSD != 0.05 || len(msgs) != 4 || msgs[3].Text != "" || msgs[3].Kind != "read" || msgs[3].RunID != msgs[0].RunID {
		t.Fatalf("%+v %+v", th, msgs)
	}
	if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 1 || asks[0].Title != "Checked all three" || asks[0].Kind != "read" || asks[0].MessageID != msgs[3].ID {
		t.Fatalf("reply text is not the read card: %+v", asks)
	}
	if evs, _ = m.Events(th.ID, 0, 0, 100); len(evs) != 6 || evs[4].Kind != "text" { // interim text kept; final reply not duplicated
		t.Fatalf("%+v", evs)
	}
	// Process is alive and idle: the next message is the next turn on it
	// (no --resume launch), with its own turn id.
	if err := m.Send(th.ID, "now the summary"); err != nil {
		t.Fatal(err)
	}
	th, _ = m.Get(th.ID)
	msgs, _ = m.Messages(th.ID, 50)
	if th.Status != "running" || len(*cmds) != 1 || msgs[4].Steered || msgs[4].RunID != msgs[0].RunID+"-t2" {
		t.Fatalf("%+v %+v %v", th, msgs[4], *cmds)
	}
	appendOut(t, out, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu3","name":"Grep","input":{"pattern":"steer","path":"/Users/owner/life/hub"}}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Summary: fine.","total_cost_usd":0.08,"session_id":"sess-1"}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ = m.Messages(th.ID, 50)
	evs, _ = m.Events(th.ID, 0, 0, 100)
	if th.Status != "needs_you" || th.CostUSD < 0.079 || th.CostUSD > 0.081 || msgs[5].RunID != msgs[0].RunID+"-t2" || evs[6].RunID != msgs[0].RunID+"-t2" || evs[6].Summary != `Search for "steer" in hub` {
		t.Fatalf("%+v %+v %+v", th, msgs[5], evs[6])
	}
	// Idle long enough → EOF sentinel, then the process exits quietly.
	m.db.Exec(`UPDATE thread_runs SET last_output=? WHERE thread_id=?`, ts(time.Now().Add(-idleTTL-time.Minute)), th.ID)
	m.Poll()
	if b, _ = os.ReadFile(in); !strings.HasSuffix(string(b), eofSentinel+"\n") {
		t.Fatal(string(b))
	}
	// A message arriving while it winds down starts a fresh process (--resume).
	if err := m.Send(th.ID, "one more"); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 2 || !strings.Contains((*cmds)[1], "'--resume' 'sess-1'") {
		t.Fatal(*cmds)
	}
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	var finished, live int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NOT NULL`, th.ID).Scan(&finished)
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&live)
	msgs, _ = m.Messages(th.ID, 50)
	if finished != 1 || live != 1 || len(msgs) != 7 || msgs[6].Kind != "message" { // no error for a clean exit
		t.Fatalf("%d %d %+v", finished, live, msgs)
	}
	if th, _ = m.Get(th.ID); th.Status != "running" {
		t.Fatal(th.Status)
	}
	// prompt file of the new process carries the owner's message but no preamble
	pb, _ := os.ReadFile(promptFile(t, m, th.ID))
	if !strings.Contains(string(pb), "one more") || strings.Contains(string(pb), "How to work") {
		t.Fatal(string(pb))
	}
}

// A message handed over after the turn's last tool boundary was not seen by
// that turn: claude starts another turn on it, and the hub waits for that
// result (status stays running) instead of settling.
// A resumed thread grows forever unless the CLI is told to compact it: the
// window rides on the run's own env so the owner's interactive sessions
// are untouched. 0 leaves the CLI's default alone.
func TestAutoCompactWindowOnRunEnv(t *testing.T) {
	m, _, cmds := setup(t)
	if _, err := m.Create("", "life", "", "No window set.", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if c := (*cmds)[0]; strings.Contains(c, "CLAUDE_CODE_AUTO_COMPACT_WINDOW") {
		t.Fatalf("unset window should add nothing: %s", c)
	}
	m.AutoCompactWindow = 200000
	if _, err := m.Create("", "life", "", "Window set.", "", "", nil); err != nil {
		t.Fatal(err)
	}
	c := (*cmds)[len(*cmds)-1]
	if !strings.Contains(c, "LIFE_RUN_ID='") || !strings.Contains(c, " CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000 ") {
		t.Fatalf("window missing from run env: %s", c)
	}
	// it must sit in the env prefix, ahead of the binary — not among its flags
	if strings.Index(c, "CLAUDE_CODE_AUTO_COMPACT_WINDOW") > strings.Index(c, "'-p'") {
		t.Fatalf("window is not in the env prefix: %s", c)
	}
}

func TestLateMessageStartsNextTurn(t *testing.T) {
	m, _, cmds := setup(t)
	th, _ := m.Create("", "life", "", "Do a thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"a b"}]}}
`)
	m.Poll()
	time.Sleep(5 * time.Millisecond)
	if err := m.Send(th.ID, "late"); err != nil {
		t.Fatal(err)
	}
	appendOut(t, out, `{"type":"result","subtype":"success","is_error":false,"result":"[end]","total_cost_usd":0.02,"session_id":"s"}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 50)
	if th.Status != "running" || len(msgs) != 3 || msgs[1].RunID != msgs[0].RunID+"-t2" || !msgs[1].Steered || msgs[2].RunID != msgs[0].RunID {
		t.Fatalf("%+v %+v", th, msgs)
	}
	// Nothing more comes for a while after the result: the message was in
	// fact folded into that turn → settle.
	m.db.Exec(`UPDATE thread_runs SET last_output=? WHERE thread_id=?`, ts(time.Now().Add(-stuckTTL-time.Minute)), th.ID)
	m.Poll()
	if th, _ = m.Get(th.ID); th.Status != "done" || len(*cmds) != 1 {
		t.Fatalf("%+v", th)
	}
	// Bash without a description gets a Haiku summary, filled in async.
	evs, _ := m.Events(th.ID, 0, 0, 10)
	if evs[0].Summary != "" {
		t.Fatal(evs[0].Summary)
	}
	m.Describe = func(dir, prompt string) (string, error) { return "List the files here.\n", nil }
	m.describeAsync(evs[0].ID, th.ID, "ls")
	if evs, _ = m.EventsByID(th.ID, []int64{evs[0].ID}); evs[0].Summary != "List the files here" {
		t.Fatalf("%+v", evs)
	}
}

// A late message claude does NOT fold in but opens the next turn on loses its
// steered stamp at that turn's init: it is the turn's starter, and the phone
// would draw "steered in mid-turn" under a message with its own step block.
// A message steered into the turn already underway
// keeps the stamp.
func TestLateMessageOpeningNextTurnIsNotSteered(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do a thing.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"a b"}]}}
`)
	m.Poll()
	time.Sleep(5 * time.Millisecond)
	if err := m.Send(th.ID, "late"); err != nil {
		t.Fatal(err)
	}
	appendOut(t, out, `{"type":"result","subtype":"success","is_error":false,"result":"[end]","total_cost_usd":0.02,"session_id":"s"}
`)
	m.Poll()
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != 3 || !msgs[1].Steered || msgs[1].RunID != msgs[0].RunID+"-t2" {
		t.Fatalf("before init: %+v", msgs)
	}
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu2","name":"Bash","input":{"command":"pwd"}}]}}
`)
	m.Poll()
	time.Sleep(5 * time.Millisecond)
	if err := m.Send(th.ID, "and this too"); err != nil { // steered into turn 2, mid-way
		t.Fatal(err)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if len(msgs) != 4 || msgs[1].Steered || msgs[1].RunID != msgs[0].RunID+"-t2" || !msgs[3].Steered || msgs[3].RunID != msgs[0].RunID+"-t2" {
		t.Fatalf("after init: %+v", msgs)
	}
}

// A turn that ends while a background task is pending is a progress line,
// not the reply. The CLI re-enters a turn when the task notifies; every result until
// the last one folds into the run block, and the reply carries the whole
// span's cost and tokens.
func TestBackgroundTaskTurnsFoldIntoOneReply(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Build it and walk it.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	// A background Bash, then the completion ends on prose → result #1 with
	// the task still pending.
	appendOut(t, out, `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"id":"m1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"make app-ui","description":"Walk the phone chat","run_in_background":true}}]}}
{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash","description":"Walk the phone chat"}]}
{"type":"system","subtype":"task_started","task_id":"b1"}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"started in background"}]}}
{"type":"assistant","message":{"id":"m2","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"Phone walk is building in the Simulator now."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Phone walk is building in the Simulator now.","total_cost_usd":0.50,"session_id":"sess-1","usage":{"input_tokens":20,"output_tokens":10}}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 50)
	if th.Status != "running" || len(msgs) != 1 {
		t.Fatalf("folded result became a reply: %+v %+v", th, msgs)
	}
	var held string
	m.db.QueryRow(`SELECT held FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&held)
	if !strings.Contains(held, "Simulator now") {
		t.Fatal(held)
	}
	// The task notifies, the CLI re-enters, another wait, another prose
	// ending → result #2, still pending.
	appendOut(t, out, `{"type":"system","subtype":"task_updated","task_id":"b1"}
{"type":"system","subtype":"task_notification","task_id":"b1"}
{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"id":"m3","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"tool_use","id":"tu2","name":"Bash","input":{"command":"make app-ui","description":"Re-walk","run_in_background":true}}]}}
{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b2","task_type":"local_bash","description":"Re-walk"}]}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"started in background"}]}}
{"type":"assistant","message":{"id":"m4","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"Re-walking after the edit."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Re-walking after the edit.","total_cost_usd":0.80,"session_id":"sess-1","usage":{"input_tokens":20,"output_tokens":10}}
{"type":"system","subtype":"background_tasks_changed","tasks":[]}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	if msgs, _ = m.Messages(th.ID, 50); th.Status != "running" || len(msgs) != 1 {
		t.Fatalf("second folded result became a reply: %+v %+v", th, msgs)
	}
	// The last task notifies, the CLI re-enters with nothing pending, and
	// the turn ends for real: ONE reply, the whole span's cost and tokens.
	appendOut(t, out, `{"type":"system","subtype":"task_notification","task_id":"b2"}
{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"id":"m5","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"Did: walked both surfaces."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Did: walked both surfaces.","total_cost_usd":1.00,"session_id":"sess-1","usage":{"input_tokens":20,"output_tokens":10}}
`)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ = m.Messages(th.ID, 50)
	if th.Status != "needs_you" || len(msgs) != 2 || msgs[1].Text != "" || msgs[1].Kind != "read" || msgs[1].CostUSD < 0.999 || msgs[1].CostUSD > 1.001 || msgs[1].In != 60 || msgs[1].Out != 30 {
		t.Fatalf("%+v %+v", th, msgs)
	}
	if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 1 || asks[0].Title != "Walked both surfaces" || asks[0].MessageID != msgs[1].ID {
		t.Fatalf("the span's reply is one read card, label stripped: %+v", asks)
	}
	if th.CostUSD < 0.999 || th.CostUSD > 1.001 {
		t.Fatalf("thread cost %v", th.CostUSD)
	}
	m.db.QueryRow(`SELECT held FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&held)
	if held != "" {
		t.Fatal(held)
	}
	// The progress lines are fold text, the reply is not duplicated there.
	evs, _ := m.Events(th.ID, 0, 0, 100)
	var texts []string
	for _, e := range evs {
		if e.Kind == "text" {
			texts = append(texts, e.Body)
		}
	}
	if len(texts) != 2 || texts[0] != "Phone walk is building in the Simulator now." || texts[1] != "Re-walking after the edit." {
		t.Fatalf("%q", texts)
	}
}

// A folded result is still the reply when the process exits on it, and
// when its background task never comes back.
func TestHeldResultLandsOnExitOrTimeout(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Build it.", "", "", nil)
	out := pendingOut(t, m, th.ID)
	appendOut(t, out, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash","description":"Build"}]}
{"type":"result","subtype":"success","is_error":false,"result":"Build is running.","total_cost_usd":0.30,"session_id":"sess-1"}
`)
	m.Poll()
	if msgs, _ := m.Messages(th.ID, 50); len(msgs) != 1 {
		t.Fatalf("%+v", msgs)
	}
	// Quiet for longer than holdTTL: the held text lands, the turn settles.
	m.db.Exec(`UPDATE thread_runs SET last_output=? WHERE thread_id=?`, ts(time.Now().Add(-holdTTL-time.Minute)), th.ID)
	m.Poll()
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 50)
	if th.Status != "needs_you" || len(msgs) != 2 || msgs[1].Text != "" || msgs[1].Kind != "read" || msgs[1].CostUSD < 0.299 {
		t.Fatalf("%+v %+v", th, msgs)
	}
	if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 1 || asks[0].Title != "Build is running" {
		t.Fatalf("held text is the read card: %+v", asks)
	}
	// Same again, but the process exits on the folded result.
	th2, _ := m.Create("", "life", "", "Build it again.", "", "", nil)
	out2 := pendingOut(t, m, th2.ID)
	appendOut(t, out2, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash","description":"Build"}]}
{"type":"result","subtype":"success","is_error":false,"result":"Build is running.","total_cost_usd":0.30,"session_id":"sess-2"}
`)
	m.Poll()
	os.WriteFile(out2+".done", nil, 0o644)
	m.Poll()
	th2, _ = m.Get(th2.ID)
	msgs, _ = m.Messages(th2.ID, 50)
	if th2.Status != "needs_you" || len(msgs) != 2 || msgs[1].Text != "" || msgs[1].Kind != "read" {
		t.Fatalf("%+v %+v", th2, msgs)
	}
}

// THERE IS NO WHITE CELL: anything to read or do arrives as a card. A reply that ends on text with no card raised
// becomes a read card — first line the title (markdown and wrap-up labels
// stripped, at most 80 characters), the rest the detail — linked to an empty
// `read` row, and nothing pushes (a read card never does). The message row
// itself carries no words.
func TestTextReplyBecomesAReadCard(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Migrate the movie site.", "", "", nil)
	complete(t, m, th.ID, "**Done:** movie site is on BigQuery.\n\nPR #87 merged, 18 routes diffed.\n- Supabase is frozen")
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Role != "claude" || last.Text != "" || last.Kind != "read" || th.Status != "needs_you" || th.Unread < 1 {
		t.Fatalf("text reply: role=%s text=%q kind=%s status=%s unread=%d", last.Role, last.Text, last.Kind, th.Status, th.Unread)
	}
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Kind != "read" || asks[0].Class != "read" || asks[0].MessageID != last.ID {
		t.Fatalf("no read card: %+v", asks)
	}
	// One shape: the first paragraph is the message, spoken and
	// leading the card; the paragraphs after it are the detail.
	if asks[0].Title != "Movie site is on BigQuery" || asks[0].Detail != "PR #87 merged, 18 routes diffed.\n- Supabase is frozen" {
		t.Fatalf("card words: %q / %q", asks[0].Title, asks[0].Detail)
	}
	// The card is the REPLY, not a cut in the tool chain: a turn with no tool
	// calls has no block to draw it in, so /steps must leave it to message_id
	// (otherwise the board counts one to read with no cell to show it).
	if steps, _ := m.Steps(th.ID); len(steps) != 1 || steps[0].Segments != nil {
		t.Fatalf("reply card cut into the chain: %+v", steps)
	}
	// The next lean wake still knows what it said: the recap quotes the card.
	if r := m.recap(th); !strings.Contains(r, "you, last time: Movie site is on BigQuery — PR #87 merged") {
		t.Fatalf("recap lost the card: %s", r)
	}
	// The streamed copy of the reply is gone from the tool chain, as before.
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_events WHERE thread_id=? AND kind='text'`, th.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("%d text events kept beside the card", n)
	}
}

// Text beside a card the turn already raised is a restatement of it: it never becomes a second card or a bubble. It stays only as
// the run's last fold line, and the row is empty like any other.
func TestTextBesideACardIsFolded(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "When is the vest?", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	if _, err := m.AddAsk(th.ID, run, "Vest is 2026-09-01", "", "read", ""); err != nil {
		t.Fatal(err)
	}
	m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body) VALUES (?,?,?,?,?,?)`, th.ID, run, ts(time.Now()), "text", "", "Did: checked Pulley; the card has the date.")
	complete(t, m, th.ID, "Did: checked Pulley; the card has the date.")
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Text != "" || last.Kind != "read" {
		t.Fatalf("folded reply: text=%q kind=%s", last.Text, last.Kind)
	}
	if asks, _ := m.ListAsks("active", th.ID, 10); len(asks) != 1 {
		t.Fatalf("restatement became a card: %+v", asks)
	}
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_events WHERE thread_id=? AND kind='text' AND body LIKE 'Did: checked%'`, th.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("fold line kept %d times, want 1", n)
	}
}

// A closing reply brings its own spoken sentence on a "Say:" line, first or
// last; the card is what is left. A "say:" in the middle is body.
func TestSplitSay(t *testing.T) {
	for _, c := range []struct{ in, rest, say string }{
		{"Say: Hey Alex, the puzzle thread is finished.\n\nThread finished\n\nNothing open.", "Thread finished\n\nNothing open.", "Hey Alex, the puzzle thread is finished."},
		{"Thread finished\nNothing open.\n\n**Say:** Hey Alex, it is done.", "Thread finished\nNothing open.", "Hey Alex, it is done."},
		{"Thread finished\nThey say: nothing\nopen.", "Thread finished\nThey say: nothing\nopen.", ""},
		{"Filed a rec.", "Filed a rec.", ""},
		// An opening message may wrap: it runs to the first blank line.
		{"Say: Hey Alex, the build is up.\nIt leads with what it spoke.\n\n1. Tap Install.\n2. Open Sessions.", "1. Tap Install.\n2. Open Sessions.", "Hey Alex, the build is up. It leads with what it spoke."},
	} {
		rest, say := splitSay(c.in)
		if rest != c.rest || say != c.say {
			t.Errorf("splitSay(%q) = %q / %q, want %q / %q", c.in, rest, say, c.rest, c.say)
		}
	}
}

// spokenGreeting is the opener notify.Spoken puts on a line nobody wrote
// (e.g. "Hey Alex, " or "Hey, "), so tests pin what follows it.
func spokenGreeting() string {
	return strings.TrimSuffix(spokenFallback("read", "", "X", ""), "X.")
}

// The message is the reply: a Say message is spoken, leads the card, and
// titles it from its first sentence; only the steps after it are the detail.
// A reply with no Say is read as if its first paragraph were one — ONE card
// shape, no plain replies.
func TestTheMessageIsTheReply(t *testing.T) {
	hey := spokenGreeting()
	for _, c := range []struct{ in, title, detail, say string }{
		{"Say: Hey Alex, the 401k raise is booked: $17.71 a paycheck through February. Two things changed in the books.",
			"The 401k raise is booked: $17.71 a paycheck through February", "",
			"Hey Alex, the 401k raise is booked: $17.71 a paycheck through February. Two things changed in the books."},
		{"Say: Hey Alex, build 1198 is ready. Two steps are on the card.\n\n1. Tap Install.\n2. Open a read card.",
			"Build 1198 is ready", "1. Tap Install.\n2. Open a read card.", "Hey Alex, build 1198 is ready. Two steps are on the card."},
		{"Say: Hey Alex! " + strings.Repeat("word ", 30) + "done.", strings.TrimSpace("Word "+strings.Repeat("word ", 15)) + "…", "", "Hey Alex! " + strings.Repeat("word ", 30) + "done."},
		// A greeting needs its punctuation: "Okay the…" keeps its first word.
		{"Say: Okay the build is up.", "Okay the build is up", "", "Okay the build is up."},
		// No Say: the first paragraph is the message, spoken whole and leading
		// the card; the rest is the detail (never a title cut mid-list and
		// the text drawn twice).
		{"Filed a rec.\nSee the ledger.", "Filed a rec", "", hey + "about Recs. Filed a rec. See the ledger."},
		{"All four overnight sessions (code review, spend audit, speech corpus, preferences) are done, their review docs exist in docs/reviews/. `make check` passes.\n\n[end]",
			"All four overnight sessions (code review, spend audit, speech corpus,…", "",
			hey + "about Recs. All four overnight sessions (code review, spend audit, speech corpus, preferences) are done, their review docs exist in docs/reviews/. make check passes."},
		{"Did: shipped it.\n\n```json\n{\"a\": 1}\n```\n\n- **bold** [link](https://x.y)\n| a | b |\n[end]", "Shipped it", "```json\n{\"a\": 1}\n```\n\n- **bold** [link](https://x.y)\n| a | b |", hey + "about Recs. Shipped it."},
		// A reply that opens with a code block speaks the prose around it.
		{"```\nls\n```\n\nThe build is up.", "The build is up", "```\nls\n```\n\nThe build is up.", hey + "about Recs. The build is up."},
		{"", "Session reply", "", hey + "about Recs. Session reply."},
		// A link in the message moves to the detail whole; the title and the
		// spoken line keep its text (a title cut inside a URL links to a 404).
		{"Say: Hey Alex, the trip PR is up: [trip#1](https://github.com/example/trip/pull/1). The checks passed.\n\n1. Open the PR.",
			"The trip PR is up: trip#1",
			"- [trip#1](https://github.com/example/trip/pull/1)\n\n1. Open the PR.",
			"Hey Alex, the trip PR is up: trip#1. The checks passed."},
		// A bare URL the title cut lands in goes whole.
		{"Say: Hey Alex, the long pull request for the spring trip planner is up at https://github.com/example/trip/pull/1 now",
			"The long pull request for the spring trip planner is up at…", "",
			"Hey Alex, the long pull request for the spring trip planner is up at https://github.com/example/trip/pull/1 now"},
	} {
		title, detail, say := cardFromReply(c.in, "Recs")
		if title != c.title || detail != c.detail || say != c.say {
			t.Errorf("cardFromReply(%q) =\n %q / %q / %q, want\n %q / %q / %q", c.in, title, detail, say, c.title, c.detail, c.say)
		}
	}
}

// A card nobody wrote a sentence for is still one message to the owner, never
// "To read. <title>. <session>".
func TestSpokenFallback(t *testing.T) {
	hey := spokenGreeting()
	if !strings.HasPrefix(hey, "Hey") {
		t.Fatalf("greeting %q", hey)
	}
	for _, c := range []struct{ kind, class, title, thread, want string }{
		{"read", "read", "This thread is completed", "Puzzle layout refinement", "about Puzzle layout refinement. This thread is completed."},
		{"physical", "step", "Buy a new kettle", "Calendar", "a reminder. Buy a new kettle."},
		{"decision", "unblock", "Pick a venue?", "Party", "I need you on Party. Pick a venue?"},
		{"read", "read", "Done.", "", "Done."},
	} {
		if got := spokenFallback(c.kind, c.class, c.title, c.thread); got != hey+c.want {
			t.Errorf("spokenFallback(%q,%q) = %q, want %q", c.kind, c.title, got, c.want)
		}
	}
}

// The events window must be extendable, never re-cut: since=<newest held>
// adds what came after, before=<oldest held> adds the page before it, and the
// two meet with neither a hole nor a duplicate. A client that instead re-read
// "the newest N" watched the block above a steering message count DOWN as
// each new tool call pushed an old event out.
func TestEventsPageBothWays(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Page me.", "", "", nil)
	for i := 1; i <= 7; i++ {
		m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`, th.ID, "r1", ts(time.Now()), "tool_use", "step "+string(rune('0'+i)), "", "")
	}
	newest, _ := m.Events(th.ID, 0, 0, 3)
	if len(newest) != 3 || newest[0].Title != "step 5" || newest[2].Title != "step 7" {
		t.Fatalf("%+v", newest)
	}
	older, _ := m.Events(th.ID, 0, newest[0].ID, 3)
	if len(older) != 3 || older[0].Title != "step 2" || older[2].Title != "step 4" {
		t.Fatalf("%+v", older)
	}
	first, _ := m.Events(th.ID, 0, older[0].ID, 3)
	if len(first) != 1 || first[0].Title != "step 1" {
		t.Fatalf("%+v", first)
	}
	m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`, th.ID, "r1", ts(time.Now()), "tool_use", "step 8", "", "")
	after, _ := m.Events(th.ID, newest[2].ID, 0, 3)
	if len(after) != 1 || after[0].Title != "step 8" {
		t.Fatalf("%+v", after)
	}
}

// The headline of a run block is the HUB's count, so it is right however many
// events a surface holds: 450 tool calls before a steering message stay 450
// while the block under the message grows. first_id/last_id
// bound the block so its steps are one ranged read.
func TestStepsCountedByTheHub(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do the long thing.", "", "", nil)
	msgs, _ := m.Messages(th.ID, 10)
	start := msgs[0]
	base := start.TS
	ev := func(at time.Time, kind string) {
		m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`,
			th.ID, start.RunID, ts(at), kind, "step", "", "")
	}
	ev(base.Add(-time.Second), "thinking") // stamped before its own starter
	for i := 1; i <= 450; i++ {
		ev(base.Add(time.Duration(i)*time.Second), "tool_use")
	}
	steerAt := base.Add(500 * time.Second)
	m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, kind, text, run_id, steered) VALUES (?,?,?,?,?,?,1)`,
		th.ID, ts(steerAt), "owner", "message", "and the phone", start.RunID)
	for i := 1; i <= 3; i++ {
		ev(steerAt.Add(time.Duration(i)*time.Second), "tool_use")
	}
	steps, err := m.Steps(th.ID)
	if err != nil || len(steps) != 2 {
		t.Fatalf("%+v %v", steps, err)
	}
	if steps[0].Tools != 450 || steps[0].Thoughts != 1 || steps[0].Steps != 451 || steps[0].MessageID != start.ID {
		t.Fatalf("%+v", steps[0])
	}
	if steps[1].Tools != 3 || steps[1].Steps != 3 || steps[1].RunID != start.RunID {
		t.Fatalf("%+v", steps[1])
	}
	if steps[0].FirstID != 1 || steps[0].LastID != 451 || steps[1].FirstID != 452 || steps[1].LastID != 454 {
		t.Fatalf("%+v", steps)
	}
	// The bounds are a real window: reading them back gives that block only.
	evs, _ := m.Events(th.ID, steps[1].FirstID-1, steps[1].LastID+1, 500)
	if len(evs) != 3 {
		t.Fatalf("%+v", evs)
	}
}

// A card raised mid-chain CUTS the block it was raised in: the surfaces draw
// fold · card · fold, each fold with the hub's own count for it: a card
// appears where it was raised, so it can be answered mid-chain. The segments must tile the block exactly — same total, same
// bounds — or a surface prints a number that does not add up.
func TestStepsCutAtTheCardsRaisedMidRun(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do the long thing.", "", "", nil)
	msgs, _ := m.Messages(th.ID, 10)
	start := msgs[0]
	base := start.TS
	for i := 1; i <= 10; i++ {
		m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`,
			th.ID, start.RunID, ts(base.Add(time.Duration(i)*time.Second)), "tool_use", "step", "", "")
	}
	// The agent hands the owner a card at step 3 and another at step 7, and keeps
	// working. (AddAsk stamps `now`; the chain is written in the future here,
	// so the card is dated onto the step it was raised at.)
	raise := func(title string, at time.Duration) string {
		a, err := m.AddAsk(th.ID, start.RunID, title, "", "decision", "")
		if err != nil {
			t.Fatal(err)
		}
		m.db.Exec(`UPDATE items SET created_at=? WHERE id=?`, ts(base.Add(at)), a.ID)
		return a.ID
	}
	a1 := raise("Which account?", 3500*time.Millisecond)
	a2 := raise("Sell 3 or 5?", 7500*time.Millisecond)

	steps, err := m.Steps(th.ID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("%+v %v", steps, err)
	}
	b := steps[0]
	if b.Steps != 10 || len(b.Segments) != 3 {
		t.Fatalf("%+v", b)
	}
	if b.Segments[0].Ref != "ask:"+a1 || b.Segments[1].Ref != "ask:"+a2 || b.Segments[2].Ref != "" {
		t.Fatalf("segments name the card that closes them, the tail names none: %+v", b.Segments)
	}
	if b.Segments[0].Steps != 3 || b.Segments[1].Steps != 4 || b.Segments[2].Steps != 3 {
		t.Fatalf("%+v", b.Segments)
	}
	// They tile the block: no step counted twice, none lost, bounds contiguous.
	total := 0
	for i, s := range b.Segments {
		total += s.Steps
		if i > 0 && s.FirstID != b.Segments[i-1].LastID+1 {
			t.Fatalf("hole between segments %d and %d: %+v", i-1, i, b.Segments)
		}
	}
	if total != b.Steps || b.Segments[0].FirstID != b.FirstID || b.Segments[2].LastID != b.LastID {
		t.Fatalf("segments must tile the block: %+v", b)
	}
	// A card raised after the last step so far leaves no empty fold behind it.
	a3 := raise("Ship it?", 20*time.Second)
	steps, _ = m.Steps(th.ID)
	if segs := steps[0].Segments; len(segs) != 3 || segs[2].Ref != "ask:"+a3 || segs[2].Steps != 3 {
		t.Fatalf("trailing card must close the tail, not open an empty one: %+v", segs)
	}
	// A superseded card is drawn by nobody, so it cuts nothing.
	m.db.Exec(`UPDATE items SET state='superseded' WHERE id=?`, a1)
	steps, _ = m.Steps(th.ID)
	if segs := steps[0].Segments; len(segs) != 2 || segs[0].Ref != "ask:"+a2 || segs[0].Steps != 7 {
		t.Fatalf("superseded card still cutting the chain: %+v", segs)
	}
	// No cards at all: one fold, exactly as before.
	m.db.Exec(`UPDATE items SET state='superseded' WHERE thread_id=?`, th.ID)
	steps, _ = m.Steps(th.ID)
	if steps[0].Segments != nil || steps[0].Steps != 10 {
		t.Fatalf("%+v", steps[0])
	}
}

// A card raised without its run id lands nowhere: /steps cannot cut the chain
// at it and finishTurn links message_id BY run_id, so it is drawn on no screen
// while the board still counts it. LiveRun is what the hub fills in at
// the moment the card is raised, and backfillAskRun repairs the rows written
// before it did.
func TestCardWithNoRunIsTiedToTheRunItWasRaisedIn(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Do the long thing.", "", "", nil)
	msgs, _ := m.Messages(th.ID, 10)
	start := msgs[0]
	if m.LiveRun(th.ID) != start.RunID {
		t.Fatalf("live run: %q want %q", m.LiveRun(th.ID), start.RunID)
	}
	for i := 1; i <= 6; i++ {
		m.db.Exec(`INSERT INTO thread_events (thread_id, run_id, ts, kind, title, body, summary) VALUES (?,?,?,?,?,?,?)`,
			th.ID, start.RunID, ts(start.TS.Add(time.Duration(i)*time.Second)), "tool_use", "step", "", "")
	}
	a, err := m.AddAsk(th.ID, "", "Which account?", "", "decision", "")
	if err != nil {
		t.Fatal(err)
	}
	m.db.Exec(`UPDATE items SET created_at=? WHERE id=?`, ts(start.TS.Add(3500*time.Millisecond)), a.ID)
	if steps, _ := m.Steps(th.ID); len(steps) != 1 || steps[0].Segments != nil {
		t.Fatalf("a card with no run cuts nothing until it is repaired: %+v", steps)
	}
	m.backfillAskRun()
	got, _ := m.GetAsk(a.ID)
	if got.RunID != start.RunID {
		t.Fatalf("run_id %q, want %q", got.RunID, start.RunID)
	}
	steps, _ := m.Steps(th.ID)
	if len(steps) != 1 || len(steps[0].Segments) != 2 || steps[0].Segments[0].Ref != "ask:"+a.ID {
		t.Fatalf("repaired card must cut its chain: %+v", steps)
	}
	if steps[0].Segments[0].Steps != 3 || steps[0].Segments[1].Steps != 3 {
		t.Fatalf("%+v", steps[0].Segments)
	}
	// Idempotent, and a card raised outside every run of its thread keeps none
	// (the surfaces place that one by time instead).
	m.db.Exec(`UPDATE thread_runs SET finished_at=? WHERE id=?`, ts(start.TS.Add(time.Minute)), start.RunID)
	b, _ := m.AddAsk(th.ID, "", "Later, by hand", "", "decision", "")
	m.db.Exec(`UPDATE items SET created_at=? WHERE id=?`, ts(start.TS.Add(time.Hour)), b.ID)
	m.backfillAskRun()
	if got, _ := m.GetAsk(b.ID); got.RunID != "" {
		t.Fatalf("card raised outside every run must stay unanchored: %q", got.RunID)
	}
	if got, _ := m.GetAsk(a.ID); got.RunID != start.RunID {
		t.Fatalf("backfill moved a card it had already tied: %q", got.RunID)
	}
}

func TestToolSummary(t *testing.T) {
	cases := map[string]string{
		`{"command":"git status","description":"Show working tree status"}`: "Show working tree status",
	}
	for in, want := range cases {
		if got, _ := toolSummary("Bash", []byte(in)); got != want {
			t.Fatalf("%s: %q", in, got)
		}
	}
	home, _ := os.UserHomeDir()
	if s, _ := toolSummary("Edit", []byte(`{"file_path":"`+filepath.Join(home, "x", "y.go")+`"}`)); s != "Edit ~/x/y.go" {
		t.Fatal(s)
	}
	if s, _ := toolSummary("WebSearch", []byte(`{"query":"claude stream-json"}`)); s != `Search the web for "claude stream-json"` {
		t.Fatal(s)
	}
	if s, d := toolSummary("Bash", []byte(`{"command":"ls -la"}`)); s != "" || d != "ls -la" {
		t.Fatal(s, d)
	}
}
