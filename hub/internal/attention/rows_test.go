package attention

import (
	"testing"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/recs"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

// Phase 5: the board and the agenda read ONE row shape, and each package
// projects its own objects into it. These pin the row contract — if a
// projection stops carrying what a reader decides from, the board silently
// changes its mind about what is waiting, which is the class of bug this
// refactor exists to make impossible.
func TestRowsCarryWhatTheReadersDecideFrom(t *testing.T) {
	ask := threads.Ask{ID: "a1", ThreadID: "t1", ThreadTitle: "Gold exit", Kind: "physical", Surface: "web",
		State: "open", CalID: "cal-1", CalDay: "2026-08-26", Title: "Buy the tranche", GoalID: "g1"}
	r := threads.Row(ask)
	if r.Kind != "ask" || r.AskKind != "physical" || r.Ref != "ask:a1" || r.AskID != "a1" {
		t.Fatalf("ask row identity: %+v", r)
	}
	// Surface, CalID and the day are exactly the fields the board's two
	// surfaces disagreed on before the hub owned this.
	if r.Surface != "web" || r.CalID != "cal-1" || r.Day != "2026-08-26" {
		t.Fatalf("ask row lost a board field: %+v", r)
	}
	if r.ThreadTitle != "Gold exit" || r.GoalID != "g1" || r.Actor != "claude:thread:t1" {
		t.Fatalf("ask row provenance: %+v", r)
	}
	if got, ok := r.Obj.(threads.Ask); !ok || got.ID != "a1" {
		t.Fatalf("ask row must carry its object: %+v", r.Obj)
	}

	// An action's day is the day it was DECIDED, else the day it was proposed;
	// its actor is whoever decided it, else whoever proposed it.
	made := time.Date(2026, 8, 20, 9, 0, 0, 0, time.Local)
	done := time.Date(2026, 8, 26, 21, 30, 0, 0, time.Local)
	x := actions.Row(actions.Action{ID: "x1", ThreadID: "t1", State: "proposed", Title: "Delete the stray binary",
		CreatedAt: made, Source: "claude:thread:t1", RunID: "run-7"})
	if x.Kind != "action" || x.Ref != "action:x1" || x.Day != store.Day(made) || x.Actor != "claude:thread:t1" || x.RunID != "run-7" {
		t.Fatalf("proposed action row: %+v", x)
	}
	x = actions.Row(actions.Action{ID: "x1", State: "approved", CreatedAt: made, DecidedAt: &done, DecidedVia: "app", Source: "claude:job:daily-sweep"})
	if x.Day != store.Day(done) || x.Actor != "app" || x.Source != "claude:job:daily-sweep" {
		t.Fatalf("decided action row: %+v", x)
	}

	// A rec's day is the day the owner asked to be asked again, and it belongs
	// to no surface in particular: recs are pulled, never pushed.
	q := recs.Row(recs.Rec{ID: "rec-1", Title: "Buy staples", Status: "deferred", ReviewOn: "2026-10-15", Source: "claude:thread:t2"})
	if q.Kind != "rec" || q.Ref != "rec:rec-1" || q.Day != "2026-10-15" || q.Surface != "" {
		t.Fatalf("rec row: %+v", q)
	}
}

// The board is the ask/action window over the read model: everything else the
// agenda carries is dated, not waiting. A deferred rec, a scheduled item and a
// projected run must not add a single number to "your turn".
func TestBuildRowsIgnoresWhatIsNotWaiting(t *testing.T) {
	ths := []threads.Thread{{ID: "t1", Title: "Gold exit", Status: "idle"}}
	asks := []threads.Ask{{ID: "a1", ThreadID: "t1", Surface: "any", State: "open", Kind: "decision"}}
	acts := []actions.Action{{ID: "x1", ThreadID: "t1", State: "proposed"}}

	want := Build("web", asks, acts, ths)
	rows := []store.Dated{
		actions.Row(acts[0]),
		threads.Row(asks[0]),
		recs.Row(recs.Rec{ID: "rec-1", Status: "deferred", ReviewOn: "2026-10-15"}),
		{ID: "cal-1", Kind: "owner", Ref: "cal:cal-1", Day: "2026-08-27", State: "scheduled", Title: "Buy tranche 3"},
		{ID: "t1", Kind: "run", Ref: "thread:t1", Day: "2026-08-27", State: "scheduled", Title: "Gold exit"},
		{ID: "daily-sweep", Kind: "job", Ref: "job:daily-sweep", Day: "2026-08-27", State: "scheduled"},
	}
	got := BuildRows("web", rows, ths)
	if got.Count != want.Count || got.Count != 2 || got.Badges.YourTurn != 2 {
		t.Fatalf("dated-but-not-waiting rows changed the count: %d (want %d)", got.Count, want.Count)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].N != 2 || got.Sessions[0].First != "x1" {
		t.Fatalf("sessions %+v", got.Sessions)
	}
	// The payload is still whole objects: the clients decode Asks and Actions,
	// and the row is a hub-side read model they never see.
	if len(got.Sessions[0].Asks) != 1 || got.Sessions[0].Asks[0].Kind != "decision" {
		t.Fatalf("ask object did not survive the row: %+v", got.Sessions[0].Asks)
	}
	if len(got.Sessions[0].Actions) != 1 || got.Sessions[0].Actions[0].ID != "x1" {
		t.Fatalf("action object did not survive the row: %+v", got.Sessions[0].Actions)
	}
}
