// Calendar: the plan layer's agenda, add, get, resolve.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	it.Away = s.Cal.AwayOn(it.Day)
	writeJSON(w, 201, it)
}

// calendarAway: the spans the owner is away, from today (or ?from) on.
func (s *Server) calendarAway(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	from := r.URL.Query().Get("from")
	if from == "" {
		from = s.Cal.Now().Format("2006-01-02")
	}
	list, err := s.Cal.Aways(from, r.URL.Query().Get("to"))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) calendarAwayAdd(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in calendar.Away
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	a, err := s.Cal.AddAway(in)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) calendarAwayEnd(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct{ Note string }
	json.NewDecoder(r.Body).Decode(&in)
	a, err := s.Cal.EndAway(r.PathValue("id"), in.Note)
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

// calendarMerge: fold the path's item into `into`.
func (s *Server) calendarMerge(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct{ Into, By string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Into == "" {
		jsonErr(w, 400, "need {into}")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
	it, err := s.Cal.Merge(r.PathValue("id"), in.Into, in.By)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, it)
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

// calendarSteps: replace an open item's step list.
func (s *Server) calendarSteps(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct{ Steps []string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "need {steps: [titles]}")
		return
	}
	it, err := s.Cal.SetSteps(r.PathValue("id"), in.Steps)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, it)
}

// calendarStep: one step's Did / Skip (or its undo).
func (s *Server) calendarStep(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct {
		Outcome, By, Note string
		Days              []string // a repeat's days ahead (Calendar.PlanStep)
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		jsonErr(w, 400, "step must be its number")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
	var it calendar.Item
	if len(in.Days) > 0 {
		it, err = s.Cal.PlanStep(r.PathValue("id"), n, in.Days, in.Outcome, in.By, in.Note)
	} else {
		it, err = s.Cal.CloseStep(r.PathValue("id"), n, in.Outcome, in.By)
	}
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, it)
}

// calendarRaise: a waiting item's look found nothing; its one card on the owner.
func (s *Server) calendarRaise(w http.ResponseWriter, r *http.Request) {
	if s.Cal == nil {
		jsonErr(w, 503, "calendar not configured")
		return
	}
	var in struct{ By, Note string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.By == "" {
		in.By = "hub"
	}
	it, err := s.Cal.Raise(r.PathValue("id"), in.By, in.Note)
	if err != nil {
		jsonErr(w, 409, err.Error())
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
	if in.State == "" && (in.Outcome == "nudge" || in.Outcome == "extend") {
		it, err := s.Cal.AnswerWaiting(r.PathValue("id"), in.Outcome, in.Note)
		if err != nil {
			jsonErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, it)
		return
	}
	if in.State == "" && in.Outcome != "" {
		if in.State = map[string]string{"done": "done", "wont": "dismissed"}[in.Outcome]; in.State == "" {
			jsonErr(w, 400, "outcome must be done|wont|nudge|extend")
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
