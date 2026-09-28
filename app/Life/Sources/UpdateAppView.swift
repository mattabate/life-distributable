import SwiftUI

/// Getting the newest app is its own thing, not a setting: a row at the top of More opens this modal, one tap from the tab bar
/// instead of More → Settings → scroll past the hub fields, push and hub
/// status. Settings keeps what is genuinely a setting and shows the same row.
///
/// ONE number, one action: which build is newest and what tap gets it is
/// decided in AppUpdate.swift, and this screen renders the answer rather than
/// the arithmetic. The lanes below the answer are
/// deliberately unnumbered.
struct UpdateAppSheet: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @Environment(BoardStore.self) private var store
    private var ota: OTABuild? { store.status?.ota }
    @State private var installing = false
    @State private var installTail: [String] = []
    @State private var installError: String?
    @State private var buildStartedAt: Date?
    @State private var logAt: Date?
    private var myBuild: Int { Int(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "") ?? 0 }
    private var update: AppUpdate { AppUpdate(ota: ota, mine: myBuild) }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    if installing {
                        HStack { ProgressView(); Text("Building on the Mac…").foregroundStyle(.secondary) }
                    } else {
                        switch update.action {
                        case .install(let u):
                            Button { install(u) } label: {
                                Label(update.buttonTitle, systemImage: "square.and.arrow.down")
                            }
                        case .build:
                            Button { runInstall(lane: "ota", thenInstall: true) } label: {
                                Label(update.buttonTitle, systemImage: "square.and.arrow.down")
                            }
                        case .none:
                            Label(update.buttonTitle, systemImage: "checkmark.circle").foregroundStyle(.secondary)
                        }
                    }
                    Text(update.caption).font(.caption).foregroundStyle(.secondary)
                }
                Section {
                    Button { runInstall(lane: "ota", thenInstall: true) } label: {
                        Label("Rebuild and reinstall (over the air)", systemImage: "hammer")
                    }
                    .disabled(installing)
                    Button { runInstall(lane: "lan", thenInstall: false) } label: {
                        Label("Reinstall over the Mac's Wi-Fi (LAN only)", systemImage: "arrow.down.app")
                    }
                    .disabled(installing)
                } header: { Text("Other ways") }
                if installError != nil || !freshTail.isEmpty {
                    Section("On the Mac") {
                        if let installError { Text(installError).font(.caption).foregroundStyle(.red) }
                        ForEach(freshTail, id: \.self) { Text($0).font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(2) }
                    }
                }
            }
            .navigationTitle("Update app")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task { await store.loadStatus(hub) }
            .refreshable { await store.loadStatus(hub) }
        }
    }

    /// Hand the link to iOS, then let InstallState take over: once the install
    /// prompt is answered the app quits itself, so the next icon tap runs the
    /// new bundle with no manual restart. The old Settings row
    /// was a plain Link and skipped that.
    private func install(_ u: URL) {
        openURL(u)
        AppDelegate.push.install.tapped("build \(update.newest)")
    }

    /// Build on the Mac, then hand the finished build straight to iOS — the
    /// point is to end up on the newest app, not to be told a build exists.
    private func runInstall(lane: String, thenInstall: Bool) {
        buildStartedAt = Date()
        installTail = []
        installError = nil
        Task {
            do { try await hub.appInstall(lane: lane); installing = true } catch { installError = error.localizedDescription; return }
            while installing {
                try? await Task.sleep(for: .seconds(4))
                guard let s = try? await hub.appInstallStatus() else { continue }
                installing = s.running; installTail = s.tail; logAt = s.log_at
                if let o = s.ota { store.status?.ota = o }
            }
            if thenInstall, let o = ota, o.build > myBuild, let u = o.installURL { install(u) }
        }
    }

    /// The log file outlives its run, so anything older than the build we
    /// started belongs to a previous one and must not be shown as progress.
    private var freshTail: [String] {
        guard let started = buildStartedAt else { return [] }
        if installing { return installTail }
        guard let at = logAt, at >= started.addingTimeInterval(-5) else { return [] }
        return installTail
    }
}

/// The row that opens the modal. Same wording in More and in Settings, so the
/// two read as one thing in two places.
struct UpdateAppRow: View {
    @Binding var showUpdate: Bool
    let ota: OTABuild?
    private var update: AppUpdate {
        AppUpdate(ota: ota, mine: Int(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "") ?? 0)
    }
    var body: some View {
        Button { showUpdate = true } label: {
            HStack {
                Label("Update app", systemImage: "square.and.arrow.down")
                Spacer()
                // Plain grey text, never a badge or a red dot: this is pulled,
                // like Recommendations. It says what is waiting; it does not nag.
                // And it is the SAME number the modal will show.
                if !update.isUpToDate {
                    Text("build \(update.newest)").font(.caption).foregroundStyle(.secondary).monospacedDigit()
                }
            }
        }
        .foregroundStyle(.primary)  // a Button in a List tints itself blue otherwise
    }
}
