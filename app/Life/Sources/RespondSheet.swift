// Respond: the one thing you do to a card.
//
// One control carries the two things a response actually has: an OUTCOME
// (the owner's claim about the card — the card leaves the board the moment
// they say it) and WORDS. Destination and time are not choices here
// (responding always means now, in this session): the answer goes to the session that raised the card, right away.
// The general primitive — a message to any session at a time — still exists
// (`POST /prompts`, and the chat composer's quiet send-options row); it just
// has no seat in the respond flow. It posts a single `prompts` row; the
// reference travels as data (`in_reply_to`), so the agent is never told what
// the owner meant by a sentence the hub composed and then re-read.
import SwiftUI

/// What a response can be about. One shape so the same sheet serves an ask
/// today and an approval or a rec later — the hub's reference vocabulary is
/// already typed (`ask:` | `action:` | `rec:` | `cal:` | `message:`).
struct RespondSubject {
    /// "<type>:<id>" — what the prompt answers.
    var ref: String
    var title: String
    var detail: String = ""
    /// The session that raised it: "this session".
    var threadID: String
    var threadTitle: String = ""
    /// The answer chips this card offers, in the hub's order — the card's
    /// own row, never reshuffled. These are the hub's words (`ask.outcomes`, one vocabulary for both surfaces:
    /// a read card says "Read it", a decision "Decided", an access ask
    /// "Granted"), decisive first and "Reply" last; an older hub gets
    /// the old three.
    var outcomes: [RespondOutcome] = []
    /// The chip lit when the sheet opens: the card button tapped, or
    /// "Reply" (words alone) when it was opened bare.
    var initial: RespondOutcome = .none
    /// Can the owner answer with words alone and leave the card open? Every
    /// card but a read: a read closes when answered, typed or not, so
    /// the sheet never offers "keep it open" and shows no chip row at all —
    /// there is nothing to choose.
    var allowsWordsOnly = true
    /// A card whose wordless answer says nothing to the session (a read): the
    /// hub records it and stops there rather than spinning the session up.
    var silentWithoutWords = false
    /// Send needs words even with an outcome chosen. True for one of the
    /// owner's own calendar steps, so a stray tap can't mark one done.
    var requireWords = false
    /// A to-do no session is behind (`CalEntry.isLoneTodo`): an outcome closes
    /// the item on the hub with the owner's words as its note — no prompt, no
    /// session.
    var loneTodo: String?
    /// Homework: closes without starting a session, or starts one if words
    /// are typed. Did it / Skip with nothing typed closes it on the
    /// hub and wakes nobody; words go to a session — a new one titled
    /// `newTitle` when nothing is behind it — with `footer` under them.
    var quiet: QuietClose?
    var newTitle = ""
    var footer = ""
    enum QuietClose { case cal(String) }

    /// A homework row on the calendar: the console's calHomeworkBox.
    init(homework e: CalEntry, first: String) {
        ref = "cal:" + e.id
        title = e.title
        detail = e.detail ?? ""
        threadID = e.thread_id ?? ""
        quiet = .cal(e.id)
        newTitle = e.title
        outcomes = Self.chips(e.outcomes)
        initial = outcomes.first { $0.value == first } ?? .none
    }

    /// The hub's answers as chips (store/close.go); words alone when it sent
    /// none.
    static func chips(_ o: [AskOutcome]?) -> [RespondOutcome] {
        let c = (o ?? []).map { RespondOutcome(value: $0.value, label: $0.label) }
        return c.isEmpty ? [.none] : c
    }

    /// One of the owner's own dated steps. The prompt carries `cal:<id>` with
    /// done|wont, and the hub closes the item (and its ask) through that row —
    /// the same write the console's box and `lifectl cal … done <note>` make.
    /// `first` is the chip swiped or tapped, so it starts lit.
    init(cal e: CalEntry, first: String) {
        ref = "cal:" + e.id
        title = e.title
        detail = e.detail ?? ""
        threadID = e.thread_id ?? "calendar"
        requireWords = true
        loneTodo = e.isLoneTodo ? e.id : nil
        outcomes = Self.chips(e.outcomes)
        initial = outcomes.first { $0.value == first } ?? .none
    }

    /// A rec on the desktop's Recs pages: Accept · Decline · Reply as the
    /// chips over the chat bar, so a rec is answered like any other card. The
    /// prompt carries `rec:<id>` with accepted|declined and the hub decides
    /// the rec through that row (threads DecideRec); words go to the session
    /// that filed it, a new one when none did.
    init(rec r: Rec) {
        ref = "rec:" + r.id
        title = r.title
        threadID = r.sourceThreadID ?? ""
        newTitle = mdPlain(r.title)
        outcomes = Self.chips(r.outcomes ?? [AskOutcome(value: "accepted", label: "Accept"),
                                             AskOutcome(value: "declined", label: "Decline"),
                                             AskOutcome(value: "", label: "Reply")])
        initial = .none
    }

    /// `first` is the card button tapped (the row is the hub's outcomes), so the sheet starts on that chip; "" = words alone.
    init(ask: Ask, first: String = "") {
        ref = "ask:" + ask.id
        title = ask.title
        detail = ask.detailWithoutLinks
        threadID = ask.thread_id
        threadTitle = ask.thread_title ?? ""
        allowsWordsOnly = ask.kind != "read"
        silentWithoutWords = ask.kind == "read"
        outcomes = (ask.outcomes ?? []).map { RespondOutcome(value: $0.value, label: $0.label) }
        // Words alone is a real chip on every card but a read (the hub puts it
        // last; a card with no answers at all — a crash — is words only).
        if allowsWordsOnly, !outcomes.contains(where: { $0.value.isEmpty }) { outcomes.append(.none) }
        initial = outcomes.first { $0.value == first } ?? outcomes.first ?? .none
    }
}

/// One claim a response can carry: the `outcome` posted with the prompt and
/// the label the chip shows. An empty value is a real choice: words with no
/// outcome answer the card without closing it — the ball goes back to the
/// agent and the card stays on the board as "waiting on agent". Icon and
/// tint follow the value, which is the hub's fixed set ("" | done | wont);
/// the label is whatever the hub says for this kind of card.
struct RespondOutcome: Hashable {
    var value: String
    var label: String
    static let none = RespondOutcome(value: "", label: "Reply")
    var closes: Bool { !value.isEmpty }
    var icon: String {
        switch value { case "done", "accepted": "checkmark"; case "wont", "declined": "xmark"; default: "bubble.left" }
    }
    var tint: Color {
        switch value { case "done", "accepted": .green; case "wont", "declined": .gray; default: .accentColor }
    }
}

/// When the words reach a session — the chat composer's send-options row and
/// the Recs page use this; the Respond sheet always sends now. The outcome
/// does not wait for it — a card closes when the owner says it.
enum RespondWhen: Hashable {
    case now, inAnHour, tomorrowMorning, at(Date)

    var label: String {
        switch self {
        case .now: "Now"
        case .inAnHour: "In an hour"
        case .tomorrowMorning: "Tomorrow 9am"
        case .at(let d): d.formatted(.dateTime.weekday(.abbreviated).hour().minute())
        }
    }
    /// The three fields `POST /api/v1/prompts` takes. A day + clock time is
    /// read by the hub in Eastern, so the instant picked is formatted in
    /// Eastern here rather than in whatever zone the phone is standing in.
    var fields: (inAfter: String, on: String, at: String) {
        switch self {
        case .now: ("", "", "")
        case .inAnHour: ("1h", "", "")
        case .tomorrowMorning:
            ("", RespondWhen.day(Date().addingTimeInterval(86400)), "09:00")
        case .at(let d):
            ("", RespondWhen.day(d), RespondWhen.clock(d))
        }
    }
    static func day(_ d: Date) -> String { Fmt.easternDay.string(from: d) }
    static func clock(_ d: Date) -> String { Fmt.easternClock.string(from: d) }
}

/// Which session hears it — the chat composer's send-options row; the Respond
/// sheet always answers the session that raised the card.
enum RespondTarget: Hashable {
    case thisSession, newSession
    var label: String { self == .thisSession ? "This session" : "New session" }
    var icon: String { self == .thisSession ? "bubble.left.and.bubble.right" : "plus.bubble" }
    /// "new-or:<id>" rather than the bare id on purpose: a session that has
    /// since ended must not swallow the answer — the hub starts a fresh one
    /// and spells the reference out in its opening message.
    func value(thread: String) -> String { self == .newSession ? "new" : "new-or:" + thread }
}

/// One response in progress — the pick, the words, the send. Its own object
/// so the same answer draws two ways: the Respond sheet (a card on the board)
/// and INLINE on a calendar item's own page, with no inner page. One set of rules, one submit, whichever page holds it.
@Observable @MainActor
final class RespondModel {
    let subject: RespondSubject
    /// Starts on the chip tapped (`subject.initial`); the row keeps the
    /// hub's order around it.
    var outcome: RespondOutcome
    let draft = Draft()
    let attachments = AttachmentDraft()
    var busy = false
    var error: String?
    var confirmingEmpty = false

    init(_ subject: RespondSubject) {
        self.subject = subject
        outcome = subject.initial
    }

    /// An outcome IS a response, so Send stays live with nothing typed —
    /// except on the owner's own calendar step, which closes only with words.
    var allowEmpty: Bool { outcome.closes && !subject.requireWords }
    var placeholder: String { allowEmpty ? "Anything to add (optional)" : "Your reply" }

    /// One sentence saying exactly what Send will do. Responding always means
    /// now and this session, so the sentence only has to explain the outcome.
    var explain: String {
        // A read card: it closes when answered, and with nothing typed
        // nothing reaches the session at all.
        if subject.silentWithoutWords {
            return "This card leaves your board now. Anything you type goes to the session that raised it right away; with nothing typed, nothing wakes."
        }
        if subject.quiet != nil {
            let words = subject.threadID.isEmpty ? "start a session about it" : "go to its session"
            switch outcome.value {
            case "done": return "Marked done. With nothing typed it just closes; anything you type will \(words)."
            case "wont": return "Skipped. With nothing typed it just closes; anything you type will \(words)."
            default: return "It stays as it is — your words \(words)."
            }
        }
        if subject.loneTodo != nil {
            switch outcome.value {
            case "done": return "The to-do closes as done, with your words on it (Reopen undoes it)."
            case "wont": return "The to-do closes as won't do, with your words on it (Reopen undoes it)."
            default: return "The to-do stays open — your words start a session about it."
            }
        }
        var s = ""
        switch outcome.value {
        case "done": s = subject.requireWords ? "This step is marked done, with your words on it (Reopen undoes it). " : "This card leaves your board now. "
        case "wont": s = subject.requireWords ? "This step is cleared and the agent is told you're not doing it (Reopen undoes it). " : "This card leaves your board now and the agent is told you're not doing it. "
        default: s = subject.requireWords ? "The step stays open — your words go to its session. " : "The card stays open — the agent has the ball. "
        }
        return s + "The session that raised it gets it right away."
    }

    /// Sends it; `finished` runs once it landed (refresh, close the page).
    func submit(hub: HubClient, confirmed: Bool = false, finished: @escaping () async -> Void) {
        // An empty reply to a read asks first ("Are you sure?" → "Send
        // without").
        if subject.silentWithoutWords, !confirmed, !draft.text.contains(where: { !$0.isWhitespace }), attachments.isEmpty {
            confirmingEmpty = true
            return
        }
        busy = true
        let text = draft.text
        Task {
            do {
                if let q = subject.quiet {
                    let spoke = text.contains(where: { !$0.isWhitespace }) || !attachments.isEmpty
                    if !spoke {
                        if case .cal(let id) = q, outcome.closes {
                            _ = try await hub.resolveCalItem(id, state: outcome.value == "done" ? "done" : "dismissed")
                        }
                        await finished()
                        return
                    }
                    let refs = try await attachments.upload(to: hub, threadID: subject.threadID.isEmpty ? nil : subject.threadID)
                    try await hub.postPrompt(target: subject.threadID.isEmpty ? "new" : "new-or:" + subject.threadID,
                                             text: text + subject.footer, re: subject.ref,
                                             outcome: outcome.value,
                                             title: subject.newTitle, attachments: refs)
                    await finished()
                    return
                }
                if let id = subject.loneTodo, outcome.closes, attachments.isEmpty {
                    _ = try await hub.resolveCalItem(id, state: outcome.value == "done" ? "done" : "dismissed", note: text)
                    await finished()
                    return
                }
                let refs = try await attachments.upload(to: hub, threadID: subject.threadID)
                // Always now, always the session that raised it ("new-or:" so
                // an ended session does not swallow the answer — the hub
                // starts a fresh one and spells the reference out).
                // A rec no session filed has nowhere to "this session": new.
                try await hub.postPrompt(target: subject.threadID.isEmpty ? "new" : RespondTarget.thisSession.value(thread: subject.threadID),
                                         text: text, re: subject.ref, outcome: outcome.value,
                                         inAfter: "", on: "", at: "",
                                         title: subject.newTitle,
                                         attachments: refs)
                await finished()
            } catch {
                self.error = error.localizedDescription
                busy = false
            }
        }
    }
}

/// The outcome chips in the hub's order, as the card's row draws them;
/// "Reply" is already in `outcomes` wherever words alone is a real answer (a
/// read card closes either way, so it never gets that chip).
struct RespondChips: View {
    let model: RespondModel
    var body: some View {
        FlowChips {
            ForEach(model.subject.outcomes, id: \.self) { o in
                PickChip(o.label, icon: o.icon, tint: o.tint, on: model.outcome == o) { model.outcome = o }
            }
        }
    }
}

/// The chat bar under a response: a session's own composer, so a photo of
/// what was done rides with the words (its + button).
struct RespondBar: View {
    @Environment(HubClient.self) private var hub
    let model: RespondModel
    let finished: () async -> Void
    var body: some View {
        Composer(draft: model.draft, attachments: model.attachments,
                 placeholder: model.placeholder, sending: model.busy,
                 send: { model.submit(hub: hub, finished: finished) }, allowEmpty: model.allowEmpty)
            .padding(10)
            .confirmationDialog("Are you sure?", isPresented: Binding(get: { model.confirmingEmpty }, set: { model.confirmingEmpty = $0 }), titleVisibility: .visible) {
                Button("Send without") { model.submit(hub: hub, confirmed: true, finished: finished) }
                Button("Cancel", role: .cancel) {}
            }
    }
}

struct RespondSheet: View {
    @Environment(\.dismiss) private var dismiss
    /// Refresh whatever showed the card (the board, the session).
    var reload: () async -> Void = {}
    @State private var model: RespondModel

    init(subject: RespondSubject, reload: @escaping () async -> Void = {}) {
        self.reload = reload
        _model = State(initialValue: RespondModel(subject))
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        card
                        // One outcome is not a choice (a read card): the sheet
                        // is then purely "reply", and the close is implied.
                        if model.subject.outcomes.count > 1 {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("What happened").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                                RespondChips(model: model)
                            }
                        }
                        Text(model.explain).font(.footnote).foregroundStyle(.secondary)
                        if let error = model.error { ErrorBanner(message: error) }
                    }.padding()
                }
                Divider()
                RespondBar(model: model) { await reload(); dismiss() }
            }
            .navigationTitle("Respond")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
    }

    /// The thing being answered, quoted, so the response references what it
    /// came from.
    private var card: some View {
        let subject = model.subject
        return VStack(alignment: .leading, spacing: 4) {
            Text(md(subject.title)).font(.subheadline.weight(.semibold))
            if !subject.detail.isEmpty {
                Text(md(subject.detail)).font(.footnote).foregroundStyle(.secondary).lineLimit(4)
            }
            HStack(spacing: 6) {
                Text(subject.ref).font(.caption2).foregroundStyle(.tertiary)
                if !subject.threadTitle.isEmpty {
                    Text("· \(mdPlain(subject.threadTitle))").font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
    }

}

/// A chip you choose, not a chip you read (`Chip` is the read-only one).
struct PickChip: View {
    let text: String
    var icon: String? = nil
    var tint: Color = .accentColor
    let on: Bool
    let tap: () -> Void
    init(_ text: String, icon: String? = nil, tint: Color = .accentColor, on: Bool, tap: @escaping () -> Void) {
        self.text = text; self.icon = icon; self.tint = tint; self.on = on; self.tap = tap
    }
    var body: some View {
        Button(action: tap) {
            HStack(spacing: 5) {
                if let icon { Image(systemName: icon).font(.system(size: 11, weight: .semibold)) }
                Text(text).font(.subheadline.weight(on ? .semibold : .regular))
            }
            .padding(.horizontal, 11).padding(.vertical, 7)
            .background(on ? AnyShapeStyle(tint.opacity(0.18)) : AnyShapeStyle(.quaternary.opacity(0.5)), in: Capsule())
            .foregroundStyle(on ? tint : .secondary)
            .overlay { Capsule().strokeBorder(on ? tint.opacity(0.6) : .clear, lineWidth: 1) }
            // The whole capsule takes the click, not just its glyphs: a plain
            // button's hit area is its drawn content, and on the Mac a click
            // beside the word fell through (the owner 2026-10-01: "it's not
            // letting me click on did or skip").
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? [.isSelected, .isButton] : .isButton)
    }
}
