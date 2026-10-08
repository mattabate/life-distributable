import SwiftUI

/// Recs: the list the owner browses when they are in the mood to spend
/// money, pick a tool or change a habit.
///
/// It must never PUSH. That is the whole distinction the object exists to
/// draw: an ask is push — their turn, now — and a rec is pull, welcome only
/// in the moment they came looking. Nothing in this file may ever touch
/// PushState or a notification.
///
/// Its tab carries a count, the console's oval: a number seen only once the
/// app is already open is still pull — it is what is sitting there
/// undecided, not a tap on the shoulder.
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
        #if targetEnvironment(macCatalyst)
        // The desktop is the console's page, not the phone's list (the owner
        // 2026-09-29: "I want the same exact layout within the desktop app"):
        // the heading, every open rec a white card with its chat bar, the
        // All recommendations line last.
        pageModifiers(macPage.toolbar(.hidden, for: .navigationBar))
        #else
        pageModifiers(phoneList)
        #endif
    }

    #if targetEnvironment(macCatalyst)
    /// The heading, every open rec a card ending in its chat bar, the All
    /// recommendations line last, in a column with room either side. No
    /// domain select: the open list is meant to stay short enough that
    /// sorting it buys nothing.
    private var macPage: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                Text("Recommendations").font(.system(size: 17, weight: .semibold))
                if let e = list.error { ErrorBanner(message: e) }
                if recs.isEmpty {
                    Group {
                        if list.loaded { Text("Nothing open.").foregroundStyle(Web.muted) } else { ProgressView() }
                    }.frame(maxWidth: .infinity).padding(.vertical, 30)
                }
                ForEach(recs) { r in
                    MacRecCard(rec: r, open: { openRec = r }, done: { await load() })
                        .id(r.id)
                }
                if !every.isEmpty {
                    Button { showAll = true } label: {
                        HStack {
                            Text("All recommendations").font(.system(size: 13.5, weight: .medium))
                            Spacer()
                            Text("\(every.count)").foregroundStyle(Web.muted)
                        }
                        .padding(.horizontal, 14).padding(.vertical, 10)
                        .background(Web.panel, in: RoundedRectangle(cornerRadius: 10))
                        .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Web.line, lineWidth: 1) }
                        .contentShape(Rectangle())
                    }.buttonStyle(.plain).padding(.top, 2)
                }
            }
            .frame(maxWidth: MacRecLayout.column, alignment: .leading)
            .padding(.horizontal, 32).padding(.vertical, 24)
            .frame(maxWidth: .infinity)
        }
    }
    #endif

    private var phoneList: some View {
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
    }

    private func pageModifiers(_ page: some View) -> some View {
        page
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
    #if targetEnvironment(macCatalyst)
    /// The console's pager: twenty rows, then Show 20 more / Show all.
    @State private var limit = 20
    @State private var openRec: Rec?

    /// The console's #/recs/all (the owner 2026-09-29: the desktop is the web's
    /// layout): ‹ Recommendations, the heading with its count, the find box on
    /// its right, then ONE card holding a table —
    /// Recommendation · Domain · where it ended up · When · Cost.
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 6) {
                Button("‹ Recommendations") { dismiss() }.buttonStyle(.plain)
                    .font(.system(size: 12.5)).foregroundStyle(Web.accent)
                HStack(spacing: 8) {
                    (Text("All recommendations ") + Text("· \(recs.count)").foregroundColor(Web.muted))
                        .font(.system(size: 17, weight: .semibold))
                    Spacer()
                    TextField("Find a rec…", text: $query).textFieldStyle(.roundedBorder).frame(maxWidth: 240)
                        .onChange(of: query) { limit = 20 }
                }.padding(.bottom, 6)
                VStack(alignment: .leading, spacing: 0) {
                    if shown.isEmpty {
                        Text("No rec by that name.").foregroundStyle(Web.muted).frame(maxWidth: .infinity).padding(.vertical, 30)
                    } else {
                        table
                        pager
                    }
                }.webCard(padding: 6)
            }.padding(16)
        }
        .toolbar(.hidden, for: .navigationBar)
        .navigationDestination(item: $openRec) { RecDetail(rec: $0, onChange: onChange) }
    }
    @Environment(\.dismiss) private var dismiss

    private var table: some View {
        Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 16, verticalSpacing: 0) {
            GridRow {
                Text("Recommendation"); Text("Domain"); Text(""); Text("When")
                Text("Cost").gridColumnAlignment(.trailing)
            }.font(.system(size: 12)).foregroundStyle(Web.muted).padding(.vertical, 9)
            ForEach(shown.prefix(limit)) { r in
                Divider().overlay(Web.line)
                GridRow {
                    Text(md(r.title)).font(.system(size: 14, weight: r.isOpen ? .semibold : .regular))
                        .frame(maxWidth: .infinity, alignment: .leading)
                    WebTag(r.domain, tint: MacRecPill.domain(r.domain))
                    HStack(spacing: 5) {
                        WebTag(r.status == "deferred" ? "later" : r.status == "proposed" ? "open" : r.status, tint: MacRecPill.status(r.status))
                        if let o = r.outcome, !o.isEmpty { WebTag(o, tint: MacRecPill.outcome(o)) }
                    }
                    Text(shortAgo(r.decided_at ?? r.created_at)).font(.system(size: 12.5)).foregroundStyle(Web.muted)
                    Text(r.costLabel).font(.system(size: 12.5)).monospacedDigit()
                }
                .padding(.vertical, 9).contentShape(Rectangle())
                .onTapGesture { openRec = r }
            }
        }.padding(.horizontal, 10)
    }

    @ViewBuilder private var pager: some View {
        let total = shown.count
        if total > limit {
            HStack(spacing: 8) {
                Button("Show \(min(20, total - limit)) more") { limit += 20 }.buttonStyle(WebButtonStyle(small: true))
                Button("Show all \(total)") { limit = .max }.buttonStyle(WebButtonStyle(small: true))
                Text("\(limit) of \(total)").font(.system(size: 12.5)).foregroundStyle(Web.muted)
            }.padding(10)
        } else if total > 20 {
            Text("All \(total) shown.").font(.system(size: 12.5)).foregroundStyle(Web.muted).padding(10)
        }
    }
    #else
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
    #endif
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
        #if targetEnvironment(macCatalyst)
        detailModifiers(macPage.toolbar(.hidden, for: .navigationBar))
        #else
        detailModifiers(phoneList)
        #endif
    }

    #if targetEnvironment(macCatalyst)
    @Environment(\.dismiss) private var dismiss
    @Environment(RefNav.self) private var nav

    /// One rec, read top to bottom: ‹ back, the title and its cost, one muted
    /// line of facts, the chat bar (chips over the composer) right under it so
    /// answering is the first thing on the page, then the record under plain
    /// headings (The recommendation · Why · If it works · Decision · Outcome)
    /// as text in a column with room either side, no boxes: a run of boxed
    /// sections read as dense.
    private var macPage: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Button("‹ Recommendations") { dismiss() }.buttonStyle(.plain)
                    .font(.system(size: 12.5)).foregroundStyle(Web.accent)
                HStack(alignment: .firstTextBaseline) {
                    Text(md(rec.title)).font(.system(size: 17, weight: .semibold)).textSelection(.enabled)
                    Spacer(minLength: 12)
                    Text(rec.costLabel).font(.system(size: 12.5)).monospacedDigit()
                }.padding(.top, 10).padding(.bottom, 8)
                FlowRow(spacing: 8) {
                    MacRecPills(rec: rec, status: true)
                    if !recDates(rec).isEmpty { Text(recDates(rec)).foregroundStyle(recDatesColor(rec) ?? Web.muted) }
                    if let g = rec.goal_id, !g.isEmpty { Text(g).foregroundStyle(Web.accent) }
                    if rec.sourceThreadID != nil {
                        Button(openingSource ? "Opening…" : "its chat ›") { Task { await openSource() } }
                            .buttonStyle(.plain).foregroundStyle(Web.accent).disabled(openingSource)
                    }
                }.font(.system(size: 12.5)).foregroundStyle(Web.muted)
                if let error { ErrorBanner(message: error).padding(.top, 8) }
                if !rec.isClosed {
                    MacRecReplyBox(rec: rec) { await reload(); await onChange() }
                        .id(rec.id + rec.status)
                        .padding(.top, 18)
                }
                macField("The recommendation", rec.detail)
                macField("Why", rec.because)
                macField("If it works", rec.expect)
                // Undecided says nothing the chat bar above does not.
                if rec.status != "proposed" {
                    macSection("Decision") {
                        (Text(rec.status == "deferred" ? "later — back on \(dayLabel(rec.review_on ?? ""))" : rec.status).bold()
                         + Text((rec.decided_at.map { " · " + $0.formatted(date: .abbreviated, time: .shortened) } ?? "")
                                + (rec.decided_by.map { $0.isEmpty ? "" : " · by \($0)" } ?? "")).foregroundColor(Web.muted))
                        if let n = rec.decision_note, !n.isEmpty { StyledText(text: n, style: .body).padding(.top, 6) }
                    }
                }
                // Unscored and undecided: the review day is on the facts line.
                if rec.outcome?.isEmpty == false || rec.status == "accepted" || rec.status == "done" {
                macSection("Outcome") {
                    if let o = rec.outcome, !o.isEmpty {
                        Text(o).bold() + Text(rec.outcome_at.map { " · " + $0.formatted(date: .abbreviated, time: .shortened) } ?? "").foregroundColor(Web.muted)
                        if let n = rec.outcome_note, !n.isEmpty { StyledText(text: n, style: .body).padding(.top, 6) }
                    } else {
                        Text("Not scored yet" + (rec.review_on.map { " — due \(dayLabel($0))" } ?? "")).foregroundStyle(Web.muted)
                    }
                    if rec.status == "accepted" || rec.status == "done" {
                        HStack(spacing: 8) {
                            ForEach([("worked", "It worked"), ("mixed", "Mixed"), ("failed", "It failed"), ("unclear", "Too early / unclear")], id: \.0) { o, label in
                                Button(label) { Task { await score(o) } }.buttonStyle(WebButtonStyle(small: true))
                            }
                        }.padding(.top, 8)
                        TextField("What actually happened (optional)", text: $scoreNote, axis: .vertical)
                            .lineLimit(1...5).textFieldStyle(.roundedBorder).padding(.top, 8)
                    }
                }
                }
                if let links = rec.links, !links.isEmpty {
                    macSection(rec.status == "deferred" ? "What deferring it minted" : "What accepting it minted") {
                        Text(links.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.joined(separator: " · "))
                            .font(.system(size: 12.5).monospaced()).textSelection(.enabled)
                    }
                }
                if let prev = rec.prev_id, !prev.isEmpty {
                    Text("Supersedes \(prev).").font(.system(size: 12.5)).foregroundStyle(Web.muted).padding(.top, 4)
                }
                Text("\(rec.id) · filed \(rec.created_at.formatted(date: .abbreviated, time: .shortened)) by \(rec.source.isEmpty ? "unknown" : rec.source)"
                     + (rec.model.map { " · \($0)" } ?? ""))
                    .font(.system(size: 12.5)).foregroundStyle(Web.muted).textSelection(.enabled).padding(.top, 28)
            }
            .frame(maxWidth: MacRecLayout.column, alignment: .leading)
            .padding(.horizontal, 32).padding(.vertical, 24)
            .frame(maxWidth: .infinity)
        }
    }

    /// One part of the record as text under its heading, nothing when empty.
    @ViewBuilder private func macField(_ label: String, _ body: String?) -> some View {
        if let body, !body.isEmpty { macSection(label) { StyledText(text: body, style: .body).lineSpacing(3) } }
    }

    /// A plain heading over plain text: the page's sections are paragraphs,
    /// not white boxes.
    private func macSection<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.system(size: 13.5, weight: .semibold))
            VStack(alignment: .leading, spacing: 2) { content() }.font(.system(size: 13.5))
        }.padding(.top, 26)
    }
    #endif

    private var phoneList: some View {
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
    }

    private func detailModifiers(_ page: some View) -> some View {
        page
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
        do {
            let t = try await hub.thread(id)
            #if targetEnvironment(macCatalyst)
            macOpenSession(t, nav: nav)
            #else
            openThread = t
            #endif
            error = nil
        }
        catch { self.error = error.localizedDescription }
    }

    private func score(_ outcome: String) async {
        do { rec = try await hub.scoreRec(rec.id, outcome: outcome, note: scoreNote.trimmingCharacters(in: .whitespacesAndNewlines)); scoreNote = ""; error = nil }
        catch { self.error = error.localizedDescription }
        await onChange()
    }
}

#if targetEnvironment(macCatalyst)
// MARK: - The desktop's Recs: the console's cards (the owner 2026-09-29: "I spent a
// lot of time on this web UI layout. I want the same exact layout within the
// desktop app.")

/// The console's pill colours for a rec (ui.js DOM_PILL, recs.js STATUS_PILL
/// and OUTCOME_PILL): amber, green (`done`), the accent blue (`purple` and
/// `running` are both the accent), red (`needs`), nil = the grey pill.
enum MacRecPill {
    static let amber = Color(red: 0xd9 / 255, green: 0x77 / 255, blue: 0x06 / 255)
    static let green = Color(red: 0x05 / 255, green: 0x96 / 255, blue: 0x69 / 255)
    static let red = Color(red: 0xdc / 255, green: 0x26 / 255, blue: 0x26 / 255)
    static func domain(_ d: String) -> Color? {
        switch d { case "money": return amber; case "health": return green; case "audience", "tools": return Web.accent; default: return nil }
    }
    static func status(_ s: String) -> Color? {
        switch s { case "proposed": return amber; case "deferred", "accepted": return Web.accent; case "done": return green; default: return nil }
    }
    static func outcome(_ o: String) -> Color? {
        switch o { case "worked": return green; case "mixed": return amber; case "failed": return red; default: return nil }
    }
}

/// The pill run of a rec, the console's order: kind, model, where it
/// stands (the list skips "proposed" — every card on it is), how it turned
/// out, and "running" while its session has a turn in flight.
struct MacRecPills: View {
    let rec: Rec
    /// Draw the status even when proposed (the rec's own page does).
    var status = false
    var body: some View {
        WebTag(rec.kind)
        if let m = rec.modelShort { WebTag(m) }
        if status || rec.status != "proposed" {
            WebTag(rec.status == "deferred" && !status ? "later" : rec.status, tint: MacRecPill.status(rec.status))
        }
        if let o = rec.outcome, !o.isEmpty { WebTag(o, tint: MacRecPill.outcome(o)) }
        if rec.thread_running == true {
            HStack(spacing: 6) { LiveDot(color: Web.accent); Text("running") }
                .font(.system(size: 12, weight: .semibold)).foregroundStyle(Web.accent)
                .padding(.horizontal, 8).padding(.vertical, 2)
                .background(Web.accent.opacity(0.15), in: RoundedRectangle(cornerRadius: 6))
                .help("its session is working right now — nothing to follow up on")
        }
    }
}

/// The Recs pages' column: room either side of the cards and the text, so a
/// wide window does not stretch a line edge to edge.
enum MacRecLayout { static let column: CGFloat = 860 }

/// One open rec: bold title and its cost, the pill line with "its chat ›"
/// and "details ›" at its end, why in two lines, then its chat bar
/// (MacRecReplyBox). A decided rec's note sits where the bar would.
struct MacRecCard: View {
    @Environment(HubClient.self) private var hub
    @Environment(RefNav.self) private var nav
    let rec: Rec
    let open: () -> Void
    let done: () async -> Void
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline) {
                    Button(action: open) {
                        Text(md(rec.title)).font(.system(size: 15.5, weight: .semibold)).multilineTextAlignment(.leading)
                    }.buttonStyle(.plain)
                    Spacer(minLength: 10)
                    Text(rec.costLabel).font(.system(size: 12.5)).monospacedDigit().lineLimit(1)
                }
                HStack(alignment: .center, spacing: 8) {
                    FlowRow(spacing: 8) {
                        MacRecPills(rec: rec)
                        if !recDates(rec).isEmpty { Text(recDates(rec)).foregroundStyle(recDatesColor(rec) ?? Web.muted) }
                    }
                    Spacer(minLength: 8)
                    if rec.sourceThreadID != nil {
                        Button("its chat ›") { Task { await chat() } }.buttonStyle(.plain).foregroundStyle(Web.accent)
                            .help("The session that filed it")
                    }
                    Button("details ›", action: open).buttonStyle(.plain).foregroundStyle(Web.accent)
                }.font(.system(size: 12.5)).foregroundStyle(Web.muted).padding(.top, 8)
                if let why = rec.because ?? rec.detail, !why.isEmpty {
                    Text(md(why)).font(.system(size: 12.5)).foregroundStyle(Web.muted).lineLimit(2).padding(.top, 8)
                }
                if let error { Text(error).font(.system(size: 12.5)).foregroundStyle(.red).padding(.top, 6) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if rec.status != "proposed", let n = rec.decision_note, !n.isEmpty {
                (Text("your note: ").foregroundColor(Web.muted) + Text(md(n)))
                    .font(.system(size: 12.5)).padding(.top, 12)
            }
            if !rec.isClosed {
                MacRecReplyBox(rec: rec) { await done() }.padding(.top, 14)
            }
        }
        .padding(.horizontal, 18).padding(.top, 15).padding(.bottom, 16)
        .background(Web.panel)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Web.line, lineWidth: 1) }
    }

    private func chat() async {
        guard let id = rec.sourceThreadID else { return }
        do { macOpenSession(try await hub.thread(id), nav: nav); error = nil } catch { self.error = error.localizedDescription }
    }
}

/// "its chat ›" leaves Recs for the Sessions page with that session open, as
/// the calendar's "open session" does, never a chat pushed inside Recs (a
/// second copy of the Sessions page with no sidebar).
@MainActor func macOpenSession(_ t: Thread, nav: RefNav) {
    nav.card = OpenAsk(thread: t, message: nil)
    nav.tab = "sessions"
}

/// A rec's chat bar, the calendar step's shape: Accept · Decline · Reply as
/// chips, then the session composer (+ and Send in it), as on every other
/// card. Send with Accept or Decline lit decides the rec (words optional);
/// Reply needs words and leaves it open. Either way the answer goes to the
/// session that filed it, a new one when none did.
struct MacRecReplyBox: View {
    let rec: Rec
    let sent: () async -> Void
    @State private var model: RespondModel
    init(rec: Rec, sent: @escaping () async -> Void) {
        self.rec = rec
        self.sent = sent
        _model = State(initialValue: RespondModel(RespondSubject(rec: rec)))
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            RespondChips(model: model)
            if let e = model.error { ErrorBanner(message: e) }
            RespondBar(model: model) {
                // A Reply leaves the rec open: a fresh box, not a spent one.
                model = RespondModel(model.subject)
                await sent()
            }
            .background(Web.panel, in: RoundedRectangle(cornerRadius: 10))
            .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Web.line, lineWidth: 1) }
        }
    }
}
#endif
