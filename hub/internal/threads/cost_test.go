package threads

import (
	"strings"
	"testing"
	"time"
)

// Cost cuts: preflight once per CLI version, the board as a diff, a cold card
// answer one rung down.

// Preflight runs once per CLI version + ladder: a clean pass is remembered,
// a new version probes again.
func TestPreflightSkipsAPassedVersion(t *testing.T) {
	m, _, _ := setup(t)
	version := "2.1.300 (Claude Code)"
	m.CLIVersion = func() string { return version }
	probes := 0
	m.Probe = func(string) (string, error) { probes++; return "OK\n", nil }
	ladder := []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5"}
	m.PreflightModels(ladder)
	m.PreflightModels(ladder)
	if probes != 3 {
		t.Fatalf("same version probed again: %d probes", probes)
	}
	version = "2.1.301 (Claude Code)"
	m.PreflightModels(ladder)
	if probes != 6 {
		t.Fatalf("a new CLI must be probed: %d probes", probes)
	}
	m.PreflightModels(append(ladder, "claude-haiku-4-5"))
	if probes != 10 {
		t.Fatalf("a new ladder must be probed: %d probes", probes)
	}
}

// The board rides whole on the first turn, then only as what changed.
func TestBoardBlocksSendOnlyChanges(t *testing.T) {
	m, _, _ := setup(t)
	me, _ := m.CreateIdle("me", "Me", "life")
	other, _ := m.CreateIdle("other", "Face and supplements", "life")
	if _, err := m.AddAsk(other.ID, "", "Hand over the signed form", "", "physical", ""); err != nil {
		t.Fatal(err)
	}
	first := m.boardBlocks(me.ID, false)
	if !strings.Contains(first, boardHead) || !strings.Contains(first, "Hand over the signed form") {
		t.Fatal(first)
	}
	if again := m.boardBlocks(me.ID, false); again != "[Board and other sessions: unchanged since your last turn.]\n\n" {
		t.Fatal(again)
	}
	a2, _ := m.AddAsk(other.ID, "", "Pick the venue", "", "decision", "")
	d := m.boardBlocks(me.ID, false)
	if strings.Contains(d, boardHead) || !strings.Contains(d, a2.ID) || strings.Contains(d, "signed form") {
		t.Fatalf("delta should carry only the new row:\n%s", d)
	}
	m.ResolveAsk(a2.ID, "done", "owner", "")
	if d := m.boardBlocks(me.ID, false); !strings.Contains(d, "gone: "+a2.ID) {
		t.Fatal(d)
	}
	// A fresh start, or a stale last full copy, gets the whole block again.
	// A block that was empty, so never carried its rules, arrives whole.
	lone, _ := m.CreateIdle("lone", "Lone", "life")
	m.db.Exec(`UPDATE threads SET status='idle'`)
	m.boardBlocks(lone.ID, false)
	m.db.Exec(`UPDATE threads SET status='running' WHERE id=?`, other.ID)
	if d := m.boardBlocks(lone.ID, false); !strings.Contains(d, peersHead) {
		t.Fatal(d)
	}
	if f := m.boardBlocks(me.ID, true); !strings.Contains(f, boardHead) {
		t.Fatal(f)
	}
	m.boardMu.Lock()
	s := m.boardSeen[me.ID]
	s.full = time.Now().Add(-boardRefresh - time.Minute)
	m.boardSeen[me.ID] = s
	m.boardMu.Unlock()
	if f := m.boardBlocks(me.ID, false); !strings.Contains(f, boardHead) {
		t.Fatal(f)
	}
}

// A card answer on a cold thread starts one rung down; the owner's next
// message inside the hour stays there; a warm answer stays on the top rung.
func TestColdCardAnswerStartsBelowFable(t *testing.T) {
	m, _, _ := setup(t)
	m.ColdStart = "claude-opus-5-5"
	th, _ := m.CreateIdle("t", "T", "life")
	th.ClaudeSessionID = "sess"
	reply := func(ago time.Duration) {
		m.db.Exec(`DELETE FROM thread_messages WHERE thread_id=?`, th.ID)
		m.db.Exec(`INSERT INTO thread_messages (thread_id, ts, role, text) VALUES (?,?,?,?)`, th.ID, ts(time.Now().Add(-ago)), "claude", "done")
	}
	lastRun := func(model string) {
		m.db.Exec(`DELETE FROM thread_runs WHERE thread_id=?`, th.ID)
		m.db.Exec(`INSERT INTO thread_runs (id, thread_id, started_at, trigger, out_file, model) VALUES ('r', ?, ?, 'decision', '', ?)`, th.ID, ts(time.Now()), model)
	}
	reply(3 * time.Hour)
	if !m.coldStart(th, "decision", "") || !m.coldStart(th, "decision", "claude-fable-5-1") {
		t.Fatal("cold decision should start below fable")
	}
	if m.coldStart(th, "decision", "claude-sonnet-5-5") || m.coldStart(th, "message", "") || m.coldStart(th, "checkin", "") {
		t.Fatal("only a cold card answer on a fable start moves")
	}
	reply(10 * time.Minute)
	lastRun("claude-fable-5-1")
	if m.coldStart(th, "decision", "") || m.coldStart(th, "message", "") {
		t.Fatal("a warm thread keeps its rung")
	}
	lastRun("claude-opus-5-5")
	if !m.coldStart(th, "message", "") {
		t.Fatal("the owner's message inside the hour stays on the cold answer's rung")
	}
	m.ColdStart = ""
	reply(3 * time.Hour)
	if m.coldStart(th, "decision", "") {
		t.Fatal("off when ColdStart is empty")
	}
}
