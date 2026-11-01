package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
