import SwiftUI
import UIKit

/// The editable draft box, backed by a UITextView.
///
/// SwiftUI's `TextField(axis: .vertical)` re-measures its entire string on
/// every change, so past a few hundred characters each keystroke costs more
/// than the one before it, and a long draft crawls. A UITextView owns its
/// text and lays it out incrementally through TextKit, and once the box has
/// reached its line ceiling we stop measuring at all (the measurement is the
/// O(length) work we are here to avoid), so the cost of a keystroke stops
/// growing with the length of the draft.
struct ComposerField: UIViewRepresentable {
    @Binding var text: String
    let placeholder: String
    /// Lines the box grows to before it stops growing and scrolls internally.
    var maxLines: Int = 6
    /// Two-way: set true to open the keyboard; flipped back to false when the
    /// field resigns (tap outside, dictation starting, keyboard dismissed).
    @Binding var focused: Bool
    /// A picture on the clipboard, pasted INTO the box — long-press → Paste, or
    /// ⌘V on a keyboard. A plain UITextView refuses an image outright, so
    /// PasteTextView below hands it here and it becomes an attachment.
    var onPasteImages: (([UIImage]) -> Void)? = nil

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> UITextView {
        let v = PasteTextView()
        v.onPasteImages = { [weak coordinator = context.coordinator] imgs in
            coordinator?.parent.onPasteImages?(imgs)
        }
        v.delegate = context.coordinator
        v.font = UIFont.preferredFont(forTextStyle: .body)
        v.adjustsFontForContentSizeCategory = true
        v.backgroundColor = .clear
        // Nothing may draw outside the rounded box behind it.
        v.clipsToBounds = true
        v.textContainerInset = .zero
        v.textContainer.lineFragmentPadding = 0
        v.isScrollEnabled = false
        v.text = text
        // The window-wide dismiss-on-tap recognizer (KeyboardDismiss.swift)
        // already treats a UITextView as "not a dismiss target", so tapping
        // inside the box keeps the keyboard up.
        let label = context.coordinator.placeholderLabel
        label.font = v.font
        // Without this the placeholder keeps the size it was born with while
        // the typed text scales (Dynamic Type / accessibility sizes).
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .placeholderText
        label.numberOfLines = 1
        label.lineBreakMode = .byTruncatingTail
        // Auto Layout would otherwise keep the label at its full intrinsic
        // width and let the box overflow rather than shorten the text.
        label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        label.text = placeholder
        label.isHidden = !text.isEmpty
        label.translatesAutoresizingMaskIntoConstraints = false
        v.addSubview(label)
        // frameLayoutGuide, NOT the text view's own anchors: a UITextView is a
        // UIScrollView, so `v.trailingAnchor` is the edge of its *content*, a
        // width the engine is free to grow. Pinned that way the placeholder
        // kept its intrinsic width and ran out of the box. The frame guide is
        // the visible box.
        let frame = v.frameLayoutGuide
        NSLayoutConstraint.activate([
            label.topAnchor.constraint(equalTo: frame.topAnchor),
            label.leadingAnchor.constraint(equalTo: frame.leadingAnchor),
            label.trailingAnchor.constraint(lessThanOrEqualTo: frame.trailingAnchor),
        ])
        return v
    }

    func updateUIView(_ v: UITextView, context: Context) {
        context.coordinator.parent = self
        // Only on a programmatic change (an ask prefix, clearing after send):
        // assigning identical text would reset the cursor and re-lay out.
        if v.text != text { v.text = text }
        let label = context.coordinator.placeholderLabel
        if label.text != placeholder { label.text = placeholder }
        if label.font != v.font { label.font = v.font }
        label.isHidden = !v.text.isEmpty
        if focused, !v.isFirstResponder { v.becomeFirstResponder() }
        if !focused, v.isFirstResponder { v.resignFirstResponder() }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView v: UITextView, context: Context) -> CGSize? {
        let font = v.font ?? UIFont.preferredFont(forTextStyle: .body)
        let line = ceil(font.lineHeight)
        let maxH = line * CGFloat(maxLines)
        // SwiftUI sizes an HStack by probing each child at width 0 (how small
        // can you get?) and width ∞ (how big do you want to be?). Answering
        // both with a concrete width made the box inflexible: the stack could
        // not shrink the buttons around it, so it shrank the box instead and
        // the draft wrapped every two words. Pass
        // the proposal straight back whenever it is a real width — 0 included —
        // and the box takes whatever the row has left.
        let proposed = proposal.width
        let layoutWidth = proposed.flatMap { $0.isFinite && $0 > 0 ? $0 : nil } ?? v.window?.bounds.width ?? 300
        // 60 characters is wider than any line this box renders on a phone, so
        // past maxLines × 60 the text certainly overflows: take the ceiling
        // without measuring. This is what keeps a long draft cheap.
        let height: CGFloat
        if v.text.count > maxLines * 60 {
            height = maxH
        } else {
            let fit = v.sizeThatFits(CGSize(width: layoutWidth, height: .greatestFiniteMagnitude)).height
            height = min(max(fit, line), maxH)
        }
        let scrolls = height >= maxH
        if v.isScrollEnabled != scrolls { v.isScrollEnabled = scrolls }
        let width = proposed.flatMap { $0.isFinite ? $0 : nil } ?? layoutWidth
        return CGSize(width: width, height: ceil(height))
    }

    @MainActor final class Coordinator: NSObject, UITextViewDelegate {
        var parent: ComposerField
        let placeholderLabel = UILabel()
        init(_ parent: ComposerField) { self.parent = parent }

        func textViewDidChange(_ v: UITextView) {
            placeholderLabel.isHidden = !v.text.isEmpty
            if parent.text != v.text { parent.text = v.text }
            if v.isScrollEnabled { v.scrollRangeToVisible(v.selectedRange) }
        }

        func textViewDidEndEditing(_ v: UITextView) {
            if parent.focused { parent.focused = false }
        }
    }
}

/// A UITextView that accepts a pasted picture. UIKit offers Paste only for
/// what the responder says it can take, and a text view says text — so a
/// screenshot on the clipboard leaves the menu item greyed out. Here Paste is
/// offered whenever the clipboard holds an image, and the image goes to the
/// composer as an attachment instead of into the string (`hasImages` is a
/// type check, not a read: iOS shows its "Allow Paste?" prompt only when
/// `images` is actually read, which is after the user chose Paste).
final class PasteTextView: UITextView {
    var onPasteImages: (([UIImage]) -> Void)?

    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        if action == #selector(paste(_:)), UIPasteboard.general.hasImages { return true }
        return super.canPerformAction(action, withSender: sender)
    }

    override func paste(_ sender: Any?) {
        let pb = UIPasteboard.general
        if pb.hasImages, let images = pb.images, !images.isEmpty {
            onPasteImages?(images)
            // Text on the clipboard alongside the picture still goes in the box.
            if pb.hasStrings { super.paste(sender) }
            return
        }
        super.paste(sender)
    }
}
