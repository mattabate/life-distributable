// Gated actions: propose, list, approve/deny.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"life/hub/internal/actions"
)

// decider: the second credential approve/deny needs, kept beside hub.token as
// a SHA-256 only. See actions/decider.go — the hub token belongs to every
// session, so on its own it must not be able to approve anything.
func (s *Server) decider() *actions.Decider {
	if s.cfg == nil || s.cfg.TokenFile == "" {
		return actions.NewDecider("")
	}
	return actions.NewDecider(filepath.Join(filepath.Dir(s.cfg.TokenFile), "decider.hash"))
}

// deciderStatus answers "is the code this surface holds the right one?" before
// an approval rides on it, instead of after. A phone with a WRONG code
// saved in the Keychain re-sends it silently on every approve, and without
// this nothing on either surface could say the stored value was bad.
//
// This is a guessing oracle by construction, which is only safe because the
// secret is 100 bits (actions.NewSecret). Never point it at a short code
// without a lockout — a session holding the hub token can call it in a loop.
func (s *Server) deciderStatus(w http.ResponseWriter, r *http.Request) {
	d := s.decider()
	code := strings.TrimSpace(r.Header.Get("X-Life-Decider"))
	writeJSON(w, 200, map[string]bool{
		"armed": d.Armed(),
		"sent":  code != "",
		// Unarmed: approve/deny take the hub token alone, so whatever this
		// surface holds is good enough. Say so rather than reporting a
		// failure the owner cannot act on.
		"ok": !d.Armed() || d.Verify(code),
	})
}

func (s *Server) listActions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	as, err := s.acts.ListFor(r.URL.Query().Get("state"), r.URL.Query().Get("thread"), limit)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, as)
}

func (s *Server) proposeAction(w http.ResponseWriter, r *http.Request) {
	var p actions.Proposal
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		jsonErr(w, 400, "bad json: "+err.Error())
		return
	}
	a, err := s.acts.Propose(p)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) getAction(w http.ResponseWriter, r *http.Request) {
	a, err := s.acts.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such action")
		return
	}
	// The audit trail rides along on the one-action read only: the list and
	// the board keep the bare Action.
	events, err := s.acts.Events(a.ID)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, struct {
		actions.Action
		Events []actions.Event `json:"events"`
	}{a, events})
}

// moveAction is Dismiss (to `dismissed`) and its undo, Reopen (to
// `proposed`): the silent close every card has (2026-09-18). No decider —
// nothing runs and nobody is told; the code guards only what can run.
func (s *Server) moveAction(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		via := r.URL.Query().Get("via")
		if via == "" {
			via = "app"
		}
		move := s.acts.Dismiss
		if to == "proposed" {
			move = s.acts.Reopen
		}
		a, err := move(r.PathValue("id"), via)
		if err != nil {
			jsonErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, a)
	}
}

// deciderOK is the gate on deciding a proposal: with a decider secret armed,
// the request must carry it in X-Life-Decider or it is refused (403, and the
// refusal is on the action's record). The same gate guards a prompt that
// carries an approve/deny (addPrompt), since the card arms the composer.
func (s *Server) deciderOK(w http.ResponseWriter, r *http.Request, id, what, via string) bool {
	d := s.decider()
	if !d.Armed() {
		return true
	}
	code := strings.TrimSpace(r.Header.Get("X-Life-Decider"))
	if d.Verify(code) {
		return true
	}
	s.acts.Refused(id, via)
	log.Printf("actions: refused %s of %s via %s — %s decider code",
		what, id, via, map[bool]string{true: "missing", false: "wrong"}[code == ""])
	jsonErr(w, 403, "this needs your decider code, which the hub token does not carry: "+
		"the phone sends it automatically (Settings → Decider code), the console asks you for it once. "+
		"Set or reset it with ops/decider-set.sh in a Terminal.")
	return false
}

func (s *Server) decideAction(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		via := r.URL.Query().Get("via")
		if via == "" {
			via = "app"
		}
		// Deciding is the one thing the hub token cannot do by itself.
		if !s.deciderOK(w, r, r.PathValue("id"), map[bool]string{true: "approve", false: "deny"}[approve], via) {
			return
		}
		// Optional body {message}: the owner's note to the proposing session.
		var body struct {
			Message string `json:"message"`
		}
		if r.Body != nil {
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
		}
		a, err := s.acts.Decide(r.PathValue("id"), approve, via, body.Message)
		if err != nil {
			jsonErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, a)
	}
}
