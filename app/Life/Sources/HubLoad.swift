import SwiftUI

// One loader for a screen that reads the hub. Every GET is kept
// on the phone (HubClient `request`), so a screen that has been opened before
// paints what it last showed at once, then the request revalidates it (a 304
// with no body when nothing changed) and swaps in anything new. Errors land in
// one place; a cancelled request is never one. It replaces the do/catch +
// `loaded` flag + `.task` + `.refreshable` that each screen used to repeat.

/// Set around a load: `HubClient.send` hands it the kept copy of any GET it
/// is about to make, before the network answers.
enum HubPaint {
    @TaskLocal static var cached: (@MainActor @Sendable (any Sendable) async -> Void)?
}

@MainActor @Observable
final class HubLoad<T: Sendable> {
    var value: T?
    var error: String?
    /// At least one load finished (or failed) — "empty" is now a real answer.
    var loaded = false

    init(_ value: T? = nil) { self.value = value }

    /// Run `fetch`, painting from the phone's copy first when there is no
    /// value yet. A fetch that makes several GETs paints only from the one
    /// whose type is `T`.
    func run(_ fetch: @MainActor () async throws -> T) async {
        let paint: @MainActor @Sendable (any Sendable) async -> Void = { [weak self] v in
            guard let self, self.value == nil, let v = v as? T else { return }
            self.value = v
        }
        await HubPaint.$cached.withValue(paint) {
            do {
                let v = try await fetch()
                value = v
                error = nil
            } catch {
                if !error.isCancellation { self.error = error.localizedDescription }
            }
        }
        loaded = true
    }
}

extension View {
    /// `.task` + `.refreshable` through one `HubLoad`. `id` restarts it, like
    /// `.task(id:)`.
    func hubTask<T: Sendable, ID: Equatable>(_ load: HubLoad<T>, id: ID, _ fetch: @escaping @MainActor () async throws -> T) -> some View {
        self.task(id: id) { await load.run(fetch) }
            .refreshable { await load.run(fetch) }
    }
    func hubTask<T: Sendable>(_ load: HubLoad<T>, _ fetch: @escaping @MainActor () async throws -> T) -> some View {
        hubTask(load, id: 0, fetch)
    }
}

/// The error banner / first-load spinner every hub screen shows above its
/// content. Draws nothing once there is a value and no error.
struct HubLoadStatus<T: Sendable>: View {
    let load: HubLoad<T>
    var body: some View {
        if let e = load.error { Section { ErrorBanner(message: e) } }
        else if load.value == nil { Section { ProgressView() } }
    }
}
