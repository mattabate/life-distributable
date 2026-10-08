import SwiftUI

/// A recommendation's own cell in the chat, so a rec can be answered from
/// the chat without going to the Recs page. Drawn under the reply that filed
/// it, in the rec pill's violet — a thing the owner may answer whenever they
/// like, never a stopped session (red) and never a read (blue). The shared
/// `Card`; its row is
/// the hub's words (`outcomes`: Accept · Decline · Reply), each arming
/// the chat bar with that pick so the decision is recorded and relayed into
/// this chat exactly as one made on the Recs page is — then Dismiss, the
/// silent close (expired, note "dismissed" — HubClient.cardFold),
/// which folds the cell with Reopen inside. A decided rec is the grey card
/// with the verdict: the ✓ of a rec. Pull stays pull — the cell is inside
/// the session and notifies nobody.
struct RecCard: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.armedPicks) private var armedPicks
    let rec: Rec
    /// Arm the chat bar with this rec and a pick ("accepted" | "declined" |
    /// "" for words alone).
    let decide: (String) -> Void
    var reload: () async -> Void = {}
    @State private var busy = false
    @State private var error: String?

    /// Not the hub's `closed` (store.RecStanding): a parked (deferred) rec
    /// keeps its buttons.
    private var open: Bool { !rec.isClosed }
    private var dismissed: Bool { rec.folded == "dismissed" }
    private var stateWord: String { rec.status == "deferred" ? "later" : rec.status }

    var body: some View {
        Card(tint: .purple, mark: open ? "lightbulb.fill" : "lightbulb",
             caption: open ? "recommendation" : stateWord, right: rec.costLabel,
             title: rec.title, text: rec.because ?? rec.detail ?? "", lines: 3,
             meta: AnyView(chips),
             closed: !open && !dismissed,
             // The record of what they chose, and where: the note itself is the
             // "↩ …" message under this cell, from either surface.
             line: decidedLine,
             folded: rec.folded,
             buttons: buttons, busy: busy, error: error)
        .contextMenu {
            if open { Button { decide("") } label: { Label("Reply", systemImage: "arrowshape.turn.up.left") } }
            Button { UIPasteboard.general.string = copyText(rec.title, rec.because ?? rec.detail ?? "") } label: { Label("Copy text", systemImage: "doc.on.doc") }
            Button { UIPasteboard.general.string = rec.id } label: { Label("Copy id", systemImage: "doc.on.doc") }
        }
    }

    private var buttons: [CardButton] {
        guard open else { return rec.reopen == true ? [fold(back: true)] : [] }
        return outcomeButtons(rec.outcomes ?? [], armed: armedPicks["rec:" + rec.id], arm: decide) + [fold(back: false)]
    }

    /// Dismiss / Reopen: the same call the console makes (cardFold), by the owner's hand.
    private func fold(back: Bool) -> CardButton {
        foldButton(.rec, rec.id, back: back, hub: hub, busy: $busy, error: $error, reload: reload)
    }

    private var chips: some View {
        HStack(spacing: 8) {
            RecChip(text: rec.domain, color: recDomainColor(rec.domain))
            RecChip(text: rec.kind, color: .secondary)
            if let m = rec.modelShort { RecChip(text: m, color: .indigo) }
            if rec.status == "deferred", let d = rec.review_on { Text("back \(d)").font(.caption2).foregroundStyle(.tertiary) }
            if rec.status == "deferred", let n = rec.decision_note, !n.isEmpty { Text("parked — \(n)").font(.caption2).foregroundStyle(.tertiary).lineLimit(1) }
        }
    }

    private var decidedLine: String {
        var s = stateWord
        if let by = rec.decided_by, !by.isEmpty { s += " by \(by)" }
        if let at = rec.decided_at { s += " · \(shortAgo(at))" }
        if let n = rec.decision_note, !n.isEmpty { s += " — \(n)" }
        return s
    }
}
