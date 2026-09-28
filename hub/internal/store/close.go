package store

// close.go — THE vocabulary of answering. Every word a surface prints about
// closing something — the buttons on a card (`outcomes`), the one-word tag a
// card wears ("decide"), the "↩ Accepted · …" line under the owner's answer,
// the "Read: …" a record row is prefixed with — is written here once, and
// asks, proposals, recs, calendar entries and learn items carry it on the
// wire. Neither client keeps a table of its own (no rule written twice). Where the console and
// the phone used to say different things, the console's words won.

// Reply is the words-only answer: value "", the card stays open.
var Reply = Outcome{Value: "", Label: "Reply"}

func oc(value, label string) Outcome { return Outcome{Value: value, Label: label} }

// askOutcomes is the response vocabulary, per ask kind, decisive first and
// "Reply" last — the one order every row and chip strip draws. A `read`
// card has exactly one outcome, and it is not a choice: the owner read it.
// `error` cards have none: they carry Restart, not Respond.
var askOutcomes = map[string][]Outcome{
	"read":     {oc("done", "Read it")},
	"decision": {oc("done", "Decided"), oc("wont", "Not deciding"), Reply},
	"access":   {oc("done", "Granted"), oc("wont", "Won't grant"), Reply},
	"physical": {oc("done", "I did this"), oc("wont", "Won't do"), Reply},
	"install":  {oc("done", "Installed"), oc("wont", "Won't install"), Reply},
	"other":    {oc("done", "I did this"), oc("wont", "Won't do"), Reply},
	"error":    {},
}

// AskOutcomes returns the answer chips for an ask kind (an unknown kind gets
// `other`'s). Always a non-nil copy so the JSON is `[]`, never `null`.
func AskOutcomes(kind string) []Outcome {
	o, ok := askOutcomes[kind]
	if !ok {
		o = askOutcomes["other"]
	}
	return append([]Outcome{}, o...)
}

// askVerbs: what an ask wants from the owner, in one word — the chat card's head
// line and the Sessions cell's corner, on both surfaces.
var askVerbs = map[string]string{"read": "read", "install": "install", "decision": "decide", "access": "grant",
	"physical": "do", "other": "do", "error": "restart"}

// AskVerb: the kind's one word; "for you" for a kind the table does not know.
func AskVerb(kind string) string {
	if v, ok := askVerbs[kind]; ok {
		return v
	}
	return "for you"
}

// ActionOutcomes: a proposal's buttons. A session's proposal is answered
// through its chat bar (Approve · Deny · Reply); a job's has no session to
// talk to, so it is Approve · Deny, decided from the card.
func ActionOutcomes(session bool) []Outcome {
	o := []Outcome{
		{Value: "approved", Label: "Approve", Hint: "approved the moment you send; the session carries it out with your note"},
		{Value: "denied", Label: "Deny", Hint: "denied; your reason goes to the session"},
	}
	if session {
		o = append(o, Outcome{Value: "", Label: "Reply", Hint: "the proposal stays open — your words go to the session"})
	}
	return o
}

// RecOutcomes: a rec's three answers, each saying on hover what it does.
func RecOutcomes() []Outcome {
	return []Outcome{
		{Value: "accepted", Label: "Accept", Hint: "recorded as accepted; the session carries it out with your note"},
		{Value: "declined", Label: "Decline", Hint: "recorded as declined; your reason goes to the session"},
		{Value: "", Label: "Reply", Hint: "the rec stays open — your words go to the session"},
	}
}

// StepOutcomes: one of the owner's own calendar steps with a session behind it —
// closed with words (Done · Won't do), or just talked about (Reply).
func StepOutcomes() []Outcome {
	return []Outcome{oc("done", "Done"), oc("wont", "Won't do"), Reply}
}

// TickOutcomes: a completion, not a conversation — homework, a chore, a learn
// item: tick it off without a session, or start a session about it. Did it / Skip close it; Send (words only) keeps it as it is.
// `open` false (a learn item already done) leaves Send alone.
func TickOutcomes(open bool) []Outcome {
	send := Outcome{Value: "", Label: "Send", Hint: "it stays open — your words start a session about it"}
	if !open {
		return []Outcome{send}
	}
	return []Outcome{
		{Value: "done", Label: "Did it", Hint: "ticked off — with words, they go to a session about it"},
		{Value: "wont", Label: "Skip", Hint: "skipped — with words, they go to a session about it"},
		send,
	}
}

// ReplyLabel: the verdict word on the "↩ <label> · <card>" line under a
// message that answered a card. An ask's is the card's own button word
// (askKind); a rec's is the Recs page's; a calendar step's is the row's.
func ReplyLabel(refKind, outcome, askKind string) string {
	fallback := map[string]string{"done": "Done", "wont": "Won't do", "approved": "Approved", "denied": "Denied", "": "Replied"}
	var table map[string]string
	switch refKind {
	case "ask":
		if askKind != "" {
			for _, o := range AskOutcomes(askKind) {
				if o.Value == outcome {
					return o.Label
				}
			}
		}
	case "rec":
		table = map[string]string{"accepted": "Accepted", "declined": "Declined", "deferred": "Later", "": "Replied"}
	case "cal":
		table = map[string]string{"done": "Did it", "wont": "Won't do", "": "About"}
	}
	if l, ok := table[outcome]; ok {
		return l
	}
	if l, ok := fallback[outcome]; ok && refKind != "rec" {
		return l
	}
	return outcome
}

// The record verbs: a closed thing drawn as what the owner (or an agent) did —
// "Read: …", "Approved: …" — at the minute it happened.

// AskDidVerb: what closing that KIND of ask meant — a read card is read
// whether they tapped done or dismiss, a decision decided, an access ask
// granted, a build installed; a non-read ask they dismissed was skipped.
func AskDidVerb(kind, state string) string {
	switch {
	case kind == "read":
		return "Read"
	case state == "dismissed":
		return "Skipped"
	case kind == "install":
		return "Installed"
	case kind == "decision":
		return "Decided"
	case kind == "access":
		return "Granted"
	}
	return "Did"
}

// Owner reports whether a doer (a resolved_by, decided_via or event actor) is
// the owner — from one of their surfaces (the phone app, the web console,
// lifectl) or named outright — rather than the hub, the gate or a session.
// The one actor set: an action's record verb and the calendar's red "mine"
// lane both ask it.
func Owner(by string) bool {
	switch by {
	case "owner", "app", "web", "cli":
		return true
	}
	return false
}

// Standing: where a row stands with the owner, said once by the hub so
// neither client keeps a list of which states are open.
// Asks, proposals and recs carry it flat on the wire (open, closed, folded,
// lane); a calendar entry carries closed, lane and mark (stampEntry).
//   - Open: the owner's move — their buttons show.
//   - Closed: finished — ✓/✕, no buttons. An answered ask is neither (their
//     words went in and the card waits on its session), and so is a parked
//     (deferred) rec.
//   - Folded: a silent close drawn as one grey line with Reopen —
//     "dismissed", or "read" for a read card closed with its own button.
//   - Lane: whose colour it wears — mine | chores | homework | recs | agents.
type Standing struct {
	Open   bool
	Closed bool
	Folded string
	Lane   string
}

// DoerLane: a record's lane is its doer's — red when the owner did it (Owner), grey
// when the hub, the gate or a session did.
func DoerLane(by string) string {
	if Owner(by) {
		return "mine"
	}
	return "agents"
}

// Chore: a dated step of the owner's that no session is behind (watering
// the plants, an errand they filed themselves) — the chores lane, apart from a step a session waits on. It closes with a tick, not words.
func Chore(kind, day, thread string) bool {
	return kind == "owner" && day != "" && (thread == "" || thread == "calendar")
}

// AskStanding: open and answered are live; done, dismissed and superseded are
// closed. A practice card is homework's and a chore's card the chores' — both
// their whole life; any other closed card is its closer's.
func AskStanding(kind, class, state, resolution, by string, chore bool) Standing {
	s := Standing{Open: state == "open" || state == "", Lane: "mine"}
	s.Closed = !s.Open && state != "answered"
	switch {
	case state == "dismissed":
		s.Folded = "dismissed"
	case kind == "read" && state == "done" && resolution == AskOutcomes("read")[0].Label:
		s.Folded = "read"
	}
	switch {
	case class == "practice":
		s.Lane = "homework"
	case chore:
		s.Lane = "chores"
	case s.Closed:
		s.Lane = DoerLane(by)
	}
	return s
}

// ActionStanding: a proposal is the owner's until decided; after that it is a
// record, its decider's.
func ActionStanding(state, decidedVia string) Standing {
	s := Standing{Open: state == "proposed" || state == "", Lane: "mine"}
	s.Closed = !s.Open
	if state == "dismissed" {
		s.Folded = "dismissed"
	}
	if s.Closed {
		s.Lane = DoerLane(decidedVia)
	}
	return s
}

// RecDismissed is the note that marks an expired rec as dismissed by the owner
// (Dismiss is not an outcome: it closes the rec silently).
const RecDismissed = "dismissed"

// RecStanding: a proposed rec is open; a deferred one is parked — neither open
// nor closed, like an answered ask: it keeps its buttons (it may be decided
// early) but is off the open list until it wakes. A rec is purple — the recs
// lane — its whole life.
func RecStanding(status, note string) Standing {
	s := Standing{Open: status == "proposed", Lane: "recs"}
	s.Closed = !s.Open && status != "deferred"
	if status == "expired" && note == RecDismissed {
		s.Folded = "dismissed"
	}
	return s
}

// Wont: the refusals — a state that ended without the thing being done.
func Wont(state string) bool {
	switch state {
	case "dismissed", "declined", "denied", "failed", "expired":
		return true
	}
	return false
}

// CalMark: a calendar row's glyph in the one vocabulary both grids draw —
// "wont" (✕ and the strike: refused or fell over), "done" (✓), "todo" (○, an
// open row of the owner's lane), or "" (no glyph).
func CalMark(state string, did bool, lane string) string {
	switch {
	case Wont(state):
		return "wont"
	case state == "done" || state == "accepted" || did:
		return "done"
	case lane == "mine" || lane == "homework" || lane == "chores":
		return "todo"
	}
	return ""
}

// ActionDidVerb: what the decider did — Approved even once the action has
// since run, because the running was the hub's part (byOwner = decided from one of
// the owner's surfaces).
func ActionDidVerb(state string, byOwner bool) string {
	switch {
	case state == "denied":
		return "Denied"
	case state == "failed":
		return "Failed"
	case byOwner:
		return "Approved"
	case state == "running":
		return "Running"
	}
	return "Ran"
}

// RecDidVerb: a decided rec's record word ("" = not a record).
func RecDidVerb(status string) string {
	return map[string]string{"accepted": "Accepted", "declined": "Declined", "done": "Did"}[status]
}

// RecScoreVerb: a scored rec's record word.
func RecScoreVerb(outcome string) string {
	if v, ok := map[string]string{"worked": "Worked", "mixed": "Mixed", "failed": "Didn't work", "unclear": "Unclear"}[outcome]; ok {
		return v
	}
	return "Scored"
}

// CalDidVerb: a closed calendar item's record word.
func CalDidVerb(kind, state string) string {
	switch {
	case state == "dismissed":
		return "Skipped"
	case kind == "agent":
		return "Ran"
	case kind == "note":
		return "Read"
	}
	return "Did"
}
