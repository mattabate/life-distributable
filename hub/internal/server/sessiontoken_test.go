package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"life/hub/internal/threads"
)

func TestSessionFromToken(t *testing.T) {
	tok := SessionToken("hub", "a.b-1")
	if got := sessionFromToken("hub", tok); got != "a.b-1" {
		t.Fatalf("%q", got)
	}
	for _, bad := range []string{"", "hub", "s1.", "s1.a", "s1.a.b-1.00", tok + "0", SessionToken("other", "a.b-1"),
		strings.Replace(tok, "a.b-1", "other", 1)} {
		if got := sessionFromToken("hub", bad); got != "" {
			t.Errorf("%q gave %q", bad, got)
		}
	}
}

// With a session's own token the header is not its word to give: leaving it
// off or naming the target changes nothing, and the hub token is still the
// owner.
func TestSessionTokenCannotCallAsOwner(t *testing.T) {
	s := newTest(t)
	mk := func(p string) string {
		var th threads.Thread
		json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": p}).Body.Bytes(), &th)
		return th.ID
	}
	a, b := mk("first"), mk("second")
	post := func(token, header, path, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if header != "" {
			r.Header.Set("X-Life-Thread", header)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	tok := SessionToken("secret", a)
	msg := "/api/v1/threads/" + b + "/messages"
	prompt := `{"author":"owner","target":"` + b + `","text":"do it","in":"1h"}`

	s.cfg.SessionTokens = false
	if got := post(tok, "", msg, `{"text":"x"}`); got != 401 {
		t.Fatalf("off: a session token must not authenticate, got %d", got)
	}
	s.cfg.SessionTokens = true
	for _, c := range []struct {
		name, header, path, body string
		want                     int
	}{
		{"send, no header", "", msg, `{"text":"x"}`, 403},
		{"send, naming the target", b, msg, `{"text":"x"}`, 403},
		{"prompt, no header", "", "/api/v1/prompts", prompt, 400},
		{"prompt, naming the target", b, "/api/v1/prompts", prompt, 400},
		{"prompt itself", "", "/api/v1/prompts", `{"target":"` + a + `","text":"later","in":"1h"}`, 201},
	} {
		if got := post(tok, c.header, c.path, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	if got := post("secret", "", "/api/v1/prompts", prompt); got != 201 {
		t.Errorf("hub token: %d", got)
	}
	if got := post("s1."+a+".00", "", msg, `{"text":"x"}`); got != 401 {
		t.Errorf("forged: %d", got)
	}

	// The console opens with a session token (ops/hub.sh url in a clone), and
	// its cookie is that session's: it still cannot write into another one.
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/?token="+tok, nil))
	cookies := w.Result().Cookies()
	if w.Code != 303 || len(cookies) != 1 || cookies[0].Value != tok {
		t.Fatalf("console with a session token: %d %v", w.Code, cookies)
	}
	r := httptest.NewRequest("POST", msg, strings.NewReader(`{"text":"x"}`))
	r.AddCookie(cookies[0])
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("session cookie wrote into another session: %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/?token=s1."+a+".00", nil))
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("forged token set a cookie")
	}
}

// A scheduled job calls with a token for "job-<name>" (sched.JobToken): it
// starts no session, installs nothing and approves nothing, and still closes
// an ask, which is what the ask verifier does.
func TestJobTokenIsASessionNotTheOwner(t *testing.T) {
	s := newTest(t)
	s.cfg.SessionTokens = true
	tok := SessionToken("secret", "job-ask-verifier")
	call := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/threads", `{"prompt":"an owner session"}`},
		{"POST", "/api/v1/app/install", `{}`},
		{"POST", "/api/v1/actions/x/approve", `{}`},
	} {
		if got := call(c.method, c.path, c.body); got != 403 {
			t.Errorf("job %s %s: %d, want 403", c.method, c.path, got)
		}
	}
	if got := call("POST", "/api/v1/asks/ask-none/resolve", `{"state":"done","note":"verifier: x"}`); got == 401 || got == 403 {
		t.Errorf("job ask close: %d", got)
	}
}

// The routes where a session's own token would land as the owner's words,
// clicks or devices.
func TestSessionTokenCannotSpeakForOwner(t *testing.T) {
	s := newTest(t)
	var th threads.Thread
	json.Unmarshal(s.do(t, "POST", "/api/v1/threads", map[string]string{"prompt": "first"}).Body.Bytes(), &th)
	s.cfg.SessionTokens = true
	tok := SessionToken("secret", th.ID)
	post := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/threads/" + th.ID + "/messages", `{"text":"as if the owner typed it"}`},
		{"POST", "/api/v1/devices", `{"token":"x"}`},
		{"POST", "/api/v1/app/install", `{"ask":"x"}`},
		{"POST", "/api/v1/app/mac", `{"build":999}`},
		{"POST", "/api/v1/recs/x/reply", `{"note":"x","deliver":"source"}`},
		{"POST", "/api/v1/recs/x/decide", `{"status":"accepted","deliver":"source"}`},
		{"POST", "/api/v1/calendar/x/resolve", `{"outcome":"nudge"}`},
		{"POST", "/api/v1/observations", `{"source":"app","kind":"speech","payload":{"text":"x"}}`},
		{"POST", "/api/v1/observations/batch", `{"source":"app","items":[{"kind":"speech"}]}`},
	} {
		if got := post(c.method, c.path, c.body); got != 403 {
			t.Errorf("%s %s: %d, want 403", c.method, c.path, got)
		}
	}
	// Its own kinds of observation still go in.
	if got := post("POST", "/api/v1/observations", `{"source":"claude","kind":"note","payload":{}}`); got >= 300 {
		t.Errorf("a session's own observation: %d", got)
	}
	// A sub-session of its own is fine; claiming the New session box does not
	// send it past the router as the owner's message.
	r := httptest.NewRequest("POST", "/api/v1/threads", strings.NewReader(`{"prompt":"hi","via":"console"}`))
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 201 || strings.Contains(w.Body.String(), `"routed"`) {
		t.Errorf("via console from a session: %d %s", w.Code, w.Body.String())
	}
}
