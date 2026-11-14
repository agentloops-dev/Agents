// Probe for spike #4945: a Core Audio process tap as a microphone, with no driver.
//
//   tapmic devices                       list the audio devices (id, uid, in/out channels)
//   tapmic mic <pid> <seconds> <out.wav> [--vp]
//       Taps the sound of process <pid> (muted on the speakers), makes a public aggregate
//       device of it ("Puppet Mic"), and records that device as a microphone with
//       AVAudioEngine, its input unit pointed at the device; --vp turns voice processing
//       on first, as voice mode does.
//   tapmic hear <pid> <seconds> <out.wav>
//       Records what process <pid> plays (muted), for Whisper.
//
// Throwaway: it proves the option, it is not the puppet.
import AVFoundation
import CoreAudio
import Foundation

nonisolated(unsafe) var restoreInput: (() -> Void)?
func fail(_ s: String) -> Never { restoreInput?(); FileHandle.standardError.write((s + "\n").data(using: .utf8)!); exit(1) }
func check(_ st: OSStatus, _ what: String) { if st != noErr { fail("\(what): OSStatus \(st)") } }

func prop<T>(_ obj: AudioObjectID, _ sel: AudioObjectPropertySelector, _ scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal, _ value: T) -> T {
    var addr = AudioObjectPropertyAddress(mSelector: sel, mScope: scope, mElement: kAudioObjectPropertyElementMain)
    var v = value
    var size = UInt32(MemoryLayout<T>.size)
    _ = AudioObjectGetPropertyData(obj, &addr, 0, nil, &size, &v)
    return v
}

func devices() -> [AudioObjectID] {
    var addr = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyDevices, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    var size: UInt32 = 0
    AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size)
    var ids = [AudioObjectID](repeating: 0, count: Int(size) / 4)
    AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &ids)
    return ids
}

func string(_ obj: AudioObjectID, _ sel: AudioObjectPropertySelector) -> String {
    prop(obj, sel, kAudioObjectPropertyScopeGlobal, "" as CFString) as String
}

func channels(_ dev: AudioObjectID, _ scope: AudioObjectPropertyScope) -> Int {
    var addr = AudioObjectPropertyAddress(mSelector: kAudioDevicePropertyStreamConfiguration, mScope: scope, mElement: kAudioObjectPropertyElementMain)
    var size: UInt32 = 0
    guard AudioObjectGetPropertyDataSize(dev, &addr, 0, nil, &size) == noErr, size > 0 else { return 0 }
    let raw = UnsafeMutableRawPointer.allocate(byteCount: Int(size), alignment: 16)
    defer { raw.deallocate() }
    guard AudioObjectGetPropertyData(dev, &addr, 0, nil, &size, raw) == noErr else { return 0 }
    let list = UnsafeMutableAudioBufferListPointer(raw.assumingMemoryBound(to: AudioBufferList.self))
    return list.reduce(0) { $0 + Int($1.mNumberChannels) }
}

func processObject(_ pid: pid_t) -> AudioObjectID {
    var addr = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyTranslatePIDToProcessObject, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    var p = pid
    var obj = AudioObjectID(kAudioObjectUnknown)
    var size = UInt32(MemoryLayout<AudioObjectID>.size)
    check(AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, UInt32(MemoryLayout<pid_t>.size), &p, &size, &obj), "pid → process object")
    if obj == kAudioObjectUnknown { fail("pid \(pid) has no audio process object (is it playing?)") }
    return obj
}

/// A muted tap of `pid` and a public aggregate device that has it as its input.
func makeTapDevice(_ pid: pid_t, name: String) -> (tap: AudioObjectID, device: AudioObjectID) {
    let desc = CATapDescription(stereoMixdownOfProcesses: [processObject(pid)])
    desc.muteBehavior = .muted
    desc.isPrivate = false
    desc.name = name
    var tap = AudioObjectID(kAudioObjectUnknown)
    check(AudioHardwareCreateProcessTap(desc, &tap), "create process tap")
    let tapUID = desc.uuid.uuidString
    let uid = "dev.agentloops.puppet.\(UUID().uuidString)"
    let dict: [String: Any] = [
        kAudioAggregateDeviceNameKey: name,
        kAudioAggregateDeviceUIDKey: uid,
        kAudioAggregateDeviceIsPrivateKey: false,
        kAudioAggregateDeviceTapAutoStartKey: true,
        kAudioAggregateDeviceSubDeviceListKey: ProcessInfo.processInfo.environment["CLOCK_UID"].map { [[kAudioSubDeviceUIDKey: $0]] } ?? [],
        kAudioAggregateDeviceMainSubDeviceKey: ProcessInfo.processInfo.environment["CLOCK_UID"] ?? "",
        kAudioAggregateDeviceTapListKey: [[kAudioSubTapUIDKey: tapUID, kAudioSubTapDriftCompensationKey: true]],
    ]
    var dev = AudioObjectID(kAudioObjectUnknown)
    check(AudioHardwareCreateAggregateDevice(dict as CFDictionary, &dev), "create aggregate device")
    return (tap, dev)
}

func record(device: AudioObjectID, seconds: Double, to path: String, voiceProcessing: Bool) {
    let engine = AVAudioEngine()
    let input = engine.inputNode
    var dev = device
    guard let unit = input.audioUnit else { fail("no input audio unit") }
    if voiceProcessing {
        do { try input.setVoiceProcessingEnabled(true) } catch { fail("voice processing: \(error)") }
    }
    var st: OSStatus = noErr
    if !CommandLine.arguments.contains("--default") {
        do { try input.auAudioUnit.setDeviceID(dev) } catch { st = OSStatus((error as NSError).code) }
    }
    _ = unit
    if !voiceProcessing { engine.reset() }
    print("set input unit device → \(st)")
    let format = input.outputFormat(forBus: 0)
    print("input format: \(format)")
    guard format.sampleRate > 0, format.channelCount > 0 else { fail("input format empty") }
    let file: AVAudioFile
    do { file = try AVAudioFile(forWriting: URL(fileURLWithPath: path), settings: format.settings) } catch { fail("file: \(error)") }
    var peak: Float = 0
    input.installTap(onBus: 0, bufferSize: 1024, format: format) { buf, _ in
        try? file.write(from: buf)
        if let d = buf.floatChannelData { for i in 0..<Int(buf.frameLength) { peak = max(peak, abs(d[0][i])) } }
    }
    if voiceProcessing { engine.connect(engine.mainMixerNode, to: engine.outputNode, format: nil) }
    do { engine.prepare(); try engine.start() } catch { fail("engine start: \(error)") }
    Thread.sleep(forTimeInterval: seconds)
    engine.stop()
    print("recorded \(path), peak \(peak)")
}

/// Reads the device with an IOProc, no AVAudioEngine: is the tap giving sound at all?
func rawPeak(device: AudioObjectID, seconds: Double, to path: String? = nil) {
    final class Box: @unchecked Sendable { var peak: Float = 0; var frames = 0; var out: FileHandle? }
    let box = Box()
    if let path { FileManager.default.createFile(atPath: path, contents: nil); box.out = FileHandle(forWritingAtPath: path) }
    var proc: AudioDeviceIOProcID?
    check(AudioDeviceCreateIOProcIDWithBlock(&proc, device, nil) { _, input, _, _, _ in
        for buf in UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: input)) {
            guard let d = buf.mData?.assumingMemoryBound(to: Float.self) else { continue }
            let n = Int(buf.mDataByteSize) / 4
            box.frames += n
            box.out?.write(Data(bytes: d, count: n * 4))
            for i in 0..<n { box.peak = max(box.peak, abs(d[i])) }
        }
    }, "ioproc")
    check(AudioDeviceStart(device, proc), "device start")
    Thread.sleep(forTimeInterval: seconds)
    AudioDeviceStop(device, proc)
    try? box.out?.close()
    print("raw: \(box.frames) samples at \(prop(device, kAudioDevicePropertyNominalSampleRate, kAudioObjectPropertyScopeGlobal, Float64(0))) Hz, peak \(box.peak)")
}

func defaultInput() -> AudioObjectID {
    prop(AudioObjectID(kAudioObjectSystemObject), kAudioHardwarePropertyDefaultInputDevice, kAudioObjectPropertyScopeGlobal, AudioObjectID(0))
}
func setDefaultInput(_ d: AudioObjectID) {
    var addr = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyDefaultInputDevice, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    var v = d
    check(AudioObjectSetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, UInt32(MemoryLayout<AudioObjectID>.size), &v), "set default input")
}
/// Whether any process has its microphone running (Alex's own app, a call): then no swap.
func inUse(_ d: AudioObjectID) -> UInt32 {
    var addr = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyProcessObjectList, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    var size: UInt32 = 0
    AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size)
    var ids = [AudioObjectID](repeating: 0, count: Int(size) / 4)
    AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &ids)
    return ids.contains { prop($0, kAudioProcessPropertyIsRunningInput, kAudioObjectPropertyScopeGlobal, UInt32(0)) != 0 } ? 1 : 0
}

/// The voice-processing unit itself, not AVAudioEngine: will it take another input device?
func vpioTakes(_ dev: AudioObjectID) {
    var desc = AudioComponentDescription(componentType: kAudioUnitType_Output, componentSubType: kAudioUnitSubType_VoiceProcessingIO,
                                         componentManufacturer: kAudioUnitManufacturer_Apple, componentFlags: 0, componentFlagsMask: 0)
    guard let comp = AudioComponentFindNext(nil, &desc) else { fail("no VPIO") }
    var unit: AudioUnit?
    check(AudioComponentInstanceNew(comp, &unit), "VPIO new")
    var d = dev
    for (scope, el) in [(kAudioUnitScope_Global, UInt32(0)), (kAudioUnitScope_Global, UInt32(1)), (kAudioUnitScope_Input, UInt32(1)), (kAudioUnitScope_Output, UInt32(1))] {
        let st = AudioUnitSetProperty(unit!, kAudioOutputUnitProperty_CurrentDevice, scope, el, &d, 4)
        var got: AudioObjectID = 0; var z = UInt32(4)
        AudioUnitGetProperty(unit!, kAudioOutputUnitProperty_CurrentDevice, scope, el, &got, &z)
        print("VPIO CurrentDevice scope \(scope) element \(el): set \(st), now \(got)")
    }
    print("VPIO initialize: \(AudioUnitInitialize(unit!))")
    AudioComponentInstanceDispose(unit!)
}

let args = CommandLine.arguments
switch args.count > 1 ? args[1] : "" {
case "clean":
    // Every device and tap this probe made, also those a failed run left.
    for d in devices() where string(d, kAudioDevicePropertyDeviceUID).hasPrefix("dev.agentloops.puppet.") {
        print("destroy \(d): \(AudioHardwareDestroyAggregateDevice(d))")
    }
case "vpio" where args.count >= 3:
    vpioTakes(AudioObjectID(args[2])!)
case "devices":
    for d in devices() {
        print("\(d)\t\(string(d, kAudioDevicePropertyDeviceUID))\t\(string(d, kAudioObjectPropertyName))\tin \(channels(d, kAudioObjectPropertyScopeInput))\tout \(channels(d, kAudioObjectPropertyScopeOutput))")
    }
case "mic" where args.count >= 5, "hear" where args.count >= 5:
    let pid = pid_t(args[2])!, seconds = Double(args[3])!, out = args[4]
    let (tap, dev) = makeTapDevice(pid, name: "Puppet Mic")
    let restore = restoreInput
    restoreInput = { restore?(); AudioHardwareDestroyAggregateDevice(dev); AudioHardwareDestroyProcessTap(tap) }
    print("tap \(tap), aggregate device \(dev) '\(string(dev, kAudioDevicePropertyDeviceUID))' in \(channels(dev, kAudioObjectPropertyScopeInput))")
    let was = defaultInput()
    if args.contains("--default") {
        if inUse(was) != 0 { AudioHardwareDestroyAggregateDevice(dev); AudioHardwareDestroyProcessTap(tap); fail("default input in use: not swapping") }
        setDefaultInput(dev)
        let cleanUp = restoreInput
        restoreInput = { setDefaultInput(was); cleanUp?() }
        signal(SIGINT) { _ in exit(2) }
        atexit { }
        print("can be default input: \(prop(dev, kAudioDevicePropertyDeviceCanBeDefaultDevice, kAudioObjectPropertyScopeInput, UInt32(9))); default input \(was) → \(defaultInput())")
        if defaultInput() != dev { fail("the default input did not change: not recording the room") }
    }
    defer { if args.contains("--default") { setDefaultInput(was); print("default input back to \(defaultInput())") } }
    if args.contains("--hold") { print("holding \(dev)"); fflush(stdout); Thread.sleep(forTimeInterval: seconds) } else if args.contains("--vpio") { vpioTakes(dev) } else if args.contains("--raw") || args[1] == "hear" { rawPeak(device: dev, seconds: seconds, to: args[1] == "hear" ? out : nil) } else {
        record(device: dev, seconds: seconds, to: out, voiceProcessing: args.contains("--vp"))
    }
    AudioHardwareDestroyAggregateDevice(dev)
    AudioHardwareDestroyProcessTap(tap)
default:
    fail("usage: tapmic devices | mic <pid> <s> <out.wav> [--vp] | hear <pid> <s> <out.wav>")
}
