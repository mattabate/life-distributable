// Drive the phone app in the Simulator — tap, type, swipe, screenshot — so a
// session can WALK a flow.
//
// `ops/screens.sh` launches the app straight into a tab and photographs it;
// nothing behind a tap (an account's own page, a sheet, a context menu, the
// composer mid-type, Respond's chips) could be looked at by anything but
// the owner. This is one test that reads a STEP LIST from the environment, so the
// step list lives in the caller (ops/app-ui.sh) and this file never changes
// when the flow does.
//
// Run it with `make app-ui STEPS='tab more; tap Spend; shot spend; tap
// Goals; shot goals'` — never directly; the wrapper passes the hub
// token, the output directory and the steps as TEST_RUNNER_* variables.
//
// Steps (verb + argument):
//   tab <sessions|recs|calendar|more>  tap that tab bar button
//   tap <text>          first hittable element whose label/id contains <text>
//   tapid <identifier>  exact accessibility identifier
//   type <text>         type into whatever has keyboard focus
//   copy <text>         put text on the device clipboard (for a Paste button)
//   alert <button>      tap a button on a system sheet ("Allow Paste")
//   swipe <up|down|left|right>[ xN]
//   back                the navigation bar's back button
//   wait <seconds>
//   shot <name>         PNG → $LIFE_UI_OUT/NN-<name>.png (also attached)
//   tree <name>         the accessibility tree → $LIFE_UI_OUT/NN-<name>.txt
//   text <substring>    report whether it is on screen (never fails the run)
//   assert <substring>  FAIL the test if it is not on screen
import XCTest

@MainActor
final class FlowTests: XCTestCase {
    private var app: XCUIApplication!
    private var out: String = ""
    private var step = 0

    override func setUp() {
        continueAfterFailure = true
    }

    func testFlow() throws {
        let env = ProcessInfo.processInfo.environment
        let steps = (env["LIFE_UI_STEPS"] ?? "")
            .split(whereSeparator: { $0 == ";" || $0 == "\n" })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty && !$0.hasPrefix("#") }
        out = env["LIFE_UI_OUT"] ?? NSTemporaryDirectory()
        try? FileManager.default.createDirectory(atPath: out, withIntermediateDirectories: true)

        app = XCUIApplication()
        // The app reads the same variables ops/screens.sh uses, so a flow can
        // start anywhere: a tab, a thread, the composer, a More screen, or the
        // thing behind an id (OPEN=rec-…, as if that id had been tapped).
        for key in ["LIFE_HUB_TOKEN", "LIFE_TAB", "LIFE_THREAD", "LIFE_MORE", "LIFE_COMPOSE", "LIFE_OPEN"] {
            if let v = env[key], !v.isEmpty { app.launchEnvironment[key] = v }
        }
        app.launch()
        // The first screen waits on the hub; a step list that opens with a tap
        // would otherwise race the very first render.
        _ = app.wait(for: .runningForeground, timeout: 30)
        Thread.sleep(forTimeInterval: 3)

        for s in steps {
            let parts = s.split(separator: " ", maxSplits: 1).map(String.init)
            let verb = parts[0].lowercased()
            let arg = parts.count > 1 ? parts[1] : ""
            step += 1
            run(verb: verb, arg: arg)
        }
        // Always end with a picture: a flow whose last step is a tap is
        // otherwise a list of clicks nobody can see the result of.
        shot(name: "final")
    }

    private func run(verb: String, arg: String) {
        switch verb {
        case "tab":
            let labels = ["sessions": "Sessions", "recs": "Recs", "calendar": "Calendar",
                          "more": "More"]
            let want = labels[arg.lowercased().replacingOccurrences(of: " ", with: "")] ?? arg
            let b = app.tabBars.buttons[want]
            if b.waitForExistence(timeout: 10) { b.tap(); log("tab \(want)") }
            else { log("tab \(want) → NOT FOUND") }
            settle()
        case "tap":
            tap(matching: arg, exactID: false)
        case "tapid":
            tap(matching: arg, exactID: true)
        case "type":
            app.typeText(arg)
            log("type \(arg.prefix(40))")
            settle()
        case "swipe":
            let bits = arg.split(separator: " ")
            let dir = (bits.first.map(String.init) ?? "up").lowercased()
            var times = 1
            if bits.count > 1, let n = Int(bits[1].replacingOccurrences(of: "x", with: "")) { times = n }
            for _ in 0..<max(1, times) {
                switch dir {
                case "down": app.swipeDown()
                case "left": app.swipeLeft()
                case "right": app.swipeRight()
                default: app.swipeUp()
                }
            }
            log("swipe \(dir) x\(times)")
            settle()
        case "back":
            // boundBy(0) is enumeration order, not screen position — on a
            // root tab (nothing pushed, e.g. a prior `tap` that didn't find
            // its target) the only nav-bar button is a TRAILING one (the
            // Snap camera FAB every screen carries, or a refresh button),
            // and blindly tapping it opened "Session from this screen" and
            // its mic-permission alert, which then blocked every following
            // tap for the rest of the run. A genuine back
            // chevron is always leading, so require the button to sit in the
            // left ~15% of the bar.
            let bar = app.navigationBars.firstMatch
            let leading = app.navigationBars.buttons.allElementsBoundByIndex
                .first { $0.frame.minX < bar.frame.minX + bar.frame.width * 0.15 }
            if let b = leading, b.exists { b.tap(); log("back") } else { log("back → no leading nav bar button") }
            settle()
        case "copy":
            // The Simulator's clipboard is one per device, so what the runner
            // copies is what the app's Paste buttons read — and because it
            // came from another app, iOS asks before the app may read it,
            // the same sheet the phone shows after a copy in Passwords.
            UIPasteboard.general.string = arg
            log("copy \(arg.prefix(40))")
        case "alert":
            // A system sheet (Allow Paste, a permission) belongs to
            // SpringBoard, not the app, so `tap` never finds its buttons.
            let b = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons[arg]
            if b.waitForExistence(timeout: 4) { b.tap(); log("alert \(arg)") } else { log("alert \(arg) → NOT SHOWN") }
            settle()
        case "wait":
            Thread.sleep(forTimeInterval: Double(arg) ?? 1)
            log("wait \(arg)s")
        case "shot":
            shot(name: arg.isEmpty ? "shot" : arg)
        case "tree":
            let name = arg.isEmpty ? "tree" : arg
            write(app.debugDescription.data(using: .utf8) ?? Data(), name: name, ext: "txt")
        case "text":
            log("text \"\(arg)\" → \(onScreen(arg) ? "present" : "absent")")
        case "assert":
            let ok = onScreen(arg)
            log("assert \"\(arg)\" → \(ok ? "present" : "ABSENT")")
            XCTAssertTrue(ok, "not on screen: \(arg)")
        default:
            log("unknown step: \(verb)")
        }
    }

    // Text is how a person names a control, so that is what a step names too.
    // Ranked, not first-match: an exact label beats a prefix beats a
    // substring, and the shortest label wins the tie — otherwise a longer
    // row that merely contains the word can be found first and tapped.
    private func tap(matching arg: String, exactID: Bool) {
        for attempt in 0..<6 {
            if let (e, why) = best(matching: arg, exactID: exactID) {
                // Read the label BEFORE the tap: afterwards the element is a
                // stale query against a screen that has moved on, and it
                // resolves to "".
                let label = e.label.prefix(40)
                e.tap()
                log("tap \(arg) → \(why) \"\(label)\"")
                settle()
                return
            }
            // A row can simply be below the fold.
            if attempt < 5 { app.swipeUp(); Thread.sleep(forTimeInterval: 0.6) }
        }
        log("tap \(arg) → NOT FOUND (after scrolling)")
    }

    private func best(matching arg: String, exactID: Bool) -> (XCUIElement, String)? {
        let p = exactID
            ? NSPredicate(format: "identifier == %@", arg)
            // placeholderValue: an empty field in a labelled row is named only
            // by its placeholder ("paste from Apple Passwords").
            : NSPredicate(format: "label CONTAINS[c] %@ OR identifier CONTAINS[c] %@ OR placeholderValue CONTAINS[c] %@", arg, arg, arg)
        // ONE query over every element type. Asking type by type meant asking
        // a type nothing on screen has (.link in SwiftUI), and resolving a
        // snapshot for an empty query fails the whole test rather than
        // returning nothing.
        let q = app.descendants(matching: .any).matching(p)
        guard q.firstMatch.waitForExistence(timeout: 2) else { return nil }
        let want = arg.lowercased()
        var pick: (element: XCUIElement, rank: Int, kind: Int, len: Int, why: String)?
        for e in q.allElementsBoundByIndex.prefix(30) {
            guard e.isHittable else { continue }
            let label = e.label.lowercased(), id = e.identifier.lowercased()
            let rank = (label == want || id == want) ? 0
                     : (label.hasPrefix(want) || id.hasPrefix(want)) ? 1 : 2
            // A control beats the text inside it when both match the words.
            let kind: Int
            switch e.elementType {
            case .button, .cell: kind = 0
            case .staticText, .image: kind = 1
            default: kind = 2
            }
            let cand = (e, rank, kind, e.label.count, "rank\(rank)/kind\(kind)")
            if pick == nil || (rank, kind, e.label.count) < (pick!.rank, pick!.kind, pick!.len) { pick = cand }
        }
        guard let got = pick else { return nil }
        return (got.element, got.why)
    }

    private func onScreen(_ s: String) -> Bool {
        let p = NSPredicate(format: "label CONTAINS[c] %@", s)
        return app.descendants(matching: .any).matching(p).count > 0
    }

    private func shot(name: String) {
        let png = XCUIScreen.main.screenshot().pngRepresentation
        write(png, name: name, ext: "png")
        // The attachment is the fallback lane: if the simulator cannot write
        // to the host path, the picture still rides inside the .xcresult and
        // ops/app-ui.sh exports it from there.
        let a = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        a.name = "\(String(format: "%02d", step))-\(name).png"
        a.lifetime = .keepAlways
        add(a)
    }

    private func write(_ data: Data, name: String, ext: String) {
        let file = "\(out)/\(String(format: "%02d", step))-\(name).\(ext)"
        do {
            try data.write(to: URL(fileURLWithPath: file))
            log("\(ext) → \(file) (\(data.count) bytes)")
        } catch {
            log("\(ext) \(name) → could not write \(file): \(error.localizedDescription)")
        }
    }

    // A tap is not the same as the screen being finished: the push animates,
    // then the new screen fetches from the hub. At 1.5 s the first account
    // screenshot came out mid-transition — the row still highlighted, the old
    // screen still sliding out — so the wait is long enough for both.
    private func settle() { Thread.sleep(forTimeInterval: 2.75) }

    private func log(_ s: String) { print("UISTEP \(String(format: "%02d", step)) \(s)") }
}
