package server

import (
	"encoding/json"
	"testing"
	"time"

	"life/hub/internal/attention"
	"life/hub/internal/calendar"
	"life/hub/internal/recs"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

// The board and the agenda read everything waiting on the owner through one list
// (store.Items, review step 8). This pins that the switch changed nothing
// either surface is sent: the board served over Items is byte for byte the
// board built the old way (ListAsks + List("proposed") + Stats + the DueCount
// query), and Items holds exactly the four tables' open halves.
func TestItemsIsTheBoardsOldRead(t *testing.T) {
	s, db := newTestDB(t)
	w := s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "Split my brokerage cash sensibly"})
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	open, err := s.thr.AddAskOn(th.ID, "", "Move $2k to Broker B?", "", "decision", "", "any")
	if err != nil {
		t.Fatal(err)
	}
	read, _ := s.thr.AddAskOn(th.ID, "", "The statement is in", "", "read", "", "any")
	gone, _ := s.thr.AddAskOn(th.ID, "", "Old question", "", "decision", "", "any")
	if _, err := s.thr.ResolveAsk(gone.ID, "dismissed", "owner", ""); err != nil {
		t.Fatal(err)
	}
	if w := s.do(t, "POST", "/api/v1/actions", map[string]string{"kind": "money", "title": "move $5", "exec_type": "none"}); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var recIDs []string
	for _, title := range []string{"Cancel the thing", "Try the desk", "Buy the book"} {
		r, err := s.Recs.Add(recs.Rec{Title: title, Domain: "money", Kind: "stop"})
		if err != nil {
			t.Fatal(err)
		}
		recIDs = append(recIDs, r.ID)
	}
	if _, err := s.Recs.Defer(recIDs[1], time.Now().AddDate(0, 0, 7).Format("2006-01-02"), "owner", "after payday"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recs.Decide(recIDs[2], "declined", "owner", "no"); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	for _, it := range []calendar.Item{
		{Title: "Water the plants", Day: today, Kind: "owner"},
		{Title: "Book the dentist", Kind: "owner", Soon: true},
		{Title: "Wake the session", Day: today, Kind: "agent", ThreadID: th.ID},
	} {
		if _, err := s.Cal.Add(it); err != nil {
			t.Fatal(err)
		}
	}

	// The old read, verbatim.
	asks, _ := s.thr.ListAsks("active", "", 500)
	acts, _ := s.acts.List("proposed", 500)
	ths, _ := s.thr.ListBrief()
	for _, surface := range []string{"web", "mobile"} {
		want := attention.Build(surface, asks, acts, ths)
		var due int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE src='cal' AND kind IN ('owner','homework') AND state IN ('scheduled','open') AND day<>'' AND day<=?`, today).Scan(&due)
		want.Badges.Calendar = due
		st, _ := s.Recs.Stats()
		want.Badges.Recs = st.Proposed
		wantJSON, _ := json.Marshal(want)

		w := s.do(t, "GET", "/api/v1/board?surface="+surface, nil)
		var got attention.Board
		json.Unmarshal(w.Body.Bytes(), &got)
		gotJSON, _ := json.Marshal(got)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("%s board moved:\n got %s\nwant %s", surface, gotJSON, wantJSON)
		}
		if got.Badges.Recs != 1 || got.Badges.Calendar != 1 || got.Count != 3 {
			t.Fatalf("%s badges/count: %+v count %d", surface, got.Badges, got.Count)
		}
	}

	rows, err := store.Items(s.thr, s.acts, s.Recs, s.Cal)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	ids := map[string]bool{}
	for _, r := range rows {
		kinds[r.Kind]++
		ids[r.ID] = true
	}
	// Two live asks (the dismissed one is not), one proposal, the proposed and
	// the deferred rec (not the declined), the owner's two open steps (not the agent's).
	if kinds["ask"] != 2 || kinds["action"] != 1 || kinds["rec"] != 2 || kinds["owner"] != 2 || len(kinds) != 4 {
		t.Fatalf("items by kind: %v", kinds)
	}
	if !ids[open.ID] || !ids[read.ID] || ids[gone.ID] || ids[recIDs[2]] {
		t.Fatalf("items: %v", ids)
	}
	if n := len(store.ItemsOf(rows, "rec")); n != 2 {
		t.Fatalf("ItemsOf rec: %d", n)
	}
}
