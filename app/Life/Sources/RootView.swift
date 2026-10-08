import SwiftUI
import UIKit

struct RootView: View {
    @Environment(HubClient.self) private var hub
    @State private var snap = SnapState()
    /// Component being held down right now (see ChatAbout.swift).
    @State private var chat = ChatSpot()
    @Environment(PushState.self) private var push
    /// The red ovals on the bar and inside More — see Badges.swift.
    @State private var badges = Badges()
    /// The one board/threads/goals poll — see BoardStore.swift.
    @State private var store = BoardStore()
    /// Where a hub id tapped in a card's text lands — see Refs.swift.
    @State private var nav = RefNav()
    @State private var tab = {
        #if targetEnvironment(simulator) || targetEnvironment(macCatalyst)
        // ops/screens.sh (ops/mac-screens.sh): LIFE_TAB=calendar opens that tab directly for a screenshot.
        if let t = ProcessInfo.processInfo.environment["LIFE_TAB"], !t.isEmpty { return t }
        #endif
        return "sessions"
    }()
    @Environment(\.scenePhase) private var scenePhase
    #if targetEnvironment(macCatalyst)
    @State private var find = MacFind.shared
    #endif
    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            tabs
            // Tiny build label fully BELOW the floating tab bar, in the
            // home-indicator strip. The ZStack ignores the bottom safe area so
            // the label can sit there; TabView lays itself out as usual.
            // Every screen that has the tab bar shows it, a thread's chat
            // included: iOS keeps the floating tab bar in that strip and puts
            // the composer above it.
            versionTag
        }
            .ignoresSafeArea(edges: .bottom)
            // A push tap lands on the board of things that need the owner:
            // Sessions, whose first group is Your turn.
            .onChange(of: push.openSessions) { _, open in if open { tab = "sessions"; push.openSessions = false } }
            // A tapped id: its tab first, then that tab's screen takes the
            // rec / card / calendar item waiting in `nav`.
            .onChange(of: nav.tab) { _, t in if let t { tab = t; nav.tab = nil } }
            .onOpenURL { url in
                // life://inbox: LifeShare just wrote a shared file for us.
                if url.host() == "inbox" { snap.drainInbox(); return }
                Task { await nav.open(url, hub: hub) }
            }
            // After an Install tap the app quits itself once the iOS prompt
            // is answered (see InstallState in Push.swift).
            .onChange(of: scenePhase) { _, p in
                push.install.scene(p)
                // On the Mac, `.inactive` is a window that is merely not
                // frontmost — still on screen beside a browser — so the loop
                // keeps going; only `.background` (hidden, minimised) stops
                // it. Treating it as the phone's inactive froze the list for
                // as long as another app had the focus.
                #if targetEnvironment(macCatalyst)
                store.active = p != .background
                #else
                store.active = p == .active
                #endif
                if p == .active { Task { await store.refresh(hub) }; snap.drainInbox() }
            }
            // ONE loop for the board, the threads and the goals, whatever
            // screen is showing: refresh, then park on the change feed until the
            // hub says something moved (or 25 s pass), then refresh again —
            // nothing in the background, nothing while a session is open (it
            // has its own loop). An older hub without the feed (404) falls
            // back to the 6 s tick.
            .task {
                await store.seed(hub)
                while !Task.isCancelled {
                    guard store.wantsPoll else { try? await Task.sleep(for: .seconds(2)); continue }
                    await store.refresh(hub)
                    if let f = try? await hub.changes(since: store.version) { store.version = f.version }
                    else { try? await Task.sleep(for: .seconds(6)) }
                }
            }
            // The bar's numbers are the board's: a screen that closed an ask
            // and refreshed the store takes the oval with it at once.
            .onChange(of: store.board?.badges, initial: true) { _, b in if let b { badges.apply(b) } }
            .overlay { if let n = push.install.pending { installingBanner(n) } }
            // ABOVE the .environment lines, never below: a sheet inherits the
            // environment of the view `.sheet` is attached to, and once sent
            // this sheet IS the chat (ThreadDetail reads BoardStore, its Ask
            // button reads SnapState). Attached below them it had neither,
            // and the first render after Send killed the app.
            .sheet(item: $snap.pending) { s in
                #if targetEnvironment(macCatalyst)
                // The desktop: the window closes the moment the session starts
                // and the session opens on Sessions, beside the list.
                NewThreadSheet(initialImage: s.image, initialPlace: s.path, initialFiles: s.files, initialImages: s.images,
                               onCreated: { await store.refresh(hub) },
                               started: { t in snap.pending = nil; tab = "sessions"; nav.card = OpenAsk(thread: t, message: nil) })
                #else
                NewThreadSheet(initialImage: s.image, initialPlace: s.path, initialFiles: s.files, initialImages: s.images) { await store.refresh(hub) }
                #endif
            }
            // Ask is a toolbar item on every screen (see Snap.swift `askButton`).
            .environment(snap)
            .environment(chat)
            .environment(badges)
            .environment(store)
            .environment(nav)
            // Every text in the app opens its links through here: a `life://`
            // one is ours, anything else is still iOS's.
            .environment(\.openURL, OpenURLAction { url in
                guard url.scheme == "life" else { return .systemAction }
                Task { await nav.open(url, hub: hub) }
                return .handled
            })
            .task {
                #if targetEnvironment(macCatalyst)
                // A Mac that is not the hub's has no token file to read: it
                // opens on Settings for the hub address and token.
                if !hub.isConfigured { tab = "settings" }
                #endif
                #if targetEnvironment(simulator) || targetEnvironment(macCatalyst)
                // ops/screens.sh open:<id> (and ops/mac-screens.sh open:<id>)
                // — the app as it is after that id was tapped in a
                // card's text.
                if let id = ProcessInfo.processInfo.environment["LIFE_OPEN"], !id.isEmpty, let u = URL(string: "life://open/\(id)") {
                    await nav.open(u, hub: hub)
                }
                // ops/screens.sh compose: open the snap sheet over a blank
                // image, draft box focused. The composer is the one part of
                // the app a tab screenshot never shows in its typing state —
                // which is how a placeholder ran out of its box unseen.
                if let c = ProcessInfo.processInfo.environment["LIFE_COMPOSE"], !c.isEmpty, snap.pending == nil {
                    if c == "share" {
                        // ops/screens.sh share: the sheet as it opens after
                        // "Share → Life" on a bank's PDF (SharedInbox).
                        snap.pending = SnapImage(image: nil, files: [AttachedFile(name: "bank-statement.pdf", data: Data(count: 48_213))])
                    } else {
                        let size = CGSize(width: 300, height: 600)
                        snap.pending = SnapImage(image: UIGraphicsImageRenderer(size: size).image { c in
                            UIColor.systemGray5.setFill(); c.fill(CGRect(origin: .zero, size: size))
                        })
                    }
                }
                #endif
            }
    }

    private func installingBanner(_ n: Int) -> some View {
        VStack(spacing: 6) {
            ProgressView()
            Text("Installing build \(n)").font(.headline)
        }.padding(20).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 16)).padding()
    }

    #if targetEnvironment(macCatalyst)
    /// The desktop app is the console's LAYOUT, not the phone's: one bar
    /// across the top in the console's order with its red numbers,
    /// + New session on the right, and the page under it at the window's
    /// full width.
    private var tabs: some View {
        VStack(spacing: 0) {
            MacTopBar(tab: $tab)
                .accessibilityElement(children: .contain).accessibilityIdentifier(MacFind.skip)
            Divider()
            // ⌘F (MacFind): the find bar pushes the page down under the top bar.
            if find.shown {
                MacFindBar().accessibilityElement(children: .contain).accessibilityIdentifier(MacFind.skip)
                Divider()
            }
            macPage(tab)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .font(.body)  // MacFonts: a bare Text/Label takes the bigger body too
                .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { find.pageFrame = $0 }
        }
        .onChange(of: tab) { _, _ in find.pageChanged() }
        // The hidden title bar still reserves its strip; the bar takes it.
        .ignoresSafeArea(.container, edges: .top)
        .sheet(item: $chat.pending) { r in ChatAboutSheet(request: r) }
        .task { MacUpdate.shared.start(hub: hub) }
    }

    /// A page is drawn when its tab is picked — the console redraws a page
    /// on every visit too, and the store keeps the numbers warm between.
    @ViewBuilder private func macPage(_ t: String) -> some View {
        switch t {
        case "recs": NavigationStack { RecsView() }.macColumn()
        case "calendar": CalendarView()
        case "goals": NavigationStack { GoalsView() }.macColumn()
        case "spend": SpendView().macColumn()
        case "config": NavigationStack { ConfigView() }.macColumn()
        case "settings": NavigationStack { SettingsView() }.macColumn()
        default: ThreadsView()
        }
    }
    #else
    private var tabs: some View {
        TabView(selection: $tab) {
            // The bar is the console's nav, in the console's order: Sessions,
            // Recs, Calendar, then More (Configuration, Settings).
            //
            // Sessions = the agents working for the owner, every past thread,
            // AND what needs them: its first group is Your turn, each row
            // carrying the card's title and description. It wears the
            // your-turn count. No separate Your turn, Actions or board tab.
            Tab("Sessions", systemImage: "bubble.left.and.bubble.right.fill", value: "sessions") { ThreadsView() }
                .badge(badges.yourTurn)
            // Recommendations = pull, never push (docs/design/recommendations.md).
            // The number is what sits undecided when the owner looks, and it
            // never reaches the push channel. "Recs" on the bar, like the
            // console (the full word is too wide); the page title stays long.
            Tab("Recs", systemImage: "lightbulb.fill", value: "recs") { NavigationStack { RecsView() } }
                .badge(badges.recs)
            // Calendar = the plan: dated steps for the owner, agent runs,
            // reminders, every session's check-ins. Its number is the
            // dated steps that came due and are still open.
            Tab("Calendar", systemImage: "calendar", value: "calendar") { CalendarView() }
                .badge(badges.calendar)
            // Four explicit tabs, so iOS never builds its automatic "More" —
            // see MoreView. Nothing behind More carries a count, so it has no badge.
            Tab("More", systemImage: "ellipsis", value: "more") { MoreView() }
        }
        // "Chat about this" on a held-down component. Presented from the tabs,
        // not the ZStack: two `.sheet(item:)` on one view only honour the first.
        .sheet(item: $chat.pending) { r in ChatAboutSheet(request: r) }
    }
    #endif

    private var versionTag: some View {
        #if targetEnvironment(macCatalyst)
        // On the Mac the label sits in the top bar, after the status dot.
        EmptyView()
        #else
        Text(appVersionLabel)
            .font(.system(size: 9)).monospacedDigit()
            .foregroundStyle(.tertiary)
            .padding(.trailing, 44).padding(.bottom, 6)  // clear the screen's rounded corner, or the label is clipped
            .allowsHitTesting(false)
        #endif
    }
}

#if targetEnvironment(macCatalyst)
/// The console's top bar, drawn natively: the "life" wordmark, the pages in
/// the console's order with its red numbers (the picked one a filled blue
/// pill), then + New session (which snaps the page, as the console's
/// does — there is no separate Ask), the status dot and the build on the right.
/// The window's title bar is hidden (MacWindow.size), so the bar starts past
/// the traffic lights.
struct MacTopBar: View {
    @Binding var tab: String
    @Environment(Badges.self) private var badges
    @Environment(BoardStore.self) private var store
    @Environment(SnapState.self) private var snap

    static let pages: [(key: String, title: String)] = [
        ("sessions", "Sessions"), ("recs", "Recs"), ("calendar", "Calendar"), ("config", "Configuration"),
    ]
    /// The file Ask names as "the screen the owner was on", as `.askButton()` did.
    private var file: String {
        switch tab {
        case "sessions": "Life/ThreadsView.swift"
        case "recs": "Life/RecsView.swift"
        case "calendar": "Life/CalendarView.swift"
        case "goals": "Life/GoalsView.swift"
        case "spend": "Life/SpendView.swift"
        case "config": "Life/ConfigView.swift"
        default: "Life/SettingsView.swift"
        }
    }
    private func count(_ key: String) -> Int {
        switch key {
        case "sessions": badges.yourTurn
        case "recs": badges.recs
        case "calendar": badges.calendar
        default: 0
        }
    }

    /// Never squeezed: when the window is too narrow for every word (the
    /// console folds tabs into More), the buttons on the right go to icons
    /// first, with their words as tooltips; the pages keep their names.
    var body: some View {
        ViewThatFits(in: .horizontal) {
            bar(compact: false)
            bar(compact: true)
        }
        .padding(.leading, 84).padding(.trailing, 16)
        .frame(height: 44)
        .background(Color(uiColor: .systemBackground))
    }

    private func bar(compact: Bool) -> some View {
        HStack(spacing: 4) {
            Text("life").font(.system(size: 19, weight: .bold)).padding(.trailing, compact ? 6 : 14)
            ForEach(Self.pages, id: \.key) { p in item(p.key, p.title) }
            Spacer(minLength: 12)
            Group {
                MacUpdateButton()
                Button { snap.snapForDesk(from: file); tab = "sessions" } label: { Label("New session", systemImage: "plus") }
                    .buttonStyle(.borderedProminent).controlSize(.small)
                    .help("New session")
            }
            .labelStyle(BarLabel(compact: compact))
            Button { tab = "settings" } label: { Image(systemName: "gearshape") }
                .buttonStyle(.plain).foregroundStyle(tab == "settings" ? Color.accentColor : .secondary)
                .help("Settings").padding(.leading, 4)
            Circle().fill(store.error == nil ? Color.green : Color.red).frame(width: 8, height: 8)
                .help((store.error ?? "Hub reachable") + (compact ? " · build \(appVersionLabel)" : "")).padding(.leading, 6)
            if !compact {
                Text(appVersionLabel).font(.system(size: 11)).monospacedDigit().foregroundStyle(.secondary)
            }
        }
    }

    /// Icon and words, or the icon alone on a narrow window.
    struct BarLabel: LabelStyle {
        var compact: Bool
        func makeBody(configuration: Configuration) -> some View {
            HStack(spacing: 6) {
                configuration.icon
                if !compact { configuration.title }
            }
        }
    }

    private func item(_ key: String, _ title: String) -> some View {
        let on = tab == key
        let n = count(key)
        return Button { tab = key } label: {
            HStack(spacing: 5) {
                // Never wraps: a bar too tight for its words falls to the
                // compact one (ViewThatFits), not "Session / s".
                Text(title).font(.system(size: 13, weight: on ? .semibold : .medium)).lineLimit(1).fixedSize()
                if n > 0 {
                    // On the picked tab the number turns white with blue
                    // digits, as the console's does (`#nav a.on .badge`).
                    // One line, as the title is: a bar that just fits used to
                    // stack "10" as 1 over 0 rather than go compact.
                    Text("\(n)").font(.system(size: 10, weight: .bold)).monospacedDigit().lineLimit(1).fixedSize()
                        .foregroundStyle(on ? Color.accentColor : Color.white)
                        .padding(.horizontal, 5).frame(minWidth: 17, minHeight: 17)
                        .background(on ? Color.white : Color.red, in: Capsule())
                }
            }
            .foregroundStyle(on ? Color.white : Color.primary)
            .padding(.horizontal, 10).padding(.vertical, 5)
            .background(on ? Color.accentColor : Color.clear, in: RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

extension View {
    /// A list page on the desktop: wall to wall on its grey ground, with no
    /// grey bars at the sides.
    func macColumn() -> some View {
        self.frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Web.page)
            // A page opens in place, as a console route does: no slide in
            // from the side. Only an un-animated change loses its motion; a
            // withAnimation keeps it.
            .transaction { if $0.animation == nil { $0.disablesAnimations = true } }
    }
}
#endif

/// Extra vertical room for a list row of prose on the desktop, where a
/// Catalyst row hugs its text; nothing on the phone.
#if targetEnvironment(macCatalyst)
let macRowPad: CGFloat = 8
#else
let macRowPad: CGFloat = 0
#endif

/// "1.0 (193)": marketing major.minor + build number.
let appVersionLabel: String = {
    let i = Bundle.main.infoDictionary
    let v = (i?["CFBundleShortVersionString"] as? String ?? "?").split(separator: ".").prefix(2).joined(separator: ".")
    let b = i?["CFBundleVersion"] as? String ?? "?"
    return "\(v) (\(b))"
}()

/// Our own "More": Configuration, Settings.
///
/// iOS's automatic More tab wraps the tab's root view in a navigation
/// controller of its own, and every screen here brings its own
/// NavigationStack — so pushing e.g. a source drew TWO stacked nav bars: two
/// back buttons in the corner and the page shoved down the screen. Explicit tabs mean iOS never makes a
/// More tab, and these push inside this one stack instead of starting their
/// own.
///
/// The rows are value-based (`MoreDest`) rather than closure-based so the
/// stack has a path a screenshot run can preset: `ops/screens.sh more:recs`
/// opens straight into that screen, which is otherwise two taps deep and
/// therefore unlookable-at.
/// Case order = the order the rows are drawn in, and it IS the console's nav
/// after Calendar, with the console's labels. Nothing slides — see
/// `MoreView.rows`.
enum MoreDest: String, Hashable, CaseIterable {
    case config, settings
    var title: String {
        switch self {
        case .config: "Configuration"; case .settings: "Settings"
        }
    }
    var icon: String {
        switch self {
        case .config: "slider.horizontal.3"; case .settings: "gearshape.fill"
        }
    }
}

struct MoreView: View {
    @Environment(HubClient.self) private var hub
    @Environment(Badges.self) private var badges
    // NavigationPath, not [MoreDest]: a typed path can only carry ITS type, and
    // a NavigationLink(value:) of any other type inside the stack silently does
    // nothing. Spend and Sources push their own rows, so the path is untyped.
    @State private var path: NavigationPath = {
        #if targetEnvironment(simulator)
        if let m = ProcessInfo.processInfo.environment["LIFE_MORE"], let d = MoreDest(rawValue: m) { return NavigationPath([d]) }
        #endif
        return NavigationPath()
    }()
    @Environment(BoardStore.self) private var store
    @State private var showUpdate = false

    /// One fixed order, the enum's. A row
    /// with a number on it keeps its place — every row is always in view, so
    /// nothing slides.
    var rows: [MoreDest] { MoreDest.allCases }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                // Downloading the newest build is the most frequent manual
                // step, so it is the top row here, not buried in Settings. Opens
                // a modal with every lane: see UpdateAppView.swift.
                Section { UpdateAppRow(showUpdate: $showUpdate, ota: store.status?.ota) }
                // The console's nav after Calendar. Each row carries a
                // CountOval so a future count shows here too; none of these
                // has one today.
                Section {
                    ForEach(rows, id: \.self) { d in
                        NavigationLink(value: d) {
                            Label(d.title, systemImage: d.icon)
                        }
                    }
                }
            }
            .navigationTitle("More")
            .askButton()
            .navigationDestination(for: MoreDest.self) { d in
                switch d {
                case .config: ConfigView()
                case .settings: SettingsView()
                }
            }
            .sheet(isPresented: $showUpdate) { UpdateAppSheet() }
            .task {
                #if targetEnvironment(simulator)
                // ops/screens.sh more:update opens the modal for a screenshot.
                if ProcessInfo.processInfo.environment["LIFE_MORE"] == "update" { showUpdate = true }
                #endif
                await store.loadStatus(hub)
            }
        }
    }
}

// MARK: - Shared bits
// usd, tokenCount, parseDay, scheduleLabel and the other formatters: Format.swift.

/// One picker for thread check-in schedules, shared by the new-session sheet
/// and session settings. Keeps a non-preset current value selectable.
struct SchedulePicker: View {
    @Binding var schedule: String
    var offLabel = "off"
    static let presets = ["every@6h", "daily@08:00", "daily@18:00", "weekly@Mon 09:00", "weekly@Wed 09:00"]
    var body: some View {
        Picker("Check back", selection: $schedule) {
            Text(offLabel).tag("")
            ForEach(Self.presets, id: \.self) { Text(scheduleLabel($0)).tag($0) }
            if !schedule.isEmpty && !Self.presets.contains(schedule) { Text(scheduleLabel(schedule)).tag(schedule) }
        }
    }
}

extension Error {
    /// A cancelled request is always our own doing — a `.task(id:)` restarting
    /// because another range was tapped, a poll loop torn down when a screen
    /// closes — so it is never news. URLSession still reports it as an
    /// error whose entire message is "cancelled", which under a red warning
    /// triangle reads as if something of the owner's was cancelled.
    var isCancellation: Bool { self is CancellationError || (self as? URLError)?.code == .cancelled }
}

struct ErrorBanner: View {
    let message: String?
    var body: some View {
        if let message {
            Label(message, systemImage: "exclamationmark.triangle.fill")
                .font(.footnote).foregroundStyle(.red)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal)
        }
    }
}

struct Tile: View {
    let value: String, label: String
    /// Optional third line: what the number is OF. A money tile reading
    /// "$4,835 / one-off" says nothing about whose spend it is; the console
    /// has carried "accepted spend" under it from the start.
    var sub: String? = nil
    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            // A figure is one line or it is not a figure: three tiles across
            // (Recs: per month / one-off / traded) left "$12,319" as "$12,31"
            // over "9". Shrink before ever breaking the digits.
            Text(value).font(.title2.weight(.semibold)).monospacedDigit()
                .lineLimit(1).minimumScaleFactor(0.6)
            Text(label).font(.caption).foregroundStyle(.secondary)
            if let sub, !sub.isEmpty {
                Text(sub).font(.caption2).foregroundStyle(.tertiary)
            }
        }
        .tileCard()
    }
}

extension View {
    /// A statistic box that shares its row's height: `Tile` here.
    func tileCard(padding: CGFloat = 12, fill: Double = 0.5, radius: CGFloat = 12) -> some View {
        // Never let the row squeeze the label. `.frame(maxHeight: .infinity)`
        // below makes the tile accept whatever height it is offered, and an
        // HStack promptly offered the SHORT tile's height: "actually worked ·
        // 1 scored" stopped wrapping and truncated to "1 sco…". This pins the
        // ideal height first, so the HStack has to grow to the taller tile.
        fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(padding)
        // Fill the row's height, not just its width. Tiles come in HStack
        // pairs, and the width was equalised while the height was not: on
        // Recommendations "actually worked · 1 scored" wrapped to two lines,
        // so its box grew and the HStack centred the short "you accepted"
        // box against it — two panels in one row with neither their tops nor
        // their bottoms in line. The console's `.tiles` is a CSS grid, whose
        // items stretch by default, and has always looked right.
        // A tile that stands alone (Recommendations' "waiting on you") is in
        // a scrolling VStack, which proposes no height, so .infinity there
        // still resolves to the tile's own.
        .frame(maxHeight: .infinity, alignment: .top)
        .background(.quaternary.opacity(fill), in: RoundedRectangle(cornerRadius: radius))
    }
}
