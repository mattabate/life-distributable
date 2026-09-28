package server

import (
	"net/http"

	"life/hub/internal/attention"
)

// board: what needs the owner on one surface, built once by the hub
// (internal/attention) so the console and the phone draw the same numbers
// instead of each computing their own.
func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	svc := attention.Service{Thr: s.thr, Acts: s.acts, Cal: s.Cal, Recs: s.Recs}
	b, err := svc.Board(r.URL.Query().Get("surface"))
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, b)
}

// BadgeTotal is the number on the app's icon: the console nav's red ovals
// added up — your turn + calendar + recs. -1 when the board fails, so a bad
// read never zeroes the icon.
func (s *Server) BadgeTotal() int {
	svc := attention.Service{Thr: s.thr, Acts: s.acts, Cal: s.Cal, Recs: s.Recs}
	b, err := svc.Board("web")
	if err != nil {
		return -1
	}
	return b.Badges.YourTurn + b.Badges.Calendar + b.Badges.Recs
}
