// Native macOS STT helper (loqui "macos" provider). Live on-device microphone
// transcription via Apple SpeechAnalyzer + SpeechTranscriber (macOS 26+): free,
// offline, private. Spawned by main with the target locale as argv[1]; main kills
// it (SIGTERM) to stop. Single language per session (Apple has no continuous LID).
//
//   build: npm run build:stt   (scripts/build-macos-stt.sh)
//   run:   resources/native/macos-stt es-CO   (speak; SIGINT/SIGTERM to stop)
//
// Emits one JSON object per line, matching src/shared/sttHelperProtocol.ts:
// {"type":"started"} / {"type":"partial","text":..,"language":..}
// / {"type":"final","text":..,"language":..} / {"type":"canceled","error":..}.
import Foundation
import Speech
import AVFoundation

func emit(_ obj: [String: Any]) {
  guard let data = try? JSONSerialization.data(withJSONObject: obj),
        let s = String(data: data, encoding: .utf8) else { return }
  print(s)
  fflush(stdout)
}

func bcp47(_ l: Locale) -> String { l.identifier(.bcp47).lowercased() }

// MICROPHONE LEVEL, and it is not decoration.
//
// This helper opens the microphone itself, so the host never sees the audio and cannot meter it —
// the overlay pill has no bars unless the level is reported from in here. Without them a dictation
// that hears nothing looks exactly like one that is working, which is half of why this engine was
// believed dead for weeks: its only visible difference from whisper was a peak of 0.00 in the log,
// and that was the meter missing rather than the audio.
//
// FOUR THINGS A NAIVE VERSION GOT WRONG, all found by a cross-engine review:
//
//  1. NOTHING BLOCKING RUNS ON THE AUDIO THREAD. The tap is realtime; JSON encoding, `print` and
//     `fflush` are not. If the pipe fills or the host pauses, writing from the tap stalls it and
//     DROPS CAPTURED AUDIO — the engine would then mis-transcribe because of its own meter. The tap
//     only updates a number under a lock; a serial queue does the emitting.
//  2. THE MAXIMUM IS ACCUMULATED, not sampled. Callbacks land about every 85 ms (4096 frames at
//     48 kHz) and the throttle is 100 ms, so every other buffer is skipped — a short sound that fell
//     entirely inside a skipped one would vanish, and the log would still say 0.00.
//  3. RMS ×4, NOT RAW PEAK, because that is what the host computes for the cloud providers
//     (audio/pcm.go). With raw peak the same voice reads ~0.10 here and ~0.28 there, so neither the
//     bars nor the "was there audio" threshold would mean the same thing across engines.
//  4. THE BUFFER LAYOUT IS NOT ASSUMED. An interleaved input has ONE channel pointer whose samples
//     alternate channels, so indexing by frame walks into the other channel and reads most of the
//     buffer wrong; and a non-Float32 buffer has no floatChannelData at all, which would report
//     silence while the engine transcribed happily from an external interface.
final class LevelReporter {
  private let lock = NSLock()
  private var pending: Double = 0
  private let queue = DispatchQueue(label: "loqui.level")
  private var timer: DispatchSourceTimer?

  func start() {
    let t = DispatchSource.makeTimerSource(queue: queue)
    t.schedule(deadline: .now() + .milliseconds(100), repeating: .milliseconds(100))
    t.setEventHandler { [weak self] in
      guard let self else { return }
      self.lock.lock()
      let value = self.pending
      self.pending = 0
      self.lock.unlock()
      emit(["type": "level", "value": value])
    }
    t.resume()
    timer = t
  }

  func stop() {
    timer?.cancel()
    timer = nil
  }

  /// Called from the realtime tap. Takes a lock and returns — no allocation, no I/O.
  func offer(_ level: Double) {
    lock.lock()
    if level > pending { pending = level }
    lock.unlock()
  }
}
let levelReporter = LevelReporter()

// rmsLevel matches internal/audio/pcm.go's Level: RMS with the ×4 gain the Electron meter applied,
// so speech reaches a lively range instead of hovering near zero.
func rmsLevel(_ buffer: AVAudioPCMBuffer) -> Double {
  let frames = Int(buffer.frameLength)
  guard frames > 0 else { return 0 }
  let channels = Int(buffer.format.channelCount)
  var sum = 0.0
  var count = 0

  if let f32 = buffer.floatChannelData {
    // Interleaved buffers expose ONE pointer with the channels woven together; deinterleaved ones
    // expose one per channel. `stride` is what tells them apart, and ignoring it reads the wrong
    // samples on any interleaved input.
    let stride = buffer.stride
    if buffer.format.isInterleaved {
      let p = f32[0]
      for i in 0..<(frames * channels) {
        let v = Double(p[i * (stride / max(channels, 1))])
        sum += v * v
        count += 1
      }
    } else {
      for c in 0..<channels {
        let p = f32[c]
        for i in 0..<frames {
          let v = Double(p[i * stride])
          sum += v * v
          count += 1
        }
      }
    }
  } else if let i16 = buffer.int16ChannelData {
    // A device that hands over 16-bit integers is not silence, and reporting 0 for it was the bug.
    let stride = buffer.stride
    let scale = 1.0 / 32768.0
    for c in 0..<(buffer.format.isInterleaved ? 1 : channels) {
      let p = i16[c]
      let total = buffer.format.isInterleaved ? frames * channels : frames
      for i in 0..<total {
        let v = Double(p[i * stride]) * scale
        sum += v * v
        count += 1
      }
    }
  } else {
    return 0
  }

  guard count > 0 else { return 0 }
  let rms = (sum / Double(count)).squareRoot()
  return min(rms * 4, 1)
}

func matchLocale(_ wanted: Locale, in locales: [Locale]) -> Locale? {
  if let hit = locales.first(where: { bcp47($0) == bcp47(wanted) }) { return hit }
  let base = wanted.language.languageCode?.identifier.lowercased()
  return locales.first(where: { $0.language.languageCode?.identifier.lowercased() == base })
}

func run(locale: Locale) async throws {
  let supported = await SpeechTranscriber.supportedLocales
  guard let match = matchLocale(locale, in: supported) else {
    emit([
      "type": "canceled",
      "error": "locale not supported: \(locale.identifier)",
      "supported": supported.map { $0.identifier(.bcp47) }.sorted(),
    ])
    return
  }
  emit(["type": "info", "using": match.identifier(.bcp47)])

  let transcriber = SpeechTranscriber(
    locale: match,
    transcriptionOptions: [],
    reportingOptions: [.volatileResults],
    attributeOptions: []
  )

  let installed = await SpeechTranscriber.installedLocales
  if !installed.contains(where: { bcp47($0) == bcp47(match) }) {
    emit(["type": "info", "msg": "downloading language model for \(match.identifier(.bcp47))…"])
    if let req = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
      try await req.downloadAndInstall()
    }
  }

  let analyzer = SpeechAnalyzer(modules: [transcriber])
  guard let analyzerFormat = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [transcriber]) else {
    emit(["type": "canceled", "error": "no compatible audio format"])
    return
  }

  let (inputStream, continuation) = AsyncStream<AnalyzerInput>.makeStream()

  let engine = AVAudioEngine()
  let inputNode = engine.inputNode
  let inputFormat = inputNode.outputFormat(forBus: 0)
  guard let converter = AVAudioConverter(from: inputFormat, to: analyzerFormat) else {
    emit(["type": "canceled", "error": "cannot build audio converter"])
    return
  }

  inputNode.installTap(onBus: 0, bufferSize: 4096, format: inputFormat) { buffer, _ in
    let ratio = analyzerFormat.sampleRate / inputFormat.sampleRate
    let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 1024
    guard let out = AVAudioPCMBuffer(pcmFormat: analyzerFormat, frameCapacity: capacity) else { return }
    var consumed = false
    var convErr: NSError?
    converter.convert(to: out, error: &convErr) { _, status in
      if consumed {
        status.pointee = .noDataNow
        return nil
      }
      consumed = true
      status.pointee = .haveData
      return buffer
    }
    if convErr == nil {
      continuation.yield(AnalyzerInput(buffer: out))
    }
    // Measured on the INPUT buffer, before conversion: that is the microphone as it arrived, and it
    // is reported even when the conversion fails — a converter problem is not silence, and saying
    // "0.00" there would send the user to check their microphone.
    levelReporter.offer(rmsLevel(buffer))
  }

  engine.prepare()
  try engine.start()
  levelReporter.start()
  emit(["type": "started"])

  try await analyzer.start(inputSequence: inputStream)

  for try await result in transcriber.results {
    let text = String(result.text.characters)
    emit([
      "type": result.isFinal ? "final" : "partial",
      "text": text,
      "language": match.identifier(.bcp47),
    ])
  }
}

let localeId = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "es-CO"
let locale = Locale(identifier: localeId)

let sem = DispatchSemaphore(value: 0)
Task {
  let authorized = await withCheckedContinuation { (cont: CheckedContinuation<Bool, Never>) in
    SFSpeechRecognizer.requestAuthorization { status in
      cont.resume(returning: status == .authorized)
    }
  }
  guard authorized else {
    emit(["type": "canceled", "error": "speech authorization denied"])
    sem.signal()
    return
  }
  do {
    try await run(locale: locale)
  } catch {
    emit(["type": "canceled", "error": "\(error)"])
  }
  sem.signal()
}
sem.wait()
