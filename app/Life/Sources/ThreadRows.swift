// Session list rows: ThreadRow (one session, its open cards as cells),
// SessionCell/MiniCard (the console's `.ask.mini`), AllThreadsView (every
// session, a find box, twenty at a time), CostLabel and the status colour
// they share. The phone mirrors the console's list — whole title, one tinted
// cell per open card, pills in the foot, All sessions as a searchable table —
// rule for rule out of views/threads.js.
import SwiftUI

/// Every session, newest activity first — for finding something done
/// previously: a box that narrows it by name as you type, twenty rows at a time and
/// "Show 20 more" under them (the console's ALL_PAGE), one compact line per
/// session — name, pills, model, when, dollars. Tapping a row lands in the
/// chat on the card the board names first, like a session row.
struct AllThreadsView: View {
    @Environment(HubClient.self) private var hub
    /// Reads the one poll (BoardStore) — no loop of its own.
    @Environment(BoardStore.self) private var store
    @Environment(RefNav.self) private var nav
    @State private var find = ""
    @State private var shown = AllThreadsView.page
    static let page = 20

    var board: Board { store.board ?? .empty }
    /// The hub's /threads order is newest activity first already.
    var matching: [Thread] {
        let q = find.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return store.threads }
        return store.threads.filter { $0.title.localizedCaseInsensitiveContains(q) || $0.id.localizedCaseInsensitiveContains(q) }
    }

    var body: some View {
        let rows = matching
        List {
            if let error = store.error { Section { ErrorBanner(message: error) } }
            Section {
                ForEach(rows.prefix(shown)) { t in row(t) }
                if rows.count > shown {
                    HStack(spacing: 16) {
                        Button("Show \(Self.page) more") { shown += Self.page }
                        Button("Show all \(rows.count)") { shown = rows.count }
                    }.font(.subheadline)
                }
            } header: {
                Text(find.isEmpty ? "\(store.threads.count) sessions" : "\(rows.count) of \(store.threads.count)")
            }
            if rows.isEmpty && store.error == nil {
                Section { Text(find.isEmpty ? "No sessions yet" : "Nothing called “\(find)”").font(.subheadline).foregroundStyle(.secondary) }
            }
        }
        .searchable(text: $find, placement: .navigationBarDrawer(displayMode: .always), prompt: "Find a session…")
        .onChange(of: find) { _, _ in shown = Self.page }
        .navigationTitle("All sessions")
        .askButton()
        .refreshable { await load() }
    }

    /// One line per session, the console table's columns in a phone's width.
    func row(_ t: Thread) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(md(t.title)).font(.subheadline.weight(t.unread > 0 ? .semibold : .regular)).lineLimit(2)
            ChipRow {
                ForEach(board.pillsOf(t), id: \.self) { PillChip(pill: $0) }
                if let m = t.modelShort { Chip(m, icon: "cpu") }
                Spacer(minLength: 0)
                CostLabel(t.cost_usd)
                if let d = t.last_message_at { Text(shortAgo(d)).font(.caption2).foregroundStyle(.tertiary).layoutPriority(1) }
            }
        }
        .padding(.vertical, 1)
        .contentShape(Rectangle())
        .onTapGesture { nav.card = OpenAsk(thread: t, message: nil, card: board.first[t.id].flatMap { $0.isEmpty ? nil : $0 }) }
        .rowAccent(statusColor(t.status))
    }

    func load() async { await store.refresh(hub) }
}

/// One open card on a session's row, worded the console's way (`sessionCards`
/// + `miniCardHTML`, views/threads.js): what it wants from the owner in one word
/// (`verb`), its whole title, the first lines of what it says, and the tint
/// its class wears. Built from the board's bundle so the phone and the console
/// draw the same cells in the same order.
struct SessionCell: Identifiable, Hashable {
    let id: String
    let title: String
    /// The card's words under the title (nil: an install's title is the whole
    /// instruction, an error's body is a log).
    let body: String?
    let verb: String
    /// `needs` (red) · `read` (blue) · `install` (teal) · `error` (grey).
    let tone: String
    /// One of the owner's own dated steps that sits in this chat — uncounted.
    let step: Bool

    var color: Color {
        switch tone { case "read": .accentColor; case "install": .teal; case "error": .secondary; default: .red }
    }
    var mark: String {
        switch tone { case "read": "text.alignleft"; case "install": "square.and.arrow.down"; case "error": "exclamationmark.triangle"; default: "hand.raised.fill" }
    }

    /// The bundle as cells: approvals first (the session is stopped on them),
    /// then its asks, then the owner's dated steps — the board's order, untouched.
    static func cells(_ s: BoardSession) -> [SessionCell] {
        let acts = s.actions.map { a in
            SessionCell(id: a.id, title: a.title, body: body(a.title, a.detail), verb: "approve", tone: "needs", step: false)
        }
        let asks = (s.asks + (s.steps ?? [])).enumerated().map { i, a in
            let k = a.kind
            return SessionCell(id: a.id, title: a.title,
                               body: k == "install" || k == "error" ? nil : body(a.title, a.detail),
                               verb: a.verb ?? "for you",  // the hub's word (store.AskVerb)
                               tone: k == "read" ? "read" : k == "install" ? "install" : k == "error" ? "error" : "needs",
                               step: i >= s.asks.count)
        }
        return acts + asks
    }

    /// The body under the title: the detail flattened to one paragraph
    /// (`previewLine`, the console's mdPreview), less a first line that
    /// repeats the title — a card the hub minted from a reply has that reply's
    /// first line as its title AND as the first line of its body, and three
    /// lines tall it is said once.
    static func body(_ title: String, _ detail: String) -> String? {
        var lines = detail.trimmingCharacters(in: .whitespacesAndNewlines).split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if lines.count > 1, !plain(title).isEmpty, plain(lines[0]).hasPrefix(plain(title)) { lines.removeFirst() }
        return previewLine(lines.joined(separator: "\n"))
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
/// verb says what it wants; the title is whole; the body is clamped to three
/// lines. A tap lands the chat on this very card.
struct MiniCard: View {
    let c: SessionCell
    let tap: () -> Void
    var body: some View {
        Button(action: tap) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: c.mark).font(.caption2.weight(.semibold)).foregroundStyle(c.color).padding(.top, 3)
                    Text(md(c.title)).font(.subheadline.weight(.medium)).multilineTextAlignment(.leading)
                    Spacer(minLength: 4)
                    Text(c.verb).font(.caption2).foregroundStyle(c.color).layoutPriority(1)
                }
                if let b = c.body, !b.isEmpty {
                    Text(md(b)).font(.caption).foregroundStyle(.secondary).lineLimit(3).multilineTextAlignment(.leading)
                }
            }
            .padding(.horizontal, 8).padding(.vertical, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(c.color.opacity(c.tone == "error" ? 0.08 : 0.1), in: RoundedRectangle(cornerRadius: 8))
            .overlay(alignment: .leading) { RoundedRectangle(cornerRadius: 1.5).fill(c.color.opacity(c.tone == "error" ? 0.4 : 0.9)).frame(width: 3).padding(.vertical, 4).padding(.leading, 1) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

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
    /// "your turn"…) — the console draws the same ones. Every
    /// card drawn as a cell = the count pills would say it twice, so then only
    /// a RUNNING pill stays, in the foot beside the model.
    var pills: [Pill] = []
    /// The cells: one per open card, the board's bundle in its order.
    var cells: [SessionCell] = []
    /// Open cards beyond the ones drawn (board `open`): "and N more".
    var more: Int = 0
    /// An older hub counted a card it did not bundle: its title (board
    /// `todo`) and words (board `detail`) draw where the cells would.
    var todo: String? = nil
    var detail: String? = nil
    /// The card the chat opens on (board `first`), like the console's row.
    var first: String? = nil
    /// How many of a session's open cards its row draws (console ROW_CARDS).
    static let rowCards = 2

    init(t: Thread, board b: Board) {
        self.t = t
        forYou = b.forYou[t.id] ?? 0
        cells = b.bundleOf(t).map(SessionCell.cells) ?? []
        let open = b.open?[t.id] ?? 0
        more = max(0, open - min(cells.count, Self.rowCards))
        if forYou > 0, cells.isEmpty { todo = b.todo?[t.id]; detail = b.detail?[t.id] }
        let steps = cells.filter(\.step).count
        // …and "speaking" stays too: the one live fact a cell cannot carry
        // (which session's card is being spoken right now, or queued for it).
        pills = forYou > 0 && cells.count - steps >= forYou ? b.pillsOf(t).filter { ["running", "speaking", "waiting"].contains($0.tone) } : b.pillsOf(t)
        first = b.first[t.id].flatMap { $0.isEmpty ? nil : $0 }
    }

    /// The todo line's colour is the first card pill's (fallback only).
    var tone: Color { PillChip.tone(pills) }

    func open(_ card: String?) { nav.card = OpenAsk(thread: t, message: nil, card: card) }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            // The whole name, never clamped.
            Text(md(t.title)).font(.body.weight(t.unread > 0 ? .semibold : .medium))
            ForEach(cells.prefix(Self.rowCards)) { c in MiniCard(c: c) { open(c.id) } }
            if !cells.isEmpty, more > 0 {
                // Lands on the first card the row did not draw.
                Button { open(cells[min(Self.rowCards, cells.count - 1)].id) } label: {
                    Text("and \(more) more").font(.caption).foregroundStyle(.secondary)
                }.buttonStyle(.plain)
            }
            if let todo, !todo.isEmpty {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "arrow.right").font(.caption2.weight(.semibold))
                    Text(md(todo)).font(.subheadline).lineLimit(2)
                }.foregroundStyle(tone)
                if let d = previewLine(detail), !d.isEmpty {
                    Text(md(d)).font(.subheadline).foregroundStyle(.secondary).lineLimit(3)
                }
            }
            if t.status == "running" {
                // Live: what it is doing right now, not the message it was sent.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "arrow.turn.down.right").font(.caption2.weight(.semibold)).foregroundStyle(.blue)
                    Text(t.activity ?? "starting…").font(.subheadline).foregroundStyle(.blue).lineLimit(2)
                }
            } else if t.last_message_kind == "error", let m = summaryLine(t.last_message) {
                // A dead turn is a fact about the session, not a description.
                Text(md("Error: \(m)")).font(.subheadline).foregroundStyle(.red).lineLimit(2)
            }
            ChipRow {
                ForEach(pills, id: \.self) { PillChip(pill: $0) }
                if let m = t.modelShort { Chip(m, icon: "cpu") }
                if !t.schedule.isEmpty { Chip(scheduleChipLabel(t), icon: "clock") }
                Spacer(minLength: 0)
                // What this chat has cost so far, live while it runs
                // Trailing and tertiary: it is a
                // number to glance at, never the row's headline.
                CostLabel(t.cost_usd)
                if let d = t.last_message_at { Text(shortAgo(d)).font(.caption2).foregroundStyle(.tertiary).layoutPriority(1) }
            }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        // The row itself lands on the board's first card; a cell on its own.
        // (Not a NavigationLink: a link swallows the taps meant for the cells.)
        .onTapGesture { open(first) }
        .rowAccent(statusColor(t.status))
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
        // No swipe actions. Clearing and stopping happen inside the chat, where the card is.
    }
}

/// What a chat has cost, in dollars, on a session row — "$12.40". Cost, not
/// token counts, is what matters on the list. Tokens stay inside a
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
/// Every session gets exactly ONE place on the list, so the next wake rides
/// the row rather than a separate "Checking in later" section.
func scheduleChipLabel(_ t: Thread) -> String {
    let cadence = t.schedule_label ?? scheduleLabel(t.schedule)
    guard t.status != "running", let n = t.next_run_at else { return cadence }
    return "\(cadence) · \(nextLabel(n))"
}

/// "today 18:00" / "tomorrow 09:00" / "Thu 09:00" / "Sep 4" — how soon, not
/// the full date (titles get the width).
func nextLabel(_ d: Date) -> String {
    let cal = Calendar.current
    let time = d.formatted(.dateTime.hour().minute())
    if cal.isDateInToday(d) { return "today \(time)" }
    if cal.isDateInTomorrow(d) { return "tomorrow \(time)" }
    if d.timeIntervalSinceNow < 6 * 86400 { return d.formatted(.dateTime.weekday(.abbreviated)) + " \(time)" }
    return d.formatted(.dateTime.month(.abbreviated).day())
}

/// Status is carried by colour, not an icon in the text block (icons kept
/// pulling the layout around): a 4pt bar on the card's
/// leading edge plus the first chip in the chip row. Red = your turn /
/// blocked, blue = running, gray = scheduled or just a message.
func statusColor(_ status: String) -> Color {
    switch status { case "needs_you": .red; case "running": .blue; default: .secondary }
}
