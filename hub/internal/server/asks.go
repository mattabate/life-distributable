// Asks: list, add, get, resolve, retry, surface.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"life/hub/internal/threads"
)

// Asks: what agents need from the owner, as rows (docs/ASKS.md).
func (s *Server) listAsks(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	as, err := s.thr.ListAsks(r.URL.Query().Get("state"), r.URL.Query().Get("thread"), limit)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, as)
}

func (s *Server) addAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThreadID string `json:"thread_id"`
		RunID    string `json:"run_id"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
		Kind     string `json:"kind"`
		Check    string `json:"check"`
		// Surface: where it gets done — mobile | web | any; "" infers it from
		// the text (threads/surface.go).
		Surface string `json:"surface"`
		// Say: the sentence the push speaks instead of the title (notify/voice.go).
		Say string `json:"say"`
		// Replaces: this thread's open cards the new one folds in, closed as it
		// lands, so one topic stays one card.
		Replaces []string `json:"replaces"`
	}
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	// A card is its session's: the owner's Done on it wakes that session with
	// "Done: <title>" in the owner's name, so a session raises cards on itself
	// only.
	if from := s.caller(r); from != "" {
		if in.ThreadID != "" && in.ThreadID != from {
			jsonErr(w, 403, "a session raises cards on itself, not on another session; to hand a task over: `lifectl relay <session-id> \"...\"`")
			return
		}
		in.ThreadID = from
	}
	// A card raised from inside a turn belongs IN that turn's tool chain, and
	// the only thing that ties it there is the run it was raised in. lifectl
	// reads that from LIFE_RUN_ID, which the runner puts in the claude
	// process's environment — so a process that was already running when that
	// env was added (or any lane that does not set it) posts run_id:"" and its
	// card lands nowhere: no segment cuts the chain, and finishTurn links
	// message_id by run_id, so it never gets one either. The hub already knows which run of this thread is in
	// flight, so it fills the gap itself.
	if in.RunID == "" {
		in.RunID = s.thr.LiveRun(in.ThreadID)
	}
	// Checked before the card exists, so a wrong id raises nothing: only an
	// open card on this same thread can be folded in.
	for _, id := range in.Replaces {
		old, err := s.thr.GetAsk(id)
		if err != nil || old.ThreadID != in.ThreadID || (old.State != "open" && old.State != "answered") {
			jsonErr(w, 400, "replaces: "+id+" is not an open card on this session")
			return
		}
	}
	a, err := s.thr.AddAskSaid(in.ThreadID, in.RunID, in.Title, in.Detail, in.Kind, in.Check, in.Surface, in.Say)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	for _, id := range in.Replaces {
		s.thr.ResolveAsk(id, "dismissed", s.actor(r, "agent"), "folded into "+a.ID)
	}
	writeJSON(w, 201, struct {
		threads.Ask
		Note string `json:"note,omitempty"`
	}{a, s.readPile(a)})
}

// readPile: the session's other open read cards, said back to it the moment
// it raises one more, so it folds by topic while it still holds the words:
// several read cards on one hand-off, one correcting another, read as noise.
// A nudge, not a cap: several topics are several cards.
func (s *Server) readPile(a threads.Ask) string {
	if a.Class != "read" {
		return ""
	}
	open, _ := s.thr.ListAsks("active", a.ThreadID, 0)
	var b strings.Builder
	n := 0
	for _, o := range open {
		if o.ID == a.ID || o.Class != "read" {
			continue
		}
		n++
		fmt.Fprintf(&b, "; %s %q", o.ID, o.Title)
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("You now hold %d more open read card(s)%s. Same topic, or one correcting another: fold them now (`lifectl ask %s set --title … --say …` with the whole story, then `lifectl ask <old> dismiss \"folded into %s\"`). A card only echoing another session's is not yours to raise: dismiss it.", n, b.String(), a.ID, a.ID)
}

func (s *Server) getAsk(w http.ResponseWriter, r *http.Request) {
	a, err := s.thr.GetAsk(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such ask")
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) resolveAsk(w http.ResponseWriter, r *http.Request) {
	var in struct{ State, By, Note string }
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	in.By = s.actor(r, in.By)
	a, err := s.thr.ResolveAsk(r.PathValue("id"), in.State, in.By, in.Note)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// setAskSurface: move a card between surfaces — "web" (done at the Mac, quiet
// on the phone), "mobile", "any". The escape hatch for a bad inference.
func (s *Server) setAskSurface(w http.ResponseWriter, r *http.Request) {
	var in struct{ Surface string }
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	a, err := s.thr.SetAskSurface(r.PathValue("id"), in.Surface)
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// setAskKind: retag a card's answer vocabulary — the escape hatch for an ask
// filed under the wrong kind (a do-these-steps card offering Granted).
func (s *Server) setAskKind(w http.ResponseWriter, r *http.Request) {
	var in struct{ Kind string }
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	a, err := s.thr.SetAskKind(r.PathValue("id"), in.Kind)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// rewordAsk: an open card's words rewritten in place (threads.RewordAsk) —
// `lifectl ask <id> set --title … --say … --detail …`. Quiet, unless it sat
// above the newest message: then it moves below it and speaks again.
func (s *Server) rewordAsk(w http.ResponseWriter, r *http.Request) {
	var in struct{ Title, Detail, Say, By string }
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	in.By = s.actor(r, in.By)
	a, err := s.thr.RewordAsk(r.PathValue("id"), in.Title, in.Detail, in.Say, in.By)
	if err != nil {
		code := 409
		if err.Error() == "no such ask" {
			code = 404
		}
		jsonErr(w, code, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// retryAsk: the Restart button on an error card. Replays the turn the session
// died on and closes the card (threads.RetryAsk).
func (s *Server) retryAsk(w http.ResponseWriter, r *http.Request) {
	t, err := s.thr.RetryAsk(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

// No per-ask web page any more — a card is read in the chat on either
// surface: GET /a/{token}/{id}, the `url`
// every ask carried and the Open page buttons on both surfaces are gone.
