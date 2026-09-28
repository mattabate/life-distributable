import SwiftUI

/// More → Recommendations: the list the owner browses when they are in the
/// mood to spend money, pick a tool or change a habit.
///
/// It lives under More and NOT on the tab bar on purpose (the bar stays
/// uncrowded), and it must never PUSH. That is the whole distinction the
/// object exists to draw: an ask is push — their turn, now — and a rec is
/// pull, welcome only in the moment they came looking. Nothing in this file
/// may ever touch PushState or a notification.
///
/// It DOES carry a count, on the More row and folded into the More tab, the
/// same way the web app does. A number seen only once the app is already
/// open is still pull — it is what is sitting there undecided, not a tap on
/// the shoulder.
///
/// The one interaction that gets extra room is THE OWNER'S NOTE. It is taste,
/// taste is expensive to learn twice, and a note hidden behind a second tap
/// never gets typed — so either swipe opens one box for it (RecDecisionSheet),
/// and that box is the whole decision: accept or decline, their words go to
/// an agent and they land in the session that got them.
struct RecsView: View {
    @Environment(HubClient.self) private var hub
    @Environment(BoardStore.self) private var store
    @State private var list = HubLoad<[Rec]>()
    /// Everything the hub has (in the domain); the page draws the open ones and
    /// counts the rest on the All recommendations row.
    @State private var domain = ""
    private var every: [Rec] { list.value ?? [] }
    private var recs: [Rec] { every.filter(\.isOpen) }
    @State private var showAll = false
    /// Either swipe opens the same box (see RecDecisionSheet).
    @State private var deciding: RecDecisionRequest?
    /// The session a decision landed in, pushed once the sheet is gone.
    @State private var landed: Thread?
    @State private var sheetUp = false
    @State private var openThread: Thread?
    /// A rec id tapped in a card's text (Refs.swift) opens that rec's page.
    @Environment(RefNav.self) private var nav
    @State private var openRec: Rec?

    static let domains = ["", "money", "health", "audience", "tools", "home", "other"]

    var body: some View {
        List {
            if let e = list.error { Section { ErrorBanner(message: e) } }
            // TWO VIEWS, NOT SEVEN TABS (parity with the console): the open recs, then one row to every rec — the Sessions
            // page's All sessions row. The status tabs and the track record
            // are gone from both surfaces; `lifectl recs <status>|stats` stays.
            Section {
                if recs.isEmpty {
                    if list.loaded {
                        Text("Nothing open").foregroundStyle(.secondary)
                    } else { ProgressView() }
                }
                ForEach(recs) { r in
                    NavigationLink { RecDetail(rec: r, onChange: { await load() }) } label: { RecRow(rec: r) }
                        .swipeActions(edge: .leading) {
                            if !r.isClosed {
                                Button { deciding = RecDecisionRequest(rec: r, way: .accept) } label: { Label("Accept", systemImage: "checkmark") }.tint(.green)
                                // The fourth answer: a note with no verdict.
                                Button { deciding = RecDecisionRequest(rec: r, way: .reply) } label: { Label("Reply", systemImage: "arrowshape.turn.up.left") }.tint(.blue)
                            }
                        }
                        .swipeActions(edge: .trailing) {
                            if !r.isClosed {
                                Button { deciding = RecDecisionRequest(rec: r, way: .decline) } label: { Label("Decline", systemImage: "hand.thumbsdown") }.tint(.red)
                            }
                        }
                        .chatAbout(ChatSubject(
                            screen: "Recommendations",
                            title: r.title,
                            facts: ["- `\(r.id)`, \(r.domain)/\(r.kind), \(r.status), \(r.costLabel)",
                                    "- confidence \(r.confidence)%, \(r.effort) effort" + (r.review_on.map { ", review \($0)" } ?? ""),
                                    (r.because?.isEmpty == false) ? "- because: \(r.because!)" : "",
                                    (r.expect?.isEmpty == false) ? "- expect: \(r.expect!)" : ""].filter { !$0.isEmpty }.joined(separator: "\n"),
                            code: "read it with `lifectl rec \(r.id)`"))
                }
            }
            if !every.isEmpty {
                Section {
                    Button { showAll = true } label: {
                        HStack {
                            Text("All recommendations").foregroundStyle(.primary)
                            Spacer()
                            Text("\(every.count)").foregroundStyle(.secondary)
                            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
        .navigationTitle("Recommendations")
        .navigationDestination(isPresented: $showAll) { AllRecsView(recs: every, onChange: { await load() }) }
        .askButton()
        .toolbar {
            Menu {
                Picker("Domain", selection: $domain) {
                    ForEach(Self.domains, id: \.self) { Text($0.isEmpty ? "All domains" : $0).tag($0) }
                }
            } label: { Image(systemName: domain.isEmpty ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill") }
                .accessibilityLabel("Filter by domain")
        }
        .hubTask(list, id: domain) { try await fetch() }
        // A rec filed or decided anywhere moves the change feed's version;
        // that, not a timer, is when the list reads again.
        .onChange(of: store.version) { Task { await load() } }
        .sheet(item: $deciding, onDismiss: {
            // Landing in the session is the point of the flow, and a push made
            // while the sheet is still on screen is swallowed.
            sheetUp = false
            if let t = landed { landed = nil; openThread = t }
        }) { req in
            RecDecisionSheet(request: req) { thread in
                if sheetUp { landed = thread } else { openThread = thread }
                deciding = nil
                await load()
            }
            .onAppear { sheetUp = true }
        }
        .navigationDestination(item: $openThread) { ThreadDetail(thread: $0) }
        .navigationDestination(item: $openRec) { RecDetail(rec: $0, onChange: { await load() }) }
        .onChange(of: nav.rec, initial: true) { _, r in if let r { nav.rec = nil; openRec = r } }
    }

    private func load() async { await list.run { try await fetch() } }

    /// The list. The Recs oval is the board's (`GET /board` badges.recs) —
    /// this view never writes it.
    private func fetch() async throws -> [Rec] { try await hub.recs(status: "all", domain: domain) }
}

/// Every rec, newest first, found by title — the console's #/recs/all table as
/// a phone list. Each row wears its domain and where it ended up (RecRow).
struct AllRecsView: View {
    let recs: [Rec]
    var onChange: () async -> Void
    @State private var query = ""
    private var shown: [Rec] {
        let words = query.lowercased().split(separator: " ")
        return recs.filter { r in words.allSatisfy { r.title.lowercased().contains($0) } }
    }
    var body: some View {
        List {
            if shown.isEmpty { Text("No rec by that name").foregroundStyle(.secondary) }
            ForEach(shown) { r in
                NavigationLink { RecDetail(rec: r, onChange: onChange) } label: { RecRow(rec: r) }
            }
        }
        .searchable(text: $query, prompt: "Find a rec")
        .navigationTitle("All recommendations · \(recs.count)")
        .navigationBarTitleDisplayMode(.inline)
        .askButton()
    }
}

/// Which rec is being decided and which way. The sheet is the same box both
/// ways — the note is worth as much on a decline (it is why the idea does not
/// come back) as on an accept, and both go to an agent.
struct RecDecisionRequest: Identifiable {
    /// Accept, decline, or a reply with no verdict — the same options as the
    /// web app. The raw value is the status sent on `/decide`; `reply` posts
    /// to `/reply` and sets no status at all.
    ///
    /// There is no `later`: parking a rec until a day cost a fourth swipe, a
    /// fourth button and a date picker, and the hub still takes
    /// `status: deferred` for `lifectl rec <id> defer <date>`.
    enum Way: String { case accept = "accepted", decline = "declined", reply = "reply" }
    let rec: Rec
    let way: Way
    var id: String { rec.id + way.rawValue }
    var accept: Bool { way == .accept }
    var status: String { way.rawValue }
}

/// One row: what it is, why in a line, and what saying yes costs.
struct RecRow: View {
    let rec: Rec
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(md(rec.title)).font(.body.weight(.medium)).lineLimit(2)
                Spacer(minLength: 8)
                Text(rec.costLabel).font(.caption).foregroundStyle(.secondary).monospacedDigit()
            }
            if let why = rec.because ?? rec.detail, !why.isEmpty {
                Text(md(why)).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            HStack(spacing: 8) {
                RecChip(text: rec.domain, color: recDomainColor(rec.domain))
                RecChip(text: rec.kind, color: .secondary)
                if let m = rec.modelShort { RecChip(text: m, color: .indigo) }
                if !rec.isOpen { RecChip(text: rec.status == "deferred" ? "later" : rec.status, color: recStatusColor(rec.status)) }
                if let o = rec.outcome, !o.isEmpty { RecChip(text: o, color: recOutcomeColor(o)) }
                // Its session is working right now, so there is nothing to
                // follow up on — the same dot + word an
                // ask row wears.
                if rec.thread_running == true { RecRunning() }
                if let c = recDatesColor(rec) {
                    Text(recDates(rec)).font(.caption2).foregroundStyle(c)
                } else {
                    Text(recDates(rec)).font(.caption2).foregroundStyle(.tertiary)
                }
            }
        }.padding(.vertical, 2)
    }
}

/// "● running": the session that filed the rec has a turn in flight.
struct RecRunning: View {
    var body: some View {
        HStack(spacing: 4) {
            LiveDot(size: 5)
            Text("running").font(.caption2).foregroundStyle(.blue)
        }
    }
}

func recDomainColor(_ d: String) -> Color {
    switch d { case "money": .orange; case "health": .green; case "audience": .purple; case "tools": .blue; default: .secondary }
}
func recStatusColor(_ s: String) -> Color {
    switch s { case "accepted": .blue; case "done": .green; case "deferred": .orange; case "expired", "superseded": .secondary; default: .secondary }
}
func recOutcomeColor(_ o: String) -> Color {
    switch o { case "worked": .green; case "mixed": .orange; case "failed": .red; default: .secondary }
}

/// The hub's `dates_label` ("act by Jan 15 (12d) · review Oct 1"), the
/// console's exact words.
func recDates(_ r: Rec) -> String { r.dates_label ?? "" }

/// act_by is a deadline the hub enforces: amber within a week, red once gone —
/// the console's colours, off the hub's `days_left`.
func recDatesColor(_ r: Rec) -> Color? {
    guard let left = r.days_left else { return nil }
    return left < 0 ? .red : left <= 7 ? .orange : nil
}


/// The whole decision, in one box, with no second screen: what the owner
/// types is their note on the record AND the message an agent gets, accepted
/// or declined alike, and hitting the button lands them in the session that got it. The only choice on top of the
/// note is which session — the one that filed the rec (the default: it wrote
/// the argument and needs no brief) or a fresh one, which the hub gives the
/// whole record and where to find it. The text is the hub's
/// (`GET /recs/{id}/starter`), so the console sends exactly the same thing.
struct RecDecisionSheet: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    let request: RecDecisionRequest
    /// The session the note went to, or nil if the owner backed out.
    let onDecided: (Thread?) async -> Void

    @State private var starter: RecStarter?
    @State private var toSource = true
    @State private var draft = Draft()
    @State private var attachments = AttachmentDraft()
    @State private var busy = false
    @State private var error: String?
    @State private var showBrief = false
    // No "when it's heard" here: nobody schedules a future answer to a
    // recommendation. The hub's decide/reply still take the prompt timing
    // fields — the primitive stays — but a rec answer from either surface
    // goes now. RespondWhen lives on in the Respond sheet, where it belongs.

    private var rec: Rec { request.rec }
    private var hasSource: Bool { !(starter?.source_session ?? "").isEmpty }
    private var goesToSource: Bool { toSource && hasSource }
    private var reply: Bool { request.way == .reply }

    private var heading: String { reply ? "Replying" : request.accept ? "Accepting" : "Declining" }
    private var icon: String {
        reply ? "arrowshape.turn.up.left.fill" : request.accept ? "checkmark.circle.fill" : "hand.thumbsdown.fill"
    }
    private var tint: Color { reply ? .blue : request.accept ? .green : .red }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(heading, systemImage: icon)
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(tint)
                        Text(md(rec.title)).font(.headline)
                        if hasSource {
                            Picker("Where", selection: $toSource) {
                                Text("Its session").tag(true)
                                Text("New session").tag(false)
                            }.pickerStyle(.segmented)
                        }
                        if let starter {
                            DisclosureGroup("What the session is told", isExpanded: $showBrief) {
                                Text(goesToSource ? starter.relay : starter.context)
                                    .font(.caption2).foregroundStyle(.secondary)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                            }.font(.caption)
                        } else if error == nil {
                            ProgressView()
                        }
                        if let error { ErrorBanner(message: error) }
                    }.padding()
                }
                Divider()
                Composer(draft: draft, attachments: attachments,
                         placeholder: reply ? "Your note — it goes to the session" : request.accept ? "Anything it should know? (or just send)" : "Why not? (or just send)",
                         sending: busy, send: send, allowEmpty: !reply)
                    .padding(10)
            }
            .navigationTitle(reply ? "Reply" : request.accept ? "Accept" : "Decline")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not now") { Task { await onDecided(nil) }; dismiss() }
                }
            }
            .task {
                do { starter = try await hub.recStarter(rec.id) }
                catch { if !error.isCancellation { self.error = error.localizedDescription } }
            }        }.presentationDetents([.medium, .large])
    }

    private func send() {
        busy = true
        let typed = draft.text.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            do {
                // One call: the hub records the decision and hands it to the
                // agent, so the phone and the console cannot drift apart on
                // what an accepted rec actually sends.
                // A screenshot goes up first and rides IN the decision — the
                // same message as the note, not one after it. Filed against the source session when that
                // is where it is going.
                let refs = try await attachments.upload(to: hub, threadID: goesToSource ? starter?.source_session : nil)
                let deliver = goesToSource ? "source" : "new"
                // No timing fields: a rec answer goes now.
                let out: RecDecision
                if reply {
                    // The third answer: same note, same destination, no
                    // verdict — the hub refuses an empty one, since a reply
                    // that reaches nobody is not a reply.
                    guard !typed.isEmpty || !refs.isEmpty else { error = "Type the note first — a reply with nothing in it goes nowhere."; busy = false; return }
                    out = try await hub.replyRec(rec.id, note: typed, deliver: deliver, attachments: refs)
                } else {
                    out = try await hub.decideRec(rec.id, status: request.status, note: typed,
                                                  deliver: deliver, until: "", attachments: refs)
                }
                guard let session = out.session_id else {
                    error = out.delivery_error ?? "the decision was saved, but no session got it"
                    busy = false
                    return
                }
                let thread = try await hub.thread(session)
                let ids = attachments.assetIDs
                dismiss()
                await PhotoCleanup.offerDeletingScreenshots(ids)
                await onDecided(thread)
            } catch {
                self.error = error.localizedDescription
                busy = false
            }
        }
    }
}

/// One rec's page: the whole record, including how it turned out.
struct RecDetail: View {
    @Environment(HubClient.self) private var hub
    @State var rec: Rec
    /// Reload the list behind us — a decision here changes which filter it belongs to.
    let onChange: () async -> Void
    @State private var scoreNote = ""
    @State private var error: String?
    @State private var deciding: RecDecisionRequest?
    @State private var landed: Thread?
    @State private var sheetUp = false
    @State private var openThread: Thread?
    @State private var openingSource = false

    var body: some View {
        List {
            if let error { Section { ErrorBanner(message: error) } }
            Section {
                HStack {
                    RecChip(text: rec.domain, color: recDomainColor(rec.domain))
                    RecChip(text: rec.kind, color: .secondary)
                    if let m = rec.modelShort { RecChip(text: m, color: .indigo) }
                    RecChip(text: rec.status, color: recStatusColor(rec.status))
                    if let o = rec.outcome, !o.isEmpty { RecChip(text: o, color: recOutcomeColor(o)) }
                    if rec.thread_running == true { RecRunning() }
                    Spacer()
                    Text(rec.costLabel).font(.callout.weight(.medium)).monospacedDigit()
                }
                Text("\(rec.effort) effort · \(rec.confidence)% confident when filed" + (recDates(rec).isEmpty ? "" : " · \(recDates(rec))"))
                    .font(.caption).foregroundStyle(.secondary)
                // The chat that filed it, one tap away, on both mobile and
                // web. The decision box lands the owner there too,
                // but reading the reasoning should not cost a decision.
                if rec.sourceThreadID != nil {
                    Button { Task { await openSource() } } label: {
                        Label(openingSource ? "Opening…" : "Open the chat that filed this", systemImage: "bubble.left.and.bubble.right")
                    }.disabled(openingSource)
                }
            }
            if let d = rec.detail, !d.isEmpty { Section("What it is") { StyledText(text: d) } }
            if let b = rec.because, !b.isEmpty { Section("Why — the evidence behind it") { StyledText(text: b) } }
            if let e = rec.expect, !e.isEmpty { Section("What should change if it works") { StyledText(text: e) } }

            if !rec.isClosed {
                // The console's row, in the console's order: Accept · Decline ·
                // Reply. One box behind all three.
                Section {
                    Button("Accept") { deciding = RecDecisionRequest(rec: rec, way: .accept) }
                    Button("Decline", role: .destructive) { deciding = RecDecisionRequest(rec: rec, way: .decline) }
                    Button("Reply") { deciding = RecDecisionRequest(rec: rec, way: .reply) }
                }
            }
            if !rec.isOpen {
                Section("Decision") {
                    Text((rec.status == "deferred" ? "later — back on \(dayLabel(rec.review_on ?? ""))" : rec.status)
                         + (rec.decided_at.map { " · " + $0.formatted(date: .abbreviated, time: .shortened) } ?? ""))
                        .font(.callout.weight(.medium))
                    if let n = rec.decision_note, !n.isEmpty { StyledText(text: n) }
                }
            }

            Section("Outcome") {
                if let o = rec.outcome, !o.isEmpty {
                    Text("\(o)\(rec.outcome_at.map { " · " + $0.formatted(date: .abbreviated, time: .shortened) } ?? "")")
                        .font(.callout.weight(.medium))
                    if let n = rec.outcome_note, !n.isEmpty { StyledText(text: n) }
                } else if rec.status == "accepted" || rec.status == "done" {
                    TextField("What actually happened (optional)", text: $scoreNote, axis: .vertical).lineLimit(1...5)
                    ForEach([("worked", "It worked"), ("mixed", "Mixed"), ("failed", "It failed"), ("unclear", "Too early / unclear")], id: \.0) { o, label in
                        Button(label) { Task { await score(o) } }
                    }
                } else {
                    Text("Not scored yet" + (rec.review_on.map { " — due \(dayLabel($0))" } ?? "")).foregroundStyle(.secondary)
                }
            }

            if let links = rec.links, !links.isEmpty {
                Section(rec.status == "deferred" ? "What deferring it minted" : "What accepting it minted") {
                    Text(links.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.joined(separator: " · "))
                        .font(.footnote.monospaced()).foregroundStyle(.secondary)
                }
            }
            Section {
                Text("\(rec.id) · filed \(rec.created_at.formatted(date: .abbreviated, time: .shortened)) by \(rec.source)"
                     + (rec.model.map { " · \($0)" } ?? ""))
                    .font(.caption2).foregroundStyle(.tertiary)
            }
        }
        .navigationTitle(mdPlain(rec.title))
        .navigationBarTitleDisplayMode(.inline)
        .askButton()
        .task { await reload() }
        .refreshable { await reload() }
        .sheet(item: $deciding, onDismiss: {
            sheetUp = false
            if let t = landed { landed = nil; openThread = t }
        }) { req in
            RecDecisionSheet(request: req) { thread in
                if sheetUp { landed = thread } else { openThread = thread }
                deciding = nil
                await reload()
                await onChange()
            }
            .onAppear { sheetUp = true }
        }
        .navigationDestination(item: $openThread) { ThreadDetail(thread: $0) }
    }

    private func reload() async {
        do { rec = try await hub.rec(rec.id); error = nil }
        catch { if !error.isCancellation { self.error = error.localizedDescription } }
    }

    private func openSource() async {
        guard let id = rec.sourceThreadID else { return }
        openingSource = true; defer { openingSource = false }
        do { openThread = try await hub.thread(id); error = nil }
        catch { self.error = error.localizedDescription }
    }

    private func score(_ outcome: String) async {
        do { rec = try await hub.scoreRec(rec.id, outcome: outcome, note: scoreNote.trimmingCharacters(in: .whitespacesAndNewlines)); scoreNote = ""; error = nil }
        catch { self.error = error.localizedDescription }
        await onChange()
    }
}
