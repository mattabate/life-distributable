import Foundation

/// "Am I on the newest app, and what one tap gets me there?" — answered once,
/// for every surface that asks (Settings, More → Update app).
///
/// Four build numbers are knowable on the phone and the Settings screen used to
/// print all four at once: the published build (296), this app (288), what the
/// Mac would build next (297) and whatever an old log tail mentioned (293).
/// That reads as a malfunction, not as four facts. So the numbers are collapsed
/// HERE, once, and a surface shows the answer instead of the arithmetic.
///
/// The trap this exists to close: the build number is `git rev-list --count`,
/// so a docs-only commit raises `head` without changing a line of Swift. The
/// old copy offered that as "Build latest code — 297", which minted a 297
/// byte-identical to 296. `app_changed` (hub-side git
/// diff of app/ since the published commit) is the fact that decides it;
/// comparing numbers is only the fallback when the hub cannot run git.
struct AppUpdate {
    enum Action: Equatable {
        case install(URL)   // published, newer than this app — one tap installs
        case build          // app code on the Mac that no published build has
        case none           // nothing newer anywhere
    }

    let action: Action
    /// The newest build there is, by any lane. The ONLY number a surface should
    /// put in front of the owner.
    let newest: Int
    let mine: Int
    let signedUntil: String?

    init(ota: OTABuild?, mine: Int) {
        self.mine = mine
        self.signedUntil = ota?.profile_expires
        guard let o = ota else {
            (action, newest) = (.none, mine)
            return
        }
        // Two conditions, both required, and each one closes a different way of
        // lying to the owner. The number must actually go up — a session's
        // uncommitted work builds as the SAME number they are already running, so
        // "Update to build 297" while they are on 297 is the same disagreement in
        // a new costume. And the code must actually differ — head counts docs
        // commits, which is what minted the pointless 297 in the first place
        // (`app_changed` nil = no git on the hub, so trust the numbers).
        let head = o.head ?? 0
        let macHasNewCode = head > max(o.build, mine) && (o.app_changed ?? true)
        if macHasNewCode {
            (action, newest) = (.build, head)
        } else if o.build > mine, let u = o.installURL {
            (action, newest) = (.install(u), o.build)
        } else {
            (action, newest) = (.none, max(o.build, mine))
        }
    }

    /// Same words whichever lane serves it: the tap means "get me the newest".
    var buttonTitle: String {
        switch action {
        case .none: return "Up to date"
        default: return "Update to build \(newest)"
        }
    }

    var isUpToDate: Bool { action == .none }

    /// One caption, one number in it — the build this app IS. Never quotes the
    /// published build or the Mac's next build as a rival figure.
    var caption: String {
        let signed = signedUntil.map { " Signed until \($0)." } ?? ""
        switch action {
        case .install:
            return "This app is build \(mine). iOS asks to install, then the icon updates on the Home Screen." + signed
        case .build:
            return "The Mac has app changes no build carries yet. One tap builds it (~2 min), then iOS asks to install." + signed
        case .none:
            return "This app is the newest build there is (\(newest))." + signed
        }
    }
}
