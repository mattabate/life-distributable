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

    /// One round trip for all three; assign only what changed, because an
    /// unconditional reassignment re-rendered every card each tick.
    func refresh(_ hub: HubClient) async {
        guard hub.isConfigured else { return }
        do {
            async let t = hub.threads(); async let g = hub.goals(); async let b = hub.board(surface: "mobile")
            let (freshThreads, freshGoals, freshBoard) = try await (t, g, b)
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
