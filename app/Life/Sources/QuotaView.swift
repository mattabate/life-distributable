import SwiftUI

// The Spend tab's top half answers one question: when do I get locked out?
// So it shows only meters that can actually lock the owner out — the shared
// 5-hour and 7-day ones plus every per-model limit Anthropic reports — each a
// bar, a percentage and one short word about where it lands by reset; the pace
// behind that word lives in the card's chat facts, not on screen.
// Spend per model is a separate section below; it is history, not a limit
// (per-model sub-bars inside the 5h/7d cards would read as limits).

/// `GET/PUT /api/v1/spend/model` — the toggle inside the bar.
/// It lives here rather than in Models.swift because it is only ever read by
/// this card.
struct ModelSetting: Codable, Sendable {
    var default_model: String = ""
    var explicit: Bool = false
    var starts_on: String = ""
    var reason: String = ""
    var options: [String] = []
    var rungs: [ModelRung] = []
}

/// One step of the ladder: open, or shut by `why` (hub modelRung).
struct ModelRung: Codable, Sendable, Hashable {
    var model: String
    var open: Bool
    var why: String = ""

    init(model: String, open: Bool, why: String = "") {
        self.model = model; self.open = open; self.why = why
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        model = try c.decode(String.self, forKey: .model)
        open = try c.decodeIfPresent(Bool.self, forKey: .open) ?? true
        why = try c.decodeIfPresent(String.self, forKey: .why) ?? ""
    }
}

extension ModelSetting {
    /// Defaults above only cover the memberwise init; synthesized decoding
    /// still demanded every key, so a hub that omits one empty field failed
    /// the whole card.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        default_model = try c.decodeIfPresent(String.self, forKey: .default_model) ?? ""
        explicit = try c.decodeIfPresent(Bool.self, forKey: .explicit) ?? false
        starts_on = try c.decodeIfPresent(String.self, forKey: .starts_on) ?? ""
        reason = try c.decodeIfPresent(String.self, forKey: .reason) ?? ""
        options = try c.decodeIfPresent([String].self, forKey: .options) ?? []
        rungs = try c.decodeIfPresent([ModelRung].self, forKey: .rungs) ?? []
    }
}

/// Which rung of the model ladder a new session gets right now, and why it
/// is not the top one: "am I stepped down?" must be on screen. The bar is
/// also where the owner sets it, pinned into the next NEW session and never
/// into a running one, so a chat keeps its prompt cache.
struct NextModelCard: View {
    let model: String?
    let reason: String?
    /// nil while the setting is loading — the card then reads as it always did.
    var setting: ModelSetting?
    var choose: ((String) -> Void)?

    /// What the bar names: the rung new sessions start on right now (the
    /// console's `runs`, views/money.js spendModelBar).
    private var shown: String {
        if let s = setting, !s.starts_on.isEmpty { return s.starts_on }
        if let s = setting, !s.default_model.isEmpty { return s.default_model }
        return model ?? ""
    }

    /// The whole ladder, top first: every model in rank, which one is used
    /// and which ones are blocked.
    private var rungs: [ModelRung] {
        guard let s = setting else { return [] }
        return s.rungs.isEmpty ? s.options.map { ModelRung(model: $0, open: true) } : s.rungs
    }

    var body: some View {
        if !shown.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("New sessions run on \(shortModel(shown))")
                    .font(.subheadline.weight(.semibold))
                if !rungs.isEmpty {
                    // The rung in use is filled, a shut one is struck through
                    // and cannot be picked, "auto" hands it back to the ladder.
                    HStack(spacing: 6) {
                        ForEach(Array(rungs.enumerated()), id: \.element) { i, r in
                            if i > 0 { Text("›").foregroundStyle(.secondary) }
                            chip(shortModel(r.model), on: r.model == shown, open: r.open) { choose?(r.model) }
                        }
                        Text("·").foregroundStyle(.secondary)
                        chip("auto", on: setting?.explicit != true, open: true) { choose?("") }
                    }
                }
                Text(subtitle).font(.caption).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
            .chatAbout(ChatSubject(
                screen: "Spend",
                title: "New sessions run on \(shortModel(shown))",
                facts: "- new sessions are pinned to: \(setting?.default_model ?? "?")\(setting?.explicit == true ? " (your choice)" : " (not set — the ladder's pick)")\n- the picker would hand one out right now: \(shown)\n- the ladder: \(rungs.map { "\($0.model) \($0.open ? "open" : "shut — \($0.why)")" }.joined(separator: "; "))\n- a session keeps the model it started on for its whole life, so the prompt cache survives",
                code: "app/Life/Sources/QuotaView.swift (NextModelCard) ← hub GET/PUT /api/v1/spend/model, ladder in hub/spend"))
        }
    }

    private func chip(_ label: String, on: Bool, open: Bool, tap: @escaping () -> Void) -> some View {
        Button(action: tap) {
            Text(label)
                .font(.caption.weight(on ? .semibold : .regular))
                .strikethrough(!open)
                .padding(.horizontal, 10).padding(.vertical, 5)
                .foregroundStyle(on ? Color.white : .primary)
                .background(on ? Color.accentColor : Color(.tertiarySystemBackground), in: Capsule())
                .opacity(open ? 1 : 0.5)
        }
        .buttonStyle(.plain)
        .disabled(!open || choose == nil)
    }

    /// The console's words: "auto" or "pinned to X", then why each shut rung
    /// is shut.
    private var subtitle: String {
        let pin = setting?.explicit == true ? "pinned to \(shortModel(setting?.default_model ?? ""))" : "auto"
        let shut = rungs.filter { !$0.open }.map { "\(shortModel($0.model)): \($0.why.isEmpty ? "closed" : $0.why)" }
        if setting == nil, let reason, !reason.isEmpty { return "auto · " + reason }
        return ([pin] + shut).joined(separator: " · ")
    }
}

/// One plan limit: the bar, what it gates, and when it is projected to fill.
struct QuotaWindowCard: View {
    let w: QuotaWindow
    /// Families whose own meter is already full, so the shared meters' headroom
    /// is not actually spendable on them.
    var lockedFamilies: [String] = []

    var frac: Double { min(max(w.utilization / 100, 0), 1) }
    var locked: Bool { w.utilization >= 100 }
    /// The hub's tone, the same one the console's meter wears.
    var tint: Color { w.tone == "bad" ? .red : w.tone == "warn" ? .orange : .accentColor }
    /// The words wear the tone only when there is one (the console's percent
    /// is plain text otherwise); the meter's fill keeps the accent.
    var toned: Bool { w.tone == "bad" || w.tone == "warn" }
    /// `Math.round(w.utilization)`, safe from a missing reading.
    var used: String { "\(Int(jsRound(w.utilization)))%" }

    /// The label already says whose spend fills the bar ("All models · 7 days",
    /// "Fable · 7 days"); the hub's note stays in the chat facts, not on screen.
    var gates: String { w.note ?? "" }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(w.label).font(.subheadline.weight(.semibold))
                Spacer()
                VStack(alignment: .trailing, spacing: 1) {
                    Text(used)
                        .font(.title3.weight(.semibold)).monospacedDigit()
                        .foregroundStyle(toned ? tint : .primary)
                    if let o = w.outlook, !o.isEmpty {
                        Text(o).font(.caption2).foregroundStyle(toned ? tint : .secondary)
                    }
                }
            }
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.secondary.opacity(0.15))
                    Capsule().fill(tint).frame(width: g.size.width * frac)
                    // Where the window's clock stands: fill short of the tick
                    // is under pace for the week, past it is over.
                    if w.elapsedPct > 0 {
                        Rectangle().fill(Color.primary.opacity(0.55))
                            .frame(width: 2, height: 10)
                            .offset(x: g.size.width * min(w.elapsedPct / 100, 1) - 1)
                    }
                }
            }.frame(height: 10)
            if let f = w.foot, !f.isEmpty {
                Text(f).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
        // The session is handed every number behind the bar, not just the
        // sentence on screen, so it can explain how the pace is computed.
        .chatAbout(ChatSubject(screen: "Spend", title: "\(w.label) limit", facts: facts,
                               code: "app/Life/Sources/QuotaView.swift (QuotaWindowCard) ← hub GET /api/v1/spend/quota; pace = burn_pct_per_hour, full_at"))
    }

    /// Everything the bar is drawn from, in the units the hub reports.
    var facts: String {
        var f = ["- \(w.label): **\(used) used** — \(gates)",
                 "- window key `\(w.key)`" + (w.scope_model.map { ", scoped to \($0)" } ?? ", shared by every model")]
        f.append("- \(usd(w.spent_usd)) spent locally in it, \(w.messages) messages, ≈\(usd(w.headroom_usd)) headroom left")
        if w.measuredHours >= 0.3 {
            f.append("- MEASURED: Anthropic's own % moved \(w.measuredDelta >= 0 ? "+" : "")\(pctShort(w.measuredDelta)) over the last \(span(w.measuredHours)) (stored readings, not an estimate)")
        } else {
            f.append("- MEASURED: not enough stored readings yet for this window (hub keeps one every 10 min)")
        }
        if w.typical > 0 { f.append("- pace over the last day: \(pctShort(w.typical))/hr (typical_pct_per_hour — trailing 24h of local spend at this meter's own $→% rate)") }
        else { f.append("- pace over the last day: nothing that can still run has spent against it") }
        if w.burn > 0 { f.append("- pace right now: \(pctShort(w.burn))/hr (burn_pct_per_hour — last 45 min only; goes to 0 during any lull)") }
        else { f.append("- pace right now: nothing running against it this minute") }
        if let r = w.resets { f.append("- resets \(when(r)) — \(Int(jsRound(w.elapsedPct)))% of the window's clock has run") }
        if let fill = w.fillsTypical { f.append("- projected full \(when(fill)) at the last day's pace") }
        if let o = w.outlook { f.append("- outlook: \(o)") }
        if locked { f.append("- LOCKED: this meter is at 100%") }
        if !lockedFamilies.isEmpty { f.append("- locked families right now: \(lockedFamilies.joined(separator: ", "))") }
        return f.joined(separator: "\n")
    }

    func when(_ d: Date) -> String { d.formatted(.dateTime.weekday(.abbreviated).hour().minute().locale(Fmt.enUS)) }

    /// "4 h", "35 min" — the span the measured delta covers.
    func span(_ hours: Double) -> String {
        hours < 1.5 ? "\(Int(jsRound(hours * 60))) min"
                    : (hours < 10 ? fixed(hours, 1) + " h" : "\(Int(jsRound(hours))) h")
    }
}
