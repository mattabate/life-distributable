import SwiftUI
import UserNotifications

/// ONE definition of "your turn", and it is not on the phone. The hub's
/// `GET /api/v1/board?surface=mobile` ranks, bundles and counts; the Sessions
/// list's "Your turn · N" header, the number on the tab bar and the console
/// all print that one answer, so no two surfaces can disagree on the count.

/// The console's red oval (`.badge` in web/app.css), for the places iOS draws
/// no badge of its own — a row inside More. Zero draws nothing, so it can sit
/// in every row unconditionally.
struct CountOval: View {
    let n: Int
    init(_ n: Int) { self.n = n }
    var body: some View {
        if n > 0 {
            Text("\(n)")
                .font(.caption2.weight(.bold)).monospacedDigit()
                .foregroundStyle(.white)
                .padding(.horizontal, 6).padding(.vertical, 2)
                .background(.red, in: Capsule())
                .accessibilityLabel("\(n) waiting")
        }
    }
}

/// What each tab wants the owner to look at, as one number per tab — the
/// console's red ovals, on the phone (tab bar and More menu). The "your turn"
/// oval hangs off SESSIONS. Both surfaces do this.
@Observable @MainActor
final class Badges {
    /// Asks + approvals waiting on the owner — the oval on Sessions.
    var yourTurn = 0
    /// Dated steps that came due and are still open.
    var calendar = 0
    /// Recommendations sitting undecided. A COUNT, not a notification — recs
    /// are pulled, never pushed (docs/design/recommendations.md), so this
    /// number never reaches the push channel; it is only what they can see
    /// sitting there when they open the app.
    var recs = 0

    /// The numbers come from BoardStore's one poll (RootView applies them
    /// whenever the board changes). A failed fetch leaves the last numbers
    /// standing rather than zeroing the bar.
    func apply(_ b: BoardBadges) {
        yourTurn = b.yourTurn
        calendar = b.calendar
        recs = b.recs
        // The icon on the home screen wears the three added up — the console
        // nav's red ovals summed. The hub sends the same number on every push
        // and a badge-only push when it moves while the app is closed
        // (notify.SyncBadge); this keeps it true the moment the app sees the
        // board.
        UNUserNotificationCenter.current().setBadgeCount(yourTurn + calendar + recs)
    }
}
