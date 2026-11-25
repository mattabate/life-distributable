package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A session token proves which session is calling: the hub's HMAC over the
// session's id, nothing stored, so a new hub token retires them all. The runner
// hands one to every session (LIFE_SESSION_TOKEN) and the scheduler one to
// every job ("job-<name>"); lifectl and ops/hublib.py prefer it to hub.token.
// On unless hub.json has "session_tokens": false.
const sessionTokenPrefix = "s1."

// SessionToken is the token the runner hands the session with this id.
func SessionToken(hubToken, threadID string) string {
	return sessionTokenPrefix + threadID + "." + sessionTokenMAC(hubToken, threadID)
}

func sessionTokenMAC(hubToken, threadID string) string {
	h := hmac.New(sha256.New, []byte(hubToken))
	h.Write([]byte("life-session:" + threadID))
	return hex.EncodeToString(h.Sum(nil))
}

// auth accepts "Authorization: Bearer <token>" or the "life_token" cookie
// (set by the dashboard once from a ?code=, login.go). Constant-time compare.
// It lives here, in a gatePaths file, not in server.go: a landing that changes
// who the hub takes for the owner is the owner's card.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	got := s.bearer(r)
	if got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1 {
		return true
	}
	// A session token: the caller IS that session, whatever header it sent, so
	// every check on X-Life-Thread holds against it.
	if id := s.sessionFromRequest(r); id != "" {
		r.Header.Set("X-Life-Thread", id)
		return true
	}
	return false
}

// A browser token is the console's cookie: the cookie used to hold the hub
// token itself for a year, so a copied cookie was the master token until a
// rotation. It is now the hub's HMAC over whom it opens the console as
// ("" = the owner, else a session id), its expiry and hub.json's
// browser_gen: never the hub token, dead after browserTTL unless the console
// is opened again (index renews it), and raising browser_gen signs every
// browser out without a new hub token.
const (
	browserTokenPrefix = "b1."
	browserTTL         = 30 * 24 * time.Hour
)

func BrowserToken(hubToken string, gen int, who string, exp time.Time) string {
	e := strconv.FormatInt(exp.Unix(), 10)
	return browserTokenPrefix + e + "." + who + "." + browserMAC(hubToken, gen, e, who)
}

func browserMAC(hubToken string, gen int, exp, who string) string {
	h := hmac.New(sha256.New, []byte(hubToken))
	h.Write([]byte("life-browser:" + strconv.Itoa(gen) + ":" + exp + ":" + who))
	return hex.EncodeToString(h.Sum(nil))
}

// browserWho is whom a browser token opens the console as, and whether it
// holds at now.
func browserWho(hubToken string, gen int, tok string, now time.Time) (string, bool) {
	rest, ok := strings.CutPrefix(tok, browserTokenPrefix)
	if !ok {
		return "", false
	}
	e, rest, ok := strings.Cut(rest, ".")
	i := strings.LastIndexByte(rest, '.')
	if !ok || i < 0 {
		return "", false
	}
	who, mac := rest[:i], rest[i+1:]
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || now.Unix() >= exp || !hmac.Equal([]byte(mac), []byte(browserMAC(hubToken, gen, e, who))) {
		return "", false
	}
	return who, true
}

// bearer is the token a request carries: its Authorization header, or the
// one its cookie stands for (the hub token for the owner's, a session's
// token for a session's). A cookie holding a raw token opens nothing here;
// index swaps an old one for a browser token (legacyCookieUntil).
func (s *Server) bearer(r *http.Request) string {
	if got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); got != "" {
		return got
	}
	c, err := r.Cookie("life_token")
	if err != nil {
		return ""
	}
	who, ok := browserWho(s.token, s.cfg.BrowserGen, c.Value, time.Now())
	switch {
	case !ok:
		return ""
	case who == "":
		return s.token
	}
	return SessionToken(s.token, who)
}

// browserCookie is the console's cookie for a token a login code carried:
// a browser token for whom it proves, or nil for one that proves no one.
func (s *Server) browserCookie(tok string, now time.Time) *http.Cookie {
	who := ""
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		if who = sessionFromToken(s.token, tok); who == "" || !s.cfg.SessionTokens {
			return nil
		}
	}
	return &http.Cookie{Name: "life_token", Value: BrowserToken(s.token, s.cfg.BrowserGen, who, now.Add(browserTTL)),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(browserTTL.Seconds())}
}

// legacyCookieUntil: till then, opening the console with a cookie from
// before browser tokens (the raw hub token) swaps it for one, so an
// upgraded hub's browsers need no new login link; after it such a cookie
// opens nothing.
var legacyCookieUntil = time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)

// renewCookie: on a console load, the cookie for the token it stands for,
// with a fresh expiry; nil when the request did not sign in by cookie.
func (s *Server) renewCookie(r *http.Request, now time.Time) *http.Cookie {
	if r.Header.Get("Authorization") != "" {
		return nil
	}
	c, err := r.Cookie("life_token")
	if err != nil {
		return nil
	}
	if now.Before(legacyCookieUntil) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1 {
		return s.browserCookie(s.token, now)
	}
	if tok := s.bearer(r); tok != "" {
		return s.browserCookie(tok, now)
	}
	return nil
}

// sessionFromToken is the session a token proves, or "".
func sessionFromToken(hubToken, tok string) string {
	rest, ok := strings.CutPrefix(tok, sessionTokenPrefix)
	if !ok {
		return ""
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 || !hmac.Equal([]byte(rest[i+1:]), []byte(sessionTokenMAC(hubToken, rest[:i]))) {
		return ""
	}
	return rest[:i]
}
