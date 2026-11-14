// A long numbered list must draw to its last line (a card opened to its full
// height, its words stopped at "10." and the rest of the box was blank). The
// text view is the one SelectableText makes: not scrolling, sized to its
// text, drawn into a picture and read for ink.
import XCTest
import SwiftUI
@testable import Life

@MainActor
final class StyledTextTests: XCTestCase {
    /// How far down the view the last drawn pixel sits, 0...1.
    private func inkDepth(_ text: String, width: CGFloat = 330) -> CGFloat {
        let v = UITextView()
        v.isEditable = false
        v.isScrollEnabled = false
        v.backgroundColor = .clear
        v.textContainerInset = .zero
        v.textContainer.lineFragmentPadding = 0
        v.attributedText = StyledText(text: text, color: .black).build(text)
        let h = ceil(v.sizeThatFits(CGSize(width: width, height: 100_000)).height)
        guard let scene = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).first else { return 0 }
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: width, height: h)
        v.frame = window.bounds
        window.addSubview(v)
        window.isHidden = false
        v.layoutIfNeeded()
        let f = UIGraphicsImageRendererFormat(); f.scale = 1; f.opaque = false
        let img = UIGraphicsImageRenderer(size: v.bounds.size, format: f).image { _ in
            v.drawHierarchy(in: v.bounds, afterScreenUpdates: true)
        }
        guard let cg = img.cgImage, let data = cg.dataProvider?.data, let p = CFDataGetBytePtr(data) else { return 0 }
        let alpha = cg.alphaInfo == .premultipliedFirst || cg.alphaInfo == .first ? 0 : 3
        var last = 0
        for y in 0..<cg.height {
            for x in 0..<cg.width where p[y * cg.bytesPerRow + x * 4 + alpha] > 40 { last = y; break }
        }
        return CGFloat(last) / CGFloat(max(cg.height, 1))
    }

    func testAListPastNineItemsDrawsToItsEnd() {
        let items = (1...13).map { "\($0). Fixed: a card is read out once, and the words after it stay." }
        let text = "**First**\n" + items.joined(separator: "\n") + "\n\n**Smaller**\nThe last line of the card."
        XCTAssertGreaterThan(inkDepth(text), 0.9)
    }

    func testAShortListDrawsToItsEnd() {
        let text = (1...9).map { "\($0). One line." }.joined(separator: "\n") + "\n\nThe last line."
        XCTAssertGreaterThan(inkDepth(text), 0.9)
    }
}
