import Charts
import SwiftUI

// MARK: - One tooltip, for every plot on the phone
//
// Every plot gets an informative tooltip, styled alike, whose content fits
// its plot.
//
// The console's half is `hub/internal/server/web/tip.js`; this is the same card
// in the phone's idiom, so a chart says the same words on both surfaces. Shape,
// fixed everywhere — which is what makes a dozen charts feel like one system:
//
//   title   the identity of the thing tapped — a day, a month, a year
//   sub     one muted line of context — the total the parts are shares of
//   rows    ONE ROW PER SERIES: a stroke of the series colour, then the VALUE
//           in bold, then the name in muted ink, then an optional trailing note
//   foot    what the number means, or where it came from
//
// Value first and bold, name after: in a legend the reader has the number and
// wants the series; in a tooltip they have the series and want the number. The
// colour is a 10×3 stroke, not a filled swatch — at this density a filled box
// is data-weight ink doing a label's job.
//
// Two things differ from the web, both because this is a phone:
//   - the card lives UNDER the plot (`ChartTipSlot`), never floating over it.
//     The mark is under a finger; a card on top of it hides the answer.
//   - selection is a TAP, not a hover or a drag. A drag gesture over a chart
//     inside a List eats the scroll, so the page stops moving under the finger
//     that meant to scroll past the chart.

/// One row: the series colour, the value, the series name, an optional trailing
/// note (a share, a count). `on` is the row that was tapped; `dim` is context.
struct TipRow {
    var color: Color?
    var value: String
    var key: String
    var trail: String?
    var on = false
    var dim = false
}

struct TipSpec {
    var title: String
    var sub: String?
    var rows: [TipRow] = []
    var foot: String?
}

/// The card itself. Sized by its content, left-aligned, and legible on the
/// grouped-list surface both light and dark.
struct TipCard: View {
    let spec: TipSpec

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(spec.title).font(.footnote).fontWeight(.semibold)
            if let s = spec.sub, !s.isEmpty {
                Text(s).font(.caption).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !spec.rows.isEmpty {
                VStack(alignment: .leading, spacing: 3) {
                    // Position is the identity: a card is rebuilt whole on every
                    // tap, and a fresh UUID per row redrew every row each time.
                    ForEach(spec.rows.indices, id: \.self) { row(spec.rows[$0]) }
                }
                .padding(.top, 6)
            }
            if let f = spec.foot, !f.isEmpty {
                Divider().padding(.top, 7).padding(.bottom, 6)
                Text(f).font(.caption2).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color(.secondarySystemGroupedBackground)))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color(.separator)))
        .accessibilityElement(children: .combine)
    }

    /// A row with no colour keeps the key column's width, so the values still
    /// line up under the ones that have a series beside them.
    private func row(_ r: TipRow) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Capsule().fill(r.color ?? .clear).frame(width: 10, height: 3)
            Text(r.value).font(.caption).fontWeight(.semibold).monospacedDigit()
            Text(r.key).font(.caption).foregroundStyle(r.on ? .primary : .secondary)
                .lineLimit(1).truncationMode(.tail)
            if let t = r.trail, !t.isEmpty {
                Spacer(minLength: 12)
                Text(t).font(.caption).monospacedDigit()
                    .foregroundStyle(r.on ? .primary : .secondary)
            }
        }
        .opacity(r.dim ? 0.62 : 1)
    }
}

/// Where the card sits: the row under the plot, empty until something is
/// tapped (no "tap the chart" caption — no explainer text).
struct ChartTipSlot: View {
    let spec: TipSpec?

    var body: some View {
        Group {
            if let s = spec { TipCard(spec: s) }
        }
        .listRowSeparator(.hidden)
    }
}

extension View {
    /// Tap-to-inspect for a chart: reads the x under the finger and hands back
    /// the plotted value there (a month key, a date), or nil when the tap
    /// landed off the plot. A TAP on purpose — see the note at the top.
    ///
    /// `id` names the overlay so a UI flow can tap the plot at all: XCUITest
    /// finds an element by its label, and a chart has none. A tap on the
    /// element's centre is a tap in the middle of the series, which is a real
    /// day on every chart here.
    func tipTap<V: Plottable>(_ type: V.Type, id: String = "plot",
                              pick: @escaping (V?) -> Void) -> some View {
        chartOverlay { proxy in
            GeometryReader { geo in
                if let plot = proxy.plotFrame {
                    Rectangle().fill(.clear).contentShape(Rectangle())
                        .gesture(SpatialTapGesture().onEnded { v in
                            pick(proxy.value(atX: v.location.x - geo[plot].origin.x, as: V.self))
                        })
                        // An element, not just an identifier: a clear rectangle
                        // with no label is not in the accessibility tree at
                        // all, so neither VoiceOver nor a UI flow can reach the
                        // one gesture that makes the card appear.
                        .accessibilityElement()
                        .accessibilityIdentifier(id)
                        .accessibilityLabel(Text(id.replacingOccurrences(of: "plot-", with: "")
                            .replacingOccurrences(of: "-", with: " ") + " chart"))
                        .accessibilityHint(Text("tap for the numbers behind a point"))
                        .accessibilityAddTraits(.isButton)
                }
            }
        }
    }
}

extension RuleMark {
    /// The tapped point, marked on the plot: the card names a date, and this is
    /// where that date is. The console draws the same crosshair under the pointer.
    func tipCrosshair() -> some ChartContent { tipCrosshair(Color(.separator)) }
    func tipCrosshair<S: ShapeStyle>(_ ink: S) -> some ChartContent {
        foregroundStyle(ink).lineStyle(StrokeStyle(lineWidth: 1))
    }
}

/// Two series side by side on one date — views beside visitors, GitHub's
/// pairs. `position(by:)` dodges them; without it Charts STACKS them on the
/// shared x, and 9 views with 3 visitors draws as a bar of 12. Colour them with
/// `chartForegroundStyleScale` on the series names.
struct PairedBars: ChartContent {
    let x: String
    let date: Date
    let unit: Calendar.Component
    let a: (label: String, n: Int)
    let b: (label: String, n: Int)

    var body: some ChartContent {
        BarMark(x: .value(x, date, unit: unit), y: .value(a.label, a.n))
            .foregroundStyle(by: .value("Series", a.label))
            .position(by: .value("Series", a.label))
        BarMark(x: .value(x, date, unit: unit), y: .value(b.label, b.n))
            .foregroundStyle(by: .value("Series", b.label))
            .position(by: .value("Series", b.label))
    }
}

extension View {
    /// The axes every line chart here draws: values on the trailing edge,
    /// `marks` ticks on each axis. With no `y`/`x` the stock marks; with them a
    /// grid line and the label in that wording. `byMonth` puts one x mark on
    /// each month.
    func lifeAxes(marks: Int = 3, y: ((Double) -> String)? = nil,
                  x: Date.FormatStyle? = nil, byMonth: Bool = false) -> some View {
        chartYAxis {
            if let y {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: marks)) { v in
                    AxisGridLine(); AxisValueLabel { if let d = v.as(Double.self) { Text(y(d)) } }
                }
            } else {
                // The stock marks, worded en-US whatever the device region is.
                AxisMarks(position: .trailing, values: .automatic(desiredCount: marks)) { v in
                    AxisGridLine(); AxisTick()
                    AxisValueLabel { if let d = v.as(Double.self) { Text(d == d.rounded() ? num(d) : fixed(d, 1)) } }
                }
            }
        }
        .chartXAxis {
            if byMonth {
                AxisMarks(values: .stride(by: .month)) { _ in
                    AxisGridLine(); AxisValueLabel(format: .dateTime.month(.abbreviated).locale(Fmt.enUS))
                }
            } else if let x {
                AxisMarks(values: .automatic(desiredCount: marks)) { _ in
                    AxisGridLine(); AxisValueLabel(format: x.locale(Fmt.enUS))
                }
            } else {
                AxisMarks(values: .automatic(desiredCount: marks)) { v in
                    AxisGridLine(); AxisTick()
                    if v.as(Date.self) != nil {
                        AxisValueLabel(format: .dateTime.month(.abbreviated).day().locale(Fmt.enUS))
                    } else {
                        AxisValueLabel()
                    }
                }
            }
        }
    }
}

/// A date axis's wording for how much time it spans: "Mar 24" past ~13
/// months, "Mar" for a monthly series, else "Mar 5".
func axisDateFormat(span: TimeInterval, monthly: Bool) -> Date.FormatStyle {
    let f: Date.FormatStyle = span > 400 * 86400 ? .dateTime.month(.abbreviated).year(.twoDigits)
        : monthly ? .dateTime.month(.abbreviated) : .dateTime.month(.abbreviated).day()
    return f.locale(Fmt.enUS)
}

/// The console's `niceAxis` (plot.js), number for number: the range padded 6%
/// each side, a step from 1/1.5/2/2.5/3/4/5/7.5/10 × a power of ten, at most
/// nine ticks. A flat series (hi ≤ lo) is widened 10% around its value first.
func niceAxis(lo: Double, hi: Double) -> (lo: Double, hi: Double, ticks: [Double]) {
    var lo = lo.isFinite ? lo : 0, hi = hi.isFinite ? hi : 0
    if !(hi > lo) {
        let p = abs(hi) * 0.1 == 0 ? 1 : abs(hi) * 0.1
        lo = hi - p; hi = hi + p
    }
    let pad = (hi - lo) * 0.06
    lo -= pad; hi += pad
    let raw = (hi - lo) / 4
    let mag = pow(10, (log10(raw)).rounded(.down))
    let step = [1, 1.5, 2, 2.5, 3, 4, 5, 7.5, 10].map { $0 * mag }.first { $0 >= raw } ?? 10 * mag
    var ticks: [Double] = []
    var v = (lo / step).rounded(.up) * step
    while v <= hi && ticks.count < 9 {
        ticks.append(abs(v) < step / 1e6 ? 0 : v)
        v += step
    }
    return (lo, hi, ticks)
}

// MARK: - the words the cards share

/// The sample nearest the tapped x. A continuous scale answers with a date
/// BETWEEN two points, and a tooltip has to name a point that exists — the
/// console's crosshair snaps the same way.
func tipIndex(_ dates: [Date], _ x: Date?) -> Int? {
    guard let x, !dates.isEmpty else { return nil }
    return dates.indices.min { abs(dates[$0].timeIntervalSince(x)) < abs(dates[$1].timeIntervalSince(x)) }
}

/// "Sat, Sep 8, 2025" — the identity line of a dated card, the console's
/// `fullDay` (plotTipDefault's title). `short` is the console's `dayLabel`
/// ("Sep 8"), for a key inside a chart card; the console's "since" rows on the
/// money plots use `shortDay` (with the year) instead.
func tipDay(_ d: Date, short: Bool = false) -> String {
    short ? dayLabel(d) : fullDay(d)
}

/// A count with its sign kept, for "what changed" rows: +1,204 / −18 — the
/// console's `engDelta`.
func tipSigned(_ v: Double) -> String {
    let x = v.isFinite ? v : 0
    return (x < 0 ? "−" : "+") + num(abs(x))
}

func tipCount(_ v: Double) -> String { num(v) }

/// A fraction as a whole percent, `Math.round` first: 0.125 → "13%".
func tipPct(_ v: Double) -> String { "\(Int(jsRound(v * 100)))%" }

/// A change as a share of what it changed from — the trailing note on a "what
/// moved" row, the console's `pctDelta`: "+4.2%" under 10%, "+12%" at or over.
/// Nil when there is nothing to divide by, because "+$40 (+∞%)" is not a fact
/// about the day.
func tipPctOf(_ delta: Double, _ base: Double) -> String? {
    let s = pctDelta(delta, base)
    return s.isEmpty ? nil : s
}

/// The console's `tipDelta` (plot.js): the sign, then the formatter on the
/// size — "+$1,234", "−$31", "−$0.40".
func tipDelta(_ fmt: (Double) -> String, _ d: Double) -> String {
    let x = d.isFinite ? d : 0
    return (x >= 0 ? "+" : "−") + fmt(abs(x))
}

/// The comparison row every time-series card carries, the console's
/// `engVsRow`: this point against the one before it, the change and its whole
/// percent. Nil when there is no point before it — the first bar compares to
/// nothing, and a card that says "+0 vs nothing" is worse than one that says less.
func tipVsRow(_ cur: Double, _ prev: Double?, _ label: String, money: Bool = false) -> TipRow? {
    guard let prev else { return nil }
    let d = cur - prev
    let pct = pctDelta(d, prev, 0)
    return TipRow(color: nil, value: money ? tipDelta(usd, d) : tipSigned(d),
                  key: "vs " + label, trail: pct.isEmpty ? nil : pct, dim: true)
}

/// The categorical slots, validated light and dark against each surface — the
/// console's `--s1…--s8` and `--s-other`, same order. Dark is SELECTED (its
/// own steps off the same ramps), never an automatic flip of the light
/// values. Used by the Spend page's stacked day chart (slots from the hub's
/// `palette`).
enum MixSeries {
    private static let light: [UInt32] = [0x2a78d6, 0xeb6834, 0x1baf7a, 0xeda100, 0xe87ba4, 0x008300, 0x4a3aa7, 0xe34948]
    private static let dark: [UInt32] = [0x3987e5, 0xd95926, 0x199e70, 0xc98500, 0xd55181, 0x008300, 0x9085e9, 0xe66767]

    /// A series' colour is its slot in the hub's fixed order; anything past
    /// the eighth slot is "Other" grey.
    static func color(_ slot: Int) -> Color {
        guard slot >= 0, slot < light.count else { return pair(0x9aa1ad, 0x6b7280) }
        return pair(light[slot], dark[slot])
    }

    private static func pair(_ l: UInt32, _ d: UInt32) -> Color {
        Color(UIColor { $0.userInterfaceStyle == .dark ? rgb(d) : rgb(l) })
    }
    private static func rgb(_ hex: UInt32) -> UIColor {
        UIColor(red: Double((hex >> 16) & 0xff) / 255,
                green: Double((hex >> 8) & 0xff) / 255,
                blue: Double(hex & 0xff) / 255, alpha: 1)
    }
}
