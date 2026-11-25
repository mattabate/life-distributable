package server

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"life/hub/internal/notify"
	"life/hub/internal/threads"
)

// replayMax bounds one replay's hold: a page closed mid-line never sends the
// end, so the mark lapses on its own.
const replayMax = 5 * time.Minute

// postVoice: a card's Play button on either surface says its line is being
// heard (secs = how long it takes to say) and says so again with secs 0 when
// it stops. The session's row shows "speaking" meanwhile, and every push
// waits for it.
func (s *Server) postVoice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThreadID string  `json:"thread_id"`
		Secs     float64 `json:"secs"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	key := in.ThreadID
	if key == "" {
		key = threads.Replay
	}
	d := min(time.Duration(in.Secs*float64(time.Second)), replayMax)
	now := time.Now()
	s.thr.Voice.Mark(key, now.Add(d))
	writeJSON(w, 200, map[string]any{"speaking": s.thr.Voice.Live(now)})
}

// postVoiceHush: a double tap on a card's "Waiting to speak" or "Speaking" —
// the line goes (the Mac's voice stops within a second), the card stays
// (notify.Hush), and its "speaking" mark ends at once.
func (s *Server) postVoiceHush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Card string `json:"card"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if in.Card == "" {
		jsonErr(w, 400, "card required")
		return
	}
	s.Push.Hush(in.Card)
	s.thr.Voice.EndCard(in.Card)
	w.WriteHeader(204)
}

const earDeviceErr = "device: a Mac's host name (up to 40 of a-z, 0-9, -; not phone)"

// getVoiceEar: a Mac's ear polls (ops/ear.swift every 2 s, or the desktop
// app while it is open) with its output route and the seconds since its last
// input, and is handed the line it should say, if any (notify/ear.go).
func (s *Server) getVoiceEar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, ok := notify.EarDevice(q.Get("device")); !ok {
		jsonErr(w, 400, earDeviceErr)
		return
	}
	idle := -1.0
	if v, err := strconv.ParseFloat(q.Get("idle"), 64); err == nil && v >= 0 {
		idle = v
	}
	writeJSON(w, 200, s.Push.EarPoll(q.Get("device"), q.Get("route"), q.Get("output"), idle, q.Get("by") == "ear"))
}

// postVoiceEar: the ear's word on the line it was handed: playing, spoke
// or silent.
func (s *Server) postVoiceEar(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Device  string `json:"device"`
		ID      string `json:"id"`
		Outcome string `json:"outcome"`
		// Why: the voice helper's reason for a silent line; its own log
		// stays on its Mac, so the hub's log carries it.
		Why string `json:"why"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	switch in.Outcome {
	case "playing", "spoke", "silent":
	default:
		jsonErr(w, 400, "outcome must be playing, spoke or silent")
		return
	}
	dev, ok := notify.EarDevice(in.Device)
	if !ok {
		jsonErr(w, 400, earDeviceErr)
		return
	}
	if in.Outcome == "silent" && in.Why != "" {
		// %q keeps it one log line whatever the helper sent.
		why := []rune(in.Why)
		log.Printf("ear: %s left %q unsaid: %q", dev, in.ID, string(why[:min(len(why), 200)]))
	}
	writeJSON(w, 200, s.Push.EarReport(in.Device, in.ID, in.Outcome))
}

// postVoiceStop: the Devices card's Stop — the line its tile showed (`id`)
// stops if a Mac is still saying it; its card stays open and it is not said
// again. A line that has already ended is not replaced by the next one.
func (s *Server) postVoiceStop(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if in.ID == "" {
		jsonErr(w, 400, "id: the line the tile shows (settings devices[].line)")
		return
	}
	writeJSON(w, 200, map[string]bool{"stopped": s.Push.StopLine(in.ID)})
}
