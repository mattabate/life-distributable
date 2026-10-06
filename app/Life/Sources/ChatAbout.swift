import SwiftUI
import UIKit

// "Chat with this component": hold down anything on the screen — a limit
// card, an account row, a reply bubble — and talk to a session *about that
// thing*, with the component itself and the numbers it is showing handed to
// the agent. The Ask button snaps the whole screen; this is the same idea one
// component down, so a question about it needs no description of what is on
// screen.
//
// Where the answer goes: inside a session, chatting with something
// stays in that session; anywhere else it starts a new one.

/// What a component is and what it is showing right now, in words an agent can
/// act on. `facts` is written at the call site because only the view knows the
/// values it just drew; `code` points at the file that draws it, so "how is
/// this computed" is one Read away.
struct ChatSubject: Hashable {
    var screen: String
    var title: String
    var facts: String = ""
    var code: String? = nil

    /// The SwiftUI file that DREW it, taken from the call site's `#fileID`
    /// (see `chatAbout`). What a call site writes in `code` is usually where
    /// the number comes from — a hub package, an endpoint — and a session
    /// asked to change how the thing looks needs the other end too, in words
    /// rather than pixels.
    func drawnIn(_ file: String) -> ChatSubject {
        guard let name = file.split(separator: "/").last, name.hasSuffix(".swift") else { return self }
        let path = "app/Life/Sources/" + name
        var out = self
        if let c = code, !c.isEmpty { out.code = c.contains(path) ? c : path + " ← " + c } else { out.code = path }
        return out
    }
}

enum ChatTarget: Hashable {
    case newSession
    case session(id: String, title: String)

    var sessionID: String? { if case .session(let id, _) = self { return id }; return nil }
    var sessionTitle: String? { if case .session(_, let t) = self { return t }; return nil }
}

/// Opens the chat sheet for the component this is attached to; `quote` carries
/// the words the owner had selected, when the selection menu started it.
@MainActor final class ChatHook {
    let run: (String?) -> Void
    init(_ run: @escaping (String?) -> Void) { self.run = run }
}

private struct ChatTargetKey: EnvironmentKey { static let defaultValue: ChatTarget = .newSession }
private struct ChatHookKey: EnvironmentKey { static let defaultValue: ChatHook? = nil }

extension EnvironmentValues {
    /// Which session a component's chat belongs to. ThreadDetail sets it; every
    /// other screen leaves it at `.newSession`.
    var chatTarget: ChatTarget {
        get { self[ChatTargetKey.self] }
        set { self[ChatTargetKey.self] = newValue }
    }
    /// Set by `.chatAbout` for its own subtree so selectable text inside the
    /// component can open the same conversation (see StyledText).
    var chatHook: ChatHook? {
        get { self[ChatHookKey.self] }
        set { self[ChatHookKey.self] = newValue }
    }
}

struct ChatRequest: Identifiable {
    let id = UUID()
    let subject: ChatSubject
    let image: UIImage?
    let target: ChatTarget
    let quote: String?
}

/// Holds the component just held down, until the sheet is done with it.
@Observable @MainActor final class ChatSpot {
    var pending: ChatRequest?

    func open(_ subject: ChatSubject, frame: CGRect, target: ChatTarget, quote: String? = nil) {
        Task { @MainActor in
            // The context menu (or the text selection menu) is still on screen
            // with everything behind it blurred: photograph the component only
            // once the real screen is back, or the crop is a gray smear.
            try? await Task.sleep(for: .milliseconds(420))
            pending = ChatRequest(subject: subject, image: cropOfScreen(frame), target: target, quote: quote)
        }
    }
}

/// The app's own window as an image — no system screenshot, nothing lands in
/// the Photos library. Shared by the Ask button (whole screen) and
/// "Chat about this" (cropped to one component).
@MainActor func captureKeyWindow() -> UIImage? {
    guard let window = UIApplication.shared.connectedScenes
        .compactMap({ $0 as? UIWindowScene }).flatMap(\.windows).first(where: \.isKeyWindow) else { return nil }
    return hidingSecrets {
        UIGraphicsImageRenderer(bounds: window.bounds).image { _ in
            window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
        }
    }
}

/// A decider code showing in a text box is never photographed. A snap goes to
/// a session and is kept as a blob every session can read, and the code is
/// the one thing no session may hold: on 2026-09-30 a snap of Settings, taken
/// to show a failed save with the code revealed, carried it, and the code had
/// to be rotated. Any plain text field whose words have the code's shape is
/// hidden for the length of the drawing — its row stays, the box is blank.
@MainActor func hidingSecrets<T>(_ draw: () -> T) -> T {
    var roots: [UIView] = []
    for w in UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).flatMap(\.windows) {
        roots.append(w)
        var vc = w.rootViewController?.presentedViewController
        while let v = vc { if let view = v.viewIfLoaded { roots.append(view) }; vc = v.presentedViewController }
    }
    var fields: [UITextField] = []
    func walk(_ v: UIView) {
        if let f = v as? UITextField, !f.isHidden, !f.isSecureTextEntry,
           SettingsView.looksLikeDeciderCode(HubClient.normalizeDecider(f.text ?? "")),
           !fields.contains(where: { $0 === f }) { fields.append(f) }
        v.subviews.forEach(walk)
    }
    roots.forEach(walk)
    fields.forEach { $0.isHidden = true }
    defer { fields.forEach { $0.isHidden = false } }
    return draw()
}

/// Window snapshot cropped to a component's frame (global coordinates), with a
/// little air around it so the card's own edges are visible. nil when the
/// component is off-screen or too small to be worth a picture.
@MainActor func cropOfScreen(_ frame: CGRect) -> UIImage? {
    guard let shot = captureKeyWindow(), let cg = shot.cgImage else { return nil }
    let bounds = CGRect(origin: .zero, size: shot.size)
    let r = frame.insetBy(dx: -10, dy: -10).intersection(bounds)
    guard !r.isNull, r.width > 16, r.height > 16 else { return nil }
    let s = shot.scale
    let px = CGRect(x: r.minX * s, y: r.minY * s, width: r.width * s, height: r.height * s)
    guard let cropped = cg.cropping(to: px) else { return nil }
    return UIImage(cgImage: cropped, scale: s, orientation: shot.imageOrientation)
}

/// Frame of the anchored component, kept off SwiftUI state on purpose: it
/// changes on every scroll tick and must not invalidate the row it belongs to.
@MainActor private final class FrameBox { var rect: CGRect = .zero }

struct ChatAnchor: ViewModifier {
    let subject: ChatSubject
    @Environment(ChatSpot.self) private var spot: ChatSpot?
    @Environment(\.chatTarget) private var target
    @State private var box = FrameBox()

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { box.rect = $0 }
            .environment(\.chatHook, spot.map { s in
                ChatHook { quote in s.open(subject, frame: box.rect, target: target, quote: quote) }
            })
            .contextMenu {
                Button {
                    spot?.open(subject, frame: box.rect, target: target)
                } label: {
                    Label(target.sessionID == nil ? "Chat about this" : "Ask this session about it",
                          systemImage: "bubble.and.pencil")
                }
            }
    }
}

extension View {
    /// Hold this component down → "Chat about this". `file` defaults to
    /// `#fileID`, resolved at the call site, so every component reports the
    /// screen file it lives in without anyone having to type it.
    func chatAbout(_ subject: ChatSubject, file: String = #fileID) -> some View {
        modifier(ChatAnchor(subject: subject.drawnIn(file)))
    }
    /// Shorthand for the common case.
    func chatAbout(_ title: String, on screen: String, facts: String = "", code: String? = nil, file: String = #fileID) -> some View {
        modifier(ChatAnchor(subject: ChatSubject(screen: screen, title: title, facts: facts, code: code).drawnIn(file)))
    }
}

/// Everything the session is told about the component. It comes AFTER the
/// owner's question on purpose: the first line of a message becomes the
/// session's title and the board's preview, and that has to be what they
/// asked, not a block of metadata.
func chatPreamble(_ r: ChatRequest) -> String {
    var s = r.image == nil
        ? "[The user pressed \"Talk about it\" on this in the life app. No picture; it is described below.]\n"
        : "[The user held this component down in the life app and chose \"Chat about this\". The attached image is that component, cropped from their screen — not the whole page.]\n"
    s += "Screen: \(r.subject.screen)\n"
    s += "Component: \(r.subject.title)\n"
    if let c = r.subject.code, !c.isEmpty { s += "Drawn by: \(c)\n" }
    if let q = r.quote, !q.isEmpty { s += "Text they had selected: \"\(q)\"\n" }
    if !r.subject.facts.isEmpty { s += "What it was showing them:\n\(r.subject.facts)\n" }
    return s
}

func chatPrompt(_ r: ChatRequest, question: String) -> String {
    let q = question.trimmingCharacters(in: .whitespacesAndNewlines)
    let opener = "About “\(r.subject.title)” on \(r.subject.screen): " + (q.isEmpty
        ? "explain it — what it is, where each number comes from, whether it is actually right (and fix it if it is not)."
        : q)
    return opener + "\n\n" + chatPreamble(r)
}

/// The sheet a held-down component opens: the component as it was on screen, where the
/// message is going, and the same composer (dictation, photos) as a session.
struct ChatAboutSheet: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    let request: ChatRequest
    @State private var draft = Draft()
    @State private var attachments = AttachmentDraft()
    @State private var inSession: Bool
    @State private var busy = false
    @State private var error: String?
    @State private var showPreamble = false

    init(request: ChatRequest) {
        self.request = request
        // Inside a session it stays in that session;
        // anywhere else it is a new one.
        _inSession = State(initialValue: request.target.sessionID != nil)
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        if let img = request.image {
                            // Same as NewThreadSheet: a definite height (maxHeight
                            // caps nothing under a ScrollView's nil proposal), and
                            // the centring frame last so the border hugs the snap.
                            Image(uiImage: img).resizable().scaledToFit()
                                .frame(height: 260)
                                .clipShape(RoundedRectangle(cornerRadius: 12))
                                .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Color.secondary.opacity(0.35), lineWidth: 1) }
                                .frame(maxWidth: .infinity, alignment: .center)
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            Text(request.subject.title).font(.headline)
                            Text("on \(request.subject.screen)").font(.caption).foregroundStyle(.secondary)
                        }
                        if let q = request.quote, !q.isEmpty {
                            Text("“\(q)”").font(.subheadline).italic().foregroundStyle(.secondary)
                        }
                        destination
                        DisclosureGroup("What the session is told", isExpanded: $showPreamble) {
                            Text(chatPreamble(request))
                                .font(.caption2).foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }.font(.caption)
                        if let error { ErrorBanner(message: error) }
                    }.padding()
                }
                Divider()
                Composer(draft: draft, attachments: attachments,
                         placeholder: "What about this? (or just send)", sending: busy, send: send, allowEmpty: true)
                    .padding(10)
            }
            .navigationTitle("Chat about this")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }        }
    }

    @ViewBuilder private var destination: some View {
        if let title = request.target.sessionTitle {
            VStack(alignment: .leading, spacing: 4) {
                Picker("Where", selection: $inSession) {
                    Text("This session").tag(true)
                    Text("New session").tag(false)
                }.pickerStyle(.segmented)
                Text(inSession ? "Goes to “\(title)”, which already has the context." : "Starts a fresh session that knows only this component.")
                    .font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    private var threadID: String? { inSession ? request.target.sessionID : nil }

    private func send() {
        busy = true
        let question = draft.text
        Task {
            do {
                var refs = try await attachments.upload(to: hub, threadID: threadID)
                if let img = request.image, let jpeg = img.jpegData(compressionQuality: 0.85) {
                    // The component goes first: it is what the message is about.
                    let ref = try await hub.uploadSessionPhoto(jpeg, threadID: threadID, size: img.size)
                    refs.insert(ref, at: 0)
                }
                let text = chatPrompt(request, question: question)
                let id: String
                if let t = threadID {
                    id = try await hub.sendThread(t, text: text, attachments: refs).id
                } else {
                    id = try await hub.createThread(prompt: text, attachments: refs).id
                }
                let ids = attachments.assetIDs
                dismiss()
                await PhotoCleanup.offerDeletingScreenshots(ids)
            } catch {
                self.error = error.localizedDescription
                busy = false
            }
        }
    }
}
