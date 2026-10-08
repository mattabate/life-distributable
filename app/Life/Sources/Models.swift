import Foundation

// Mirrors shared/api.md. Keep field names identical to the JSON.
struct Project: Codable, Identifiable, Hashable {
    var name: String
    var dir: String
    var id: String { name }
}

struct Bucket: Codable, Identifiable, Hashable {
    var key: String
    var usd: Double
    var messages: Int
    var input_tokens: Int
    var output_tokens: Int
    var cache_read_tokens: Int
    var cache_write_tokens: Int
    /// The same dollars split by model, dearest first — on `by_day` buckets
    /// only (the stacked day chart and its card); nil elsewhere.
    var models: [Bucket]?
    var id: String { key }
}

struct SpendSession: Codable, Identifiable, Hashable {
    var session_id: String
    var project: String
    var cwd: String
    var start: Date
    var end: Date
    var usd: Double
    var messages: Int
    var models: [String]
    var id: String { session_id }
}

struct SpendSummary: Codable {
    var generated_at: Date
    var days: Int
    var window_hours: Double
    var total_usd: Double
    var today_usd: Double
    var messages: Int
    var unknown_models: [String]
    var by_day: [Bucket]
    /// Every day since the first transcript, oldest first, each with its
    /// split by model: the day chart's series, all-time whatever the range;
    /// the tiles keep the window.
    var history: [Bucket]
    var by_project: [Bucket]
    var by_model: [Bucket]
    /// Colour slot → model key ("" = unused): the hub's fixed order, so a
    /// model is the same colour in every range and on the console.
    var palette: [String]
    var sessions: [SpendSession]
}

struct QuotaWindow: Codable, Identifiable, Hashable {
    var key: String
    var label: String
    var note: String?
    var utilization: Double
    var resets_at: Date?
    var starts_at: Date?
    var spent_usd: Double
    var messages: Int
    var headroom_usd: Double
    var by_model: [Bucket]
    var scope_model: String?        // "fable" when this meter gates one family only
    var burn_pct_per_hour: Double?  // fill rate from the last 45 min of local spend
    var full_at: Date?              // projected 100% at that rate; nil when full/idle
    // Anthropic's own number now minus its own number up to 8h ago, from
    // readings the hub keeps every 10 min: the one pace that is measured
    // rather than derived from list-price dollars.
    var measured_pct_per_hour: Double?
    var measured_delta_pct: Double?
    var measured_span_hours: Double?
    // Same $→% rate as burn, over the trailing day instead of 45 minutes.
    var typical_pct_per_hour: Double?
    var full_at_typical: Date?
    /// Share of the window's clock that has run, 0..100 (0 = reset unknown);
    /// drawn as a tick on the bar, same as the console's meter.
    var elapsed_pct: Double?
    /// The hub's words and colour for the card, the console's too:
    /// tone "bad" | "warn" | "", outlook "≈80% by reset", foot "4 d 2 h left · 41% of the week gone · resets …".
    var tone: String?
    var outlook: String?
    var foot: String?
    var id: String { key }
    var elapsedPct: Double { elapsed_pct ?? 0 }

    // Go marshals an unset time.Time as year 1, not null; treat that as unknown.
    var resets: Date? { real(resets_at) }
    var fills: Date? { real(full_at) }
    var fillsTypical: Date? { real(full_at_typical) }
    var burn: Double { burn_pct_per_hour ?? 0 }
    var typical: Double { typical_pct_per_hour ?? 0 }
    var measuredHours: Double { measured_span_hours ?? 0 }
    var measuredDelta: Double { measured_delta_pct ?? 0 }
    private func real(_ d: Date?) -> Date? { (d?.timeIntervalSince1970 ?? 0) > 0 ? d : nil }
}

struct Quota: Codable {
    var generated_at: Date
    var fetched_at: Date?
    var available: Bool
    var next_model: String?
    var next_reason: String?
    var error: String?
    var windows: [QuotaWindow]
    /// The plan the sessions run on (`quota.plan`): its name, what runs it,
    /// its price when the hub has found one, and this month's spend.
    var plan: ClaudePlan?
}

/// `spend/quota.plan` — the Configuration page's "Powered by" card.
struct ClaudePlan: Codable {
    var name: String
    var via: String
    var usd: Double?
    var period: String?
    var charged_on: String?
    var month_usd: Double
    var brand: BrandMark?
}

/// A provider's small tile: its letters on its colour, or a logo path
/// (SVG `d`, 24×24) drawn in the ink colour. Sent with every source and
/// with the plan.
struct BrandMark: Codable, Hashable {
    var mark: String
    var color: String
    var ink: String
    var logo: String?
}

struct APIError: Codable, Error, LocalizedError {
    var error: String
    /// The HTTP status, filled in by `HubClient.send` — not part of the body.
    /// A caller needs it to tell 403 (your decider code) from 409 (already
    /// decided); the console has had `err.status` for the same reason.
    var status: Int?
    var errorDescription: String? { error }

    enum CodingKeys: String, CodingKey { case error }
}

/// `GET /decider` — whether the code THIS phone holds is the one the hub will
/// accept, asked before an approval depends on it.
struct DeciderStatus: Codable, Hashable {
    /// Has the owner armed a code at all? Unarmed = approve takes the hub token.
    var armed: Bool
    /// Did this phone send one?
    var sent: Bool
    /// True when the code verifies, or when nothing is armed.
    var ok: Bool
    /// When the hub's current code was made (nil when unarmed, or from a hub
    /// older than 2026-10-05). A refusal is read against this date.
    var set_at: Date?
}

struct Action: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date
    var updated_at: Date
    var project: String
    var kind: String
    var title: String
    var detail: String
    var gated: Bool
    var exec_type: String
    /// What approving runs — the console's "will run:" block.
    var exec_payload: ExecPayload?
    var state: String
    var decided_at: Date?
    var decided_via: String?
    var result: String?
    var error: String?
    var thread_id: String?  // session that proposed it; the owner's decision + note go back there
    var note: String?
    /// Who proposed it: `claude:thread:<id>` | `claude:job:<name>` | `lifectl` | `app`.
    var source: String?
    /// The run that proposed it: a thread run, or for a scheduled job the id
    /// `GET /runs/{id}` reads — the card opens on that when there is no session.
    var run_id: String?
    /// What the push said when this proposal landed (`actions.said`) — the
    /// proposer's `--say` or the hub's own sentence. Drawn under the detail
    /// exactly as an ask's is. nil on a proposal that never pushed and on rows
    /// older than that column.
    var said: String?
    /// The audit trail, oldest first — `GET /actions/{id}` only; nil on rows that came from a list or the board.
    var events: [ActionEvent]?
    /// The card's word buttons, the hub's words (Approve · Deny
    /// · Reply — the last only with a session to reply to). Optional:
    /// an older hub does not send it, and the card falls back to that list.
    var outcomes: [AskOutcome]?
    /// Where it stands, the hub's (store.ActionStanding): `open` = proposed,
    /// `closed` = decided; `folded` "dismissed" = set aside, Reopen inside.
    /// `reopen` = carries Reopen (only a dismissed one: a decided one ran).
    var open: Bool?
    var closed: Bool?
    var folded: String?
    var lane: String?
    var reopen: Bool?
    /// When it is owed (the items table, 2026-09-27): now | on | by | soon.
    var window: String?

    /// The scheduled job behind a thread-less proposal, or nil.
    var job: String? {
        guard thread_id == nil, let s = source, s.hasPrefix("claude:job:") else { return nil }
        return String(s.dropFirst("claude:job:".count))
    }
}

/// An action's `exec_payload` as text: the hub stores raw JSON, a shell command
/// as a string or a structured call as an object. Same rule as the console
/// (ui.js actionHTML): a string prints as itself, anything else pretty-printed.
struct ExecPayload: Codable, Hashable {
    var text: String

    init(text: String) { self.text = text }

    init(from decoder: Decoder) throws {
        if let s = try? String(from: decoder) { text = s; return }
        let any = try JSONAny(from: decoder).object
        let data = try JSONSerialization.data(withJSONObject: any, options: [.prettyPrinted, .sortedKeys, .fragmentsAllowed, .withoutEscapingSlashes])
        text = String(decoding: data, as: UTF8.self)
    }

    func encode(to encoder: Encoder) throws { try text.encode(to: encoder) }
}

/// Any JSON value, decoded only to be printed again.
private enum JSONAny: Decodable {
    case null, bool(Bool), num(Double), str(String), arr([JSONAny]), obj([String: JSONAny])

    init(from decoder: Decoder) throws {
        if var a = try? decoder.unkeyedContainer() {
            var out: [JSONAny] = []
            while !a.isAtEnd { out.append(try a.decode(JSONAny.self)) }
            self = .arr(out); return
        }
        if let o = try? decoder.container(keyedBy: AnyKey.self) {
            var out: [String: JSONAny] = [:]
            for k in o.allKeys { out[k.stringValue] = try o.decode(JSONAny.self, forKey: k) }
            self = .obj(out); return
        }
        let v = try decoder.singleValueContainer()
        if try v.decodeNil() { self = .null }
        else if let b = try? v.decode(Bool.self) { self = .bool(b) }
        else if let n = try? v.decode(Double.self) { self = .num(n) }
        else { self = .str(try v.decode(String.self)) }
    }

    var object: Any {
        switch self {
        case .null: return NSNull()
        case .bool(let b): return b
        case .num(let n): return n == n.rounded() && abs(n) < 1e15 ? Int(n) as Any : n
        case .str(let s): return s
        case .arr(let a): return a.map(\.object)
        case .obj(let o): return o.mapValues(\.object)
        }
    }

    private struct AnyKey: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { nil }
    }
}

/// One scheduled-job run read back for a person (`GET /runs/{id}`): the row
/// plus what the model said — `text` is its prose before the JSON envelope,
/// `findings` the things it filed (gated ones became proposals), `needs_you`
/// what it said only the owner can do. A job has no session, so a proposal from
/// one opens on this.
struct JobRun: Codable, Identifiable, Hashable {
    var id: Int
    var job: String
    var started_at: Date
    var finished_at: Date?
    var ok: Bool?
    var summary: String?
    /// Everything the job printed, unparsed — the console's "Raw output".
    var output: String?
    var error: String?
    var cost_usd: Double
    var session_id: String?
    var text: String?
    var findings: [JobFinding]
    var needs_you: [String]
}

struct JobFinding: Codable, Hashable {
    var kind: String
    var title: String
    var detail: String
}

/// One row of an action's audit trail: proposed / approved / denied / ran /
/// failed, who did it and the note they left.
struct ActionEvent: Codable, Identifiable, Hashable {
    var id: Int
    var action_id: String
    /// RFC3339 as the hub wrote it; `time` parses it for display.
    var ts: String
    var event: String
    var actor: String?
    var note: String?
    var time: Date? { rfc3339(ts) }
    /// One grey line on a card: "approved by app · 02:31 · note".
    var line: String {
        var s = event
        if let actor, !actor.isEmpty { s += " by " + actor }
        s += " · " + (time.map { $0.formatted(date: .omitted, time: .shortened) } ?? ts)
        if let note, !note.isEmpty { s += " · " + note }
        return s
    }
}

struct Goal: Codable, Identifiable, Hashable {
    var id: String
    var title: String
    var statement: String
    var horizon: String
    var cadence: String
    var status: String
    var sources: String
    var last_reviewed_at: Date?
    var note_count: Int
    // Where the goal stands, in flat prose — what a session reads instead of
    // the note history. Absent from the goal LIST (the hub omits it there to
    // keep that call small), so it is optional and only filled on a detail read.
    var digest: String?
    var digest_at: Date?
    /// The goal's face: a symbol word and a hue, derived by the hub from the
    /// name (never stored). The console draws the same symbol and hue.
    var emblem: GoalEmblem
}

/// `emblem` on a Goal (shared/api.md): `symbol` is one of health, money, agent,
/// market, audience, learn, art, goal; `hue` a whole degree.
struct GoalEmblem: Codable, Hashable {
    var symbol: String
    var hue: Int

    /// The SF Symbol for the hub's word; the console's GOAL_ICONS row for row.
    var systemImage: String {
        switch symbol {
        case "health": "heart.fill"
        case "money": "dollarsign"
        case "agent": "sparkles"
        case "market": "tag.fill"
        case "audience": "person.2.fill"
        case "learn": "book.fill"
        case "art": "paintpalette.fill"
        default: "target"
        }
    }
}

struct GoalNote: Codable, Identifiable, Hashable {
    var id: Int
    var goal_id: String
    var created_at: Date
    var author: String
    var kind: String
    var text: String
    /// Who wrote it, in words: the session's title, the owner's word (hub goals.go), a job name. The
    /// page prints this and the day, never `author` or `kind`.
    var by: String?
    /// The session that wrote it, when it still exists.
    var thread_id: String?
}

struct HubStatus: Codable {
    var ok: Bool
    var uptime_s: Int
    var pending_actions: Int
    var jobs: Int
    var app_installed: String?
    var app_profile_expires: String?
    var app_profile_days_left: Int?
    var ota: OTABuild?
    var devices: [RegisteredDevice]?
    /// The hub's one ticker (internal/clock): one row per housekeeping loop.
    var clock: [ClockTask]?
}

struct ClockTask: Codable, Identifiable {
    var name: String
    var every: String
    var next_due: Date
    var last_start: Date?
    var last_end: Date?
    var running: Bool
    var last_error: String?
    var runs: Int
    var errors: Int
    var id: String { name }
}

/// Newest over-the-air build published by `make ship` (data/ota/current.json).
struct OTABuild: Codable, Hashable {
    var version: String
    var build: Int
    var url: String             // …/ota/<token>/install.html
    var profile_expires: String?
    var aps: String?
    var built_at: String?
    var commit: String?
    /// Build the next `make ship` would produce (the Mac's commit count).
    /// Not what is published — `build` is. Nil when the hub cannot run git.
    var head: Int?
    /// Does anything under app/ differ from `commit`? The build number counts
    /// EVERY commit in the repo, so `head > build` on its own only means
    /// someone wrote a doc. Nil when the hub cannot run git.
    var app_changed: Bool?
    /// itms-services link iOS understands directly (no Safari hop).
    var installURL: URL? {
        let manifest = url.replacingOccurrences(of: "install.html", with: "manifest.plist")
        guard let enc = manifest.addingPercentEncoding(withAllowedCharacters: .alphanumerics) else { return nil }
        return URL(string: "itms-services://?action=download-manifest&url=\(enc)")
    }
}

struct RegisteredDevice: Codable, Hashable {
    var token: String
    var env: String
}

struct Obs: Codable, Identifiable, Hashable, Sendable {
    var id: Int
    var source: String
    var kind: String
    var ts: Date
    var payload: [String: JSONValue]
    var blob_ref: String?
}

/// Minimal JSON value for free-form payloads.
enum JSONValue: Codable, Hashable, Sendable {
    case string(String), number(Double), bool(Bool), null, array([JSONValue]), object([String: JSONValue])
    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        else if let n = try? c.decode(Double.self) { self = .number(n) }
        else if let s = try? c.decode(String.self) { self = .string(s) }
        else if let a = try? c.decode([JSONValue].self) { self = .array(a) }
        else { self = .object(try c.decode([String: JSONValue].self)) }
    }
    func encode(to e: Encoder) throws {
        var c = e.singleValueContainer()
        switch self {
        case .string(let s): try c.encode(s); case .number(let n): try c.encode(n); case .bool(let b): try c.encode(b)
        case .null: try c.encodeNil(); case .array(let a): try c.encode(a); case .object(let o): try c.encode(o)
        }
    }
    var string: String? { if case .string(let s) = self { return s }; return nil }
    var number: Double? { if case .number(let n) = self { return n }; return nil }
    var bool: Bool? { if case .bool(let b) = self { return b }; return nil }
}

struct Thread: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date
    var updated_at: Date
    var title: String
    var project: String
    var goal_id: String?
    var status: String
    var schedule: String
    var schedule_prompt: String
    var last_run_at: Date?
    var next_run_at: Date?
    var unread: Int
    // Dollars, the turn in flight included: the hub prices each streamed
    // message at its own model, so this climbs while a session works instead
    // of standing still until the turn ends.
    var cost_usd: Double
    // The same dollars split by model, dearest first — a long chat drops to a
    // cheaper model when a plan bucket fills. Only GET one thread returns it,
    // so it is nil on rows that came from the list.
    var cost_by_model: [ModelCost]?
    // What the chat has burned in tokens, the turn in flight included, so a
    // running session's number climbs while it works. nil on
    // an older hub; the four buckets add up to `tokens`.
    var tokens: Int?
    var tokens_in: Int?
    var tokens_out: Int?
    var tokens_cache_read: Int?
    var tokens_cache_write: Int?
    var last_message: String?
    var last_message_at: Date?
    /// Kind of that message; `error` = the turn died, so a list must not
    /// print it as the session's answer. nil on an older hub.
    var last_message_kind: String?
    var activity: String?
    /// The turn in flight (running only): its tool calls, dollars so far and
    /// the time of its newest tool call (the turn's start before the first).
    /// The chat's working line and the session card print them
    /// as `turnFacts`. nil from an older hub.
    var turn_tools: Int?
    var turn_cost_usd: Double?
    var turn_at: Date?
    var needs_you: Int
    /// The model the session runs on — its live run's, else the newest run
    /// that named one (list and single GET only; nil when it never ran with
    /// an explicit model, or on an older hub). Printed short on the row.
    var model: String?
    /// The hub's words for the model ("fable 5.1"), the cadence ("weekly Sun
    /// 17:30") and the status capsule — the console prints the same ones.
    /// nil from an older hub or a PATCH/POST reply.
    var model_label: String?
    var schedule_label: String?
    var pill: Pill?
    /// A card of this session's is being spoken right now / queued behind
    /// another (threads.Voice; the "speaking" pill). Absent = false.
    var speaking: Bool?
    var waiting_to_speak: Bool?
    /// The rung its wakes start on: "" (auto), judgment, build, or a model id.
    var model_class: String?
    var modelShort: String? { model_label.flatMap { $0.isEmpty ? nil : $0 } ?? model.flatMap { $0.isEmpty ? nil : shortModel($0) } }
}

/// One model's share of a chat's bill. `model` is the CLI's id
/// ("claude-opus-5"), empty when that run took the default and never said.
struct ModelCost: Codable, Identifiable, Hashable {
    var model: String
    var cost_usd: Double
    var id: String { model }
    /// "Opus 5", "Sonnet 5", "Haiku 4.5" — the id with the vendor prefix and
    /// the date suffix taken off, since the settings row has no width for
    /// "claude-haiku-4-5-20251001".
    var label: String {
        if model.isEmpty { return "default model" }
        var parts = model.split(separator: "-").map(String.init)
        if parts.first == "claude" { parts.removeFirst() }
        if let last = parts.last, last.count == 8, Int(last) != nil { parts.removeLast() }
        let name = parts.first?.capitalized ?? model
        let version = parts.dropFirst().joined(separator: ".")
        return version.isEmpty ? name : "\(name) \(version)"
    }
}

/// One answer chip on an ask: the prompts `outcome` it posts ("" = words only,
/// the card stays open; done | wont close it) and the label to show.
struct AskOutcome: Codable, Hashable {
    var value: String
    var label: String
    /// What picking it does, in a few words (recs, proposals, ticks); absent
    /// on an ask's own chips.
    var hint: String? = nil
}

/// One thing an agent needs from the owner (docs/ASKS.md). Lives on the board
/// until the agent closes it or the owner marks it done/dismissed.
struct Ask: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date
    var updated_at: Date
    var thread_id: String
    var thread_title: String?
    var run_id: String?
    var message_id: Int?
    var goal_id: String?
    var title: String
    var detail: String
    var kind: String
    /// Where this gets done: "mobile" (here), "web" (at the Mac), "any".
    /// Optional — an older hub does not send it, and then everything is "any".
    var surface: String?
    /// The session that raised this is running right now — on this or on
    /// anything else. Every card wears it, read asks included. Optional — an
    /// older hub does not send it. A card is answerable the moment it is
    /// raised, and the answer steers the live turn.
    var thread_running: Bool?
    /// The calendar item that raised this ask, and the day it was dated for.
    /// A dated step is a calendar entry that happens to need the owner, not a
    /// session checking in, so these cards group on their own and
    /// wear their date instead of the owning session's name and cost.
    /// Optional — an older hub does not send them; then they read as ordinary
    /// session asks, exactly as before.
    var cal_id: String?
    var cal_day: String?
    var state: String
    var check_hint: String
    /// The sentence this card's push SPOKE, word for word — the raising
    /// session's `--say`, or the one the hub built when it wrote none. Kept
    /// so the card that opens holds the words that were heard.
    /// Optional and absent on older cards, and on any card
    /// that never pushed; then nothing is drawn.
    var said: String?
    /// That sentence is queued and not yet spoken (the hub's "waiting to
    /// speak"): Play reads so, and a double tap drops the line, not the card.
    var waiting_to_speak: Bool?
    /// That sentence is being heard right now: Play reads "Speaking", and a
    /// double tap stops it.
    var speaking: Bool?
    /// The board's class for this card — the pill it wears (read, install,
    /// decision…). Optional: an older hub does not send it.
    var `class`: String?
    var resolved_at: Date?
    var resolved_by: String?
    var resolution: String?
    var superseded_by: String?
    /// The answer chips for this card, in order, first = default — the hub's
    /// vocabulary (store/close.go `AskOutcomes`), so a label change lands on
    /// both surfaces at once.
    var outcomes: [AskOutcome]?
    /// What the card wants from the owner in one word — "decide", "grant", "read",
    /// "restart"… (store.AskVerb); the card's caption.
    var verb: String?
    /// A session that hit its plan's session limit: when the hub resumes it on
    /// its own (verb "paused"). Optional — absent on every other card.
    var resumes_at: Date?

    /// More than the card's three-line preview can show.
    /// True when the detail just restates the title (prefix match on the
    /// first line, ignoring case and trailing punctuation).
    var detailEchoesTitle: Bool {
        func norm(_ s: String) -> String {
            s.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                .trimmingCharacters(in: CharacterSet(charactersIn: ".…!?:;"))
        }
        let head = norm(detailWithoutLinks.components(separatedBy: "\n").first ?? "")
        let t = norm(title)
        guard !head.isEmpty, !t.isEmpty else { return false }
        return head == t || head.hasPrefix(t) || t.hasPrefix(head)
    }

    var detailIsLong: Bool { detail.count > 160 || detail.filter { $0 == "\n" }.count >= 3 }

    /// Where the card stands, the hub's (store.AskStanding): `open`
    /// = the owner's move; `closed` = finished; neither = answered, waiting on
    /// its session. `folded` = drawn as one grey line where the card was:
    /// "dismissed", or "read" — a read marked Read; a read closed by a reply
    /// stays a full card. `lane` = mine | chores | homework | agents.
    var open: Bool?
    var closed: Bool?
    var folded: String?
    var lane: String?
    var reopen: Bool?
    /// An install card's device (2026-09-30): "phone" (the OTA link) or "mac"
    /// (the desktop app — its Install runs the hub's `mac` lane, no link).
    /// Older hubs do not send it: the title says ("Install desktop build N").
    var target: String?
    /// When it is owed (the items table, 2026-09-27): now | on | by | soon.
    var window: String?
    var isClosed: Bool { closed == true }
    /// The Mac's install card — `target`, or the title's word on an older hub.
    var isMacInstall: Bool { kind == "install" && (target == "mac" || Self.isMacInstallTitle(title)) }
    /// The OTHER device's install card — the phone's build seen from the Mac,
    /// the Mac's from the phone. Never drawn (the owner 2026-10-01, at the desktop
    /// app, of a phone build's card: "I shouldn't need to see this one because
    /// it does not actually apply to the desktop app itself… install cards
    /// should only appear in the tool that they're using"). The board already
    /// leaves it out (`surface=desktop|mobile`); this covers the chat and the
    /// calendar, which list every ask a session raised.
    var isOtherDeviceInstall: Bool { kind == "install" && buildNumber > 0 && isMacInstall != Device.isMac }
    static func isMacInstallTitle(_ t: String) -> Bool { t.range(of: #"^install (desktop|mac) build \d+"#, options: [.regularExpression, .caseInsensitive]) != nil }
    /// The card's build number ("… build 1512 …"), 0 when the title has none.
    var buildNumber: Int { Self.buildNumber(in: title) }
    static func buildNumber(in t: String) -> Int {
        guard let r = t.range(of: #"\bbuild (\d+)\b"#, options: [.regularExpression, .caseInsensitive]) else { return 0 }
        return Int(t[r].split(separator: " ").last ?? "") ?? 0
    }
    var isFolded: Bool { folded != nil }
    /// The white Read button's resolution — the hub's own word for a read
    /// card's one outcome (`outcomes[0]`, store.AskOutcomes("read")).
    var readWord: String { outcomes?.first?.label ?? "" }

    /// First https:// or itms-services:// link in the detail, if any — the
    /// board shows it as a button (OTA install asks carry one). Sessions write
    /// links bare AND in markdown — `[Install build 808](https://…)` — because
    /// the house rule is links-not-text, so the scheme is found anywhere in
    /// the text, not just at the start of a whitespace-split word. Earliest occurrence
    /// wins: an itms-services link embeds an https URL in its query string, so
    /// position, not scheme preference, must decide.
    var firstLink: URL? { Self.firstLink(in: detail) }
    static func firstLink(in detail: String) -> URL? {
        let starts = ["https://", "itms-services://"].compactMap { detail.range(of: $0)?.lowerBound }
        guard let start = starts.min() else { return nil }
        let raw = detail[start...].prefix { !$0.isWhitespace && !"()<>[],;\"'".contains($0) }
        return URL(string: String(raw))
    }

    /// The itms-services form of an install link found in `detail`: an
    /// itms-services link as is, an `/ota/…install.html` page rewritten to
    /// its manifest (same rewrite as `OTABuild.installURL`), anything else
    /// nil. Shared with the calendar's install row, which has
    /// only the entry's detail to go on.
    static func installLink(in detail: String) -> URL? {
        guard let u = firstLink(in: detail) else { return nil }
        if u.scheme == "itms-services" { return u }
        let s = u.absoluteString
        if s.contains("/ota/"), s.hasSuffix("install.html"),
           let enc = s.replacingOccurrences(of: "install.html", with: "manifest.plist")
               .addingPercentEncoding(withAllowedCharacters: .alphanumerics),
           let itms = URL(string: "itms-services://?action=download-manifest&url=\(enc)") {
            return itms
        }
        return nil
    }

    /// `detail` with the lines holding a link dropped — line-wise, not
    /// sentence-wise: ". "-chunks straddle newlines, so an older split dropped
    /// whole paragraphs that merely shared a chunk with the URL. A line inside
    /// a fenced block always stays: that is text to copy, and a copy box of
    /// links would otherwise arrive empty.
    static func detailWithoutLinks(_ detail: String) -> String {
        var fence = ""
        let kept = detail.components(separatedBy: "\n").filter { line in
            let t = line.trimmingCharacters(in: .whitespaces)
            if fence.isEmpty {
                if t.hasPrefix("```") { fence = String(t.prefix { $0 == "`" }); return true }
            } else {
                if t.hasPrefix(fence), t.allSatisfy({ $0 == "`" }) { fence = "" }
                return true
            }
            return !line.contains("https://") && !line.contains("itms-services://")
        }
        return kept.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// (A web ask — hub `surface=web` — is an ordinary card here; `surface`
    /// only says which link the phone will not draw as a button.)

    /// Raised by a dated calendar item rather than by a session mid-run. These
    /// group apart on the board so a reminder reads as the calendar entry it is.
    var fromCalendar: Bool { !(cal_id ?? "").isEmpty }

    /// The link this card may show as a button ON THE PHONE. An install link
    /// always (that IS the action); any other link only when the link is not
    /// a web-console route — the console has no phone layout, so a full-width
    /// "Open link" on that URL is clutter. Error cards never carry one: the URL
    /// in them belongs to the error text, not to anything to do.
    var phoneLink: URL? {
        guard let u = firstLink else { return nil }
        // An OTA install page is really an install action: serve the
        // itms-services form iOS understands directly (same rewrite as
        // OTABuild.installURL), so the card wears the Install button and
        // the tap runs the install/self-restart dance instead of a Safari hop.
        if let itms = Self.installLink(in: detail) { return itms }
        if kind == "error" { return nil }
        let s = u.absoluteString
        if s.contains(":8443/#/") || s.contains("/#/") { return nil }
        return u
    }

    /// Detail with raw URLs removed (the card shows them as a button instead),
    /// keeping the link-free sentences readable: "Link: <url> — or open <url>
    /// in Safari." collapses to nothing rather than a trail of dangling words.
    /// Only when the card actually replaces the link with a button: a URL the
    /// phone will not offer must stay in the text, or it is visible nowhere.
    /// The install card is the one open card that draws a link button (every
    /// other kind lost "Open link"), so a read card's links stay tappable text.
    var detailWithoutLinks: String {
        guard kind == "install", phoneLink != nil else { return detail }
        return Self.detailWithoutLinks(detail)
    }
}

// MARK: - Prompts (GET/POST /api/v1/prompts)

/// One thing said TO a session — the owner's response to a card, their typed
/// message, or an agent's note to itself for later. A reply can go to the same
/// session, a new one, or either at a specific time. The reference is data on
/// the row (`in_reply_to` + `outcome`), and the framing the agent reads is
/// rendered from the referenced card at wake time — never injected into or
/// parsed back out of the owner's words.
/// One card a prompt answers: `ref` is "<type>:<id>", `outcome` the pick
/// ("" = words alone). `POST /prompts` takes a list of them (`replies`) so one
/// message can close several cards at once.
struct PromptReply: Codable, Hashable {
    var ref: String
    var outcome: String?
    /// The verdict word the "↩ <label> · <card>" line prints (hub
    /// store.ReplyLabel, on a message's replies only; never posted).
    var label: String? = nil
}

struct Prompt: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date
    var author: String
    /// A session id, "new", or "new-or:<id>".
    var target: String
    /// When it may be delivered; nil = at once.
    var not_before: Date?
    /// "<type>:<id>" — ask, action, rec, cal or message.
    var in_reply_to: String?
    /// done | wont (about an ask), approved | denied (about a proposal).
    var outcome: String?
    var text: String
    var title: String?
    var goal_id: String?
    /// One clock: a cadence (`daily@09:00`, `weekly@Mon 09:00`,
    /// `every@1h`) makes this a STANDING row — the session's check-in itself.
    /// It is never delivered; each occurrence is a one-shot child.
    var `repeat`: String?
    /// The standing row a delivered child came from.
    var parent: String?
    var state: String
    var delivered_at: Date?
    var delivered_thread: String?
    var error: String?

    /// Still to come: the only kind that can be taken back.
    var pending: Bool { state == "queued" }
    var standing: Bool { !(`repeat` ?? "").isEmpty }
    /// "in 40m", "Thu 09:00", "tomorrow 09:00" — when this wakes the session.
    var whenLabel: String {
        guard let t = not_before else { return "now" }
        let secs = t.timeIntervalSinceNow
        if secs <= 0 { return "now" }
        if secs < 3600 { return "in \(max(1, Int(secs / 60)))m" }
        if Calendar.current.isDateInToday(t) { return t.formatted(.dateTime.hour().minute()) }
        if Calendar.current.isDateInTomorrow(t) { return "tomorrow " + t.formatted(.dateTime.hour().minute()) }
        // Past this week a weekday alone is ambiguous: a Sep 24 wake and an
        // Oct 1 wake both read "Thu 8:00 AM" in the same list, and the folded "Coming up" bar is nothing BUT that time.
        if secs < 6 * 86400 { return t.formatted(.dateTime.weekday(.abbreviated).hour().minute()) }
        return t.formatted(.dateTime.month(.abbreviated).day().hour().minute())
    }
    var startsNewSession: Bool { target == "new" || target.hasPrefix("new-or:") }
}

extension Prompt {
    /// The hub writes an unset `not_before` as Go's zero time (year 1): that
    /// is "at once", so it decodes as nil rather than a date two thousand
    /// years ago that sorts first and formats as a real day.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        created_at = try c.decode(Date.self, forKey: .created_at)
        author = try c.decodeIfPresent(String.self, forKey: .author) ?? ""
        target = try c.decodeIfPresent(String.self, forKey: .target) ?? ""
        let nb = try c.decodeIfPresent(Date.self, forKey: .not_before)
        not_before = nb.flatMap { Calendar(identifier: .gregorian).component(.year, from: $0) < 2000 ? nil : $0 }
        in_reply_to = try c.decodeIfPresent(String.self, forKey: .in_reply_to)
        outcome = try c.decodeIfPresent(String.self, forKey: .outcome)
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title)
        goal_id = try c.decodeIfPresent(String.self, forKey: .goal_id)
        `repeat` = try c.decodeIfPresent(String.self, forKey: .repeat)
        parent = try c.decodeIfPresent(String.self, forKey: .parent)
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        delivered_at = try c.decodeIfPresent(Date.self, forKey: .delivered_at)
        delivered_thread = try c.decodeIfPresent(String.self, forKey: .delivered_thread)
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}

// MARK: - Board (GET /api/v1/board?surface=mobile)

/// The hub's answer to "what needs the owner" for one surface, already ranked,
/// bundled and counted. The phone consumes it and never recomputes, so the
/// phone and the console can never print different numbers for the same pile.
struct Board: Codable, Hashable {
    var surface: String
    /// What the tab and the page header both print.
    var count: Int
    /// Counted items that ride a running session's Working row instead of
    /// being cards of their own.
    var working: Int
    // The hub also sends `calendar` (dated asks); the phone does not draw it
    // (the Calendar tab has its own read), so it is not decoded.
    /// Per listed session: every open card its chat draws, in the board's
    /// order — the cells a session row draws, one per card, the console's
    /// `sessionCards`. Optional so an older hub still decodes.
    var sessions: [BoardSession]?
    /// Per session: its cards ("N for you"). Nothing is held back for a
    /// running session — its cards are ordinary cards
    /// and answering one steers the turn in flight.
    var forYou: [String: Int]
    /// Per running session: how many of `forYou` are `read` asks — all of
    /// them means the row asks the owner only to read, drawn blue.
    /// Optional so an older hub still decodes.
    var reads: [String: Int]?
    /// Per session: how many of `forYou` are `install` asks — all of them
    /// means the row only asks the owner to install a build, drawn teal.
    /// Optional so an older hub still decodes.
    var installs: [String: Int]?
    /// Per session: the ask-or-action id its row opens on.
    var first: [String: String]
    /// Per session: the TITLE of that first card — the one line a session
    /// row prints under its name instead of the last reply.
    /// Optional so an older hub still decodes.
    var todo: [String: String]?
    /// Per session: the BODY of that first card, cut at 400 characters — the
    /// description under the todo line. Optional so an older hub still decodes.
    var detail: [String: String]?
    /// Per listed session: every open card its chat draws — `forYou` plus the
    /// owner's dated steps that sit in it. The row names one and says "and N
    /// more". Optional so an older hub still decodes.
    var open: [String: Int]?
    /// The Sessions page's groups in draw order, worded by the hub ("Your
    /// turn · 3 in 2 sessions · 1 working"); `section` files each session
    /// under one by key (absent = no heading) and `pills` are the capsules a card
    /// wears. The console draws the same three. Optional so an
    /// older hub still decodes.
    var headings: [BoardHeading]?
    var section: [String: String]?
    var pills: [String: [Pill]]?
    var badges: BoardBadges

    enum CodingKeys: String, CodingKey {
        case surface, count, working, sessions, reads, installs, first, todo, detail, open, headings, section, pills, badges
        case forYou = "for_you"
    }

    /// The heading a session sits under: the hub's, else running → Working
    /// (a session newer than this board). nil = under no heading — the hub
    /// lists only working or waiting sessions.
    func sectionOf(_ t: Thread) -> String? { section?[t.id] ?? (t.status == "running" ? "working" : nil) }
    /// The capsules a session card wears: the board's, else its own status pill.
    func pillsOf(_ t: Thread) -> [Pill] { pills?[t.id] ?? [t.pill ?? Pill(word: t.status, tone: "idle")] }
    /// The bundle of open cards behind one session's row, if it has any.
    func bundleOf(_ t: Thread) -> BoardSession? { sessions?.first { $0.id == t.id } }

    /// Before the first fetch: nothing needs the owner, nothing is working.
    static let empty = Board(surface: "mobile", count: 0, working: 0, sessions: [],
                             forYou: [:], reads: [:], installs: [:], first: [:], todo: [:], detail: [:], badges: BoardBadges(yourTurn: 0, calendar: 0, recs: 0))
}

/// One session's bundle on the board: what is open in its chat, in the order
/// the hub sorted it — approvals (the session is stopped on them), then its
/// asks, then the owner's own dated `steps` that sit in the chat (uncounted; a
/// row draws them after the session's own cards). `first` is the card
/// the row opens on.
struct BoardSession: Codable, Hashable {
    var id: String
    var title: String
    var n: Int
    var first: String
    var running: Bool
    var actions: [Action]
    var asks: [Ask]
    var steps: [Ask]?
}

/// One group on the Sessions page: `n` sessions under it, `show` of them
/// drawn (0 = all), `count` the words after the label.
struct BoardHeading: Codable, Hashable {
    var key: String
    var label: String
    var count: String
    var n: Int
    var show: Int
}

/// A capsule on a session card: the hub's word and tone (needs | read |
/// install | running | done | idle).
struct Pill: Codable, Hashable {
    var word: String
    var tone: String
}

/// One number per tab — the console's red ovals. Sessions carries none by
/// design; `recs` is a count, never a notification.
struct BoardBadges: Codable, Hashable {
    var yourTurn: Int
    var calendar: Int
    var recs: Int
    enum CodingKeys: String, CodingKey { case yourTurn = "your_turn", calendar, recs }
}

struct ThreadMessage: Codable, Identifiable, Hashable {
    var id: Int
    var thread_id: String
    var ts: Date
    var role: String
    var kind: String
    var text: String
    var cost_usd: Double
    var tokens: Int?           // this turn's tokens; 0 on turns older than the counter
    var tokens_in: Int?
    var tokens_out: Int?
    var tokens_cache_read: Int?
    var tokens_cache_write: Int?
    var attachments: [String]? // blob refs; nil on older hubs
    var run_id: String?        // links to ThreadEvents of the run this message started / produced
    var queued: Bool?          // not yet handed to claude (legacy one-shot run still in flight)
    var steered: Bool?         // handed to claude mid-turn; it took it into account while working
    /// What the owner's message answered — "ask:<id>" or "rec:<id>" — and the
    /// pick it carried (done/wont, approved/denied, accepted/declined/deferred,
    /// "" = words alone). The chat draws it as "↩ Accepted · <title>" over
    /// their note, whichever surface they decided on.
    var in_reply_to: String?
    var outcome: String?
    /// Every card the message answered when there was more than one (a read
    /// and a rec on one send); the first is the pair above.
    var replies: [PromptReply]?
    /// Who sent an owner/system row — "owner" | "hub" | "claude:thread:<id>" —
    /// and that session's title. Drawn as "sent by …" in the bubble's corner.
    var author: String?
    var author_title: String?
}

/// GET /api/v1/changes: `version` is opaque (send it back as `since`);
/// `changed` says whether it moved from the one sent.
struct ChangeFeed: Codable, Hashable {
    var version: String
    var changed: Bool
    /// The hub answered because it is shutting down: the pool of connections
    /// to it is about to go dead, so HubClient.changes drops it.
    var restarting: Bool?
}

/// One streamed step of a session run: a tool call, its result, a thinking
/// block or interim text. Shown under the message that started the run.
struct ThreadEvent: Codable, Identifiable, Hashable {
    var id: Int
    var thread_id: String
    var run_id: String
    var ts: Date
    var kind: String   // thinking | text | tool_use | tool_result
    var title: String
    var body: String
    var summary: String? // tool_use: the call in plain English (may arrive a few seconds late for shell commands)
}

/// How many steps sit under one message, counted by the HUB (GET
/// /threads/{id}/steps). The headline of a run block comes from here, so it
/// is right however many events the phone happens to hold — a turn regularly
/// runs to hundreds of tool calls and any surface that counts its own fetch
/// is one page size away from wrong. first_id/last_id bound
/// the block, so opening it is one ranged read.
struct StepCount: Codable, Identifiable, Hashable {
    var message_id: Int
    var run_id: String
    var tools: Int
    var thoughts: Int
    var steps: Int
    var first_id: Int
    var last_id: Int
    /// The block cut at the cards the agent raised while it ran — each piece
    /// with its own count, and the card drawn after it, so a card appears in
    /// the middle of the tool chain where it was raised. Absent/empty = one fold, as before.
    var segments: [StepSegment]?
    var id: Int { message_id }
}

/// One run of steps inside a block, and the card that closes it. `ref` is
/// "ask:<id>" or "action:<id>"; "" on the tail after the last card.
struct StepSegment: Codable, Hashable {
    var ref: String?
    var tools: Int
    var thoughts: Int
    var steps: Int
    var first_id: Int
    var last_id: Int
}

// MARK: - Calendar (plan layer)

/// One row of the merged agenda: a calendar item (owner/agent/note), a
/// session's projected check-in (run), a scheduler job, or an undated open
/// ask (anytime work).
struct CalEntry: Codable, Identifiable, Hashable {
    var id: String
    var day: String
    var at: String?
    var kind: String
    var title: String
    var detail: String?
    var state: String
    var goal_id: String?
    var thread_id: String?
    var ask_id: String?
    /// The ask's own kind when the row is an open ask or the record of one
    /// closing: an `install` ask is the same teal cell here as in
    /// the chat, with one Install button — never a generic Done/Dismiss row.
    var ask_kind: String?
    var `repeat`: String?
    var overdue: Bool?
    /// A to-do of the owner's with no due day ("do this soon"): `day`/`at`
    /// are the minute it was ADDED, it is never overdue, and it lists under
    /// `CalView.soon` until they close it with words.
    var soon: Bool?
    /// Its window: `on` = its day only, missed
    /// at midnight; `by` = owed until its day, then overdue.
    var due: String?
    /// No session waits on it: Did it / Skip, no words (hub `sessionless`).
    var tick: Bool?
    /// When the item behind the row is owed: now | on | by |
    /// soon. Absent on a row that is no item (a run, a job).
    var window: String?
    /// Who raised or decided the row: an item's `source`, `claude:thread:<id>`
    /// for an ask or check-in, `claude:job:<name>` for a job, `app`/`web` for
    /// a decided action.
    var actor: String?
    /// The typed pointer to the object behind the row — `cal:<id>` |
    /// `ask:<id>` | `action:<id>` | `rec:<id>` | `thread:<id>` | `job:<name>`.
    /// THE link target: never parse the id's prefix.
    var ref: String?
    /// The row is a RECORD of something that happened: a card the owner
    /// read, a decision, a grant, an approval, a rec accepted or scored, a step
    /// closed, an action the gate ran — placed at the minute it happened,
    /// closed by definition, in its doer's lane (`actor`). `verb` is the word
    /// for it ("Read", "Approved", "Accepted"…); `thread_title` names the
    /// session it was a step of, which is what a run of them collapses under.
    var did: Bool?
    var verb: String?
    var thread_title: String?
    /// What the row's session is doing RIGHT NOW: the board's own capsules —
    /// speaking / waiting to speak / running — so a step just answered says
    /// its session is on it instead of falling silent. Absent on a quiet session.
    var live: [Pill]?
    /// Stamped by the hub once for both surfaces (calendar.go
    /// `stampEntry`): the lane key (mine|chores|homework|scheduled|agents|recs),
    /// closed (the ✓/✕ look), a real cal item, how a drag moves it
    /// ("item"|"run"; absent = fixed, with `why`), and the kind's word.
    var lane: String?
    var open: Bool?
    var closed: Bool?
    /// The row's glyph (store.CalMark): "wont" (✕ + strike) |
    /// "done" (✓) | "todo" (○, an open row of the owner's) | nil (none).
    var mark: String?
    var item: Bool?
    var move: String?
    var why: String?
    var kind_label: String?
    /// The row's answers, stamped by the hub on an open row the owner can close
    /// (store/close.go): a tick's Did it · Skip · Send, a step's Done · Won't
    /// do · Reply, a proposal's Approve · Deny (· Reply). Absent = no buttons.
    var outcomes: [AskOutcome]?
    /// A LATER occurrence of a repeating item (2026-10-05), drawn on the day
    /// it will fall. No row exists for it yet, so `item` is absent and it is
    /// read-only: no buttons, no drag; `why` says so.
    var coming: Bool?

    var isItem: Bool { item == true }
    /// What the row is called on a chip, a block or an agenda line: a record
    /// reads as its deed — "Read: <title>".
    var label: String { verb.map { "\($0): \(title)" } ?? title }
    /// `ref` split at its first colon: ("rec", "rec-<id>").
    var refKind: String? { ref.flatMap { $0.split(separator: ":", maxSplits: 1).first.map(String.init) } }
    var refID: String? { ref.flatMap { r in r.firstIndex(of: ":").map { String(r[r.index(after: $0)...]) } } }
    /// Drawn as finished — the hub's `closed` (stampEntry).
    var isClosed: Bool { closed == true }
    /// Still the owner's to resolve (Done / Dismiss) — the hub's `open`, and NOT
    /// `!isClosed`: a proposed action or a deferred rec row is neither.
    var isOpen: Bool { open == true }
    /// An app build to install, or the record of one installed: its own kind
    /// of cell, and installing leaves a record behind. Same rule as
    /// `calIsInstall` in `views/calgrid.js`.
    var isInstall: Bool { ask_kind == "install" && (kind == "ask" || did == true) }
    /// The one-tap install link (itms-services form) an open install row's
    /// button carries — the same rewrite the chat's card does.
    var installLink: URL? { isInstall && isOpen ? Ask.installLink(in: detail ?? "") : nil }
    /// The desktop app's build, told by its title ("Install desktop build N",
    /// 2026-09-30): its Install runs the hub's `mac` lane instead of a link.
    var isMacInstall: Bool { isInstall && Ask.isMacInstallTitle(title) }
    /// The other device's build (see `Ask.isOtherDeviceInstall`): not this
    /// calendar's row, open or installed.
    var isOtherDeviceInstall: Bool { isInstall && Ask.buildNumber(in: title) > 0 && isMacInstall != Device.isMac }
    /// The detail without its link line: the button IS the link.
    var detailShown: String? { installLink != nil ? Ask.detailWithoutLinks(detail ?? "") : detail }
}

struct CalDay: Codable, Identifiable, Hashable {
    var day: String
    var entries: [CalEntry]
    var id: String { day }
}

struct CalView: Codable {
    var from: String
    var to: String
    var today: String
    var anytime: [CalEntry]
    var overdue: [CalEntry]
    /// The owner's open steps due today, or within a repeat's week before its day ("Due").
    var due: [CalEntry]?
    /// The owner's open to-dos with no due day, oldest first ("Do soon").
    var soon: [CalEntry]?
    var days: [CalDay]

    /// The same view without the other device's install rows (Anytime holds
    /// the open build, a day its installed record) — `Ask.isOtherDeviceInstall`.
    var forThisDevice: CalView {
        var v = self
        let keep = { (e: CalEntry) in !e.isOtherDeviceInstall }
        v.anytime = anytime.filter(keep); v.overdue = overdue.filter(keep)
        v.due = due?.filter(keep); v.soon = soon?.filter(keep)
        v.days = days.map { CalDay(day: $0.day, entries: $0.entries.filter(keep)) }
        return v
    }
}

/// Which app this is — the phone's or the Mac's (Catalyst) build of the same
/// code. The one fact a card about a build needs: a build is installed on the
/// device it is for, so its card appears there and nowhere else.
enum Device {
    static let isMac: Bool = {
        #if targetEnvironment(macCatalyst)
        true
        #else
        false
        #endif
    }()
}

struct CalItem: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date?
    var updated_at: Date?
    var title: String
    var detail: String
    var kind: String
    var day: String
    var at: String
    var `repeat`: String
    var goal_id: String?
    var thread_id: String?
    var source: String
    var state: String
    var nag_min: Int
    var nag_count: Int
    var last_nag_at: Date?
    var fired_at: Date?
    var ask_id: String?
    var ask_kind: String?
    var check_hint: String?
    /// Where the owner's step gets done (mobile | web | any) and the line its
    /// push speaks; nil on an agent's item.
    var surface: String?
    var say: String?
    /// on | by — a dated step of the owner's is owed on its day only, or by it
    /// (Overdue after). nil otherwise.
    var due: String?
    var prompt_id: String?
    var prev_id: String?
    var resolved_at: Date?
    var resolved_by: String?
    var resolution: String?
    /// Undated "do this soon" (no real deadline). Absent = false.
    var soon: Bool?
    /// now | on | by | soon (the items table).
    var window: String?
}

// GET /api/v1/sources — data-source inventory (the Configuration tab's cards).
struct SourceKindCount: Codable, Identifiable, Hashable {
    var kind: String
    var n: Int
    var first: Date
    var last: Date
    /// What ONE row is — `n` counts rows, not things.
    var note: String?
    var id: String { kind }
}

/// One thing a source is connected to — a channel, handle, repo, bank
/// account, statement folder. `via` is the lane (SimpleFin | Drive | API |
/// Google | phone); finance rows carry both a live lane and an export lane.
struct SourceAccount: Codable, Identifiable, Hashable {
    var label: String
    var via: String?
    var detail: String?
    var url: String?
    var n: Int?
    var last: Date?
    var id: String { (via ?? "") + "/" + label }
}

struct SourceEntry: Codable, Identifiable, Hashable {
    var id: String
    var title: String
    var from: String
    var storage: String
    var status: String
    var last: Date?
    /// Whether the connection WORKS, which `status` and `last` cannot say:
    /// a credential can be present and every call still refused, and `last`
    /// is the newest row of any kind — including rows the hub writes without
    /// asking the upstream anything, so a source can read "connected" while
    /// every call fails.
    var error: String?
    var lastOK: Date?
    var failingSince: Date?
    var fails: Int = 0
    var total: Int
    /// The folded row's few words for `accounts` ("21 repos", "@handle").
    var summary: String?
    var kinds: [SourceKindCount]
    var accounts: [SourceAccount] = []
    /// The provider's tile on the Configuration card.
    var brand: BrandMark?

    /// What the folded row says it is connected to.
    var shortTo: String { summary ?? accounts.first?.label ?? "" }

    // A `= []` default is NOT applied by Swift's synthesized decoder, and the
    // hub sends `"accounts": null` for a source with none (no omitempty), so
    // both the null and a cached payload from before the field existed would
    // throw and blank the whole Configuration page. Decode it leniently.
    /// Spelled out because three keys are snake_case on the wire and there is no
    /// key strategy on the decoder (HubClient.decoder reads keys verbatim).
    enum CodingKeys: String, CodingKey {
        case id, title, from, storage, status, last, error, total, summary, kinds, accounts, brand
        case lastOK = "last_ok"
        case failingSince = "failing_since"
        case fails
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        from = try c.decode(String.self, forKey: .from)
        storage = try c.decode(String.self, forKey: .storage)
        status = try c.decode(String.self, forKey: .status)
        last = try c.decodeIfPresent(Date.self, forKey: .last)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        lastOK = try c.decodeIfPresent(Date.self, forKey: .lastOK)
        failingSince = try c.decodeIfPresent(Date.self, forKey: .failingSince)
        fails = try c.decodeIfPresent(Int.self, forKey: .fails) ?? 0
        total = try c.decode(Int.self, forKey: .total)
        summary = try c.decodeIfPresent(String.self, forKey: .summary)
        kinds = try c.decodeIfPresent([SourceKindCount].self, forKey: .kinds) ?? []
        accounts = try c.decodeIfPresent([SourceAccount].self, forKey: .accounts) ?? []
        brand = try c.decodeIfPresent(BrandMark.self, forKey: .brand)
    }
}

struct SourceGroup: Codable, Identifiable, Hashable {
    var id: String
    var title: String
    var blurb: String
    var note: String?
    /// The section the group's cards sit under on the Configuration page,
    /// and its colour — groups sharing a tag share a section.
    var tag: String?
    var color: String?
    var sources: [SourceEntry]
}

struct SourcesInventory: Codable {
    var groups: [SourceGroup]
}

// MARK: - Recommendations (GET /api/v1/recs, /recs/stats)
//
// The pull half of the system: an ask notifies and closes when the deed is
// done; a rec notifies NOBODY and closes twice — `status` is what the owner
// decided, `outcome` is whether it worked (docs/design/recommendations.md).
// Nothing here may ever drive a badge, a red dot or a push.
struct Rec: Codable, Identifiable, Hashable {
    var id: String
    var created_at: Date
    var updated_at: Date
    var title: String
    var detail: String?
    var goal_id: String?
    var thread_id: String?
    var source: String
    var domain: String
    var kind: String
    var cost_cents: Int
    var cost_period: String?
    var effort: String
    var confidence: Int
    /// The evidence that produced it.
    var because: String?
    /// What should change, and how we would know — what the score judges.
    var expect: String?
    /// YYYY-MM-DD after which it is stale (the hub sweeps it to `expired`).
    var act_by: String?
    /// YYYY-MM-DD it is due to be scored.
    var review_on: String?
    var status: String
    var decided_at: Date?
    var decided_by: String?
    var decision_note: String?
    var outcome: String?
    var outcome_at: Date?
    var outcome_note: String?
    var prev_id: String?
    /// Comma list of the cal-/ask-/act- ids accepting it minted.
    var links: String?
    /// The model id of the session that filed it (`claude-opus-5`); nil when
    /// the owner filed it. Required on the hub for session-filed recs.
    var model: String?
    /// Only on `GET /recs?thread=`: the reply that filed it (the first of the
    /// session's replies after the rec), where its cell is drawn in the chat.
    /// nil = the reply is still being written; the cell sits at the end.
    var message_id: Int?
    /// The session that filed it has a turn in flight right now. Stamped by
    /// the hub on every read, never stored; the row shows "running" so the
    /// owner knows not to follow up on it.
    var thread_running: Bool?
    /// The cell's word buttons, the hub's words (Accept · Decline
    /// · Reply). Optional: an older hub does not send it.
    var outcomes: [AskOutcome]?
    /// Where it stands, the hub's (store.RecStanding): `open` = proposed (the
    /// owner's move), `closed` = decided/expired; a deferred rec is neither.
    /// `folded` "dismissed" = expired from its cell by the owner, Reopen inside.
    var open: Bool?
    var closed: Bool?
    var folded: String?
    var lane: String?
    var reopen: Bool?
    /// When it is owed (the items table, 2026-09-27): always `soon` — a rec
    /// is pulled, never pushed.
    var window: String?
    /// nil for no model; otherwise the one `shortModel`.
    var modelShort: String? { model.flatMap { $0.isEmpty ? nil : shortModel($0) } }

    /// The session that filed it — `thread_id`, or for older rows the id
    /// inside `source` (`claude:thread:<id>`); nil when the owner filed it.
    var sourceThreadID: String? {
        if let t = thread_id, !t.isEmpty { return t }
        let p = "claude:thread:"
        return source.hasPrefix(p) ? String(source.dropFirst(p.count)) : nil
    }

    var isOpen: Bool { open == true }
    /// Not still the owner's to answer. Open or parked until a named day (the
    /// day can move, or the owner can decide early) are both not closed.
    var isClosed: Bool { closed == true }
    /// The hub's words, the console's too (recs/labels.go):
    /// "$11/mo", "$33,837 one-off", "free", "price not checked".
    var cost_label: String?
    /// Eastern days until act_by (negative once past); nil with no act_by.
    var days_left: Int?
    /// "act by Jan 15 (12d) · review Oct 1".
    var dates_label: String?
    var costLabel: String { cost_label ?? "" }
}

struct RecsPage: Codable { var recs: [Rec] }

// The track record (`RecStats`, GET /recs/stats) is not in the app;
// `lifectl recs stats` is where those numbers live.

/// What an agent is told when the owner decides on a rec (GET /recs/{id}/starter).
/// `relay` is the message the session that FILED it gets — short, because it
/// already knows the argument; `context` is the block a NEW session gets under
/// `opener`, the default first line the owner's own note replaces (the first line is
/// the session's title and the board's preview). Composed by the hub so both
/// surfaces show and send the same thing.
struct RecStarter: Codable {
    var rec_id: String
    var goal_id: String?
    var opener: String
    var context: String
    var relay: String
    /// The session that filed it, empty if it is gone or the owner filed the rec.
    var source_session: String
}

/// The answer to a decision: the rec as it now stands, plus where the note
/// went. `session_id` is the session to open — `delivered` is "new" even when
/// the owner asked for its own session and that one was gone — and `delivery_error`
/// is set only when nobody got it.
struct RecDecision: Decodable, Sendable {
    var rec: Rec
    var session_id: String?
    var delivered: String?
    var delivery_error: String?

    private enum K: String, CodingKey { case session_id, delivered, delivery_error }
    init(from decoder: Decoder) throws {
        rec = try Rec(from: decoder)
        let c = try decoder.container(keyedBy: K.self)
        session_id = try c.decodeIfPresent(String.self, forKey: .session_id)
        delivered = try c.decodeIfPresent(String.self, forKey: .delivered)
        delivery_error = try c.decodeIfPresent(String.self, forKey: .delivery_error)
    }
}
