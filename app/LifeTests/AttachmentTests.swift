// A file attached to a message arrives as the file: never its CSV typed in
// as the message text, never a chip that shows and sends nothing. Two things hold
// that up: the box hands a dropped or pasted file to the composer instead of
// typing it in, and a send takes the documents along with the pictures.
import XCTest
import UIKit
import UniformTypeIdentifiers
@testable import Life

@MainActor
final class AttachmentTests: XCTestCase {
    private let body = "Account Statement ,,,\nID,Datetime,Amount\n1,2026-09-01,-5.00\n"

    private func csv() throws -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("att-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent("statement_september.csv")
        try Data(body.utf8).write(to: url)
        return url
    }

    /// The box with its file road open, and what came down it.
    private func box() -> (PasteTextView, () -> [AttachedFile]) {
        let v = PasteTextView()
        final class Got { var files: [AttachedFile] = [] }
        let got = Got()
        v.onPasteFiles = { got.files += $0 }
        return (v, { got.files })
    }

    private func settle(_ done: () -> Bool) async {
        for _ in 0..<40 where !done() { try? await Task.sleep(for: .milliseconds(50)) }
    }

    /// The session composer's send moves the whole box out: pictures, their
    /// library ids AND documents. It copied the pictures alone.
    func testTakeCarriesDocuments() {
        let draft = AttachmentDraft()
        draft.add(UIImage(), assetID: "a1")
        draft.add(AttachedFile(name: "statement.csv", data: Data(body.utf8)))
        let taken = draft.take()
        XCTAssertTrue(draft.isEmpty)
        XCTAssertEqual(taken.images.count, 1)
        XCTAssertEqual(taken.assetIDs, ["a1"])
        XCTAssertEqual(taken.files.map(\.name), ["statement.csv"])
        // A failed send puts all of it back.
        draft.restore(taken)
        XCTAssertEqual(draft.count, 2)
        XCTAssertEqual(draft.files.first?.data, Data(body.utf8))
    }

    /// A picked or dropped picture is a picture; a CSV stays a document.
    func testPictureFilesJoinThePictures() throws {
        let draft = AttachmentDraft()
        let png = try XCTUnwrap(UIGraphicsImageRenderer(size: CGSize(width: 4, height: 4)).image { _ in }.pngData())
        draft.add(AttachedFile(name: "shot.png", data: png), picturesAsImages: true)
        draft.add(AttachedFile(name: "statement.csv", data: Data(body.utf8)), picturesAsImages: true)
        XCTAssertEqual(draft.images.count, 1)
        XCTAssertEqual(draft.files.map(\.name), ["statement.csv"])
    }

    /// Finder's shape on the Mac: the item is the file's URL.
    func testFinderFileBecomesAnAttachment() async throws {
        let url = try csv()
        let (v, got) = box()
        let p = NSItemProvider(item: url as NSURL, typeIdentifier: UTType.fileURL.identifier)
        XCTAssertTrue(AttachmentDraft.isFile(p))
        XCTAssertTrue(v.canPaste([p]))
        v.paste(itemProviders: [p])
        await settle { !got().isEmpty }
        XCTAssertEqual(got().map(\.name), ["statement_september.csv"])
        XCTAssertEqual(got().first?.data, Data(body.utf8))
        XCTAssertEqual(v.text, "", "the file's contents were typed into the draft")
    }

    /// The Files app's shape: the item is the file's contents, typed CSV —
    /// which is text, so a plain text view types it in.
    func testFilesAppFileBecomesAnAttachment() async throws {
        let url = try csv()
        let (v, got) = box()
        let p = try XCTUnwrap(NSItemProvider(contentsOf: url))
        XCTAssertTrue(AttachmentDraft.isFile(p), "\(p.registeredTypeIdentifiers) \(p.suggestedName ?? "-")")
        v.paste(itemProviders: [p])
        await settle { !got().isEmpty }
        XCTAssertEqual(got().map(\.name), ["statement_september.csv"])
        XCTAssertEqual(got().first?.data, Data(body.utf8))
        XCTAssertEqual(v.text, "", "the file's contents were typed into the draft")
    }

    /// The bug itself, on the view the composer used to be: with no file
    /// road the same drop lands in the text (the simulator types the file's
    /// address, the Mac its contents), and nothing is attached.
    func testWithoutTheFileRoadTheFileBecomesText() async throws {
        let url = try csv()
        let v = PasteTextView()
        v.paste(itemProviders: [try XCTUnwrap(NSItemProvider(contentsOf: url))])
        await settle { !v.text.isEmpty }
        XCTAssertFalse(v.text.isEmpty)
    }

    /// Words are still words.
    func testCopiedTextStillPastesAsText() async {
        let (v, got) = box()
        let p = NSItemProvider(object: "is this it??" as NSString)
        XCTAssertFalse(AttachmentDraft.isFile(p))
        v.paste(itemProviders: [p])
        await settle { !v.text.isEmpty }
        XCTAssertEqual(v.text, "is this it??")
        XCTAssertTrue(got().isEmpty)
    }

    /// A PDF is not text: the box has to say it takes one.
    func testThePDFDropIsAccepted() {
        let (v, _) = box()
        let p = NSItemProvider()
        p.suggestedName = "statement.pdf"
        p.registerDataRepresentation(forTypeIdentifier: UTType.pdf.identifier, visibility: .all) { done in
            done(Data("%PDF-1.4".utf8), nil); return nil
        }
        XCTAssertTrue(AttachmentDraft.isFile(p))
        XCTAssertTrue(v.canPaste([p]))
    }
}
