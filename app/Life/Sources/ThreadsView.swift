import SwiftUI

/// Sessions — and, since 2026-09-02, Your turn as well. It draws the same hub
/// `Board` (`GET /api/v1/board?surface=mobile`) the console draws, so the two
/// surfaces can never disagree about what is his turn. Nothing here ranks,
/// holds back or counts; the hub already did.
///
/// A session is filed under Your turn or Working, or under no heading (All
/// sessions finds it) — the console's own rule (the owner 2026-08-29, Recent
/// gone 2026-09-20). Your turn leads, each row
/// carrying the card that needs him — its title, and now its own description
/// clamped to three lines (board `detail`).
///
/// THERE WAS A SECOND TAB HERE. From 2026-08-26 to 2026-09-02 this struct also
/// drew `.yourTurn`: the same board as a stack of cards, one per ask or
/// approval, with Done / Dismiss / Approve on each. The owner deleted it — "I think
/// I've decided that we don't need the Your Turn page anymore… I don't think
/// we need it since Your Turn is shown at the top of Sessions. It's not
/// obvious to me what I get inside the Your Turn things outside of a bigger
/// description." So the description came here and the tab went. The cards
/// themselves were never only there: every one of them is in its session's
/// chat (ThreadDetail), which is where a row now lands him — on the card, not
/// at the foot of the conversation.
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
    /// on top of whatever chat he is reading, so Back returns to it.
    @Environment(RefNav.self) private var nav
    @State private var path = NavigationPath()

    /// Who needs what from him, decided in ONE place — the hub. The board
    /// below, the red oval on the tab bar and the web console all read this
    /// same answer, so the header can never print a different number than
    /// the badge above it. Fetched in `load()`; nothing is recomputed here.
    /// The hub already sank a running session's bundle under the idle ones
    /// (its cards are answerable now — answering one steers the live turn —
    /// they are just not the first thing he sees), dropped the asks he has
    /// answered (the owner 2026-08-24: "this says answered, but on the Your Turn
    /// page, it still exists"; off the board whole 2026-09-20), and listed
    /// dated steps on their own with their date (the owner 2026-08-25). It is the
    /// SAME board the console gets: a `surface=web` ask sat behind a folded
    /// "N waiting on the laptop" row here, uncounted, from 2026-08-24 until
    /// 2026-09-23 ("Remove concept of waiting on laptop").
    var turn: Board { store.board ?? .empty }
    /// ONE HEADING PER SESSION, filed by the hub (board `headings` + `section`,
    /// 2026-09-14): running → Working, any card open for him → Your turn (a
    /// read or an install sits there too, wearing its blue/teal pill — the
    /// separate To read heading went on 2026-09-17: "you don't need separate
    /// areas for these two"; a session with both wears BOTH pills, one per
    /// class, since later that day: "I would like that called out
    /// separately"), else → none. The console reads the same three fields,
    /// so the headings and their words are identical there.
    var working: [Thread] { threads.filter { $0.status == "running" } }
    /// The groups in the hub's order and words. Before the first board,
    /// Working only.
    var headings: [BoardHeading] {
        turn.headings ?? [BoardHeading(key: "working", label: "Working", count: "", n: 0, show: 0)]
    }
    /// A PERSON is a session with a role. Only people are
    /// rows on this list (the owner 2026-10-24 00:20, of two plain
    /// sessions as rows: "all of the text should be
    /// personified… they should live under people"). Any other session is a
    /// row only while it waits on him — under Your turn, as an accident
    /// (00:42: "if by some insane accident, one of those sessions gets a
    /// message and is waiting, then sure, you should put it back here. But
    /// I'd consider that to be an accident"); otherwise it has no row and no
    /// page here ("you don't need any button for getting back to those
    /// sessions") — threads.js drawList's `person` / `shown`.
    func person(_ t: Thread) -> Bool { !(t.persona ?? "").isEmpty || !(t.role ?? "").isEmpty }
    /// The sessions under one heading, newest activity first (the hub's
    /// /threads order), cut to what the heading says it shows.
    func rows(_ h: BoardHeading) -> [Thread] {
        let all = threads.filter { turn.sectionOf($0) == h.key && !nested($0) && (person($0) || h.key == "your_turn") }
        return h.show > 0 ? Array(all.prefix(h.show)) : all
    }
    /// A session another one started is a row on that one's card, whatever
    /// heading it would have had (the owner 2026-10-23 01:23: "2 little
    /// subagents that are running"; 01:34: "the parent and then 2 of
    /// these… collapsing after 5 or 3"); threads.js drawList draws the same.
    /// The hub's `home` names the card: the top of the chain, so a run
    /// started by a run a person started is a cell on that person's (2026-10-27).
    func nested(_ t: Thread) -> Bool { turn.home?[t.id] != nil }
    func subs(_ t: Thread) -> [Thread] {
        threads.filter { turn.home?[$0.id] == t.id && turn.sectionOf($0) != nil }
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
    /// chat is open on the right (the owner 2026-09-29: "I don't want to have to
    /// click out of a session to see the other things that are waiting on
    /// me. I want to have a continuous list of those at all times"). A row
    /// or cell swaps the chat.
    @State private var selected: OpenAsk?
    /// The saved chat is reopened ONCE PER LAUNCH (MacResume). A page is
    /// rebuilt on every visit (RootView `macPage`), so a per-view flag ran the
    /// resume on every trip to Sessions — + New session from Calendar or Money
    /// came up as the last chat instead of the empty one with the screenshot
    /// (the owner 2026-10-11 12:02).
    private var resumed: Bool { MacResume.threadDone }
    @Environment(SnapState.self) private var snap
    /// The list as ONE run of things to draw — a heading, its sessions, the
    /// next heading — each session under its own id whichever heading it is
    /// under. It was a loop of sessions inside a loop of headings, and a
    /// session whose turn ended left one inner loop for the other: the list
    /// kept the row it had already drawn, so it went on reading "running"
    /// with the old activity line beside a chat head that read "speaking"
    /// (the owner 2026-09-30: "session card on upper left doesnt show the speaking
    /// pill"). `make mac-screens PAGES=sessions LIFE_SHOT_MOVE=<id>` rehearses
    /// that move (BoardStore).
    private enum MacItem: Identifiable {
        case heading(key: String, words: String, first: Bool)
        case row(Thread)
        /// Two idle people side by side, name over job (the owner 2026-11-06
        /// 02:03: "before… a more efficient use of space, putting these like
        /// 2 in a row… maybe with idle, we could put them 2 in a row").
        case pair([Thread])
        var id: String {
            switch self {
            case .heading(let key, _, _): "heading " + key
            case .row(let t): t.id
            case .pair(let ts): "pair " + ts.map(\.id).joined(separator: " ")
            }
        }
    }
    /// YOUR TURN, WORKING, then IDLE: every person once (the owner 2026-11-06,
    /// sidebar 3 — "Your turn / Working / Idle", no counts, idle people the
    /// same size as everyone else). Idle is the team less whoever is above.
    private var macItems: [MacItem] {
        var items: [MacItem] = []
        var placed = Set<String>()
        for h in headings.filter({ $0.key == "your_turn" }) + headings.filter({ $0.key != "your_turn" }) {
            let list = rows(h)
            if list.isEmpty { continue }
            items.append(.heading(key: h.key, words: h.label.uppercased(), first: items.isEmpty))
            items += list.map(MacItem.row)
            placed.formUnion(list.map(\.id))
        }
        let idle = store.team.filter { !placed.contains($0.id) && !nested($0) }
        if !idle.isEmpty {
            items.append(.heading(key: "idle", words: "IDLE", first: items.isEmpty))
            items += stride(from: 0, to: idle.count, by: 2).map { .pair(Array(idle[$0..<min($0 + 2, idle.count)])) }
        }
        return items
    }
    private var macBody: some View {
        HStack(spacing: 0) {
            // The console's `.pane.list` (skin.css): caps headings, one card per
            // session, All sessions as its own box — not an iOS grouped list
            // (the owner 2026-09-29: "why is the layout different?").
            ScrollView {
                // A plain VStack, never a lazy one: the list is a dozen
                // cards, and macOS 26's lazy stack froze the whole app on it
                // — its prefetch (LazyLayoutViewCache.signalPrefetch) set the
                // rows' phases, which dirtied the layout, which signalled the
                // prefetch again, in one frame that never ended (the owner
                // 2026-10-04: "Application Not Responding"; hang samples
                // 10-01 12:40, 10-04 21:40 and 23:50, all that stack).
                // Each person is a white row edge to edge with a line under it,
                // on the page grey; a heading sits on the grey with room round
                // it so it stands out (the owner 2026-11-06 01:46: "the
                // background is kind of darker than the foreground… the
                // spacing is big"). The people grid under SESSIONS (10-17 to
                // 11-06) is the Idle heading now: each person once.
                VStack(alignment: .leading, spacing: 0) {
                    if let error { ErrorBanner(message: error).padding(12) }
                    ForEach(macItems) { item in
                        switch item {
                        case .heading(_, let words, let first):
                            // The top heading is the chat head's 52pt, so its
                            // line runs on into the head's line: one line
                            // across the window, no step at the corner (the
                            // owner 2026-11-06 02:04: "this corner is just so
                            // jarring").
                            Text(words)
                                .font(.zoomed(size: 13, weight: .semibold)).tracking(0.9).foregroundStyle(Web.heading)
                                .padding(.horizontal, 18)
                                .padding(.top, first ? 0 : 24).padding(.bottom, first ? 0 : 9)
                                .frame(maxWidth: .infinity, minHeight: first ? 52 : nil, alignment: .leading)
                                .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
                        case .row(let t):
                            ThreadRow(t: t, board: turn, on: selected?.thread.id == t.id, subs: subs(t))
                        case .pair(let ts):
                            HStack(spacing: 0) {
                                ForEach(Array(ts.enumerated()), id: \.element.id) { i, t in idleCell(t, divider: i == 0) }
                                if ts.count == 1 { Color.clear.frame(maxWidth: .infinity) }
                            }
                            .background(Web.panel)
                            .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
                        }
                    }
                    // No other row: the sessions that are not people have no
                    // list and no page here since 2026-10-24 00:42 ("all
                    // sessions is no longer native… you don't need any button
                    // for getting back to those sessions"; the "All sessions
                    // · 850" link, and the Legacy box that stood in for it
                    // for one evening, went). They stay in the hub.
                    if threads.isEmpty && error == nil {
                        Text("No sessions yet").font(.subheadline).foregroundStyle(.secondary).padding(18)
                    }
                }
                .padding(.bottom, 14)
            }
            .background(Web.page)
            .refreshable { await load() }
            .task { await load() }
            .frame(width: 380)
            Divider().ignoresSafeArea()
            if let s = selected {
                NavigationStack(path: $path) {
                    ThreadDetail(thread: s.thread, focusMessage: s.message, focusCard: s.card, armOnOpen: s.arm).id(s)
                        .navigationDestination(for: OpenAsk.self) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card, armOnOpen: $0.arm) }
                }
            } else {
                // No session open = the empty chat, box ready (threads.js
                // drawSessions, the owner 2026-09-17: "there is only the chat
                // interface"): clicking Sessions means he can start typing.
                // + New session lands here with the page it was pressed on.
                NewThreadSheet(initialImage: snap.desk?.image, initialPlace: snap.desk?.path ?? "",
                               onCreated: { await load() }, embedded: true,
                               started: { t in snap.desk = nil; selected = OpenAsk(thread: t, message: nil) })
                    .id(snap.deskGen)
            }
        }
        .onChange(of: snap.deskGen) { _, _ in path = NavigationPath(); selected = nil }
        .onChange(of: nav.card, initial: true) { _, c in if let c { nav.card = nil; path = NavigationPath(); selected = c } }
        .onChange(of: openAsk) { _, o in if let o { openAsk = nil; selected = o } }
        .onChange(of: selected?.thread.id) { _, id in if resumed { MacResume.save(thread: id) } }
        .sheet(isPresented: $composing) { NewThreadSheet { await load() } }
    }

    /// One idle person, half the row: the name over
    /// the job, a line between the two; the open one a blue edge and tint.
    private func idleCell(_ t: Thread, divider: Bool) -> some View {
        let on = selected?.thread.id == t.id
        return Button { selected = OpenAsk(thread: t, message: nil) } label: {
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text(md(t.title)).font(.zoomed(size: 15, weight: .semibold)).foregroundStyle(Web.fg).lineLimit(1)
                }
                if let role = t.role { Text(role).font(.zoomed(size: 13)).foregroundStyle(Web.muted).lineLimit(1) }
            }
            .padding(.horizontal, 18).padding(.vertical, 9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(on ? Web.accent.opacity(0.1) : Color.clear)
            .overlay(alignment: .leading) { if on { Rectangle().fill(Web.accent).frame(width: 4) } }
            .overlay(alignment: .trailing) { if divider { Rectangle().fill(Web.line).frame(width: 1) } }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
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
            // not with every row (ThreadRow).
            .navigationDestination(for: OpenAsk.self) { ThreadDetail(thread: $0.thread, focusMessage: $0.message, focusCard: $0.card, armOnOpen: $0.arm) }
        }
    }

    /// The page: start a session, then the hub's headings — Your turn (paused
    /// on him), Working (in flight). Mirrors the console's sidebar heading for heading (the owner
    /// 2026-08-29: "make sure the phone displays the same way"); the layout is
    /// a phone's, the rule is the same one.
    @ViewBuilder private var sessionsSections: some View {
        // Secondary entry: the primary way to start a session is the
        // Ask button top-right on every screen, which snaps the screen
        // (the owner 2026-08-21). This stays for text-only asks.
        #if !targetEnvironment(macCatalyst)
        // (The desktop's + New session is on its top bar.)
        Section {
            Button { composing = true } label: { Label("New session", systemImage: "plus.bubble") }
        }
        #endif
        // Your turn first, then Working — each heading's words straight from
        // the hub. (A folded "N waiting on the laptop" row sat between them
        // from 2026-08-24 to 2026-09-23; a web ask is an ordinary cell now.)
        ForEach(headings.filter { $0.key == "your_turn" }, id: \.key) { h in heading(h) }
        // Sessions in flight (a running session with cards open wears "N for
        // you" and gets no second row above).
        ForEach(headings.filter { $0.key != "your_turn" }, id: \.key) { h in heading(h) }
        // THE PEOPLE, as on the Mac's foot: a row each (the hub's role
        // order), and nothing under them: no All
        // sessions, no Legacy (2026-10-24 00:42; `rows(_:)`).
        Section("Sessions") {
            ForEach(store.team.filter(person)) { t in teamRow(t) }
        }
        if (turn.threads_total ?? threads.count) == 0 && error == nil {
            Section {
                Text("No sessions yet").font(.subheadline).foregroundStyle(.secondary)
            }
        }
    }

    /// WHO IS TALKING, by name (the owner 2026-10-28 20:01, of a card
    /// speaking under "and 1 more"): the person whose session, or a run homed
    /// on them, is in his ears — the console's `voicedBy`.
    func voiced(_ id: String) -> Bool {
        let on: (String) -> Bool = { k in turn.pills?[k]?.contains { $0.tone == "speaking" } == true }
        if on(id) { return true }
        return (turn.home ?? [:]).contains { $0.value == id && on($0.key) }
    }
    /// A green dot beside the name: a pill crowds the role in a half-width cell.
    static var speakDot: some View { Circle().fill(Color.green).frame(width: 7, height: 7).accessibilityLabel("speaking") }

    /// One person's row: her name, her role under it, her open cards a gray
    /// count — the Mac's `teamCard` in a phone list.
    func teamRow(_ t: Thread) -> some View {
        let open = turn.forYou[t.id] ?? 0
        return NavigationLink(value: OpenAsk(thread: t, message: nil)) {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(t.title).font(.body.weight(.semibold)).lineLimit(1)
                        if voiced(t.id) { Self.speakDot }
                    }
                    if let role = t.role { Text(role).font(.subheadline).foregroundStyle(.secondary).lineLimit(1) }
                }
                Spacer(minLength: 0)
                if open > 0 {
                    Text("\(open)").font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
                        .padding(.horizontal, 8).padding(.vertical, 2)
                        .background(Color.secondary.opacity(0.15), in: Capsule())
                }
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
    /// card (2026-09-20, here since 2026-09-23). Everything on it comes from
    /// the one board poll, so a row says the same thing here and there.
    @ViewBuilder func row(_ t: Thread) -> some View {
        #if targetEnvironment(macCatalyst)
        // The session open on the right is tinted, as in the console.
        ThreadRow(t: t, board: turn, subs: subs(t))
            .listRowBackground(selected?.thread.id == t.id ? Color.accentColor.opacity(0.1) : nil)
        #else
        ThreadRow(t: t, board: turn, subs: subs(t))
        #endif
    }

    /// Pull-to-refresh and "after I changed something": one store refresh.
    /// The 6s tick lives in RootView, not here.
    func load() async {
        await store.refresh(hub)
        #if targetEnvironment(simulator) || targetEnvironment(macCatalyst)
        // ops/screens.sh (ops/mac-screens.sh) thread:<id> opens that session, so an agent can
        // screenshot the conversation itself and not just the board.
        if openAsk == nil, path.isEmpty, let want = ProcessInfo.processInfo.environment["LIFE_THREAD"], !want.isEmpty {
            // (A push straight after the first refresh is dropped — the
            // stack is not up yet; one second later it lands.)
            // LIFE_SCROLL=<message id> lands on that message, for a bubble
            // above the fold (a tagged one, NoteTags).
            let at = ProcessInfo.processInfo.environment["LIFE_SCROLL"].flatMap { Int($0) }
            // The list holds only live sessions; any other is one read away.
            if let t = threads.first(where: { $0.id == want }) { openAsk = OpenAsk(thread: t, message: at) }
            else if let t = try? await hub.thread(want) { openAsk = OpenAsk(thread: t, message: at) }
        }
        #endif
        #if targetEnvironment(macCatalyst)
        // Back in the chat he had open before an Update or ⌘R (MacResume).
        if !resumed {
            MacResume.threadDone = true
            if ProcessInfo.processInfo.environment["LIFE_SHOT"] == nil, selected == nil, openAsk == nil, snap.desk == nil,
               let want = MacResume.thread {
                if let t = threads.first(where: { $0.id == want }) { selected = OpenAsk(thread: t, message: nil) }
                else if let t = try? await hub.thread(want) { selected = OpenAsk(thread: t, message: nil) }
            }
        }
        #endif
    }
}
