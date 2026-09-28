package threads

import (
	"os"
	"strings"
	"testing"
)

// Tasks move between sessions only on an approved card. An approved relay
// lands in the other session as the sending agent's words — framed, never as
// the owner's — and the direct road stays shut.
func TestRelayDeliversOnlyWithAnApprovedAction(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("Site renderer", "life", "", "Render the page.", "", "", nil)
	b, _ := m.Create("Site repo", "life", "", "Sort the repo out.", "", "", nil)
	complete(t, m, b.ID, "Sorted.")

	if _, err := m.Queue(Prompt{Author: "claude:thread:" + a.ID, Target: b.ID, Text: "rename it"}); err == nil || !strings.Contains(err.Error(), "lifectl relay") {
		t.Fatalf("the direct road must stay shut and name the sanctioned one: %v", err)
	}
	if _, err := m.Relay(a.ID, b.ID, "rename it", ""); err == nil {
		t.Fatal("a relay with no approved action behind it was delivered")
	}
	res, err := m.Relay(a.ID, b.ID, "Alex wants it called Page Builder.", "act-1")
	if err != nil || !strings.Contains(res, b.ID) {
		t.Fatal(res, err)
	}
	msgs, _ := m.Messages(b.ID, 50)
	last := msgs[len(msgs)-1]
	if last.Role != "system" || last.Kind != "relay" || last.Author != "claude:thread:"+a.ID {
		t.Fatalf("%+v", last)
	}
	pb, _ := os.ReadFile(promptFile(t, m, b.ID))
	for _, want := range []string{"Handed to you from another session", `"Site renderer"`, "action act-1", "not the owner's", "lifectl relay " + a.ID, "Page Builder"} {
		if !strings.Contains(string(pb), want) {
			t.Fatalf("missing %q in:\n%s", want, pb)
		}
	}
	if strings.Contains(string(pb), "[scheduled check-in]") {
		t.Fatal("a relay is not a check-in")
	}
	ps, _ := m.ListPrompts("delivered", b.ID, 10)
	if len(ps) == 0 || ps[0].Author != "claude:thread:"+a.ID {
		t.Fatalf("the hand-off is a prompts row like every other wake: %+v", ps)
	}

	if _, err := m.RelayTarget("nope"); err == nil {
		t.Fatal("a session that does not exist took a relay")
	}
	clearCards(m, b.ID)
	if err := m.Archive(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RelayTarget(b.ID); err == nil {
		t.Fatal("an archived session took a relay")
	}
}

// Every turn starts knowing who else is live — and only them: reading the list
// wakes nobody, and a finished or archived session is not on it.
func TestPeersHeaderNamesLiveSessionsOnly(t *testing.T) {
	m, _, _ := setup(t)
	a, _ := m.Create("Duck the music", "life", "", "Build it.", "", "", nil)
	if h := m.peersHeader(a.ID); h != "" {
		t.Fatalf("alone, there is nothing to say: %q", h)
	}
	b, _ := m.Create("Site repo", "life", "", "Sort the repo out.", "", "", nil)
	c, _ := m.Create("Old errand", "life", "", "Do it.", "", "", nil)
	complete(t, m, c.ID, "[end]")
	m.Archive(c.ID)

	h := m.peersHeader(a.ID)
	if !strings.Contains(h, b.ID+" (working): Site repo") || strings.Contains(h, a.ID) || strings.Contains(h, c.ID) || !strings.Contains(h, "lifectl relay") {
		t.Fatal(h)
	}
	if err := m.Send(a.ID, "and test it"); err != nil {
		t.Fatal(err)
	}
	complete(t, m, a.ID, "Built.")
	if err := m.Send(a.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if pb, _ := os.ReadFile(promptFile(t, m, a.ID)); !strings.Contains(string(pb), "[Other sessions live right now") || !strings.Contains(string(pb), b.ID) {
		t.Fatal(string(pb))
	}
}
