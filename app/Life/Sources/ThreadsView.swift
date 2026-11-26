import SwiftUI

/// Sessions — and, since 2026-09-02, Your turn as well. It draws the same hub
/// `Board` (`GET /api/v1/board?surface=mobile`) the console draws, so the two
/// surfaces can never disagree about what is his turn. Nothing here ranks,
/// holds back or counts; the hub already did.
///
/// One list with no headings under a "Your team" band: each person a row
/// ranked by status (SideStatus), each row carrying the cards that need the
/// owner, then the plain idle people two to a line.
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
    /// Every session this page holds, by id: the live ones and the people.
    var known: [String: Thread] {
        var out: [String: Thread] = [:]
        for t in store.team + threads { out[t.id] = t }
        return out
    }
    /// ONE ROW PER PERSON: a role with two sessions is the one key, its
    /// persona, else the session's own id (threads.js drawList's `who`).
    func who(_ id: String, _ known: [String: Thread]) -> String {
        known[id]?.persona.flatMap { $0.isEmpty ? nil : $0 } ?? id
    }
    /// A session's person: the board's `home` (the top of a listed chain),
    /// else up the `parent_id` chain of the sessions this page holds. A quiet
    /// sub-session on neither list cannot be walked without another read, so
    /// its rec stays under its own id and draws on no row.
    func ownerOf(_ id: String, _ known: [String: Thread]) -> String {
        if let h = turn.home?[id] { return h }
        var id = id, seen: Set<String> = [id]
        while let p = known[id]?.parent_id, !p.isEmpty, !seen.contains(p) { seen.insert(p); id = p }
        return id
    }
    /// Each person's open recs, keyed by `who`: the filing session is
    /// `thread_id`, or on an older rec the id inside `source`
    /// (`Rec.sourceThreadID`); a rec the owner filed is on no row, and a
    /// hidden one is off the list as it is off the chat.
    var recsBy: [String: [Rec]] {
        let k = known
        var out: [String: [Rec]] = [:]
        for r in store.recs where !r.isClosed && r.hidden_at == nil {
            guard let id = r.sourceThreadID, !id.isEmpty else { continue }
            out[who(ownerOf(id, k), k), default: []].append(r)
        }
        return out
    }
    /// ONE LIST, NO HEADINGS, the console's drawList: every row with a
    /// status, highest first (SideStatus), the hub's order within one; then
    /// the plain idle people, two to a line at the foot. A row's pill is
    /// `SideStatus.of`'s (Working over the rest while the turn runs), and so
    /// is its place: a row wearing Working sorts with the working rows. Which
    /// of a person's sessions is the row goes by `base`, their own status.
    var ranked: (rows: [(t: Thread, s: SideStatus, recs: [Rec])], idle: [Thread]) {
        let live = threads.filter { !nested($0) && (person($0) || turn.sectionOf($0) == "your_turn") }
        let ids = Set(live.map(\.id))
        let all = live + store.team.filter { person($0) && !nested($0) && !ids.contains($0.id) }
        let by = recsBy, k = known
        var cands: [(i: Int, t: Thread, s: SideStatus?, base: SideStatus?, own: Int, recs: [Rec])] = []
        for (i, t) in all.enumerated() {
            let rs = by[who(t.id, k)] ?? []
            let base = SideStatus.base(t, board: turn, recs: rs.count)
            cands.append((i, t, SideStatus.of(t, board: turn, recs: rs.count), base, base?.rawValue ?? 99, rs))
        }
        cands.sort { $0.own != $1.own ? $0.own < $1.own : $0.i < $1.i }
        // Each person once: their highest-status session by its own status,
        // the first in the hub's order (the newest) within one status. Then
        // the rows run by the pill each wears, but a speaking or waiting row
        // keeps its own status's place: the audio is a pill only.
        var once = Set<String>()
        var rows: [(i: Int, t: Thread, s: SideStatus, rank: Int, recs: [Rec])] = []
        var idle: [Thread] = []
        for x in cands where once.insert(who(x.t.id, k)).inserted {
            if let s = x.s, let base = x.base { rows.append((x.i, x.t, s, ([.speak, .wait].contains(s) ? base : s).rawValue, x.recs)) }
            else if person(x.t) { idle.append(x.t) }
        }
        rows.sort { $0.rank != $1.rank ? $0.rank < $1.rank : $0.i < $1.i }
        return (rows.map { ($0.t, $0.s, $0.recs) }, idle)
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
        case row(Thread, SideStatus, [Rec])
        /// Two idle people side by side, name over job (the owner 2026-11-06
        /// 02:03: "before… a more efficient use of space, putting these like
        /// 2 in a row… maybe with idle, we could put them 2 in a row").
        case pair([Thread])
        var id: String {
            switch self {
            case .row(let t, _, _): t.id
            case .pair(let ts): "pair " + ts.map(\.id).joined(separator: " ")
            }
        }
    }
    /// Every person once: the rows by status, then the idle pairs (`ranked`).
    private var macItems: [MacItem] {
        let r = ranked
        return r.rows.map { MacItem.row($0.t, $0.s, $0.recs) }
            + stride(from: 0, to: r.idle.count, by: 2).map { .pair(Array(r.idle[$0..<min($0 + 2, r.idle.count)])) }
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
                // 11-06) is one list now: each person once.
                VStack(alignment: .leading, spacing: 0) {
                    // One 52pt band, so its line runs on into the chat head's
                    // line: one line across the window, no step at the
                    // corner.
                    Text("YOUR TEAM").font(.zoomed(size: 12.5, weight: .semibold)).kerning(0.9).foregroundStyle(Web.muted)
                        .padding(.horizontal, 18).frame(maxWidth: .infinity, minHeight: 52, maxHeight: 52, alignment: .leading)
                        .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
                    if let error { ErrorBanner(message: error).padding(12) }
                    ForEach(macItems) { item in
                        switch item {
                        case .row(let t, let s, let rs):
                            ThreadRow(t: t, board: turn, on: selected?.thread.id == t.id, subs: subs(t), status: s, recs: rs)
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
                    ThreadDetail(thread: s.thread, focusMessage: s.message, focusCard: s.card, armOnOpen: s.arm)
                        // Keyed on the session, not the card: another card of the
                        // open session keeps the chat (draft, scroll, armed replies);
                        // ThreadDetail lands the new card itself.
                        .id(s.thread.id)
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

    /// One idle person, half the row: the name (its audio pill on the right)
    /// over the job, a line between the two; the open one a blue edge and tint.
    private func idleCell(_ t: Thread, divider: Bool) -> some View {
        let on = selected?.thread.id == t.id
        return Button { selected = OpenAsk(thread: t, message: nil) } label: {
            VStack(alignment: .leading, spacing: 1) {
                // Speaking or waiting is a pill only, so an idle person wears
                // it here, on the cell's right edge at the status column's
                // width, where every row's pill sits.
                HStack(spacing: 6) {
                    Text(md(t.title)).font(.zoomed(size: 15, weight: .semibold)).foregroundStyle(Web.fg).lineLimit(1)
                    if let a = SideStatus.audio(t, board: turn) {
                        Spacer(minLength: 0)
                        SideStatusPill(status: a, pills: turn.pills?[t.id] ?? [])
                    }
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
            // The title on the camera's line.
            .toolbarTitleDisplayMode(.inlineLarge)
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

    /// The page: one list with no headings, every person once, the rows by
    /// status, then the plain idle pairs (`ranked`, the desktop sidebar's
    /// rule). There is no New session row: the camera button is the phone's
    /// one way in.
    @ViewBuilder private var sessionsSections: some View {
        let r = ranked
        let idle = r.idle
        if !r.rows.isEmpty {
            Section {
                ForEach(r.rows, id: \.t.id) { x in
                    ThreadRow(t: x.t, board: turn, subs: subs(x.t), status: x.s, recs: x.recs)
                }
            }
        }
        // The plain idle people, two to a row, name over job (the Mac's
        // `macItems` pairs), so no person is drawn twice.
        if !idle.isEmpty {
            Section {
                ForEach(Array(stride(from: 0, to: idle.count, by: 2)), id: \.self) { i in
                    HStack(spacing: 0) {
                        idleCell(idle[i])
                        Divider()
                        if i + 1 < idle.count { idleCell(idle[i + 1]) } else { Color.clear.frame(maxWidth: .infinity) }
                    }
                    .listRowInsets(EdgeInsets())
                }
            }
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

    /// One idle person, half a row: the name (a green dot while a card of
    /// theirs is speaking) over the job, the Mac's `idleCell`.
    func idleCell(_ t: Thread) -> some View {
        Button { path.append(OpenAsk(thread: t, message: nil)) } label: {
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text(t.title).font(.body).foregroundStyle(.primary).lineLimit(1)
                    if voiced(t.id) { Self.speakDot }
                }
                if let role = t.role { Text(role).font(.footnote).foregroundStyle(.secondary).lineLimit(1) }
            }
            .padding(.horizontal, 16).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
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
            // LIFE_CARD=<id> lands on that card, as a tap on the row lands on
            // the board's first (a shot of one card whole, its Approval line too).
            let card = ProcessInfo.processInfo.environment["LIFE_CARD"].flatMap { $0.isEmpty ? nil : $0 }
            if let t = threads.first(where: { $0.id == want }) { openAsk = OpenAsk(thread: t, message: at, card: card) }
            else if let t = try? await hub.thread(want) { openAsk = OpenAsk(thread: t, message: at, card: card) }
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
