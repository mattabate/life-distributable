package server

import (
	"net/http"
	"strings"
)

// Who a request is from. lifectl sends X-Life-Thread from a session
// ($LIFE_THREAD_ID); the owner's surfaces and their shell send none. The
// header is a claim, not a proof, but an honest one is held to: a session
// that names itself never acts as the owner, or as another session, whatever
// its body says (#76: the asks, workspace and actions routes took the body's
// `by`/`thread_id`, so one session could close another's card in the owner's
// voice, overwrite another role's files, or file a proposal as another
// session, to be carried out with that session's grants).

// caller is the session a request comes from, or "" for the owner.
func (s *Server) caller(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Life-Thread"))
}

// actor is who a write is by: the calling session, else the body's claim,
// else the owner — the one default every `by?=owner` route uses.
func (s *Server) actor(r *http.Request, by string) string {
	if id := s.caller(r); id != "" {
		return "claude:thread:" + id
	}
	if by == "" {
		return "owner"
	}
	return by
}

// ownerOnly answers 403 when a session asks for something only the owner's
// surfaces send (a field, not a whole route), and says so.
func (s *Server) ownerOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if s.caller(r) == "" {
		return false
	}
	jsonErr(w, 403, "only the owner does this: "+what)
	return true
}
