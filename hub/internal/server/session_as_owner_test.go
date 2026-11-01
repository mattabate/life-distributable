package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"life/hub/internal/threads"
)

// A session calling with lifectl may not write into another session as the
// owner, by either route.
func TestSessionCannotWriteAsOwnerIntoAnother(t *testing.T) {
	s := newTest(t)
	mk := func(p string) string {
		var th threads.Thread
		json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": p}).Body.Bytes(), &th)
		return th.ID
	}
	a, b := mk("first"), mk("second")
	post := func(path, from, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", "application/json")
		if from != "" {
			r.Header.Set("X-Life-Thread", from)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		name, path, from, body string
		want                   int
	}{
		{"send to another", "/api/v1/threads/" + b + "/messages", a, `{"text":"do it"}`, 403},
		{"author owner", "/api/v1/prompts", a, `{"author":"owner","target":"` + b + `","text":"do it","in":"1h"}`, 400},
		{"no author", "/api/v1/prompts", a, `{"target":"` + b + `","text":"do it","in":"1h"}`, 400},
		{"prompt itself", "/api/v1/prompts", a, `{"author":"owner","target":"` + a + `","text":"later","in":"1h"}`, 201},
		{"owner, no header", "/api/v1/prompts", "", `{"target":"` + b + `","text":"his","in":"1h"}`, 201},
	} {
		if got := post(c.path, c.from, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// A session that names itself (lifectl's X-Life-Thread) is the actor on every
// route that writes as someone, whatever its body claims: it closes another
// session's card as itself, never as the owner (so B hears no "Done" in the
// owner's voice, and a card the owner closed stays theirs), it writes only its
// own role's files, and it files a proposal under itself alone (#76: all three
// took the body's word).
func TestSessionHeaderNamesTheActor(t *testing.T) {
	s := newTest(t)
	mk := func(p string) string {
		var th threads.Thread
		json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": p}).Body.Bytes(), &th)
		return th.ID
	}
	a, b := mk("first"), mk("second")
	call := func(method, path, from, ctype, body string) (int, map[string]any) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", ctype)
		if from != "" {
			r.Header.Set("X-Life-Thread", from)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	js := "application/json"
	asA := "claude:thread:" + a

	// 1. B's card, closed by A with no `by`: A's close, and B is not told "Done" as the owner.
	_, ask := call("POST", "/api/v1/asks", "", js, `{"thread_id":"`+b+`","title":"Pick a date","kind":"decision"}`)
	code, got := call("POST", "/api/v1/asks/"+ask["id"].(string)+"/resolve", a, js, `{"state":"done","note":"picked it"}`)
	if code != 200 || got["resolved_by"] != asA {
		t.Fatalf("A closing B's card: %d resolved_by=%v", code, got["resolved_by"])
	}
	ps, _ := s.thr.ListPrompts("", b, 50)
	for _, p := range ps {
		if p.Author == "owner" {
			t.Fatalf("B hears A's close in the owner's voice: %+v", p)
		}
	}
	// A card the owner closed stays theirs: A may not move it to the other close.
	_, ask = call("POST", "/api/v1/asks", "", js, `{"thread_id":"`+b+`","title":"Book the room","kind":"physical"}`)
	if code, _ := call("POST", "/api/v1/asks/"+ask["id"].(string)+"/resolve", "", js, `{"state":"done","note":"booked"}`); code != 200 {
		t.Fatalf("the owner's close: %d", code)
	}
	if code, _ := call("POST", "/api/v1/asks/"+ask["id"].(string)+"/resolve", a, js, `{"state":"dismissed","by":"owner"}`); code != 409 {
		t.Fatalf("A rewrote the owner's close: %d", code)
	}

	// 2. Another role's files: a JSON body claiming the owner, a raw body, a raw body with ?by=.
	keeper, _ := s.thr.Create("Calendar keeper", "life", "", "p", "", "", nil)
	s.thr.Update(keeper.ID, map[string]string{"persona": threads.PersonaAssistant})
	for _, c := range []struct{ path, ctype, body string }{
		{"/api/v1/workspace/assistant/appointments.md", js, `{"text":"# Mine now","by":"owner"}`},
		{"/api/v1/workspace/assistant/appointments.md", "text/markdown", "# Mine now"},
		{"/api/v1/workspace/assistant/appointments.md?by=owner", "text/markdown", "# Mine now"},
	} {
		if code, _ := call("PUT", c.path, a, c.ctype, c.body); code != 403 {
			t.Errorf("A wrote the assistant's file (%s %s): %d", c.ctype, c.body, code)
		}
	}
	if code, _ := call("PUT", "/api/v1/workspace/assistant/appointments.md", keeper.ID, "text/markdown", "# Appointments"); code != 200 {
		t.Fatalf("the role's own session refused: %d", code)
	}

	// 3. A proposal under B is refused; one with no thread is A's.
	if code, _ := call("POST", "/api/v1/actions", a, js, `{"kind":"other","title":"move it","exec_type":"none","thread_id":"`+b+`"}`); code != 403 {
		t.Fatalf("A filed a proposal as B: %d", code)
	}
	code, act := call("POST", "/api/v1/actions", a, js, `{"kind":"other","title":"move it","exec_type":"none","source":"owner"}`)
	if code != 201 || act["thread_id"] != a || act["source"] != asA {
		t.Fatalf("A's own proposal: %d thread_id=%v source=%v", code, act["thread_id"], act["source"])
	}

	// 4. A rec answered from a session: no delivery as the owner, the decision in its name.
	_, rec := call("POST", "/api/v1/recs", "", js, `{"title":"Try the API","domain":"tools","kind":"try"}`)
	id := rec["id"].(string)
	if code, _ := call("POST", "/api/v1/recs/"+id+"/reply", a, js, `{"note":"go","deliver":"source"}`); code != 403 {
		t.Fatalf("A's reply went as the owner's: %d", code)
	}
	if code, _ := call("POST", "/api/v1/recs/"+id+"/decide", a, js, `{"status":"declined","deliver":"source"}`); code != 403 {
		t.Fatalf("A's decision was delivered as the owner's: %d", code)
	}
	if code, got := call("POST", "/api/v1/recs/"+id+"/decide", a, js, `{"status":"declined","by":"owner"}`); code != 200 || got["decided_by"] != asA {
		t.Fatalf("A's decision: %d decided_by=%v", code, got["decided_by"])
	}

	// 5. The owner's step closed by A: an agent's close, in A's name.
	_, it := call("POST", "/api/v1/calendar", "", js, `{"title":"Water the plants","day":"2030-01-01","kind":"owner"}`)
	if code, got := call("POST", "/api/v1/calendar/"+it["id"].(string)+"/resolve", a, js, `{"state":"done"}`); code != 200 || got["resolved_by"] != asA {
		t.Fatalf("A closing the owner's step: %d resolved_by=%v", code, got["resolved_by"])
	}

	// 6. Observations under the owner's sources are refused on a session's
	// token only (TestSessionTokenCannotSpeakForOwner): with just the header,
	// a session still logs what the owner told it.
	if code, _ := call("POST", "/api/v1/observations", a, js, `{"source":"app","kind":"note","payload":{"count":3}}`); code != 201 {
		t.Fatalf("a session logging for the owner with the header: %d", code)
	}
}
