import AVFoundation
import SwiftUI

/// `GET /voice/ear` (shared/api.md): whether this Mac may say a line now, and
/// the line waiting for it.
struct EarView: Codable, Sendable {
    var ears: Bool
    var line: EarLine?
    var voice: String?
}

struct EarLine: Codable, Sendable, Equatable {
    var id: String
    var card: String?
    var thread_id: String?
    var text: String
    var at: String
}

#if targetEnvironment(macCatalyst)
import Darwin

/// The desktop app as this Mac's ear (notify/ear.go): while the app is open
/// it polls the hub every 2 s (a quarter second while saying) with where this Mac's sound goes and whether
/// the owner is at it, and says the card line the hub hands it in the one
/// Life voice. On the hub's own Mac the voice helper (ops/bin/ear) polls too,
/// and the hub takes its word over this one's, so a line is said once.
/// `playing` goes up at the first word, `spoke` at the end, `silent` when
/// there is no output: the phone's push takes a line left unsaid. A line that
/// leaves the poll (answered, hushed, the Devices card's Stop, the Audio
/// switch) stops at once.
@MainActor
final class MacEar: NSObject, AVSpeechSynthesizerDelegate {
    static let shared = MacEar()

    /// This Mac as the hub tells Macs apart: its host name's first label,
    /// lower case (the hub's deviceName; ops/ear.swift names itself the same).
    static let device: String = {
        var b = [CChar](repeating: 0, count: 256)
        guard gethostname(&b, b.count) == 0 else { return "mac" }
        let h = String(cString: b).lowercased()
        let first = h.split(separator: ".").first.map(String.init) ?? ""
        return first.isEmpty ? "mac" : first
    }()

    private let synth = AVSpeechSynthesizer()
    private var hub: HubClient?
    /// The line taken last (said, saying, or reported silent): never twice.
    private var taken: String?
    /// The line being said now.
    private var saying: String?
    private var started = false

    override private init() {
        super.init()
        synth.delegate = self
    }

    func start(hub: HubClient) {
        // A screenshot copy (ops/mac-screens.sh) is not the owner's app.
        guard !started, ProcessInfo.processInfo.environment["LIFE_SHOT"] == nil else { return }
        started = true
        self.hub = hub
        // The output the line was playing on went away (AirPods out, a headset
        // unplugged): stop now, not at the next poll, before macOS moves the
        // rest of the line onto the speakers.
        NotificationCenter.default.addObserver(
            forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main
        ) { n in
            let why = (n.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt)
                .flatMap(AVAudioSession.RouteChangeReason.init(rawValue:))
            guard why == .oldDeviceUnavailable else { return }
            Task { @MainActor in MacEar.shared.outputLeft() }
        }
        Task {
            while !Task.isCancelled {
                await poll()
                // Fast while saying, as ops/ear.swift: a Stop or a lost
                // output cuts the line within a quarter second.
                try? await Task.sleep(for: saying == nil ? .seconds(2) : .milliseconds(250))
            }
        }
    }

    private func outputLeft() {
        guard let id = saying else { return }
        saying = nil
        synth.stopSpeaking(at: .immediate)
        report(id, "silent")
    }

    private func poll() async {
        guard let hub, hub.isConfigured else { return }
        let (route, output) = Self.route()
        // A Catalyst app cannot read the Mac's idle time: the owner counts as
        // at this Mac only while Life is the app in front.
        let idle: Double = UIApplication.shared.applicationState == .active ? 0 : -1
        guard let v = try? await hub.earPoll(device: Self.device, route: route, output: output, idle: idle) else { return }
        guard let l = v.line else {
            // Withdrawn mid-line: the voice stops.
            if saying != nil { saying = nil; synth.stopSpeaking(at: .immediate) }
            return
        }
        guard l.id != taken else {
            // The headphones left (or the Audio switch went off) mid-line:
            // the voice stops and the phone takes the line.
            if saying == l.id, !v.ears {
                saying = nil
                synth.stopSpeaking(at: .immediate)
                report(l.id, "silent")
            }
            return
        }
        taken = l.id
        guard v.ears else { report(l.id, "silent"); return }
        saying = l.id
        let u = AVSpeechUtterance(string: l.text)
        u.voice = Self.voice(v.voice ?? Speaker.voiceName)
        u.rate = AVSpeechUtteranceDefaultSpeechRate
        synth.speak(u)
    }

    private func report(_ id: String, _ outcome: String) {
        guard let hub else { return }
        Task { try? await hub.earReport(device: Self.device, id: id, outcome: outcome) }
    }

    /// Where this Mac's sound goes: headphones (AirPods, a headset, a car),
    /// its speakers, or none; and the output's name.
    static func route() -> (String, String) {
        if let ears = Speaker.earsName() { return ("headphones", ears) }
        guard let out = AVAudioSession.sharedInstance().currentRoute.outputs.first else { return ("none", "") }
        return ("speakers", out.portName)
    }

    private static func voice(_ name: String) -> AVSpeechSynthesisVoice? {
        let en = AVSpeechSynthesisVoice.speechVoices().filter { $0.language.hasPrefix("en") && $0.name == name }
        return en.max { $0.quality.rawValue < $1.quality.rawValue } ?? AVSpeechSynthesisVoice(language: "en-US")
    }

    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didStart u: AVSpeechUtterance) {
        Task { @MainActor in if let id = saying { report(id, "playing") } }
    }
    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didFinish u: AVSpeechUtterance) {
        Task { @MainActor in
            if let id = saying { report(id, "spoke") }
            saying = nil
        }
    }
    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didCancel u: AVSpeechUtterance) {
        // Only a withdrawn line is cancelled, and the hub has let it go.
        Task { @MainActor in saying = nil }
    }
}
#endif
