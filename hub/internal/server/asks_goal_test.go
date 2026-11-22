package server

import (
	"encoding/json"
	"testing"

	"life/hub/internal/threads"
)

// A card takes its session's goal unless it names its own, and a card already
// filed wrong, open or closed, can be refiled.
func TestCardNamesItsOwnGoal(t *testing.T) {
	s := newTest(t)
	s.do(t, "POST", "/api/v1/goals", map[string]string{"title": "Read more", "statement": "s"})
	s.do(t, "POST", "/api/v1/goals", map[string]string{"title": "Get stronger", "statement": "s"})
	var th threads.Thread
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "books"}).Body.Bytes(), &th)
	if w := s.do(t, "PATCH", "/api/v1/threads/"+th.ID, map[string]string{"goal_id": "read-more"}); w.Code != 200 {
		t.Fatalf("session goal = %d %s", w.Code, w.Body)
	}
	add := func(body map[string]any) threads.Ask {
		t.Helper()
		body["thread_id"] = th.ID
		w := s.do(t, "POST", "/api/v1/asks", body)
		if w.Code != 201 {
			t.Fatalf("add = %d %s", w.Code, w.Body)
		}
		var a threads.Ask
		json.Unmarshal(w.Body.Bytes(), &a)
		return a
	}

	if a := add(map[string]any{"title": "Pick a book", "kind": "decision"}); a.GoalID != "read-more" {
		t.Fatalf("default goal = %q, want the session's", a.GoalID)
	}
	gym := add(map[string]any{"title": "Book a gym class", "kind": "physical", "goal": "get-stronger"})
	if gym.GoalID != "get-stronger" {
		t.Fatalf("named goal = %q", gym.GoalID)
	}
	if w := s.do(t, "POST", "/api/v1/asks", map[string]any{"thread_id": th.ID, "title": "x", "goal": "no-such-goal"}); w.Code != 400 {
		t.Fatalf("unknown goal on add = %d, want 400", w.Code)
	}

	// A closed card is refiled too: the goal is what it was about.
	old := add(map[string]any{"title": "Buy running shoes", "kind": "physical"})
	s.do(t, "POST", "/api/v1/asks/"+old.ID+"/resolve", map[string]string{"state": "done"})
	w := s.do(t, "POST", "/api/v1/asks/"+old.ID+"/goal", map[string]string{"goal": "get-stronger"})
	var got threads.Ask
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.GoalID != "get-stronger" || got.State != "done" {
		t.Fatalf("refile closed = %d %+v", w.Code, got)
	}
	if w := s.do(t, "POST", "/api/v1/asks/"+old.ID+"/goal", map[string]string{"goal": "none"}); w.Code != 200 {
		t.Fatalf("unfile = %d", w.Code)
	}
	if a, _ := s.thr.GetAsk(old.ID); a.GoalID != "" {
		t.Fatalf("unfiled goal = %q", a.GoalID)
	}
	if w := s.do(t, "POST", "/api/v1/asks/"+old.ID+"/goal", map[string]string{"goal": "nope"}); w.Code != 400 {
		t.Fatalf("unknown goal = %d, want 400", w.Code)
	}
	if w := s.do(t, "POST", "/api/v1/asks/ask-nope/goal", map[string]string{"goal": "get-stronger"}); w.Code != 404 {
		t.Fatalf("unknown card = %d, want 404", w.Code)
	}
	if w := s.do(t, "POST", "/api/v1/asks/"+old.ID+"/goal", map[string]string{"goal": ""}); w.Code != 400 {
		t.Fatalf("empty goal = %d, want 400", w.Code)
	}
	// A dated ask is a calendar item until its day: the same refusal there.
	if w := s.do(t, "POST", "/api/v1/calendar", map[string]any{"title": "Renew the pass", "day": "2030-01-01", "kind": "owner", "goal_id": "no-such-goal"}); w.Code != 400 {
		t.Fatalf("unknown goal on a dated ask = %d, want 400", w.Code)
	}
	if w := s.do(t, "POST", "/api/v1/calendar", map[string]any{"title": "Renew the pass", "day": "2030-01-01", "kind": "owner", "goal_id": "get-stronger"}); w.Code != 201 {
		t.Fatalf("dated ask under a goal = %d %s", w.Code, w.Body)
	}
}
