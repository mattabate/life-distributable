#if targetEnvironment(macCatalyst)
import UIKit
import UniformTypeIdentifiers

/// A desktop window, never narrower than a phone held sideways. A screenshot
/// run (LIFE_SHOT) pins it to one size so shots compare run to run.
enum MacWindow {
    @MainActor static func size() {
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            scene.sizeRestrictions?.minimumSize = CGSize(width: 1100, height: 600)
            // No title row: the app's own top bar (MacTopBar) is the first
            // thing in the window, beside the traffic lights, like the
            // console's bar at the top of a browser tab.
            scene.titlebar?.titleVisibility = .hidden
            scene.titlebar?.toolbar = nil
            if ProcessInfo.processInfo.environment["LIFE_SHOT"] != nil {
                // LIFE_SHOT_W: a narrower window, to see the bar fold;
                // LIFE_SHOT_H: a taller one, to see a long page whole.
                let w = Double(ProcessInfo.processInfo.environment["LIFE_SHOT_W"] ?? "") ?? 1280
                let h = Double(ProcessInfo.processInfo.environment["LIFE_SHOT_H"] ?? "") ?? 860
                let s = CGSize(width: w, height: h)
                scene.sizeRestrictions?.maximumSize = s
                scene.sizeRestrictions?.minimumSize = s
            }
        }
    }
}

/// AppKit, reached by name: a Catalyst process has NSApplication loaded but
/// no header for it, and two of its calls have no UIKit twin.
enum MacApp {
    private static var shared: NSObject? {
        (NSClassFromString("NSApplication") as? NSObject.Type)?.value(forKey: "sharedApplication") as? NSObject
    }

    /// This app is the one in front, taking the keys: AppKit's own word
    /// (`NSApplication.isActive`), beside UIKit's `applicationState`.
    /// Unknown = yes.
    @MainActor static var isFront: Bool {
        guard let app = shared, app.responds(to: NSSelectorFromString("isActive")) else { return true }
        return (app.value(forKey: "isActive") as? Bool) ?? true
    }

    /// No Dock tile, no menu bar, no stolen focus: a screenshot copy runs
    /// beside the installed app without appearing next to it. Otherwise
    /// several shot runs put several "life" icons on the Dock at once, and
    /// macOS can answer the updater's `open` of the real app by bringing one
    /// of THOSE forward, which quits after its shot.
    @MainActor static func hideFromDock() {
        guard let app = shared else { return }
        let sel = NSSelectorFromString("setActivationPolicy:")
        guard app.responds(to: sel), let imp = app.method(for: sel) else { return }
        typealias Fn = @convention(c) (NSObject, Selector, Int) -> Bool
        _ = unsafeBitCast(imp, to: Fn.self)(app, sel, 1)  // NSApplicationActivationPolicyAccessory
    }

    /// A clean quit — AppKit's `terminate:` — so the window state is written
    /// and the next launch is not met with "The last time you opened life, it
    /// unexpectedly quit while reopening windows". `exit(0)` earned that dialog
    /// on every Update click and every screenshot run. Should
    /// AppKit refuse (a sheet, a delegate), the hard exit follows anyway.
    ///
    /// `terminate:` goes out as a run-loop block, never from inside a Task:
    /// a main-actor Task is a job of the main dispatch queue, the loop AppKit
    /// spins in `_shouldTerminate` does not re-enter that queue, and Catalyst's
    /// answer to "should terminate" arrives on it. Called straight from
    /// `MacUpdate.apply`'s Task, every Update click froze the app for ~33 s
    /// with its window and the green button still up, and no button or the
    /// hard exit below (then a Task too) able to run. The hard exit waits on
    /// no main thread.
    @MainActor static func quit() {
        DispatchQueue.global().asyncAfter(deadline: .now() + 5) { exit(0) }
        CFRunLoopPerformBlock(CFRunLoopGetMain(), CFRunLoopMode.commonModes.rawValue) {
            if let app = shared { _ = app.perform(NSSelectorFromString("terminate:"), with: nil) }
        }
        CFRunLoopWakeUp(CFRunLoopGetMain())
    }

    /// Every visible NSWindow's frame, the main window's first, each in the
    /// main window's own y-down points: where a screenshot run draws a sheet,
    /// which Catalyst hosts in a window of its own (2026-09-30). Neither
    /// AppKit's cached display nor the layer tree of such a window draws its
    /// UIKit content, so the frames are all this asks AppKit for.
    @MainActor static func windowFrames() -> [CGRect] {
        guard let app = shared, let windows = app.value(forKey: "windows") as? [NSObject] else { return [] }
        let frames = windows.filter { ($0.value(forKey: "isVisible") as? Bool) == true }
            .compactMap { ($0.value(forKey: "frame") as? NSValue)?.cgRectValue }
        guard let main = frames.first else { return [] }
        // AppKit's y runs up from the screen's bottom; the picture's runs down from the main window's top.
        return frames.map { CGRect(x: $0.minX - main.minX, y: main.maxY - $0.maxY, width: $0.width, height: $0.height) }
    }
}

/// ops/mac-screens.sh: launched with LIFE_SHOT=<png path>, the desktop app
/// draws its own window into that file once the page has loaded, then quits.
/// The app photographs itself, so no screen-recording permission is involved
/// and nothing but this window is ever captured.
enum MacShot {
    @MainActor static func runIfAsked() async {
        guard let path = ProcessInfo.processInfo.environment["LIFE_SHOT"], !path.isEmpty else { return }
        MacApp.hideFromDock()
        let wait = Double(ProcessInfo.processInfo.environment["LIFE_SHOT_WAIT"] ?? "") ?? 6
        try? await Task.sleep(for: .seconds(wait))
        // LIFE_FIND=<words>: the find bar open on them (⌘F), LIFE_FIND_NEXT=<n>
        // steps through to the n-th match, so the shot shows its yellow.
        if let q = ProcessInfo.processInfo.environment["LIFE_FIND"], !q.isEmpty {
            MacFind.shared.open()
            MacFind.shared.query = q
            try? await Task.sleep(for: .seconds(1))
            for _ in 0..<(Int(ProcessInfo.processInfo.environment["LIFE_FIND_NEXT"] ?? "") ?? 0) { MacFind.shared.next() }
            try? await Task.sleep(for: .seconds(1))
            print("macfind: \(MacFind.shared.count) matches for \(q), on \(MacFind.shared.current + 1)")
        }
        // LIFE_SHOT_DROP=<file>: that file dropped on the page's composer the
        // way Finder hands one over (its URL), so the shot shows what the box
        // made of it — a chip, never its contents typed in (2026-10-05).
        if let drop = ProcessInfo.processInfo.environment["LIFE_SHOT_DROP"], !drop.isEmpty,
           let root = UIApplication.shared.connectedScenes.compactMap({ ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first }).first,
           let box = firstView(PasteTextView.self, in: root) {
            box.paste(itemProviders: [NSItemProvider(item: URL(fileURLWithPath: drop) as NSURL, typeIdentifier: UTType.fileURL.identifier)])
            try? await Task.sleep(for: .seconds(1.5))
        }
        // Every AppKit window, back to front, over the main one's frame: a
        // Catalyst sheet (a calendar item's panel, 2026-09-30) is its own
        // NSWindow outside the scene's UIWindows, and a shot of the key
        // UIWindow alone showed the page under it with no sheet.
        if let window = UIApplication.shared.connectedScenes
            .compactMap({ ($0 as? UIWindowScene)?.keyWindow ?? ($0 as? UIWindowScene)?.windows.first }).first {
            // The sheets over the page, top to bottom: each presented
            // controller's view, at the frame AppKit gave its window.
            var sheets: [(UIView, CGRect)] = []
            var vc = window.rootViewController?.presentedViewController
            let frames = MacApp.windowFrames()
            var i = 1
            while let v = vc, let view = v.view {
                sheets.append((view, i < frames.count ? frames[i] : CGRect(origin: CGPoint(x: (window.bounds.width - view.bounds.width) / 2, y: (window.bounds.height - view.bounds.height) / 2), size: view.bounds.size)))
                vc = v.presentedViewController; i += 1
            }
            let image = hidingSecrets { UIGraphicsImageRenderer(bounds: window.bounds).image { ctx in
                // The page itself, not the window: with a sheet up the window
                // draws only its grey cover.
                (window.rootViewController?.view ?? window).drawHierarchy(in: window.bounds, afterScreenUpdates: true)
                for (view, rect) in sheets {
                    UIColor.black.withAlphaComponent(0.34).setFill(); ctx.fill(window.bounds)
                    view.drawHierarchy(in: rect, afterScreenUpdates: true)
                }
            } }
            try? image.pngData()?.write(to: URL(fileURLWithPath: path))
        }
        MacApp.quit()
    }

    @MainActor private static func firstView<V: UIView>(_ type: V.Type, in view: UIView) -> V? {
        if let v = view as? V { return v }
        for sub in view.subviews { if let v = firstView(type, in: sub) { return v } }
        return nil
    }
}
#endif
