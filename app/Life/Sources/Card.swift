// ONE card. Every cell in a chat — an ask, a proposal, a rec —
// is this chrome and nothing else: a tinted box, a caption line with its
// mark, the bold title, the body, a meta row, and ONE row of word buttons.
// The words come from the hub (`outcomes` on asks, actions and recs — the
// same list the console's ui.js draws), decisive first with the first one
// in the card's tint, "Reply" after them, and the client's silent close
// (Dismiss / Read) last. the owner: "they should have the same components, and
// there shouldn't be any excess styling code… reuse as much code as possible,
// standardize the UI." A closed card (done, decided, dismissed) is one
// ChatRow that opens in place to its body and Reopen (every kind but an
// install).
//
// AskCard and ApprovalCard decide WHAT the buttons say and do;
// nothing in them draws a box, a row or a button.
import SwiftUI

/// One word button on a card. A `url` opens (Install); `act` runs otherwise.
/// `primary` is FILLED in the card's tint: the pick that is on the chat bar
/// right now, or a one-shot act (Install, Restart). `suggested` is OUTLINED
/// in the tint: the first decisive pick while nothing from this card is on
/// the bar - a nudge, not a state.
struct CardButton: Identifiable {
    let label: String
    var primary = false
    var suggested = false
    var url: URL? = nil
    var act: () -> Void = {}
    var id: String { label }
}

/// A hub outcome (`{value,label}`) as a card button, in the hub's order —
/// the same order the Respond sheet's chips and the console's row and strip
/// draw (the owner 2026-09-18: "Buttons are in a different order on the bar than
/// on the card"). The hub lists the decisive picks first and words-only
/// ("Reply", value "") last. Nothing armed: the first decisive one is
/// suggested (outlined). A pick armed on the chat bar: THAT one is filled and
/// nothing is merely suggested, so arming a pick always visibly changes it.
/// `arm` is called with the value.
func outcomeButtons(_ outcomes: [AskOutcome], armed: String? = nil, arm: @escaping (String) -> Void) -> [CardButton] {
    outcomes.enumerated().map { i, o in
        CardButton(label: o.label, primary: armed == o.value, suggested: armed == nil && i == 0 && !o.value.isEmpty) { arm(o.value) }
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
/// table (views/threads.js), one call for all three (review-primitives step
/// 7; each card had its own pair before).
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

extension View {
    /// A chat card as a List row: the card is the whole cell, no white row
    /// and padding round it.
    func cardRow() -> some View {
        listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
    }
}

struct Card: View {
    /// The open card's colour: red (needs you), blue (read), teal (install),
    /// purple (rec). Grey when closed or folded.
    var tint: Color
    /// SF symbol + words on the caption line ("DECIDE", "APPROVAL",
    /// "RECOMMENDATION", or the state word once closed). No id beside it: an
    /// id means nothing to a person reading the card, and the console's head
    /// line never drew one either.
    var mark: String
    var caption: String
    /// Anything else on the caption line's right (a goal, "23m ago", a cost).
    var right: String = ""
    /// The dollars of the turn that raised it, which ride on its first card.
    var cost: Double = 0
    var title: String
    /// The body: markdown; past `isLongText` it folds behind "Show N more
    /// lines", like a long bubble, and opens to full height (no inner scroll).
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
    /// The detail is never folded behind More (an approval: its detail is
    /// what the owner says yes to).
    var unfolded = false
    /// The card's own id ("ask:ask-…"): what its More state is kept under.
    var key = ""
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
    /// `extra` holds the card's own buttons (a calendar item's steps) or a
    /// state (a pause bar): drawn after the meta row, just above the buttons.
    var extraShown = false
    var meta: AnyView? = nil
    /// The closed card's record in words: its note, what replaced it.
    var line: String = ""
    /// Set → the card is closed: one ChatRow led by this word ("✓ done",
    /// "✕ dismissed"), opening to the body and `buttons` (Reopen).
    var folded: String? = nil
    var buttons: [CardButton] = []
    /// Under the buttons: an approval's audit trail.
    var trail: AnyView? = nil
    var busy = false
    var error: String? = nil

    /// Redraws the card when More is tapped; which cards are open is `moreKeys`.
    @State private var moreTick = 0
    @MainActor private static var moreKeys = Set<String>()
    private var moreKey: String { key.isEmpty ? thread + "\u{1}" + title + "\u{1}" + said : key }
    @ObservedObject private var speaker = Speaker.shared
    @Environment(\.openURL) private var openURL

    /// "$0.60" when the card carries its turn's cost.
    private var when: String { cost > 0 ? usd(cost) : "" }

    var body: some View {
        if let folded { foldedBody(folded) } else { openBody }
    }

    /// The open card reads like a page: no box, a 2 pt rule in the tint down
    /// the left, the state in words, then the title, what was said in italic
    /// at the reading size (16 pt on the desktop, 1.6 apart), the detail a
    /// size down and grey, across the pane. Buttons are word capsules.
    private var openBody: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(caption.prefix(1).uppercased() + caption.dropFirst().lowercased()).foregroundStyle(tint)
                if !right.isEmpty { Text(right).lineLimit(1).foregroundStyle(Self.muted) }
                if !when.isEmpty { Text(when).lineLimit(1).fixedSize().foregroundStyle(Self.muted) }
                Spacer()
                // Play sits at the head line's far end - the console's head line.
                if !said.isEmpty { playButton.foregroundStyle(tint) }
            }.font(.zoomed(size: 12, weight: .semibold))
            // Selectable: the thing worth copying is often IN the title (a
            // paste-this string, an endpoint).
            let (whole, pictures) = split
            // Under a session's own spoken line only what the owner acts on
            // stays; the rest is behind More (`acted`).
            let (kept, rest) = Self.acted(whole, fold: lines == nil && !unfolded)
            // Open, More shows the whole detail in its written order. Its
            // state outlives a lazy row (`moreKeys`): a recycled row's
            // @State starts over.
            let more = moreTick >= 0 && !rest.isEmpty && Self.moreKeys.contains(moreKey)
            let detail = more ? whole : kept
            // The title is dropped only for a body that is SHOWN leading with it.
            if titled(over: detail) { StyledText(text: title, style: .body, color: .primary, bold: true, macSize: 18) }
            // What is heard leads, italic at the reading size; the detail
            // sits back, smaller and grey.
            if !said.isEmpty { StyledText(text: said, style: .body, color: .primary, macSize: 16, reading: true, italic: true) }
            // The card's pictures are drawn here, outside StyledText, so the
            // lightbox's arrow-key set is theirs.
            if !pictures.isEmpty { FileStrip(files: pictures).environment(\.pictureSet, lightboxSet(pictures.map(\.ref))) }
            if more {
                // All of it was asked for: whole, with no second "Show N more
                // lines" on top of More (the console's opened detail).
                StyledText(text: detail, style: .subheadline, color: .secondary, macSize: 14, reading: true)
            } else if !detail.isEmpty { bodyText(detail, reading: true) }
            if !rest.isEmpty {
                Button(more ? "Less" : "More") {
                    if Self.moreKeys.remove(moreKey) == nil {
                        if Self.moreKeys.count > 200 { Self.moreKeys.removeAll() }
                        Self.moreKeys.insert(moreKey)
                    }
                    moreTick += 1
                }
                .font(.zoomed(size: 13, weight: .semibold)).buttonStyle(.plain).foregroundStyle(Self.muted)
            }
            if let extra, !extraShown { extra }
            if let meta { meta }
            if let extra, extraShown { extra }
            if !buttons.isEmpty {
                FlowChips {
                    ForEach(buttons) { b in
                        // A link opens first, then `act` (Install closes the ask too).
                        Button { tap(b) } label: {
                            // One capsule for every word, a faint wash until
                            // it is the armed pick, then filled: arming never
                            // changes the row's width.
                            Text(b.label).font(.zoomed(size: 14, weight: .semibold))
                                .foregroundStyle(b.primary ? Color.white : tint)
                                .padding(.horizontal, 11).padding(.vertical, 4)
                                .background(b.primary ? tint : tint.opacity(0.1), in: Capsule())
                        }.buttonStyle(.plain).padding(.trailing, 6)
                    }
                }.disabled(busy).padding(.top, 2)
            }
            if let trail { trail }
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
        }
        .padding(.leading, 18)
        .padding(.vertical, 4)
        .overlay(alignment: .leading) { Rectangle().fill(tint).frame(width: 2) }
        // The pane's width, the rule on the rows' left edge, as the rows are.
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 6)
    }

    /// The head line's grey: the console's muted on the desktop.
    private static var muted: Color {
        #if targetEnvironment(macCatalyst)
        Web.muted
        #else
        Color.secondary
        #endif
    }

    /// The detail's pictures lead it, as a strip above the text: the detail
    /// without its picture lines, and their strip. The console's `splitPictures`.
    /// A card with two or more draft boxes keeps each picture where it is
    /// written, under its own box, so each post shows with its own picture.
    private var split: (text: String, pictures: [MdFile]) {
        if Self.draftBoxes(text) >= 2 { return (text.trimmingCharacters(in: .whitespacesAndNewlines), []) }
        var pictures: [MdFile] = [], rest: [Substring] = []
        for l in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if let f = blobLine(String(l)), isImageRef(f.ref) { pictures.append(f) } else { rest.append(l) }
        }
        return (rest.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines), pictures)
    }

    /// The detail's draft boxes: its fenced blocks. The console's `draftBoxes`.
    static func draftBoxes(_ text: String) -> Int {
        text.split(separator: "\n").filter { l in
            let t = l.drop { $0 == " " || $0 == "\t" }
            return t.hasPrefix("```") || t.hasPrefix("~~~")
        }.count / 2
    }

    /// THE SPOKEN LINE IS THE CARD; THE GREY UNDER IT STEPS BACK. On a card
    /// whose session wrote its own spoken line the detail keeps only what
    /// the owner acts on: numbered steps, anything with a link, a draft box,
    /// a table, and a one-line label over one of those; the rest is behind
    /// More. The console's `actedDetail`.
    /// What stays with a kept block: a one-line label over it (plain, bold
    /// or a heading) and an indented paragraph under a step. More opens the
    /// whole detail in its written order. An approval is never folded
    /// (`unfolded`): its detail is what the owner says yes to.
    ///
    /// WHICH CARDS FOLD (`folds`): only one whose line the session wrote
    /// (the hub speaks the title of a card raised with no --say, and under
    /// that the detail is the card), and only a kind whose detail is around
    /// what the owner does: a decision's options, an install's what-it-fixes
    /// and an error's text are what they answer to. The console's `askFolds`.
    /// The hub's own line is the title plus an opener and the session's name
    /// (cut at 80), so a session's line is one that runs well past both. A
    /// line cut short ("…") is not folded either: the detail then holds the
    /// only whole copy of it.
    static func folds(kind: String, said: String, title: String, thread: String) -> Bool {
        ["read", "physical", "access", "other"].contains(kind)
            && !said.trimmingCharacters(in: .whitespacesAndNewlines).hasSuffix("…")
            && SessionCell.plain(said).count > SessionCell.plain(title).count + min(SessionCell.plain(thread).count, 80) + 40
    }

    /// What the owner acts on, a line at a time: a numbered step, a checkbox,
    /// a link, a file on the hub, a thing to type (inline code), a draft box,
    /// a table, an email address, a phone number, a bare domain. The
    /// console's `ACTED`.
    private static let actedLine = #"(?m)^\s*\d+[.)]\s|^\s*[-*+]\s+\[[ xX]\]|\]\([^)]+\)|https?://|/api/v1/blobs/|`[^`\n]+`|^\s*(```|~~~)|^\s*\||^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$|[\w.+-]+@[\w-]+\.[\w.-]+|(\+?\d{1,2}[ .-]?)?\(?\d{3}\)?[ .-]\d{3}[ .-]\d{4}|\b[\w-]+\.(com|org|net|io|ai|co|app|dev|gov|edu)\b"#

    static func acted(_ text: String, fold: Bool) -> (kept: String, rest: String) {
        // "\r\n" is one Character to Swift and would never split on "\n".
        let text = text.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
        guard fold, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return (text, "") }
        var blocks: [String] = [], cur: [Substring] = [], fence = ""
        let fenced: (Substring) -> String = { l in
            let t = l.drop { $0 == " " || $0 == "\t" }
            return t.hasPrefix("```") ? "```" : t.hasPrefix("~~~") ? "~~~" : ""
        }
        for l in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let f = fenced(l)
            if !f.isEmpty, fence.isEmpty { fence = f } else if !f.isEmpty, f == fence { fence = "" }
            if fence.isEmpty, f.isEmpty, l.allSatisfy(\.isWhitespace) {
                if !cur.isEmpty { blocks.append(cur.joined(separator: "\n")) }
                cur = []
            } else { cur.append(l) }
        }
        if !cur.isEmpty { blocks.append(cur.joined(separator: "\n")) }
        let has: (String, String) -> Bool = { b, re in b.range(of: re, options: .regularExpression) != nil }
        var keep = blocks.map { has($0, actedLine) }
        // A list that opens the detail is the owner's steps even when
        // bulleted: a plain reply's bullets are filed as its steps.
        if let first = blocks.first, has(first, #"^\s*[-*+•]\s"#) { keep[0] = true }
        let label: (Int) -> Bool = { i in
            blocks.indices.contains(i) && !blocks[i].contains("\n") && has(blocks[i], #":\s*[*_]*\s*$|^\s*#{1,6}\s"#)
        }
        let step: (Int) -> Bool = { i in blocks.indices.contains(i) && has(blocks[i], #"^\s*\d+[.)]\s"#) }
        for i in blocks.indices where !keep[i] {
            // An indented code block under its label; a step's own paragraph,
            // indented under it or written flush between two steps.
            let kept = i > 0 && keep[i - 1]
            if label(i - 1), blocks[i].split(separator: "\n").allSatisfy({ $0.hasPrefix("    ") || $0.hasPrefix("\t") }) {
                keep[i] = true
            } else if kept, has(blocks[i], #"^( {2,}|\t)\S"#) {
                keep[i] = true
            } else if kept, step(i + 1), (0..<i).contains(where: step) {
                keep[i] = true
            }
        }
        for i in blocks.indices.dropLast() where !keep[i] && keep[i + 1] && label(i) { keep[i] = true }
        let pick: (Bool) -> String = { want in blocks.indices.filter { keep[$0] == want }.map { blocks[$0] }.joined(separator: "\n\n") }
        return (pick(true), pick(false))
    }

    /// Does the message already say the title? Compared plain (links to their
    /// text, marks dropped, case folded), a cut title's "…" off — the console's
    /// `saidCovers`. Then the title stays for rows and the card leads with
    /// the message alone. The body covers it only by starting with it: an
    /// approval titled "git commit" keeps its title over a will-run block
    /// that merely holds "git commit -m x".
    /// `shown` is the detail as drawn: a first paragraph behind More covers nothing.
    private func titled(over shown: String) -> Bool {
        Self.titled(title: title, said: said, shown: shown)
    }
    static func titled(title: String, said: String, shown: String) -> Bool {
        let t = SessionCell.plain(title).lowercased()
        guard !t.isEmpty else { return true }
        let saidSays = !said.isEmpty && SessionCell.plain(said).lowercased().contains(t)
        let bodyLeads = !shown.isEmpty && SessionCell.plain(shown).lowercased().hasPrefix(t)
        return !saidSays && !bodyLeads
    }

    /// Hear the spoken message again; tap again to stop.
    @ViewBuilder private var playButton: some View {
        // `speaking` and `waiting` come through BoardStore.hushed: one hush
        // flips this corner, the chat head and the row in one redraw.
        if speaking {
            // The line being heard reads "Speaking", and the same double tap
            // stops it — the hub cuts the Mac's voice, this stops the phone's
            // (the owner 2026-09-29: "if you double click it turns back into the
            // play button").
            Label("Speaking", systemImage: "circle.fill")
                .labelStyle(.titleAndIcon).textCase(.uppercase).foregroundStyle(.green)
                .symbolEffect(.pulse)
                // One line always: the head's other text gives way first.
                .lineLimit(1).fixedSize()
                .frame(minHeight: 22).padding(.leading, 8).contentShape(Rectangle())
                .onTapGesture(count: 2) { speaker.stop(); hush() }
                .accessibilityLabel("Speaking").accessibilityHint("Double-tap to stop speaking")
                .accessibilityAction { speaker.stop(); hush() }
        } else if waiting {
            Label("Waiting to speak", systemImage: "circle.fill")
                .labelStyle(.titleAndIcon).textCase(.uppercase).foregroundStyle(.orange)
                .lineLimit(1).fixedSize()
                .frame(minHeight: 22).padding(.leading, 8).contentShape(Rectangle())
                .onTapGesture(count: 2) { hush() }
                .accessibilityLabel("Waiting to speak").accessibilityHint("Double-tap to not speak this one")
                .accessibilityAction { hush() }
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
        StyledText(text: said, style: .subheadline, color: .primary, macSize: 13.5)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func bodyText(_ text: String, reading: Bool = false) -> some View {
        if let lines {
            StyledText(text: text, style: .subheadline, color: .secondary, lines: lines, macSize: 13.5)
        } else if reading {
            // The open card's detail: under the spoken line, a size down and
            // grey. 14 pt on the desktop keeps equations readable.
            if isLongText(text) {
                FoldedText(text: text, style: .subheadline, color: .secondary, macSize: 14, reading: true) { label, tap in
                    Button(label, action: tap).font(.zoomed(size: 13, weight: .semibold)).buttonStyle(.plain).foregroundStyle(tint)
                }
            } else {
                StyledText(text: text, style: .subheadline, color: .secondary, macSize: 14, reading: true)
            }
        } else if isLongText(text) {
            // Folds like a long chat bubble: two screens' worth fading out,
            // popped open to full height and shut again - never a box
            // scrolling inside the chat; whole when fewer than five lines
            // were behind the fold (FoldedText).
            FoldedText(text: text, style: .subheadline, color: .secondary) { label, tap in
                Button(label, action: tap).font(.caption.weight(.semibold)).buttonStyle(.bordered).tint(tint)
            }
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
                } else if b.suggested {
                    Button { tap(b) } label: { word(b) }.buttonStyle(.bordered).tint(tint)
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
            .foregroundStyle(b.primary ? AnyShapeStyle(.white) : b.suggested ? AnyShapeStyle(tint) : AnyShapeStyle(.primary))
    }

    /// A closed card keeps a footprint to reopen or reread it: its row,
    /// opening to what it said (often the thing to copy) and Reopen.
    private func foldedBody(_ word: String) -> some View {
        ChatRow(k: word, title: title, when: when) {
            if !said.isEmpty { saidLine }
            // The same body as the open card's: a long one is the window.
            if !text.isEmpty { bodyText(text) }
            if !line.isEmpty { StyledText(text: line, style: .caption1, color: .secondary, macSize: 12) }
            if !buttons.isEmpty { buttonRow }
            if let trail { trail }
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
        }
    }
}

private struct PlainRowsKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
    var plainRows: Bool {
        get { self[PlainRowsKey.self] }
        set { self[PlainRowsKey.self] = newValue }
    }
}

/// THE ROW. Everything in a chat that is not an open card is one line of the
/// same shape: a closed card, the owner's message, a note, a check-in. `k`
/// leads it (✓ DONE, YOU, ⏰ CHECK-IN), then the title cut to the line, the
/// time, ›; a tap opens it in place. `mine` is the owner's: grey, no outline;
/// every other row is plain with a hairline. The console's rowHTML (ui.js).
struct ChatRow<Content: View>: View {
    let k: String
    let title: String
    var when: String = ""
    var mine = false
    /// The word's colour when it is not grey (a check-in green, an error red).
    var tone: Color? = nil
    @ViewBuilder var content: () -> Content
    /// Inside the desktop's whole-chat line (HistoryLine): no box, tight.
    @Environment(\.plainRows) private var plain
    /// LIFE_SHOT_UNFOLD=1 (ops/screens.sh, ops/mac-screens.sh UNFOLD=1): every
    /// row drawn open, as after a tap, for a shot of that state.
    @State private var open = ProcessInfo.processInfo.environment["LIFE_SHOT_UNFOLD"] == "1"
    /// The phone has no room for "today": a bare clock is today's.
    private var stamp: String {
        #if targetEnvironment(macCatalyst)
        when
        #else
        when.hasPrefix("today ") ? String(when.dropFirst(6)) : when
        #endif
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button { withAnimation(.snappy) { open.toggle() } } label: {
                HStack(spacing: 8) {
                    Text(k).textCase(.uppercase).font(.system(size: 11, weight: .semibold)).tracking(0.4)
                        .foregroundStyle(tone ?? (mine ? Color.primary.opacity(0.75) : Color.secondary))
                        .lineLimit(1)
                        #if targetEnvironment(macCatalyst)
                        .frame(width: 100, alignment: .leading)
                        #else
                        .fixedSize()
                        #endif
                    Text(md(title)).font(.system(size: 13)).foregroundStyle(Color.primary.opacity(0.8)).lineLimit(1)
                    Spacer(minLength: 4)
                    if !when.isEmpty { Text(stamp).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1).fixedSize() }
                    Text("›").font(.system(size: 13)).foregroundStyle(Color.secondary.opacity(0.6)).rotationEffect(.degrees(open ? 90 : 0))
                }
                .padding(.horizontal, plain ? 0 : 10).padding(.vertical, plain ? 4 : 7)
                .contentShape(Rectangle())
            }.buttonStyle(.plain)
            if open {
                VStack(alignment: .leading, spacing: 6) { content() }
                    .padding(.horizontal, plain ? 0 : 12).padding(.top, 2).padding(.bottom, plain ? 6 : 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .background(plain ? Color.clear : mine ? Color.primary.opacity(0.07) : Color(.systemBackground), in: RoundedRectangle(cornerRadius: 8))
        .overlay { if !mine && !plain { RoundedRectangle(cornerRadius: 8).strokeBorder(Color.primary.opacity(0.1), lineWidth: 1) } }
        // The cards' width, never a column of its own: a width cap left the
        // owner's row stopping short under full-width cards.
        .padding(.horizontal)
    }
}
