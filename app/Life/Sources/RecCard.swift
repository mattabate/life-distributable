import SwiftUI

/// A recommendation's card in its chat, violet: a long-lived task for the
/// owner, mirrored on the Recs page. The shared `Card`; its row is the hub's
/// words (`outcomes`: Accept · Decline · Reply), each arming the chat bar
/// with that pick, and Send posts the Recs page's own decide / reply, so a
/// decision in either place is the same row. Then Hide: off the chat and the
/// Sessions list, still open on Recs, whose Show in chat brings it back. A
/// decided rec is its row with the verdict, Reopen inside. Open, it stays
/// until the owner answers it — never in Your turn.
struct RecCard: View {
    @Environment(HubClient.self) private var hub
    @Environment(BoardStore.self) private var store
    @Environment(\.armedPicks) private var armedPicks
    let rec: Rec
    /// Arm the chat bar with this rec and a pick ("accepted" | "declined" |
    /// "" for words alone).
    let decide: (String) -> Void
    var reload: () async -> Void = {}
    @State private var busy = false
    @State private var error: String?

    private var open: Bool { !rec.isClosed }
    private var dismissed: Bool { rec.folded == "dismissed" }

    var body: some View {
        Card(tint: .purple, mark: "lightbulb.fill", caption: "recommendation", right: rec.costLabel,
             title: rec.title, text: rec.because ?? rec.detail ?? "", lines: 3,
             line: rec.decision_note ?? "",
             folded: open ? nil : dismissed ? "✕ dismissed" : (rec.status == "declined" ? "✕ " : "✓ ") + recStateWord(rec.status),
             buttons: buttons, busy: busy, error: error)
        .contextMenu {
            if open { Button { decide("") } label: { Label("Reply", systemImage: "arrowshape.turn.up.left") } }
            Button { UIPasteboard.general.string = copyText(rec.title, rec.because ?? rec.detail ?? "") } label: { Label("Copy text", systemImage: "doc.on.doc") }
        }
        .id(rec.id)
    }

    private var buttons: [CardButton] {
        guard open else { return rec.reopen == true ? [fold(back: true)] : [] }
        let id = rec.id
        let hide = CardButton(label: "Hide") {
            Task {
                await cardAct(busy: $busy, error: $error, reload: reload) {
                    _ = try await hub.hideRec(id, hidden: true)
                    store.setHidden(id, true)
                }
            }
        }
        return outcomeButtons(rec.outcomes ?? [], armed: armedPicks["rec:" + rec.id], arm: decide) + [hide]
    }

    /// Reopen on a decided rec: the same call the console makes (cardFold).
    private func fold(back: Bool) -> CardButton {
        foldButton(.rec, rec.id, back: back, hub: hub, busy: $busy, error: $error, reload: reload)
    }
}

/// A rec's status in the console's words (ui.js REC_STATE_WORD): an open one
/// reads "open", a parked one "later", never the hub's raw "deferred".
func recStateWord(_ s: String) -> String {
    switch s { case "proposed": "open"; case "deferred": "later"; default: s }
}
