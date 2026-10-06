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
        #if targetEnvironment(macCatalyst)
        macBody
        #else
        phoneBody
        #endif
    }

    #if targetEnvironment(macCatalyst)
    /// The console's Sessions page: the list stays down the left while a
    /// chat is open on the right, so what is waiting stays in view. A row
    /// or cell swaps the chat; All sessions opens in the list's own column.
    @State private var selected: OpenAsk?
    @State private var showAll = false
    @Environment(SnapState.self) private var snap
    /// The list as ONE run of things to draw — a heading, its sessions, the
    /// next heading — each session under its own id whichever heading it is
    /// under. It was a loop of sessions inside a loop of headings, and a
    /// session whose turn ended left one inner loop for the other: the list
    /// kept the row it had already drawn, so it went on reading "running"
    /// with the old activity line beside a chat head that had moved on.
    /// `make mac-screens PAGES=sessions LIFE_SHOT_MOVE=<id>` rehearses
    /// that move (BoardStore).
    private enum MacItem: Identifiable {
        case heading(key: String, words: String, first: Bool)
        case row(Thread)
        var id: String {
            switch self {
            case .heading(let key, _, _): "heading " + key
            case .row(let t): t.id
            }
        }
    }
    private var macItems: [MacItem] {
        var items: [MacItem] = []
        for h in headings.filter({ $0.key == "your_turn" }) + headings.filter({ $0.key != "your_turn" }) {
            let list = rows(h)
            if list.isEmpty { continue }
            items.append(.heading(key: h.key, words: "\(h.label) · \(h.count.isEmpty ? "\(list.count)" : h.count)".uppercased(), first: items.isEmpty))
            items += list.map(MacItem.row)
        }
        return items
    }
    private var macBody: some View {
        HStack(spacing: 0) {
            // The console's `.pane.list` (skin.css): caps headings, one card per
            // session, All sessions as its own box — not an iOS grouped list.
            ScrollView {
                // A plain VStack, never a lazy one: the list is a dozen
                // cards, and macOS 26's lazy stack froze the whole app on it
                // — its prefetch (LazyLayoutViewCache.signalPrefetch) set the
                // rows' phases, which dirtied the layout, which signalled the
                // prefetch again, in one frame that never ended ("Application
                // Not Responding").
                VStack(alignment: .leading, spacing: 8) {
                    if let error { ErrorBanner(message: error) }
                    ForEach(macItems) { item in
                        switch item {
                        case .heading(_, let words, let first):
                            Text(words)
                                .font(.system(size: 11.5, weight: .semibold)).tracking(0.8).foregroundStyle(.secondary)
                                .padding(.horizontal, 4).padding(.top, first ? 4 : 16).padding(.bottom, 2)
                        case .row(let t):
                            ThreadRow(t: t, board: turn, on: selected?.thread.id == t.id && !showAll)
                        }
                    }
                    Button { selected = nil; showAll = true } label: {
                        // The console's `.all-link`: the words and the count,
                        // no phone dot.
                        HStack {
                            Text("All sessions").font(.system(size: 13.5, weight: .semibold))
                            Spacer()
                            Text("\(threads.count)").font(.system(size: 13.5)).foregroundStyle(.secondary)
                        }
                        .padding(.horizontal, 14).padding(.vertical, 10)
                        .background(Color(uiColor: .systemBackground), in: RoundedRectangle(cornerRadius: 10))
                        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(showAll ? Color.primary.opacity(0.3) : Color.primary.opacity(0.1)))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).padding(.top, 6)
                    if threads.isEmpty && error == nil {
                        Text("No sessions yet").font(.subheadline).foregroundStyle(.secondary).padding(.horizontal, 4)
                    }
                }
                .padding(.horizontal, 12).padding(.vertical, 14)
            }
            .background(Color(uiColor: .systemGroupedBackground))
            .refreshable { await load() }
            .task { await load() }
            .frame(width: 380)
            Divider().ignoresSafeArea()
            if showAll {
                // The console draws All sessions in the chat's pane, so the
                // list of what is waiting stays beside it.
                NavigationStack { AllThreadsView() }
            } else if let s = selected {
                NavigationStack(path: $path) {
                    ThreadDetail(thread: s.thread, focusMessage: s.message, focusCard: s.card, armOnOpen: s.arm).id(s)
                        .navigationDestination(for: OpenAsk.self) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card, armOnOpen: $0.arm) }
                }
            } else {
                // No session open = the empty chat, box ready (threads.js
                // drawSessions): clicking Sessions means typing can start.
                // + New session lands here with the page it was pressed on.
                NewThreadSheet(initialImage: snap.desk?.image, initialPlace: snap.desk?.path ?? "",
                               onCreated: { await load() }, embedded: true,
                               started: { t in snap.desk = nil; selected = OpenAsk(thread: t, message: nil) })
                    .id(snap.deskGen)
            }
        }
        .onChange(of: snap.deskGen) { _, _ in path = NavigationPath(); selected = nil; showAll = false }
        .onChange(of: nav.card, initial: true) { _, c in if let c { nav.card = nil; path = NavigationPath(); showAll = false; selected = c } }
        .onChange(of: openAsk) { _, o in if let o { openAsk = nil; showAll = false; selected = o } }
        .sheet(isPresented: $composing) { NewThreadSheet { await load() } }
    }
    #endif

    private var phoneBody: some View {
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
            .navigationDestination(item: $openAsk) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card, armOnOpen: $0.arm) }
            // A session row is a value link, so its chat is built on tap,
            // not with every row (ThreadRow, here and in All sessions).
            .navigationDestination(for: OpenAsk.self) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card, armOnOpen: $0.arm) }
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
        #if !targetEnvironment(macCatalyst)
        // (The desktop's + New session is on its top bar.)
        Section {
            Button { composing = true } label: { Label("New session", systemImage: "plus.bubble") }
        }
        #endif
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
        #if targetEnvironment(macCatalyst)
        // The session open on the right is tinted, as in the console.
        ThreadRow(t: t, board: turn)
            .listRowBackground(selected?.thread.id == t.id ? Color.accentColor.opacity(0.1) : nil)
        #else
        ThreadRow(t: t, board: turn)
        #endif
    }

    /// Pull-to-refresh and "after I changed something": one store refresh.
    /// The 6s tick lives in RootView, not here.
    func load() async {
        await store.refresh(hub)
        #if targetEnvironment(simulator) || targetEnvironment(macCatalyst)
        // ops/screens.sh (ops/mac-screens.sh) thread:<id> opens that session, so an agent can
        // screenshot the conversation itself and not just the board;
        // thread:all opens the All sessions page.
        if openAsk == nil, path.isEmpty, let want = ProcessInfo.processInfo.environment["LIFE_THREAD"], !want.isEmpty {
            // (A push straight after the first refresh is dropped — the
            // stack is not up yet; one second later it lands.)
            #if targetEnvironment(macCatalyst)
            if want == "all" { showAll = true; return }
            #endif
            if want == "all" { try? await Task.sleep(for: .seconds(1)); path.append(AllSessions()) }
            else if let t = threads.first(where: { $0.id == want }) { openAsk = OpenAsk(thread: t, message: nil) }
        }
        #endif
    }
}
