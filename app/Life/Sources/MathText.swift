import SwiftUI
import WebKit

/// Math in a reply or a card: a session writes $…$ and $$…$$ and it shows
/// typeset. A paragraph that holds math is not the UITextView every other
/// line is: it is a web view running the console's own `md()` (ui.js) and
/// the KaTeX it vendors, both fetched from the hub through `lifehub://`
/// (HubScheme). So the delimiter rule ($12 and $15 stay money, code is never
/// math) and the drawing are the web's, not a Swift copy that drifts. The
/// page reports its height, so it never scrolls inside the card.
struct MathText: View {
    let text: String
    let font: UIFont
    let color: UIColor
    /// The text view's around it.
    var lineHeight: CGFloat = 1.35
    @Environment(HubClient.self) private var hub
    /// The last height each paragraph drew at: a chat scrolled back into
    /// view opens at its size instead of jumping from zero.
    @MainActor static var heights: [String: CGFloat] = [:]
    /// What each paragraph looked like once drawn: a chat opened again shows
    /// it at once while its web view draws, never a white gap that fills in.
    nonisolated(unsafe) static var shots: [String: UIImage] = [:]
    @State private var height: CGFloat = 0
    /// The web view is hidden until its text and fonts are drawn.
    @State private var drawn = false

    var body: some View {
        let key = "\(font.pointSize)|\(text)"
        MathWeb(hub: hub, text: text, font: font, color: color, lineHeight: lineHeight, key: key, height: $height, drawn: $drawn)
            .opacity(drawn ? 1 : 0)
            .background(alignment: .topLeading) {
                if !drawn, let shot = Self.shots[key] { Image(uiImage: shot) }
            }
            .frame(height: height > 0 ? height : Self.heights[text] ?? font.lineHeight * 1.6)
            .clipped()
            .onChange(of: height) { _, h in Self.heights[text] = h }
    }

    /// ui.js is a classic script with no DOM work at load, so it loads
    /// alone; katex before it, as in index.html. Links leave the view
    /// (MathWeb's policy). The page clips overflow, so no scroll bar; the
    /// display box does not, so a fraction's denominator is never cut.
    static let page = """
    <!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">
    <link rel="stylesheet" href="/static/vendor/katex/katex.min.css">
    <style>html, body { margin: 0; overflow: hidden; background: transparent; -webkit-text-size-adjust: none; }
    #out { font-family: -apple-system, system-ui; line-height: 1.35; overflow-wrap: anywhere; padding-bottom: .25em; }
    #out p, #out ul, #out ol, #out h3 { margin: 0 0 .5em; } #out > :last-child { margin-bottom: 0; }
    #out ul, #out ol { padding-left: 1.3em; } #out h3 { font-size: 1em; }
    #out a { color: inherit; } #out code { font-family: ui-monospace, Menlo; font-size: .9em; }
    .md-math, .katex-display { margin: .3em 0; max-width: 100%; }
    .katex-display { padding: .2em 0; }
    .md-math .katex-display { margin: 0; }</style></head>
    <body><div id="out"></div>
    <script src="/static/vendor/katex/katex.min.js"></script>
    <script src="/static/ui.js"></script>
    <script>
    const out = document.getElementById('out');
    // The box or what spills past it, whichever is taller: a display
    // fraction's denominator can hang below the box.
    // [height, draw number, 1 once the fonts are in]: a reused view never
    // reports its last paragraph's height as this one's.
    let seq = 0;
    const tell = (done) => webkit.messageHandlers.h.postMessage([Math.ceil(Math.max(out.getBoundingClientRect().height, out.scrollHeight)), seq, done ? 1 : 0]);
    function draw(t, px, rgb, weight, lh, n) {
      seq = n;
      out.style.fontSize = px + 'px'; out.style.color = rgb; out.style.fontWeight = weight; out.style.lineHeight = lh;
      out.innerHTML = md(t); tell(); void out.offsetHeight;
      document.fonts.ready.then(() => tell(1)); setTimeout(() => tell(1), 300);
    }
    new ResizeObserver(() => tell()).observe(out);
    </script></body></html>
    """
}

private struct MathWeb: UIViewRepresentable {
    let hub: HubClient
    let text: String
    let font: UIFont
    let color: UIColor
    let lineHeight: CGFloat
    let key: String
    @Binding var height: CGFloat
    @Binding var drawn: Bool

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> WKWebView {
        let w = MathPool.take(hub: hub)
        w.configuration.userContentController.add(context.coordinator, name: "h")
        w.navigationDelegate = context.coordinator
        context.coordinator.ready = MathPool.loaded.contains(ObjectIdentifier(w))
        return w
    }

    func updateUIView(_ w: WKWebView, context: Context) {
        context.coordinator.parent = self
        context.coordinator.push(w)
    }

    static func dismantleUIView(_ w: WKWebView, coordinator: Coordinator) {
        w.configuration.userContentController.removeScriptMessageHandler(forName: "h")
        MathPool.give(w)
    }

    final class Coordinator: NSObject, WKScriptMessageHandler, WKNavigationDelegate {
        var parent: MathWeb
        var ready = false
        private var sent = ""
        private var seq = 0
        init(_ p: MathWeb) { parent = p }

        func push(_ w: WKWebView) {
            guard ready else { return }
            var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
            parent.color.resolvedColor(with: w.traitCollection).getRed(&r, green: &g, blue: &b, alpha: &a)
            let rgb = "rgba(\(Int(r * 255)),\(Int(g * 255)),\(Int(b * 255)),\(a))"
            let weight = parent.font.fontDescriptor.symbolicTraits.contains(.traitBold) ? "600" : "400"
            guard let t = try? JSONEncoder().encode([parent.text]), let arg = String(data: t, encoding: .utf8) else { return }
            let js = "draw(\(arg)[0], \(parent.font.pointSize), '\(rgb)', '\(weight)', \(parent.lineHeight)"
            if js != sent { sent = js; seq += 1; w.evaluateJavaScript(js + ", \(seq))") }
        }

        func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
            MathPool.loaded.insert(ObjectIdentifier(w))
            ready = true
            sent = ""
            push(w)
        }

        /// The page itself loads; a link in the text opens where a link in
        /// the text view would (the browser, or the app for a life:// one).
        func webView(_ w: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
            guard action.navigationType == .linkActivated, let u = action.request.url else { return decisionHandler(.allow) }
            UIApplication.shared.open(u)
            decisionHandler(.cancel)
        }

        func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
            guard let a = m.body as? [NSNumber], a.count == 3, a[1].intValue == seq else { return }
            let h = CGFloat(truncating: a[0])
            guard h > 0 else { return }
            if abs(h - parent.height) > 0.5 { parent.height = h }
            guard a[2].intValue == 1, !parent.drawn else { return }
            parent.drawn = true
            // Kept for the next time this paragraph opens (MathText.shots),
            // once the 300 ms re-measure has settled its height.
            let key = parent.key, n = seq
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self, weak w = m.webView] in
                guard let w, self?.seq == n else { return }
                w.takeSnapshot(with: nil) { img, _ in if let img { MathText.shots[key] = img } }
            }
        }
    }
}

/// Loaded math pages waiting for a paragraph. A fresh WKWebView takes a
/// beat to start and load before it can draw, which showed as a white gap
/// in every card each time a chat opened; a page from here only has to run
/// draw().
@MainActor enum MathPool {
    static var free: [WKWebView] = []
    static var loaded = Set<ObjectIdentifier>()
    private static let watcher = Watcher()

    static func take(hub: HubClient) -> WKWebView { free.popLast() ?? make(hub: hub) }

    static func give(_ w: WKWebView) {
        w.navigationDelegate = watcher
        if free.count < 16, loaded.contains(ObjectIdentifier(w)) { free.append(w) } else { loaded.remove(ObjectIdentifier(w)) }
    }

    /// A few pages loaded at launch, so the first chat draws from the pool too.
    static func warm(hub: HubClient) { while free.count < 6 { free.append(make(hub: hub)) } }

    private static func make(hub: HubClient) -> WKWebView {
        let c = WKWebViewConfiguration()
        c.setURLSchemeHandler(HubScheme(hub: hub), forURLScheme: "lifehub")
        let w = WKWebView(frame: CGRect(x: 0, y: 0, width: 600, height: 40), configuration: c)
        w.isOpaque = false
        w.backgroundColor = .clear
        w.scrollView.isScrollEnabled = false
        // Never a scroll bar inside the card while its height settles.
        w.scrollView.showsVerticalScrollIndicator = false
        w.scrollView.showsHorizontalScrollIndicator = false
        w.scrollView.backgroundColor = .clear
        w.navigationDelegate = watcher
        w.load(URLRequest(url: URL(string: "lifehub://hub/math-text")!))
        return w
    }

    /// Marks a pooled page loaded before any paragraph has taken it.
    final class Watcher: NSObject, WKNavigationDelegate {
        func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
            MainActor.assumeIsolated { _ = MathPool.loaded.insert(ObjectIdentifier(w)) }
        }
    }
}

/// `lifehub://hub/<path>` → the hub's `<path>` fetched with the token, so a
/// page in a web view can load the console's files; `/math-text` is the
/// app's own page (MathText).
final class HubScheme: NSObject, WKURLSchemeHandler {
    let hub: HubClient
    private var stopped = Set<ObjectIdentifier>()
    init(hub: HubClient) { self.hub = hub }

    /// The console's static files, fetched once per launch: every math
    /// paragraph is its own web view, and each one fetching KaTeX and ui.js
    /// from the hub made a card fill in paragraph by paragraph.
    nonisolated(unsafe) static var kept: [String: (Data, String)] = [:]

    /// Fetch what a math paragraph loads before the first card needs it.
    @MainActor static func warm(hub: HubClient) async {
        let fonts = ["Main-Regular", "Main-Bold", "Main-Italic", "Math-Italic", "Size1-Regular", "Size2-Regular", "AMS-Regular"]
        let paths = ["/static/vendor/katex/katex.min.css", "/static/vendor/katex/katex.min.js", "/static/ui.js"]
            + fonts.map { "/static/vendor/katex/fonts/KaTeX_\($0).woff2" }
        for p in paths where kept[p] == nil {
            if let r = try? await hub.asset(p) { kept[p] = r }
        }
    }

    func webView(_ w: WKWebView, start task: any WKURLSchemeTask) {
        guard let u = task.request.url else { return }
        let path = u.path(percentEncoded: true)
        if path == "/math-text" { reply(task, u, Data(MathText.page.utf8), "text/html"); return }
        let q = u.query(percentEncoded: true).map { "?" + $0 } ?? ""
        if let (d, mime) = Self.kept[path + q] { reply(task, u, d, mime); return }
        Task { @MainActor in
            do {
                let (d, mime) = try await hub.asset(path + q)
                if path.hasPrefix("/static/") { Self.kept[path + q] = (d, mime) }
                reply(task, u, d, mime)
            } catch {
                if stopped.remove(ObjectIdentifier(task)) == nil { task.didFailWithError(error) }
            }
        }
    }

    func webView(_ w: WKWebView, stop task: any WKURLSchemeTask) { stopped.insert(ObjectIdentifier(task)) }

    private func reply(_ task: any WKURLSchemeTask, _ u: URL, _ d: Data, _ mime: String) {
        if stopped.remove(ObjectIdentifier(task)) != nil { return }
        task.didReceive(URLResponse(url: u, mimeType: mime, expectedContentLength: d.count, textEncodingName: mime.hasPrefix("text") ? "utf-8" : nil))
        task.didReceive(d)
        task.didFinish()
    }
}

/// Whether a paragraph holds math by the console's rule (ui.js MATH_DISPLAY,
/// MATH_INLINE): only those leave the text view. A near miss costs a web
/// view drawing plain words, never lost text, since md() decides for real.
func hasMath(_ s: String) -> Bool {
    guard s.contains("$") else { return false }
    return s.contains(/\$\$[^$]+?\$\$/) || s.contains(/(?:^|[^\\\w$])\$[^\s$](?:[^$\n]*?[^\s$\\])?\$(?![\w$])/)
}
