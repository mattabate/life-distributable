package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"life/hub/internal/notify"
)

// A Mac's ear and its Devices tile: the poll is the row, the Audio switch is
// a settings row (never hub.json), Stop answers whether a line was cut.
func TestVoiceEarRoutes(t *testing.T) {
	s, db := newTestDB(t)
	s.Push = notify.NewVoiceOnly(db)

	var v notify.EarView
	w := s.do(t, "GET", "/api/v1/voice/ear?device=MacBook-Air.local&route=headphones&output=AirPods&idle=4", nil)
	if json.Unmarshal(w.Body.Bytes(), &v); w.Code != 200 || !v.Ears || v.Line != nil {
		t.Fatalf("poll: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "POST", "/api/v1/voice/ear", map[string]string{"device": "macbook-air", "id": "x", "outcome": "shouted"}); w.Code != 400 {
		t.Fatalf("bad outcome: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "POST", "/api/v1/voice/ear", map[string]string{"device": "macbook-air", "id": "x", "outcome": "spoke"}); w.Code != 200 {
		t.Fatalf("a stale id: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "POST", "/api/v1/voice/stop", map[string]any{}); w.Code != 400 {
		t.Fatalf("stop with no line: %d %s", w.Code, w.Body)
	}
	if w := s.do(t, "POST", "/api/v1/voice/stop", map[string]string{"id": "ear-ended"}); w.Code != 200 || w.Body.String() != "{\"stopped\":false}\n" {
		t.Fatalf("stop: %d %q", w.Code, w.Body)
	}

	// The hub's own Mac has a tile from the start; the MacBook's comes with
	// its first poll.
	var got settingsOut
	air := func() notify.MacEars {
		for _, d := range got.Devices {
			if d.Device == "macbook-air" {
				return d
			}
		}
		return notify.MacEars{}
	}
	w = s.do(t, "GET", "/api/v1/settings", nil)
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Devices) != 2 || !got.Devices[0].Hub || air().Ears != "headphones" || air().Output != "AirPods" || !air().Audio || !air().Here {
		t.Fatalf("devices: %s", w.Body)
	}
	w = s.do(t, "PATCH", "/api/v1/settings", map[string]any{"audio_by": map[string]bool{"macbook-air": false}})
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || air().Audio || air().Device == "" {
		t.Fatalf("audio off: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(s.cfg.OpsPath("hub.json")); err == nil {
		t.Fatal("an Audio switch wrote hub.json")
	}
	if db.Setting(notify.AudioKey("macbook-air")) != "off" {
		t.Fatal("the switch is not a settings row")
	}
	w = s.do(t, "GET", "/api/v1/voice/ear?device=macbook-air&route=headphones", nil)
	if json.Unmarshal(w.Body.Bytes(), &v); v.Ears {
		t.Fatal("Audio off, yet the Mac may speak")
	}
	for _, bad := range []map[string]bool{{"": true}, {"phone": false}, {" Phone ": false}, {"phone.local": false}, {".x": true}} {
		if w := s.do(t, "PATCH", "/api/v1/settings", map[string]any{"audio_by": bad}); w.Code != 400 {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body)
		}
	}
	// A 400 on any field saves nothing, the Audio switch included.
	if w := s.do(t, "PATCH", "/api/v1/settings", map[string]any{"model_cap": "gpt-9", "audio_by": map[string]bool{"macbook-air": true}}); w.Code != 400 {
		t.Fatalf("bad cap: %d %s", w.Code, w.Body)
	}
	if db.Setting(notify.AudioKey("macbook-air")) != "off" {
		t.Fatal("a refused PATCH turned the Audio switch on")
	}
	if db.Setting(notify.AudioKey("phone")) != "" {
		t.Fatal("a refused name was saved as a Mac")
	}
	// hub.json that cannot be written saves no Audio row either.
	root := s.cfg.Root
	s.cfg.Root = t.TempDir()
	if w := s.do(t, "PATCH", "/api/v1/settings", map[string]any{"model_cap": "", "audio_by": map[string]bool{"macbook-air": true}}); w.Code != 500 {
		t.Fatalf("no hub.json: %d %s", w.Code, w.Body)
	}
	if db.Setting(notify.AudioKey("macbook-air")) != "off" {
		t.Fatal("a failed hub.json write left the Audio switch on")
	}
	s.cfg.Root = root
	// The name is the Devices card's: the host name's first label, lower case.
	if w := s.do(t, "PATCH", "/api/v1/settings", map[string]any{"audio_by": map[string]bool{"MacBook-Air.local": true}}); w.Code != 200 {
		t.Fatalf("host name: %d %s", w.Code, w.Body)
	}
	if db.Setting(notify.AudioKey("macbook-air")) == "off" {
		t.Fatal("MacBook-Air.local did not switch the macbook-air row")
	}
	// An ear names its Mac: up to 40 of a-z, 0-9 and "-", never the phone.
	for _, d := range []string{"", "phone", "Phone.local", "mac_book", "a%20b", strings.Repeat("m", 41)} {
		if w := s.do(t, "GET", "/api/v1/voice/ear?route=headphones&device="+d, nil); w.Code != 400 {
			t.Fatalf("poll as %q: %d %s", d, w.Code, w.Body)
		}
		if w := s.do(t, "POST", "/api/v1/voice/ear", map[string]string{"device": d, "id": "x", "outcome": "spoke"}); w.Code != 400 {
			t.Fatalf("report as %q: %d %s", d, w.Code, w.Body)
		}
	}
}

// An Audio row that cannot be written after hub.json was: the running hub
// still matches hub.json.
func TestAudioRowFailureKeepsLiveSettings(t *testing.T) {
	s, db := newTestDB(t)
	s.Push = notify.NewVoiceOnly(db)
	os.MkdirAll(s.cfg.OpsPath(), 0o755)
	if err := os.WriteFile(s.cfg.OpsPath("hub.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db.Close()
	cap := s.modelChoices()[1]
	w := s.do(t, "PATCH", "/api/v1/settings", map[string]any{"model_cap": cap, "audio_by": map[string]bool{"macbook-air": false}})
	if w.Code != 500 {
		t.Fatalf("closed db: %d %s", w.Code, w.Body)
	}
	if s.cfg.ModelCap != cap {
		t.Fatalf("hub.json says model_cap, the running hub does not (%s)", w.Body)
	}
}
