import SwiftUI

// The goals are the system's long-term memory: sessions create them, write the
// notes and rewrite the digest. The owner reads here. The one thing they still
// edit by hand is the name and the statement the sessions steer by, and whether
// the goal is active. The console's goals.js draws the same three controls.
//
// The list is one row per goal — its emblem and the name, no subtitles or
// numbers. The emblem is the hub's `emblem` — a
// symbol in the goal's own hue, the same one the console's tiles wear. The
// statement and the note count stay off the list.

extension GoalEmblem {
    /// The hub's hue as a colour that reads on both light and dark; the
    /// console's `hsl(h 60% 42%)`.
    var color: Color { Color(hue: Double(hue % 360) / 360, saturation: 0.6, brightness: 0.62) }
}

/// A goal's emblem: the symbol on a pale disc of the goal's hue. An inactive
/// goal wears it grey.
struct GoalEmblemView: View {
    let emblem: GoalEmblem
    var on = true
    var size: CGFloat = 36

    var body: some View {
        let tint = on ? emblem.color : Color.secondary
        Image(systemName: emblem.systemImage)
            .font(.system(size: size * 0.42, weight: .semibold))
            .foregroundStyle(tint)
            .frame(width: size, height: size)
            .background(tint.opacity(on ? 0.14 : 0.10), in: Circle())
    }
}

struct GoalsView: View {
    @Environment(HubClient.self) private var hub
    @State private var load = HubLoad<[Goal]>()
    private var goals: [Goal] { load.value ?? [] }

    var body: some View {
        #if targetEnvironment(macCatalyst)
        // The console's tile grid on the desktop (the owner 2026-09-29: "I want the
        // same exact layout within the desktop app").
        GoalTiles(load: load, goals: goals)
            .askButton()
            .hubTask(load) { try await hub.goals() }
            .navigationDestination(item: $shot) { GoalDetail(goal: $0) }
            .onChange(of: goals) { _, gs in
                guard shot == nil, let id = ProcessInfo.processInfo.environment["LIFE_GOAL"], !id.isEmpty else { return }
                shot = gs.first { $0.id == id }
            }
        #else
        list
        #endif
    }

    private var list: some View {
        List {
            if let e = load.error { Section { ErrorBanner(message: e) } }
            Section {
                if goals.isEmpty && load.error == nil {
                    if load.loaded { Text("No goals.").foregroundStyle(.secondary) } else { ProgressView() }
                }
                ForEach(goals) { g in
                    let on = g.status == "active"
                    NavigationLink { GoalDetail(goal: g) } label: {
                        HStack(spacing: 12) {
                            GoalEmblemView(emblem: g.emblem, on: on)
                            Text(g.title).font(.body.weight(.semibold)).foregroundStyle(on ? .primary : .secondary)
                            Spacer()
                            if !on { Text(g.status).font(.caption).foregroundStyle(.secondary) }
                        }
                        .padding(.vertical, 5)
                    }
                    .chatAbout(ChatSubject(
                        screen: "Goals",
                        title: g.title,
                        facts: ["- `\(g.id)`, \(g.status), \(g.horizon) horizon",
                                "- \(g.note_count) notes",
                                g.sources.isEmpty ? "" : "- sources: \(g.sources)",
                                "- statement: \(g.statement)"].filter { !$0.isEmpty }.joined(separator: "\n"),
                        code: "read the notes with `lifectl goal \(g.id)`"))
                }
            }
        }
        .navigationTitle("Goals")
        .askButton()
        .hubTask(load) { try await hub.goals() }
        #if targetEnvironment(simulator) || targetEnvironment(macCatalyst)
        // ops/mac-screens.sh goal:<id> opens that goal's page, so its notes
        // can be looked at without a click.
        .navigationDestination(item: $shot) { GoalDetail(goal: $0) }
        .onChange(of: goals) { _, gs in
            guard shot == nil, let id = ProcessInfo.processInfo.environment["LIFE_GOAL"], !id.isEmpty else { return }
            shot = gs.first { $0.id == id }
        }
        #endif
    }
    @State private var shot: Goal?
}

#if targetEnvironment(macCatalyst)
/// The console's goals list (goals.js, `.goal-grid`): "Goals", then tiles of
/// at least 215 wide, 12 apart — the emblem at the top, the name on the floor
/// so a two-line name ends where a one-line one does; a goal that is not
/// active goes grey with its state in a word (the owner 2026-09-29: "the same
/// exact layout within the desktop app").
private struct GoalTiles: View {
    let load: HubLoad<[Goal]>
    let goals: [Goal]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Text("Goals").font(.system(size: 17, weight: .bold))
                if let e = load.error { ErrorBanner(message: e) }
                if goals.isEmpty && load.error == nil {
                    if load.loaded { Text("No goals.").foregroundStyle(Web.muted) } else { ProgressView() }
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 215), spacing: 12)], spacing: 12) {
                    ForEach(goals) { g in
                        NavigationLink { GoalDetail(goal: g) } label: { tile(g) }
                            .buttonStyle(.plain)
                            .chatAbout(ChatSubject(
                                screen: "Goals",
                                title: g.title,
                                facts: ["- `\(g.id)`, \(g.status), \(g.horizon) horizon",
                                        "- \(g.note_count) notes",
                                        g.sources.isEmpty ? "" : "- sources: \(g.sources)",
                                        "- statement: \(g.statement)"].filter { !$0.isEmpty }.joined(separator: "\n"),
                                code: "read the notes with `lifectl goal \(g.id)`"))
                    }
                }
            }
            .webPage()
        }
        .background(Web.page)
        .toolbar(.hidden, for: .navigationBar)
    }

    private func tile(_ g: Goal) -> some View {
        let on = g.status == "active"
        // Emblem and name on one row, the tile hugging it (the console's
        // skin.css .goal-tile, the owner 2026-09-30: the tall tile with the name on
        // its floor was mostly empty at desktop width).
        return HStack(spacing: 13) {
            GoalEmblemView(emblem: g.emblem, on: on, size: 38)
            VStack(alignment: .leading, spacing: 2) {
                Text(g.title).font(.system(size: 15.5, weight: .semibold))
                    .foregroundStyle(on ? Color.primary : Web.muted)
                    .multilineTextAlignment(.leading)
                if !on { Text(g.status).font(.system(size: 12, weight: .medium)).foregroundStyle(Web.muted) }
            }
            Spacer(minLength: 0)
        }
        .padding(EdgeInsets(top: 15, leading: 16, bottom: 15, trailing: 16))
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Web.panel, in: RoundedRectangle(cornerRadius: 14))
        .overlay { RoundedRectangle(cornerRadius: 14).strokeBorder(Web.line, lineWidth: 1) }
        .contentShape(RoundedRectangle(cornerRadius: 14))
    }
}

private extension View {
    /// The goal page's `.card`: white, hairline, radius 12, its padding set
    /// by the caller (18×20, the notes 0×20).
    func goalCard() -> some View {
        self.frame(maxWidth: .infinity, alignment: .leading)
            .background(Web.panel, in: RoundedRectangle(cornerRadius: 12))
            .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Web.line, lineWidth: 1) }
    }
}
#endif

// The goal's page: the head (name, statement, the switch), the digest, then
// the notes as a dated list — the day and who wrote it over each note, no
// kind pill (the kind meant nothing to a reader).
// The by-line is the session's title, from the hub's `by`, and opens it.
struct GoalDetail: View {
    @Environment(HubClient.self) private var hub
    @State var goal: Goal
    @State private var load = HubLoad<[GoalNote]>()
    private var notes: [GoalNote] { load.value ?? [] }
    /// A save that failed; the load's own error shows beside it.
    @State private var saveError: String?
    private var error: String? { saveError ?? load.error }
    @State private var editing = false
    @State private var title = ""
    @State private var statement = ""

    /// The switch is the whole status control: on = active, off = paused. A
    /// goal a session closed as done reads off; flipping it on reopens it.
    private var active: Binding<Bool> {
        Binding(get: { goal.status == "active" }, set: { on in patch(["status": on ? "active" : "paused"]) })
    }

    var body: some View {
        #if targetEnvironment(macCatalyst)
        webPage.askButton().hubTask(load, fetch)
        #else
        list
        #endif
    }

    private var list: some View {
        List {
            if let error { Section { ErrorBanner(message: error) } }
            // The head panel: name, statement, the active switch.
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 10) {
                        GoalEmblemView(emblem: goal.emblem, on: goal.status == "active", size: 32)
                        Text(goal.title).font(.title3.weight(.semibold))
                    }
                    if goal.statement.isEmpty { Text("No statement yet.").foregroundStyle(.secondary) }
                    else { Text(md(goal.statement)).font(.callout) }
                    Text("\(goal.horizon) · \(goal.cadence) review").font(.caption).foregroundStyle(.secondary)
                }
                .padding(.vertical, 4)
                Toggle("Active", isOn: active)
            }
            Section {
                // A Catalyst list row hugs its text; the digest gets room
                // above and below so it does not touch the card's edge.
                if let d = goal.digest, !d.isEmpty { StyledText(text: d).padding(.vertical, macRowPad) }
                else { Text("No digest yet.").foregroundStyle(.secondary) }
            } header: {
                HStack {
                    Text("Where this stands")
                    Spacer()
                    if let at = goal.digest_at { Text("updated \(shortAgo(at))").font(.caption2).foregroundStyle(.tertiary) }
                }
            }
            Section("Notes") {
                if notes.isEmpty { Text("No notes yet.").foregroundStyle(.secondary) }
                ForEach(notes) { n in
                    VStack(alignment: .leading, spacing: 5) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(n.created_at.formatted(noteDay(n.created_at))).font(.subheadline.weight(.semibold))
                            LinkOrText(n.by ?? n.author, href: n.thread_id.flatMap { $0.isEmpty ? nil : "life://open/\($0)" })
                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        StyledText(text: n.text)
                    }.padding(.vertical, 4 + macRowPad)
                }
            }
        }
        .navigationTitle("Goal")
        .navigationBarTitleDisplayMode(.inline)
        .askButton()
        .toolbar {
            Button("Edit") { title = goal.title; statement = goal.statement; editing = true }
        }
        .hubTask(load, fetch)
        .sheet(isPresented: $editing) {
            NavigationStack {
                Form {
                    TextField("Name", text: $title)
                    TextField("Statement — what the sessions steer by", text: $statement, axis: .vertical).lineLimit(3...12)
                }
                .navigationTitle("Edit goal")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { editing = false } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") {
                            patch(["title": title.trimmingCharacters(in: .whitespaces),
                                   "statement": statement.trimmingCharacters(in: .whitespacesAndNewlines)])
                            editing = false
                        }.disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
            }
        }
    }

    #if targetEnvironment(macCatalyst)
    // The console's goal page (goals.js), on the desktop (the owner 2026-09-29:
    // "the same exact layout within the desktop app"; of this page, "the
    // markdown formatting is kind of weird and bad"): "← Goals", the head card
    // — emblem, name, the status word with its switch, Edit — whose Edit
    // swaps the card for the two fields in place; then "Where this stands" and
    // the digest card, then "Notes": one card of hairline rows, the day and
    // who wrote it down a 150-wide left column, the note on the right.
    @Environment(\.dismiss) private var dismiss

    /// UIKit's Mac text style nearest the console's 14px reading size.
    private let prose: UIFont.TextStyle = .body

    private var webPage: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Button("← Goals") { dismiss() }.buttonStyle(.plain)
                    .font(.system(size: 13)).foregroundStyle(Web.muted)
                    .padding(.top, 2).padding(.bottom, 10)
                if let error { ErrorBanner(message: error).padding(.bottom, 12) }
                Group { if editing { editCard } else { headCard } }
                    .padding(.vertical, 18).padding(.horizontal, 20).goalCard()
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    WebHeading("Where this stands").fixedSize()
                    if let at = goal.digest_at {
                        Text("updated \(shortAgo(at))").font(.system(size: 11.5, weight: .medium)).foregroundStyle(Web.muted)
                    }
                    Spacer()
                }
                .padding(.top, 24).padding(.bottom, 8)
                Group {
                    if let d = goal.digest, !d.isEmpty { StyledText(text: d, style: prose) }
                    else { Text("No digest yet.").foregroundStyle(Web.muted) }
                }
                .padding(.vertical, 18).padding(.horizontal, 20).goalCard()
                WebHeading("Notes").padding(.top, 24).padding(.bottom, 8)
                VStack(alignment: .leading, spacing: 0) {
                    if notes.isEmpty {
                        Text(load.loaded ? "No notes yet." : "Loading…").foregroundStyle(Web.muted).padding(.vertical, 15)
                    }
                    ForEach(Array(notes.enumerated()), id: \.element.id) { i, n in
                        if i > 0 { Rectangle().fill(Web.line).frame(height: 1) }
                        HStack(alignment: .top, spacing: 18) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(n.created_at.formatted(noteDay(n.created_at))).font(.system(size: 13, weight: .semibold))
                                LinkOrText(n.by ?? n.author, href: n.thread_id.flatMap { $0.isEmpty ? nil : "life://open/\($0)" })
                                    .font(.system(size: 12.5)).foregroundStyle(Web.muted).tint(Web.muted)
                            }
                            .frame(width: 150, alignment: .leading)
                            StyledText(text: n.text, style: prose).frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .padding(.vertical, 15)
                    }
                }
                .padding(.horizontal, 20).goalCard()
            }
            .webPage()
        }
        .background(Web.page)
        .toolbar(.hidden, for: .navigationBar)
    }

    private var headCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 10) {
                GoalEmblemView(emblem: goal.emblem, on: goal.status == "active", size: 34)
                    .padding(.top, -3).padding(.leading, -2)
                Text(md(goal.title)).font(.system(size: 20, weight: .bold))
                    .frame(maxWidth: .infinity, alignment: .leading)
                HStack(spacing: 6) {
                    Text(goal.status).font(.system(size: 12.5)).foregroundStyle(Web.muted)
                    Toggle("", isOn: active).labelsHidden().toggleStyle(.switch).controlSize(.mini)
                }
                Button("Edit") { title = goal.title; statement = goal.statement; editing = true }
                    .buttonStyle(WebButtonStyle(small: true))
            }
            Group {
                if goal.statement.isEmpty { Text("No statement yet.").foregroundStyle(Web.muted) }
                else { StyledText(text: goal.statement, style: prose) }
            }
            .padding(.top, 10)
            Text("\(goal.horizon) · \(goal.cadence) review").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                .padding(.top, 10)
        }
    }

    private var editCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Name").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                TextField("", text: $title).textFieldStyle(.roundedBorder)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text("Statement — what the sessions steer by").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                TextEditor(text: $statement).font(.system(size: 14)).frame(minHeight: 130)
                    .scrollContentBackground(.hidden).padding(4)
                    .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) }
            }
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel") { editing = false }.buttonStyle(WebButtonStyle(small: true))
                Button("Save") {
                    patch(["title": title.trimmingCharacters(in: .whitespaces),
                           "statement": statement.trimmingCharacters(in: .whitespacesAndNewlines)])
                    editing = false
                }
                .buttonStyle(WebButtonStyle(primary: true, small: true))
                .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        }
    }
    #endif

    /// "Sep 24", with the year once it is not this one — the console's noteDay.
    private func noteDay(_ d: Date) -> Date.FormatStyle {
        let thisYear = Calendar.current.isDate(d, equalTo: .now, toGranularity: .year)
        return thisYear ? .dateTime.month(.abbreviated).day() : .dateTime.month(.abbreviated).day().year()
    }

    func fetch() async throws -> [GoalNote] {
        // The goal arrives from the list, which carries no digest — re-read it.
        async let g = hub.goal(goal.id)
        let ns = try await hub.goalNotes(goal.id)
        goal = try await g
        return ns
    }

    /// A save that fails says so instead of showing a value that never saved.
    func patch(_ fields: [String: String]) {
        Task {
            do { goal = try await hub.patchGoal(goal.id, fields); saveError = nil; await load.run(fetch) }
            catch { saveError = error.localizedDescription }
        }
    }
}
