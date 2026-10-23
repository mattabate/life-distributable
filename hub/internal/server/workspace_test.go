package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A role's files over the API: the owner and the role's own session write,
// anyone reads, by role key or session id; unknown roles and hidden names
// are refused; the whole thing is a 404 with team mode off.
func TestWorkspaceRoutes(t *testing.T) {
	s := newTest(t)
	th, err := s.thr.Create("Calendar keeper", "life", "", "p", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.thr.Update(th.ID, map[string]string{"persona": "assistant"}); err != nil {
		t.Fatal(err)
	}
	put := func(path, text, by string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"text": text, "by": by})
		req := httptest.NewRequest("PUT", path, strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}
	if w := put("/api/v1/workspace/assistant/appointments.md", "# Appointments\n\nDentist is Tuesday.", "owner"); w.Code != 200 {
		t.Fatalf("owner write: %d %s", w.Code, w.Body)
	}
	if w := put("/api/v1/workspace/assistant/appointments.md", "x", "claude:thread:someone-else"); w.Code != 403 {
		t.Fatalf("another session wrote: %d %s", w.Code, w.Body)
	}
	if w := put("/api/v1/workspace/assistant/appointments.md", "# Appointments\n\nDentist is Tuesday, haircut Friday.", "claude:thread:"+th.ID); w.Code != 200 {
		t.Fatalf("the role's own session: %d %s", w.Code, w.Body)
	}
	if w := put("/api/v1/workspace/butler/x.md", "x", "owner"); w.Code != 404 {
		t.Fatalf("unknown role: %d %s", w.Code, w.Body)
	}
	if w := put("/api/v1/workspace/assistant/.versions/x.md", "x", "owner"); w.Code != 400 {
		t.Fatalf("hidden name: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/api/v1/workspace/assistant/appointments.md", "/api/v1/workspace/" + th.ID + "/appointments.md"} {
		w := s.do(t, "GET", path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "haircut Friday") || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
			t.Fatalf("%s: %d %q %s", path, w.Code, w.Header().Get("Content-Type"), w.Body)
		}
	}
	if w := s.do(t, "GET", "/api/v1/workspace/assistant/nope.md", nil); w.Code != 404 {
		t.Fatalf("missing file: %d", w.Code)
	}
	w := s.do(t, "GET", "/api/v1/workspace?persona="+th.ID, nil)
	var fs []struct{ Persona, Name, Head string }
	if err := json.Unmarshal(w.Body.Bytes(), &fs); err != nil || len(fs) != 1 || fs[0].Persona != "assistant" || fs[0].Head != "Appointments" {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "GET", "/api/v1/workspace?persona=butler", nil); w.Code != 404 {
		t.Fatalf("list unknown role: %d", w.Code)
	}
	if w := s.do(t, "GET", "/api/v1/threads?persona=1", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"Executive assistant"`) {
		t.Fatalf("persona list: %d %s", w.Code, w.Body)
	}

	// The switch off: no roles, no workspace, no persona to set.
	s.thr.TeamMode = false
	if w := s.do(t, "GET", "/api/v1/workspace", nil); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "team mode is off") {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "GET", "/api/v1/threads?persona=1", nil); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("off list: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "PATCH", "/api/v1/threads/"+th.ID, map[string]string{"persona": "assistant"}); w.Code != 400 || !strings.Contains(w.Body.String(), "team mode is off") {
		t.Fatalf("off patch: %d %s", w.Code, w.Body)
	}
}
