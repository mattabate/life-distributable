// Money and counts read the same on the phone, the console (ui.test.js) and in
// hub-worded labels (internal/format): all three suites run this one table.
import XCTest
@testable import Life

final class FormatTests: XCTestCase {
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
