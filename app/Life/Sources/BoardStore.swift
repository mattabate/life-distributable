// The one poll of the hub's board. RootView owns a single loop (paused in
// the background and while a session is open — ThreadDetail has its own
// then) and every screen reads this object, so no screen runs its own
// poller. The loop does not tick on a timer: it parks on
// `GET /api/v1/changes` and refreshes when the hub says something moved,
// and every refresh is a conditional GET (304 when nothing did).
import SwiftUI

@Observable @MainActor
final class BoardStore {
    /// The hub's answer to "what needs the owner", ranked, bundled and counted.
    /// nil until the first fetch lands.
    var board: Board?
    /// The change feed's last version — what the next `changes` call waits on.
    var version = ""

    /// Paint from the phone's copy before the first request: on a cold start
    /// away from home the first round trip can take seconds, and a Sessions
    /// tab that already shows yesterday's list beats a spinner.
    func seed(_ hub: HubClient) async {
        guard hub.isConfigured, threads.isEmpty, board == nil else { return }
        let keys = (hub.cacheKey(HubClient.threadsPath), hub.cacheKey(HubClient.goalsPath), hub.cacheKey(HubClient.boardPath))
        let (t, g, b): ([Thread]?, [Goal]?, Board?) = await Task.detached {
            (HubClient.cachedValue(key: keys.0), HubClient.cachedValue(key: keys.1), HubClient.cachedValue(key: keys.2))
        }.value
        // Only if the network has not beaten us to it.
        if threads.isEmpty, let t { threads = t }
        if goals.isEmpty, let g { goals = g }
        if board == nil, let b { board = b }
    }
    /// Every thread, for the Sessions tab, All sessions and the cost figure on
    /// an ask card (an ask carries a thread id, not a thread).
    var threads: [Thread] = []
    var goals: [Goal] = []
    var error: String?
    /// How many ThreadDetails are open: each polls its own session, so the
    /// list behind them stops (it made a chat slow). A count, not a Bool — a chat opened from a chat disappearing
    /// must not un-pause while the outer one is still on screen.
    var pauses = 0
    var paused: Bool { pauses > 0 }
    func pause() { pauses += 1 }
    func resume() { pauses = max(0, pauses - 1) }
    /// Scene is in the foreground. Nothing polls from the background.
    var active = true

    var wantsPoll: Bool { active && !paused }

    #if targetEnvironment(macCatalyst)
    /// A screenshot run's rehearsal of a turn ending (`make mac-screens
    /// PAGES=sessions LIFE_SHOT_MOVE=<id> LIFE_SHOT_STEPS=bare:4,cards:2,said:2,done:50
    /// LIFE_SHOT_ORDER=last`): the list is drawn step by step as it stands
    /// through `finishTurn` — `bare` running with no cards, `cards` its card
    /// minted, `said` speaking too, `done` under Your turn speaking, `turn`
    /// the hub as it is — each held for its seconds, then the live hub. So
    /// the shot shows whether its row followed it across the headings: the
    /// one change a still shot of the live hub never catches, and the one
    /// that left a row reading "running" beside a chat head reading
    /// "speaking" (the owner 2026-09-30; ThreadsView.macItems).
    private var rehearse = ProcessInfo.processInfo.environment["LIFE_SHOT_MOVE"].flatMap { $0.isEmpty ? nil : $0 }
    private var rehearsing = false
    #endif

    /// One round trip for all three; assign only what changed, because an
    /// unconditional reassignment re-rendered every card each tick.
    func refresh(_ hub: HubClient) async {
        guard hub.isConfigured else { return }
        do {
            async let t = hub.threads(); async let g = hub.goals(); async let b = hub.board()
            let (freshThreads, freshGoals, freshBoard) = try await (t, g, b)
            #if targetEnvironment(macCatalyst)
            if rehearsing { return }
            if let id = rehearse, freshThreads.contains(where: { $0.id == id }) {
                rehearse = nil; rehearsing = true
                goals = freshGoals
                let steps = (ProcessInfo.processInfo.environment["LIFE_SHOT_STEPS"] ?? "cards:2").split(separator: ",").map { $0.split(separator: ":") }
                let last = ProcessInfo.processInfo.environment["LIFE_SHOT_ORDER"] == "last"
                Task {
                    for s in steps {
                        let kind = String(s[0]), secs = s.count > 1 ? Double(s[1]) ?? 2 : 2
                        if kind == "turn" { threads = freshThreads; board = freshBoard } else if kind == "done" {
                            var ts = freshThreads, now = freshBoard
                            if let i = ts.firstIndex(where: { $0.id == id }) {
                                ts[i].status = "needs_you"; ts[i].activity = nil
                                ts.insert(ts.remove(at: i), at: 0)
                            }
                            now.section?[id] = "your_turn"
                            now.pills?[id] = [Pill(word: "speaking", tone: "speaking")] + (freshBoard.pills?[id] ?? []).filter { !["speaking", "waiting", "running"].contains($0.tone) }
                            threads = ts; board = now
                        } else {
                            var ts = freshThreads, was = freshBoard
                            if let i = ts.firstIndex(where: { $0.id == id }) {
                                ts[i].status = "running"; ts[i].activity = "Rehearsing the end of a turn (\(kind))"
                                if last { ts.append(ts.remove(at: i)) }
                            }
                            was.section?[id] = "working"
                            var pills = [Pill(word: "running", tone: "running")]
                            if kind == "bare" {
                                was.sessions?.removeAll { $0.id == id }
                                was.forYou[id] = nil; was.open?[id] = nil; was.first[id] = nil
                                was.todo?[id] = nil; was.detail?[id] = nil; was.reads?[id] = nil; was.installs?[id] = nil
                            } else {
                                pills += (freshBoard.pills?[id] ?? []).filter { !["speaking", "waiting", "running"].contains($0.tone) }
                                if kind == "said" { pills.insert(Pill(word: "speaking", tone: "speaking"), at: 0) }
                            }
                            was.pills?[id] = pills
                            threads = ts; board = was
                        }
                        try? await Task.sleep(for: .seconds(secs))
                    }
                    rehearsing = false; await refresh(hub)
                }
                return
            }
            #endif
            if freshThreads != threads { threads = freshThreads }
            if freshGoals != goals { goals = freshGoals }
            if freshBoard != board { board = freshBoard }
            error = nil
        } catch { if !error.isCancellation { self.error = error.localizedDescription } }
    }

    /// `GET /status`, one copy for More, Settings and the Update sheet: the
    /// newest OTA build and the push routes. Opening Update from More used to
    /// fetch it a second time and paint "checking" in between.
    var status: HubStatus?
    func loadStatus(_ hub: HubClient) async {
        guard hub.isConfigured, let s = try? await hub.status() else { return }
        status = s
    }
}
