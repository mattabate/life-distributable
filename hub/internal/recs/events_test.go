package recs

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

// A rec's decisions are a trail, not an overwrite (review-primitives step 4):
// accept, put back and decline leave three lines in item_events, and the
// score is a fourth — the row keeps only the latest, as before.
func TestDecisionsAppendToTheTrail(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	r, err := s.Add(Rec{Title: "Try a standing desk", Domain: "health", Kind: "try", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct{ status, note string }{{"accepted", "yes"}, {"proposed", ""}, {"declined", "too big for the flat"}} {
		if _, err := s.Decide(r.ID, d.status, "app", d.note); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Score(r.ID, "unclear", "", "never tried"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`SELECT actor, from_state, to_state, note FROM item_events WHERE item_id=? ORDER BY id`, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var by, from, to, note string
		rows.Scan(&by, &from, &to, &note)
		got = append(got, by+":"+from+">"+to+":"+note)
	}
	want := "owner:>proposed: | app:proposed>accepted:yes | app:accepted>proposed: | app:proposed>declined:too big for the flat | hub:declined>declined:scored unclear: never tried"
	if strings.Join(got, " | ") != want {
		t.Fatalf("trail\n got %s\nwant %s", strings.Join(got, " | "), want)
	}
	if cur, _ := s.Get(r.ID); cur.Status != "declined" || cur.DecisionNote != "too big for the flat" {
		t.Fatalf("row: %+v", cur)
	}
}

// A rec filed before the merge (step 9) reads back the same from items: its
// decision and trail moved, its ledger stayed, and it waits in the recs lane,
// soon — never now.
func TestPreMergeRecMovesToItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate("recs", Schema[:len(Schema)-1]); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO recs (id,created_at,updated_at,title,domain,kind,cost_cents,cost_period,status,decided_at,decided_by,decision_note,review_on,outcome,model)
		VALUES ('rec-old','2026-09-01T12:00:00Z','2026-09-02T12:00:00Z','Cancel the gym','money','stop',4000,'monthly','accepted','2026-09-02T12:00:00Z','app','yes','2026-10-02','','claude-opus-5')`)
	db.Exec(`INSERT INTO rec_events (rec_id,ts,actor,from_state,to_state,note) VALUES ('rec-old','2026-09-02T12:00:00Z','app','proposed','accepted','yes')`)
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Get("rec-old")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "accepted" || r.DecidedBy != "app" || r.DecisionNote != "yes" || r.ReviewOn != "2026-10-02" || r.MonthlyCents() != 4000 || r.Model != "claude-opus-5" {
		t.Fatalf("moved rec: %+v", r)
	}
	if r.Window != "soon" || r.Lane != "recs" {
		t.Fatalf("a rec is pulled, soon: window %q lane %q", r.Window, r.Lane)
	}
	var verb, trail string
	db.QueryRow(`SELECT verb FROM items WHERE id='rec-old'`).Scan(&verb)
	db.QueryRow(`SELECT from_state||'>'||to_state||':'||note FROM item_events WHERE item_id='rec-old'`).Scan(&trail)
	if verb != "accept" || trail != "proposed>accepted:yes" {
		t.Fatalf("verb %q trail %q", verb, trail)
	}
}
