import SwiftUI

// Configuration: how the sessions are powered, what the owner's goals are,
// what the hub is connected to. The fourth tab on every surface; the
// console's twin is views/config.js, block for block. Row 1 is Powered by
// beside Devices, row 2 the Team beside Goals, then the data sources:
//   Powered by: the plan (its price when the hub has found one), this
//     month's list-price spend, each limit's headroom, the model new sessions
//     start on. One block per provider, so a second one is a second block.
//   Devices: the hub's Mac (uptime, zone) and the phone (its app's profile).
//   Team: the org chart: the owner on top, a card per role reporting to
//     them, that role's reports listed inside, its goals' emblems on the card.
//   Goals: the active goals as small tiles under their sections, each the
//     door to its page.
//   Data sources: one card per source — the provider's tile, rows, the newest
//     row — edged in its group's colour. On the desktop one grid filled left
//     to right, a group's name over its first card; on the phone a section
//     per group (sources.go sourceTags). A card opens SourceDetail.

/// Everything the page reads, in one load. Only the sources are required;
/// the other blocks each draw their own failure.
struct ConfigData: Sendable {
    var quota: Quota?
    var model: ModelSetting?
    var goals: [Goal]?
    var sources: SourcesInventory
    var status: HubStatus?
    /// The roles (`GET /threads?persona=1`); empty with team mode off.
    var people: [Thread]?
    var settings: HubSettings?
}

/// The part of `GET /settings` this page reads: owner_name, hub.json's name
/// for the owner ("" when unset), the top of the org chart.
struct HubSettings: Codable, Sendable, Equatable {
    var owner_name: String = ""

    enum CodingKeys: String, CodingKey { case owner_name }

    init(owner_name: String = "") { self.owner_name = owner_name }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        owner_name = (try c.decodeIfPresent(String.self, forKey: .owner_name) ?? "").trimmingCharacters(in: .whitespaces)
    }

    /// The name on top of the org chart: owner_name, else You.
    var ownerLabel: String { owner_name.isEmpty ? "You" : owner_name }
}

/// The goals under the owner's section names, each section where its first
/// goal falls; unsectioned goals last, under "Other" when anything is
/// sectioned (the console's goalGroups).
struct GoalSection: Identifiable {
    var name: String
    var goals: [Goal]
    var id: String { name }
}

func goalSections(_ goals: [Goal]) -> [GoalSection] {
    var out: [GoalSection] = []
    var rest: [Goal] = []
    for g in goals {
        let name = (g.section ?? "").trimmingCharacters(in: .whitespaces)
        if name.isEmpty { rest.append(g); continue }
        if let i = out.firstIndex(where: { $0.name == name }) { out[i].goals.append(g) }
        else { out.append(GoalSection(name: name, goals: [g])) }
    }
    if !rest.isEmpty { out.append(GoalSection(name: out.isEmpty ? "" : "Other", goals: rest)) }
    return out
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
    /// The page's width: two blocks a row only when each gets ~440 pt.
    @State private var width: CGFloat = 1200
    private var wide: Bool { width >= 900 }

    @ViewBuilder private func pair<A: View, B: View>(@ViewBuilder _ a: () -> A, @ViewBuilder _ b: () -> B) -> some View {
        if wide {
            // One height, the taller one's, like the console's grid row.
            HStack(alignment: .top, spacing: 12) { a(); b() }
                .fixedSize(horizontal: false, vertical: true)
        } else {
            VStack(alignment: .leading, spacing: 12) { a(); b() }
        }
    }

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
                        // Two blocks a row; below ~900 pt (a half-screen
                        // window) one block a row.
                        pair {
                            block("Powered by") { plan(d) }
                        } _: {
                            block("Devices") { devices(d.status) }
                        }
                        pair {
                            block("Team") { orgTree(d) }
                        } _: {
                            block("Goals", opens: { GoalsView() }) { goals(d.goals) }
                        }
                    } else {
                        // The phone keeps its own page (Devices and the Team
                        // are desktop blocks for now).
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
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
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
            let st = try? await hub.status()
            let people = try? await hub.personaThreads()
            let set = try? await hub.settings()
            return ConfigData(quota: q, model: m, goals: g, sources: try await hub.sources(), status: st, people: people, settings: set)
        }
    }

    // MARK: devices

    /// The console's cfgDevicesHTML: the hub's Mac, then the phone, a row
    /// each with a hairline between.
    @ViewBuilder private func devices(_ s: HubStatus?) -> some View {
        if let s {
            let up = s.uptime_s
            let upWord = up >= 86400 ? "\(up / 86400)d" : up >= 3600 ? "\(up / 3600)h" : "\(up / 60)m"
            let macWord = ["up " + upWord, s.time_zone ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
            let phoneWord: String = {
                guard let a = s.app_installed, !a.isEmpty else { return "app not installed" }
                let left = s.app_profile_days_left.map { "profile \($0) \($0 == 1 ? "day" : "days") left" } ?? "app installed"
                let until = s.app_profile_expires.flatMap { $0.isEmpty ? nil : "until " + dayLabel(String($0.prefix(10))) }
                return [left, until].compactMap { $0 }.joined(separator: " · ")
            }()
            VStack(alignment: .leading, spacing: 9) {
                deviceRow("Mac · hub", macWord)
                Rectangle().fill(line).frame(height: 1)
                deviceRow("Phone", phoneWord)
            }
        } else {
            Text("Couldn't load devices").font(.system(size: 13)).foregroundStyle(.red)
        }
    }

    private func deviceRow(_ title: String, _ sub: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title).font(.system(size: 14, weight: .semibold))
            if !sub.isEmpty {
                Text(sub).font(.system(size: 12.5)).foregroundStyle(muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    // MARK: team

    /// The org chart, the console's cfgOrgHTML: the owner on top, a card per
    /// role under them (reports_to empty or unknown), that role's goals'
    /// emblems on the card's top line; a role's own reports are a list inside
    /// its card, so the block stays near Goals' height. A name opens the
    /// chat, an emblem the goal.
    @ViewBuilder private func orgTree(_ d: ConfigData) -> some View {
        if let all = d.people {
            var seen = Set<String>()
            let ps = all.filter { p in guard let k = p.persona, !k.isEmpty else { return false }; return seen.insert(k).inserted }
            let byId = Dictionary((d.goals ?? []).map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
            let goalsOf = { (p: Thread) in (p.owns_goals ?? []).compactMap { byId[$0] } }
            VStack(spacing: 0) {
                Text(d.settings?.ownerLabel ?? "You").font(.system(size: 14, weight: .semibold))
                    .padding(.horizontal, 16).padding(.vertical, 5)
                    .overlay { Capsule().strokeBorder(line, lineWidth: 1) }
                if !ps.isEmpty {
                    Rectangle().fill(line).frame(width: 1, height: 10)
                    Rectangle().fill(line).frame(height: 1).padding(.horizontal, 24)
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 160), spacing: 8, alignment: .top)], alignment: .leading, spacing: 8) {
                        ForEach(orgRows(ps, under: "", depth: 0).filter { $0.1 == 0 }.map(\.0)) { p in
                            orgCard(p, reports: orgRows(ps, under: p.persona ?? "", depth: 0), goalsOf: goalsOf)
                        }
                    }
                    .padding(.top, 10)
                }
            }
            .frame(maxWidth: .infinity)
        } else {
            Text("Couldn't load the team").font(.system(size: 13)).foregroundStyle(.red)
        }
    }

    private func orgCard(_ p: Thread, reports: [(Thread, Int)], goalsOf: @escaping (Thread) -> [Goal]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            // Name and emblems share the top line (the emblems overlap to
            // leave the name room); the role runs the card's width under both.
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .center, spacing: 6) {
                    NavigationLink { ThreadDetail(thread: p) } label: {
                        Text(p.title).font(.system(size: 13.5, weight: .semibold)).lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    emblems(goalsOf(p), size: 22)
                }
                if let r = p.role, !r.isEmpty {
                    NavigationLink { ThreadDetail(thread: p) } label: {
                        Text(r).font(.system(size: 11.5)).foregroundStyle(muted)
                            .lineLimit(1).minimumScaleFactor(0.85)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            ForEach(reports, id: \.0.id) { r, depth in
                HStack(alignment: .center, spacing: 6) {
                    NavigationLink { ThreadDetail(thread: r) } label: {
                        HStack(alignment: .firstTextBaseline, spacing: 5) {
                            Text(r.title).font(.system(size: 12.5, weight: .semibold)).fixedSize()
                            if let role = r.role, !role.isEmpty {
                                Text(role).font(.system(size: 11)).foregroundStyle(muted).lineLimit(1)
                            }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    Spacer(minLength: 0)
                    emblems(goalsOf(r), size: 18)
                }
                .padding(.leading, CGFloat(depth) * 10)
                .padding(.top, 5)
                .overlay(alignment: .top) { Rectangle().fill(line).frame(height: 1) }
            }
        }
        .padding(EdgeInsets(top: 9, leading: 11, bottom: 9, trailing: 9))
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(bg, in: RoundedRectangle(cornerRadius: 10))
        .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(line, lineWidth: 1) }
    }

    /// A role's goals as their emblems, each the door to the goal, stacked a
    /// third over the last, each ringed in the card's colour.
    private func emblems(_ gs: [Goal], size: CGFloat) -> some View {
        HStack(spacing: -size / 3) {
            ForEach(gs) { g in
                NavigationLink { GoalDetail(goal: g) } label: {
                    GoalEmblemView(emblem: g.emblem, size: size)
                        .background(Circle().fill(bg).padding(-1.5))
                }
                    .buttonStyle(.plain)
                    #if targetEnvironment(macCatalyst)
                    // .help never shows on Catalyst; the hover card does.
                    .macTip(TipSpec(title: g.title))
                    #else
                    .help(g.title)
                    #endif
            }
        }
    }

    /// Depth-first: each role, then the roles under it, in the hub's role
    /// order (the chief of staff first), as the console draws it. A
    /// reports_to that names no role is the owner.
    private func orgRows(_ ps: [Thread], under boss: String, depth: Int) -> [(Thread, Int)] {
        let keys = Set(ps.compactMap(\.persona))
        let kids = ps.filter { p in
            let r = p.reports_to ?? ""
            return (keys.contains(r) ? r : "") == boss
        }
        return kids.flatMap { [($0, depth)] + orgRows(ps, under: $0.persona ?? "", depth: depth + 1) }
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
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(panel, in: RoundedRectangle(cornerRadius: 12))
        .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(line, lineWidth: 1) }
    }

    @ViewBuilder private func plan(_ d: ConfigData) -> some View {
        if let q = d.quota, let p = q.plan {
            VStack(alignment: .leading, spacing: 10) {
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
                    // On the Mac the card always sits beside Goals, too narrow
                    // for bars on a row: one full-width bar per row instead,
                    // the console's .cfg-lims.
                    LazyVGrid(columns: mac ? [GridItem(.flexible())] : [GridItem(.adaptive(minimum: 230), spacing: 18)], alignment: .leading, spacing: mac ? 8 : 6) {
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
    // The hub's amber `warn` is a pace call that lights early; this is red:
    // the row's label, number and meter. No banner names it over the block.
    private static let lowLeft = 10
    private func left(_ w: QuotaWindow) -> Int { max(0, 100 - Int(jsRound(w.utilization))) }
    private func low(_ w: QuotaWindow) -> Bool { left(w) <= Self.lowLeft }

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
            ForEach(goalSections(on)) { grp in
            if !grp.name.isEmpty {
                // The console's div.goal-group.
                Text(grp.name).font(.system(size: 12.5, weight: .semibold)).foregroundStyle(muted).padding(.top, 4)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: mac ? 170 : 140), spacing: 6)], alignment: .leading, spacing: 6) {
                ForEach(grp.goals) { g in
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
            }
        } else {
            Text("Couldn't load goals").font(.system(size: 13)).foregroundStyle(.red)
        }
    }

    /// A section per group — the console's cfgSourcesHTML. Groups sharing a
    /// tag are one section; the hub's group order is the section order,
    /// inside a section by name. Each section is a row of its own on every
    /// surface: two goals side by side read as one list.
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

    /// One cell of the desktop's flow: every cell keeps the group's name line
    /// (shown on the group's first card only) so a row's cards line up.
    private struct Cell: Identifiable {
        var section: Section
        var ref: SourceRef
        var first: Bool
        var id: String { section.tag + "/" + ref.id }
    }

    @ViewBuilder private func sources(_ groups: [SourceGroup]) -> some View {
        let secs = sections(groups)
        if secs.isEmpty { Text("Nothing connected.").font(.system(size: 13)).foregroundStyle(muted) }
        if mac { flow(secs) } else {
            VStack(alignment: .leading, spacing: 14) { ForEach(secs) { section($0) } }
        }
    }

    /// The desktop: one grid filled left to right, the console's .sflow; the
    /// next group starts in the hole the last one left.
    private func flow(_ secs: [Section]) -> some View {
        let cells = secs.flatMap { s in s.refs.enumerated().map { Cell(section: s, ref: $1, first: $0 == 0) } }
        return LazyVGrid(columns: [GridItem(.adaptive(minimum: 215), spacing: 8)], alignment: .leading, spacing: 4) {
            ForEach(cells) { c in
                VStack(alignment: .leading, spacing: 5) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(c.section.tag).font(.system(size: 12.5, weight: .semibold)).foregroundStyle(Color(hex: c.section.color))
                        Text("\(c.section.refs.count)").font(.system(size: 11.5)).foregroundStyle(muted)
                    }
                    .lineLimit(1)
                    .opacity(c.first ? 1 : 0)
                    .accessibilityHidden(!c.first)
                    NavigationLink { SourceDetail(source: c.ref.source, group: c.ref.group.tag ?? c.ref.group.title) } label: { card(c.ref) }
                        .buttonStyle(.plain)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
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
        .frame(maxWidth: .infinity, alignment: .leading)
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
        // The goal's bar, clipped by the card's corners.
        .overlay(alignment: .leading) { Rectangle().fill(c).frame(width: 3) }
        .clipShape(RoundedRectangle(cornerRadius: 10))
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
