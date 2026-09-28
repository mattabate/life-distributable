import SwiftUI
import UIKit

/// Tap anywhere outside a text input → keyboard goes away, like Messages.
/// SwiftUI's TapGesture on a ScrollView does not fire reliably (bubbles,
/// spacing and the scroll view itself swallow it), so this installs one
/// UIKit recognizer on the window. It never cancels the touch, so buttons,
/// links and scrolling underneath keep working; taps that land inside a
/// text field/view are ignored so the caret can still be moved.
struct KeyboardDismissOnTap: ViewModifier {
    /// Views that summon the keyboard on tap (the session composer, which is
    /// plain Text until tapped) set this so the same tap does not also
    /// resign the just-focused field.
    @MainActor static var suppressUntil: Date = .distantPast
    @MainActor static func suppress(for seconds: TimeInterval = 0.6) { suppressUntil = Date().addingTimeInterval(seconds) }

    func body(content: Content) -> some View {
        content.background(Installer())
    }

    private struct Installer: UIViewRepresentable {
        func makeUIView(context: Context) -> UIView {
            let v = UIView(frame: .zero); v.isUserInteractionEnabled = false
            DispatchQueue.main.async { context.coordinator.install(from: v) }
            return v
        }
        func updateUIView(_ uiView: UIView, context: Context) {
            if context.coordinator.recognizer == nil { context.coordinator.install(from: uiView) }
        }
        func makeCoordinator() -> Coordinator { Coordinator() }

        final class Coordinator: NSObject, UIGestureRecognizerDelegate {
            var recognizer: UITapGestureRecognizer?
            func install(from view: UIView) {
                guard recognizer == nil, let window = view.window else { return }
                let tap = UITapGestureRecognizer(target: self, action: #selector(dismiss))
                tap.cancelsTouchesInView = false
                tap.delegate = self
                window.addGestureRecognizer(tap)
                recognizer = tap
            }
            @objc func dismiss() {
                if Date() < KeyboardDismissOnTap.suppressUntil { return }
                UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            }
            func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
            func gestureRecognizer(_ g: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
                var v = touch.view
                while let cur = v {
                    if cur is UITextInput || cur is UITextField || cur is UITextView { return false }
                    // The keyboard and its accessory live in another window; ignore.
                    if String(describing: type(of: cur)).contains("Keyboard") { return false }
                    v = cur.superview
                }
                return true
            }
        }
    }
}

extension View {
    func dismissesKeyboardOnTap() -> some View { modifier(KeyboardDismissOnTap()) }
}
