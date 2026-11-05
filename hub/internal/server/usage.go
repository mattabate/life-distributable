package server

import (
	"io/fs"
	"net/http"
	"strings"

	"life/hub/internal/usage"
)

// ConsolePages counts the console's tabs, for the heartbeat's "pages": each
// view file registers its own tab (`registerTab({`, web/ui.js), so it grows
// when the owner's agent adds a page for a goal.
func ConsolePages() int {
	files, err := fs.Glob(webFS, "web/views/*.js")
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range files {
		b, err := webFS.ReadFile(f)
		if err != nil {
			continue
		}
		n += strings.Count(string(b), "\nregisterTab({")
	}
	return n
}

type usageState struct {
	On        bool           `json:"on"`
	LastSent  string         `json:"last_sent"`
	InstallID string         `json:"install_id"`
	Host      string         `json:"host"`
	Payload   map[string]any `json:"payload"`
}

func (s *Server) usageView(h *usage.Heartbeat) (usageState, error) {
	p, err := h.Payload()
	if err != nil {
		return usageState{}, err
	}
	return usageState{On: h.On(), LastSent: h.LastSent(), InstallID: h.InstallID(), Host: h.Destination(), Payload: p}, nil
}

func (s *Server) getUsage(w http.ResponseWriter, r *http.Request) {
	if !need(w, s.Usage != nil, "usage heartbeat not configured") {
		return
	}
	st, err := s.usageView(s.Usage)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) setUsage(w http.ResponseWriter, r *http.Request) {
	if !need(w, s.Usage != nil, "usage heartbeat not configured") {
		return
	}
	var in struct {
		On *bool `json:"on"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	if in.On == nil {
		jsonErr(w, 400, "on (true|false) required")
		return
	}
	if err := s.Usage.SetOn(*in.On); err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	st, err := s.usageView(s.Usage)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}
