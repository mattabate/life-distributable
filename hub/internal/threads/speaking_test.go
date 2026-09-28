package threads

import (
	"testing"
	"time"
)

// A marked session is heard until its time, then not; a mark in the past
// ends it early (playback finished). Nil-safe throughout.
func TestVoiceMarksAndExpires(t *testing.T) {
	var none *Voice
	none.Mark("x", time.Now().Add(time.Minute))
	if none.Is("x", time.Now()) || none.Live(time.Now()) != nil {
		t.Fatal("a nil voice hears nothing")
	}
	v := &Voice{}
	now := time.Now()
	v.Mark("b", now.Add(20*time.Second))
	v.Mark("a", now.Add(10*time.Second))
	if got := v.Live(now); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("live %v", got)
	}
	if got := v.Live(now.Add(15 * time.Second)); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after a lapses: %v", got)
	}
	v.Mark("b", now) // playback finished
	if v.Is("b", now) || len(v.Live(now)) != 0 {
		t.Fatal("a mark in the past ends the voice")
	}
}
