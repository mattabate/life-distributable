// Every number, date and name the phone prints, in one file:
// money, token counts, "ago", model names, day strings. Each is word for word
// the console's helper of the same job (hub/internal/server/web/ui.js), so the
// two surfaces print the same thing, and every formatter here is built ONCE —
// DateFormatter and friends cost real time to create, and a view body runs
// many times a second while a list scrolls.
import Foundation

enum Fmt {
    /// Hub day strings ("2026-08-22", local calendar day). POSIX-pinned: the
    /// four per-file copies that used to exist drifted.
    static let localDay: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        f.timeZone = .current
        return f
    }()
    /// Eastern day / clock — what `POST /prompts` `on` + `at_time` take.
    static let easternDay: DateFormatter = eastern("yyyy-MM-dd")
    static let easternClock: DateFormatter = eastern("HH:mm")
    private static func eastern(_ format: String) -> DateFormatter {
        let d = DateFormatter()
        d.locale = Locale(identifier: "en_US_POSIX")
        d.timeZone = TimeZone(identifier: "America/New_York")
        d.dateFormat = format
        return d
    }
    /// RFC3339 with fractional seconds, for what the phone writes. A value
    /// type, so safe from any thread.
    static let iso = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
    static func isoString(_ d: Date) -> String { d.formatted(iso) }
    /// "312 KB" for an attached file's size.
    static func bytes(_ n: Int) -> String { ByteCountFormatter.string(fromByteCount: Int64(n), countStyle: .file) }
    /// The console formats every number and date as en-US; so does the phone,
    /// whatever the device's region is (a German locale would print "1.234").
    /// Pass it to any `.formatted(…)` date style: `.dateTime…locale(Fmt.enUS)`.
    static let enUS = Locale(identifier: "en_US")
    /// The console's date words, device-local day, en_US words.
    static let dayLabel = local("MMM d")
    static let shortDay = local("MMM d, y")
    static let fullDay = local("EEE, MMM d, y")
    static let numericDay = local("M/d/y")
    /// "Sep 25" for September 2025 — a month column's title.
    static let monthLabel = local("MMM yy")
    /// "3:15 PM" / "Mon": the two halves of `dayClock`.
    static let clock = local("h:mm a")
    static let weekday = local("EEE")
    private static func local(_ format: String) -> DateFormatter {
        let d = DateFormatter()
        d.locale = Locale(identifier: "en_US_POSIX")
        d.timeZone = .current
        d.dateFormat = format
        return d
    }
}

func parseDay(_ s: String) -> Date? { Fmt.localDay.date(from: s) }
func dayString(_ d: Date) -> String { Fmt.localDay.string(from: d) }

/// "Sep 8, 2025" from a hub day string — the console's `shortDay`, year
/// included; the string itself when it is not one. Inside a chart or a chip,
/// where the year is noise, use `dayLabel`.
func shortDay(_ s: String) -> String {
    guard let d = parseDay(s) else { return s }
    return shortDay(d)
}
func shortDay(_ d: Date) -> String { Fmt.shortDay.string(from: d) }

/// "Sep 8" — the console's `dayLabel`: a day inside a chart or a chip.
func dayLabel(_ s: String) -> String {
    guard let d = parseDay(s) else { return s }
    return dayLabel(d)
}
func dayLabel(_ d: Date) -> String { Fmt.dayLabel.string(from: d) }

/// "Mon, Sep 8, 2025" — the console's `fullDay`, the title of a plot tooltip.
func fullDay(_ s: String) -> String {
    guard let d = parseDay(s) else { return s }
    return fullDay(d)
}
func fullDay(_ d: Date) -> String { Fmt.fullDay.string(from: d) }

// MARK: - rounding the console's way

/// JavaScript's `Math.round`: halves go UP, so −2.5 → −2 and 2.5 → 3 (Swift's
/// `.rounded()` sends −2.5 to −3). Not a number is 0, as `Number(n) || 0` is.
func jsRound(_ x: Double) -> Double {
    guard x.isFinite else { return 0 }
    let r = x.rounded(.down)
    return x - r >= 0.5 ? r + 1 : r
}

/// JavaScript's `toFixed`: a tie goes to the larger digit (0.125 → "0.13",
/// 12.5 → "13"), where `String(format:)` rounds a tie to even ("0.12"). A tie
/// is judged on the double itself, as JS does — 1.005 is really 1.00499…, so
/// it is "1.00" on both surfaces. Negatives keep an ASCII "-", as JS prints.
func fixed(_ x: Double, _ digits: Int) -> String {
    let d = max(0, digits)
    guard x.isFinite else { return d > 0 ? "0." + String(repeating: "0", count: d) : "0" }
    let a = abs(x), p = pow(10, Double(d)), scaled = a * p
    var v = a
    // Scaled lands on exactly n.5, and the multiply was exact (fused residue 0).
    if scaled - scaled.rounded(.down) == 0.5, (-scaled).addingProduct(a, p) == 0 {
        v = scaled.rounded(.toNearestOrAwayFromZero) / p
    }
    return (x < 0 ? "-" : "") + String(format: "%.\(d)f", v)
}

/// Whole-number grouping for a double that may be huge or not a number.
private func wholeCommas(_ a: Double) -> String {
    commas(Int(Swift.min(Swift.max(jsRound(a), -9e18), 9e18)))
}

// MARK: - money

// One wording with the hub (internal/format) and the console (ui.js), pinned
// by shared/format-cases.json, which FormatTests reads.

private func moneySign(_ v: Double, _ digits: String) -> String {
    (v < 0 && digits.contains(where: { ("1"..."9").contains($0) }) ? "−$" : "$") + digits
}

/// "$1,234" from $100 up, "$12.34" below, "$15" for a whole amount; negatives
/// as "−$12.34" (never "$-12.34"). Not a number, or infinite, is "$0".
func usd(_ x: Double) -> String {
    let v = x.isFinite ? x : 0
    let a = abs(v), cents = jsRound(a * 100)
    let whole = cents >= 10_000 || cents.truncatingRemainder(dividingBy: 100) == 0
    return moneySign(v, whole ? wholeCommas(a) : fixed(cents / 100, 2))
}

/// Whole dollars, always: "$12", "$1,234" — the console's `usd0`.
func usd0(_ x: Double) -> String {
    let v = x.isFinite ? x : 0
    return moneySign(v, wholeCommas(abs(v)))
}

/// "22,635", "−1,204" — grouping by hand, so it never follows a device locale.
func commas(_ n: Int) -> String {
    let s = String(n.magnitude)
    var out = ""
    for (i, c) in s.enumerated() {
        if i > 0 && (s.count - i) % 3 == 0 { out.append(",") }
        out.append(c)
    }
    return (n < 0 ? "−" : "") + out
}

/// "+$1,234" / "−$1,234" — an explicit sign, for a gain or a flow. A function
/// and not an inline `(v >= 0 ? "+" : "−") + usd(abs(v))`, because that
/// expression inside a ViewBuilder is what blew the Swift type-checker's
/// budget and failed the app build.
func usdSigned(_ v: Double) -> String { signed(usd(v)) }

/// "+$4,222" / "−$350" — whole dollars with an explicit sign.
func usd0Signed(_ v: Double) -> String { signed(usd0(v)) }

private func signed(_ s: String) -> String { s.hasPrefix("−") ? s : "+" + s }

/// A whole count with its grouping: "20,125", "−3" — the console's `num`
/// (`Math.round`, so −2.5 is "−2" and −0.4 is "0").
func num(_ n: Double) -> String {
    let v = jsRound(n)
    return (v < 0 ? "−" : "") + wholeCommas(abs(v))
}

/// A count with its sign: "+12", "−3", "0" — the console's `signed`.
func signed(_ n: Double) -> String { (jsRound(n) > 0 ? "+" : "") + num(n) }

/// "−$389" for $389 of spend, "$0" for none — money out reads like money out
/// the console's `spent`.
func spent(_ v: Double) -> String { usd0(-v) }

/// A per-share price or a fill, which is not a balance: always cents
/// ("$432.10"), and four decimals under a cent — a
/// $0.0015 price would render "$0.00" with two.
func usdPrice(_ x: Double) -> String {
    let v = x.isFinite ? x : 0, a = abs(v)
    let parts = fixed(a, a > 0 && a < 0.01 ? 4 : 2).split(separator: ".")
    return moneySign(v, commas(Int(parts[0]) ?? 0) + "." + (parts.count > 1 ? parts[1] : "00"))
}

/// Chart axis money: "$950", "$1.2k", "$12k", "$1.4M". Ties round up before
/// printing, as the hub and console do ("%.1f" alone rounds to even).
func usdShort(_ x: Double) -> String {
    let v = x.isFinite ? x : 0, a = abs(v)
    func tenths(_ x: Double) -> String {
        let s = fixed(jsRound(x * 10) / 10, 1)
        return s.hasSuffix(".0") ? String(s.dropLast(2)) : s
    }
    let s: String
    if jsRound(a / 1e5) >= 10 { s = tenths(a / 1e6) + "M" }
    else if jsRound(a / 1e3) >= 10 { s = wholeCommas(a / 1e3) + "k" }
    else if jsRound(a) >= 1000 { s = tenths(a / 1e3) + "k" }
    else { s = String(Int(jsRound(a))) }
    return moneySign(v, s)
}

// MARK: - percents and counts

/// Axis label for a percent series, the console's `axisPct` (plot.js): "0%",
/// "5.0%", "13%" (12.5 rounds up), "−12%".
func axisPct(_ x: Double) -> String {
    let v = x.isFinite ? x : 0
    if v == 0 { return "0%" }
    return (v < 0 ? "−" : "") + fixed(abs(v), abs(v) < 10 ? 1 : 0) + "%"
}

/// The old name of `axisPct`; the money views' axes call it.
func pctShort(_ v: Double) -> String { axisPct(v) }

/// A fraction as a whole percent: 0.237 → "24%".
func pctOf(_ fraction: Double) -> String { "\(Int(jsRound(fraction * 100)))%" }

/// A percent already in percent units, whole: 23.7 → "24%".
func pctWhole(_ v: Double) -> String { "\(Int(jsRound(v)))%" }

/// Percent points with a sign, the console's `pctPoints`: "+2.5%", "−0.3%".
func pctPoints(_ x: Double, _ digits: Int = 1) -> String {
    let v = x.isFinite ? x : 0
    return (v < 0 ? "−" : "+") + fixed(abs(v), digits) + "%"
}

/// Signed, one decimal: "+2.5%", "−0.3%" — `pctPoints(v, 1)`.
func pctSigned1(_ v: Double) -> String { pctPoints(v, 1) }

/// A fraction as a signed percent, the console's `pct`: 0.025 → "+2.5%".
func pct(_ fraction: Double, _ digits: Int = 1) -> String {
    let v = fraction.isFinite ? fraction : 0
    return (v >= 0 ? "+" : "−") + fixed(abs(v * 100), digits) + "%"
}

/// A change as a percent of where it started, the console's `pctDelta`: one
/// decimal under 10% ("+4.2%"), whole at or over ("+12%"); "" with no base.
func pctDelta(_ d: Double, _ base: Double, _ digits: Int? = nil) -> String {
    guard base != 0, base.isFinite, d.isFinite else { return "" }
    let p = d / abs(base) * 100
    return pctPoints(p, digits ?? (abs(p) < 10 ? 1 : 0))
}

/// "13%" of a whole, the console's `ratioPct`; "" when the whole is not
/// positive. A negative prints JS's ASCII "-12%".
func ratioPct(_ n: Double, _ d: Double) -> String {
    guard d > 0, n.isFinite, d.isFinite else { return "" }
    return "\(Int(jsRound(n / d * 100)))%"
}

/// A big count on a tile: "740", "1.2k", "12k".
func compactCount(_ n: Int) -> String {
    n >= 10_000 ? "\(n / 1000)k" : n >= 1_000 ? fixed(Double(n) / 1000, 1) + "k" : commas(n)
}

/// What a chat has cost in tokens: "8.4M", "1.3M" (1,250,000), "312k", "740" —
/// the console's `tokens` without its " tok" (callers here add it; or call
/// `tokens`).
func tokenCount(_ n: Int) -> String {
    if n >= 10_000_000 { return fixed(Double(n) / 1_000_000, 0) + "M" }
    if n >= 1_000_000 { return fixed(Double(n) / 1_000_000, 1) + "M" }
    if n >= 1_000 { return "\(Int(jsRound(Double(n) / 1000)))k" }
    return "\(n)"
}

/// "1.3M tok", "312k tok", "740 tok" — the console's `tokens`, word and all.
func tokens(_ n: Int) -> String { tokenCount(n) + " tok" }

// MARK: - time and names

/// THE "ago" on the phone, word for word the console's `ago` (ui.js): "just
/// now", "2m ago", "3h ago", "4d ago", then the date past 30 days. "ago" is
/// part of the answer — callers never append it. Rounds like the console; a
/// unit only carries the bucket below it, so 59.7 minutes is "1h ago".
func shortAgo(_ d: Date, now: Date = Date()) -> String {
    let s = now.timeIntervalSince(d)
    if s < 45 { return "just now" }
    if s < 3600 - 30 { return "\(Int((s / 60).rounded()))m ago" }
    if s < 86400 - 1800 { return "\(Int((s / 3600).rounded()))h ago" }
    if s < 86400 * 30 { return "\(Int((s / 86400).rounded()))d ago" }
    return Fmt.numericDay.string(from: d)
}

/// A chat row's clock with its day said in words, the console's `dayWhen`
/// (ui.js) word for word: a bare time reads wrong a day later, so the row
/// says "today 3:15 PM", "yesterday 12:53 AM", "Mon 9:15 AM" inside the
/// week, "Oct 1 4:02 PM" past it, the year once it differs.
func dayClock(_ d: Date, now: Date = Date()) -> String {
    let clock = Fmt.clock.string(from: d)
    let cal = Calendar.current
    let days = cal.dateComponents([.day], from: cal.startOfDay(for: d), to: cal.startOfDay(for: now)).day ?? 0
    if days == 0 { return "today " + clock }
    if days == 1 { return "yesterday " + clock }
    if days > 1 && days < 7 { return Fmt.weekday.string(from: d) + " " + clock }
    let sameYear = cal.component(.year, from: d) == cal.component(.year, from: now)
    return (sameYear ? Fmt.dayLabel : Fmt.shortDay).string(from: d) + " " + clock
}

/// The turn in flight in a finished turn's words, the console's `turnFacts`
/// (ui.js) word for word: "23 tool calls · today 10:09 PM · $1.12" (its tool
/// calls, when the newest of them was made, its dollars so far: the clock
/// moves with the count). The chat's working line and
/// the session card print it where a raw step label used to sit.
/// `withCalls: false` leaves the count to a live run block that already shows it;
/// `withCost: false` leaves the dollars to the card's state line.
func turnFactsLine(calls n: Int?, at: Date?, cost: Double?, withCalls: Bool = true, withCost: Bool = true, now: Date = Date()) -> String {
    var parts: [String] = []
    if withCalls, let n, n > 0 { parts.append("\(n) tool call" + (n == 1 ? "" : "s")) }
    if let at { parts.append(dayClock(at, now: now)) }
    if withCost, let cost, cost > 0 { parts.append(usd(cost)) }
    return parts.joined(separator: " · ")
}

extension Thread {
    func turnFacts(withCalls: Bool = true, withCost: Bool = true) -> String {
        turnFactsLine(calls: turn_tools, at: turn_at, cost: turn_cost_usd, withCalls: withCalls, withCost: withCost)
    }
}

/// THE short model name, the console's `modelShort` (ui.js): "claude-fable-5[1m]"
/// → "fable 5", "claude-haiku-4-5-20251001" → "haiku 4.5"; "" for none.
func shortModel(_ id: String) -> String {
    if id.isEmpty || id == "owner" || id == "unknown" { return id }
    var m = id
    if m.hasPrefix("claude-") { m.removeFirst(7) }
    if m.hasSuffix("[1m]") { m.removeLast(4) }
    var parts = m.split(separator: "-", omittingEmptySubsequences: false).map(String.init)
    if parts.count > 1, let last = parts.last, last.count == 8, last.allSatisfy(\.isNumber) { parts.removeLast() }
    return parts.count > 1 ? parts[0] + " " + parts[1...].joined(separator: ".") : parts[0]
}

/// Label for a hub schedule string ("every@6h" → "every 6h"), the console's
/// `readableSchedule`: no schedule is "", unknown formats pass through.
func scheduleLabel(_ s: String) -> String {
    guard let at = s.firstIndex(of: "@") else { return s }
    let kind = s[..<at], arg = s[s.index(after: at)...]
    switch kind {
    case "every", "daily", "weekly": return "\(kind) \(arg)"
    default: return s
    }
}

// MARK: - parity helpers

/// A share count, the console's `qty`: en-US grouping, up to four decimals,
/// trailing zeros dropped — "41.5", "1,204", "0.0312".
func qty(_ x: Double) -> String {
    let v = x.isFinite ? x : 0
    var s = fixed(abs(v), 4)
    while s.contains("."), s.hasSuffix("0") { s.removeLast() }
    if s.hasSuffix(".") { s.removeLast() }
    let parts = s.split(separator: ".", omittingEmptySubsequences: false)
    let whole = commas(Int(parts[0]) ?? 0) + (parts.count > 1 ? "." + parts[1] : "")
    return (v < 0 && whole != "0" ? "-" : "") + whole
}

/// "1st", "2nd", "11th", "23rd" — the console's ordinal in the account tip.
func ordinal(_ n: Int) -> String {
    let t = n % 100
    let s = (11...13).contains(t) ? "th" : ([1: "st", 2: "nd", 3: "rd"][n % 10] ?? "th")
    return "\(n)\(s)"
}
