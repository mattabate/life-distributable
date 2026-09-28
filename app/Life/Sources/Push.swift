import SwiftUI
import UIKit
import UserNotifications

/// APNs (needs a paid Apple Developer team). The hub pushes whenever a
/// session raises an ask / proposes an action (scheduler NeedsYou → APNs);
/// a tap lands on the Sessions board. The device token is re-posted to the
/// hub on every launch (`POST /api/v1/devices`) since Apple may rotate it.
@MainActor @Observable final class PushState {
    /// Tab the app should show (set by a notification tap; RootView observes).
    var openSessions = false
    var status = "not registered"
    var token: String? = nil
    var hub: HubClient?
    let install = InstallState()

    /// Silent push from the hub (`content-available`) after an Install tap:
    /// iOS launches the new build in the background; re-posting the device
    /// token reports the build we now run, which closes the install card
    /// without the owner opening the app. Also refreshes the token otherwise.
    func wake() {
        if hub == nil { hub = HubClient() } // background launch: RootView's .task never ran
        UIApplication.shared.registerForRemoteNotifications() // → gotToken → registerDevice(build)
    }

    func register() {
        #if targetEnvironment(simulator)
        // ops/screens.sh: the permission alert would cover every screenshot.
        if ProcessInfo.processInfo.environment["LIFE_TAB"] != nil { status = "skipped (screenshot run)"; return }
        // `make app-test` hosts the tests in this app on the same simulator,
        // with no LIFE_TAB, so this asked there — and an authorization alert
        // nobody answers stays alive inside SpringBoard for the bundle and
        // comes back on every later launch, guard or no guard, reinstall or
        // not (only a simulator reboot clears it), covering every later
        // screenshot. Under XCTest there is nobody to answer; do not ask.
        if ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil { status = "skipped (test run)"; return }
        #endif
        Task {
            do {
                let ok = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
                status = ok ? "authorized" : "denied in Settings"
                guard ok else { return }
                UIApplication.shared.registerForRemoteNotifications()
            } catch { status = "authorization failed: \(error.localizedDescription)" }
        }
    }

    func gotToken(_ data: Data) {
        let hex = data.map { String(format: "%02x", $0) }.joined()
        token = hex
        guard let hub, hub.isConfigured else { status = "token (hub not configured)"; return }
        let env = Self.apsEnvironment()
        Task {
            do { try await hub.registerDevice(token: hex, env: env); status = "registered with hub (\(env))" }
            catch { status = "hub refused token: \(error.localizedDescription)" }
        }
    }

    /// aps-environment of this build, read from the embedded provisioning
    /// profile (CMS-wrapped plist; a plain substring search is enough):
    /// "development" = LAN/Xcode build → APNs sandbox host, "production" =
    /// ad-hoc OTA build → production host. "" when unreadable (simulator).
    static func apsEnvironment() -> String {
        guard let p = Bundle.main.path(forResource: "embedded", ofType: "mobileprovision"),
              let d = FileManager.default.contents(atPath: p),
              let s = String(data: d, encoding: .isoLatin1),
              let r = s.range(of: "<key>aps-environment</key>") else { return "" }
        let rest = s[r.upperBound...]
        guard let a = rest.range(of: "<string>"), let b = rest.range(of: "</string>") else { return "" }
        return String(rest[a.upperBound..<b.lowerBound])
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    static let push = PushState()

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { @MainActor in Self.push.gotToken(deviceToken) }
    }

    /// A content-available push. `kind: wake` is the silent one after an
    /// install → report our build to the hub. Any other is a card's alert
    /// that the hub marked for the phone to SPEAK (for headphones connected to
    /// the phone): a passive alert Siri never announces; woken in the
    /// background, the app says the whole line itself (Speaker.announce; the
    /// `audio` background mode keeps it alive while it talks). In the
    /// foreground willPresent already spoke it.
    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any], fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void) {
        Task { @MainActor in
            if userInfo["kind"] as? String == "wake" {
                Self.push.wake()
                try? await Task.sleep(for: .seconds(8)) // let the registration round-trip finish
                completionHandler(.newData)
                return
            }
            if application.applicationState != .active, let line = Self.spokenLine(userInfo) {
                Speaker.shared.announce(line, thread: userInfo["thread"] as? String ?? "", state: "background", sentAt: Self.sentAt(userInfo))
            }
            completionHandler(.noData)
        }
    }

    /// What the push says: the card's spoken line is the whole alert body; the
    /// label form is "<title>. <body>", as the Mac says it.
    nonisolated static func spokenLine(_ userInfo: [AnyHashable: Any]) -> String? {
        guard let aps = userInfo["aps"] as? [String: Any], let alert = aps["alert"] as? [String: Any],
              let body = alert["body"] as? String, !body.isEmpty else { return nil }
        if let title = alert["title"] as? String, !title.isEmpty { return title + ". " + body }
        return body
    }

    /// When the hub sent the push (`at`, unix seconds; 0 for an older hub).
    nonisolated static func sentAt(_ userInfo: [AnyHashable: Any]) -> TimeInterval {
        if let n = userInfo["at"] as? NSNumber { return n.doubleValue }
        return 0
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Task { @MainActor in Self.push.status = "APNs registration failed: \(error.localizedDescription)" }
    }

    /// Foreground: still show the banner + sound (the board is what they want to see).
    /// A read card (`kind: read`, pushed so headphones speak it) is already
    /// on the screen they are looking at: list only, no banner.
    /// Either way the app speaks the line when the hub asked it to (Siri never
    /// announces to an app in the foreground).
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        let info = notification.request.content.userInfo
        // A badge-only push (the icon's number moved) has nothing to show.
        if info["kind"] as? String == "badge" {
            completionHandler([.badge])
            return
        }
        let read = info["kind"] as? String == "read"
        if let aps = info["aps"] as? [String: Any], aps["content-available"] != nil, let line = Self.spokenLine(info) {
            let said = line
            let thread = info["thread"] as? String ?? ""
            let at = Self.sentAt(info)
            Task { @MainActor in Speaker.shared.announce(said, thread: thread, sentAt: at) }
        }
        completionHandler(read ? [.list] : [.banner, .sound, .list])
    }

    /// Tap → Sessions tab.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        Task { @MainActor in Self.push.openSessions = true }
        completionHandler()
    }
}

/// Self-restart after an OTA install: the app closes itself so nobody has to
/// manage restarting. iOS gives no API to relaunch an app, but quitting right
/// after the install prompt is answered guarantees the next icon tap (or the
/// hub's silent wake push) runs the new bundle instead of the old one
/// lingering in memory.
@MainActor @Observable final class InstallState {
    /// Build number from the tapped "Install app build N" card; nil = idle.
    var pending: Int?
    private var wentInactive = false

    func tapped(_ title: String) {
        let digits = title.drop { !$0.isNumber }.prefix { $0.isNumber }
        pending = Int(digits) ?? 0
        wentInactive = false
        // Fallback: if the prompt never surfaces (already installing), quit anyway.
        Task { try? await Task.sleep(for: .seconds(12)); quit() }
    }

    /// RootView feeds scene phases. The iOS "Install?" alert makes the app
    /// inactive; coming back active means the owner answered it → quit.
    func scene(_ phase: ScenePhase) {
        guard pending != nil else { return }
        switch phase {
        case .inactive, .background: wentInactive = true
        case .active where wentInactive: Task { try? await Task.sleep(for: .seconds(1.5)); quit() }
        default: break
        }
    }

    private func quit() {
        guard pending != nil else { return }
        pending = nil
        exit(0)
    }
}
