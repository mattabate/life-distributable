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
    /// The decider code's own line (`hub.deciderStatus`): a Keychain save
    /// that failed, a wrong paste, a check that errored. It shared `status`
    /// with Test connection, so "OK · 40 projects" wiped out "could not save
    /// the code to the Keychain" (2026-09-29); it lives on the client since
    /// 2026-09-30 so leaving this screen does not lose it.
    private var deciderStatus: String? {
        get { hub.deciderStatus }
        nonmutating set { hub.deciderStatus = newValue }
    }
    @Environment(BoardStore.self) private var store
    private var hubStatus: HubStatus? { store.status }
    // The install lanes and every build number live in UpdateAppView.swift
    // now; this screen only opens that modal.
    @State private var showUpdate = false
    // LIFE_DECIDER_SHOT (a screenshot run): the box opens revealed, to prove
    // a snap leaves a showing code out (`hidingSecrets`).
    @State private var revealCode = ProcessInfo.processInfo.environment["LIFE_DECIDER_SHOT"] != nil
    @State private var deciderCheck: DeciderStatus?
    @State private var usageState: UsageState?
    /// Replace was tapped: the paste box is open over a code that is STILL
    /// saved. Replace used to delete the saved code on the tap, and on the
    /// phone it shared a row with Check code — two plain Buttons in one Form
    /// row both fire on any tap in that row — so a Check code could also
    /// erase the code it was checking. Now nothing is deleted until a new
    /// code is saved over it.
    @State private var replacing = false

    /// What the code row says. Never a bare "set" again: either the hub has
    /// confirmed this exact code, or the row admits it does not know. The old
    /// row said "set" about a code the hub was refusing.
    var deciderNote: String {
        if let c = deciderCheck {
            if !c.armed { return "no code armed on the hub — approvals take the token alone" }
            if !c.sent { return "not saved — approvals will fail until you paste it in" }
            // Not "the one in Apple Passwords": that entry can be a stale
            // code the hub refuses.
            return c.ok
                ? "checked — the hub accepts this code, and \(DeciderKeychain.unlockName) unlocks it"
                : "WRONG — the hub refuses this code. \(Self.refusalAdvice(c))"
        }
        if replacing && hub.deciderSaved { return "the saved code is still in place" }
        if !hub.deciderSaved { return "paste it from Apple Passwords once; \(DeciderKeychain.unlockName) unlocks it after that" }
        return "saved, not checked — tap Check code"
    }

    /// What to do about a refused code, with the one date the hub knows: when
    /// its own code was made. It used to say the refused code "is older than
    /// the hub's" — a guess the phone cannot make. A date can be held against
    /// the Passwords entry's history.
    nonisolated static func refusalAdvice(_ c: DeciderStatus) -> String {
        let made = c.set_at.map { "The hub's code was made \($0.formatted(date: .abbreviated, time: .shortened)): paste the one saved then" } ?? "Paste the hub's current one"
        return "\(made), or make a new one with ops/decider-set.sh on the Mac."
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

    /// Save the pasted code behind Face ID / Touch ID, empty the box, and
    /// prove the code against the hub. Every outcome lands on the decider
    /// line: "saved" is a claim the Keychain has to back by finding the item
    /// again, and a failed add prints its OSStatus.
    ///
    /// The pasted code goes to the hub FIRST, and one the hub refuses is not
    /// saved at all: otherwise pasting an old code replaces whatever the
    /// phone held. A hub that cannot be
    /// reached is not a refusal — then it saves as before and the check
    /// that follows says why.
    func saveDecider() {
        let code = hub.decider
        deciderCheck = nil
        deciderStatus = "checking it with the hub…"
        Task {
            if let c = try? await hub.checkDecider(code: code), c.armed, !c.ok {
                deciderStatus = "the hub refuses that code, so it was not saved. \(Self.refusalAdvice(c))"
                return
            }
            storeDecider(code)
        }
    }

    private func storeDecider(_ code: String) {
        guard hub.decider == code else { deciderStatus = nil; return }   // edited while the hub was asked
        guard DeciderKeychain.save(code) else {
            deciderStatus = "could not save the code to the Keychain (\(DeciderKeychain.lastStatus))"
            return
        }
        hub.deciderSaved = DeciderKeychain.exists()
        guard hub.deciderSaved else {
            deciderStatus = "the Keychain took the code but cannot find it again (\(DeciderKeychain.lastStatus)) — the pasted code will be used until that is fixed"
            return
        }
        hub.decider = ""            // never held in the clear again
        revealCode = false
        replacing = false
        deciderStatus = "saved — checking it with the hub…"
        checkDeciderNow()           // prove it before it is needed
    }

    /// Ask the hub, with Face ID, whether the saved code is the right one.
    func checkDeciderNow() {
        deciderCheck = nil
        Task {
            do { deciderCheck = try await hub.checkDecider(unlock: true); deciderStatus = nil }
            catch { deciderCheck = nil; deciderStatus = error.localizedDescription }
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
                        else if Self.looksLikeDeciderCode(HubClient.normalizeDecider(t)) { status = "that is the decider code — it goes in the row below, not in Token" }
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
                if !hub.deciderSaved || !hub.decider.isEmpty || replacing {
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
                    // On the Mac these are drawn buttons with a tap gesture
                    // (TapButton): a Button in a List row is the row's click
                    // there. Same words, same actions.
                    HStack {
                        // The pasted code lands SHOWING, so it can be held
                        // against the Passwords entry before Save (as dots,
                        // nobody could tell a different code). And iOS
                        // asks before the app may read another app's copy,
                        // with Don't Allow Paste as the blue button: a
                        // refused read is not "not a decider code".
                        SettingsButton("Paste code") {
                            let had = UIPasteboard.general.hasStrings
                            let raw = UIPasteboard.general.string
                            let c = HubClient.normalizeDecider(raw ?? "")
                            // Paste token's complaint about this same copy is
                            // settled by pasting it here; it sat red under a
                            // good paste otherwise.
                            if let s = status, !s.hasPrefix("OK") { status = nil }
                            if Self.looksLikeDeciderCode(c) { hub.decider = c; revealCode = true; deciderStatus = nil }
                            else if raw == nil { deciderStatus = had ? "iOS did not hand over the clipboard — tap Paste code again and choose Allow Paste" : "the clipboard is empty — copy the code in Passwords first" }
                            else if Self.looksLikeToken(c.lowercased()) { deciderStatus = "that is the hub token — the decider code is XXXXX-XXXXX-XXXXX-XXXXX, in Apple Passwords" }
                            else { deciderStatus = "the clipboard is not a decider code (XXXXX-XXXXX-XXXXX-XXXXX)" }
                        }
                        Spacer()
                        SettingsButton("Save behind \(DeciderKeychain.unlockName)", primary: true, disabled: hub.decider.isEmpty) {
                            saveDecider()
                        }
                    }
                    if replacing && hub.deciderSaved {
                        SettingsButton("Keep the saved code") {
                            hub.decider = ""
                            revealCode = false
                            replacing = false
                            deciderStatus = nil
                        }
                    }
                } else {
                    LabeledContent("Decider code", value: "saved · \(DeciderKeychain.unlockName)")
                    HStack {
                        SettingsButton("Check code") { checkDeciderNow() }
                        Spacer()
                        // Opens the paste box and deletes nothing: the saved
                        // code goes only when a new one is saved over it.
                        SettingsButton("Replace", destructive: true) {
                            replacing = true
                            deciderCheck = nil
                            deciderStatus = nil
                        }
                    }
                }
                Text(deciderNote).font(.caption).foregroundStyle(deciderColor)
                if let deciderStatus { Text(deciderStatus).font(.footnote).foregroundStyle(deciderStatus.hasPrefix("saved") ? .green : deciderStatus.hasPrefix("checking") ? .secondary : .red) }
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
            // None of the handset rows apply on the Mac: it never registers
            // for push, and it is rebuilt by `make mac`, not downloaded.
            #if !targetEnvironment(macCatalyst)
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
            #endif
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

/// A button in a Settings row. The phone's is a plain SwiftUI Button
/// (prominent for Save, destructive for Replace). On the Mac a Button inside
/// a Form row is the row's click and never fires (shipping.md, TapButton), so
/// it is the same words drawn as a web-style button with a tap gesture.
struct SettingsButton: View {
    let title: String
    var primary = false
    var destructive = false
    var disabled = false
    let action: () -> Void
    init(_ title: String, primary: Bool = false, destructive: Bool = false, disabled: Bool = false, action: @escaping () -> Void) {
        self.title = title; self.primary = primary; self.destructive = destructive; self.disabled = disabled; self.action = action
    }
    var body: some View {
        #if targetEnvironment(macCatalyst)
        TapButton(action: { if !disabled { action() } }) {
            Text(title).font(.system(size: 13.5, weight: primary ? .semibold : .medium)).lineLimit(1)
                .padding(.horizontal, 14).padding(.vertical, 6)
                .foregroundStyle(primary ? Color.white : (destructive ? Color.red : Color.accentColor))
                .background(primary ? Color.accentColor : Web.panel, in: RoundedRectangle(cornerRadius: 8))
                .overlay { if !primary { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) } }
                .opacity(disabled ? 0.45 : 1)
        }
        #else
        // Borderless, never the default style: two default-style Buttons in
        // one Form row are both fired by a tap on either, which is how Check
        // code also ran Replace (`make app-ui STEPS='tab more; tap Settings;
        // tap Check code; text Paste code'` reproduces it).
        if primary {
            Button(title, action: action).disabled(disabled).buttonStyle(.borderedProminent)
        } else if destructive {
            Button(title, role: .destructive, action: action).disabled(disabled).buttonStyle(.borderless)
        } else {
            Button(title, action: action).disabled(disabled).buttonStyle(.borderless)
        }
        #endif
    }
}

/// A custom-drawn button that clicks on the Mac. On the phone a `.plain`
/// Button. In a List or Form row on Mac Catalyst a `.plain` Button never
/// fires, while a tap gesture on the label's own shape does (the desktop's
/// rows click that way).
struct TapButton<Label: View>: View {
    let action: () -> Void
    @ViewBuilder let label: () -> Label

    var body: some View {
        #if targetEnvironment(macCatalyst)
        label()
            .contentShape(Rectangle())
            .onTapGesture(perform: action)
            .accessibilityAddTraits(.isButton)
        #else
        Button(action: action, label: label).buttonStyle(.plain)
        #endif
    }
}
