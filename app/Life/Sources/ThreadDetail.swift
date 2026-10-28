// One session's chat: ThreadDetail and the settings sheet. Every message and
// closed card is one ChatRow; the tool calls are not drawn.
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
    @State private var posedCard: String?
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
    /// the (non-lazy) message list and re-sorting `ordered`. That is the
    /// composer getting slower the longer the text and the thread.
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
    /// This session's queued wakes, soonest first, for the "Coming up" strip
    /// over the composer (ComingUpStrip). Refilled on every `load()`.
    @State private var queuedPrompts: [Prompt] = []
    @State private var pickingWhen = false
    @State private var pickWhenDate = Date().addingTimeInterval(3600)
    /// How many of the newest rows are drawn. The stack below is deliberately
    /// NOT lazy (the opening scroll needs real targets), so a 90-message
    /// session built every row before the first frame could appear, and tore
    /// them all down again on the way back to Sessions. Older turns are one
    /// tap away.
    @State private var shown = rowWindow
    /// The whole conversation is drawn (the toggle at the top of the chat).
    /// Off every time a chat opens: only the cards still open are drawn. The
    /// console's chatState.full (threads.js).
    @State private var full = false
    /// THE TOOL CHAIN of the turn in flight, opened by a tap on the working
    /// line's count (the console's chatState.chain): the run it belongs to,
    /// its events as read so far (only ever added to), and the window's ends.
    @State private var chainOpen = false
    @State private var chainRun = ""
    @State private var chainEvents: [ThreadEvent] = []
    @State private var chainFirst: Int64 = 0
    @State private var chainLast: Int64 = 0
    @State private var chainLoaded = false
    @State private var chainAll = false
    @State private var chainBusy = false
    /// With helpers, the session's own calls and each helper's open on a
    /// second tap.
    @State private var chainMe = false
    @State private var chainSubs: [String: SubChain] = [:]
    static let chainRows = 12

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

    /// When the turn in flight started: its starter, the latest message of a
    /// run with no reply yet.
    var liveSince: Date? {
        guard thread.status == "running" else { return nil }
        let replied = Set(messages.filter { $0.role == "claude" || $0.kind == "error" }.compactMap(\.run_id))
        return messages.last { $0.run_id != nil && !replied.contains($0.run_id!) && $0.role != "claude" }?.ts
    }

    /// That turn's run id: its events are the tool chain.
    var liveRun: String {
        guard thread.status == "running" else { return "" }
        let replied = Set(messages.filter { $0.role == "claude" || $0.kind == "error" }.compactMap(\.run_id))
        return messages.last { $0.run_id != nil && !replied.contains($0.run_id!) && $0.role != "claude" }?.run_id ?? ""
    }

    /// Where "starting · N s" counts from (the console's startingSince): a
    /// message queued while the previous turn was still working is filed
    /// minutes before its turn opens, and counting from its ts read
    /// "starting · 548 s" the moment the previous reply landed. The hub's
    /// turn_at is the turn's start until its first tool call, so the later
    /// of the two.
    var startingSince: Date? {
        guard let s = liveSince else { return nil }
        if let at = thread.turn_at, at > s { return at }
        return s
    }

    /// The owner's messages the session is working on: the turn in flight's
    /// starters, with no reply yet. The open-cards view keeps them, so a Send
    /// does not vanish into the working line.
    var liveStarters: Set<Int> {
        guard thread.status == "running" else { return [] }
        let replied = Set(messages.filter { $0.role == "claude" || $0.kind == "error" }.compactMap(\.run_id))
        return Set(messages.filter { $0.run_id != nil && !replied.contains($0.run_id!) && $0.role != "claude" }.map(\.id))
    }

    /// One cell of the open-cards view (`openCells`), in conversational order.
    enum OpenCell: Identifiable {
        case message(ThreadMessage), ask(Ask, Double), action(Action, Double), rec(Rec, Double)
        var id: String {
            switch self {
            case .message(let m): "msg:\(m.id)"
            case .ask(let a, _): a.id
            case .action(let a, _): a.id
            case .rec(let r, _): r.id
            }
        }
    }

    /// What is the owner's to act on: every ask not closed, approval still
    /// open and rec not closed, where it sits in the conversation, plus the
    /// message in flight. Every message row and closed card is behind "Show
    /// full chat" (the console's openCards, threads.js).
    func openCells(_ rows: [ThreadMessage]) -> [OpenCell] {
        var out: [OpenCell] = []
        let live = liveStarters
        for m in rows {
            if live.contains(m.id) { out.append(.message(m)) }
            for a in asksByMessage[m.id] ?? [] where !a.isClosed { out.append(.ask(a, leadCost(m, "ask:" + a.id))) }
            for a in actionsByMessage[m.id] ?? [] where a.open == true { out.append(.action(a, leadCost(m, "action:" + a.id))) }
            for r in recsByMessage[m.id] ?? [] where !r.isClosed { out.append(.rec(r, leadCost(m, "rec:" + r.id))) }
        }
        for a in tailAsks where !a.isClosed { out.append(.ask(a, 0)) }
        for a in tailActions where a.open == true { out.append(.action(a, 0)) }
        for r in tailRecs where !r.isClosed { out.append(.rec(r, 0)) }
        return out
    }

    /// Conversational order, not timestamp order. A message the owner sends while
    /// a run is in flight is stored at its own time (before that run's
    /// reply) but only becomes the *next* turn, so by timestamp it — and its
    /// activity block — sat above the previous reply, making the earlier
    /// reply look like it was mid-tool-chain and the later one orphaned.
    /// Turns are sequential and each is one run id (`<run>`, `<run>-t2`, …),
    /// so a turn draws as one piece (starter(s) → reply → its cards) and
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
    func visible(_ rows: [ThreadMessage]) -> [ThreadMessage] { rows.count <= shown ? rows : Array(rows.suffix(shown)) }

    /// Make sure a given message is inside the drawn window — the row the
    /// thread scrolls to on open, and any ask still waiting on the owner, which
    /// must never be hidden behind "Show earlier".
    func widen(toRow id: Int?) {
        let rows = ordered
        guard let id, let i = rows.firstIndex(where: { $0.id == id }) else { return }
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
    #if targetEnvironment(macCatalyst) || targetEnvironment(simulator)
    /// LIFE_SHOT_STEPCARD=1: a morning-routine card posed on this chat's
    /// newest card, two of its steps staged on the bar (ops/mac-screens.sh,
    /// ops/screens.sh). Nothing is sent.
    func poseStepCard() {
        guard ProcessInfo.processInfo.environment["LIFE_SHOT_STEPCARD"] == "1" else { return }
        Task {
            try? await Task.sleep(for: .seconds(3))
            guard var a = asks.last else { print("shot-stepcard: no card in this chat"); return }
            a.id = "cal-shot0001"; a.cal_id = a.id; a.title = "Do the morning routine"; a.detail = ""
            a.kind = "physical"; a.state = "active"; a.outcomes = StepCardShot.outcomes
            a.message_id = nil; a.created_at = .now
            a.open = true; a.resolved_at = nil; a.resolution = nil
            a.class = "physical"; a.verb = "do"; a.said = nil; a.superseded_by = nil
            a.steps = [CalStep(n: 1, title: "Stretch", state: "done", mark: "done"),
                       CalStep(n: 2, title: "Read 10 pages", state: "open", mark: "todo"),
                       CalStep(n: 3, title: "Water the plants", state: "open", mark: "todo")]
            StepCardShot.posed = a
            placeAsks(asks)
            posedCard = a.id
            arm(ArmedReply(kind: .step, id: "cal-shot0001.2", title: "Read 10 pages", outcome: "done", outcomes: ArmedReply.stepOutcomes))
            arm(ArmedReply(kind: .step, id: "cal-shot0001.3", title: "Water the plants", outcome: "wont", outcomes: ArmedReply.stepOutcomes))
        }
    }
    #endif

    /// The card to scroll to, once placed: the focus card, or a shot's posed one.
    private var focusScroll: FocusScroll? {
        if let p = posedCard { return FocusScroll(id: p, anchor: .bottom) }
        if focusLanded, let f = focusCard { return FocusScroll(id: f, anchor: .center) }
        return nil
    }

    func placeAsks(_ given: [Ask]) {
        var all = given
        #if targetEnvironment(macCatalyst) || targetEnvironment(simulator)
        if let p = StepCardShot.posed, !all.contains(where: { $0.id == p.id }) { all.append(p) }
        #endif
        asks = all
        var by: [Int: [Ask]] = [:], tail: [Ask] = []
        let order = ordered
        let active = { (a: Ask) in !a.isClosed }
        for a in all {
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
    /// One older than every loaded message is history and is not drawn
    /// unless it is still the owner's to decide (the web's refreshThread
    /// agrees): old decided relays sat under today's cards because the
    /// chat loads a newer window.
    func placeActions(_ pending: [Action]) {
        actions = pending
        var by: [Int: [Action]] = [:], tail: [Action] = []
        let order = ordered
        for a in pending {
            if let host = order.last(where: { $0.ts <= a.created_at }) {
                by[host.id, default: []].append(a)
                if a.open == true || a.id == focusCard { widen(toRow: host.id) }
            } else if a.open == true || a.id == focusCard || order.isEmpty {
                tail.append(a)
            }
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
        let newest = order.map(\.id).max() ?? 0
        for r in all {
            if let mid = r.message_id, order.contains(where: { $0.id == mid }) {
                by[mid, default: []].append(r)
            } else if r.message_id.map({ $0 == 0 || $0 > newest }) ?? true {
                tail.append(r)
            }
            // Its reply is older than the loaded messages: not drawn (it sat
            // at the end, under the newest message); the Recs tab still holds it.
        }
        recsByMessage = by
        tailRecs = tail
    }

    /// One rec's cell. Each way arms the chat bar with the rec and that pick
    /// (the console's composer does the same): the owner's note is typed where a
    /// message is, and the decision lands in this chat as "↩ Accepted ·
    /// <title>" over it — alongside whatever other cards are armed.
    func recCell(_ r: Rec, cost: Double = 0) -> some View {
        RecCard(rec: r, decide: { outcome in
            arm(ArmedReply(kind: .rec, id: r.id, title: r.title, outcome: outcome, outcomes: r.outcomes ?? []))
        }, reload: { await load() }, cost: cost).id(r.id)
    }

    /// One proposal's cell: Approve · Deny · Reply each arm the chat bar
    /// with the action and that pick, exactly as a rec's buttons do. Send posts one prompt naming it; the hub decides the row.
    func approvalCell(_ a: Action, cost: Double = 0) -> some View {
        ApprovalCard(a: a, inThread: true, arm: { outcome in
            arm(ArmedReply(kind: .action, id: a.id, title: a.title, outcome: outcome, outcomes: a.outcomes ?? []))
        }, cost: cost) { await load() }.id(a.id)
    }

    /// Arm a card without dropping the others; the same card again only
    /// changes its pick.
    func arm(_ r: ArmedReply) {
        if let i = replies.firstIndex(where: { $0.ref == r.ref }) { replies[i].outcome = r.outcome } else { replies.append(r) }
        draft.focusRequest += 1
    }


    /// The first card under a message: an empty reply's dollars ride on it
    /// (the console's msgHTML), so the turn is one row, not a reply row and
    /// a card.
    func leadRef(_ m: ThreadMessage) -> String? {
        if let a = asksByMessage[m.id]?.first { return "ask:" + a.id }
        if let a = actionsByMessage[m.id]?.first { return "action:" + a.id }
        return recsByMessage[m.id]?.first.map { "rec:" + $0.id }
    }

    func leadCost(_ m: ThreadMessage, _ ref: String) -> Double {
        m.role == "claude" && leadRef(m) == ref ? m.cost_usd : 0
    }

    /// The working line under the chat, the console's paintWorking: a dot
    /// and its words; on the phone, Stop beside it.
    @ViewBuilder func workingLine(_ text: String, stop canStop: Bool = false) -> some View {
        workingLine(Text(text), stop: canStop)
    }

    /// The working line of a turn at work: "working · 23 tool calls · 3
    /// subagents running · today 10:09 PM · $1.12", the count and the helpers
    /// the way into the tool chain (the console's chain-link).
    func workingFacts() -> some View {
        let n: Int = thread.turn_tools ?? 0
        let ownChain: Bool = chainLoaded && !chainRun.isEmpty
        let helpers: Int = ownChain ? toolChain(chainEvents, tasks: thread.turn_tasks).helpers.count : thread.taskHelpers
        let words: String = [n > 0 ? callsWord(n) : "", helpersWords(helpers)].filter { !$0.isEmpty }.joined(separator: " · ")
        let facts: String = thread.turnFacts(withCalls: false)
        let rest: String = facts.isEmpty ? "" : " · " + facts
        #if targetEnvironment(macCatalyst)
        let link: Color = chainOpen ? Web.accent : Web.muted
        #else
        let link: Color = chainOpen ? Color.accentColor : Color.secondary
        #endif
        let count: Text = Text(words).foregroundColor(link).underline(true, color: link.opacity(0.4))
        let line: Text = words.isEmpty ? Text("working" + rest) : Text("working · \(count)\(rest)")
        return workingLine(line, stop: true, tap: words.isEmpty ? nil : { toggleChain() })
    }

    func callsWord(_ n: Int) -> String { "\(n) tool call" + (n == 1 ? "" : "s") }

    func toggleChain() {
        chainOpen.toggle()
        for (k, s) in chainSubs where !s.ok { chainSubs[k]?.tried = false }
        if chainOpen { Task { await loadChain() } }
    }

    /// The chain under the working line (the console's chainHTML). With no
    /// helper, the calls, newest last, the waiting ones darker; with helpers,
    /// a row per worker, each opening its own calls on a second tap, a helper
    /// once the hub reads its transcript. No dot inside: the working line's
    /// is the one live mark. The last `chainRows` until the earlier ones are
    /// asked for; it goes with the working line when the turn ends.
    @ViewBuilder var chainView: some View {
        let ch: ToolChain = toolChain(chainEvents, tasks: thread.turn_tasks)
        let agents: Int = ch.steps.filter(\.agent).count
        let total: Int = max(0, (thread.turn_tools ?? 0) - agents)
        #if targetEnvironment(macCatalyst)
        let font = Font.system(size: 12.5), inset: CGFloat = 13
        #else
        let font = Font.caption, inset: CGFloat = 26
        #endif
        VStack(alignment: .leading, spacing: 3) {
            if ch.helpers.isEmpty {
                callsView(ch.steps, all: chainAll, total: total, earlier: { Task { await chainEarlier() } })
            } else {
                workerRow(open: chainMe, words: "\(thread.title) · \(callsWord(max(ch.steps.count - agents, total)))") { chainMe.toggle() }
                if chainMe {
                    callsView(ch.steps, all: chainAll, total: total, earlier: { Task { await chainEarlier() } }).padding(.leading, 16)
                }
                ForEach(ch.helpers, id: \.self) { d in helperRow(d) }
            }
        }
        .font(font)
        .padding(.leading, inset)
        .padding(.horizontal)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder func helperRow(_ d: String) -> some View {
        let task: TurnTask? = (thread.turn_tasks ?? []).first { $0.type == "local_agent" && $0.description == d }
        if let tid = task?.id, let s = chainSubs[tid], s.ok {
            let sc: ToolChain = toolChain(s.evs, tasks: nil)
            workerRow(open: s.open, words: "\(d) · \(callsWord(sc.steps.filter { !$0.agent }.count))") { chainSubs[tid]?.open.toggle() }
            if s.open { callsView(sc.steps, all: true, total: 0, earlier: nil).padding(.leading, 16) }
        } else {
            workerRow(open: false, words: "\(d) · subagent", tap: nil)
        }
    }

    /// A worker: a chevron that turns when open, then its words.
    func workerRow(open: Bool, words: String, tap: (() -> Void)?) -> some View {
        #if targetEnvironment(macCatalyst)
        let accent = Web.accent, muted = Web.muted
        #else
        let accent = Color.accentColor, muted = Color.secondary
        #endif
        let lit: Bool = tap != nil && open
        return HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: "chevron.right").font(.system(size: 8, weight: .semibold)).foregroundStyle(muted)
                .rotationEffect(.degrees(open ? 90 : 0)).frame(width: 10).opacity(tap == nil ? 0 : 1)
            Text(words).foregroundStyle(lit ? accent : Color.primary).fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .onTapGesture { tap?() }
    }

    /// One worker's calls, newest last; "N earlier" when more ran than shown.
    @ViewBuilder func callsView(_ steps: [ToolChain.Step], all: Bool, total: Int, earlier more: (() -> Void)?) -> some View {
        #if targetEnvironment(macCatalyst)
        let muted = Web.muted
        #else
        let muted = Color.secondary
        #endif
        let calls: [ToolChain.Step] = steps.filter { !$0.agent }
        let shown: [ToolChain.Step] = all ? calls : Array(calls.suffix(Self.chainRows))
        let earlier: Int = max(calls.count, total) - shown.count
        VStack(alignment: .leading, spacing: 3) {
            if earlier > 0, let more {
                Button(action: more) { Text("\(earlier) earlier").underline().foregroundStyle(muted) }.buttonStyle(.plain)
            }
            ForEach(shown) { s in
                Text(s.label).foregroundStyle(s.error ? Color.red : s.open ? Color.primary : muted).fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// The turn's events, read once and then only added to (since=). The turn
    /// is the newest event's run: a turn a helper's end woke has no message of
    /// its own, and an unanswered message hours back is not this turn; only a
    /// message newer than every event (a send, before its first call) names
    /// the run instead.
    func loadChain() async {
        guard chainOpen, thread.status == "running", !chainBusy else { return }
        chainBusy = true
        defer { chainBusy = false }
        let id = thread.id
        guard let page = try? await hub.threadEvents(id, since: chainLast, limit: chainLast > 0 ? 500 : 300), thread.id == id else { return }
        let newest: ThreadEvent? = page.last
        let since: Date? = liveSince
        let sent: Bool = !liveRun.isEmpty && (since ?? .distantPast) > (newest?.ts ?? .distantPast)
        let run: String = sent ? liveRun : (newest?.run_id ?? chainRun)
        if run != chainRun {
            chainRun = run; chainEvents = []; chainFirst = 0; chainAll = false; chainSubs = [:]
        }
        for e in page {
            chainLast = max(chainLast, e.id)
            chainFirst = chainFirst == 0 ? e.id : min(chainFirst, e.id)
        }
        let mine = page.filter { $0.run_id == run }
        if !mine.isEmpty { chainEvents += mine }
        chainLoaded = true
        await loadSubs()
    }

    /// Each helper's own steps, from its transcript while the turn runs; a
    /// helper the hub cannot read stays a plain row (tried once per opening).
    func loadSubs() async {
        for k in (thread.turn_tasks ?? []) where k.type == "local_agent" {
            guard let tid = k.id else { continue }
            var s: SubChain = chainSubs[tid] ?? SubChain()
            if s.tried && !s.ok { continue }
            s.tried = true
            chainSubs[tid] = s
            guard let page = try? await hub.taskEvents(thread.id, task: tid, since: s.last) else { continue }
            var cur: SubChain = chainSubs[tid] ?? s
            cur.ok = true
            for e in page where e.id > cur.last { cur.last = e.id; cur.evs.append(e) }
            chainSubs[tid] = cur
        }
    }

    /// "N earlier": walk back (before=) to the start of the turn.
    func chainEarlier() async {
        chainAll = true
        let run = chainRun
        while chainFirst > 1 {
            guard let page = try? await hub.threadEvents(thread.id, before: chainFirst, limit: 1000), !page.isEmpty, chainRun == run else { break }
            chainFirst = min(chainFirst, page.map(\.id).min() ?? chainFirst)
            chainEvents = page.filter { $0.run_id == run } + chainEvents
            if page.contains(where: { $0.run_id != run }) { break }
        }
    }

    @ViewBuilder func workingLine(_ text: Text, stop canStop: Bool = false, tap: (() -> Void)? = nil) -> some View {
        #if targetEnvironment(macCatalyst)
        HStack(spacing: 7) {
            LiveDot(color: Web.accent, size: 6)
            text.font(.system(size: 12.5)).foregroundStyle(Web.muted).monospacedDigit()
                .contentShape(Rectangle()).onTapGesture { tap?() }
        }.padding(.horizontal)
        #else
        HStack(spacing: 8) {
            ProgressView()
            text.font(.caption).foregroundStyle(.secondary).monospacedDigit().lineLimit(2)
                .contentShape(Rectangle()).onTapGesture { tap?() }
            Spacer()
            if canStop {
                Button { stop() } label: { Label("Stop", systemImage: "stop.fill").font(.caption.weight(.semibold)) }
                    .buttonStyle(.bordered).tint(.orange).controlSize(.small).disabled(stopping)
            }
        }.padding(.horizontal)
        #endif
    }

    var body: some View {
        let rows = ordered
        let _ = Perf.event("ThreadDetail.body rows=\(rows.count) shown=\(shown)")
        VStack(spacing: 0) {
            #if targetEnvironment(macCatalyst)
            macHead
            #endif
            ScrollViewReader { proxy in
                ScrollView {
                    // Plain VStack (not lazy): rows exist as soon as messages
                    // load, so the initial scrollTo has real targets. That is
                    // affordable because only `shown` of them are built.
                    VStack(alignment: .leading, spacing: 8) {
                        // The one mode button at the top: Show full chat over
                        // the open cards, Open cards only over the whole
                        // conversation (the console's chatFullHTML).
                        Button { withAnimation { full.toggle() } } label: {
                            Label(full ? "Open cards only" : "Show full chat", systemImage: full ? "rectangle.compress.vertical" : "text.alignleft")
                                .font(.caption).foregroundStyle(.secondary)
                        }.buttonStyle(.plain).padding(.horizontal)
                        if full {
                            if rows.count > shown {
                                Button { withAnimation { shown += rowWindow * 2 } } label: {
                                    Label("Show \(min(rowWindow * 2, rows.count - shown)) earlier of \(rows.count)", systemImage: "chevron.up")
                                        .font(.caption).foregroundStyle(.secondary)
                                }.buttonStyle(.plain).padding(.horizontal)
                            }
                            ForEach(visible(rows)) { m in
                                messageRow(m).id(m.id)
                                ForEach(asksByMessage[m.id] ?? []) { a in
                                    AskCard(a: a, goals: goals, cost: leadCost(m, "ask:" + a.id), reload: { await load() }).id(a.id)
                                }
                                ForEach(actionsByMessage[m.id] ?? []) { approvalCell($0, cost: leadCost(m, "action:" + $0.id)) }
                                ForEach(recsByMessage[m.id] ?? []) { recCell($0, cost: leadCost(m, "rec:" + $0.id)) }
                            }
                            ForEach(tailAsks) { a in
                                AskCard(a: a, goals: goals, reload: { await load() }).id(a.id)
                            }
                            ForEach(tailActions) { approvalCell($0) }
                            ForEach(tailRecs) { recCell($0) }
                        } else {
                            let cells = openCells(rows)
                            ForEach(cells) { cell in
                                switch cell {
                                case .message(let m): messageRow(m).id(m.id)
                                case .ask(let a, let cost): AskCard(a: a, goals: goals, cost: cost, reload: { await load() }).id(a.id)
                                case .action(let a, let cost): approvalCell(a, cost: cost)
                                case .rec(let r, let cost): recCell(r, cost: cost)
                                }
                            }
                            if cells.isEmpty, thread.status != "running", cardsLoaded {
                                Text("Nothing open").font(.caption).foregroundStyle(.secondary).padding(.horizontal)
                            }
                        }
                        // THE working line, the console's paintWorking
                        // (views/threads.js); keep them in step: "starting ·
                        // N s" until the first tool call, then "working · "
                        // and the turn's facts. Stop sits beside it on the
                        // phone (it kills the tool chain mid-turn; the
                        // conversation stays resumable); the desktop's is the
                        // head's pill. All of it is inside `status ==
                        // "running"`, so a stopped session shows none of it.
                        if thread.status == "running" {
                            if (thread.turn_tools ?? 0) == 0, let s = startingSince {
                                TimelineView(.periodic(from: .now, by: 1)) { ctx in
                                    workingLine("starting · \(max(0, Int(ctx.date.timeIntervalSince(s)))) s", stop: true)
                                }
                            } else {
                                workingFacts()
                                if chainOpen, chainLoaded, !chainRun.isEmpty { chainView }
                            }
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
                    // Opened on a reply (an ask card's "sent by"): the full
                    // chat, so the row is there to land on.
                    if let f = focusMessage, messages.contains(where: { $0.id == f }) { full = true }
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
                // A shot's posed card (LIFE_SHOT_STEPCARD) is scrolled to
                // the same way, at the foot.
                .onChange(of: focusScroll) { _, target in
                    guard let target else { return }
                    Task { @MainActor in
                        for delay in [0, 120, 400] {
                            try? await Task.sleep(for: .milliseconds(delay))
                            proxy.scrollTo(target.id, anchor: target.anchor)
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
            // The console's `details.queued` strip, between the transcript and
            // the box (threads.js queuedHTML).
            ComingUpStrip(prompts: queuedPrompts, cancel: cancelPrompt)
            Composer(draft: draft, attachments: attachments,
                     placeholder: replyPlaceholder,
                     sending: sending, send: { send() }, allowEmpty: replies.contains { !$0.outcome.isEmpty },
                     banner: replies.isEmpty ? nil : AnyView(macReplyStrip))
            #else
            Divider()
            VStack(spacing: 6) {
                if let error { ErrorBanner(message: error) }
                if let sendError { sendFailedBanner(sendError) }
                ComingUpStrip(prompts: queuedPrompts, cancel: cancelPrompt)
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
        .environment(\.askReply, AskReplyHook({ a, outcome in
            arm(ArmedReply(kind: .ask, id: a.id, title: a.title, outcome: outcome, outcomes: a.outcomes ?? []))
        }, step: { a, s, outcome in
            arm(ArmedReply(kind: .step, id: "\(a.cal_id ?? a.id).\(s.n)", title: s.title, outcome: outcome, outcomes: ArmedReply.stepOutcomes))
        }))
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
            #if targetEnvironment(macCatalyst) || targetEnvironment(simulator)
            poseStepCard()
            #endif
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
        // kicking a refresh from here competed with the pop animation.
        // The board landed after this chat opened (a relaunch draws both at
        // once): its cards, until the chat's own fetch answers.
        .onChange(of: store.board?.bundleOf(thread)) { seedCards() }
        .onDisappear {
            #if !targetEnvironment(macCatalyst)
            store.resume()
            #endif
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
            // changed (messages/asks by ETag).
            if messages.isEmpty {
                let mk = hub.cacheKey(HubClient.messagesPath(thread.id))
                let seeded: [ThreadMessage]? = await Task.detached { HubClient.cachedValue(key: mk) }.value
                if messages.isEmpty, let m = seeded, !m.isEmpty { messages = m }
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

    /// Take a queued wake back from the strip (the hub's `cancel`; a standing
    /// row's Turn off is the schedule going off). The list refills from the
    /// hub rather than being edited here, so the bar never disagrees with it.
    func cancelPrompt(_ p: Prompt) {
        Task {
            do { _ = try await hub.cancelPrompt(p.id); await load() }
            catch { self.error = error.localizedDescription }
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
        // A picture still being read out of Photos goes WITH this message:
        // the spinner shows, the send waits for it, then takes the lot.
        if attachments.loading > 0 { sending = true; Task { await attachments.settle(); send(confirmed: confirmed) }; return }
        let t = draft.text
        let armed = replies
        // Steps ticked with no words are the point of a wordless Send: no
        // "Are you sure?" for them.
        let ticksOnly = !armed.isEmpty && armed.allSatisfy { $0.kind == .step && !$0.outcome.isEmpty }
        if !armed.isEmpty, !confirmed, !ticksOnly, !t.contains(where: { !$0.isWhitespace }), attachments.isEmpty {
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

    /// One message is one row, the console's msgHTML: its label, its first
    /// line, when (and an empty reply's dollars when no card under it carries
    /// them), with the whole of it behind a tap. The owner's rows are grey.
    @ViewBuilder func messageRow(_ m: ThreadMessage) -> some View {
        let text = m.text
        let line = text.split(separator: "\n").first { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.map(String.init) ?? ""
        let when = dayClock(m.ts) + (m.role == "claude" && m.cost_usd > 0 && leadRef(m) == nil ? " · " + usd(m.cost_usd) : "")
        if m.kind == "error" {
            // The CLI's "You've hit your session limit · resets 2:40am" is
            // not a crash: the hub resumes the session then.
            let paused = text.range(of: #"(?i)hit your (?:[a-z0-9 ]{0,24} )?limit[^\n]*?resets\s"#, options: .regularExpression) != nil
            ChatRow(k: paused ? "⏸ Paused" : "⚠ Error", title: paused ? "session limit" : line, when: when, tone: paused ? .orange : .red) { rowBody(m) }
        } else if m.kind == "schedule" {
            // The check-in it set: when it comes back, then the instructions.
            let cut = text.range(of: "\n\n")
            ChatRow(k: "⏰ Check-in", title: cut.map { String(text[..<$0.lowerBound]) } ?? text, when: when, tone: .green) {
                if let cut { StyledText(text: String(text[cut.upperBound...]), style: .subheadline, color: .secondary) }
            }
        } else if m.role == "claude" {
            if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !(m.attachments ?? []).isEmpty {
                ChatRow(k: "Reply", title: line, when: when) { rowBody(m) }
            }
        } else if m.kind == "checkin" {
            ChatRow(k: "⏰ Check-in", title: line, when: when, tone: .green) { rowBody(m) }
        } else if m.role == "system" {
            ChatRow(k: "Note", title: line, when: when) { rowBody(m) }
        } else {
            mineRow(m, line: line, when: when)
        }
    }

    /// The owner's message, or one another session or the hub sent in their
    /// place: the card it answered leads the title ("↩ Got it · …"), whole in
    /// the body.
    func mineRow(_ m: ThreadMessage, line: String, when: String) -> some View {
        let a = m.author.flatMap { $0.isEmpty ? nil : $0 } ?? "owner"
        let k = a == "owner" ? "You" : a == "hub" ? "Hub" : m.author_title.flatMap { $0.isEmpty ? nil : $0 } ?? "Session"
        let re = replyList(m).first { !($0.label ?? $0.outcome ?? "").isEmpty }
        var what = !line.isEmpty ? line : re.map { refTitle($0.ref) } ?? ""
        if what.isEmpty, !(m.attachments ?? []).isEmpty { what = "📎" }
        let title = re.map { "↩ " + ($0.label ?? $0.outcome ?? "") + (what.isEmpty ? "" : " · " + what) } ?? what
        let by = a == "owner" ? nil : sentBy(m)
        return ChatRow(k: k, title: title, when: when, mine: true) {
            ForEach(replyLines(m), id: \.self) { r in
                (Text("↩ ") + Text(r.label).bold() + Text(r.title.isEmpty ? "" : " · " + r.title))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    .padding(.leading, 9)
                    .overlay(alignment: .leading) { Rectangle().fill(Color.primary.opacity(0.2)).frame(width: 2) }
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            rowBody(m)
            if let by {
                HStack(spacing: 8) {
                    if let id = by.thread {
                        Button { Task { if let t = try? await hub.thread(id) { openThread = t } } } label: {
                            Text("sent by \(Text(by.who).underline())").lineLimit(1)
                        }.buttonStyle(.plain)
                    } else {
                        Text("sent by \(by.who)").lineLimit(1)
                    }
                    Spacer(minLength: 0)
                }.font(.caption2).foregroundStyle(.secondary)
            }
        }
        // Hold a message down to ask about it.
        .chatAbout(bubbleSubject(m))
    }

    /// A row's body: its attachments, then its words.
    @ViewBuilder func rowBody(_ m: ThreadMessage) -> some View {
        // By position: the same screenshot can be attached twice.
        let refs = m.attachments ?? []
        ForEach(refs.indices, id: \.self) { i in BlobAttachment(ref: refs[i], maxHeight: 240) }
        if !m.text.isEmpty { StyledText(text: m.text, color: .primary).frame(maxWidth: .infinity, alignment: .leading) }
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
    func replyList(_ m: ThreadMessage) -> [PromptReply] {
        (m.replies?.isEmpty == false) ? m.replies! : m.in_reply_to.map { [PromptReply(ref: $0, outcome: m.outcome)] } ?? []
    }

    func replyLines(_ m: ThreadMessage) -> [ReplyLine] {
        replyList(m).compactMap { r in
            r.ref.contains(":") ? ReplyLine(label: r.label ?? r.outcome ?? "", title: refTitle(r.ref)) : nil
        }
    }

    /// The title of the card a ref names, when the chat holds it ("" otherwise).
    func refTitle(_ ref: String) -> String {
        let parts = ref.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return "" }
        let id = parts[1]
        switch parts[0] {
        case "rec": return mdPlain(recs.first { $0.id == id }?.title ?? "")
        case "ask": return mdPlain(asks.first { $0.id == id }?.title ?? "")
        case "action": return mdPlain(actions.first { $0.id == id }?.title ?? "")
        case "step": return stepTitle(id)
        default: return ""
        }
    }

    /// A `step:<item>.<n>`: that row's own name, off the item's card in this
    /// chat; "step n" when the card isn't here.
    func stepTitle(_ id: String) -> String {
        guard let dot = id.lastIndex(of: "."), let n = Int(id[id.index(after: dot)...]) else { return id }
        let item = String(id[..<dot])
        return asks.first { ($0.cal_id ?? $0.id) == item }?.steps?.first { $0.n == n }?.title ?? "step \(n)"
    }

    /// The proposals this chat draws: all of its own — open ones with their
    /// buttons, dismissed ones folded, decided ones grey with who decided. A
    /// decided card used to leave the chat, so an approved proposal was only
    /// the owner's "↩ Approved" reply; same as the web chat.
    func inChat(_ a: Action) -> Bool { a.thread_id == thread.id }

    /// Draw the session's open cards from the board until this chat's own
    /// fetch lands. The opening paint is the saved messages, which
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
            async let t = hub.thread(thread.id); async let m = hub.threadMessages(thread.id)
            async let k = hub.threadAsks(thread.id)
            // Every state: a dismissed proposal keeps its folded line with
            // Reopen inside (2026-09-18), a decided one its grey card (09-30).
            async let p = hub.actions(state: "", thread: thread.id)
            async let rc = hub.threadRecs(thread.id)
            // The wakes still to come, for the strip over the composer.
            async let qp = hub.prompts(state: "queued", thread: thread.id)
            // The conversation is drawn as soon as the MESSAGES land; the
            // cards fill in after.
            let (freshThread, freshMessages) = try await (t, m)
            // Opened on a card: place the approvals BEFORE the first draw, so
            // the opening scroll (onChange of messages.count) has its target.
            // Every other time the approvals fill in after the messages.
            let early: [Action]? = focusCard != nil && messages.isEmpty ? (try? await p)?.filter(inChat) : nil
            let msgsMoved = freshMessages != messages
            // Only assign what actually changed: a plain reassignment every
            // poll (3s while running) rebuilt the whole message list even
            // when the hub returned exactly what we already had.
            if freshThread != thread { thread = freshThread }
            if msgsMoved { messages = freshMessages }
            if let early { placeActions(early) }
            // Goals name the goal on an ask card and barely ever change —
            // re-fetching them on every 3s poll was a round trip for nothing.
            if goals.isEmpty, let g = try? await hub.goals() { goals = g }
            // The other device's install card is not this app's to draw
            // (the owner 2026-10-01); the board already left it out.
            let gotAsks = (try? await k)?.filter { !$0.isOtherDeviceInstall }
            if let gotAsks, gotAsks != asks || (msgsMoved && !gotAsks.isEmpty) { placeAsks(gotAsks) }
            // A fetch that failed keeps the cards already drawn (asks always
            // did): an approval must not leave the chat because one request
            // timed out — `?? []` read a failure as "no proposals".
            let mine: [Action]?
            if let early { mine = early } else { mine = (try? await p)?.filter(inChat) }
            if let mine, mine != actions || (msgsMoved && !mine.isEmpty) { placeActions(mine) }
            if gotAsks != nil, mine != nil { cardsLoaded = true }
            // A rec moves under its reply when that lands, and changes
            // without a message when the owner decides it from the Recs page.
            if let rc = try? await rc, rc != recs || (msgsMoved && !rc.isEmpty) { placeRecs(rc) }
            // Soonest first: the hub lists prompts newest-CREATED first, and
            // the folded bar names the next one, so its order is the list's.
            // A failed fetch keeps the strip as it was.
            if let qp = try? await qp {
                let soon = qp.filter(\.pending).sorted { ($0.not_before ?? .distantPast) < ($1.not_before ?? .distantPast) }
                if soon != queuedPrompts { queuedPrompts = soon }
            }
            if let f = focusCard, !focusLanded, asks.contains(where: { $0.id == f }) || actions.contains(where: { $0.id == f }) {
                // A deep link to a closed card opens the full chat, so the
                // card is there to land on; the console's focusNeedsFull.
                if asks.contains(where: { $0.id == f && $0.isClosed }) || actions.contains(where: { $0.id == f && $0.open != true }) { full = true }
                focusLanded = true
            }
            await loadChain()
            error = nil
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
    /// The title alone on the left, and at the right everything about the
    /// session in one run: its pills, the model, schedule · $ · tokens.
    /// ← only on a chat pushed over another page; beside the Sessions list
    /// the list is the way back.
    var macHead: some View {
        HStack(spacing: 8) {
            if pushed {
                Button { dismiss() } label: {
                    Text("←").font(.system(size: 13)).foregroundStyle(Web.muted)
                        .frame(width: 30, height: 30)
                        .background(Web.panel, in: RoundedRectangle(cornerRadius: 8))
                        .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) }
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Back")
            }
            Text(md(thread.title)).font(.system(size: 15, weight: .semibold)).tracking(-0.18)
                .lineLimit(1).truncationMode(.tail)
                .padding(.leading, pushed ? 4 : 0)
                .layoutPriority(1)
            Spacer(minLength: 12)
            HStack(spacing: 6) {
                HStack(spacing: 4) {
                    ForEach(Array(headPills.enumerated()), id: \.offset) { _, p in
                        HeadPill(pill: p, act: headAct(p))
                    }
                }
                if let m = thread.modelShort {
                    Text(m).font(.system(size: 11.5, weight: .semibold)).foregroundStyle(Web.muted)
                        .padding(.horizontal, 7).frame(height: 20)
                        .background(Web.code, in: RoundedRectangle(cornerRadius: 6))
                }
                if !headMeta.isEmpty {
                    Text(headMeta).font(.system(size: 12.5)).monospacedDigit().foregroundStyle(Web.muted)
                        .lineLimit(1)
                        .padding(.leading, 2).padding(.trailing, 4)
                }
            }
            .fixedSize()
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
#endif

/// One "↩ <verdict> · <card>" line of the owner's message: the console's
/// replyLineHTML.
struct ReplyLine: Hashable { let label: String; let title: String }

#if targetEnvironment(macCatalyst)

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
                // A run is drawn once: one wake is its row, no fold; several
                // fold, and open, the label is the count alone.
                if queued.count == 1, let p = queued.first {
                    Section("Coming up") { queuedRow(p) }
                } else if !queued.isEmpty {
                    Section {
                        DisclosureGroup(isExpanded: $queuedOpen) {
                            ForEach(queued) { p in queuedRow(p) }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text("Coming up · \(queued.count)").font(.subheadline.weight(.semibold))
                                    Spacer(minLength: 0)
                                    if !queuedOpen, let n = queued.first {
                                        Text("next " + n.whenLabel).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                                    }
                                }
                                // The name only if it fits on its own line —
                                // the time is what the bar owes the owner.
                                if !queuedOpen, let n = queued.first {
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

    /// One queued wake: who queued it, its cadence (standing), when, Cancel,
    /// and its words underneath.
    private func queuedRow(_ p: Prompt) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Image(systemName: p.standing ? "clock.arrow.circlepath" : p.author == "owner" ? "person.crop.circle" : "arrow.uturn.left.circle").font(.caption2)
                // A standing row is the check-in itself: its cadence in the
                // same words as the session card, and Turn off, because
                // cancelling it IS the schedule going off.
                if p.standing { Chip(scheduleLabel(p.repeat ?? ""), icon: "repeat") }
                Text((p.standing ? "next " : "") + p.whenLabel).font(.caption.weight(.semibold))
                // Only a bare "new" target starts a session. "new-or:<this
                // thread>" RESUMES this one, so it is not labelled "new session".
                if p.target == "new" { Chip("new session", icon: "plus.bubble") }
                Spacer(minLength: 0)
                Button(p.standing ? "Turn off" : "Cancel") {
                    Task {
                        do { _ = try await hub.cancelPrompt(p.id); await loadPrompts() }
                        catch { self.error = error.localizedDescription }
                    }
                }.font(.caption).buttonStyle(.borderless)
            }.foregroundStyle(.secondary)
            // Title first, text only when there is none. A calendar wake's
            // text is its title + "\n\n" + the whole instruction, so showing
            // text printed the title twice and then a paragraph of prose.
            Text(p.rowLabel).font(.subheadline).lineLimit(2)
        }.padding(.vertical, 2)
    }

    /// Quiet on failure (`prompts.error` is never drawn): an older hub has no
    /// /prompts, and that is not worth an error banner over the settings the
    /// owner actually came here for.
    func loadPrompts() async {
        await prompts.run { try await hub.prompts(state: "queued", thread: thread.id) }
    }
}

/// How many wakes an open "Coming up" strip draws: the next five, or every
/// one once the owner asked for the rest. Below five there is nothing to hide.
let comingUpWindow = 5
func comingUpShown(_ count: Int, all: Bool) -> Int { all ? count : min(count, comingUpWindow) }
/// The row under the five: "19 more", the console's words.
func comingUpMoreLabel(_ hidden: Int) -> String { "\(hidden) more" }

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

/// "Coming up · 2 · next in 28m · <its words>" over the composer: this
/// session's queued wakes (a message the owner queued for the morning, the
/// check-back the session set itself), the console's `details.queued` strip
/// (threads.js queuedHTML). Without it a session that said "retrying in half
/// an hour" left no trace of that on the chat bar; only Session settings
/// listed it. Folded at rest and remembered under the console's key
/// (queuedOpen); shut it is the count, when the next one fires and its
/// words; open, every wake with Cancel (Turn off on a standing row).
/// Nothing queued = no strip. Open, the next five and an "N more" row that
/// shows the rest, this visit only; the console's strip folds the same way.
struct ComingUpStrip: View {
    let prompts: [Prompt]
    let cancel: (Prompt) -> Void
    @AppStorage("queuedOpen") private var open = false
    @State private var all = false

    var body: some View {
        if let next = prompts.first {
            VStack(alignment: .leading, spacing: 0) {
                if prompts.count == 1 {
                    HStack(spacing: 8) {
                        Text("Coming up").font(small.weight(.semibold)).foregroundStyle(.secondary).fixedSize()
                        row(next)
                    }
                    .accessibilityLabel("Coming up, \(next.whenLabel), \(next.rowLabel)")
                } else {
                    Button { open.toggle() } label: {
                        HStack(spacing: 8) {
                            Text(open ? "▾" : "▸").font(.system(size: 10)).foregroundStyle(.secondary)
                            Text("Coming up · \(prompts.count)").font(small.weight(.semibold)).foregroundStyle(.secondary)
                                .fixedSize()
                            if !open {
                                pill("next " + next.whenLabel)
                                Text(next.rowLabel).font(small).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                            }
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                        // The words stay with the bar. The button style
                        // animates its press release and the label's other
                        // animatable attributes inherit it, so the rows
                        // snapped open or shut while the count, chevron and
                        // next-wake pill slid into place half a second later.
                        .transaction { $0.animation = nil }
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Coming up, \(prompts.count), next \(next.whenLabel)")
                    if open {
                        let shown = comingUpShown(prompts.count, all: all)
                        ForEach(prompts.prefix(shown)) { p in row(p).padding(.top, 6) }
                        if shown < prompts.count {
                            Button(comingUpMoreLabel(prompts.count - shown)) { all = true }
                                .font(small).buttonStyle(.borderless).padding(.top, 6)
                                .accessibilityLabel("Show the \(prompts.count - shown) later wakes")
                        }
                    }
                }
            }
            #if targetEnvironment(macCatalyst)
            .padding(.horizontal, 16).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Web.panel)
            .overlay(alignment: .top) { Rectangle().fill(Web.line).frame(height: 1) }
            #else
            .padding(.horizontal, 4).padding(.vertical, 2)
            #endif
        }
    }

    /// One wake: cadence (standing), when, its words, who queued it, Cancel.
    /// On a narrow screen the words give way first - whole if they fit,
    /// truncated at a readable width if that fits, gone otherwise - and the
    /// button never wraps.
    private func row(_ p: Prompt) -> some View {
        ViewThatFits(in: .horizontal) {
            rowContent(p, words: .whole)
            rowContent(p, words: .clipped)
            rowContent(p, words: .none)
        }
    }

    private enum Words { case whole, clipped, none }

    private func rowContent(_ p: Prompt, words: Words) -> some View {
        HStack(spacing: 8) {
            if p.standing { pill(scheduleLabel(p.repeat ?? "")) }
            pill((p.standing ? "next " : "") + p.whenLabel)
            if p.target == "new" { pill("new session") }
            if words != .none {
                (Text(p.rowLabel) + Text(" · from " + from(p)).foregroundStyle(.secondary))
                    .font(small).lineLimit(1).truncationMode(.tail)
                    .frame(maxWidth: words == .clipped ? 140 : nil, alignment: .leading)
            }
            Spacer(minLength: 0)
            Button(p.standing ? "Turn off" : "Cancel") { cancel(p) }
                .font(small).buttonStyle(.borderless).lineLimit(1).fixedSize()
        }
    }

    private var small: Font {
        #if targetEnvironment(macCatalyst)
        .system(size: 12.5)
        #else
        .caption
        #endif
    }

    /// The console's grey `.pill`.
    private func pill(_ s: String) -> some View {
        Text(s).font(.system(size: 12, weight: .medium)).foregroundStyle(.secondary).monospacedDigit()
            .padding(.horizontal, 7).padding(.vertical, 2)
            .background(Color.primary.opacity(0.07), in: RoundedRectangle(cornerRadius: 5))
            .fixedSize()
    }

    private func from(_ p: Prompt) -> String {
        p.author == "owner" ? "you" : p.author == "hub" ? "the hub" : "this session"
    }
}

/// A card to scroll the chat to, and where in the view it lands.
struct FocusScroll: Equatable {
    let id: String
    let anchor: UnitPoint
}

#if targetEnvironment(macCatalyst) || targetEnvironment(simulator)
/// The posed card's own words (LIFE_SHOT_STEPCARD): a step item's.
@MainActor enum StepCardShot {
    /// Kept through every reload of the chat while the shot is taken.
    static var posed: Ask?
    static let outcomes = [AskOutcome(value: "done", label: "Done"), AskOutcome(value: "wont", label: "Won't do"), AskOutcome(value: "", label: "Reply")]
}
#endif

/// One card armed on the chat bar (several can be at once). `ref`
/// is what the hub takes — "ask:<id>" / "rec:<id>" / "action:<id>" — and
/// `outcome` the pick: a read's "done", a rec's accepted/declined, a
/// proposal's approved/denied, or "" for words alone.
struct ArmedReply: Identifiable, Hashable {
    /// `step`: one step of an item's card, "step:<item>.<n>".
    enum Kind { case ask, rec, action, step }
    let kind: Kind
    /// A step's chips, the row's own words.
    static let stepOutcomes = [AskOutcome(value: "done", label: "Did it"), AskOutcome(value: "wont", label: "Skip"), AskOutcome(value: "", label: "Reply")]
    let id: String
    let title: String
    var outcome: String
    /// The card's own answers as the hub sent them — where the banner's word
    /// comes from.
    var outcomes: [AskOutcome] = []
    var ref: String {
        switch kind { case .ask: "ask:" + id; case .rec: "rec:" + id; case .action: "action:" + id; case .step: "step:" + id }
    }
    /// The banner's word for the pick: the hub's label for it — the word on
    /// the button tapped (I did this · Won't do · Decided · Accepted…).
    /// Words alone on an ask read "Replying to" (the owner 2026-10-02: "make it so
    /// that the physical text in the chat bar says done or wont do instead
    /// of replying to").
    var label: String {
        if let l = outcomes.first(where: { $0.value == outcome && !$0.value.isEmpty })?.label { return l }
        return kind == .ask || kind == .step ? "Replying to" : "Reply"
    }
    /// The banner's mark: the card's own icon and colour.
    var icon: String {
        switch kind { case .ask, .step: "arrowshape.turn.up.left.fill"; case .rec: "lightbulb.fill"; case .action: "hand.raised.fill" }
    }
    var tint: Color {
        switch kind { case .ask, .step: Color.accentColor; case .rec: .purple; case .action: .red }
    }
}
