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
	m.team.Store(true)
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

// With plain sessions off, a message no role claims goes to the chief of
// staff; with them on it starts a plain session, as with team mode off.
func TestAMessageNoRoleClaimsGoesToTheChiefOfStaff(t *testing.T) {
	m, _, _ := setup(t)
	m.Pick, m.Local = nil, nil
	m.StartTeam()
	if got, err := m.Get(StepThread); err != nil || got.Persona != PersonaStaff {
		t.Fatalf("the calendar session is the chief of staff's: %+v %v", got, err)
	}
	if m.CatchAll() != StepThread {
		t.Fatalf("catch-all %q", m.CatchAll())
	}
	th, routed, err := m.CreateRouted("what is on this week?", nil, "app")
	if err != nil || !routed || th.ID != StepThread {
		t.Fatalf("plain off: %s routed=%v %v", th.ID, routed, err)
	}
	m.SetPlainSessions(true)
	th, routed, err = m.CreateRouted("start something new", nil, "app")
	if err != nil || routed || th.ID == StepThread {
		t.Fatalf("plain on: %s routed=%v %v", th.ID, routed, err)
	}
	m.SetTeamMode(false)
	m.SetPlainSessions(false)
	if _, routed, _ := m.CreateRouted("team off", nil, "app"); routed {
		t.Fatal("team mode off routes nothing")
	}
}

// A role the chief of staff adds is a role at once: listed, routable, with
// its own session, and its words can be rewritten without losing the old.
func TestNewPersona(t *testing.T) {
	m, _, _ := setup(t)
	if _, err := m.NewPersona(Persona{Key: "coach", Role: "Coach", Who: "a coach", Brief: "Train."}, "", "owner"); err == nil {
		t.Fatal("team mode off: no new role")
	}
	m.StartTeam()
	for _, bad := range []Persona{{Key: "Coach", Role: "C", Who: "w", Brief: "b"}, {Key: "staff", Role: "C", Who: "w", Brief: "b"}, {Key: "coach", Role: "C"}} {
		if _, err := m.NewPersona(bad, "", "owner"); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	p, err := m.NewPersona(Persona{Key: "coach", Role: "Fitness coach", Who: "the owner's coach", Covers: "training", Brief: "Keep the plan.", Team: "Health"}, "Sam", "owner")
	if err != nil || p.Session == "" || p.Builtin || p.Lane == "" {
		t.Fatalf("new: %+v %v", p, err)
	}
	if th, _ := m.Get(p.Session); th.Persona != "coach" || th.Title != "Sam" {
		t.Fatalf("its session: %+v", th)
	}
	defs := m.PersonaDefs()
	if len(defs) != 2 || defs[0].Key != PersonaStaff || defs[1].Key != "coach" {
		t.Fatalf("defs: %+v", defs)
	}
	if p, err = m.EditPersona("coach", map[string]string{"team": "Body"}, "owner"); err != nil || p.Team != "Body" || p.Role != "Fitness coach" {
		t.Fatalf("edit: %+v %v", p, err)
	}
	if _, err := m.EditPersona("coach", map[string]string{"brief": " "}, "owner"); err == nil {
		t.Fatal("emptied the brief")
	}
	if _, err := m.EditPersona(PersonaStaff, map[string]string{"team": "Office"}, "owner"); err != nil || m.RoleOf(PersonaStaff) != "Chief of staff" {
		t.Fatalf("a built-in role takes a team: %v", err)
	}
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM persona_defs WHERE key='coach'`).Scan(&n)
	if n != 2 {
		t.Fatalf("persona_defs is append-only: %d rows", n)
	}
}
