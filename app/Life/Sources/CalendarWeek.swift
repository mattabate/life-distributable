#if targetEnvironment(macCatalyst)
import SwiftUI

/// The desktop app's Calendar: the console's page, not the phone's (the owner
/// 2026-09-29, on the first build's Day view: "the web app was only by
/// week… only by week, not the full calendar. And the thing that was useful
/// about the web app, was that on the side? It had the open action items…
/// and it had like the different categories which I could tick on and
/// off"). So: one week, Sunday to Saturday, beside the rail — mini month,
/// Show, Overdue, Due, Do soon, Sessions waiting, Anytime — in the words and
/// order of `views/calgrid.js` `calRailHTML`. The rows, colours, piles and
/// glyphs are the phone's (`CalendarGrid.swift`), which already match it.

/// Sunday on or before `day`.
func calWeekStart(_ day: String) -> String { calAddDays(day, -calWeekdayIndex(day)) }

/// The 6×7 block the mini month draws: Sunday on or before the 1st, 42 days.
func calMonthGridStart(_ day: String) -> String {
    guard let d = parseDay(day) else { return day }
    let c = Calendar.current.dateComponents([.year, .month], from: d)
    guard let first = Calendar.current.date(from: c) else { return day }
    return calWeekStart(dayString(first))
}

/// "September 27 – October 3, 2026" across a month, "September 6 – 12, 2026"
/// inside one — `calRangeLabel` in `views/calgrid.js`.
func calWeekLabel(_ start: String) -> String {
    let end = calAddDays(start, 6)
    guard let a = parseDay(start), let b = parseDay(end) else { return start }
    let cal = Calendar.current
    if cal.component(.month, from: a) == cal.component(.month, from: b) {
        return "\(a.formatted(.dateTime.month(.wide))) \(cal.component(.day, from: a)) – \(cal.component(.day, from: b)), \(cal.component(.year, from: b))"
    }
    return "\(a.formatted(.dateTime.month(.abbreviated).day())) – \(b.formatted(.dateTime.month(.abbreviated).day().year()))"
}

// MARK: - the rail

struct CalRail: View {
    let view: CalView?
    let anchor: String
    let today: String
    @Binding var filters: CalFilters
    var onJump: (String) -> Void
    var onOpen: (CalEntry) -> Void

    private var weekStart: String { calWeekStart(anchor) }

    var body: some View {
        let shows = filters.shows
        let all = (view?.anytime ?? []).filter(shows)
        let overdue = (view?.overdue ?? []).filter(shows)
        let due = (view?.due ?? []).filter(shows)
        let soon = (view?.soon ?? []).filter(shows)
        let waiting = all.filter { $0.item != true }
        let anytime = all.filter { $0.item == true }
        // The console's `.cal-rail`: 226 wide, no card behind it — it sits on
        // the grey page — each section 16 below the last (the owner 2026-09-29:
        // "the same exact layout within the desktop app").
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                mini
                section("Show", count: nil) {
                    ForEach(CalCals.all) { c in check(c) }
                }
                if !overdue.isEmpty { section("Overdue", count: overdue.count, color: calWebRed) { rows(overdue, dated: true) } }
                if !due.isEmpty { section("Due", count: due.count) { rows(due, dated: true) } }
                if !soon.isEmpty { section("Do soon", count: soon.count) { rows(soon, dated: true) } }
                if !waiting.isEmpty { section("Sessions waiting", count: waiting.count) { rows(waiting, dated: false) } }
                section("Anytime", count: anytime.count) { rows(anytime, dated: false) }
            }
            .padding(.bottom, 20)
        }
        .scrollIndicators(.never)
    }

    /// `.cal-railhead`: 11.5 caps, weight 700, tracking .04em, grey — the
    /// count beside it in the same colour at 600, untracked.
    private func section<C: View>(_ title: String, count: Int?, color: Color = Web.muted, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Text(title.uppercased()).font(.system(size: 11.5, weight: .bold)).tracking(0.46)
                if let count { Text("\(count)").font(.system(size: 11.5, weight: .semibold)) }
            }
            .foregroundStyle(color)
            VStack(alignment: .leading, spacing: 0) { content() }
        }
    }

    /// A lane's tick: its colour, its word, on or off — the console's checkbox.
    private func check(_ c: CalCal) -> some View {
        let on = !filters.off.contains(c.key)
        return Button {
            if on { filters.off.insert(c.key) } else { filters.off.remove(c.key) }
        } label: {
            // `.cal-check`: a 13.5 label in the lane's colour at 600, 3 above
            // and below, the box ticked in the lane's colour (accent-color).
            HStack(spacing: 8) {
                Image(systemName: on ? "checkmark.square.fill" : "square")
                    .foregroundStyle(on ? c.color : Web.lineStrong).font(.system(size: 14))
                Text(c.label).font(.system(size: 13.5, weight: .semibold)).foregroundStyle(c.color).lineLimit(1)
            }
            .padding(.vertical, 3)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// One row away from its cell: the lane's dot, the day it slipped from
    /// (`calWhenShort`) when `dated`, the title — `calRailRow`.
    private func rows(_ es: [CalEntry], dated: Bool) -> some View {
        ForEach(es) { e in
            Button { onOpen(e) } label: {
                // `.cal-ev.rail`: one 12.5 line, the dot, the day at .7 in
                // tabular figures, the title ellipsized — the tooltip has it whole.
                HStack(spacing: 5) {
                    Circle().fill(CalCals.shade(e)).frame(width: 7, height: 7)
                    if dated { Text(calWhenShort(e)).monospacedDigit().opacity(0.7).fixedSize() }
                    Text(mdPlain(e.title)).lineLimit(1).truncationMode(.tail)
                    Spacer(minLength: 0)
                }
                .font(.system(size: 12.5))
                .foregroundStyle(.primary)
                .padding(.vertical, 2)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(e.title)
        }
    }

    /// The mini month: the week on screen tinted, today ringed, a click jumps.
    private var mini: some View {
        let start = calMonthGridStart(anchor)
        let month = parseDay(anchor).map { Calendar.current.component(.month, from: $0) } ?? 0
        let counts = Dictionary((view?.days ?? []).map { ($0.day, $0.entries.filter(filters.shows).count) }, uniquingKeysWith: +)
        // `.cal-mini`: the month's name 12/600 on a 30 line, the weekday
        // letters 10.5 grey, the days 11.5 on 24-high cells; the week on
        // screen a square accent band, today a filled accent circle.
        return VStack(spacing: 0) {
            Text(parseDay(anchor)?.formatted(.dateTime.month(.wide).year()) ?? "")
                .font(.system(size: 12, weight: .semibold)).frame(maxWidth: .infinity).frame(height: 30)
                .padding(.bottom, 4)
            let cols = Array(repeating: GridItem(.flexible(), spacing: 0), count: 7)
            LazyVGrid(columns: cols, spacing: 0) {
                // The days are keyed by date, not 0–41: one grid, and ids
                // 0–6 shared with this header row dropped the month's first week.
                ForEach(Array(calDOW.enumerated()), id: \.offset) { _, d in
                    Text(String(d.prefix(1))).font(.system(size: 10.5)).foregroundStyle(Web.muted).padding(.vertical, 2)
                }
                ForEach((0..<42).map { calAddDays(start, $0) }, id: \.self) { day in
                    let inMonth = parseDay(day).map { Calendar.current.component(.month, from: $0) } == month
                    let inWeek = day >= weekStart && day <= calAddDays(weekStart, 6)
                    Button { onJump(day) } label: {
                        Text("\(parseDay(day).map { Calendar.current.component(.day, from: $0) } ?? 0)")
                            .font(.system(size: 11.5, weight: day == today ? .bold : .regular))
                            .foregroundStyle(day == today ? Color.white : inMonth ? Color.primary : Web.muted.opacity(0.55))
                            .frame(width: 24, height: 24)
                            .background { if day == today { Circle().fill(Web.accent) } }
                            .frame(maxWidth: .infinity)
                            .background(inWeek && day != today ? Web.accent.opacity(0.14) : .clear)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help("\(counts[day] ?? 0) on \(day)")
                }
            }
        }
    }
}

// MARK: - the week grid

/// Seven columns of the clock under one all-day band — `calTimeGridHTML`.
/// A bar drags to another day and time (a plain drag: a mouse has no scroll
/// to fight, so there is no hold first); the caller decides what a drop means.
struct CalWeekGrid: View {
    let start: String
    let today: String
    let byDay: [String: [CalEntry]]
    let filters: CalFilters
    var onTap: (CalEntry) -> Void
    var onTapGroup: ([CalEntry]) -> Void
    /// (entry, new day, new time).
    var onMove: (CalEntry, String, String) -> Void

    // The console's numbers (app.css `.cal-*`, calgrid.js): 44 an hour, a
    // 56 gutter, 17-line chips 2 apart, a 20-wide record strip.
    static let hourH: CGFloat = 44
    static let gutterW: CGFloat = 56
    private static let chipH: CGFloat = 19
    private static let stripW: CGFloat = 20
    private static let headH: CGFloat = 52

    @GestureState private var drag: (id: String, t: CGSize)? = nil

    private var days: [String] { (0..<7).map { calAddDays(start, $0) } }
    private func shown(_ d: String) -> [CalEntry] { (byDay[d] ?? []).filter(filters.shows) }
    private func timed(_ d: String) -> [CalEntry] { shown(d).filter { calIsPlan($0) && calMinutes($0.at) != nil } }
    private func band(_ d: String) -> [CalEntry] { shown(d).filter { calMinutes($0.at) == nil } }
    private func record(_ d: String) -> [CalEntry] { shown(d).filter { calIsRecord($0) && calMinutes($0.at) != nil } }
    /// The strip is reserved across the whole week or none of it.
    private var strip: Bool { days.contains { !record($0).isEmpty } }
    private var openingHour: Int {
        let ms = days.flatMap { timed($0) + record($0) }.compactMap { calMinutes($0.at) }
        // Open near the morning: the earliest thing, but never past 8 AM.
        guard let m = ms.min() else { return 7 }
        return max(0, min(8, m / 60 - 1))
    }

    var body: some View {
        GeometryReader { geo in
            let colW = (geo.size.width - Self.gutterW) / 7
            VStack(spacing: 0) {
                heads(colW)
                Web.line.frame(height: 1)
                allDay(colW)
                Web.lineStrong.frame(height: 1)
                ScrollViewReader { proxy in
                    ScrollView {
                        HStack(alignment: .top, spacing: 0) {
                            hours
                            ForEach(days, id: \.self) { d in column(d, colW: colW) }
                        }
                        .frame(height: Self.hourH * 24, alignment: .top)
                        .padding(.bottom, 12)
                    }
                    .onAppear { proxy.scrollTo("h\(openingHour)", anchor: .top) }
                    .onChange(of: start) { _, _ in proxy.scrollTo("h\(openingHour)", anchor: .top) }
                }
            }
        }
        // `.cal-grid`: one white card, a 1px hairline at radius 10.
        .background(Web.panel)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Web.line, lineWidth: 1))
    }

    /// `.cal-dhead`: 52 high, the weekday 10.5 caps at .05em, the date 19 in
    /// a 28 circle — filled accent, white, 600 on today; a hairline left of each.
    private func heads(_ colW: CGFloat) -> some View {
        HStack(spacing: 0) {
            Color.clear.frame(width: Self.gutterW, height: 1)
            ForEach(days, id: \.self) { d in
                let isToday = d == today
                VStack(spacing: 0) {
                    Text(calDOW[calWeekdayIndex(d)].uppercased()).font(.system(size: 10.5)).tracking(0.5)
                        .foregroundStyle(isToday ? Web.accent : Web.muted)
                    Text("\(parseDay(d).map { Calendar.current.component(.day, from: $0) } ?? 0)")
                        .font(.system(size: 19, weight: isToday ? .semibold : .regular))
                        .foregroundStyle(isToday ? Color.white : .primary)
                        .frame(width: 28, height: 28)
                        .background { if isToday { Circle().fill(Web.accent) } }
                }
                .padding(.top, 5)
                .frame(width: colW, height: Self.headH, alignment: .top)
                .overlay(alignment: .leading) { Web.line.frame(width: 1) }
            }
        }
    }

    /// `.cal-allday`: the band is as tall as its fullest day (21 a chip + 8),
    /// never a box of its own that scrolls — the console grows it the same
    /// way. "All day" sits right in the gutter at 12.5, grey.
    private func allDay(_ colW: CGFloat) -> some View {
        let rows = max(1, days.map { band($0).count }.max() ?? 0)
        let h = CGFloat(rows) * (Self.chipH + 2) + 8
        return HStack(alignment: .top, spacing: 0) {
            Text("All day").font(.system(size: 12.5)).foregroundStyle(Web.muted)
                .padding(.horizontal, 6).padding(.top, 4)
                .frame(width: Self.gutterW, alignment: .trailing)
            ForEach(days, id: \.self) { d in
                VStack(spacing: 2) {
                    ForEach(band(d)) { e in
                        CalChip(e: e).frame(height: Self.chipH).onTapGesture { onTap(e) }.help(e.title)
                    }
                }
                .padding(.horizontal, 3).padding(.top, 3).padding(.bottom, 1)
                .frame(width: colW, alignment: .top)
                .frame(maxHeight: .infinity, alignment: .top)
                .overlay(alignment: .leading) { Web.line.frame(width: 1) }
            }
        }
        .frame(minHeight: h, alignment: .top)
        .fixedSize(horizontal: false, vertical: true)
    }

    private var hours: some View {
        VStack(alignment: .trailing, spacing: 0) {
            ForEach(0..<24, id: \.self) { h in
                // `.cal-hour span`: 10.5 grey, 6 in from the right, 7 above its line.
                Text(h == 0 ? "" : "\((h + 11) % 12 + 1) \(h < 12 ? "AM" : "PM")")
                    .font(.system(size: 10.5)).foregroundStyle(Web.muted)
                    .padding(.trailing, 6)
                    .frame(width: Self.gutterW, height: Self.hourH, alignment: .topTrailing)
                    .offset(y: -7)
                    .id("h\(h)")
            }
        }
    }

    private func column(_ d: String, colW: CGFloat) -> some View {
        let planW = colW - (strip ? Self.stripW : 0)
        return ZStack(alignment: .topLeading) {
            if d == today { Web.accent.opacity(0.05) }
            ForEach(0..<24, id: \.self) { h in
                Web.line.frame(height: 1).offset(y: CGFloat(h) * Self.hourH)
            }
            if strip {
                // `.cal-strip`: grey at 13% with its own hairline on the left.
                Web.muted.opacity(0.13).frame(width: Self.stripW)
                    .overlay(alignment: .leading) { Web.line.frame(width: 1) }
                    .offset(x: planW)
                marks(d, x: planW)
            }
            blocks(d, day: d, planW: planW, colW: colW)
            if d == today {
                TimelineView(.periodic(from: .now, by: 60)) { _ in nowLine(planW) }
            }
        }
        .frame(width: colW, height: Self.hourH * 24, alignment: .topLeading)
        .overlay(alignment: .leading) { Web.line.frame(width: 1) }
    }

    /// `calBlockHTML`: a column's share of the plan, 1 in and 3 short; one
    /// slot (22) tall, a pile as tall as its run — never under 20.
    private func blocks(_ d: String, day: String, planW: CGFloat, colW: CGFloat) -> some View {
        ForEach(calLayout(timed(d))) { p in
            let w = planW / CGFloat(p.cols)
            let h = max(20, CGFloat(p.end - p.start) / 60 * Self.hourH)
            let group = p.members.count > 1
            let name = group ? calGroupLabel(p.members) : nil
            let moving = drag?.id == p.e.id
            CalBlock(e: p.e, title: name?.title, badge: name?.badge,
                     glyph: group ? calGroupGlyph(p.members) : nil,
                     closed: p.members.allSatisfy(CalCals.isClosed),
                     overdue: p.members.contains { $0.overdue == true && !CalCals.isClosed($0) })
                // `span` columns wide: a box fills every column that is empty
                // beside it (calLayout; the owner 2026-09-30: "the row should always
                // be full if they've got an event at that time").
                .frame(width: max(4, w * CGFloat(p.span) - 3), height: h, alignment: .topLeading)
                // `.cal-ev.block { overflow: hidden }`: a pile's +N pill can be
                // wider than a four-column share; it is cut at the edge, never
                // drawn over the next column or the strip (the owner 2026-09-30).
                .clipped()
                .offset(x: CGFloat(p.col) * w + 1, y: CGFloat(p.start) / 60 * Self.hourH)
                .offset(moving ? drag!.t : .zero)
                .opacity(moving ? 0.8 : 1)
                .zIndex(moving ? 3 : 1)
                .help(name.map { "\($0.title) · \($0.badge)" } ?? p.e.title)
                .onTapGesture { group ? onTapGroup(p.members) : onTap(p.e) }
                .gesture(DragGesture(minimumDistance: 6)
                    .updating($drag) { v, s, _ in s = (p.e.id, v.translation) }
                    .onEnded { v in
                        let n = Int((v.translation.width / colW).rounded())
                        let raw = Double(p.start) + Double(v.translation.height / Self.hourH * 60)
                        let at = calHHMM(min(1440 - 15, max(0, Int((raw / 15).rounded()) * 15)))
                        let to = calAddDays(day, n)
                        if to != day || at != (p.e.at ?? "") { onMove(p.e, to, at) }
                    },
                    including: group || calMovable(p.e) == .none ? .none : .all)
        }
    }

    /// The record strip: the owner's marks left, the agents' right, no words.
    private func marks(_ d: String, x: CGFloat) -> some View {
        ForEach(calLayout(record(d))) { p in
            let mine = CalCals.of(p.e).owner
            // `.cal-ev.mark`: 7 wide at .8, the owner's at 2, the agents' at 11.
            RoundedRectangle(cornerRadius: 4)
                .fill(CalCals.shade(p.e).opacity(0.8))
                .frame(width: 7, height: max(7, CGFloat(p.end - p.start) / 60 * Self.hourH - 2))
                .offset(x: x + (mine ? 2 : 11), y: CGFloat(p.start) / 60 * Self.hourH)
                .help(calGroupLabel(p.members).title)
                .onTapGesture { p.members.count > 1 ? onTapGroup(p.members) : onTap(p.e) }
        }
    }

    private func nowLine(_ w: CGFloat) -> some View {
        let c = Calendar.current.dateComponents([.hour, .minute], from: Date())
        let mins = CGFloat((c.hour ?? 0) * 60 + (c.minute ?? 0))
        // `.cal-now`: 2 of red, a 10 dot hanging off the column's left edge.
        return ZStack(alignment: .leading) {
            Rectangle().fill(calWebRed).frame(height: 2)
            Circle().fill(calWebRed).frame(width: 10, height: 10).offset(x: -5)
        }
        .frame(width: w, alignment: .leading)
        .offset(y: mins / 60 * Self.hourH - 5)
        .allowsHitTesting(false)
    }
}

// MARK: - the console's chrome, for this page

/// The console's `--red` (#dc2626): the now line and the Overdue heading.
let calWebRed = Color(red: 0.863, green: 0.149, blue: 0.149)

/// The console's `button.sm` (skin.css): 13 at 500, 5×11, radius 8, a 1px
/// `--line-strong` border on white — or, `primary`, filled accent with white
/// 600 ink; `icon` is the bar's borderless grey ‹ › at 20 (the owner 2026-09-29:
/// "the same exact layout" as the web).
struct CalWebButtonStyle: ButtonStyle {
    enum Kind { case plain, primary, icon }
    var kind: Kind = .plain
    /// A primary's fill other than the accent: the install cell's teal.
    var fill: Color? = nil
    @Environment(\.isEnabled) private var enabled
    func makeBody(configuration: Configuration) -> some View {
        let pressed = configuration.isPressed
        switch kind {
        case .icon:
            configuration.label.font(.system(size: 20)).foregroundStyle(pressed ? Color.primary : Web.muted)
                .padding(.horizontal, 8).padding(.vertical, 1)
                .background(pressed ? Web.code : .clear, in: RoundedRectangle(cornerRadius: 8))
                .contentShape(Rectangle())
        case .primary:
            configuration.label.font(.system(size: 13, weight: .semibold)).foregroundStyle(.white)
                .padding(.horizontal, 11).padding(.vertical, 5)
                .background((fill ?? Web.accent).opacity(pressed ? 0.85 : 1), in: RoundedRectangle(cornerRadius: 8))
                .opacity(enabled ? 1 : 0.5)
        case .plain:
            configuration.label.font(.system(size: 13, weight: .medium)).foregroundStyle(.primary)
                .padding(.horizontal, 11).padding(.vertical, 5)
                .background(pressed ? Web.code : Web.panel, in: RoundedRectangle(cornerRadius: 8))
                .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(Web.lineStrong, lineWidth: 1) }
                .opacity(enabled ? 1 : 0.5)
        }
    }
}
#endif
