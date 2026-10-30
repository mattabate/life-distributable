import SwiftUI
import UIKit

/// Chat-bubble text with light markdown: inline **bold**, *italic*, `code`,
/// ~~strike~~, links; block-level bullets ("- ", "* ", "• "), numbered
/// lists ("1. "), and "# headings" (rendered bold, SAME size — no differing
/// text sizes). Everything else is plain text;
/// unknown syntax falls back to the literal line, never to a blank.
///
/// Rendered as ONE UITextView, not a VStack of Texts: SwiftUI selection stops
/// at each `Text`, so hold-to-select could only ever grab a single line and a
/// drag across a paragraph selected nothing. One text view
/// = one selection range over the whole block, with the system Copy menu.
/// Bullet hanging indents move from the old HStack to real tab stops.
struct StyledText: View {
    let text: String
    var style: UIFont.TextStyle = .callout
    /// Explicit — a UITextView does not inherit SwiftUI's `.foregroundStyle`.
    var color: Color = .primary
    /// Whole block semibold (ask/card titles, the text copied most often).
    var bold: Bool = false
    /// Cut to this many lines with "…" (a rec card's `because`), still
    /// selectable — a `Text` with `.lineLimit` cannot be dragged over on the
    /// Mac (the owner 2026-09-30: "just highlight it and then right click and copy").
    var lines: Int? = nil
    /// The Mac's point size, when the text stands where a SwiftUI `Text` in
    /// a MacFonts size stood: UIKit's own styles are the Mac's small desk
    /// sizes (subheadline 11), which MacFonts.swift only lifts for SwiftUI.
    var macSize: CGFloat? = nil
    /// Re-renders the attributed string when the system text size changes.
    @Environment(\.dynamicTypeSize) private var typeSize
    /// Inside a component that can be chatted about (`.chatAbout`), the text
    /// selection menu gets "Chat about this" next to Copy — long-pressing the
    /// words themselves selects text, so the component's own hold-down menu
    /// can never appear over a paragraph.
    @Environment(\.chatHook) private var chatHook

    var body: some View {
        // A fenced block is its own box with a Copy button, so a text holding
        // one is a stack: text view, box, text view. Anything else — nearly
        // every message — stays the single text view it was.
        // A line that is only a hub file is the file itself (`blobLine`).
        // A pipe table is a grid of its own too, unless the text is a
        // clamped preview (`lines`), where it stays one stacked text view.
        if text.contains("```") || text.contains("/api/v1/blobs/")
            || (lines == nil && (text.hasPrefix("|") || text.contains("\n|") || text.contains("\n |"))) {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(Array(mdSegments(text).enumerated()), id: \.offset) { _, seg in
                    switch seg {
                    case .text(let t): SelectableText(attributed: cached(t), tint: UIColor(color), chat: chatHook, lines: lines)
                    case .code(let code, let lang): CodeBlock(code: code, lang: lang, style: style)
                    case .files(let files): FileStrip(files: files)
                    case .table(let rows, let header, let align): tableView(rows, header: header, align: align)
                    }
                }
            }
        } else {
            SelectableText(attributed: cached(text), tint: UIColor(color), chat: chatHook, lines: lines)
        }
    }

    /// `build()` walks the text twice — once per line for the block layout,
    /// once per run for inline markdown — and it ran on EVERY body evaluation:
    /// every 3s poll, every keystroke that resized the composer, every scroll
    /// that re-laid out a bubble, for every message on screen. The output only
    /// depends on the five inputs below, so it is built once and kept.
    private static let cache = NSCache<NSString, NSAttributedString>()
    private func cached(_ text: String) -> NSAttributedString {
        let key = "\(style.rawValue)|\(bold)|\(color)|\(typeSize)|\(macSize ?? 0)|\(text)" as NSString
        if let hit = Self.cache.object(forKey: key) { return hit }
        let built = Perf.time("StyledText.build") { build(text) }
        Self.cache.countLimit = 400
        Self.cache.setObject(built, forKey: key)
        return built
    }

    /// Marker column: 14pt wide, right-aligned, 5pt gap — the geometry the
    /// old HStack(spacing: 5) { Text(marker).frame(minWidth: 14) } produced.
    private static let markerWidth: CGFloat = 14
    private static let hang: CGFloat = 19

    private func baseFont() -> UIFont {
        #if targetEnvironment(macCatalyst)
        let plain = macSize.map { UIFont.systemFont(ofSize: $0) } ?? UIFont.preferredFont(forTextStyle: style)
        #else
        let plain = UIFont.preferredFont(forTextStyle: style)
        #endif
        return bold ? plain.bolded() : plain
    }

    private func build(_ text: String) -> NSAttributedString {
        let base = baseFont()
        let heading = base.bolded()
        let ui = UIColor(color)
        var paras: [NSAttributedString] = []
        var blank = false
        let lines = text.components(separatedBy: "\n")
        var i = 0
        while i < lines.count {
            let raw = lines[i]; i += 1
            // Collapse runs of blank lines into one small gap.
            let trimmedAll = raw.trimmingCharacters(in: .whitespaces)
            if trimmedAll.isEmpty {
                #if !targetEnvironment(macCatalyst)
                if !blank, !paras.isEmpty { paras.append(NSAttributedString(string: " ", attributes: [.font: base, .foregroundColor: ui])) }
                #endif
                blank = true; continue
            }
            // On the Mac a blank line is a half-line gap above the next
            // paragraph, not a whole empty line (the owner 2026-09-29, on Goals:
            // "the markdown formatting is kind of weird and bad").
            let gapBefore: CGFloat = blank && !paras.isEmpty ? base.pointSize * 0.55 : 0
            blank = false
            // A pipe table that reaches here sits in a clamped preview (body
            // cuts every other one out as a grid): one record per row.
            if let (seg, next) = mdTable(lines, at: i - 1), case .table(let rows, let header, _) = seg {
                paras.append(contentsOf: stacked(rows, header: header, base: base, color: ui))
                i = next
                continue
            }
            let leading = raw.prefix { $0 == " " || $0 == "\t" }.reduce(0) { $0 + ($1 == "\t" ? 4 : 1) }
            var s = Substring(trimmedAll)
            var marker: String? = nil
            var indent = CGFloat(leading / 2) * 14
            var font = base
            if let m = s.firstMatch(of: /^([-*•]|\d{1,2}[.)])\s+/) {
                let tok = String(m.1)
                marker = tok.first!.isNumber ? tok.replacingOccurrences(of: ")", with: ".") : "•"
                s = s[m.range.upperBound...]
            } else if let m = s.firstMatch(of: /^#{1,6}\s+/) {
                s = s[m.range.upperBound...]
                font = heading
            } else if indent > 0 {
                // Continuation of a list item: keep its hanging indent.
                indent += Self.hang
            }
            let p = NSMutableParagraphStyle()
            p.lineBreakMode = .byWordWrapping
            p.paragraphSpacing = 2          // the old VStack(spacing: 2)
            p.firstLineHeadIndent = indent
            #if targetEnvironment(macCatalyst)
            p.paragraphSpacingBefore = gapBefore
            p.lineSpacing = 1.5
            #endif
            let line = NSMutableAttributedString()
            if let mk = marker {
                p.headIndent = indent + Self.hang
                p.tabStops = [NSTextTab(textAlignment: .right, location: indent + Self.markerWidth),
                              NSTextTab(textAlignment: .left, location: indent + Self.hang)]
                line.append(NSAttributedString(string: "\t\(mk)\t", attributes: [.font: font, .foregroundColor: ui]))
            } else {
                p.headIndent = indent
            }
            line.append(inline(String(s), base: font, color: ui))
            line.addAttribute(.paragraphStyle, value: p, range: NSRange(location: 0, length: line.length))
            paras.append(line)
        }
        let out = NSMutableAttributedString()
        for (i, para) in paras.enumerated() {
            if i > 0 { out.append(NSAttributedString(string: "\n", attributes: [.font: base])) }
            out.append(para)
        }
        return out
    }

    /// A pipe table as a real grid. Tab stops in the prose's text view let a
    /// cell wider than its stop push the rest of its row one stop over, so
    /// headers sat over the wrong values, and the last column wrapped a word
    /// per line in whatever width was left. Now each cell wraps by words
    /// inside its own column, widths shared out the way a browser's table
    /// does (MdTableLayout), with the console's rules under the header and
    /// between rows.
    ///
    /// A table whose columns cannot fit the phone's text column even at one
    /// word each (five columns on a read card ran the last one off the edge)
    /// stacks instead: each row's first cell bold on its own line, the rest
    /// as "Header value · Header value" under it.
    @ViewBuilder
    private func tableView(_ rows: [[String]], header: Bool, align: [NSTextAlignment]) -> some View {
        let model = tableModel(rows, header: header)
        if model.least <= Self.tableBudget {
            MdTableGrid(model: model, align: align, rule: color)
        } else {
            SelectableText(attributed: model.stacked, tint: UIColor(color), chat: chatHook)
        }
    }

    private static let tableCache = NSCache<NSString, MdTableModel>()
    private func tableModel(_ rows: [[String]], header: Bool) -> MdTableModel {
        let key = "\(style.rawValue)|\(bold)|\(color)|\(typeSize)|\(macSize ?? 0)|\(header)|\(rows)" as NSString
        if let hit = Self.tableCache.object(forKey: key) { return hit }
        let base = baseFont(), ui = UIColor(color)
        let cols = rows.map(\.count).max() ?? 0
        let cells = rows.enumerated().map { (r, row) in
            (0..<cols).map { c in inline(c < row.count ? row[c] : "", base: header && r == 0 ? base.bolded() : base, color: ui) }
        }
        let flat = NSMutableAttributedString()
        for (k, p) in stacked(rows, header: header, base: base, color: ui).enumerated() {
            if k > 0 { flat.append(NSAttributedString(string: "\n", attributes: [.font: base])) }
            flat.append(p)
        }
        let model = MdTableModel(cells: cells, header: header, stacked: flat)
        Self.tableCache.countLimit = 100
        Self.tableCache.setObject(model, forKey: key)
        return model
    }

    /// The phone's text column inside a card; the Mac's columns are wide.
    private static var tableBudget: CGFloat {
        #if targetEnvironment(macCatalyst)
        return .greatestFiniteMagnitude
        #else
        let w = UIApplication.shared.connectedScenes.lazy
            .compactMap { ($0 as? UIWindowScene)?.screen.bounds.width }.first ?? 390
        return w - 80
        #endif
    }
    /// The too-wide table, one record per row: first cell bold, then the
    /// other cells after their header in the secondary colour.
    private func stacked(_ rows: [[String]], header: Bool, base: UIFont, color: UIColor) -> [NSAttributedString] {
        let heads = header ? rows[0] : []
        let body = header ? Array(rows.dropFirst()) : rows
        let label = UIColor.secondaryLabel
        var out: [NSAttributedString] = []
        for (r, row) in body.enumerated() {
            let p = NSMutableParagraphStyle()
            p.lineBreakMode = .byWordWrapping
            p.paragraphSpacingBefore = r > 0 ? 6 : 0
            let title = NSMutableAttributedString(attributedString: inline(row.first ?? "", base: base.bolded(), color: color))
            title.addAttribute(.paragraphStyle, value: p, range: NSRange(location: 0, length: title.length))
            out.append(title)
            let rest = NSMutableAttributedString()
            for c in 1..<max(row.count, 1) where !row[c].isEmpty {
                // No-break spaces: a line ends on the dot, never starts with
                // it, and "Total -$12.00" never splits.
                if rest.length > 0 { rest.append(NSAttributedString(string: "\u{00A0}\u{00A0}·  ", attributes: [.font: base, .foregroundColor: label])) }
                if c < heads.count, !heads[c].isEmpty {
                    rest.append(NSAttributedString(string: heads[c].replacingOccurrences(of: " ", with: "\u{00A0}") + "\u{00A0}",
                                                   attributes: [.font: base, .foregroundColor: label]))
                }
                // A word joiner keeps "+$10.00" from breaking after the sign.
                let value = ["+", "-", "−"].reduce(row[c]) { $0.replacingOccurrences(of: $1 + "$", with: $1 + "\u{2060}$") }
                rest.append(inline(value, base: base, color: color))
            }
            if rest.length > 0 {
                let q = NSMutableParagraphStyle()
                q.lineBreakMode = .byWordWrapping
                rest.addAttribute(.paragraphStyle, value: q, range: NSRange(location: 0, length: rest.length))
                out.append(rest)
            }
        }
        return out
    }

    /// Inline markdown, run by run, so every span carries a real UIFont
    /// (SwiftUI-scoped attributes are dropped on the way into UIKit).
    private func inline(_ s: String, base: UIFont, color: UIColor) -> NSAttributedString {
        // A bare hub id (rec-…, ask-…, cal-…, a proposal's stamp) is a link
        // the app opens itself — Refs.swift.
        let parsed = (try? AttributedString(markdown: escapingStrayAsterisks(linkRefs(s)),
                                            options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
        let out = NSMutableAttributedString()
        for run in parsed.runs {
            let intent = run.inlinePresentationIntent ?? []
            var font = base
            // Code spans: monospaced at the same size so nothing jumps.
            #if targetEnvironment(macCatalyst)
            // On the Mac a code span is a point smaller on a faint wash, so
            // an id or a path reads as a token, not a wide run of type.
            if intent.contains(.code) { font = UIFont.monospacedSystemFont(ofSize: base.pointSize - 1.5, weight: .regular) }
            #else
            if intent.contains(.code) { font = UIFont.monospacedSystemFont(ofSize: base.pointSize, weight: .regular) }
            #endif
            var traits = font.fontDescriptor.symbolicTraits
            if intent.contains(.stronglyEmphasized) { traits.insert(.traitBold) }
            if intent.contains(.emphasized) { traits.insert(.traitItalic) }
            if traits != font.fontDescriptor.symbolicTraits, let d = font.fontDescriptor.withSymbolicTraits(traits) {
                font = UIFont(descriptor: d, size: font.pointSize)
            }
            var attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color]
            if intent.contains(.strikethrough) { attrs[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
            if let link = run.link { attrs[.link] = link }
            #if targetEnvironment(macCatalyst)
            if intent.contains(.code) { attrs[.backgroundColor] = UIColor.label.withAlphaComponent(0.06) }
            #endif
            out.append(NSAttributedString(string: String(parsed[run.range].characters), attributes: attrs))
        }
        return out
    }
}

/// Inline markdown for the one-liners: row titles, card previews, anything
/// truncated with `.lineLimit`. StyledText is the full renderer but it is a
/// UITextView per instance — too heavy for a list row and it cannot be line
/// limited — so these get a plain SwiftUI `Text` carrying the same **bold**,
/// *italic*, `code` and [link](url) runs, instead of literal asterisks.
///
/// Block syntax is left alone: a leading "- " in a preview stays as it is,
/// exactly as it looks today, and bad syntax falls back to the raw string.
func md(_ s: String) -> AttributedString {
    (try? AttributedString(markdown: escapingStrayAsterisks(s), options: .init(
        allowsExtendedAttributes: false,
        interpretedSyntax: .inlineOnlyPreservingWhitespace,
        failurePolicy: .returnPartiallyParsedIfPossible))) ?? AttributedString(s)
}

/// Asterisks that were never meant as emphasis, escaped so they survive the
/// parser. CommonMark says the `*` in `Bash(npm install:*)` opens emphasis —
/// it is preceded by punctuation and followed by punctuation, which makes it
/// left-flanking — so the thread message "you have Bash(npm install:*) and
/// Bash(npm run:*)" rendered on the phone as `Bash(npm install:)` followed by
/// an italic run, with BOTH literal asterisks deleted. `ops/*.py and src/*.go`
/// mangles the same way, and so does `2**32 and 2**64`. Losing characters out
/// of a chat log is worse than losing italics, so a run of asterisks only
/// counts as syntax when it opens at a word boundary (start of line,
/// whitespace, or an opening bracket) or closes a run that did; anything else
/// is escaped and comes out literal.
///
/// Code spans are stepped over untouched: inside backticks a backslash is a
/// backslash, not an escape, so escaping there would print `\*`.
func escapingStrayAsterisks(_ s: String) -> String {
    guard s.contains("*") || s.contains("~") else { return s }
    let chars = Array(s)
    var out = ""
    var open = 0        // emphasis runs opened at a word boundary, still unclosed
    var fence = 0       // length of the backtick run holding a code span open
    var i = 0
    while i < chars.count {
        let c = chars[i]
        if fence == 0, c == "\\", i + 1 < chars.count {
            out.append(c); out.append(chars[i + 1]); i += 2; continue   // already escaped
        }
        if c == "`" {
            var n = 0
            while i + n < chars.count, chars[i + n] == "`" { n += 1 }
            if fence == 0 { fence = n } else if fence == n { fence = 0 }
            out += String(repeating: "`", count: n); i += n; continue
        }
        if c == "*", fence == 0 {
            var n = 0
            while i + n < chars.count, chars[i + n] == "*" { n += 1 }
            // Off the ends of the string, treat the neighbour as whitespace.
            let before: Character = i > 0 ? chars[i - 1] : " "
            let after: Character = i + n < chars.count ? chars[i + n] : " "
            if !before.isWhitespace, open > 0 {
                open -= 1
                out += String(repeating: "*", count: n)
            } else if before.isWhitespace || "([{".contains(before), !after.isWhitespace {
                open += 1
                out += String(repeating: "*", count: n)
            } else {
                out += String(repeating: "\\*", count: n)
            }
            i += n; continue
        }
        // A lone `~` is "about" ("~$2,600"), never strikethrough: Apple's
        // parser struck a pair of them through, crossing out whole sentences.
        // The console has no strikethrough at all, so the tilde stays literal
        // on both; `~~like this~~` is left to the parser.
        if c == "~", fence == 0 {
            var n = 0
            while i + n < chars.count, chars[i + n] == "~" { n += 1 }
            out += n == 1 ? "\\~" : String(repeating: "~", count: n)
            i += n; continue
        }
        out.append(c); i += 1
    }
    return out
}

/// The fold line: past this a chat bubble MAY fold. This is the cheap gate,
/// counted on the text so both surfaces agree; the console's isLongText
/// (ui.js) is the same rule: >18 non-blank lines or >1600 characters.
/// Whether it does fold is FoldedText's call, on the rendered height.
let longLines = 18, longChars = 1600
func longLineCount(_ s: String) -> Int {
    s.split(separator: "\n", omittingEmptySubsequences: false)
        .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.count
}
func isLongText(_ s: String) -> Bool { longLineCount(s) > longLines || s.count > longChars }

/// The fold's second rule: a short message that wraps to many lines would
/// otherwise offer a button that reveals almost nothing. Past `isLongText` a
/// body clamps to `foldClamp` points with its fade, but the button exists only
/// when opening it would show at least `foldMinLines` more lines, and says how
/// many. Measured on the rendered height (the text's own full height against
/// the clamp, in line-heights) because the text's newlines say nothing about
/// how many lines it wraps to. Under the line the body is shown whole. The
/// console's fitFolds (ui.js) is the same rule.
let foldMinLines = 5
/// Twice the old 320, the console's 30 line-heights.
let foldClamp: CGFloat = 640

/// A long body behind the fold: the clamped text, its fade, and, when there
/// is enough behind it, the one button that pops it open to full height and
/// shut again, drawn by the caller's `button` so a tinted card keeps its own
/// style. `fadeTo` is the box's own colour; nil masks to clear (a tinted
/// card). The console's askBody is the same.
struct FoldedText<Fold: View>: View {
    let text: String
    var style: UIFont.TextStyle = .callout
    var color: Color = .primary
    var macSize: CGFloat? = nil
    var fadeTo: Color? = nil
    @ViewBuilder var button: (_ label: String, _ tap: @escaping () -> Void) -> Fold
    /// The whole text's height at this width; nil until the first layout,
    /// and the fold is assumed until then so a long one never flashes open.
    @State private var full: CGFloat? = nil
    @State private var open = false

    /// The font `StyledText.build` sets the body in, so a line is its line.
    private var lineHeight: CGFloat {
        #if targetEnvironment(macCatalyst)
        if let macSize { return UIFont.systemFont(ofSize: macSize).lineHeight }
        #endif
        return UIFont.preferredFont(forTextStyle: style).lineHeight
    }
    /// Lines behind the fold once measured.
    private var hidden: Int { full.map { Int((($0 - foldClamp) / lineHeight).rounded()) } ?? Int.max }
    private var folds: Bool { hidden >= foldMinLines }
    private var clamped: Bool { folds && !open }

    var body: some View {
        StyledText(text: text, style: style, color: color, macSize: macSize)
            // The text view answers its own full height whatever the frame
            // below proposes, so this is the whole body's height.
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { full = $0 }
            .frame(maxHeight: clamped ? foldClamp : nil, alignment: .top)
            .clipped()
            .overlay(alignment: .bottom) {
                if clamped, let fadeTo {
                    LinearGradient(colors: [.clear, fadeTo], startPoint: .top, endPoint: .bottom)
                        .frame(height: 64).allowsHitTesting(false)
                }
            }
            .mask {
                if clamped, fadeTo == nil {
                    LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black, location: 0.9), .init(color: .clear, location: 1)],
                                   startPoint: .top, endPoint: .bottom)
                } else {
                    Color.black
                }
            }
        if folds {
            button(open ? "Show less" : "Show \(hidden) more lines") { open.toggle() }
        }
    }
}

/// A pipe-table row "| a | b |" (outer pipes optional) → its trimmed cells,
/// nil when the line has no pipe at all. Splits on pipes outside code spans,
/// so "`a | b`" is one cell. The console's ui.js mdTableRow is the same rule.
func mdTableRow(_ line: String) -> [String]? {
    let t = line.trimmingCharacters(in: .whitespaces)
    guard t.contains("|") else { return nil }
    var cells: [String] = [], cur = "", fence = false
    for c in t {
        if c == "`" { fence.toggle() }
        if c == "|", !fence { cells.append(cur); cur = "" } else { cur.append(c) }
    }
    cells.append(cur)
    if t.hasPrefix("|") { cells.removeFirst() }
    if t.hasSuffix("|"), !cells.isEmpty { cells.removeLast() }
    return cells.map { $0.trimmingCharacters(in: .whitespaces) }
}

/// "|---|:--:|---:|" → one alignment per column, nil when the line is not a
/// separator row. The grid aligns each column by it, as the console does.
func mdTableSeparator(_ line: String) -> [NSTextAlignment]? {
    guard let cells = mdTableRow(line), !cells.isEmpty else { return nil }
    var out: [NSTextAlignment] = []
    for c in cells {
        guard c.wholeMatch(of: /:?-+:?/) != nil else { return nil }
        out.append(c.hasSuffix(":") ? (c.hasPrefix(":") ? .center : .right) : .left)
    }
    return out
}

/// A text cut at its fenced blocks. The console's ui.js md() is the same
/// rule: a line of three or more backticks plus an optional language word
/// opens, a line of only backticks at least as long closes, an unclosed fence
/// runs to the end (a reply still streaming in), and a fence indented under a
/// list item gives that indent up on every line.
enum MdSegment: Equatable {
    case text(String)
    case code(String, lang: String)
    /// Hub blobs on lines in a row (blank lines between are fine): one strip
    /// of small tiles, pictures or file chips.
    case files([MdFile])
    /// A pipe table: its rows (the header first when `header`), and one
    /// alignment per column from the separator row.
    case table([[String]], header: Bool, align: [NSTextAlignment])
}

/// A pipe table starting at `lines[i]`: the header row, an optional
/// separator, then every pipe row after it. Returns the segment and the
/// index past the table, or nil when `lines[i]` does not open one.
func mdTable(_ lines: [String], at i: Int) -> (MdSegment, Int)? {
    let t = lines[i].trimmingCharacters(in: .whitespaces)
    guard t.hasPrefix("|"), let first = mdTableRow(t), mdTableSeparator(t) == nil else { return nil }
    var rows = [first], j = i + 1
    var align: [NSTextAlignment] = []
    if j < lines.count, let a = mdTableSeparator(lines[j]) { align = a; j += 1 }
    while j < lines.count, lines[j].trimmingCharacters(in: .whitespaces).hasPrefix("|"), let r = mdTableRow(lines[j]) {
        if mdTableSeparator(lines[j]) == nil { rows.append(r) }
        j += 1
    }
    return (.table(rows, header: !align.isEmpty, align: align), j)
}

struct MdFile: Equatable, Hashable {
    let ref: String
    /// The link's [text], drawn under the tile.
    let caption: String
}

/// A line that is only a hub file - `![caption](.../api/v1/blobs/<ref>)`,
/// `[name](...)` or the bare URL - else nil. The line IS the picture, fetched
/// with the token from whichever hub this is. The console's twin is
/// `blobLine` in ui.js, same pattern.
func blobLine(_ line: String) -> MdFile? {
    guard let m = line.wholeMatch(of: /\s*(?:!?\[([^\]]*)\]\()?(?:https?:\/\/[^\s\/)]+)?\/api\/v1\/blobs\/([\w.\/-]+)\)?\s*/),
          !m.2.contains("..") else { return nil }
    return MdFile(ref: String(m.2), caption: String(m.1 ?? "").trimmingCharacters(in: .whitespaces))
}
func blobLineRef(_ line: String) -> String? { blobLine(line)?.ref }

/// The strip: small same-size tiles, wrapping, each captioned, a click (tap)
/// opening the whole file. The console's `filesHTML`.
struct FileStrip: View {
    let files: [MdFile]
    #if targetEnvironment(macCatalyst)
    static let tile = CGSize(width: 240, height: 150)
    #else
    static let tile = CGSize(width: 160, height: 104)
    #endif
    var body: some View {
        FlowRow(spacing: 10) {
            ForEach(files, id: \.self) { f in
                VStack(alignment: .leading, spacing: 3) {
                    if isImageRef(f.ref) {
                        BlobImage(ref: f.ref, maxHeight: Self.tile.height)
                            .frame(width: Self.tile.width, height: Self.tile.height)
                            .background(Color.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 8))
                            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Color.primary.opacity(0.1)))
                    } else if isAudioRef(f.ref) {
                        BlobAudio(ref: f.ref)
                    } else if isHTMLRef(f.ref) {
                        // A page is a file for a browser, never a viewer here.
                        BlobHTML(ref: f.ref, title: f.caption)
                    } else {
                        BlobDocument(ref: f.ref, title: f.caption)
                    }
                    if !f.caption.isEmpty && (isImageRef(f.ref) || isAudioRef(f.ref) || isHTMLRef(f.ref)) {
                        Text(f.caption).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            .frame(maxWidth: Self.tile.width, alignment: .leading)
                    }
                }
            }
        }
    }
}

/// A pipe table's cells, rendered once, with each column's narrowest (its
/// longest word) and widest (its longest cell on one line) widths.
final class MdTableModel {
    let cells: [[AttributedString]]
    let header: Bool
    let minW: [CGFloat], maxW: [CGFloat]
    /// The one-record-per-row text, for a phone too narrow for the grid.
    let stacked: NSAttributedString
    static let gap: CGFloat = 16
    /// The grid at its narrowest: every column one word wide.
    var least: CGFloat { minW.reduce(0, +) + Self.gap * CGFloat(max(minW.count - 1, 0)) }

    init(cells ns: [[NSAttributedString]], header: Bool, stacked: NSAttributedString) {
        let cols = ns.first?.count ?? 0
        let width = { (s: NSAttributedString) in ceil(s.size().width) + 1 }
        // A word longer than this (a URL as its own text) breaks inside
        // itself rather than widening its column past the page.
        let wordCap: CGFloat = 220
        let words = { (s: NSAttributedString) -> CGFloat in
            var widest: CGFloat = 0
            let str = s.string as NSString
            str.enumerateSubstrings(in: NSRange(location: 0, length: str.length), options: .byWords) { _, _, enclosing, _ in
                let w = width(s.attributedSubstring(from: enclosing).trimmed)
                widest = max(widest, w)
            }
            return min(widest, wordCap)
        }
        minW = (0..<cols).map { c in ns.map { words($0[c]) }.max() ?? 0 }
        maxW = (0..<cols).map { c in ns.map { width($0[c]) }.max() ?? 0 }
        let sole = ns.map { $0.map(MdTableModel.soleLink) }
        links = sole
        hasLinks = ns.contains { $0.contains { MdTableModel.linkCount($0) > 0 } }
        cells = zip(ns, sole).map { row, s in zip(row, s).map { MdTableModel.swiftUI($0, keepLink: $1 == nil) } }
        self.header = header
        self.stacked = stacked
    }

    /// A cell whose links all go one place: the whole cell is that link, so a
    /// tap anywhere in it opens the link, like a hyperlink in an email.
    let links: [[URL?]]
    /// Any link at all: such a table is not selectable, since selectable
    /// text on the Mac answers a click a line above the drawn link.
    let hasLinks: Bool

    private static func url(_ v: Any?) -> URL? {
        if let u = v as? URL { return u }
        if let s = v as? String { return URL(string: s) }
        return nil
    }

    private static func linkCount(_ s: NSAttributedString) -> Int {
        var seen = Set<URL>()
        s.enumerateAttribute(.link, in: NSRange(location: 0, length: s.length)) { v, _, _ in
            if let u = url(v) { seen.insert(u) }
        }
        return seen.count
    }

    private static func soleLink(_ s: NSAttributedString) -> URL? {
        guard linkCount(s) == 1 else { return nil }
        var out: URL?
        s.enumerateAttribute(.link, in: NSRange(location: 0, length: s.length)) { v, _, stop in
            if let u = url(v) { out = u; stop.pointee = true }
        }
        return out
    }

    /// UIKit runs → SwiftUI runs: a `Text` ignores a UIFont attribute. A
    /// sole-link cell drops the run's link (still underlined): the cell's tap
    /// opens it, so the glyphs never compete with the cell for the click.
    private static func swiftUI(_ s: NSAttributedString, keepLink: Bool) -> AttributedString {
        var out = AttributedString()
        s.enumerateAttributes(in: NSRange(location: 0, length: s.length)) { attrs, range, _ in
            var run = AttributedString(s.attributedSubstring(from: range).string)
            if let f = attrs[.font] as? UIFont { run.font = Font(f as CTFont) }
            if let c = attrs[.foregroundColor] as? UIColor { run.foregroundColor = Color(c) }
            if let c = attrs[.backgroundColor] as? UIColor { run.backgroundColor = Color(c) }
            let link = url(attrs[.link])
            if keepLink { run.link = link }
            // A link in a cell is underlined, or it reads as plain text in the
            // cell's ink.
            if link != nil { run.underlineStyle = .single }
            if attrs[.strikethroughStyle] != nil { run.strikethroughStyle = .single }
            out += run
        }
        return out
    }
}

private extension NSAttributedString {
    /// Without the whitespace a word enumeration's enclosing range carries.
    var trimmed: NSAttributedString {
        let s = string as NSString
        var lo = 0, hi = s.length
        while lo < hi, CharacterSet.whitespacesAndNewlines.contains(UnicodeScalar(s.character(at: lo)) ?? " ") { lo += 1 }
        while hi > lo, CharacterSet.whitespacesAndNewlines.contains(UnicodeScalar(s.character(at: hi - 1)) ?? " ") { hi -= 1 }
        return attributedSubstring(from: NSRange(location: lo, length: hi - lo))
    }
}

/// The grid: cells wrap by words in their own column, a rule under the
/// header (the text colour) and a faint one between rows, as the console's
/// `.md-table` (app.css) draws it.
private struct MdTableGrid: View {
    let model: MdTableModel
    let align: [NSTextAlignment]
    let rule: Color
    @Environment(\.openURL) private var openURL

    var body: some View {
        let rows = model.cells.count
        let grid = MdTableLayout(cols: model.minW.count, minW: model.minW, maxW: model.maxW, align: align) {
            ForEach(0..<rows, id: \.self) { r in
                ForEach(0..<model.minW.count, id: \.self) { c in cell(r, c) }
            }
            ForEach(0..<rows, id: \.self) { r in
                Rectangle().fill(r == rows - 1 ? .clear : model.header && r == 0 ? rule.opacity(0.7) : rule.opacity(0.15))
            }
        }
        if model.hasLinks { grid } else { grid.textSelection(.enabled) }
    }

    @ViewBuilder private func cell(_ r: Int, _ c: Int) -> some View {
        let a = c < align.count ? align[c] : .left
        let text = Text(model.cells[r][c])
            .multilineTextAlignment(a == .right ? .trailing : a == .center ? .center : .leading)
        if let u = model.links[r][c] {
            // The whole cell, its padding and half the gap to its neighbours.
            text.frame(maxWidth: .infinity, maxHeight: .infinity,
                       alignment: a == .right ? .topTrailing : a == .center ? .top : .topLeading)
                .contentShape(Rectangle().inset(by: -MdTableLayout.pad))
                .onTapGesture { openURL(u) }
        } else {
            text
        }
    }
}

/// Column widths as a browser's auto table layout shares them: every column
/// gets its longest word, and what is left of the width on offer goes to
/// each column in proportion to how much more it would take to sit on one
/// line. A table that fits on one line per row is as wide as it is, no wider.
/// Subviews: rows × cols cells in reading order, then one rule per row.
private struct MdTableLayout: Layout {
    let cols: Int
    let minW: [CGFloat], maxW: [CGFloat]
    let align: [NSTextAlignment]
    static let pad: CGFloat = 5

    private func widths(_ proposed: CGFloat?) -> [CGFloat] {
        guard let p = proposed, p.isFinite else { return maxW }
        let avail = p - MdTableModel.gap * CGFloat(cols - 1)
        let natural = maxW.reduce(0, +), least = minW.reduce(0, +)
        if natural <= avail { return maxW }
        if least >= avail || natural <= least { return minW }
        let share = (avail - least) / (natural - least)
        return zip(minW, maxW).map { floor($0 + ($1 - $0) * share) }
    }

    private func heights(_ w: [CGFloat], _ s: Subviews) -> [CGFloat] {
        let rows = cols == 0 ? 0 : s.count / (cols + 1)
        return (0..<rows).map { r in
            (0..<cols).map { c in s[r * cols + c].sizeThatFits(ProposedViewSize(width: w[c], height: nil)).height }
                .max().map { ceil($0) + 2 * Self.pad } ?? 0
        }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let w = widths(proposal.width)
        return CGSize(width: w.reduce(0, +) + MdTableModel.gap * CGFloat(max(cols - 1, 0)),
                      height: heights(w, subviews).reduce(0, +))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let w = widths(bounds.width)
        let hs = heights(w, subviews)
        let total = w.reduce(0, +) + MdTableModel.gap * CGFloat(max(cols - 1, 0))
        var y = bounds.minY
        for (r, h) in hs.enumerated() {
            var x = bounds.minX
            for c in 0..<cols {
                let a = c < align.count ? align[c] : .left
                let (px, anchor): (CGFloat, UnitPoint) = a == .right ? (x + w[c], .topTrailing) : a == .center ? (x + w[c] / 2, .top) : (x, .topLeading)
                // The row's height on offer, so a link cell's tap area is the
                // whole cell; a plain cell still takes only its text's.
                subviews[r * cols + c].place(at: CGPoint(x: px, y: y + Self.pad), anchor: anchor,
                                             proposal: ProposedViewSize(width: w[c], height: h - 2 * Self.pad))
                x += w[c] + MdTableModel.gap
            }
            subviews[hs.count * cols + r].place(at: CGPoint(x: bounds.minX, y: y + h - 1), anchor: .topLeading,
                                                proposal: ProposedViewSize(width: total, height: 1))
            y += h
        }
    }
}

func mdSegments(_ text: String) -> [MdSegment] {
    var out: [MdSegment] = []
    var prose: [String] = []
    let flush = {
        let t = prose.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        if !t.isEmpty { out.append(.text(t)) }
        prose = []
    }
    let lines = text.components(separatedBy: "\n")
    var i = 0
    while i < lines.count {
        if let first = blobLine(lines[i]) {
            flush()
            var files = [first]
            i += 1
            while i < lines.count {
                if lines[i].trimmingCharacters(in: .whitespaces).isEmpty { i += 1; continue }
                guard let f = blobLine(lines[i]) else { break }
                files.append(f); i += 1
            }
            out.append(.files(files)); continue
        }
        if let (table, next) = mdTable(lines, at: i) {
            flush(); out.append(table); i = next; continue
        }
        guard let m = lines[i].wholeMatch(of: /(\s*)(`{3,})\s*([\w.+#-]*)\s*/) else {
            prose.append(lines[i]); i += 1; continue
        }
        flush()
        let indent = String(m.1), fence = String(m.2)
        var body: [String] = []
        i += 1
        while i < lines.count {
            let t = lines[i].trimmingCharacters(in: .whitespaces)
            if t.hasPrefix(fence), t.allSatisfy({ $0 == "`" }) { break }
            body.append(lines[i].hasPrefix(indent) ? String(lines[i].dropFirst(indent.count))
                                                   : String(lines[i].drop { $0 == " " || $0 == "\t" }))
            i += 1
        }
        i += 1      // the closing fence
        out.append(.code(body.joined(separator: "\n"), lang: String(m.3)))
    }
    flush()
    return out
}

/// A fenced block (a settings.json to paste, say), which otherwise arrives as
/// a column of paragraphs with its indentation gone. Text copied verbatim:
/// monospaced, whitespace kept, long lines wrap to the card, the language and Copy on a bar above. Copy puts exactly what the
/// session wrote on the pasteboard.
private struct CodeBlock: View {
    let code: String
    let lang: String
    let style: UIFont.TextStyle
    @State private var copied = false

    var body: some View {
        // Two points under the prose: monospace runs wide, and the phone is narrow.
        let size = UIFont.preferredFont(forTextStyle: style).pointSize - 2
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(lang).font(.system(size: 11, design: .monospaced)).foregroundStyle(.secondary)
                Spacer()
                Button {
                    UIPasteboard.general.string = code
                    copied = true
                    Task { try? await Task.sleep(for: .seconds(1.5)); copied = false }
                } label: {
                    Label(copied ? "Copied" : "Copy", systemImage: copied ? "checkmark" : "doc.on.doc")
                        .font(.caption2.weight(.semibold))
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 10).padding(.vertical, 5)
            Divider()
            // The same text view the prose is: a `Text` with `.textSelection`
            // copies only the whole block, and on the Mac a drag over it
            // selects nothing. Wrapped to the card's width, never a sideways
            // scroll; whitespace and line breaks are kept, and Copy still
            // copies the text as written.
            SelectableText(attributed: Self.mono(code, size: size), tint: .label, chat: nil)
                .padding(10)
        }
        .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Color.primary.opacity(0.15)))
    }

    /// One string per (code, size): SelectableText keys its measured size on
    /// the object it is handed.
    private static let cache = NSCache<NSString, NSAttributedString>()
    private static func mono(_ code: String, size: CGFloat) -> NSAttributedString {
        let key = "\(size)|\(code)" as NSString
        if let hit = cache.object(forKey: key) { return hit }
        let built = NSAttributedString(string: code, attributes: [
            .font: UIFont.monospacedSystemFont(ofSize: size, weight: .regular),
            .foregroundColor: UIColor.label,
        ])
        cache.countLimit = 200
        cache.setObject(built, forKey: key)
        return built
    }
}

/// Same text with the markdown removed, for the few places iOS gives us no
/// way to render it: a navigation bar title, a notification body.
func mdPlain(_ s: String) -> String {
    String(md(s).characters)
}

/// A card's words as plain text for the pasteboard — "Copy text" in a card's
/// hold menu, for passing the words on. A link keeps its URL, fence lines go.
func copyText(_ parts: String...) -> String {
    let link = /\[([^\]]+)\]\((\S+?)\)/
    return parts.filter { !$0.isEmpty }.map { s in
        let kept = s.components(separatedBy: "\n")
            .filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("```") }
            .joined(separator: "\n")
        return mdPlain(kept.replacing(link) { m in m.1 == m.2 ? String(m.2) : "\(m.1) (\(m.2))" })
    }.joined(separator: "\n\n")
}

private extension UIFont {
    func bolded() -> UIFont {
        guard let d = fontDescriptor.withSymbolicTraits(fontDescriptor.symbolicTraits.union(.traitBold)) else { return self }
        return UIFont(descriptor: d, size: pointSize)
    }
}

/// Non-scrolling, non-editable UITextView sized to its content: gives the
/// native long-press → word highlight → drag-the-handles → Copy behaviour that
/// SwiftUI's `.textSelection` cannot do across separate Text views.
private struct SelectableText: UIViewRepresentable {
    let attributed: NSAttributedString
    let tint: UIColor
    var chat: ChatHook?
    var lines: Int? = nil
    /// A fenced block: every line as long as it is, never wrapped to the
    /// width on offer (the box scrolls sideways).
    var unwrapped = false
    @Environment(\.openURL) private var openURL

    /// Adds "Chat about this" to the selection menu, carrying the selected
    /// words as the quote.
    final class Coordinator: NSObject, UITextViewDelegate {
        var chat: ChatHook?
        /// A `life://` link (a hub id, Refs.swift) goes to the app's own
        /// navigation; a text view on its own would hand it to iOS.
        var open: OpenURLAction?
        func textView(_ tv: UITextView, primaryActionFor item: UITextItem, defaultAction: UIAction) -> UIAction? {
            guard case .link(let url) = item.content, url.scheme == "life", let open else { return defaultAction }
            return UIAction { _ in open(url) }
        }
        /// What `sizeThatFits` has already worked out for the text this view
        /// holds. SwiftUI asks a view in a plain stack for its size again and
        /// again — every ancestor stack probes it at several widths, and
        /// every state change anywhere in the chat starts over — and each
        /// answer was two full TextKit layouts — thousands of calls for a few
        /// dozen text views, seconds of freeze on opening a long chat. The answer only
        /// depends on the text and the width, so it is worked out once each.
        var measured: NSAttributedString?
        var ideal: CGFloat = 0
        var heights: [CGFloat: CGFloat] = [:]
        func textView(_ tv: UITextView, editMenuForTextIn range: NSRange, suggestedActions: [UIMenuElement]) -> UIMenu? {
            guard let chat, range.length > 0 else { return nil }
            let quote = (tv.text as NSString).substring(with: range)
            let action = UIAction(title: "Chat about this", image: UIImage(systemName: "bubble.and.pencil")) { _ in
                MainActor.assumeIsolated { chat.run(quote) }
            }
            return UIMenu(children: suggestedActions + [action])
        }
    }
    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> UITextView {
        let v = Perf.time("SelectableText.make") { UITextView() }
        v.delegate = context.coordinator
        v.isEditable = false
        v.isSelectable = true
        v.isScrollEnabled = false            // the surrounding ScrollView/List scrolls
        v.backgroundColor = .clear
        v.textContainerInset = .zero
        v.textContainer.lineFragmentPadding = 0
        v.adjustsFontForContentSizeCategory = true
        v.textDragInteraction?.isEnabled = false  // long-press selects, never drags the text out
        v.setContentCompressionResistancePriority(.required, for: .vertical)
        v.setContentHuggingPriority(.required, for: .vertical)
        return v
    }

    func updateUIView(_ v: UITextView, context: Context) {
        context.coordinator.chat = chat
        context.coordinator.open = openURL
        v.textContainer.maximumNumberOfLines = lines ?? 0
        v.textContainer.lineBreakMode = lines == nil ? .byWordWrapping : .byTruncatingTail
        // Threads poll every 3s; reassigning identical text would kill an
        // in-progress selection and re-lay out the whole list.
        Perf.time("SelectableText.update") { if v.attributedText != attributed { v.attributedText = attributed } }
        #if targetEnvironment(macCatalyst)
        // Links are the tint alone on the Mac: a note full of ids read as a
        // wall of underlines.
        v.linkTextAttributes = [.foregroundColor: UIColor.tintColor]
        #else
        v.linkTextAttributes = [.foregroundColor: tint, .underlineStyle: NSUnderlineStyle.single.rawValue]
        #endif
    }

    /// The phone's width, for a text view that has no window yet: the widest
    /// any chat row can honestly be.
    private static var screenWidth: CGFloat {
        UIApplication.shared.connectedScenes.lazy
            .compactMap { ($0 as? UIWindowScene)?.screen.bounds.width }.first ?? 390
    }

    /// Hug the text like `Text` does — never claim the full proposed width, or
    /// every chat bubble would stretch edge to edge.
    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UITextView, context: Context) -> CGSize? {
        let huge = CGFloat(100_000)
        let proposed = proposal.width ?? .infinity
        // No usable proposal (rare): fall back to the window, or to the
        // screen while the view is not in one yet — a bubble inserted into a
        // live chat is measured before it is attached, and answering with
        // the unwrapped line width there is what let one row
        // grow past the phone and the whole chat pan sideways.
        let bound = unwrapped ? huge : proposed.isFinite && proposed > 0 ? proposed : (uiView.window?.bounds.width ?? Self.screenWidth)
        return Perf.time("SelectableText.sizeThatFits") {
            let c = context.coordinator
            // Same object = same text, font and colour (StyledText hands out
            // one cached string per input); a new one starts over.
            if c.measured !== attributed {
                c.measured = attributed
                c.heights = [:]
                c.ideal = Perf.time("SelectableText.measure") { ceil(uiView.sizeThatFits(CGSize(width: huge, height: huge)).width) }
            }
            let w = min(bound, c.ideal)
            if let h = c.heights[w] { return CGSize(width: w, height: h) }
            let h = Perf.time("SelectableText.measure") { ceil(uiView.sizeThatFits(CGSize(width: w, height: huge)).height) }
            c.heights[w] = h
            return CGSize(width: w, height: h)
        }
    }
}
