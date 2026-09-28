package goals

import (
	"path/filepath"
	"testing"

	"life/hub/internal/store"
)

func TestGoalsCRUD(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Create(Goal{Title: "Make more money!", Statement: "grow income", Sources: "finance"})
	if err != nil || g.ID != "make-more-money" || g.Cadence != "weekly" {
		t.Fatal(g, err)
	}
	if _, err := s.Create(Goal{Title: "make more money"}); err == nil {
		t.Fatal("dup accepted")
	}
	if _, err := s.Create(Goal{Title: "x", Horizon: "decade"}); err == nil {
		t.Fatal("bad horizon accepted")
	}
	if _, err := s.Update(g.ID, map[string]string{"status": "paused"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(g.ID, map[string]string{"id": "nope"}); err == nil {
		t.Fatal("id update accepted")
	}
	if _, err := s.AddNote(g.ID, "claude:goal-review", "review", "looked at things"); err != nil {
		t.Fatal(err)
	}
	g2, _ := s.Get(g.ID)
	if g2.Status != "paused" || g2.NoteCount != 1 || g2.LastReviewedAt == nil {
		t.Fatalf("%+v", g2)
	}
	act, _ := s.List("active")
	all, _ := s.List("")
	if len(act) != 0 || len(all) != 1 {
		t.Fatal(act, all)
	}
	ns, _ := s.Notes(g.ID, 10)
	if len(ns) != 1 || ns[0].Kind != "review" || ns[0].By != "goal review" {
		t.Fatal(ns)
	}
}

// The by-line is words, never a machine author.
func TestByLine(t *testing.T) {
	for _, c := range []struct{ author, title, by, id string }{
		{"owner", "", "You", ""},
		{"claude", "", "Claude", ""},
		{"claude:goal-review", "", "goal review", ""},
		{"claude:thread:abc-1234", "Packs check-in", "Packs check-in", "abc-1234"},
		{"claude:thread:abc-1234", "", "abc-1234", ""},
		{"claude:thread:calendar", "", "calendar", ""},
	} {
		by, id := byLine(c.author, c.title)
		if by != c.by || id != c.id {
			t.Errorf("%s/%q → %q %q, want %q %q", c.author, c.title, by, id, c.by, c.id)
		}
	}
}

// TestEmblem: the seven goals of 2026-09-25 each get their own symbol, the
// stems match whole words ("smarter" is learn, never art), and a goal no stem
// fits is a target with a stable hashed hue.
func TestEmblem(t *testing.T) {
	want := map[string]string{
		"make-me-healthier":             "health",
		"make-more-money":               "money",
		"build-a-compelling-life-agent": "agent",
		"commercialize-the-life-agent":  "market",
		"grow-my-audience":              "audience",
		"get-smarter":                   "learn",
		"make-more-art":                 "art",
	}
	for id, sym := range want {
		if e := EmblemFor(id, ""); e.Symbol != sym || e.Hue < 0 || e.Hue > 359 {
			t.Errorf("%s: got %+v, want %s", id, e, sym)
		}
	}
	a, b := EmblemFor("plant-a-garden", ""), EmblemFor("plant-a-garden", "Plant a garden")
	if a.Symbol != "goal" || a != b || a.Hue < 0 || a.Hue > 359 {
		t.Errorf("fallback: %+v %+v", a, b)
	}
	db, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	s, _ := New(db)
	g, err := s.Create(Goal{Title: "Get smarter"})
	if err != nil || g.Emblem.Symbol != "learn" {
		t.Fatalf("created goal carries no emblem: %+v %v", g, err)
	}
}
