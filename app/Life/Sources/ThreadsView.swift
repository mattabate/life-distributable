import SwiftUI

/// Sessions and Your turn. It draws the same hub
/// `Board` (`GET /api/v1/board?surface=mobile`) the console draws, so the two
/// surfaces can never disagree about whose turn it is. Nothing here ranks,
/// holds back or counts; the hub already did.
///
/// A session is filed under Your turn or Working, or under no heading (All
/// sessions finds it) — the console's own rule. Your turn leads, each row
/// carrying the card that needs the owner — its title and its own description
/// clamped to three lines (board `detail`).
///
/// There is no separate Your turn tab: every card is in its session's chat
/// (ThreadDetail), which is where a row lands — on the card, not at the foot
/// of the conversation.
struct ThreadsView: View {
    @Environment(HubClient.self) private var hub
    /// The one poll (BoardStore, 6s, owned by RootView): board, threads and
    /// goals. Both tabs, the All-sessions list and the tab-bar numbers read
    /// it, so swiping an ask Done takes the oval with the card.
    @Environment(BoardStore.self) private var store
    var threads: [Thread] { store.threads }
    var error: String? { store.error }
    @State private var composing = false
    /// The simulator's `LIFE_THREAD` (ops/screens.sh thread:<id>): the one
    /// session opened without a tap. A tapped row or cell goes through
    /// `nav.card` and the path instead.
    @State private var openAsk: OpenAsk?
    /// An ask or proposal id tapped in a card's text (Refs.swift) is pushed
    /// on top of whatever chat is open, so Back returns to it.
    @Environment(RefNav.self) private var nav
    @State private var path = NavigationPath()

    /// Who needs what from the owner, decided in ONE place — the hub. The board
    /// below, the red oval on the tab bar and the web console all read this
    /// same answer, so the header can never print a different number than
    /// the badge above it. Fetched in `load()`; nothing is recomputed here.
    /// The hub already sank a running session's bundle under the idle ones
    /// (its cards are answerable now — answering one steers the live turn —
    /// they are just not the first thing seen), dropped answered asks, and
    /// listed dated steps on their own with their date. It is the SAME board
    /// the console gets: a `surface=web` ask is an ordinary cell here.
    var turn: Board { store.board ?? .empty }
    /// ONE HEADING PER SESSION, filed by the hub (board `headings` + `section`):
    /// running → Working, any card open for the owner → Your turn (a read or an
    /// install sits there too, wearing its blue/teal pill; a session with both
    /// wears BOTH pills, one per class), else → none. The console reads the same three fields,
    /// so the headings and their words are identical there.
    var working: [Thread] { threads.filter { $0.status == "running" } }
    /// The groups in the hub's order and words. Before the first board,
    /// Working only.
    var headings: [BoardHeading] {
        turn.headings ?? [BoardHeading(key: "working", label: "Working", count: "", n: 0, show: 0)]
    }
    /// The sessions under one heading, newest activity first (the hub's
    /// /threads order), cut to what the heading says it shows.
    func rows(_ h: BoardHeading) -> [Thread] {
        let all = threads.filter { turn.sectionOf($0) == h.key }
        return h.show > 0 ? Array(all.prefix(h.show)) : all
    }
    var body: some View {
        NavigationStack(path: $path) {
            List {
                if let error { Section { ErrorBanner(message: error) } }
                sessionsSections
            }
            .onChange(of: nav.card, initial: true) { _, c in if let c { nav.card = nil; path.append(c) } }
            .navigationTitle("Sessions")
            .askButton()
            .refreshable { await load() }
            .task { await load() }
            .sheet(isPresented: $composing) { NewThreadSheet { await load() } }
            .navigationDestination(item: $openAsk) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card) }
            // A session row is a value link, so its chat is built on tap,
            // not with every row (ThreadRow, here and in All sessions).
            .navigationDestination(for: OpenAsk.self) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card) }
            .navigationDestination(for: AllSessions.self) { _ in AllThreadsView() }
        }
    }

    /// The All sessions page as a path value, so a screenshot run can push it
    /// (`ops/screens.sh thread:all`) the way a tap on the foot row does.
    struct AllSessions: Hashable {}

    /// The page: start a session, then the hub's headings — Your turn (paused
    /// on the owner), Working (in flight). Mirrors the console's sidebar heading
    /// for heading; the layout is a phone's, the rule is the same one.
    @ViewBuilder private var sessionsSections: some View {
        // Secondary entry: the primary way to start a session is the
        // Ask button top-right on every screen, which snaps the screen.
        // This stays for text-only asks.
        Section {
            Button { composing = true } label: { Label("New session", systemImage: "plus.bubble") }
        }
        // Your turn first, then Working — each heading's words straight from
        // the hub.
        ForEach(headings.filter { $0.key == "your_turn" }, id: \.key) { h in heading(h) }
        // Sessions in flight (a running session with cards open wears "N for
        // you" and gets no second row above); the rest behind All sessions.
        ForEach(headings.filter { $0.key != "your_turn" }, id: \.key) { h in heading(h) }
        Section {
            NavigationLink(value: AllSessions()) {
                HStack(spacing: 8) {
                    if !working.isEmpty { LiveDot() }
                    Text(working.isEmpty ? "All sessions" : "All sessions · \(working.count) working")
                    Spacer()
                    Text("\(threads.count)").foregroundStyle(.secondary)
                }
            }
        }
        if threads.isEmpty && error == nil {
            Section {
                Text("No sessions yet").font(.subheadline).foregroundStyle(.secondary)
            }
        }
    }

    /// One heading and its rows; a heading with no rows is not drawn. With no
    /// hub words (older hub) it falls back to the row count.
    @ViewBuilder func heading(_ h: BoardHeading) -> some View {
        let list = rows(h)
        if !list.isEmpty {
            Section("\(h.label) · \(h.count.isEmpty ? "\(list.count)" : h.count)") {
                ForEach(list) { t in row(t) }
            }
        }
    }

    /// One session row, under whichever heading it landed: its whole title,
    /// one tinted cell per open card, its pills in the foot — the console's
    /// card. Everything on it comes from
    /// the one board poll, so a row says the same thing here and there.
    @ViewBuilder func row(_ t: Thread) -> some View {
        ThreadRow(t: t, board: turn)
    }

    /// Pull-to-refresh and "after I changed something": one store refresh.
    /// The 6s tick lives in RootView, not here.
    func load() async {
        await store.refresh(hub)
        #if targetEnvironment(simulator)
        // ops/screens.sh thread:<id> opens that session, so an agent can
        // screenshot the conversation itself and not just the board;
        // thread:all opens the All sessions page.
        if openAsk == nil, path.isEmpty, let want = ProcessInfo.processInfo.environment["LIFE_THREAD"], !want.isEmpty {
            // (A push straight after the first refresh is dropped — the
            // stack is not up yet; one second later it lands.)
            if want == "all" { try? await Task.sleep(for: .seconds(1)); path.append(AllSessions()) }
            else if let t = threads.first(where: { $0.id == want }) { openAsk = OpenAsk(thread: t, message: nil) }
        }
        #endif
    }
}
