import SwiftUI

/// Settings — ONLY the things that live on this phone.
///
/// Anything a session or a terminal command can set is set there, not here;
/// the console has no Settings page at all (`web/app.js` lists where each of
/// its old boxes went).
///
/// This screen survives for one reason, and every row on it has to earn that
/// reason: **the hub address, the hub token and the decider code exist only in
/// this device's Keychain**. No session, no terminal command and no console
/// page can reach them, and until they are set the app cannot talk to the hub
/// at all — so a fresh install has nowhere else to start. Push registration
/// and the install lanes are the same kind of thing: they are facts about this
/// handset, not about the hub.
///
/// What was here and is not any more: pending-approval and job counts (the
/// board already says it), and the hub clock (a stalled connector shows up as
/// a stale "Newest" on Sources; a session reads `GET /status` for the rest).
/// Nothing that a session could set for the owner belongs on this screen. If a
/// new row wants in, ask whether it can be typed on the hub's machine instead — if it can,
/// it does not go here.
struct SettingsView: View {
    @Environment(HubClient.self) private var hub
    @Environment(PushState.self) private var push
    @State private var pushResult: String?
    @State private var status: String?
    @Environment(BoardStore.self) private var store
    private var hubStatus: HubStatus? { store.status }
    // The install lanes and every build number live in UpdateAppView.swift
    // now; this screen only opens that modal.
    @State private var showUpdate = false
    @State private var revealCode = false
    @State private var deciderCheck: DeciderStatus?
    @State private var usageState: UsageState?

    /// What the code row says. Never a bare "set" again: either the hub has
    /// confirmed this exact code, or the row admits it does not know. The old
    /// row said "set" about a code the hub was refusing.
    var deciderNote: String {
        if let c = deciderCheck {
            if !c.armed { return "no code armed on the hub — approvals take the token alone" }
            if !c.sent { return "not saved — approvals will fail until you paste it in" }
            return c.ok
                ? "checked — the hub accepts this code, and Face ID unlocks it"
                : "WRONG — the hub refuses this code. Replace it with the one in Apple Passwords."
        }
        if !hub.deciderSaved { return "paste it from Apple Passwords once; Face ID unlocks it after that" }
        return "saved, not checked — tap Check code"
    }

    var deciderColor: Color {
        guard let c = deciderCheck else { return hub.deciderSaved ? .secondary : .red }
        if !c.armed { return .secondary }
        return c.ok ? .green : .red
    }

    /// The hub token as `cmd/hub` mints it: 48 hex characters.
    nonisolated static func looksLikeToken(_ s: String) -> Bool {
        s.count == 48 && s.allSatisfy { $0.isHexDigit }
    }
    /// The decider code as `ops/decider-set.sh` mints it: four groups of five
    /// from a base32 alphabet without I, L, O, U — after `normalizeDecider`.
    nonisolated static func looksLikeDeciderCode(_ s: String) -> Bool {
        let groups = s.split(separator: "-", omittingEmptySubsequences: false)
        let alphabet = Set("0123456789ABCDEFGHJKMNPQRSTVWXYZ")
        return groups.count == 4 && groups.allSatisfy { $0.count == 5 && $0.allSatisfy { alphabet.contains($0) } }
    }

    /// Ask the hub, with Face ID, whether the saved code is the right one.
    func checkDeciderNow() {
        deciderCheck = nil
        Task {
            do { deciderCheck = try await hub.checkDecider(unlock: true) }
            catch { deciderCheck = nil; status = error.localizedDescription }
        }
    }

    var body: some View {
        @Bindable var hub = hub
        Form {
            Section("Hub") {
                // Labelled: once filled, a placeholder-only field is just a
                // URL and a row of dots with nothing saying what they are.
                LabeledContent("Address") {
                    TextField("https://…", text: $hub.baseURL).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .keyboardType(.URL).multilineTextAlignment(.trailing)
                }
                LabeledContent("Token") {
                    SecureField("ops/secrets/hub.token", text: $hub.token).multilineTextAlignment(.trailing)
                }
                HStack {
                    // Paste token sits one row above Paste code; a decider
                    // code in the token field sends every request 401 and
                    // locks the phone out of the hub — its own install card
                    // included. A paste only
                    // lands in the box its shape belongs to; the wrong shape
                    // is named, and the working value stays.
                    Button("Paste token") {
                        let t = (UIPasteboard.general.string ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                        if Self.looksLikeToken(t) { hub.token = t; status = nil }
                        else if Self.looksLikeDeciderCode(t) { status = "that is the decider code — it goes in the row below, not in Token" }
                        else { status = "the clipboard is not a hub token (48 hex characters, ops/secrets/hub.token)" }
                    }
                    Spacer()
                    Text("\(hub.token.count) chars").font(.caption).foregroundStyle(hub.token.count == 48 ? Color.secondary : Color.red)
                }
                // Second credential for Approve/Deny: the hub token is on the
                // hub's machine where every session reads it, this is not.
                // `ops/decider-set.sh` prints the code; type it here.
                //
                // It is REVEALABLE and it is CHECKABLE. A plain SecureField
                // is a row of dots that says "set" whatever it holds, so a
                // wrong code only shows up as an approval failing at the
                // moment it is needed. "set" is not a fact about the code;
                // `ok` from the hub is.
                //
                // Typed ONCE, then never again: Face ID unlocks it at the
                // moment of an approval. The field is only a box to paste into — the moment it is
                // saved it empties, because the stored copy is behind Face ID
                // and this screen cannot read it back.
                if !hub.deciderSaved || !hub.decider.isEmpty {
                    LabeledContent("Decider code") {
                        HStack {
                            if revealCode {
                                TextField("paste from Apple Passwords", text: $hub.decider)
                                    .textInputAutocapitalization(.characters).autocorrectionDisabled()
                                    .keyboardType(.asciiCapable).multilineTextAlignment(.trailing)
                                    .font(.system(.body, design: .monospaced))
                            } else {
                                SecureField("paste from Apple Passwords", text: $hub.decider).multilineTextAlignment(.trailing)
                            }
                            Button { revealCode.toggle() } label: {
                                Image(systemName: revealCode ? "eye.slash" : "eye")
                            }.buttonStyle(.plain).foregroundStyle(.tint)
                        }
                    }
                    HStack {
                        Button("Paste code") {
                            let c = HubClient.normalizeDecider(UIPasteboard.general.string ?? "")
                            if Self.looksLikeDeciderCode(c) { hub.decider = c; status = nil }
                            else if Self.looksLikeToken(c.lowercased()) { status = "that is the hub token — the decider code is XXXXX-XXXXX-XXXXX-XXXXX, in Apple Passwords" }
                            else { status = "the clipboard is not a decider code (XXXXX-XXXXX-XXXXX-XXXXX)" }
                        }
                        Spacer()
                        Button("Save behind Face ID") {
                            deciderCheck = nil
                            if DeciderKeychain.save(hub.decider) {
                                hub.decider = ""            // never held in the clear again
                                hub.deciderSaved = DeciderKeychain.exists()
                                revealCode = false
                                checkDeciderNow()           // prove it before it is needed
                            } else {
                                status = "could not save the code to the Keychain"
                            }
                        }.disabled(hub.decider.isEmpty).buttonStyle(.borderedProminent)
                    }
                } else {
                    LabeledContent("Decider code", value: "saved · Face ID")
                    HStack {
                        Button("Check code") { checkDeciderNow() }
                        Spacer()
                        Button("Replace", role: .destructive) {
                            DeciderKeychain.clear()
                            hub.deciderSaved = false
                            deciderCheck = nil
                        }
                    }
                }
                Text(deciderNote).font(.caption).foregroundStyle(deciderColor)
                Button("Test connection") {
                    Task { do { status = "OK · \(try await hub.check()) projects" } catch { status = error.localizedDescription } }
                }
                if let status { Text(status).font(.footnote).foregroundStyle(status.hasPrefix("OK") ? .green : .red) }
            }
            // Push registration is a fact about this handset — which APNs
            // environment this build registered against, and whether a push
            // actually arrives. The route moved up here from the old "Hub
            // status" section, which is gone: its other two rows (pending
            // approvals, scheduled jobs) were numbers the board already
            // carries, and a second copy is the rival-number pattern.
            Section("Push notifications") {
                LabeledContent("Phone", value: push.status)
                if let devs = hubStatus?.devices, !devs.isEmpty {
                    LabeledContent("Route", value: devs.map { $0.env.isEmpty ? "default" : $0.env }.joined(separator: ", "))
                }
                Button("Send test push") {
                    pushResult = nil
                    Task { do { try await hub.testPush(); pushResult = "sent — should buzz within seconds" } catch { pushResult = error.localizedDescription } }
                }
                if let pushResult { Text(pushResult).font(.caption).foregroundStyle(.secondary) }
            }
            Section {
                // Downloading a build by hand is the most common thing done
                // on this page. The lanes live
                // in one modal off More; this row opens the same modal, so
                // looking in the old place still works and costs no scrolling.
                UpdateAppRow(showUpdate: $showUpdate, ota: hubStatus?.ota)
            } header: { Text("App") }
            Section {
                NavigationLink("Claude app servers (advanced)") { ServersView() }
            }
            // The one outbound call the hub makes on its own: set at setup,
            // switched here. The caption is the literal payload.
            if let u = usageState {
                Section {
                    Toggle("Weekly usage heartbeat", isOn: Binding(get: { u.on }, set: { on in
                        Task { usageState = try? await hub.setUsage(on: on) }
                    }))
                    Text("v\(u.payload.version) · \(u.payload.active_days) active days · \(u.payload.sessions_started) sessions · \(u.payload.goals) goals · \(u.payload.pages) pages · \(u.payload.token_bucket) tokens")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            Section {
                // The build this app IS is under the tab bar on every screen
                // and in the Update modal's caption; a third copy here is the
                // rival-number pattern again.
                LabeledContent("Signing", value: PushState.apsEnvironment() == "production" ? "ad-hoc (over the air) · 1 year" : "development (Xcode/LAN) · 1 year")
            }
        }
        .navigationTitle("Settings")
        .askButton()
        .sheet(isPresented: $showUpdate) { UpdateAppSheet() }
        // A check is about one exact code: the moment a character is edited it
        // stops being evidence, so the row drops back to "not checked".
        // Not checked on open — a check needs Face ID now, and a face prompt
        // for merely opening Settings is the kind of tax this change removes.
        .onChange(of: hub.decider) { deciderCheck = nil }
        .task {
            await store.loadStatus(hub)
            usageState = try? await hub.usage()
        }
        .refreshable { await store.loadStatus(hub) }
    }

}

/// `GET/PUT /api/v1/usage` — the opt-in weekly heartbeat. Only this screen
/// reads it, so it lives here rather than in Models.swift.
struct UsageState: Codable, Sendable {
    struct Payload: Codable, Sendable {
        var version: String = ""
        var active_days: Int = 0
        var sessions_started: Int = 0
        var goals: Int = 0
        var pages: Int = 0
        var token_bucket: String = ""
    }
    var on: Bool = false
    var last_sent: String = ""
    var install_id: String = ""
    var payload: Payload = Payload()
}

/// Advanced: per-project `claude remote-control` servers for the official
/// Claude app (Code tab). Everything in this app works without them.
struct ServersView: View {
    @Environment(HubClient.self) private var hub
    @State private var load = HubLoad<[TmuxSession]>()
    private var servers: [TmuxSession] { load.value ?? [] }
    @State private var projects: [Project] = []
    @State private var error: String?

    var body: some View {
        List {
            if let error { Section { ErrorBanner(message: error) } }
            HubLoadStatus(load: load)
            Section("Running") {
                if servers.isEmpty { Text("none").foregroundStyle(.secondary) }
                ForEach(servers.filter { $0.kind == "remote-control" }) { s in
                    HStack {
                        Image(systemName: "dot.radiowaves.left.and.right").foregroundStyle(.green)
                        VStack(alignment: .leading) {
                            Text("life/\(s.project)").font(.subheadline.monospaced())
                            Text("up for \(s.created, style: .relative)").font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("Stop", role: .destructive) { Task { try? await hub.killSession(name: s.name); await load.run(fetch) } }.buttonStyle(.bordered)
                    }
                }
            }
            Section("Start for project") {
                ForEach(projects) { p in
                    Button {
                        Task { do { _ = try await hub.startSession(project: p.name); error = nil } catch { self.error = error.localizedDescription }; await load.run(fetch) }
                    } label: { Label(p.name, systemImage: "play.fill") }
                    .disabled(servers.contains { $0.project == p.name && $0.kind == "remote-control" })
                }
            }
        }
        .navigationTitle("Claude app servers")
        .askButton()
        .hubTask(load, fetch)
    }

    func fetch() async throws -> [TmuxSession] {
        async let p = hub.projects()
        let s = try await hub.sessions()
        projects = try await p
        return s
    }
}
