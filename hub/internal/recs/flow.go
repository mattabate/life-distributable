package recs

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"life/hub/internal/actions"
	"life/hub/internal/calendar"
	"life/hub/internal/deliver"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

// Flow is what happens AROUND a decision: the rec's brief resolved against
// the rest of the hub (linked items, the goal's calendar, the filing
// session's last reply), the check-in a deferral puts on the calendar, and
// the delivery of the owner's words to a session. A handler decodes and
// answers; this decides. Cal and Acts may
// be nil — everything here is best-effort, a missing store costs a line.
type Flow struct {
	Store *Store
	Cal   *calendar.Calendar
	Thr   *threads.Manager
	Acts  *actions.Queue
}

func (f *Flow) deliverer() deliver.Deliverer { return deliver.Deliverer{Thr: f.Thr} }

// DeferCheckIn is the "check back in with me on a different day" half of a
// deferral, as a scheduled agent job on the calendar. Wake alone would only
// put the rec back on a list the owner has to come looking at; this puts an agent run on that day, in the session
// that filed the rec when it still exists, to re-serve it with what has
// changed. Returns the calendar item id, or "" when the hub has no calendar.
func (f *Flow) DeferCheckIn(rec Rec) (string, error) {
	if f.Cal == nil {
		return "", nil
	}
	// Deferred again: one check-in per rec. Same day keeps the item; a new
	// day moves it (the old one closes with a note saying where it went).
	source := "hub:rec:" + rec.ID
	if prev, ok := f.Cal.OpenBySource(source); ok {
		if prev.Day == rec.ReviewOn {
			return prev.ID, nil
		}
		f.Cal.Resolve(prev.ID, "dismissed", "hub", "moved to "+rec.ReviewOn)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The owner deferred rec %s (%s) to today", rec.ID, rec.Title)
	if n := strings.TrimSpace(rec.DecisionNote); n != "" {
		fmt.Fprintf(&b, " — their note when they did: %s", n)
	}
	b.WriteString(".\n\nThe hub has put it back on their list already. Your job: read the record (`lifectl rec " + rec.ID + "`), check what has changed since it was filed (price, need, the evidence in `because`), and either raise a decision ask that says in three lines whether it still stands and what it costs now, or supersede it with a better rec (`lifectl rec add … --supersedes " + rec.ID + "`) if the idea has moved on. Do not just re-argue the original.")
	it := calendar.Item{Title: "Check back on: " + rec.Title, Detail: b.String(), Kind: "agent", Day: rec.ReviewOn,
		GoalID: rec.GoalID, Source: source, ThreadID: SourceThread(rec)}
	added, err := f.Cal.Add(it)
	if err != nil && it.ThreadID != "" {
		// The filing session is gone: a fresh one picks it up that day.
		it.ThreadID = ""
		added, err = f.Cal.Add(it)
	}
	if err != nil {
		return "", err
	}
	return added.ID, nil
}

// DeliverDecision hands the owner's accept/decline to an agent — the point of
// the whole flow: whatever they type into the box goes to an agent. Default
// destination is the
// session that filed the rec, which already knows the argument; "new" opens
// a fresh one with the full brief. A source session that no longer exists
// falls back to a new session rather than swallowing their words.
// `at` (zero = now) is when the session is woken — the chat composer's
// "in an hour / tomorrow" applies to a rec answer too. `attachments` are
// blob refs (a screenshot: "I already have this") that ride in the same
// message as the note, on either road (chat is image-heavy).
func (f *Flow) DeliverDecision(rec Rec, want string, at time.Time, attachments []string) (delivered, threadID, errMsg string) {
	rel := f.Related(rec)
	outcome, text := rec.Status, strings.TrimSpace(rec.DecisionNote)
	if !threads.CarriesOutcome("rec", outcome) {
		// done / expired / reopened carry no verdict word the prompts engine
		// knows; the status travels as a line instead.
		outcome, text = "", strings.TrimSpace("Marked "+rec.Status+": "+rec.Title+"\n"+text)
	}
	return f.Deliver(rec, want, outcome, text, at, StarterFor(rec, rel), attachments)
}

// DeliverReply: the owner's note without a verdict, received exactly the way
// a decision's note is. Same two destinations; the rec stays undecided.
func (f *Flow) DeliverReply(rec Rec, note, want string, at time.Time, attachments []string) (delivered, threadID, errMsg string) {
	rel := f.Related(rec)
	return f.Deliver(rec, want, "", note, at, ReplyStarter(rec, note, rel), attachments)
}

// Deliver hands one answer about a rec to a session. Into the session that
// filed it (when asked for and still there) it goes as a PROMPT answering
// `rec:<id>` — the owner's words as the message, the verdict as `outcome`,
// the frame rendered from the row when the session wakes (Manager.RecHeader)
// — so the chat shows "↩ Accepted · <rec>" over their note exactly like an
// answered ask card. Otherwise `starter` opens a new one, linked to the rec
// like any minted item. The session exists either way; a failed link is
// reported, not hidden. Attachments ride in that one message on either
// road — never as a second message after it.
func (f *Flow) Deliver(rec Rec, want, outcome, text string, at time.Time, starter Starter, attachments []string) (delivered, threadID, errMsg string) {
	if want == "source" && f.Thr != nil {
		if src := SourceThread(rec); src != "" {
			if th, err := f.Thr.Get(src); err == nil && th.Status != "archived" {
				_, err := f.Thr.Queue(threads.Prompt{Author: "owner", Target: src, InReplyTo: "rec:" + rec.ID,
					Outcome: outcome, Text: text, NotBefore: at, Attachments: attachments})
				if err == nil {
					return "source", src, ""
				}
				log.Printf("recs: relay %s to %s: %v (starting a new session instead)", rec.ID, src, err)
			}
		}
	}
	t := deliver.Target{Project: "life", GoalID: rec.GoalID, Starter: starter.Text(), Attachments: attachments}
	id, how, err := f.deliverer().ToSession(t)
	if err != nil {
		return "", "", err.Error()
	}
	if how == "new" {
		if _, err := f.Store.Link(rec.ID, id); err != nil {
			log.Printf("recs: link %s to session %s: %v", rec.ID, id, err)
			return how, id, "delivered to a new session, but linking it to the rec failed: " + err.Error()
		}
	}
	return how, id, ""
}

// Related resolves what a rec points at into lines a session can act on:
// the record names ids and a source thread, and a new session had to look
// every one of them up itself; as much usable information from the rec and
// its original thread as possible rides along. A missing store or a dangling id costs a line, never the
// delivery.
func (f *Flow) Related(rec Rec) Related {
	var rel Related
	seen := map[string]bool{rec.ID: true}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, id := range strings.Split(rec.Links, ",") {
		add(id)
	}
	add(rec.PrevID)
	for _, m := range refPattern.FindAllString(rec.Title+" "+rec.Detail+" "+rec.Because+" "+rec.Expect+" "+rec.DecisionNote, -1) {
		add(m)
	}
	for _, id := range ids {
		if line := f.RefLine(id); line != "" {
			rel.Linked = append(rel.Linked, line)
		}
	}
	// The rest of the plan for this goal: what is open on the calendar in
	// the next two months that the rec does not itself name.
	if f.Cal != nil && rec.GoalID != "" {
		today := time.Now().Format("2006-01-02")
		if items, err := f.Cal.List(today, time.Now().AddDate(0, 0, 60).Format("2006-01-02"), ""); err == nil {
			for _, it := range items {
				if it.GoalID != rec.GoalID || seen[it.ID] || (it.State != "scheduled" && it.State != "fired") {
					continue
				}
				if len(rel.Calendar) == 10 {
					rel.Calendar = append(rel.Calendar, "… more: `lifectl cal`")
					break
				}
				rel.Calendar = append(rel.Calendar, CalLine(it))
			}
		}
	}
	if src := SourceThread(rec); src != "" && f.Thr != nil {
		if t, err := f.Thr.Get(src); err == nil {
			line := fmt.Sprintf("session `%s` — %q, %s", t.ID, t.Title, t.Status)
			if t.Schedule != "" {
				line += ", checks in " + t.Schedule
			}
			rel.Source = line + fmt.Sprintf(" (`lifectl thread %s` reads it)", t.ID)
			if msgs, err := f.Thr.Messages(src, 12); err == nil {
				for i := len(msgs) - 1; i >= 0; i-- {
					if m := msgs[i]; m.Role == "claude" && m.Kind != "error" && strings.TrimSpace(m.Text) != "" {
						rel.SourceSaid = fmt.Sprintf("(%s) %s", m.TS.Local().Format("2006-01-02"), Cut(m.Text, 900))
						break
					}
				}
			}
		}
	}
	return rel
}

// refPattern: the ids a rec's prose names — calendar items, other recs,
// asks and proposals — in the form every surface prints them. A proposal's
// id is the actions queue's `YYYYMMDD-HHMMSS-hex` (actions.newID), not an
// `act-` prefix; the old pattern could never link one (2026-08-28). What
// kind each match is comes from store.KindOf, never from a prefix here.
var refPattern = regexp.MustCompile(`\b(cal|rec|ask)-[0-9a-f]{4,8}\b|\b\d{8}-\d{6}-[0-9a-f]{8}\b`)

// CancelCheckIn closes the "check back on" agent item a deferral minted, once
// the rec is decided for real. Without it, accepting on the 1st a rec deferred
// to the 15th still woke a session on the 15th to re-argue a settled question.
func (f *Flow) CancelCheckIn(rec Rec) {
	if f.Cal == nil {
		return
	}
	if prev, ok := f.Cal.OpenBySource("hub:rec:" + rec.ID); ok {
		f.Cal.Resolve(prev.ID, "dismissed", "hub", "rec "+rec.Status+" before its check-in")
	}
}

// RefLine is one id as it stands today, or "" when nothing answers to it.
// One lookup: store.KindOf names the table, lines knows how each reads.
func (f *Flow) RefLine(id string) string {
	if line := f.lines()[store.KindOf(id)]; line != nil {
		return line(id)
	}
	return ""
}

// lines: how a ref of each kind reads as one line. A missing store costs
// the line, as everywhere in Flow.
func (f *Flow) lines() map[string]func(id string) string {
	return map[string]func(id string) string{
		"cal": func(id string) string {
			if f.Cal == nil {
				return ""
			}
			it, err := f.Cal.Get(id)
			if err != nil {
				return ""
			}
			return CalLine(it)
		},
		"rec": func(id string) string {
			r, err := f.Store.Get(id)
			if err != nil {
				return ""
			}
			line := fmt.Sprintf("%s · rec · %s", r.ID, r.Status)
			if r.DecidedAt != nil {
				line += " " + r.DecidedAt.Local().Format("2006-01-02")
			}
			line += " · " + r.Title
			if n := strings.TrimSpace(r.DecisionNote); n != "" {
				line += " · owner's note: " + Cut(n, 200)
			}
			if r.Outcome != "" {
				line += " · scored " + r.Outcome
			}
			return line
		},
		"ask": func(id string) string {
			if f.Thr == nil {
				return ""
			}
			a, err := f.Thr.GetAsk(id)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%s · ask/%s · %s · %s", a.ID, a.Kind, a.State, a.Title)
		},
		"action": func(id string) string {
			if f.Acts == nil {
				return ""
			}
			a, err := f.Acts.Get(id)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%s · proposal/%s · %s · %s", a.ID, a.Kind, a.State, a.Title)
		},
		"thread": func(id string) string {
			if f.Thr == nil {
				return ""
			}
			t, err := f.Thr.Get(id)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("session `%s` · %s · %q", t.ID, t.Status, t.Title)
		},
	}
}

// CalLine: a calendar item in one line — who acts, when, whether it is
// still to do, and what it says.
func CalLine(it calendar.Item) string {
	when := it.Day
	if it.At != "" {
		when += " " + it.At
	}
	line := fmt.Sprintf("%s · %s · %s · %s · %s", it.ID, it.Kind, when, it.State, it.Title)
	if it.Resolution != "" {
		line += " (" + Cut(it.Resolution, 120) + ")"
	}
	return line
}

// Cut trims a text to n runes on a word boundary, one line.
func Cut(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	c := string(r[:n])
	if i := strings.LastIndexAny(c, " \n"); i > n/2 {
		c = c[:i]
	}
	return c + "…"
}
