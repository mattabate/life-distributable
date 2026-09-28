import Foundation
import UIKit
import Observation
import Security
import LocalAuthentication
import CoreGraphics

/// Thin client for the hub API. Token lives in the Keychain; base URL in
/// UserDefaults (only ever a tailnet host).
@Observable @MainActor
final class HubClient {
    var baseURL: String {
        didSet { UserDefaults.standard.set(baseURL, forKey: "hub.baseURL") }
    }
    var token: String {
        didSet {
            let t = token.trimmingCharacters(in: .whitespacesAndNewlines)
            if t != token { token = t; return }
            Keychain.set(token, for: "hub.token")
        }
    }
    /// The decider code: the second credential Approve/Deny needs, on top of
    /// the hub token. The token is on the hub host's disk where every session
    /// can read it, so on its own it must not be able to approve
    /// money/delete/commit actions. This code exists only here (Keychain) and
    /// in the owner's browser; the hub keeps a SHA-256 of it.
    /// `ops/decider-set.sh` prints it once. Empty until the owner types it in —
    /// the hub only demands it once they have armed one.
    /// The box the code is typed INTO — not where it is kept. The stored copy
    /// lives in `DeciderKeychain`, behind Face ID, and this
    /// buffer is cleared the moment it is saved. Nothing sends it on its own.
    var decider: String {
        didSet {
            let d = Self.normalizeDecider(decider)
            if d != decider { decider = d }
        }
    }
    /// Is a code saved on this phone? Cheap, no Face ID prompt (see `exists`).
    var deciderSaved: Bool = DeciderKeychain.exists()

    /// The code is XXXXX-XXXXX-XXXXX-XXXXX in Crockford-ish base32, and it is
    /// typed by hand into a field that shows dots — so a wrong character is
    /// INVISIBLE, and the phone then re-sends it silently on every approve
    /// forever, which looks like the Approve button being broken.
    ///
    /// iOS's smart punctuation turns a typed "-" into an en/em dash, and the
    /// keyboard likes to insert a non-breaking hyphen, none of which anyone
    /// can see. Fold them all back to a plain hyphen and drop stray spaces before
    /// anything is stored. `Check code` in Settings covers the rest.
    nonisolated static func normalizeDecider(_ raw: String) -> String {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        for dash in ["\u{2010}", "\u{2011}", "\u{2012}", "\u{2013}", "\u{2014}", "\u{2212}"] {
            s = s.replacingOccurrences(of: dash, with: "-")
        }
        return s.replacingOccurrences(of: " ", with: "")
    }
    var isConfigured: Bool { !baseURL.isEmpty && !token.isEmpty }

    init() {
        baseURL = UserDefaults.standard.string(forKey: "hub.baseURL") ?? ""
        token = Keychain.get("hub.token") ?? ""
        // The code is NOT loaded here any more: launching the app must not ask
        // for a face, and a copy sitting in memory for the whole session is the
        // thing that used to be sent wrong on every request.
        decider = ""
        #if targetEnvironment(simulator)
        // ops/screens.sh launches the simulator build with the hub token in the
        // environment so the quality agent can screenshot real screens.
        if let t = ProcessInfo.processInfo.environment["LIFE_HUB_TOKEN"], !t.isEmpty { token = t }
        #endif
    }

    /// A fresh decoder per response: these are used from `send`, which runs off
    /// the main actor, so several can be decoding at once and a shared one is
    /// not documented to be safe for that. Dates are read by `rfc3339`, not by
    /// an ISO8601DateFormatter — formatters are stateful (a real race here) and
    /// cost roughly ten times as much per row.
    /// Internal, not private: LifeTests decodes api.md fixtures with it.
    nonisolated static func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            guard let date = rfc3339(s) else {
                throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "bad date \(s)"))
            }
            return date
        }
        return d
    }

    /// ONE session for every request. Away from
    /// home the hub is reached over a relayed Tailscale link where a fresh
    /// TLS handshake costs most of a second; `URLSession.shared` reused
    /// connections too, but its 15 s per-request timeout and its own cache
    /// policy fought the change feed (a request that sits for 25 s on
    /// purpose) and the ETag handshake below (ours, on disk, not Foundation's
    /// in-memory one). `waitsForConnectivity` makes a request placed while the
    /// radio is asleep wait for the link instead of failing at once.
    nonisolated static let session: URLSession = {
        let c = URLSessionConfiguration.default
        c.waitsForConnectivity = true
        c.timeoutIntervalForRequest = 30
        c.timeoutIntervalForResource = 120
        c.urlCache = nil
        c.requestCachePolicy = .reloadIgnoringLocalCacheData
        c.httpMaximumConnectionsPerHost = 4
        return URLSession(configuration: c)
    }()

    /// What the hub last said, by request — the phone's local store.
    nonisolated static let cache = ResponseCache()

    /// The cache key of a GET: the hub's URL is part of it, so pointing the
    /// app at another hub never shows the old one's data.
    func cacheKey(_ path: String) -> String { baseURL + path }

    /// The last body this path returned, decoded — nil when the phone has
    /// never seen it. Screens paint from this BEFORE the first request; call
    /// it off the main actor for a big body (a chat's events).
    nonisolated static func cachedValue<T: Decodable & Sendable>(key: String) -> T? {
        guard let e = cache.entry(key) else { return nil }
        if let v = e.value as? T { return v }
        guard let v = try? decoder().decode(T.self, from: e.data) else { return nil }
        cache.setValue(key, v)
        return v
    }

    /// Write a value the phone assembled itself (a chat's events, seeded from
    /// disk plus every `since` delta) back under the path it would have come
    /// from, so the next open seeds from it.
    nonisolated static func remember<T: Encodable & Sendable>(key: String, _ value: T) {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .custom { date, e in
            var c = e.singleValueContainer(); try c.encode(Fmt.isoString(date))
        }
        guard let data = try? enc.encode(value) else { return }
        cache.store(key, etag: "", data: data, value: value)
    }

    /// Every GET is cached by default: the reply is kept (memory +
    /// disk), the next request carries its ETag so an unchanged answer is a 304
    /// with no body and no decode, and a `HubLoad` paints the kept copy before
    /// the request leaves. `cache: false` only for reads whose URL is unique
    /// per call (the change feed, event deltas) — they would only churn disk.
    private func request<T: Decodable & Sendable>(_ method: String, _ path: String, body: Encodable? = nil, cache: Bool = true, timeout: TimeInterval = 30, decider: String? = nil) async throws -> T {
        guard let url = URL(string: baseURL + path) else { throw APIError(error: "bad base URL") }
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        // Only the three calls that need it pass a code, and each one has just
        // unlocked it with Face ID. It used to ride on EVERY request from a
        // copy held in memory since launch — which is how a wrong one could
        // go unnoticed for a day.
        if let decider, !decider.isEmpty { req.setValue(decider, forHTTPHeaderField: "X-Life-Decider") }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONEncoder().encode(AnyEncodable(body))
        }
        return try await Self.send(req, cacheKey: cache && method == "GET" ? cacheKey(path) : nil)
    }

    /// `nonisolated` on purpose, and it is the whole point of this file's shape:
    /// HubClient is @MainActor, so parsing here would put every response on
    /// the main thread. Opening a long session decodes hundreds of KB of
    /// event JSON, and the 3s poll does it again and again — on the main
    /// actor the app freezes mid-push.
    /// Nothing here touches actor state: the request is built on the main
    /// actor and handed over whole.
    nonisolated private static func send<T: Decodable & Sendable>(_ req: URLRequest, cacheKey: String? = nil) async throws -> T {
        var req = req
        let held = cacheKey.flatMap { cache.entry($0) }
        if let held, !held.etag.isEmpty { req.setValue(held.etag, forHTTPHeaderField: "If-None-Match") }
        // Stale-while-revalidate: a screen loading through HubLoad draws the
        // kept copy now, decoded here off the main actor, while the request runs.
        if let paint = HubPaint.cached, let cacheKey, held != nil, let v: T = cachedValue(key: cacheKey) {
            await paint(v)
        }
        let (data, resp) = try await dataRetryingRestart(req)
        let http = resp as? HTTPURLResponse
        let code = http?.statusCode ?? 0
        if code == 304, let held, let cacheKey {
            if let v = held.value as? T { return v }
            let v = try decoder().decode(T.self, from: held.data)
            cache.setValue(cacheKey, v)
            return v
        }
        if code == 204, let empty = Empty() as? T { return empty }
        guard (200..<300).contains(code) else {
            if var e = try? decoder().decode(APIError.self, from: data) { e.status = code; throw e }
            throw APIError(error: "HTTP \(code)", status: code)
        }
        let v = try decoder().decode(T.self, from: data)
        if let cacheKey { cache.store(cacheKey, etag: http?.value(forHTTPHeaderField: "ETag") ?? "", data: data, value: v) }
        return v
    }

    /// A hub restart refuses connections for a second or two; retry through it
    /// (0.25 … 4 s, ~8 s in all) instead of showing an error at a Send.
    /// Refused/dropped before an answer only — a graceful drain
    /// finishes in-flight requests, so a handled POST is never sent twice.
    nonisolated private static func dataRetryingRestart(_ req: URLRequest) async throws -> (Data, URLResponse) {
        var attempt = 0
        while true {
            do { return try await session.data(for: req) } catch let e as URLError
                where attempt < 5 && [.cannotConnectToHost, .networkConnectionLost].contains(e.code) {
                try await Task.sleep(nanoseconds: UInt64(250_000_000) << UInt64(attempt))
                attempt += 1
            }
        }
    }

    /// Same deal for multipart: the photo bytes leave, and the reply is parsed,
    /// off the main actor.
    nonisolated private static func upload<T: Decodable & Sendable>(_ req: URLRequest, _ body: Data) async throws -> T {
        let (data, resp) = try await session.upload(for: req, from: body)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(code) else {
            if let e = try? decoder().decode(APIError.self, from: data) { throw e }
            throw APIError(error: "HTTP \(code)")
        }
        return try decoder().decode(T.self, from: data)
    }

    /// Blobs are bytes, so there is nothing to decode — but the fetch itself
    /// still leaves the main actor.
    nonisolated private static func fetch(_ req: URLRequest) async throws -> Data {
        let (data, resp) = try await session.data(for: req)
        guard (200..<300).contains((resp as? HTTPURLResponse)?.statusCode ?? 0) else { throw APIError(error: "blob fetch failed") }
        return data
    }

    /// The window scopes only the unpriced-models line; the day chart is
    /// all-time (`history[]`) whatever is asked for.
    func spend() async throws -> SpendSummary { try await request("GET", "/api/v1/spend/summary?days=30") }
    func quota() async throws -> Quota { try await request("GET", "/api/v1/spend/quota") }
    /// The Spend page's model toggle: what a NEW session is pinned to. Setting
    /// it never touches a session that already exists.
    func modelSetting() async throws -> ModelSetting { try await request("GET", "/api/v1/spend/model") }
    func setModel(_ id: String) async throws -> ModelSetting { try await request("PUT", "/api/v1/spend/model", body: ["default_model": id]) }
    func usage() async throws -> UsageState { try await request("GET", "/api/v1/usage", cache: false) }
    func setUsage(on: Bool) async throws -> UsageState { try await request("PUT", "/api/v1/usage", body: ["on": on]) }
    func projects() async throws -> [Project] { try await request("GET", "/api/v1/projects") }
    func sessions() async throws -> [TmuxSession] { try await request("GET", "/api/v1/sessions") }
    func startSession(project: String) async throws -> TmuxSession { try await request("POST", "/api/v1/sessions", body: ["project": project]) }
    /// A replayed card's line is being heard for `secs` (0 = it stopped): the
    /// session reads "speaking" and no push talks over it.
    func voice(thread: String, secs: Double) async throws -> VoiceFloor {
        try await request("POST", "/api/v1/voice", body: VoiceIn(thread_id: thread, secs: secs), cache: false)
    }
    func killSession(name: String) async throws { let _: Empty = try await request("DELETE", "/api/v1/sessions/\(name)") }
    /// `thread` narrows to one session's proposals on the hub; an older hub
    /// ignores it, so callers still filter by `thread_id` themselves.
    func actions(state: String = "", thread: String = "") async throws -> [Action] {
        try await request("GET", "/api/v1/actions?state=\(state)&limit=100" + (thread.isEmpty ? "" : "&thread=\(thread)"), cache: true)
    }
    /// One action with its audit trail (`events`, oldest first) — the only
    /// read that carries it.
    func action(_ id: String) async throws -> Action { try await request("GET", "/api/v1/actions/\(id)") }
    func run(_ id: String) async throws -> JobRun { try await request("GET", "/api/v1/runs/\(id)") }
    /// `note` is relayed to the session that proposed the action as the owner's next message.
    func approve(_ id: String, note: String = "") async throws -> Action {
        try await request("POST", "/api/v1/actions/\(id)/approve?via=app", body: ["message": note],
                          decider: try await unlockDecider("Approve this action"))
    }
    func deny(_ id: String, note: String = "") async throws -> Action {
        try await request("POST", "/api/v1/actions/\(id)/deny?via=app", body: ["message": note],
                          decider: try await unlockDecider("Deny this action"))
    }
    /// Dismiss: the owner is not going to answer this one (it went out of
    /// date). The row folds where it was; nothing is relayed, no decider code, and
    /// Reopen undoes it.
    func dismissAction(_ id: String) async throws -> Action { try await request("POST", "/api/v1/actions/\(id)/dismiss?via=app") }
    func reopenAction(_ id: String) async throws -> Action { try await request("POST", "/api/v1/actions/\(id)/reopen?via=app") }

    /// Face ID, then the code — the whole of what the owner has to do to
    /// approve something. Nothing is cached: the next approval asks again.
    ///
    /// Reading a protected Keychain item BLOCKS while the prompt is up, so it
    /// runs off the main actor; doing it inline would freeze the card the
    /// button is on. An unarmed hub needs no code, so a phone that has none
    /// saved sends none and lets the hub answer — that is the pre-armed
    /// behaviour, and it is also the honest error if none was ever saved.
    func unlockDecider(_ reason: String) async throws -> String? {
        // Face ID is the test, not exists(): a probe that says "nothing
        // saved" when a code IS saved must never stop an approve from trying.
        // Only a read the Keychain answers with not-found means no code.
        let (code, found) = await Task.detached(priority: .userInitiated) {
            DeciderKeychain.read(reason: reason)
        }.value
        deciderSaved = found
        guard found else { return nil }
        guard let code else {
            throw APIError(error: "Face ID was cancelled, so the decider code stayed locked. Nothing was sent.", status: 0)
        }
        return code
    }

    /// Is the code on this phone the one the hub will accept? Asked from
    /// Settings — with Face ID, like a real approval — so a wrong code is
    /// caught there and not at the moment of a real approval.
    func checkDecider(unlock: Bool) async throws -> DeciderStatus {
        try await request("GET", "/api/v1/decider",
                          decider: unlock ? try await unlockDecider("Check your decider code") : nil)
    }
    func status() async throws -> HubStatus { try await request("GET", "/api/v1/status") }
    func goals() async throws -> [Goal] { try await request("GET", Self.goalsPath, cache: true) }
    func goalNotes(_ id: String) async throws -> [GoalNote] { try await request("GET", "/api/v1/goals/\(id)/notes?limit=30") }
    /// The list omits `digest`; the detail carries it, so an open goal re-reads itself.
    func goal(_ id: String) async throws -> Goal { try await request("GET", "/api/v1/goals/\(id)") }
    // Name, statement, status — the three things the owner edits by hand; goals are
    // created and noted by sessions (lifectl), so the app has no create/note call.
    func patchGoal(_ id: String, _ patch: [String: String]) async throws -> Goal { try await request("PATCH", "/api/v1/goals/\(id)", body: patch) }
    /// Upload an observation, optionally with a file. Multipart to match shared/api.md.
    /// Returns the stored observation (its `blob_ref` is what thread attachments use).
    @discardableResult
    func upload(source: String, kind: String, ts: Date, payload: [String: Any], file: Data?, filename: String?) async throws -> Obs {
        guard let url = URL(string: baseURL + "/api/v1/observations") else { throw APIError(error: "bad base URL") }
        let boundary = "life-" + UUID().uuidString
        var body = Data()
        func field(_ name: String, _ value: String) {
            body.append("--\(boundary)\r\nContent-Disposition: form-data; name=\"\(name)\"\r\n\r\n\(value)\r\n".data(using: .utf8)!)
        }
        field("source", source); field("kind", kind); field("ts", Fmt.isoString(ts))
        field("tz", TimeZone.current.identifier)
        if let pl = try? JSONSerialization.data(withJSONObject: payload), let s = String(data: pl, encoding: .utf8) { field("payload", s) }
        if let file, let filename {
            body.append("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"\(filename)\"\r\nContent-Type: application/octet-stream\r\n\r\n".data(using: .utf8)!)
            body.append(file); body.append("\r\n".data(using: .utf8)!)
        }
        body.append("--\(boundary)--\r\n".data(using: .utf8)!)
        var req = URLRequest(url: url, timeoutInterval: 60)
        req.httpMethod = "POST"
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        return try await Self.upload(req, body)
    }

    /// Upload a photo destined for a session as an `app/photo` observation; returns its blob ref.
    func uploadSessionPhoto(_ jpeg: Data, threadID: String?, size: CGSize) async throws -> String {
        var payload: [String: Any] = ["via": "session", "width": Int(size.width), "height": Int(size.height)]
        if let threadID { payload["thread_id"] = threadID }
        let o = try await upload(source: "app", kind: "photo", ts: Date(), payload: payload, file: jpeg, filename: "photo.jpg")
        guard let ref = o.blob_ref else { throw APIError(error: "hub stored no blob") }
        return ref
    }
    /// Upload a document destined for a session as an `app/upload` observation
    /// (the console's own kind for a non-picture, composer.js uploadFiles);
    /// the bytes and the name go up as they are, so the blob keeps its
    /// extension and the session Reads it as a PDF. Returns its blob ref.
    func uploadSessionFile(_ data: Data, name: String, threadID: String?) async throws -> String {
        // The name travels in a multipart header: keep it to one safe line.
        let safe = String(name.map { $0.isLetter || $0.isNumber || "._- ".contains($0) ? $0 : "_" }).trimmingCharacters(in: .whitespaces)
        var payload: [String: Any] = ["via": "session", "filename": safe]
        if let threadID { payload["thread_id"] = threadID }
        let o = try await upload(source: "app", kind: "upload", ts: Date(), payload: payload, file: data, filename: safe.isEmpty ? "file" : safe)
        guard let ref = o.blob_ref else { throw APIError(error: "hub stored no blob") }
        return ref
    }
    /// Fetch a stored blob (authenticated; AsyncImage can't send the bearer token).
    func blob(_ ref: String) async throws -> Data {
        guard let url = URL(string: baseURL + "/api/v1/blobs/" + ref) else { throw APIError(error: "bad base URL") }
        var req = URLRequest(url: url, timeoutInterval: 30)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return try await Self.fetch(req)
    }
    func sources() async throws -> SourcesInventory { try await request("GET", "/api/v1/sources") }
    func threads(archived: Bool = false) async throws -> [Thread] { try await request("GET", archived ? Self.threadsPath + "?archived=1" : Self.threadsPath) }
    func thread(_ id: String) async throws -> Thread { try await request("GET", "/api/v1/threads/\(id)", cache: true) }
    func threadMessages(_ id: String) async throws -> [ThreadMessage] { try await request("GET", Self.messagesPath(id), cache: true) }
    /// The full window (since 0) is what the phone keeps; a delta is appended
    /// to it by ThreadDetail, which writes the result back with `remember`.
    func threadEvents(_ id: String, since: Int = 0) async throws -> [ThreadEvent] { try await request("GET", since == 0 ? Self.eventsPath(id) : "/api/v1/threads/\(id)/events?since=\(since)&limit=1000", cache: since == 0) }
    /// One run block's own steps: `since`/`before` bound it (see StepCount),
    /// so a long finished run is read only when the owner opens it.
    func threadEvents(_ id: String, since: Int, before: Int) async throws -> [ThreadEvent] {
        try await request("GET", "/api/v1/threads/\(id)/events?since=\(since)&before=\(before)&limit=2000", cache: false)
    }
    /// The hub's per-message step counts — what a run block's headline prints.
    func threadSteps(_ id: String) async throws -> [StepCount] { try await request("GET", "/api/v1/threads/\(id)/steps", cache: true) }
    nonisolated static let threadsPath = "/api/v1/threads"
    nonisolated static func messagesPath(_ id: String) -> String { "/api/v1/threads/\(id)/messages?limit=200" }
    nonisolated static func eventsPath(_ id: String) -> String { "/api/v1/threads/\(id)/events?since=0&limit=1000" }
    nonisolated static let boardPath = "/api/v1/board?surface=mobile"
    nonisolated static let goalsPath = "/api/v1/goals"
    /// The change feed: parks until something the phone draws has changed
    /// (or `wait` seconds pass), then answers with the version to send next
    /// time. `since: ""` answers at once.
    func changes(since: String, thread: String = "", wait: Int = 25) async throws -> ChangeFeed {
        try await request("GET", "/api/v1/changes?since=\(since)&wait=\(wait)&thread=\(thread)", cache: false, timeout: TimeInterval(wait) + 15)
    }
    func threadEvents(_ id: String, ids: [Int]) async throws -> [ThreadEvent] { try await request("GET", "/api/v1/threads/\(id)/events?ids=\(ids.map(String.init).joined(separator: ","))", cache: false) }
    /// Start a session from the owner's words and pictures alone: no title
    /// (the hub auto-titles), no goal, no cadence — the session infers both
    /// from the message.
    func createThread(prompt: String, attachments: [String] = []) async throws -> Thread {
        struct B: Encodable { var prompt: String; var attachments: [String] }
        return try await request("POST", "/api/v1/threads", body: B(prompt: prompt, attachments: attachments))
    }
    func sendThread(_ id: String, text: String, attachments: [String] = []) async throws -> Thread {
        struct B: Encodable { var text: String; var attachments: [String] }
        return try await request("POST", "/api/v1/threads/\(id)/messages", body: B(text: text, attachments: attachments))
    }
    func patchThread(_ id: String, _ patch: [String: String]) async throws -> Thread { try await request("PATCH", "/api/v1/threads/\(id)", body: patch) }
    func checkinThread(_ id: String) async throws -> Thread { try await request("POST", "/api/v1/threads/\(id)/checkin") }
    func stopThread(_ id: String) async throws -> Thread { try await request("POST", "/api/v1/threads/\(id)/stop") }
    func archiveThread(_ id: String) async throws -> Thread { try await request("POST", "/api/v1/threads/\(id)/archive") }
    func calendar(from: String = "", to: String = "") async throws -> CalView { try await request("GET", "/api/v1/calendar?from=\(from)&to=\(to)") }
    /// One item by id — where a `cal-…` id tapped in a card's text lives (Refs.swift).
    func calItem(_ id: String) async throws -> CalItem { try await request("GET", "/api/v1/calendar/\(id)") }
    func resolveCalItem(_ id: String, state: String, note: String = "") async throws -> CalItem { try await request("POST", "/api/v1/calendar/\(id)/resolve", body: ["state": state, "by": "owner", "note": note]) }
    /// Move/retitle a still-scheduled item: any of day, at ("" = all day),
    /// title, detail, repeat. What a drag in the grid writes.
    func patchCalItem(_ id: String, _ patch: [String: String]) async throws -> CalItem { try await request("PATCH", "/api/v1/calendar/\(id)", body: patch) }
    /// What needs the owner on one surface, ranked, bundled and counted by the hub
    /// (GET /api/v1/board) — the phone prints these numbers, it never derives
    /// its own, so it and the console can no longer disagree.
    func board(surface: String = "mobile") async throws -> Board { try await request("GET", "/api/v1/board?surface=\(surface)", cache: true) }
    /// Every ask a session ever raised (open, answered, done, dismissed) — the
    /// thread view shows each under the reply that raised it.
    func threadAsks(_ threadID: String) async throws -> [Ask] { try await request("GET", "/api/v1/asks?state=all&thread=\(threadID)", cache: true) }
    /// One ask by id — the calendar's install row draws the real install cell
    /// from it; `GET /api/v1/asks/{id}`.
    func ask(_ id: String) async throws -> Ask { try await request("GET", "/api/v1/asks/\(id)", cache: true) }
    /// by "owner" (default; the hub's key for the owner) relays a Done to the thread; "app" is a silent
    /// optimistic close (the Install tap — the hub verifies via the build report).
    func resolveAsk(_ id: String, state: String, note: String = "", by: String = "owner") async throws -> Ask { try await request("POST", "/api/v1/asks/\(id)/resolve", body: ["state": state, "by": by, "note": note]) }
    /// Every prompt aimed at a session or delivered to it — its own future
    /// (queued) and its past. `state: "queued"` is what the session view shows.
    func prompts(state: String = "queued", thread: String = "") async throws -> [Prompt] {
        try await request("GET", "/api/v1/prompts?state=\(state)&thread=\(thread)")
    }
    /// The one write behind Respond: what this answers (`re`, e.g.
    /// "ask:ask-1234"), what the owner claims about it (`outcome`), which session
    /// hears it (`target`) and when (`in` "45m" / `on` "2026-09-03" + `at`
    /// "09:00"; none = now). The hub closes the card the moment an outcome
    /// arrives, even when the words are aimed at tomorrow morning.
    @discardableResult
    /// `replies` answers several cards with the one message; the
    /// hub closes each by its own rule and sets in_reply_to/outcome to the first.
    /// An `action:<id>` reply with approved/denied DECIDES the proposal from
    /// this prompt (the approval card's buttons arm the chat bar),
    /// so it carries the decider code like `approve` does; `via=app` is the
    /// surface on the audit row.
    func postPrompt(target: String, text: String, re: String = "", outcome: String = "",
                    inAfter: String = "", on: String = "", at: String = "",
                    title: String = "", attachments: [String] = [], replies: [PromptReply] = [],
                    decider: String? = nil) async throws -> Prompt {
        struct B: Encodable {
            var target, text, in_reply_to, outcome, `in`, on, at_time, title: String
            var attachments: [String]
            var replies: [PromptReply]
        }
        return try await request("POST", "/api/v1/prompts?via=app",
                                 body: B(target: target, text: text, in_reply_to: re, outcome: outcome,
                                         in: inAfter, on: on, at_time: at, title: title, attachments: attachments,
                                         replies: replies), decider: decider)
    }

    /// Was this error the hub refusing a decision for want of the code (403)?
    static func isDeciderRefusal(_ error: Error) -> Bool {
        (error as? APIError)?.status == 403
    }

    /// The words for a refused decision: a 403 is the decider code, and the
    /// phone knows which of the two it is — nothing saved here at all, or a
    /// saved code the hub does not accept (the hub logs the same split).
    /// Anything else is the hub's own line. The screen that shows this puts
    /// a button to Settings beside it, so the words stop at the fact.
    static func deciderHint(_ error: Error) -> String {
        if isDeciderRefusal(error) {
            return DeciderKeychain.exists()
                ? "Not sent: the decider code saved on this phone is wrong. Replace it in Settings, then send again."
                : "Not sent: no decider code is saved on this phone. Paste it from Apple Passwords in Settings, then send again."
        }
        return error.localizedDescription
    }
    @discardableResult
    func cancelPrompt(_ id: String) async throws -> Prompt { try await request("POST", "/api/v1/prompts/\(id)/cancel") }
    /// Restart the session an error card came from: the hub replays the turn
    /// that died and closes the card. 409 if it is already running again.
    @discardableResult func retryAsk(_ id: String) async throws -> Thread { try await request("POST", "/api/v1/asks/\(id)/retry") }
    /// env = this build's aps-environment ("development" for LAN/Xcode builds,
    /// "production" for ad-hoc OTA) so the hub pushes via the matching APNs host.
    /// Also reports this build (CFBundleVersion) so the hub can close
    /// "Install app build N" asks once the phone actually runs N.
    func registerDevice(token: String, env: String) async throws {
        struct Body: Encodable { var token, device, env: String; var build: Int }
        let build = Int(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "") ?? 0
        let _: Empty = try await request("POST", "/api/v1/devices", body: Body(token: token, device: UIDevice.current.name, env: env, build: build))
    }
    func testPush() async throws { let _: Empty = try await request("POST", "/api/v1/devices/test") }
    func readThread(_ id: String) async throws { let _: Empty = try await request("POST", "/api/v1/threads/\(id)/read") }
    /// Drop a card's line still waiting to speak; the card stays open.
    func hushVoice(card: String) async throws { let _: Empty = try await request("POST", "/api/v1/voice/hush", body: ["card": card]) }
    /// lane "ota" (default): `make ship`, then open `InstallStatus.ota.installURL`; "lan": Xcode install over the hub host's Wi-Fi.
    func appInstall(lane: String = "ota") async throws { struct R: Decodable { var status: String }; let _: R = try await request("POST", "/api/v1/app/install", body: ["lane": lane]) }
    struct InstallStatus: Decodable { var running: Bool; var tail: [String]; var ota: OTABuild?; var log_at: Date? }
    func appInstallStatus() async throws -> InstallStatus { try await request("GET", "/api/v1/app/install/status") }
    /// Recommendations. Deliberately no unread/badge call anywhere: a rec is
    /// pulled, never pushed (docs/design/recommendations.md).
    /// `status` is "open" (proposed), "all", or any one status.
    func recs(status: String = "open", domain: String = "") async throws -> [Rec] {
        let page: RecsPage = try await request("GET", "/api/v1/recs?status=\(status)&domain=\(domain)")
        return page.recs
    }
    func rec(_ id: String) async throws -> Rec { try await request("GET", "/api/v1/recs/\(id)") }
    /// The recs one session filed, every status, each with the reply it was
    /// filed in — the session's own cells. Still pull: fetched only when the
    /// chat is open, and it drives no badge.
    func threadRecs(_ threadID: String) async throws -> [Rec] {
        let page: RecsPage = try await request("GET", "/api/v1/recs?status=all&thread=\(threadID)", cache: true)
        return page.recs
    }
    /// The message the session that accepting a rec opens starts with. Composed
    /// by the hub, not here, so the console's Accept sends the same brief.
    func recStarter(_ id: String) async throws -> RecStarter { try await request("GET", "/api/v1/recs/\(id)/starter") }
    /// The owner's decision and their note — which is both the half a future session
    /// reads before it recommends the same thing again AND the message an
    /// agent gets. `deliver` says which agent: "source" = the session that
    /// filed it (the default, and the hub falls back to a new one if that
    /// session is gone), "new" = a fresh session with the whole brief, "" =
    /// nobody (record only).
    @discardableResult
    /// `until` (YYYY-MM-DD) goes with status "deferred": the day the rec
    /// comes back to the list, and the day an agent checks in on it.
    /// `attachments` are blob refs from uploadSessionPhoto: they ride IN the
    /// same message as the note, and need a `deliver`.
    /// `inAfter`/`on`/`at` are the same three timing fields `POST /prompts`
    /// takes (hub: `recWhen`): WHEN the session hears the note. The verdict
    /// itself never waits — the rec leaves the list the moment it is decided.
    func decideRec(_ id: String, status: String, note: String = "", deliver: String = "", until: String = "",
                   attachments: [String] = [], inAfter: String = "", on: String = "", at: String = "") async throws -> RecDecision {
        struct B: Encodable {
            var status, by, note, deliver, until: String
            var attachments: [String]
            var `in`, on, at_time: String
        }
        return try await request("POST", "/api/v1/recs/\(id)/decide",
                                 body: B(status: status, by: "owner", note: note, deliver: deliver, until: until,
                                         attachments: attachments, in: inAfter, on: on, at_time: at))
    }
    /// The fourth button: the note travels exactly as a
    /// decision's would — same two destinations — and the rec stays where it
    /// is, undecided. The hub refuses an empty `deliver`, and a reply with
    /// neither a note nor an attachment.
    func replyRec(_ id: String, note: String, deliver: String, attachments: [String] = [],
                  inAfter: String = "", on: String = "", at: String = "") async throws -> RecDecision {
        struct B: Encodable {
            var by, note, deliver: String
            var attachments: [String]
            var `in`, on, at_time: String
        }
        return try await request("POST", "/api/v1/recs/\(id)/reply",
                                 body: B(by: "owner", note: note, deliver: deliver, attachments: attachments,
                                         in: inAfter, on: on, at_time: at))
    }
    func scoreRec(_ id: String, outcome: String, note: String = "") async throws -> Rec {
        try await request("POST", "/api/v1/recs/\(id)/score", body: ["outcome": outcome, "by": "owner", "note": note])
    }
    func observations(source: String = "", kind: String = "", limit: Int = 50) async throws -> [Obs] {
        try await request("GET", "/api/v1/observations?source=\(source)&kind=\(kind)&limit=\(limit)")
    }
    /// Authenticated probe: proves URL, TLS, tailnet AND token.
    func check() async throws -> Int {
        let ps: [Project] = try await request("GET", "/api/v1/projects")
        return ps.count
    }
}

struct Empty: Decodable, Sendable {}

/// RFC3339 parsed by hand, because it is the hottest loop in the app: one
/// session's event stream carries thousands of timestamps, and an
/// ISO8601DateFormatter costs about ten times as much per call — and, being
/// stateful, cannot be shared once decoding runs off the main actor.
/// Accepts what the hub writes: `2026-08-25T17:20:31Z`,
/// `...31.482-04:00`, and a bare `2026-08-25`.
nonisolated func rfc3339(_ s: String) -> Date? {
    let b = Array(s.utf8)
    func num(_ lo: Int, _ hi: Int) -> Int? {
        guard hi <= b.count else { return nil }
        var v = 0
        for i in lo..<hi {
            let d = Int(b[i]) - 48
            guard d >= 0, d <= 9 else { return nil }
            v = v * 10 + d
        }
        return v
    }
    guard let year = num(0, 4), let month = num(5, 7), let day = num(8, 10), month >= 1, month <= 12 else { return nil }

    // Days since the epoch (Howard Hinnant's days_from_civil): no calendar,
    // no time zone database, no allocation.
    let y = year - (month <= 2 ? 1 : 0)
    let era = (y >= 0 ? y : y - 399) / 400
    let yoe = y - era * 400
    let doy = (153 * (month + (month > 2 ? -3 : 9)) + 2) / 5 + day - 1
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy
    var t = Double(era * 146097 + doe - 719468) * 86400

    if b.count >= 19 {                                  // has a clock
        guard let h = num(11, 13), let m = num(14, 16), let sec = num(17, 19) else { return nil }
        t += Double(h * 3600 + m * 60 + sec)
        var i = 19
        if i < b.count, b[i] == UInt8(ascii: ".") {     // fractional seconds
            i += 1
            var scale = 0.1
            while i < b.count, b[i] >= 48, b[i] <= 57 {
                t += Double(Int(b[i]) - 48) * scale
                scale /= 10
                i += 1
            }
        }
        if i < b.count, b[i] != UInt8(ascii: "Z"), b[i] != UInt8(ascii: "z") {
            let sign = b[i] == UInt8(ascii: "-") ? 1.0 : -1.0   // subtract the offset to reach UTC
            guard let oh = num(i + 1, i + 3), let om = num(i + 4, i + 6) else { return nil }
            t += sign * Double(oh * 3600 + om * 60)
        }
    }
    return Date(timeIntervalSince1970: t)
}

/// The phone's copy of what the hub last said, one entry per GET: the body, its ETag, and the decoded value once it has
/// been decoded once. Memory first, then a file per key under Caches/hub —
/// so a cold start paints the Sessions tab and a chat from disk before the
/// first byte arrives, and every request after that is a conditional one.
/// Nothing here leaves the device; Caches is purgeable and that is fine —
/// a missing entry just means one full fetch.
final class ResponseCache: @unchecked Sendable {
    struct Entry {
        var etag: String
        var data: Data
        var value: (any Sendable)?
    }
    private let lock = NSLock()
    private var memory: [String: Entry] = [:]
    private let dir: URL

    init() {
        dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("hub", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    private func file(_ key: String, _ ext: String) -> URL {
        // FNV-1a of the key: a stable file name with no path characters in it.
        var h: UInt64 = 0xcbf29ce484222325
        for b in key.utf8 { h = (h ^ UInt64(b)) &* 0x100000001b3 }
        return dir.appendingPathComponent(String(h, radix: 16) + "." + ext)
    }

    func entry(_ key: String) -> Entry? {
        lock.lock(); defer { lock.unlock() }
        if let e = memory[key] { return e }
        guard let data = try? Data(contentsOf: file(key, "json")) else { return nil }
        let etag = (try? String(contentsOf: file(key, "etag"), encoding: .utf8)) ?? ""
        let e = Entry(etag: etag, data: data, value: nil)
        memory[key] = e
        return e
    }

    func store(_ key: String, etag: String, data: Data, value: any Sendable) {
        lock.lock(); memory[key] = Entry(etag: etag, data: data, value: value); lock.unlock()
        try? data.write(to: file(key, "json"), options: .atomic)
        try? etag.write(to: file(key, "etag"), atomically: true, encoding: .utf8)
    }

    func setValue(_ key: String, _ value: any Sendable) {
        lock.lock(); defer { lock.unlock() }
        if var e = memory[key] { e.value = value; memory[key] = e }
    }
}

private struct AnyEncodable: Encodable {
    let value: Encodable
    init(_ v: Encodable) { value = v }
    func encode(to encoder: Encoder) throws { try value.encode(to: encoder) }
}

/// The decider code, kept where nothing — not this app's own code at rest, not
/// a session, not anyone holding the unlocked phone — can read it without
/// the owner's face.
///
/// A plain Keychain item loaded at launch and pinned to every outgoing request
/// would let a wrong value sit there invisibly and re-send itself forever.
/// Instead it is written once behind `.userPresence` and read only at the
/// instant of an approval, which is both stronger than typing a code (a rogue
/// session cannot present a face) and no effort at all.
///
/// `.userPresence`, not `.biometryCurrentSet`: the latter destroys the item
/// whenever a face is re-enrolled, and re-typing a 23-character code off the
/// back of a Face ID change is exactly the nuisance this removes. Passcode
/// stays as the fallback for a failed scan.
enum DeciderKeychain {
    private static let account = "hub.decider.protected"

    /// Write the code behind Face ID. Also clears the old unprotected item, so
    /// there is never a plaintext copy left to be read or to go stale.
    static func save(_ code: String) -> Bool {
        clear()
        Keychain.set("", for: "hub.decider") // delete the legacy plaintext item
        guard !code.isEmpty else { return true }
        guard let access = SecAccessControlCreateWithFlags(nil,
            kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .userPresence, nil) else { return false }
        let add: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: account,
            kSecAttrService as String: "life.hub",
            kSecValueData as String: Data(code.utf8),
            kSecAttrAccessControl as String: access,
        ]
        return SecItemAdd(add as CFDictionary, nil) == errSecSuccess
    }

    static func clear() {
        SecItemDelete([kSecClass as String: kSecClassGenericPassword,
                       kSecAttrAccount as String: account,
                       kSecAttrService as String: "life.hub"] as CFDictionary)
    }

    /// Is a code stored at all? Answered WITHOUT a Face ID prompt: attributes
    /// only, through a context that may not show UI. The answer is "no" ONLY
    /// when the Keychain says the item is not there. Asking for the DATA and
    /// counting only `errSecInteractionNotAllowed`/`errSecSuccess` as a yes
    /// misreads a saved code that comes back with some other status, so every
    /// Approve would send no code and the hub would refuse it.
    static func exists() -> Bool {
        let ctx = LAContext()
        ctx.interactionNotAllowed = true
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: account,
            kSecAttrService as String: "life.hub",
            kSecReturnAttributes as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
            kSecUseAuthenticationContext as String: ctx,
        ]
        return SecItemCopyMatching(q as CFDictionary, nil) != errSecItemNotFound
    }

    /// Read the code, prompting for Face ID. Blocking, so it is called off the
    /// main actor. `found` is false only when no item exists; a cancelled or
    /// failed scan is found with a nil code.
    nonisolated static func read(reason: String) -> (code: String?, found: Bool) {
        let ctx = LAContext()
        ctx.localizedReason = reason
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: account,
            kSecAttrService as String: "life.hub",
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
            kSecUseAuthenticationContext as String: ctx,
        ]
        var out: AnyObject?
        let st = SecItemCopyMatching(q as CFDictionary, &out)
        guard st != errSecItemNotFound else { return (nil, false) }
        guard st == errSecSuccess, let d = out as? Data else { return (nil, true) }
        return (String(data: d, encoding: .utf8), true)
    }
}

enum Keychain {
    static func set(_ value: String, for key: String) {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrAccount as String: key, kSecAttrService as String: "life.hub"]
        SecItemDelete(q as CFDictionary)
        guard !value.isEmpty else { return }
        var add = q
        add[kSecValueData as String] = Data(value.utf8)
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(add as CFDictionary, nil)
    }
    static func get(_ key: String) -> String? {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrAccount as String: key, kSecAttrService as String: "life.hub",
                                kSecReturnData as String: true, kSecMatchLimit as String: kSecMatchLimitOne]
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }
}
