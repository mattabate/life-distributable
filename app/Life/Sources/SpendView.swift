import SwiftUI
import Charts

/// The page is the model bar, the plan limits and the all-time day chart —
/// nothing sliced by time: no range picker and no period tiles. The
/// per-model split lives inside each day's bar.

/// One bar of the by-day chart: every day of the window, including the ones
/// nothing ran on — a $0 day is a fact about the window, not a missing bar.
struct SpendDay: Identifiable {
    var day: Date
    var usd: Double
    var messages: Int
    /// The day's dollars by model, dearest first (the hub's order).
    var models: [Bucket]
    var id: Date { day }
}

/// One segment of one day's bar, flattened for Swift Charts. The span is
/// explicit rather than left to the default stacking: it fixes the order
/// (slot 1 on the baseline in every bar). Segments sit flush (the owner
/// 2026-09-30: the 2pt surface gaps between thin models read as white bands).
/// `none` is the one-point accent hairline of a day nothing ran on.
struct SpendSlice: Identifiable {
    var day: Date
    var model: String
    var slot: Int
    var from: Double
    var to: Double
    var none = false
    var id: String { "\(day.timeIntervalSince1970)|\(model)" }
}

struct SpendView: View {
    @Environment(HubClient.self) private var hub
    /// The tapped bar. Nothing is selected until a tap — the page reads as
    /// a summary first.
    @State private var pickedDay: Date?
    @State private var load = HubLoad<SpendSummary>()
    private var summary: SpendSummary? { load.value }
    private var error: String? { load.error }
    @State private var loadedAt: Date?
    @State private var quota: Quota?
    @State private var quotaError: String?
    @State private var modelSetting: ModelSetting?
    /// Pushed from More: no stack of its own there, or the page gets two nav
    /// bars.
    var pushed = false

    var body: some View {
        if pushed { content } else { NavigationStack { content } }
    }

    /// Between the page's blocks. Desktop is tighter (the owner 2026-09-30: "too
    /// much white space" on the desktop Spend page).
    #if targetEnvironment(macCatalyst)
    static let gap: CGFloat = 12
    #else
    static let gap: CGFloat = 18
    #endif

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Self.gap) {
                #if targetEnvironment(macCatalyst)
                // The console's heading, on the page (the owner 2026-09-29: the
                // desktop is the web's layout); no bar above it.
                Text("Spend").font(.system(size: 17, weight: .semibold)).padding(.bottom, -6)
                #endif
                planLimits
                ErrorBanner(message: error)
                if let s = summary {
                    section("Spend by day") {
                        chart(s)
                            #if targetEnvironment(macCatalyst)
                            .webCard()
                            #endif
                            .chatAbout("Spend by day (all time)", on: "Spend",
                                           facts: "- every day since \(s.history.first?.key ?? "the first transcript"); the last \(min(14, s.history.count)) with spend, split by model:\n"
                                               + s.history.suffix(14).map { d in
                                                   "  - \(d.key): \(usd(d.usd)), \(d.messages) msgs"
                                                       + ((d.models ?? []).isEmpty ? "" : " (" + (d.models ?? []).map { "\(shortModel($0.key)) \(usd($0.usd))" }.joined(separator: ", ") + ")")
                                               }.joined(separator: "\n")
                                               + "\n- today: \(usd(s.today_usd)); last 30 days: \(usd(s.total_usd)) over \(s.messages) messages, by model:\n" + s.by_model.map { "  - \(shortModel($0.key)): \(usd($0.usd)), \($0.messages) msgs" }.joined(separator: "\n")
                                               + "\n- these are list-price estimates from local transcripts, not billed amounts",
                                           code: "app/Life/Sources/SpendView.swift (chart) ← hub GET /api/v1/spend/summary?days=30 history[].models")
                    }
                    if !s.unknown_models.isEmpty {
                        Text("Unpriced (assumed Opus rate): " + s.unknown_models.joined(separator: ", ")).font(.caption).foregroundStyle(.red)
                    }
                } else if error == nil {
                    ProgressView().frame(maxWidth: .infinity).padding(40)
                }
            }.padding()
        }
        .navigationTitle("Spend")
        .askButton()
        .toolbar {
            // A bare clock in the toolbar read as a random time; say what it is.
            if let loadedAt { Text("updated \(loadedAt.formatted(.dateTime.hour().minute().locale(Fmt.enUS)))").font(.caption).foregroundStyle(.secondary) }
        }
        #if targetEnvironment(macCatalyst)
        .toolbar(.hidden, for: .navigationBar)
        #endif
        .hubTask(load) {
            async let q: Void = loadQuota()
            let s = try await hub.spend()
            loadedAt = .now
            await q
            return s
        }
    }

    func loadQuota() async {
        do { quota = try await hub.quota(); quotaError = nil }
        catch { if !error.isCancellation { quotaError = error.localizedDescription } }
        modelSetting = try? await hub.modelSetting()
    }

    /// A tap on the bar's toggle. It sets what the NEXT new session starts
    /// on; nothing running moves.
    func setModel(_ id: String) async {
        do { modelSetting = try await hub.setModel(id); load.error = nil }
        catch { if !error.isCancellation { load.error = "Couldn't set the model: \(error.localizedDescription)" } }
    }

    // MARK: plan limits — every meter that can lock you out, then per-model spend

    /// Families whose own meter is full: the shared meters' remaining headroom
    /// cannot be spent on them (chat facts only).
    var lockedFamilies: [String] {
        (quota?.windows ?? []).compactMap { $0.utilization >= 100 ? $0.scope_model : nil }
    }

    @ViewBuilder var planLimits: some View {
        if let q = quota {
            if q.available {
                VStack(alignment: .leading, spacing: Self.gap) {
                    NextModelCard(model: q.next_model, reason: q.next_reason, setting: modelSetting) { pick in
                        Task { await setModel(pick) }
                    }
                    if q.windows.isEmpty {
                        Text("Anthropic reported no limit windows.").font(.caption).foregroundStyle(.secondary)
                    } else {
                        section("Limits") {
                            #if targetEnvironment(macCatalyst)
                            // One card, a hairline between windows (the
                            // console's .card.limits; the owner 2026-09-30: three
                            // stacked cards were "too much white space").
                            VStack(alignment: .leading, spacing: 10) {
                                ForEach(Array(q.windows.enumerated()), id: \.element.id) { i, w in
                                    if i > 0 { Rectangle().fill(Web.line).frame(height: 1) }
                                    QuotaWindowCard(w: w, lockedFamilies: lockedFamilies, bare: true)
                                }
                            }
                            .webCard()
                            #else
                            VStack(spacing: 12) {
                                ForEach(q.windows) { w in
                                    QuotaWindowCard(w: w, lockedFamilies: lockedFamilies)
                                }
                            }
                            #endif
                        }
                    }
                }
            } else {
                #if targetEnvironment(macCatalyst)
                // The console's `.card.err` under the Limits heading.
                section("Limits") {
                    Text("Plan usage unavailable: \(q.error ?? "")").foregroundStyle(.red).webCard()
                }
                #else
                VStack(alignment: .leading, spacing: 4) {
                    Text("Plan usage unavailable").font(.subheadline)
                    Text(q.error ?? "unknown").font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading).padding(12)
                .background(Color.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
                #endif
            }
        } else if let quotaError {
            ErrorBanner(message: quotaError)
        } else {
            ProgressView().frame(maxWidth: .infinity).padding(12)
        }
    }

    @ViewBuilder func section<C: View>(_ title: String, @ViewBuilder _ c: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            #if targetEnvironment(macCatalyst)
            WebHeading(title).padding(.horizontal, 4).padding(.bottom, 2)
            #else
            Text(title.uppercased()).font(.caption).foregroundStyle(.secondary).kerning(0.5)
            #endif
            c()
        }
    }

    /// A model's colour slot is the hub's `palette` index — a property of the
    /// model, so 7d and 1y paint Opus the same blue; past the eighth slot, or
    /// unlisted, it draws grey. Same table as the console's `spendColor`.
    func slot(_ s: SpendSummary, _ model: String) -> Int {
        s.palette.firstIndex(of: model) ?? 99
    }

    /// The tapped day as a card: the split by model, dearest first, each with
    /// its dollars and its share of the day (the owner 2026-09-17: "hold the bar on
    /// the phone, I see the breakdown in model spend for the day — '200 to
    /// Fable, 300 to Fable 5.1'"); the key under the chart names the colours
    /// (the owner 2026-09-30, "put the models under the chart"). Same rows and words as the console's
    /// `spendDayTip` (views/money.js).
    func dayTip(_ s: SpendSummary, _ i: Int, _ days: [SpendDay], today: Date) -> TipSpec {
        let d = days[i]
        let isToday = d.day == today
        let peak = days.map(\.usd).max() ?? 0
        let rows = d.models.map { m in
            TipRow(color: MixSeries.color(slot(s, m.key)), value: usd(m.usd), key: shortModel(m.key),
                   trail: d.usd > 0 ? tipPct(m.usd / d.usd) : nil)
        }
        return TipSpec(
            title: tipDay(d.day),
            sub: d.usd > 0 ? usd(d.usd) + " spent · " + commas(d.messages) + (d.messages == 1 ? " message" : " messages") : nil,
            rows: rows,
            foot: isToday ? "today, and the day is not over"
                : d.usd == 0 ? "nothing ran that day"
                : d.usd == peak ? "the most expensive day so far" : nil)
    }

    func chart(_ s: SpendSummary) -> some View {
        // All-time: every day from the first transcript to today (a fixed
        // window hides the earliest days). The hub buckets by Eastern day,
        // so "today" is Eastern (the console's easternFields) and the days are
        // walked in the same local-day formatter the keys are parsed with; a
        // device in another zone would otherwise shift every bar by one.
        let byKey = Dictionary(uniqueKeysWithValues: s.history.map { ($0.key, $0) })
        let todayKey = Fmt.easternDay.string(from: .now)
        let firstKey = s.history.first?.key ?? todayKey
        let points: [SpendDay] = {
            guard let first = parseDay(firstKey), let last = parseDay(todayKey) else { return [] }
            let n = max(1, (Calendar.current.dateComponents([.day], from: first, to: last).day ?? 0) + 1)
            return (0..<n).compactMap { i in
                guard let d = Calendar.current.date(byAdding: .day, value: i, to: first) else { return nil }
                let b = byKey[dayString(d)]
                return SpendDay(day: d, usd: b?.usd ?? 0, messages: b?.messages ?? 0, models: b?.models ?? [])
            }
        }()
        let today = points.last?.day ?? Calendar.current.startOfDay(for: .now)
        let hit = tipIndex(points.map(\.day), pickedDay)
        // The console's bars (money.js spendDays, app.css .daycol), in points
        // of a 120pt plot: each segment exactly its share of the dearest day,
        // flush, slot 1 on the baseline; bars 92% of their day so the gaps
        // between days are hairlines, not stripes. A day with nothing on it is
        // a one-point accent hairline. No y axis, as on the console.
        let plotH = 120.0
        let dearest = points.map(\.usd).max() ?? 0
        let top = dearest > 0 ? dearest : 1
        let perPt = top / plotH
        var slices: [SpendSlice] = []
        var tallest = top
        for p in points {
            if p.models.isEmpty {
                slices.append(SpendSlice(day: p.day, model: "", slot: 99, from: 0, to: perPt, none: true))
                continue
            }
            var base = 0.0
            for m in p.models.sorted(by: { slot(s, $0.key) < slot(s, $1.key) }) {
                let h = m.usd / top * plotH
                slices.append(SpendSlice(day: p.day, model: m.key, slot: slot(s, m.key),
                                         from: base * perPt, to: (base + h) * perPt))
                base += h
            }
            tallest = max(tallest, base * perPt)
        }
        // The models that drew a bar, in stack order: the key under the chart;
        // the grey ones past the palette share one "other" (money.js spendKey).
        let seen = Set(points.flatMap { $0.models.map(\.key) })
        var keys: [(slot: Int, name: String)] = seen.filter { slot(s, $0) < 8 }
            .sorted { slot(s, $0) < slot(s, $1) }.map { (slot(s, $0), shortModel($0)) }
        if seen.contains(where: { slot(s, $0) >= 8 }) { keys.append((99, "other")) }
        return VStack(alignment: .leading, spacing: 6) {
            Chart {
                // A zero-height mark per day keeps every day of the window on
                // the axis — a $0 day is a fact about the window, not a gap.
                ForEach(points) { p in
                    BarMark(x: .value("Day", p.day, unit: .day), y: .value("USD", 0)).foregroundStyle(.clear)
                }
                ForEach(slices) { sl in
                    BarMark(x: .value("Day", sl.day, unit: .day), yStart: .value("From", sl.from), yEnd: .value("To", sl.to),
                            width: .ratio(0.92))
                        .foregroundStyle(sl.none ? Color.accentColor : MixSeries.color(sl.slot))
                }
            }
            .chartLegend(.hidden)
            .chartYScale(domain: 0...tallest)
            .chartYAxis(.hidden)
            .chartXAxis(.hidden)
            .frame(height: plotH)
            .tipTap(Date.self, id: "plot-spend-day") { pickedDay = $0 }
            HStack {
                Text(points.first.map { dayLabel($0.day) } ?? "")
                Spacer()
                Text("today")
            }
            .font(.caption).foregroundStyle(.secondary)
            // Centred under the chart like the console's key (the owner
            // 2026-10-01 12:42: "this legend, can we make it center aligned").
            FlowRow(spacing: 14, alignment: .center) {
                ForEach(keys, id: \.name) { k in
                    HStack(spacing: 5) {
                        RoundedRectangle(cornerRadius: 2).fill(MixSeries.color(k.slot)).frame(width: 10, height: 10)
                        Text(k.name)
                    }
                }
            }
            .font(.caption).foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .center)
            ChartTipSlot(spec: hit.map { dayTip(s, $0, points, today: today) })
        }
    }
}
