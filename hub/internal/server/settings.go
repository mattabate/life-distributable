// The owner's switches that live in ops/hub.json (team mode, plain sessions,
// the model cap), and the team's roles.
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"life/hub/internal/spend"
	"life/hub/internal/threads"
)

// settingsOut: GET /api/v1/settings. models is the ladder model_cap is one
// of, best first ("" = no cap, the top).
type settingsOut struct {
	TeamMode      bool     `json:"team_mode"`
	PlainSessions bool     `json:"plain_sessions"`
	ModelCap      string   `json:"model_cap"`
	Models        []string `json:"models"`
}

func (s *Server) settingsNow() settingsOut {
	out := settingsOut{TeamMode: s.thr.TeamMode(), PlainSessions: s.thr.PlainSessions(), Models: s.modelChoices()}
	if s.picker != nil {
		out.ModelCap = s.picker.Cap()
	} else {
		out.ModelCap = s.cfg.ModelCap
	}
	return out
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.settingsNow())
}

// patchSettings writes the keys given into ops/hub.json (every other key
// and its place kept) and applies them at once: no restart.
func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TeamMode      *bool   `json:"team_mode"`
		PlainSessions *bool   `json:"plain_sessions"`
		ModelCap      *string `json:"model_cap"`
	}
	dec := json.NewDecoder(jsonBody(w, r))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		jsonErr(w, 400, "bad json (keys: team_mode, plain_sessions, model_cap): "+err.Error())
		return
	}
	set := map[string]any{}
	if in.TeamMode != nil {
		set["team_mode"] = *in.TeamMode
	}
	if in.PlainSessions != nil {
		set["plain_sessions"] = *in.PlainSessions
	}
	if in.ModelCap != nil {
		c := spend.Current(strings.TrimSpace(*in.ModelCap))
		if c != "" && !slices.Contains(s.modelChoices(), c) {
			jsonErr(w, 400, "model_cap must be \"\" or one of "+strings.Join(s.modelChoices(), ", "))
			return
		}
		set["model_cap"] = c
		in.ModelCap = &c
	}
	if len(set) == 0 {
		jsonErr(w, 400, "nothing to set (keys: team_mode, plain_sessions, model_cap)")
		return
	}
	if err := writeHubJSON(s.cfg.OpsPath("hub.json"), set); err != nil {
		jsonErr(w, 500, "ops/hub.json: "+err.Error())
		return
	}
	if in.ModelCap != nil {
		s.cfg.ModelCap = *in.ModelCap
		if s.picker != nil {
			s.picker.SetCap(*in.ModelCap)
		}
	}
	if in.PlainSessions != nil {
		s.cfg.PlainSessions = *in.PlainSessions
		s.thr.SetPlainSessions(*in.PlainSessions)
	}
	if in.TeamMode != nil {
		s.cfg.TeamMode = *in.TeamMode
		if *in.TeamMode {
			s.thr.StartTeam()
		} else {
			s.thr.SetTeamMode(false)
		}
	}
	writeJSON(w, 200, s.settingsNow())
}

// writeHubJSON sets keys in the hub.json at path, keeping every other key
// in its place, through a temporary file renamed over it (same mode).
func writeHubJSON(path string, set map[string]any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("not a JSON object")
	}
	type kv struct {
		k string
		v json.RawMessage
	}
	var kvs []kv
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		kvs = append(kvs, kv{tok.(string), v})
	}
	for _, k := range []string{"team_mode", "plain_sessions", "model_cap"} {
		v, ok := set[k]
		if !ok {
			continue
		}
		raw, _ := json.Marshal(v)
		i := slices.IndexFunc(kvs, func(e kv) bool { return e.k == k })
		if i < 0 {
			kvs = append(kvs, kv{k, raw})
		} else {
			kvs[i].v = raw
		}
	}
	var out bytes.Buffer
	out.WriteString("{\n")
	for i, e := range kvs {
		key, _ := json.Marshal(e.k)
		var val bytes.Buffer
		if json.Indent(&val, e.v, "  ", "  ") != nil {
			val.Write(e.v)
		}
		fmt.Fprintf(&out, "  %s: %s", key, val.Bytes())
		if i < len(kvs)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// The team's roles (team mode). Reading them is anyone's; adding or
// rewriting one is the owner's or the chief of staff's: the owner asks the
// chief of staff for a new role, never a form.

func (s *Server) listPersonas(w http.ResponseWriter, r *http.Request) {
	if !s.thr.TeamMode() {
		jsonErr(w, 404, "team mode is off (team_mode in ops/hub.json)")
		return
	}
	writeJSON(w, 200, s.thr.PersonaDefs())
}

// teamWriter: may this request add or rewrite a role? The owner, or the
// session that is the chief of staff (an older install's executive assistant
// where there is none).
func (s *Server) teamWriter(w http.ResponseWriter, r *http.Request) bool {
	if !s.thr.TeamMode() {
		jsonErr(w, 404, "team mode is off (team_mode in ops/hub.json)")
		return false
	}
	if id := s.caller(r); id != "" && id != s.thr.CatchAll() {
		jsonErr(w, 403, "only the chief of staff adds or changes a role: relay the owner's wish there (`lifectl relay <its id> \"…\"`)")
		return false
	}
	return true
}

func (s *Server) newPersona(w http.ResponseWriter, r *http.Request) {
	if !s.teamWriter(w, r) {
		return
	}
	var in struct {
		threads.Persona
		Name string `json:"name"`
		By   string `json:"by"`
	}
	if !decode(w, r, &in, 0) {
		return
	}
	p, err := s.thr.NewPersona(in.Persona, in.Name, s.actor(r, in.By))
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) patchPersona(w http.ResponseWriter, r *http.Request) {
	if !s.teamWriter(w, r) {
		return
	}
	var patch map[string]string
	if err := json.NewDecoder(jsonBody(w, r)).Decode(&patch); err != nil {
		jsonErr(w, 400, "bad json (string fields only)")
		return
	}
	by := s.actor(r, patch["by"])
	delete(patch, "by")
	delete(patch, "author")
	p, err := s.thr.EditPersona(r.PathValue("key"), patch, by)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}
