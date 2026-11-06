import SwiftUI

/// What a chat shows of a rec: one violet line that it was filed, a small
/// notification the owner can dismiss. Recs are long-lived and theirs to do
/// any time, so the card and its Accept · Decline · Reply live on the Recs
/// page (the title opens it), never in Your turn; the open-cards view and
/// "Show full chat" draw the same line. × marks an open one seen
/// (`POST /recs/{id}/seen`).
struct RecNotice: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.openURL) private var openURL
    let rec: Rec
    var reload: () async -> Void = {}
    @State private var busy = false

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "lightbulb.fill").foregroundStyle(.purple).font(.caption)
            Button {
                if let u = URL(string: "life://open/\(rec.id)") { openURL(u) }
            } label: {
                (Text("Recommendation filed: ").foregroundStyle(.secondary) + Text(rec.title).foregroundStyle(.purple))
                    .font(.callout).lineLimit(1).truncationMode(.tail)
            }
            .buttonStyle(.plain)
            Spacer(minLength: 4)
            if !rec.isClosed && (rec.seen_at ?? "").isEmpty {
                Button {
                    busy = true
                    Task { _ = try? await hub.recSeen(rec.id); await reload(); busy = false }
                } label: { Image(systemName: "xmark").font(.caption.weight(.semibold)).foregroundStyle(.secondary) }
                .buttonStyle(.plain).disabled(busy).accessibilityLabel("Dismiss")
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 7)
        .background(Color.purple.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.purple.opacity(0.25)))
        // The cards' own inset (Card), so the line is as wide as they are.
        .padding(.horizontal)
        .id(rec.id)
    }
}
