#if targetEnvironment(macCatalyst)
import SwiftUI
import Darwin

/// A new build waiting to go in, so a rebuild never quits the app out from
/// under the owner. `ops/install-mac.sh` stages a build made
/// while the app is open instead of quitting it, and starts `ops/mac-swap.sh`,
/// which holds a copy beside the old app and swaps it in the moment this app
/// has quit. This watches for that staged build and, when there is one, the
/// top bar shows Update: the click leaves a note asking to be reopened, then
/// quits, and the app is back in seconds.
///
/// The ordinary way in is the "Install desktop build N" card
/// (the phone's teal install cell, for the Mac): its click runs the hub's
/// `mac` lane (`ops/mac-apply.sh`), which builds if nothing is staged and
/// then drops an `apply` flag in the updater's folder; this sees the flag and
/// quits the same way the button does. On every launch the app tells the hub
/// which build it runs (`POST /api/v1/app/mac`), and the hub closes the cards
/// that build satisfies — the Mac's version of the phone's device report.
@Observable @MainActor
final class MacUpdate {
    static let shared = MacUpdate()
    /// The staged build's number, while one is staged AND the updater is
    /// alive to put it in — a button that quit into nothing would be worse
    /// than no button. Any staged build counts; `ops/install-mac.sh` numbers
    /// it past the one running, and the button says the number, so it never
    /// reads as an update to the build the corner already shows.
    private(set) var staged: Int?
    /// A build the Install card asked the hub to make (nothing was staged):
    /// the bar says so until the build lands and the `apply` flag quits us.
    var building: Int?

    static let build = Int(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "") ?? 0
    private let dir = URL(fileURLWithPath: NSHomeDirectory()).appending(path: "Library/Application Support/life-mac")
    private var started = false
    private var quitting = false

    func start(hub: HubClient) {
        guard !started else { return }
        started = true
        Task { await report(hub: hub) }
        Task {
            while !Task.isCancelled {
                check()
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    /// Tell the hub this build, until it hears (the hub may be restarting).
    private func report(hub: HubClient) async {
        guard Self.build > 0 else { return }
        // A screenshot copy (ops/mac-screens.sh) is not the installed app:
        // its report would overwrite `mac_build` with the build under test,
        // closing that build's Install card before the owner had it, and a
        // `make check` copy would say "build 1" and reopen one.
        guard ProcessInfo.processInfo.environment["LIFE_SHOT"] == nil else { return }
        for _ in 0..<20 {
            if (try? await hub.reportMacBuild(Self.build)) != nil { return }
            try? await Task.sleep(for: .seconds(30))
        }
    }

    private func check() {
        let plist = dir.appending(path: "next/life.app/Contents/Info.plist")
        guard let info = NSDictionary(contentsOf: plist),
              let n = Int(info["CFBundleVersion"] as? String ?? ""),
              updaterAlive() else { staged = nil; return }
        staged = n
        if let b = building, n >= b { building = nil }
        // The Install card's click (ops/mac-apply.sh) asks us to go now.
        let flag = dir.appending(path: "apply")
        if FileManager.default.fileExists(atPath: flag.path) {
            try? FileManager.default.removeItem(at: flag)
            apply()
        }
    }

    private func updaterAlive() -> Bool {
        guard let s = try? String(contentsOf: dir.appending(path: "swap.pid"), encoding: .utf8),
              let pid = pid_t(s.trimmingCharacters(in: .whitespacesAndNewlines)) else { return false }
        return kill(pid, 0) == 0
    }

    /// Quit so the updater can swap the build in, and ask it to reopen.
    func apply() {
        guard !quitting else { return }
        quitting = true
        Task {
            FileManager.default.createFile(atPath: dir.appending(path: "relaunch").path, contents: Data())
            MacApp.quit()
        }
    }

    /// The card's click on this Mac when the build is already staged: no
    /// need to wait for the hub's flag. A box with unsent words still gets
    /// its say — the bar's Update asks the same question.
    func applyFromCard() {
        if Draft.anyUnsent { askBeforeApply = true } else { apply() }
    }
    var askBeforeApply = false
}

/// The top bar's Update: only there while a build is waiting (or being made).
struct MacUpdateButton: View {
    @State private var update = MacUpdate.shared
    @State private var asking = false
    var body: some View {
        if let b = update.building, update.staged == nil {
            Label("Building \(b)…", systemImage: "hammer.fill")
                .font(.caption).foregroundStyle(.secondary)
                .help("The Install card is building the desktop app; it restarts on its own when the build is ready")
        } else if let n = update.staged {
            Button { if Draft.anyUnsent { asking = true } else { update.apply() } } label: {
                Label("Update \(String(n))", systemImage: "arrow.down.circle.fill")
            }
            .buttonStyle(.borderedProminent).tint(.green).controlSize(.small)
            .help("Build \(n) is ready: quit and reopen on it")
            // The reopened app starts with empty boxes: words typed
            // and not sent would go with the old one.
            .confirmationDialog("A message box has words you haven't sent", isPresented: $asking) {
                Button("Update anyway", role: .destructive) { update.apply() }
                Button("Not now", role: .cancel) {}
            }
            .confirmationDialog("A message box has words you haven't sent", isPresented: $update.askBeforeApply) {
                Button("Install anyway", role: .destructive) { update.apply() }
                Button("Not now", role: .cancel) {}
            }
        }
    }
}
#endif
