package notify

import "testing"

type needOnly struct{ needs []string }

func (n *needOnly) NeedsYou(s string) error { n.needs = append(n.needs, s); return nil }

type withRead struct {
	needOnly
	reads []string
}

func (n *withRead) ToRead(s string) error { n.reads = append(n.reads, s); return nil }

type withCard struct {
	withRead
	cards []string
}

func (n *withCard) Card(kind, line, say, thread, card string) error {
	n.cards = append(n.cards, kind+"|"+line+"|"+say+"|"+thread+"|"+card)
	return nil
}

// One funnel: the card lane when the notifier has it, else To read for a
// read card, else NEEDS YOU — and a read card never buzzes as NEEDS YOU.
func TestCardPicksOneLane(t *testing.T) {
	c := &withCard{}
	if said, _ := Card(c, "needs_you", "line", "Hey Alex.", "th-1", "cal-1"); said != "Hey Alex." || len(c.cards) != 1 || len(c.needs) != 0 {
		t.Fatalf("card lane: said=%q %+v", said, c)
	}
	if c.cards[0] != "needs_you|line|Hey Alex.|th-1|cal-1" {
		t.Fatalf("card lane args: %q", c.cards[0])
	}
	r := &withRead{}
	if said, _ := Card(r, "read", "line", "Hey Alex.", "th-1", "ask-1"); said != "" || len(r.reads) != 1 || len(r.needs) != 0 {
		t.Fatalf("read lane: said=%q %+v", said, r)
	}
	if said, _ := Card(r, "approval", "line", "Hey Alex.", "th-1", "act-1"); said != "" || len(r.needs) != 1 {
		t.Fatalf("needs lane: said=%q %+v", said, r)
	}
	n := &needOnly{}
	if Card(n, "read", "line", "", "", ""); len(n.needs) != 0 {
		t.Fatalf("a read card buzzed as NEEDS YOU: %+v", n)
	}
	if said, err := Card(nil, "needs_you", "line", "x", "", ""); said != "" || err != nil {
		t.Fatalf("nil notifier: %q %v", said, err)
	}
}

func TestSpokenSentences(t *testing.T) {
	for _, c := range []struct{ kind, class, title, thread, want string }{
		{"approval", "", "Buy VEA", "", "Hey, I need your approval. Buy VEA."},
		{"approval", "", "", "", "Hey, something is waiting for your approval."},
		{"read", "", "Done!", "Money", "Hey, about Money. Done!"},
		{"needs_you", "step", "Water the plants", "Calendar", "Hey, a reminder. Water the plants."},
		{"error", "", "Crashed", "Build", "Hey, Build stopped on an error. Crashed."},
		{"needs_you", "", "Pick one", "Build", "Hey, I need you on Build. Pick one."},
		{"needs_you", "", "Pick one", "", "Hey, Pick one."},
	} {
		if got := Spoken(c.kind, c.class, c.title, c.thread); got != c.want {
			t.Errorf("Spoken(%q,%q,%q,%q) = %q, want %q", c.kind, c.class, c.title, c.thread, got, c.want)
		}
	}
}
