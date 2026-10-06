import SwiftUI

// Data-source inventory: every source, where it comes from, where it is
// stored, live counts. Groups → sources → what each is connected to → kinds.
// A row is a SOURCE: a service, a Google Drive folder (with every folder
// inside it listed), or the tables that came from neither. Content comes from GET /api/v1/sources so the page never claims
// access from memory.

/// One row on the Sources page, keyed by its group too: the "Not syncing"
/// section holds rows from every group, and each opens under its own.
struct SourceRef: Identifiable, Hashable {
    let group: SourceGroup
    let source: SourceEntry
    var id: String { group.id + "/" + source.id }
}

struct SourceSection: Identifiable {
    let id: String
    let title: String
    let note: String?
    let rows: [SourceRef]
}

/// The page's sections, the console's sourcesListHTML (the owner 2026-09-30): each
/// group's working sources alphabetical by title, a group with none left out,
/// then every failing source from any group in one "Not syncing" section at
/// the bottom ("somehow signal it into a second category, that all of those
/// are at the bottom").
func sourceSections(_ groups: [SourceGroup]) -> [SourceSection] {
    let byTitle: (SourceRef, SourceRef) -> Bool = {
        $0.source.title.localizedLowercase < $1.source.title.localizedLowercase
    }
    var out: [SourceSection] = []
    var bad: [SourceRef] = []
    for g in groups {
        let refs = g.sources.map { SourceRef(group: g, source: $0) }
        bad += refs.filter { $0.source.status == "failing" }
        let ok = refs.filter { $0.source.status != "failing" }.sorted(by: byTitle)
        if !ok.isEmpty { out.append(SourceSection(id: g.id, title: g.title, note: g.note, rows: ok)) }
    }
    if !bad.isEmpty { out.append(SourceSection(id: "_failing", title: "Not syncing", note: nil, rows: bad.sorted(by: byTitle))) }
    return out
}

struct SourcesView: View {
    @Environment(HubClient.self) private var hub
    @State private var load = HubLoad<SourcesInventory>()
    /// Rows unfolded to their full "connected to" list; every row starts folded.
    @State private var open: Set<String> = []

    // Pushed from MoreView's stack — no NavigationStack of its own, or the
    // detail page gets two nav bars (two back buttons, content pushed down).
    var body: some View {
        page
        .navigationTitle("Sources")
        .askButton()
        .hubTask(load) { try await hub.sources() }
    }

    @ViewBuilder private var page: some View {
        #if targetEnvironment(macCatalyst)
        // The desktop draws the console's page, not the phone's list (the owner
        // 2026-09-29: "I want the same exact layout within the desktop app").
        SourcesWebPage(load: load)
        #else
        List {
            HubLoadStatus(load: load)
            if let v = load.value {
                // Only what is connected: the hub leaves
                // unconnected sources out, so an empty group is skipped.
                let sections = sourceSections(v.groups)
                if sections.isEmpty {
                    Section { Text("No data sources connected yet.").foregroundStyle(.secondary) }
                }
                ForEach(sections) { sec in
                    Section {
                        ForEach(sec.rows) { r in foldRow(r) }
                    } header: { Text(sec.title) } footer: { if let n = sec.note { Text(n).lineLimit(1) } }
                }
            }
        }
        #endif
    }

    /// One folded row (the owner 2026-09-30: "all these kind of become one row"):
    /// the chevron unfolds what it is connected to under it, the row itself
    /// opens the source's page.
    @ViewBuilder private func foldRow(_ r: SourceRef) -> some View {
        let isOpen = open.contains(r.id)
        HStack(spacing: 6) {
            Button {
                withAnimation(.snappy(duration: 0.15)) {
                    if isOpen { open.remove(r.id) } else { open.insert(r.id) }
                }
            } label: {
                Image(systemName: "chevron.right").font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
                    .rotationEffect(.degrees(isOpen ? 90 : 0)).frame(width: 16, height: 28)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            // One thing connected is not a list (the owner 2026-10-01: "there's no
            // need to drop it open if it's just one thing"): the row says it.
            .opacity(r.source.accounts.count > 1 ? 1 : 0).disabled(r.source.accounts.count <= 1)
            NavigationLink { SourceDetail(source: r.source, group: r.group.title) } label: { SourceRow(source: r.source) }
        }
        if isOpen {
            ForEach(r.source.accounts) { a in
                VStack(alignment: .leading, spacing: 2) {
                    if let u = a.url, let url = URL(string: u) { Link(a.label, destination: url).font(.subheadline) }
                    else { Text(a.label).font(.subheadline) }
                    let bits = [a.detail, a.last.map { shortAgo($0) }].compactMap { $0 }
                    if !bits.isEmpty { Text(bits.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary) }
                }
                .padding(.leading, 22)
            }
        }
    }
}

#if targetEnvironment(macCatalyst)
// MARK: - The console's Sources page (sources.js), on the desktop
//
// the owner 2026-09-29: "I spent a lot of time on this web UI layout. I want the
// same exact layout within the desktop app." The head line with its legend,
// then one white card per group: its title and note, then a four-column table
// — Source (a third) · Connected to · Rows · Newest — one FOLDED row per
// source (2026-09-30): the name the door to the source's page, the rest of the
// row unfolding its account lines, each account name its own link.

/// The console's status dot: green live, red refusing us, grey the owner keeps it.
func sourceDotColor(_ status: String) -> Color {
    status == "failing" ? .red : status == "connected" || status == "live" ? .green : Web.muted
}

private struct SourcesWebPage: View {
    let load: HubLoad<SourcesInventory>
    @State private var open: SourceRef?
    /// Rows unfolded in place; every row starts folded (the owner 2026-09-30).
    @State private var unfolded: Set<String> = []
    @State private var width: CGFloat = 1068

    private var groups: [SourceGroup] { (load.value?.groups ?? []).filter { !$0.sources.isEmpty } }
    private var sections: [SourceSection] { sourceSections(groups) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .firstTextBaseline) {
                    Text("Sources").font(.system(size: 17, weight: .bold))
                    Spacer()
                    legend
                }
                if let e = load.error { ErrorBanner(message: e) }
                else if load.value == nil { ProgressView().frame(maxWidth: .infinity) }
                if load.value != nil, groups.isEmpty {
                    Text("No data sources connected yet.").foregroundStyle(Web.muted).webCard()
                }
                let secs = sections
                ForEach(secs) { sec in card(sec, head: sec.id == secs.first?.id) }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            .webPage()
        }
        .background(Web.page)
        .toolbar(.hidden, for: .navigationBar)
        .navigationDestination(item: $open) { r in
            SourceDetail(source: r.source, group: r.group.title)
        }
    }

    /// "12 sources · 1 not syncing ● live feed ● refusing us ● you maintain it".
    private var legend: some View {
        let all = groups.flatMap(\.sources)
        let bad = all.filter { $0.status == "failing" }.count
        return HStack(spacing: 5) {
            Text(bad > 0 ? "\(all.count) sources · \(bad) not syncing" : "\(all.count) sources")
            dot(.green).padding(.leading, 7); Text("live feed")
            if bad > 0 { dot(.red).padding(.leading, 7); Text("refusing us") }
            dot(Web.muted).padding(.leading, 7); Text("you maintain it")
        }
        .font(.system(size: 12.5)).foregroundStyle(Web.muted).lineLimit(1)
    }

    private func dot(_ c: Color) -> some View { Circle().fill(c).frame(width: 7, height: 7) }

    // Fixed columns, as the console's colgroup: a third, the rest, 76, 92.
    private var nameW: CGFloat { (width - 2) * 0.33 }

    /// Only the first card names the columns, as the console (sourceSectionHTML).
    private func card(_ sec: SourceSection, head: Bool) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 3) {
                WebHeading(sec.title)
                // One line, as the console's .src-note; the sentence is its help.
                if let n = sec.note {
                    Text(n).font(.system(size: 12.5)).foregroundStyle(Web.muted).lineLimit(1).truncationMode(.tail).help(n)
                }
            }
            .padding(EdgeInsets(top: 12, leading: 16, bottom: 10, trailing: 16))
            if head {
                Rectangle().fill(Web.line).frame(height: 1)
                row(Text("SOURCE"), Text("CONNECTED TO"), Text("ROWS"), Text("NEWEST"))
                    .font(.system(size: 11.5, weight: .semibold)).tracking(0.5).foregroundStyle(Web.muted)
                    .padding(.vertical, -2)
            }
            ForEach(sec.rows) { r in
                Rectangle().fill(Web.line).frame(height: 1)
                sourceRow(r)
            }
        }
        .background(Web.panel, in: RoundedRectangle(cornerRadius: 12))
        .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Web.line, lineWidth: 1) }
    }

    private func row(_ name: some View, _ to: some View, _ rows: some View, _ last: some View) -> some View {
        HStack(alignment: .top, spacing: 0) {
            name.frame(width: max(nameW - 32, 120), alignment: .leading).padding(.leading, 16).padding(.trailing, 24)
            to.frame(maxWidth: .infinity, alignment: .leading)
            rows.frame(width: 76 - 16, alignment: .trailing).padding(.leading, 16)
            last.frame(width: 92 - 16, alignment: .trailing).padding(.horizontal, 16)
        }
        .padding(.vertical, 10)
    }

    /// The console's folded row (sourceRow in sources.js): the name opens the
    /// source's page; anywhere else unfolds the account lines in place.
    private func sourceRow(_ r: SourceRef) -> some View {
        let s = r.source
        // One thing connected is not a list (the owner 2026-10-01: "there's no need
        // to drop it open if it's just one thing"): no chevron, the one name
        // (its link, when it has one) on the row, the row the door to the page.
        let folds = s.accounts.count > 1
        let isOpen = folds && unfolded.contains(r.id)
        return row(
            HStack(alignment: .firstTextBaseline, spacing: 9) {
                dot(sourceDotColor(s.status)).alignmentGuide(.firstTextBaseline) { $0[.bottom] }
                Button { open = r } label: { Text(s.title).font(.system(size: 15, weight: .bold)) }
                    .buttonStyle(.plain)
            },
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .firstTextBaseline, spacing: 0) {
                    if s.accounts.isEmpty {
                        Text("—").foregroundStyle(Web.muted)
                    } else if folds {
                        Text("▸").font(.system(size: 10)).foregroundStyle(Web.muted)
                            .rotationEffect(.degrees(isOpen ? 90 : 0)).frame(width: 14, alignment: .leading)
                        Text(s.shortTo).foregroundStyle(Web.muted)
                    } else {
                        Text(oneAccount(s.accounts[0])).foregroundStyle(Web.muted).padding(.leading, 14)
                    }
                    if s.status == "failing" {
                        Text(SourceRow(source: s).failBits).font(.system(size: 12)).foregroundStyle(.red).padding(.leading, 10)
                    }
                }
                .font(.system(size: 13)).lineLimit(1)
                if isOpen {
                    VStack(alignment: .leading, spacing: 3) { ForEach(s.accounts) { SourceAccountLine(account: $0) } }
                        .padding(.leading, 14)
                }
            },
            Text(s.total.formatted()).font(.system(size: 12.5)).foregroundStyle(Web.muted).monospacedDigit(),
            Text(s.last.map { shortAgo($0) } ?? "").font(.system(size: 12.5)).foregroundStyle(Web.muted).lineLimit(1)
        )
        .contentShape(Rectangle())
        .onTapGesture {
            if !folds { open = r; return }
            if isOpen { unfolded.remove(r.id) } else { unfolded.insert(r.id) }
        }
    }

    /// The one thing a source is connected to, its own link when it has one.
    private func oneAccount(_ a: SourceAccount) -> AttributedString {
        var t = AttributedString(a.label)
        if let u = a.url, let url = URL(string: u) { t.link = url; t.foregroundColor = Web.muted }
        return t
    }
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

/// The console's source page (sourcePageHTML): "Sources / X" with the group on
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
                        Button("Sources") { dismiss() }.buttonStyle(.plain).foregroundStyle(Web.accent)
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
