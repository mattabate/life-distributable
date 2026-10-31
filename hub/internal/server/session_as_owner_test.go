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
