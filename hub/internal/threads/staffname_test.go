package threads

import (
	"strings"
	"testing"
)

func hellos(t *testing.T, m *Manager) int {
	t.Helper()
	ms, err := m.Messages(StepThread, 50)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, msg := range ms {
		if msg.Text == staffHello {
			n++
		}
	}
	return n
}

// The chief of staff starts as Herbert, not "Calendar", and its first
// message says the name is the owner's to change (#260).
func TestTheChiefOfStaffStartsAsHerbert(t *testing.T) {
	m, _, _ := setup(t)
	m.StartTeam()
	got, err := m.Get(StepThread)
	if err != nil || got.Title != StaffName {
		t.Fatalf("title %q %v", got.Title, err)
	}
	if hellos(t, m) != 1 || !strings.Contains(staffHello, "ask me") {
		t.Fatalf("one hello that offers a rename: %d", hellos(t, m))
	}
	m.StartTeam()
	if hellos(t, m) != 1 {
		t.Fatalf("a restart says hello again: %d", hellos(t, m))
	}
	if p := m.personaWith(got, nil); !strings.Contains(p, "Herbert, the owner's chief of staff") || !strings.Contains(p, "lifectl thread me title") {
		t.Fatalf("its brief: %s", p)
	}
}

// An install from before #260 has a session named "Calendar": the next start
// names it Herbert and says hello once. A name the owner gave it stays.
func TestACalendarSessionBecomesHerbertOnce(t *testing.T) {
	m, _, _ := setup(t)
	m.CreateIdle(StepThread, "Calendar", "life")
	m.db.Exec(`DELETE FROM thread_messages WHERE text=?`, staffHello)
	m.StartTeam()
	if got, _ := m.Get(StepThread); got.Title != StaffName || hellos(t, m) != 1 {
		t.Fatalf("title %q, hellos %d", got.Title, hellos(t, m))
	}
	m.Update(StepThread, map[string]string{"title": "Dana"})
	m.StartTeam()
	if got, _ := m.Get(StepThread); got.Title != "Dana" || hellos(t, m) != 1 {
		t.Fatalf("the owner's name holds: %q, hellos %d", got.Title, hellos(t, m))
	}
}
