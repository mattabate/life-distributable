package recs

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"life/hub/internal/store"
)

func newStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now }
	return s
}

// A rec the owner decided is on the calendar at the minute they decided it,
// and again when it is scored; a deferred one keeps its review-day row; one a session took is
// the session's deed.
func TestDatedCarriesDecisionsAndScores(t *testing.T) {
	now := time.Date(2026, 9, 8, 21, 30, 0, 0, time.Local)
	s := newStore(t, now)
	add := func(title string) Rec {
		r, err := s.Add(Rec{Title: title, Domain: "audience", Kind: "try", Source: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	yes, no, later, theirs, open := add("Try Phantombuster"), add("Buy the ring light"), add("Look at Descript"), add("Post daily"), add("Untouched")
	s.Decide(yes.ID, "accepted", "owner", "")
	s.Decide(no.ID, "declined", "owner", "too dear")
	if _, err := s.Defer(later.ID, "2026-09-20", "owner", ""); err != nil {
		t.Fatal(err)
	}
	s.Decide(theirs.ID, "accepted", "claude:thread:t1", "")
	s.Now = func() time.Time { return now.Add(26 * time.Hour) } // scored the next evening
	if _, err := s.Score(yes.ID, "worked", "owner", "38x views"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Dated("2026-09-08", "2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]store.Dated{}
	for _, d := range rows {
		byKey[d.Key] = d
	}
	// Five filed bars (one per rec, decided or not) + the deferred check-back
	// + four decision/score records.
	if len(rows) != 10 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	if d := byKey["did:rec:"+yes.ID]; !d.Did || d.Verb != "Accepted" || d.State != "accepted" || d.Actor != "owner" || d.Day != "2026-09-08" || d.At != "21:30" || d.Ref != "rec:"+yes.ID {
		t.Fatalf("accepted: %+v", d)
	}
	if d := byKey["did:rec:"+yes.ID+":outcome"]; !d.Did || d.Verb != "Worked" || d.State != "done" || d.Day != "2026-09-09" || d.At != "23:30" {
		t.Fatalf("scored: %+v", d)
	}
	if d := byKey["did:rec:"+no.ID]; d.Verb != "Declined" || d.State != "declined" {
		t.Fatalf("declined: %+v", d)
	}
	if d := byKey["did:rec:"+theirs.ID]; d.Verb != "Accepted" || d.Actor != "claude:thread:t1" {
		t.Fatalf("theirs: %+v", d)
	}
	if d := byKey[""]; d.ID != later.ID || d.Did || d.State != "deferred" || d.Day != "2026-09-20" {
		t.Fatalf("deferred: %+v", d)
	}
	// EVERY rec is on the calendar at the minute it was FILED (a dark bar; the
	// decision is a second, light one) — never a record row, and its state is
	// the rec's CURRENT one (an answered rec's filed bar must not look open),
	// so the readers fade an accepted rec's filed bar and strike a declined one.
	for _, r := range []Rec{open, yes, no, later, theirs} {
		d, ok := byKey["filed:rec:"+r.ID]
		want := map[string]string{open.ID: "proposed", yes.ID: "accepted", no.ID: "declined", later.ID: "deferred", theirs.ID: "accepted"}[r.ID]
		if !ok || d.ID != r.ID || d.Did || d.Verb != "Filed" || d.State != want ||
			d.Kind != "rec" || d.Ref != "rec:"+r.ID || d.Day != "2026-09-08" || d.At != "21:30" {
			t.Fatalf("filed %s: %+v", r.Title, d)
		}
	}
	if rows, _ := s.Dated("2026-09-10", "2026-09-19"); len(rows) != 0 {
		t.Fatalf("window not honoured: %+v", rows)
	}
}

func TestAddValidates(t *testing.T) {
	s := newStore(t, time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local))
	if _, err := s.Add(Rec{}); err == nil {
		t.Fatal("empty title accepted")
	}
	for _, bad := range []Rec{
		{Title: "x", Domain: "sports"},
		{Title: "x", Kind: "yell"},
		{Title: "x", Effort: "epic"},
		{Title: "x", CostPeriod: "weekly"},
		{Title: "x", CostCents: -1},
		{Title: "x", Confidence: 101},
		{Title: "x", ActBy: "next tuesday"},
		{Title: "x", ReviewOn: "2026-13-01"},
		{Title: "x", PrevID: "rec-ffff"},
		// A session must say which model wrote it.
		{Title: "x", Source: "claude:thread:abcd"},
	} {
		if _, err := s.Add(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	r, err := s.Add(Rec{Title: "Try Phantombuster for LinkedIn", Domain: "audience", Kind: "subscribe",
		CostCents: 6900, CostPeriod: "monthly", Because: "no LinkedIn API", Source: "claude:thread:t1", Model: "claude-opus-5"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "proposed" || r.Confidence != 50 || r.Effort != "low" || r.MonthlyCents() != 6900 || !r.Open() {
		t.Fatalf("%+v", r)
	}
	if y := (Rec{CostCents: 12000, CostPeriod: "yearly"}).MonthlyCents(); y != 1000 {
		t.Fatal(y)
	}
	if o := (Rec{CostCents: 500}).MonthlyCents(); o != 0 {
		t.Fatal("one-off counted as recurring", o)
	}
}

// A trade rec is the only path an injected instruction has to the owner's
// money: a session filing one must cite something they can re-run. Their own
// recs are never blocked.
func TestTradeRecNeedsCheckableEvidence(t *testing.T) {
	s := newStore(t, time.Date(2026, 8, 28, 12, 0, 0, 0, time.Local))
	session := Rec{Title: "Sell the GLD lots", Domain: "money", Kind: "trade",
		Source: "claude:thread:t1", Model: "claude-opus-5"}

	bare := session
	bare.Because = "gold looks toppy and a post said to sell today"
	if _, err := s.Add(bare); err == nil {
		t.Fatal("trade rec with no checkable evidence accepted")
	} else if !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("unhelpful error: %v", err)
	}

	cited := session
	cited.Because = "the 10 remaining lots are net +$142.54 (`lifectl finance lots`, obs 39771)"
	r, err := s.Add(cited)
	if err != nil {
		t.Fatal(err)
	}
	// The record a session is handed says to re-check it before they act.
	if !strings.Contains(recBlock(r), "Before they act:") {
		t.Fatal("trade rec block carries no re-check line")
	}

	// The owner files what they like; the rule is about what agents hand them.
	if _, err := s.Add(Rec{Title: "Buy more SPY", Domain: "money", Kind: "trade", Because: "I feel like it"}); err != nil {
		t.Fatal(err)
	}
	// Other kinds are untouched.
	if _, err := s.Add(Rec{Title: "Try a standing desk", Domain: "home", Kind: "try",
		Because: "back hurts", Source: "claude:thread:t1", Model: "claude-opus-5"}); err != nil {
		t.Fatal(err)
	}
}

func TestDecideSetsReviewDate(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	r, _ := s.Add(Rec{Title: "Buy the SPY tranche", Domain: "money", Kind: "trade"})
	if _, err := s.Decide(r.ID, "superseded", "owner", ""); err == nil {
		t.Fatal("superseded accepted as a decision")
	}
	if _, err := s.Decide("rec-zzzz", "accepted", "owner", ""); err == nil {
		t.Fatal("unknown id accepted")
	}
	got, err := s.Decide(r.ID, "accepted", "owner", "ok, spread over three weeks")
	if err != nil {
		t.Fatal(err)
	}
	// Accepting starts the clock: 30 days out when the rec named no date.
	if got.Status != "accepted" || got.ReviewOn != "2026-09-22" || got.DecidedAt == nil || got.DecisionNote == "" {
		t.Fatalf("%+v", got)
	}
	// A rec that named its own review date keeps it.
	r2, _ := s.Add(Rec{Title: "x", ReviewOn: "2026-10-01"})
	g2, _ := s.Decide(r2.ID, "accepted", "owner", "")
	if g2.ReviewOn != "2026-10-01" {
		t.Fatal(g2.ReviewOn)
	}
	// Putting it back on the list clears the decision.
	back, _ := s.Decide(r.ID, "proposed", "owner", "")
	if back.Status != "proposed" || back.DecidedAt != nil || back.DecisionNote != "" {
		t.Fatalf("%+v", back)
	}
}

func TestSupersedeClosesPrevious(t *testing.T) {
	s := newStore(t, time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local))
	old, _ := s.Add(Rec{Title: "Buy GLD", Domain: "money"})
	nw, err := s.Add(Rec{Title: "Sell GLD, buy SPY/SNOW/TWLO", Domain: "money", PrevID: old.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(old.ID)
	if got.Status != "superseded" || nw.PrevID != old.ID {
		t.Fatalf("%+v %+v", got, nw)
	}
	open, _ := s.List(Filter{})
	if len(open) != 1 || open[0].ID != nw.ID {
		t.Fatalf("%+v", open)
	}
}

func TestExpireAndDueFilter(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	stale, _ := s.Add(Rec{Title: "Buy before earnings", Domain: "money", ActBy: "2026-08-20"})
	live, _ := s.Add(Rec{Title: "Buy next week", Domain: "money", ActBy: "2026-09-20"})
	n, err := s.Expire()
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if g, _ := s.Get(stale.ID); g.Status != "expired" || g.DecidedBy != "hub" {
		t.Fatalf("%+v", g)
	}
	if g, _ := s.Get(live.ID); g.Status != "proposed" {
		t.Fatalf("%+v", g)
	}
	// Only accepted-but-unscored recs whose review date has arrived are due.
	due, _ := s.Add(Rec{Title: "Cancel the gym", Domain: "health", ReviewOn: "2026-08-01"})
	s.Decide(due.ID, "accepted", "owner", "")
	got, _ := s.List(Filter{Status: "all", DueBy: now.Format("2006-01-02")})
	if len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("%+v", got)
	}
	if _, err := s.Score(due.ID, "brilliant", "claude", ""); err == nil {
		t.Fatal("bad outcome accepted")
	}
	if _, err := s.Score(due.ID, "worked", "claude:thread:t1", "saved $40/mo"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.List(Filter{Status: "all", DueBy: now.Format("2006-01-02")})
	if len(got) != 0 {
		t.Fatalf("scored rec still due: %+v", got)
	}
}

func TestLinkDedupes(t *testing.T) {
	s := newStore(t, time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local))
	r, _ := s.Add(Rec{Title: "Tranche plan", Domain: "money"})
	s.Link(r.ID, "cal-1111", "cal-2222")
	got, _ := s.Link(r.ID, "cal-2222", "ask-3333", "")
	if got.Links != "cal-1111,cal-2222,ask-3333" {
		t.Fatal(got.Links)
	}
	if _, err := s.Link("rec-zzzz", "cal-1"); err == nil {
		t.Fatal("unknown id accepted")
	}
}

func TestStatsTrackRecord(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	a, _ := s.Add(Rec{Title: "Phantombuster", Domain: "audience", Kind: "subscribe", CostCents: 6900, CostPeriod: "monthly",
		Source: "claude:thread:aud-1", Model: "claude-opus-5"})
	b, _ := s.Add(Rec{Title: "Semantic Scholar key", Domain: "audience", Kind: "try", Source: "claude:thread:aud-1", Model: "claude-opus-5"})
	c, _ := s.Add(Rec{Title: "Buy tranche 2", Domain: "money", Kind: "trade", CostCents: 161000})
	s.Add(Rec{Title: "Untouched idea", Domain: "tools"})
	s.Decide(a.ID, "declined", "owner", "not paying for tools before the goals earn")
	s.Decide(b.ID, "accepted", "owner", "")
	s.Decide(c.ID, "accepted", "owner", "")
	s.Score(b.ID, "worked", "claude", "822 rows where Scholar had 14")
	s.Score(c.ID, "mixed", "claude", "filled, but SNOW slipped")

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 || st.Proposed != 1 || st.Accepted != 2 || st.Declined != 1 {
		t.Fatalf("%+v", st)
	}
	if st.AcceptRate != 2.0/3.0 {
		t.Fatal(st.AcceptRate)
	}
	// worked + half a mixed, over two judged.
	if st.HitRate != 0.75 {
		t.Fatal(st.HitRate)
	}
	// The declined subscription must NOT count against the wallet, and the
	// accepted trade is money moved, not spent: it sits in traded, not one-off.
	if st.MonthlyUSD != "0.00" || st.OneOffUSD != "0.00" || st.TradedUSD != "1610.00" {
		t.Fatalf("monthly=%s oneoff=%s traded=%s", st.MonthlyUSD, st.OneOffUSD, st.TradedUSD)
	}
	if st.Scored != 2 || st.Worked != 1 || st.Mixed != 1 || st.LastScored != "2026-08-23" {
		t.Fatalf("%+v", st)
	}
	// Per model: opus filed two (one declined, one accepted and worked), the owner the other two.
	if len(st.ByModel) != 2 || st.ByModel[0].Model != "claude-opus-5" || st.ByModel[0].Total != 2 ||
		st.ByModel[0].Accepted != 1 || st.ByModel[0].Declined != 1 || st.ByModel[0].Worked != 1 || st.ByModel[1].Model != "owner" {
		t.Fatalf("%+v", st.ByModel)
	}
	if got, _ := s.List(Filter{Status: "all", Model: "claude-opus-5"}); len(got) != 2 {
		t.Fatalf("model filter: %d", len(got))
	}
	if len(st.ByDomain) == 0 || st.ByDomain[0].Domain != "audience" || st.ByDomain[0].Total != 2 {
		t.Fatalf("%+v", st.ByDomain)
	}
	if st.OldestOpen != "2026-08-23" || st.DueForRev != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestListFilters(t *testing.T) {
	s := newStore(t, time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local))
	s.Add(Rec{Title: "cheap", Domain: "tools", Kind: "buy", CostCents: 900, GoalID: "make-more-money"})
	s.Add(Rec{Title: "dear", Domain: "tools", Kind: "subscribe", CostCents: 50000})
	s.Add(Rec{Title: "health thing", Domain: "health", Kind: "habit"})
	if got, _ := s.List(Filter{Domain: "tools"}); len(got) != 2 {
		t.Fatal(len(got))
	}
	if got, _ := s.List(Filter{MaxCents: 1000}); len(got) != 2 { // 900 and the free one
		t.Fatal(len(got))
	}
	if got, _ := s.List(Filter{Kind: "habit"}); len(got) != 1 {
		t.Fatal(len(got))
	}
	if got, _ := s.List(Filter{GoalID: "make-more-money"}); len(got) != 1 {
		t.Fatal(len(got))
	}
	if got, _ := s.List(Filter{Limit: 1}); len(got) != 1 {
		t.Fatal(len(got))
	}
	if _, err := s.List(Filter{Status: "nonsense"}); err == nil {
		t.Fatal("bad status accepted")
	}
}

// What an agent is told when the owner decides. The session that filed the
// rec gets the short relay (it wrote the argument); a new one gets the whole
// record and where to find it. Their note leads either way — it is the session
// title and the board preview — and a decline says plainly not to do it.
func TestStarterAndRelay(t *testing.T) {
	r := Rec{ID: "rec-1234", Title: "Stay on BigQuery", Domain: "tools", Kind: "process",
		Because: "600ms per query is the scheduler, not the rows", Expect: "the puzzle page feels instant",
		GoalID: "grow-my-audience", ThreadID: "puzzle-work-9a54", Source: "claude:thread:puzzle-work-9a54",
		ReviewOn: "2026-09-24", Status: "accepted", DecisionNote: "already did it this morning", Model: "claude-opus-5"}
	if st := StarterFor(r, Related{}).Context; !strings.Contains(st, "Filed by: claude:thread:puzzle-work-9a54 (claude-opus-5)") {
		t.Fatal(st)
	}

	if got := SourceThread(r); got != "puzzle-work-9a54" {
		t.Fatal(got)
	}
	if got := SourceThread(Rec{Source: "claude:thread:from-source-only"}); got != "from-source-only" {
		t.Fatal(got)
	}
	if got := SourceThread(Rec{Source: "owner"}); got != "" {
		t.Fatal("the owner is not a session:", got)
	}

	st := StarterFor(r, Related{})
	if st.Opener != "already did it this morning" {
		t.Fatal("the owner's note is the first line, so it is the session title:", st.Opener)
	}
	for _, want := range []string{"rec-1234", "grow-my-audience", "600ms per query", "carry it out", "lifectl rec rec-1234", "2026-09-24"} {
		if !strings.Contains(st.Context, want) {
			t.Fatalf("new-session brief missing %q: %s", want, st.Context)
		}
	}
	if !strings.HasPrefix(st.Text(), st.Opener) || !strings.Contains(st.Text(), st.Context) {
		t.Fatal("the message is the owner's line then the block:", st.Text())
	}
	if relay := RelayFor(r, Related{}); !strings.Contains(relay, "Accepted.") ||
		!strings.Contains(relay, "Their note (blank = they added none): already did it this morning") ||
		!strings.Contains(relay, "link <ids>") || strings.Contains(relay, "600ms per query") {
		t.Fatal("relay should be short and carry the owner's note:", relay)
	}

	// Declining is delivered the same way and must not read as a job to do.
	no := r
	no.Status, no.DecisionNote = "declined", "not while it is free"
	if st := StarterFor(no, Related{}); !strings.Contains(st.Context, "They said no") || strings.Contains(st.Context, "Your job is to carry it out") {
		t.Fatal("declined brief:", st.Context)
	}
	if relay := RelayFor(no, Related{}); !strings.Contains(relay, "Declined.") || strings.Contains(relay, "Carry it out") {
		t.Fatal("declined relay:", relay)
	}

	// No note is not an error — the default first line stands in.
	quiet := r
	quiet.DecisionNote = ""
	if st := StarterFor(quiet, Related{}); st.Opener != "I accepted your recommendation: Stay on BigQuery" {
		t.Fatal(st.Opener)
	}
	if relay := RelayFor(quiet, Related{}); !strings.Contains(relay, "(blank = they added none): (none)") {
		t.Fatal(relay)
	}

	// Deferring names the day and is not a job either.
	later := r
	later.Status, later.ReviewOn, later.DecisionNote = "deferred", "2026-09-23", "not before the goals earn"
	if st := StarterFor(later, Related{}); !strings.Contains(st.Context, "check back on 2026-09-23") || strings.Contains(st.Context, "carry it out") ||
		st.Opener != "not before the goals earn" {
		t.Fatal("deferred brief:", st.Opener, st.Context)
	}
	if relay := RelayFor(later, Related{}); !strings.Contains(relay, "Deferred until 2026-09-23") || strings.Contains(relay, "Carry it out") {
		t.Fatal("deferred relay:", relay)
	}

	// A reply is neither: the owner's line, the record, and "answer, do not act".
	open := r
	open.Status, open.DecisionNote = "proposed", ""
	st = ReplyStarter(open, "which fund would you put it in?", Related{})
	if st.Opener != "which fund would you put it in?" || !strings.Contains(st.Context, "WITHOUT deciding") ||
		!strings.Contains(st.Context, "still open") || !strings.Contains(st.Context, "600ms per query") || strings.Contains(st.Context, "Your job is to carry it out") {
		t.Fatal("reply brief:", st.Opener, st.Context)
	}
	if relay := ReplyRelay(later, "is the price still 100?", Related{}); !strings.Contains(relay, "parked until 2026-09-23") ||
		!strings.Contains(relay, "Their note: is the price still 100?") || strings.Contains(relay, "Deferred until") {
		t.Fatal("reply relay:", relay)
	}
}

// What the rec points at rides along, so a new session does not spend its
// first minute rediscovering the calendar, the earlier rec and the filing
// thread.
// A new session gets all of it; the filing session, which wrote the argument,
// gets only the state of what it minted.
func TestStarterCarriesWhatItPointsAt(t *testing.T) {
	r := Rec{ID: "rec-26da", Title: "Finish the GLD exit", Domain: "money", Kind: "trade", GoalID: "make-more-money",
		ThreadID: "investing-19e6", Source: "claude:thread:investing-19e6", Status: "accepted", ActBy: "2026-09-03", ReviewOn: "2026-09-17",
		PrevID: "rec-0001", DecisionNote: "yes, but spread the sales out"}
	rel := Related{
		Linked:     []string{"cal-29e0 · owner ·2026-09-03 · scheduled · Buy tranche 3", "rec-0036 · rec · accepted 2026-08-19 · Sell half the GLD"},
		Calendar:   []string{"cal-aaaa · owner ·2026-08-27 · scheduled · Buy TWLO tranche 2"},
		Source:     "session `investing-19e6` — \"Investing analysis\", idle, checks in weekly@Sun 17:00",
		SourceSaid: "(2026-08-24) GLD is the only losing rate in the account.\nSell the rest on 09-03.",
	}
	st := StarterFor(r, rel)
	for _, want := range []string{"What it points at:", "- cal-29e0 · owner ·2026-09-03 · scheduled", "- rec-0036 · rec · accepted",
		"Also on their calendar for make-more-money", "- cal-aaaa", "Filed from: session `investing-19e6`",
		"That session's last reply:\n  (2026-08-24) ", "\n  Sell the rest on 09-03.", "Dates: act by 2026-09-03 · score on 2026-09-17 · supersedes rec-0001"} {
		if !strings.Contains(st.Context, want) {
			t.Fatalf("brief missing %q:\n%s", want, st.Context)
		}
	}
	// The section sits between the record and the instruction, so the last
	// thing the session reads is still what to do.
	if strings.Index(st.Context, "Around it") > strings.Index(st.Context, "Your job is to carry it out") {
		t.Fatal("related section after the instruction:\n" + st.Context)
	}
	if strings.Contains(StarterFor(r, Related{}).Context, "Around it") {
		t.Fatal("empty Related still drew a section")
	}
	relay := RelayFor(r, rel)
	if !strings.Contains(relay, "- cal-29e0") || strings.Contains(relay, "Also on their calendar") || strings.Contains(relay, "last reply") {
		t.Fatal("the filing session gets the links only:\n" + relay)
	}
	if reply := ReplyRelay(r, "still on?", rel); !strings.Contains(reply, "- rec-0036") || strings.Contains(reply, "Filed from") {
		t.Fatal(reply)
	}
	if reply := ReplyStarter(r, "still on?", rel); !strings.Contains(reply.Context, "Filed from: session") {
		t.Fatal(reply.Context)
	}
}

// Reply keeps the owner's line on the record without touching the verdict.
func TestReplyKeepsTheLine(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	r, _ := s.Add(Rec{Title: "Raise the 401k deferral", Domain: "money", Kind: "habit", Detail: "Room of 17,500."})
	if _, err := s.Reply(r.ID, "owner", "  "); err == nil {
		t.Fatal("took an empty reply")
	}
	got, err := s.Reply(r.ID, "owner", "which fund?")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "proposed" || got.DecisionNote != "" || got.DecidedAt != nil ||
		got.Detail != "Room of 17,500.\n\n_owner replied on 2026-08-26: which fund?_" {
		t.Fatalf("%+v", got)
	}
}

// The third answer: not a decline, "check back in with me on a different
// day". Off the list until then, back on it as filed, with the wait written down.
func TestDeferAndWake(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	r, _ := s.Add(Rec{Title: "Switch on the X user token", Domain: "audience", Kind: "subscribe", Detail: "Parked until the goals earn."})
	for _, bad := range []string{"", "next month", "2026-08-26", "2026-08-01"} {
		if _, err := s.Defer(r.ID, bad, "owner", ""); err == nil {
			t.Fatalf("deferred to %q", bad)
		}
	}
	if _, err := s.Decide(r.ID, "deferred", "owner", ""); err == nil {
		t.Fatal("Decide took deferred without a date")
	}
	got, err := s.Defer(r.ID, "2026-09-23", "owner", "check back end of September")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "deferred" || got.ReviewOn != "2026-09-23" || got.DecidedAt == nil || got.DecisionNote != "check back end of September" {
		t.Fatalf("%+v", got)
	}
	// Gone from the open list, not a decision, not due for scoring.
	if open, _ := s.List(Filter{}); len(open) != 0 {
		t.Fatalf("deferred rec still open: %+v", open)
	}
	if due, _ := s.List(Filter{Status: "all", DueBy: "2026-12-31"}); len(due) != 0 {
		t.Fatalf("deferred rec in the review queue: %+v", due)
	}
	st, _ := s.Stats()
	if st.Deferred != 1 || st.Proposed != 0 || st.AcceptRate != 0 || st.DueForRev != 0 {
		t.Fatalf("%+v", st)
	}
	// Only a waiting rec can wait; an accepted one is a commitment.
	acc, _ := s.Add(Rec{Title: "x"})
	s.Decide(acc.ID, "accepted", "owner", "")
	if _, err := s.Defer(acc.ID, "2026-09-23", "owner", ""); err == nil {
		t.Fatal("deferred an accepted rec")
	}
	// A deferred rec can be re-deferred (the owner moves the day), or reopened by
	// hand, which drops the wake-up date rather than leaving it to score.
	if _, err := s.Defer(r.ID, "2026-10-01", "owner", "later still"); err != nil {
		t.Fatal(err)
	}
	back, _ := s.Decide(r.ID, "proposed", "owner", "")
	if back.Status != "proposed" || back.ReviewOn != "" {
		t.Fatalf("%+v", back)
	}
	s.Defer(r.ID, "2026-09-23", "owner", "check back end of September")

	// Not yet.
	if n, _ := s.Wake(); n != 0 {
		t.Fatal("woke early", n)
	}
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 8, 0, 0, 0, time.Local) }
	n, err := s.Wake()
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	woke, _ := s.Get(r.ID)
	if woke.Status != "proposed" || woke.DecidedAt != nil || woke.DecisionNote != "" || woke.ReviewOn != "" {
		t.Fatalf("%+v", woke)
	}
	if !strings.Contains(woke.Detail, "Parked until the goals earn.") || !strings.Contains(woke.Detail, "Deferred on 2026-08-26 until 2026-09-23 (owner): check back end of September") {
		t.Fatal("the wait was not written down:", woke.Detail)
	}
	if open, _ := s.List(Filter{}); len(open) != 1 || open[0].ID != r.ID {
		t.Fatalf("%+v", open)
	}
	if n, _ := s.Wake(); n != 0 {
		t.Fatal("woke twice", n)
	}
}

// Phase 0 (docs/reviews/2026-08-26-structure.md §3): accepting straight from
// "later" must not keep the wake-up day as the scoring date, and deferring
// twice keeps the first reason on the record.
func TestAcceptFromDeferredResetsReviewAndSecondDeferKeepsNote(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local)
	s := newStore(t, now)
	r, _ := s.Add(Rec{Title: "Switch on the X user token", Domain: "audience", Kind: "subscribe"})
	s.Defer(r.ID, "2026-09-23", "owner", "check back end of September")
	again, err := s.Defer(r.ID, "2026-10-15", "owner", "still not yet")
	if err != nil {
		t.Fatal(err)
	}
	if again.DecisionNote != "still not yet" || !strings.Contains(again.Detail, "Deferred on 2026-08-26 until 2026-09-23 (owner): check back end of September") {
		t.Fatalf("first deferral lost: %+v", again)
	}
	acc, err := s.Decide(r.ID, "accepted", "owner", "fine, do it")
	if err != nil {
		t.Fatal(err)
	}
	if acc.ReviewOn != "2026-09-25" {
		t.Fatalf("review_on should be 30 days out, not the wake date: %q", acc.ReviewOn)
	}
	if len(acc.ID) != len("rec-")+8 {
		t.Fatalf("ids are 4 random bytes: %s", acc.ID)
	}
}
