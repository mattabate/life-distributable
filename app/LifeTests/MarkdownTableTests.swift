// A pipe table in a reply must be recognised the same way on both surfaces
// (otherwise a table comes out on the console as paragraphs of
// "| Date | Buy |"). ui.js mdTableRow/mdTableSep carry the same rules; these
// pin the phone's parser to them.
import XCTest
@testable import Life

final class MarkdownTableTests: XCTestCase {
    func testRowSplitsOnPipesOutsideCodeSpans() {
        XCTAssertEqual(mdTableRow("| Date | Buy |"), ["Date", "Buy"])
        XCTAssertEqual(mdTableRow("a | `b | c` | d"), ["a", "`b | c`", "d"])
        XCTAssertEqual(mdTableRow("| **Today, 09-17** | 1 AMZN + 1 NVDA |"), ["**Today, 09-17**", "1 AMZN + 1 NVDA"])
    }

    // The fold line is the console's (ui.js isLongText): >18 non-blank lines
    // or >1600 characters, blanks not counted.
    func testLongTextFoldsLikeTheConsole() {
        XCTAssertFalse(isLongText("short"))
        XCTAssertFalse(isLongText(Array(repeating: "a line", count: 18).joined(separator: "\n")))
        XCTAssertFalse(isLongText(Array(repeating: "a line", count: 18).joined(separator: "\n\n")))
        XCTAssertTrue(isLongText(Array(repeating: "a line", count: 19).joined(separator: "\n")))
        XCTAssertTrue(isLongText(String(repeating: "x", count: 1601)))
        XCTAssertEqual(longLineCount("a\n\n b \n"), 2)
    }

    func testProseWithoutPipesIsNotARow() {
        XCTAssertNil(mdTableRow("plain words"))
    }

    func testSeparatorRow() {
        XCTAssertEqual(mdTableSeparator("|---|---|"), [.left, .left])
        XCTAssertEqual(mdTableSeparator("| :--- | :---: | ---: |"), [.left, .center, .right])
        XCTAssertNil(mdTableSeparator("| Date | Buy |"))
        XCTAssertNil(mdTableSeparator("|--|x|"))
    }

    // Fenced blocks, the same cases as ui.test.js: verbatim
    // text, the list-item indent given up, an unclosed fence runs to the end.
    func testFencedBlocksSplitTheText() {
        XCTAssertEqual(mdSegments("1. Paste:\n```json\n{\n  \"a\": [\n    \"**b**\"\n  ]\n}\n```\n2. Save"),
                       [.text("1. Paste:"), .code("{\n  \"a\": [\n    \"**b**\"\n  ]\n}", lang: "json"), .text("2. Save")])
        XCTAssertEqual(mdSegments("- run\n  ```\n  a\n    b\n  ```"), [.text("- run"), .code("a\n  b", lang: "")])
        XCTAssertEqual(mdSegments("```sh\nls -la"), [.code("ls -la", lang: "sh")])
        XCTAssertEqual(mdSegments("three ``` ticks in prose"), [.text("three ``` ticks in prose")])
    }
}
