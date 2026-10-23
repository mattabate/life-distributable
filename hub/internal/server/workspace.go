package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"life/hub/internal/workspace"
)

// Workspaces (team mode): a role's own files, read by every session and
// written only by the session that plays the role, or the owner. 404 while
// team mode is off, so a session told about workspaces by an old note learns
// the switch is off from the one line.

func (s *Server) workspaceOn(w http.ResponseWriter) bool {
	if !s.thr.TeamMode {
		jsonErr(w, 404, "team mode is off (team_mode in ops/hub.json)")
		return false
	}
	return need(w, s.WS != nil, "workspaces not configured")
}

func (s *Server) listWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.workspaceOn(w) {
		return
	}
	key := r.URL.Query().Get("persona")
	if key != "" {
		if key = s.WS.Resolve(key); key == "" {
			jsonErr(w, 404, "no such persona")
			return
		}
	}
	fs, err := s.WS.List(key)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, fs)
}

func (s *Server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !s.workspaceOn(w) {
		return
	}
	key := s.WS.Resolve(r.PathValue("persona"))
	if key == "" {
		jsonErr(w, 404, "no such persona")
		return
	}
	name := r.PathValue("name")
	b, err := s.WS.Read(key, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			jsonErr(w, 404, "no such file")
			return
		}
		jsonErr(w, 400, err.Error())
		return
	}
	ct := "text/plain; charset=utf-8"
	switch {
	case strings.HasSuffix(name, ".md"):
		ct = "text/markdown; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		ct = "application/json"
	case strings.HasSuffix(name, ".csv"):
		ct = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	// A file is data, never a page: nothing in it runs in the console's origin.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.WriteHeader(200)
	w.Write(b)
}

func (s *Server) putWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !s.workspaceOn(w) {
		return
	}
	key := s.WS.Resolve(r.PathValue("persona"))
	if key == "" {
		jsonErr(w, 404, "no such persona")
		return
	}
	var body []byte
	by := ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if !decode(w, r, &in, workspace.MaxBytes+1<<20) {
			return
		}
		body, by = []byte(in.Text), in.By
	} else {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, workspace.MaxBytes+1))
		if err != nil {
			jsonErr(w, 400, "body too large")
			return
		}
		body = b
	}
	f, err := s.WS.Write(key, r.PathValue("name"), body, by)
	if err != nil {
		if errors.Is(err, workspace.ErrForbidden) {
			jsonErr(w, 403, err.Error())
			return
		}
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, f)
}
