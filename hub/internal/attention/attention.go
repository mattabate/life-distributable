// Package attention is the ONE definition of "your turn": what is waiting on
// the owner, bundled by session, on a given surface. Both clients used to
// compute this themselves and printed different numbers for one pile of asks.
// The hub now builds the board and the clients draw it:
// GET /api/v1/board?surface=web|mobile|desktop.
//
// The rules below read
// `store.Dated` rows, the same read model `calendar.Agenda` places on days —
// "what is waiting" and "what is dated" are one query over one row shape now,
// not two queries that drifted. Each package projects its own objects
// (`threads.Row`, `actions.Row`, `recs.Row`); nothing here knows a table. The
// whole Ask/Action still rides in `Dated.Obj` and goes out in the JSON, so
// what either surface prints is unchanged — `attention_test.go` and
// `server/board_test.go` pin that, and neither moved.
//
// The rules come from docs/ASKS.md / shared/api.md ("Surface"):
//
//   - Neither surface holds anything back. Both count every active ask and
//     every proposal; a running session's items are counted too and its
//     bundle sinks under the idle ones. A card is answerable the moment it
//     is raised, and the owner's answer is delivered into the live turn as a
//     steering message, so the agent keeps working mid tool chain.
//   - ONE BOARD, WHATEVER THE SURFACE: the phone mirrors the web UI.
//     `surface` still rides the ask (it decides which link the phone draws
//     as a button), but nothing here reads it: both surfaces get the same
//     count, the same bundles, the same order.
//   - THE ONE EXCEPTION: AN INSTALL CARD IS THE DEVICE'S. The phone's board
//     (`mobile`) leaves out the Mac's build, the desktop app's (`desktop`)
//     leaves out the phone's; the web console (`web`) lists both.
//   - An ask the owner has answered is the agent's move: it is on neither
//     board, only in its chat.
//   - On both, dated steps (a fired calendar item is its own card) list on their own
//     with their date, never under the session that owns their thread.
package attention

import (
	"sort"
	"strconv"
	"strings"

	"life/hub/internal/actions"
	"life/hub/internal/calendar"
	"life/hub/internal/recs"
	"life/hub/internal/store"
	"life/hub/internal/threads"
)

// Session is one bundle on the board: a session with something waiting.
// Approvals lead inside it (the session is blocked on them); First is what
// the bundle heading opens on — the approval card inline in the chat, or
// the first ask.
type Session struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	N       int              `json:"n"`
	First   string           `json:"first"`
	Running bool             `json:"running"`
	Actions []actions.Action `json:"actions"`
	Asks    []threads.Ask    `json:"asks"`
	// Steps: the owner's own dated work that sits in THIS session's chat (a
	// fired `owner`-assigned item keeps its session). Still the calendar's —
	// not in N, not counted, never why a session is listed — but a row that
	// is on the page draws them after its own cards, so the row and the chat
	// agree.
	Steps []threads.Ask `json:"steps"`
}

// Badges: one number per tab — the console's red ovals, the phone's tab
// counts. Sessions carries none by design. Recs is a
// COUNT, not a notification: recs are pulled, never pushed.
type Badges struct {
	YourTurn int `json:"your_turn"`
	Calendar int `json:"calendar"`
	Recs     int `json:"recs"`
}

// Board is the surface's view of what needs the owner.
type Board struct {
	Surface string `json:"surface"`
	// Count is the number the tab and the page header both print.
	Count int `json:"count"`
	// Working: counted items that sit on a running session's row ("N for
	// you" under Working) rather than as cards of their own.
	Working int `json:"working"`
	// Calendar: dated steps that came due — the calendar's work, listed first.
	Calendar []threads.Ask `json:"calendar"`
	// Sessions: one bundle per session, in the order they should be drawn.
	Sessions []Session `json:"sessions"`
	// ForYou: per thread id, what that session's row prints as "N for you".
	ForYou map[string]int `json:"for_you"`
	// Reads: per thread id, how many of ForYou are `read` asks. Equal to
	// ForYou means the row asks the owner only to read — the clients draw
	// that blue ("N to read"), never red.
	Reads map[string]int `json:"reads"`
	// Installs: per thread id, how many of ForYou are `install` asks. Equal
	// to ForYou means the row only asks the owner to install a build — the
	// clients draw that teal ("N to install"), the shade the install cell
	// already wears, never the red "for you".
	Installs map[string]int `json:"installs"`
	// First: per thread id, the card its row opens on (approval, else ask).
	First map[string]string `json:"first"`
	// Todo: per thread id, the TITLE of that First card — the one line a
	// session card prints under its name instead of the last reply.
	Todo map[string]string `json:"todo"`
	// Detail: per thread id, the BODY of that First card, capped at
	// DetailCap, so a session row shows more than a title. Both surfaces
	// clamp it to three lines under the todo.
	Detail map[string]string `json:"detail"`
	// Open: per listed session, every open card its chat draws — ForYou plus
	// its Steps. A row draws a couple and says "and N more" for the rest,
	// so the rest has to be a number both surfaces share.
	Open map[string]int `json:"open"`
	// Headings: the Sessions page's groups in draw order, each with the
	// words its heading prints. Section files a session under one of them by
	// key; a session absent from Section is not on the page. Pills: the capsules a
	// session's card wears, in order, for every session with a card open or a
	// turn running; any other session wears its own `pill` (GET /threads).
	// Both clients used to derive all three and printed different words for
	// one board.
	Headings []Heading                 `json:"headings"`
	Section  map[string]string         `json:"section"`
	Pills    map[string][]threads.Pill `json:"pills"`
	Badges   Badges                    `json:"badges"`
}

// Heading is one group on the Sessions page. N is how many sessions it holds
// and Show how many of them are drawn (0 = all); Count is what follows the
// label, "1 in 1 session · 1 working".
type Heading struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count string `json:"count"`
	N     int    `json:"n"`
	Show  int    `json:"show"`
}

// headings: EVERY SESSION HAS AT MOST ONE HEADING, by a ladder — running →
// Working; else any card open for the owner → Your turn; else it is off the
// page. There is no Recent group: a session is on the page because it is
// working or waiting on the owner, and everything else is one tap away under
// All sessions (the phone's AllThreadsView, the console's #/sessions/all).
// A session whose cards are reads or installs sits under Your turn with the
// others, in the list's own order — one list, no separate "To read"
// heading. What tells them apart is the pills, one per class of card
// (threads.CardsPills): a read beside an install wears "1 to read" and "1 to
// install", never a red "2 for you". Every count is of CARDS, the unit the
// badge counts, so Your turn + Working's "for you" add up to it.
func (b *Board) headings(ths []threads.Thread) {
	b.Section, b.Pills = map[string]string{}, map[string][]threads.Pill{}
	var turnN, workN, turnCards int
	for _, t := range ths {
		n := b.ForYou[t.ID]
		running := t.Status == "running"
		var pills []threads.Pill
		// A session whose card is in the owner's ears RIGHT NOW leads with
		// "speaking" — so the voice just heard has a name on the list, and a
		// row to click.
		if t.Speaking {
			pills = append(pills, threads.Pill{Word: "speaking", Tone: "speaking"})
		} else if t.WaitingToSpeak {
			// Its line is queued behind the owner's microphone, a replay or a
			// pitch round: show what is waiting before it starts speaking.
			pills = append(pills, threads.Pill{Word: "waiting to speak", Tone: "waiting"})
		}
		if running {
			pills = append(pills, threads.StatusPill(t.Status))
		}
		pills = append(pills, threads.CardsPills(n, b.Reads[t.ID], b.Installs[t.ID])...)
		if len(pills) > 0 {
			b.Pills[t.ID] = pills
		}
		switch {
		case running:
			b.Section[t.ID] = "working"
			workN++
		case n > 0:
			b.Section[t.ID] = "your_turn"
			turnN++
			turnCards += n
		case t.Speaking || t.WaitingToSpeak:
			// A card replayed after it was closed still names its session on
			// the list while it plays.
			b.Section[t.ID] = "working"
			workN++
		}
	}
	inSessions := func(cards, n int) string {
		s := strconv.Itoa(cards) + " in " + strconv.Itoa(n) + " session"
		if n != 1 {
			s += "s"
		}
		return s
	}
	turn := inSessions(turnCards, turnN)
	work := strconv.Itoa(workN)
	if b.Working > 0 {
		turn += " · " + strconv.Itoa(b.Working) + " working"
		work += " · " + strconv.Itoa(b.Working) + " for you"
	}
	b.Headings = []Heading{
		{Key: "your_turn", Label: "Your turn", Count: turn, N: turnN},
		{Key: "working", Label: "Working", Count: work, N: workN},
	}
}

// Build is the pure rule: the same inputs give the same board. Asks are the
// active ones (open+answered — BuildRows drops the answered; `held` already
// decided by the hub), acts the
// proposed approvals, ths every non-archived thread. It projects its inputs
// into read-model rows and hands them to BuildRows — the objects themselves
// are only carried, never read, from here down.
func Build(surface string, asks []threads.Ask, acts []actions.Action, ths []threads.Thread) Board {
	rows := make([]store.Dated, 0, len(asks)+len(acts))
	for _, x := range acts {
		rows = append(rows, actions.Row(x))
	}
	for _, a := range asks {
		rows = append(rows, threads.Row(a))
	}
	return BuildRows(surface, rows, ths)
}

// BuildRows is the board over the shared read model. Rows of any other kind
// (`rec`, `cal`, `run`, `job`) are the agenda's business and are ignored here:
// a rec is pulled, never pushed, and a scheduled item is not waiting on the
// owner until it fires and becomes its own card.
//
// An ask of class `practice` or `step` (the owner's own dated work — a daily
// practice, an appointment) is listed under `calendar` and NOTHING else: not
// counted, not a session's "for you", not the card a row opens on. Sessions
// is where something is blocked on the owner; the Calendar tab already
// counts those rows.
func BuildRows(surface string, rows []store.Dated, ths []threads.Thread) Board {
	if surface != "mobile" && surface != "desktop" {
		surface = "web"
	}
	b := Board{Surface: surface, Calendar: []threads.Ask{}, Sessions: []Session{},
		ForYou: map[string]int{}, Reads: map[string]int{}, Installs: map[string]int{}, First: map[string]string{},
		Todo: map[string]string{}, Detail: map[string]string{}, Open: map[string]int{}}
	running := map[string]bool{}
	title := map[string]string{}
	for _, t := range ths {
		running[t.ID] = t.Status == "running"
		title[t.ID] = t.Title
	}
	var askRows, ownerRows, actRows []store.Dated
	for _, r := range rows {
		switch {
		// AN ANSWERED ASK IS THE AGENT'S MOVE, NOT THE OWNER'S. It leaves the
		// board whole — count, pills, bundle, steps, the card a row opens on —
		// and lives on in its chat, where it says "waiting on agent".
		case r.Kind == "ask" && r.State == "answered":
		// AN INSTALL CARD SHOWS ONLY ON THE DEVICE IT UPDATES: the phone's
		// board drops the Mac's build, the desktop app's drops the phone's,
		// and the web console lists both. That is the ONE thing `surface`
		// decides; every other card is on every board.
		case r.Kind == "ask" && hiddenInstall(surface, r):
		case r.Kind == "ask" && threads.OwnerClass(r.AskClass):
			ownerRows = append(ownerRows, r)
		case r.Kind == "ask":
			askRows = append(askRows, r)
		case r.Kind == "action":
			actRows = append(actRows, r)
		}
	}
	// The same board for every surface but for the install rule
	// above; `surface` is otherwise only echoed.
	b.build(askRows, actRows, running, title)
	// The owner's own dated work joins the calendar list after the counted rows,
	// oldest day first — the list is the same on both surfaces.
	sort.SliceStable(ownerRows, func(i, j int) bool { return ownerRows[i].Day < ownerRows[j].Day })
	b.Calendar = append(b.Calendar, askObjs(ownerRows)...)
	b.steps(ownerRows)
	b.Badges.YourTurn = b.Count
	b.headings(ths)
	return b
}

// hiddenInstall: an install card for the other device. `mobile` never lists
// the Mac's build, `desktop` never lists the phone's; `web` lists both.
func hiddenInstall(surface string, r store.Dated) bool {
	if r.AskKind != "install" {
		return false
	}
	switch threads.InstallTarget(r.AskKind, r.Title) {
	case "mac":
		return surface == "mobile"
	case "phone":
		return surface == "desktop"
	}
	return false
}

// steps hands each LISTED session the dated steps that sit in its chat, and
// totals what its row stands for. The owner's work alone never lists a
// session (the BuildRows rule above holds): only a bundle that a counted card made gets
// them, and nothing here touches Count, ForYou or First.
func (b *Board) steps(owners []store.Dated) {
	by := map[string][]store.Dated{}
	for _, r := range owners {
		by[r.ThreadID] = append(by[r.ThreadID], r)
	}
	for i := range b.Sessions {
		s := &b.Sessions[i]
		s.Steps = askObjs(by[s.ID])
		if s.ID != "" {
			b.Open[s.ID] = b.ForYou[s.ID] + len(s.Steps)
		}
	}
}

// tally counts one ask on its session's row: the total, and the shares that
// pick the pill's colour — reads (blue) and installs (teal).
func (b *Board) tally(a store.Dated, running map[string]bool) {
	b.ForYou[a.ThreadID]++
	switch a.AskKind {
	case "read":
		b.Reads[a.ThreadID]++
	case "install":
		b.Installs[a.ThreadID]++
	}
	if running[a.ThreadID] {
		b.Working++
	}
}

// build: every active ask and every proposal, bundled by session with
// approvals first, asks in the order they were raised. A running session's
// items are still counted — its row wears "N for you" under Working — which
// is what Working reports. One rule for both surfaces, so the two lists match.
func (b *Board) build(asks, acts []store.Dated, running map[string]bool, title map[string]string) {
	b.Count = len(asks) + len(acts)
	// A dated card a SESSION raised (`lifectl ask add --on`) is listed on the
	// calendar AND drawn as a cell on its session's row, like every other
	// card. One card, one look.
	var cal []store.Dated
	for _, a := range asks {
		if a.CalID != "" {
			cal = append(cal, a)
		}
	}
	b.Calendar = askObjs(cal)
	b.Sessions = bundle(acts, asks, running, title)
	// A running session's bundle sinks under the idle ones: it is still
	// counted and still steerable here, but the sessions that have STOPPED and
	// wait on the owner come first. The order inside each class is unchanged.
	sort.SliceStable(b.Sessions, func(i, j int) bool { return !b.Sessions[i].Running && b.Sessions[j].Running })
	for _, a := range asks {
		b.tally(a, running)
	}
	for _, x := range acts {
		b.ForYou[x.ThreadID]++
		if running[x.ThreadID] {
			b.Working++
		}
	}
	b.First, b.Todo, b.Detail = firstByThread(acts, asks)
}

// askObjs / actObjs: the rows' own objects, for the payload the clients
// decode. A row whose Obj is not the type its Kind promises is dropped rather
// than faked — that can only be a projection bug, and half a card is worse
// than no card.
func askObjs(rows []store.Dated) []threads.Ask {
	out := make([]threads.Ask, 0, len(rows))
	for _, r := range rows {
		if a, ok := r.Obj.(threads.Ask); ok {
			out = append(out, a)
		}
	}
	return out
}

func actObjs(rows []store.Dated) []actions.Action {
	out := make([]actions.Action, 0, len(rows))
	for _, r := range rows {
		if x, ok := r.Obj.(actions.Action); ok {
			out = append(out, x)
		}
	}
	return out
}

// bundle: one Session per thread, approvals' sessions first in the order the
// approvals come, then the asks' sessions in the order their first ask comes.
// A proposal from a scheduled job (no session) gets a bundle of its own.
func bundle(acts, asks []store.Dated, running map[string]bool, title map[string]string) []Session {
	var order []string
	type rows struct{ acts, asks []store.Dated }
	by := map[string]*rows{}
	get := func(id string) *rows {
		r := by[id]
		if r == nil {
			r = &rows{}
			by[id] = r
			order = append(order, id)
		}
		return r
	}
	for _, x := range acts {
		r := get(x.ThreadID)
		r.acts = append(r.acts, x)
	}
	for _, a := range asks {
		r := get(a.ThreadID)
		r.asks = append(r.asks, a)
	}
	out := make([]Session, 0, len(order))
	for _, id := range order {
		r := by[id]
		s := Session{ID: id, Running: running[id], N: len(r.acts) + len(r.asks),
			Actions: actObjs(r.acts), Asks: askObjs(r.asks)}
		switch {
		case id == "":
			s.Title = "Proposed by a scheduled job"
		case title[id] != "":
			s.Title = title[id]
		case len(r.asks) > 0 && r.asks[0].ThreadTitle != "":
			s.Title = r.asks[0].ThreadTitle
		default:
			s.Title = id
		}
		if len(r.acts) > 0 {
			s.First = r.acts[0].ID
		} else {
			s.First = lead(r.asks).ID
		}
		out = append(out, s)
	}
	return out
}

// askRank: the order the owner works a session's cards, and so the order a
// row names them — what blocks the session (0), then answers to read (1),
// then a build to install (2). The build is the phone's job and can wait for
// the next one; naming it first over a read hid the read.
func askRank(a store.Dated) int {
	switch a.AskKind {
	case "read":
		return 1
	case "install":
		return 2
	}
	return 0
}

// lead: the card a session's asks open on — the lowest askRank, first raised
// among equals. Callers pass at least one row.
func lead(asks []store.Dated) store.Dated {
	best := asks[0]
	for _, a := range asks[1:] {
		if askRank(a) < askRank(best) {
			best = a
		}
	}
	return best
}

// DetailCap: how much of a card's body rides on a session row. Three clamped
// lines is what either surface draws; the rest is one tap away, inside the
// session. A board polled every 6s does not carry a 2KB proposal per thread.
const DetailCap = 400

// firstByThread: the card a session's row opens on — its first approval,
// else its asks by askRank (blocking, read, install; first raised among
// equals) — same rule as the bundle heading. Todo is its title, detail its
// (capped) body. An install card gets no detail: "Install app build N" is the
// whole instruction, and its body only repeats the build's changelog.
func firstByThread(acts, asks []store.Dated) (first, todo, detail map[string]string) {
	first, todo, detail = map[string]string{}, map[string]string{}, map[string]string{}
	take := func(id, cardID, title, body string) {
		if _, ok := first[id]; ok {
			return
		}
		first[id], todo[id] = cardID, title
		if body = cut(body, DetailCap); body != "" {
			detail[id] = body
		}
	}
	for _, x := range acts {
		// A proposal's row carries no Detail (actions.Row says why); its case
		// is argued in the Action itself, which rides along in Obj.
		body := ""
		if a, ok := x.Obj.(actions.Action); ok {
			body = a.Detail
		}
		take(x.ThreadID, x.ID, x.Title, body)
	}
	for rank := 0; rank <= 2; rank++ {
		for _, a := range asks {
			if askRank(a) != rank {
				continue
			}
			body := a.Detail
			if a.AskKind == "install" {
				body = ""
			}
			take(a.ThreadID, a.ID, a.Title, body)
		}
	}
	return first, todo, detail
}

// cut: n characters, ending on a word where one is near, with an ellipsis so
// the row never pretends the card is short.
func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	head := string(r[:n])
	if i := strings.LastIndexAny(head, " \n\t"); i > n*3/4 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \n\t.,;:-") + "…"
}

// Service reads the stores and builds the board. Cal and Recs may be nil
// (their badges read 0); Acts may be nil on a hub without a queue.
type Service struct {
	Thr  *threads.Manager
	Acts *actions.Queue
	Cal  *calendar.Calendar
	Recs *recs.Store
}

// Board reads the one waiting-on-the-owner list (store.Items: asks, proposals, open
// recs) and builds over it; BuildRows takes the asks and proposals, and the
// Recs badge is the proposed recs among the same rows. A rec read that fails
// leaves that badge at 0, as Stats failing always did.
func (s *Service) Board(surface string) (Board, error) {
	rows, err := store.Items(s.Thr, s.Acts)
	if err != nil {
		return Board{}, err
	}
	ths, err := s.Thr.ListBrief()
	if err != nil {
		return Board{}, err
	}
	b := BuildRows(surface, rows, ths)
	if s.Cal != nil {
		b.Badges.Calendar, _ = s.Cal.DueCount()
	}
	if recs, err := store.Items(s.Recs); err == nil {
		for _, r := range recs {
			if r.State == "proposed" {
				b.Badges.Recs++
			}
		}
	}
	return b, nil
}
