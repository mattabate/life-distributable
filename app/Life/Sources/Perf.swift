// Measuring a slow screen from inside the app. Instruments
// cannot be attached from an agent's shell (xctrace parks on a prompt nobody
// can answer), so `ops/py.sh profile-sim.py` launches the simulator build with
// LIFE_PERF=<a file on the Mac> and reads these lines out of it:
//
//   PERF stall 1840ms                 the main thread did not run for that long
//   PERF <name> n=312 total=2210ms    a counted section, reported every second
//
// Off (one env lookup, then a nil check per call) unless LIFE_PERF is set, and
// compiled out of release builds entirely.
import Foundation

enum Perf {
    #if DEBUG
    /// LIFE_PERF is the file the lines go to: a simulator app writes to the
    /// Mac's disk directly, and `simctl launch --console` handed a file for
    /// stdout forwarded nothing.
    private static let sink: FileHandle? = {
        guard let path = ProcessInfo.processInfo.environment["LIFE_PERF"], !path.isEmpty else { return nil }
        FileManager.default.createFile(atPath: path, contents: nil)
        return FileHandle(forWritingAtPath: path)
    }()
    static var on: Bool { sink != nil }
    nonisolated(unsafe) private static var counts: [String: (n: Int, ns: UInt64)] = [:]
    private static let lock = NSLock()
    private static let started = DispatchTime.now().uptimeNanoseconds

    private static func say(_ s: String) {
        // `started` first: a lazy static read AFTER `now()` is the later of
        // the two on its first use, and the unsigned subtraction trapped.
        let t0 = started
        let ms = (DispatchTime.now().uptimeNanoseconds &- t0) / 1_000_000
        lock.lock(); defer { lock.unlock() }
        sink?.write(Data("PERF t=\(ms)ms \(s)\n".utf8))
    }

    /// Call once at launch: a background thread pings the main queue every
    /// 50 ms and reports any ping that took over 150 ms to be answered — the
    /// freeze as the user feels it, whatever causes it.
    static func start() {
        guard on else { return }
        _ = started
        let t = Foundation.Thread {
            var lastReport = DispatchTime.now().uptimeNanoseconds
            while true {
                let sent = DispatchTime.now().uptimeNanoseconds
                let sem = DispatchSemaphore(value: 0)
                DispatchQueue.main.async { sem.signal() }
                sem.wait()
                let took = (DispatchTime.now().uptimeNanoseconds - sent) / 1_000_000
                if took > 150 { say("stall \(took)ms") }
                if DispatchTime.now().uptimeNanoseconds - lastReport > 1_000_000_000 {
                    lastReport = DispatchTime.now().uptimeNanoseconds
                    lock.lock(); let snap = counts; counts = [:]; lock.unlock()
                    for (k, v) in snap.sorted(by: { $0.value.ns > $1.value.ns }) {
                        say("\(k) n=\(v.n) total=\(v.ns / 1_000_000)ms")
                    }
                }
                Foundation.Thread.sleep(forTimeInterval: 0.05)
            }
        }
        t.name = "perf"
        t.start()
    }

    static func event(_ s: @autoclosure () -> String) { if on { say(s()) } }

    /// Time one section; totals are reported once a second.
    @inline(__always) static func time<T>(_ name: String, _ body: () -> T) -> T {
        guard on else { return body() }
        let a = DispatchTime.now().uptimeNanoseconds
        let v = body()
        let d = DispatchTime.now().uptimeNanoseconds - a
        lock.lock()
        let c = counts[name] ?? (0, 0)
        counts[name] = (c.n + 1, c.ns + d)
        lock.unlock()
        return v
    }
    #else
    static let on = false
    static func start() {}
    static func event(_ s: @autoclosure () -> String) {}
    @inline(__always) static func time<T>(_ name: String, _ body: () -> T) -> T { body() }
    #endif
}
