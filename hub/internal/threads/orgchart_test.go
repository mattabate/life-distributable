package threads

import (
	"slices"
	"testing"
)

// The org chart (Configuration's Team): a role's reports_to and goals are
// written like any other field, read back at the newest row, and a line that
// names no role, the role itself or one of its own reports is refused.
func TestPersonaReportsToAndGoals(t *testing.T) {
	m, _, _ := setup(t)
	m.StartTeam()
	if p, _ := m.persona(PersonaStaff); p.ReportsTo != "" || p.Goals == nil || len(p.Goals) != 0 {
		t.Fatalf("the chief of staff reports to the owner, owns nothing: %+v", p)
	}
	if _, err := m.NewPersona(Persona{Key: "coach", Role: "Coach", Who: "w", Brief: "b", ReportsTo: "nobody"}, "", "owner"); err == nil {
		t.Fatal("reports to no role")
	}
	p, err := m.NewPersona(Persona{Key: "coach", Role: "Coach", Who: "w", Brief: "b", ReportsTo: PersonaStaff, Goals: []string{" fit ", "", "fit", "sleep"}}, "", "owner")
	if err != nil || p.ReportsTo != PersonaStaff || !slices.Equal(p.Goals, []string{"fit", "sleep"}) {
		t.Fatalf("new: %+v %v", p, err)
	}
	if _, err := m.NewPersona(Persona{Key: "trainer", Role: "Trainer", Who: "w", Brief: "b", ReportsTo: "coach"}, "", "owner"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]string{{"reports_to": "coach"}, {"reports_to": "trainer"}, {"reports_to": "ghost"}} {
		if _, err := m.EditPersona("coach", bad, "owner"); err == nil {
			t.Fatalf("coach accepted %v", bad)
		}
	}
	if _, err := m.EditPersona(PersonaStaff, map[string]string{"reports_to": "trainer"}, "owner"); err == nil {
		t.Fatal("a loop through two roles")
	}
	if p, err = m.EditPersona("coach", map[string]string{"goals": "sleep, run", "team": "Body"}, "owner"); err != nil ||
		!slices.Equal(p.Goals, []string{"sleep", "run"}) || p.ReportsTo != PersonaStaff || p.Team != "Body" {
		t.Fatalf("edit goals keeps the line: %+v %v", p, err)
	}
	if p, err = m.EditPersona("coach", map[string]string{"reports_to": "", "goals": ""}, "owner"); err != nil || p.ReportsTo != "" || len(p.Goals) != 0 {
		t.Fatalf("cleared: %+v %v", p, err)
	}
	if d, ok := m.PersonaDef("trainer"); !ok || d.ReportsTo != "coach" {
		t.Fatalf("trainer: %+v", d)
	}
}
