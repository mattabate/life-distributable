package attention

import (
	"strings"
	"testing"

	"life/hub/internal/actions"
	"life/hub/internal/threads"
)

// One pile of asks and approvals, both surfaces' numbers pinned side by side.
// Neither holds anything back: a running session's cards are counted and
// drawn on both, its bundle just sinks under the idle ones. The two boards
// are the SAME board — web-surface asks (a2, a4 here) count on the phone too,
// and `surface` orders nothing. An ask the owner has answered (a5) is the
// agent's move and is on neither board.
func TestBoardBothSurfaces(t *testing.T) {
	ths := []threads.Thread{
		{ID: "t1", Title: "Gold exit", Status: "running"},
		{ID: "t2", Title: "Bank A cash", Status: "needs_you"},
		{ID: "t3", Title: "", Status: "idle"},
	}
	asks := []threads.Ask{
		{ID: "a1", ThreadID: "t1", Surface: "any", State: "open", Kind: "decision"},
		{ID: "a2", ThreadID: "t1", Surface: "web", State: "open", Kind: "access"},
		{ID: "a3", ThreadID: "t2", Surface: "any", State: "open", Kind: "physical", CalID: "cal-1", CalDay: "2026-08-26"},
		{ID: "a4", ThreadID: "t2", Surface: "web", State: "open", Kind: "access"},
		{ID: "a5", ThreadID: "t3", Surface: "any", State: "answered", Kind: "decision", ThreadTitle: "Lunch routine", Title: "Pick a day"},
		{ID: "a6", ThreadID: "t3", Surface: "any", State: "open", Kind: "error", ThreadTitle: "Lunch routine", Title: "The run crashed"},
		{ID: "a7", ThreadID: "t2", Surface: "any", State: "open", Kind: "decision"},
	}
	acts := []actions.Action{
		{ID: "x1", ThreadID: "t1", State: "proposed"},
		{ID: "x2", ThreadID: "", State: "proposed"},
		{ID: "x3", ThreadID: "t2", State: "proposed", Title: "Move $500 to savings"},
	}
	ids := func(as []threads.Ask) string {
		var s []string
		for _, a := range as {
			s = append(s, a.ID)
		}
		return strings.Join(s, " ")
	}
	sessions := func(b Board) string {
		var s []string
		for _, x := range b.Sessions {
			var a []string
			for _, y := range x.Actions {
				a = append(a, y.ID)
			}
			s = append(s, x.ID+"["+x.Title+"|"+x.First+"]:"+strings.Join(append(a, strings.Fields(ids(x.Asks))...), ","))
		}
		return strings.Join(s, " ")
	}

	web := Build("web", asks, acts, ths)
	if web.Count != 9 || web.Badges.YourTurn != 9 || web.Working != 3 {
		t.Fatalf("web count=%d working=%d", web.Count, web.Working)
	}
	if got := ids(web.Calendar); got != "a3" {
		t.Fatalf("web calendar %q", got)
	}
	// t1 is running: its bundle is still there and still counted, but sinks
	// under the sessions that have stopped and wait on the owner. Inside a
	// bundle the asks are in the order raised (a1 before a2 — the web one does
	// not jump the queue).
	// a3 is dated: on the calendar list AND a cell on its session.
	want := "[Proposed by a scheduled job|x2]:x2 t2[Bank A cash|x3]:x3,a3,a4,a7 t3[Lunch routine|a6]:a6 t1[Gold exit|x1]:x1,a1,a2"
	if got := sessions(web); got != want {
		t.Fatalf("web sessions\n got %s\nwant %s", got, want)
	}
	if !web.Sessions[3].Running || web.Sessions[1].Running {
		t.Fatalf("web running flags %+v", web.Sessions)
	}
	if web.ForYou["t1"] != 3 || web.ForYou["t2"] != 4 || web.ForYou["t3"] != 1 || web.ForYou[""] != 1 || web.First["t2"] != "x3" || web.First["t3"] != "a6" {
		t.Fatalf("web for_you=%v first=%v", web.ForYou, web.First)
	}
	// Todo is First's title: what the session card prints under its name
	// (an approval's title beats an ask's, same rule as First).
	if web.Todo["t2"] != "Move $500 to savings" || web.Todo["t3"] != "The run crashed" || web.Todo["t1"] != "" {
		t.Fatalf("web todo=%v", web.Todo)
	}
	// The phone: the same board, number for number and card for card. a2 and
	// a4 are web-surface asks and count here too; a1 rides t1's bundle even though t1 is
	// running.
	mob := Build("mobile", asks, acts, ths)
	if mob.Surface != "mobile" || mob.Count != 9 || mob.Badges.YourTurn != 9 || mob.Working != 3 {
		t.Fatalf("mobile count=%d working=%d", mob.Count, mob.Working)
	}
	if got := ids(mob.Calendar); got != "a3" {
		t.Fatalf("mobile calendar %q", got)
	}
	if got := sessions(mob); got != want {
		t.Fatalf("mobile sessions\n got %s\nwant %s", got, want)
	}
	if mob.ForYou["t1"] != 3 || mob.ForYou["t2"] != 4 || mob.ForYou["t3"] != 1 || mob.First["t2"] != "x3" {
		t.Fatalf("mobile for_you=%v first=%v", mob.ForYou, mob.First)
	}
	// Any other surface is the web one; empty inputs give empty lists, not nulls.
	e := Build("", nil, nil, nil)
	if e.Surface != "web" || e.Count != 0 || e.Sessions == nil || e.Calendar == nil || e.ForYou == nil || e.Reads == nil || e.Installs == nil {
		t.Fatalf("empty board %+v", e)
	}
}

// The owner's own dated work is not a session's turn: an ask of class
// `practice` (a daily practice) or `step` (the dentist) is listed under
// `calendar` and nowhere else — not counted, no "for you" on its session,
// not the card its row opens on — on both surfaces. Sessions is where
// something is BLOCKED on the owner; the Calendar tab counts these. And a
// row whose cards only ask the owner to install a build says so: `installs`
// equal to `for_you` draws teal, never red.
func TestHisWorkIsTheCalendarsNotASessions(t *testing.T) {
	ths := []threads.Thread{{ID: "pitch", Status: "done"}, {ID: "dentist", Status: "done"}, {ID: "movie", Status: "needs_you"}, {ID: "ship", Status: "done"}}
	asks := []threads.Ask{
		{ID: "a1", ThreadID: "calendar", Surface: "any", State: "open", Kind: "physical", Class: "practice", CalID: "cal-1", CalDay: "2026-09-12", Title: "Perfect pitch: one round"},
		{ID: "a2", ThreadID: "dentist", Surface: "any", State: "open", Kind: "physical", Class: "step", CalID: "cal-2", CalDay: "2026-09-11", Title: "Book a dentist"},
		{ID: "a3", ThreadID: "movie", Surface: "any", State: "answered", Kind: "physical", Class: "unblock", Title: "Link a billing account"},
		{ID: "a4", ThreadID: "ship", Surface: "mobile", State: "open", Kind: "install", Class: "install", Title: "Install app build 884 (tap the link)"},
		{ID: "a5", ThreadID: "movie", Surface: "any", State: "open", Kind: "physical", CalID: "cal-3", CalDay: "2026-09-12", Title: "Buy the tranche"},
		{ID: "a6", ThreadID: "movie", Surface: "any", State: "open", Kind: "physical", Class: "step", CalID: "cal-4", CalDay: "2026-09-13", Title: "Renew the domain"},
		{ID: "a7", ThreadID: "movie", Surface: "any", State: "open", Kind: "decision", Class: "unblock", Title: "Pick a billing account"},
		// Answered = the agent's move: a3 is off the count and the bundle, and
		// a step the owner replied to (a8) is off its session's row and the
		// calendar list.
		{ID: "a8", ThreadID: "movie", Surface: "any", State: "answered", Kind: "physical", Class: "step", CalID: "cal-5", CalDay: "2026-09-10", Title: "Order the new computer"},
	}
	for _, surface := range []string{"web", "mobile"} {
		b := Build(surface, asks, nil, ths)
		if b.Count != 3 || b.Badges.YourTurn != 3 {
			t.Fatalf("%s: only what blocks the owner counts: count=%d", surface, b.Count)
		}
		if b.ForYou["pitch"] != 0 || b.ForYou["calendar"] != 0 || b.ForYou["dentist"] != 0 || b.ForYou["movie"] != 2 || b.ForYou["ship"] != 1 {
			t.Fatalf("%s: for_you=%v", surface, b.ForYou)
		}
		if _, ok := b.First["dentist"]; ok || b.Todo["dentist"] != "" {
			t.Fatalf("%s: a step is not the card a session row opens on: first=%v todo=%v", surface, b.First, b.Todo)
		}
		// The calendar list: the counted dated ask first (a5, no class = it
		// unblocks a session), then the owner's own work oldest day first.
		var got []string
		for _, a := range b.Calendar {
			got = append(got, a.ID)
		}
		if strings.Join(got, " ") != "a5 a2 a1 a6" {
			t.Fatalf("%s: calendar %v", surface, got)
		}
		// A step in a LISTED session's chat rides that row, uncounted: the row
		// draws what the chat draws. It never lists a session.
		for _, s := range b.Sessions {
			if s.ID == "movie" && (len(s.Steps) != 1 || s.Steps[0].ID != "a6" || s.N != 2 || s.Asks[0].ID != "a5" || s.Asks[1].ID != "a7") {
				t.Fatalf("%s: movie's step rides its row: %+v", surface, s)
			}
		}
		if b.Open["movie"] != 3 || b.Open["ship"] != 1 || b.Open["dentist"] != 0 {
			t.Fatalf("%s: open=%v", surface, b.Open)
		}
		if b.Installs["ship"] != 1 || b.Installs["movie"] != 0 {
			t.Fatalf("%s: installs=%v", surface, b.Installs)
		}
		for _, s := range b.Sessions {
			if s.ID == "dentist" || s.ID == "calendar" {
				t.Fatalf("%s: no bundle for the owner's own work: %+v", surface, s)
			}
		}
	}
}

// `reads` says how many of a row's "N for you" only ask the owner to read, so
// a session that answered a question wears a blue "1 to read", not a red "1
// for you". A running session's read-ask counts on both surfaces, a
// web-surface one too.
func TestReadsCount(t *testing.T) {
	ths := []threads.Thread{{ID: "t1", Status: "running"}, {ID: "t2", Status: "needs_you"}}
	asks := []threads.Ask{
		{ID: "a1", ThreadID: "t1", Surface: "any", State: "open", Kind: "read"},
		{ID: "a2", ThreadID: "t1", Surface: "web", State: "open", Kind: "read"},
		{ID: "a3", ThreadID: "t2", Surface: "any", State: "open", Kind: "read"},
		{ID: "a4", ThreadID: "t2", Surface: "any", State: "open", Kind: "decision"},
	}
	web := Build("web", asks, nil, ths)
	if web.ForYou["t1"] != 2 || web.Reads["t1"] != 2 || web.ForYou["t2"] != 2 || web.Reads["t2"] != 1 {
		t.Fatalf("web for_you=%v reads=%v", web.ForYou, web.Reads)
	}
	mob := Build("mobile", asks, nil, ths)
	if mob.ForYou["t1"] != 2 || mob.Reads["t1"] != 2 || mob.ForYou["t2"] != 2 || mob.Reads["t2"] != 1 {
		t.Fatalf("mobile for_you=%v reads=%v", mob.ForYou, mob.Reads)
	}
}

// An install card is listed only on the device it updates. The phone's board leaves out the Mac's build, the
// desktop's the phone's, and the web console lists both — out of the count,
// the pills, the bundle and the section alike, never a hidden-but-counted row.
func TestInstallCardIsTheDevices(t *testing.T) {
	ths := []threads.Thread{{ID: "ship", Status: "needs_you"}, {ID: "mac", Status: "needs_you"}, {ID: "both", Status: "needs_you"}}
	asks := []threads.Ask{
		{ID: "p1", ThreadID: "ship", Surface: "mobile", State: "open", Kind: "install", Class: "install", Title: "Install app build 1553 (tap the link)"},
		{ID: "m1", ThreadID: "mac", Surface: "web", State: "open", Kind: "install", Class: "install", Title: "Install desktop build 1553"},
		{ID: "p2", ThreadID: "both", Surface: "mobile", State: "open", Kind: "install", Class: "install", Title: "Install app build 1554 (tap the link)"},
		{ID: "r1", ThreadID: "both", Surface: "any", State: "open", Kind: "read", Title: "The toolbar is in"},
	}
	for _, tc := range []struct {
		surface         string
		count           int
		ship, mac, both int
		shipSec, macSec string
		bothPill        string
	}{
		{"web", 4, 1, 1, 2, "your_turn", "your_turn", "1 to read/read 1 to install/install"},
		{"mobile", 3, 1, 0, 2, "your_turn", "", "1 to read/read 1 to install/install"},
		{"desktop", 2, 0, 1, 1, "", "your_turn", "1 to read/read"},
	} {
		b := Build(tc.surface, asks, nil, ths)
		if b.Surface != tc.surface || b.Count != tc.count || b.Badges.YourTurn != tc.count {
			t.Fatalf("%s: surface=%s count=%d badge=%d", tc.surface, b.Surface, b.Count, b.Badges.YourTurn)
		}
		if b.ForYou["ship"] != tc.ship || b.ForYou["mac"] != tc.mac || b.ForYou["both"] != tc.both {
			t.Fatalf("%s: for_you=%v", tc.surface, b.ForYou)
		}
		if b.Installs["ship"] != tc.ship || b.Installs["mac"] != tc.mac {
			t.Fatalf("%s: installs=%v", tc.surface, b.Installs)
		}
		if b.Section["ship"] != tc.shipSec || b.Section["mac"] != tc.macSec {
			t.Fatalf("%s: section=%v", tc.surface, b.Section)
		}
		var ps []string
		for _, p := range b.Pills["both"] {
			ps = append(ps, p.Word+"/"+p.Tone)
		}
		if got := strings.Join(ps, " "); got != tc.bothPill {
			t.Fatalf("%s: both pills=%q", tc.surface, got)
		}
		listed := map[string]bool{}
		for _, s := range b.Sessions {
			for _, a := range s.Asks {
				listed[a.ID] = true
			}
		}
		if listed["p1"] != (tc.ship == 1) || listed["m1"] != (tc.mac == 1) || listed["p2"] != (tc.both == 2) || !listed["r1"] {
			t.Fatalf("%s: listed=%v", tc.surface, listed)
		}
	}
}

// The Sessions page's headings and pills come from the hub, so both surfaces
// print the same words: one heading per session, counts in cards.
func TestHeadingsAndPills(t *testing.T) {
	ths := []threads.Thread{{ID: "run", Status: "running"}, {ID: "read", Status: "needs_you"}, {ID: "act", Status: "needs_you"},
		{ID: "ship", Status: "done"}, {ID: "old", Status: "idle"}, {ID: "mix", Status: "needs_you"}}
	asks := []threads.Ask{
		{ID: "a1", ThreadID: "run", Surface: "any", State: "open", Kind: "decision"},
		{ID: "a2", ThreadID: "read", Surface: "any", State: "open", Kind: "read"},
		{ID: "a3", ThreadID: "act", Surface: "any", State: "open", Kind: "decision"},
		{ID: "a4", ThreadID: "act", Surface: "any", State: "open", Kind: "read"},
		{ID: "a5", ThreadID: "ship", Surface: "mobile", State: "open", Kind: "install", Title: "Install app build 884 (tap the link)", Detail: "What changed: the Money page."},
		// The build was raised BEFORE the read, and the read still leads.
		{ID: "a6", ThreadID: "mix", Surface: "mobile", State: "open", Kind: "install", Title: "Install app build 1003 (tap the link)", Detail: "Sessions is three groups now."},
		{ID: "a7", ThreadID: "mix", Surface: "any", State: "open", Kind: "read", Title: "One install card from now on", Detail: "The two build 1001s were one build."},
	}
	b := Build("web", asks, nil, ths)
	var got []string
	for _, h := range b.Headings {
		got = append(got, h.Label+" · "+h.Count)
	}
	// One heading for everything paused on the owner — a session whose only
	// card is a read files under Your turn too, wearing its blue pill.
	if want := "Your turn · 6 in 4 sessions · 1 working|Working · 1 · 1 for you"; strings.Join(got, "|") != want {
		t.Fatalf("headings\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if b.Section["run"] != "working" || b.Section["read"] != "your_turn" || b.Section["act"] != "your_turn" || b.Section["ship"] != "your_turn" || b.Section["mix"] != "your_turn" || b.Section["old"] != "" {
		t.Fatalf("section %v", b.Section)
	}
	pill := func(id string) string {
		var s []string
		for _, p := range b.Pills[id] {
			s = append(s, p.Word+"/"+p.Tone)
		}
		return strings.Join(s, " ")
	}
	// ONE PILL PER CLASS: a decision beside a read is "1 for
	// you" + "1 to read", a read beside a build "1 to read" + "1 to install" —
	// never a red "2 for you" that hides what the session waits on.
	if pill("run") != "running/running 1 for you/needs" || pill("read") != "1 to read/read" || pill("act") != "1 for you/needs 1 to read/read" || pill("ship") != "1 to install/install" || pill("mix") != "1 to read/read 1 to install/install" || pill("old") != "" {
		t.Fatalf("pills %v", b.Pills)
	}
	// The row opens on, and names, the read before the install even though the
	// install came first; a session that only has a build to install names it
	// and prints no description under it — the title is the whole instruction.
	if b.First["mix"] != "a7" || b.Todo["mix"] != "One install card from now on" || b.Detail["mix"] != "The two build 1001s were one build." {
		t.Fatalf("mix first=%q todo=%q detail=%q", b.First["mix"], b.Todo["mix"], b.Detail["mix"])
	}
	if b.First["ship"] != "a5" || b.Todo["ship"] != "Install app build 884 (tap the link)" || b.Detail["ship"] != "" {
		t.Fatalf("ship first=%q todo=%q detail=%q", b.First["ship"], b.Todo["ship"], b.Detail["ship"])
	}
	for _, s := range b.Sessions {
		if s.ID == "mix" && s.First != "a7" {
			t.Fatalf("mix bundle opens on %q, want the read", s.First)
		}
	}
	// Ended sessions with nothing open have no heading at all: the page is
	// what is working or waiting on the owner, the rest is behind All sessions.
	ended := []threads.Thread{{ID: "x", Status: "done"}, {ID: "y", Status: "idle"}}
	if eb := Build("mobile", nil, nil, ended); len(eb.Headings) != 2 || len(eb.Section) != 0 {
		t.Fatalf("ended sessions filed: headings %+v section %v", eb.Headings, eb.Section)
	}
	if threads.ModelLabel("claude-opus-4-5-20251101") != "opus 4.5" || threads.ModelLabel("claude-fable-5-1[1m]") != "fable 5.1" || threads.ScheduleLabel("weekly@Sun 17:30") != "weekly Sun 17:30" {
		t.Fatal("labels")
	}
}
