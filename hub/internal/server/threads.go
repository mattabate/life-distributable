// Threads: create, message, stream events, check in, stop, archive, read.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"life/hub/internal/threads"
)

// threadOut is a Thread as the list and the single-thread GET send it: the
// thread plus `model`, the model it is running on (threads.Manager.Models /
// LiveModel — a fact of its runs, not a column of its own). Both surfaces
// print it on the session card.
// The labels ride along so both surfaces print the same words: `model_label`
// ("fable 5.1"), `schedule_label` ("weekly Sun 17:30") and `pill`, the status
// capsule a card wears when the board has none for it.
type threadOut struct {
	threads.Thread
	Model         string       `json:"model,omitempty"`
	ModelLabel    string       `json:"model_label,omitempty"`
	ScheduleLabel string       `json:"schedule_label,omitempty"`
	Pill          threads.Pill `json:"pill"`
}

func newThreadOut(t threads.Thread, model string) threadOut {
	return threadOut{Thread: t, Model: model, ModelLabel: threads.ModelLabel(model),
		ScheduleLabel: threads.ScheduleLabel(t.Schedule), Pill: threads.StatusPill(t.Status)}
}

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	ts, err := s.thr.Find(r.URL.Query().Get("q"), r.URL.Query().Get("archived") == "1")
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	models := s.thr.Models()
	out := make([]threadOut, len(ts))
	for i, t := range ts {
		out[i] = newThreadOut(t, models[t.ID])
	}
	writeJSON(w, 200, out)
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title, Project, GoalID, Prompt, Schedule, SchedulePrompt string
		Attachments                                              []string
		// Via: the surface this came through ("console" from the web console);
		// the session then gets a pointer to the console's own log, not the log.
		Via string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.Project == "" {
		in.Project = "life"
	}
	t, err := s.thr.CreateVia(in.Title, in.Project, in.GoalID, in.Prompt, in.Schedule, in.SchedulePrompt, in.Attachments, via(in.Via))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	t, err := s.thr.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such thread")
		return
	}
	writeJSON(w, 200, newThreadOut(t, s.thr.LiveModel(t.ID)))
}

func (s *Server) patchThread(w http.ResponseWriter, r *http.Request) {
	var patch map[string]string
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	t, err := s.thr.Update(r.PathValue("id"), patch)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) threadMessages(w http.ResponseWriter, r *http.Request) {
	limit := qInt(r, "limit", 100, 500)
	ms, err := s.thr.Messages(r.PathValue("id"), limit)
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, ms)
}

// threadEvents: streamed steps (tool calls, results, thinking, interim text)
// of a thread's runs; `since` = last event id the client already has.
func (s *Server) threadEvents(w http.ResponseWriter, r *http.Request) {
	limit := qInt(r, "limit", 500, 2000)
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	var ids []int64
	for _, p := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			ids = append(ids, n)
		}
	}
	var evs []threads.Event
	var err error
	if len(ids) > 0 {
		evs, err = s.thr.EventsByID(r.PathValue("id"), ids)
	} else {
		evs, err = s.thr.Events(r.PathValue("id"), since, before, limit)
	}
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, evs)
}

// threadSteps: how many steps sit under each message of the chat, counted in
// SQL. The headline of a run block comes from here, so it is right however
// many events the client happens to hold.
func (s *Server) threadSteps(w http.ResponseWriter, r *http.Request) {
	steps, err := s.thr.Steps(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, steps)
}

func (s *Server) sendThread(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string
		Attachments []string
		Via         string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (strings.TrimSpace(in.Text) == "" && len(in.Attachments) == 0) {
		jsonErr(w, 400, "need {text} and/or {attachments}")
		return
	}
	if err := s.thr.SendVia(r.PathValue("id"), in.Text, in.Attachments, via(in.Via)); err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	s.threadAfter(w, r, 202)
}

// via: the only surface value the hub stores today. Anything else (a typo,
// a client inventing one) is "", i.e. no stamp — the column is not free text.
func via(v string) string {
	if v == "console" {
		return v
	}
	return ""
}

func (s *Server) checkinThread(w http.ResponseWriter, r *http.Request) {
	if err := s.thr.CheckIn(r.PathValue("id"), "api"); err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	s.threadAfter(w, r, 202)
}

func (s *Server) stopThread(w http.ResponseWriter, r *http.Request) {
	if err := s.thr.Stop(r.PathValue("id")); err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	s.threadAfter(w, r, 200)
}

func (s *Server) archiveThread(w http.ResponseWriter, r *http.Request) {
	if err := s.thr.Archive(r.PathValue("id")); err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	s.threadAfter(w, r, 200)
}

// threadAfter writes the thread as it stands after a mutation. The mutation
// succeeded, so a failed re-read is a 500 with the reason, not a zero thread.
func (s *Server) threadAfter(w http.ResponseWriter, r *http.Request, code int) {
	t, err := s.thr.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 500, "changed, but re-reading it failed: "+err.Error())
		return
	}
	writeJSON(w, code, t)
}

func (s *Server) readThread(w http.ResponseWriter, r *http.Request) {
	s.thr.MarkRead(r.PathValue("id"))
	w.WriteHeader(204)
}
