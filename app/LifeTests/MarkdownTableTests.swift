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

    /// The console's 'md: a line that is only a hub file…' case (ui.test.js).
    func testALineThatIsOnlyAHubFileIsTheFile() {
        let ref = "sha256/a6/a620d8.png"
        for line in ["![before](https://host.example.ts.net:8443/api/v1/blobs/\(ref))",
                     "[shot](https://host:8443/api/v1/blobs/\(ref))",
                     "https://host:8443/api/v1/blobs/\(ref)", "  /api/v1/blobs/\(ref)  "] {
            XCTAssertEqual(blobLineRef(line), ref, line)
        }
        XCTAssertNil(blobLineRef("see ![x](https://h/api/v1/blobs/\(ref)) here"))
        XCTAssertNil(blobLineRef("- ![x](https://h/api/v1/blobs/\(ref))"))
        XCTAssertNil(blobLineRef("/api/v1/blobs/../secret"))
        XCTAssertEqual(mdSegments("**Now**\n\n![now](https://h:8443/api/v1/blobs/\(ref))\n\nafter"),
                       [.text("**Now**"), .files([MdFile(ref: ref, caption: "now")]), .text("after")])
        // Files on lines in a row, blank lines between, are one strip.
        XCTAssertEqual(mdSegments("![before](/api/v1/blobs/sha256/00/b.png)\n\n![after](/api/v1/blobs/sha256/00/a.png)\nend"),
                       [.files([MdFile(ref: "sha256/00/b.png", caption: "before"), MdFile(ref: "sha256/00/a.png", caption: "after")]), .text("end")])
    }

    /// A table is its own segment, the grid: the paragraph above stays prose
    /// even with no blank line between.
    func testATableIsItsOwnSegment() {
        XCTAssertEqual(mdSegments("Your steer:\n| a | b |\n|---|--:|\n| 1 | 2 |\nafter"),
                       [.text("Your steer:"), .table([["a", "b"], ["1", "2"]], header: true, align: [.left, .right]), .text("after")])
        XCTAssertEqual(mdSegments("| a | b |\n| c | d |"), [.table([["a", "b"], ["c", "d"]], header: false, align: [])])
    }
}
