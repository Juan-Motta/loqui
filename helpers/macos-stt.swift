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

// Apple supports a fixed set of regional locales on-device; match exactly on
// BCP-47, else fall back to any locale of the same base language (es/en).
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
  }

  engine.prepare()
  try engine.start()
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
