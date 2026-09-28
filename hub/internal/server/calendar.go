// Calendar: the plan layer's agenda, add, get, resolve.
package server

import (
	"encoding/json"
	"net/http"

	"life/hub/internal/calendar"
)

// Calendar: the plan layer (internal/calendar).
func (s *Server) calendarView(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	v, err := s.Cal.Agenda(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) calendarAdd(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in calendar.Item
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	it, err := s.Cal.Add(in)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, it)
}

func (s *Server) calendarGet(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	it, err := s.Cal.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such item")
		return
	}
	writeJSON(w, 200, it)
}

// calendarUpdate: edit a scheduled item (title/detail/day/at/repeat) without
// dismissing it — dismissing a repeating item spawns its next occurrence, so
// it was never a way to move a standing check-in.
func (s *Server) calendarUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in map[string]string
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	it, err := s.Cal.Update(r.PathValue("id"), in)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, it)
}

func (s *Server) calendarResolve(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct{ State, By, Note, Outcome string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
	// A button of the row's `outcomes` pressed: the hub, not the client, says
	// which state done|wont closes it with.
	if in.State == "" && in.Outcome != "" {
		if in.State = map[string]string{"done": "done", "wont": "dismissed"}[in.Outcome]; in.State == "" {
			jsonErr(w, 400, "outcome must be done|wont")
			return
		}
	}
	it, err := s.Cal.Resolve(r.PathValue("id"), in.State, in.By, in.Note)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, it)
}
