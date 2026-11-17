// Meetings: sessions talking in one file, woken by each other's posts
// (threads/meeting.go). A session is who its header says (caller); with no
// session behind the request it is the owner.
package server

import (
	"net/http"

	"life/hub/internal/threads"
)

func (s *Server) listMeetings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ms, err := s.thr.Meetings(q.Get("state"), q.Get("thread"))
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ms)
}

func (s *Server) getMeeting(w http.ResponseWriter, r *http.Request) {
	mt, err := s.thr.Meeting(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, mt)
}

func (s *Server) openMeeting(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Topic   string   `json:"topic"`
		Members []string `json:"members"`
		Parent  string   `json:"parent"`
		Text    string   `json:"text"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	mt, err := s.thr.OpenMeeting(s.actor(r, ""), in.Topic, in.Members, in.Parent, in.Text)
	s.meetingDone(w, 201, mt, err)
}

func (s *Server) sayMeeting(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	mt, err := s.thr.SayMeeting(r.PathValue("id"), s.actor(r, ""), in.Text)
	s.meetingDone(w, 201, mt, err)
}

func (s *Server) joinMeeting(w http.ResponseWriter, r *http.Request) {
	from := s.caller(r)
	if from == "" {
		jsonErr(w, 400, "a session rejoins a meeting; the owner posts into any")
		return
	}
	mt, err := s.thr.JoinMeeting(r.PathValue("id"), from)
	s.meetingDone(w, 200, mt, err)
}

func (s *Server) leaveMeeting(w http.ResponseWriter, r *http.Request) {
	from := s.caller(r)
	if from == "" {
		jsonErr(w, 400, "a session leaves a meeting; the owner closes it")
		return
	}
	mt, err := s.thr.LeaveMeeting(r.PathValue("id"), from)
	s.meetingDone(w, 200, mt, err)
}

func (s *Server) closeMeeting(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in, 0) {
		return
	}
	mt, err := s.thr.CloseMeeting(r.PathValue("id"), s.actor(r, ""), in.Text)
	s.meetingDone(w, 200, mt, err)
}

func (s *Server) meetingDone(w http.ResponseWriter, code int, mt threads.Meeting, err error) {
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, code, mt)
}
