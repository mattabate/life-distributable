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
/// (slot 1 on the baseline in every bar) and leaves the console's 2pt surface
/// gap between segments, which is what makes two adjacent colours read as two.
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

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                planLimits
                ErrorBanner(message: error)
                if let s = summary {
                    section("By day") {
                        chart(s).chatAbout("Spend by day (all time)", on: "Spend",
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
                VStack(alignment: .leading, spacing: 18) {
                    NextModelCard(model: q.next_model, reason: q.next_reason, setting: modelSetting) { pick in
                        Task { await setModel(pick) }
                    }
                    if q.windows.isEmpty {
                        Text("Anthropic reported no limit windows.").font(.caption).foregroundStyle(.secondary)
                    } else {
                        section("Limits") {
                            VStack(spacing: 12) {
                                ForEach(q.windows) { w in
                                    QuotaWindowCard(w: w, lockedFamilies: lockedFamilies)
                                }
                            }
                        }
                    }
                }
            } else {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Plan usage unavailable").font(.subheadline)
                    Text(q.error ?? "unknown").font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading).padding(12)
                .background(Color.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
            }
        } else if let quotaError {
            ErrorBanner(message: quotaError)
        } else {
            ProgressView().frame(maxWidth: .infinity).padding(12)
        }
    }

    @ViewBuilder func section<C: View>(_ title: String, @ViewBuilder _ c: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased()).font(.caption).foregroundStyle(.secondary).kerning(0.5)
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
    /// its dollars and its share of the day. No legend on the chart: this card is the
    /// legend, one day at a time. Same rows and words as the console's
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
        // at least 3pt; a 2pt surface gap at the foot of every segment but the
        // bottom one, drawn INSIDE its height so a column stays proportional to
        // its dollars; slot 1 on the baseline. A day with nothing on it is a
        // one-point accent hairline. No y axis, as on the console.
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
            for (j, m) in p.models.sorted(by: { slot(s, $0.key) < slot(s, $1.key) }).enumerated() {
                let h = max(3, m.usd / top * plotH)
                slices.append(SpendSlice(day: p.day, model: m.key, slot: slot(s, m.key),
                                         from: (base + (j == 0 ? 0 : 2)) * perPt, to: (base + h) * perPt))
                base += h
            }
            tallest = max(tallest, base * perPt)
        }
        return VStack(alignment: .leading, spacing: 6) {
            Chart {
                // A zero-height mark per day keeps every day of the window on
                // the axis — a $0 day is a fact about the window, not a gap.
                ForEach(points) { p in
                    BarMark(x: .value("Day", p.day, unit: .day), y: .value("USD", 0)).foregroundStyle(.clear)
                }
                ForEach(slices) { sl in
                    BarMark(x: .value("Day", sl.day, unit: .day), yStart: .value("From", sl.from), yEnd: .value("To", sl.to))
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
            ChartTipSlot(spec: hit.map { dayTip(s, $0, points, today: today) })
        }
    }
}
