import SwiftUI

@main
struct LifeApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var client = HubClient()
    init() {
        #if targetEnvironment(macCatalyst)
        DeciderKeychain.probeIfAsked()
        #endif
        Perf.start()
    }
    var body: some Scene {
        WindowGroup {
            RootView().environment(client).environment(AppDelegate.push).dismissesKeyboardOnTap()
                .task {
                    // Re-register on every launch: the hub keeps the latest token.
                    AppDelegate.push.hub = client
                    AppDelegate.push.register()
                }
                #if targetEnvironment(macCatalyst)
                // The window's first size, then a screenshot run if one was
                // asked for (ops/mac-screens.sh).
                .task { MacWindow.size(); await MacShot.runIfAsked() }
                #endif
        }
    }
}
