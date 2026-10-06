#if targetEnvironment(macCatalyst)
import SwiftUI

/// The desktop app's type sizes (the owner 2026-09-29: "make sure the text is big
/// enough to read on literally everything"). A Mac-idiom Catalyst app draws
/// the text styles at the Mac's desk sizes — body 13, caption and footnote 10
/// — and ignores `.dynamicTypeSize`. These shadow SwiftUI's styles inside this
/// module on the Mac only, so every `.font(.caption)` in the shared sources
/// reads about the console's size without a per-view edit. The phone keeps
/// Apple's sizes (this file is Mac-only).
extension Font {
    // Weights as Apple's styles: regular, headline semibold.
    static var largeTitle: Font { .system(size: 28) }
    static var title: Font { .system(size: 24) }
    static var title2: Font { .system(size: 20) }
    static var title3: Font { .system(size: 17) }
    static var headline: Font { .system(size: 15, weight: .semibold) }
    static var body: Font { .system(size: 15) }
    static var callout: Font { .system(size: 14) }
    static var subheadline: Font { .system(size: 13.5) }
    static var footnote: Font { .system(size: 12.5) }
    static var caption: Font { .system(size: 12) }
    static var caption2: Font { .system(size: 11.5) }
}

// The phone's third grey is too faint to read on the Mac's grey page (Sources'
// "rows · newest row", a rec's review date): tertiary text is secondary here.
extension ShapeStyle where Self == HierarchicalShapeStyle {
    static var tertiary: HierarchicalShapeStyle { HierarchicalShapeStyle.secondary }
}

/// The web console's shapes, for the pages drawn "the same exact layout" as
/// the web (the owner 2026-09-29: "It needs to be similar to the web app"). Values
/// are app.css + skin.css: page `--bg` #f6f7f9, card `--panel` white with a
/// 1px `--line` #e3e5ea hairline at radius 10, `h3` 11.5px/650 caps at .07em.
enum Web {
    // Light, then the console's dark-scheme value (app.css @media dark).
    static let page = pair(0xf6f7f9, 0x0e1014)
    static let panel = pair(0xffffff, 0x171a21)
    static let line = pair(0xe3e5ea, 0x272a33)
    static let lineStrong = pair(0xccd1d9, 0x3a3f4c)
    static let code = pair(0xf3f4f6, 0x1e222b)
    static let muted = pair(0x6b7280, 0x9aa1ad)
    static let accent = pair(0x2563eb, 0x6aa1ff)
    static let fg = pair(0x14161a, 0xe8eaee)
    static let red = pair(0xdc2626, 0xf87171)
    static let amber = pair(0xd97706, 0xfbbf24)
    static let green = pair(0x059669, 0x34d399)
    static let violet = pair(0x7c3aed, 0xa78bfa)
    /// GitHub's five contribution greens, `--gh0`…`--gh4`.
    static let gh = [pair(0xebedf0, 0x161b22), pair(0x9be9a8, 0x0e4429), pair(0x40c463, 0x006d32),
                     pair(0x30a14e, 0x26a641), pair(0x216e39, 0x39d353)]

    private static func pair(_ light: UInt32, _ dark: UInt32) -> Color {
        func ui(_ h: UInt32) -> UIColor {
            UIColor(red: CGFloat(h >> 16 & 0xff) / 255, green: CGFloat(h >> 8 & 0xff) / 255,
                    blue: CGFloat(h & 0xff) / 255, alpha: 1)
        }
        return Color(UIColor { $0.userInterfaceStyle == .dark ? ui(dark) : ui(light) })
    }
}

/// The console's `h3`: a small caps, letter-spaced, grey section heading.
struct WebHeading: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Text(text.uppercased()).font(.system(size: 11.5, weight: .semibold)).tracking(0.8)
            .foregroundStyle(Web.muted).frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension View {
    /// The console's `.card`: white, 1px hairline, radius 10, 13×16 padding.
    func webCard(padding: CGFloat = 14) -> some View {
        self.padding(.vertical, padding - 1).padding(.horizontal, padding + 2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Web.panel, in: RoundedRectangle(cornerRadius: 10))
            .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Web.line, lineWidth: 1) }
    }

    /// The light grey page, content wall to wall with 16 at the edges (the
    /// console's 1100 column went 2026-09-30: the owner wants every page "all the
    /// way to the app walls"; only a chat keeps a measure). `idealWidth` is
    /// not a cap: it answers only the ideal-size pass, which proposes no
    /// width — a page with an adaptive grid (Learn, Engagement) answered
    /// infinity there and trapped in FrameLayout once the 1100 cap that had
    /// been clamping it was gone. (`containerRelativeFrame` was tried: on
    /// Catalyst it sized to something wider than the window, centred.)
    func webPage() -> some View {
        self.padding(16).frame(idealWidth: 1100, maxWidth: .infinity)
            .background(Web.page)
    }
}

/// The console's `button`: bordered white at radius 8, hover a grey fill;
/// `primary` the filled blue one (skin.css). `small` is `button.sm`.
struct WebButtonStyle: ButtonStyle {
    var primary = false
    var small = false
    @Environment(\.isEnabled) private var enabled
    @State private var hover = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: small ? 13 : 13.5, weight: primary ? .semibold : .medium))
            .lineLimit(1)
            .padding(.horizontal, small ? 11 : 14).padding(.vertical, small ? 5 : 6)
            .foregroundStyle(primary ? Color.white : Color.primary)
            .background(primary ? Web.accent : (hover ? Web.code : Web.panel), in: RoundedRectangle(cornerRadius: 8))
            .overlay { if !primary { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) } }
            .brightness(primary && (hover || configuration.isPressed) ? 0.06 : 0)
            .opacity(enabled ? 1 : 0.45)
            .contentShape(Rectangle())
            .onHover { hover = $0 }
    }
}

/// The console's `.pill`: a small squared chip (skin.css radius 6), grey, or
/// in a tint (ThreadRows' `WebPill` is the grey one the sessions draw).
struct WebTag: View {
    let text: String
    var tint: Color? = nil
    init(_ text: String, tint: Color? = nil) { self.text = text; self.tint = tint }
    var body: some View {
        Text(text).font(.system(size: 12, weight: .semibold)).lineLimit(1)
            .padding(.horizontal, 8).padding(.vertical, 2)
            .foregroundStyle(tint ?? Web.muted)
            .background((tint ?? Color.clear).opacity(tint == nil ? 0 : 0.14), in: RoundedRectangle(cornerRadius: 6))
            .background(tint == nil ? Web.code : Color.clear, in: RoundedRectangle(cornerRadius: 6))
    }
}
#endif
