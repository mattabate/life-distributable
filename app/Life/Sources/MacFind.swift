#if targetEnvironment(macCatalyst)
import SwiftUI
import UIKit

/// Find in page on the desktop: ⌘F opens a bar under the top bar, every
/// match on the page turns yellow, the current one orange, and Return / ⌘G
/// scroll to the next (the owner 2026-10-05 23:49: "it'd be super nice if I
/// was able to find in page here… a little find bar appeared under the bar…
/// at any page… search things that scroll you to the height of the actual…
/// it should be just highlighting yellow… not the tool tips").
///
/// The page's words are read from its accessibility tree — every Text,
/// button title and link SwiftUI draws is a node there with its frame — so no
/// page has to opt in. What is searched is what is ON the page: hover
/// tooltips and chart callouts are not drawn until hovered, so they never
/// match; a lazy list finds only its loaded rows. The bars carry the
/// `MacFind.skip` identifier and are left out.
@MainActor @Observable final class MacFind {
    static let shared = MacFind()
    static let skip = "macfind-skip"

    var shown = false
    var query = "" { didSet { if query != oldValue { queryChanged() } } }
    /// Bumped to pull the keyboard into the field (⌘F with the bar open).
    var focusTick = 0
    private(set) var count = 0
    private(set) var current = 0
    /// The page under the bars, in window points (RootView measures it).
    var pageFrame: CGRect = .zero

    @ObservationIgnored private var matches: [Match] = []
    @ObservationIgnored private var overlay: FindOverlay?
    @ObservationIgnored private var observed: [NSKeyValueObservation] = []
    @ObservationIgnored private var tick: Timer?

    struct Match {
        let node: NSObject
        let label: String
        let range: NSRange
    }

    // MARK: actions (menu, bar, keys)

    func open() {
        shown = true
        focusTick += 1
        search(keep: true)
        startTick()
    }

    func close() {
        shown = false
        matches = []; count = 0; current = 0
        tick?.invalidate(); tick = nil
        observed = []
        overlay?.removeFromSuperview(); overlay = nil
    }

    func next() { step(+1) }
    func previous() { step(-1) }

    private func step(_ d: Int) {
        if !shown { open(); return }
        guard count > 0 else { return }
        current = (current + d + count) % count
        reveal()
    }

    /// The query changed: search afresh and go to the first match.
    func queryChanged() { current = 0; search(keep: false); reveal() }

    /// The tab changed: the new page draws first, then it is searched.
    func pageChanged() {
        guard shown else { return }
        current = 0
        Task { try? await Task.sleep(for: .milliseconds(450)); search(keep: false); reveal() }
    }

    // MARK: search

    private var window: UIWindow? {
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first }.first
    }

    /// Walk the page's nodes in reading order and collect every occurrence.
    /// `keep`: the page redrew under an open bar (the store's poll) — stay on
    /// the same match rather than jumping back to the first.
    func search(keep: Bool) {
        let q = query.trimmingCharacters(in: .whitespaces)
        let was = keep && current < matches.count ? matches[current] : nil
        matches = []
        guard shown, !q.isEmpty, let root = window?.rootViewController?.view else { count = 0; current = 0; paint(); return }
        var seen = Set<String>()
        Self.walk(root) { node, label in
            var at = label.startIndex
            while at < label.endIndex, let r = label.range(of: q, options: [.caseInsensitive, .diacriticInsensitive], range: at..<label.endIndex) {
                let m = Match(node: node, label: label, range: NSRange(r, in: label))
                // One Text can surface twice (a node and its twin); one hit each.
                let f = node.accessibilityFrame
                let key = "\(Int(f.minX)),\(Int(f.minY)),\(m.range.location),\(label)"
                if seen.insert(key).inserted { matches.append(m) }
                at = r.upperBound
            }
        }
        count = matches.count
        if let was, let i = matches.firstIndex(where: { $0.label == was.label && $0.range == was.range }) { current = i }
        else if current >= count { current = 0 }
        paint()
    }

    /// Every labelled node under `view`, depth first. A node that is an
    /// element is read and not entered (a button's title is its children's
    /// words joined); anything carrying the skip identifier is passed over.
    private static func walk(_ object: NSObject, depth: Int = 0, _ visit: (NSObject, String) -> Void) {
        guard depth < 80 else { return }
        if object.responds(to: NSSelectorFromString("accessibilityIdentifier")),
           object.value(forKey: "accessibilityIdentifier") as? String == skip { return }
        if let v = object as? UIView, v.isHidden || v.alpha < 0.01 { return }
        if object.isAccessibilityElement {
            if let l = object.accessibilityLabel, !l.isEmpty, !object.accessibilityElementsHidden { visit(object, l) }
            return
        }
        if object.accessibilityElementsHidden { return }
        var kids: [NSObject] = []
        if let els = object.accessibilityElements as? [NSObject] { kids = els }
        else {
            let n = object.accessibilityElementCount()
            if n != NSNotFound, n > 0 { kids = (0..<n).compactMap { object.accessibilityElement(at: $0) as? NSObject } }
        }
        if kids.isEmpty, let v = object as? UIView { kids = v.subviews }
        for k in kids { walk(k, depth: depth + 1, visit) }
    }

    // MARK: scroll + paint

    /// Bring the current match to the middle of its scroll view.
    private func reveal() {
        guard current < matches.count, let window else { paint(); return }
        let f = window.convert(matches[current].node.accessibilityFrame, from: window.screen.coordinateSpace)
        if let sv = Self.scroller(at: CGPoint(x: f.midX, y: f.midY), in: window) {
            let box = sv.convert(sv.bounds, to: window)
            if f.minY < box.minY + 40 || f.maxY > box.maxY - 40 {
                let inset = sv.adjustedContentInset
                let top = -inset.top, bottom = max(top, sv.contentSize.height - sv.bounds.height + inset.bottom)
                let y = min(max(sv.contentOffset.y + f.midY - box.midY, top), bottom)
                sv.setContentOffset(CGPoint(x: sv.contentOffset.x, y: y), animated: false)
                sv.layoutIfNeeded()
            }
        }
        paint()
    }

    /// The innermost vertical scroll view whose column holds `p`. Offscreen
    /// content sits outside its visible box, so only the x decides, then the
    /// smallest box whose column contains it.
    private static func scroller(at p: CGPoint, in window: UIWindow) -> UIScrollView? {
        var best: (UIScrollView, CGFloat)?
        func look(_ v: UIView) {
            if v.isHidden { return }
            if let s = v as? UIScrollView, s.contentSize.height > s.bounds.height + 1 {
                let b = s.convert(s.bounds, to: window)
                if b.minX <= p.x, p.x <= b.maxX {
                    let inside = b.minY <= p.y && p.y <= b.maxY
                    // A box the point is inside wins; else the column's own scroller.
                    let score = (inside ? 0 : 1_000_000) + b.width * b.height / 1_000
                    if best == nil || score < best!.1 { best = (s, score) }
                }
            }
            for s in v.subviews { look(s) }
        }
        look(window)
        return best?.0
    }

    /// Redraw the yellow: one box per visible match, clipped to its scroll
    /// view and to the page under the bars.
    func paint() {
        guard let window, let host = window.rootViewController?.view, shown, !matches.isEmpty else { overlay?.boxes = []; return }
        // On the root view, not the window: a screenshot draws that view.
        if overlay == nil || overlay?.superview !== host {
            overlay?.removeFromSuperview()
            let o = FindOverlay(frame: host.bounds)
            o.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            host.addSubview(o)
            overlay = o
        }
        host.bringSubviewToFront(overlay!)
        var boxes: [(CGRect, Bool)] = []
        var scrollers = Set<ObjectIdentifier>()
        let page = pageFrame == .zero ? window.bounds : pageFrame
        for (i, m) in matches.enumerated() {
            let f = window.convert(m.node.accessibilityFrame, from: window.screen.coordinateSpace)
            guard f.width > 0, f.height > 0, f.intersects(page) else { continue }
            var clip = page
            if let sv = Self.scroller(at: CGPoint(x: f.midX, y: f.midY), in: window) {
                clip = clip.intersection(sv.convert(sv.bounds, to: window))
                if scrollers.insert(ObjectIdentifier(sv)).inserted { watch(sv) }
            }
            for r in FindGeometry.rects(of: m.range, in: m.label, frame: f) {
                let c = r.insetBy(dx: -2, dy: -1).intersection(clip)
                if !c.isNull, c.height > 2 { boxes.append((c, i == current)) }
            }
        }
        overlay?.boxes = boxes
    }

    /// Repaint as a page scrolls under the boxes.
    private func watch(_ sv: UIScrollView) {
        observed.append(sv.observe(\.contentOffset, options: []) { _, _ in
            Task { @MainActor in MacFind.shared.paint() }
        })
    }

    /// The page redraws on its poll; while the bar is open the matches are
    /// read again every second and a half, holding the current one.
    private func startTick() {
        tick?.invalidate()
        tick = Timer.scheduledTimer(withTimeInterval: 1.5, repeats: true) { _ in
            Task { @MainActor in
                let f = MacFind.shared
                if f.shown { f.observed = []; f.search(keep: true) }
            }
        }
    }
}

/// The boxes, drawn over the window and never in the way of a click: yellow
/// multiplied into the page so the words stay black, the current one orange.
final class FindOverlay: UIView {
    var boxes: [(CGRect, Bool)] = [] { didSet { setNeedsDisplay() } }
    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        isOpaque = false
        backgroundColor = .clear
        layer.compositingFilter = "multiplyBlendMode"
        accessibilityElementsHidden = true
    }
    required init?(coder: NSCoder) { fatalError() }
    override func draw(_ rect: CGRect) {
        for (r, on) in boxes {
            (on ? UIColor(red: 1, green: 0.62, blue: 0.04, alpha: 1) : UIColor(red: 1, green: 0.93, blue: 0.2, alpha: 1)).setFill()
            UIBezierPath(roundedRect: r, cornerRadius: 3).fill()
        }
    }
}

/// Where in a node's frame the matched words sit. A node gives its text and
/// its box, not its font, so the font is guessed: the system font at the
/// size whose laid-out text best fills that box. A poor fit (a button whose
/// title is several texts joined, a padded label) boxes the whole node.
enum FindGeometry {
    private static let sizes: [CGFloat] = [10, 11, 11.5, 12, 12.5, 13, 14, 15, 16, 17, 19, 22, 26, 30]

    static func rects(of range: NSRange, in text: String, frame: CGRect) -> [CGRect] {
        var best: (err: CGFloat, rects: [CGRect])?
        for s in sizes {
            for w in [UIFont.Weight.regular, .semibold] {
                let font = UIFont.systemFont(ofSize: s, weight: w)
                guard let (err, rects) = fit(range, text, frame, font) else { continue }
                if best == nil || err < best!.err { best = (err, rects) }
            }
        }
        if let best, best.err < 0.16, !best.rects.isEmpty { return best.rects }
        return [frame]
    }

    /// Lay the text out at the box's width in `font`; the error is how far
    /// the result's size is from the box's.
    private static func fit(_ range: NSRange, _ text: String, _ frame: CGRect, _ font: UIFont) -> (CGFloat, [CGRect])? {
        let storage = NSTextStorage(string: text, attributes: [.font: font])
        let layout = NSLayoutManager()
        let box = NSTextContainer(size: CGSize(width: frame.width + 1, height: .greatestFiniteMagnitude))
        box.lineFragmentPadding = 0
        layout.addTextContainer(box)
        storage.addLayoutManager(layout)
        layout.ensureLayout(for: box)
        let used = layout.usedRect(for: box)
        guard used.height > 0, range.location + range.length <= (text as NSString).length else { return nil }
        let lines = max(1, (used.height / font.lineHeight).rounded())
        let err: CGFloat
        if lines <= 1 {
            // One line: the box hugs the words, so both sides count.
            err = abs(used.width - frame.width) / frame.width + abs(font.lineHeight - frame.height) / frame.height
        } else {
            err = abs(used.height - frame.height) / frame.height + 0.04
        }
        let glyphs = layout.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
        var out: [CGRect] = []
        // A single line drawn narrower than its box was laid out centred or
        // trailing only if the box is wider; boxes hug leading text here.
        let dy = (frame.height - used.height) / 2
        layout.enumerateEnclosingRects(forGlyphRange: glyphs, withinSelectedGlyphRange: NSRange(location: NSNotFound, length: 0), in: box) { r, _ in
            out.append(CGRect(x: frame.minX + r.minX, y: frame.minY + max(0, dy) + r.minY, width: r.width, height: r.height))
        }
        return (err, out)
    }
}

/// The bar under the top bar: field, "3 of 12", up, down, Done.
struct MacFindBar: View {
    @State private var find = MacFind.shared
    @FocusState private var focused: Bool

    var body: some View {
        @Bindable var find = find
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField("Find in page", text: $find.query)
                .textFieldStyle(.plain).font(.system(size: 13))
                .frame(width: 260)
                .focused($focused)
                .onSubmit { find.next(); focused = true }
                .onKeyPress(.escape) { find.close(); return .handled }
                .onKeyPress(.return, phases: .down) { press in
                    guard press.modifiers.contains(.shift) else { return .ignored }
                    find.previous(); return .handled
                }
            if !find.query.trimmingCharacters(in: .whitespaces).isEmpty {
                Text(find.count == 0 ? "not found" : "\(find.current + 1) of \(find.count)")
                    .font(.system(size: 12)).monospacedDigit()
                    .foregroundStyle(find.count == 0 ? Color.red : .secondary)
            }
            Button { find.previous() } label: { Image(systemName: "chevron.up") }
                .buttonStyle(.borderless).disabled(find.count == 0).help("Previous (⇧⌘G)")
            Button { find.next() } label: { Image(systemName: "chevron.down") }
                .buttonStyle(.borderless).disabled(find.count == 0).help("Next (⌘G)")
            Spacer()
            Button("Done") { find.close() }.buttonStyle(.bordered).controlSize(.small)
        }
        .padding(.leading, 16).padding(.trailing, 16).frame(height: 36)
        .background(Color(uiColor: .secondarySystemBackground))
        .onChange(of: find.focusTick, initial: true) { _, _ in focused = true }
    }
}
#endif
