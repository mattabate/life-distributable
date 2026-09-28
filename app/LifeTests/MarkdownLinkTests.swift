// Bare-URL autolinking must follow GFM's trailing-punctuation rule: an ask
// detail ending "…pull/82, merge when ready" must not link the comma — that
// URL 404s on GitHub. The web
// console (ui.js mdInline) and the ask page (hub markdown.go) carry the same
// rule; these pin the phone's renderer to it.
import XCTest
@testable import Life

final class MarkdownLinkTests: XCTestCase {
    /// Every linked run in the app's one-liner renderer: (visible text, href).
    private func links(_ s: String) -> [(text: String, url: String)] {
        let a = md(s)
        var out: [(String, String)] = []
        for run in a.runs {
            if let u = run.link { out.append((String(a[run.range].characters), u.absoluteString)) }
        }
        return out
    }

    func testTrailingCommaStaysOutOfTheLink() {
        let got = links("x https://a.com/1, y")
        XCTAssertEqual(got.map(\.url), ["https://a.com/1"], "got: \(got)")
    }

    func testTrailingPeriodStaysOutOfTheLink() {
        let got = links("x https://a.com/1. y")
        XCTAssertEqual(got.map(\.url), ["https://a.com/1"], "got: \(got)")
    }

    func testWrappingParenStaysOutOfTheLink() {
        let got = links("x (see https://a.com/1) y")
        XCTAssertEqual(got.map(\.url), ["https://a.com/1"], "got: \(got)")
    }

    func testBalancedParenStaysInTheLink() {
        let got = links("x https://en.wikipedia.org/wiki/A_(b) y")
        XCTAssertEqual(got.map(\.url), ["https://en.wikipedia.org/wiki/A_(b)"], "got: \(got)")
    }

    func testExplicitLinkKeepsItsExactURL() {
        let got = links("[PR](https://g.test/pull/82), then")
        XCTAssertEqual(got.map(\.url), ["https://g.test/pull/82"], "got: \(got)")
        XCTAssertEqual(got.map(\.text), ["PR"])
    }
}
