// Starting a session: NewThreadSheet, the Draft it types into and the
// Composer bar (shared with ThreadDetail).
import SwiftUI

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
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            } else {
                composing
            }
        }
    }

    /// The empty chat: what they attached (if anything), then the composer.
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
            }    }

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
                created = t
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
                                      onPasteImages: { imgs in for i in imgs { attachments.add(i) } })
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
