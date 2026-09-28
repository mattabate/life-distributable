// Asks: list, add, get, resolve, retry, surface.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
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
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
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
	a, err := s.thr.AddAskSaid(in.ThreadID, in.RunID, in.Title, in.Detail, in.Kind, in.Check, in.Surface, in.Say)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
