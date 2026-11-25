// ear is the hub Mac's voice: it polls the hub as this Mac's ear and says the
// card line it is handed with the Mac's own `say`, whether or not the desktop
// app is open (notify/ear.go; the desktop app is the ear on any other Mac).
//
// Every 2 s (a quarter second while a line plays): GET /api/v1/voice/ear
// with this Mac's output route (headphones · speakers · none), the output's
// name, the seconds since its last keyboard or mouse input, and `by=ear`, so
// the hub takes this over the desktop app's poll on the same Mac. The hub
// alone decides who speaks; this only follows the poll:
//   a line, ears    → `say -v <voice>`, playing at launch, spoke at exit 0,
//                     silent at any other exit;
//   a line, no ears → silent (the phone takes it);
//   the line leaves the poll (answered, hushed, Stop, the Audio switch off)
//                   → `say` is stopped.
// Address and token: ops/hub.json public_host, ops/secrets/hub.token (the
// files lifectl reads). launchd: com.life.ear, written and loaded by
// `ops/hub.sh install`. Log: ops/logs/ear.log.
import CoreAudio
import CoreGraphics
import Darwin
import Foundation

setvbuf(stdout, nil, _IOLBF, 0)
let stamp = ISO8601DateFormatter()
func log(_ s: String) { print("\(stamp.string(from: Date())) ear: \(s)") }

// MARK: - where things are

let bin = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath().deletingLastPathComponent()
let root = bin.deletingLastPathComponent().deletingLastPathComponent()

func hubBase() -> String {
    if let h = ProcessInfo.processInfo.environment["LIFE_HUB"], !h.isEmpty { return h }
    let cfg = root.appendingPathComponent("ops/hub.json")
    guard let d = try? Data(contentsOf: cfg),
          let j = try? JSONSerialization.jsonObject(with: d) as? [String: Any],
          let host = j["public_host"] as? String, !host.isEmpty else {
        log("no public_host in \(cfg.path)")
        exit(2)
    }
    return "https://" + host
}
let base = hubBase()
let token = (try? String(contentsOf: root.appendingPathComponent("ops/secrets/hub.token"), encoding: .utf8))?
    .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
if token.isEmpty { log("no token in ops/secrets/hub.token"); exit(2) }

/// This Mac as the hub tells the ears apart: its host name's first label,
/// lower case — what the hub calls its own Mac (os.Hostname) and what the
/// desktop app sends (MacEar.swift).
let device: String = {
    var b = [CChar](repeating: 0, count: 256)
    guard gethostname(&b, b.count) == 0 else { return "mac" }
    let h = String(cString: b).lowercased().split(separator: ".").first.map(String.init) ?? ""
    return h.isEmpty ? "mac" : h
}()

// MARK: - this Mac

let sys = AudioObjectID(kAudioObjectSystemObject)
func get<T>(_ id: AudioObjectID, _ sel: AudioObjectPropertySelector, _ value: inout T, scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal) -> Bool {
    var a = AudioObjectPropertyAddress(mSelector: sel, mScope: scope, mElement: kAudioObjectPropertyElementMain)
    var size = UInt32(MemoryLayout<T>.size)
    return AudioObjectGetPropertyData(id, &a, 0, nil, &size, &value) == noErr
}

/// The default output as the hub words it: headphones (Bluetooth — AirPods or
/// any other pair — AirPlay, or the built-in jack with headphones in), none
/// (no output), anything else (the built-in or a display's speakers) is
/// speakers; and its name as Sound settings shows it.
struct Route: Equatable {
    var word: String
    var name = ""
    var described: String { name.isEmpty ? word : "\(word) (\(name))" }
}

func outputName(_ id: AudioDeviceID) -> String {
    var a = AudioObjectPropertyAddress(mSelector: kAudioObjectPropertyName, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    var ref: Unmanaged<CFString>?
    var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    let ok = withUnsafeMutablePointer(to: &ref) { AudioObjectGetPropertyData(id, &a, 0, nil, &size, $0) == noErr }
    guard ok, let ref else { return "" }
    return ref.takeRetainedValue() as String
}

func route() -> Route {
    var dev = AudioDeviceID(0)
    guard get(sys, kAudioHardwarePropertyDefaultOutputDevice, &dev), dev != 0 else { return Route(word: "none") }
    var alive = UInt32(1)
    if get(dev, kAudioDevicePropertyDeviceIsAlive, &alive), alive == 0 { return Route(word: "none") }
    var r = Route(word: "speakers", name: outputName(dev))
    var t = UInt32(0)
    _ = get(dev, kAudioDevicePropertyTransportType, &t)
    switch t {
    case kAudioDeviceTransportTypeBluetooth, kAudioDeviceTransportTypeBluetoothLE, kAudioDeviceTransportTypeAirPlay:
        r.word = "headphones"
    case kAudioDeviceTransportTypeBuiltIn:
        var src = UInt32(0)
        if get(dev, kAudioDevicePropertyDataSource, &src, scope: kAudioDevicePropertyScopeOutput), src == 0x6864_706E /* 'hdpn' */ {
            r.word = "headphones"
        }
    default:
        break
    }
    return r
}

/// Seconds since this Mac last saw a keyboard, mouse or trackpad (any event
/// type: kCGAnyInputEventType, ~0, which Swift does not import).
func idleSeconds() -> Int {
    Int(CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: CGEventType(rawValue: ~0)!))
}

// MARK: - the hub

struct Line: Decodable, Equatable { var id: String; var text: String }
struct View: Decodable { var ears: Bool; var line: Line?; var voice: String? }

func request(_ method: String, _ path: String, body: [String: String]? = nil) -> Data? {
    guard let url = URL(string: base + path) else { return nil }
    var r = URLRequest(url: url, timeoutInterval: 10)
    r.httpMethod = method
    r.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
    if let body {
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.httpBody = try? JSONSerialization.data(withJSONObject: body)
    }
    let done = DispatchSemaphore(value: 0)
    var out: Data?
    URLSession.shared.dataTask(with: r) { d, resp, _ in
        if (resp as? HTTPURLResponse)?.statusCode == 200 { out = d }
        done.signal()
    }.resume()
    done.wait()
    return out
}

let reports = DispatchQueue(label: "ear.reports") // in order, off the poll
/// `why`: a silent line's reason, for the hub's log (this log stays on this
/// Mac, so the hub could not otherwise tell why a line went unsaid).
func report(_ id: String, _ outcome: String, _ why: String = "") {
    reports.async {
        if request("POST", "/api/v1/voice/ear", body: ["device": device, "id": id, "outcome": outcome, "why": why]) == nil {
            log("\(id): the hub did not take \(outcome)")
        }
    }
}

// MARK: - the voice

/// The line `say` is saying, if any. Touched on the main thread only.
var playing: (id: String, say: Process, withdrawn: Bool)?
/// The lines already taken (said, or reported silent): never twice.
var acted: [String] = []

func say(_ l: Line, voice: String) {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/usr/bin/say")
    p.arguments = (voice.isEmpty ? [] : ["-v", voice]) + ["--", l.text]
    let id = l.id
    p.terminationHandler = { p in
        DispatchQueue.main.async {
            guard let cur = playing, cur.id == id else { return }
            playing = nil
            if cur.withdrawn {
                log("\(id): stopped (withdrawn)")
                return
            }
            let ok = p.terminationStatus == 0
            log("\(id): say exited \(p.terminationStatus); " + (ok ? "spoke" : "silent"))
            report(id, ok ? "spoke" : "silent", ok ? "" : "say exited \(p.terminationStatus)")
        }
    }
    do {
        try p.run()
    } catch {
        log("\(id): say would not start (\(error)); silent")
        report(id, "silent", "say would not start (\(error))")
        return
    }
    playing = (id, p, false)
    log("\(id): saying \(l.text.count) chars into \(route().described)")
    report(id, "playing")
}

func stop(_ why: String) {
    guard let p = playing, !p.withdrawn else { return }
    playing!.withdrawn = true
    log("\(p.id): \(why); the voice stops")
    if p.say.isRunning { p.say.terminate() }
}

func take(_ v: View) {
    if let p = playing {
        guard let l = v.line, l.id == p.id else { stop("withdrawn by the hub"); return }
        if !v.ears {
            stop("the route left mid-line")
            report(p.id, "silent", "the route left mid-line")
        }
        return
    }
    guard let l = v.line, !l.id.isEmpty, !acted.contains(l.id) else { return }
    acted.append(l.id)
    if acted.count > 50 { acted.removeFirst(acted.count - 50) }
    guard v.ears else {
        log("\(l.id): no output, or this Mac's Audio is off; silent")
        report(l.id, "silent", "no output, or this Mac's Audio is off")
        return
    }
    say(l, voice: v.voice ?? "")
}

// MARK: - the poll

for sig in [SIGINT, SIGTERM] {
    signal(sig) { _ in
        if let p = playing?.say, p.isRunning { p.terminate() }
        exit(0)
    }
}
log("polling \(base) as \(device)")
var lastRoute = Route(word: "")
/// The binary as launched: `make build` (every hub restart) rebuilds it, and
/// a new one is picked up by exiting between lines; launchd's KeepAlive
/// starts it again.
let binPath = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath().path
func built() -> Date? { (try? FileManager.default.attributesOfItem(atPath: binPath))?[.modificationDate] as? Date }
let launchedBuild = built()
Thread.detachNewThread {
    while true {
        if let b = built(), b != launchedBuild, DispatchQueue.main.sync(execute: { playing == nil }) {
            log("rebuilt; restarting")
            exit(0)
        }
        let r = route()
        if r != lastRoute {
            log("route \(r.described)")
            lastRoute = r
        }
        let path = "/api/v1/voice/ear?by=ear&route=\(r.word)&idle=\(idleSeconds())&device="
            + (device.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? "")
            + "&output=" + (r.name.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? "")
        if let d = request("GET", path), let v = try? JSONDecoder().decode(View.self, from: d) {
            DispatchQueue.main.sync { take(v) }
        }
        let busy = DispatchQueue.main.sync { playing != nil }
        Thread.sleep(forTimeInterval: busy ? 0.25 : 2)
    }
}
dispatchMain()
