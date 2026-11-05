// mic listens on the Mac's default input while it runs and prints what it
// hears as text, one JSON line per event, with a time on every word — a
// speech log that joins to the owner's other observations by time. The hub
// runs it (server/mic.go), reads stdout, and drives it over stdin:
//
//   launch      loads the on-device speech model and prepares the analyzer —
//               the slow part (seconds), done BEFORE any tap so the tap is
//               instant. The microphone is NOT opened here: no orange dot,
//               no permission prompt, nothing heard. Prints {"k":"warm"}.
//   "go\n"      opens the input and starts transcribing. Only the owner's
//               switch writes it; the hub never opens the microphone on its own.
//               The input is the system default (hub.json mic_input
//               "default": the AirPods mic when in — on macOS 26 they keep
//               playing at 48 kHz while their mic is open, the probe showed)
//               or the Mac's own microphone (LIFE_MIC=builtin). Only that
//               one device is opened — see Input below — nothing else on the
//               system is touched.
//   stdin EOF   the switch-off. The input stays open 0.6 s more so the word
//               in flight lands, then the analyzer finalizes everything it heard —
//               the tail arrives as a last `final` — and this exits 0. EOF
//               before "go" = exit 0, the microphone never touched.
//
// The words are made ON THIS MAC by Apple's SpeechAnalyzer + SpeechTranscriber
// (macOS 26; the same engine the phone app's dictation uses), never by a
// service: nothing leaves the tailnet, no API is paid.
// Volatile (still-changing) text is printed as it comes so the console can
// draw it; a final result carries its words with their start/end on the audio
// clock, which the hub pins to wall clock by the `ready` line's `at`.
//
// Lines (stdout, one JSON object each):
//   {"k":"status","text":"…"}                 what it is doing (loading, permission) —
//                                              the console's button shows this word for word
//   {"k":"log","text":"…"}                    for hub.log only (devices, rates), never the button
//   {"k":"warm"}                             model ready, mic closed, waiting for "go"
//   {"k":"ready","at":<unix ms>,"device":"…"}  the input is open; audio second 0 = at
//   {"k":"partial","text":"…"}                 the tentative tail, replaced by the next
//   {"k":"final","text":"…","t0":s,"t1":s,"words":[[t0,t1,"word"],…]}
//   {"k":"error","text":"…"}                   then exit 4
// Exit: 0 stopped cleanly · 2 no permission · 3 no input/model · 4 failed mid-run.
// Audio is not kept. Built to ops/bin/mic by `make build`; the Info.plist with
// the usage strings rides inside the binary (Makefile) and `make build` signs
// it so the one Microphone grant survives rebuilds.
@preconcurrency import AVFoundation
import CoreAudio
import Foundation
import Speech

setvbuf(stdout, nil, _IOLBF, 0)

/// How long the input stays open after the switch-off, for the word in flight.
let tailSeconds = 0.6
/// How long the finalize may take before this exits regardless.
let finishSeconds = 2.5

func emit(_ obj: [String: Any]) {
    if let d = try? JSONSerialization.data(withJSONObject: obj), let s = String(data: d, encoding: .utf8) {
        print(s)
        fflush(stdout)
    }
}

func fail(_ text: String, _ code: Int32) -> Never {
    emit(["k": "error", "text": text])
    exit(code)
}

func globalAddress(_ sel: AudioObjectPropertySelector, _ scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: sel, mScope: scope, mElement: kAudioObjectPropertyElementMain)
}

/// The system's default input device (what macOS picks: the AirPods when in).
func defaultInput() -> AudioDeviceID {
    var dev = AudioDeviceID(0)
    var size = UInt32(MemoryLayout<AudioDeviceID>.size)
    var addr = globalAddress(kAudioHardwarePropertyDefaultInputDevice)
    _ = AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &dev)
    return dev
}

/// The Mac's own microphone, if there is one: the LIFE_MIC=builtin choice.
/// Kept for a headset whose mic really does drag the link down to the
/// hands-free profile; AirPods on macOS 26 do not (coarse music there was
/// AVAudioEngine's aggregate device, see Input), so the default input is the
/// default setting.
func builtInInput() -> AudioDeviceID? {
    var addr = globalAddress(kAudioHardwarePropertyDevices)
    var size: UInt32 = 0
    guard AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size) == noErr else { return nil }
    var ids = [AudioDeviceID](repeating: 0, count: Int(size) / MemoryLayout<AudioDeviceID>.size)
    guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &ids) == noErr else { return nil }
    for dev in ids {
        var transport: UInt32 = 0
        var tsize = UInt32(MemoryLayout<UInt32>.size)
        var taddr = globalAddress(kAudioDevicePropertyTransportType)
        guard AudioObjectGetPropertyData(dev, &taddr, 0, nil, &tsize, &transport) == noErr,
              transport == kAudioDeviceTransportTypeBuiltIn else { continue }
        var saddr = globalAddress(kAudioDevicePropertyStreams, kAudioObjectPropertyScopeInput)
        var ssize: UInt32 = 0
        guard AudioObjectGetPropertyDataSize(dev, &saddr, 0, nil, &ssize) == noErr, ssize > 0 else { continue }
        return dev
    }
    return nil
}

/// The system's default output and its nominal rate right now — printed as
/// the input opens and as it closes, so a probe shows whether opening this
/// microphone changed what the AirPods are doing (a drop from 44.1/48 kHz to
/// 8/16/24 kHz = they went to the hands-free profile, music with it).
func outputNow() -> String {
    var dev = AudioDeviceID(0)
    var size = UInt32(MemoryLayout<AudioDeviceID>.size)
    var addr = globalAddress(kAudioHardwarePropertyDefaultOutputDevice)
    guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &dev) == noErr, dev != 0 else { return "none" }
    var rate = Float64(0)
    var rsize = UInt32(MemoryLayout<Float64>.size)
    var raddr = globalAddress(kAudioDevicePropertyNominalSampleRate)
    _ = AudioObjectGetPropertyData(dev, &raddr, 0, nil, &rsize, &rate)
    return "\(deviceName(dev)) \(Int(rate)) Hz"
}

/// A device's name (what the console shows as "device").
func deviceName(_ dev: AudioDeviceID) -> String {
    guard dev != 0 else { return "" }
    var name: Unmanaged<CFString>?
    var nsize = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    var naddr = globalAddress(kAudioObjectPropertyName)
    guard AudioObjectGetPropertyData(dev, &naddr, 0, nil, &nsize, &name) == noErr, let cf = name else { return "" }
    return cf.takeRetainedValue() as String
}

/// One microphone, opened by hand: a single AUHAL unit with its OUTPUT side
/// switched off, so the only device that runs is the chosen input. This is
/// why AVAudioEngine is not used here: on the Mac its graph is always driven
/// by the default OUTPUT device too, and when that is not the input's device
/// it pairs the two in a private aggregate device — which reconfigures the
/// AirPods every time the switch goes on, and whatever they are playing stops
/// or turns coarse (background music pauses and drops in quality). Here the
/// AirPods are never touched: they keep playing at
/// full quality while the laptop's microphone listens.
final class Input {
    private var unit: AudioUnit?
    /// What the callback delivers: the device's own rate and channels, Float32.
    let format: AVAudioFormat
    private let handler: (AVAudioPCMBuffer) -> Void

    init?(device: AudioDeviceID, handler: @escaping (AVAudioPCMBuffer) -> Void) {
        self.handler = handler
        var desc = AudioComponentDescription(componentType: kAudioUnitType_Output, componentSubType: kAudioUnitSubType_HALOutput,
                                             componentManufacturer: kAudioUnitManufacturer_Apple, componentFlags: 0, componentFlagsMask: 0)
        guard let comp = AudioComponentFindNext(nil, &desc) else { return nil }
        var u: AudioUnit?
        guard AudioComponentInstanceNew(comp, &u) == noErr, let u else { return nil }
        var on: UInt32 = 1, off: UInt32 = 0
        let four = UInt32(MemoryLayout<UInt32>.size)
        // Element 1 = input, on. Element 0 = output, OFF: no output device is ever opened.
        guard AudioUnitSetProperty(u, kAudioOutputUnitProperty_EnableIO, kAudioUnitScope_Input, 1, &on, four) == noErr,
              AudioUnitSetProperty(u, kAudioOutputUnitProperty_EnableIO, kAudioUnitScope_Output, 0, &off, four) == noErr
        else { AudioComponentInstanceDispose(u); return nil }
        var dev = device
        guard AudioUnitSetProperty(u, kAudioOutputUnitProperty_CurrentDevice, kAudioUnitScope_Global, 0,
                                   &dev, UInt32(MemoryLayout<AudioDeviceID>.size)) == noErr
        else { AudioComponentInstanceDispose(u); return nil }
        // The device's hardware format (rate, channels) on the input side of
        // element 1; ask for the same rate and channels as Float32 on its
        // output side, which is what the callback reads. AUHAL does not
        // resample on input, so the rate must be the device's own.
        var hw = AudioStreamBasicDescription()
        var hsize = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
        guard AudioUnitGetProperty(u, kAudioUnitProperty_StreamFormat, kAudioUnitScope_Input, 1, &hw, &hsize) == noErr,
              hw.mSampleRate > 0, hw.mChannelsPerFrame > 0,
              let fmt = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: hw.mSampleRate,
                                      channels: hw.mChannelsPerFrame, interleaved: false)
        else { AudioComponentInstanceDispose(u); return nil }
        var asbd = fmt.streamDescription.pointee
        guard AudioUnitSetProperty(u, kAudioUnitProperty_StreamFormat, kAudioUnitScope_Output, 1, &asbd, hsize) == noErr
        else { AudioComponentInstanceDispose(u); return nil }
        format = fmt
        unit = u
        var cb = AURenderCallbackStruct(inputProc: { refCon, flags, ts, _, frames, _ in
            Unmanaged<Input>.fromOpaque(refCon).takeUnretainedValue().render(flags, ts, frames)
        }, inputProcRefCon: Unmanaged.passUnretained(self).toOpaque())
        guard AudioUnitSetProperty(u, kAudioOutputUnitProperty_SetInputCallback, kAudioUnitScope_Global, 0,
                                   &cb, UInt32(MemoryLayout<AURenderCallbackStruct>.size)) == noErr,
              AudioUnitInitialize(u) == noErr
        else { AudioComponentInstanceDispose(u); unit = nil; return nil }
    }

    private func render(_ flags: UnsafeMutablePointer<AudioUnitRenderActionFlags>, _ ts: UnsafePointer<AudioTimeStamp>,
                        _ frames: UInt32) -> OSStatus {
        guard let unit, let buf = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: frames) else { return noErr }
        buf.frameLength = frames
        let st = AudioUnitRender(unit, flags, ts, 1, frames, buf.mutableAudioBufferList)
        if st == noErr { handler(buf) }
        return st
    }

    func start() -> Bool { unit.map { AudioOutputUnitStart($0) == noErr } ?? false }

    func stop() {
        guard let u = unit else { return }
        unit = nil
        AudioOutputUnitStop(u)
        AudioUnitUninitialize(u)
        AudioComponentInstanceDispose(u)
    }
}

/// The open microphone with its feeder, reopened when the device changes its
/// rate underneath: the AirPods do exactly that a second or two after their
/// mic opens (48 kHz → 24 kHz, the two-way link — and that IS the change
/// heard in the music, not anything this helper does), and an AUHAL client
/// fixed at the old rate hears nothing from then on.
final class Listening {
    private let q = DispatchQueue(label: "life.mic.io")
    private let dev: AudioDeviceID
    private let analyzerFormat: AVAudioFormat
    private let sink: AsyncStream<AnalyzerInput>.Continuation
    private var input: Input?
    private var feeder: Feeder?
    private var closed = false
    private var rateAddr = globalAddress(kAudioDevicePropertyNominalSampleRate)
    private var listener: AudioObjectPropertyListenerBlock?

    init?(device: AudioDeviceID, analyzer: AVAudioFormat, sink: AsyncStream<AnalyzerInput>.Continuation) {
        dev = device; analyzerFormat = analyzer; self.sink = sink
        guard open() else { return nil }
    }

    var format: AVAudioFormat? { q.sync { input?.format } }

    private func open() -> Bool {
        var fd: Feeder?
        guard let inp = Input(device: dev, handler: { b in fd?.handle(b) }) else { return false }
        fd = Feeder(input: inp.format, analyzer: analyzerFormat, sink: sink)
        input = inp; feeder = fd
        return true
    }

    /// Starts the input and watches its rate.
    func start() -> Bool {
        q.sync {
            guard let inp = input, inp.start() else { return false }
            let block: AudioObjectPropertyListenerBlock = { [weak self] _, _ in self?.reopen() }
            listener = block
            AudioObjectAddPropertyListenerBlock(dev, &rateAddr, q, block)
            return true
        }
    }

    // On q (the listener's queue).
    private func reopen() {
        guard !closed else { return }
        input?.stop(); feeder?.close()
        input = nil; feeder = nil
        guard open(), let inp = input, inp.start() else {
            emit(["k": "status", "text": "input \(deviceName(dev)) changed and could not be reopened"])
            return
        }
        emit(["k": "log", "text": "input \(deviceName(dev)) now \(Int(inp.format.sampleRate)) Hz × \(inp.format.channelCount); reopened"])
    }

    func stop() {
        q.sync {
            closed = true
            if let block = listener { AudioObjectRemovePropertyListenerBlock(dev, &rateAddr, q, block); listener = nil }
            input?.stop(); feeder?.close()
            input = nil; feeder = nil
        }
    }
}

/// Runs on the audio thread: converts each buffer to the analyzer's format
/// and feeds it. Same shape as the phone's AudioWriter, without the file.
final class Feeder: @unchecked Sendable {
    private let converter: AVAudioConverter?
    private let format: AVAudioFormat
    private let sink: AsyncStream<AnalyzerInput>.Continuation
    private let lock = NSLock()
    private var closed = false

    init(input: AVAudioFormat, analyzer: AVAudioFormat, sink: AsyncStream<AnalyzerInput>.Continuation) {
        converter = input == analyzer ? nil : AVAudioConverter(from: input, to: analyzer)
        format = analyzer
        self.sink = sink
    }

    private var levelAt = Date()

    func handle(_ buffer: AVAudioPCMBuffer) {
        lock.lock(); defer { lock.unlock() }
        guard !closed else { return }
        // Once a second, how loud the input is (a probe's proof the mic hears).
        if Date().timeIntervalSince(levelAt) >= 1, let ch = buffer.floatChannelData, buffer.frameLength > 0 {
            levelAt = Date()
            var sum: Float = 0
            for i in 0..<Int(buffer.frameLength) { sum += ch[0][i] * ch[0][i] }
            emit(["k": "level", "rms": Double((sum / Float(buffer.frameLength)).squareRoot()), "frames": Int(buffer.frameLength)])
        }
        guard let converter else { sink.yield(AnalyzerInput(buffer: buffer)); return }
        let ratio = format.sampleRate / buffer.format.sampleRate
        let cap = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 16
        guard let out = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: cap) else { return }
        var fed = false
        var err: NSError?
        converter.convert(to: out, error: &err) { _, status in
            if fed { status.pointee = .noDataNow; return nil }
            fed = true; status.pointee = .haveData; return buffer
        }
        if err == nil, out.frameLength > 0 { sink.yield(AnalyzerInput(buffer: out)) }
    }

    func close() { lock.lock(); closed = true; lock.unlock() }
}

/// The switch, as seen from stdin: "go" opens the input, EOF closes it. The
/// reader is a plain thread; run() waits on it without blocking a pool thread.
final class Switch: @unchecked Sendable {
    private let lock = NSLock()
    private var went = false
    private var eof = false
    private var goWaiter: (() -> Void)?
    private var onStop: (() -> Void)?

    func start() {
        let t = Thread { [self] in
            var went = false
            while let line = readLine() {
                if line.trimmingCharacters(in: .whitespaces) == "go", !went { went = true; go() }
            }
            if !went { exit(0) } // never opened the microphone
            closed()
        }
        t.start()
    }

    private func go() {
        lock.lock(); went = true; let w = goWaiter; goWaiter = nil; lock.unlock()
        w?()
    }

    func waitGo() async {
        await withCheckedContinuation { (c: CheckedContinuation<Void, Never>) in
            lock.lock()
            if went { lock.unlock(); c.resume(); return }
            goWaiter = { c.resume() }
            lock.unlock()
        }
    }

    /// The stop work runs on its own thread, whichever side arrives second.
    func stop(_ f: @escaping () -> Void) {
        lock.lock(); onStop = f; let e = eof; lock.unlock()
        if e { Thread { f() }.start() }
    }

    private func closed() {
        lock.lock(); eof = true; let f = onStop; lock.unlock()
        f?()
    }
}

func seconds(_ t: CMTime) -> Double { t.isValid ? CMTimeGetSeconds(t) : 0 }

@available(macOS 26, *)
func run() async {
    let sw = Switch()
    sw.start()

    // 1. Warm: the model and the analyzer, before any tap. No microphone yet.
    emit(["k": "status", "text": "loading the speech model"])
    let locale = Locale(identifier: "en_US")
    let transcriber = SpeechTranscriber(locale: locale, transcriptionOptions: [],
                                        reportingOptions: [.volatileResults], attributeOptions: [.audioTimeRange])
    do {
        if await AssetInventory.status(forModules: [transcriber]) != .installed,
           let req = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
            emit(["k": "status", "text": "downloading the on-device speech model (once)"])
            try await req.downloadAndInstall()
        }
    } catch {
        fail("speech model: \(error.localizedDescription)", 3)
    }
    let analyzer = SpeechAnalyzer(modules: [transcriber])
    guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [transcriber]) else {
        fail("no compatible audio format for the speech model", 3)
    }
    do {
        try await analyzer.prepareToAnalyze(in: format)
    } catch {
        fail("speech model: \(error.localizedDescription)", 3)
    }
    emit(["k": "warm"])

    // 2. The owner's tap. Until it comes, nothing here touches the input.
    await sw.waitGo()
    // The grant is remembered by the signed identity; only a first-ever run
    // shows the prompt, attributed to the hub. Granted = no status line at
    // all (a permission line on every tap reads as something wrong).
    if AVCaptureDevice.authorizationStatus(for: .audio) != .authorized {
        emit(["k": "status", "text": "asking for microphone permission"])
        let ok = await AVCaptureDevice.requestAccess(for: .audio)
        if !ok { fail("microphone permission is off for Life — System Settings › Privacy & Security › Microphone", 2) }
    }

    // LIFE_MIC=builtin (hub.json mic_input) asks for the Mac's own microphone;
    // anything else keeps the system's pick (the AirPods mic when in).
    let wantBuiltIn = ProcessInfo.processInfo.environment["LIFE_MIC"] == "builtin"
    let dev = (wantBuiltIn ? builtInInput() : nil) ?? defaultInput()
    if dev == 0 { fail("no microphone input available", 3) }
    let (stream, cont) = AsyncStream<AnalyzerInput>.makeStream()
    guard let input = Listening(device: dev, analyzer: format, sink: cont), let inputFormat = input.format else {
        fail("could not open \(deviceName(dev))", 3)
    }
    // Device and rate lines are for hub.log, never the button: the button
    // says Listening, the log says which device at what rate.
    emit(["k": "log", "text": "input \(deviceName(dev)) \(Int(inputFormat.sampleRate)) Hz × \(inputFormat.channelCount) → analyzer \(Int(format.sampleRate)) Hz × \(format.channelCount)"])
    do {
        try await analyzer.start(inputSequence: stream)
    } catch {
        fail("could not start: \(error.localizedDescription)", 3)
    }
    if !input.start() { fail("could not start \(deviceName(dev))", 3) }
    let started = Date()
    emit(["k": "ready", "at": Int(started.timeIntervalSince1970 * 1000), "device": deviceName(dev),
          "rate": Int(inputFormat.sampleRate)])
    emit(["k": "log", "text": "output now \(outputNow())"])

    // 3. The switch-off: keep hearing for the tail, close the input, then
    // finalize — the analyzer turns what is still volatile into a last final
    // and the results loop below ends. If it has not within finishSeconds,
    // exit anyway: off means off.
    sw.stop {
        Thread.sleep(forTimeInterval: tailSeconds)
        input.stop()
        cont.finish()
        emit(["k": "log", "text": "input closed; output now \(outputNow())"])
        Task { try? await analyzer.finalizeAndFinishThroughEndOfInput() }
        Thread.sleep(forTimeInterval: finishSeconds)
        exit(0)
    }

    do {
        for try await r in transcriber.results {
            let text = String(r.text.characters)
            if !r.isFinal {
                emit(["k": "partial", "text": text])
                continue
            }
            var words: [[Any]] = []
            for run in r.text.runs {
                let w = String(r.text[run.range].characters).trimmingCharacters(in: .whitespacesAndNewlines)
                guard !w.isEmpty, let tr = run.audioTimeRange else { continue }
                words.append([seconds(tr.start), seconds(tr.end), w])
            }
            emit(["k": "final", "text": text, "t0": seconds(r.range.start), "t1": seconds(r.range.end), "words": words])
        }
    } catch {
        fail("transcription stopped: \(error.localizedDescription)", 4)
    }
    exit(0)
}

if #available(macOS 26, *) {
    Task { await run() }
    dispatchMain()
} else {
    fail("needs macOS 26 (SpeechAnalyzer)", 3)
}
