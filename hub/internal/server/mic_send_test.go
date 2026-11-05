package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/obs"
)

// The micStub again, but its audio second 0 is NOW (the send reads the rows of
// this take by time).
const micStubNow = `#!/bin/sh
echo '{"k":"warm"}'
read go || exit 0
echo "{\"k\":\"ready\",\"at\":$(date +%s)000,\"device\":\"Test Mic\"}"
echo '{"k":"final","text":"why is this bar red","t0":0.1,"t1":0.9}'
cat >/dev/null
echo '{"k":"final","text":"fix it","t0":1.0,"t1":1.4}'
exit 0
`

// The tap off away from Sessions starts ONE session with the words that went
// into no box; a phrase that went into a box is left out; a second off sends
// nothing; an off without `send` (the Sessions page, the idle switch, a
// restart) never starts one.
func TestMicOffAwayFromSessionsSendsOnce(t *testing.T) {
	s := newTest(t)
	m := s.micStore()
	stub := filepath.Join(t.TempDir(), "mic")
	if err := os.WriteFile(stub, []byte(micStubNow), 0o755); err != nil {
		t.Fatal(err)
	}
	m.bin = stub
	m.onFile = filepath.Join(t.TempDir(), "mic.on")
	t.Cleanup(s.micShutdown)

	var v micView
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), &v)
	if v.OriginMS == 0 {
		t.Fatalf("no origin on: %+v", v)
	}
	origin := v.OriginMS
	// The switch file carries the origin, so a restart keeps the take whole.
	if sw, was := s.micWasOn(m); !was || sw.origin.UnixMilli() != origin {
		t.Fatalf("remembered origin: %+v %v (want %d)", sw, was, origin)
	}
	// A phrase that went into a composer box on another page: the owner sends it.
	s.micRow(time.Now(), "", "cal-step", "Test Mic", "put this in the step", 0.2, 0.5, nil)
	micWait(t, s, func(v micView) bool { return v.Segments == 1 })
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false, "origin_ms": origin, "send": map[string]any{"page": "#/goals"}}).Body.Bytes(), &v)
	if v.Sent == "" || v.On {
		t.Fatalf("off on Goals sent nothing: %+v", v)
	}
	th, err := s.thr.Get(v.Sent)
	if err != nil || !strings.HasPrefix(th.Title, "why is this bar red") {
		t.Fatalf("session: %+v %v", th, err)
	}
	ms, _ := s.thr.Messages(v.Sent, 10)
	body, _ := json.Marshal(ms)
	if !strings.Contains(string(body), "why is this bar red fix it") || !strings.Contains(string(body), "Goals page") || strings.Contains(string(body), "put this in the step") {
		t.Fatalf("message: %s", body)
	}
	// A double tap: nothing new since the send.
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false, "origin_ms": origin, "send": map[string]any{"page": "#/goals"}}).Body.Bytes(), &v)
	if v.Sent != "" {
		t.Fatalf("second off sent again: %+v", v)
	}
	// Off with no send (the Sessions page): a take, rows, no session.
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), new(micView))
	micWait(t, s, func(v micView) bool { return v.Segments == 1 })
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false}).Body.Bytes(), &v)
	if v.Sent != "" {
		t.Fatalf("off on Sessions sent: %+v", v)
	}
	// The box phrase is a row all the same: the database keeps every word.
	rows, _ := s.obs.List(obs.Query{Source: "console", Kind: "speech"})
	boxed := 0
	for _, o := range rows {
		if strings.Contains(string(o.Payload), `"box":"cal-step"`) {
			boxed++
		}
	}
	if boxed != 1 {
		t.Fatalf("boxed rows: %d of %d", boxed, len(rows))
	}
}
