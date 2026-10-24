package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"life/hub/internal/calendar"
)

// Away over the wire: POST /away, GET /away lists it, a step filed on one of
// its days answers `away`, one off it does not, and /end takes it off.
func TestAwayOverTheWire(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := do("POST", "/api/v1/away", `{"title":"Trip","from":"2030-06-07","until":"2030-06-13"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	id := strings.SplitN(strings.SplitN(w.Body.String(), `"id":"`, 2)[1], `"`, 2)[0]
	if w := do("GET", "/api/v1/away", ""); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/calendar", `{"title":"Go to the fair","day":"2030-06-09","kind":"owner"}`); w.Code != 201 || !strings.Contains(w.Body.String(), `"away":"The owner is away on 2030-06-09: Trip 2030-06-07..2030-06-13`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/calendar", `{"title":"Unpack","day":"2030-06-14","kind":"owner"}`); w.Code != 201 || strings.Contains(w.Body.String(), `"away"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/away/"+id+"/end", `{"note":"moved"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"ended"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/away/"+id+"/end", `{}`); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The merge verb over the wire: 400 without `into`, 200 with the keeper, and
// a session's near-duplicate add is refused until it says why.
func TestCalendarMergeAndNearDuplicate(t *testing.T) {
	s := newTest(t)
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	// Days far ahead: an agent item dated today fires on Add and a fired item
	// cannot merge, so a fixture near today goes stale.
	keep, _ := s.Cal.Add(calendar.Item{Title: "Autopay check", Day: "2030-10-20", Kind: "owner"})
	add := `{"title":"autopay check","day":"2030-10-21","kind":"agent","source":"claude:thread:x"%s}`
	if w := do("POST", "/api/v1/calendar", strings.Replace(add, "%s", "", 1)); w.Code != 400 || !strings.Contains(w.Body.String(), "near-duplicate of "+keep.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	w := do("POST", "/api/v1/calendar", strings.Replace(add, "%s", `,"distinct":"the agent half"`, 1))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	id := strings.SplitN(strings.SplitN(w.Body.String(), `"id":"`, 2)[1], `"`, 2)[0]
	if w := do("POST", "/api/v1/calendar/"+id+"/merge", `{}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do("POST", "/api/v1/calendar/"+id+"/merge", `{"into":"`+keep.ID+`"}`); w.Code != 200 || !strings.Contains(w.Body.String(), "MERGED") {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := s.Cal.Get(id); got.State != "dismissed" {
		t.Fatalf("merged item still %s", got.State)
	}
}

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
