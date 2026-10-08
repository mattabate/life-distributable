// Starting a session: NewThreadSheet, the Draft it types into and the
// Composer bar (shared with ThreadDetail).
import SwiftUI
import UniformTypeIdentifiers

/// A NEW SESSION IS AN EMPTY CHAT: no goal link or check-back fields — the
/// owner says those in the prompt. Nothing above the composer but what
/// they attached; the goal and the cadence are the session's to infer from
/// their words. Sending turns this sheet INTO the session (ThreadDetail), the
/// same chat they would open from the list — the sheet never was a form, and now it
/// is not a detour either.
struct NewThreadSheet: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    /// In-app snapshot from the floating snap button (see Snap.swift).
    var initialImage: UIImage? = nil
    /// The file that drew the snapped screen ("app/Life/Sources/…"), sent as
    /// TEXT above the message so the session knows where to look without
    /// reading it off the pixels.
    var initialPlace: String = ""
    /// What arrived through the share sheet of another app (SharedInbox):
    /// documents and pictures, attached the moment the sheet opens.
    var initialFiles: [AttachedFile] = []
    var initialImages: [UIImage] = []
    let onCreated: () async -> Void
    /// The desktop's Sessions page with no session open: this empty chat sits
    /// in the right pane instead of a sheet (the console's `#/sessions`), so
    /// there is nothing to close, and the session the message starts is
    /// handed to the page to open like any other (`started`).
    var embedded = false
    var started: ((Thread) -> Void)? = nil
    /// Same object the thread composer uses, for the same reason: dictation
    /// partials land many times a second and must not invalidate this sheet.
    @State private var draft = Draft()
    @State private var busy = false
    @State private var error: String?
    @State private var attachments = AttachmentDraft()
    /// Which model this session will start on, and why.
    @State private var startLine: String?
    /// The session the first message started: from here on the sheet is that
    /// chat, and its reply lands where the owner is looking.
    @State private var created: Thread?

    /// The snap was taken off with the ✕: what is left is a plain new session,
    /// and the "[Screenshot of the life app…]" preamble must not go out over
    /// no picture.
    @State private var snapRemoved = false

    var isSnap: Bool { initialImage != nil && !snapRemoved }

    var body: some View {
        // Same shape as the thread composer: everything actionable sits in a
        // bar at the bottom of the screen, within thumb reach.
        NavigationStack {
            if let t = created {
                ThreadDetail(thread: t)
                    #if targetEnvironment(macCatalyst)
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button { dismiss() } label: { Image(systemName: "xmark") }.accessibilityLabel("Close") } }
                    #else
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
                    #endif
            } else {
                composing
            }
        }
    }

    #if targetEnvironment(macCatalyst)
    /// The console's empty chat (threads.js, `#/sessions`):
    /// the head — "New session" in bold, the model line small under it — the
    /// greeting centred in the white pane, and the composer's box along the
    /// bottom with anything attached as chips inside it, "Start session" on
    /// the blue button.
    private var composing: some View {
        VStack(spacing: 0) {
            if embedded {
                VStack(alignment: .leading, spacing: 1) {
                    Text(isSnap ? "Session from this screen" : "New session")
                        .font(.system(size: 15, weight: .semibold)).tracking(-0.18).lineLimit(1)
                    if let startLine {
                        Text(startLine).font(.system(size: 12.5)).foregroundStyle(Web.muted).lineLimit(1)
                    }
                }
                .padding(.horizontal, 20)
                .frame(maxWidth: .infinity, minHeight: 52, maxHeight: 52, alignment: .leading)
                .background(Web.panel)
                .overlay(alignment: .bottom) { Rectangle().fill(Web.line).frame(height: 1) }
            }
            VStack(spacing: 12) {
                if let error { ErrorBanner(message: error) }
                Text("Ready when you are.")
                    .font(.system(size: 26, weight: .semibold)).tracking(-0.65)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            .padding(.top, 24).padding(.bottom, 18).padding(.horizontal, 16)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Web.panel)
            if !embedded, let startLine {
                Text(startLine).font(.system(size: 12.5)).foregroundStyle(Web.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16).padding(.bottom, 6).background(Web.panel)
            }
            Composer(draft: draft, attachments: attachments,
                     placeholder: attachments.isEmpty ? "What do you want done?" : (isSnap ? "About this screen (optional)" : attachments.images.isEmpty ? "What to do with it? (optional)" :"About these photos (optional)"),
                     sending: busy, send: start, sendLabel: "Start session")
        }
        // The whole pane takes a Finder drop, the empty chat included, not
        // only the bar along the bottom.
        .dropsFiles(into: attachments)
        .onAppear {
            if let initialImage, attachments.isEmpty, !snapRemoved { attachments.add(initialImage) }
            if attachments.files.isEmpty, attachments.images.count == (initialImage == nil ? 0 : 1) {
                for img in initialImages { attachments.add(img) }
                for f in initialFiles { attachments.add(f) }
            }
            // Focused on arrival, as the console's box is.
            draft.focusRequest += 1
            Task {
                guard let q = try? await hub.quota(), let model = q.next_model, !model.isEmpty else { return }
                let reason = q.next_reason ?? ""
                startLine = "Starts on \(shortModel(model))" + (reason.isEmpty ? "" : " — \(reason)")
            }
        }
        // The snap's chip is its ✕ now: removing the first picture of a snap
        // drops the snap preamble too.
        .onChange(of: attachments.images.count) { _, n in if initialImage != nil, n == 0 { snapRemoved = true } }
        .navigationTitle(isSnap ? "Session from this screen" : "New session")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(embedded ? .hidden : .automatic, for: .navigationBar)
        .toolbar {
            // The Mac draws this corner as a round glass button, and
            // "Cancel" came out "C…" in it.
            if !embedded {
                ToolbarItem(placement: .cancellationAction) { Button { dismiss() } label: { Image(systemName: "xmark") }.accessibilityLabel("Cancel") }
            }
        }
    }
    #else
    /// The empty chat: what the owner attached (if anything), then the composer.
    private var composing: some View {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        // Show what is actually attached. The sheet used to be a
                        // sentence and a screenful of nothing, with the snap only
                        // visible as a 72pt thumbnail down by the composer — the
                        // one thing worth seeing was the smallest thing on screen.
                        // (ChatAboutSheet already shows its component this way.)
                        if let img = attachments.images.first {
                            // A DEFINITE height, and the centring frame LAST.
                            // `.frame(maxHeight: 320)` capped nothing in here: a
                            // ScrollView proposes nil height, maxHeight passes it
                            // straight through, and a phone screenshot laid itself
                            // out ~800pt tall — 3x the viewport — so the "+N more"
                            // line and the upload ErrorBanner were scrolled off the
                            // sheet entirely. A definite height makes scaledToFit
                            // fit inside it; leaving the width unspecified lets the
                            // frame take the fitted width, so the border and the ✕
                            // sit on the photo instead of out in a white gutter.
                            // …but 320 was still more than the sheet has once the
                            // keyboard is up: the viewport is ~334pt, so the
                            // preview plus its footnote overflowed by ~60pt and
                            // the ErrorBanner below was off-screen again — the
                            // upload could fail and the sheet would look idle.
                            // Ask the scroll container how much room there is.
                            Image(uiImage: img).resizable().scaledToFit()
                                .containerRelativeFrame(.vertical) { h, _ in min(320, h * 0.62) }
                                .clipShape(RoundedRectangle(cornerRadius: 12))
                                .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Color.secondary.opacity(0.3), lineWidth: 1) }
                                .overlay(alignment: .topTrailing) {
                                    // The snap is always first (added on appear).
                                    // remove(at:) keeps assetIDs in step with images.
                                    Button { if initialImage != nil { snapRemoved = true }; attachments.remove(at: 0) } label: {
                                        Image(systemName: "xmark.circle.fill").font(.title3).foregroundStyle(.white, .black.opacity(0.6))
                                    }.padding(6).accessibilityLabel("Remove photo")
                                }
                                .frame(maxWidth: .infinity, alignment: .center)
                            if attachments.count > 1 {
                                Text("+\(attachments.count - 1) more attached").font(.caption).foregroundStyle(.secondary)
                            }
                        } else if !attachments.files.isEmpty {
                            // Files alone (a PDF shared from a bank app): one
                            // chip each, removable, where the snap would sit.
                            ForEach(Array(attachments.files.enumerated()), id: \.element.id) { i, f in
                                FileChip(file: f) { attachments.removeFile(at: i) }
                            }
                        }
                        if let error { ErrorBanner(message: error) }
                        // Nothing attached: one greeting, centred where the
                        // first bubble will land — the same two words the
                        // console's empty chat says (threads.js HELLO).
                        if attachments.isEmpty {
                            Text("Ready when you are.").font(.title3.weight(.semibold))
                                .frame(maxWidth: .infinity)
                                .containerRelativeFrame(.vertical) { h, _ in h * 0.8 }
                        }
                    }.padding()
                }
                if let startLine {
                    Text(startLine).font(.caption).foregroundStyle(.secondary)
                        .padding(.horizontal).padding(.bottom, 4)
                }
                Divider()
                VStack(spacing: 6) {
                    Composer(draft: draft, attachments: attachments,
                             placeholder: attachments.isEmpty ? "What do you want done?" : (isSnap ? "About this screen (optional)" : attachments.images.isEmpty ? "What to do with it? (optional)" :"About these photos (optional)"),
                             sending: busy, send: start, showAttachments: false)
                }.padding(10)
            }
            .onAppear {
                if let initialImage, attachments.isEmpty, !snapRemoved { attachments.add(initialImage) }
                if attachments.files.isEmpty, attachments.images.count == (initialImage == nil ? 0 : 1) {
                    for img in initialImages { attachments.add(img) }
                    for f in initialFiles { attachments.add(f) }
                }
                #if targetEnvironment(simulator)
                // ops/screens.sh sent:<thread-id> — the sheet AFTER Send, when it
                // has become that session's chat: a state that once crashed
                // and that no other screenshot reaches.
                if let c = ProcessInfo.processInfo.environment["LIFE_COMPOSE"], !c.isEmpty {
                    if c == "1" || c == "share" { draft.focusRequest += 1 } else { Task { created = try? await hub.thread(c) } }
                }
                #endif
                Task {
                    guard let q = try? await hub.quota(), let model = q.next_model, !model.isEmpty else { return }
                    let reason = q.next_reason ?? ""
                    startLine = "Starts on \(shortModel(model))" + (reason.isEmpty ? "" : " — \(reason)")
                }
            }
            .navigationTitle(isSnap ? "Session from this screen" : "New session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
    }
    #endif

    /// What goes above whatever was typed on a snap. The text must match the
    /// hub's `snapPreamble` (threads.go) byte for byte. The first line is the one
    /// the hub strips from titles and cards (`stripSnapPreamble`); the bullets
    /// under it say WHERE, and the hub strips those the same way.
    private var snapPreamble: String {
        var s = "[Screenshot of the life app, taken in-app by the owner from the screen they were on — the UI you and they are building together.]\n"
        if !initialPlace.isEmpty { s += "- Screen: \(initialPlace)\n" }
        return s
    }

    private func start() {
        busy = true
        Task {
            do {
                let prompt = draft.text
                let refs = try await attachments.upload(to: hub, threadID: nil)
                let text = isSnap ? snapPreamble + prompt : prompt
                let t = try await hub.createThread(prompt: text, attachments: refs)
                let ids = attachments.assetIDs
                // The moment the hub has the session, this sheet becomes its
                // chat (the message is the first bubble, the reply streams in
                // under it). The list behind refreshes on its own time — this
                // Task is unstructured, so the refresh still lands.
                if let started { started(t) } else { created = t }
                Task { await onCreated() }
                await PhotoCleanup.offerDeletingScreenshots(ids)
            } catch { self.error = error.localizedDescription; busy = false }
        }
    }
}

/// The text being typed/dictated. A separate observable object so that
/// only the view that shows it (`Composer`) is invalidated as it changes —
/// Observation tracks per-property reads, and the session page above never
/// reads `text` in its body.
@Observable @MainActor final class Draft {
    var text = ""
    /// Bumped to ask the composer to open the keyboard (reply-to-an-ask).
    var focusRequest = 0
    /// Mirrors the composer's focus so the session page can keep the latest
    /// message visible when the keyboard shrinks the viewport.
    var keyboardOpen = false
    var binding: Binding<String> { Binding(get: { self.text }, set: { self.text = $0 }) }
    #if targetEnvironment(macCatalyst)
    /// Every box alive on screen, so the desktop's Update can ask before it
    /// quits over unsent words (MacUpdateButton).
    @ObservationIgnored static let live = NSHashTable<Draft>.weakObjects()
    static var anyUnsent: Bool { live.allObjects.contains { $0.text.contains { !$0.isWhitespace } } }
    /// The unsent words themselves, quoted in the Update dialog so the owner
    /// can tell which box they are in.
    static var unsentPreview: String {
        let texts = live.allObjects.map { $0.text.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
        return texts.map { "\u{201C}\($0.count > 80 ? String($0.prefix(80)) + "…" : $0)\u{201D}" }.joined(separator: "\n")
    }
    init() { Self.live.add(self) }
    #endif
}

/// Bottom bar of a session: attachments, the draft box, dictation, send.
/// Voice-first: unfocused the draft is plain text — nothing
/// focusable, so dictating can't summon the keyboard — and it only becomes a
/// TextField when tapped. No Done button; tap outside closes it.
struct Composer: View {
    @Bindable var draft: Draft
    let attachments: AttachmentDraft
    let placeholder: String
    let sending: Bool
    let send: () -> Void
    /// "Chat about this" can be sent with nothing typed — the component and
    /// its numbers are the message ("explain it").
    var allowEmpty = false
    /// The new-session sheet shows the attachment full width in its body, so
    /// it turns the thumbnail strip off rather than showing the same photo twice.
    var showAttachments = true
    /// The desktop only (the phone ignores both): the send button's word —
    /// the console's "Start session" on a new chat — and a strip drawn inside
    /// the box over the text, where the console puts its reply-strip.
    var sendLabel = "Send"
    var banner: AnyView? = nil

    /// Drives the text view's first-responder state (see ComposerField); it
    /// reports back here when the keyboard goes away on its own.
    @State private var focused = false
    /// Editable field (true) vs plain Text (false); separate from focus so the
    /// field exists before focus is requested.
    @State private var editing = false

    /// Stops at the first non-space instead of copying the whole draft the way
    /// `trimmingCharacters` did — this runs on every keystroke and every
    /// dictation partial.
    var canSend: Bool { !sending && (allowEmpty || draft.text.contains { !$0.isWhitespace } || !attachments.isEmpty) }

    /// What the unfocused box shows. Only the tail of a long draft is
    /// rendered: the box is six lines tall whatever happens, and while
    /// dictating the words worth seeing are the newest ones. Handing the whole
    /// transcript to `Text` several times a second is what made a long voice
    /// recording seize up.
    var preview: String {
        let budget = 400
        guard draft.text.count > budget else { return draft.text }
        return "…" + draft.text.suffix(budget)
    }

    var body: some View {
        #if targetEnvironment(macCatalyst)
        macBody
            .onChange(of: draft.focusRequest) { _, _ in editing = true; focused = true }
            .onChange(of: focused) { _, f in draft.keyboardOpen = f }
        #else
        phoneBody
        #endif
    }

    #if targetEnvironment(macCatalyst)
    /// The console's composer (app.css "ONE box, three bands"), the same
    /// layout as the console's. The box runs wall to wall under a strong
    /// hairline that turns blue while typing; inside it the reply-strip
    /// (`banner`), the body — chips, then the text — and the bar: Attach on
    /// the left, the blue Send on the right. Words, not icons, as the
    /// console's buttons are.
    private var macBody: some View {
        VStack(spacing: 0) {
            if let banner { banner }
            VStack(alignment: .leading, spacing: 8) {
                if showAttachments, !attachments.isEmpty { AttachBar(draft: attachments, compact: true) }
                macField
            }
            .padding(.top, 9).padding(.horizontal, 16).padding(.bottom, 6)
            HStack(spacing: 8) {
                AttachBar.Menu(draft: attachments, label: "Attach")
                Spacer(minLength: 8)
                Button { send() } label: { Text(sending ? "Sending…" : sendLabel) }
                    .buttonStyle(WebSendStyle())
                    .disabled(!canSend)
                    .keyboardShortcut(.return, modifiers: .command)
                    .accessibilityLabel(sendLabel)
            }
            .padding(.vertical, 8).padding(.leading, 16).padding(.trailing, 14)
            .overlay(alignment: .top) { Rectangle().fill(Web.line).frame(height: 1) }
        }
        .background(Web.panel)
        .overlay(alignment: .top) {
            Rectangle().fill(focused ? Web.accent.opacity(0.6) : Web.lineStrong).frame(height: 1)
        }
        // A file dropped anywhere on the box, the bar and the chips included
        // (the text takes its own drops, PasteTextView).
        .dropsFiles(into: attachments)
    }

    /// The textarea: ONE view, 15pt, at least 44 tall, focused on arrival as
    /// the console's is. The phone swaps a plain Text in while unfocused so
    /// dictation cannot summon its keyboard; the Mac has no keyboard to keep
    /// away, and the swap showed two boxes — a 15pt Text of the last 400
    /// characters that became a 13pt field on a click. The field holds every
    /// word, at one size.
    private var macField: some View {
        ComposerField(text: $draft.text, placeholder: placeholder, maxLines: 12, focused: $focused,
                             onPasteImages: { imgs in for i in imgs { attachments.add(i) } },
                             onPasteFiles: { files in for f in files { attachments.add(f, picturesAsImages: true) } })
            .frame(maxWidth: .infinity, alignment: .leading)
            .frame(minHeight: 44, alignment: .topLeading)
            .onAppear { editing = true; focused = true }
    }
    #endif

    private var phoneBody: some View {
        VStack(spacing: 6) {
            if showAttachments, !attachments.isEmpty { AttachBar(draft: attachments, compact: true).padding(.horizontal, 4) }
            HStack(alignment: .bottom, spacing: 8) {
                AttachBar.Menu(draft: attachments).padding(.bottom, 2)
                let hint = placeholder
                Group {
                    if editing {
                        // A pasted screenshot becomes an attachment, not text —
                        // the same thing ⌘V does in the console's composer.
                        ComposerField(text: $draft.text, placeholder: hint, focused: $focused,
                                      onPasteImages: { imgs in for i in imgs { attachments.add(i) } },
                                      onPasteFiles: { files in for f in files { attachments.add(f, picturesAsImages: true) } })
                            // Same as the unfocused Text below: claim the whole
                            // row minus the buttons, so tapping the box does not
                            // narrow it.
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .onAppear { focused = true }
                            .onChange(of: focused) { _, f in if !f { editing = false } }
                    } else {
                        Text(draft.text.isEmpty ? hint : preview).lineLimit(6)
                            .foregroundStyle(draft.text.isEmpty ? .tertiary : .primary)
                            .frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
                            // Deferred: the window-wide dismiss-on-tap recognizer
                            // (KeyboardDismiss.swift) fires on this same tap and
                            // resigns first responder; focusing synchronously
                            // races it and the keyboard never shows.
                            .onTapGesture {
                                KeyboardDismissOnTap.suppress()
                                editing = true   // TextField appears → its onAppear focuses it
                            }
                    }
                }
                .padding(8).background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 14))
                Button { send() } label: {
                    if sending { ProgressView().frame(width: 30, height: 30) } else { Image(systemName: "arrow.up.circle.fill").font(.system(size: 30)) }
                }
                .accessibilityLabel("Send")
                .disabled(!canSend)
            }
        }
        .onChange(of: draft.focusRequest) { _, _ in editing = true; focused = true }
        .onChange(of: focused) { _, f in draft.keyboardOpen = f }
    }
}

#if targetEnvironment(macCatalyst)
/// The console's `.composer .bar button.sm` as a label (Attach's menu): a
/// word in a hairline box, 13pt, radius 8; `on` draws it white on red.
struct WebSmallButton: View {
    let text: String
    var on = false
    @State private var hover = false
    var body: some View {
        Text(text).font(.system(size: 13, weight: .medium)).lineLimit(1)
            .padding(.horizontal, 12).padding(.vertical, 5)
            .foregroundStyle(on ? Color.white : Color.primary)
            .background(on ? Color.red : (hover ? Web.code : Web.panel), in: RoundedRectangle(cornerRadius: 8))
            .overlay { if !on { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) } }
            .contentShape(Rectangle())
            .onHover { hover = $0 }
    }
}

/// The bar's primary: the filled blue Send, 6×16, semibold white.
struct WebSendStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled
    @State private var hover = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13.5, weight: .semibold)).lineLimit(1)
            .padding(.horizontal, 16).padding(.vertical, 6)
            .foregroundStyle(Color.white)
            .background(Web.accent, in: RoundedRectangle(cornerRadius: 8))
            .brightness(hover || configuration.isPressed ? 0.06 : 0)
            .opacity(enabled ? 1 : 0.45)
            .contentShape(Rectangle())
            .onHover { hover = $0 }
    }
}
#endif
