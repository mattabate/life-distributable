import SwiftUI
import UIKit

/// "Start a session from wherever I am." An Ask button in the top-right
/// toolbar of every screen captures the app's own window (no system
/// screenshot, nothing lands in the Photos library) and opens the new-session
/// sheet with that image attached, so the agent sees exactly what the owner was
/// looking at when the thought struck.
@Observable @MainActor
final class SnapState {
    /// Button hidden while capturing so it is not in the picture.
    var capturing = false
    var pending: SnapImage?

    func snap(from file: String = "") async {
        capturing = true
        try? await Task.sleep(for: .milliseconds(60)) // let the hide render
        defer { capturing = false }
        // Same renderer "Chat about this" crops one component out of.
        guard let img = captureKeyWindow() else { return }
        pending = SnapImage(image: img, file: file)
    }

    /// The desktop's "+ New session": the console's one button (the owner
    /// 2026-09-29: "those are supposed to be the same button named new session
    /// and its supposed to take a screenshot"). It photographs the page it was
    /// pressed on and hands the picture to the Sessions page's empty chat —
    /// no sheet, no second window ("it should close the window when a session
    /// starts"). `deskGen` rebuilds that chat on every press.
    var desk: SnapImage?
    var deskGen = 0
    func snapForDesk(from file: String) {
        desk = captureKeyWindow().map { SnapImage(image: $0, file: file) }
        deskGen += 1
    }

    /// Whatever LifeShare left in the app-group inbox becomes a new-session
    /// sheet with those files attached — called when the app comes forward
    /// and when the extension opens `life://inbox`. Nothing there: no-op.
    func drainInbox() {
        let got = SharedInbox.drain()
        guard !got.files.isEmpty || !got.images.isEmpty else { return }
        if var p = pending {
            p.files += got.files; p.images += got.images; pending = p
        } else {
            pending = SnapImage(image: nil, files: got.files, images: got.images)
        }
    }
}

/// The hand-off from the share extension (app/LifeShare) to the app: the
/// extension cannot reach the hub (no token, no Keychain of ours) and cannot
/// present our composer, so it writes each shared item into the app group's
/// `Inbox/` and hands over; the app picks them up on its next foreground.
/// Both sides read the app group from Info.plist `LifeAppGroup`, which
/// project.yml fills from the LIFE_APP_GROUP build setting.
enum SharedInbox {
    static let group = Bundle.main.object(forInfoDictionaryKey: "LifeAppGroup") as? String ?? ""
    static var dir: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)?.appendingPathComponent("Inbox", isDirectory: true)
    }

    /// Read every file out of the inbox and delete it there: a picture
    /// becomes an image attachment, anything else a document.
    static func drain() -> (files: [AttachedFile], images: [UIImage]) {
        guard let dir, let names = try? FileManager.default.contentsOfDirectory(atPath: dir.path) else { return ([], []) }
        var files: [AttachedFile] = [], images: [UIImage] = []
        // The extension prefixes a sortable stamp so several items keep the
        // order they were shared in; the name after the first "_" is the real one.
        for n in names.sorted() {
            let url = dir.appendingPathComponent(n)
            defer { try? FileManager.default.removeItem(at: url) }
            guard let data = try? Data(contentsOf: url), !data.isEmpty else { continue }
            let name = n.split(separator: "_", maxSplits: 1).count == 2 ? String(n.split(separator: "_", maxSplits: 1)[1]) : n
            if isImageRef(name), let img = UIImage(data: data) { images.append(img) }
            else { files.append(AttachedFile(name: name, data: data)) }
        }
        return (files, images)
    }
}

/// A snap and WHERE it was taken. `file` is the Swift file that drew the
/// screen, picked up from `#fileID` at the button's call site — no table to
/// keep, and it cannot drift from the code. The prompt carries the location in
/// words, not only in the picture. The console does the same with its route
/// and view file (web/places.js).
struct SnapImage: Identifiable {
    let id = UUID()
    /// nil when the sheet opens over files alone (the share sheet road).
    let image: UIImage?
    var file = ""
    /// Files that came in through the share sheet of another app (a bank's
    /// PDF, an export) — SharedInbox hands them here, the sheet attaches them.
    var files: [AttachedFile] = []
    /// Pictures that came the same way.
    var images: [UIImage] = []
    /// "app/Life/Sources/ThreadsView.swift" from a `#fileID` of
    /// "Life/ThreadsView.swift"; empty when the call site named nothing.
    var path: String {
        guard let name = file.split(separator: "/").last, name.hasSuffix(".swift") else { return "" }
        return "app/Life/Sources/" + name
    }
}

struct SnapButton: View {
    @Bindable var state: SnapState
    /// The screen's own source file (see AskButtonModifier).
    var file = ""
    var body: some View {
        Button { Task { await state.snap(from: file) } } label: {
            Label("Ask", systemImage: "camera.viewfinder")
        }
        #if !targetEnvironment(macCatalyst)
        // (The desktop bar picks the style: words, or icons when narrow.)
        .labelStyle(.titleAndIcon)
        #endif
        .help("Snap this screen and start a session")
        .buttonStyle(.borderedProminent)
        .opacity(state.capturing ? 0 : 1)
        .accessibilityLabel("Snap this screen and start a session")
    }
}

/// Ask lives in each screen's toolbar (top-right) rather than as a floating
/// overlay: an overlay sat on top of whatever button the screen put there
/// (Goals +, thread … menu). Toolbar items share the row, so every
/// screen adds `.askButton()` and keeps its own trailing items.
struct AskButtonModifier: ViewModifier {
    @Environment(SnapState.self) private var snap
    let file: String
    func body(content: Content) -> some View {
        #if targetEnvironment(macCatalyst)
        // The desktop app has ONE Ask, on its top bar beside + New session
        // (MacTopBar), the way the console has one.
        content
        #else
        content.toolbar { ToolbarItem(placement: .topBarTrailing) { SnapButton(state: snap, file: file) } }
        #endif
    }
}

extension View {
    /// `file` defaults to `#fileID`, which Swift resolves AT THE CALL SITE —
    /// so every `.askButton()` already reports the screen it is attached to
    /// and no screen has to remember to say its own name.
    func askButton(file: String = #fileID) -> some View { modifier(AskButtonModifier(file: file)) }
}
