package threads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

type nfy struct{ msgs []string }

func (n *nfy) NeedsYou(s string) error { n.msgs = append(n.msgs, "needs:"+s); return nil }

func setup(t *testing.T) (*Manager, *nfy, *[]string) { return setupAt(t, t.TempDir()) }

// setupAt opens (or reopens) the manager over dir/t.db.
func setupAt(t *testing.T, dir string) (*Manager, *nfy, *[]string) {
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	n := &nfy{}
	m, err := New(db, "/usr/local/bin/claude", filepath.Join(dir, "runs"), "life", func(p string) (string, bool) { return dir, p == "life" }, []string{"Read"}, n)
	if err == nil {
		m.Summarize = nil // no claude in tests
	}
	if err != nil {
		t.Fatal(err)
	}
	var cmds []string
	m.Run = func(name string, args ...string) ([]byte, error) {
		cmds = append(cmds, strings.Join(append([]string{name}, args...), " "))
		return nil, nil
	}
	return m, n, &cmds
}

// simulate claude finishing a run by writing its output file
func complete(t *testing.T, m *Manager, threadID, result string) {
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, threadID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	if out == "" {
		t.Fatal("no pending run")
	}
	os.WriteFile(out, []byte(`{"result":`+jsonStr(result)+`,"is_error":false,"total_cost_usd":0.12,"session_id":"sess-123"}`), 0o644)
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
}

// clearCards dismisses a thread's open asks, as the owner would before
// archiving it (Archive refuses while one waits on them).
func clearCards(m *Manager, threadID string) {
	for _, a := range m.activeAsks(threadID) {
		m.ResolveAsk(a.ID, "dismissed", "owner", "")
	}
}

func jsonStr(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

func TestLifecycle(t *testing.T) {
	m, n, cmds := setup(t)
	th, err := m.Create("", "life", "save-more", "Figure out how to split my savings cash.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if th.Status != "running" || !strings.HasPrefix(th.ID, "figure-out-how-to-split") {
		t.Fatalf("%+v", th)
	}
	if !strings.Contains((*cmds)[0], "tmux new-session -d -s life-th-"+th.ID) || strings.Contains((*cmds)[0], "'--resume'") {
		t.Fatal((*cmds)[0])
	}
	// prompt file has preamble + goal hint
	prompt, _ := filepath.Glob(filepath.Join(m.RunsDir, th.ID+"-*.prompt"))
	b, _ := os.ReadFile(prompt[0])
	if !strings.Contains(string(b), "lifectl goal save-more") || !strings.Contains(string(b), "savings cash") {
		t.Fatal(string(b))
	}
	if !strings.Contains((*cmds)[0], "'stream-json' '--verbose'") {
		t.Fatal((*cmds)[0])
	}
	// lifectl propose inside the run tags actions with this thread
	if !strings.Contains((*cmds)[0], "LIFE_THREAD_ID='"+th.ID+"' ") {
		t.Fatal((*cmds)[0])
	}
	// bulleted / bold variants must count too (a "- NEEDS YOU:" line once fell through)
	complete(t, m, th.ID, "I looked at things.\n- NEEDS YOU: tell me your risk tolerance\n2. **NEEDS YOU:** confirm the date")
	th, _ = m.Get(th.ID)
	// Unread is one per card plus one for the reply itself.
	if th.Status != "needs_you" || th.ClaudeSessionID != "sess-123" || th.Unread != 3 || th.NeedsYou != 2 || th.CostUSD != 0.12 {
		t.Fatalf("%+v", th)
	}
	// each NEEDS YOU line became an ask row pointing at the reply it came from
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 2 || asks[0].State != "open" || asks[0].MessageID == 0 || asks[0].Title != "tell me your risk tolerance" {
		t.Fatalf("%+v", asks)
	}
	// One card, one buzz, at the moment the card is raised — no
	// digest push that bundles a turn's cards into one line, because the cards
	// themselves are never bundled either.
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[0], "risk tolerance") || !strings.Contains(n.msgs[1], "confirm the date") {
		t.Fatal(n.msgs)
	}
	// a reply that references one ask answers only that ask (a response to
	// the card directly). The reference is data now, not prose:
	// ReplyRef only translates the app's legacy "Re ask-xxxx" at the door.
	m.markAnswered(th.ID, ReplyRef("Re "+asks[1].ID+" \"confirm\": yes the 4th"))
	asks, _ = m.ListAsks("active", th.ID, 10)
	if asks[0].State != "open" || asks[1].State != "answered" {
		t.Fatalf("targeted reply: %+v", asks)
	}
	if ReplyRef("re: nothing") != "" || ReplyRef(" Re ask-1a2b: x") != "ask:ask-1a2b" {
		t.Fatal("ReplyRef")
	}
	// follow-up resumes the same claude session
	if err := m.Send(th.ID, "moderate risk; Thursday the 4th"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*cmds)[1], "'--resume' 'sess-123'") {
		t.Fatal((*cmds)[1])
	}
	// the owner's reply answers the asks but they stay on the board until closed;
	// the resumed run is told what is pending
	asks, _ = m.ListAsks("active", th.ID, 10)
	if len(asks) != 2 || asks[0].State != "answered" || asks[1].State != "answered" {
		t.Fatalf("%+v", asks)
	}
	if pb, _ := os.ReadFile(promptFile(t, m, th.ID)); !strings.Contains(string(pb), "[Open asks on this thread") || !strings.Contains(string(pb), asks[0].ID) {
		t.Fatal(string(pb))
	}
	if _, err := m.ResolveAsk(asks[0].ID, "done", "claude:thread:"+th.ID, "moderate"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveAsk(asks[1].ID, "done", "claude:thread:"+th.ID, "Sep 4"); err != nil {
		t.Fatal(err)
	}
	// The plan went out as a proposal; the reply itself is `[end]` (any words
	// here would be a read card).
	complete(t, m, th.ID, EndSentinel)
	th, _ = m.Get(th.ID)
	if th.NeedsYou != 0 || th.Status != "done" {
		t.Fatalf("%+v", th)
	}
	msgs, _ := m.Messages(th.ID, 50)
	if len(msgs) != 4 || msgs[0].Role != "owner" || msgs[3].Kind != "message" || msgs[0].RunID == "" || msgs[1].RunID != msgs[0].RunID || msgs[0].Queued {
		t.Fatalf("%+v", msgs)
	}
	// schedule + check-in: the schedule is a standing prompt row; the clock
	// fires it when its not_before passes (one clock).
	if _, err := m.Update(th.ID, map[string]string{"schedule": "every@6h"}); err != nil {
		t.Fatal(err)
	}
	th, _ = m.Get(th.ID)
	if th.Schedule != "every@6h" || th.NextRunAt == nil {
		t.Fatalf("schedule not derived from the standing row: %+v", th)
	}
	m.DuePrompts(time.Now().Add(time.Minute))
	if th, _ = m.Get(th.ID); th.Status == "running" {
		t.Fatal("fired before its time")
	}
	m.DuePrompts(time.Now().Add(7 * time.Hour))
	th, _ = m.Get(th.ID)
	if th.Status != "running" || !strings.Contains((*cmds)[2], "'--resume' 'sess-123'") {
		t.Fatalf("%+v %s", th, (*cmds)[2])
	}
	before, _ := m.Get(th.ID)
	complete(t, m, th.ID, EndSentinel)
	// quiet check-in: no notification, stays idle (scheduled), unread unchanged
	th, _ = m.Get(th.ID)
	if len(n.msgs) != 2 || th.Status != "idle" || th.Unread != before.Unread {
		t.Fatalf("%v %+v", n.msgs, th)
	}
	// the owner approves an action this thread proposed, with a note → its next turn
	if err := m.Decision(th.ID, true, "act-1", "Sell 3 shares", "only if SPY is above 500"); err != nil {
		t.Fatal(err)
	}
	msgs, _ = m.Messages(th.ID, 1)
	// the message is the owner's words only; WHICH action rides on in_reply_to
	if msgs[0].Role != "owner" || msgs[0].Kind != "decision" || !strings.HasPrefix(msgs[0].Text, "Approved: Sell 3 shares\nonly if SPY") {
		t.Fatalf("%+v", msgs[0])
	}
	var outFile string
	m.db.QueryRow(`SELECT out_file FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&outFile)
	b, _ = os.ReadFile(strings.TrimSuffix(outFile, ".out") + ".prompt")
	if !strings.Contains(string(b), "[The owner APPROVED the action you proposed — act-1.") || !strings.Contains(string(b), "only if SPY is above 500") {
		t.Fatal(string(b))
	}
	complete(t, m, th.ID, "Sold.")
	clearCards(m, th.ID)
	if _, err := m.Update(th.ID, map[string]string{"status": "archived"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := m.List(false); len(l) != 0 {
		t.Fatal(l)
	}
	if l, _ := m.List(true); len(l) != 1 {
		t.Fatal(l)
	}
}

// The list is ordered by the last MESSAGE, not by updated_at: closing an ask
// or settling a cost on an old thread must not lift it above the one the
// owner just wrote to.
func TestListOrderedByLastMessage(t *testing.T) {
	m, _, _ := setup(t)
	old, err := m.Create("", "life", "", "Older thread.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, old.ID, "Done with the old one.")
	time.Sleep(5 * time.Millisecond)
	newer, err := m.Create("", "life", "", "Newer thread.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, newer.ID, "Done with the new one.")
	// Something that is not a message touches the old thread: an ask closed
	// by the verifier, a status flip, a cost settling.
	time.Sleep(5 * time.Millisecond)
	m.db.Exec(`UPDATE threads SET updated_at=? WHERE id=?`, ts(time.Now()), old.ID)
	l, _ := m.List(false)
	if len(l) != 2 || l[0].ID != newer.ID || l[1].ID != old.ID {
		t.Fatalf("expected %s before %s, got %v", newer.ID, old.ID, l)
	}
	// A message on the old thread — from anyone — is what lifts it.
	time.Sleep(5 * time.Millisecond)
	if err := m.Send(old.ID, "Look in the intake folder."); err != nil {
		t.Fatal(err)
	}
	l, _ = m.List(false)
	if len(l) != 2 || l[0].ID != old.ID {
		t.Fatalf("expected %s first after a message, got %v", old.ID, l)
	}
}

func pendingOut(t *testing.T, m *Manager, threadID string) string {
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, threadID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	if out == "" {
		t.Fatal("no pending run")
	}
	return out
}

func TestErrorRun(t *testing.T) {
	m, n, _ := setup(t)
	th, _ := m.Create("x", "life", "", "do it", "", "", nil)
	rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=?`, th.ID)
	var out string
	for rows.Next() {
		rows.Scan(&out)
	}
	rows.Close()
	os.WriteFile(out, []byte("not json"), 0o644)
	os.WriteFile(out+".err", []byte("claude: boom"), 0o644)
	os.WriteFile(out+".done", nil, 0o644)
	m.Poll()
	th, _ = m.Get(th.ID)
	if th.Status != "needs_you" || th.NeedsYou != 1 || len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "Session failed") {
		t.Fatalf("%+v %v", th, n.msgs)
	}
	if th.LastMessageKind != "error" {
		t.Fatalf("last_message_kind = %q", th.LastMessageKind)
	}
}

// A turn that ends in an error envelope (API 500, plan limit) keeps kind
// `error` even though it also raised a read-ask: the failure ask used to
// relabel it `needs_you`, and then every list drew the CLI's error paragraph
// as if it were the session's answer.
func TestFailedTurnIsAnError(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("x", "life", "", "do it", "", "", nil)
	out := pendingOut(t, m, th.ID)
	// Not an API 5xx: that is retried in silence now (retry_test.go). This is
	// about what a failure that survives looks like in the message list.
	os.WriteFile(out, []byte(`{"type":"result","result":"claude: invalid tool permission","is_error":true,"total_cost_usd":0.5,"session_id":"sess-1"}`+"\n"), 0o644)
	m.Poll()
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Kind != "error" || !strings.Contains(last.Text, "invalid tool permission") {
		t.Fatalf("%+v", last)
	}
	th, _ = m.Get(th.ID)
	if th.LastMessageKind != "error" || th.Status != "needs_you" {
		t.Fatalf("%+v", th)
	}
}

// A repeating hub check (the reconcile tick) must not re-raise a finding the
// owner has already closed: AddAsk only supersedes the ACTIVE twin, so the tick asks
// AskSeen first, which counts dismissed and done ones too.
func TestAskSeenCoversClosedAsks(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "reconcile", "", "", nil)
	title := "broker-individual-1234: SPY doesn't match the trade log as of 2026-07-31"
	if m.AskSeen(th.ID, title) {
		t.Fatal("nothing raised yet")
	}
	a, err := m.AddAsk(th.ID, "", title, "- **SPY**: trade log 2 shares", "read", "")
	if err != nil {
		t.Fatal(err)
	}
	if !m.AskSeen(th.ID, "  "+strings.ToUpper(title)+"  ") {
		t.Fatal("open ask not seen (the title match is normalised)")
	}
	if _, err := m.ResolveAsk(a.ID, "dismissed", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if !m.AskSeen(th.ID, title) {
		t.Fatal("a dismissed ask still counts as seen, or the tick nags forever")
	}
	if m.AskSeen("some-other-thread", title) {
		t.Fatal("AskSeen is per thread")
	}
}

// An ask raised mid-run via the API (what `lifectl ask add` does) is the
// primary path: it is linked to the reply at finish, a same-titled re-raise
// supersedes it, the owner's Done posts a decision message, Clear dismisses.
// A reply whose only asks are `read` is an answer, not a stopped session:
// its message kind is `read` (blue on both surfaces), not `needs_you` (red).
// One non-read ask in the same turn makes the whole reply `needs_you`.
// The card is the reply (a wrap-up repeating the card is noise; the agent
// need not send a final message): a turn that raised a
// card and then ended with NO text still settles as one message row — empty
// text, kind `read`, the card linked to it by message_id, the cost on it — so
// both surfaces draw "turn ended · $" and the card lands where it was raised.
// A lean wake's recap quotes the card in the row's place.
func TestEmptyReplyIsTheTurnEnding(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "How far is the run?", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	a, err := m.AddAsk(th.ID, run, "Run is 0.7% done, ETA 6pm", "7e9 of 1e12 checked, all clean.", "read", "")
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "")
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Role != "claude" || last.Text != "" || last.Kind != "read" || last.CostUSD != 0.12 || th.Status != "needs_you" {
		t.Fatalf("empty reply: role=%s text=%q kind=%s cost=%v status=%s", last.Role, last.Text, last.Kind, last.CostUSD, th.Status)
	}
	var mid int64
	m.db.QueryRow(`SELECT message_id FROM items WHERE id=?`, a.ID).Scan(&mid)
	if mid != last.ID {
		t.Fatalf("card not linked to the empty reply: message_id=%d want %d", mid, last.ID)
	}
	if got, _ := m.ListAsks("active", th.ID, 10); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("the card must survive an empty reply: %+v", got)
	}
	// A lean wake's recap quotes the card, not the empty row.
	if r := m.recap(th); !strings.Contains(r, "you, last time: Run is 0.7% done, ETA 6pm — 7e9 of 1e12 checked, all clean.") {
		t.Fatalf("recap does not quote the card: %s", r)
	}
}

// The model cannot actually return nothing: the claude CLI (2.1.183+)
// re-prompts once on a turn with no visible text, which is exactly the second
// completion that must not appear (a "the harness demanded visible output"
// bubble under the card). So the reply is the one
// token EndSentinel — backticks tolerated — and the hub stores the same empty
// row, with no stray text event left behind.
func TestEndSentinelIsAnEmptyReply(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "How far is the run?", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	if _, err := m.AddAsk(th.ID, run, "Run is 0.7% done", "", "read", ""); err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "`"+EndSentinel+"`")
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Role != "claude" || last.Text != "" || last.Kind != "read" || th.Status != "needs_you" {
		t.Fatalf("sentinel reply: role=%s text=%q kind=%s status=%s", last.Role, last.Text, last.Kind, th.Status)
	}
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM thread_events WHERE kind='text' AND body LIKE '%' || ? || '%'`, EndSentinel).Scan(&n)
	if n != 0 {
		t.Fatalf("sentinel left %d text events behind", n)
	}
}

func TestReadOnlyReplyKind(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "When is the vest?", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	if _, err := m.AddAsk(th.ID, run, "Vest is 2026-09-01", "The portal shows the cliff on 09-01.", "read", ""); err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "Did: checked the portal.\nAsk: read-card with the date.")
	th, _ = m.Get(th.ID)
	msgs, _ := m.Messages(th.ID, 10)
	if th.Status != "needs_you" || msgs[len(msgs)-1].Kind != "read" {
		t.Fatalf("read-only reply: status=%s kind=%s", th.Status, msgs[len(msgs)-1].Kind)
	}
	if err := m.Send(th.ID, "and the next one?"); err != nil {
		t.Fatal(err)
	}
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=? AND finished_at IS NULL`, th.ID).Scan(&run)
	m.AddAsk(th.ID, run, "Next vest 2026-12-01", "", "read", "")
	m.AddAsk(th.ID, run, "Send me the portal login", "", "access", "")
	complete(t, m, th.ID, "Did: two cards.")
	msgs, _ = m.Messages(th.ID, 10)
	if msgs[len(msgs)-1].Kind != "needs_you" {
		t.Fatalf("mixed reply kind=%s", msgs[len(msgs)-1].Kind)
	}
}

// Closing a read card says nothing the session can act on, so it queues no
// prompt and the session is not woken. Every other
// kind still relays Done/Won't do, and words typed with the close still go.
func TestReadCloseQueuesNoPrompt(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "When is the vest?", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	rd, _ := m.AddAsk(th.ID, run, "Vest is 2026-09-01", "", "read", "")
	other, _ := m.AddAsk(th.ID, run, "Send me the portal login", "", "access", "")
	withNote, _ := m.AddAsk(th.ID, run, "Cliff math", "", "read", "")
	complete(t, m, th.ID, "Did: three cards.")

	pending := func() int {
		var n int
		m.db.QueryRow(`SELECT COUNT(*) FROM prompts WHERE target=?`, th.ID).Scan(&n)
		return n
	}
	if _, err := m.ResolveAsk(rd.ID, "dismissed", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 0 {
		t.Fatalf("dismissing a read queued %d prompts, want 0", n)
	}
	if _, err := m.ResolveAsk(other.ID, "done", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 1 {
		t.Fatalf("closing an access ask queued %d prompts, want 1", n)
	}
	// Not even with a note: the note is the resolution written on the card,
	// and the button that carries one still only says the card was read.
	// Words the owner TYPES are a prompt of their own (Queue), which is the
	// one thing that wakes a session for a read.
	if _, err := m.ResolveAsk(withNote.ID, "done", "owner", "the cliff is right"); err != nil {
		t.Fatal(err)
	}
	if n := pending(); n != 1 {
		t.Fatalf("closing a read WITH a note queued %d prompts, want 1", n)
	}
	if a, _ := m.GetAsk(withNote.ID); a.State != "done" || a.Resolution != "the cliff is right" {
		t.Fatalf("the note is still the record on the card: %+v", a)
	}
	if a, _ := m.GetAsk(rd.ID); a.State != "dismissed" {
		t.Fatalf("read ask state=%s, want dismissed", a.State)
	}
}

func TestAsks(t *testing.T) {
	m, n, _ := setup(t)
	th, _ := m.Create("", "life", "", "Set up the sync.", "", "", nil)
	var run string
	m.db.QueryRow(`SELECT id FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&run)
	a, err := m.AddAsk(th.ID, run, "Send me the sync setup token", "Tried the connector; needs a token from the provider", "access", "lifectl sources shows it")
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "open" || a.MessageID != 0 || a.CheckHint == "" {
		t.Fatalf("%+v", a)
	}
	complete(t, m, th.ID, "Did: wired the connector.\nAsk: send me the sync setup token.")
	th, _ = m.Get(th.ID)
	a, _ = m.GetAsk(a.ID)
	msgs, _ := m.Messages(th.ID, 10)
	// Unread is 2: the card the moment it was raised (mid-run) and the reply
	// that ended the turn — two new things, one push (the card's, at raise
	// time; finishTurn no longer pushes a second buzz for the same card).
	if th.Status != "needs_you" || th.Unread != 2 || a.MessageID != msgs[len(msgs)-1].ID || msgs[len(msgs)-1].Kind != "needs_you" || len(n.msgs) != 1 {
		t.Fatalf("%+v %+v %v", th, a, n.msgs)
	}
	// no duplicate from the prose fallback: the run already raised its ask
	if as, _ := m.ListAsks("all", th.ID, 10); len(as) != 1 {
		t.Fatalf("%+v", as)
	}
	// re-raise by hand (outside a run) supersedes
	b, _ := m.AddAsk(th.ID, "", "Send me the sync setup token.", "", "access", "")
	a, _ = m.GetAsk(a.ID)
	if a.State != "superseded" || a.SupersededBy != b.ID {
		t.Fatalf("%+v", a)
	}
	// a second, unrelated ask is open on the same thread
	d, _ := m.AddAsk(th.ID, "", "Which account is primary?", "", "decision", "")
	// the owner marks ONE done from the app → decision message wakes the agent
	if _, err := m.ResolveAsk(b.ID, "done", "owner", "token pasted in Settings"); err != nil {
		t.Fatal(err)
	}
	th, _ = m.Get(th.ID)
	msgs, _ = m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if th.Status != "running" || last.Role != "owner" || last.Kind != "decision" || !strings.Contains(last.Text, "Done: Send me") {
		t.Fatalf("%+v %+v", th, last)
	}
	// the relay is about b only: d is NOT flipped to answered (marking one
	// done must not approve its sibling), and the
	// agent is told so instead of the proposal verdict header
	if d, _ = m.GetAsk(d.ID); d.State != "open" {
		t.Fatalf("sibling ask touched by a Done relay: %+v", d)
	}
	if pb, _ := os.ReadFile(promptFile(t, m, th.ID)); !strings.Contains(string(pb), "responded to ONE ask — "+b.ID) || strings.Contains(string(pb), "action you proposed") || !strings.Contains(string(pb), d.ID+" (decision, open)") {
		t.Fatal(string(pb))
	}
	if _, err := m.ResolveAsk(d.ID, "done", "claude:thread:"+th.ID, "checking"); err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, EndSentinel)
	th, _ = m.Get(th.ID)
	if th.Status != "done" || th.NeedsYou != 0 {
		t.Fatalf("%+v", th)
	}
	// Clear from board dismisses whatever is active
	c, _ := m.AddAsk(th.ID, "", "Pick a rebalance date", "", "decision", "")
	th, _ = m.Get(th.ID)
	if th.Status != "needs_you" {
		t.Fatalf("%+v", th)
	}
	if _, err := m.Update(th.ID, map[string]string{"status": "done"}); err != nil {
		t.Fatal(err)
	}
	c, _ = m.GetAsk(c.ID)
	th, _ = m.Get(th.ID)
	if c.State != "dismissed" || c.ResolvedBy != "owner" || th.Status != "done" {
		t.Fatalf("%+v %+v", c, th)
	}
}

func promptFile(t *testing.T, m *Manager, threadID string) string {
	var out string
	m.db.QueryRow(`SELECT out_file FROM thread_runs WHERE thread_id=? ORDER BY started_at DESC, id DESC LIMIT 1`, threadID).Scan(&out)
	return strings.TrimSuffix(out, ".out") + ".prompt"
}

func TestStop(t *testing.T) {
	m, _, cmds := setup(t)
	th, err := m.Create("", "life", "", "Do a long thing.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(th.ID); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 2 || !strings.HasPrefix((*cmds)[1], "tmux kill-session -t life-th-"+th.ID+"-") {
		t.Fatal(*cmds)
	}
	th, _ = m.Get(th.ID)
	if th.Status != "idle" || th.LastMessage != "Stopped by you." {
		t.Fatalf("%+v", th)
	}
	if err := m.Stop(th.ID); err == nil {
		t.Fatal("second stop should fail")
	}
	// Poll must not resurrect the killed run.
	m.Poll()
	if th, _ = m.Get(th.ID); th.Status != "idle" {
		t.Fatal(th.Status)
	}
	// Resumable afterwards.
	if err := m.Send(th.ID, "continue"); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveKillsAndClearsSchedule(t *testing.T) {
	m, _, cmds := setup(t)
	th, err := m.Create("", "life", "", "Do a long thing.", "every@6h", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Archiving a running thread kills the run and drops the schedule.
	if err := m.Archive(th.ID); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 2 || !strings.HasPrefix((*cmds)[1], "tmux kill-session -t life-th-"+th.ID+"-") {
		t.Fatal(*cmds)
	}
	th, _ = m.Get(th.ID)
	if th.Status != "archived" || th.Schedule != "" {
		t.Fatalf("%+v", th)
	}
	m.Poll()
	if th, _ = m.Get(th.ID); th.Status != "archived" {
		t.Fatal(th.Status)
	}
	// Hidden from the default list, never due.
	if l, _ := m.List(false); len(l) != 0 {
		t.Fatal(l)
	}
	// Its standing row went with it: nothing queued, nothing fires.
	if ps, _ := m.ListPrompts("queued", "", 10); len(ps) != 0 {
		t.Fatalf("archived thread still has queued prompts: %+v", ps)
	}
	n := len(*cmds)
	m.DuePrompts(time.Now().Add(7 * time.Hour))
	if len(*cmds) != n {
		t.Fatal("archived thread was considered for check-in")
	}
	// Idempotent.
	if err := m.Archive(th.ID); err != nil {
		t.Fatal(err)
	}
	// Un-archive → resumable.
	if _, err := m.Update(th.ID, map[string]string{"status": "idle"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(th.ID, "continue"); err != nil {
		t.Fatal(err)
	}
}

// A card waiting on the owner keeps its session off the archive.
func TestArchiveRefusedWhileACardWaits(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "Ask them something.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.AddAsk(th.ID, "", "Pick one", "", "decision", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Archive(th.ID); err == nil {
		t.Fatal("archived a session with an open card")
	}
	if got, _ := m.Get(th.ID); got.Status == "archived" {
		t.Fatal(got.Status)
	}
	if _, err := m.ResolveAsk(a.ID, "dismissed", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Archive(th.ID); err != nil {
		t.Fatal(err)
	}
}

// Only the newest "Install app build N" card may show: a new one supersedes
// every lower-N active install ask on any thread.
func TestInstallAskSupersedesOlderBuilds(t *testing.T) {
	m, _, _ := setup(t)
	t1, _ := m.Create("", "life", "", "Calendar tab.", "", "", nil)
	t2, _ := m.Create("", "life", "", "Inline ask cards.", "", "", nil)
	a, _ := m.AddAsk(t1.ID, "", "Install app build 182 (tap the link)", "itms://x", "physical", "")
	b, _ := m.AddAsk(t2.ID, "", "Install app build 184 (asks as cards)", "itms://x", "physical", "")
	a, _ = m.GetAsk(a.ID)
	if a.State != "superseded" || a.SupersededBy != b.ID {
		t.Fatalf("%+v", a)
	}
	// The stale card stays in its session, so it must say why it is closed.
	if a.Resolution != "replaced by build 184" {
		t.Fatalf("resolution: %q", a.Resolution)
	}
	// an older build raised later does not touch the newer one, and is born
	// superseded: only the newest may show
	c, _ := m.AddAsk(t1.ID, "", "Install app build 183 (late)", "", "physical", "")
	b, _ = m.GetAsk(b.ID)
	c, _ = m.GetAsk(c.ID)
	if b.State != "open" || c.State != "superseded" || c.SupersededBy != b.ID {
		t.Fatalf("%+v %+v", b, c)
	}
	// The SAME number from another session is the same install (two sessions
	// can ship one tree as the same build minutes apart): the later
	// card retires the earlier one, so only one shows.
	d, _ := m.AddAsk(t1.ID, "", "Install app build 184 (tap the link)", "itms://x", "install", "")
	b, _ = m.GetAsk(b.ID)
	if b.State != "superseded" || b.SupersededBy != d.ID || b.Resolution != "replaced by build 184" {
		t.Fatalf("equal build: %+v", b)
	}
}

// A tapped (done) install card is not superseded by a newer raise, so the
// reconciler used to reopen it 10 min later while the phone still ran the
// old build: two install cards side by side. An older card is never
// reopened, and a stale open one is superseded on the next tick.
func TestInstallReconcileNeverReopensOlder(t *testing.T) {
	m, _, _ := setup(t)
	t1, _ := m.Create("", "life", "", "Calendar rail.", "", "", nil)
	t2, _ := m.Create("", "life", "", "App icon.", "", "", nil)
	a, _ := m.AddAsk(t1.ID, "", "Install app build 1438 (tap the link)", "itms://x", "install", "")
	m.ResolveAsk(a.ID, "done", "app", "tapped Install")
	m.db.Exec(`UPDATE items SET resolved_at=? WHERE id=?`, ts(time.Now().Add(-11*time.Minute)), a.ID)
	b, _ := m.AddAsk(t2.ID, "", "Install app build 1442 (tap the link)", "itms://x", "install", "")
	m.ReconcileInstalls(1433)
	a, _ = m.GetAsk(a.ID)
	if a.State != "done" {
		t.Fatalf("older tapped card reopened: %+v", a)
	}
	// a stale open older card (left by the old rule) is superseded on reconcile
	m.db.Exec(`UPDATE items SET state='open' WHERE id=?`, a.ID)
	m.ReconcileInstalls(1433)
	a, _ = m.GetAsk(a.ID)
	if a.State != "superseded" || a.SupersededBy != b.ID {
		t.Fatalf("stale older card not superseded: %+v", a)
	}
}

// A kind=install card keeps its number however the session worded the title.
// A strict "Install app build N" matcher misses "Install build 1251: …", so
// neither a newer card nor the phone's report would close it.
func TestInstallAskAnyTitleSupersedesAndReconciles(t *testing.T) {
	m, _, _ := setup(t)
	t1, _ := m.Create("", "life", "", "Waiting to speak UI.", "", "", nil)
	t2, _ := m.Create("", "life", "", "Speaking pill.", "", "", nil)
	a, _ := m.AddAsk(t1.ID, "", "Install build 1251: Play says speaking, pushes wait their turn", "itms://x", "install", "")
	if a.Kind != "install" {
		t.Fatalf("kind: %+v", a)
	}
	// a newer card, in the standard form, retires the oddly worded one
	b, _ := m.AddAsk(t2.ID, "", "Install app build 1286 (tap the link)", "itms://x", "install", "")
	a, _ = m.GetAsk(a.ID)
	if a.State != "superseded" || a.SupersededBy != b.ID || a.Resolution != "replaced by build 1286" {
		t.Fatalf("custom title not superseded: %+v", a)
	}
	// and the reverse: an oddly worded newer card retires a standard older one
	c, _ := m.AddAsk(t1.ID, "", "Install build 1290 — the Learn page fix", "itms://x", "install", "")
	b, _ = m.GetAsk(b.ID)
	if b.State != "superseded" || b.SupersededBy != c.ID || b.Resolution != "replaced by build 1290" {
		t.Fatalf("standard title not superseded by custom: %+v", b)
	}
	// the phone reporting ≥N closes an oddly worded card too
	m.ReconcileInstalls(1290)
	c, _ = m.GetAsk(c.ID)
	if c.State != "done" || c.Resolution != "phone reports build 1290" {
		t.Fatalf("custom title not reconciled: %+v", c)
	}
	// a read card that merely mentions a build is not an install
	r, _ := m.AddAsk(t1.ID, "", "Build 1300 crashed on launch — read the log", "…", "read", "")
	if r.Kind != "read" || askBuild(r.Kind, r.Title) != 0 {
		t.Fatalf("read card counted as an install: %+v", r)
	}
}

// An "Install app build N" ask IS kind install whatever the session sent —
// the teal cell must show every time — with surface mobile
// and the Installed / Won't install chips.
func TestInstallTitleNormalisesToInstallKind(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Ship a build.", "", "", nil)
	a, _ := m.AddAsk(th.ID, "", "Install app build 812 (tap the link)", "what changed\n\nhttps://hub/ota/x/install.html", "physical", "")
	if a.Kind != "install" || a.Surface != "mobile" {
		t.Fatalf("%+v", a)
	}
	if len(a.Outcomes) != 3 || a.Outcomes[0] != (Outcome{Value: "done", Label: "Installed"}) {
		t.Fatalf("outcomes: %+v", a.Outcomes)
	}
	// declared directly, same result; a non-install title keeps its own kind
	b, _ := m.AddAsk(th.ID, "", "Install app build 813 (tap the link)", "", "install", "")
	if b.Kind != "install" || b.Surface != "mobile" {
		t.Fatalf("%+v", b)
	}
	c, _ := m.AddAsk(th.ID, "", "Connect Apple Health", "Settings › Health", "physical", "")
	if c.Kind != "physical" {
		t.Fatalf("%+v", c)
	}
}

// "NEEDS YOU 2026-08-27: …" is parked on the calendar for that morning
// instead of landing on the board today (a step that can only be taken on
// Thursday shows up on Thursday). Undated lines are unchanged.
func TestDatedNeedsYouGoesToTheCalendar(t *testing.T) {
	m, n, _ := setup(t)
	type parked struct{ title, kind, day, at string }
	var got []parked
	m.DateAsk = func(threadID, title, detail, kind, check, day, at, surface string) error {
		got = append(got, parked{title, kind, day, at})
		return nil
	}
	th, _ := m.Create("", "life", "", "Plan the tranches.", "", "", nil)
	complete(t, m, th.ID, "Planned it.\n- NEEDS YOU 2026-08-27: buy tranche 2 at the broker\nNEEDS YOU on 2026-09-03 09:30: sweep the rest\nNEEDS YOU: pick a risk tolerance")
	if len(got) != 2 || got[0].day != "2026-08-27" || got[0].at != "" || got[0].title != "buy tranche 2 at the broker" {
		t.Fatalf("%+v", got)
	}
	if got[1].day != "2026-09-03" || got[1].at != "09:30" {
		t.Fatalf("%+v", got)
	}
	// only the undated line is on the board now
	asks, _ := m.ListAsks("active", th.ID, 10)
	if len(asks) != 1 || asks[0].Title != "pick a risk tolerance" {
		t.Fatalf("%+v", asks)
	}
	if len(n.msgs) != 1 || strings.Contains(n.msgs[0], "broker") {
		t.Fatalf("notified about a future day: %v", n.msgs)
	}
	// no calendar wired (nil DateAsk) → dated lines still reach the owner today
	m2, _, _ := setup(t)
	th2, _ := m2.Create("", "life", "", "x", "", "", nil)
	complete(t, m2, th2.ID, "NEEDS YOU 2026-08-27: buy tranche 2")
	if a2, _ := m2.ListAsks("active", th2.ID, 10); len(a2) != 1 || a2[0].Title != "buy tranche 2" {
		t.Fatalf("%+v", a2)
	}
}

// A scheduled session is named after the JOB IT REPEATS, and that name holds
// still: the calendar draws it on every future occurrence, so re-summarizing
// it from each run's conversation would put one run's topic ("dark mode") on
// the agenda for every day to come.
func TestScheduledThreadTitleIsTheJobNotTheLastRun(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "check the app", "", "", nil)
	m.Summarize = func(dir, prompt string) (string, error) {
		if strings.Contains(prompt, "recurring background session") {
			return "Daily app and console quality pass", nil
		}
		return "Dark mode UI verification and display fixes", nil
	}
	// no cadence yet: the title tracks the conversation, as it always has
	m.autoTitle(th.ID)
	if got, _ := m.Get(th.ID); got.Title != "Dark mode UI verification and display fixes" {
		t.Fatalf("unscheduled thread should follow its conversation: %q", got.Title)
	}
	// giving it a cadence names it after the standing prompt...
	if err := m.SetSchedule(th.ID, "daily@05:00", "Autonomous app-improvement run, once a day. Screenshot every tab and fix what is wrong."); err != nil {
		t.Fatal(err)
	}
	// titleFromSchedule runs in the background (Summarize shells out to Haiku)
	for i := 0; i < 200; i++ {
		if g, _ := m.Get(th.ID); g.Title == "Daily app and console quality pass" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// ...and no later run can rename it back
	m.autoTitle(th.ID)
	if got, _ := m.Get(th.ID); got.Title != "Daily app and console quality pass" {
		t.Fatalf("a run re-titled a standing session: %q", got.Title)
	}
	// a hand rename still wins, and still sticks
	if _, err := m.Update(th.ID, map[string]string{"title": "App quality"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSchedule(th.ID, "daily@06:00", "Autonomous app-improvement run, once a day. Screenshot every tab and fix what is wrong."); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(th.ID); got.Title != "App quality" {
		t.Fatalf("renamed by hand, then overwritten: %q", got.Title)
	}
}

// Titles KEEP their markdown — the app renders true bold. What must not happen is a
// title that is a truncated echo of the reply, or a push banner showing the
// asterisks, since a notification cannot render anything.
func TestTitleMarkdownSurvivesButNotEchoes(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"**Done:** 222,623 rows synced", "Done: 222,623 rows synced"},
		{"- **Health sync** shipped", "Health sync shipped"},
		{"1. fix `app.js` and *ship* it", "fix app.js and ship it"},
		{"See [the run](https://x/y)", "See the run"},
		{"Account history chart", "Account history chart"},
	} {
		if got := plainTitle(c.in); got != c.want {
			t.Fatalf("plainTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	m, n, _ := setup(t)
	th, _ := m.Create("", "life", "", "**Ship** the health sync", "", "", nil)
	if th.Title != "**Ship** the health sync" {
		t.Fatalf("create stripped the title: %q", th.Title)
	}

	// a summarizer that echoes the reply instead of summarizing keeps the
	// old title rather than replacing it with a truncated copy
	m.Summarize = func(dir, prompt string) (string, error) {
		return "**Done:** 222,623 rows synced (10 years of steps, workouts, sleep, weight, everything Apple Health had)", nil
	}
	m.autoTitle(th.ID)
	if got, _ := m.Get(th.ID); got.Title != "**Ship** the health sync" {
		t.Fatalf("echoed summary became the title: %q", got.Title)
	}
	// ...and so does one that starts a tool call instead of naming the chat
	for _, junk := range []string{"<function_calls>", "```bash", "---"} {
		m.Summarize = func(dir, prompt string) (string, error) { return junk, nil }
		m.autoTitle(th.ID)
		if got, _ := m.Get(th.ID); got.Title != "**Ship** the health sync" {
			t.Fatalf("summary %q became the title: %q", junk, got.Title)
		}
	}
	// junk summary over a junk title: the opening words instead
	m.db.Exec(`UPDATE threads SET title='<function_calls>' WHERE id=?`, th.ID)
	m.autoTitle(th.ID)
	if got, _ := m.Get(th.ID); got.Title != "Ship the health sync" {
		t.Fatalf("junk title kept: %q", got.Title)
	}
	m.db.Exec(`UPDATE threads SET title='**Ship** the health sync' WHERE id=?`, th.ID)
	m.Summarize = func(dir, prompt string) (string, error) { return "**Health sync** shipped\n", nil }
	m.autoTitle(th.ID)
	if got, _ := m.Get(th.ID); got.Title != "**Health sync** shipped" {
		t.Fatalf("auto-title %q", got.Title)
	}

	// ...but the push it sends is flat
	n.msgs = nil
	m.db.Exec(`UPDATE threads SET status='idle' WHERE id=?`, th.ID) // not mid-run: AddAsk pushes itself
	if _, err := m.AddAsk(th.ID, "", "**Install** app build 9", "tap it", "physical", ""); err != nil {
		t.Fatal(err)
	}
	if len(n.msgs) != 1 || strings.Contains(n.msgs[0], "*") {
		t.Fatalf("push carries markdown: %v", n.msgs)
	}
}

// A rec filed from a session is stamped with the model that wrote it: the live
// run's model first, else the newest run that named one, else nothing.
func TestLiveModel(t *testing.T) {
	m, _, _ := setup(t)
	if got := m.LiveModel("nope"); got != "" {
		t.Fatal(got)
	}
	m.db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, finished_at, trigger, model, out_file, in_file) VALUES ('r1','th-1','2026-08-26T01:00:00Z','2026-08-26T01:10:00Z','message','claude-opus-5','','')`)
	if got := m.LiveModel("th-1"); got != "claude-opus-5" {
		t.Fatal(got)
	}
	m.db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, model, out_file, in_file) VALUES ('r2','th-1','2026-08-26T02:00:00Z','message','claude-fable-5','','')`)
	if got := m.LiveModel("th-1"); got != "claude-fable-5" {
		t.Fatal(got)
	}
}

// A message from the web console carries a one-line stamp — its minute and
// the trail.py commands that read the console's own log for it — and never
// the log itself: the log is queried, never injected. Both a new session's first
// message and a reply are stamped; a message from anywhere else is not.
func TestConsoleMessageIsStampedNotInjected(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.CreateVia("", "life", "", "which number did I highlight?", "", "", nil, "console")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SendVia(th.ID, "and the one after it", nil, "console"); err != nil {
		t.Fatal(err)
	}
	if err := m.SendAttached(th.ID, "from the phone", nil); err != nil {
		t.Fatal(err)
	}
	// The rows carry the surface; the test's stub claude may already have
	// taken them off the queue, so they are put back and read as queued() reads.
	m.db.Exec(`UPDATE thread_messages SET queued=1 WHERE thread_id=? AND role='owner'`, th.ID)
	qs, _ := m.queued(th.ID)
	if len(qs) != 3 || qs[0].via != "console" || qs[1].via != "console" || qs[2].via != "" || qs[0].ts.IsZero() {
		t.Fatalf("via per row: %+v", qs)
	}
	p, err := m.prompt(th, qs[:1], false, true)
	if err != nil {
		t.Fatal(err)
	}
	at := qs[0].ts.In(time.Local).Format("15:04")
	for _, want := range []string{"[Sent from the web console at " + at, "ops/py.sh trail.py at " + at + "`", "trail.py motion --at " + at + "`", "trail.py said --at " + at + " --minutes 30`", "--date " + qs[0].ts.In(time.Local).Format("2006-01-02")} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "the last 10 minutes on the console") || strings.Contains(p, "highlighted \"") {
		t.Fatalf("the log must not be in the prompt:\n%s", p)
	}
	// A reply through the console is stamped too; one from the app is not.
	p, _ = m.prompt(th, qs[1:], false, false)
	if n := strings.Count(p, "[Sent from the web console"); n != 1 {
		t.Fatalf("one stamp for the console reply only, got %d:\n%s", n, p)
	}
	if i, j := strings.Index(p, "and the one after it"), strings.Index(p, "[Sent from the web console"); !(i < j && j < strings.Index(p, "from the phone")) {
		t.Fatalf("the stamp sits under the console message:\n%s", p)
	}
}
