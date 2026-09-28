import SwiftUI

// Data-source inventory: every source, where it comes from, where it is
// stored, live counts. Groups → sources → what each is connected to → kinds.
// A row is a SOURCE: a service, a Google Drive folder (with every folder
// inside it listed), or the tables that came from neither. Content comes from GET /api/v1/sources so the page never claims
// access from memory.

struct SourcesView: View {
    @Environment(HubClient.self) private var hub
    @State private var load = HubLoad<SourcesInventory>()

    // Pushed from MoreView's stack — no NavigationStack of its own, or the
    // detail page gets two nav bars (two back buttons, content pushed down).
    var body: some View {
        List {
            HubLoadStatus(load: load)
            if let v = load.value {
                // Only what is connected: the hub leaves
                // unconnected sources out, so an empty group is skipped.
                if v.groups.allSatisfy({ $0.sources.isEmpty }) {
                    Section { Text("No data sources connected yet.").foregroundStyle(.secondary) }
                }
                ForEach(v.groups.filter { !$0.sources.isEmpty }) { g in
                    Section {
                        ForEach(g.sources) { s in
                            NavigationLink { SourceDetail(source: s) } label: { SourceRow(source: s) }
                        }
                    } header: { Text(g.title) } footer: { if let n = g.note { Text(n) } }
                }
            }
        }
        .navigationTitle("Sources")
        .askButton()
        .hubTask(load) { try await hub.sources() }
    }
}

struct SourceRow: View {
    let source: SourceEntry
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(source.title).font(.body)
                Spacer()
                Text(source.status).font(.caption).foregroundStyle(statusColor)
            }
            // The point of this line is not that there is an error: it is HOW
            // OLD the numbers are. The console says the same thing under the
            // source name on its row.
            if source.status == "failing" {
                Text(failLine).font(.caption).foregroundStyle(.red)
            }
            // What it is connected to: every channel, handle, repo, bank
            // account or statement folder, so "connected" is never bare.
            if !source.accounts.isEmpty {
                Text(source.accounts.map(\.label).joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            HStack(spacing: 6) {
                if source.total > 0 { Text("\(source.total.formatted()) \(source.total == 1 ? "row" : "rows")") }
                // Short form, like the Sessions and Goals rows.
                if let l = source.last { Text("· newest row \(shortAgo(l))") }
            }.font(.caption).foregroundStyle(.tertiary)
        }
    }
    var statusColor: Color {
        let s = source.status
        if s == "failing" { return .red }
        if s.hasPrefix("connected") || s.hasPrefix("live") { return .green }
        if s.contains("pending") || s.contains("not") { return .orange }
        return .secondary
    }
    var failLine: String {
        let n = max(source.fails, 1) // absent means one, and `fails` is omitted at zero
        var bits = ["\(n) \(n == 1 ? "try" : "tries") failed"]
        if let since = source.failingSince { bits.append("since \(shortAgo(since))") }
        bits.append(source.lastOK.map { "last worked \(shortAgo($0))" } ?? "never worked")
        return "Not syncing — " + bits.joined(separator: " · ")
    }
}

struct SourceDetail: View {
    let source: SourceEntry
    var body: some View {
        List {
            Section("Status") {
                LabeledContent("State") {
                    Text(source.status).foregroundStyle(source.status == "failing" ? Color.red : .secondary)
                }
                LabeledContent("Rows", value: source.total.formatted())
                if let l = source.last { LabeledContent("Last", value: shortAgo(l)) }
                if let l = source.lastOK { LabeledContent("Last worked", value: shortAgo(l)) }
                if let e = source.error {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("What the service said").font(.caption).foregroundStyle(.secondary)
                        Text(e).font(.caption.monospaced()).foregroundStyle(.red)
                    }
                }
            }
            // One flat list: a row is a source now, so everything under it is
            // the one thing that source is pointed at — every channel, repo,
            // live account, or folder inside the Drive folder.
            if !source.accounts.isEmpty {
                Section("Connected to") {
                    ForEach(source.accounts) { a in
                        VStack(alignment: .leading, spacing: 2) {
                            HStack {
                                if let u = a.url, let url = URL(string: u) { Link(a.label, destination: url) } else { Text(a.label) }
                                Spacer()
                                if let l = a.last { Text(shortAgo(l)).font(.caption).foregroundStyle(.secondary) }
                            }
                            if let d = a.detail { Text(d).font(.caption).foregroundStyle(.secondary) }
                        }
                    }
                }
            }
            Section("Where it comes from") { Text(source.from) }
            Section("Where it is stored") { Text(source.storage) }
            if !source.kinds.isEmpty {
                Section("What is kept (rows by kind)") {
                    ForEach(source.kinds) { k in
                        VStack(alignment: .leading, spacing: 2) {
                            HStack {
                                Text(k.kind).font(.subheadline.monospaced())
                                Spacer()
                                Text("\(k.n.formatted()) rows").monospacedDigit()
                            }
                            // A row is often a snapshot of everything, not one thing.
                            if let note = k.note {
                                Text(note).font(.caption).foregroundStyle(.secondary)
                            }
                            Text("\(k.first.formatted(date: .abbreviated, time: .omitted)) → \(k.last.formatted(date: .abbreviated, time: .omitted))")
                                .font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
        .navigationTitle(source.title)
        .navigationBarTitleDisplayMode(.inline)
        .askButton()
    }
}
