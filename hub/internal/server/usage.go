package server

import (
	"net/http"
	"strings"

	"life/hub/internal/usage"
)

// ConsolePages counts the console's tabs, for the heartbeat's "pages": it
// grows when the owner's agent adds a page for a goal.
func ConsolePages() int {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return 0
	}
	return strings.Count(string(b), `data-tab="`)
}

type usageState struct {
	On        bool           `json:"on"`
	LastSent  string         `json:"last_sent"`
	InstallID string         `json:"install_id"`
	Payload   map[string]any `json:"payload"`
}

func (s *Server) usageView(h *usage.Heartbeat) usageState {
	return usageState{On: h.On(), LastSent: h.LastSent(), InstallID: h.InstallID(), Payload: h.Payload()}
}

func (s *Server) getUsage(w http.ResponseWriter, r *http.Request) {
	if !need(w, s.Usage != nil, "usage heartbeat not configured") {
		return
	}
	writeJSON(w, 200, s.usageView(s.Usage))
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
	writeJSON(w, 200, s.usageView(s.Usage))
}
