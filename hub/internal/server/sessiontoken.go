package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
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
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie("life_token"); err == nil {
			got = c.Value
		}
	}
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
