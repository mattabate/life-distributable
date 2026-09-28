package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"life/hub/internal/calendar"
)

// The owner closing their own step over the wire is a message, not a button:
// no note → 409 and nothing written; with one → 200, the item
// closes with their words on it, and exactly one prompt carrying them is
// queued for its session with `cal:<id>` + done. Reopen needs no words.
func TestCalendarResolveByOwnerNeedsWords(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	tr, err := s.thr.Create("Groceries", "life", "", "p", "", "restock", nil)
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.Cal.Add(calendar.Item{Title: "Warehouse run", Day: "2026-09-03", Kind: "owner", ThreadID: tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if w := do("POST", "/api/v1/calendar/"+it.ID+"/resolve", `{"state":"done"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "say what happened") {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := s.Cal.Get(it.ID); got.State != "scheduled" {
		t.Fatalf("refused close still wrote: %+v", got)
	}
	w := do("POST", "/api/v1/calendar/"+it.ID+"/resolve", `{"state":"done","note":"Went Saturday"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"done"`) || !strings.Contains(w.Body.String(), `"resolution":"Went Saturday"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ps, _ := s.thr.ListPrompts("", tr.ID, 10)
	var n int
	for _, p := range ps {
		if p.Author == "owner" && p.InReplyTo == "cal:"+it.ID && p.Outcome == "done" && p.Text == "Went Saturday" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one prompt with the owner's words: %+v", ps)
	}
	if w := do("POST", "/api/v1/calendar/"+it.ID+"/resolve", `{"state":"scheduled"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"scheduled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// The same words posted as a prompt straight at the reference close the
	// item too — the phone's Respond sheet takes this road.
	if w := do("POST", "/api/v1/prompts", `{"target":"new-or:`+tr.ID+`","in_reply_to":"cal:`+it.ID+`","outcome":"wont","text":"Skipping this month"}`); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := s.Cal.Get(it.ID); got.State != "dismissed" || got.Resolution != "Skipping this month" || got.ResolvedBy != "owner" {
		t.Fatalf("prompt did not close the item: %+v", got)
	}
	// An agent closing it needs no words.
	ag, _ := s.Cal.Add(calendar.Item{Title: "Renew parking", Day: "2026-09-03", Kind: "owner", ThreadID: tr.ID})
	if w := do("POST", "/api/v1/calendar/"+ag.ID+"/resolve", `{"state":"done","by":"claude:thread:`+tr.ID+`"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
