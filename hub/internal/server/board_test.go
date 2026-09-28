package server

import (
	"encoding/json"
	"testing"

	"life/hub/internal/attention"
	"life/hub/internal/threads"
)

// One board, two surfaces: both count everything and draw everything, a
// running session's cards included (a card is answerable the moment it is
// raised, and the answer steers the live turn). "Working" says
// how much of "your turn" came from a session that has not stopped.
func TestBoardBothSurfaces(t *testing.T) {
	s := newTest(t)
	w := s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "Split my brokerage cash sensibly"})
	var th threads.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	if th.Status != "running" {
		t.Fatal(w.Body.String())
	}
	ask, err := s.thr.AddAskOn(th.ID, "", "Move $2k to Broker B?", "", "decision", "", "any")
	if err != nil {
		t.Fatal(err)
	}
	if w := s.do(t, "POST", "/api/v1/actions", map[string]string{"kind": "money", "title": "move $5", "exec_type": "none"}); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	board := func(surface string) attention.Board {
		w := s.do(t, "GET", "/api/v1/board?surface="+surface, nil)
		if w.Code != 200 {
			t.Fatal(surface, w.Code, w.Body.String())
		}
		var b attention.Board
		json.Unmarshal(w.Body.Bytes(), &b)
		return b
	}
	web := board("web")
	if web.Surface != "web" || web.Count != 2 || web.Working != 1 || len(web.Sessions) != 2 || web.Badges.YourTurn != 2 {
		t.Fatalf("web: %+v", web)
	}
	// Approvals' sessions lead, then the asks' in first-ask order.
	if web.Sessions[0].Title != "Proposed by a scheduled job" || web.Sessions[0].First == "" {
		t.Fatalf("web sessions[0]: %+v", web.Sessions[0])
	}
	if got := web.Sessions[1]; got.ID != th.ID || len(got.Asks) != 1 || got.Asks[0].ID != ask.ID {
		t.Fatalf("web sessions[1]: %+v", got)
	}
	mob := board("mobile")
	if mob.Surface != "mobile" || mob.Count != 2 || mob.Working != 1 {
		t.Fatalf("mobile: %+v", mob)
	}
	// Same two bundles as the console; the running one sinks to the bottom.
	if len(mob.Sessions) != 2 || mob.Sessions[0].Title != "Proposed by a scheduled job" || mob.ForYou[th.ID] != 1 {
		t.Fatalf("mobile sessions: %+v for_you %v", mob.Sessions, mob.ForYou)
	}
	if got := mob.Sessions[1]; got.ID != th.ID || !got.Running || len(got.Asks) != 1 || got.Asks[0].ID != ask.ID {
		t.Fatalf("mobile sessions[1]: %+v", got)
	}
	if board("").Surface != "web" {
		t.Fatal("default surface must be web")
	}
}
