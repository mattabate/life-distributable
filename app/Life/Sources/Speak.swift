import AVFoundation
import SwiftUI

/// The phone's voice for a card. Two ways in:
///
/// `announce` — a push arrived (Push.swift). Siri's Announce Notifications
/// only reads a push to a LOCKED phone whose headphones are its own — and
/// then only a short one; with the app open, or the headphones carrying
/// another device's audio, every card was a silent banner. So the app speaks
/// the whole line itself, foreground or woken in
/// the background (UIBackgroundModes audio; the push is passive, so Siri is
/// quiet), through the same playback session the 🔊 tap uses — activating it
/// is what pulls the headphones over to the phone. It speaks only into
/// headphones: on the phone's own speaker it says nothing, so a room never
/// hears a card. Lines queue behind one another, never cut off.
///
/// `toggle` — the 🔊 tap on a card reads the recorded sentence (`asks.said`)
/// again, on command. A tap while it speaks stops it; a tap on another card's
/// line moves to that one.
@MainActor
final class Speaker: NSObject, ObservableObject, AVSpeechSynthesizerDelegate {
    static let shared = Speaker()

    /// The text being spoken now, or nil. A card compares its own line to
    /// draw the stop glyph.
    @Published private(set) var saying: String?

    private let synth = AVSpeechSynthesizer()

    /// The push line being said on its own (nil = nothing, or a replay). A
    /// push speaks only into headphones, so the moment the route leaves them
    /// mid-line — headphones out, or iOS handing the sound back to the
    /// speaker for headphones that are connected but not in the owner's
    /// ears — it stops. A replay is an explicit tap: it plays wherever they
    /// pointed the phone, speaker included, and is never cut.
    private var announcing: (line: String, thread: String, state: String)?

    override private init() {
        super.init()
        synth.delegate = self
        NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.routeChanged() }
        }
    }

    /// The route moved: a push line still going with nothing on their ears
    /// stops at once and the hub hears why (its "speaking" mark ends).
    private func routeChanged() {
        guard let a = announcing, Speaker.earsName() == nil else { return }
        announcing = nil
        if synth.isSpeaking { synth.stopSpeaking(at: .immediate) }
        saying = nil
        Speaker.report("stopped", route: Speaker.routeNames(), state: a.state, line: a.line, thread: a.thread)
    }

    /// The session whose card is being replayed ("" = a card outside one),
    /// nil when no replay is playing. While it plays the hub holds the floor:
    /// the row says "speaking" and no push talks over it.
    private var replay: String?

    func toggle(_ text: String, thread: String = "") {
        if saying == text { stop(); return }
        stop()
        let clean = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !clean.isEmpty else { return }
        // `.playback`, spoken: it must sound with the ring switch off (a read
        // they went looking for is one they want to hear) and duck music the
        // way the push did.
        activate()
        announcing = nil
        speak(clean)
        replay = thread
        floor(thread, secs: Double(clean.split(separator: " ").count) / 2.5 + 2)
    }

    private func floor(_ thread: String, secs: Double) {
        let hub = AppDelegate.push.hub ?? HubClient()
        guard hub.isConfigured else { return }
        Task { _ = try? await hub.voice(thread: thread, secs: secs) }
    }

    private func endReplay() {
        guard let t = replay else { return }
        replay = nil
        floor(t, secs: 0)
    }

    /// A push's line, spoken if something is on the owner's ears. Queued
    /// behind whatever is being said now.
    ///
    /// The route is read AFTER the headphones have had time to come over, not
    /// the instant the session activates: headphones shared with another
    /// device switch to the phone only once it activates playback, a second or
    /// more later; checking at once sees the phone's own speaker and says
    /// nothing. Every
    /// decision is posted to the hub as an `app/speech` observation, so the
    /// hub log shows what the phone did right under "the phone speaks" — and
    /// `spoke`, carrying the card's session (`thread`, from the push), is
    /// what marks that session "speaking" on the board: the hub no longer
    /// marks it when the push goes out, because the phone may rightly say
    /// nothing.
    func announce(_ text: String, thread: String = "", state: String = "foreground", sentAt: TimeInterval = 0) {
        let clean = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !clean.isEmpty else { return }
        // A push iOS delivered late is left unspoken: the card is on the
        // board either way, and saying it now pulls the headphones off
        // whatever device has them since.
        if sentAt > 0, Date().timeIntervalSince1970 - sentAt > Speaker.staleAfter {
            Speaker.report("stale", route: Speaker.routeNames(), state: state, line: clean, thread: thread)
            return
        }
        activate()
        Task { @MainActor in
            var route = Speaker.earsName()
            var how = ""
            // The app is the phone's only voice, locked or not. Siri's
            // Announce Notifications does not read a long push: past a
            // sentence or two it says "Life sent a long notification. Read
            // it?". So the hub sends the push the phone speaks as PASSIVE,
            // the one level Siri never announces (Announce Notifications
            // itself stays on), and the whole line is said here, once.
            if route == nil {
                // Nothing on the owner's ears yet. Merely waiting leaves the
                // route on "Speaker": headphones shared with another device
                // come over only when the phone
                // actually PLAYS — the 🔊 tap pulls them because it speaks
                // outright. So play silence to become their source, watch the
                // route, and only then say the words; nothing ever sounds on
                // the phone's own speaker.
                let pull = Silence()
                pull.start()
                route = await Speaker.earsRoute(within: routeWait)
                pull.stop()
                how = " (pulled)"
            }
            if route != nil {
                // Let it settle: headphones that are connected but out of
                // the owner's ears come over as the route and, a beat later, iOS hands
                // the sound back to the phone's own speaker. Read it again
                // before a word is said.
                try? await Task.sleep(for: .milliseconds(400))
                route = Speaker.earsName()
            }
            if let route {
                announcing = (clean, thread, state)
                speak(clean)
                Speaker.report("spoke", route: route + how, state: state, line: clean, thread: thread)
                return
            }
            if !synth.isSpeaking { try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation) }
            Speaker.report("silent", route: Speaker.routeNames(), state: state, line: clean, thread: thread)
        }
    }

    /// How long a push waits for headphones to become the route.
    private let routeWait: Duration = .seconds(5)

    /// A push older than this when it arrives is stale: unspoken, reported.
    private static let staleAfter: TimeInterval = 120

    /// The name of the headphone route, polled until one appears or the wait
    /// runs out; nil = still the phone's own speaker.
    private static func earsRoute(within wait: Duration) async -> String? {
        let clock = ContinuousClock()
        let deadline = clock.now + wait
        while true {
            if let name = earsName() { return name }
            if clock.now >= deadline { return nil }
            try? await Task.sleep(for: .milliseconds(250))
        }
    }

    /// Tell the hub what the phone did with a push (`POST /observations`,
    /// source app, kind speech): outcome, the route it saw, foreground or
    /// background, and the line. Best effort.
    private static func report(_ outcome: String, route: String, state: String, line: String, thread: String = "") {
        let hub = AppDelegate.push.hub ?? HubClient()
        guard hub.isConfigured else { return }
        var payload: [String: Any] = ["outcome": outcome, "route": route, "state": state, "line": line]
        if !thread.isEmpty { payload["thread"] = thread }
        Task { _ = try? await hub.upload(source: "app", kind: "speech", ts: Date(), payload: payload, file: nil, filename: nil) }
    }

    func stop() {
        if synth.isSpeaking { synth.stopSpeaking(at: .immediate) }
        saying = nil
        endReplay()
    }

    private func activate() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .spokenAudio, options: [.duckOthers])
        try? session.setActive(true)
    }

    private func speak(_ clean: String) {
        let u = AVSpeechUtterance(string: clean)
        u.voice = Speaker.voice
        u.rate = AVSpeechUtteranceDefaultSpeechRate
        if !synth.isSpeaking { saying = clean }
        synth.speak(u)
    }

    /// Whether the active session's output is headphones (Bluetooth, wired,
    /// car, AirPlay) rather than the phone's own speaker.
    static func onHisEars() -> Bool { earsName() != nil }

    /// The headphone output's name ("Alex's Headphones"), or nil on the speaker.
    static func earsName() -> String? {
        let ears: Set<AVAudioSession.Port> = [.bluetoothA2DP, .bluetoothHFP, .bluetoothLE, .headphones, .carAudio, .airPlay]
        return AVAudioSession.sharedInstance().currentRoute.outputs.first { ears.contains($0.portType) }?.portName
    }

    /// Every current output, named, for the report ("Speaker").
    static func routeNames() -> String {
        let names = AVAudioSession.sharedInstance().currentRoute.outputs.map { "\($0.portName) (\($0.portType.rawValue))" }
        return names.isEmpty ? "none" : names.joined(separator: ", ")
    }

    /// The one voice every Life line is spoken in, on every surface — the
    /// hub's notify.SpokenVoice and the console's SAY_VOICE carry the same name
    /// "Best quality en-US" was a tie among a dozen default voices, so the
    /// pick moved between launches (Grandpa, Shelley, Sandy…).
    static let voiceName = "Samantha"
    private static let voice: AVSpeechSynthesisVoice? = {
        let en = AVSpeechSynthesisVoice.speechVoices().filter { $0.language.hasPrefix("en") && $0.name == voiceName }
        return en.max { $0.quality.rawValue < $1.quality.rawValue } ?? AVSpeechSynthesisVoice(language: "en-US")
    }()

    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didStart u: AVSpeechUtterance) {
        let line = u.speechString
        Task { @MainActor in
            saying = line
            // A push line that starts with nothing on their ears stops here.
            routeChanged()
        }
    }
    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didFinish u: AVSpeechUtterance) { Task { @MainActor in finished() } }
    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didCancel u: AVSpeechUtterance) { Task { @MainActor in finished() } }

    private func finished() {
        // Another line may be queued behind this one: only the last lets the
        // music back up.
        guard !synth.isSpeaking else { return }
        saying = nil
        // The hub marked the session "speaking" for a reckoned length on
        // `spoke`; the real end is here, and it ends the mark.
        if let a = announcing {
            Speaker.report("finished", route: Speaker.routeNames(), state: a.state, line: a.line, thread: a.thread)
        }
        announcing = nil
        endReplay()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}

/// One second of silence, looped at zero volume: playback that makes no sound,
/// so the phone becomes the headphones' source (they follow whichever device
/// plays) before a card is spoken into them.
@MainActor
private final class Silence {
    private var player: AVAudioPlayer?

    func start() {
        player = try? AVAudioPlayer(data: Silence.wav)
        player?.numberOfLoops = -1
        player?.volume = 0
        player?.play()
    }

    func stop() {
        player?.stop()
        player = nil
    }

    /// A 16-bit mono 8 kHz WAV of one second of zeros, built in memory.
    private static let wav: Data = {
        let rate: UInt32 = 8000
        let bytes = rate * 2
        var d = Data()
        func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { d.append(contentsOf: $0) } }
        func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { d.append(contentsOf: $0) } }
        d.append(contentsOf: Array("RIFF".utf8)); u32(36 + bytes); d.append(contentsOf: Array("WAVE".utf8))
        d.append(contentsOf: Array("fmt ".utf8)); u32(16); u16(1); u16(1); u32(rate); u32(bytes); u16(2); u16(16)
        d.append(contentsOf: Array("data".utf8)); u32(bytes)
        d.append(Data(count: Int(bytes)))
        return d
    }()
}

/// `POST /voice` (shared/api.md): a replayed line and how long it lasts.
struct VoiceIn: Encodable, Sendable {
    var thread_id: String
    var secs: Double
}

struct VoiceFloor: Codable, Sendable {
    var speaking: [String]?
}
