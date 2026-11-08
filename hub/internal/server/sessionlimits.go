package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"life/hub/internal/actions"
	"life/hub/internal/workspace"
)

// What a session token may not do, whatever the hub token may. Nothing here
// applies to the hub token, and all of it is off with "session_tokens": false.

// ownerOnlyRoutes: the owner's decisions, settings, words and devices. Each
// would land as the owner's: an approval, a message they typed, an Install
// click, a phone at build N. The app and the console send these; lifectl from
// a session needs none of them. Dismiss is not here: a session may withdraw
// its OWN open proposal (moveAction checks the thread), so a reworded card
// does not leave the old one on the owner's board. Nothing runs on a dismiss.
var ownerOnlyRoutes = map[string]bool{
	"POST /api/v1/actions/{id}/approve":  true,
	"POST /api/v1/actions/{id}/deny":     true,
	"POST /api/v1/actions/{id}/reopen":   true,
	"POST /api/v1/spend/budget/clear":    true,
	"PUT /api/v1/spend/model":            true,
	"PATCH /api/v1/settings":             true,
	"PUT /api/v1/usage":                  true,
	"POST /api/v1/threads/{id}/messages": true,
	"POST /api/v1/devices":               true,
	"POST /api/v1/app/install":           true,
	"POST /api/v1/app/mac":               true,
	"POST /api/v1/recs/{id}/reply":       true, // always delivers the note as the owner's
	// The owner's room: the microphone, the live take's words, and the floor a
	// reply holds while it plays, which every push waits out (#168: a session
	// re-marking any key kept the owner's cards up to 45 min late). The app and
	// the console are the only callers.
	"POST /api/v1/mic":        true,
	"GET /api/v1/mic":         true,
	"POST /api/v1/voice":      true,
	"POST /api/v1/voice/hush": true,
}

// ownerSources: observations only the owner's devices make. App speech moves
// the phone's audio route; health and phone rows are the owner's own.
var ownerSources = map[string]bool{"app": true, "health": true, "phone": true}

// ownerSource answers 403 when a session's token posts an observation under
// one of ownerSources. The token alone is refused here, not the header.
func (s *Server) ownerSource(w http.ResponseWriter, r *http.Request, source string) bool {
	if !ownerSources[strings.ToLower(strings.TrimSpace(source))] || s.sessionFromRequest(r) == "" {
		return false
	}
	jsonErr(w, 403, "only the owner does this: observations from "+source)
	return true
}

// sessionFromRequest is the session a request's token proves, or "".
func (s *Server) sessionFromRequest(r *http.Request) string {
	if !s.cfg.SessionTokens {
		return ""
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie("life_token"); err == nil {
			got = c.Value
		}
	}
	return sessionFromToken(s.token, got)
}

// sessionLimits refuses a session what is the owner's, and writes its name over
// every claim of who is acting: handlers default a missing by or author to
// "owner".
func (s *Server) sessionLimits(pattern string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := s.sessionFromRequest(r)
		if id == "" {
			h(w, r)
			return
		}
		if ownerOnlyRoutes[pattern] {
			jsonErr(w, 403, "only the owner does this")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && !rawBody(pattern, r) && !stampActor(w, r, "claude:thread:"+id, stampCap(pattern)) {
			return
		}
		h(w, r)
	}
}

// rawBody: the routes whose body is the stored bytes, not a request. A
// session's .json blob or a notes file that happened to be a JSON object came
// back with by/author written into it, and an empty upload became that
// object. Neither needs the stamp: a blob names nobody, and the workspace PUT
// writes as the caller whatever the body.
func rawBody(pattern string, r *http.Request) bool {
	switch pattern {
	case "POST /api/v1/blobs":
		return true
	case "PUT /api/v1/workspace/{persona}/{name...}":
		return !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
	}
	return false
}

// stampCaps: the routes whose handler takes more than maxJSON, at the
// handler's own cap. stampActor holds the whole body in memory before the
// handler runs, and read up to 96 MB on every route, so one session request
// could make the hub hold 96 MB where the handler would refuse past 4 (#159).
var stampCaps = map[string]int64{
	"POST /api/v1/land":                         64 << 20,
	"POST /api/v1/observations":                 65 << 20,
	"POST /api/v1/observations/batch":           32 << 20,
	"PUT /api/v1/workspace/{persona}/{name...}": workspace.MaxBytes + 1<<20,
}

func stampCap(pattern string) int64 {
	if n, ok := stampCaps[pattern]; ok {
		return n
	}
	return maxJSON
}

// stampActor rewrites a JSON object body. Any other body passes as it came: a
// handler that decodes JSON refuses it, and an upload names nobody.
func stampActor(w http.ResponseWriter, r *http.Request, who string, max int64) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		jsonErr(w, 413, "body too large")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	in := map[string]json.RawMessage{}
	if t := bytes.TrimSpace(raw); len(t) > 0 && json.Unmarshal(raw, &in) != nil {
		// An object Unmarshal refuses is still one a json.Decoder reads up to
		// its first value: `{"author":"owner"} {}` kept a forged author on
		// every decode() handler.
		if t[0] == '{' {
			jsonErr(w, 400, "expected one JSON object")
			return false
		}
		return true
	}
	if in == nil { // the body was JSON null, and in["by"] below would panic
		jsonErr(w, 400, "expected a JSON object")
		return false
	}
	// Go matches a JSON key to a field whatever its case.
	for k := range in {
		switch strings.ToLower(k) {
		case "by", "author", "actor":
			delete(in, k)
		}
	}
	q, _ := json.Marshal(who)
	in["by"], in["author"] = q, q
	out, _ := json.Marshal(in)
	r.Body = io.NopCloser(bytes.NewReader(out))
	r.ContentLength = int64(len(out))
	return true
}

// sessionProposal: with sessions as their own user (session_user), a shell
// command a session proposes to run anywhere but the main checkout always
// waits for the owner, however free the command: a folder that user can write
// (its clone) decides what the owner's user would run there.
func (s *Server) sessionProposal(p *actions.Proposal) {
	if s.cfg.SessionUser == "" || p.ExecType != "shell" {
		return
	}
	var pay struct {
		Dir string `json:"dir"`
	}
	json.Unmarshal(p.ExecPayload, &pay)
	if pay.Dir != "" && filepath.Clean(pay.Dir) != filepath.Clean(s.cfg.Root) {
		gated := true
		p.Gated = &gated
	}
}

// unstamped drops the by/author stampActor wrote into a free-field patch: the
// PATCH handlers for threads, goals and calendar items hand the whole body to
// an updater that refuses a key it does not know, and none of those updaters
// records an actor. Any other unknown key is still refused by name.
func unstamped(patch map[string]string) map[string]string {
	delete(patch, "by")
	delete(patch, "author")
	return patch
}
