// Goals and their notes.
package server

import (
	"net/http"

	"life/hub/internal/goals"
)

func (s *Server) listGoals(w http.ResponseWriter, r *http.Request) {
	gs, err := s.goals.List(r.URL.Query().Get("status"))
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, gs)
}

func (s *Server) createGoal(w http.ResponseWriter, r *http.Request) {
	var g goals.Goal
	if !decode(w, r, &g, 0) {
		return
	}
	g, err := s.goals.Create(g)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, g)
}

func (s *Server) getGoal(w http.ResponseWriter, r *http.Request) {
	g, err := s.goals.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such goal")
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) patchGoal(w http.ResponseWriter, r *http.Request) {
	var patch map[string]string
	if !decode(w, r, &patch, 0) {
		return
	}
	g, err := s.goals.Update(r.PathValue("id"), patch)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) goalNotes(w http.ResponseWriter, r *http.Request) {
	limit := qInt(r, "limit", 50, 500)
	get := s.goals.Notes
	if r.URL.Query().Get("since") == "digest" {
		get = s.goals.SinceDigest
	}
	ns, err := get(r.PathValue("id"), limit)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ns)
}

func (s *Server) addGoalNote(w http.ResponseWriter, r *http.Request) {
	var in struct{ Author, Kind, Text string }
	if !decode(w, r, &in, 0) {
		return
	}
	if in.Author == "" {
		in.Author = "owner"
	}
	n, err := s.goals.AddNote(r.PathValue("id"), in.Author, in.Kind, in.Text)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, n)
}
