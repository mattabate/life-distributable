package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A browser signs in with a code, never the token: `/?token=` set the cookie
// but stayed in the browser's history, the redirect's Referer and anything
// that logs URLs (#161). A caller that already holds a token (hub.sh url, the
// phone's Open in browser) asks for a code, and the code is good for one
// visit inside a minute.

const loginCodeTTL = time.Minute

type loginCodes struct {
	mu sync.Mutex
	m  map[string]loginCode
}

type loginCode struct {
	token string // what the cookie becomes: the hub token or a session's
	until time.Time
}

func (l *loginCodes) mint(token string, now time.Time) string {
	b := make([]byte, 16)
	rand.Read(b)
	code := hex.EncodeToString(b)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]loginCode{}
	}
	for k, c := range l.m {
		if now.After(c.until) {
			delete(l.m, k)
		}
	}
	l.m[code] = loginCode{token, now.Add(loginCodeTTL)}
	return code
}

// take spends a code: its token, or "" for one unknown, used or expired.
func (l *loginCodes) take(code string, now time.Time) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.m[code]
	delete(l.m, code)
	if !ok || now.After(c.until) {
		return ""
	}
	return c.token
}

// postLoginCode: POST /api/v1/login-codes, a code for the token this request
// carries, so the cookie it sets proves no more than the caller already does.
func (s *Server) postLoginCode(w http.ResponseWriter, r *http.Request) {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie("life_token"); err == nil {
			got = c.Value
		}
	}
	writeJSON(w, 201, map[string]any{"code": s.logins.mint(got, time.Now()), "expires_in": int(loginCodeTTL.Seconds())})
}

// pageHeaders: what every console document carries (#158). No other site may
// frame it, it loads no plugin and no <base> moves its relative URLs; the
// browser takes no type it was not given, and a link out of it sends no
// Referer.
func pageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}
