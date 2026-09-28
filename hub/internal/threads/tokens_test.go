package threads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A chat's token total: live off the streamed assistant messages while the
// turn is in flight, then the result line's exact usage once it settles.
func TestTokensLiveThenSettled(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "How much am I burning?", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runOut(t, m, th.ID)

	// Mid-turn: one message, emitted once per content block. The repeat must
	// not be counted twice.
	assistant := `{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":1000,"cache_creation_input_tokens":100},"content":[{"type":"text","text":"working"}]}}`
	// A parallel subagent's message lands between the repeats; each still
	// counts once.
	sub := `{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_2","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[{"type":"text","text":"sub"}]}}`
	stream := assistant + "\n" + sub + "\n" + assistant + "\n" + sub + "\n" + assistant + "\n"
	os.WriteFile(out, []byte(stream), 0o644)
	m.Poll()
	live, _ := m.Get(th.ID)
	if live.Total != 1117 || live.CacheRead != 1000 {
		t.Fatalf("live: %+v", live.Tokens)
	}

	// The result line settles the turn with the real usage, replacing (not
	// adding to) the live estimate.
	os.WriteFile(out, []byte(stream+
		`{"type":"result","result":"burned a lot","is_error":false,"total_cost_usd":0.5,"session_id":"s1","usage":{"input_tokens":10,"output_tokens":300,"cache_read_input_tokens":1000,"cache_creation_input_tokens":100}}`+"\n"), 0o644)
	m.Poll()
	got, _ := m.Get(th.ID)
	if got.Total != 1410 || got.In != 10 || got.Out != 300 || got.CacheRead != 1000 || got.CacheWrite != 100 {
		t.Fatalf("settled: %+v", got.Tokens)
	}
	msgs, _ := m.Messages(th.ID, 10)
	last := msgs[len(msgs)-1]
	if last.Total != 1410 || last.Out != 300 {
		t.Fatalf("message: %+v", last.Tokens)
	}
}

// The dollar figure is what the owner reads, so it has to move while
// a turn is in flight — and each message must be priced at ITS OWN model: a
// chat that drops from Opus to Sonnet part-way through is billed at both
// rates, never at one blended one.
func TestLiveCostIsPricedPerModel(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "What is this costing me?", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runOut(t, m, th.ID)

	// 100k input on Sonnet 5 ($3/M) = $0.30; the same on Opus 5 ($5/M) = $0.50.
	sonnet := `{"type":"assistant","message":{"id":"msg_s","model":"claude-sonnet-5","usage":{"input_tokens":100000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	opus := `{"type":"assistant","message":{"id":"msg_o","model":"claude-opus-5","usage":{"input_tokens":100000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	os.WriteFile(out, []byte(sonnet+"\n"+opus+"\n"), 0o644)
	m.Poll()
	live, _ := m.Get(th.ID)
	if d := live.CostUSD - 0.80; d > 0.001 || d < -0.001 {
		t.Fatalf("live cost %v, want 0.80 (0.30 Sonnet + 0.50 Opus)", live.CostUSD)
	}

	// The result line carries the CLI's own figure, which is authoritative:
	// the estimate is dropped, not added to.
	os.WriteFile(out, []byte(sonnet+"\n"+opus+"\n"+
		`{"type":"result","result":"done","is_error":false,"total_cost_usd":2,"session_id":"s1","usage":{"input_tokens":200000,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`+"\n"), 0o644)
	m.Poll()
	got, _ := m.Get(th.ID)
	if d := got.CostUSD - 2; d > 0.001 || d < -0.001 {
		t.Fatalf("settled cost %v, want 2", got.CostUSD)
	}
	// And the same dollars, split by the model that earned them.
	var sum float64
	for _, c := range got.CostByModel {
		sum += c.CostUSD
	}
	if len(got.CostByModel) == 0 || sum < 1.99 || sum > 2.01 {
		t.Fatalf("cost by model %+v, want it to add to 2", got.CostByModel)
	}
}

// Chats that ran before the columns existed get their totals from the run
// logs still on disk, so the phone shows a number against every session.
func TestBackfillTokensFromRunLogs(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "Older chat", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runOut(t, m, th.ID)
	os.WriteFile(out, []byte(
		`{"type":"result","result":"one","is_error":false,"total_cost_usd":0.5,"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":30,"cache_creation_input_tokens":4}}`+"\n"+
			`{"type":"result","result":"two","is_error":false,"total_cost_usd":0.9,"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":30,"cache_creation_input_tokens":4}}`+"\n"), 0o644)

	tok, ok := tokensInRunLog(out)
	if !ok || tok.Total != 74 || tok.CacheRead != 60 {
		t.Fatalf("parse: %+v ok=%v", tok, ok)
	}

	// The backfill parses in the background, so wait for it to land.
	m.backfillTokens()
	var after Thread
	for i := 0; i < 200; i++ {
		after, _ = m.Get(th.ID)
		if after.Total > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after.Total != 74 || after.CacheRead != 60 {
		t.Fatalf("backfilled: %+v", after.Tokens)
	}

	// Guard: once a thread counts, a second pass must not double it.
	m.backfillTokens()
	time.Sleep(50 * time.Millisecond)
	again, _ := m.Get(th.ID)
	if again.Total != 74 {
		t.Fatalf("re-run doubled the total: %+v", again.Tokens)
	}
}

func runOut(t *testing.T, m *Manager, threadID string) string {
	t.Helper()
	outs, _ := filepath.Glob(filepath.Join(m.RunsDir, threadID+"-*.out"))
	if len(outs) == 0 {
		var out string
		rows, _ := m.db.Query(`SELECT out_file FROM thread_runs WHERE thread_id=?`, threadID)
		for rows.Next() {
			rows.Scan(&out)
		}
		rows.Close()
		if out == "" {
			t.Fatal("no run")
		}
		return out
	}
	return strings.TrimSpace(outs[0])
}
