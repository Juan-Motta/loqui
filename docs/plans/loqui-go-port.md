# Plan — Porting Loqui (Electron/TS) to Go + Wails v3

- **Date:** 2026-07-27
- **Source:** `~/Desktop/personal/projects/loqui` — Electron + TypeScript, ~13.4k LOC,
  517 tests, 3 native helpers, signed and notarised DMG.
- **Goal:** a 1:1 port of the functionality. macOS (arm64) only in this phase; Windows
  stays an open door, not a requirement.
- **Underlying research:** `docs/research/2026-07-27-azure-speech-go-macos.md`

## Decisions taken

| Decision | Chosen | Why |
| --- | --- | --- |
| Framework | **Wails v3 alpha** | v2 has no systray and no multi-window; Loqui needs both. v3 marks the app/windows/menus/events/services APIs as stable. |
| Frontend | **Reuse verbatim** | The current UI is vanilla HTML+CSS+TS with no framework, and Wails also renders in a webview. The HTML is copied as-is; only the IPC layer changes. |
| Pure logic | **All to Go** | In Electron, main and renderer shared TS. Here they do not share a language, so the rules live in Go once and the UI only paints. Loqui already had two bugs from duplicated/derived rules (`sessionPolicy` classifying by translated prose, i18n breaking a non-UI decision). |
| Order | **Azure Speech first** | It was the only risk with no Go equivalent. Resolved: see the research. |
| Bundle id | **`com.jualopezmo.loquigo`** | Do NOT reuse `com.jualopezmo.loqui`: macOS ties Accessibility and Input Monitoring permissions to bundle id + signature, so two differently signed binaries with the same id fight over the same TCC record. Taking the original id will be an explicit migration step when the port replaces Electron. |

## The central architectural change: the `engine` window goes away

Electron had **3 renderers**. The `engine` one existed for two reasons that disappear in
Go: hosting Azure Speech's JS SDK, and being the only window with microphone permission
(`getUserMedia`).

In the port **Go captures the audio** (malgo/miniaudio → CoreAudio) and **Go drives all
the providers**. Consequences:

- **2 windows** remain: `settings` and `overlay`. Both with the webview's permissions
  explicitly denied: they need none.
- **A single audio path**: PCM16 16 kHz mono from Go to any provider (push stream for
  Azure, WebSocket for Grok/ElevenLabs/OpenAI, stdin for the native helpers). In Electron
  there were 3 different capture topologies.
- The dependence on `getUserMedia` in WKWebView, which is dubious territory, is removed.
- The whole `engine:*` IPC surface and its per-window guard
  (`ipcGuard`/`ENGINE_CHANNELS`) disappears: there is no longer a renderer handling
  secrets.
- The level meter (`meter.ts`, AnalyserNode) becomes RMS in Go.

## Module map: Electron → Go

### Pure logic → Go packages with tests (the heart of the port)

| Electron (`src/shared/`) | Go | Note |
| --- | --- | --- |
| `sessionController.ts`, `dictationState.ts`, `sessionTracker.ts`, `sessionPolicy.ts` | `internal/session/` | The controller with `desired` vs `actual`, generations, backoff. Ported with its full suite. |
| `overlayState.ts` | `internal/session/overlay.go` | The reducer moves to Go; the frontend receives `{status,error}` already computed. **Done** on the frontend side. |
| `settings.ts`, `azureConfig.ts`, `azureOpenAi.ts`, `openaiRealtime.ts`, `grokStt.ts`, `elevenLabs.ts` | `internal/settings/`, `internal/stt/<provider>/` | Validation, region normalisation, v2 endpoint, URL/payload construction. |
| `languageSlots.ts`, `languageCatalog.ts`, `secretSlots.ts` | `internal/settings/` | Languages per provider slot + migration of the legacy global `languages`. |
| `triggerKey.ts` | `internal/hotkey/` | Careful: the accelerators stop being Electron's. See "Risks". |
| `history.ts`, `historyFilter.ts` | `internal/store/` | |
| `logFile.ts` | `internal/store/` | Format, redaction and retention. |
| `modelSpec.ts` | `internal/model/` | Download arithmetic + sha256. |
| `audioPcm.ts` | `internal/audio/` | `downsample`, `floatTo16BitPCM`. |
| `permissions.ts`, `mediaPermission.ts` | `internal/permissions/` | |
| `connectionStatus.ts` | `internal/ui/` | Per-provider availability model. |
| `helperExit.ts`, `sttHelperProtocol.ts`, `globeProtocol.ts` | `internal/stt/helper/`, `internal/hotkey/` | The helpers' line protocols. |
| `i18n/` | `internal/i18n/` | es/en catalogues. The UI receives the resolved catalogue at launch. |
| `preRollBuffer.ts`, `pasteQueue.ts` | `internal/audio/`, `internal/inject/` | |

### I/O and glue

| Electron (`src/main/`) | Go | The real change |
| --- | --- | --- |
| `main.ts` | `main.go` + `internal/app/` | **Skeleton done.** |
| `configStore.ts` (safeStorage) | `internal/store/keychain_darwin.go` | `safeStorage` does not exist. It goes to the **macOS Keychain** directly through cgo (Security.framework), one slot per provider. With a timeout: without a stable signature the read hangs (see risk 1). |
| `historyStore.ts`, `logStore.ts`, `modelStore.ts`, `deviceState.ts` | `internal/store/` | `app.getPath("userData")` → `~/Library/Application Support/LoquiGo`. **Not `Loqui`**: macOS is case-insensitive, so that name is the SAME directory as Electron's `loqui`. |
| `tokenService.ts`, `azureProbe.ts` | `internal/stt/azure/` | Plain HTTP. |
| `injection.ts` | `internal/inject/` | **An improvement on the original.** Electron left it documented that the clipboard restore needed `NSPasteboard.changeCount` and it did not have it. In Go with cgo it **is** possible, and the PRD already asked for it (R6). And the paste can be `CGEventPost` instead of `osascript`. |
| `focusGuard.ts` | `internal/inject/focus.go` | Same: the AX read can be AXUIElement through cgo instead of AppleScript. |
| `hotkey.ts` | `internal/hotkey/` | The Swift helper is kept; only who launches it changes. |
| `streamingStt.ts` | **per provider**, not shared yet | `ws` → `coder/websocket`. **Corrected 2026-07-28:** this mapping said `internal/stt/stream.go`, i.e. inside the **contract** package, which would put a network dependency in the package that `session`, `app` and the local providers import. And extracting it was premature anyway: Electron's `SttAdapter` **contains** the bug Grok had to fix (it closes on any final after the finalize, which truncates). The lifecycle lives in `internal/stt/grok/provider.go`; it will be extracted into its own package when ElevenLabs provides the second real implementation. See `docs/plans/grok-stt-provider.md`. |
| `windowOptions.ts`, `ipcGuard.ts` | `main.go` | `ipcGuard` disappears: there are no generic channels, Wails exposes typed methods. |
| `preload/` | — | Disappears. Wails' bindings are the surface. |

### Ported untouched

The three native helpers are separate processes speaking a line protocol, so they are
independent of the host's language. Already copied into `helpers/`:

- `macos-globe-listener.swift` — the only way to detect `fn` down **and** up
- `macos-stt.swift` — Apple SpeechAnalyzer (macOS 26+)
- `whisper-stt.cpp` — local whisper.cpp

## Phases

- **Phase 0 — foundations.** ✅ **DONE.** Azure spike verified; Wails v3 scaffold;
  2 windows; tray with template/active icon; single instance; AppKit shim for the
  non-activating overlay (verified: `layer=25`, on screen, without stealing focus);
  `patch-plists.sh` for the usage descriptions.
- **Phase 1 — real Azure Speech.** ✅ **DONE**, except real transcription (needs a key).
  Provider in `internal/stt/azure`, `scripts/vendor-speech-sdk.sh` with a pinned sha256,
  capture with malgo verified, `tokenService` with tests, `cmd/stt-probe`.
- **Phase 2 — session and delivery.** ✅ **DONE** (blocked by signing, see risks).
  `internal/session` complete with tests, `fn` hotkey, injection with a real `changeCount`,
  focus guard through AX, history, settings + Keychain, all wired. Still missing the
  non-`fn` global shortcut (see risk 2) and the first real transcription (needs a stable
  signature + a key).
- **Phase 3 — the remaining providers.** PARTIAL.
  - ✅ `internal/stt/helper` — one provider for both local engines (same JSON line
    protocol), with tests. `internal/permissions` — microphone + speech recognition.
  - ✅ **whisper: works.** First real transcript verified 2026-07-28.
  - ⛔ **macos (Apple SpeechAnalyzer): blocked, cause unknown.** See risk 6.
  - ✅ **grok (xAI): implemented** 2026-07-28, `internal/stt/grok`. Real transcription is
    still missing (needs an xAI key); everything else is verified against a local WebSocket
    server, and the handshake rejection against the real service. **The event parsing was
    not ported verbatim**: Electron's loses text (see `docs/plans/grok-stt-provider.md`).
  - ⬜ **openai, elevenlabs** are missing. ElevenLabs comes out of the same mould as Grok
    and that is when the WebSocket lifecycle should be extracted into a shared package;
    OpenAI realtime does **not** fit that mould (it needs a setup message and has a
    different lifecycle).
- **Phase 4 — the UI.** Port `settings.ts` (1828 lines) against a bootstrap payload
  computed in Go; i18n; onboarding; history; permissions; About; logs.
- **Phase 5 — packaging.** Signing, entitlements, Azure's dylib in
  `Contents/Frameworks` with `@rpath`, notarisation, DMG.

## Lessons from the port (do not re-introduce)

- **The mutex Go needs creates reentrancy JavaScript did not have.** Electron's
  `SessionController` called `io.startEngine()` from inside a method and received the
  provider's failure synchronously back in `engineEvent()`. Without a lock that works; with
  a lock it is a deadlock. **Rule: decide under the lock, run effects outside.**
- **macOS has a case-insensitive filesystem.** `Loqui` and `loqui` are the same directory
  (verified by inode), so "I capitalised it to separate them" separates nothing.
- **The port's hangs leave no log.** The two previous bugs were found with
  `GOTRACEBACK=all` + `kill -QUIT`. When something goes still, dumping goroutines is the
  first step, not the last.
- **`BackgroundType` does NOTHING on macOS.** It appears zero times in Wails' darwin code;
  transparency and translucency come from `Mac.Backdrop`. The overlay ended up drawn over
  an opaque white rectangle because of this, and it **looked perfectly configured from
  Go** — only the pixels disagreed. Hence `macos.WindowOpacity`, which reads the real
  `NSWindow` state: when the config and the screen can disagree, read the screen.
- **Relative paths do not work in a `.app`.** An app launched from Finder has its cwd at
  `/`, so `helpers/bin/...` resolves to nothing. Every resource lookup goes through
  `app.HelperPath`, which looks inside the bundle first. It is the same mistake as cgo's
  relative `-I` flags, in another disguise.
- **A silence watchdog is armed on STOP, not from the last output.** A helper prints
  nothing while it is listening, so measuring from its last line spends the whole grace
  period before the user releases the key, and kills the flush that grace was protecting.
- **Tests with child processes need generous waits.** Starting `sh` under a test binary
  linked with cgo can take hundreds of ms; a 200 ms drain made four tests fail against code
  that worked.

## Open risks

1. **Ad-hoc signing in development — CONFIRMED AND WORSE THAN EXPECTED.** It does not just
   revoke Accessibility and Input Monitoring on every build: **it hangs the Keychain.**
   `SecItemCopyMatching` never returns when macOS does not recognise the binary, because it
   wants to ask for authorisation and the prompt cannot be presented. `GetKey` now has a
   3 s timeout (`ErrKeychainTimeout`) so it fails diagnosably instead of freezing, but that
   does not resolve it: **without a stable identity the app cannot read its own key, so it
   cannot dictate.** It is the project's next step, not a matter of convenience.
2. **`triggerKey` can no longer talk about Electron accelerators.** The format
   (`"CommandOrControl+Shift+D"`) is Electron's and there is no `globalShortcut` in Go. A
   library will have to be chosen (`golang.design/x/hotkey`) or a global `NSEvent` monitor
   registered through cgo, and the stored format mapped. Settings already persisted must
   keep loading.
3. **Wails v3 is alpha.** The API can move between alphas. Pin the version in `go.mod` and
   raise it deliberately.
4. **No cross-compilation.** cgo is mandatory (Azure, AppKit, malgo) → the macOS build is
   done on macOS. That was already the case because of the Swift helpers.
5. **Apple's engine is blocked and no cause has been identified.** Standalone it reaches
   `started`; launched from the `.app` it stops just before, after choosing `es-CL`. It does
   not fail —it emits no `canceled`, it writes nothing to stderr— it is blocked on an
   `await`. Microphone and speech recognition are granted and it makes no difference. The
   candidate `await`s are `SpeechTranscriber.installedLocales`,
   `AssetInventory.assetInstallationRequest` and `SpeechAnalyzer.bestAvailableAudioFormat`.
   **Next step: instrument the Swift with prints between each await, or retry with a stable
   signature. Do not invent the cause.**
6. **Two whisper warts, neither from the pipeline.** `language` stores the setting
   (`"auto"`) instead of the detected language, because the helper does not report it even
   though whisper.cpp knows it — the Electron project stores the same. And the helper opens
   **SDL's default device**, ignoring `inputDeviceId`, so the microphone picker does not
   apply to it.
7. **The Azure key is exposed and pending regeneration** (it comes from `loqui`).
   Real transcription cannot be verified until there is a new one.
