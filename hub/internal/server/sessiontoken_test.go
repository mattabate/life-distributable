package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// The console's cookie is never the hub token: a browser token for whom it
// opens, dead past its expiry, revoked all at once by browser_gen, and
// forged or re-pointed it proves no one.
func TestBrowserToken(t *testing.T) {
	now := time.Now()
	tok := BrowserToken("hub", 0, "a.b-1", now.Add(time.Hour))
	if who, ok := browserWho("hub", 0, tok, now); !ok || who != "a.b-1" {
		t.Fatalf("%q %v", who, ok)
	}
	if who, ok := browserWho("hub", 0, BrowserToken("hub", 0, "", now.Add(time.Hour)), now); !ok || who != "" {
		t.Fatalf("the owner's: %q %v", who, ok)
	}
	for name, bad := range map[string]string{
		"expired":     BrowserToken("hub", 0, "", now.Add(-time.Second)),
		"next gen":    tok,
		"other hub":   BrowserToken("other", 0, "a.b-1", now.Add(time.Hour)),
		"re-pointed":  strings.Replace(tok, ".a.b-1.", "..", 1),
		"later":       strings.Replace(tok, browserTokenPrefix, browserTokenPrefix+"9", 1),
		"raw token":   "hub",
		"session tok": SessionToken("hub", "a.b-1"),
		"no mac":      "b1.99999999999.",
	} {
		gen := 0
		if name == "next gen" {
			gen = 1
		}
		if who, ok := browserWho("hub", gen, bad, now); ok {
			t.Errorf("%s: opened as %q", name, who)
		}
	}
}

// A raw-token cookie opens no route; till legacyCookieUntil the console swaps
// an old one for a browser token on load, and each load renews the cookie.
func TestCookieIsABrowserToken(t *testing.T) {
	s := newTest(t)
	call := func(path, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "life_token", Value: cookie})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/v1/asks", "secret"); w.Code != 401 {
		t.Fatalf("a raw hub token cookie: %d", w.Code)
	}
	defer func(was time.Time) { legacyCookieUntil = was }(legacyCookieUntil)
	legacyCookieUntil = time.Now().Add(time.Hour)
	w := call("/", "secret")
	set := w.Result().Cookies()
	if w.Code != 200 || len(set) != 1 || !strings.HasPrefix(set[0].Value, browserTokenPrefix) {
		t.Fatalf("the old cookie was not swapped: %d %v", w.Code, set)
	}
	if w := call("/api/v1/asks", set[0].Value); w.Code != 200 {
		t.Fatalf("the browser token: %d", w.Code)
	}
	if w := call("/", set[0].Value); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("a load did not renew: %d", w.Code)
	}
	legacyCookieUntil = time.Now().Add(-time.Hour)
	if w := call("/", "secret"); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("an old cookie after the swap window: %d", w.Code)
	}
	old := set[0].Value
	s.cfg.BrowserGen++
	if w := call("/api/v1/asks", old); w.Code != 401 {
		t.Fatalf("browser_gen did not sign it out: %d", w.Code)
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
	s.ServeHTTP(w, httptest.NewRequest("GET", "/?code="+codeFor(t, s, tok), nil))
	cookies := w.Result().Cookies()
	if w.Code != 303 || len(cookies) != 1 || cookies[0].Value == tok || !strings.Contains(cookies[0].Value, "."+a+".") {
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
	r = httptest.NewRequest("POST", "/api/v1/login-codes", nil)
	r.Header.Set("Authorization", "Bearer s1."+a+".00")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("a forged token got a login code: %d", w.Code)
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
		{"POST", "/api/v1/mic", `{"on":true}`},
		{"GET", "/api/v1/mic", ``},
		{"POST", "/api/v1/voice", `{"thread_id":"x","secs":300}`},
		{"POST", "/api/v1/voice/hush", `{"card":"x"}`},
		{"GET", "/api/v1/voice/ear?device=mac&route=headphones", ``},
		{"POST", "/api/v1/voice/ear", `{"device":"mac","id":"x","outcome":"spoke"}`},
		{"POST", "/api/v1/voice/stop", `{"id":"x"}`},
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
