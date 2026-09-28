// Recommendations: list, add, stats, starter, decide, reply, score, link.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"life/hub/internal/recs"
)

// Recommendations: the ledger of what the agent told the owner to do
// (internal/recs). Unlike asks these never notify — the list is pulled, not
// pushed — so there is no board hook here, only CRUD plus the two closes
// (decide = what the owner chose, score = whether it worked).
func (s *Server) recsList(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	q := r.URL.Query()
	max, _ := strconv.Atoi(q.Get("max_cents"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := s.Recs.List(recs.Filter{Status: q.Get("status"), Domain: q.Get("domain"), Kind: q.Get("kind"),
		GoalID: q.Get("goal"), DueBy: q.Get("due"), MaxCents: max, Limit: limit, Model: q.Get("model"), Thread: q.Get("thread")})
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	s.stampRunning(list)
	writeJSON(w, 200, map[string]any{"recs": list})
}

// stampRunning marks each rec whose filing session has a turn in flight, so
// the card can say not to follow up yet. One threads query for the whole
// list; a rec the owner filed themselves has no session and stays unmarked.
func (s *Server) stampRunning(list []recs.Rec) {
	if s.thr == nil || len(list) == 0 {
		return
	}
	live := s.thr.RunningThreads()
	if len(live) == 0 {
		return
	}
	for i := range list {
		list[i].ThreadRunning = live[recs.SourceThread(list[i])]
	}
}

func (s *Server) recsAdd(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	var in recs.Rec
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	// The model is required on a session-filed rec, and the session need not
	// know it: the hub reads it off the session's own run.
	if in.Model == "" && s.thr != nil {
		if id := recs.SourceThread(in); id != "" {
			in.Model = s.thr.LiveModel(id)
		}
	}
	rec, err := s.Recs.Add(in)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

func (s *Server) recsGet(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	rec, err := s.Recs.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such rec")
		return
	}
	one := []recs.Rec{rec}
	s.stampRunning(one)
	writeJSON(w, 200, one[0])
}

// recsStarter: what an agent is told when the owner decides on this rec —
// `relay` for the session that filed it, `opener`+`context` for a new one.
// The surfaces show it (so the owner can see what they are sending); the hub itself
// sends it from POST /decide, so the brief is composed in exactly one place.
func (s *Server) recsStarter(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	rec, err := s.Recs.Get(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 404, "no such rec")
		return
	}
	rel := s.flow().Related(rec)
	st := recs.StarterFor(rec, rel)
	// Only offer the session that filed it if it is still there to answer:
	// an archived or deleted one would silently become a new session.
	src := recs.SourceThread(rec)
	if src != "" {
		if t, err := s.thr.Get(src); err != nil || t.Status == "archived" {
			src = ""
		}
	}
	writeJSON(w, 200, map[string]any{"rec_id": rec.ID, "goal_id": rec.GoalID, "opener": st.Opener,
		"context": st.Context, "relay": recs.RelayFor(rec, rel), "source_session": src})
}

func (s *Server) recsStats(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	st, err := s.Recs.Stats()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) recsDecide(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	var in struct {
		Status, By, Note, Deliver, Until string
		// Attachments: blob refs (an `app/photo` observation's `blob_ref`) —
		// a screenshot with the note, in the same message.
		Attachments []string
		recWhen
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
	at, err := in.when()
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	if len(in.Attachments) > 0 && in.Deliver == "" {
		jsonErr(w, 409, "attachments need somewhere to go: set deliver to source|new")
		return
	}
	var rec recs.Rec
	if in.Status == "deferred" {
		rec, err = s.Recs.Defer(r.PathValue("id"), in.Until, in.By, in.Note)
	} else {
		rec, err = s.Recs.Decide(r.PathValue("id"), in.Status, in.By, in.Note)
	}
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	if in.Status != "deferred" {
		s.flow().CancelCheckIn(rec)
	}
	out := recDecided{Rec: rec}
	if in.Status == "deferred" {
		if id, err := s.flow().DeferCheckIn(rec); err != nil {
			out.DeliverErr = "deferred, but no calendar item: " + err.Error()
		} else if id != "" {
			if linked, err := s.Recs.Link(rec.ID, id); err != nil {
				log.Printf("recs: link %s to %s: %v", rec.ID, id, err)
				out.DeliverErr = "deferred, but the check-in " + id + " is not linked: " + err.Error()
			} else {
				rec, out.Rec = linked, linked
			}
		}
	}
	if in.Deliver == "source" || in.Deliver == "new" {
		out.Delivered, out.Session, out.DeliverErr = s.flow().DeliverDecision(rec, in.Deliver, at, in.Attachments)
	}
	writeJSON(w, 200, out)
}

// recWhen: when the session hears the answer — the chat composer's own
// "now / in an hour / tomorrow 9am / at…" (same fields as POST /prompts),
// since a rec answered from inside the chat is sent with that composer.
type recWhen struct {
	At     string `json:"at"`
	In     string `json:"in"`
	On     string `json:"on"`
	AtTime string `json:"at_time"`
}

func (r recWhen) when() (time.Time, error) { return promptWhen(r.At, r.In, r.On, r.AtTime) }

// SetRecs installs the ledger and the hook that frames a prompt answering one
// of its rows: `rec:<id>` + outcome → recs.RelayHeader (a verdict) or
// recs.ReplyRelayHeader (words only), resolved against the rest of the hub at
// the moment the session wakes. The threads package cannot import recs
// (recs imports threads), so the frame arrives by function.
func (s *Server) SetRecs(rc *recs.Store) {
	s.Recs = rc
	if s.thr == nil {
		return
	}
	s.thr.RecHeader = func(id, outcome string) string {
		if s.Recs == nil {
			return ""
		}
		rec, err := s.Recs.Get(id)
		if err != nil {
			return ""
		}
		rel := s.flow().Related(rec)
		if outcome == "" {
			return recs.ReplyRelayHeader(rec, rel)
		}
		return recs.RelayHeader(rec, rel)
	}
	// A verdict arriving on a prompt that answers several cards at once
	// (2026-09-17): the same write recsDecide does, minus the relay, which
	// IS the prompt. Already in that state (recsDecide went first) = nothing.
	s.thr.DecideRec = func(id, outcome, note string) error {
		if s.Recs == nil {
			return nil
		}
		rec, err := s.Recs.Get(id)
		if err != nil {
			return err
		}
		if rec.Status == outcome {
			return nil
		}
		rec, err = s.Recs.Decide(id, outcome, "owner", note)
		if err != nil {
			return err
		}
		s.flow().CancelCheckIn(rec)
		return nil
	}
}

// recDecided is the rec plus where the answer went: `delivered` is the
// destination that actually took it (which is "new" even when the owner asked for
// "source", if that session is gone), and `session_id` is the one to open.
type recDecided struct {
	recs.Rec
	Delivered  string `json:"delivered,omitempty"`
	Session    string `json:"session_id,omitempty"`
	DeliverErr string `json:"delivery_error,omitempty"`
}

// MarshalJSON keeps the three delivery fields: the embedded Rec's own
// MarshalJSON (its wire labels) would otherwise be promoted and drop them.
func (d recDecided) MarshalJSON() ([]byte, error) {
	rec, err := json.Marshal(d.Rec)
	if err != nil {
		return nil, err
	}
	extra, err := json.Marshal(struct {
		Delivered  string `json:"delivered,omitempty"`
		Session    string `json:"session_id,omitempty"`
		DeliverErr string `json:"delivery_error,omitempty"`
	}{d.Delivered, d.Session, d.DeliverErr})
	if err != nil || string(extra) == "{}" {
		return rec, err
	}
	return append(append(rec[:len(rec)-1], ','), extra[1:]...), nil
}

// flow: what happens around a rec decision (recs/flow.go) — the brief, the
// deferral's check-in, the delivery. Built per call because Cal and Recs
// are set after New.
func (s *Server) flow() *recs.Flow {
	return &recs.Flow{Store: s.Recs, Cal: s.Cal, Thr: s.thr, Acts: s.acts}
}

// recsReply is the fourth button: reply without deciding. The note goes to a session exactly as a decision's would —
// same two destinations — and the rec stays where it is, undecided. A reply
// that reaches nobody is not a reply, so `deliver` is required here.
func (s *Server) recsReply(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	var in struct {
		By, Note, Deliver string
		Attachments       []string
		recWhen
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	if in.By == "" {
		in.By = "owner"
	}
	at, err := in.when()
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.Note == "" && len(in.Attachments) == 0 {
		jsonErr(w, 409, "a reply needs a note or an attachment")
		return
	}
	if in.Deliver != "source" && in.Deliver != "new" {
		jsonErr(w, 409, "deliver must be source|new")
		return
	}
	// The record keeps a line either way: a screenshot with no words is still
	// a reply, and the rec's detail should say one was sent (the picture
	// itself is in the chat).
	record := in.Note
	if record == "" {
		record = "(sent " + plural(len(in.Attachments), "attachment") + ")"
	}
	rec, err := s.Recs.Reply(r.PathValue("id"), in.By, record)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	out := recDecided{Rec: rec}
	out.Delivered, out.Session, out.DeliverErr = s.flow().DeliverReply(rec, in.Note, in.Deliver, at, in.Attachments)
	writeJSON(w, 200, out)
}

func (s *Server) recsScore(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	var in struct{ Outcome, By, Note string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	rec, err := s.Recs.Score(r.PathValue("id"), in.Outcome, in.By, in.Note)
	if err != nil {
		jsonErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) recsLink(w http.ResponseWriter, r *http.Request) {
	if s.Recs == nil {
		jsonErr(w, 503, "recs not configured")
		return
	}
	var in struct {
		Refs []string `json:"refs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonErr(w, 400, "bad json")
		return
	}
	rec, err := s.Recs.Link(r.PathValue("id"), in.Refs...)
	if err != nil {
		jsonErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, rec)
}
