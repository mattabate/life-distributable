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
    }
}

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
                if let d = goal.digest, !d.isEmpty { StyledText(text: d) }
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
                    }.padding(.vertical, 4)
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
