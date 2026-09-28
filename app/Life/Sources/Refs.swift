import SwiftUI
import UIKit

// A hub id written in a card's text is a tap that opens the thing: a bare id
// is meaningless to read, so it must be a link.
// The console does it in ui.js (REF_ID, mdInline, `#/open/<id>`); this is the
// same rule and the same table for the phone, pinned to the console's by
// shared/ref-cases.json (RefTests.swift).

/// rec-/ask-/cal- plus 8 hex (4 in older ids), or a proposal's stamp.
private let refID = #"(?:rec|ask|cal)-(?:[0-9a-f]{8}|[0-9a-f]{4})|\d{8}-\d{6}-[0-9a-f]{4,8}"#
/// What the scan steps over, then the id itself: a code span, a markdown
/// link, a bare URL — an id inside any of those is not prose. Never a piece
/// of a longer slug ("…-rec-<id>-accepted-…") or of a path.
private let refScan = try! NSRegularExpression(pattern:
    "`([^`]+)`" + #"|\[[^\]]+\]\([^)\s]+\)|https?://[^\s<\]]+|(?<![\w/#-])("# + refID + #")(?![\w-])"#)
private let refWhole = try! NSRegularExpression(pattern: "^(?:" + refID + ")$")

/// The kind a bare id is, by its shape — store.KindOf, the console's refOf.
func refKind(_ id: String) -> String {
    if id.hasPrefix("rec-") { return "rec" }
    if id.hasPrefix("ask-") { return "ask" }
    if id.hasPrefix("cal-") { return "cal" }
    return "action"
}

/// The text with every bare id turned into a markdown link the app opens
/// itself: `[rec-<id>](life://open/rec-<id>)`. Code the owner copies is left
/// alone, except a code span that is ONLY an id — how a session usually
/// writes one.
func linkRefs(_ s: String) -> String {
    guard s.contains("-") else { return s }
    let ns = s as NSString
    var out = "", at = 0
    for m in refScan.matches(in: s, range: NSRange(location: 0, length: ns.length)) {
        var link: String?
        if m.range(at: 2).location != NSNotFound {
            let id = ns.substring(with: m.range(at: 2))
            link = "[\(id)](life://open/\(id))"
        } else if m.range(at: 1).location != NSNotFound {
            let code = ns.substring(with: m.range(at: 1))
            if refWhole.firstMatch(in: code, range: NSRange(location: 0, length: (code as NSString).length)) != nil {
                link = "[`\(code)`](life://open/\(code))"
            }
        }
        guard let link else { continue }
        out += ns.substring(with: NSRange(location: at, length: m.range.location - at)) + link
        at = m.range.location + m.range.length
    }
    return at == 0 ? s : out + ns.substring(from: at)
}

/// Where a tapped id lands: the hub is asked where the thing lives, the tab
/// changes, and that tab's own screen takes what is waiting for it — a rec on
/// its page (RecsView), an ask or a proposal on its card in its session
/// (ThreadsView), a calendar item in its sheet (CalendarView). The console's
/// openRef (app.js) makes the same three reads.
@MainActor @Observable final class RefNav {
    var tab: String?
    var rec: Rec?
    var card: OpenAsk?
    var cal: CalEntry?

    func open(_ url: URL, hub: HubClient) async {
        guard url.scheme == "life", url.host() == "open" else { return }
        let id = url.lastPathComponent
        do {
            switch refKind(id) {
            case "rec":
                let r = try await hub.rec(id)
                land("recs"); rec = r
            case "cal":
                let item = try await hub.calItem(id)
                let day = try await hub.calendar(from: item.day, to: item.day)
                let all = day.days.flatMap(\.entries) + day.overdue + day.anytime
                land("calendar"); cal = all.first { $0.id == id }
            default:
                // A session's own id (a goal note's by-line) opens
                // that session; a proposal's stamp is dated, a session's never is.
                if refKind(id) == "action", id.range(of: #"^\d{8}-\d{6}-"#, options: .regularExpression) == nil {
                    let t = try await hub.thread(id)
                    land("sessions"); card = OpenAsk(thread: t, message: nil)
                    return
                }
                let tid: String? = try await (refKind(id) == "ask" ? hub.ask(id).thread_id : hub.action(id).thread_id)
                guard let tid, !tid.isEmpty else { land("sessions"); return }
                let t = try await hub.thread(tid)
                land("sessions"); card = OpenAsk(thread: t, message: nil, card: id)
            }
        } catch {
            // An id the hub does not know: the page of its kind, like the console.
            land(["rec": "recs", "cal": "calendar"][refKind(id)] ?? "sessions")
        }
    }

    /// A sheet the id was tapped in would sit over the tab it opens.
    private func land(_ t: String) {
        let scene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
        scene?.keyWindow?.rootViewController?.dismiss(animated: true)
        tab = t
    }
}
