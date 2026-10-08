package threads

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"life/hub/internal/goals"
	"life/hub/internal/store"
)

// One green card per session: a rewritten schedule replaces the old card
// where the rewrite happened, and once a check-in has run the card moves
// below that run — the next check-in is what matters.
func TestOneScheduleCard(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "Watch the thing.", "every@6h", "look at the thing", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, EndSentinel)
	m.Update(th.ID, map[string]string{"schedule_prompt": "look at the thing, v2"})
	m.Send(th.ID, "hi")
	complete(t, m, th.ID, EndSentinel)
	m.Update(th.ID, map[string]string{"schedule_prompt": "look at the thing, v3"})
	cards := func() (n int, at int, text string, msgs []Message) {
		msgs, _ = m.Messages(th.ID, 50)
		msgs = byRun(msgs)
		for i, x := range msgs {
			if x.Kind == "schedule" {
				n, at, text = n+1, i, x.Text
			}
		}
		return n, at, text, msgs
	}
	if n, at, text, msgs := cards(); n != 1 || !strings.HasSuffix(text, "v3") || at != len(msgs)-1 {
		t.Fatalf("%d cards, at %d of %d: %q", n, at, len(msgs), text)
	}
	// A check-in runs: the card follows it, with the row's new next time.
	m.DuePrompts(time.Now().Add(7 * time.Hour))
	complete(t, m, th.ID, EndSentinel)
	standing, _ := m.Standing(th.ID)
	n, at, text, msgs := cards()
	if n != 1 || at != len(msgs)-1 || !strings.Contains(text, "· next "+standing.NotBefore.Local().Format("Mon Jan 2 15:04")+"\n") {
		t.Fatalf("%d cards, at %d of %d: %q", n, at, len(msgs), text)
	}
}

// byRun orders a chat the way both surfaces draw it (ThreadDetail.ordered,
// threads.js orderedMsgs): a run's messages at its start, else by time.
func byRun(msgs []Message) []Message {
	start := map[string]time.Time{}
	for _, x := range msgs {
		if s, ok := start[x.RunID]; x.RunID != "" && (!ok || x.TS.Before(s)) {
			start[x.RunID] = x.TS
		}
	}
	key := func(x Message) time.Time {
		if s, ok := start[x.RunID]; ok {
			return s
		}
		return x.TS
	}
	out := append([]Message(nil), msgs...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := key(out[i]), key(out[j]); !a.Equal(b) {
			return a.Before(b)
		}
		return out[i].TS.Before(out[j].TS)
	})
	return out
}

// One clock: a session's schedule is a standing prompt row. The
// clock fires it by writing a one-shot child and delivering that; the
// standing row advances and is never delivered itself.
func TestStandingRowFiresAChildAndAdvances(t *testing.T) {
	m, _, cmds := setup(t)
	th, err := m.Create("", "life", "", "Watch the thing.", "every@6h", "look at the thing", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, EndSentinel)
	ps, _ := m.ListPrompts("queued", "", 10)
	if len(ps) != 1 || ps[0].Repeat != "every@6h" || ps[0].Author != "hub" || ps[0].Target != th.ID || ps[0].Text != "look at the thing" {
		t.Fatalf("standing row: %+v", ps)
	}
	standing := ps[0]
	if th, _ = m.Get(th.ID); th.Schedule != "every@6h" || th.SchedulePrompt != "look at the thing" || th.NextRunAt == nil || !th.NextRunAt.Equal(standing.NotBefore) {
		t.Fatalf("thread does not read its schedule off the row: %+v", th)
	}
	// The schedule card names the row's own next time.
	msgs, _ := m.Messages(th.ID, 20)
	var card string
	for _, x := range msgs {
		if x.Kind == "schedule" {
			card = x.Text
		}
	}
	if !strings.Contains(card, "Checks back every 6h · next") || !strings.Contains(card, "look at the thing") {
		t.Fatal(card)
	}
	// Not yet.
	n := len(*cmds)
	m.DuePrompts(time.Now().Add(time.Minute))
	if len(*cmds) != n {
		t.Fatal("fired early")
	}
	// Due: one child, delivered as a check-in; the standing row moved on.
	at := time.Now().Add(7 * time.Hour)
	m.DuePrompts(at)
	if len(*cmds) != n+1 {
		t.Fatal("did not fire")
	}
	all, _ := m.ListPrompts("", "", 10)
	var child *Prompt
	for i := range all {
		if all[i].Parent == standing.ID {
			child = &all[i]
		}
	}
	if child == nil || child.State != "delivered" || child.Repeat != "" || child.Text != "look at the thing" || child.Author != "hub" {
		t.Fatalf("child: %+v", all)
	}
	again, ok := m.Standing(th.ID)
	if !ok || again.ID != standing.ID || !again.NotBefore.After(at) || again.State != "queued" {
		t.Fatalf("standing row did not advance: %+v", again)
	}
	if pb, _ := os.ReadFile(promptFile(t, m, th.ID)); !strings.Contains(string(pb), "[scheduled check-in]") || !strings.Contains(string(pb), "look at the thing") {
		t.Fatal(string(pb))
	}
	// Firing again at the same instant does nothing: one child per occurrence.
	m.DuePrompts(at)
	if len(*cmds) != n+1 {
		t.Fatal("fired twice for one occurrence")
	}
	// A running session holds its wake — nothing written, still due.
	m.DuePrompts(again.NotBefore.Add(time.Minute))
	if held, _ := m.Standing(th.ID); !held.NotBefore.Equal(again.NotBefore) {
		t.Fatal("advanced while the session was running")
	}
	if len(*cmds) != n+1 {
		t.Fatal("fired into a running session")
	}
	// Quiet check-in reply — `[end]`, since any words would be a read card:
	// idle (still scheduled), no card.
	complete(t, m, th.ID, EndSentinel)
	if th, _ = m.Get(th.ID); th.Status != "idle" {
		t.Fatalf("%+v", th)
	}
	// An empty standing text fires the default check-in.
	if _, err := m.Update(th.ID, map[string]string{"schedule": "every@6h", "schedule_prompt": ""}); err != nil {
		t.Fatal(err)
	}
	m.DuePrompts(time.Now().Add(7 * time.Hour))
	if pb, _ := os.ReadFile(promptFile(t, m, th.ID)); !strings.Contains(string(pb), "Scheduled check-in. Review this thread") {
		t.Fatal(string(pb))
	}
}

// A check-in is delivered under every goal decision newer than its
// instructions or the goal's digest, so a standing watch never acts on
// instructions a later decision overturned. Older decisions (the digest has them) and evidence logs stay out.
func TestCheckinCarriesDecisionsNewerThanItsInstructions(t *testing.T) {
	m, _, _ := setup(t)
	gs, err := goals.New(m.db)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gs.Create(goals.Goal{Title: "Card paid"})
	if err != nil {
		t.Fatal(err)
	}
	gs.AddNote(g.ID, "owner", "decision", "OLD: pay the card by hand")
	if _, err := gs.Update(g.ID, map[string]string{"digest": "Card paid by hand."}); err != nil {
		t.Fatal(err)
	}
	th, err := m.Create("", "life", g.ID, "Watch the card.", "every@6h", "remind Alex to pay the card", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, EndSentinel)
	gs.AddNote(g.ID, "claude:thread:x", "decision", "AutoPay is live on the card since 08-29")
	gs.AddNote(g.ID, "claude:thread:x", "evidence", "daily log: nothing moved")
	m.DuePrompts(time.Now().Add(7 * time.Hour))
	pb, _ := os.ReadFile(promptFile(t, m, th.ID))
	p := string(pb)
	if !strings.Contains(p, "[CHANGED SINCE THESE INSTRUCTIONS") || !strings.Contains(p, "AutoPay is live") {
		t.Fatal(p)
	}
	if strings.Contains(p, "OLD: pay the card") || strings.Contains(p, "nothing moved") {
		t.Fatalf("carried a note the digest or a log already covers:\n%s", p)
	}
	if strings.Index(p, "AutoPay is live") > strings.Index(p, "remind Alex to pay the card") {
		t.Fatal("the newer decisions must come before the stale instructions")
	}
	// The thread's stored message shows what the session was told.
	msgs, _ := m.Messages(th.ID, 20)
	seen := false
	for _, x := range msgs {
		seen = seen || strings.Contains(x.Text, "AutoPay is live")
	}
	if !seen {
		t.Fatal("stored check-in message lacks the block")
	}
}

// A refused wake (budget) is a failed child + a system line, and the standing
// row still advances — the same wake is not retried every minute.
func TestStandingRefusedByBudget(t *testing.T) {
	m, _, cmds := setup(t)
	th, _ := m.Create("", "life", "", "Watch.", "every@6h", "", nil)
	complete(t, m, th.ID, "ok")
	m.Allow = func(kind, id string) (bool, string) { return false, "over budget" }
	standing, _ := m.Standing(th.ID)
	n := len(*cmds)
	at := time.Now().Add(7 * time.Hour)
	m.DuePrompts(at)
	if len(*cmds) != n {
		t.Fatal("ran while refused")
	}
	failed, _ := m.ListPrompts("failed", "", 10)
	if len(failed) != 1 || failed[0].Parent != standing.ID || !strings.Contains(failed[0].Error, "over budget") {
		t.Fatalf("%+v", failed)
	}
	msgs, _ := m.Messages(th.ID, 20)
	if !strings.Contains(msgs[len(msgs)-1].Text, "Check-in skipped by the hub: over budget") {
		t.Fatalf("%+v", msgs[len(msgs)-1])
	}
	if again, _ := m.Standing(th.ID); !again.NotBefore.After(at) {
		t.Fatal("did not advance")
	}
}

// Schedule off — by PATCH or by cancelling the standing row — clears the
// schedule, posts the card, and the session settles as done, not idle.
func TestScheduleOffIsTheStandingRowCancelled(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Watch.", "daily@09:00", "", nil)
	complete(t, m, th.ID, EndSentinel)
	if th, _ = m.Get(th.ID); th.Status != "idle" {
		t.Fatal(th.Status)
	}
	standing, _ := m.Standing(th.ID)
	if _, err := m.CancelPrompt(standing.ID); err != nil {
		t.Fatal(err)
	}
	th, _ = m.Get(th.ID)
	if th.Schedule != "" || th.NextRunAt != nil {
		t.Fatalf("%+v", th)
	}
	msgs, _ := m.Messages(th.ID, 20)
	if last := msgs[len(msgs)-1]; last.Kind != "schedule" || !strings.Contains(last.Text, "cleared") {
		t.Fatalf("%+v", last)
	}
	if _, ok := m.Standing(th.ID); ok {
		t.Fatal("still standing")
	}
	// Back on via PATCH, then off via PATCH: one row at a time.
	m.Update(th.ID, map[string]string{"schedule": "daily@09:00"})
	m.Update(th.ID, map[string]string{"schedule": "every@2h"})
	if ps, _ := m.ListPrompts("queued", "", 10); len(ps) != 1 || ps[0].Repeat != "every@2h" {
		t.Fatalf("%+v", ps)
	}
	if _, err := m.Update(th.ID, map[string]string{"schedule": "hourly"}); err == nil {
		t.Fatal("bad cadence accepted")
	}
	if _, err := m.Update(th.ID, map[string]string{"schedule": "off"}); err != nil {
		t.Fatal(err)
	}
	if ps, _ := m.ListPrompts("queued", "", 10); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
	m.Send(th.ID, "hi")
	complete(t, m, th.ID, EndSentinel)
	if th, _ = m.Get(th.ID); th.Status != "done" {
		t.Fatal(th.Status)
	}
}

// A job's standing row (target job:<name>) fires through RunJob; what the
// scheduler answers decides the child: started → delivered, skipped →
// failed, held → nothing (asked again), gone → the standing row is cancelled.
func TestJobStandingRowFiresThroughRunJob(t *testing.T) {
	m, _, _ := setup(t)
	if _, err := m.Queue(Prompt{Author: "owner", Target: "job:sweep", Repeat: "every@1h", Text: "x"}); err == nil {
		t.Fatal("a job target must be the hub's")
	}
	if _, err := m.Queue(Prompt{Author: "hub", Target: "sweep", Repeat: "every2d"}); err == nil {
		t.Fatal("a stride cadence is not a clock")
	}
	if err := m.SetStanding("hub", "job:sweep", "every@1h", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	standing, ok := m.Standing("job:sweep")
	if !ok {
		t.Fatal("no standing row")
	}
	if r, ok := m.StandingRepeat("job:sweep"); !ok || r != "every@1h" {
		t.Fatal(r, ok)
	}
	if ts := m.StandingTargets("job:"); len(ts) != 1 || ts[0] != "job:sweep" {
		t.Fatal(ts)
	}
	var calls []string
	answer := "held"
	m.RunJob = func(name, promptID string) (string, string) {
		calls = append(calls, name+":"+promptID)
		return answer, "because"
	}
	at := time.Now().Add(time.Hour)
	m.DuePrompts(at)
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "sweep:p-") {
		t.Fatal(calls)
	}
	if s, _ := m.Standing("job:sweep"); !s.NotBefore.Equal(standing.NotBefore) {
		t.Fatal("held wake advanced")
	}
	if all, _ := m.ListPrompts("", "", 10); len(all) != 1 {
		t.Fatalf("held wake wrote a child: %+v", all)
	}
	answer = "skipped"
	m.DuePrompts(at)
	failed, _ := m.ListPrompts("failed", "", 10)
	if len(failed) != 1 || failed[0].Parent != standing.ID || failed[0].ID != strings.TrimPrefix(calls[1], "sweep:") {
		t.Fatalf("%+v", failed)
	}
	s, _ := m.Standing("job:sweep")
	if !s.NotBefore.After(at) {
		t.Fatal("did not advance")
	}
	answer = "started"
	m.DuePrompts(s.NotBefore)
	if ds, _ := m.ListPrompts("delivered", "", 10); len(ds) != 1 || ds[0].Parent != standing.ID || ds[0].ID != strings.TrimPrefix(calls[2], "sweep:") {
		t.Fatalf("%+v", ds)
	}
	answer = "gone"
	s, _ = m.Standing("job:sweep")
	m.DuePrompts(s.NotBefore)
	if _, ok := m.Standing("job:sweep"); ok {
		t.Fatal("gone job kept its row")
	}
	// Reconcile off: no row.
	m.SetStanding("hub", "job:other", "daily@07:00", "", time.Time{})
	m.SetStanding("hub", "job:other", "", "", time.Time{})
	if ts := m.StandingTargets("job:"); len(ts) != 0 {
		t.Fatal(ts)
	}
}

// An occurrence is claimed before it fires: a second pass holding the same
// (now stale) copy of the row starts nothing — advancing after the start with
// its error dropped could fire one twice.
func TestStandingOccurrenceFiresOnce(t *testing.T) {
	m, _, _ := setup(t)
	if err := m.SetStanding("hub", "job:sweep", "every@1h", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	stale, _ := m.Standing("job:sweep")
	calls := 0
	m.RunJob = func(name, promptID string) (string, string) { calls++; return "started", "" }
	at := time.Now().Add(time.Hour)
	m.fireStanding(stale, at)
	m.fireStanding(stale, at)
	if calls != 1 {
		t.Fatalf("one occurrence started %d jobs", calls)
	}
	if s, _ := m.Standing("job:sweep"); !s.NotBefore.After(at) {
		t.Fatal("did not advance")
	}
	// A job the scheduler says is held is not even claimed.
	s, _ := m.Standing("job:sweep")
	m.JobHeld = func(string) (bool, string) { return true, "gate pdfs is shut" }
	m.fireStanding(s, s.NotBefore.Add(time.Minute))
	if again, _ := m.Standing("job:sweep"); calls != 1 || !again.NotBefore.Equal(s.NotBefore) {
		t.Fatalf("held job: %d calls, %v → %v", calls, s.NotBefore, again.NotBefore)
	}
}

// The live DB predates the row: the migration that adds prompts.repeat moves
// every scheduled session's columns into a standing row, once.
func TestBackfillStandingRunsOnce(t *testing.T) {
	dir := t.TempDir()
	db, _ := store.Open(dir + "/t.db")
	// A pre-one-clock DB: prompts without repeat, a scheduled thread.
	if _, err := db.Migrate("threads", Schema); err != nil {
		t.Fatal(err)
	}
	old := []string{}
	for _, s := range PromptsSchema {
		if s == stmtPromptRepeat {
			break
		}
		old = append(old, s)
	}
	if _, err := db.Migrate("prompts", old); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO threads (id,created_at,updated_at,title,project,status,schedule,schedule_prompt,last_run_at) VALUES ('w','2026-08-01T00:00:00Z','2026-08-01T00:00:00Z','W','life','idle','every@6h','look','2026-08-27T12:00:00Z')`)
	db.Exec(`INSERT INTO threads (id,created_at,updated_at,title,project,status,schedule,schedule_prompt) VALUES ('gone','2026-08-01T00:00:00Z','2026-08-01T00:00:00Z','G','life','archived','daily@09:00','')`)
	db.Close()
	m, _, _ := setupAt(t, dir)
	ps, _ := m.ListPrompts("queued", "", 10)
	if len(ps) != 1 || ps[0].Target != "w" || ps[0].Repeat != "every@6h" || ps[0].Text != "look" {
		t.Fatalf("%+v", ps)
	}
	if !ps[0].NotBefore.Equal(time.Date(2026, 8, 27, 18, 0, 0, 0, time.UTC)) {
		t.Fatal("every@ should count from last_run_at:", ps[0].NotBefore)
	}
	th, _ := m.Get("w")
	if th.Schedule != "every@6h" || th.SchedulePrompt != "look" {
		t.Fatalf("%+v", th)
	}
	// A second boot does not add a second row.
	m2, _, _ := setupAt(t, dir)
	if ps, _ := m2.ListPrompts("queued", "", 10); len(ps) != 1 {
		t.Fatalf("%+v", ps)
	}
}

// One query lists every future wake the hub holds, whatever its kind.
func TestOneQueryListsEveryWake(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("", "life", "", "Watch.", "daily@09:00", "", nil)
	complete(t, m, th.ID, "ok")
	if _, err := m.Queue(Prompt{Author: "claude:thread:" + th.ID, Target: th.ID, Text: "check back", NotBefore: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	m.SetStanding("hub", "job:sweep", "every@2h", "", time.Time{})
	ps, _ := m.ListPrompts("queued", "", 10)
	kinds := map[string]bool{}
	for _, p := range ps {
		switch {
		case p.Target == "job:sweep" && p.Repeat == "every@2h":
			kinds["job"] = true
		case p.Target == th.ID && p.Repeat == "daily@09:00":
			kinds["session"] = true
		case p.Target == th.ID && p.Repeat == "" && p.Author != "hub":
			kinds["self"] = true
		}
	}
	if len(ps) != 3 || len(kinds) != 3 {
		t.Fatalf("%v %+v", kinds, ps)
	}
}
