import UIKit
import UniformTypeIdentifiers

/// "Share → Life" from any app on the phone: a way to get PDFs and pictures
/// to a session from the phone. The share sheet's
/// PDFs and pictures land here; this extension has no hub token and no
/// composer, so it does one thing: copy each item into the app group's
/// `Inbox/` and hand over to the app (`life://inbox`), which opens the
/// new-session sheet with them attached (SharedInbox in Snap.swift). If iOS
/// refuses the hand-over, the app drains the inbox the next time it comes
/// forward — nothing is lost either way.
final class ShareViewController: UIViewController {
    /// Info.plist `LifeAppGroup` ← project.yml LIFE_APP_GROUP.
    private let group = Bundle.main.object(forInfoDictionaryKey: "LifeAppGroup") as? String ?? ""
    private let label = UILabel()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        label.text = "Sending to Life…"
        label.font = .preferredFont(forTextStyle: .headline)
        label.textAlignment = .center
        label.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(label)
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            label.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            label.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 20),
        ])
        Task { await run() }
    }

    private var inbox: URL? {
        guard let c = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
        let dir = c.appendingPathComponent("Inbox", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private func run() async {
        guard let inbox else { finish("Life is not installed with sharing"); return }
        let providers = (extensionContext?.inputItems as? [NSExtensionItem])?.flatMap { $0.attachments ?? [] } ?? []
        var n = 0
        for (i, p) in providers.enumerated() {
            guard let (name, data) = await load(p) else { continue }
            // A sortable stamp keeps the shared order; the app strips it.
            let stamp = String(format: "%.0f-%02d", Date().timeIntervalSince1970, i)
            let url = inbox.appendingPathComponent(stamp + "_" + name)
            if (try? data.write(to: url, options: .atomic)) != nil { n += 1 }
        }
        guard n > 0 else { finish("Nothing Life can take"); return }
        label.text = n == 1 ? "Sent to Life" : "Sent \(n) files to Life"
        try? await Task.sleep(for: .milliseconds(400))
        openApp(URL(string: "life://inbox")!)
        extensionContext?.completeRequest(returningItems: nil)
    }

    private func finish(_ why: String) {
        label.text = why
        Task {
            try? await Task.sleep(for: .seconds(1))
            extensionContext?.cancelRequest(withError: NSError(domain: "life", code: 1, userInfo: [NSLocalizedDescriptionKey: why]))
        }
    }

    /// The bytes and a name for one shared item. A PDF or any other file
    /// comes with its name; a picture without one is stamped as a JPEG.
    private func load(_ p: NSItemProvider) async -> (String, Data)? {
        // Files first (public.data covers PDF, CSV, JSON, images-as-files).
        if p.hasItemConformingToTypeIdentifier(UTType.data.identifier), !p.hasItemConformingToTypeIdentifier(UTType.url.identifier) || p.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
            if let r = await file(p, type: UTType.data.identifier) { return r }
        }
        if p.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier),
           let item = try? await p.loadItem(forTypeIdentifier: UTType.fileURL.identifier), let url = item as? URL,
           let d = try? Data(contentsOf: url) {
            return (url.lastPathComponent, d)
        }
        if p.hasItemConformingToTypeIdentifier(UTType.image.identifier),
           let item = try? await p.loadItem(forTypeIdentifier: UTType.image.identifier) {
            if let url = item as? URL, let d = try? Data(contentsOf: url) { return (url.lastPathComponent, d) }
            if let img = item as? UIImage, let d = img.jpegData(compressionQuality: 0.85) { return ("shared.jpg", d) }
            if let d = item as? Data { return ("shared.jpg", d) }
        }
        return nil
    }

    /// `loadFileRepresentation` hands over a temp URL that dies with the
    /// handler, so the copy happens inside it.
    private func file(_ p: NSItemProvider, type: String) async -> (String, Data)? {
        await withCheckedContinuation { cont in
            p.loadFileRepresentation(forTypeIdentifier: type) { url, _ in
                guard let url, let d = try? Data(contentsOf: url), !d.isEmpty else { cont.resume(returning: nil); return }
                var name = url.lastPathComponent
                if name.isEmpty || !name.contains(".") { name = (p.suggestedName ?? "shared") + "." + (UTType(type)?.preferredFilenameExtension ?? "bin") }
                cont.resume(returning: (name, d))
            }
        }
    }

    /// Ask the app to come forward. `extensionContext.open` is the documented
    /// road (Today widgets) and works for share extensions on recent iOS; the
    /// responder-chain fallback is the long-standing one. Neither working is
    /// fine: the app drains the inbox when it is next opened.
    private func openApp(_ url: URL) {
        extensionContext?.open(url) { ok in
            guard !ok else { return }
            DispatchQueue.main.async {
                var r: UIResponder? = self
                while let x = r {
                    if x.responds(to: NSSelectorFromString("openURL:")) { _ = x.perform(NSSelectorFromString("openURL:"), with: url); return }
                    r = x.next
                }
            }
        }
    }
}
