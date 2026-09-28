package threads

import (
	"testing"
	"time"

	"life/hub/internal/store"
)

// The calendar's past is a record of what the OWNER closed: an ask they
// answered lands at the minute they answered it, with the word for what
// answering it meant; a session's own closes and the ones the calendar minted
// stay off it.
func TestDatedIsTheRecordOfWhatTheOwnerClosed(t *testing.T) {
	m, _, _ := setup(t)
	th, _ := m.Create("Migrate the blog", "life", "", "Move it.", "", "", nil)
	read, _ := m.AddAsk(th.ID, "", "The hosting plan is $15.99/yr", "", "read", "")
	dec, _ := m.AddAsk(th.ID, "", "Merge PR #38?", "", "decision", "")
	acc, _ := m.AddAsk(th.ID, "", "Add the env var", "", "access", "")
	phys, _ := m.AddAsk(th.ID, "", "Install build 808", "", "physical", "phone reports build 808")
	skip, _ := m.AddAsk(th.ID, "", "Buy pineapple juice", "", "physical", "")
	theirs, _ := m.AddAsk(th.ID, "", "Session bookkeeping", "", "read", "")
	// Install asks: the tap closes one by "app", the phone reporting the
	// build closes one by "hub" with no check hint — both are the owner's
	// install, both land on the calendar as "Installed: build N". One at
	// a time: a newer build's ask supersedes an open older one.
	tap, _ := m.AddAsk(th.ID, "", "Install app build 880 (tap the link)", "https://x/ota/install.html", "install", "")
	for _, x := range []struct{ id, state, by string }{
		{read.ID, "dismissed", "owner"}, // a read card is read whichever button was tapped
		{dec.ID, "done", "owner"},
		{acc.ID, "done", "owner"},
		{phys.ID, "done", "hub"}, // the verifier confirmed the OWNER's install
		{skip.ID, "dismissed", "owner"},
		{theirs.ID, "done", "claude:thread:" + th.ID},
		{tap.ID, "done", "app"},
	} {
		if _, err := m.ResolveAsk(x.id, x.state, x.by, ""); err != nil {
			t.Fatal(err)
		}
	}
	rep, _ := m.AddAsk(th.ID, "", "Install app build 881 (tap the link)", "https://x/ota/install.html", "install", "")
	if _, err := m.ResolveAsk(rep.ID, "done", "hub", "phone reports build 881"); err != nil {
		t.Fatal(err)
	}
	today := store.Day(time.Now())
	rows, err := m.Dated(today, today)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.Dated{}
	for _, d := range rows {
		got[d.AskID] = d
	}
	if _, ok := got[theirs.ID]; ok || len(rows) != 7 {
		t.Fatalf("a session's own close on the calendar: %+v", rows)
	}
	want := map[string][2]string{
		read.ID: {"Read", "done"}, dec.ID: {"Decided", "done"}, acc.ID: {"Granted", "done"},
		phys.ID: {"Did", "done"}, skip.ID: {"Skipped", "dismissed"},
		tap.ID: {"Installed", "done"}, rep.ID: {"Installed", "done"},
	}
	for id, w := range want {
		d := got[id]
		if !d.Did || d.Verb != w[0] || d.State != w[1] || d.Actor != "owner" || d.Key != "did:ask:"+id ||
			d.Ref != "ask:"+id || d.Day != today || d.At == "" || d.ThreadTitle != "Migrate the blog" {
			t.Fatalf("%s: %+v", w[0], d)
		}
	}
	// The record reads "Installed: build 880", and still says which kind of
	// ask it was so the surfaces can draw it teal.
	if d := got[tap.ID]; d.Title != "build 880" || d.AskKind != "install" {
		t.Fatalf("install record: %+v", d)
	}
	if rows, _ := m.Dated("2020-01-01", "2020-01-02"); len(rows) != 0 {
		t.Fatalf("window not honoured: %+v", rows)
	}
	if _, err := m.Dated("nope", today); err == nil {
		t.Fatal("bad day accepted")
	}
}
