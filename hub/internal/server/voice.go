package server

import (
	"net/http"
	"time"

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

// postVoiceHush: a double tap on a card's "Waiting to speak" — the line goes,
// the card stays (notify.Hush).
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
	w.WriteHeader(204)
}
