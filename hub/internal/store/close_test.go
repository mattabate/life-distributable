package store

import "testing"

func TestOwnerIsTheOneActorSet(t *testing.T) {
	for _, by := range []string{"owner", "app", "web", "cli"} {
		if !Owner(by) {
			t.Errorf("Owner(%q) = false", by)
		}
	}
	for _, by := range []string{"", "hub", "auto", "ask", "session", "gate", "Alex"} {
		if Owner(by) {
			t.Errorf("Owner(%q) = true", by)
		}
	}
}

// The standings reproduce the state lists both clients kept before step 7
// deleted them (ui.js askHTML/actionHTML/recHTML, threads.js activeAsk,
// calgrid.js CAL_WONT; AskCard.closed, ApprovalCard.isOpen, RecCard.open).
func TestStandingIsTheClientsOldLists(t *testing.T) {
	for _, c := range []struct {
		kind, class, state, res, by string
		chore                       bool
		want                        Standing
	}{
		{"physical", "step", "open", "", "", true, Standing{Open: true, Lane: "chores"}},
		{"physical", "step", "done", "", "hub", true, Standing{Closed: true, Lane: "chores"}},
		{"decision", "", "open", "", "", false, Standing{Open: true, Lane: "mine"}},
		{"decision", "", "answered", "", "", false, Standing{Lane: "mine"}},
		{"decision", "", "done", "Read it", "app", false, Standing{Closed: true, Lane: "mine"}},
		{"read", "", "done", "Read it", "web", false, Standing{Closed: true, Folded: "read", Lane: "mine"}},
		{"read", "", "done", "thanks", "claude:thread:t", false, Standing{Closed: true, Lane: "agents"}},
		{"read", "", "dismissed", "", "app", false, Standing{Closed: true, Folded: "dismissed", Lane: "mine"}},
		{"install", "", "superseded", "", "claude:thread:t", false, Standing{Closed: true, Lane: "agents"}},
		{"physical", "practice", "done", "", "app", false, Standing{Closed: true, Lane: "homework"}},
	} {
		if got := AskStanding(c.kind, c.class, c.state, c.res, c.by, c.chore); got != c.want {
			t.Errorf("AskStanding(%s,%s,%s,%q,%s) = %+v, want %+v", c.kind, c.class, c.state, c.res, c.by, got, c.want)
		}
	}
	for st, want := range map[string]Standing{
		"proposed":  {Open: true, Lane: "mine"},
		"approved":  {Closed: true, Lane: "mine"},
		"dismissed": {Closed: true, Folded: "dismissed", Lane: "mine"},
	} {
		if got := ActionStanding(st, "web"); got != want {
			t.Errorf("ActionStanding(%s) = %+v, want %+v", st, got, want)
		}
	}
	if got := ActionStanding("executed", "gate"); got.Lane != "agents" {
		t.Errorf("a gate-run action is the agents' record, got %+v", got)
	}
	for _, c := range []struct {
		status, note string
		want         Standing
	}{
		{"proposed", "", Standing{Open: true, Lane: "recs"}},
		{"deferred", "", Standing{Lane: "recs"}},
		{"accepted", "", Standing{Closed: true, Lane: "recs"}},
		{"expired", RecDismissed, Standing{Closed: true, Folded: "dismissed", Lane: "recs"}},
		{"expired", "", Standing{Closed: true, Lane: "recs"}},
	} {
		if got := RecStanding(c.status, c.note); got != c.want {
			t.Errorf("RecStanding(%s,%q) = %+v, want %+v", c.status, c.note, got, c.want)
		}
	}
	for _, c := range []struct {
		state string
		did   bool
		lane  string
		want  string
	}{
		{"dismissed", true, "mine", "wont"}, {"denied", true, "mine", "wont"}, {"failed", true, "agents", "wont"},
		{"expired", false, "recs", "wont"}, {"declined", true, "recs", "wont"},
		{"done", false, "agents", "done"}, {"accepted", false, "recs", "done"}, {"approved", true, "mine", "done"},
		{"scheduled", false, "mine", "todo"}, {"scheduled", false, "homework", "todo"}, {"fired", false, "chores", "todo"},
		{"scheduled", false, "scheduled", ""}, {"proposed", false, "recs", ""},
	} {
		if got := CalMark(c.state, c.did, c.lane); got != c.want {
			t.Errorf("CalMark(%s,%v,%s) = %q, want %q", c.state, c.did, c.lane, got, c.want)
		}
	}
}

// The one vocabulary reproduces, word for word, what the console printed from
// its own tables before they were deleted (2026-09-26).
func TestReplyLabelIsTheConsolesWords(t *testing.T) {
	for _, c := range []struct{ ref, outcome, askKind, want string }{
		{"ask", "done", "read", "Read it"},
		{"ask", "", "read", "Replied"}, // a read has no "" button
		{"ask", "wont", "access", "Won't grant"},
		{"ask", "", "decision", "Reply"},
		{"ask", "done", "", "Done"}, // the ask is gone
		{"rec", "accepted", "", "Accepted"},
		{"rec", "deferred", "", "Later"},
		{"rec", "", "", "Replied"},
		{"action", "approved", "", "Approved"},
		{"action", "", "", "Replied"},
		{"cal", "done", "", "Did it"},
		{"cal", "", "", "About"},
		{"message", "", "", "Replied"},
	} {
		if got := ReplyLabel(c.ref, c.outcome, c.askKind); got != c.want {
			t.Errorf("ReplyLabel(%q,%q,%q) = %q, want %q", c.ref, c.outcome, c.askKind, got, c.want)
		}
	}
}

func TestOutcomeSets(t *testing.T) {
	if o := TickOutcomes(true); len(o) != 3 || o[0].Label != "Did it" || o[1].Value != "wont" || o[2].Label != "Send" {
		t.Errorf("tick: %+v", o)
	}
	if o := TickOutcomes(false); len(o) != 1 || o[0].Value != "" {
		t.Errorf("a done tick only talks: %+v", o)
	}
	if o := ActionOutcomes(false); len(o) != 2 {
		t.Errorf("a job's proposal has no Reply: %+v", o)
	}
	if AskVerb("access") != "grant" || AskVerb("bogus") != "for you" {
		t.Error("ask verbs")
	}
	if AskDidVerb("read", "dismissed") != "Read" || AskDidVerb("install", "dismissed") != "Skipped" {
		t.Error("ask did verbs")
	}
}
