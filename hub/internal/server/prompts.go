// Prompts: queue what a session is told, now or later, by the owner or by itself.
// The write side of the one inbound lane (threads/prompts.go).
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"life/hub/internal/store"
	"life/hub/internal/threads"
)

func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	ps, err := s.thr.ListPrompts(q.Get("state"), q.Get("thread"), limit)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ps)
}

// addPrompt: {author, target|new, text, in_reply_to, outcome, at|in|on,
// title, goal_id, attachments}. Time is given as an absolute RFC3339 `at`, a
// duration `in` ("30m", "2h"), or a day + optional time (`on`, `at_time`) in
// Eastern — the hub's clock.
func (s *Server) addPrompt(w http.ResponseWriter, r *http.Request) {
	// Tags, not bare fields: encoding/json matches names case-insensitively but
	// NOT across underscores, so `in_reply_to` would silently arrive empty —
	// which is the whole reference the surfaces send.
	var in struct {
		Author      string   `json:"author"`
		Target      string   `json:"target"`
		Text        string   `json:"text"`
		InReplyTo   string   `json:"in_reply_to"`
		Outcome     string   `json:"outcome"`
		Title       string   `json:"title"`
		GoalID      string   `json:"goal_id"`
		At          string   `json:"at"`
		In          string   `json:"in"`
		On          string   `json:"on"`
		AtTime      string   `json:"at_time"`
		New         bool     `json:"new"`
		Attachments []string `json:"attachments"`
		// Replies: several cards on one message, each with its own outcome;
		// in_reply_to/outcome alone is still one card.
		Replies []threads.Reply `json:"replies"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if in.Author == "" {
		in.Author = "owner"
	}
	if in.New && in.Target == "" {
		in.Target = "new"
	}
	when, err := promptWhen(in.At, in.In, in.On, in.AtTime)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	via := r.URL.Query().Get("via")
	if via == "" {
		via = "app"
	}
	// An approve/deny riding on a message (the approval card arms the
	// composer, 09-17) is gated exactly as /actions/{id}/approve is: the
	// decider code, which the hub token — every session's — does not carry.
	replies := append([]threads.Reply{{Ref: in.InReplyTo, Outcome: in.Outcome}}, in.Replies...)
	for _, re := range replies {
		if kind, id := store.SplitRef(strings.TrimSpace(re.Ref)); kind == "action" && (re.Outcome == "approved" || re.Outcome == "denied") {
			if !s.deciderOK(w, r, id, strings.TrimSuffix(re.Outcome, "d"), via) {
				return
			}
		}
	}
	p, err := s.thr.Queue(threads.Prompt{Author: in.Author, Target: in.Target, Text: in.Text,
		InReplyTo: in.InReplyTo, Outcome: in.Outcome, Replies: in.Replies, NotBefore: when,
		Title: in.Title, GoalID: in.GoalID, Attachments: in.Attachments, Via: via})
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) cancelPrompt(w http.ResponseWriter, r *http.Request) {
	p, err := s.thr.CancelPrompt(r.PathValue("id"))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// promptWhen turns the three ways of naming a time into one instant. Zero =
// deliver now.
func promptWhen(at, in, on, atTime string) (time.Time, error) {
	switch {
	case in != "":
		d, err := time.ParseDuration(in)
		if err != nil {
			return time.Time{}, fmt.Errorf("in: %v (try 30m, 2h)", err)
		}
		if d < 0 {
			return time.Time{}, fmt.Errorf("in: must be in the future")
		}
		return time.Now().Add(d), nil
	case on != "":
		hm := atTime
		if hm == "" {
			hm = at // `--on 2026-09-01 --at 09:00`: at is a clock time, not RFC3339
		}
		if hm == "" {
			hm = "09:00"
		}
		if !strings.Contains(hm, ":") {
			return time.Time{}, fmt.Errorf("at: want HH:MM, got %q", hm)
		}
		t, err := time.ParseInLocation("2006-01-02 15:04", on+" "+hm, eastern())
		if err != nil {
			return time.Time{}, fmt.Errorf("on/at: %v", err)
		}
		return t, nil
	case at != "":
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return time.Time{}, fmt.Errorf("at: want RFC3339 or --on <day> --at HH:MM: %v", err)
		}
		return t, nil
	}
	return time.Time{}, nil
}

// eastern: the owner's clock. A prompt for "09:00" means their morning.
func eastern() *time.Location {
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		return loc
	}
	return time.Local
}
