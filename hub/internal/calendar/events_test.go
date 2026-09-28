package calendar

import (
	"strings"
	"testing"
	"time"
)

// Every state move is appended to item_events by the one writer, and a Reopen
// adds a line instead of erasing the close (review-primitives step 4).
func TestReopenAppendsToTheTrail(t *testing.T) {
	c, _, _ := newTest(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	c.Now = func() time.Time { return now }
	it, err := c.Add(Item{Title: "Water the plants", Day: "2026-09-03", Kind: "owner", Source: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	c.Tick()
	if _, err := c.Resolve(it.ID, "done", "hub", "evidence landed"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(it.ID, "scheduled", "owner", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := c.db.Query(`SELECT actor, from_state, to_state, note FROM item_events WHERE item_id=? ORDER BY id`, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var by, from, to, note string
		rows.Scan(&by, &from, &to, &note)
		got = append(got, by+":"+from+">"+to+":"+note)
	}
	want := []string{"owner:>scheduled:", "hub:scheduled>open:fired", "hub:open>done:evidence landed", "owner:done>scheduled:"}
	if len(got) != len(want) {
		t.Fatalf("trail %q, want %q", got, want)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("trail %q, want %q", got, want)
		}
	}
	if re, _ := c.Get(it.ID); re.State != "scheduled" || re.ResolvedBy != "" {
		t.Fatalf("reopened row: %+v", re)
	}
}
