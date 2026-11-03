package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"life/hub/internal/threads"
)

func TestSessionTokenCannotActAsOwner(t *testing.T) {
	s := newTest(t)
	s.cfg.SessionTokens = true
	tok := SessionToken(s.token, "th-1")
	call := func(bearer, method, path, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/actions/x/approve"},
		{"POST", "/api/v1/actions/x/deny"},
		{"POST", "/api/v1/actions/x/dismiss"},
		{"POST", "/api/v1/actions/x/reopen"},
		{"POST", "/api/v1/spend/budget/clear"},
		{"PUT", "/api/v1/spend/model"},
		{"PUT", "/api/v1/usage"},
	} {
		if code, body := call(tok, c.method, c.path, "{}"); code != 403 {
			t.Errorf("%s %s: %d %s", c.method, c.path, code, body)
		}
	}
	// The hub token still reaches them: a 403 here would be the limit leaking.
	if code, body := call(s.token, "POST", "/api/v1/actions/x/dismiss", "{}"); code == 403 {
		t.Fatalf("hub token refused: %s", body)
	}

	// Every spelling of a claim, and none at all, comes out as the session.
	for _, body := range []string{`{}`, `{"by":"owner"}`, `{"BY":"owner"}`, `{"By":"owner","Author":"owner"}`, ``} {
		var got map[string]string
		h := s.sessionLimits("POST /x", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &got)
		})
		r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tok)
		h(httptest.NewRecorder(), r)
		if got["by"] != "claude:thread:th-1" || got["author"] != "claude:thread:th-1" || len(got) != 2 {
			t.Errorf("%q: %v", body, got)
		}
	}

	var th threads.Thread
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "mine"}).Body.Bytes(), &th)
	mine := SessionToken(s.token, th.ID)

	// A proposal is filed under the session its token proves, whatever the
	// body or the header claims.
	r := httptest.NewRequest("POST", "/api/v1/actions", strings.NewReader(`{"kind":"other","title":"move it","exec_type":"none","source":"owner"}`))
	r.Header.Set("Authorization", "Bearer "+mine)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var act map[string]any
	json.Unmarshal(w.Body.Bytes(), &act)
	if w.Code != 201 || act["thread_id"] != th.ID || act["source"] != "claude:thread:"+th.ID {
		t.Fatalf("a session's proposal: %d %s", w.Code, w.Body.String())
	}

	// A free-field PATCH from a session still works: the stamp is dropped
	// before the updater sees the keys.
	if code, body := call(mine, "PATCH", "/api/v1/threads/"+th.ID, `{"schedule":"weekly@Sun 03:00","schedule_prompt":"pass"}`); code != 200 {
		t.Fatalf("thread patch as a session: %d %s", code, body)
	}
	if got, _ := s.thr.Get(th.ID); got.Schedule != "weekly@Sun 03:00" || got.SchedulePrompt != "pass" {
		t.Fatalf("schedule not set: %+v", got)
	}
	var goal struct {
		ID string `json:"id"`
	}
	json.Unmarshal(s.do(t, "POST", "/api/v1/goals", map[string]any{"title": "Get smarter", "statement": "a thing a week",
		"horizon": "year", "cadence": "weekly"}).Body.Bytes(), &goal)
	if code, body := call(mine, "PATCH", "/api/v1/goals/"+goal.ID, `{"digest":"on track"}`); code != 200 {
		t.Fatalf("goal patch as a session: %d %s", code, body)
	}
	if code, body := call(mine, "PATCH", "/api/v1/threads/"+th.ID, `{"author":"owner","nope":"x"}`); code != 400 || !strings.Contains(body, "nope") {
		t.Fatalf("an unknown key is still refused by name: %d %s", code, body)
	}

	// Off, nothing changes.
	s.cfg.SessionTokens = false
	r = httptest.NewRequest("POST", "/x", strings.NewReader(`{"by":"owner"}`))
	r.Header.Set("Authorization", "Bearer "+tok)
	var seen string
	s.sessionLimits("POST /api/v1/actions/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
	})(httptest.NewRecorder(), r)
	if seen != `{"by":"owner"}` {
		t.Fatalf("off: %q", seen)
	}
}

// A session's upload is stored as sent: a .json blob or a notes file that
// is a JSON object came back with by/author written into it, and JSON null
// panicked on the stamp.
func TestSessionRawBodiesAreStoredAsSent(t *testing.T) {
	s := newTest(t)
	th, _ := s.thr.Create("Notes", "life", "", "Notes.", "", "", nil)
	s.thr.Update(th.ID, map[string]string{"persona": threads.PersonaAssistant})
	s.cfg.SessionTokens = true
	call := func(method, path, ctype, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+SessionToken(s.token, th.ID))
		r.Header.Set("Content-Type", ctype)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	doc := "{\n  \"value\": 42\n}\n"
	w := call("POST", "/api/v1/blobs?ext=json", "application/octet-stream", doc)
	var got struct{ Ref string }
	json.Unmarshal(w.Body.Bytes(), &got)
	p, err := s.obs.BlobPath(got.Ref)
	if b, _ := os.ReadFile(p); w.Code != 201 || err != nil || string(b) != doc {
		t.Fatalf("blob: %d %v %q", w.Code, err, b)
	}
	if w := call("PUT", "/api/v1/workspace/assistant/notes.md", "text/plain", doc); w.Code != 200 {
		t.Fatalf("workspace: %d %s", w.Code, w.Body)
	}
	if b, _ := s.WS.Read(threads.PersonaAssistant, "notes.md"); string(b) != doc {
		t.Fatalf("workspace stored %q", b)
	}
	if w := call("POST", "/api/v1/goals", "application/json", "null"); w.Code != 400 {
		t.Fatalf("null: %d %s", w.Code, w.Body)
	}
}
