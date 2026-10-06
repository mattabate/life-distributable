// ONE card. Every cell in a chat — an ask, a proposal, a rec —
// is this chrome and nothing else: a tinted box, a caption line with its
// mark, the bold title, the body, a meta row, and ONE row of word buttons.
// The words come from the hub (`outcomes` on asks, actions and recs — the
// same list the console's ui.js draws), decisive first with the first one
// in the card's tint, "Reply" after them, and the client's silent close
// (Dismiss / Read) last. the owner: "they should have the same components, and
// there shouldn't be any excess styling code… reuse as much code as possible,
// standardize the UI." A closed card is the same box in grey with its state
// line and Reopen where the buttons were (every kind but an install, the owner
// 2026-09-29); a dismissed one folds to a grey line and unfolds to the body
// + Reopen.
//
// AskCard, ApprovalCard and RecCard decide WHAT the buttons say and do;
// nothing in them draws a box, a row or a button.
import SwiftUI

/// One word button on a card. A `url` opens (Install); `act` runs otherwise.
struct CardButton: Identifiable {
    let label: String
    var primary = false
    var url: URL? = nil
    var act: () -> Void = {}
    var id: String { label }
}

/// A hub outcome (`{value,label}`) as a card button, in the hub's order —
/// the same order the Respond sheet's chips and the console's row and strip
/// draw (the owner 2026-09-18: "Buttons are in a different order on the bar than
/// on the card"). The hub lists the decisive picks first and words-only
/// ("Reply", value "") last; the first decisive one is the primary — unless
/// a pick is armed on the chat bar, and then THAT one is (the owner 2026-10-02:
/// tapping Won't do after I did this switched the pick but nothing on the
/// card or the bar showed it, so it read as "clicking does nothing").
/// `arm` is called with the value.
func outcomeButtons(_ outcomes: [AskOutcome], armed: String? = nil, arm: @escaping (String) -> Void) -> [CardButton] {
    outcomes.enumerated().map { i, o in
        let primary = armed.map { $0 == o.value } ?? (i == 0 && !o.value.isEmpty)
        return CardButton(label: o.label, primary: primary) { arm(o.value) }
    }
}

/// The picks armed on the chat bar of the session a card sits in, by the
/// card's ref ("ask:<id>" / "rec:<id>" / "action:<id>") → outcome value ("" =
/// words alone). ThreadDetail sets it from its `replies`; the card draws its
/// armed pick as the primary button. Empty outside a session.
private struct ArmedPicksKey: EnvironmentKey { static let defaultValue: [String: String] = [:] }

extension EnvironmentValues {
    var armedPicks: [String: String] {
        get { self[ArmedPicksKey.self] }
        set { self[ArmedPicksKey.self] = newValue }
    }
}

/// Which kind of card a Dismiss / Reopen moves — the console's `cardFold`
/// table (views/threads.js), one call for all three (review-primitives
/// step 7; each card had its own pair before).
enum CardRef { case ask, action, rec }

extension HubClient {
    /// THE silent close (back = false) and its undo (back = true) of every
    /// chat card. A rec's dismiss is the hub's `expired` with the note
    /// "dismissed" (store.RecDismissed).
    func cardFold(_ ref: CardRef, _ id: String, back: Bool) async throws {
        switch ref {
        case .ask: _ = try await resolveAsk(id, state: back ? "open" : "dismissed")
        case .action: _ = back ? try await reopenAction(id) : try await dismissAction(id)
        case .rec: _ = try await decideRec(id, status: back ? "proposed" : "expired", note: back ? "" : "dismissed")
        }
    }
}

/// One card act with the card's shared plumbing: busy while it runs, the
/// error on the card (nil on success), then the owner's reload. Every card's
/// buttons that talk to the hub go through here.
@MainActor
func cardAct(busy: Binding<Bool>, error: Binding<String?>, reload: () async -> Void,
             message: (Error) -> String = { $0.localizedDescription }, _ body: () async throws -> Void) async {
    busy.wrappedValue = true
    do { try await body(); error.wrappedValue = nil } catch let e { error.wrappedValue = message(e) }
    await reload()
    busy.wrappedValue = false
}

/// The fold row of a card: Reopen on a folded one, Dismiss on an open one.
@MainActor
func foldButton(_ ref: CardRef, _ id: String, back: Bool, hub: HubClient,
                busy: Binding<Bool>, error: Binding<String?>, reload: @escaping () async -> Void) -> CardButton {
    CardButton(label: back ? "Reopen" : "Dismiss") {
        Task { await cardAct(busy: busy, error: error, reload: reload) { try await hub.cardFold(ref, id, back: back) } }
    }
}

struct Card: View {
    /// The open card's colour: red (needs you), blue (read), teal (install),
    /// purple (rec). Grey when closed or folded.
    var tint: Color
    /// SF symbol + words on the caption line ("DECIDE", "APPROVAL",
    /// "RECOMMENDATION", or the state word once closed), the id in tertiary.
    var mark: String
    var caption: String
    var id: String = ""
    /// Anything else on the caption line's right (a goal, "23m ago", a cost).
    var right: String = ""
    var title: String
    /// The body: markdown; past `isLongText` it folds behind Show all, like a
    /// long bubble, and opens to full height (no inner scroll).
    /// `lines` caps it instead (a rec's `because` shows two).
    var text: String = ""
    var lines: Int? = nil
    /// What was spoken aloud when this card arrived (`asks.said`): the
    /// session's `--say`, or the sentence the hub built when it wrote none.
    /// THE MESSAGE IS THE CARD: it leads, in body type behind a speaker
    /// rule in the tint, and the detail — the owner's steps — follows it. A title
    /// the message already says (a read minted from a `Say:` reply is titled
    /// from its first sentence) is not drawn twice.
    var said: String = ""
    /// The session the card belongs to: Play marks it "speaking" while it plays.
    var thread: String = ""
    /// The line is queued, not yet spoken: Play reads "Waiting to speak", and
    /// a double tap runs `hush` — the audio goes, the card stays.
    var waiting = false
    /// Its line is being heard right now (the hub's per-card "speaking").
    var speaking = false
    var hush: () -> Void = {}
    /// Between the body and the meta row (an approval's "will run" block).
    var extra: AnyView? = nil
    var meta: AnyView? = nil
    var closed = false
    /// The closed card's record ("done · 2h ago", "denied by app").
    var line: String = ""
    /// Set → one grey folded line headed with this word ("dismissed",
    /// "read"); tap to unfold the body and `buttons` (Reopen).
    var folded: String? = nil
    var buttons: [CardButton] = []
    /// Under the buttons: an approval's audit trail.
    var trail: AnyView? = nil
    var busy = false
    var error: String? = nil

    @State private var unfolded = false
    @State private var bodyOpen = false
    @State private var hushed = false
    @ObservedObject private var speaker = Speaker.shared
    @Environment(\.openURL) private var openURL

    private var shade: Color { closed || folded != nil ? .secondary : tint }

    var body: some View {
        if let folded { foldedBody(folded) } else { openBody }
    }

    private var openBody: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: mark)
                Text(caption).textCase(.uppercase)
                if !id.isEmpty { Text("· \(id)").foregroundStyle(.tertiary) }
                Spacer()
                if !right.isEmpty { Text(right).lineLimit(1).foregroundStyle(.tertiary) }
                // Play sits at the caption line's far end, never beside the
                // mark — the console's head line.
                if !said.isEmpty { playButton }
            }.font(.caption2.weight(.semibold)).foregroundStyle(shade)
            // Selectable: the thing worth copying is often IN the title (a
            // paste-this string, an endpoint).
            if titled { StyledText(text: title, style: .body, color: closed ? .secondary : .primary, bold: !closed) }
            if !said.isEmpty { saidLine }
            // A closed card keeps its body: what it said is often what the owner came
            // back to copy (the owner 2026-09-29: "there was text I was supposed to
            // copy out of it").
            if !text.isEmpty { bodyText }
            if let extra { extra }
            if let meta { meta }
            if closed, !line.isEmpty { StyledText(text: line, style: .caption1, color: .secondary, macSize: 12) }
            // A closed card's row is Reopen alone, when its owner gives one.
            if !buttons.isEmpty { buttonRow }
            if let trail { trail }
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(closed ? Color(.secondarySystemBackground).opacity(0.6) : tint.opacity(0.08), in: RoundedRectangle(cornerRadius: 14))
        .overlay { RoundedRectangle(cornerRadius: 14).strokeBorder(shade.opacity(closed ? 0.15 : 0.45), lineWidth: 1) }
        .padding(.horizontal)
    }

    /// Does the message already say the title? Compared plain (links to their
    /// text, marks dropped, case folded), a cut title's "…" off — the console's
    /// `saidCovers`. Then the title stays for rows and the card leads with
    /// the message alone.
    private var titled: Bool {
        guard !said.isEmpty else { return true }
        let t = SessionCell.plain(title).lowercased()
        return t.isEmpty || !SessionCell.plain(said).lowercased().contains(t)
    }

    /// Hear the spoken message again; tap again to stop.
    @ViewBuilder private var playButton: some View {
        if speaking && !hushed {
            // The line being heard reads "Speaking", and the same double tap
            // stops it — the hub cuts the Mac's voice, this stops the phone's
            // (the owner 2026-09-29: "if you double click it turns back into the
            // play button").
            Label("Speaking", systemImage: "circle.fill")
                .labelStyle(.titleAndIcon).textCase(.uppercase).foregroundStyle(.green)
                .symbolEffect(.pulse)
                .frame(minHeight: 22).padding(.leading, 8).contentShape(Rectangle())
                .onTapGesture(count: 2) { hushed = true; speaker.stop(); hush() }
                .accessibilityLabel("Speaking").accessibilityHint("Double-tap to stop speaking")
                .accessibilityAction { hushed = true; speaker.stop(); hush() }
        } else if waiting && !hushed {
            Label("Waiting to speak", systemImage: "circle.fill")
                .labelStyle(.titleAndIcon).textCase(.uppercase).foregroundStyle(.orange)
                .frame(minHeight: 22).padding(.leading, 8).contentShape(Rectangle())
                .onTapGesture(count: 2) { hushed = true; hush() }
                .accessibilityLabel("Waiting to speak").accessibilityHint("Double-tap to not speak this one")
                .accessibilityAction { hushed = true; hush() }
        } else {
            replayButton
        }
    }

    private var replayButton: some View {
        let playing = speaker.saying == said
        return Button { speaker.toggle(said, thread: thread) } label: {
            Label(playing ? "Stop" : "Play", systemImage: playing ? "stop.fill" : "play.fill")
                .labelStyle(.titleAndIcon).textCase(.uppercase)
                .frame(minHeight: 22).padding(.leading, 8).contentShape(Rectangle())
        }.buttonStyle(.plain).accessibilityLabel(playing ? "Stop" : "Play again")
    }

    /// The spoken message, recorded, in plain body type under the caption
    /// line. Selectable, because a line worth hearing is a line one may
    /// want to reuse. A text view, not a `Text`: `.textSelection` copies only
    /// the whole line, and on the Mac a drag over it selected nothing (the owner
    /// 2026-09-30: "I can copy the text, but it's only the full thing").
    private var saidLine: some View {
        StyledText(text: said, style: .subheadline, color: closed ? .secondary : .primary, macSize: 13.5)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var bodyText: some View {
        if let lines {
            StyledText(text: text, style: .subheadline, color: .secondary, lines: lines, macSize: 13.5)
        } else if isLongText(text) {
            // Folds like a long chat bubble: a screen's worth fading out, then
            // its full height — never a box scrolling inside the chat.
            StyledText(text: text, style: .subheadline, color: .secondary)
                .frame(maxHeight: bodyOpen ? nil : 320, alignment: .top)
                .clipped()
                .mask(LinearGradient(stops: [.init(color: .black, location: 0),
                                             .init(color: .black, location: bodyOpen ? 1 : 0.75),
                                             .init(color: bodyOpen ? .black : .clear, location: 1)],
                                     startPoint: .top, endPoint: .bottom))
            Button(bodyOpen ? "Show less" : "Show all · \(longLineCount(text)) lines") { bodyOpen.toggle() }
                .font(.caption.weight(.semibold)).buttonStyle(.bordered).tint(tint)
        } else {
            StyledText(text: text, style: .subheadline, color: .secondary)
        }
    }

    /// The one row. Word buttons at their own width, wrapping to a second
    /// line when four of them do not fit a phone; the primary in the tint.
    private var buttonRow: some View {
        FlowChips {
            ForEach(buttons) { b in
                // A link opens first, then `act` (Install closes the ask too).
                if b.primary {
                    Button { tap(b) } label: { word(b) }.buttonStyle(.borderedProminent).tint(tint)
                } else {
                    Button { tap(b) } label: { word(b) }.buttonStyle(.bordered)
                }
            }
        }.disabled(busy)
    }

    private func tap(_ b: CardButton) {
        if let u = b.url { openURL(u) }
        b.act()
    }

    private func word(_ b: CardButton) -> some View {
        // No extra side padding: with it, I did this · Won't do · Reply ·
        // Dismiss ran 7pt past a 402pt phone's card and Dismiss wrapped alone.
        Text(b.label).font(.subheadline.weight(.semibold)).padding(.vertical, 2)
            .foregroundStyle(b.primary ? .white : .primary)
    }

    /// A dismissed card keeps a footprint to reopen or reread it: one grey line where the card was; tap unfolds the body + Reopen.
    private func foldedBody(_ word: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Button { withAnimation(.snappy) { unfolded.toggle() } } label: {
                HStack(spacing: 6) {
                    Image(systemName: word == "read" ? "checkmark.circle" : "xmark.circle")
                    Text("\(word) ·").textCase(.uppercase).font(.caption2.weight(.semibold))
                    Text(md(title)).font(.subheadline).lineLimit(unfolded ? nil : 1).multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                    Image(systemName: unfolded ? "chevron.up" : "chevron.down").font(.caption2)
                }.foregroundStyle(.secondary).contentShape(Rectangle())
            }.buttonStyle(.plain)
            if unfolded {
                if !said.isEmpty { saidLine }
                if !text.isEmpty { StyledText(text: text, style: .subheadline, color: .secondary) }
                if !buttons.isEmpty { buttonRow }
                if let error { Text(error).font(.caption).foregroundStyle(.red) }
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Color.secondary.opacity(0.15), lineWidth: 1) }
        .padding(.horizontal)
    }
}
