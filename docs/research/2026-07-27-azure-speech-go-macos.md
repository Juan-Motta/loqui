# Research — Azure Speech from Go on macOS arm64 (risk spike)

- **Date:** 2026-07-27
- **Question:** can a Go backend reproduce Loqui's Azure Speech route (universal v2
  endpoint + continuous LID + continuous recognition) on macOS arm64, with audio
  arriving over a push stream instead of the SDK's microphone?
- **Why it is the first question:** it is the port's only dependency with no pure Go
  equivalent. Everything else (WebSockets, native helpers, UI) is mechanical. If this
  does not work, the port's architecture changes.
- **Verdict: VIABLE — verified by running it.**

## Context: why there was doubt

The official binding [`Microsoft/cognitive-services-speech-sdk-go`](https://github.com/microsoft/cognitive-services-speech-sdk-go)
is cgo over the Speech SDK's native library. The installation documentation covers
Linux and Windows; macOS is undocumented and there are open issues reporting exactly
that:

- [#66](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/66) — `fatal error: 'speechapi_c_error.h' file not found` on Mac
- [#72](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/72) — "Finish installation documentation for macs"
- [#102](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/102) — "Unable to run on Mac Again!"

The hypothesis in those issues is that on macOS the SDK ships as
`MicrosoftCognitiveServicesSpeech.xcframework` (meant for Xcode/ObjC) and that there
would therefore be no C headers for cgo.

## Finding 1 — the xcframework DOES ship the C headers

`https://aka.ms/csspeech/macosbinary` → `MicrosoftCognitiveServicesSpeech-MacOSXCFramework-1.51.1.zip`
(3.8 MB compressed). Inside
`MicrosoftCognitiveServicesSpeech.xcframework/macos-arm64_x86_64/MicrosoftCognitiveServicesSpeech.framework`:

| What | Verified detail |
| --- | --- |
| `Headers/` | 191 headers, of which **117 are `speechapi_c_*.h`** — exactly the ones cgo needs, including `speechapi_c_auto_detect_source_lang_config.h` |
| Binary | `Versions/A/MicrosoftCognitiveServicesSpeech`, universal Mach-O **arm64 + x86_64**, 10.3 MB |
| Install name | `@rpath/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech` |
| LID symbols | present: `create_auto_detect_source_lang_config_from_languages`, `recognizer_create_speech_recognizer_from_auto_detect_source_lang_config` |

In other words: the issues describe a *documentation* problem, not an availability one.
The macOS package has everything needed; cgo just has to be pointed at the framework.

**Version:** the Go binding and the macOS framework are at parity — both `1.51.1`.

## Finding 2 — the Go SDK sets no cgo flags, so they are passed through the environment

The SDK's files only declare `#include <speechapi_c_*.h>`; there are no `#cgo LDFLAGS`
directives. The flags are injected from outside:

```sh
FW=<path>/MicrosoftCognitiveServicesSpeech.xcframework/macos-arm64_x86_64
export CGO_CFLAGS="-I$FW/MicrosoftCognitiveServicesSpeech.framework/Headers"
export CGO_LDFLAGS="-F$FW -framework MicrosoftCognitiveServicesSpeech -Wl,-rpath,$FW"
go build ./...
```

It compiles and links cleanly on `darwin/arm64` (Go 1.26.5, Xcode 26.6). `otool -L` on
the resulting binary confirms the dependency via `@rpath`.

## Finding 3 — Loqui's whole configuration reproduces, push stream included

The spike (`spike-azure/main.go`, see "Reproducing" below) replicates what the Electron
project's `src/engine/engine.ts` does, with one deliberate difference: the audio arrives
over a **push stream** instead of the SDK's microphone, because in the port it is Go
that captures and every provider receives the same PCM16 frames.

Real output from the run (no credentials):

```
  [1] SpeechConfig created from the universal v2 endpoint (dylib loaded)
  [2] LanguageIdMode=Continuous set
  [3] AutoDetectSourceLanguageConfig over [es-CO en-US]
  [4] push audio stream at 16000Hz/16bit/mono
  [5] SpeechRecognizer built (LID + push stream)
  [6] callbacks wired (started/partial/final/canceled/stopped)
  <- started  session= a174cd5534d14849adfd3ed0b14069e7
  [7] continuous recognition started
  -> pushing 187500 bytes of PCM from /tmp/loqui-spike.wav
  <- canceled reason=Error errorCode=AuthenticationFailure details="WebSocket upgrade
     failed: Authentication error (401). Please check subscription information and
     region name. SessionId: a174cd5534d14849adfd3ed0b14069e7"
  <- stopped
```

What this proves, step by step:

1. The dylib **loads at runtime** (step 1 is the first call into the framework).
2. `LanguageIdMode = "Continuous"` and `AutoDetectSourceLanguageConfig` over
   `[es-CO, en-US]` are **accepted** — the LID route exists in the Go binding.
3. The **push stream at 16 kHz/16-bit/mono is accepted** as an `AudioConfig`, which is
   the piece that makes moving capture into Go possible.
4. Continuous recognition **starts and opens a session** (a real `SessionId`).
5. It **genuinely connects to the v2 endpoint** and Azure answers a legitimate 401.
6. The SDK delivers **`errorCode=AuthenticationFailure`**: the same structured code
   that `sessionPolicy.classifyCancel` depends on in Electron to decide retry vs. give
   up. **The reconnection policy ports 1:1.**

## What is NOT verified

- **Real transcription and live language switching.** Requires a valid key. The spike
  is ready for it: `SPEECH_KEY=... SPEECH_REGION=... ./spike file.wav`. There is a
  bilingual test WAV generated with `say` at `/tmp/loqui-spike.wav`.
  Low risk: it is the same service, the same endpoint and the same properties the JS
  SDK already uses in production; what the spike does not cover is service behaviour,
  not binding behaviour.
- **The Electron project's Azure key is marked as exposed** and pending regeneration
  (see `loqui`'s `CONTINUITY.md`). It was not used here.
- **Packaging.** The 10.3 MB dylib will have to go inside the `.app`, signed, with the
  `@rpath` pointing at `Contents/Frameworks`, and pass notarisation. Not tried yet;
  it is known work, not an unknown. See the plan.
- **Binary size.** The spike is 3 MB + 10.3 MB of framework.

## Consequences for the port

1. The Azure Speech route **stays in Go**. There is no need to keep a hidden webview
   with the JS SDK, nor to reimplement Azure's WebSocket protocol by hand.
2. **Go owns audio capture** for *all* providers (a single PCM16 path → each provider).
   This removes Electron's hidden `engine` window and its dependence on
   `getUserMedia` in the webview, which in WKWebView is dubious territory.
3. The framework has to be **vendored with a script**, the same way `loqui` does with
   whisper.cpp (`scripts/build-whisper-stt.sh`): download, verify and place.
   The dylib is not committed.
4. `CGO_ENABLED=1` is mandatory → **no cross-compilation**; the macOS build is done on
   macOS. That was already the case because of the Swift helpers.

## Reproducing

The spike lives in the session's scratchpad (not in the repo, yet):
`spike-azure/main.go`. It will move to `internal/stt/azure` as the basis for the real
provider during phase 1.
