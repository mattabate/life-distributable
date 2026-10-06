// One session's chat: ThreadDetail, its run activity rows and the settings
// sheet.
import SwiftUI

/// Rows drawn on open, and how many more each "Show earlier" adds.
private let rowWindow = 25

struct ThreadDetail: View {
    @Environment(HubClient.self) private var hub
    /// Paused while this is open: the chat polls its own session at 3s and
    /// the list behind it has nothing to draw until the owner goes back.
    @Environment(BoardStore.self) private var store
    @State var thread: Thread
    /// Opened from an ask card: scroll to (and highlight) the reply that raised it.
    var focusMessage: Int? = nil
    /// The card to open on — an ask or an action id: the board's `first` for a
    /// session row (the console's rule, views/threads.js), or the approval
    /// card that was tapped. Scrolled to once that card has been placed.
    var focusCard: String? = nil
    @State private var focusLanded = false
    /// Opened from a calendar row's Approve/Deny: the chat bar is armed with
    /// that pick once, on open, so the words are typed here where the composer
    /// is (the calendar sheet has none).
    var armOnOpen: ArmedReply? = nil
    @State private var armedOnOpen = false
    /// Mirrors scenePhase for the change loop (an @Environment value read
    /// from inside a long-running task is a stale copy).
    @State private var foreground = true
    @Environment(\.scenePhase) private var scenePhase
    @State private var messages: [ThreadMessage] = []
    @State private var events: [ThreadEvent] = []
    /// Replies whose folded text the owner has tapped open (there is no white
    /// cell: a reply row draws only as the "turn ended" line).
    @State private var openReplies: Set<Int> = []
    /// Long messages the owner has unfolded. Closed, a bubble past `isLongText` clamps to a screen's
    /// worth behind "Show all · N lines". Same fold as the console's
    /// `.body.long` (views/threads.js msgHTML).
    @State private var openLong: Set<Int> = []
    /// The hub's step count per message — the headline of every run block.
    /// The phone holds only the events it DRAWS (the newest page, what
    /// streams in, and any block the owner opens), so a count must not come from
    /// them: a single turn regularly runs to hundreds of tool calls.
    @State private var steps: [Int: StepCount] = [:]
    /// Blocks whose own steps are being fetched right now (one at a time).
    @State private var loadingBlocks: Set<Int> = []
    /// Every ask this session raised, shown as a card under the reply that
    /// raised it, so it can be answered here rather than from a bare id on
    /// another screen.
    @State private var asks: [Ask] = []
    /// Where each ask is drawn (see `placeAsks`): under a message, or — when
    /// it has none — at the end of the transcript.
    @State private var asksByMessage: [Int: [Ask]] = [:]
    @State private var tailAsks: [Ask] = []
    /// This session's pending approvals, drawn the same way as asks. A proposal has no message of its own, so each sits under
    /// the last message written before it — the turn that proposed it — or
    /// at the end when it is newer than everything (reply not landed yet).
    @State private var actions: [Action] = []
    @State private var actionsByMessage: [Int: [Action]] = [:]
    @State private var tailActions: [Action] = []
    /// `load` has fetched this chat's own asks and proposals at least once;
    /// until then the board's copy stands in (`seedCards`).
    @State private var cardsLoaded = false
    /// The recommendations this session filed, each a cell under the reply
    /// that filed it, answerable without going to the Recs page. Every status
    /// is drawn — a decided one is the line that says what they chose. The card's
    /// buttons open the same sheet the Recs page uses, so the decision is
    /// recorded and relayed into this chat the same way from either place.
    @State private var recs: [Rec] = []
    @State private var recsByMessage: [Int: [Rec]] = [:]
    @State private var tailRecs: [Rec] = []
    /// Another session, opened from a "sent by" corner.
    @State private var openThread: Thread?
    @State private var goals: [Goal] = []
    /// The draft lives in its own object, read only by `Composer`. As plain
    /// @State here, every keystroke — and every dictation partial, which
    /// arrives many times a second — invalidated this whole view, rebuilding
    /// the (non-lazy) message list, re-sorting `ordered` and regrouping
    /// `eventsByRun`. That is the composer getting slower the longer the text
    /// and the thread.
    @State private var draft = Draft()
    @State private var error: String?
    /// A send that the hub refused, kept apart from `error`: `load()` clears
    /// `error` on every successful reload, and the change feed reloads the
    /// chat the instant the hub records a refusal — so a refused Approve
    /// would show its reason for half a second and vanish. This one stays
    /// until the next send goes through or the owner taps ×.
    @State private var sendError: String?
    /// The refusal was the decider code: the banner offers Settings.
    @State private var sendErrorNeedsDecider = false
    @State private var showDeciderSettings = false
    @State private var showSettings = false
    @State private var attachments = AttachmentDraft()
    @State private var sending = false
    @State private var stopping = false
    /// Where the draft goes and when: this session or a new one, now or at a
    /// set time. Anything other than here-and-now is
    /// posted as a prompt instead of a message; both end up as the same row.
    /// The cards the chat bar is answering, each shown as a banner on the
    /// bar; several can be armed at once. A read's
    /// Read and a rec's Accept/Decline/Reply each ADD a row; one send
    /// answers every row, and each card closes by its own rule.
    @State private var replies: [ArmedReply] = []
    /// An empty reply asks first: "Are you sure?" → "Send without".
    @State private var confirmingEmpty = false
    @State private var confirmArchive = false
    @State private var sendTarget: RespondTarget = .thisSession
    @State private var sendWhen: RespondWhen = .now
    @State private var pickingWhen = false
    @State private var pickWhenDate = Date().addingTimeInterval(3600)
    /// The drawn rows, rebuilt only when the data behind them changes.
    /// `turnRows` sorts every message, groups every event by run and slices
    /// each run at its steering points — real work over hundreds of events —
    /// and as a computed property in `body` it ran on every single view
    /// update and made the chat slow.
    @State private var rows: [TurnRow] = []
    /// How many of the newest rows are drawn. The stack below is deliberately
    /// NOT lazy (the opening scroll needs real targets), so a 90-message
    /// session built every bubble — each one a UITextView — plus every
    /// activity block before the first frame could appear, and tore them all
    /// down again on the way back to Sessions. Older turns are one tap away.
    @State private var shown = rowWindow

    /// The owner can always write — while the agent works the message is steered
    /// into the running turn (it sees it at its next step), so the composer
    /// never locks.
    var composerPlaceholder: String {
        #if targetEnvironment(macCatalyst)
        // The console's words (composer.js), the owner 2026-09-29: the same box.
        return thread.status == "running" ? "Steer it while it works…" : "Message…"
        #else
        switch thread.status {
        case "needs_you": "Answer…"
        case "running": "Steer it (it reads this next)"
        default: "Message…"
        }
        #endif
    }
    @Environment(\.dismiss) private var dismiss
    #if targetEnvironment(macCatalyst)
    /// Pushed (a card's "sent by", All sessions) vs the pane's root: the
    /// head's ← goes back a step, or to the empty chat as the console's
    /// `#/sessions` does.
    @Environment(\.isPresented) private var pushed
    @Environment(SnapState.self) private var snap
    #endif

    /// Events grouped by run, in stream order.
    var eventsByRun: [String: [ThreadEvent]] { Dictionary(grouping: events, by: \.run_id) }
    /// The run currently in flight = the latest run id that has no reply yet.
    var liveRun: String? {
        guard thread.status == "running" else { return nil }
        let replied = Set(messages.filter { $0.role == "claude" || $0.kind == "error" }.compactMap(\.run_id))
        return messages.last { $0.run_id != nil && !replied.contains($0.run_id!) && $0.role != "claude" }?.run_id
    }
    /// Events of the live run that have no starter message in the list (shouldn't happen; shown anyway).
    var orphanLive: [ThreadEvent] {
        guard thread.status == "running", liveRun == nil else { return [] }
        let known = Set(messages.compactMap(\.run_id))
        return events.filter { !known.contains($0.run_id) }
    }

    /// Conversational order, not timestamp order. A message the owner sends while
    /// a run is in flight is stored at its own time (before that run's
    /// reply) but only becomes the *next* turn, so by timestamp it — and its
    /// activity block — sat above the previous reply, making the earlier
    /// reply look like it was mid-tool-chain and the later one orphaned.
    /// Turns are sequential and each is one run id (`<run>`, `<run>-t2`, …),
    /// so a turn draws as one piece — starter(s) → activity → reply — and
    /// turns follow one another in the order they STARTED: the ts of each
    /// run's earliest message. That key never changes, so nothing jumps when
    /// a reply lands. (Keying a run by its LATEST message instead put a
    /// steering message above the turn it followed.) Same rule as the
    /// console's `orderedMsgs` (threads.js).
    var ordered: [ThreadMessage] {
        var runStart: [String: Date] = [:]
        for m in messages { if let r = m.run_id { runStart[r] = min(runStart[r] ?? .distantFuture, m.ts) } }
        func key(_ m: ThreadMessage) -> Date { m.run_id.flatMap { runStart[$0] } ?? m.ts }
        return messages.sorted { a, b in
            let ka = key(a), kb = key(b)
            if ka != kb { return ka < kb }
            return a.ts != b.ts ? a.ts < b.ts : a.id < b.id
        }
    }

    /// The tail of the conversation — what a chat opens on. Anything older is
    /// behind "Show earlier"; a target further up (an ask card that was tapped) pulls
    /// the window back to cover it, see `widen`.
    var visibleRows: [TurnRow] { rows.count <= shown ? rows : Array(rows.suffix(shown)) }

    /// Make sure a given message is inside the drawn window — the row the
    /// thread scrolls to on open, and any ask still waiting on the owner, which
    /// must never be hidden behind "Show earlier".
    func widen(toRow id: Int?) {
        guard let id, let i = rows.firstIndex(where: { $0.m.id == id }) else { return }
        shown = max(shown, rows.count - i + 2)
    }

    /// File each ask under the reply that raised it — and, when it has no
    /// reply to sit under, under the last message before it, or at the end.
    /// An ask raised OUTSIDE a run (the hub itself, `lifectl ask add` from a
    /// terminal, a calendar item) has `message_id` null forever, and one
    /// raised in a turn that has not landed yet has none until the turn
    /// finishes; both used to draw nowhere at all, so a running session's
    /// "1 for you" chip pointed at a card that did not exist on any screen.
    /// Held asks
    /// are deliberately kept off "Your turn" while their session works, so
    /// the session itself is the only place they can appear.
    func placeAsks(_ all: [Ask]) {
        asks = all
        var by: [Int: [Ask]] = [:], tail: [Ask] = []
        let order = ordered
        let inChain = chainRefs
        let active = { (a: Ask) in !a.isClosed }
        for a in all {
            // Raised mid-run: the run block draws it where it happened, not
            // under a bubble.
            if inChain.contains("ask:" + a.id) { continue }
            // Dismissed — or a read marked Read — folds to one grey line
            // where it was, with Reopen inside, so it can still be answered.
            if let mid = a.message_id, order.contains(where: { $0.id == mid }) {
                by[mid, default: []].append(a)
                if active(a) { widen(toRow: mid) }
            } else if let host = order.last(where: { $0.ts <= a.created_at }), a.isFolded {
                by[host.id, default: []].append(a)
            } else if let host = order.last(where: { $0.ts <= a.created_at }), active(a) {
                by[host.id, default: []].append(a)
                widen(toRow: host.id)
            } else if active(a) {
                tail.append(a)  // newer than every message: the turn is still in flight
            }
            // A resolved ask with no home stays hidden: it is history, and it
            // was never drawn here anyway.
        }
        asksByMessage = by
        tailAsks = tail
    }

    /// File each pending approval under the last message before it (in
    /// conversational order), and keep that message drawn so the card is
    /// on screen when the board opens the session on it. Only one still
    /// waiting on the owner (`proposed`), or the one tapped to get here, pulls
    /// the window back: a dismissed proposal is history, and every row it
    /// drags in is paid for on open.
    func placeActions(_ pending: [Action]) {
        actions = pending
        var by: [Int: [Action]] = [:], tail: [Action] = []
        let order = ordered
        let inChain = chainRefs
        for a in pending {
            if inChain.contains("action:" + a.id) { continue } // drawn where it was proposed
            if let host = order.last(where: { $0.ts <= a.created_at }) {
                by[host.id, default: []].append(a)
                if a.open == true || a.id == focusCard { widen(toRow: host.id) }
            } else { tail.append(a) }
        }
        actionsByMessage = by
        tailActions = tail
    }

    /// File each rec under the reply that filed it — the hub names it
    /// (`message_id`) — or at the end while that reply is still being
    /// written. A rec never pulls the window back:
    /// recs are pull, the Recs tab holds every open one, and a days-old open
    /// rec made a long chat draw three times the rows on every open. Its
    /// cell is still there under its reply once "Show earlier" reaches it.
    func placeRecs(_ all: [Rec]) {
        recs = all
        var by: [Int: [Rec]] = [:], tail: [Rec] = []
        let order = ordered
        for r in all {
            if let mid = r.message_id, order.contains(where: { $0.id == mid }) {
                by[mid, default: []].append(r)
            } else {
                tail.append(r)
            }
        }
        recsByMessage = by
        tailRecs = tail
    }

    /// One rec's cell. Each way arms the chat bar with the rec and that pick
    /// (the console's composer does the same): the owner's note is typed where a
    /// message is, and the decision lands in this chat as "↩ Accepted ·
    /// <title>" over it — alongside whatever other cards are armed.
    func recCell(_ r: Rec) -> some View {
        RecCard(rec: r, decide: { outcome in
            arm(ArmedReply(kind: .rec, id: r.id, title: r.title, outcome: outcome, outcomes: r.outcomes ?? []))
        }, reload: { await load() }).id(r.id)
    }

    /// One proposal's cell: Approve · Deny · Reply each arm the chat bar
    /// with the action and that pick, exactly as a rec's buttons do. Send posts one prompt naming it; the hub decides the row.
    func approvalCell(_ a: Action) -> some View {
        ApprovalCard(a: a, inThread: true, arm: { outcome in
            arm(ArmedReply(kind: .action, id: a.id, title: a.title, outcome: outcome, outcomes: a.outcomes ?? []))
        }) { await load() }.id(a.id)
    }

    /// Arm a card without dropping the others; the same card again only
    /// changes its pick.
    func arm(_ r: ArmedReply) {
        if let i = replies.firstIndex(where: { $0.ref == r.ref }) { replies[i].outcome = r.outcome } else { replies.append(r) }
        draft.focusRequest += 1
    }

    /// A message that opened a run (as opposed to the reply that ended it).
    func startsRun(_ m: ThreadMessage) -> Bool { m.run_id != nil && m.role != "claude" && m.kind != "error" }

    /// One drawn row: a message plus the steps that belong under *it*, cut
    /// into pieces at the cards the agent raised while it worked.
    struct TurnRow: Identifiable {
        let m: ThreadMessage
        let chain: [TurnPiece]
        let live: Bool
        /// The hub's counts for this message (nil only for a run the hub has
        /// not answered for yet — then the held events are all we can say).
        let count: StepCount?
        var id: Int { m.id }
        var events: [ThreadEvent] { chain.flatMap(\.events) }
    }

    /// One piece of a run block: its steps, its own count, and the card drawn
    /// after it. `card` is nil on the tail — the steps after the last card,
    /// which is where a running session still is.
    struct TurnPiece: Identifiable {
        let id: String
        let events: [ThreadEvent]
        let count: StepCount?
        let live: Bool
        let card: String?
    }

    /// A message steered in mid-turn carries the SAME run_id as the message
    /// that started the turn, and every starter drew that run's whole event
    /// list — so the same "100 tool calls" block appeared twice, once above
    /// the steering message and once below. Each starter
    /// now shows only the steps between it and the next starter, which is
    /// what steering is for: it breaks the chain where it actually landed.
    /// Only the last starter carries the live dot.
    func computeRows() -> [TurnRow] { Perf.time("computeRows") { computeRowsNow() } }

    private func computeRowsNow() -> [TurnRow] {
        let byRun = eventsByRun, live = liveRun
        var starters: [String: [ThreadMessage]] = [:]
        for m in ordered where startsRun(m) { starters[m.run_id!, default: []].append(m) }
        var slice: [Int: [ThreadEvent]] = [:]
        var lastStarter: [String: Int] = [:]
        for (run, ms) in starters {
            let evs = byRun[run] ?? []
            lastStarter[run] = ms.last?.id
            for (i, m) in ms.enumerated() {
                // First starter also takes anything stamped before it.
                let from = i == 0 ? Date.distantPast : m.ts
                let to = i + 1 < ms.count ? ms[i + 1].ts : Date.distantFuture
                slice[m.id] = evs.filter { $0.ts >= from && $0.ts < to }
            }
        }
        return ordered.map { m in
            let isLast = m.run_id.flatMap { lastStarter[$0] } == m.id
            let isLive = isLast && m.run_id != nil && m.run_id == live
            let c = steps[m.id]
            return TurnRow(m: m, chain: pieces(m, slice[m.id] ?? [], c, isLive), live: isLive, count: c)
        }
    }

    /// Cut one run block at the cards the agent raised while it ran. The hub
    /// gives the cut and each piece's own count (`StepCount.segments`) — a
    /// count must never be "what we happen to hold" (see `steps`), and the cut
    /// must be the same one the console draws.
    ///
    /// A segment whose card this chat is not drawing — a proposal the pending
    /// list no longer carries — is folded INTO the next piece rather than
    /// left as a cut with nothing between, so the chain still reads as one run
    /// of N steps.
    func pieces(_ m: ThreadMessage, _ evs: [ThreadEvent], _ count: StepCount?, _ live: Bool) -> [TurnPiece] {
        let segs = count?.segments ?? []
        guard !segs.isEmpty else { return [TurnPiece(id: "\(m.id)", events: evs, count: count, live: live, card: nil)] }
        var drawn: [StepSegment] = []
        var carry: StepSegment?
        for s in segs {
            var acc = s
            if let c = carry {
                acc.tools += c.tools; acc.thoughts += c.thoughts; acc.steps += c.steps
                acc.first_id = c.first_id == 0 ? s.first_id : (s.first_id == 0 ? c.first_id : min(c.first_id, s.first_id))
                acc.last_id = max(c.last_id, s.last_id)
            }
            if let ref = s.ref, !ref.isEmpty, cardInChain(ref) == nil { carry = acc; continue }
            drawn.append(acc)
            carry = nil
        }
        if var c = carry { c.ref = nil; drawn.append(c) }
        return drawn.enumerated().map { i, s in
            TurnPiece(id: "\(m.id):\(i)",
                      events: s.steps == 0 ? [] : evs.filter { $0.id >= s.first_id && $0.id <= s.last_id },
                      count: count.map { StepCount(message_id: $0.message_id, run_id: $0.run_id, tools: s.tools, thoughts: s.thoughts,
                                                   steps: s.steps, first_id: s.first_id, last_id: s.last_id, segments: nil) },
                      live: live && i == drawn.count - 1,
                      card: s.ref.flatMap { $0.isEmpty ? nil : $0 })
        }
    }

    /// The object behind a segment's `ref`, if this chat is drawing it. A
    /// proposal the pending list no longer carries is not.
    func cardInChain(_ ref: String) -> ChatCard? {
        let parts = ref.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        if parts[0] == "ask" {
            guard let a = asks.first(where: { $0.id == parts[1] }) else { return nil }
            return .ask(a)
        }
        guard let x = actions.first(where: { $0.id == parts[1] }) else { return nil }
        return .action(x)
    }

    /// A card drawn inside the tool chain: the ordinary card, because
    /// answering one mid-chain is the point — the reply is delivered into the
    /// turn still running as a steering message and the agent carries on.
    enum ChatCard { case ask(Ask), action(Action) }

    @ViewBuilder func chainCard(_ ref: String) -> some View {
        switch cardInChain(ref) {
        case .ask(let a): AskCard(a: a, goals: goals, reload: { await load() }).id(a.id)
        case .action(let x): approvalCell(x)
        case nil: EmptyView()
        }
    }

    /// Refs the run blocks are drawing, so `placeAsks`/`placeActions` leave
    /// those cards alone instead of drawing them a second time under a bubble.
    var chainRefs: Set<String> {
        var out: Set<String> = []
        for c in steps.values { for s in c.segments ?? [] { if let r = s.ref, !r.isEmpty, cardInChain(r) != nil { out.insert(r) } } }
        return out
    }

    var body: some View {
        let _ = Perf.event("ThreadDetail.body rows=\(rows.count) shown=\(shown) events=\(events.count)")
        VStack(spacing: 0) {
            #if targetEnvironment(macCatalyst)
            macHead
            #endif
            ScrollViewReader { proxy in
                ScrollView {
                    // Plain VStack (not lazy): rows exist as soon as messages
                    // load, so the initial scrollTo has real targets. That is
                    // affordable because only `shown` of them are built.
                    VStack(alignment: .leading, spacing: 12) {
                        if rows.count > shown {
                            Button { withAnimation { shown += rowWindow * 2 } } label: {
                                Label("Show \(min(rowWindow * 2, rows.count - shown)) earlier of \(rows.count)", systemImage: "chevron.up")
                                    .font(.caption).foregroundStyle(.secondary)
                            }.buttonStyle(.plain).padding(.horizontal)
                        }
                        ForEach(visibleRows) { r in
                            bubble(r.m).id(r.m.id)
                            ForEach(asksByMessage[r.m.id] ?? []) { a in
                                AskCard(a: a, goals: goals, reload: { await load() }).id(a.id)
                            }
                            ForEach(actionsByMessage[r.m.id] ?? []) { approvalCell($0) }
                            ForEach(recsByMessage[r.m.id] ?? []) { recCell($0) }
                            // The run block, cut at the cards raised while it
                            // ran: fold · card · fold · card · the tail still
                            // running.
                            ForEach(r.chain) { p in
                                if !p.events.isEmpty || p.live || (p.count?.steps ?? 0) > 0 {
                                    RunActivity(events: p.events, live: p.live, count: p.count,
                                                open: { await loadBlock(r.count) }).id("run-\(p.id)")
                                }
                                if let ref = p.card { chainCard(ref) }
                            }
                        }
                        ForEach(tailAsks) { a in
                            AskCard(a: a, goals: goals, reload: { await load() }).id(a.id)
                        }
                        ForEach(tailActions) { approvalCell($0) }
                        ForEach(tailRecs) { recCell($0) }
                        if !orphanLive.isEmpty { RunActivity(events: orphanLive, live: true) }
                        if thread.status == "running" {
                            // Stop is right here while it works:
                            // kills the tool chain mid-turn; the conversation
                            // stays resumable with the next message.
                            // THE rule for "is it working", same as the web
                            // console's `showWorking` (views/threads.js) —
                            // keep them in step. A live run block already says
                            // it ("starting…" / "3 tool calls · now: …" with
                            // the dot), so this row is then just the Stop
                            // button; "working…" only when there is no block
                            // yet, never both at once. The whole row
                            // is inside `status == "running"`, so a stopped
                            // session shows neither.
                            let liveBlock = rows.contains { $0.live } || !orphanLive.isEmpty
                            #if targetEnvironment(macCatalyst)
                            // The desktop's Stop is the head's running pill,
                            // as on the console (the owner 2026-09-29): here only
                            // its "working…" line, a pulsing dot, no block yet.
                            if !liveBlock {
                                HStack(spacing: 7) {
                                    LiveDot(color: Web.accent, size: 6)
                                    Text("working…").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                                }.padding(.horizontal)
                            }
                            #else
                            HStack(spacing: 8) {
                                if !liveBlock { ProgressView(); Text("working…").font(.caption).foregroundStyle(.secondary) }
                                Spacer()
                                Button { stop() } label: { Label("Stop", systemImage: "stop.fill").font(.caption.weight(.semibold)) }
                                    .buttonStyle(.bordered).tint(.orange).controlSize(.small).disabled(stopping)
                            }.padding(.horizontal)
                            #endif
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    #if targetEnvironment(macCatalyst)
                    // `.msgs`: 24 over, 18 under, the right wall 8 wider.
                    .padding(.top, 24).padding(.bottom, 18).padding(.trailing, 8)
                    #else
                    .padding(.vertical, 10)
                    #endif
                    // Exactly the viewport's width, never wider (the owner
                    // 2026-09-23: "the messages went to left and right" — a
                    // row measured a hair too wide while a turn was live made
                    // the whole chat pan sideways). A vertical ScrollView takes
                    // its content's own width, so one over-wide child turns the
                    // page into a two-axis pan; pinned here, an over-wide child
                    // is its own problem and the chat stays a column.
                    // On the Mac the container is the whole window (it ran
                    // under the sidebar), so there the column is the detail
                    // pane's width — wall to wall, like the console's `.msgs`
                    // : the owner's blocks against the right wall,
                    // the session's side capped per row (cards 860, runs 760).
                    #if targetEnvironment(macCatalyst)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    #else
                    .containerRelativeFrame(.horizontal)
                    #endif
                }
                // Initial position: open on the latest
                // message — unless the thread is waiting on the owner for an ask
                // that sits further up, in which case open on that ask.
                // defaultScrollAnchor(.bottom) was tried and left short
                // threads pushed out of frame once the keyboard came up, so
                // this scrolls explicitly, retrying after layout settles.
                // Keyboard gets out of the way when the owner wants to read: any
                // drag on the history dismisses it; tap-outside is handled
                // app-wide by KeyboardDismiss.swift (a SwiftUI TapGesture here
                // did not fire reliably on the scroll view).
                .scrollDismissesKeyboard(.immediately)
                // After the opening placement the chat NEVER scrolls for the
                // owner: no jump to the bottom on a new
                // message, a new tool step or the keyboard — the web console
                // keeps its reading anchor the same way (threads.js
                // chatAnchor). Only the deep-linked focus card below still
                // scrolls, once.
                .onChange(of: messages.count) { old, new in
                    guard new > old, old == 0 else { return }
                    let (target, anchor) = openingTarget()
                    widen(toRow: target as? Int)
                    Task { @MainActor in
                        for delay in [0, 120, 400] {
                            try? await Task.sleep(for: .milliseconds(delay))
                            proxy.scrollTo(target, anchor: anchor)
                        }
                    }
                }
                // The focus card (board `first`) is placed after the messages
                // draw, so the scroll to it waits for that.
                .onChange(of: focusLanded) { _, landed in
                    guard landed, let f = focusCard else { return }
                    Task { @MainActor in
                        for delay in [0, 120, 400] {
                            try? await Task.sleep(for: .milliseconds(delay))
                            proxy.scrollTo(f, anchor: .center)
                        }
                    }
                }
            }
            #if targetEnvironment(macCatalyst)
            // The console's composer, wall to wall under the transcript:
            // errors sit over the box, the armed cards are its reply-strip
            // inside it. The Mac has no route line: a message there is
            // always this session, now.
            if error != nil || sendError != nil {
                VStack(spacing: 6) {
                    if let error { ErrorBanner(message: error) }
                    if let sendError { sendFailedBanner(sendError) }
                }
                .padding(.horizontal, 16).padding(.vertical, 8)
                .background(Web.panel)
            }
            Composer(draft: draft, attachments: attachments,
                     placeholder: replyPlaceholder,
                     sending: sending, send: { send() }, allowEmpty: replies.contains { !$0.outcome.isEmpty },
                     banner: replies.isEmpty ? nil : AnyView(macReplyStrip))
            #else
            Divider()
            VStack(spacing: 6) {
                if let error { ErrorBanner(message: error) }
                if let sendError { sendFailedBanner(sendError) }
                // A reply is now + this session, so the route line steps aside.
                if replies.isEmpty { sendOptions } else { replyBanner }
                Composer(draft: draft, attachments: attachments,
                         placeholder: replyPlaceholder,
                         sending: sending, send: { send() }, allowEmpty: replies.contains { !$0.outcome.isEmpty })
            }.padding(10)
            #endif
        }
        #if targetEnvironment(macCatalyst)
        .background(Web.panel)
        // A Finder drop anywhere on the chat — the messages, not only the
        // composer — attaches the file.
        .dropsFiles(into: attachments)
        #endif
        .environment(\.askReply, AskReplyHook { a, outcome in
            arm(ArmedReply(kind: .ask, id: a.id, title: a.title, outcome: outcome, outcomes: a.outcomes ?? []))
        })
        // Every card in this chat draws its armed pick as the primary button
        // (Card.swift `outcomeButtons`).
        .environment(\.armedPicks, Dictionary(replies.map { ($0.ref, $0.outcome) }, uniquingKeysWith: { _, b in b }))
        .confirmationDialog("Are you sure?", isPresented: $confirmingEmpty, titleVisibility: .visible) {
            Button("Send without") { send(confirmed: true) }
            Button("Cancel", role: .cancel) {}
        }
        .confirmationDialog("Archive this session?", isPresented: $confirmArchive, titleVisibility: .visible) {
            Button("Archive", role: .destructive) {
                Task {
                    do { thread = try await hub.archiveThread(thread.id); dismiss() }
                    catch { self.error = error.localizedDescription }
                }
            }
            Button("Cancel", role: .cancel) {}
        }
        .navigationTitle(mdPlain(thread.title))
        .navigationBarTitleDisplayMode(.inline)
        .askButton()
        // Holding down anything inside a session talks to THAT session — it
        // already has the context. The sheet still offers a
        // new session when the question is really a fresh one.
        .environment(\.chatTarget, .session(id: thread.id, title: thread.title))
        #if targetEnvironment(macCatalyst)
        // The head above is the whole bar, as on the console: no window bar
        // over it and no ⋯ menu (the same layout as the console's head; Stop
        // is the running pill).
        .toolbar(.hidden, for: .navigationBar)
        #else
        .toolbar {
            Menu {
                Button { Task { thread = (try? await hub.checkinThread(thread.id)) ?? thread; await load() } } label: { Label("Check in now", systemImage: "arrow.clockwise") }.disabled(thread.status == "running")
                Button { showSettings = true } label: { Label("Schedule & settings", systemImage: "slider.horizontal.3") }
                if thread.status == "running" {
                    Button(role: .destructive) { stop() } label: { Label("Stop session", systemImage: "stop.circle") }
                }
                if thread.status == "needs_you" {
                    Button { Task { thread = (try? await hub.patchThread(thread.id, ["status": "done"])) ?? thread; try? await hub.readThread(thread.id); dismiss() } } label: { Label("Clear from board", systemImage: "checkmark.circle") }
                }
                // The console's Archive, same confirm.
                Button(role: .destructive) { confirmArchive = true } label: { Label("Archive", systemImage: "archivebox") }
                    .disabled(thread.status == "archived")
            } label: { Image(systemName: "ellipsis.circle") }.accessibilityLabel("Session actions")
        }
        #endif
        .sheet(isPresented: $showSettings) { ThreadSettings(thread: $thread) }
        // The app's Settings, reached from the refusal banner: the owner fixes
        // the code and comes straight back to the armed card and their words.
        .sheet(isPresented: $showDeciderSettings) {
            NavigationStack {
                SettingsView().toolbar { Button("Done") { showDeciderSettings = false } }
            }
        }
        .navigationDestination(item: $openThread) { ThreadDetail(thread: $0) }
        .onAppear {
            // On the Mac the sessions sidebar stays on screen beside the chat,
            // so the board keeps polling there: paused, a card read here kept
            // its cell on the left (the owner 2026-09-29).
            #if !targetEnvironment(macCatalyst)
            store.pause()
            #endif
            if let r = armOnOpen, !armedOnOpen { armedOnOpen = true; arm(r) }
        }
        // Back from the background: the change loop may be parked on a
        // request the OS froze, so read the session now (as RootView does).
        .onChange(of: scenePhase) { _, p in
            // The Mac's `.inactive` is a window behind Chrome, still read:
            // the chat keeps its loop (RootView does the same for the board).
            #if targetEnvironment(macCatalyst)
            foreground = p != .background
            #else
            foreground = p == .active
            #endif
            if p == .active { Task { await load() } }
        }
        // Back: the board loop resumes on its own within 2 s (RootView) —
        // kicking a refresh from here competed with the pop animation. The
        // events the phone assembled (disk seed + every delta) are written
        // back so the next open of this chat seeds from them, not from a
        // 460 KB fetch (speed phase, 2026-08-26).
        // The board landed after this chat opened (a relaunch draws both at
        // once): its cards, until the chat's own fetch answers.
        .onChange(of: store.board?.bundleOf(thread)) { seedCards() }
        .onDisappear {
            #if !targetEnvironment(macCatalyst)
            store.resume()
            #endif
            let key = hub.cacheKey(HubClient.eventsPath(thread.id)), evs = Array(events.suffix(1000))
            if !evs.isEmpty { Task.detached { HubClient.remember(key: key, evs) } }
        }
        .task {
            #if targetEnvironment(simulator)
            // LIFE_SEND_ERROR=decider stages the refusal banner for a
            // screenshot (ops/screens.sh): a real one needs a real proposal
            // refused by the hub.
            if ProcessInfo.processInfo.environment["LIFE_SEND_ERROR"] == "decider" {
                sendError = HubClient.deciderHint(APIError(error: "staged", status: 403))
                sendErrorNeedsDecider = true
            }
            #endif
            // Paint from the phone's copy first: the chat as of the last open
            // draws before the first byte arrives, then `load` fetches what
            // changed (messages/asks by ETag, events by `since`).
            if messages.isEmpty {
                let mk = hub.cacheKey(HubClient.messagesPath(thread.id)), ek = hub.cacheKey(HubClient.eventsPath(thread.id))
                let seeded: ([ThreadMessage]?, [ThreadEvent]?) = await Task.detached {
                    (HubClient.cachedValue(key: mk), HubClient.cachedValue(key: ek))
                }.value
                if messages.isEmpty, let m = seeded.0, !m.isEmpty {
                    messages = m
                    if let e = seeded.1 { events = e }
                    rows = computeRows()
                }
            }
            seedCards()
            await load(); try? await hub.readThread(thread.id)
            // Then park on the change feed for THIS chat and reload only when
            // it moves — a working session's steps land within a second, an
            // idle one costs a ~200-byte request every 25 s. Older hub (404):
            // the timed poll it replaced.
            var version = ""
            while !Task.isCancelled {
                guard foreground else { try? await Task.sleep(for: .seconds(2)); continue }
                if let f = try? await hub.changes(since: version, thread: thread.id) {
                    let first = version.isEmpty
                    version = f.version
                    if first { continue }   // the opening load just ran
                } else {
                    try? await Task.sleep(for: .seconds(thread.status == "running" ? 3 : 15))
                }
                await load()
            }
        }
    }

    /// One quiet line above the composer: where this message goes and when.
    /// It stays out of the way at its default (this session, now) and says so
    /// loudly the moment it is not, because a message that will not arrive for
    /// twelve hours must not look like one that already has.
    @ViewBuilder var sendOptions: some View {
        let plain = sendTarget == .thisSession && sendWhen == .now
        HStack(spacing: 6) {
            Menu {
                Section("Send to") {
                    Picker("Send to", selection: $sendTarget) {
                        ForEach([RespondTarget.thisSession, .newSession], id: \.self) { Label($0.label, systemImage: $0.icon).tag($0) }
                    }.pickerStyle(.inline).labelsHidden()
                }
                Section("When") {
                    Button { sendWhen = .now } label: { Label("Now", systemImage: "paperplane") }
                    Button { sendWhen = .inAnHour } label: { Label("In an hour", systemImage: "clock") }
                    Button { sendWhen = .tomorrowMorning } label: { Label("Tomorrow 9am", systemImage: "sun.horizon") }
                    Button { pickingWhen = true } label: { Label("Pick a time…", systemImage: "calendar.badge.clock") }
                }
            } label: {
                HStack(spacing: 5) {
                    Image(systemName: plain ? "arrow.turn.down.right" : sendTarget.icon).font(.caption2)
                    Text(plain ? "This session · now" : "\(sendTarget.label) · \(sendWhen.label.lowercased())")
                        .font(.caption.weight(plain ? .regular : .semibold))
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 8))
                }
                .foregroundStyle(plain ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.accentColor))
            }
            Spacer(minLength: 0)
            if !plain {
                Button("Reset") { sendTarget = .thisSession; sendWhen = .now }.font(.caption)
            }
        }
        .padding(.horizontal, 4)
        .sheet(isPresented: $pickingWhen) {
            NavigationStack {
                DatePicker("Deliver at", selection: $pickWhenDate, in: Date()...).datePickerStyle(.graphical).padding()
                    .navigationTitle("Pick a time").navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) { Button("Cancel") { pickingWhen = false } }
                        ToolbarItem(placement: .confirmationAction) { Button("Use") { sendWhen = .at(pickWhenDate); pickingWhen = false } }
                    }
            }.presentationDetents([.medium, .large])
        }
    }

    /// Where the thread opens: the pending "Your turn" ask if the owner still owes
    /// an answer and it is not already the last message; otherwise the bottom.
    func openingTarget() -> (AnyHashable, UnitPoint) {
        // Opened from an ask card: land on the reply that raised it.
        if let f = focusMessage, messages.contains(where: { $0.id == f }) {
            return (f, .center)
        }
        // Opened on a card (board `first`, or a tapped approval): land on it
        // if it is already placed; otherwise `focusLanded` scrolls there
        // once it is. No guessing from message kinds — the console doesn't.
        if let f = focusCard, asks.contains(where: { $0.id == f }) || actions.contains(where: { $0.id == f }) {
            return (f, .center)
        }
        return ("bottom", .bottom)
    }

    func stop() {
        stopping = true
        Task {
            do { thread = try await hub.stopThread(thread.id); await load() } catch { self.error = error.localizedDescription }
            stopping = false
        }
    }

    /// The banner over the chat bar: one row per armed card — which card, the
    /// pick it carries, and × to drop just that one.
    private var replyBanner: some View {
        VStack(spacing: 0) {
            ForEach(replies) { r in
                HStack(spacing: 8) {
                    Image(systemName: r.icon).font(.caption).foregroundStyle(r.tint)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(r.label).font(.caption2.weight(.semibold)).foregroundStyle(r.tint)
                        Text(mdPlain(r.title)).font(.caption).foregroundStyle(.primary).lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    Button { replies.removeAll { $0.ref == r.ref } } label: {
                        Image(systemName: "xmark.circle.fill").font(.body).foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Drop \(mdPlain(r.title))")
                }
                .padding(.horizontal, 10).padding(.vertical, 6)
                if r.ref != replies.last?.ref { Divider().padding(.leading, 10) }
            }
        }
        .background(Color.accentColor.opacity(0.1), in: RoundedRectangle(cornerRadius: 10))
        .overlay(alignment: .leading) { RoundedRectangle(cornerRadius: 1.5).fill(Color.accentColor).frame(width: 3).padding(.vertical, 6) }
    }

    /// The hub refused the last send: why, in red, with × to drop the line
    /// and — when it was the decider code — a button to the Settings row
    /// that takes it. Survives reloads (see `sendError`).
    private func sendFailedBanner(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill").font(.footnote).foregroundStyle(.red)
                Text(message).font(.footnote).foregroundStyle(.red).fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 4)
                Button { sendError = nil; sendErrorNeedsDecider = false } label: {
                    Image(systemName: "xmark.circle.fill").font(.body).foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Dismiss")
            }
            if sendErrorNeedsDecider {
                Button { showDeciderSettings = true } label: {
                    Label("Decider code in Settings", systemImage: "faceid")
                }
                .buttonStyle(.borderedProminent).tint(.red).controlSize(.small)
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
        .background(Color.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
    }

    /// What the box asks for, by what is armed above it (the console's words).
    private var replyPlaceholder: String {
        guard let r = replies.first else { return composerPlaceholder }
        if replies.count > 1 { return "One message for all of them (optional)" }
        if r.kind == .rec { return r.outcome.isEmpty ? "Your line — it goes to the session, the rec stays open" : "Your note to the session (optional)" }
        if r.kind == .action { return r.outcome.isEmpty ? "Your line — it goes to the session, the proposal stays open" : "Your note to the session (optional)" }
        #if targetEnvironment(macCatalyst)
        // An ask, the console's words (composer.js, the owner 2026-09-29).
        return r.outcome.isEmpty ? "Your answer…" : "Anything to add (optional)"
        #else
        return "Your reply (optional)"
        #endif
    }

    func send(confirmed: Bool = false) {
        let t = draft.text
        let armed = replies
        if !armed.isEmpty, !confirmed, !t.contains(where: { !$0.isWhitespace }), attachments.isEmpty {
            confirmingEmpty = true
            return
        }
        // Clear the composer immediately (optimistic). Clearing only after the
        // round-trip left text in the bar when the hub was slow or the reply
        // was lost even though the message was stored; restore on failure.
        let atts = attachments.take(), ids = atts.assetIDs
        draft.text = ""; replies = []
        sendError = nil; sendErrorNeedsDecider = false
        sending = true
        Task {
            do {
                // An approve/deny riding on the message is a decision: Face
                // ID unlocks the decider code first (nothing is sent when
                // cancelled), and the hub refuses the whole prompt without it.
                var decider: String? = nil
                if let d = armed.first(where: { $0.kind == .action && !$0.outcome.isEmpty }) {
                    decider = try await hub.unlockDecider(d.outcome == "approved" ? "Approve this action" : "Deny this action")
                }
                let refs = try await atts.upload(to: hub, threadID: thread.id)
                if armed.count > 1 {
                    // Several cards, ONE prompt naming each with its pick: the
                    // hub closes each by its own rule and frames every one for
                    // the session. Now, this session.
                    try await hub.postPrompt(target: RespondTarget.thisSession.value(thread: thread.id),
                                             text: t, attachments: refs,
                                             replies: armed.map { PromptReply(ref: $0.ref, outcome: $0.outcome) },
                                             decider: decider)
                } else if let re = armed.first, re.kind == .rec {
                    // A rec alone goes the Recs page's way: the hub records
                    // the verdict and relays it into this session.
                    if re.outcome.isEmpty {
                        _ = try await hub.replyRec(re.id, note: t, deliver: "source", attachments: refs)
                    } else {
                        _ = try await hub.decideRec(re.id, status: re.outcome, note: t, deliver: "source", attachments: refs)
                    }
                } else if let re = armed.first {
                    // One prompt referencing the card, now, to this session —
                    // the hub closes the read and wakes it only for words.
                    try await hub.postPrompt(target: RespondTarget.thisSession.value(thread: thread.id),
                                             text: t, re: re.ref, outcome: re.outcome, attachments: refs, decider: decider)
                } else if sendTarget == .thisSession, sendWhen == .now {
                    // The plain case stays the plain call: a message to a
                    // running session is steered into the turn in flight (202),
                    // which queueing it as a prompt would not do.
                    thread = try await hub.sendThread(thread.id, text: t, attachments: refs)
                } else {
                    let f = sendWhen.fields
                    try await hub.postPrompt(target: sendTarget.value(thread: thread.id), text: t,
                                             inAfter: f.inAfter, on: f.on, at: f.at, attachments: refs)
                    sendTarget = .thisSession; sendWhen = .now
                }
                error = nil
                await load()
                await PhotoCleanup.offerDeletingScreenshots(ids)
            } catch {
                sendError = HubClient.deciderHint(error)
                sendErrorNeedsDecider = HubClient.isDeciderRefusal(error)
                if replies.isEmpty { replies = armed }
                if draft.text.isEmpty { draft.text = t }
                attachments.restore(atts)
            }
            sending = false
        }
    }

    @ViewBuilder func bubble(_ m: ThreadMessage) -> some View {
        if m.kind == "schedule" { scheduleCard(m) } else { chatBubble(m) }
    }

    /// One message, described for a session that is about to be asked about it.
    /// The text is included whole up to a point — the session can read the rest
    /// of the thread itself, and a huge bubble should not become a huge prompt.
    func bubbleSubject(_ m: ThreadMessage) -> ChatSubject {
        let who = m.role == "owner" ? "you" : "the session"
        let body = m.text.count > 1500 ? String(m.text.prefix(1500)) + " …[truncated]" : m.text
        var facts = ["- session: \(thread.title) (`\(thread.id)`), message id \(m.id)",
                     "- from \(who) at \(m.ts.formatted(.dateTime.month(.abbreviated).day().hour().minute()))\(m.kind.isEmpty ? "" : ", kind \(m.kind)")"]
        if m.cost_usd > 0 { facts.append("- that turn cost \(usd(m.cost_usd))") }
        if let a = m.attachments, !a.isEmpty { facts.append("- \(a.count) attachment(s)") }
        facts.append("- the message:\n\(body)")
        return ChatSubject(screen: "Session · \(thread.title)",
                           title: "\(m.role == "owner" ? "Your" : "Its") message, \(shortAgo(m.ts))",
                           facts: facts.joined(separator: "\n"),
                           code: nil)
    }

    /// Green card: the agent set (or cleared) standing instructions here —
    /// when it checks back and what it is for.
    func scheduleCard(_ m: ThreadMessage) -> some View {
        let lines = m.text.components(separatedBy: "\n\n")
        return VStack(alignment: .leading, spacing: 6) {
            Label(lines.first ?? "", systemImage: "clock.badge.checkmark").font(.subheadline.weight(.semibold)).foregroundStyle(.green)
            if lines.count > 1 {
                StyledText(text: lines.dropFirst().joined(separator: "\n\n"), color: .primary)
            }
            Text(m.ts, style: .time).font(.caption2).foregroundStyle(.secondary)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.green.opacity(0.12), in: RoundedRectangle(cornerRadius: 14))
        .overlay { RoundedRectangle(cornerRadius: 14).strokeBorder(Color.green.opacity(0.35), lineWidth: 1) }
        .padding(.horizontal)
    }

    @ViewBuilder func chatBubble(_ m: ThreadMessage) -> some View {
        let mine = m.role == "owner"
        // The CLI's "You've hit your session limit · resets 2:40am" is not a
        // crash: the hub resumes the session then, so it draws as the turn's
        // end line in amber, never a red bubble (the console's paused row).
        let paused = m.kind == "error" && m.text.range(of: #"(?i)hit your (?:[a-z0-9 ]{0,24} )?limit[^\n]*?resets\s"#, options: .regularExpression) != nil
        if !mine && (m.kind != "error" || paused) {
            // THERE IS NO WHITE CELL: anything the owner must read or do is
            // one of the special cells. A session's reply is the turn ENDING, never a
            // text bubble: the cards the turn raised are the reply (the hub
            // mints a read card from any reply text, so a new row
            // is always empty), and the row is one muted line — that it
            // ended, when, what it cost — in the card's colour. A row that
            // still carries text (an older row, or words folded beside a card)
            // keeps it behind the line, closed until tapped, for the record
            // only. Same as the console's `.msg.end` (views/threads.js msgHTML).
            let hasOld = !paused && (!m.text.isEmpty || !(m.attachments ?? []).isEmpty)
            let open = openReplies.contains(m.id)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    Text(paused ? "turn paused · session limit" : "turn ended")
                    Text("·")
                    Text(m.ts, style: .time)
                    if m.cost_usd > 0 { Text("· " + usd(m.cost_usd)) }
                    if let n = m.tokens, n > 0 { Text("· \(tokenCount(n)) tok") }
                    if hasOld { Text(open ? "· hide" : "· text").opacity(0.7) }
                    Spacer()
                }
                .font(.caption2)
                .foregroundStyle(paused ? Color.orange : m.kind == "read" ? Color.accentColor : Color.secondary)
                .contentShape(Rectangle())
                .onTapGesture {
                    guard hasOld else { return }
                    if open { openReplies.remove(m.id) } else { openReplies.insert(m.id) }
                }
                if hasOld && open {
                    chatBubbleBox(m, mine: false).padding(.horizontal, -16).opacity(0.85)
                }
            }
            .padding(.horizontal)
        } else {
            #if targetEnvironment(macCatalyst)
            if mine { macMine(m) } else { chatBubbleBox(m, mine: false) }
            #else
            chatBubbleBox(m, mine: mine)
            #endif
        }
    }

    @ViewBuilder func chatBubbleBox(_ m: ThreadMessage, mine: Bool) -> some View {
        HStack {
            if mine { Spacer(minLength: 40) }
            VStack(alignment: .leading, spacing: 4) {
                if m.kind == "checkin" { Label("scheduled check-in", systemImage: "clock").font(.caption2).foregroundStyle(.secondary) }
                if m.kind == "decision" { Label("your decision", systemImage: "checkmark.seal").font(.caption2).foregroundStyle(mine ? .white.opacity(0.8) : .secondary) }
                if m.kind == "needs_you" { Label("Your turn", systemImage: "hand.raised.fill").font(.caption2.weight(.semibold)).foregroundStyle(.red) }
                // Only read-asks under it: an answer, blue, not a stopped session.
                if m.kind == "read" { Label("For you to read", systemImage: "text.alignleft").font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor) }
                // What the owner's message answered, in the card's own word:
                // a decision made on the Recs page (or on the cell above)
                // shows here as "↩ Accepted · <rec>" over their note.
                // One line per card it answered.
                if mine {
                    ForEach(replyLines(m), id: \.self) { line in
                        Label(line, systemImage: "arrowshape.turn.up.left").font(.caption2.weight(.semibold))
                            .foregroundStyle(Color.white.opacity(0.85)).lineLimit(2)
                    }
                }
                // By position: the same screenshot can be attached twice.
                let refs = m.attachments ?? []
                ForEach(refs.indices, id: \.self) { i in BlobAttachment(ref: refs[i], maxHeight: 240) }
                if !m.text.isEmpty {
                    let color: Color = m.kind == "error" ? .red : mine ? .white : .primary
                    let long = isLongText(m.text)
                    let open = openLong.contains(m.id)
                    if long && !open {
                        StyledText(text: m.text, color: color)
                            .frame(maxHeight: 320, alignment: .top)
                            .clipped()
                            .overlay(alignment: .bottom) {
                                LinearGradient(colors: [.clear, mine ? Color.accentColor : Color(.secondarySystemBackground)],
                                               startPoint: .top, endPoint: .bottom)
                                    .frame(height: 64).allowsHitTesting(false)
                            }
                    } else {
                        StyledText(text: m.text, color: color)
                    }
                    if long {
                        Button(open ? "Show less" : "Show all · \(longLineCount(m.text)) lines") {
                            if open { openLong.remove(m.id) } else { openLong.insert(m.id) }
                        }
                        .font(.caption.weight(.semibold))
                        .buttonStyle(.bordered)
                        .tint(mine ? .white : .accentColor)
                        .controlSize(.small)
                    }
                }
                HStack {
                    Text(m.ts, style: .time)
                    if m.cost_usd > 0 { Text("· " + usd(m.cost_usd)) }
                    if let n = m.tokens, n > 0 { Text("· \(tokenCount(n)) tok") }
                    if m.queued == true { Label("queued · delivered when it's ready", systemImage: "clock").lineLimit(1) }
                    if m.steered == true { Label("steered in mid-turn", systemImage: "arrow.turn.right.down").lineLimit(1) }
                    if let by = sentBy(m) {
                        Spacer(minLength: 12)
                        if let id = by.thread {
                            Button { Task { if let t = try? await hub.thread(id) { openThread = t } } } label: {
                                Text("sent by \(Text(by.who).underline())").lineLimit(1)
                            }.buttonStyle(.plain)
                        } else {
                            Text("sent by \(by.who)").lineLimit(1)
                        }
                    }
                }.font(.caption2).foregroundStyle(mine ? Color.white.opacity(0.7) : Color.secondary)
            }
            .padding(10)
            .background(mine ? Color.accentColor : m.kind == "needs_you" ? Color.red.opacity(0.12) : m.kind == "read" ? Color.accentColor.opacity(0.10) : Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 14))
            .overlay { if m.id == focusMessage { RoundedRectangle(cornerRadius: 14).strokeBorder(Color.red.opacity(0.6), lineWidth: 1.5) } }
            // Hold a message down to ask about it. Over the words themselves
            // iOS's own selection wins, so StyledText puts "Chat about this"
            // in the selection menu too — with the selected words quoted.
            .chatAbout(bubbleSubject(m))
            if !mine { Spacer(minLength: 40) }
        }.padding(.horizontal)
    }

    /// Who wrote a row, for its lower-right corner; `thread` is set
    /// when another session wrote it, and the words open that session. Same
    /// words as the console's `sentByHTML` (views/threads.js).
    func sentBy(_ m: ThreadMessage) -> (who: String, thread: String?)? {
        guard let a = m.author, !a.isEmpty else { return nil }
        switch a {
        case "owner": return ("you", nil)
        case "hub": return ("the hub", nil)
        default:
            let prefix = "claude:thread:"
            guard a.hasPrefix(prefix) else { return (a, nil) }
            let id = String(a.dropFirst(prefix.count))
            if id == m.thread_id { return ("this session", nil) }
            return (m.author_title.flatMap { $0.isEmpty ? nil : $0 } ?? id, id)
        }
    }

    /// "Accepted · <rec title>" / "Done · <ask title>", one per card the
    /// message answered (`replies` when there were several, else the
    /// in_reply_to/outcome pair); empty for a plain message. The verdict word
    /// is the hub's (`replies[].label`, store.ReplyLabel), the console's too.
    func replyLines(_ m: ThreadMessage) -> [String] {
        let list = (m.replies?.isEmpty == false) ? m.replies! : [PromptReply(ref: m.in_reply_to ?? "", outcome: m.outcome)]
        return list.compactMap { replyLine(ref: $0.ref, label: $0.label ?? $0.outcome ?? "") }
    }

    func replyLine(ref: String, label verdict: String) -> String? {
        guard !ref.isEmpty else { return nil }
        let parts = ref.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        let kind = parts[0], id = parts[1]
        let title: String
        switch kind {
        case "rec": title = recs.first { $0.id == id }.map { "💡 " + $0.title } ?? id
        case "ask": title = asks.first { $0.id == id }?.title ?? id
        case "action": title = actions.first { $0.id == id }.map { "🔴 " + $0.title } ?? id
        default: title = "\(kind) \(id)"
        }
        return "\(verdict) · \(title)"
    }

    /// Read one folded block's own steps when the owner opens it — one ranged
    /// read (`since`/`before` from the hub's count), however long the run is.
    /// Anything already held is skipped, so the live block, which streams in
    /// through `since`, never re-reads.
    func loadBlock(_ c: StepCount?) async {
        guard let c, c.steps > 0, !loadingBlocks.contains(c.message_id) else { return }
        let have = events.filter { $0.id >= c.first_id && $0.id <= c.last_id }.count
        guard have < c.steps else { return }
        loadingBlocks.insert(c.message_id)
        defer { loadingBlocks.remove(c.message_id) }
        var since = c.first_id - 1
        while since < c.last_id {
            guard let page = try? await hub.threadEvents(thread.id, since: since, before: c.last_id + 1), !page.isEmpty else { break }
            let known = Set(events.map(\.id))
            events.append(contentsOf: page.filter { !known.contains($0.id) })
            since = page[page.count - 1].id
        }
        events.sort { $0.id < $1.id }
        rows = computeRows()
    }

    /// The proposals this chat draws: all of its own — open ones with their
    /// buttons, dismissed ones folded, decided ones grey with who decided. A
    /// decided card used to leave the chat, so an approved proposal was only
    /// the owner's "↩ Approved" reply; same as the web chat.
    func inChat(_ a: Action) -> Bool { a.thread_id == thread.id }

    /// Draw the session's open cards from the board until this chat's own
    /// fetch lands. The opening paint is the saved messages and steps, which
    /// carry no cards, and `load` can be seconds away: the desktop app came
    /// back from an update on a running session whose row beside it read
    /// APPROVE while the chat showed "12 tool calls · now: thinking" and no
    /// card, for the ten seconds the hub took to answer (the owner 2026-09-30:
    /// "desktop app doesnt show approval cell in running chat"). The board
    /// already holds them — it is what drew the row. Once `load` has the
    /// chat's own lists those are the truth and the board is not read again:
    /// it can lag a card just answered here.
    func seedCards() {
        guard !cardsLoaded, let b = store.board?.bundleOf(thread) else { return }
        let a = b.asks.filter { x in x.thread_id == thread.id && !asks.contains { $0.id == x.id } }
        let p = b.actions.filter(inChat).filter { x in !actions.contains { $0.id == x.id } }
        if !a.isEmpty { placeAsks(asks + a) }
        if !p.isEmpty { placeActions(actions + p) }
    }

    func load() async {
        Perf.event("load start")
        defer { Perf.event("load end") }
        do {
            let since = events.last?.id ?? 0
            async let t = hub.thread(thread.id); async let m = hub.threadMessages(thread.id); async let e = hub.threadEvents(thread.id, since: since)
            async let st = hub.threadSteps(thread.id)
            async let k = hub.threadAsks(thread.id)
            // Every state: a dismissed proposal keeps its folded line with
            // Reopen inside (2026-09-18), a decided one its grey card (09-30).
            async let p = hub.actions(state: "", thread: thread.id)
            async let rc = hub.threadRecs(thread.id)
            // The conversation is drawn as soon as the MESSAGES land. The
            // event stream is by far the biggest response (hundreds of KB of
            // tool output on a long session) and awaiting it first left the
            // chat blank for seconds until it arrived. Steps fill in underneath a moment later.
            let (freshThread, freshMessages) = try await (t, m)
            // Opened on a card: place the approvals BEFORE the first draw, so
            // the opening scroll (onChange of messages.count) has its target.
            // Every other time the approvals fill in after the messages.
            let early: [Action]? = focusCard != nil && messages.isEmpty ? (try? await p)?.filter(inChat) : nil
            var moved = false
            let msgsMoved = freshMessages != messages
            // Only assign what actually changed: a plain reassignment every
            // poll (3s while running) rebuilt the whole message list even
            // when the hub returned exactly what we already had.
            if freshThread != thread { thread = freshThread; moved = true }
            if msgsMoved { messages = freshMessages; moved = true }
            if moved { rows = computeRows(); moved = false }
            if let early { placeActions(early) }
            // Goals name the goal on an ask card and barely ever change —
            // re-fetching them on every 3s poll was a round trip for nothing.
            if goals.isEmpty, let g = try? await hub.goals() { goals = g }
            var cardsMoved = false
            // The other device's install card is not this app's to draw
            // (the owner 2026-10-01); the board already left it out.
            let gotAsks = (try? await k)?.filter { !$0.isOtherDeviceInstall }
            if let gotAsks, gotAsks != asks || (msgsMoved && !gotAsks.isEmpty) {
                cardsMoved = gotAsks != asks
                placeAsks(gotAsks)
            }
            // A fetch that failed keeps the cards already drawn (asks always
            // did): an approval must not leave the chat because one request
            // timed out — `?? []` read a failure as "no proposals".
            let mine: [Action]?
            if let early { mine = early } else { mine = (try? await p)?.filter(inChat) }
            if let mine, mine != actions || (msgsMoved && !mine.isEmpty) {
                cardsMoved = cardsMoved || mine != actions
                placeActions(mine)
            }
            if gotAsks != nil, mine != nil { cardsLoaded = true }
            // A run block draws only the cards this chat holds (`pieces`), and
            // `place…` leaves those to it: a card that arrives after the cut
            // did — no new step, no new message — needs the blocks rebuilt, or
            // it is drawn nowhere until the next step.
            if cardsMoved, !steps.isEmpty { rows = computeRows() }
            // A rec moves under its reply when that lands, and changes
            // without a message when the owner decides it from the Recs page.
            if let rc = try? await rc, rc != recs || (msgsMoved && !rc.isEmpty) { placeRecs(rc) }
            if let f = focusCard, !focusLanded, asks.contains(where: { $0.id == f }) || actions.contains(where: { $0.id == f }) {
                focusLanded = true
            }
            let fresh = try await e
            if !fresh.isEmpty { events.append(contentsOf: fresh.filter { $0.id > since }); moved = true }
            // The headline of every block: the hub's own count, so a number
            // never depends on how much of the stream this screen holds.
            var cutMoved = false
            if let st = try? await st {
                // uniquingKeysWith: a repeated message_id from the hub must
                // never trap the app; the later row wins.
                let byMsg = Dictionary(st.map { ($0.message_id, $0) }, uniquingKeysWith: { _, b in b })
                if byMsg != steps { steps = byMsg; moved = true; cutMoved = true }
            }
            if moved { rows = computeRows() }
            // The hub moved the cut — a card was raised mid-run, or answered
            // away. Re-file them: one drawn inside a run block must not also
            // sit under a bubble.
            if cutMoved { placeAsks(asks); placeActions(actions) }
            error = nil
            // Shell commands without a description get their English summary
            // from Haiku a few seconds after the event: re-fetch the recent
            // ones still blank.
            //
            // Only while they are still plausibly coming. The summary is
            // written once, by describeAsync, right after the event — and that
            // function gives up silently when the Describe call errors, so a
            // row that missed its chance stays blank forever. Without a bound
            // this filter kept handing the hub the same dead ids on every poll
            // (3s while running, 15s idle) for as long as the chat stayed
            // open, and the reply was always the same nothing. It is not
            // hypothetical: 1089 tool_use rows in this hub are permanently
            // blank across 27 chats, and several have 19 of them inside this
            // very last-40 window — 19 ids re-asked every three seconds.
            let stillComing = Date().addingTimeInterval(-120)
            let blank = events.suffix(40)
                .filter { $0.kind == "tool_use" && ($0.summary ?? "").isEmpty && $0.ts > stillComing }
                .map(\.id)
            if !blank.isEmpty, let filled = try? await hub.threadEvents(thread.id, ids: blank) {
                var filledAny = false
                for f in filled where !(f.summary ?? "").isEmpty {
                    if let i = events.firstIndex(where: { $0.id == f.id }) { events[i] = f; filledAny = true }
                }
                if filledAny { rows = computeRows() }
            }
        } catch { if !error.isCancellation { self.error = error.localizedDescription } }
    }
}

#if targetEnvironment(macCatalyst)
extension ThreadDetail {
    /// The console's `.chat-head` (skin.css, one 52pt row), the owner 2026-09-29:
    /// "I want the same exact layout within the desktop app". ← in a hairline
    /// box, the title, its pills (the live ones are the controls — hovered,
    /// running reads Stop, speaking reads Stop speaking), the grey model pill,
    /// and at the right the schedule · next · $ · tokens line.
    var macHead: some View {
        HStack(spacing: 8) {
            Button {
                if pushed { dismiss() } else { snap.desk = nil; snap.deskGen += 1 }
            } label: {
                Text("←").font(.system(size: 13)).foregroundStyle(Web.muted)
                    .frame(width: 30, height: 30)
                    .background(Web.panel, in: RoundedRectangle(cornerRadius: 8))
                    .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) }
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Back")
            HStack(spacing: 6) {
                Text(md(thread.title)).font(.system(size: 15, weight: .semibold)).tracking(-0.18)
                    .lineLimit(1).truncationMode(.tail)
                    .padding(.trailing, 6)
                    .layoutPriority(1)
                HStack(spacing: 4) {
                    ForEach(Array(headPills.enumerated()), id: \.offset) { _, p in
                        HeadPill(pill: p, act: headAct(p))
                    }
                }.fixedSize()
                if let m = thread.modelShort {
                    Text(m).font(.system(size: 11.5, weight: .semibold)).foregroundStyle(Web.muted)
                        .padding(.horizontal, 7).frame(height: 20)
                        .background(Web.code, in: RoundedRectangle(cornerRadius: 6))
                        .fixedSize()
                }
            }
            .padding(.leading, 4)
            Spacer(minLength: 12)
            if !headMeta.isEmpty {
                Text(headMeta).font(.system(size: 12.5)).monospacedDigit().foregroundStyle(Web.muted)
                    .lineLimit(1).truncationMode(.head)
                    .padding(.trailing, 4)
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 52)
        .background(Web.panel)
        .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
    }

    /// The row's pills, the board's words (views/threads.js headPills): the
    /// session's own pill, else "running" while it works.
    var headPills: [Pill] {
        if let ps = store.board?.pills?[thread.id], !ps.isEmpty { return ps }
        if let p = thread.pill { return [p] }
        return thread.status == "running" ? [Pill(word: "running", tone: "running")] : []
    }

    /// `schedule · next when · $ · tokens`, the console's head-meta.
    var headMeta: String {
        var parts: [String] = []
        if !thread.schedule.isEmpty { parts.append(thread.schedule_label ?? scheduleLabel(thread.schedule)) }
        if let n = thread.next_run_at { parts.append("next " + nextLabel(n)) }
        if thread.cost_usd > 0 { parts.append(usd(thread.cost_usd)) }
        if let n = thread.tokens, n > 0 { parts.append(tokens(n)) }
        return parts.joined(separator: " · ")
    }

    /// What a live pill does when clicked (HEAD_ACT): Stop the turn, or hush
    /// what the session is saying — the card stays.
    /// The hover word is never longer than the pill's own: the pill is sized
    /// by the wider of the two, and "Stop speaking" under "speaking" left the
    /// resting pill twice its word (the owner 2026-09-30: "that pill was far too
    /// wide… about the size of the word"). The tooltip says the whole thing.
    func headAct(_ p: Pill) -> (label: String, help: String, run: () -> Void)? {
        switch p.tone {
        case "running": return ("Stop", "Stop", { stop() })
        case "speaking", "waiting": return ("Stop", "Stop speaking", { headHush() })
        default: return nil
        }
    }

    /// The console's headHush: the local voice, the hub's "speaking" mark,
    /// and every open card of this session that has a line.
    func headHush() {
        Speaker.shared.stop()
        let id = thread.id
        let lines = asks.filter { !$0.isClosed && !($0.said ?? "").isEmpty }.map(\.id)
        Task {
            _ = try? await hub.voice(thread: id, secs: 0)
            for card in lines { try? await hub.hushVoice(card: card) }
            try? await Task.sleep(for: .milliseconds(1200))
            await load()
        }
    }

    /// The owner's own message (skin.css): a soft grey block
    /// against the right wall, dark text, the "↩ Read it · <card>" quote over
    /// it, and only the time under it — "sent by the owner" dropped, any other
    /// sender kept. At most 680 wide (860 for a long one).
    @ViewBuilder func macMine(_ m: ThreadMessage) -> some View {
        let long = isLongText(m.text)
        HStack(spacing: 0) {
            Spacer(minLength: 60)
            VStack(alignment: .leading, spacing: 5) {
                if m.kind == "checkin" { Text("⏰ scheduled check-in").font(.system(size: 12.5)).foregroundStyle(Web.muted) }
                ForEach(macReplyLines(m), id: \.self) { r in
                    (Text("↩ ") + Text(r.label).bold() + Text(" · " + r.title))
                        .font(.system(size: 13)).foregroundStyle(Web.muted).lineLimit(2)
                        .padding(.leading, 9)
                        .overlay(alignment: .leading) { Rectangle().fill(Web.lineStrong).frame(width: 2) }
                }
                let refs = m.attachments ?? []
                ForEach(refs.indices, id: \.self) { i in BlobAttachment(ref: refs[i], maxHeight: 180) }
                if !m.text.isEmpty {
                    let open = openLong.contains(m.id)
                    if long && !open {
                        StyledText(text: m.text, color: .primary)
                            .frame(maxHeight: 320, alignment: .top)
                            .clipped()
                            .overlay(alignment: .bottom) {
                                LinearGradient(colors: [.clear, Web.code], startPoint: .top, endPoint: .bottom)
                                    .frame(height: 64).allowsHitTesting(false)
                            }
                    } else {
                        StyledText(text: m.text, color: .primary)
                    }
                    if long {
                        Button(open ? "Show less" : "Show all · \(longLineCount(m.text)) lines") {
                            if open { openLong.remove(m.id) } else { openLong.insert(m.id) }
                        }
                        .buttonStyle(WebButtonStyle(small: true))
                    }
                }
                HStack(spacing: 8) {
                    Spacer(minLength: 0)
                    if let by = sentBy(m), by.who != "you" {
                        if let id = by.thread {
                            Button { Task { if let t = try? await hub.thread(id) { openThread = t } } } label: {
                                Text("sent by \(Text(by.who).underline())").lineLimit(1)
                            }.buttonStyle(.plain)
                        } else {
                            Text("sent by \(by.who)").lineLimit(1)
                        }
                    }
                    Text(macMeta(m))
                }
                .font(.system(size: 11)).foregroundStyle(Web.muted)
                .padding(.top, -1)
            }
            .padding(.vertical, 11).padding(.horizontal, 16)
            .background(Web.code, in: RoundedRectangle(cornerRadius: 16))
            .overlay { if m.id == focusMessage { RoundedRectangle(cornerRadius: 16).strokeBorder(Color.red.opacity(0.6), lineWidth: 1.5) } }
            .chatAbout(bubbleSubject(m))
            .frame(maxWidth: long ? 860 : 680, alignment: .trailing)
        }
        .padding(.horizontal)
        // `.msg.owner { margin: 26px 0 16px }` against the stack's 12.
        .padding(.top, 14).padding(.bottom, 4)
    }

    /// The block's foot: when, and what that turn cost if it did.
    func macMeta(_ m: ThreadMessage) -> String {
        var s = m.ts.formatted(date: .omitted, time: .shortened)
        if m.cost_usd > 0 { s += " · " + usd(m.cost_usd) }
        if let n = m.tokens, n > 0 { s += " · " + tokens(n) }
        return s
    }

    /// The console's replyLineHTML: the verdict word bold, the card's title
    /// plain, no emoji.
    func macReplyLines(_ m: ThreadMessage) -> [MacReplyLine] {
        let list = (m.replies?.isEmpty == false) ? m.replies! : [PromptReply(ref: m.in_reply_to ?? "", outcome: m.outcome)]
        return list.compactMap { r in
            let parts = r.ref.split(separator: ":", maxSplits: 1).map(String.init)
            guard parts.count == 2 else { return nil }
            let kind = parts[0], id = parts[1]
            let title: String
            switch kind {
            case "rec": title = recs.first { $0.id == id }?.title ?? id
            case "ask": title = asks.first { $0.id == id }?.title ?? id
            case "action": title = actions.first { $0.id == id }?.title ?? id
            default: title = "\(kind) \(id)"
            }
            return MacReplyLine(label: r.label ?? r.outcome ?? "", title: mdPlain(title))
        }
    }

    /// The armed cards as the console's `.reply-strip`: a blue rail and an 8%
    /// blue wash across the top of the box, one line per card — its pick,
    /// its title — and × to drop that one.
    var macReplyStrip: some View {
        VStack(spacing: 0) {
            ForEach(replies) { r in
                HStack(spacing: 8) {
                    (Text(r.label).foregroundStyle(Web.accent).fontWeight(.semibold) + Text(" · " + mdPlain(r.title)).foregroundStyle(Color.primary))
                        .font(.system(size: 13)).lineLimit(1)
                    Spacer(minLength: 4)
                    Button { replies.removeAll { $0.ref == r.ref } } label: {
                        Text("×").font(.system(size: 15)).foregroundStyle(Web.muted)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Drop \(mdPlain(r.title))")
                }
                .padding(.vertical, 6).padding(.leading, 13).padding(.trailing, 16)
            }
        }
        .background(Web.accent.opacity(0.08))
        .overlay(alignment: .leading) { Rectangle().fill(Web.accent).frame(width: 3) }
        .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
    }
}

struct MacReplyLine: Hashable {
    let label: String
    let title: String
}

/// One of the head's pills (skin.css `.chat-head .head-id .pill`): 20 tall,
/// 11.5pt semibold, radius 6, in its tone. A live one (running, speaking,
/// waiting) is a button: hovered it turns red and reads its act — Stop, Stop
/// speaking — at the same size, so nothing jumps (the owner 2026-09-28 on the
/// console; the desktop the same, 2026-09-29).
struct HeadPill: View {
    let pill: Pill
    let act: (label: String, help: String, run: () -> Void)?
    @State private var hover = false

    var tint: Color? {
        switch pill.tone {
        case "running", "read": Web.accent
        case "speaking", "done": Color.green
        case "waiting": Color.orange
        case "needs": Color.red
        case "install": Color.teal
        default: nil
        }
    }

    var body: some View {
        if let act {
            Button(action: act.run) { face(hot: hover, word: act.label) }
                .buttonStyle(.plain)
                .onHover { hover = $0 }
                .help(act.help)
        } else {
            face(hot: false, word: nil)
        }
    }

    /// The dot and word and, hovered, the square and the act laid over them
    /// in the same cell (the console's `.w` and `.h`): the pill keeps the
    /// wider width, and the act sits in its middle — "Stop" used to hang at
    /// the left of a pill sized for "running" (the owner 2026-10-05 00:44:
    /// "this is a little misaligned… it's in this weird left-turn state").
    private func face(hot: Bool, word: String?) -> some View {
        let color = hot ? Color.red : (tint ?? Web.muted)
        return ZStack {
            HStack(spacing: 6) {
                if act != nil {
                    if pill.tone == "waiting" { Circle().fill(color).frame(width: 6, height: 6) }
                    else { LiveDot(color: color, size: 6) }
                }
                Text(pill.word)
            }
            .opacity(hot ? 0 : 1)
            if let word {
                HStack(spacing: 6) {
                    RoundedRectangle(cornerRadius: 1).fill(Color.red).frame(width: 6, height: 6)
                    Text(word)
                }
                .opacity(hot ? 1 : 0)
            }
        }
        .font(.system(size: 11.5, weight: .semibold)).lineLimit(1)
        .foregroundStyle(color)
        .padding(.horizontal, 7).frame(height: 20)
        .background(hot ? Color.red.opacity(0.12) : (tint.map { $0.opacity(0.15) } ?? Web.code), in: RoundedRectangle(cornerRadius: 6))
        .contentShape(Rectangle())
    }
}
#endif

/// What the agent did during one run: tool calls (with their results),
/// thinking and interim text, in order. ALWAYS collapsed to its one-line
/// summary until the owner taps it — live or finished. A live run used to open itself,
/// so opening a working session buried the conversation under its steps; the
/// folded headline still reads "N tool calls · now: <step>" with the dot.
struct RunActivity: View {
    let events: [ThreadEvent]
    let live: Bool
    /// The hub's count for this block, counted in SQL over every event it
    /// stored. It is the headline whenever it exists: the events held here
    /// are only what this screen happened to draw, so counting them made the
    /// number move as the page slid.
    var count: StepCount? = nil
    /// Fetch this block's own events, called once when the owner opens it.
    var open: (() async -> Void)? = nil
    @State private var expanded: Bool? = nil
    #if targetEnvironment(macCatalyst)
    @State private var summaryHover = false
    #endif
    var isOpen: Bool { expanded ?? false }
    /// LIFE_SHOT_STEPS=1 (ops/mac-screens.sh STEPS=1): every run block and
    /// each of its rows open, so a shot shows the step UI itself.
    static let shotSteps = ProcessInfo.processInfo.environment["LIFE_SHOT_STEPS"] == "1"

    var tools: Int { count?.tools ?? events.filter { $0.kind == "tool_use" }.count }
    var summary: String {
        var parts: [String] = []
        if tools > 0 { parts.append("\(tools) tool call\(tools == 1 ? "" : "s")") }
        let th = count?.thoughts ?? events.filter { $0.kind == "thinking" }.count
        if th > 0 { parts.append("\(th) thought\(th == 1 ? "" : "s")") }
        let all = count?.steps ?? events.count
        if parts.isEmpty { parts.append("\(all) step\(all == 1 ? "" : "s")") }
        return parts.joined(separator: " · ")
    }
    /// The step in flight: the last tool call still waiting for its result;
    /// once every call has answered the model is thinking or writing.
    var now: String? {
        guard live, !events.isEmpty else { return nil }
        if let p = rows.last(where: { $0.0.kind == "tool_use" && $0.1 == nil }) {
            let s = p.0.summary ?? ""
            return s.isEmpty ? p.0.title : s
        }
        return "thinking"
    }
    var headline: String {
        if live && events.isEmpty && (count?.steps ?? 0) == 0 { return "starting…" }
        if let now { return summary + " · now: " + now }
        return summary
    }
    /// Steps the hub counted that this screen has not read yet — shown as one
    /// line inside the open block while the ranged read runs.
    var missing: Int { max(0, (count?.steps ?? 0) - rows.reduce(0) { $0 + 1 + ($1.1 == nil ? 0 : 1) }) }
    /// Pair each tool_result with the tool_use before it.
    var rows: [(ThreadEvent, ThreadEvent?)] {
        var out: [(ThreadEvent, ThreadEvent?)] = []
        for e in events {
            if e.kind == "tool_result", let i = out.lastIndex(where: { $0.0.kind == "tool_use" && $0.1 == nil }) { out[i].1 = e } else { out.append((e, nil)) }
        }
        return out
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                let opening = !isOpen
                withAnimation { expanded = opening }
                // A folded block holds no events until it is opened: one
                // ranged read here keeps a 500-step turn as cheap to open as
                // a 5-step one.
                if opening, let open { Task { await open() } }
            } label: {
                #if targetEnvironment(macCatalyst)
                // The console's `.run > summary` (skin.css), the owner 2026-09-29:
                // a faint ▸/▾, the headline in 12.5 grey, no gear; hovered, a
                // grey wash behind the row.
                HStack(spacing: 6) {
                    Text(isOpen ? "▾" : "▸").opacity(0.55)
                    Text(headline).lineLimit(1)
                    if live { LiveDot(color: Web.muted, size: 6) }
                }
                .font(.system(size: 12.5)).foregroundStyle(Web.muted)
                .padding(.vertical, 3).padding(.leading, 6).padding(.trailing, 8)
                .background(summaryHover ? Web.code : Color.clear, in: RoundedRectangle(cornerRadius: 6))
                .padding(.leading, -6)
                .contentShape(Rectangle())
                .onHover { summaryHover = $0 }
                #else
                HStack(spacing: 6) {
                    Image(systemName: isOpen ? "chevron.down" : "chevron.right").font(.caption2.weight(.semibold))
                    Image(systemName: "gearshape.2").font(.caption)
                    Text(headline).font(.caption).lineLimit(1)
                    if live { LiveDot(color: .secondary, size: 5) }
                    Spacer()
                }.foregroundStyle(.secondary)
                #endif
            }.buttonStyle(.plain)
            if isOpen {
                VStack(alignment: .leading, spacing: 4) {
                    if missing > 0 {
                        Text("loading \(missing) earlier step\(missing == 1 ? "" : "s")…")
                            .font(.caption2).foregroundStyle(.secondary)
                    }
                    ForEach(rows, id: \.0.id) { r in EventRow(e: r.0, result: r.1) }
                }.padding(.leading, 4)
            }
        }
        #if targetEnvironment(macCatalyst)
        // Flush with the column's left wall, at most 760 wide (`.activity`).
        .frame(maxWidth: 760, alignment: .leading)
        .padding(.horizontal)
        #else
        .padding(.horizontal)
        .padding(.leading, 8)
        #endif
        .chatAbout(ChatSubject(screen: "Session activity", title: "What it did here · \(summary)", facts: runFacts))
        .task {
            if Self.shotSteps, expanded == nil {
                expanded = true
                if let open { await open() }
            }
        }
    }

    /// The run's steps in order, for "why did you do this?".
    var runFacts: String {
        let steps = events.prefix(30).map { e -> String in
            switch e.kind {
            case "tool_use": "- tool: \((e.summary ?? "").isEmpty ? e.title : e.summary!)"
            case "thinking": "- thought: \(e.body.prefix(160))"
            case "tool_result": "- result: \(e.body.prefix(160))"
            default: "- said: \(e.body.prefix(200))"
            }
        }
        return (steps + (events.count > 30 ? ["- …\(events.count - 30) more steps"] : [])).joined(separator: "\n")
    }
}

/// One step of a run. Opened, it is one plain terminal block, not a stack of
/// grey boxes (the owner 2026-10-05 23:28: "the UI just looks a little
/// funky with these gray boxes… a light mode terminal display with lightly
/// colored text"; the console's `.ev .io`). A thought: the purple line IS
/// the thought — shut, its first line; open, all of it, nothing printed
/// twice under it. A tool call: the call in blue (a shell command behind
/// `$`, whole; any other tool's title line), what went in when the title
/// cannot say it (an edit's lines), then the output in grey — its first
/// `fold` lines, the rest behind "… N more lines" — or the error in red,
/// whole ("the error is good"). A body that is still the input's JSON
/// (stored before 10-05) is not shown: the title already says it.
struct EventRow: View {
    let e: ThreadEvent
    let result: ThreadEvent?
    @State private var open = RunActivity.shotSteps
    @State private var whole = false
    static let fold = 12
    #if targetEnvironment(macCatalyst)
    static let violet = Web.violet, red = Web.red, green = Web.green, blue = Web.accent
    #else
    static let violet = Color.purple, red = Color.red, green = Color.green, blue = Color.blue
    #endif

    var icon: String {
        switch e.kind { case "thinking": "brain"; case "tool_use": "terminal"; case "text": "text.bubble"; default: "arrow.turn.down.right" }
    }
    var tint: Color { e.kind == "thinking" ? Self.violet : result?.title == "error" ? Self.red : .secondary }
    /// Tool calls read as plain English ("List goals", "Read DESIGN.md").
    var headline: String {
        switch e.kind {
        case "tool_use": (e.summary ?? "").isEmpty ? e.title : e.summary!
        case "thinking": open ? e.body : "Thinking · " + (e.body.split(whereSeparator: \.isNewline).first.map(String.init) ?? "")
        default: e.body
        }
    }
    /// "Bash · grep …" → "Bash".
    var tool: Substring { e.title.prefix { $0 != " " } }
    /// The call line: a shell command whole behind `$`. Any other tool's
    /// headline already names the call ("Edit app/…/x.swift"), so its block
    /// holds only what went in (an edit's lines) and what came out.
    var call: String { tool == "Bash" ? "$ " + e.body : "" }
    var input: String { tool == "Bash" || e.body.hasPrefix("{") ? "" : e.body }
    var outLines: [Substring] {
        guard let r = result else { return [] }
        return (r.body.isEmpty ? "(no output)" : r.body).split(separator: "\n", omittingEmptySubsequences: false)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Button { withAnimation { open.toggle() } } label: {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: icon).font(.caption2).frame(width: 14)
                    Text(headline).font(.caption).lineLimit(open ? nil : (e.kind == "text" ? 6 : 1)).multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                    if result != nil, !open { Image(systemName: "checkmark").font(.caption2).foregroundStyle(result?.title == "error" ? Self.red : Self.green) }
                }.foregroundStyle(tint)
            }.buttonStyle(.plain)
            if open, e.kind == "tool_use" {
                let bad = result?.title == "error"
                let shown = whole || bad ? outLines : Array(outLines.prefix(Self.fold))
                VStack(alignment: .leading, spacing: 2) {
                    if !call.isEmpty { Text(call).foregroundStyle(Self.blue) }
                    if !input.isEmpty { Text(input).foregroundStyle(.secondary) }
                    if result != nil {
                        Text(shown.joined(separator: "\n")).foregroundStyle(bad ? Self.red : .secondary)
                        if !whole, !bad, outLines.count > Self.fold {
                            Button("… \(outLines.count - Self.fold) more lines") { withAnimation { whole = true } }
                                .buttonStyle(.plain).foregroundStyle(.secondary)
                        }
                    }
                }
                .font(.system(.caption2, design: .monospaced)).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.leading, 20)
            }
        }
    }
}

struct ThreadSettings: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    @Binding var thread: Thread
    @State private var schedule = ""
    @State private var schedulePrompt = ""
    @State private var title = ""
    @State private var error: String?
    /// This session's own future: prompts queued for it — the owner's message
    /// for tomorrow morning, and the check-backs the agent set itself.
    /// Soonest first: the hub lists prompts newest-CREATED first, which put a
    /// Sep 28 wake above a Sep 19 one — and the folded bar names the next one,
    /// so the order it reports has to be the order under it.
    private var queued: [Prompt] {
        (prompts.value ?? []).sorted { ($0.not_before ?? .distantPast) < ($1.not_before ?? .distantPast) }
    }
    @State private var prompts = HubLoad<[Prompt]>()
    /// The list FOLDS, and shut is where it rests: the closed row is the count, when the next one fires, and its words underneath.
    /// Same fold, same key as the console's strip.
    @AppStorage("queuedOpen") private var queuedOpen = false

    var body: some View {
        NavigationStack {
            Form {
                if let error { Section { ErrorBanner(message: error) } }
                Section("Title") { TextField("Title", text: $title) }
                Section {
                    SchedulePicker(schedule: $schedule)
                    TextField("What to do at each check-in (optional)", text: $schedulePrompt, axis: .vertical).lineLimit(2...6)
                } header: { Text("Schedule") }
                if !queued.isEmpty {
                    Section {
                        DisclosureGroup(isExpanded: $queuedOpen) {
                        ForEach(queued) { p in
                            VStack(alignment: .leading, spacing: 3) {
                                HStack(spacing: 6) {
                                    Image(systemName: p.standing ? "clock.arrow.circlepath" : p.author == "owner" ? "person.crop.circle" : "arrow.uturn.left.circle").font(.caption2)
                                    // A standing row (one clock) is the
                                    // check-in itself: its cadence in the same words
                                    // as the session card, and Turn off, because
                                    // cancelling it IS the schedule going off.
                                    if p.standing { Chip(scheduleLabel(p.repeat ?? ""), icon: "repeat") }
                                    Text((p.standing ? "next " : "") + p.whenLabel).font(.caption.weight(.semibold))
                                    // Only a bare "new" target starts a session.
                                    // "new-or:<this thread>" RESUMES this one —
                                    // labelling those "new session" is what read
                                    // as a pile of future threads being created.
                                    if p.target == "new" { Chip("new session", icon: "plus.bubble") }
                                    Spacer(minLength: 0)
                                    Button(p.standing ? "Turn off" : "Cancel") {
                                        Task {
                                            do { _ = try await hub.cancelPrompt(p.id); await loadPrompts() }
                                            catch { self.error = error.localizedDescription }
                                        }
                                    }.font(.caption).buttonStyle(.borderless)
                                }.foregroundStyle(.secondary)
                                // TITLE first, text only when there is none
                                // A calendar wake's text is its title + "\n\n" +
                                // the whole instruction, so showing text printed
                                // the title twice and then a paragraph of prose.
                                Text(p.rowLabel).font(.subheadline).lineLimit(2)
                            }.padding(.vertical, 2)
                        }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text("Coming up · \(queued.count)").font(.subheadline.weight(.semibold))
                                    Spacer(minLength: 0)
                                    if let n = queued.first {
                                        Text("next " + n.whenLabel).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                                    }
                                }
                                // The name only if it fits on its own line —
                                // the time is what the bar owes the owner.
                                if let n = queued.first {
                                    Text(n.rowLabel).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                                }
                            }
                        }
                    }
                }
                Section("About") {
                    LabeledContent("Status", value: thread.status.replacingOccurrences(of: "_", with: " "))
                    if let r = thread.last_run_at { LabeledContent("Last run", value: shortAgo(r)) }
                    LabeledContent("Total cost", value: usd(thread.cost_usd))
                    // Split by model: a session that ran all night usually
                    // changed model when a plan bucket filled, and the rates
                    // differ by 5x, so one blended number hides where the
                    // money went.
                    if let by = thread.cost_by_model, by.count > 1 {
                        ForEach(by) { c in
                            LabeledContent("· \(c.label)", value: usd(c.cost_usd))
                        }
                    }
                    if let n = thread.tokens, n > 0 {
                        LabeledContent("Tokens", value: n.formatted(.number))
                        // Where they went: a resumed session is nearly all
                        // cache reads, which is why the total looks enormous.
                        LabeledContent("· in / out", value: "\(tokenCount(thread.tokens_in ?? 0)) / \(tokenCount(thread.tokens_out ?? 0))")
                        LabeledContent("· cache read / write", value: "\(tokenCount(thread.tokens_cache_read ?? 0)) / \(tokenCount(thread.tokens_cache_write ?? 0))")
                    }
                    LabeledContent("Project", value: thread.project)
                }
            }
            .navigationTitle("Session settings")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            // Stay open on failure (e.g. a schedule the hub rejects) so the edit isn't lost.
                            do { thread = try await hub.patchThread(thread.id, ["title": title, "schedule": schedule, "schedule_prompt": schedulePrompt]); dismiss() }
                            catch { self.error = error.localizedDescription }
                        }
                    }
                }
            }
            .onAppear { schedule = thread.schedule; schedulePrompt = thread.schedule_prompt; title = thread.title }
            .task { await loadPrompts() }
        }
    }

    /// Quiet on failure (`prompts.error` is never drawn): an older hub has no
    /// /prompts, and that is not worth an error banner over the settings the
    /// owner actually came here for.
    func loadPrompts() async {
        await prompts.run { try await hub.prompts(state: "queued", thread: thread.id) }
    }
}

extension Prompt {
    /// One short line for the "Coming up" list. A calendar wake's `text` is its
    /// title, a blank line, and the whole instruction — printing that put the
    /// title twice and then a paragraph in a row three lines tall, which is
    /// what made them unreadable. The title is the label; the words are on
    /// the calendar item itself.
    var rowLabel: String {
        if let t = title, !t.isEmpty { return t }
        if !text.isEmpty { return text }
        return outcome ?? (standing ? "the default check-in" : "")
    }
}

/// One card armed on the chat bar (several can be at once). `ref`
/// is what the hub takes — "ask:<id>" / "rec:<id>" / "action:<id>" — and
/// `outcome` the pick: a read's "done", a rec's accepted/declined, a
/// proposal's approved/denied, or "" for words alone.
struct ArmedReply: Identifiable, Hashable {
    enum Kind { case ask, rec, action }
    let kind: Kind
    let id: String
    let title: String
    var outcome: String
    /// The card's own answers as the hub sent them — where the banner's word
    /// comes from.
    var outcomes: [AskOutcome] = []
    var ref: String {
        switch kind { case .ask: "ask:" + id; case .rec: "rec:" + id; case .action: "action:" + id }
    }
    /// The banner's word for the pick: the hub's label for it — the word on
    /// the button tapped (I did this · Won't do · Decided · Accepted…).
    /// Words alone on an ask read "Replying to" (the owner 2026-10-02: "make it so
    /// that the physical text in the chat bar says done or wont do instead
    /// of replying to").
    var label: String {
        if let l = outcomes.first(where: { $0.value == outcome && !$0.value.isEmpty })?.label { return l }
        return kind == .ask ? "Replying to" : "Reply"
    }
    /// The banner's mark: the card's own icon and colour.
    var icon: String {
        switch kind { case .ask: "arrowshape.turn.up.left.fill"; case .rec: "lightbulb.fill"; case .action: "hand.raised.fill" }
    }
    var tint: Color {
        switch kind { case .ask: Color.accentColor; case .rec: .purple; case .action: .red }
    }
}
