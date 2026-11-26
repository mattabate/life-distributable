// Session list rows: ThreadRow (one session, its open cards as cells),
// SessionCell/MiniCard (the console's `.ask.mini`), CostLabel and the status
// colour they share. Moved verbatim out of ThreadsView.swift (Phase 3,
// 2026-08-26); ScheduledRow was deleted 2026-08-29 with the "Checking in
// later" section it drew, AllThreadsView (every session as a searchable
// table) on 2026-10-24. The console's 2026-09-20 list — whole title, one
// tinted cell per open card, pills in the foot — was ported here on
// 2026-09-23 (the owner: "Let's have the phone's layout / UI approximately
// mirror the web UI"), rule for rule out of views/threads.js.
import SwiftUI

// (AllThreadsView — every session that is not a person as a searchable
// table, "All sessions" 2026-09-20 to 10-24 and "Legacy" for one evening —
// went 2026-10-24 00:42: the owner, "all sessions is no longer native… you
// don't need any button for getting back to those sessions". The sessions
// stay in the hub, `GET /threads`.)

/// A row's status on the Sessions list, one list with no headings: the
/// console's `statusOf`. The highest a person has, and the rows run in this
/// order: install, your turn, to read, working, idle with only recs, idle
/// with to-dos. nil = a plain idle pair at the foot. An install card makes
/// the row Install; any other card of the owner's but a read is Your turn as
/// the person's own status, ahead of a running turn, though the row then
/// wears Working.
/// Speaking and waiting are pills only: the row keeps its own status's place
/// and edge colour, so a card never jumps up the list while it is read
/// aloud. An install card is the top stack and Working never covers it.
enum SideStatus: Int, Comparable {
    case speak, wait, install, turn, read, work, recs, todo
    static func < (a: Self, b: Self) -> Bool { a.rawValue < b.rawValue }

    /// The pill, one per person, never stacked: the card being said, else a
    /// running turn wears Working over anything but an install (the
    /// console's `pillOf`).
    @MainActor static func of(_ t: Thread, board b: Board, recs: Int) -> SideStatus? {
        guard let s = base(t, board: b, recs: recs) else { return nil }
        if let a = audio(t, board: b) { return a }
        return s != .install && t.status == "running" ? .work : s
    }

    /// Speaking or waiting to speak (the console's `audioOf`).
    @MainActor static func audio(_ t: Thread, board b: Board) -> SideStatus? {
        let ps = b.pills?[t.id] ?? []
        if ps.contains(where: { $0.tone == "speaking" }) { return .speak }
        if ps.contains(where: { $0.tone == "waiting" }) { return .wait }
        return nil
    }

    /// The person's own status: it picks which of their sessions is the
    /// row and where the row sorts, and `of` turns it into the pill. The
    /// cards are the person's own and every sub-session's the board's `home`
    /// files under them, so a sub-session's decision makes the row Your turn
    /// even where the hub's rollup says Working.
    @MainActor static func base(_ t: Thread, board b: Board, recs: Int) -> SideStatus? {
        let ids = [t.id] + (b.home ?? [:]).filter { $0.value == t.id }.map(\.key)
        let cs = ids.flatMap { id in b.sessions?.first { $0.id == id }.map { SessionCell.cells($0, board: b) } ?? [] }
        let asks = ids.reduce(0) { $0 + (b.forYou[$1] ?? 0) }
        let kinds = cs.map { $0.tone == "speaking" ? $0.under ?? "" : $0.tone }
        if kinds.contains("install") { return .install }
        if kinds.contains(where: { $0 != "read" }) || (asks > 0 && cs.isEmpty) { return .turn }
        if !cs.isEmpty { return .read }
        if ids.contains(where: { b.section?[$0] == "your_turn" }) { return .turn }
        if b.sectionOf(t) == "working" || t.status == "running" { return .work }
        return !ThreadRow.todos(t).isEmpty ? .todo : recs > 0 ? .recs : nil
    }

    var word: String {
        switch self {
        case .speak: "Speaking"
        case .wait: "Waiting"
        case .install: "Install"
        case .turn: "Your turn"
        case .read: "To read"
        case .work: "Working"
        case .todo, .recs: "Idle"
        }
    }
    /// The side line and the pill's colour.
    var color: Color {
        #if targetEnvironment(macCatalyst)
        switch self {
        case .turn: Web.red
        case .speak: Web.green
        case .wait: Web.amber
        case .install: Web.install
        case .read: Web.read
        case .work: Web.accent
        case .todo, .recs: Web.lineStrong
        }
        #else
        switch self {
        case .turn: .red
        case .speak: .green
        case .wait: .orange
        case .install: .teal
        case .read, .work: .accentColor
        case .todo, .recs: .secondary
        }
        #endif
    }
    /// The idle pill's ink: grey, never the side line's lighter grey.
    var ink: Color {
        guard self == .todo || self == .recs else { return color }
        #if targetEnvironment(macCatalyst)
        return Web.muted
        #else
        return .secondary
        #endif
    }
}

/// The status pill on the right of a row: the live audio one (speaking,
/// waiting), else the status's word in its colour. Every pill is one width,
/// the word centred, and the queued line says "Waiting", not the hub's
/// "Waiting to speak", which would make that width too wide.
struct SideStatusPill: View {
    static let width: CGFloat = 76
    let status: SideStatus
    var pills: [Pill] = []
    private var audio: [Pill] { pills.map { $0.tone == "waiting" ? Pill(word: SideStatus.wait.word, tone: $0.tone) : $0 } }
    var body: some View {
        switch status {
        case .speak, .wait:
            if let p = audio.first(where: { $0.tone == "speaking" }) ?? audio.first(where: { $0.tone == "waiting" }) {
                PillChip(pill: p, width: Self.width).fixedSize()
            }
        default:
            Chip(status.word, lead: nil, tint: status.ink, fill: 0.14).wide(Self.width).fixedSize()
        }
    }
}

/// One open card on a session's row, worded the console's way (`sessionCards`
/// + `miniCardHTML`, views/threads.js): what it wants from him in one word
/// (`verb`), its whole title, and the tint
/// its class wears. Built from the board's bundle so the phone and the console
/// draw the same cells in the same order.
struct SessionCell: Identifiable, Hashable {
    let id: String
    let title: String
    // No words under the title (the owner 2026-10-10 15:14, of the cards
    // without their descriptions: "I'm totally cool to move to the new
    // system"): the title says what it wants, the chat holds the rest.
    let verb: String
    /// `needs` (red) · `read` (blue) · `install` (teal) · `error` (grey).
    let tone: String
    /// One of his own dated steps that sits in this chat — uncounted.
    let step: Bool
    /// A sub-session's row (`quiet` when it has no card of its own): the
    /// session it opens, the card to land on, and whether it is running.
    var sub: Thread? = nil
    var subFirst: String? = nil
    /// A job running for the session (a hub-run action or the turn's helper):
    /// a grey "running" cell with its bar — the console's `jobCells`.
    var job: TurnTask? = nil
    /// The card being spoken keeps its own kind here (read, needs, install):
    /// the desktop sidebar draws its speaker in that colour, never green.
    var under: String? = nil

    /// The session's running jobs as cells, ahead of its cards (the owner
    /// 2026-11-05 00:43, mock B: "have a running be the highest priority…
    /// maybe it should be grey").
    static func jobs(_ t: Thread) -> [SessionCell] {
        t.liveTasks.map { k in
            SessionCell(id: "job:" + (k.id ?? k.description ?? ""), title: k.description ?? "helper",
                        verb: "running", tone: "quiet", step: false, job: k)
        }
    }

    var color: Color {
        switch tone { case "read": .accentColor; case "install": .teal; case "speaking": .green; case "rec": .purple; case "error", "quiet": .secondary; default: .red }
    }
    var mark: String {
        if tone == "quiet" { return sub?.status == "running" ? "circle.fill" : "circle" }
        return switch tone { case "read": "text.alignleft"; case "install": "square.and.arrow.down"; case "speaking": "circle.fill"; case "rec": "lightbulb.fill"; case "error": "exclamationmark.triangle"; default: "hand.raised.fill" }
    }

    /// A session another one spun off, as a row on that one's card (the owner
    /// 2026-10-23 01:34: "the parent and then 2 of these… collapsing after 5
    /// or 3"): its title, its first open card's tint and verb if it has one,
    /// else gray and "running"/"idle" — the console's `subCell`.
    static func sub(_ k: Thread, board b: Board) -> SessionCell {
        let c = b.bundleOf(k).map { cells($0, board: b) }?.first
        return SessionCell(id: "sub " + k.id, title: k.title,
                           verb: c?.verb ?? (k.status == "running" ? "running" : "idle"),
                           tone: c?.tone ?? "quiet", step: false,
                           sub: k, subFirst: b.first[k.id].flatMap { $0.isEmpty ? nil : $0 }, under: c?.under)
    }

    /// The bundle as cells: approvals first (the session is stopped on them),
    /// then its asks, then his dated steps — the board's order, untouched.
    /// The card in his ears reads "speaking" in green, a sub-session's row
    /// included (the owner 2026-10-25 01:56, of a sub-session's READ cell
    /// while its card spoke: "this spot should say speaking
    /// also"): the card the hub marks, else — a replay holds the session's
    /// floor, not the card's — the first card with a line.
    static func cells(_ s: BoardSession, board b: Board = .empty) -> [SessionCell] {
        let acts = s.actions.map { a in
            SessionCell(id: a.id, title: a.title, verb: "approve", tone: "needs", step: false)
        }
        let floor = b.pills?[s.id]?.contains { $0.tone == "speaking" } == true
        let voiced = s.asks.first { $0.speaking == true }?.id
            ?? (floor ? s.asks.first { !($0.said ?? "").isEmpty }?.id : nil)
        let asks = (s.asks + (s.steps ?? [])).enumerated().map { i, a in
            let k = a.kind
            let tone = k == "read" ? "read" : k == "install" ? "install" : k == "error" ? "error" : "needs"
            if a.id == voiced { return SessionCell(id: a.id, title: a.title, verb: "speaking", tone: "speaking", step: false, under: tone) }
            return SessionCell(id: a.id, title: a.title,
                               verb: a.verb ?? "for you",  // the hub's word (store.AskVerb)
                               tone: tone,
                               step: i >= s.asks.count)
        }
        return acts + asks
    }

    /// Markdown links to their text, then every mark that would make two
    /// spellings of one sentence differ — the console's `plain`.
    static func plain(_ s: String) -> String {
        var out = s.replacingOccurrences(of: #"\[([^\]]*)\]\([^)]*\)"#, with: "$1", options: .regularExpression)
        out.removeAll { "*_`#…".contains($0) }
        return out.trimmingCharacters(in: .whitespaces)
    }
}

/// One cell: the chat's card, small — the console's `.ask.mini`. The corner
/// verb says what it wants; the title is whole. A tap lands the chat on this
/// very card.
struct MiniCard: View {
    let c: SessionCell
    let tap: () -> Void
    var body: some View {
        Button(action: tap) {
            VStack(alignment: .leading, spacing: 2) {
                // One line a card; the chat holds the whole title.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: c.mark).font(.caption2.weight(.semibold))
                        .foregroundStyle(c.tone == "quiet" && c.sub?.status == "running" ? Color.accentColor : c.color)
                    Text(md(c.title)).font(.subheadline.weight(.medium)).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(c.verb).font(.caption2).foregroundStyle(c.color).layoutPriority(1)
                }
                if let k = c.job { JobProgress(k: k).padding(.leading, 18) }
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(c.color.opacity(c.tone == "error" || c.tone == "quiet" ? 0.08 : 0.1), in: RoundedRectangle(cornerRadius: 8))
            .overlay(alignment: .leading) { RoundedRectangle(cornerRadius: 1.5).fill(c.color.opacity(c.tone == "error" || c.tone == "quiet" ? 0.4 : 0.9)).frame(width: 3).padding(.vertical, 4).padding(.leading, 1) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// One to-do under a name on the Sessions list (the console's
/// sideTodosHTML): a hollow grey dot and the task, a waiting one grey and
/// dotted with what it waits on.
struct TodoLine: View {
    let d: Todo
    var body: some View {
        let mid = mid
        return HStack(alignment: .firstTextBaseline, spacing: 0) {
            Circle().strokeBorder(muted, style: StrokeStyle(lineWidth: 1.2, dash: d.waiting ? [1.5, 1.5] : []))
                .frame(width: 7, height: 7)
                .frame(width: lead, alignment: .leading)
                .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + mid }
            Text(md(d.text + (d.waiting ? " · " + ((d.note ?? "").isEmpty ? "waiting" : d.note ?? "") : "")))
                .font(font).foregroundStyle(d.waiting ? muted : fg).multilineTextAlignment(.leading).lineLimit(2)
            Spacer(minLength: 0)
        }
        .padding(.top, top)
    }
    #if targetEnvironment(macCatalyst)
    private var font: Font { .zoomed(size: 13.5) }
    private var mid: CGFloat { UIFont.systemFont(ofSize: 13.5 * Zoom.scale).capHeight / 2 }
    private let lead: CGFloat = 13
    private let top: CGFloat = 6
    private var muted: Color { Web.muted }
    private var fg: Color { Web.fg }
    #else
    private var muted: Color { .secondary }
    private var fg: Color { .primary }
    private var font: Font { .subheadline }
    private var mid: CGFloat { UIFont.preferredFont(forTextStyle: .subheadline).capHeight / 2 }
    private let lead: CGFloat = 15
    private let top: CGFloat = 0
    #endif
}

#if targetEnvironment(macCatalyst)
/// A session's state line, the console's `.sess-foot`: its pills, the model
/// as the grey `WebTag` (the one small pill the desktop has — `WebPill`, a
/// second grey style for the same word, folded into it 2026-10-11,
/// rec-104dfb6b), then "ago · schedule · dollars". The session card and the
/// All sessions row both draw this, never a rendering of their own.
struct WebSessionState: View {
    let pills: [Pill]
    let t: Thread
    var body: some View {
        HStack(spacing: 5) {
            ForEach(pills, id: \.self) { PillChip(pill: $0) }
            if let m = t.modelShort { WebTag(m) }
            Text(sessionFootWords(t)).font(.zoomed(size: 12)).foregroundStyle(.secondary).monospacedDigit().lineLimit(1)
        }
    }
}

/// "20h ago · daily 08:00 · $47.92" — the console's foot span.
func sessionFootWords(_ t: Thread) -> String {
    var s = t.last_message_at.map { shortAgo($0) } ?? ""
    if !t.schedule.isEmpty { s += " · " + scheduleChipLabel(t) }
    if t.cost_usd > 0 { s += " · " + usd(t.cost_usd) }
    return s
}

/// One open card under a name on the desktop sidebar (the owner 2026-11-06,
/// sidebar 3): a coloured dot and its whole title — red do, blue read, teal
/// install, a green speaker while spoken, grey a quiet sub-session ("dots… a little
/// teeny bit smaller": 8pt). A running job is its bar and what it is doing.
/// A click lands the chat on that card.
struct SideItem: View {
    let c: SessionCell
    let tap: () -> Void
    /// The card's kind colour; the card being spoken is green, like its
    /// row's edge and pill, whatever its kind.
    var dot: Color {
        switch c.tone {
        case "speaking": Web.green
        case "read": Web.read
        case "install": Web.install
        case "rec": Web.violet
        case "error": Web.muted
        case "quiet": Web.muted
        default: Web.red
        }
    }
    /// The width the marker sits in, so every title starts at one x.
    static let lead: CGFloat = 13
    var body: some View {
        Button(action: tap) {
            Group {
                // A background process: a grey dot and its name, and a bar
                // only off its real count (the owner 2026-11-06 02:12: "grey
                // dots… background processes and subagents").
                if let k = c.job {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(alignment: .firstTextBaseline, spacing: 0) {
                            marker(Circle().fill(Web.muted).frame(width: 6, height: 6))
                            Text(md(c.title)).font(.zoomed(size: 13.5)).foregroundStyle(Web.muted).lineLimit(1)
                        }
                        if k.total ?? 0 > 0 { JobProgress(k: k).padding(.leading, Self.lead) }
                    }
                } else {
                    // 13.5 under a 15 name, a 6pt dot (the owner 2026-11-06
                    // 02:01: "the text should be smaller here… this dot is
                    // like too big"). The card being spoken is a small
                    // green speaker, the dot's size.
                    HStack(alignment: .firstTextBaseline, spacing: 0) {
                        if c.tone == "speaking" {
                            marker(Image(systemName: "speaker.wave.2.fill").font(.system(size: 8 * Zoom.scale, weight: .semibold))
                                .foregroundStyle(dot))
                        } else {
                            marker(Circle().fill(dot).frame(width: 6, height: 6))
                        }
                        Text(md(c.title)).font(.zoomed(size: 13.5)).foregroundStyle(Web.fg).multilineTextAlignment(.leading).lineLimit(2)
                        Spacer(minLength: 0)
                    }
                }
            }
            .padding(.top, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
    /// The dot (or speaker) centred on the middle of the first line's
    /// capitals, not sat on its baseline.
    private func marker(_ v: some View) -> some View {
        let mid = UIFont.systemFont(ofSize: 13.5 * Zoom.scale).capHeight / 2
        return v.frame(width: Self.lead, alignment: .leading)
            .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + mid }
    }
}

#endif

/// One session on the list — the console's `sessionCardHTML`: its whole
/// title, then its open cards as cells (up to `rowCards`, then "and N more"),
/// what it is doing if it runs, and a foot of pills · model · schedule · cost
/// · when. Every field comes from the one board poll, so a row says the same
/// thing on Sessions and in the console.
struct ThreadRow: View {
    @Environment(RefNav.self) private var nav
    let t: Thread
    /// Open asks/approvals this session carries (board `for_you`).
    var forYou: Int = 0
    /// The capsules the hub worded for this card ("running", "1 to read",
    /// "your turn"…) — the console draws the same ones (2026-09-14). Every
    /// card drawn as a cell = the count pills would say it twice, so then only
    /// a RUNNING pill stays (the owner 2026-08-26), in the foot beside the model.
    var pills: [Pill] = []
    /// The cells: one per open card, the board's bundle in its order.
    var cells: [SessionCell] = []
    /// Open cards beyond the ones drawn (board `open`): "and N more".
    var more: Int = 0
    /// Every open card (board `open`, at least the cells drawn).
    var openCount: Int = 0
    /// An older hub counted a card it did not bundle: its title (board
    /// `todo`) and words (board `detail`) draw where the cells would.
    var todo: String? = nil
    var detail: String? = nil
    /// The card the chat opens on (board `first`), like the console's row.
    var first: String? = nil
    /// How many of a session's open cards its row draws (console ROW_CARDS).
    static let rowCards = 2
    /// The cells drawn folded: the first `rowCards`, and the card in his ears
    /// past them — never under "and N more" (the owner 2026-10-28 20:01, a
    /// card speaking out of sight; the console's rowCards).
    static func folded(_ cells: [SessionCell]) -> [SessionCell] {
        let head = Array(cells.prefix(rowCards))
        let tail = cells.dropFirst(rowCards)
        if let v = tail.first(where: { $0.tone == "speaking" }) { return head + [v] }
        return head
    }

    /// The desktop: this session is the one open on the right (a blue edge).
    var on = false
    /// The sessions this one spun off, a row each under its own cards.
    var subs: [SessionCell] = []
    /// How many of them the card draws before "and N more" (console SUB_CELLS).
    static let subRows = 3
    @State private var subsOpen = false
    @State private var expanded = false
    /// The session's to-do list, whole under the cards and never folded.
    var todos: [Todo] { Self.todos(t) }
    static func todos(_ t: Thread) -> [Todo] {
        TodoShot.posed && t.id == ProcessInfo.processInfo.environment["LIFE_THREAD"] ? TodoShot.sample : t.todos ?? []
    }

    /// The row's status (its pill and side line) and the person's open recs,
    /// the lowest lines under the name.
    var status: SideStatus = .todo
    var edge: SideStatus = .todo
    var recs: [Rec] = []
    var recCells: [SessionCell] {
        recs.map { SessionCell(id: "rec:" + $0.id, title: $0.title, verb: "rec", tone: "rec", step: false) }
    }

    init(t: Thread, board b: Board, on: Bool = false, subs: [Thread] = [], status: SideStatus? = nil, recs: [Rec] = []) {
        self.t = t
        self.on = on
        self.recs = recs
        self.status = status ?? SideStatus.of(t, board: b, recs: recs.count) ?? .todo
        // A speaking or waiting row keeps its own status's edge; only the pill
        // turns green or orange.
        edge = [.speak, .wait].contains(self.status) ? SideStatus.base(t, board: b, recs: recs.count) ?? self.status : self.status
        self.subs = subs.map { SessionCell.sub($0, board: b) }
        forYou = b.forYou[t.id] ?? 0
        let cards = b.bundleOf(t).map { SessionCell.cells($0, board: b) } ?? []
        // Its running jobs lead the row, inside the same two (console card()).
        let jobs = SessionCell.jobs(t)
        cells = jobs + cards
        let open = b.open?[t.id] ?? 0
        openCount = max(open + jobs.count, cells.count)
        more = max(0, openCount - Self.folded(cells).count)
        if forYou > 0, cards.isEmpty { todo = b.todo?[t.id]; detail = b.detail?[t.id] }
        let steps = cards.filter(\.step).count
        // …and "speaking" stays too: the one live fact a cell cannot carry
        // (which session's card is in his ears right now, or queued for it,
        // 2026-09-25).
        pills = forYou > 0 && cards.count - steps >= forYou ? b.pillsOf(t).filter { ["running", "speaking", "waiting"].contains($0.tone) } : b.pillsOf(t)
        first = b.first[t.id].flatMap { $0.isEmpty ? nil : $0 }
    }

    /// The todo line's colour is the first card pill's (fallback only).
    var tone: Color { PillChip.tone(pills) }

    func open(_ card: String?) { nav.card = OpenAsk(thread: t, message: nil, card: card.flatMap { $0.hasPrefix("job:") ? nil : $0 }) }
    func open(sub c: SessionCell) { if let k = c.sub { nav.card = OpenAsk(thread: k, message: nil, card: c.subFirst) } }
    /// The first card the folded row did not draw.
    var firstHidden: String? {
        let shown = Set(Self.folded(cells).map(\.id))
        return cells.first { !shown.contains($0.id) }?.id ?? cells.last?.id
    }
    var subsShown: ArraySlice<SessionCell> { subs.prefix(subsOpen ? subs.count : Self.subRows) }

    var body: some View {
        #if targetEnvironment(macCatalyst)
        macCard
        #else
        phoneRow
        #endif
    }

    #if targetEnvironment(macCatalyst)
    /// No model, dollars or tool-call row — the chat's head carries those
    /// (rec-e4b035dd, 2026-10-17). Only the live audio pills stay (speaking,
    /// waiting): the one fact a dot cannot carry.
    /// THE SIDEBAR ROW (the owner 2026-11-06 01:51, sidebar 3 of the redesign
    /// mocks: "love number 3… just the sidebar"): edge to edge, a line under
    /// each person, the name with the job on the right, then EVERY open card
    /// as a coloured dot and its title (red do, blue read, teal install — "do
    /// and read words too much", no accordion), and a bar with what it is
    /// doing while it runs. The open person is a blue edge and a blue tint
    /// (no tan, no ring, no rounded corners).
    static let sideRows = 2

    /// Every line under a name, in order: his to-do, the subagents as one
    /// grey count (02:12: "multiple subagents… the names are stupid"), then
    /// each card and background process, then its sub-sessions.
    private var sideItems: [(SessionCell, () -> Void)] {
        var out: [(SessionCell, () -> Void)] = []
        if let todo, !todo.isEmpty, cells.isEmpty {
            out.append((SessionCell(id: "todo", title: todo, verb: "", tone: "needs", step: false), { open(first) }))
        }
        let agents = cells.filter { $0.job?.type == "local_agent" }
        if !agents.isEmpty {
            out.append((SessionCell(id: "agents", title: agents.count == 1 ? "1 subagent" : "\(agents.count) subagents",
                                    verb: "", tone: "quiet", step: false), { open(nil) }))
        }
        out += cells.filter { $0.job?.type != "local_agent" }.map { c in (c, { open(c.id) }) }
        out += subs.map { c in (c, { open(sub: c) }) }
        return out
    }

    var macCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Three columns: the name (the chat head's 15), the role
            // left-aligned in its own column, the status pill on the right,
            // the audio one (speaking, waiting) when there is one.
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                let role = t.role ?? ""
                Text(md(t.title)).font(.zoomed(size: 15, weight: t.unread > 0 ? .bold : .semibold))
                    .foregroundStyle(Web.fg).lineLimit(1)
                    .frame(width: role.isEmpty ? nil : 82 * Zoom.scale, alignment: .leading)
                if !role.isEmpty {
                    Text(role).font(.zoomed(size: 13)).foregroundStyle(Web.muted).lineLimit(1)
                        .truncationMode(.tail).layoutPriority(-1)
                }
                Spacer(minLength: 6)
                SideStatusPill(status: status, pills: pills)
            }
            if t.status != "running", t.last_message_kind == "error", let m = summaryLine(t.last_message) {
                Text(md("Error: \(m)")).font(.zoomed(size: 13.5)).foregroundStyle(Web.red).lineLimit(2).padding(.top, 4)
            }
            // Two items, then "N more" opens the rest in place (the owner
            // 2026-11-06 09:50: "it's more compressed… collapsed to like two,
            // and then you can click to expand").
            let items = sideItems
            let shown = expanded ? items : Array(items.prefix(Self.sideRows))
            ForEach(shown, id: \.0.id) { c, tap in SideItem(c: c, tap: tap) }
            let hidden = items.count - shown.count + (openCount > cells.count && !cells.isEmpty ? openCount - cells.count : 0)
            if hidden > 0 {
                Button { if items.count > shown.count { expanded = true } else { open(nil) } } label: {
                    Text("\(hidden) more").font(.zoomed(size: 13.5)).foregroundStyle(Web.muted).padding(.top, 6).padding(.leading, 13)
                }.buttonStyle(.plain)
            }
            if !todos.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(todos) { d in TodoLine(d: d) }
                }
                .padding(.top, items.isEmpty ? 0 : 4)
            }
            // Its open recs, the lowest lines: a violet dot and the title.
            ForEach(recCells) { c in SideItem(c: c) { open(nil) } }
        }
        // Room between people, as the old list had (09:50: "the way that
        // really worked well was when there was space between these").
        .padding(.horizontal, 18).padding(.top, 13).padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(on ? Web.accent.opacity(0.1) : Color.clear)
        .background(Web.panel)
        .overlay(alignment: .leading) { Rectangle().fill(edge.color).frame(width: 4) }
        .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
        .contentShape(Rectangle())
        .onTapGesture { open(first) }
    }

    #endif

    private var phoneRow: some View {
        VStack(alignment: .leading, spacing: 5) {
            // Three columns, as on the desktop: the name on one line in an
            // 82pt column (a longer one ends in "…"; with no role it takes
            // the room the role would have), the role in its own column, the
            // status pill.
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                let role = t.role ?? ""
                Text(md(t.title)).font(.body.weight(t.unread > 0 ? .semibold : .medium)).lineLimit(1)
                    .frame(width: role.isEmpty ? nil : 82, alignment: .leading)
                if !role.isEmpty {
                    Text(role).font(.subheadline).foregroundStyle(.secondary).lineLimit(1).layoutPriority(-1)
                }
                Spacer(minLength: 4)
                SideStatusPill(status: status, pills: pills)
            }
            ForEach(Self.folded(cells)) { c in MiniCard(c: c) { open(c.id) } }
            if !cells.isEmpty, more > 0 {
                // Lands on the first card the row did not draw.
                Button { open(firstHidden) } label: {
                    Text("and \(more) more").font(.caption).foregroundStyle(.secondary)
                }.buttonStyle(.plain)
            }
            ForEach(subsShown) { c in MiniCard(c: c) { open(sub: c) } }
            if subs.count > Self.subRows {
                Button { subsOpen.toggle() } label: {
                    Text(subsOpen ? "Show less" : "and \(subs.count - Self.subRows) more").font(.caption).foregroundStyle(.secondary)
                }.buttonStyle(.plain)
            }
            ForEach(todos) { d in TodoLine(d: d) }
            if let todo, !todo.isEmpty {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "arrow.right").font(.caption2.weight(.semibold))
                    Text(md(todo)).font(.subheadline).lineLimit(2)
                }.foregroundStyle(tone)
                if let d = previewLine(detail), !d.isEmpty {
                    Text(md(d)).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            // Its open recs, the lowest lines.
            ForEach(recCells) { c in MiniCard(c: c) { open(nil) } }
            if t.status != "running", t.last_message_kind == "error", let m = summaryLine(t.last_message) {
                // A dead turn is a fact about the session, not a description.
                Text(md("Error: \(m)")).font(.subheadline).foregroundStyle(.red).lineLimit(2)
            }
            // No foot: the model, cost, schedule and the live call count are
            // the chat head's, as on the desktop sidebar; the row is the name
            // and its cards. The audio pill is the status pill by the name.
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        // The row itself lands on the board's first card; a cell on its own.
        // (Not a NavigationLink: a link swallows the taps meant for the cells.)
        .onTapGesture { open(first) }
        .rowAccent(edge.color)
        // Chatting about a session row goes to a NEW session (the board is not
        // inside one): "what is this thing doing?" without interrupting it.
        .chatAbout(ChatSubject(
            screen: "Sessions board",
            title: "Session “\(t.title)”",
            facts: ["- `\(t.id)`, status \(t.status), \(t.needs_you) open ask(s), \(usd(t.cost_usd)) and \(tokenCount(t.tokens ?? 0)) tokens spent so far",
                    t.schedule.isEmpty ? "- no check-in schedule" : "- checks in \(scheduleLabel(t.schedule))",
                    t.goal_id.map { "- goal: \($0)" } ?? "- no goal set",
                    t.activity.map { "- doing right now: \($0)" } ?? "",
                    t.last_message.map { "- last reply:\n\($0.prefix(800))" } ?? ""].filter { !$0.isEmpty }.joined(separator: "\n")))
        // No swipe actions: the row had Clear/Stop under a trailing swipe until
        // 2026-09-23 ("we don't need the swipe to clear buttons on phone").
        // Clearing and stopping happen inside the chat, where the card is.
    }
}

/// One background helper of a session's turn (a shell job or a subagent), the
/// console's taskRowHTML (the owner 2026-11-03: where are my two jobs?):
/// its kind, what it is, how long it has run, a bar when its output counts
/// done/total, and its newest line. `open`: the caller draws its log under it.
/// A job cell's insides under its title: the bar with its count, then what
/// it is doing and for how long — the console's taskBarHTML + jobFootHTML.
struct JobProgress: View {
    let k: TurnTask
    var body: some View {
        #if targetEnvironment(macCatalyst)
        let accent = Web.accent, muted = Web.muted, size: CGFloat = 12
        #else
        let accent = Color.accentColor, muted = Color.secondary, size: CGFloat = 12
        #endif
        VStack(alignment: .leading, spacing: 4) {
            if let t = k.total, t > 0 {
                HStack(spacing: 8) {
                    GeometryReader { g in
                        ZStack(alignment: .leading) {
                            Capsule().fill(accent.opacity(0.18))
                            Capsule().fill(accent).frame(width: g.size.width * min(1, Double(k.done ?? 0) / Double(t)))
                        }
                    }.frame(height: 5)
                    Text(taskCount(k)).font(.system(size: size - 0.5)).foregroundStyle(muted).monospacedDigit()
                }
            }
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                let words = [k.doing ?? "", taskAge(k, now: ctx.date)].filter { !$0.isEmpty }.joined(separator: " · ")
                if !words.isEmpty {
                    Text(words).font(.system(size: size - 0.5)).foregroundStyle(muted).lineLimit(1).truncationMode(.tail)
                }
            }
        }
    }
}

struct TaskRow<Log: View>: View {
    let k: TurnTask
    var open: Bool = false
    var tap: (() -> Void)? = nil
    @ViewBuilder var log: () -> Log

    var body: some View {
        #if targetEnvironment(macCatalyst)
        let accent = Web.accent, muted = Web.muted, size: CGFloat = 12.5
        #else
        let accent = Color.accentColor, muted = Color.secondary, size: CGFloat = 13
        #endif
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if tap != nil {
                    Image(systemName: "chevron.right").font(.system(size: 8, weight: .semibold)).foregroundStyle(muted)
                        .rotationEffect(.degrees(open ? 90 : 0)).frame(width: 10)
                }
                Text(taskKind(k).uppercased()).font(.system(size: size - 3, weight: .bold)).foregroundStyle(accent)
                Text(k.description ?? "helper").font(.system(size: size, weight: .medium)).lineLimit(open ? nil : 1)
                Spacer(minLength: 4)
                TimelineView(.periodic(from: .now, by: 30)) { ctx in
                    Text(taskAge(k, now: ctx.date)).font(.system(size: size - 1)).foregroundStyle(muted).monospacedDigit()
                }
            }
            if let t = k.total, t > 0 {
                HStack(spacing: 8) {
                    GeometryReader { g in
                        ZStack(alignment: .leading) {
                            Capsule().fill(muted.opacity(0.2))
                            Capsule().fill(accent).frame(width: g.size.width * min(1, Double(k.done ?? 0) / Double(t)))
                        }
                    }.frame(height: 5)
                    Text(taskCount(k)).font(.system(size: size - 1.5, design: .monospaced)).foregroundStyle(muted)
                }
            }
            if !open, let d = k.doing, !d.isEmpty {
                Text(d).font(.system(size: size - 1.5, design: .monospaced)).foregroundStyle(muted).lineLimit(1).truncationMode(.tail)
            }
            log()
        }
        .padding(.horizontal, 10).padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(Color.primary.opacity(0.035)))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(open ? accent.opacity(0.5) : Color.primary.opacity(0.1), lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { tap?() }
    }
}

extension TaskRow where Log == EmptyView {
    init(k: TurnTask) { self.init(k: k, log: { EmptyView() }) }
}

/// What a chat has cost, in dollars, on a session row — "$12.40". The owner
/// 2026-08-25, after seeing token counts here: "I don't really care about the
/// total number of tokens. I care about the total cost." Tokens stay inside a
/// session (per reply, and the breakdown in Session settings). Live: the hub
/// prices the turn in flight as it streams, so this climbs while a session
/// works. Draws nothing when the hub has no figure at all — a chat that has
/// genuinely spent nothing still shows "$0.00", which is a fact, not a gap.
struct CostLabel: View {
    let value: Double?
    init(_ value: Double?) { self.value = value }
    var body: some View {
        if let value {
            Text(usd(value))
                .font(.caption2).foregroundStyle(.tertiary).monospacedDigit().layoutPriority(1)
        }
    }
}

/// The clock chip on a session row: its cadence, and — when the hub knows one
/// and the session is not running — the next wake, "daily 08:00 · today 18:00".
/// The Sessions page used to carry a separate "Checking in later" section for
/// this; it went on 2026-08-29 because a scheduled session was then listed
/// twice on one screen, and every session now gets exactly ONE heading (Your
/// turn / Working / Recent). The next wake had to survive that, so it rides
/// the row.
func scheduleChipLabel(_ t: Thread) -> String {
    let cadence = t.schedule_label ?? scheduleLabel(t.schedule)
    guard t.status != "running", let n = t.next_run_at else { return cadence }
    return "\(cadence) · \(nextLabel(n))"
}

/// "today 18:00" / "tomorrow 09:00" / "Thu 09:00" / "Sep 4" — how soon, not
/// the full date (the owner 2026-08-22: titles get the width).
func nextLabel(_ d: Date) -> String {
    let cal = Calendar.current
    if cal.isDateInToday(d) { return "today \(clock(d))" }
    if cal.isDateInTomorrow(d) { return "tomorrow \(clock(d))" }
    if d.timeIntervalSinceNow < 6 * 86400 { return weekdayClock(d) }
    return dayLabel(d)
}

/// Status is carried by colour, not an icon in the text block (icons kept
/// pulling the layout around — the owner 2026-08-22): a 4pt bar on the card's
/// leading edge plus the first chip in the chip row. Red = your turn /
/// blocked, blue = running, gray = scheduled or just a message.
func statusColor(_ status: String) -> Color {
    switch status { case "needs_you": .red; case "running": .blue; default: .secondary }
}
