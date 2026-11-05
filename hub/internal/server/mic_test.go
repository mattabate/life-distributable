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

// A stub helper that speaks the real line protocol (ops/mic.swift header):
// warm, then nothing until "go"; then status/ready/partial/final; on stdin
// EOF one more final (the tail), exit 0.
const micStub = `#!/bin/sh
echo '{"k":"warm"}'
read go || exit 0
[ "$go" = "go" ] || exit 4
echo '{"k":"status","text":"asking for microphone permission"}'
echo '{"k":"ready","at":1700000000000,"device":"Test Mic"}'
echo '{"k":"log","text":"output now Test Speakers 48000 Hz"}'
echo '{"k":"partial","text":"open the"}'
echo '{"k":"final","text":"open the money page","t0":0.5,"t1":1.9,"words":[[0.5,0.8,"open"],[0.8,0.9,"the"],[0.9,1.3,"money"],[1.3,1.9,"page"]]}'
cat >/dev/null
echo '{"k":"final","text":"then stop","t0":2.0,"t1":2.6,"words":[[2.0,2.3,"then"],[2.3,2.6,"stop"]]}'
exit 0
`

func micStubAt(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "mic")
	if err := os.WriteFile(stub, []byte(micStub), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func micWait(t *testing.T, s *Server, ok func(micView) bool) micView {
	t.Helper()
	var v micView
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// A fresh struct each poll: omitempty fields would otherwise linger.
		v = micView{}
		json.Unmarshal(s.do(t, "GET", "/api/v1/mic", nil).Body.Bytes(), &v)
		if ok(v) {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mic never reached the state wanted: %+v", v)
	return v
}

// The mic switch end to end: on runs the stub and says go, its partial shows
// in `volatile`, its final becomes text AND a console/speech row with words
// pinned to wall clock by the ready line; off closes stdin, the stub's tail
// lands, on=false.
func TestMicSwitchFilesSpeechRows(t *testing.T) {
	s := newTest(t)
	s.micStore().bin = micStubAt(t)
	t.Cleanup(s.micShutdown)

	if w := s.do(t, "GET", "/api/v1/mic", nil); w.Code != 200 {
		t.Fatalf("GET mic: %d %s", w.Code, w.Body)
	}
	w := s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true, "thread_id": "t-1"})
	if w.Code != 200 {
		t.Fatalf("POST on: %d %s", w.Code, w.Body)
	}
	v := micWait(t, s, func(v micView) bool { return v.Segments == 1 })
	if !v.On || v.Text != "open the money page" || v.Device != "Test Mic" || v.ThreadID != "t-1" || v.Status != "listening" {
		t.Fatalf("after first final: %+v", v)
	}
	// Off: the stub flushes one more final on stdin EOF, then exits; the POST
	// answers with the whole take.
	w = s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false})
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.On || v.Segments != 2 || v.Text != "open the money page then stop" {
		t.Fatalf("after off: %d %+v", w.Code, v)
	}

	rows, err := s.obs.List(obs.Query{Source: "console", Kind: "speech", Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatalf("speech rows: %d %v", len(rows), err)
	}
	// Newest first: the second final, then the first.
	var p struct {
		Text    string  `json:"text"`
		Started string  `json:"started"`
		Ended   string  `json:"ended"`
		Words   [][]any `json:"words"`
		Thread  string  `json:"thread_id"`
	}
	json.Unmarshal(rows[1].Payload, &p)
	if p.Text != "open the money page" || p.Thread != "t-1" || len(p.Words) != 4 {
		t.Fatalf("first row: %+v", p)
	}
	// ready at = 1700000000000 ms; t0 0.5 s → started 1700000000500.
	if !rows[1].TS.Equal(time.UnixMilli(1700000000500)) {
		t.Fatalf("first row ts: %v", rows[1].TS)
	}
	if p.Started != time.UnixMilli(1700000000500).UTC().Format(time.RFC3339Nano) || p.Ended != time.UnixMilli(1700000001900).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("first row times: %s → %s", p.Started, p.Ended)
	}
	// Word times count from the row's own start: "open" at 0, "the" at 300 ms.
	if w0, _ := p.Words[0][0].(float64); w0 != 0 || p.Words[0][2] != "open" {
		t.Fatalf("first word: %v", p.Words[0])
	}
	if w1, _ := p.Words[1][0].(float64); w1 != 300 || p.Words[1][2] != "the" {
		t.Fatalf("second word: %v", p.Words[1])
	}
	if rows[1].UniqKey != "speech:1700000000500" {
		t.Fatalf("uniq_key: %q", rows[1].UniqKey)
	}
	// Off when already off is a no-op, and on again starts a fresh take.
	if w := s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false}); w.Code != 200 {
		t.Fatalf("second off: %d", w.Code)
	}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), &v)
	if !v.On || v.Text != "" || v.Segments != 0 {
		t.Fatalf("second take: %+v", v)
	}
	s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false})
}

// With keepWarm the hub holds a helper ready before the tap and spawns the
// next one after every off, so a tap finds the model loaded; the warm helper
// has said "warm" and is not "on".
func TestMicKeepsAHelperWarm(t *testing.T) {
	s := newTest(t)
	m := s.micStore()
	m.bin = micStubAt(t)
	m.keepWarm = true
	t.Cleanup(s.micShutdown)
	s.micWarmUp(m)
	v := micWait(t, s, func(v micView) bool { return v.Warm })
	if v.On || v.Status != "" {
		t.Fatalf("warm: %+v", v)
	}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), &v)
	if !v.On || v.Warm || v.Status != "starting" {
		t.Fatalf("on from warm: %+v", v)
	}
	micWait(t, s, func(v micView) bool { return v.Segments == 1 })
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false}).Body.Bytes(), &v)
	if v.On || v.Segments != 2 {
		t.Fatalf("off: %+v", v)
	}
	// The next helper is already warming.
	micWait(t, s, func(v micView) bool { return v.Warm })
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), &v)
	if !v.On || v.Text != "" {
		t.Fatalf("second take from warm: %+v", v)
	}
	s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false})
}

// A take that hears nothing for the idle span switches itself off, with the
// reason in `error` for the console to toast; the stub's tail still lands and
// the next helper warms up as after any off.
func TestMicIdleSwitchesOff(t *testing.T) {
	s := newTest(t)
	m := s.micStore()
	m.bin = micStubAt(t)
	m.idle = 300 * time.Millisecond
	t.Cleanup(s.micShutdown)
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), new(micView))
	// ≥1, not ==1: on a loaded machine the idle span can pass between two
	// polls, and the take is already at 2 when this looks.
	micWait(t, s, func(v micView) bool { return v.Segments >= 1 })
	v := micWait(t, s, func(v micView) bool { return !v.On && v.Segments == 2 })
	if !strings.Contains(v.Error, "mic off by itself") {
		t.Fatalf("idle off: %+v", v)
	}
	// A new take clears the reason (fresh struct: omitempty would keep it).
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true}).Body.Bytes(), &v)
	if !v.On || v.Error != "" {
		t.Fatalf("take after idle off: %+v", v)
	}
	s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false})
}

// The mic is a sensor the owner's switch owns: `on: true` while on
// moves the take to the box named now, so the next row carries that thread;
// the switch is remembered beside the DB while on and forgotten at off; a
// hub that boots and finds it remembered switches on again with that thread.
func TestMicRetargetsAndSurvivesRestart(t *testing.T) {
	s := newTest(t)
	m := s.micStore()
	m.bin = micStubAt(t)
	m.onFile = filepath.Join(t.TempDir(), "mic.on")
	t.Cleanup(s.micShutdown)

	var v micView
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true, "thread_id": ""}).Body.Bytes(), &v)
	if !v.On || v.ThreadID != "" {
		t.Fatalf("on with no box: %+v", v)
	}
	if sw, was := s.micWasOn(m); !was || sw.thread != "" {
		t.Fatalf("remembered while on (no thread): %+v %v", sw, was)
	}
	micWait(t, s, func(v micView) bool { return v.Segments == 1 })
	// The caret moves into a session's box: same take, new thread.
	seq := v.Seq
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true, "thread_id": "t-2"}).Body.Bytes(), &v)
	if !v.On || v.ThreadID != "t-2" || v.Seq <= seq || v.Text != "open the money page" {
		t.Fatalf("retarget: %+v", v)
	}
	if sw, _ := s.micWasOn(m); sw.thread != "t-2" {
		t.Fatalf("remembered thread: %+v", sw)
	}
	v = micView{}
	json.Unmarshal(s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false}).Body.Bytes(), &v)
	if v.On || v.Segments != 2 {
		t.Fatalf("off: %+v", v)
	}
	rows, _ := s.obs.List(obs.Query{Source: "console", Kind: "speech", Limit: 10})
	if len(rows) != 2 {
		t.Fatalf("speech rows: %d", len(rows))
	}
	// Newest first: the tail landed after the retarget and carries t-2; the
	// first phrase, before it, carries no thread.
	var p0, p1 map[string]any
	json.Unmarshal(rows[0].Payload, &p0)
	json.Unmarshal(rows[1].Payload, &p1)
	if p0["thread_id"] != "t-2" || p1["thread_id"] != nil {
		t.Fatalf("threads on rows: %v / %v", p0["thread_id"], p1["thread_id"])
	}
	if _, err := os.Stat(m.onFile); !os.IsNotExist(err) {
		t.Fatalf("still remembered after off: %v", err)
	}

	// The last hub stopped with the switch on: this one puts it back.
	os.WriteFile(m.onFile, []byte("t-3\n"), 0o600)
	sw, was := s.micWasOn(m)
	if !was || sw.thread != "t-3" {
		t.Fatalf("was on: %+v %v", sw, was)
	}
	s.micResume(m, sw)
	v = micWait(t, s, func(v micView) bool { return v.On && v.Segments == 1 })
	if v.ThreadID != "t-3" {
		t.Fatalf("resumed: %+v", v)
	}
	s.do(t, "POST", "/api/v1/mic", map[string]any{"on": false})
}

// A helper that cannot start is a 503 with the reason, and the switch stays off.
func TestMicHelperMissing(t *testing.T) {
	s := newTest(t)
	s.micStore().bin = filepath.Join(t.TempDir(), "nope")
	w := s.do(t, "POST", "/api/v1/mic", map[string]any{"on": true})
	var v micView
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 503 || v.On || v.Error == "" {
		t.Fatalf("missing helper: %d %+v", w.Code, v)
	}
}
