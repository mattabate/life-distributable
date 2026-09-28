// Ask cards: one ask drawn inside its own session (the one shared `Card`,
// Card.swift). On the Sessions list a card is a cell on its session's row
// (`MiniCard`, ThreadRows.swift), the console's `.ask.mini`.
//
// A `surface=web` ask is a cell like any other: the phone's layout mirrors
// the web UI.
//
// The inline face draws nothing of its own. It decides
// the WORDS on the row (the console's askHTML, one list): Restart · Dismiss
// on a crash, Install · Dismiss on a build, Respond · Read on a read, and on
// everything else the hub's outcomes (Decided · Not deciding · Reply)
// then Dismiss. Each outcome arms the chat bar with that pick.
import SwiftUI

/// One ask on the board: what the agent needs, from which session, how long
/// ago. Tap → the session scrolled to the reply that raised it. Swipe →
/// Done (agent is told and carries on) or Dismiss (just recorded).
struct OpenAsk: Identifiable, Hashable {
    let thread: Thread
    let message: Int?
    /// The card to land on in the chat: a tapped approval, or the board's
    /// `first` for a session row.
    var card: String? = nil
    var id: String { thread.id }
}

/// How a card's button reaches the chat bar of the session it sits in (the
/// bar then shows a "replying to this" banner): the ask and the
/// outcome value picked ("" for words alone). ThreadDetail sets it; anywhere
/// else the button opens the Respond sheet on that pick instead.
@MainActor final class AskReplyHook {
    let arm: (Ask, String) -> Void
    init(_ arm: @escaping (Ask, String) -> Void) { self.arm = arm }
}

private struct AskReplyHookKey: EnvironmentKey { static let defaultValue: AskReplyHook? = nil }

extension EnvironmentValues {
    var askReply: AskReplyHook? {
        get { self[AskReplyHookKey.self] }
        set { self[AskReplyHookKey.self] = newValue }
    }
}

/// One ask inside its own session, right under the reply that raised it: full
/// detail (no truncation — this is the place to read it) and the one button
/// row. Closed asks stay as a faded record so the thread still reads sensibly
/// afterwards.
struct AskCard: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.askReply) private var askReply
    let a: Ask
    var goals: [Goal] = []
    var reload: () async -> Void = {}
    @State private var error: String?
    @State private var busy = false
    /// The Respond sheet, opened on this outcome (nil = not open) — outside a
    /// session, where there is no chat bar to arm.
    @State private var responding: Pick?
    private struct Pick: Identifiable { let outcome: String; var id: String { outcome } }

    /// The hub's one word for what the card wants (store.AskVerb) — the
    /// console's words ("grant"/"restart"/"for you").
    var kindLabel: String { a.verb ?? "for you" }
    var kindIcon: String {
        switch a.kind { case "decision": "questionmark"; case "access": "key"; case "physical": "hand.raised"; case "read": "text.alignleft"; case "install": "square.and.arrow.down"; case "error": "exclamationmark.triangle"; default: "exclamationmark" }
    }
    /// Anything that is not still the owner's to act on — the hub's `closed`
    /// (store.AskStanding). "superseded" is closed: an old install card must
    /// not keep an Install button once a newer build is on the phone. Only
    /// open and answered cards draw buttons.
    var closed: Bool { a.isClosed }
    var closedIcon: String {
        switch a.state { case "done": "checkmark.circle.fill"; case "superseded": "arrow.trianglehead.2.clockwise"; default: "xmark.circle" }
    }
    /// What "Copy link" copies: the ask's own link.
    var shareURL: URL? { a.firstLink }
    /// Needs action → red; something to read → blue (a read is the owner's
    /// next action but not a stopped session, so it can never wear red). An
    /// app update → teal, its own kind of cell. Inside a session a replied ask stays red until the
    /// agent closes it; a closed one is grey.
    var tint: Color {
        closed || a.kind == "error" ? .secondary : a.kind == "read" ? .accentColor : a.kind == "install" ? .teal : .red
    }

    var body: some View {
        Card(tint: tint,
             mark: closed ? closedIcon : a.kind == "read" || a.kind == "install" ? kindIcon : "hand.raised.fill",
             caption: closed ? a.state : a.state == "answered" ? "waiting on agent" : kindLabel,
             id: a.id,
             right: a.goal_id.map { g in goals.first { $0.id == g }?.title ?? g } ?? "",
             title: a.title,
             text: a.detailWithoutLinks,
             said: a.said ?? "",
             thread: a.thread_id,
             waiting: !closed && a.waiting_to_speak == true,
             hush: { Task { await cardAct(busy: $busy, error: $error, reload: reload) { try await hub.hushVoice(card: a.id) } } },
             closed: closed,
             line: a.resolution.flatMap { $0.isEmpty ? nil : $0 } ?? (a.state == "superseded" ? "replaced by a newer card" : ""),
             // A dismissed card — or a read marked Read —
             // folds to one grey line where it was, with Reopen inside.
             folded: a.folded,
             buttons: buttons, busy: busy, error: error)
        .contextMenu {
            if !closed { Button { respond("") } label: { Label("Respond", systemImage: "arrowshape.turn.up.left") } }
            if !closed, a.kind == "read" {
                Button { Task { await resolve("done", note: a.readWord) } } label: { Label("Read", systemImage: "checkmark") }
            } else if !closed {
                Button { dismiss.act() } label: { Label("Dismiss", systemImage: "xmark") }
            }
            if let u = shareURL {
                Button { UIPasteboard.general.url = u } label: { Label("Copy link", systemImage: "doc.on.doc") }
            }
        }
        .sheet(item: $responding) { RespondSheet(subject: RespondSubject(ask: a, first: $0.outcome), reload: reload) }
    }

    /// The row's words — the console's askHTML list, one card at a time.
    private func fold(back: Bool) -> CardButton {
        foldButton(.ask, a.id, back: back, hub: hub, busy: $busy, error: $error, reload: reload)
    }
    private var dismiss: CardButton { fold(back: false) }

    private var buttons: [CardButton] {
        if a.isFolded {
            // Open link stays on a folded card: the URL was the point of it.
            let link = a.firstLink.map { u in [CardButton(label: "Open link", url: u)] } ?? []
            return link + [fold(back: true)]
        }
        if closed { return [] }
        switch a.kind {
        case "error":
            // The error text is the whole story; the one thing worth doing to
            // a dead run is running it again.
            return [CardButton(label: busy ? "Restarting…" : "Restart", primary: true) { Task { await restart() } }, dismiss]
        case "install" where a.phoneLink != nil:
            // The install cell is title + description + the one Install button.
            // Tapping it also closes the ask on the spot (by "app", no thread
            // relay): the board is a to-do list, so a tapped card must leave. The hub reopens
            // it if the phone still reports an older build 10 min later.
            let u = a.phoneLink!
            return [CardButton(label: u.scheme == "itms-services" ? "Install" : "Open link", primary: true, url: u) {
                guard u.scheme == "itms-services" else { return }
                // InstallState takes it from here: once the iOS prompt is
                // answered the app quits itself so the next icon tap is the
                // new build, with no manual restart.
                AppDelegate.push.install.tapped(a.title)
                Task { await resolve("done", by: "app", note: "tapped Install") }
            }, dismiss]
        case "read":
            // Respond ARMS THE REPLY; Read is the silent close — done,
            // "Read it", the session never wakes — and folds the card.
            return [CardButton(label: "Respond", primary: true) { respond(a.outcomes?.first?.value ?? "done") },
                    CardButton(label: "Read") { Task { await resolve("done", note: a.readWord) } }]
        default:
            // The hub's words (Decided · Not deciding · Reply), each
            // arming the bar with that pick, then Dismiss — the silent close.
            let os = a.outcomes ?? []
            let picks = os.isEmpty ? [CardButton(label: "Respond", primary: true) { respond("") }] : outcomeButtons(os) { respond($0) }
            return picks + [dismiss]
        }
    }

    // MARK: - Acts

    /// A pick: inside a session it arms that session's chat bar with it;
    /// anywhere else it opens the sheet on that chip.
    func respond(_ outcome: String) {
        if let h = askReply { h.arm(a, outcome) } else { responding = Pick(outcome: outcome) }
    }

    func resolve(_ state: String, by: String = "owner", note: String = "") async {
        await cardAct(busy: $busy, error: $error, reload: reload) { _ = try await hub.resolveAsk(a.id, state: state, note: note, by: by) }
    }

    /// Replay the turn this session died on; the card closes itself.
    func restart() async {
        await cardAct(busy: $busy, error: $error, reload: reload) { try await hub.retryAsk(a.id) }
    }
}
