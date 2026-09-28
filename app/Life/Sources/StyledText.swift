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
        if text.contains("```") {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(mdSegments(text).enumerated()), id: \.offset) { _, seg in
                    switch seg {
                    case .text(let t): SelectableText(attributed: cached(t), tint: UIColor(color), chat: chatHook)
                    case .code(let code, let lang): CodeBlock(code: code, lang: lang, style: style)
                    }
                }
            }
        } else {
            SelectableText(attributed: cached(text), tint: UIColor(color), chat: chatHook)
        }
    }

    /// `build()` walks the text twice — once per line for the block layout,
    /// once per run for inline markdown — and it ran on EVERY body evaluation:
    /// every 3s poll, every keystroke that resized the composer, every scroll
    /// that re-laid out a bubble, for every message on screen. The output only
    /// depends on the five inputs below, so it is built once and kept.
    private static let cache = NSCache<NSString, NSAttributedString>()
    private func cached(_ text: String) -> NSAttributedString {
        let key = "\(style.rawValue)|\(bold)|\(color)|\(typeSize)|\(text)" as NSString
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

    private func build(_ text: String) -> NSAttributedString {
        let plain = UIFont.preferredFont(forTextStyle: style)
        let base = bold ? plain.bolded() : plain
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
                if !blank, !paras.isEmpty { paras.append(NSAttributedString(string: " ", attributes: [.font: base, .foregroundColor: ui])) }
                blank = true; continue
            }
            blank = false
            // A pipe table ("| Date | Buy |"): the run of pipe rows from here becomes rows
            // of tab-separated cells over shared tab stops, header bold.
            if trimmedAll.hasPrefix("|"), let first = mdTableRow(trimmedAll), mdTableSeparator(trimmedAll) == nil {
                var rows: [[String]] = [first]
                var hasHeader = false
                if i < lines.count, mdTableSeparator(lines[i]) != nil { hasHeader = true; i += 1 }
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("|"),
                      let r = mdTableRow(lines[i]) {
                    if mdTableSeparator(lines[i]) == nil { rows.append(r) }
                    i += 1
                }
                paras.append(contentsOf: table(rows, header: hasHeader, base: base, color: ui))
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

    /// Rows of a pipe table as paragraphs of one text view: every column
    /// starts at a tab stop shared by all rows, each stop the widest cell in
    /// that column (capped, so one long cell cannot push the rest off the
    /// phone) plus a gap. The last column has no cap — it wraps, and its
    /// continuation lines hang under its own start, not under column one.
    /// The header row is bold. No rules: a hairline would need attachments,
    /// and the alignment already reads as a table.
    private static let cellCap: CGFloat = 150
    private static let cellGap: CGFloat = 14
    private func table(_ rows: [[String]], header: Bool, base: UIFont, color: UIColor) -> [NSAttributedString] {
        let cols = rows.map(\.count).max() ?? 0
        guard cols > 0 else { return [] }
        let cells: [[NSAttributedString]] = rows.enumerated().map { (r, row) in
            let font = header && r == 0 ? base.bolded() : base
            return (0..<cols).map { c in inline(c < row.count ? row[c] : "", base: font, color: color) }
        }
        var stops: [CGFloat] = []      // where column c (c ≥ 1) starts
        var x: CGFloat = 0
        for c in 0..<(cols - 1) {
            let widest = cells.map { ceil($0[c].size().width) }.max() ?? 0
            x += min(widest, Self.cellCap) + Self.cellGap
            stops.append(x)
        }
        return cells.map { row in
            let p = NSMutableParagraphStyle()
            p.lineBreakMode = .byWordWrapping
            p.paragraphSpacing = 3
            p.tabStops = stops.map { NSTextTab(textAlignment: .left, location: $0) }
            p.defaultTabInterval = Self.cellGap
            p.headIndent = stops.last ?? 0
            let line = NSMutableAttributedString()
            for (c, cell) in row.enumerated() {
                if c > 0 { line.append(NSAttributedString(string: "\t", attributes: [.font: base, .foregroundColor: color])) }
                line.append(cell)
            }
            line.addAttribute(.paragraphStyle, value: p, range: NSRange(location: 0, length: line.length))
            return line
        }
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
            if intent.contains(.code) { font = UIFont.monospacedSystemFont(ofSize: base.pointSize, weight: .regular) }
            var traits = font.fontDescriptor.symbolicTraits
            if intent.contains(.stronglyEmphasized) { traits.insert(.traitBold) }
            if intent.contains(.emphasized) { traits.insert(.traitItalic) }
            if traits != font.fontDescriptor.symbolicTraits, let d = font.fontDescriptor.withSymbolicTraits(traits) {
                font = UIFont(descriptor: d, size: font.pointSize)
            }
            var attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color]
            if intent.contains(.strikethrough) { attrs[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
            if let link = run.link { attrs[.link] = link }
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
    guard s.contains("*") else { return s }
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
        out.append(c); i += 1
    }
    return out
}

/// The fold line: past this a chat bubble clamps behind a
/// "Show all · N lines" button. Counted on the text, not the rendered height,
/// so the same message folds the same on both surfaces; the console's
/// isLongText (ui.js) is the same rule: >18 non-blank lines or >1600 characters.
let longLines = 18, longChars = 1600
func longLineCount(_ s: String) -> Int {
    s.split(separator: "\n", omittingEmptySubsequences: false)
        .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.count
}
func isLongText(_ s: String) -> Bool { longLineCount(s) > longLines || s.count > longChars }

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
/// separator row. The phone lays every column out left-aligned; the
/// alignment is parsed so the row is recognised (and dropped), and so both
/// renderers agree on what is a separator.
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
/// monospaced, whitespace kept, long lines scroll sideways instead of
/// wrapping, the language and Copy on a bar above. Copy puts exactly what the
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
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(size: size, design: .monospaced))
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: true, vertical: true)
                    .padding(10)
            }
        }
        .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Color.primary.opacity(0.15)))
    }
}

/// Same text with the markdown removed, for the few places iOS gives us no
/// way to render it: a navigation bar title, a notification body.
func mdPlain(_ s: String) -> String {
    String(md(s).characters)
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
        // Threads poll every 3s; reassigning identical text would kill an
        // in-progress selection and re-lay out the whole list.
        Perf.time("SelectableText.update") { if v.attributedText != attributed { v.attributedText = attributed } }
        v.linkTextAttributes = [.foregroundColor: tint, .underlineStyle: NSUnderlineStyle.single.rawValue]
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
        let bound = proposed.isFinite && proposed > 0 ? proposed : (uiView.window?.bounds.width ?? Self.screenWidth)
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
