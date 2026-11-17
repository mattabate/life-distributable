package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A console write sent again under the same X-Life-Request runs once; the
// repeat gets the first answer (a retried POST could send a chat message
// twice). Another name, another caller or no name at all runs anew.
func TestRepeatedWriteRunsOnce(t *testing.T) {
	c := &replayCache{seen: map[string]*replayed{}}
	var runs atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := runs.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"n":` + string(rune('0'+n)) + `}`))
	})
	send := func(id, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/threads/x/messages", strings.NewReader(`{"text":"hi"}`))
		if id != "" {
			r.Header.Set(replayHeader, id)
		}
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		c.serveOnce(w, r, h)
		return w
	}
	first := send("a1", "Bearer t")
	again := send("a1", "Bearer t")
	if runs.Load() != 1 {
		t.Fatalf("the repeat ran the handler: %d runs", runs.Load())
	}
	if again.Code != http.StatusCreated || again.Body.String() != first.Body.String() || again.Header().Get("X-Life-Replayed") != "1" {
		t.Fatalf("repeat answered %d %q, first %q", again.Code, again.Body.String(), first.Body.String())
	}
	send("a2", "Bearer t")
	send("a1", "Bearer other")
	send("", "Bearer t")
	send("", "Bearer t")
	if runs.Load() != 5 {
		t.Fatalf("want 5 runs (new name, other caller, two unnamed), got %d", runs.Load())
	}
	r := httptest.NewRequest("GET", "/api/v1/board", nil)
	r.Header.Set(replayHeader, "a1")
	if replayKey(r) != "" {
		t.Fatal("a GET is never held back")
	}
}
