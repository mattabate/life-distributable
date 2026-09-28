// A bare hub id in a card's text is a link on both surfaces. The console (ui.test.js, mdInline) and the phone (linkRefs,
// which StyledText runs every line through) read this one table: text → the
// ids that come out linked, in order.
import XCTest
@testable import Life

final class RefTests: XCTestCase {
    /// The ids behind every `life://open/<id>` run, as the renderer sees them.
    private func linked(_ s: String) -> [String] {
        let a = md(linkRefs(s))
        return a.runs.compactMap { run in
            guard let u = run.link, u.scheme == "life", u.host() == "open" else { return nil }
            return u.lastPathComponent
        }
    }

    func testSharedCases() throws {
        // The simulator shares the Mac's filesystem, so the repo file is readable.
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("shared/ref-cases.json")
        let obj = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any] ?? [:]
        let rows = try XCTUnwrap(obj["links"] as? [[Any]])
        XCTAssertFalse(rows.isEmpty)
        for row in rows {
            let src = try XCTUnwrap(row[0] as? String), want = try XCTUnwrap(row[1] as? [String])
            XCTAssertEqual(linked(src), want, src)
        }
    }

    func testTextIsUntouchedAroundTheLink() {
        XCTAssertEqual(linkRefs("Decide rec-120a5f85, then go"), "Decide [rec-120a5f85](life://open/rec-120a5f85), then go")
        XCTAssertEqual(linkRefs("no ids - here"), "no ids - here")
        XCTAssertEqual(String(md(linkRefs("see `ask-955bfb8e` now")).characters), "see ask-955bfb8e now")
    }

    func testKindByShape() {
        XCTAssertEqual(refKind("rec-120a5f85"), "rec")
        XCTAssertEqual(refKind("ask-955bfb8e"), "ask")
        XCTAssertEqual(refKind("cal-53e0"), "cal")
        XCTAssertEqual(refKind("20260907-053232-0a1b2c3d"), "action")
    }
}
