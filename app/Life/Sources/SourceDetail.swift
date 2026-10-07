import SwiftUI

// One data source's page, opened from a card on the Configuration tab:
// where it comes from, where it is stored, what it is connected to, live
// counts by kind. Content comes from GET /api/v1/sources, so the page never
// claims access from memory. The console's twin is views/sources.js.

/// One source card, keyed by its group too: groups sharing a tag share a
/// section on the Configuration page, and each card opens under its own group.
struct SourceRef: Identifiable, Hashable {
    let group: SourceGroup
    let source: SourceEntry
    var id: String { group.id + "/" + source.id }
}

#if targetEnvironment(macCatalyst)
/// The console's status dot: green live, red refusing us, grey the owner keeps it.
func sourceDotColor(_ status: String) -> Color {
    status == "failing" ? .red : status == "connected" || status == "live" ? .green : Web.muted
}

/// One account line, the console's sourceChip: the name (its own link when
/// it has one), then "detail · ago" in grey.
struct SourceAccountLine: View {
    let account: SourceAccount
    var body: some View {
        let bits = [account.detail, account.last.map { shortAgo($0) }].compactMap { $0 }
        (nameText + Text(bits.isEmpty ? "" : "  " + bits.joined(separator: " · ")).foregroundColor(Web.muted))
            .font(.system(size: 13)).lineSpacing(2)
            .fixedSize(horizontal: false, vertical: true)
    }
    private var nameText: Text {
        var s = AttributedString(account.label)
        s.font = .system(size: 13, weight: .medium)
        if let u = account.url, let url = URL(string: u) { s.link = url; s.foregroundColor = .primary }
        return Text(s)
    }
}

/// The console's source page (sourcePageHTML): "Configuration / X" with the group on
/// the right, then one card — the status line, what the service said, the
/// counts, and the small-caps sections the phone draws as list sections.
private struct SourceWebPage: View {
    let source: SourceEntry
    let group: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .firstTextBaseline) {
                    HStack(spacing: 5) {
                        Button("Configuration") { dismiss() }.buttonStyle(.plain).foregroundStyle(Web.accent)
                        Text("/")
                        Text(source.title)
                    }
                    .font(.system(size: 17, weight: .bold))
                    Spacer()
                    if let group { Text(group).font(.system(size: 12.5)).foregroundStyle(Web.muted) }
                }
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .firstTextBaseline, spacing: 9) {
                        Circle().fill(sourceDotColor(source.status)).frame(width: 7, height: 7)
                        Text(source.status)
                        if source.status == "failing" {
                            Text(SourceRow(source: source).failLine).font(.system(size: 12)).foregroundStyle(.red)
                                .padding(.leading, 1)
                        }
                    }
                    if let e = source.error {
                        (Text("What the service said: ") + Text(e).font(.system(size: 11.5, design: .monospaced)))
                            .font(.system(size: 12)).foregroundStyle(.red).textSelection(.enabled)
                            .padding(.top, 6)
                    }
                    Text(counts).font(.system(size: 12.5)).foregroundStyle(Web.muted).padding(.top, 6)
                    if !source.accounts.isEmpty {
                        h4("Connected to")
                        VStack(alignment: .leading, spacing: 3) { ForEach(source.accounts) { SourceAccountLine(account: $0) } }
                    }
                    h4("Where it comes from")
                    Text(source.from).font(.system(size: 12.5)).fixedSize(horizontal: false, vertical: true)
                    h4("Where it is stored")
                    Text(source.storage).font(.system(size: 12.5)).fixedSize(horizontal: false, vertical: true)
                    if !source.kinds.isEmpty {
                        h4("What is kept (rows by kind)")
                        kinds
                    }
                }
                .padding(EdgeInsets(top: 14, leading: 16, bottom: 16, trailing: 16))
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Web.panel, in: RoundedRectangle(cornerRadius: 12))
                .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Web.line, lineWidth: 1) }
            }
            .webPage()
        }
        .background(Web.page)
        .toolbar(.hidden, for: .navigationBar)
    }

    private var counts: String {
        var s = "\(source.total.formatted()) rows"
        if let l = source.last { s += " · newest row \(shortAgo(l))" }
        if let l = source.lastOK { s += " · last worked \(shortAgo(l))" }
        return s
    }

    private func h4(_ t: String) -> some View {
        Text(t.uppercased()).font(.system(size: 11.5, weight: .semibold)).tracking(0.5)
            .foregroundStyle(Web.muted).padding(.top, 16).padding(.bottom, 6)
    }

    /// kind (mono) · note · first → last · n, hairlines between.
    private var kinds: some View {
        Grid(alignment: .topLeading, horizontalSpacing: 8, verticalSpacing: 6) {
            ForEach(Array(source.kinds.enumerated()), id: \.offset) { i, k in
                if i > 0 { Rectangle().fill(Web.line).frame(height: 1).gridCellUnsizedAxes(.horizontal) }
                GridRow {
                    Text(k.kind).font(.system(size: 13, design: .monospaced))
                    Text(k.note ?? "").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                        .frame(maxWidth: .infinity, alignment: .leading).fixedSize(horizontal: false, vertical: true)
                    Text("\(day(k.first)) → \(day(k.last))").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                        .lineLimit(1).fixedSize()
                    Text(k.n.formatted()).monospacedDigit().gridColumnAlignment(.trailing)
                }
            }
        }
    }

    private func day(_ d: Date) -> String { d.formatted(.iso8601.year().month().day()) }
}
#endif

/// A folded source row: dot, name, newest; under it what it is connected to
/// in a few words (or, in "Not syncing", how stale it is) and the row count.
struct SourceRow: View {
    let source: SourceEntry
    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 7) {
                Circle().fill(statusColor).frame(width: 7, height: 7)
                Text(source.title).font(.body).lineLimit(1)
                Spacer(minLength: 6)
                // Short form, like the Sessions and Goals rows.
                if let l = source.last { Text(shortAgo(l)).font(.caption).foregroundStyle(.secondary) }
            }
            HStack(spacing: 6) {
                // The point of the red line is not that there is an error: it
                // is HOW OLD the numbers are. The section heading says the rest.
                if source.status == "failing" {
                    Text(failBits).foregroundStyle(.red)
                } else {
                    Text(source.shortTo).foregroundStyle(.secondary)
                }
                Spacer(minLength: 6)
                if source.total > 0 { Text("\(source.total.formatted()) \(source.total == 1 ? "row" : "rows")").foregroundStyle(.tertiary) }
            }
            .font(.caption).lineLimit(1).padding(.leading, 14)
        }
    }
    var statusColor: Color {
        let s = source.status
        if s == "failing" { return .red }
        if s.hasPrefix("connected") || s.hasPrefix("live") { return .green }
        if s.contains("pending") || s.contains("not") { return .orange }
        return .secondary
    }
    var failBits: String {
        let n = max(source.fails, 1) // absent means one, and `fails` is omitted at zero
        var bits = ["\(n) \(n == 1 ? "try" : "tries") failed"]
        if let since = source.failingSince { bits.append("since \(shortAgo(since))") }
        bits.append(source.lastOK.map { "last worked \(shortAgo($0))" } ?? "never worked")
        return bits.joined(separator: " · ")
    }
    var failLine: String { "Not syncing — " + failBits }
}

struct SourceDetail: View {
    let source: SourceEntry
    /// The group it sits in, which the console's page names on the right.
    var group: String? = nil
    var body: some View {
        #if targetEnvironment(macCatalyst)
        SourceWebPage(source: source, group: group).askButton()
        #else
        list
        #endif
    }

    private var list: some View {
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
