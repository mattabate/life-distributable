import SwiftUI

// Configuration: how the sessions are powered, what the owner's goals are,
// what the hub is connected to. The fourth tab on every surface; the
// console's twin is views/config.js, block for block:
//   Powered by: the plan (its price when the hub has found one), this
//     month's list-price spend, each limit's headroom, the model new sessions
//     start on. One block per provider, so a second one is a second block.
//   Goals: the active goals as small tiles, each the door to its page.
//   Data sources: one card per source — the provider's tile, rows, the newest
//     row — edged in its group's colour, a section per group (sources.go
//     sourceTags) under its name. A card opens SourceDetail.

/// Everything the page reads, in one load. Only the sources are required;
/// the plan and the goals each draw their own failure.
struct ConfigData: Sendable {
    var quota: Quota?
    var model: ModelSetting?
    var goals: [Goal]?
    var sources: SourcesInventory
}

/// The newest row: "12m ago" inside a day, the date after that — the
/// console's cfgDay.
func configDay(_ d: Date, now: Date = Date()) -> String {
    if now.timeIntervalSince(d) < 86400 { return shortAgo(d, now: now) }
    let sameYear = Calendar.current.component(.year, from: d) == Calendar.current.component(.year, from: now)
    return sameYear ? dayLabel(d) : shortDay(d)
}

struct ConfigView: View {
    @Environment(HubClient.self) private var hub
    @State private var load = HubLoad<ConfigData>()

    #if targetEnvironment(macCatalyst)
    private let mac = true
    private var bg: Color { Web.page }
    private var panel: Color { Web.panel }
    private var line: Color { Web.line }
    private var muted: Color { Web.muted }
    private var track: Color { Web.code }
    private var accent: Color { Web.accent }
    #else
    private var track: Color { Color(.systemFill) }
    private var accent: Color { .accentColor }
    private let mac = false
    private var bg: Color { Color(.systemGroupedBackground) }
    private var panel: Color { Color(.secondarySystemGroupedBackground) }
    private var line: Color { Color(.separator).opacity(0.5) }
    private var muted: Color { .secondary }
    #endif

    var body: some View {
        ScrollViewReader { proxy in
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if mac { Text("Configuration").font(.system(size: 17, weight: .bold)) }
                if let e = load.error { ErrorBanner(message: e) }
                if let d = load.value {
                    if mac {
                        HStack(alignment: .top, spacing: 12) {
                            block("Powered by") { plan(d) }
                            block("Goals", opens: { GoalsView() }) { goals(d.goals) }
                        }
                    } else {
                        block("Powered by") { plan(d) }
                        block("Goals", opens: { GoalsView() }) { goals(d.goals) }
                    }
                    heading("Data sources").padding(.top, 6).id("sources")
                    sources(d.sources.groups)
                        #if targetEnvironment(simulator)
                        // LIFE_SCROLL=sources ops/screens.sh more:config — the sections are below the fold.
                        .task {
                            guard ProcessInfo.processInfo.environment["LIFE_SCROLL"] == "sources" else { return }
                            try? await Task.sleep(for: .seconds(1))
                            proxy.scrollTo("sources", anchor: .top)
                        }
                        #endif
                } else if load.error == nil {
                    ProgressView().frame(maxWidth: .infinity).padding(40)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        }
        .background(bg)
        .navigationTitle("Configuration")
        .askButton()
        #if targetEnvironment(macCatalyst)
        .toolbar(.hidden, for: .navigationBar)
        #endif
        .hubTask(load) {
            let q = try? await hub.quota()
            let m = try? await hub.modelSetting()
            let g = try? await hub.goals()
            return ConfigData(quota: q, model: m, goals: g, sources: try await hub.sources())
        }
    }

    // MARK: blocks

    private func heading(_ t: String) -> some View {
        Text(t.uppercased()).font(.system(size: 11.5, weight: .semibold)).tracking(0.8).foregroundStyle(muted)
    }

    private func block<C: View>(_ title: String, @ViewBuilder _ c: () -> C) -> some View {
        block(title, opens: { EmptyView() }, c)
    }

    /// A block whose heading opens a page — the Goals block opens the goals
    /// page; the console's `<h3><a>` is the same door.
    private func block<D: View, C: View>(_ title: String, opens dest: @escaping () -> D, @ViewBuilder _ c: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            if D.self == EmptyView.self {
                heading(title)
            } else {
                NavigationLink { dest() } label: {
                    HStack(spacing: 4) {
                        heading(title)
                        Image(systemName: "chevron.right").font(.system(size: 9, weight: .bold)).foregroundStyle(muted)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            c()
        }
        .padding(EdgeInsets(top: 13, leading: 15, bottom: 14, trailing: 15))
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(panel, in: RoundedRectangle(cornerRadius: 12))
        .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(line, lineWidth: 1) }
    }

    @ViewBuilder private func plan(_ d: ConfigData) -> some View {
        if let q = d.quota, let p = q.plan {
            VStack(alignment: .leading, spacing: 10) {
                if q.available { ForEach(q.windows.filter(low)) { alert($0) } }
                NavigationLink { SpendView(pushed: true) } label: {
                    HStack(spacing: 10) {
                        BrandTile(brand: p.brand, size: 34)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(p.name).font(.system(size: 15, weight: .semibold))
                            Text(planLine(p)).font(.system(size: 12.5)).foregroundStyle(muted).lineLimit(2)
                        }
                        Spacer(minLength: 8)
                        VStack(alignment: .trailing, spacing: 0) {
                            Text(usd0(p.month_usd)).font(.system(size: 20, weight: .semibold)).monospacedDigit()
                            Text("this month").font(.system(size: 12)).foregroundStyle(muted)
                        }
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                if q.available, !q.windows.isEmpty {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: mac ? 190 : 230), spacing: 18)], alignment: .leading, spacing: 6) {
                        ForEach(q.windows) { limit($0) }
                    }
                } else if let e = q.error, !e.isEmpty {
                    Text(e).font(.system(size: 12.5)).foregroundStyle(.red)
                }
                let runs = d.model.map { $0.starts_on.isEmpty ? $0.default_model : $0.starts_on } ?? q.next_model ?? ""
                if !runs.isEmpty { models(runs, d.model) }
            }
        } else {
            Text("Couldn't load the plan").font(.system(size: 13)).foregroundStyle(.red)
        }
    }

    /// "Claude Code · $100/mo · charged Oct 1"
    private func planLine(_ p: ClaudePlan) -> String {
        var bits = [p.via]
        if let usd = p.usd, usd > 0 {
            bits.append(usd0(usd) + "/" + (p.period == "monthly" ? "mo" : (p.period ?? "")))
            if let on = p.charged_on, !on.isEmpty { bits.append("charged " + dayLabel(String(on.prefix(10)))) }
        }
        return bits.joined(separator: " · ")
    }

    /// The ladder's toggle — the console's cfgModelHTML: the rung in use
    /// filled, a shut one struck through, auto. The same PUT as the Spend
    /// page's NextModelCard.
    private func models(_ runs: String, _ s: ModelSetting?) -> some View {
        let rungs = s.map { $0.rungs.isEmpty ? $0.options.map { ModelRung(model: $0, open: true) } : $0.rungs } ?? []
        let chips = HStack(spacing: mac ? 4 : 6) {
            ForEach(Array(rungs.enumerated()), id: \.element) { i, r in
                if i > 0 { Text("›").foregroundStyle(muted) }
                chip(shortModel(r.model), on: r.model == runs && r.open, open: r.open) { pick(r.model) }
            }
            if !rungs.isEmpty {
                Text("·").foregroundStyle(muted)
                chip("auto", on: s?.explicit != true, open: true) { pick("") }
            }
        }
        let label = Text(rungs.isEmpty ? "New sessions run on \(shortModel(runs))" : "New sessions run on")
            .font(.system(size: 12.5)).foregroundStyle(muted)
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: 10) { label; chips }
            VStack(alignment: .leading, spacing: 6) { label; chips }
        }
    }

    @ViewBuilder private func chip(_ t: String, on: Bool, open: Bool, tap: @escaping () -> Void) -> some View {
        #if targetEnvironment(macCatalyst)
        Button(t, action: tap)
            .buttonStyle(WebButtonStyle(primary: on, small: true))
            .strikethrough(!open).opacity(open ? 1 : 0.5)
            .disabled(!open)
        #else
        Button(action: tap) {
            Text(t)
                .font(.caption.weight(on ? .semibold : .regular))
                .strikethrough(!open)
                .padding(.horizontal, 10).padding(.vertical, 5)
                .foregroundStyle(on ? Color.white : .primary)
                .background(on ? Color.accentColor : Color(.tertiarySystemFill), in: Capsule())
                .opacity(open ? 1 : 0.5)
        }
        .buttonStyle(.plain)
        .disabled(!open)
        #endif
    }

    private func pick(_ id: String) {
        Task {
            do {
                let m = try await hub.setModel(id)
                load.value?.model = m
                load.error = nil
            } catch {
                if !error.isCancellation { load.error = "Couldn't set the model: \(error.localizedDescription)" }
            }
        }
    }

    // A limit with this little left is "nearly out" — the console's cfgLow.
    // The hub's amber `warn` is a pace call that lights early; this is red and
    // named at the top of the block.
    private static let lowLeft = 10
    private func left(_ w: QuotaWindow) -> Int { max(0, 100 - Int(jsRound(w.utilization))) }
    private func low(_ w: QuotaWindow) -> Bool { left(w) <= Self.lowLeft }

    /// "7 days nearly out   2% left · resets Oct 7 12:00 AM"
    private func alert(_ w: QuotaWindow) -> some View {
        let resets = (w.foot ?? "").components(separatedBy: " · ").first { $0.hasPrefix("resets") }
        let rest = ["\(left(w))% left", resets].compactMap { $0 }.joined(separator: " · ")
        return ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text("\(w.label) nearly out").fontWeight(.semibold)
                Text(rest).monospacedDigit().opacity(0.9)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text("\(w.label) nearly out").fontWeight(.semibold)
                Text(rest).monospacedDigit().opacity(0.9)
            }
        }
        .font(.system(size: 13)).foregroundStyle(.red)
        .padding(.horizontal, 10).padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.red.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
        .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(Color.red.opacity(0.35), lineWidth: 1) }
    }

    /// One limit: its label, the headroom left, a thin meter with the clock's tick.
    private func limit(_ w: QuotaWindow) -> some View {
        let isLow = low(w)
        let tone: Color? = isLow || w.tone == "bad" ? .red : w.tone == "warn" ? .orange : nil
        let used = min(max(w.utilization / 100, 0), 1)
        return VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(w.label).font(.system(size: 12.5, weight: isLow ? .semibold : .regular)).lineLimit(1)
                    .foregroundStyle(isLow ? Color.red : .primary)
                Spacer(minLength: 4)
                Text("\(left(w))% left")
                    .font(.system(size: 12.5, weight: .semibold)).monospacedDigit()
                    .foregroundStyle(tone ?? .primary)
            }
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(track)
                    Capsule().fill(tone ?? accent).frame(width: g.size.width * used)
                    if w.elapsedPct > 0 {
                        Rectangle().fill(Color.primary.opacity(0.55)).frame(width: 2)
                            .offset(x: g.size.width * min(w.elapsedPct / 100, 1) - 1)
                    }
                }
                .clipShape(Capsule())
            }
            .frame(height: 6)
        }
    }

    @ViewBuilder private func goals(_ all: [Goal]?) -> some View {
        if let all {
            let on = all.filter { $0.status == "active" }
            if on.isEmpty { Text("No active goals.").font(.system(size: 13)).foregroundStyle(muted) }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: mac ? 170 : 140), spacing: 6)], alignment: .leading, spacing: 6) {
                ForEach(on) { g in
                    NavigationLink { GoalDetail(goal: g) } label: {
                        HStack(spacing: 9) {
                            GoalEmblemView(emblem: g.emblem, size: 30)
                            Text(g.title).font(.system(size: 13.5, weight: .semibold)).lineLimit(2)
                                .multilineTextAlignment(.leading)
                            Spacer(minLength: 0)
                        }
                        .padding(.vertical, 4)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
        } else {
            Text("Couldn't load goals").font(.system(size: 13)).foregroundStyle(.red)
        }
    }

    /// A section per group — the console's cfgSourcesHTML. Groups sharing a
    /// tag are one section; the hub's group order is the section order,
    /// inside a section by name.
    private struct Section: Identifiable {
        var tag: String
        var color: String
        var refs: [SourceRef]
        var id: String { tag }
    }

    private func sections(_ groups: [SourceGroup]) -> [Section] {
        var out: [Section] = []
        for g in groups {
            let tag = g.tag ?? g.title
            let refs = g.sources.map { SourceRef(group: g, source: $0) }
            if let i = out.firstIndex(where: { $0.tag == tag }) { out[i].refs += refs }
            else { out.append(Section(tag: tag, color: g.color ?? "#64748B", refs: refs)) }
        }
        for i in out.indices { out[i].refs.sort { $0.source.title.localizedLowercase < $1.source.title.localizedLowercase } }
        return out.filter { !$0.refs.isEmpty }
    }

    @ViewBuilder private func sources(_ groups: [SourceGroup]) -> some View {
        let secs = sections(groups)
        if secs.isEmpty { Text("Nothing connected.").font(.system(size: 13)).foregroundStyle(muted) }
        if mac {
            // Side by side, each as wide as its cards want, so a group of one
            // shares a row instead of taking it (app.css .sgroups).
            SectionFlow(unit: 215, inner: 8, gap: 16, rowGap: 14) {
                ForEach(secs) { section($0).layoutValue(key: SectionCards.self, value: min($0.refs.count, 4)) }
            }
        } else {
            VStack(alignment: .leading, spacing: 14) { ForEach(secs) { section($0) } }
        }
    }

    private func section(_ s: Section) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(s.tag).font(.system(size: 12.5, weight: .semibold)).foregroundStyle(Color(hex: s.color))
                Text("\(s.refs.count)").font(.system(size: 11.5)).foregroundStyle(muted)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: mac ? 215 : 160), spacing: 8)], spacing: 8) {
                ForEach(s.refs) { r in
                    NavigationLink { SourceDetail(source: r.source, group: r.group.tag ?? r.group.title) } label: { card(r) }
                        .buttonStyle(.plain)
                }
            }
        }
    }

    private func card(_ r: SourceRef) -> some View {
        let s = r.source
        let c = Color(hex: r.group.color ?? "#64748B")
        let bad = s.status == "failing"
        let facts = ["\(s.total.formatted()) rows", s.last.map { configDay($0) }].compactMap { $0 }.joined(separator: " · ")
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                BrandTile(brand: s.brand, size: 32)
                VStack(alignment: .leading, spacing: 1) {
                    Text(s.title).font(.system(size: 14, weight: .semibold)).lineLimit(1)
                    Text(bad ? SourceRow(source: s).failBits : s.shortTo)
                        .font(.system(size: 12)).foregroundStyle(bad ? Color.red : muted).lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            Text(facts).font(.system(size: 12)).foregroundStyle(muted).lineLimit(1)
        }
        .padding(EdgeInsets(top: 10, leading: 13, bottom: 9, trailing: 12))
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(panel, in: RoundedRectangle(cornerRadius: 10))
        .overlay(alignment: .leading) {
            UnevenRoundedRectangle(topLeadingRadius: 10, bottomLeadingRadius: 10).fill(c).frame(width: 3)
        }
        .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(bad ? Color.red.opacity(0.45) : line, lineWidth: 1) }
        .contentShape(RoundedRectangle(cornerRadius: 10))
    }
}

/// A provider's tile (the console's `.bmark`): its logo in the ink colour
/// when the hub sent one, else its letters, on its colour.
struct BrandTile: View {
    let brand: BrandMark?
    var size: CGFloat = 32
    var body: some View {
        Group {
            if let logo = brand?.logo, !logo.isEmpty {
                SVGPathShape(d: logo).fill(Color(hex: brand?.ink ?? "#FFFFFF"))
                    .frame(width: size * 0.56, height: size * 0.56)
                    .frame(width: size, height: size)
                    .accessibilityLabel(brand?.mark ?? "")
            } else {
                Text(brand?.mark ?? "·")
                    .font(.system(size: size * 0.4, weight: .heavy, design: .rounded))
                    .lineLimit(1).minimumScaleFactor(0.5)
                    .foregroundStyle(Color(hex: brand?.ink ?? "#FFFFFF"))
                    .padding(.horizontal, 4)
                    .frame(minWidth: size).frame(height: size)
            }
        }
        .background(Color(hex: brand?.color ?? "#64748B"),
                    in: RoundedRectangle(cornerRadius: size * 0.28, style: .continuous))
    }
}

extension Color {
    /// `#RRGGBB` from the hub (brand colours, group tags); anything else is
    /// the neutral slate.
    init(hex: String) {
        let s = hex.hasPrefix("#") ? String(hex.dropFirst()) : hex
        let v = s.count == 6 ? (UInt32(s, radix: 16) ?? 0x64748B) : 0x64748B
        self.init(red: Double((v >> 16) & 0xFF) / 255, green: Double((v >> 8) & 0xFF) / 255, blue: Double(v & 0xFF) / 255)
    }
}

/// How many cards a source section holds (capped at 4): its flex weight.
private struct SectionCards: LayoutValueKey { static let defaultValue = 1 }

/// The console's `.sgroups` flex-wrap: each section's basis is `n` cards
/// wide, sections fill a row while their bases fit, then the row's spare
/// width is shared out by `n` (flex-grow) so the row ends flush.
private struct SectionFlow: Layout {
    var unit: CGFloat, inner: CGFloat, gap: CGFloat, rowGap: CGFloat

    private func basis(_ n: Int) -> CGFloat { CGFloat(n) * unit + CGFloat(n - 1) * inner }

    /// Rows of (index, width).
    private func rows(_ subviews: Subviews, width: CGFloat) -> [[(Int, CGFloat)]] {
        var rows: [[Int]] = [[]]
        var used: CGFloat = 0
        for i in subviews.indices {
            let b = basis(subviews[i][SectionCards.self])
            if !rows[rows.count - 1].isEmpty, used + gap + b > width { rows.append([]); used = 0 }
            used += (rows[rows.count - 1].isEmpty ? 0 : gap) + b
            rows[rows.count - 1].append(i)
        }
        return rows.map { r in
            let ns = r.map { subviews[$0][SectionCards.self] }
            let spare = max(0, width - ns.map(basis).reduce(0, +) - gap * CGFloat(r.count - 1))
            let weight = CGFloat(ns.reduce(0, +))
            return zip(r, ns).map { ($0, min(width, basis($1) + spare * CGFloat($1) / weight)) }
        }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let w = proposal.width ?? 1000
        let h = rows(subviews, width: w).map { r in
            r.map { subviews[$0.0].sizeThatFits(ProposedViewSize(width: $0.1, height: nil)).height }.max() ?? 0
        }
        return CGSize(width: w, height: h.reduce(0, +) + rowGap * CGFloat(max(0, h.count - 1)))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for r in rows(subviews, width: bounds.width) {
            var x = bounds.minX, rowH: CGFloat = 0
            for (i, w) in r {
                let p = ProposedViewSize(width: w, height: nil)
                subviews[i].place(at: CGPoint(x: x, y: y), anchor: .topLeading, proposal: p)
                rowH = max(rowH, subviews[i].sizeThatFits(p).height)
                x += w + gap
            }
            y += rowH + rowGap
        }
    }
}
