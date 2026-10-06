// The small pills, badges and helpers every row uses: Chip, ChipRow,
// StatusChip, LiveDot, RowAccent, shortAgo, summaryLine. Moved verbatim out
// of ThreadsView.swift.
import SwiftUI

/// "Something is running" marker. Was `ProgressView().controlSize(.mini)`,
/// which on iOS 26 draws nothing at all yet still claims ~4pt of width — so
/// the "running" capsule looked like a blank left inset with the label pushed
/// right. A dot we
/// draw ourselves can't vanish; if the animation is ever dropped it just sits
/// solid.
///
/// The pulse must never be an implicit animation. It was
/// `.animation(.easeInOut.repeatForever(autoreverses: true), value: dim)`, and
/// a repeating animation attached to a view is inherited by that view's OTHER
/// animatable attributes — including its position. While a session runs the
/// transcript appends events and re-scrolls with `withAnimation`, so the live
/// run's header moves; the dot then replayed that move forever and shuttled up
/// and down the whole page at a fixed x.
/// TimelineView drives the opacity off the clock, so there is no animation for
/// a layout change to inherit.
struct LiveDot: View {
    var color: Color = .blue
    var size: CGFloat = 6
    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.6)) { ctx in
            let lit = Int(ctx.date.timeIntervalSinceReferenceDate / 0.6) % 2 == 0
            Circle().fill(color).frame(width: size, height: size).opacity(lit ? 1 : 0.35)
        }
        .accessibilityHidden(true)
    }
}

/// A capsule the hub worded (board `pills`, thread `pill`): the word as sent,
/// the tone picks colour and icon. The console maps the same tones.
struct PillChip: View {
    let pill: Pill
    var body: some View {
        switch pill.tone {
        case "running": Chip(pill.word, lead: .live, tint: .blue)
        case "speaking":
            // This session's card is being spoken aloud right now — green
            // and pulsing, the console's pill.
            Chip(pill.word, lead: .live, tint: .green, fill: 0.16)
        case "waiting":
            // Its line is queued behind another spoken line — amber and
            // still until it is said.
            Chip(pill.word, lead: .dot, tint: .orange, fill: 0.15)
        case "needs": Chip(pill.word, icon: "exclamationmark", tint: .red)
        case "read": Chip(pill.word, icon: "text.alignleft", tint: .accentColor)
        case "install": Chip(pill.word, icon: "arrow.down.circle", tint: .teal)
        case "done": Chip(pill.word, icon: "checkmark")
        default: Chip(pill.word, icon: "bubble.left")
        }
    }
    /// The todo line under a title shares its card pill's colour.
    static func tone(_ pills: [Pill]) -> Color {
        switch pills.first(where: { ["needs", "read", "install"].contains($0.tone) })?.tone {
        case "read": .accentColor
        case "install": .teal
        default: .red
        }
    }
}

struct RowAccent: ViewModifier {
    let color: Color
    let fill: Double
    func body(content: Content) -> some View {
        content.listRowBackground(
            ZStack(alignment: .leading) {
                Color(.secondarySystemGroupedBackground)
                if fill > 0 { color.opacity(fill) }
                Rectangle().fill(color.opacity(color == .secondary ? 0.35 : 1)).frame(width: 4)
            }
        )
    }
}
extension View {
    func rowAccent(_ color: Color, fill: Double = 0) -> some View { modifier(RowAccent(color: color, fill: fill)) }
}

/// One-line row of small capsules (goal, schedule, kind).
struct ChipRow<Content: View>: View {
    @ViewBuilder let content: Content
    var body: some View { HStack(spacing: 6) { content }.lineLimit(1) }
}

/// Every read-only capsule and badge in the app: the text, a tint (nil = grey
/// on quaternary) and a style that fixes font, padding and shape.
///   .pill  session/board pills, schedule and model chips
///   .kind  an approval's kind
///   .tag   UPPERCASE square-ish tag — a rec's domain/status, a run's state
///   .due   UPPERCASE due capsule — Learn's homework line
///   .flag  a tiny capsule inside a line that sets its own font
/// (`PickChip` is the chip you choose; `CalChip` is a calendar block.)
struct Chip: View {
    enum Style { case pill, kind, tag, due, flag }
    /// What sits before the word: an SF Symbol, the pulsing dot, a still dot.
    enum Lead { case icon(String), live, dot }
    let text: String
    let lead: Lead?
    let tint: Color?
    let style: Style
    let fill: Double?
    init(_ text: String, icon: String? = nil, tint: Color? = nil, style: Style = .pill) {
        self.init(text, lead: icon.map { .icon($0) }, tint: tint, style: style)
    }
    init(_ text: String, lead: Lead?, tint: Color? = nil, style: Style = .pill, fill: Double? = nil) {
        self.text = text; self.lead = lead; self.tint = tint; self.style = style; self.fill = fill
    }

    private var pad: (h: CGFloat, v: CGFloat) {
        switch style { case .pill: (7, 3); case .kind, .tag: (6, 2); case .due: (8, 2); case .flag: (5, 1) }
    }
    private var tintFill: Double {
        fill ?? { switch style { case .pill: 0.14; case .due: 0.12; default: 0.15 } }()
    }
    private var font: Font? {
        switch style { case .pill, .kind: .caption2.weight(.medium); case .tag, .due: .caption2.weight(.semibold); case .flag: nil }
    }
    private var shape: AnyShape {
        style == .tag ? AnyShape(RoundedRectangle(cornerRadius: 5)) : AnyShape(Capsule())
    }

    private var ink: Color { tint ?? .secondary }

    @ViewBuilder var body: some View {
        let core = HStack(spacing: { if case .icon = lead { 4 } else { 5 } }()) {
            switch lead {
            case .icon(let name): Image(systemName: name).font(.system(size: 9, weight: .semibold))
            case .live: LiveDot(color: ink)
            case .dot: Circle().fill(ink).frame(width: 6, height: 6)
            case nil: EmptyView()
            }
            Text(text).font(font)
                .textCase(style == .tag || style == .due ? .uppercase : nil)
        }
        .padding(.horizontal, pad.h).padding(.vertical, pad.v)
        if style == .flag {
            // Keeps the line's own ink; the wash is the page's tint.
            core.background(.tint.opacity(tintFill), in: shape)
        } else {
            core.background(tint.map { AnyShapeStyle($0.opacity(tintFill)) } ?? AnyShapeStyle(.quaternary), in: shape)
                .foregroundStyle(ink)
                .lineLimit(style == .pill ? 1 : nil)
        }
    }
}

/// A rec's domain/kind/status/outcome/model tag.
struct RecChip: View {
    let text: String
    var color: Color = .secondary
    var body: some View { Chip(text, tint: color, style: .tag) }
}

/// Left-to-right, wrapping to the next line when the row runs out of width.
/// SwiftUI has no wrapping stack; four interest chips on one HStack run off the
/// side of the phone.
struct FlowRow: Layout {
    var spacing: CGFloat = 6
    /// Where a short row sits inside the width: `.leading` (chips) or `.center`
    /// (a chart's key — the console's `.spend-key { justify-content: center }`).
    var alignment: HorizontalAlignment = .leading

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let rows = lines(width: proposal.width ?? .infinity, subviews: subviews)
        let height = rows.reduce(0) { $0 + $1.height } + spacing * CGFloat(Swift.max(rows.count - 1, 0))
        return CGSize(width: proposal.width ?? rows.map(\.width).max() ?? 0, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        var i = 0
        for row in lines(width: bounds.width, subviews: subviews) {
            let slack = Swift.max(bounds.width - row.width, 0)
            var x = bounds.minX + (alignment == .center ? slack / 2 : alignment == .trailing ? slack : 0)
            for _ in 0..<row.count {
                let size = subviews[i].sizeThatFits(.unspecified)
                subviews[i].place(at: CGPoint(x: x, y: y), anchor: .topLeading, proposal: ProposedViewSize(size))
                x += size.width + spacing
                i += 1
            }
            y += row.height + spacing
        }
    }

    private func lines(width: CGFloat, subviews: Subviews) -> [(count: Int, width: CGFloat, height: CGFloat)] {
        var rows: [(count: Int, width: CGFloat, height: CGFloat)] = []
        var count = 0, w: CGFloat = 0, h: CGFloat = 0
        for v in subviews {
            let size = v.sizeThatFits(.unspecified)
            if count > 0 && w + spacing + size.width > width {
                rows.append((count, w, h))
                count = 0; w = 0; h = 0
            }
            w += (count > 0 ? spacing : 0) + size.width
            h = Swift.max(h, size.height)
            count += 1
        }
        if count > 0 { rows.append((count, w, h)) }
        return rows
    }
}

/// Chips that wrap onto the next line instead of squeezing (three "when"
/// choices plus a picked date do not fit one phone row): a FlowRow at 8pt. The
/// fallback used to be an adaptive grid, which dealt a card's four buttons into
/// two wide, oddly spaced columns.
struct FlowChips<Content: View>: View {
    @ViewBuilder let content: Content
    var body: some View { FlowRow(spacing: 8) { content } }
}

/// A title that opens its page when it has one and is plain text when it does
/// not. Font, line limit and ink set on it reach either.
struct LinkOrText: View {
    let text: String
    let url: URL?
    init(_ text: String, url: URL?) { self.text = text; self.url = url }
    /// A string from the hub: nil, empty or unparsable is plain text.
    init(_ text: String, href: String?) { self.init(text, url: href.flatMap { URL(string: $0) }) }
    var body: some View {
        if let url { Link(text, destination: url) } else { Text(text) }
    }
}

/// First meaningful line of a reply for a row preview: drops the "Did:"
/// header and list markers so the row reads as a sentence, not a transcript.
func summaryLine(_ m: String?) -> String? {
    guard let m else { return nil }
    for raw in m.split(separator: "\n") {
        var line = raw.trimmingCharacters(in: .whitespaces)
        while line.hasPrefix("-") || line.hasPrefix("•") || line.hasPrefix("*") { line = String(line.dropFirst()).trimmingCharacters(in: .whitespaces) }
        for h in ["Did:", "Ask:", "Next:"] where line.hasPrefix(h) { line = String(line.dropFirst(h.count)).trimmingCharacters(in: .whitespaces) }
        if !line.isEmpty { return line }
    }
    return nil
}

/// A whole card body flattened onto one paragraph, for the description under a
/// session row (board `detail`). Blank lines go, list markers become "• " and headings
/// lose their hashes, so a bulleted ask reads as prose in the three lines the
/// row clamps it to instead of exploding its height. Same flattening as the
/// console's `mdPreview`, so both surfaces print the same sentence.
func previewLine(_ s: String?) -> String? {
    guard let s else { return nil }
    let flat = s.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespaces) }
        .filter { !$0.isEmpty }
        .map { raw -> String in
            var line = raw
            while line.hasPrefix("#") { line = String(line.dropFirst()) }
            line = line.trimmingCharacters(in: .whitespaces)
            for p in ["- ", "* ", "• "] where line.hasPrefix(p) {
                line = "• " + line.dropFirst(p.count)
                break
            }
            return line
        }.joined(separator: "  ")
    return flat.isEmpty ? nil : flat
}
