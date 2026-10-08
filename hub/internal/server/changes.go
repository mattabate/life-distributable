package server

import (
	"net/http"
	"strconv"
	"time"
)

// changes: the change feed (speed phase, 2026-08-26). The phone used to ask
// "anything new?" by re-fetching the board, the threads and the goals every
// 6 s, and an open chat its five reads every 3 s — most of them returning
// exactly what it had. Now it parks ONE request here with the version it
// last saw; the hub answers the moment the version moves, or after `wait`
// seconds with changed=false, and the phone re-fetches (with ETags, so an
// unchanged screen is a 304) only then. Idle traffic drops to one small
// request per half-minute, and a change shows up within a second.
//
// Query `since` (a version from an earlier answer; empty = answer at once),
// `wait` (seconds, default 25, max 30 — under the phone's request timeout),
// `thread` (narrow to one chat's messages/events/summaries/cost). The
// version is opaque: compare, never parse.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since := q.Get("since")
	wait, _ := strconv.Atoi(q.Get("wait"))
	if wait <= 0 || wait > 30 {
		wait = 25
	}
	thread := q.Get("thread")
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	for {
		v, err := s.thr.Version(thread)
		if err != nil {
			jsonErr(w, 400, err.Error())
			return
		}
		if since == "" || v != since || !time.Now().Before(deadline) {
			writeJSON(w, 200, map[string]any{"version": v, "changed": since != "" && v != since})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.stopping:
			// The hub is restarting: answer now so Shutdown is not held, and
			// SAY so. An app's pooled connection to the old hub would go on
			// taking requests for minutes after a restart; `restarting` is
			// its cue to drop the pool and connect afresh (HubClient.changes).
			writeJSON(w, 200, map[string]any{"version": v, "changed": false, "restarting": true})
			return
		case <-time.After(changesTick):
		}
	}
}

// changesTick: how often a parked request re-reads the version. A second is
// well under what the phone could tell apart, and the read is a handful of
// indexed MAX()es.
var changesTick = time.Second
