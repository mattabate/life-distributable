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

    // The grey-detail fold is the console's (ui.js actedDetail / askFolds;
    // ui.test.js has the same cases).
    @MainActor func testActedKeepsWhatIsActedOnLikeTheConsole() {
        func kept(_ s: String) -> String { Card.acted(s, fold: true).kept }
        let d = "Why this matters, at length.\n\n1. Open the page\n2. Click Save\n\nDraft:\n\n```\nHi Sam, yes.\n```\n\n- a bullet of background\n\nSee [the doc](https://x.test/doc)."
        let a = Card.acted(d, fold: true)
        XCTAssertEqual(a.kept, "1. Open the page\n2. Click Save\n\nDraft:\n\n```\nHi Sam, yes.\n```\n\nSee [the doc](https://x.test/doc).")
        XCTAssertEqual(a.rest, "Why this matters, at length.\n\n- a bullet of background")
        XCTAssertEqual(Card.acted(d, fold: false).kept, d)
        XCTAssertEqual(Card.acted(d, fold: false).rest, "")
        XCTAssertEqual(kept("Background.\n\n**Draft:**\n\n```\nHi\n```"), "**Draft:**\n\n```\nHi\n```")
        XCTAssertEqual(kept("Background.\n\n## The draft\n\n```\nHi\n```"), "## The draft\n\n```\nHi\n```")
        XCTAssertEqual(kept("1. Open the page\n\n   It is the blue one.\n\n2. Save\n\nWhy."), "1. Open the page\n\n   It is the blue one.\n\n2. Save")
        XCTAssertEqual(kept("1. Open it\n\nIt is the blue one.\n\n2. Save\n\nWhy."), "1. Open it\n\nIt is the blue one.\n\n2. Save")
        XCTAssertEqual(kept("```\na\n~~~\n\nb\n```\n\nWhy."), "```\na\n~~~\n\nb\n```")
        XCTAssertEqual(kept("Why.\r\n\r\n1. Go\r\n"), "1. Go")
        XCTAssertEqual(kept("Why.\n\nRun this:\n\n    make ship"), "Run this:\n\n    make ship")
        for b in ["- [ ] Sign the form", "Run `make ship` there.", "Write to sam@example.org today.", "Call 800-555-0128 after two.",
                  "It is on example.com under Book.", "Day | Price\n--- | ---\nMon | 4"] {
            XCTAssertEqual(kept("Why.\n\n" + b), b)
        }
        XCTAssertEqual(kept("On 2026-11-22 it moved.\n\n- background"), "")
        // A list that opens the detail is the steps, bulleted or not; a file
        // on the hub written as a bare path is something to open.
        XCTAssertEqual(kept("- Restart the modem\n- Tell me the light colour\n\nWhy."), "- Restart the modem\n- Tell me the light colour")
        XCTAssertEqual(kept("Why.\n\n/api/v1/blobs/plan.pdf"), "/api/v1/blobs/plan.pdf")
    }

    @MainActor func testOnlyASessionsOwnLineOverSomethingToDoFolds() {
        let own = "Hey Sam, it's your chief of staff. The page is ready and wants one click from you before Friday, then I finish the rest."
        let t = "Save the page", s = "Plan lunches"
        XCTAssertTrue(Card.folds(kind: "physical", said: own, title: t, thread: s))
        XCTAssertTrue(Card.folds(kind: "read", said: own, title: t, thread: s))
        XCTAssertFalse(Card.folds(kind: "physical", said: "Hey Sam, it's your chief of staff. Save the page.", title: t, thread: s))
        XCTAssertFalse(Card.folds(kind: "physical", said: "", title: t, thread: s))
        for kind in ["decision", "install", "error"] { XCTAssertFalse(Card.folds(kind: kind, said: own, title: t, thread: s)) }
        // The hub's line names the session too, however long its name is; a
        // line cut short leaves the only whole copy in the detail.
        let named = "Sort the garage shelves before the movers come on Friday"
        XCTAssertFalse(Card.folds(kind: "physical", said: "Hey, Save the page. It's for \(named).", title: t, thread: named))
        XCTAssertFalse(Card.folds(kind: "read", said: "Hey, about \(named). Save the page.", title: t, thread: named))
        XCTAssertFalse(Card.folds(kind: "physical", said: own + " And then a great deal more…", title: t, thread: s))
        // A first paragraph behind More does not stand in for the title.
        XCTAssertTrue(Card.titled(title: "Renew passport", said: "", shown: "1. Book the slot"))
        XCTAssertFalse(Card.titled(title: "Renew passport", said: "", shown: "Renew passport by March.\n\n1. Book the slot"))
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
        // A link line right above a fence heads the box; a blank line between keeps it a line.
        XCTAssertEqual(mdSegments("Drafts:\n[Sam Lee](https://m.test/1)\n```\nHi Sam\n```"),
                       [.text("Drafts:"), .code("Hi Sam", lang: "", head: MdLink(line: "[Sam Lee](https://m.test/1)"))])
        XCTAssertEqual(mdSegments("[Sam](https://m.test/1)\n\n```\nHi\n```"), [.text("[Sam](https://m.test/1)"), .code("Hi", lang: "")])
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

    /// Math (MathText): a paragraph holding it is its own segment, by the
    /// console's rule; money is not math.
    func testAParagraphWithMathIsItsOwnSegment() {
        XCTAssertEqual(mdSegments("Euler:\n\n$e^{i\\pi} + 1 = 0$ holds.\n\nDone.", math: true),
                       [.text("Euler:"), .math("$e^{i\\pi} + 1 = 0$ holds."), .text("Done.")])
        XCTAssertEqual(mdSegments("Sum:\n$$\n\\sum_n n\n$$\nafter\n\nnext", math: true),
                       [.math("Sum:\n$$\n\\sum_n n\n$$\nafter"), .text("next")])
        for money in ["$12 and $15", "$5-$10 a month", "$12+$15=$27", "$PATH:$HOME"] {
            XCTAssertFalse(hasMath(money), money)
        }
        XCTAssertTrue(hasMath("so $x$."))
        XCTAssertEqual(mdSegments("a $x$ b"), [.text("a $x$ b")])
    }
}
