import SwiftUI

@main
struct LifeApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var client = HubClient()
    init() { Perf.start() }
    var body: some Scene {
        WindowGroup {
            RootView().environment(client).environment(AppDelegate.push).dismissesKeyboardOnTap()
                .task {
                    // Re-register on every launch: the hub keeps the latest token.
                    AppDelegate.push.hub = client
                    AppDelegate.push.register()
                }
        }
    }
}
