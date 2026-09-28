package attention

import (
	"strings"
	"testing"

	"life/hub/internal/threads"
)

// A session whose card is in the owner's ears right now leads with
// "speaking": the voice just heard has a name on the list, whichever
// heading it is under.
func TestSpeakingPillLeads(t *testing.T) {
	ths := []threads.Thread{{ID: "run", Status: "running", Speaking: true}, {ID: "read", Status: "needs_you", Speaking: true, WaitingToSpeak: true}, {ID: "quiet", Status: "needs_you"}, {ID: "held", Status: "needs_you", WaitingToSpeak: true}}
	asks := []threads.Ask{
		{ID: "a1", ThreadID: "run", Surface: "any", State: "open", Kind: "decision"},
		{ID: "a2", ThreadID: "read", Surface: "any", State: "open", Kind: "read"},
		{ID: "a3", ThreadID: "quiet", Surface: "any", State: "open", Kind: "read"},
		{ID: "a4", ThreadID: "held", Surface: "any", State: "open", Kind: "read"},
	}
	b := Build("web", asks, nil, ths)
	pill := func(id string) string {
		var s []string
		for _, p := range b.Pills[id] {
			s = append(s, p.Word+"/"+p.Tone)
		}
		return strings.Join(s, " ")
	}
	if pill("run") != "speaking/speaking running/running 1 for you/needs" || pill("read") != "speaking/speaking 1 to read/read" || pill("quiet") != "1 to read/read" ||
		pill("held") != "waiting to speak/waiting 1 to read/read" {
		t.Fatalf("pills %v", b.Pills)
	}
}

// A session with no open card and not running is still on the list while
// its line plays (a replay of a closed card), and leaves it after.
func TestSpeakingSessionIsListed(t *testing.T) {
	ths := []threads.Thread{{ID: "replay", Status: "idle", Speaking: true}, {ID: "queued", Status: "idle", WaitingToSpeak: true}, {ID: "gone", Status: "idle"}}
	b := Build("web", nil, nil, ths)
	if b.Section["replay"] != "working" || b.Section["queued"] != "working" || b.Section["gone"] != "" {
		t.Fatalf("sections %v", b.Section)
	}
}
