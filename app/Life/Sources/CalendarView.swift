import SwiftUI

/// One forward-looking agenda mixing the owner's own
/// dated steps, one-shot agent runs, reminders,
/// every session's standing check-ins and the scheduler's jobs — plus the
/// undated open asks as "anytime" work. A dated step becomes an ask on the
/// board the day it is due and the hub nags until it is closed, so nothing
/// slips past.
struct CalendarView: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.openURL) private var openURL
    @Environment(BoardStore.self) private var store
    @State private var cal = HubLoad<CalView>()
    private var view: CalView? { cal.value }
    /// The store's copy: RootView's one poll keeps it current.
    private var goals: [Goal] { store.goals }
    /// A failed move or close; the next good load clears it.
    @State private var actionError: String?
    private var error: String? { actionError ?? cal.error }
    @State private var selected: CalEntry?
    /// A `rec` row (a deferred rec on its review day) opens the rec's own page.
    @State private var openRec: Rec?
    /// A calendar id tapped in a card's text (Refs.swift) opens that item.
    @Environment(RefNav.self) private var nav
    @State private var showPast = false
    /// Closing one of the owner's own steps opens Respond: it closes with a
    /// message, never a bare swipe.
    @State private var respondTo: CalRespond?
    @State private var horizonDays = 60
    @State private var anytimeOpen = false
    @State private var expandedDays: Set<String> = []
    /// Day / Schedule, and only those two on the phone (wider views are too
    /// cramped); the tab lands on Schedule.
    /// Remembered so the tab comes back the way it was left, like the console.
    @AppStorage("cal.mode") private var modeRaw = CalMode.schedule.rawValue
    /// Which of the three lanes are switched off. An install from the
    /// six-calendar era has keys like "log"/"me"/"checkins" in here; none of
    /// them is a lane, so `CalFilters` drops them and every lane opens on
    /// rather than one staying invisible.
    @AppStorage("cal.off") private var offRaw = ""
    /// The day the grid is centred on once the owner moves off today; of the moment,
    /// so it is not persisted. nil = today.
    @State private var picked: String?
    /// TODAY IS THE HUB'S: the agenda's own
    /// `today`, computed once in the hub's zone, so the phone never rings a
    /// different day than the console. The device clock is only the first
    /// guess, before the first answer lands — a wrong guess reloads once.
    @State private var today = dayString(Date())
    private var anchor: String { picked ?? today }
    @State private var showFilters = false
    @State private var showAnytime = false
    /// The pile behind a collapsed box, listed in a sheet. A tapped row is held in `pendingOpen` until that sheet is
    /// gone, then opened — two sheets cannot swap in one breath.
    @State private var group: CalGroup?
    @State private var pendingOpen: CalEntry?
    /// A drop on a `run` row rewrites a whole cadence — it asks first.
    @State private var cadenceMove: CalCadenceMove?
    /// A drop on a repeating item asks: this one, or all future ones?
    @State private var repeatMove: CalRepeatMove?

    /// An install that last showed Week or Month has that word in its
    /// `@AppStorage` and there is no such mode any more, so it lands on
    /// Schedule — and `body` writes the migration back, or the mode picker
    /// would open with nothing selected.
    private var mode: CalMode { CalMode(rawValue: modeRaw) ?? .schedule }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                // The range lives in the page, not the navigation bar: the bar
                // gives a title about 90pt and clips "August 2026" to "ugust
                // 2026". Here it is always whole, and it is still the button
                // that opens the mode menu — Google Calendar's own title.
                Menu {
                    modeMenu
                } label: {
                    HStack(spacing: 4) {
                        Text(rangeLabel).font(.title3.weight(.semibold))
                        Image(systemName: "chevron.down").font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(.secondary)
                        Spacer()
                    }
                }
                .tint(.primary)
                .accessibilityIdentifier("cal-view")
                .padding(.horizontal).padding(.bottom, 4)
                if mode == .schedule { scheduleList } else { grid }
            }
            .navigationTitle("")
            .navigationBarTitleDisplayMode(.inline)
            .askButton()
            .toolbar { toolbar }
            .onAppear {
                // ops/screens.sh calendar:day — the mode a screenshot opens on.
                if let m = ProcessInfo.processInfo.environment["LIFE_CAL_MODE"], CalMode(rawValue: m) != nil { modeRaw = m }
                if CalMode(rawValue: modeRaw) == nil { modeRaw = CalMode.schedule.rawValue }
            }
            .task(id: modeRaw + anchor) { await load() }
            .sheet(item: $selected, onDismiss: { Task { await load() } }) { e in CalEntrySheet(entry: e, goals: goals) }
            .sheet(item: $respondTo) { r in RespondSheet(subject: r.entry.isHomework ? RespondSubject(homework: r.entry, first: r.outcome) : RespondSubject(cal: r.entry, first: r.outcome), reload: { await load() }) }
            .sheet(item: $group, onDismiss: {
                if let e = pendingOpen { pendingOpen = nil; open(e) } else { Task { await load() } }
            }) { g in groupSheet(g) }
            .sheet(isPresented: $showFilters) { CalFiltersSheet(filters: filtersBinding) }
            .sheet(isPresented: $showAnytime) { anytimeSheet }
            .navigationDestination(item: $openRec) { RecDetail(rec: $0, onChange: { await load() }) }
            .onChange(of: nav.cal, initial: true) { _, e in if let e { nav.cal = nil; picked = e.day; open(e) } }
            .alert("Move every check-in?", isPresented: Binding(get: { cadenceMove != nil }, set: { if !$0 { cadenceMove = nil } })) {
                Button("Cancel", role: .cancel) { cadenceMove = nil }
                Button("Move them all") { if let m = cadenceMove { cadenceMove = nil; Task { await applyCadence(m) } } }
            } message: {
                Text(cadenceMove.map { "This is a standing check-in, not one event.\n\nMove EVERY check-in of “\($0.entry.title)” to \($0.cadence.replacingOccurrences(of: "@", with: " at "))?" } ?? "")
            }
            .confirmationDialog("This repeats", isPresented: Binding(get: { repeatMove != nil }, set: { if !$0 { repeatMove = nil } }), titleVisibility: .visible) {
                Button("Only this one") { if let m = repeatMove { repeatMove = nil; Task { await applyRepeat(m, scope: "one") } } }
                Button("This and all future ones") { if let m = repeatMove { repeatMove = nil; Task { await applyRepeat(m, scope: "future") } } }
                Button("Cancel", role: .cancel) { repeatMove = nil }
            } message: {
                Text(repeatMove.map { "“\($0.entry.title)” repeats \($0.entry.repeat ?? ""). Move it to \($0.at.isEmpty ? "the all-day band" : $0.at)?" } ?? "")
            }
        }
    }

    // MARK: chrome

    private var rangeLabel: String {
        switch mode {
        case .schedule: return "Calendar"
        case .day: return parseDay(anchor)?.formatted(.dateTime.weekday(.wide).month(.abbreviated).day()) ?? anchor
        }
    }

    /// The menu behind the title. No "Today" button in it, and
    /// it is not a loss — the tab opens on Schedule, which always starts at
    /// today, and Day opens on today until the arrows move it.
    @ViewBuilder private var modeMenu: some View {
        Picker("Mode", selection: $modeRaw) {
            ForEach(CalMode.allCases) { m in Label(m.label, systemImage: m.icon).tag(m.rawValue) }
        }.pickerStyle(.inline)
        // Reducing to "what do I have to do today" is a toggle in the menu
        // itself, not three taps into a sheet. Lit, only the red rows are left.
        Toggle(isOn: Binding(get: { filters.mineOnly }, set: { on in
            var f = filters; f.mineOnly = on; filtersBinding.wrappedValue = f
        })) { Label("Just mine", systemImage: "person.fill") }
        Button { showFilters = true } label: { Label("Show…", systemImage: "line.3.horizontal.decrease.circle") }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        // No leading item: the title below the bar IS the mode menu, so an
        // icon up here would be the same menu twice.
        ToolbarItemGroup(placement: .topBarTrailing) {
            if mode == .day {
                Button { step(-1) } label: { Image(systemName: "chevron.left") }
                    .accessibilityLabel("Back").accessibilityIdentifier("cal-prev")
                Button { step(1) } label: { Image(systemName: "chevron.right") }
                    .accessibilityLabel("Forward").accessibilityIdentifier("cal-next")
                // The anytime tasks: the grid has no cell for a row with no date, so they live one tap
                // away, with the overdue rows that are past their cell.
                Button { showAnytime = true } label: {
                    Image(systemName: "tray").overlay(alignment: .topTrailing) {
                        if anytimeCount > 0 {
                            Circle().fill(.red).frame(width: 7, height: 7).offset(x: 4, y: -3)
                        }
                    }
                }
                .accessibilityLabel("Inbox").accessibilityIdentifier("cal-anytime")
            } else {
                Picker("Range", selection: $showPast) { Text("Upcoming").tag(false); Text("Past").tag(true) }
                    .pickerStyle(.segmented).frame(width: 170)
                    .onChange(of: showPast) { _, _ in Task { await load() } }
            }
        }
    }

    private func step(_ n: Int) {
        switch mode {
        case .day: picked = calAddDays(anchor, n)
        case .schedule: break
        }
    }

    private var visibleRange: (String, String) {
        switch mode {
        case .day: return (anchor, anchor)
        case .schedule:
            let t = today
            return showPast ? (calAddDays(t, -30), calAddDays(t, -1)) : (t, calAddDays(t, horizonDays))
        }
    }

    private var filters: CalFilters {
        let keys = Set(CalCals.all.map(\.key))
        return CalFilters(off: Set(offRaw.split(separator: ",").map(String.init)).intersection(keys))
    }
    private var filtersBinding: Binding<CalFilters> {
        Binding(get: { filters }, set: { f in
            offRaw = f.off.sorted().joined(separator: ",")
        })
    }

    // MARK: the grid modes

    /// Only `days` feeds the grid — an overdue or undated row has no cell to
    /// sit in and belongs in the tray, exactly as it does in the console rail.
    private var byDay: [String: [CalEntry]] {
        var m: [String: [CalEntry]] = [:]
        for d in view?.days ?? [] { m[d.day] = d.entries }
        return m
    }
    private var anytimeCount: Int {
        ((view?.anytime ?? []) + (view?.overdue ?? []) + (view?.due ?? []) + (view?.soon ?? [])).filter { filters.shows($0) }.count
    }

    @ViewBuilder private var grid: some View {
        VStack(spacing: 0) {
            if let error { ErrorBanner(message: error).padding(.horizontal) }
            if view == nil && error == nil {
                Spacer(); ProgressView(); Spacer()
            } else {
                CalTimeGrid(day: anchor, today: today,
                            entries: byDay[anchor] ?? [], filters: filters,
                            onTap: { open($0) },
                            onTapGroup: { group = CalGroup(entries: $0) },
                            onMove: { e, at in Task { await move(e, day: anchor, at: at) } })
                    .id(anchor)   // a new day redraws, but a reload keeps the scroll
            }
        }
    }

    /// The inbox behind the tray: what the owner still owes. Overdue first
    /// (missed steps stack up here), and the rows stay in their own day cells
    /// too — this is a tray over the calendar, not a move. The heading says
    /// "Overdue", as the console rail does, and each row wears the day it
    /// slipped from.
    private var anytimeSheet: some View {
        NavigationStack {
            List {
                let overdue = (view?.overdue ?? []).filter { filters.shows($0) }
                if !overdue.isEmpty {
                    Section {
                        ForEach(overdue) { row($0, day: $0.day, dated: true) }
                    } header: {
                        HStack { Text("Overdue").foregroundStyle(.red); Spacer(); Text("\(overdue.count)") }
                    }
                }
                // Due: today's steps, and a weekly homework all week before
                // its day — the console rail's list, each row wearing its day.
                let due = (view?.due ?? []).filter { filters.shows($0) }
                if !due.isEmpty {
                    Section {
                        ForEach(due) { row($0, day: $0.day, dated: true) }
                    } header: {
                        HStack { Text("Due"); Spacer(); Text("\(due.count)") }
                    }
                }
                // Do soon: the owner's steps due on a later day, each
                // wearing its day.
                let soon = (view?.soon ?? []).filter { filters.shows($0) }
                if !soon.isEmpty {
                    Section {
                        ForEach(soon) { row($0, day: $0.day, dated: true) }
                    } header: {
                        HStack { Text("Do soon"); Spacer(); Text("\(soon.count)") }
                    }
                }
                // Anytime = the owner's undated to-dos (calendar items);
                // Sessions waiting = the open asks a live session is blocked on.
                let all = (view?.anytime ?? []).filter { filters.shows($0) }
                let anytime = all.filter { $0.item == true }
                let waiting = all.filter { $0.item != true }
                if !waiting.isEmpty {
                    Section {
                        ForEach(waiting) { row($0, day: "") }
                    } header: {
                        HStack { Text("Sessions waiting"); Spacer(); Text("\(waiting.count)") }
                    }
                }
                if !anytime.isEmpty || (overdue.isEmpty && due.isEmpty && soon.isEmpty && waiting.isEmpty) {
                    Section {
                        if anytime.isEmpty {
                            Text("Nothing owed.").foregroundStyle(.secondary)
                        }
                        ForEach(anytime) { row($0, day: "") }
                    } header: { Text("Anytime") }
                }
            }
            .navigationTitle("Inbox").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Close") { showAnytime = false } } }
        }
    }

    /// The popout behind a collapsed box: every member on its own line, in the
    /// order the box holds them (time order), each row opening its full card.
    private func groupSheet(_ g: CalGroup) -> some View {
        NavigationStack {
            List {
                Section {
                    ForEach(g.entries) { e in
                        Button { pendingOpen = e; group = nil } label: { CalEntryRow(entry: e, goals: goals) }
                            .buttonStyle(.plain)
                    }
                }
            }
            // A run of record is named for its session — "7 steps in Migrate
            // the movie site": the deliverable, then the steps that made it.
            .navigationTitle(g.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Close") { group = nil } } }
        }
        .presentationDetents([.medium, .large])
    }

    private func open(_ e: CalEntry) {
        if e.refKind == "rec", let id = e.refID { Task { openRec = try? await hub.rec(id) } } else { selected = e }
    }

    /// A drop: an item takes the new day/time; a repeating item asks one-or-
    /// all-future first; a check-in's drop is a cadence rewrite and asks first.
    private func move(_ e: CalEntry, day: String, at: String) async {
        switch calMovable(e) {
        case .item:
            if !(e.repeat ?? "").isEmpty { repeatMove = CalRepeatMove(entry: e, day: day, at: at); return }
            do { _ = try await hub.patchCalItem(e.id, ["day": day, "at": at]); await load() }
            catch { actionError = error.localizedDescription }
        case .run:
            let cad = calNewCadence(e.repeat ?? "", day: day, at: at)
            if cad.isEmpty { actionError = "A check-in needs a time — drop it on the grid, not the all-day band."; return }
            cadenceMove = CalCadenceMove(entry: e, cadence: cad)
        case .none:
            actionError = calWhyStuck(e)
        }
    }

    private func applyCadence(_ m: CalCadenceMove) async {
        guard let t = m.entry.thread_id else { return }
        do { _ = try await hub.patchThread(t, ["schedule": m.cadence]); await load() }
        catch { actionError = error.localizedDescription }
    }

    private func applyRepeat(_ m: CalRepeatMove, scope: String) async {
        do { _ = try await hub.patchCalItem(m.entry.id, ["day": m.day, "at": m.at, "scope": scope]); await load() }
        catch { actionError = error.localizedDescription }
    }

    // MARK: Schedule — the agenda, exactly as it was

    private var scheduleList: some View {
            List {
                if let error { Section { ErrorBanner(message: error) } }
                if let v = view {
                    // Schedule is the mode the tab opens on, so "Just mine"
                    // and the lane switches have to reach it too — a toggle
                    // that changes Day and not Schedule is a lie (the console
                    // rail has always filtered all four of its modes).
                    // Under Past the days themselves are drawn, and an overdue
                    // row lives on one of them — listing the tray as well would
                    // print it twice on one screen.
                    let overdue = showPast ? [] : v.overdue.filter { filters.shows($0) }
                    let due = showPast ? [] : (v.due ?? []).filter { filters.shows($0) }
                    let anytime = v.anytime.filter { filters.shows($0) && $0.item == true }
                    let waiting = v.anytime.filter { filters.shows($0) && $0.item != true }
                    let soon = (v.soon ?? []).filter { filters.shows($0) }
                    if !overdue.isEmpty {
                        Section {
                            ForEach(overdue) { row($0, day: $0.day, dated: true) }
                        } header: {
                            HStack { Text("Overdue").foregroundStyle(.red); Spacer(); Text("\(overdue.count)") }
                        }
                    }
                    // Due: what the owner owes that has not slipped — today's
                    // steps, and a weekly homework all week before its day.
                    // The console rail's list, each row wearing its day.
                    if !due.isEmpty {
                        Section {
                            ForEach(due) { row($0, day: $0.day, dated: true) }
                        } header: {
                            HStack { Text("Due"); Spacer(); Text("\(due.count)") }
                        }
                    }
                    // Do soon: the owner's steps on a later day, the console's order
                    // (Overdue, Due, Do soon, Sessions waiting, Anytime, the days).
                    if !soon.isEmpty {
                        Section {
                            ForEach(soon) { row($0, day: $0.day, dated: true) }
                        } header: {
                            HStack { Text("Do soon"); Spacer(); Text("\(soon.count)") }
                        }
                    }
                    if !waiting.isEmpty {
                        Section {
                            ForEach(waiting) { row($0, day: "") }
                        } header: {
                            HStack { Text("Sessions waiting"); Spacer(); Text("\(waiting.count)") }
                        }
                    }
                    if !anytime.isEmpty {
                        Section {
                            DisclosureGroup(isExpanded: $anytimeOpen) {
                                ForEach(anytime) { row($0, day: "") }
                            } label: {
                                HStack {
                                    Label("Anytime", systemImage: "tray").font(.body.weight(.medium))
                                    Spacer()
                                    Text("\(anytime.count)").foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                    ForEach(visibleDays(v)) { d in
                        // The owner's items first; the standing check-ins/jobs
                        // of the day fold into one row so they never drown
                        // the thing they have to do. Decided actions (the
                        // agenda carries every action on its decided day)
                        // fold the same way; a still-proposed one is waiting
                        // on the owner and keeps its own row. So does their
                        // own record: what they read, decided and did that
                        // day is the point of the day; only the agents'
                        // trail folds.
                        let routine = d.entries.filter { $0.kind == "run" || $0.kind == "job" }
                        let decided = d.entries.filter { $0.did == true ? CalCals.of($0).key == "agents" : ($0.kind == "action" && $0.state != "proposed") }
                        let primary = d.entries.filter { !routine.contains($0) && !decided.contains($0) }
                        Section {
                            if d.entries.isEmpty { Text("Nothing scheduled").foregroundStyle(.tertiary) }
                            ForEach(primary) { row($0, day: d.day) }
                            if !routine.isEmpty {
                                foldedRows(routine, day: d.day, key: d.day, icon: "clock.arrow.circlepath",
                                           label: routine.count == 1 ? "1 agent check-in" : "\(routine.count) agent check-ins",
                                           sub: routine.map { ($0.at ?? "") + " " + $0.title }.joined(separator: " · "))
                            }
                            if !decided.isEmpty {
                                foldedRows(decided, day: d.day, key: d.day + "/actions", icon: CalKind.icon("action"),
                                           label: decided.count == 1 ? "1 agent action" : "\(decided.count) agent actions",
                                           sub: decided.map { $0.verb == nil ? $0.state + " · " + $0.title : $0.label }.joined(separator: " · "))
                            }
                        } header: { dayHeader(d.day, today: v.today) }
                    }
                    if !showPast {
                        Section {
                            Button("Show \(horizonDays + 60) days") { horizonDays += 60; Task { await load() } }
                        }
                    }
                } else if error == nil {
                    ProgressView()
                }
            }
            // No "+": every dated thing is put here by a session (`lifectl cal
            // add`); items are never added by hand.
            .refreshable { await load() }
    }

    /// One summary row per day for the rows that are not the owner's to do, opening
    /// into the list (the check-ins, the decided actions).
    private func foldedRows(_ rows: [CalEntry], day: String, key: String, icon: String, label: String, sub: String) -> some View {
        DisclosureGroup(isExpanded: Binding(get: { expandedDays.contains(key) }, set: { if $0 { expandedDays.insert(key) } else { expandedDays.remove(key) } })) {
            ForEach(rows) { row($0, day: day) }
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: icon).foregroundStyle(CalCals.agents.color).frame(width: 22)
                VStack(alignment: .leading, spacing: 2) {
                    Text(label).font(.subheadline)
                    Text(sub).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
        }
    }

    /// Closed is a look, never a filter: a done row is ✓, a dismissed one ✕
    /// and struck, and both stay on their day — as on the console's Schedule.
    /// Upcoming drops the empty days except today.
    private func visibleDays(_ v: CalView) -> [CalDay] {
        let f = filters
        // An open to-do is listed once, under Do soon — not again on the day
        // it was added (the Day grid does draw it there).
        var days = v.days.map { CalDay(day: $0.day, entries: $0.entries.filter { f.shows($0) && !($0.soon == true && !CalCals.isClosed($0)) }) }
        if !showPast {
            days = days.filter { !$0.entries.isEmpty || $0.day == v.today }
        }
        if !showPast, !days.contains(where: { $0.day == v.today }) {
            days.insert(CalDay(day: v.today, entries: []), at: 0)
            days.sort { $0.day < $1.day }
        }
        return days
    }

    @ViewBuilder private func dayHeader(_ day: String, today: String) -> some View {
        let d = parseDay(day)
        let label: String = {
            guard let d else { return day }
            if day == today { return "Today · " + d.formatted(.dateTime.weekday(.wide).month(.abbreviated).day()) }
            if let t = parseDay(today), Calendar.current.date(byAdding: .day, value: 1, to: t).map({ dayString($0) }) == day {
                return "Tomorrow · " + d.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
            }
            return d.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
        }()
        Text(label).foregroundStyle(day == today ? Color.accentColor : .secondary)
    }

    /// `dated`: the row is drawn away from its day (the Overdue pile), so it
    /// says the day too.
    private func row(_ e: CalEntry, day: String, dated: Bool = false) -> some View {
        Button {
            // `ref` names what is behind the row; a rec opens its own page,
            // everything else the entry sheet.
            if e.refKind == "rec", let id = e.refID { Task { openRec = try? await hub.rec(id) } } else { selected = e }
        } label: {
            CalEntryRow(entry: e, goals: goals, dated: dated)
        }
        .buttonStyle(.plain)
        .chatAbout(ChatSubject(
            screen: "Calendar",
            title: e.title,
            facts: calChatFacts(e, day: day),
            code: "app/Life/Sources/CalendarView.swift (CalEntryRow) ← hub GET /api/v1/calendar"))
        .swipeActions(edge: .trailing) { swipes(e) }
    }

    /// Done / Dismiss / Reopen on a swipe (items and asks only).
    @ViewBuilder private func swipes(_ e: CalEntry) -> some View {
            if e.isOpen && e.isOwnerStep {
                // The owner's own step closes with words: the swipe opens Respond
                // with that chip lit, and Send waits for the note.
                Button { respondTo = CalRespond(entry: e, outcome: "done") } label: { Label(e.outcomeLabel("done") ?? "Done", systemImage: "checkmark") }.tint(.green)
                Button { respondTo = CalRespond(entry: e, outcome: "wont") } label: { Label(e.outcomeLabel("wont") ?? "Won't do", systemImage: "xmark") }.tint(.gray)
            } else if e.isOpen && e.isInstall {
                // An app build is installed, not "done": the same
                // tap dance as the chat card — open the manifest, quit after
                // the prompt, close by "app" (the hub reopens it if the phone
                // never reports the build). No link → the sheet's Respond.
                if let u = e.installLink {
                    Button { install(e, u) } label: { Label("Install", systemImage: "square.and.arrow.down") }.tint(CalCals.install)
                }
                Button { Task { await resolve(e, "dismissed") } } label: { Label("Won't install", systemImage: "xmark") }.tint(.gray)
            } else if e.isOpen && (e.isItem || e.ask_id != nil) {
                // Homework is a tick: the hub's Did it / Skip, nothing to say;
                // any other open row, Done / Dismiss.
                Button { Task { await resolve(e, "done") } } label: { Label(e.outcomeLabel("done") ?? "Done", systemImage: "checkmark") }.tint(.green)
                Button { Task { await resolve(e, "dismissed") } } label: { Label(e.outcomeLabel("wont") ?? "Dismiss", systemImage: "xmark") }.tint(.gray)
            } else if e.isItem && e.isClosed {
                // Closed by mistake → flip it back. Reopen alone, as on the
                // console: a closed step of the owner's re-closes with words, never a bare Dismiss.
                Button { Task { await resolve(e, "scheduled") } } label: { Label("Reopen", systemImage: "arrow.uturn.backward") }.tint(.orange)
            }
    }

    private func resolve(_ e: CalEntry, _ state: String) async {
        do {
            if e.isItem { _ = try await hub.resolveCalItem(e.id, state: state) }
            else if let a = e.ask_id { _ = try await hub.resolveAsk(a, state: state) }
            await load()
        } catch { actionError = error.localizedDescription }
    }

    /// The install tap, as `AskCard.linkButton` does it: open the manifest,
    /// hand the self-restart to InstallState, close the ask by "app" so the
    /// row leaves at once — and the record row lands at this minute.
    private func install(_ e: CalEntry, _ u: URL) {
        openURL(u)
        AppDelegate.push.install.tapped(e.title)
        Task {
            if let a = e.ask_id { _ = try? await hub.resolveAsk(a, state: "done", note: "tapped Install", by: "app") }
            await load()
        }
    }

    /// Through `cal` (HubLoad), not `.hubTask`: the range follows the mode
    /// and anchor, and only the Schedule list pulls to refresh.
    private func load() async {
        // One fetch covers the mode's range plus a week either side, so a
        // step to the next week draws from what is already here.
        let (a, b) = visibleRange
        let pad = mode == .schedule ? 0 : 7
        // Goals and the tab's oval come from the store: a change here bumps
        // the hub's change feed and RootView's poll refreshes both, so a
        // board, threads and goals refetch on every step was three wasted calls.
        await cal.run { try await hub.calendar(from: calAddDays(a, -pad), to: calAddDays(b, pad)) }
        if cal.error == nil { actionError = nil }
        // The hub's today decides; a wrong guess moves the range (and the
        // task id), which fetches again.
        if let t = view?.today, t != today { today = t }
    }
}

/// The members of a collapsed box, on their way to the popout sheet.
struct CalGroup: Identifiable {
    let entries: [CalEntry]
    var id: String { entries.map(\.id).joined(separator: ",") }
    var title: String {
        let name = calGroupLabel(entries)
        return name.badge.hasPrefix("+") ? "\(entries.count) at \(entries.first?.at ?? "")" : "\(name.badge) in \(name.title)"
    }
}

/// A check-in dropped somewhere new: the whole cadence, waiting on an OK.
struct CalCadenceMove: Identifiable {
    let entry: CalEntry
    let cadence: String
    var id: String { entry.id + ":" + cadence }
}

/// A repeating item dropped somewhere new: one occurrence or the whole
/// chain? Google's question, waiting on an answer.
struct CalRepeatMove: Identifiable {
    let entry: CalEntry
    let day: String
    let at: String
    var id: String { entry.id + ":" + day + ":" + at }
}

/// An owner's step with the chip they chose, on its way to the Respond sheet.
struct CalRespond: Identifiable {
    let entry: CalEntry
    let outcome: String
    var id: String { entry.id + ":" + outcome }
}

extension CalEntry {
    /// One of the owner's own tasks (the hub's kind `owner`, a real item): the
    /// only rows that close with words rather than a tap.
    var isOwnerStep: Bool { isItem && kind == "owner" && !isHomework }
    /// A to-do no session is behind (soon, no thread): the owner's words close
    /// it on the hub and nothing is woken to hear them. `calLoneTodo` on the
    /// console.
    var isLoneTodo: Bool { soon == true && (thread_id ?? "").isEmpty }
    /// Homework is a COMPLETION, not a conversation: one tick, no words — and
    /// the hub ticks it itself when the evidence lands. A chore no session is
    /// behind closes the same way: the hub's `tick`.
    var isHomework: Bool { isItem && (tick == true || kind == "homework") }
    /// The hub's word for one of the row's answers (`outcomes`), nil when it
    /// sent none for that value.
    func outcomeLabel(_ value: String) -> String? { outcomes?.first { $0.value == value }?.label }
}

/// Kind → icon and label. The COLOUR is not here: it is the row's lane
/// (`CalCals.of`), because a proposed action is red and a decided one is grey,
/// which the kind alone cannot say.
enum CalKind {
    static func icon(_ k: String) -> String {
        switch k {
        // An open owner's step is a hollow circle — the ○ of "still to do"
        // (calGlyph), Reminders' own mark — not a hand: the row must read as
        // unfinished until the owner closes it.
        case "owner": "circle"
        // Homework is the owner's too — the same hollow ○ until they (or a finished
        // round) closes it; the light-blue lane says which kind of step it is.
        case "homework": "circle"
        case "agent": "sparkles"
        case "note": "bell.fill"
        case "run": "clock.arrow.circlepath"
        case "job": "gearshape.2"
        case "ask": "exclamationmark.bubble"
        case "action": "checkmark.shield"
        case "rec": "lightbulb"
        default: "circle"
        }
    }
    /// The row's icon, with the one ask kind that is its own cell: an app
    /// build wears the chat card's download arrow.
    static func icon(_ e: CalEntry) -> String { e.isInstall ? "square.and.arrow.down" : icon(e.kind) }
    /// The kind's word is the hub's `kind_label` — the console's pill says the same.
    static func label(_ e: CalEntry) -> String { e.kind_label ?? e.kind }
}

struct CalEntryRow: View {
    let entry: CalEntry
    let goals: [Goal]
    /// Drawn away from its day (the Overdue pile): the meta line says
    /// "Tue 16 20:00" instead of the bare minute — `calWhenShort`, the same
    /// words the console rail prints.
    var dated = false
    var body: some View {
        // The grid's one closed rule (Models.isClosed plus an accepted rec's
        // filed bar, a decided action, and won't-do) — the list and the grid
        // must fade the same rows.
        let closed = CalCals.isClosed(entry)
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: closed ? (calStruck(entry) ? "xmark.circle" : "checkmark.circle.fill") : CalKind.icon(entry))
                .foregroundStyle(closed ? Color.secondary : CalCals.shade(entry))
                .frame(width: 22)
                .padding(.top, 2)
            VStack(alignment: .leading, spacing: 3) {
                // A deferred rec on its review day reads as the question it
                // is: "Check back: <title>"; a record reads as its
                // deed: "Read: <title>", "Approved: <title>". The
                // strike is won't-do's alone (✓ + strikethrough reads as its
                // own opposite — see calGlyph); a
                // done row keeps its ✓ icon and fades, unstruck.
                Text(md(entry.verb == nil && entry.kind == "rec" ? "Check back: " + entry.title : entry.label))
                    .font(.body.weight(calIsOwner(entry) && !closed ? .semibold : .regular))
                    .strikethrough(calStruck(entry))
                    .foregroundStyle(closed ? .secondary : .primary)
                    .multilineTextAlignment(.leading)
                if let d = entry.detailShown, !d.isEmpty, entry.kind != "run", entry.kind != "job" {
                    Text(md(d)).font(.caption).foregroundStyle(.secondary).lineLimit(2).multilineTextAlignment(.leading)
                }
                HStack(spacing: 6) {
                    // An open to-do never says a time as if it were due then.
                    if entry.soon == true && !closed { Text(calAdded(entry)) }
                    else if dated { Text(calWhenShort(entry)) }
                    else if let at = entry.at, !at.isEmpty { Text(at) }
                    Text(CalKind.label(entry))
                    if let r = entry.repeat, !r.isEmpty { Text(entry.kind == "run" || entry.kind == "job" ? scheduleLabel(r) : r) }
                    if entry.overdue == true { Text("overdue").foregroundStyle(.red) }
                    if entry.state == "fired" { Text("on your board") }
                    // The row's session, live: the board's own pills, so the
                    // step just answered says its session is on it.
                    // Nothing on a quiet session.
                    ForEach(Array((entry.live ?? []).enumerated()), id: \.offset) { _, p in PillChip(pill: p) }
                    // A record says whose hands and which session — "by
                    // <actor> · <session>", the portfolio line under the
                    // deed; an action's row says what became of it and who decided.
                    if entry.did == true {
                        if let a = entry.actor { Text("by \(a)") }
                        if let t = entry.thread_title, !t.isEmpty { Text(t).lineLimit(1) }
                    } else if entry.kind == "action" { Text(entry.state + (entry.actor.map { " by \($0)" } ?? "")) }
                    if let g = entry.goal_id, let t = goals.first(where: { $0.id == g })?.title { Text("◎ \(t)") }
                }.font(.caption2).foregroundStyle(.tertiary)
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
    }
}

/// Detail sheet: full text, Done/Dismiss for items and asks, open the session.
/// What a chat about a row is told (its own function: inline, the array
/// outgrew the type-checker) — the held-down row.
func calChatFacts(_ e: CalEntry, day: String) -> String {
    let at: String = e.at.map { $0.isEmpty ? "" : " at \($0)" } ?? ""
    let when: String = e.soon == true && e.isOpen
        ? "no due day (an anytime to-do), \(calAdded(e))"
        : (day.isEmpty ? "no date (anytime)" : day) + at + (e.overdue == true ? " — OVERDUE" : "")
    var lines: [String] = [
        "- `\(e.id)`, kind \(e.kind) (\(CalKind.label(e))), state \(e.state)",
        "- " + when,
    ]
    if let r = e.repeat, !r.isEmpty { lines.append("- repeats \(r)") }
    if let g = e.goal_id { lines.append("- goal: \(g)") }
    if let t = e.thread_id { lines.append("- session: `\(t)`") }
    if let a = e.ask_id { lines.append("- ask: `\(a)`") }
    if let a = e.actor { lines.append("- by: \(a)") }
    if e.did == true { lines.append("- a record: \(e.verb ?? "did") at that minute" + (e.thread_title.map { ", a step of “\($0)”" } ?? "")) }
    if let r = e.ref { lines.append("- ref: `\(r)`") }
    if let d = e.detail, !d.isEmpty { lines.append("- detail:\n\(d.prefix(1000))") }
    return lines.joined(separator: "\n")
}

struct CalEntrySheet: View {
    @Environment(HubClient.self) private var hub
    @Environment(\.dismiss) private var dismiss
    let entry: CalEntry
    let goals: [Goal]
    @State private var thread: Thread?
    @State private var error: String?
    /// A proposed action's row carries its approval card (the
    /// calendar decides, not just lists) — fetched by `ref`, drawn like the
    /// board's card, and deciding it closes the sheet (the agenda reloads).
    @State private var action: Action?
    /// An open install row carries the chat's own install cell, not a Done
    /// button: the ask itself, fetched by id, drawn as
    /// `AskCard(.inline)` — title, description, one teal Install button, the
    /// same tap dance and self-restart. Installing closes this sheet.
    @State private var ask: Ask?
    /// The owner's step or homework, still open: the answer drawn on this page.
    @State private var respond: RespondModel?
    private var isProposedAction: Bool { entry.refKind == "action" && entry.state == "proposed" }

    init(entry: CalEntry, goals: [Goal]) {
        self.entry = entry
        self.goals = goals
        let subject: RespondSubject? = !entry.isOpen ? nil
            : entry.isHomework ? RespondSubject(homework: entry, first: "")
            : entry.isOwnerStep ? RespondSubject(cal: entry, first: "") : nil
        _respond = State(initialValue: subject.map { RespondModel($0) })
    }
    private var isOpenInstall: Bool { entry.isInstall && entry.isOpen && entry.ask_id != nil }
    var body: some View {
        NavigationStack {
            List {
                if isProposedAction {
                    Section {
                        if let a = action {
                            ApprovalCard(a: a, inThread: false) { dismiss() }
                        } else if error == nil {
                            HStack { ProgressView(); Text("Loading the proposal…").font(.caption).foregroundStyle(.secondary) }
                        }
                    } header: { Text("Waiting on you") }
                }
                if isOpenInstall {
                    Section {
                        if let a = ask {
                            AskCard(a: a, goals: goals, reload: { dismiss() })
                                .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                        } else if error == nil {
                            HStack { ProgressView(); Text("Loading the build…").font(.caption).foregroundStyle(.secondary) }
                        }
                        Button(role: .destructive) { Task { await resolve("dismissed") } } label: { Label("Won't install", systemImage: "xmark.circle") }
                    } header: { Text("App update") }
                }
                Section {
                    CalEntryRow(entry: entry, goals: goals)
                    if let d = entry.detailShown, !d.isEmpty, !isOpenInstall { StyledText(text: d, style: .body) }
                    if entry.soon == true && entry.isOpen, let dd = parseDay(entry.day) {
                        LabeledContent("Added", value: dd.formatted(.dateTime.weekday(.wide).month(.abbreviated).day().year()))
                    } else if !entry.day.isEmpty, let dd = parseDay(entry.day) {
                        LabeledContent("When", value: dd.formatted(.dateTime.weekday(.wide).month(.abbreviated).day().year()) + (entry.at.map { $0.isEmpty ? "" : " \($0)" } ?? " (all day)"))
                    }
                    LabeledContent("State", value: entry.state)
                }
                if respond != nil {
                    // The owner's step or homework answers ON this page: the chips and
                    // the chat bar at its foot (below), never a second sheet
                    // with the same three choices.
                } else if isOpenInstall {
                    // The install cell above IS the buttons.
                } else if entry.isOpen && (entry.isItem || entry.ask_id != nil) {
                    Section {
                        Button { Task { await resolve("done") } } label: { Label("Done", systemImage: "checkmark.circle.fill") }
                        Button(role: .destructive) { Task { await resolve("dismissed") } } label: { Label("Dismiss", systemImage: "xmark.circle") }
                    }
                } else if entry.isItem && entry.isClosed {
                    Section {
                        Button { Task { await resolve("scheduled") } } label: { Label("Reopen", systemImage: "arrow.uturn.backward.circle") }
                    }
                }
                if let t = thread {
                    Section {
                        // An action row lands ON its card in the session (the
                        // audit trail is drawn there), via `ref`, not the id.
                        NavigationLink { ThreadDetail(thread: t, focusCard: entry.refKind == "action" ? entry.refID : nil) } label: {
                            HStack(spacing: 6) {
                                Label(t.title, systemImage: "bubble.left.and.bubble.right")
                                // What it is doing right now — the row's `live`
                                // pills, the board's own words.
                                ForEach(Array((entry.live ?? []).enumerated()), id: \.offset) { _, p in PillChip(pill: p) }
                            }
                        }
                    } header: { Text("Session") }
                }
                if let error = error ?? respond?.error { Section { ErrorBanner(message: error) } }
            }
            .safeAreaInset(edge: .bottom) {
                // No inner page. Homework: Did it + Send over an empty box
                // closes it and wakes nobody; words or a photo go to a session
                // named for it. The owner's step closes only with words. Sent → this sheet goes.
                if let r = respond {
                    VStack(alignment: .leading, spacing: 0) {
                        RespondChips(model: r).padding(.horizontal, 12).padding(.top, 10)
                        RespondBar(model: r) { dismiss() }
                    }.background(.bar)
                }
            }
            .navigationTitle(entry.verb ?? CalKind.label(entry).capitalized)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Close") { dismiss() } } }
            .task {
                if let id = entry.thread_id { thread = try? await hub.thread(id) }
                if isProposedAction, let id = entry.refID {
                    do { action = try await hub.action(id) } catch { self.error = error.localizedDescription }
                }
                if isOpenInstall, let id = entry.ask_id {
                    do { ask = try await hub.ask(id) } catch { self.error = error.localizedDescription }
                }
            }
        }
    }

    private func resolve(_ state: String) async {
        do {
            if entry.isItem { _ = try await hub.resolveCalItem(entry.id, state: state) }
            else if let a = entry.ask_id { _ = try await hub.resolveAsk(a, state: state) }
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}
