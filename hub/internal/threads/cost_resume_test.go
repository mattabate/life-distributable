package threads

import (
	"os"
	"testing"

	"life/hub/internal/budget"
)

// The CLI's total_cost_usd is the session's, summed across every --resume:
// a resumed run books only what it added, not the session's lifetime again
// each run (#104).
func TestResumedRunBooksOnlyItsOwnCost(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "First turn.", "", "", nil)
	finish := func(total string) {
		out := pendingOut(t, m, th.ID)
		os.WriteFile(out, []byte(`{"type":"result","result":"done","is_error":false,"total_cost_usd":`+total+`,"session_id":"s1"}`+"\n"), 0o644)
		os.WriteFile(out+".done", nil, 0o644)
		m.Poll()
	}
	finish("2")
	if err := m.Send(th.ID, "Second turn."); err != nil {
		t.Fatal(err)
	}
	finish("2.5")
	th, _ = m.Get(th.ID)
	if d := th.CostUSD - 2.5; d > 0.001 || d < -0.001 {
		t.Fatalf("thread cost %v, want 2.5 (2 then 0.5 more)", th.CostUSD)
	}
	var sum float64
	m.db.QueryRow(`SELECT SUM(cost_seen) FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&sum)
	if d := sum - 2.5; d > 0.001 || d < -0.001 {
		t.Fatalf("runs' own cost %v, want 2.5", sum)
	}
	// A fresh session (its total below the base) counts in full.
	m.db.Exec(`UPDATE threads SET claude_session_id='' WHERE id=?`, th.ID)
	if err := m.Send(th.ID, "Third turn."); err != nil {
		t.Fatal(err)
	}
	finish("0.3")
	if th, _ = m.Get(th.ID); th.CostUSD < 2.799 || th.CostUSD > 2.801 {
		t.Fatalf("thread cost %v, want 2.8", th.CostUSD)
	}
}

// A run that dies before its result still counts its cost: Stop mid-turn
// used to set finished_at and strand the live estimate, which the daily and
// per-thread meters stop counting once the run is closed. Now it is booked
// like a result, and the resumed CLI's total (which includes the dead turn)
// books only the rest.
func TestStoppedRunBooksItsEstimateOnce(t *testing.T) {
	m, _, _ := setup(t)
	g, err := budget.New(m.db, 0, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	th, _ := m.Create("", "life", "", "First turn.", "", "", nil)
	result := func(total string) {
		appendOut(t, pendingOut(t, m, th.ID), `{"type":"result","result":"done","is_error":false,"total_cost_usd":`+total+`,"session_id":"s1"}`+"\n")
		m.Poll()
	}
	// $2.00 of output on opus-5-5 ($20/MTok), streamed, then Stop.
	stream := func(id string) {
		appendOut(t, pendingOut(t, m, th.ID), `{"type":"assistant","message":{"id":"`+id+`","model":"claude-opus-5-5","usage":{"input_tokens":0,"output_tokens":100000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[{"type":"text","text":"working"}]}}`+"\n")
		m.Poll()
		if err := m.Stop(th.ID); err != nil {
			t.Fatal(err)
		}
	}
	agree := func(want float64) {
		t.Helper()
		th, _ := m.Get(th.ID)
		var live float64
		m.db.QueryRow(`SELECT COALESCE(SUM(live_cost_usd),0) FROM thread_runs WHERE thread_id=?`, th.ID).Scan(&live)
		day, thr := g.SpentToday(), g.ThreadSpentToday(th.ID)
		for _, v := range []float64{day, thr, th.CostUSD} {
			if v < want-0.001 || v > want+0.001 {
				t.Fatalf("want %.2f everywhere: today %.4f, thread today %.4f, thread %.4f (live left %.4f)", want, day, thr, th.CostUSD, live)
			}
		}
		if live != 0 {
			t.Fatalf("live estimate stranded: %v", live)
		}
	}
	result("1")
	agree(1)
	if err := m.Send(th.ID, "Second turn."); err != nil {
		t.Fatal(err)
	}
	stream("msg_a")
	agree(3)
	// The resumed session's total includes the dead turn (it really cost
	// $2.30): 1 + 2.30 + 0.50 for the new turn. Booked: the $0.80 left.
	if err := m.Send(th.ID, "Third turn."); err != nil {
		t.Fatal(err)
	}
	result("3.8")
	agree(3.8)
	// An estimate that ran high: the dead turn really cost $1.20 and the next
	// $0.30, a total $0.50 under the base. Not a fresh session (which would
	// book all $5.30 again): nothing more.
	if err := m.Send(th.ID, "Fourth turn."); err != nil {
		t.Fatal(err)
	}
	stream("msg_b")
	agree(5.8)
	if err := m.Send(th.ID, "Fifth turn."); err != nil {
		t.Fatal(err)
	}
	result("5.3")
	agree(5.8)
	// ...and the turn after counts from the CLI's figure again.
	if err := m.Send(th.ID, "Sixth turn."); err != nil {
		t.Fatal(err)
	}
	result("5.5")
	agree(6)
}
