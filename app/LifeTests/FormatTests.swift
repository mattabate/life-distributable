// Money and counts read the same on the phone, the console (ui.test.js) and in
// hub-worded labels (internal/format): all three suites run this one table.
import XCTest
@testable import Life

final class FormatTests: XCTestCase {
    // The tool chain under a working line, the console's toolChain
    // (sending.test.js): the same events, the same rows.
    func testToolChain() {
        func ev(_ id: Int64, _ kind: String, _ title: String, _ body: String = "", _ summary: String = "") -> ThreadEvent {
            ThreadEvent(id: id, run_id: "r", kind: kind, title: title, body: body, summary: summary)
        }
        let evs = [
            ev(1, "tool_result", "result", "answers a call before the window"),
            ev(2, "tool_use", "Bash · make check", "", "Run the checks"),
            ev(3, "tool_result", "error", "Exit code 1"),
            ev(4, "tool_use", "Agent · Survey the literature", "", "Delegate: Survey the literature"),
            ev(5, "tool_result", "result", "Async agent launched successfully."),
            ev(6, "tool_use", "Agent · Review the diff", "", "Delegate: Review the diff"),
            ev(7, "thinking", "", "hmm"),
            ev(8, "tool_use", "Read · README.md", "", "Read README.md"),
        ]
        let ch = toolChain(evs, tasks: nil)
        XCTAssertEqual(ch.steps.map(\.label), ["Run the checks", "Survey the literature", "Review the diff", "Read README.md"])
        XCTAssertEqual(ch.steps.map(\.open), [false, false, true, true])
        XCTAssertEqual(ch.steps.map(\.error), [true, false, false, false])
        XCTAssertEqual(ch.helpers, ["Survey the literature", "Review the diff"])
        XCTAssertEqual(ch.waiting, ["Read README.md"])
        XCTAssertEqual(toolChain(evs, tasks: []).helpers, ["Review the diff"])
        XCTAssertEqual(toolChain(evs, tasks: [TurnTask(id: "a", type: "local_agent", subagent_type: nil, description: "Survey the literature"),
                                              TurnTask(id: "b", type: "local_bash", subagent_type: nil, description: "make ship")]).helpers,
                       ["Survey the literature", "Review the diff"])
        XCTAssertEqual(helpersWords(1), "1 subagent running")
        XCTAssertEqual(helpersWords(3), "3 subagents running")
    }

    func testSharedCases() throws {
        // The simulator shares the Mac's filesystem, so the repo file is readable.
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("shared/format-cases.json")
        let obj = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any] ?? [:]
        let fns: [String: (Double) -> String] = [
            "usd": usd, "usdWhole": usd0, "usdSigned": usdSigned, "usdWholeSigned": usd0Signed,
            "usdShort": usdShort, "usdPrice": usdPrice, "commas": { commas(Int($0)) },
        ]
        for (name, value) in obj where name != "_" {
            let fn = try XCTUnwrap(fns[name], "no phone formatter for \(name)")
            for row in value as? [[Any]] ?? [] {
                let v = (row[0] as? NSNumber)?.doubleValue ?? .nan, want = row[1] as? String
                XCTAssertEqual(fn(v), want, "\(name)(\(v))")
            }
        }
    }

    // The console's rules the shared table does not pin (parity audit 09-27).

    func testTiesRoundUpAsJavaScriptDoes() {
        XCTAssertEqual(fixed(0.125, 2), "0.13")
        XCTAssertEqual(fixed(12.5, 0), "13")
        XCTAssertEqual(fixed(1.005, 2), "1.00")  // really 1.00499…
        XCTAssertEqual(fixed(-0.125, 2), "-0.13")
        XCTAssertEqual(usdPrice(0.125), "$0.13")
        XCTAssertEqual(jsRound(2.5), 3)
        XCTAssertEqual(jsRound(-2.5), -2)
        XCTAssertEqual(num(-2.5), "−2")
        XCTAssertEqual(num(-0.4), "0")
        XCTAssertEqual(signed(0.5), "+1")
        XCTAssertEqual(signed(-0.5), "0")
        XCTAssertEqual(ratioPct(1, 8), "13%")
    }

    func testPctDeltaThreshold() {
        XCTAssertEqual(pctDelta(5, 100), "+5.0%")
        XCTAssertEqual(pctDelta(-9.94, 100), "−9.9%")
        XCTAssertEqual(pctDelta(12, 100), "+12%")
        XCTAssertEqual(pctDelta(10, -100), "+10%")
        XCTAssertEqual(pctDelta(5, 100, 0), "+5%")
        XCTAssertEqual(pctDelta(1, 0), "")
    }

    func testDaysCarryTheYear() {
        XCTAssertEqual(shortDay("2025-09-08"), "Sep 8, 2025")
        XCTAssertEqual(dayLabel("2025-09-08"), "Sep 8")
        XCTAssertEqual(fullDay("2025-09-08"), "Mon, Sep 8, 2025")
    }

    func testAxisPct() {
        XCTAssertEqual(axisPct(0), "0%")
        XCTAssertEqual(axisPct(5), "5.0%")
        XCTAssertEqual(axisPct(12.5), "13%")
        XCTAssertEqual(axisPct(-12), "−12%")
        XCTAssertEqual(pctShort(1234), "1234%")
    }

    func testNotANumberIsZeroDollars() {
        XCTAssertEqual(usd(.nan), "$0")
        XCTAssertEqual(usd0(.infinity), "$0")
        XCTAssertEqual(usdShort(.nan), "$0")
        XCTAssertEqual(usdPrice(.nan), "$0.00")
        XCTAssertEqual(num(.nan), "0")
    }

    /// The chat row's clock says its day (ui.test.js `dayWhen`, same table).
    func testDayClock() {
        let cal = Calendar.current
        func at(_ y: Int, _ mo: Int, _ d: Int, _ h: Int, _ mi: Int) -> Date {
            cal.date(from: DateComponents(year: y, month: mo, day: d, hour: h, minute: mi))!
        }
        let now = at(2026, 10, 7, 21, 16) // Wed
        XCTAssertEqual(dayClock(at(2026, 10, 7, 0, 53), now: now), "today 12:53 AM")
        XCTAssertEqual(dayClock(at(2026, 10, 7, 15, 15), now: now), "today 3:15 PM")
        XCTAssertEqual(dayClock(at(2026, 10, 6, 23, 59), now: now), "yesterday 11:59 PM")
        XCTAssertEqual(dayClock(at(2026, 10, 5, 9, 15), now: now), "Mon 9:15 AM")
        XCTAssertEqual(dayClock(at(2026, 10, 1, 16, 2), now: now), "Thu 4:02 PM")
        XCTAssertEqual(dayClock(at(2026, 9, 30, 16, 2), now: now), "Sep 30 4:02 PM")
        XCTAssertEqual(dayClock(at(2025, 10, 1, 16, 2), now: now), "Oct 1, 2025 4:02 PM")
        // The turn in flight, the console's turnFacts word for word.
        XCTAssertEqual(turnFactsLine(calls: 23, at: at(2026, 10, 7, 22, 9), cost: 1.12, now: now), "23 tool calls · today 10:09 PM · $1.12")
        XCTAssertEqual(turnFactsLine(calls: 23, at: at(2026, 10, 7, 22, 9), cost: 1.12, withCalls: false, now: now), "today 10:09 PM · $1.12")
        XCTAssertEqual(turnFactsLine(calls: 23, at: at(2026, 10, 7, 22, 9), cost: 1.12, withCost: false, now: now), "23 tool calls · today 10:09 PM")
        XCTAssertEqual(turnFactsLine(calls: 1, at: nil, cost: nil, now: now), "1 tool call")
        XCTAssertEqual(turnFactsLine(calls: nil, at: nil, cost: 0, now: now), "")
    }

    func testTokens() {
        XCTAssertEqual(tokenCount(1_250_000), "1.3M")
        XCTAssertEqual(tokens(1_250_000), "1.3M tok")
        XCTAssertEqual(tokens(312_400), "312k tok")
        XCTAssertEqual(tokens(740), "740 tok")
    }

    /// Grouping and month names are fixed to en_US, whatever the device says.
    func testLocaleIndependent() {
        XCTAssertEqual(commas(1234), "1,234")
        XCTAssertEqual(num(1234567), "1,234,567")
        XCTAssertEqual(Fmt.shortDay.locale.identifier, "en_US_POSIX")
        XCTAssertEqual(Fmt.enUS.identifier, "en_US")
    }
}
