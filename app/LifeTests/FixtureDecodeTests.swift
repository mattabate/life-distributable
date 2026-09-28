// The contract from real hub output. hub/internal/server
// TestFixtures serves every GET the app reads from a seeded test hub and
// writes the reply to shared/fixtures/<name>.json (`make generate`); `make
// test` fails when one is stale. Here each is decoded with the app's own type
// and decoder, then walked again with a decoder that records every key the
// type asked for: a key the hub sends that no Swift type reads fails, unless
// it is on the allow-list below with the reason nobody needs it.
//
// The simulator shares the Mac's filesystem, so the repo files are read in
// place (as FormatTests does) — nothing to bundle, no project churn per file.
import XCTest
@testable import Life

final class FixtureDecodeTests: XCTestCase {
    private static let dir = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("shared/fixtures")

    /// Fixture name → the type the app decodes that reply into.
    private static var routes: [(String, any Decodable.Type)] { [
        ("spend_summary", SpendSummary.self),
        ("spend_model", ModelSetting.self),
        ("usage", UsageState.self),
        ("projects", [Project].self),
        ("sessions", [TmuxSession].self),
        ("actions", [Action].self),
        ("actions_id", Action.self),
        ("runs_id", JobRun.self),
        ("decider", DeciderStatus.self),
        ("status", HubStatus.self),
        ("goals", [Goal].self),
        ("goals_id", Goal.self),
        ("goals_id_notes", [GoalNote].self),
        ("sources", SourcesInventory.self),
        ("threads", [Life.Thread].self),
        ("threads_id", Life.Thread.self),
        ("threads_id_messages", [ThreadMessage].self),
        ("threads_id_events", [ThreadEvent].self),
        ("threads_id_steps", [StepCount].self),
        ("changes", ChangeFeed.self),
        ("calendar", CalView.self),
        ("calendar_id", CalItem.self),
        ("board", Board.self),
        ("asks", [Ask].self),
        ("asks_id", Ask.self),
        ("prompts", [Prompt].self),
        ("app_install_status", HubClient.InstallStatus.self),
        ("observations", [Obs].self),
        ("recs", RecsPage.self),
        ("recs_id", Rec.self),
        ("recs_id_starter", RecStarter.self),
    ] }

    /// Keys the hub sends that the app deliberately does not read, as
    /// `<fixture><path>` — each with why.
    private static let allowed: Set<String> = [
        // The Calendar tab fetches /calendar itself; the board's copy is for the console.
        "board.calendar",
        // Row timestamps no page prints.
        "goals[].created_at", "goals[].updated_at", "goals_id.created_at", "goals_id.updated_at",
        "observations[].ingested_at", "observations[].schema_version", "observations[].tz",
        "status.time",
        // A prompt's replies live in its chat, not on the Settings row.
        "prompts[].replies",
        // A finding's exec payload is the approval's, drawn from /actions.
        "runs_id.findings[].exec_payload", "runs_id.findings[].exec_type",
        // The spend page draws by model and by day; the console draws these two.
        "spend_summary.by_trigger", "spend_summary.jobs",
    ]

    func testEveryFixtureHasAType() throws {
        let files = try FileManager.default.contentsOfDirectory(atPath: Self.dir.path)
            .filter { $0.hasSuffix(".json") }.map { String($0.dropLast(5)) }
        XCTAssertEqual(Set(files), Set(Self.routes.map(\.0)), "a fixture with no Swift type, or a type with no fixture")
    }

    func testFixturesDecodeAndEveryKeyIsRead() throws {
        var unread: [String] = []
        for (name, type) in Self.routes {
            let data = try Data(contentsOf: Self.dir.appendingPathComponent(name + ".json"))
            do {
                _ = try HubClient.decoder().decode(type, from: data)
            } catch {
                XCTFail("\(name): the app's decoder refuses the hub's reply: \(error)")
                continue
            }
            let tree = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
            let audit = KeyAudit()
            do {
                _ = try type.init(from: AuditDecoder(value: tree, path: "", audit: audit, codingPath: []))
            } catch {
                XCTFail("\(name): audit decode failed: \(error)")
                continue
            }
            unread += audit.unread(in: tree).map { name + $0 }.filter { !Self.allowed.contains($0) }
        }
        XCTAssert(unread.isEmpty, "keys the hub sends that no Swift type reads — add them to the model, or to `allowed` with a reason:\n" + unread.sorted().joined(separator: "\n"))
    }

    func testAllowListHasNoDeadRows() throws {
        // An allowed key the hub no longer sends is a stale row.
        var sent: Set<String> = []
        for (name, _) in Self.routes {
            let data = try Data(contentsOf: Self.dir.appendingPathComponent(name + ".json"))
            let tree = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
            sent.formUnion(KeyAudit.paths(in: tree).map { name + $0 })
        }
        XCTAssert(Self.allowed.subtracting(sent).isEmpty, "allowed but never sent: \(Self.allowed.subtracting(sent).sorted())")
    }
}

// MARK: - The recording decoder

/// Which key paths a decode touched: `.threads[].speaking`, arrays as `[]`.
final class KeyAudit {
    var read: Set<String> = []

    /// A key present in the JSON and never asked for. Only the topmost one is
    /// reported: an unread object's own keys are its parent's problem.
    func unread(in tree: Any, at path: String = "") -> [String] {
        var out: [String] = []
        if let obj = tree as? [String: Any] {
            for (k, v) in obj {
                let p = path + "." + k
                if read.contains(p) { out += unread(in: v, at: p) } else { out.append(KeyAudit.collapse(p)) }
            }
        } else if let arr = tree as? [Any] {
            for v in arr { out += unread(in: v, at: path + "[]") }
        }
        return Array(Set(out))
    }

    static func paths(in tree: Any, at path: String = "") -> Set<String> {
        var out: Set<String> = []
        if let obj = tree as? [String: Any] {
            for (k, v) in obj {
                out.insert(collapse(path + "." + k))
                out.formUnion(paths(in: v, at: path + "." + k))
            }
        } else if let arr = tree as? [Any] {
            for v in arr { out.formUnion(paths(in: v, at: path + "[]")) }
        }
        return out
    }

    /// Keys that are data (ids, dates) read as `*`, as the hub's shape check does.
    static func collapse(_ path: String) -> String {
        path.split(separator: ".", omittingEmptySubsequences: false).map { seg -> String in
            let s = String(seg)
            let key = s.hasSuffix("[]") ? String(s.dropLast(2)) : s
            let isData = key.first?.isNumber == true || key.contains(":") || key.contains("/") || key.contains(" ")
                || key.range(of: #"-[0-9a-f]{8}$"#, options: .regularExpression) != nil
                || (!key.isEmpty && key.allSatisfy { $0.isUppercase || $0.isNumber || $0 == "_" })
            return isData ? "*" + (s.hasSuffix("[]") ? "[]" : "") : s
        }.joined(separator: ".")
    }
}

private struct AnyKey: CodingKey {
    var stringValue: String
    var intValue: Int?
    init(stringValue: String) { self.stringValue = stringValue }
    init(intValue: Int) { stringValue = String(intValue); self.intValue = intValue }
}

/// A Decoder over a JSONSerialization tree that decodes like HubClient's
/// JSONDecoder (RFC 3339 dates, URLs from strings) and records every key read.
private struct AuditDecoder: Decoder {
    let value: Any
    let path: String
    let audit: KeyAudit
    var codingPath: [CodingKey]
    var userInfo: [CodingUserInfoKey: Any] { [:] }

    func container<Key: CodingKey>(keyedBy type: Key.Type) throws -> KeyedDecodingContainer<Key> {
        guard let obj = value as? [String: Any] else {
            throw DecodingError.typeMismatch([String: Any].self, .init(codingPath: codingPath, debugDescription: "not an object at \(path)"))
        }
        return KeyedDecodingContainer(Keyed<Key>(obj: obj, path: path, audit: audit, codingPath: codingPath))
    }
    func unkeyedContainer() throws -> UnkeyedDecodingContainer {
        guard let arr = value as? [Any] else {
            throw DecodingError.typeMismatch([Any].self, .init(codingPath: codingPath, debugDescription: "not an array at \(path)"))
        }
        return Unkeyed(arr: arr, path: path + "[]", audit: audit, codingPath: codingPath)
    }
    func singleValueContainer() throws -> SingleValueDecodingContainer { Single(value: value, path: path, audit: audit, codingPath: codingPath) }
}

/// Objects and arrays are walked (that is where keys are); a scalar has no
/// keys, so it goes to the app's own decoder as a fragment — dates, URLs,
/// numbers and string enums decode exactly as they do on the phone.
private func unbox<T: Decodable>(_ type: T.Type, _ v: Any, _ path: String, _ audit: KeyAudit, _ cp: [CodingKey]) throws -> T {
    if v is [String: Any] || v is [Any] {
        // A scalar asked of an object is a mismatch (a `try?` probe in some
        // init(from:)), not a walk: Double(from:) would ask for itself forever.
        if T.self is any BinaryInteger.Type || T.self is any BinaryFloatingPoint.Type || T.self == String.self || T.self == Bool.self {
            throw DecodingError.typeMismatch(T.self, .init(codingPath: cp, debugDescription: "container at \(path)"))
        }
        return try T(from: AuditDecoder(value: v, path: path, audit: audit, codingPath: cp))
    }
    let data = try JSONSerialization.data(withJSONObject: v, options: [.fragmentsAllowed])
    return try HubClient.decoder().decode(T.self, from: data)
}

private struct Keyed<Key: CodingKey>: KeyedDecodingContainerProtocol {
    let obj: [String: Any]
    let path: String
    let audit: KeyAudit
    var codingPath: [CodingKey]
    var allKeys: [Key] { obj.keys.compactMap { Key(stringValue: $0) } }

    private func p(_ key: Key) -> String {
        let p = path + "." + key.stringValue
        audit.read.insert(p)
        return p
    }
    private func value(_ key: Key) throws -> Any {
        guard let v = obj[key.stringValue] else {
            throw DecodingError.keyNotFound(key, .init(codingPath: codingPath, debugDescription: "at \(path)"))
        }
        return v
    }
    func contains(_ key: Key) -> Bool { _ = p(key); return obj[key.stringValue] != nil }
    func decodeNil(forKey key: Key) throws -> Bool { _ = p(key); return obj[key.stringValue] == nil || obj[key.stringValue] is NSNull }
    func decode<T: Decodable>(_ type: T.Type, forKey key: Key) throws -> T {
        try unbox(T.self, try value(key), p(key), audit, codingPath + [key])
    }
    func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type, forKey key: Key) throws -> KeyedDecodingContainer<NestedKey> {
        try AuditDecoder(value: try value(key), path: p(key), audit: audit, codingPath: codingPath + [key]).container(keyedBy: type)
    }
    func nestedUnkeyedContainer(forKey key: Key) throws -> UnkeyedDecodingContainer {
        try AuditDecoder(value: try value(key), path: p(key), audit: audit, codingPath: codingPath + [key]).unkeyedContainer()
    }
    func superDecoder() throws -> Decoder { AuditDecoder(value: obj, path: path, audit: audit, codingPath: codingPath) }
    func superDecoder(forKey key: Key) throws -> Decoder {
        AuditDecoder(value: try value(key), path: p(key), audit: audit, codingPath: codingPath + [key])
    }
}

private struct Unkeyed: UnkeyedDecodingContainer {
    let arr: [Any]
    let path: String
    let audit: KeyAudit
    var codingPath: [CodingKey]
    var count: Int? { arr.count }
    var isAtEnd: Bool { currentIndex >= arr.count }
    var currentIndex = 0

    init(arr: [Any], path: String, audit: KeyAudit, codingPath: [CodingKey]) {
        self.arr = arr; self.path = path; self.audit = audit; self.codingPath = codingPath
    }
    private mutating func next() throws -> Any {
        guard !isAtEnd else { throw DecodingError.valueNotFound(Any.self, .init(codingPath: codingPath, debugDescription: "end of \(path)")) }
        defer { currentIndex += 1 }
        return arr[currentIndex]
    }
    mutating func decodeNil() throws -> Bool {
        if !isAtEnd, arr[currentIndex] is NSNull { currentIndex += 1; return true }
        return false
    }
    mutating func decode<T: Decodable>(_ type: T.Type) throws -> T {
        let key = AnyKey(intValue: currentIndex)
        return try unbox(T.self, try next(), path, audit, codingPath + [key])
    }
    mutating func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type) throws -> KeyedDecodingContainer<NestedKey> {
        try AuditDecoder(value: try next(), path: path, audit: audit, codingPath: codingPath).container(keyedBy: type)
    }
    mutating func nestedUnkeyedContainer() throws -> UnkeyedDecodingContainer {
        try AuditDecoder(value: try next(), path: path, audit: audit, codingPath: codingPath).unkeyedContainer()
    }
    mutating func superDecoder() throws -> Decoder { AuditDecoder(value: try next(), path: path, audit: audit, codingPath: codingPath) }
}

private struct Single: SingleValueDecodingContainer {
    let value: Any
    let path: String
    let audit: KeyAudit
    var codingPath: [CodingKey]
    func decodeNil() -> Bool { value is NSNull }
    func decode<T: Decodable>(_ type: T.Type) throws -> T { try unbox(T.self, value, path, audit, codingPath) }
}
