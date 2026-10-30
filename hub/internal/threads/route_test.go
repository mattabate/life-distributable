package threads

import (
	"strings"
	"testing"
)

// A message opened to a role by name goes to that role's session with no
// model asked. A name later in the message is about the role, and the router
// decides.
func TestAMessageOpenedByNameGoesToThatRole(t *testing.T) {
	m, _, _ := setup(t)
	m.TeamMode = true
	if _, err := m.CreateIdle(StepThread, "Calendar", "life"); err != nil {
		t.Fatal(err)
	}
	m.Update(StepThread, map[string]string{"title": "Dana"})
	asked := 0
	m.Pick = nil
	m.Local = func(string, []RouteLane, []string) (string, error) { asked++; return StepThread + " | calendar", nil }
	for _, msg := range []string{
		"Hey, Dana, what is this charge?",
		"Uh, dana, um, so here's the deal.",
		"Hey Dana is this right?",
		"[Screenshot of the app]\n\nUm Dana, what happened?",
		"Dana: move my haircut.",
	} {
		if id, why := m.Route(msg); id != StepThread || !strings.Contains(why, "by name") {
			t.Fatalf("%q -> %s (%s)", msg, id, why)
		}
	}
	if asked != 0 {
		t.Fatalf("the model was asked %d times for named messages", asked)
	}
	for _, msg := range []string{"Something seems odd with Dana.", "Dana said the rent is due.", "Peter, it looks like 3 things.", "So I noticed Dana, again."} {
		if _, why := m.Route(msg); strings.Contains(why, "by name") {
			t.Fatalf("%q was taken as addressed: %s", msg, why)
		}
	}
	if asked != 4 {
		t.Fatalf("asked=%d: an unaddressed message goes to the model", asked)
	}
}
