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

    /// #157: a [label](url) is one link, its URL whole; the same "anchors"
    /// rows the console's mdInline reads.
    func testSharedAnchors() throws {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("shared/ref-cases.json")
        let obj = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any] ?? [:]
        let rows = try XCTUnwrap(obj["anchors"] as? [[Any]])
        XCTAssertFalse(rows.isEmpty)
        for row in rows {
            let src = try XCTUnwrap(row[0] as? String), want = try XCTUnwrap(row[1] as? [[String]])
            let a = md(linkRefs(src))
            var got: [[String]] = []
            var last: URL?
            for run in a.runs {
                guard let u = run.link else { last = nil; continue }
                let words = String(a[run.range].characters)
                if u == last, var tail = got.popLast() { tail[0] += words; got.append(tail) } else { got.append([words, u.absoluteString]) }
                last = u
            }
            XCTAssertEqual(got, want, src)
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

    // A link in a session's text may open a page or mail, an install only
    // from our own hub, and a blob the Mac saved opens only when it is a
    // picture, sound, video or document.
    func testOnlySafeLinksOpen() {
        let hub = "https://hub.example.ts.net:8443"
        let ok = { (s: String) in LifeLinks.mayOpen(URL(string: s)!, hubBase: hub) }
        XCTAssertTrue(ok("https://github.com/x"))
        XCTAssertTrue(ok("mailto:a@example.com"))
        XCTAssertTrue(ok("itms-services://?action=download-manifest&url=https%3A%2F%2Fhub.example.ts.net%3A8443%2Fota%2Fx%2Fmanifest.plist"))
        XCTAssertFalse(ok("itms-services://?action=download-manifest&url=https%3A%2F%2Fevil.example.com%2Fmanifest.plist"))
        XCTAssertFalse(ok("itms-services://?action=download-manifest"))
        XCTAssertFalse(LifeLinks.mayOpen(URL(string: "itms-services://?action=download-manifest&url=https%3A%2F%2Fhub.example.ts.net%2Fm.plist")!, hubBase: ""))
        XCTAssertFalse(ok("file:///Applications/Calculator.app"))
        XCTAssertFalse(ok("x-apple-systempreferences:com.apple.preference.security"))
        XCTAssertFalse(ok("shortcuts://run-shortcut?name=x"))

        XCTAssertTrue(macOpensSaved(URL(fileURLWithPath: "/tmp/shot.PNG")))
        XCTAssertTrue(macOpensSaved(URL(fileURLWithPath: "/tmp/report.pdf")))
        XCTAssertFalse(macOpensSaved(URL(fileURLWithPath: "/tmp/run.command")))
        XCTAssertFalse(macOpensSaved(URL(fileURLWithPath: "/tmp/link.webloc")))
        XCTAssertFalse(macOpensSaved(URL(fileURLWithPath: "/tmp/noext")))
    }
}
