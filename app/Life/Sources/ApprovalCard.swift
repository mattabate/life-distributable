import SwiftUI

/// One proposal, drawn inside the session that proposed it — and, for a dated
/// one, in the calendar item's sheet. There is no Actions tab: past actions
/// are noise; only what's waiting on the owner matters.
/// History is still available via `lifectl actions`.
///
/// The card IS the shared `Card` (Card.swift): full width, the same styling
/// as every other cell, buttons on the bottom that arm a message. Its row is
/// the hub's words (`outcomes`: Approve · Deny · Reply, the last only
/// with a session to talk to) and Dismiss. Approve/Deny/Reply ARM the
/// session's chat bar (`arm`), exactly as a rec's do: the owner's note is typed
/// where a message is, Send posts one prompt naming the action with their pick,
/// Face ID unlocks the decider code for it (ThreadDetail.send), and the hub
/// decides the row from that prompt.
///
/// Dismiss (for a card gone out of date) is the silent close: the row moves to `dismissed`, nothing is
/// relayed, no decider code, and the card folds where it was with Reopen
/// inside — the ask card's fold.
///
/// Two places: `inThread` inside the session itself, under the reply that
/// proposed it, where the buttons arm the bar in place; and inside a calendar
/// item's sheet, where a button opens the session ON this card with that pick
/// already armed (the composer is there, not on the calendar).
///
/// A proposal from a scheduled job (`source` claude:job:<name>, no session)
/// has no chat bar to arm: its Approve/Deny decide it from the card, and the
/// card opens the job's RUN (`JobRunView`). Approving it starts a session
/// (hub `OnDecided`) and the card hands off to it.
///
/// A decided one (reached from a calendar row or an old link) is the same
/// card, grey, with its state where the buttons were; the audit trail below
/// says who decided it and when.
struct ApprovalCard: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.armedPicks) private var armedPicks
    let a: Action
    /// Drawn inside its own session's chat: the buttons arm that chat bar.
    var inThread = false
    /// How the board opens a session (the board's own navigation, so the
    /// pushed page is the same one an ask card opens, scrolled to this card).
    var open: ((OpenAsk) -> Void)? = nil
    /// Arm the chat bar with this card and a pick ("approved" | "denied" |
    /// "" for words alone) — ThreadDetail's `arm`. nil outside a session.
    var arm: ((String) -> Void)? = nil
    let onDecided: () async -> Void
    @State private var error: String?
    @State private var busy = false
    @State private var openThread: Thread?
    /// The pick to arm in the session this card opens (the calendar copy).
    @State private var armOnOpen: ArmedReply?
    @State private var payloadShown = false
    /// The audit trail (Phase 2b): `GET /actions/{id}` is the one read that
    /// carries events[], so only the card drawn in its own session reads it —
    /// one request per card — and the calendar's copy came with them already.
    @State private var events: [ActionEvent]?
    /// The job run the card opens on (a scheduled job's proposal).
    @State private var openRun: RunRef?

    /// Where it stands is the hub's (store.ActionStanding).
    private var isOpen: Bool { a.open ?? false }
    private var dismissed: Bool { a.folded == "dismissed" }

    var body: some View {
        Card(tint: .red,
             mark: isOpen ? "hand.raised.fill" : (a.state == "denied" || a.state == "failed") ? "xmark" : "checkmark",
             caption: isOpen ? "approval" : a.state, right: shortAgo(a.created_at),
             title: a.title, text: a.detail, said: a.said ?? "", thread: a.thread_id ?? "",
             extra: payload, meta: AnyView(metaRow),
             closed: !isOpen && !dismissed, line: decidedLine,
             folded: dismissed ? "dismissed" : nil,
             buttons: buttons, trail: trail, busy: busy, error: error)
        .task(id: a.id) {
            if let e = a.events { events = e; return }
            guard inThread else { return }
            events = try? await hub.action(a.id).events
        }
        .contextMenu {
            if isOpen, a.thread_id != nil { Button { pick("") } label: { Label("Reply", systemImage: "arrowshape.turn.up.left") } }
            Button { UIPasteboard.general.string = a.id } label: { Label("Copy id", systemImage: "doc.on.doc") }
        }
        .navigationDestination(item: $openThread) { ThreadDetail(thread: $0, focusCard: a.id, armOnOpen: armOnOpen) }
        .sheet(item: $openRun) { JobRunView(runID: $0.id, job: a.job ?? "") }
    }

    /// The row: the hub's words, each arming the bar (a job's proposal, with
    /// no session, decides on the spot), then Dismiss. A folded one: Reopen.
    private var buttons: [CardButton] {
        guard isOpen else { return a.reopen == true ? [fold(back: true)] : [] }
        return outcomeButtons(a.outcomes ?? [], armed: armedPicks["action:" + a.id]) { pick($0) } + [fold(back: false)]
    }

    /// Dismiss / Reopen: a plain move, no code, no relay.
    private func fold(back: Bool) -> CardButton {
        foldButton(.action, a.id, back: back, hub: hub, busy: $busy, error: $error, reload: onDecided)
    }

    /// What it will run, folded: an approval is read before it is given.
    private var payload: AnyView? {
        guard isOpen, let p = a.exec_payload, !p.text.isEmpty, !a.exec_type.isEmpty, a.exec_type != "none" else { return nil }
        return AnyView(DisclosureGroup(isExpanded: $payloadShown) {
            Text(p.text).font(.caption.monospaced()).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(8).background(Color.secondary.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
        } label: {
            Text("will run: \(a.exec_type)").font(.caption).foregroundStyle(.secondary)
        })
    }

    /// The kind capsule, the project, who proposed it (the job's chip opens
    /// its run) and — outside the session — the session it belongs to.
    private var metaRow: some View {
        HStack(spacing: 6) {
            Chip(a.kind, tint: kindColor, style: .kind)
            if !a.project.isEmpty { Text(a.project).font(.caption).foregroundStyle(.secondary) }
            if let job = a.job {
                Button { openRun = a.run_id.map { RunRef(id: $0) } } label: {
                    Label("\(job) job", systemImage: "gearshape.2").font(.caption)
                }.buttonStyle(.plain).foregroundStyle(.secondary).disabled(a.run_id == nil)
            }
            Spacer()
            // Inside a session the session IS the screen; the calendar's
            // copy of the card is the one that has to say whose it is.
            if let tid = a.thread_id, !inThread {
                Button { Task { await openSession(tid) } } label: {
                    Label(tid, systemImage: "bubble.left.and.bubble.right").font(.caption).lineLimit(1)
                }.buttonStyle(.plain).foregroundStyle(.tint)
            }
        }
    }

    private var trail: AnyView? {
        guard let events, !events.isEmpty else { return nil }
        return AnyView(VStack(alignment: .leading, spacing: 2) {
            ForEach(events) { e in Text(e.line).font(.caption2).foregroundStyle(.secondary).lineLimit(2) }
        })
    }

    private var decidedLine: String {
        var s = a.state
        if let by = a.decided_via, !by.isEmpty { s += " by \(by)" }
        if let at = a.decided_at { s += " · \(shortAgo(at))" }
        return s
    }

    var kindColor: Color {
        switch a.kind { case "money": .orange; case "delete": .red; case "contact", "share", "commit": .purple; default: .secondary }
    }

    /// One of the word buttons: arm the bar here, open the session with the
    /// pick armed, or (a job's proposal) decide on the spot.
    func pick(_ outcome: String) {
        if let arm { arm(outcome); return }
        if let tid = a.thread_id {
            armOnOpen = ArmedReply(kind: .action, id: a.id, title: a.title, outcome: outcome, outcomes: a.outcomes ?? [])
            Task { await openSession(tid) }
            return
        }
        Task { await decide(outcome == "approved") }
    }

    /// Open the proposing session ON this card (ThreadDetail.focusCard) —
    /// through the board's navigation when it gave us one, else our own.
    func openSession(_ tid: String) async {
        do {
            let t = try await hub.thread(tid)
            if let open, armOnOpen == nil { open(OpenAsk(thread: t, message: nil, card: a.id)) } else { openThread = t }
        } catch { self.error = error.localizedDescription }
    }

    /// A job's proposal, decided from the card (there is no session yet; an
    /// approval starts one and the card hands off to it).
    func decide(_ approve: Bool) async {
        busy = true; defer { busy = false }
        do {
            let decided = approve ? try await hub.approve(a.id) : try await hub.deny(a.id)
            error = nil
            if let tid = decided.thread_id { await openSession(tid) }
        } catch {
            self.error = HubClient.deciderHint(error)
        }
        await onDecided()
    }
}

/// A run id as a sheet item.
struct RunRef: Identifiable, Hashable { let id: String }

/// One scheduled-job run, read like the chat it stands in for: what the job
/// said, its summary, every finding it filed, what only the owner can do.
/// Presented as a sheet from an approval card (the job's proposal).
struct JobRunView: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    let runID: String
    let job: String
    @State private var load = HubLoad<JobRun>()

    var body: some View {
        NavigationStack {
            List {
                HubLoadStatus(load: load)
                if let r = load.value {
                    Section {
                        HStack(spacing: 8) {
                            Image(systemName: "gearshape.2").foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("\(r.job) job · run \(r.id)").font(.headline)
                                Text(meta(r)).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            StateBadge(state: r.ok == false ? "failed" : r.ok == true ? "done" : "running")
                        }
                        if let e = r.error, !e.isEmpty { Text(e).font(.caption).foregroundStyle(.red) }
                    }
                    if let t = r.text, !t.isEmpty {
                        Section("What it said") { StyledText(text: t, style: .body) }
                    }
                    if let s = r.summary, !s.isEmpty {
                        Section("Summary") { StyledText(text: s, style: .body) }
                    }
                    if !r.findings.isEmpty {
                        Section("Findings · \(r.findings.count)") {
                            ForEach(Array(r.findings.enumerated()), id: \.offset) { _, f in
                                VStack(alignment: .leading, spacing: 4) {
                                    HStack(alignment: .firstTextBaseline) {
                                        Text(md(f.title)).font(.body.weight(.semibold))
                                        Spacer()
                                        Chip(f.kind)
                                    }
                                    StyledText(text: f.detail, style: .subheadline, color: .secondary)
                                }.padding(.vertical, 2)
                            }
                        }
                    }
                    if !r.needs_you.isEmpty {
                        Section("Only you can") { ForEach(r.needs_you, id: \.self) { Text(md($0)) } }
                    }
                    if let o = r.output, !o.isEmpty {
                        Section {
                            DisclosureGroup("Raw output") {
                                Text(o).font(.caption.monospaced()).textSelection(.enabled)
                            }
                        }
                    }
                }
            }
            .navigationTitle(job.isEmpty ? "Job run" : "\(job) run")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Close") { dismiss() } } }
            .hubTask(load, id: runID) { try await hub.run(runID) }
        }
    }

    private func meta(_ r: JobRun) -> String {
        var parts = [r.started_at.formatted(date: .abbreviated, time: .shortened)]
        if let f = r.finished_at { parts.append("took \(max(1, Int(f.timeIntervalSince(r.started_at))))s") }
        parts.append(usd(r.cost_usd))
        return parts.joined(separator: " · ")
    }
}

struct StateBadge: View {
    let state: String
    var body: some View { Chip(state, tint: color, style: .tag) }
    var color: Color {
        switch state { case "proposed": .red; case "approved", "running": .blue; case "done": .green; case "denied": .gray; case "failed": .orange; default: .secondary }
    }
}
