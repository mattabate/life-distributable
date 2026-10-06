// Observations (single, batch, counts) and their blobs.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"life/hub/internal/notify"
	"life/hub/internal/obs"
)

// registerKinds: what the server does when rows of a kind land, registered
// on the store so it happens whichever path wrote them — the phone's single
// or batch POST, a syncer, the mic (obs.Ingest).
func (s *Server) registerKinds() {
	s.obs.Register(obs.Kind{Source: "app", Kind: "speech", OnInsert: func(rows []obs.Observation) {
		for _, o := range rows {
			s.phoneSpeech(o)
		}
	}})
}

func (s *Server) listObs(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q := obs.Query{Source: v.Get("source"), Kind: v.Get("kind"), Account: v.Get("account")}
	q.Limit, _ = strconv.Atoi(v.Get("limit"))
	var err error
	for _, b := range []struct {
		name string
		t    *time.Time
	}{{"since", &q.Since}, {"until", &q.Until}} {
		if x := v.Get(b.name); x != "" {
			if *b.t, err = time.Parse(time.RFC3339, x); err != nil {
				jsonErr(w, 400, "bad "+b.name+": want RFC3339")
				return
			}
		}
	}
	if x := v.Get("after_id"); x != "" {
		if q.AfterID, err = strconv.ParseInt(x, 10, 64); err != nil || q.AfterID < 0 {
			jsonErr(w, 400, "bad after_id")
			return
		}
		q.Forward = true
	}
	os_, err := s.obs.List(q)
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, os_)
}

func (s *Server) obsCounts(w http.ResponseWriter, r *http.Request) {
	cs, err := s.obs.Counts()
	if err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, cs)
}

// postObs accepts either JSON {source, kind, ts?, tz?, payload?} or
// multipart/form-data with the same fields plus a "file" part (photo).
func (s *Server) postObs(w http.ResponseWriter, r *http.Request) {
	var o obs.Observation
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			jsonErr(w, 400, "bad multipart: "+err.Error())
			return
		}
		o.Source, o.Kind, o.TZ = r.FormValue("source"), r.FormValue("kind"), r.FormValue("tz")
		if v := r.FormValue("ts"); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				jsonErr(w, 400, "bad ts")
				return
			}
			o.TS = t
		}
		if v := r.FormValue("payload"); v != "" {
			o.Payload = json.RawMessage(v)
		}
		if f, hdr, err := r.FormFile("file"); err == nil {
			defer f.Close()
			ext := strings.TrimPrefix(filepath.Ext(hdr.Filename), ".")
			ref, size, err := s.obs.PutBlob(f, ext)
			if err != nil {
				jsonErr(w, 400, "blob: "+err.Error())
				return
			}
			o.BlobRef = ref
			// record size + original name alongside whatever payload came in
			var pl map[string]any
			json.Unmarshal(o.Payload, &pl)
			if pl == nil {
				pl = map[string]any{}
			}
			pl["blob_bytes"], pl["filename"] = size, hdr.Filename
			// A document sent to a session (the composer's File picker, a
			// drop on the chat, the share sheet) is saved in the finance
			// intake's in-box under its own name as well — the blob is a
			// hash, and a bank statement is a file to import and file away
			// (obs.PutIntake). A failed copy never loses the upload.
			if o.Kind == "upload" {
				if p, err := s.obs.PutIntake(ref, hdr.Filename); err != nil {
					log.Printf("obs: intake copy of %s: %v", hdr.Filename, err)
				} else if p != "" {
					pl["intake_path"] = p
				}
			}
			o.Payload, _ = json.Marshal(pl)
		}
	} else {
		if !decode(w, r, &o, 4<<20) {
			return
		}
	}
	o, err := s.obs.Insert(o) // the kind's hooks (registerKinds) run in there
	if err != nil && err != obs.ErrDuplicate {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, o)
}

// postBlob: blob first, then refs. The raw body is stored content-addressed
// and its ref comes back for the rows that name it (a batch of screenshots,
// the Mac recorder's frames); `ext` names the file type. The same bytes
// twice are one blob.
func (s *Server) postBlob(w http.ResponseWriter, r *http.Request) {
	ext := r.URL.Query().Get("ext")
	if ext == "" || !fieldSafe(ext) {
		jsonErr(w, 400, "ext required (letters and digits: png, jpg, m4a)")
		return
	}
	ref, size, err := s.obs.PutBlob(http.MaxBytesReader(w, r.Body, 64<<20), ext)
	if err != nil {
		jsonErr(w, 400, "blob: "+err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ref": ref, "bytes": size})
}

func fieldSafe(s string) bool {
	if len(s) > 8 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// phoneSpeech takes the phone's word on a push (Speak.swift report, source
// app, kind speech): it goes in the hub log next to the "the phone speaks"
// line that sent it — "phone: spoke into Headphones (foreground)",
// "phone: silent, route Speaker", "phone: stopped, route left headphones" —
// and it is what marks the session "speaking" on the board. The push itself
// marks nothing for the phone (notify/apns.go): only a line that is being
// heard is speaking, and the phone is the one that knows. `spoke` marks the
// session for the line's reckoned length; `finished`, `silent` and `stopped`
// end any mark.
func (s *Server) phoneSpeech(o obs.Observation) {
	if o.Source != "app" || o.Kind != "speech" {
		return
	}
	var p struct{ Outcome, Route, State, Line, Thread string }
	json.Unmarshal(o.Payload, &p)
	now := time.Now()
	// The phone's route is the only word the hub gets on whether its ears
	// are in: it decides whether a held push reads "waiting to speak"
	// (notify/ears.go). A stale push says nothing about the route.
	if s.Push != nil {
		switch p.Outcome {
		case "spoke", "finished":
			s.Push.PhoneRoute(true)
		case "silent", "stopped":
			s.Push.PhoneRoute(false)
		}
	}
	switch p.Outcome {
	case "spoke":
		log.Printf("phone: spoke into %s (%s)", p.Route, p.State)
		if s.thr != nil && p.Thread != "" {
			s.thr.Voice.MarkCard(p.Thread, s.Push.LineCard(p.Thread, p.Line), now.Add(notify.PhoneSpeakTime(p.Line)))
		}
		if s.Push != nil {
			// The line was heard: its voice_queue row closes, so a hub
			// restart does not speak it again (notify/queue.go).
			s.Push.Heard(p.Thread, p.Line)
		}
		return
	case "stopped":
		log.Printf("phone: stopped, route left headphones for %s (%s)", p.Route, p.State)
	case "finished":
		// The line's real end (Speak.swift finished): the mark `spoke` set
		// for a reckoned length ends here, not when the estimate runs out.
		log.Printf("phone: finished the line into %s (%s)", p.Route, p.State)
	case "stale":
		// iOS delivered the push late and the phone left it unspoken (a late
		// line can pull the headphones off another device mid-use).
		log.Printf("phone: stale push left unspoken, route %s (%s)", p.Route, p.State)
	default:
		log.Printf("phone: silent, route %s (%s)", p.Route, p.State)
	}
	if s.thr != nil && p.Thread != "" {
		s.thr.Voice.MarkCard(p.Thread, "", now)
	}
}

// postObsBatch: one write for a whole window of a connector's data (the app's
// Health sync sends a day's metrics, or years of backfill, in chunks). Rows
// carrying a uniq_key the store already has are skipped, not duplicated, so a
// re-sync is free.
func (s *Server) postObsBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string            `json:"source"` // default for items that omit it
		Items  []obs.Observation `json:"items"`
	}
	if !decode(w, r, &body, 32<<20) {
		return
	}
	if len(body.Items) == 0 {
		jsonErr(w, 400, "items required")
		return
	}
	if len(body.Items) > 2000 {
		jsonErr(w, 400, "at most 2000 items per batch")
		return
	}
	for i := range body.Items {
		if body.Items[i].Source == "" {
			body.Items[i].Source = body.Source
		}
	}
	// One transaction; the kinds' hooks run once after it (obs.Ingest).
	res, err := s.obs.Ingest(body.Items)
	if err != nil {
		jsonErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]int{"inserted": res.Inserted, "skipped": res.Skipped})
}

func (s *Server) getBlob(w http.ResponseWriter, r *http.Request) {
	p, err := s.obs.BlobPath(r.PathValue("ref"))
	if err != nil {
		jsonErr(w, 404, "no such blob")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}
