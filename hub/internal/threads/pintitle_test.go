package threads

import "testing"

// A fixed session keeps its name: the auto-titler never renames a CreateIdle
// row, and PinTitle restores one renamed before that held.
func TestFixedSessionKeepsItsName(t *testing.T) {
	m, _, _ := setup(t)
	if _, err := m.CreateIdle("hub", "Hub", "life"); err != nil {
		t.Fatal(err)
	}
	m.db.Exec(`UPDATE threads SET title=? WHERE id=? AND title_auto=1`, "Storage cleanup", "hub")
	if th, _ := m.Get("hub"); th.Title != "Hub" {
		t.Fatalf("auto-titler renamed the fixed session: %q", th.Title)
	}
	m.db.Exec(`UPDATE threads SET title='Storage cleanup', title_auto=1 WHERE id='hub'`)
	if err := m.PinTitle("hub", "Hub"); err != nil {
		t.Fatal(err)
	}
	m.db.Exec(`UPDATE threads SET title=? WHERE id=? AND title_auto=1`, "Something else", "hub")
	if th, _ := m.Get("hub"); th.Title != "Hub" {
		t.Fatalf("PinTitle did not hold: %q", th.Title)
	}
}
