#if targetEnvironment(macCatalyst)
import UIKit
import ObjectiveC

/// Keeps the desktop app alive through a UIKit/SwiftUI hover re-entrancy bug
/// (macOS 26.0, build 26A425; a trap in SwiftUI on a mouse move).
///
/// On a mouse move UIKit re-hit-tests the pointer (`-[UIHoverEvent
/// _setNeedsUpdateForWindow:forcingHitTest:]` → `-[UITouch
/// _rehitTestWithEvent:]` → `performHitTest`). `performHitTest` keeps its
/// `_UIHitTestContext` in ONE global slot, runs the window's hit test, then
/// sends `_hitTestWithContext:` to the view it found with whatever is in the
/// slot. When the window's hit test is itself re-entered (the view tree is
/// changing under the pointer while a page refreshes), the inner call clears
/// the slot on its way out and the outer one passes nil. UIKit's own views
/// tolerate nil; SwiftUI's `_UIHostingView` is Swift and traps on the implicit
/// unwrap of the context. Reproduced in lldb:
/// `[rootViewController.view _hitTestWithContext:nil]` → EXC_BREAKPOINT.
///
/// Fix: for every class a view controller's view belongs to that overrides
/// `_hitTestWithContext:` (each `_UIHostingView<Content>` specialization is
/// its own ObjC class, so sheets and popovers bring new ones), wrap the
/// override so a nil context answers nil. `performHitTest` keeps the view it
/// already hit when the answer is nil, so the hover lands where UIKit's own
/// hit test put it and nothing is lost.
@MainActor
enum MacHitTestGuard {
    private typealias HitTestIMP = @convention(c) (UIView, Selector, AnyObject?) -> UIView?
    private typealias HitTestBlock = @convention(block) (UIView, AnyObject?) -> UIView?
    private typealias DidLoadIMP = @convention(c) (UIViewController, Selector) -> Void
    private typealias DidLoadBlock = @convention(block) (UIViewController) -> Void

    private static let hitTestSel = NSSelectorFromString("_hitTestWithContext:")
    private static var patched = Set<ObjectIdentifier>()
    private static var installed = false

    /// Call once, before any view loads (LifeApp.init).
    static func install() {
        guard !installed else { return }
        installed = true
        // UIViewController itself defines viewDidLoad, and UIHostingController
        // calls super (seen under lldb), so swapping the base IMP reaches every
        // hosting controller, including the presentation ones SwiftUI makes.
        let sel = #selector(UIViewController.viewDidLoad)
        guard let method = class_getInstanceMethod(UIViewController.self, sel) else { return }
        let original = unsafeBitCast(method_getImplementation(method), to: DidLoadIMP.self)
        let block: DidLoadBlock = { vc in
            original(vc, sel)
            // viewDidLoad is a main-thread call by UIKit's contract.
            MainActor.assumeIsolated {
                if vc.isViewLoaded { patch(viewClass: object_getClass(vc.view)) }
            }
        }
        method_setImplementation(method, imp_implementationWithBlock(block))
    }

    /// Wraps `_hitTestWithContext:` on `cls` and each superclass short of
    /// UIView that implements its own.
    private static func patch(viewClass: AnyClass?) {
        var cls: AnyClass? = viewClass
        while let c = cls, c != UIView.self {
            defer { cls = class_getSuperclass(c) }
            let id = ObjectIdentifier(c)
            if patched.contains(id) { continue }
            patched.insert(id)
            guard let method = class_getInstanceMethod(c, hitTestSel),
                  let superclass = class_getSuperclass(c),
                  let superMethod = class_getInstanceMethod(superclass, hitTestSel),
                  method_getImplementation(method) != method_getImplementation(superMethod)
            else { continue }
            let original = unsafeBitCast(method_getImplementation(method), to: HitTestIMP.self)
            let sel = hitTestSel
            let block: HitTestBlock = { view, context in
                guard let context else { return nil }
                return original(view, sel, context)
            }
            class_replaceMethod(c, sel, imp_implementationWithBlock(block), method_getTypeEncoding(method))
        }
    }
}
#endif
