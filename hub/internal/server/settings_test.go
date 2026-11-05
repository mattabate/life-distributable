package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"life/hub/internal/spend"
	"life/hub/internal/threads"
)

// The owner's switches: read, written into ops/hub.json with every other key
// kept, and live at once.
func TestSettings(t *testing.T) {
	s := newTest(t)
	s.UsePicker(spend.NewPicker(nil, nil, nil))
	os.MkdirAll(s.cfg.OpsPath(), 0o755)
	hub := s.cfg.OpsPath("hub.json")
	os.WriteFile(hub, []byte("{\n  \"listen_addr\": \"127.0.0.1:0\",\n  \"surfaces\": [\"web\"],\n  \"team_mode\": true\n}\n"), 0o600)

	var got settingsOut
	w := s.do(t, "GET", "/api/v1/settings", nil)
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || !got.TeamMode || !got.PlainSessions || got.ModelCap != "" || len(got.Models) == 0 {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}

	cap := got.Models[1]
	w = s.do(t, "PATCH", "/api/v1/settings", map[string]any{"plain_sessions": false, "model_cap": cap})
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.PlainSessions || got.ModelCap != cap {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body)
	}
	if s.thr.PlainSessions() || s.picker.Cap() != cap {
		t.Fatal("not applied live")
	}
	var file map[string]any
	b, _ := os.ReadFile(hub)
	if err := json.Unmarshal(b, &file); err != nil || file["listen_addr"] != "127.0.0.1:0" || file["plain_sessions"] != false || file["model_cap"] != cap || file["team_mode"] != true {
		t.Fatalf("hub.json: %v %s", err, b)
	}
	if st, _ := os.Stat(hub); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if !strings.HasPrefix(string(b), "{\n  \"listen_addr\"") {
		t.Fatalf("keys kept in place: %s", b)
	}
	// A model above the cap is not the default either.
	if w := s.do(t, "PUT", "/api/v1/spend/model", map[string]string{"default_model": got.Models[0]}); w.Code != 400 {
		t.Fatalf("above the cap: %d %s", w.Code, w.Body)
	}

	for _, bad := range []map[string]any{{"session_tokens": false}, {"model_cap": "gpt-9"}, {}} {
		if w := s.do(t, "PATCH", "/api/v1/settings", bad); w.Code != 400 {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body)
		}
	}

	// Off, roles go; on again, the chief of staff holds the calendar.
	s.do(t, "PATCH", "/api/v1/settings", map[string]any{"team_mode": false})
	if s.thr.TeamMode() {
		t.Fatal("team mode still on")
	}
	if w := s.do(t, "GET", "/api/v1/personas", nil); w.Code != 404 {
		t.Fatalf("personas with team mode off: %d", w.Code)
	}
	s.do(t, "PATCH", "/api/v1/settings", map[string]any{"team_mode": true, "model_cap": ""})
	if th, err := s.thr.Get(threads.StepThread); err != nil || th.Persona != threads.PersonaStaff || s.picker.Cap() != "" {
		t.Fatalf("team on: %+v %v cap %q", th, err, s.picker.Cap())
	}
}

// Roles are added and rewritten by the owner or the chief of staff, and by
// no other session.
func TestPersonasRoutes(t *testing.T) {
	s := newTest(t)
	s.cfg.SessionTokens = true
	s.thr.StartTeam()
	call := func(session, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		tok := s.token
		if session != "" {
			tok = SessionToken(s.token, session)
		}
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	var list []threads.Persona
	w := call("", "GET", "/api/v1/personas", "")
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list) != 1 || list[0].Key != threads.PersonaStaff || list[0].Session != threads.StepThread {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	other, _ := s.thr.Create("Other", "life", "", "x", "", "", nil)
	body := `{"key":"coach","role":"Fitness coach","who":"the owner's coach","brief":"Keep the plan.","team":"Health"}`
	if w := call(other.ID, "POST", "/api/v1/personas", body); w.Code != 403 {
		t.Fatalf("another session made a role: %d %s", w.Code, w.Body)
	}
	var p threads.Persona
	w = call(threads.StepThread, "POST", "/api/v1/personas", body)
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 201 || p.Key != "coach" || p.Session == "" {
		t.Fatalf("the chief of staff's new role: %d %s", w.Code, w.Body)
	}
	if w := call("", "POST", "/api/v1/personas", body); w.Code != 400 {
		t.Fatalf("a taken key: %d %s", w.Code, w.Body)
	}
	w = call("", "PATCH", "/api/v1/personas/coach", `{"team":"Body"}`)
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || p.Team != "Body" {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	if w := call(other.ID, "PATCH", "/api/v1/personas/coach", `{"team":"X"}`); w.Code != 403 {
		t.Fatalf("another session patched: %d", w.Code)
	}
	if w := call("", "PATCH", "/api/v1/personas/coach", `{"name":"X"}`); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
	if w := call(other.ID, "PATCH", "/api/v1/settings", `{"team_mode":false}`); w.Code != 403 {
		t.Fatalf("a session flipped a setting: %d", w.Code)
	}
}
