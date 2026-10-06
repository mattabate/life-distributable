import SwiftUI

/// The Calendar tab drawn as a real calendar — the phone half of
/// `hub/internal/server/web/views/calgrid.js` (surface parity: same API, same
/// numbers, same words, different rendering).
///
/// A real calendar UI in the manner of Google or Apple Calendar, with the
/// anytime tasks one tap away.
///
/// The phone has TWO modes, not the console's four: Day and Schedule, landing
/// on Schedule, no Today button. Week and Month are gone from here and only
/// here — seven columns
/// in 390pt is four legible characters a row — while `views/calgrid.js` keeps
/// all four on a screen wide enough for them. Parity is the same rows, the same
/// numbers and the same words, not the same rendering.
///
/// **Schedule is the agenda list exactly as it was** (the composer, Done ·
/// Won't do · Reopen, the folds), and it is where the tab opens. Both modes
/// read the same `GET /calendar` response and tap through to the same
/// `CalEntrySheet`, so a row can never say two things in two places.

enum CalMode: String, CaseIterable, Identifiable {
    case day, schedule
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
    var icon: String {
        switch self {
        case .day: "calendar.day.timeline.left"
        case .schedule: "list.bullet"
        }
    }
}

// MARK: - the calendars

/// FEW COLOURS: red for the owner's tasks, grey for agent actions, purple for
/// recommendations — easy to reduce to "what do I have to do today".
///
/// Every row belongs to one LANE, and the lane is both its colour
/// and the one switch that hides it: red = the owner's (a dated step, an ask, a
/// reminder, an approval still waiting on them), light blue = their HOMEWORK (a
/// study step), grey = the agents' (runs, check-ins, jobs, the trail of
/// decided actions), purple = an open rec — the minute it was filed, and the
/// day it comes back for review.
/// Gone with the six calendars: colour-by-goal, the eight goal colours, the
/// per-goal switches, the separate "Decided actions" calendar, and the "Done &
/// decided" switch that hid closed rows (a calendar always shows the past).
/// Same lanes, same colours, same words as `views/calgrid.js` — surface parity.
struct CalCal: Identifiable, Hashable {
    let key: String
    let label: String
    let color: Color
    /// A lane of the owner's: what "Just mine" keeps, what wears ○ when open.
    var owner = false
    var id: String { key }
}

enum CalCals {
    /// HOMEWORK, light blue. A `homework` item is the owner's step: the hub
    /// fires it as a physical ask, nags it, stacks it in Overdue, and they
    /// close it — so both lanes are `owner`. What differs is what the step is
    /// FOR: red unblocks an agent, blue teaches the owner something. Same
    /// `#039be5` as the console.
    /// SCHEDULED RUNS is its own lane, its own shade of grey. A
    /// scheduled run is a prompt an AGENT wrote to be delivered to a session
    /// at a minute — an `agent` item, or a one-shot prompt wake (a `run` row
    /// whose ref is `prompt:`). Blue-grey `#78909c`, apart from the plain
    /// grey of Agent runs, which keeps the standing check-ins, the hub's
    /// jobs, decided actions and the agents' record.
    /// CHORES is the sixth: a dated step of the owner's no session is behind
    /// — an errand, watering plants. Theirs (Just mine keeps it) but unblocking nothing, so orange `#e8710a`, apart from the
    /// red a session waits on. Same six as the console's `CAL_LANES`.
    static let all: [CalCal] = [
        CalCal(key: "mine", label: "My tasks", color: Color(red: 0.851, green: 0.188, blue: 0.145), owner: true),
        CalCal(key: "chores", label: "Chores", color: Color(red: 0.910, green: 0.443, blue: 0.039), owner: true),
        CalCal(key: "homework", label: "Homework", color: Color(red: 0.012, green: 0.608, blue: 0.898), owner: true),
        CalCal(key: "scheduled", label: "One-off runs", color: Color(red: 0.471, green: 0.565, blue: 0.612)),
        CalCal(key: "agents", label: "Recurring runs", color: Color(red: 0.373, green: 0.388, blue: 0.408)),
        CalCal(key: "recs", label: "Recs", color: Color(red: 0.557, green: 0.141, blue: 0.667)),
    ]
    private static func lane(_ k: String) -> CalCal { all.first { $0.key == k }! }
    static var mine: CalCal { lane("mine") }
    static var chores: CalCal { lane("chores") }
    static var homework: CalCal { lane("homework") }
    static var scheduled: CalCal { lane("scheduled") }
    static var agents: CalCal { lane("agents") }
    static var recs: CalCal { lane("recs") }
    /// The lanes "Just mine" switches off: everything that is not the owner's.
    static var notOwner: [CalCal] { all.filter { !$0.owner } }
    /// Two shades of the one purple, because a rec has a LIFE and both ends
    /// of it belong on the calendar (declined ones struck). Dark = still
    /// asking; light = answered. Same two colours as `CAL_REC_DONE` /
    /// `CAL_REC_INK` in `views/calgrid.js`.
    static let recDone = Color(red: 0.808, green: 0.576, blue: 0.847)
    static let recInk = Color(red: 0.290, green: 0.078, blue: 0.549)

    /// The row's lane is the hub's `lane` (calendar.go `stampEntry`, the one
    /// rule both surfaces draw: a rec purple its whole life, homework blue, a
    /// record its doer's, a pending approval the owner's). A row without one is grey —
    /// a new row must never dilute the red.
    static func of(_ e: CalEntry) -> CalCal { all.first { $0.key == e.lane } ?? agents }
    static func color(_ e: CalEntry) -> Color { of(e).color }
    /// An answered rec: the pale purple bar, dark ink on it.
    static func recDone(_ e: CalEntry) -> Bool { calIsRec(e) && e.did == true }
    /// An app build is its own teal cell wherever it is drawn — open in
    /// the Inbox, or as the "Installed: build N" mark in the strip. It stays in
    /// the owner's lane (it is their tap; Just mine keeps it): the teal is the cell's,
    /// as in the chat. `#0d9488`, `CAL_INSTALL` in `views/calgrid.js`.
    static let install = Color(red: 0.051, green: 0.580, blue: 0.533)
    /// The shade a bar, chip, mark or row icon is drawn in — its lane's
    /// colour, except the answered rec and the install cell. Same rule as
    /// `calShade` in `views/calgrid.js`.
    static func shade(_ e: CalEntry) -> Color { recDone(e) ? recDone : e.isInstall ? install : color(e) }
    static func ink(_ e: CalEntry) -> Color { recDone(e) ? recInk : .white }
    /// A closed row fades — except the answered rec, whose shade IS its
    /// "closed": a faded pale purple would be a third purple.
    static func fill(_ e: CalEntry, closed: Bool) -> Color { shade(e).opacity(closed && !recDone(e) ? 0.45 : 1) }
    /// "Closed" is the hub's `closed`: done, won't-do, accepted, a decided
    /// action, any record. A LOOK (✓ or ✕, faded), never a filter.
    static func isClosed(_ e: CalEntry) -> Bool { e.isClosed }
}

// MARK: - three forms, three places

/// THREE FORMS, THREE PLACES: what was done must read differently from what
/// was planned, and less cluttered. Colour alone could not carry it — everything was a bar in
/// one grid — so the SHAPE says which of three things a row is:
///
///   PLAN    — the grid, and only the grid: a time the owner or an agent CHOSE. A
///             closed item keeps its planned slot, ✓ and faded.
///   RECORD  — a strip of marks down the right edge of the day, at the minute
///             each thing happened; the owner's marks and the agents' in their own
///             halves of it. No words: the tap opens them.
///   RECS    — TWO BARS in the clock per rec: dark at the minute it was
///             filed, light at the minute it was answered (declined struck).
///             In a band above the clock they read as all-day events, so a
///             rec is
///             plan-shaped — a bar, never a strip mark — and only a rec with
///             no minute (a "Check back" on its review day) is an all-day chip.
///
/// A closed cal item is NOT a record row: its time is the plan's. Same three
/// predicates as `calIsRec` / `calIsRecord` / `calIsPlan` in `views/calgrid.js`.
let calItemKinds: Set<String> = ["owner", "homework", "agent", "note"]
/// An owner's step: the hub's `owner` and `homework` kinds — both fire a physical
/// ask, both close with words, both bold with a ○. Same as `calIsOwner` in
/// `views/calgrid.js`.
func calIsOwner(_ e: CalEntry) -> Bool { e.kind == "owner" || e.kind == "homework" }
func calIsRec(_ e: CalEntry) -> Bool { e.kind == "rec" }
func calIsRecord(_ e: CalEntry) -> Bool { e.did == true && !calIsRec(e) && !calItemKinds.contains(e.kind) }
func calIsPlan(_ e: CalEntry) -> Bool { !calIsRecord(e) }

/// What the filter sheet holds: which lanes are switched off. The same
/// switches the console's rail has, so the two surfaces can be set the same
/// way. A closed row always shows — the agenda has always kept a done row on
/// its day, ✓ and faded, and the "Done & decided" switch
/// that could hide it is gone; a stale `cal.showClosed` in
/// AppStorage is simply never read.
struct CalFilters {
    var off: Set<String> = []

    func shows(_ e: CalEntry) -> Bool { !off.contains(CalCals.of(e).key) }
    func color(_ e: CalEntry) -> Color { CalCals.color(e) }

    /// The one question the screen exists to answer, as one switch: every lane
    /// that is not the owner's off, or back on. All their lanes stay —
    /// homework is theirs to do as much as a task is.
    var mineOnly: Bool {
        get { CalCals.notOwner.allSatisfy { off.contains($0.key) } }
        set {
            for c in CalCals.notOwner { if newValue { off.insert(c.key) } else { off.remove(c.key) } }
        }
    }
}

// MARK: - local-day arithmetic (a YYYY-MM-DD is a local day, never UTC)

func calAddDays(_ day: String, _ n: Int) -> String {
    guard let d = parseDay(day), let x = Calendar.current.date(byAdding: .day, value: n, to: d) else { return day }
    return dayString(x)
}
/// "Tue 16" for an all-day row, "Tue 16 20:00" for a timed one — what a row
/// away from its day (the Overdue pile) needs to say about when it was due.
/// The console's `calWhenShort` in calgrid.js prints the same words.
func calWhenShort(_ e: CalEntry) -> String {
    guard let d = parseDay(e.day) else { return e.at ?? "" }
    if e.soon == true && !CalCals.isClosed(e) { return calAdded(e) }
    let day = d.formatted(.dateTime.weekday(.abbreviated).day())
    if let at = e.at, !at.isEmpty { return day + " " + at }
    return day
}
/// "added Sep 20" — what an open to-do says instead of a due day (the
/// console's `calAdded` prints the same words).
func calAdded(_ e: CalEntry) -> String {
    guard let d = parseDay(e.day) else { return "" }
    let c = Calendar.current.dateComponents([.month, .day], from: d)
    let months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
    return "added \(months[(c.month ?? 1) - 1]) \(c.day ?? 1)"
}
func calMinutes(_ at: String?) -> Int? {
    guard let at, at.count >= 4 else { return nil }
    let p = at.split(separator: ":")
    guard p.count == 2, let h = Int(p[0]), let m = Int(p[1]) else { return nil }
    return h * 60 + m
}
func calHHMM(_ m: Int) -> String { String(format: "%02d:%02d", m / 60, m % 60) }
let calDOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
func calWeekdayIndex(_ day: String) -> Int {
    guard let d = parseDay(day) else { return 0 }
    return Calendar.current.component(.weekday, from: d) - 1
}

// MARK: - what can be dragged, and why the rest cannot

enum CalMove: String {
    /// A real cal_item, still scheduled: PATCH day/at. One row, one date.
    case item
    /// A session's standing check-in — not an item at all, but a cadence
    /// PROJECTED onto a day, so moving one occurrence is meaningless and the
    /// drop rewrites the whole cadence (behind a confirm that says so).
    case run
    case none
}

/// The hub's `move` (a fired item moves too — the hub reads that drag as a
/// reschedule) and, for a fixed row, its `why`.
func calMovable(_ e: CalEntry) -> CalMove { CalMove(rawValue: e.move ?? "") ?? .none }

func calWhyStuck(_ e: CalEntry) -> String { e.why ?? "Not movable." }

/// daily@HH:MM keeps its cadence and takes the new time; weekly@Dow HH:MM
/// takes the dropped day's weekday too. An all-day drop has no time to give,
/// so it is refused rather than guessed.
func calNewCadence(_ repeatSpec: String, day: String, at: String) -> String {
    if at.isEmpty { return "" }
    if repeatSpec.hasPrefix("daily@") { return "daily@\(at)" }
    if repeatSpec.hasPrefix("weekly@") { return "weekly@\(calDOW[calWeekdayIndex(day)]) \(at)" }
    return ""
}

// MARK: - side-by-side layout

/// Where one block sits: minute of the day, which of `cols` columns it takes
/// inside its cluster — and everything the box swallowed. `members` is one
/// entry for a plain block; two or more is a collapsed pile drawn as one box
/// with a +N (see `calCollapse`).
struct CalPlaced: Identifiable {
    let members: [CalEntry]
    let start: Int
    /// Minute the box reaches down to: start + slot, stretched over the last
    /// member of a collapsed run.
    let end: Int
    let col: Int
    let cols: Int
    /// How many of `cols` the box covers, from `col` rightward: every
    /// column empty for its whole height, never fewer than one (the owner
    /// 2026-09-30: "the row should always be full if they've got an event
    /// at that time"). `span` in `calLayout`, views/calgrid.js.
    let span: Int
    var e: CalEntry { members[0] }
    var id: String { e.id }
}

let calSlotMinutes = 30
/// Two blocks overlapping by less than this sit side by side, not stacked —
/// `CAL_TOUCH_MIN` in views/calgrid.js.
let calTouchMinutes = 5

/// A lane's pile collapses to ONE box (a title plus +N) rather than a row of
/// unreadable slivers. The grouping never
/// crosses lanes — the owner's step keeps its own box beside the agents' — and it
/// chains through near-misses too: 10:00/10:15/10:30 in one lane, which used
/// to overlap illegibly, become one box spanning the run. The tap opens the
/// pile as a list, one row per member. Same rule as `calCollapse` in
/// `views/calgrid.js` — surface parity.
///
/// The record collapses one level up: did rows chain by lane AND session, and
/// reach two hours rather than thirty minutes — so the cards read and answered
/// over an afternoon in one session become one box wearing that session's
/// title. A chain never crosses lanes, so the owner's side of a session and the
/// agent's sit side by side, and each hides with its own switch.
let calDidReachMinutes = 120
/// A rec's two shades never chain into one box: a dark "filed" pile and a
/// light "answered" pile at the same hour are two different things.
func calChainKey(_ e: CalEntry) -> String {
    if calIsRec(e) { return CalCals.of(e).key + (e.did == true ? ":answered" : ":filed") }
    return CalCals.of(e).key + (e.did == true ? ":did:" + (e.thread_id ?? "") : "")
}
func calReach(_ e: CalEntry) -> Int { e.did == true && !calIsRec(e) ? calDidReachMinutes : calSlotMinutes }
/// A rec is a MOMENT, answered or not: its pile is one slot tall and holds
/// only the recs filed (or answered) inside that slot — a later member never
/// stretches the box or the chain. The record proper still chains.
func calChains(_ e: CalEntry) -> Bool { !calIsRec(e) }

func calCollapse(_ list: [CalEntry]) -> [(members: [CalEntry], s: Int, n: Int)] {
    let items = list.compactMap { e -> (CalEntry, Int)? in calMinutes(e.at).map { (e, $0) } }
        .sorted { $0.1 == $1.1 ? $0.0.id < $1.0.id : $0.1 < $1.1 }
    var groups: [(members: [CalEntry], s: Int, n: Int)] = []
    var open: [String: Int] = [:]   // chain key → index of the group still within reach
    var until: [Int: Int] = [:]     // group index → minute its chain still reaches to
    for (e, s) in items {
        let key = calChainKey(e)
        if let gi = open[key], s < until[gi] ?? 0 {
            groups[gi].members.append(e)
            if calChains(e) {
                groups[gi].n = s + calSlotMinutes     // the box ends at the last member…
                until[gi] = s + calReach(e)           // …the chain reaches further
            }
        } else {
            open[key] = groups.count
            until[groups.count] = s + calReach(e)
            groups.append((members: [e], s: s, n: s + calSlotMinutes))
        }
    }
    return groups
}

/// What a collapsed box is called: a session's run of steps wears the
/// session's title and "N steps"; any other pile, its first member and "+N".
/// Same words as `calGroupLabel` in `views/calgrid.js`.
func calGroupLabel(_ es: [CalEntry]) -> (title: String, badge: String) {
    // A pile of answered recs is "Accepted: X +1", not "2 steps in <session>":
    // the session form is the RECORD's, and a rec's answer is about the rec.
    if let first = es.first, !calIsRec(first), es.allSatisfy({ $0.did == true && $0.thread_id == first.thread_id }),
       let t = first.thread_title, !t.isEmpty {
        return (t, "\(es.count) steps")
    }
    return (es.first?.label ?? "", "+\(es.count - 1)")
}

/// Side-by-side layout, Google's rule — over the COLLAPSED groups, so at one
/// minute there is at most one box per lane, and only different lanes ever
/// share the width.
func calLayout(_ list: [CalEntry]) -> [CalPlaced] {
    var out: [CalPlaced] = []
    var cluster: [(members: [CalEntry], s: Int, n: Int, col: Int)] = []
    var clusterEnd = -1
    func flush() {
        guard !cluster.isEmpty else { return }
        var colsEnd: [Int] = []
        for i in cluster.indices {
            if let c = colsEnd.firstIndex(where: { $0 <= cluster[i].s }) {
                colsEnd[c] = cluster[i].n
                cluster[i].col = c
            } else {
                colsEnd.append(cluster[i].n)
                cluster[i].col = colsEnd.count - 1
            }
        }
        for it in cluster {
            var span = 1
            // Every column to the right that is empty for the box's height;
            // a brush under calTouchMinutes is not taken (views/calgrid.js).
            for c in (it.col + 1)..<colsEnd.count {
                if cluster.contains(where: { $0.col == c && min($0.n, it.n) - max($0.s, it.s) > calTouchMinutes }) { break }
                span += 1
            }
            out.append(CalPlaced(members: it.members, start: it.s, end: it.n, col: it.col, cols: colsEnd.count, span: span))
        }
        cluster = []
        clusterEnd = -1
    }
    for g in calCollapse(list) {
        if !cluster.isEmpty && g.s >= clusterEnd { flush() }
        cluster.append((g.members, g.s, g.n, 0))
        clusterEnd = max(clusterEnd, g.n)
    }
    flush()
    return out
}

// MARK: - the time grid (one day)

/// One day of the clock, the whole of the phone's grid. It
/// takes a single `day` rather than a list of them: a week of columns on a
/// 390pt screen is too cramped, and a view that cannot hold seven
/// columns should not carry the code for them.
struct CalTimeGrid: View {
    let day: String
    let today: String
    let entries: [CalEntry]
    let filters: CalFilters
    var onTap: (CalEntry) -> Void
    /// A collapsed pile tapped: the members, for the caller to list.
    var onTapGroup: ([CalEntry]) -> Void
    /// (entry, new time). The day is not a drop target any more — there is one
    /// column, so a drag can only change the clock; the arrows change the day.
    var onMove: (CalEntry, String) -> Void

    /// The in-flight move. `@GestureState`, not `@State`: the system CANCELS a
    /// gesture without calling `.onEnded` (the scroll steals it, or the finger
    /// lifts before the drag reports a change), and plain state set in
    /// `.onChanged` then survives forever — the hint capsule stuck on screen
    /// and the grid unscrollable. `@GestureState` resets itself on end AND on cancel.
    @GestureState private var dragState: CalDragState = .inactive

    private enum CalDragState {
        case inactive
        case pressing(id: String)
        case dragging(id: String, start: Int, translation: CGSize)
        var id: String? {
            switch self {
            case .inactive: nil
            case .pressing(let id): id
            case .dragging(let id, _, _): id
            }
        }
        var translation: CGSize {
            if case .dragging(_, _, let t) = self { return t }
            return .zero
        }
    }

    /// The capsule over the grid: an instruction while held, the landing
    /// time once dragged. Derived, so it can never outlive the gesture.
    private var dropLabel: String {
        switch dragState {
        case .inactive: ""
        case .pressing: "drag to move"
        case .dragging(_, let start, let tr): target(start: start, translation: tr)
        }
    }

    /// Taller than the console's row: one column has the width to spare, and
    /// the point of dropping Week was to stop reading 9-point text.
    static let hourH: CGFloat = 56
    static let gutterW: CGFloat = 46
    private static let chipH: CGFloat = 18

    private var shown: [CalEntry] { entries.filter(filters.shows) }
    /// Bars: the plan, and both ends of every rec.
    private var timed: [CalEntry] { shown.filter { calIsPlan($0) && calMinutes($0.at) != nil } }
    /// The band under the title: dated but untimed rows — a "Check back" rec
    /// on its review day, and a record if one ever arrives without a minute
    /// (nothing files one today).
    private var allDay: [CalEntry] { shown.filter { calMinutes($0.at) == nil } }
    /// The record strip.
    private var record: [CalEntry] { shown.filter { calIsRecord($0) && calMinutes($0.at) != nil } }
    private static let stripW: CGFloat = 22

    /// The hour the grid opens on: an hour before the first thing of the day,
    /// or 7 AM when the day has nothing timed in it. A day whose whole content
    /// is the record counts — else the owner's marks open off-screen.
    private var openingHour: Int {
        guard let m = (timed + record).compactMap({ calMinutes($0.at) }).min() else { return 7 }
        return max(0, m / 60 - 1)
    }

    /// The band is as tall as its chips, up to four of them — then it scrolls,
    /// rather than pushing the clock off the screen or hiding rows behind a
    /// "+N more" that has nowhere left to open.
    private var bandH: CGFloat { min(4, CGFloat(allDay.count)) * (Self.chipH + 2) }

    var body: some View {
        GeometryReader { geo in
            let colW = geo.size.width - Self.gutterW
            // The plan's column. The record column sits right of it on EVERY
            // day, empty on one still to come, so the all-day chips, every bar
            // and the now line share two edges whatever the day holds.
            let planW = colW - Self.stripW
            VStack(alignment: .leading, spacing: 0) {
                // No column head: the page title above already says
                // "Monday, Aug 31", and there is only the one day under it.
                band(planW)
                Divider()
                ScrollViewReader { proxy in
                    ScrollView {
                        ZStack(alignment: .topLeading) {
                            // One anchor per hour: what `scrollTo` aims at, and
                            // the only reason the grid does not open at midnight.
                            VStack(spacing: 0) {
                                ForEach(0..<24, id: \.self) { h in
                                    Color.clear.frame(width: 1, height: Self.hourH).id("h\(h)")
                                }
                            }
                            lines(width: geo.size.width)
                            blocks(colW: planW)
                            strip(x: Self.gutterW + planW)
                            // A minute tick, so the red line does not stand
                            // still until something else redraws the day.
                            if day == today {
                                TimelineView(.periodic(from: .now, by: 60)) { _ in
                                    // Stops at the strip: the clock is the plan's, not the record's.
                                    nowLine(colW: planW)
                                }
                            }
                        }
                        .frame(height: Self.hourH * 24, alignment: .topLeading)
                        .padding(.bottom, 90)
                    }
                    .scrollDisabled(dragState.id != nil)
                    // Only on appear — the parent gives this view a new identity
                    // when the DAY changes, so a reload (or a tap that opens
                    // the sheet) leaves the scroll exactly where it was.
                    .onAppear { proxy.scrollTo("h\(openingHour)", anchor: .top) }
                }
            }
            // The record column runs the whole height, band included, so it
            // reads as a margin of the page rather than a bar that starts
            // under the chips.
            .background(alignment: .trailing) {
                Rectangle().fill(Color.secondary.opacity(0.13))
                    .frame(width: Self.stripW)
                    // Its own left edge: without it a mark reads as a bar that
                    // ran off the end of the plan rather than as a separate margin.
                    .overlay(alignment: .leading) {
                        Rectangle().fill(Color.secondary.opacity(0.35)).frame(width: 0.5)
                    }
            }
        }
        .overlay(alignment: .top) {
            if !dropLabel.isEmpty {
                Text(dropLabel)
                    .font(.caption.weight(.semibold)).padding(.horizontal, 10).padding(.vertical, 6)
                    .background(.thinMaterial, in: Capsule()).padding(.top, 4)
            }
        }
        // The buzz that says "picked up, drag me" — once, when the hold lands.
        .sensoryFeedback(.impact(weight: .medium), trigger: dragState.id) { _, new in new != nil }
    }

    /// The all-day band: rows with a date but no time on it.
    /// Its label and chips take the hour labels' and the bars' edges.
    private func band(_ planW: CGFloat) -> some View {
        HStack(alignment: .top, spacing: 0) {
            Text("all-day").font(.caption2).foregroundStyle(.tertiary)
                .frame(width: Self.gutterW - 6, alignment: .trailing)
                .frame(width: Self.gutterW, alignment: .leading)
            ScrollView {
                VStack(spacing: 2) {
                    ForEach(allDay) { e in
                        CalChip(e: e)
                            .frame(height: Self.chipH)
                            .onTapGesture { onTap(e) }
                    }
                }
            }
            .scrollDisabled(allDay.count <= 4)
            // A bar's frame: 1 in from the gutter, 2 short of the strip.
            .frame(width: planW - 3, height: bandH, alignment: .top)
            .padding(.leading, 1)
        }
        .padding(.vertical, 3)
    }

    /// The record strip: one mark per pile at the minute it happened, the owner's in
    /// the left half and the agents' in the right, so "what was I doing then"
    /// and "what was it doing then" never sit on top of each other. No words —
    /// the tap opens them.
    /// The column itself is the page's background (`body`); this is the marks.
    private func strip(x: CGFloat) -> some View {
        ZStack(alignment: .topLeading) {
            ForEach(calLayout(record)) { p in
                let mine = CalCals.of(p.e).owner
                RoundedRectangle(cornerRadius: 4)
                    .fill(CalCals.shade(p.e).opacity(0.85))
                    .frame(width: 8, height: max(8, CGFloat(p.end - p.start) / 60 * Self.hourH - 2))
                    .offset(x: x + (mine ? 2 : 12), y: CGFloat(p.start) / 60 * Self.hourH)
                    .contentShape(Rectangle())
                    .onTapGesture { p.members.count > 1 ? onTapGroup(p.members) : onTap(p.e) }
                    .accessibilityLabel(Text(calGroupLabel(p.members).title))
                    .accessibilityAddTraits(.isButton)
            }
        }
    }

    private func lines(width: CGFloat) -> some View {
        ZStack(alignment: .topLeading) {
            ForEach(0..<24, id: \.self) { h in
                let y = CGFloat(h) * Self.hourH
                Rectangle().fill(Color.secondary.opacity(0.18)).frame(height: 0.5)
                    .offset(x: Self.gutterW, y: y)
                // Below its line, not centred on it: the grid opens scrolled to
                // an hour, and a label straddling that edge is sliced in half.
                Text(h == 0 ? "" : hourLabel(h)).font(.caption2).foregroundStyle(.secondary)
                    .frame(width: Self.gutterW - 6, alignment: .trailing)
                    .offset(x: 0, y: y + 2)
            }
        }
        .frame(width: width, height: Self.hourH * 24, alignment: .topLeading)
    }

    private func hourLabel(_ h: Int) -> String { "\((h + 11) % 12 + 1) \(h < 12 ? "AM" : "PM")" }

    private func nowLine(colW: CGFloat) -> some View {
        let c = Calendar.current.dateComponents([.hour, .minute], from: Date())
        let mins = CGFloat((c.hour ?? 0) * 60 + (c.minute ?? 0))
        return ZStack(alignment: .leading) {
            Circle().fill(Color.red).frame(width: 7, height: 7)
            Rectangle().fill(Color.red).frame(height: 1.5).padding(.leading, 3)
        }
        .frame(width: colW, alignment: .leading)
        .offset(x: Self.gutterW, y: mins / 60 * Self.hourH - 3)
    }

    private func blocks(colW: CGFloat) -> some View {
        ForEach(calLayout(timed)) { p in
            let w = (colW - 3) / CGFloat(p.cols)
            // Less the hairline, so 08:30 and 09:00 read as two blocks and not
            // one tall one. A collapsed pile's box spans first member to last.
            let h = max(18, CGFloat(p.end - p.start) / 60 * Self.hourH - 2)
            let group = p.members.count > 1
            let name = group ? calGroupLabel(p.members) : nil
            CalBlock(e: p.e, title: name?.title, badge: name?.badge,
                     glyph: group ? calGroupGlyph(p.members) : nil,
                     closed: p.members.allSatisfy(CalCals.isClosed),
                     overdue: p.members.contains { $0.overdue == true && !CalCals.isClosed($0) })
                .frame(width: w * CGFloat(p.span), height: h, alignment: .topLeading)
                .offset(x: Self.gutterW + CGFloat(p.col) * w + 1,
                        y: CGFloat(p.start) / 60 * Self.hourH)
                .offset(dragState.id == p.e.id ? dragState.translation : .zero)
                .opacity(dragState.id == p.e.id ? 0.85 : 1)
                .zIndex(dragState.id == p.e.id ? 2 : 1)
                .onTapGesture { group ? onTapGroup(p.members) : onTap(p.e) }
                // An immovable row masks the gesture off entirely — it must
                // not eat the hold-then-drag a scroll could have had. A
                // collapsed pile never moves: dragging five things is a guess.
                .gesture(moveGesture(p.e, start: p.start),
                         including: group || calMovable(p.e) == .none ? .none : .all)
        }
    }

    /// Long-press then drag, the gesture Apple Calendar uses to move an event.
    /// All transient UI lives in `dragState` via `.updating`; `.onEnded` only
    /// commits the move, so a cancelled gesture can strand nothing.
    private func moveGesture(_ e: CalEntry, start: Int) -> some Gesture {
        LongPressGesture(minimumDuration: 0.35)
            .sequenced(before: DragGesture(minimumDistance: 0, coordinateSpace: .local))
            .updating($dragState) { value, state, _ in
                switch value {
                case .first(true):
                    state = .pressing(id: e.id)
                case .second(true, let d):
                    // The sequence flips to .second the instant the hold
                    // lands; until the drag reports a change there is no
                    // translation and the label keeps saying what to do.
                    state = d.map { .dragging(id: e.id, start: start, translation: $0.translation) }
                        ?? .pressing(id: e.id)
                default:
                    state = .inactive
                }
            }
            .onEnded { value in
                guard calMovable(e) != .none, case .second(true, let d) = value, let tr = d?.translation else { return }
                let at = target(start: start, translation: tr)
                if at != (e.at ?? "") { onMove(e, at) }
            }
    }

    /// Where the finger let go: the quarter-hour it lines up with, clamped to
    /// the day.
    private func target(start: Int, translation: CGSize) -> String {
        let raw = Double(start) + Double(translation.height / Self.hourH * 60)
        return calHHMM(min(1440 - 15, max(0, Int((raw / 15).rounded()) * 15)))
    }
}

/// One glyph language on every row, both surfaces (✓ + strikethrough together
/// is ambiguous; ✕ is for things not completed). Each mark says one thing:
///   ○            still to do — an open row of the owner's. Full colour, never
///                struck: time passing never closes a task; only the owner or
///                an agent does.
///   ✓            done. Muted but NOT struck — a check crossed out reads as
///                its own opposite.
///   ✕ + strike   won't do / cleared. The only rows that get the line.
/// `views/calgrid.js` (calGlyph) must keep saying the same three things.
/// A record wears them too: what happened gets ✓ (read, decided, approved,
/// accepted, ran, scored); what was refused or fell over — skipped, declined,
/// denied, failed — gets ✕ and the line.
/// Which of the three a row wears is the hub's `mark` (store.CalMark —
/// "wont" | "done" | "todo" | nil), so no list of refusal states lives here.
func calGlyph(_ e: CalEntry) -> String {
    switch e.mark {
    case "wont": return "✕ "
    case "done": return "✓ "
    case "todo": return "○ "
    default: return ""
    }
}
/// The strikethrough: won't-do and its cousins only, per the vocabulary above.
func calStruck(_ e: CalEntry) -> Bool { e.mark == "wont" }
/// A pile's glyph is the pile's fate, not its first member's: ✕ only when
/// every step was refused, ✓ when every step is closed, else the first open
/// step's. A session box whose first read was skipped is not a skipped session.
/// `calGroupGlyph` in `views/calgrid.js` must say the same.
func calGroupGlyph(_ es: [CalEntry]) -> String {
    if es.allSatisfy(calStruck) { return "✕ " }
    if es.allSatisfy(CalCals.isClosed) { return "✓ " }
    return calGlyph(es.first { !CalCals.isClosed($0) } ?? es[0])
}
/// The amber ring an open, overdue row wears in the grid — the same signal as
/// the console's `.cal-ev.overdue` inset ring.
private let calOverdueAmber = Color(red: 0.95, green: 0.6, blue: 0.05)

/// One event in the time grid — or a collapsed pile of them, when a `badge`
/// is given: the pile's `title` (the first member, or the session a run of
/// record belongs to) plus the pill ("+N" / "N steps"), faded only when EVERY
/// member is closed, ringed when ANY open member is overdue.
/// Its shade is `CalCals.shade` — the lane's colour, or the pale purple of a
/// rec that has been answered, dark ink on it (`CalCals.ink`).
struct CalBlock: View {
    let e: CalEntry
    var title: String?
    var badge: String?
    var glyph: String?
    var closed: Bool
    var overdue: Bool
    init(e: CalEntry, title: String? = nil, badge: String? = nil, glyph: String? = nil, closed: Bool? = nil, overdue: Bool? = nil) {
        self.e = e
        self.title = title
        self.badge = badge
        self.glyph = glyph
        self.closed = closed ?? CalCals.isClosed(e)
        self.overdue = overdue ?? (e.overdue == true && !CalCals.isClosed(e))
    }
    var body: some View {
        #if targetEnvironment(macCatalyst)
        macBody
        #else
        phoneBody
        #endif
    }

    #if targetEnvironment(macCatalyst)
    /// The console's `.cal-ev.block` (the owner 2026-09-29: the desktop app is the
    /// web's layout, exactly): 11.5 regular on a 1.45 line, the time at .85 in
    /// tabular figures, 1×5 padding, radius 5, a 1px white-at-.35 edge; a
    /// closed bar fades whole to .5 (the answered rec keeps full strength —
    /// its pale shade is its "closed"), an overdue one wears the 2px amber ring.
    ///
    /// The box is its column's share and nothing more (the owner 2026-09-30, a
    /// four-wide Sunday morning whose "08:00" bars ran over each other and
    /// into the record strip — a `fixedSize` time and pill inside a
    /// `maxWidth: .infinity` frame grew the whole bar past its 37 points).
    /// So: time and title are ONE line that ellipsizes as the web's does
    /// ("08…" in a four-column share); the +N pill is drawn only when it
    /// fits whole and takes its width before the line does (the count is the
    /// point of a pile, the owner 2026-09-08); `CalendarWeek` clips the frame the
    /// way `overflow: hidden` does.
    private var macBody: some View {
        let ink = CalCals.ink(e)
        let words = Text((glyph ?? calGlyph(e)) + (title ?? e.label)).strikethrough(badge == nil && calStruck(e))
        let at = e.at ?? ""
        let line = at.isEmpty ? words : Text(at).monospacedDigit().foregroundColor(ink.opacity(0.85)) + Text(" ") + words
        return HStack(alignment: .firstTextBaseline, spacing: 4) {
            line.lineLimit(1).truncationMode(.tail).layoutPriority(-1)
            if let badge {
                ViewThatFits(in: .horizontal) {
                    Text(badge).font(.system(size: 11.5, weight: .bold)).lineLimit(1).fixedSize()
                        .padding(.horizontal, 4)
                        .background(ink.opacity(CalCals.recDone(e) ? 0.18 : 0.28), in: RoundedRectangle(cornerRadius: 8))
                    Color.clear.frame(width: 0, height: 0)
                }
            }
            Spacer(minLength: 0)
        }
        .font(.system(size: 11.5))
        .padding(.horizontal, 5).padding(.vertical, 1)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(CalCals.ink(e))
        .background(CalCals.shade(e), in: RoundedRectangle(cornerRadius: 5))
        .overlay {
            RoundedRectangle(cornerRadius: 5)
                .strokeBorder(CalCals.recDone(e) ? CalCals.recInk.opacity(0.25) : Color.white.opacity(0.35), lineWidth: 1)
        }
        .overlay {
            if overdue { RoundedRectangle(cornerRadius: 5).strokeBorder(calOverdueAmber, lineWidth: 2) }
        }
        .clipShape(RoundedRectangle(cornerRadius: 5))
        .opacity(closed && !CalCals.recDone(e) ? 0.5 : 1)
        .contentShape(RoundedRectangle(cornerRadius: 5))
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }
    #endif

    private var phoneBody: some View {
        HStack(spacing: 3) {
            if let at = e.at, !at.isEmpty {
                Text(at).font(.system(size: 10, weight: .semibold)).opacity(0.9).lineLimit(1).fixedSize()
            }
            // The title gives way first: in a five-column morning the time and
            // the "N steps" pill stay whole and the words truncate.
            Text((glyph ?? calGlyph(e)) + (title ?? e.label)).font(.system(size: 11, weight: .medium)).lineLimit(1)
                .strikethrough(badge == nil && calStruck(e))
                .layoutPriority(-1)
            if let badge {
                Text(badge).font(.system(size: 10, weight: .bold)).lineLimit(1).fixedSize()
                    .padding(.horizontal, 4).padding(.vertical, 0.5)
                    .background(CalCals.ink(e).opacity(CalCals.recDone(e) ? 0.18 : 0.28), in: Capsule())
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 3)
        .padding(.vertical, 2)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(CalCals.ink(e))
        .background(CalCals.fill(e, closed: closed), in: RoundedRectangle(cornerRadius: 4))
        .clipped()
        .overlay {
            if overdue {
                RoundedRectangle(cornerRadius: 4).strokeBorder(calOverdueAmber, lineWidth: 2)
            }
        }
        .contentShape(RoundedRectangle(cornerRadius: 4))
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }
}

/// One all-day chip, in the same shade and ink as a bar.
struct CalChip: View {
    let e: CalEntry
    var body: some View {
        #if targetEnvironment(macCatalyst)
        // The console's `.cal-ev.chip` (the owner 2026-09-29: the web's layout):
        // 11.5 regular on a 17 line, 1×5 padding, radius 5, faded whole to .5
        // when closed (never the answered rec), 2px amber when overdue.
        Text(calGlyph(e) + e.label)
            .strikethrough(calStruck(e))
            .font(.system(size: 11.5))
            .lineLimit(1)
            .padding(.horizontal, 5).padding(.vertical, 1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .foregroundStyle(CalCals.ink(e))
            .background(CalCals.shade(e), in: RoundedRectangle(cornerRadius: 5))
            .overlay {
                if e.overdue == true && !CalCals.isClosed(e) {
                    RoundedRectangle(cornerRadius: 5).strokeBorder(calOverdueAmber, lineWidth: 2)
                }
            }
            .opacity(CalCals.isClosed(e) && !CalCals.recDone(e) ? 0.5 : 1)
            .contentShape(Rectangle())
            .accessibilityAddTraits(.isButton)
        #else
        Text(calGlyph(e) + e.label)
            .strikethrough(calStruck(e))
            .font(.system(size: 11, weight: .medium))
            .lineLimit(1)
            .padding(.horizontal, 3).padding(.vertical, 1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .foregroundStyle(CalCals.ink(e))
            .background(CalCals.fill(e, closed: CalCals.isClosed(e)), in: RoundedRectangle(cornerRadius: 3))
            .overlay {
                if e.overdue == true && !CalCals.isClosed(e) {
                    RoundedRectangle(cornerRadius: 3).strokeBorder(calOverdueAmber, lineWidth: 1.5)
                }
            }
            .contentShape(Rectangle())
            .accessibilityAddTraits(.isButton)
        #endif
    }
}

// MARK: - the filter sheet (the console's left rail)

struct CalFiltersSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Binding var filters: CalFilters

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Toggle("Just mine", isOn: $filters.mineOnly)
                }
                Section {
                    ForEach(CalCals.all) { c in
                        Toggle(isOn: binding(c.key, in: \.off)) {
                            Label { Text(c.label).foregroundStyle(c.color) }
                            icon: { Circle().fill(c.color).frame(width: 10, height: 10) }
                        }
                    }
                } header: {
                    Text("Show")
                }
            }
            .navigationTitle("Show")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    /// A switch is ON when the key is NOT in the off-set.
    private func binding(_ key: String, in path: WritableKeyPath<CalFilters, Set<String>>) -> Binding<Bool> {
        Binding(get: { !filters[keyPath: path].contains(key) },
                set: { on in if on { filters[keyPath: path].remove(key) } else { filters[keyPath: path].insert(key) } })
    }
}
