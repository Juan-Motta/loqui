# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port of **Loqui** (Electron/TS, in `../loqui`) to **Go + Wails v3**, macOS arm64 only.
  Phases 0-3 done except two providers. **Phase 4 (the UI) nearly closed:** the app can be
  navigated, configured and used. The user tried it and says "it all looks good"; some minor UI
  details are left, which he will polish himself.

- **Next step:** **port "Test connection" to OpenAI, Grok and ElevenLabs** — asked for by the user on
  2026-08-01 and declared out of scope for the previous change. All three are WebSocket with
  different handshakes (`wss://api.openai.com/v1/realtime`, `wss://api.x.ai/v1/stt`,
  `wss://api.elevenlabs.io/v1/speech-to-text/realtime`), so they do not share a test with Azure's
  HTTP token exchange: each needs its own, reusing the dial from its package. The mould is in
  `internal/app/settings_probe.go` (the `slotsWithProbe` allowlist, preflight before the network,
  three distinct outcomes from reading the credential) and the card already has the button wired.
  **The green path for Azure still needs verifying too**: `✓ Conexión correcta` has never been
  executed because there is no valid key on the machine — the bug itself deleted it (see below).

- **After that:** **port the whisper model row**, the only thing left from the fidelity assignment
  (the previous session's work is already merged into `main`, up to `a83b2f5`). Start with the red
  `modelSpec` test in Go: port `../loqui/src/shared/modelSpec.ts` to `internal/store/model.go`
  (file name, expected size, download URL) with tests, and only then the download service with
  progress and the DOM of `renderModelInto` in `#modelRow`. It is **load-bearing**: without
  `ggml-small.bin` whisper does not start, and today the only way to get it is
  `./scripts/build-whisper-stt.sh`.

- **Blockers:**
  1. **No remote.** `git remote -v` is empty: there is no copy off this machine. The user said he
     would set it up later. When the time comes: the module path says
     `github.com/Juan-Motta/loqui-go` but `gh` is authenticated as `Juan-Andres-LM`, and creating
     the repo would publish the code → an owner and public/private have to be chosen.
  2. **Ad-hoc signing.** Down from three symptoms to two: permissions are revoked on every rebuild,
     and probably Apple's engine. The third — the Keychain not answering — was routed around rather
     than fixed: the credentials now live in a cleartext file (`store/secrets.go`), a trade the owner
     accepted for a personal build. Signing is still the only thing that fixes the rest, and the only
     thing that would let the keys go back to being encrypted at rest. Pending decision: a fixed
     self-signed identity vs a Developer ID.
  3. **Cloud keys.** The Azure one is marked as exposed; there is no xAI one at all. Without them,
     real transcription through those routes cannot be verified.

- **The Azure key is in the Keychain, which the app no longer reads.** The bug that deleted it is
  fixed (the card speaks), and the user pasted a new key on 2026-08-03 — but the credentials moved to
  a file on 2026-08-06, and migrating the old item needs an interactive Keychain prompt that only the
  user can approve. Until they do (`security find-generic-password -s com.jualopezmo.loquigo -a
  azure-speech -w`) or paste it again in the app, **the `azure-speech` slot is empty** and the green
  path — `✓ Conexión correcta` — still has never been executed.

- **Debt, unowned: the frontend does not type-check.** `typescript@^4.9.3` against a `tsconfig.json`
  with TS5 options, so `tsc` cannot read the config and vite strips the types without validating
  them. Around 1500 lines of TS have already been written with no safety net. **Upgrade typescript
  before writing more.**

- **Known debt, owned:** two pre-existing bugs in `internal/session` that affect Azure **today** —
  the retry budget bounds nothing if the connection opens (a spend loop against a service that bills
  by the hour) and reconnection leaks the previous capture. With `file:line` at the end of
  `docs/plans/grok-stt-provider.md`. They go in their own change.

- **Active workflow:** none. The last one closed (the Settings setters) is in `.workflow/state.md` —
  **gitignored**, so a fresh clone does not have it.
- **Updated:** 2026-08-01

## Handoff notes

1. **The UI works and is ported FAITHFULLY to the original layout.** `frontend/index.html` is still
   the Electron markup almost verbatim, and the CSS is its own — which is why **what the page emits
   has to match the classes that CSS expects**. A first attempt invented `.hist-item`/`.hist-meta`
   and produced unstyled rows. Now ported, with their classes: History (`.hrow`, expand, copy, empty
   states), Connections (`.conn-state` with the state AS A CLASS, which is what colours the dot),
   languages (chips/select depending on capability), System (shortcut, appearance, mode, device) and
   Permissions (`.prow` with three-way state).

   The TS modules are split per view: `settings.ts` (shell + connections), `history.ts`,
   `language.ts`, `system.ts`, `permissions.ts`. The **rules** all live in Go
   (`internal/store/{connection,language,language_catalog,trigger}.go`,
   `internal/app/permission_rows.go`) with tests; the page decides nothing.

2. **Three things that "just persist it" does NOT cover, and each has already bitten once.** Any new
   setting has to check them:
   - **The mode** is read once when the engine is built → it has to be pushed to the live controller
     (`LiveHooks.ModeChanged`).
   - **The shortcut** lives in a child process launched at startup → the listener has to be
     restarted (`LiveHooks.TriggerChanged`), or the new one is saved and the old one keeps working.
   - **The appearance** is applied by Wails exactly once and it exposes no way to change it → cgo in
     `internal/macos/appearance_darwin.go`.

   And the hooks are passed in the **constructor**, not through methods: Wails binds every exported
   method of a service to the webview.

3. **To test the UI without a mouse** — a `<select>` inside a Wails webview cannot be clicked from a
   script, so there are environment-variable probes, all gated:
   `LOQUI_DEBUG_NAVIGATE=<view>`, `LOQUI_DEBUG_RECORD_CLICK=1`, `LOQUI_DEBUG_SET_PROVIDER=<engine>`,
   `LOQUI_DEBUG_APPEARANCE=<mode>`, `LOQUI_DEBUG_HISTORY_EVENT=1`, `LOQUI_DEBUG_OVERLAY=1`,
   `LOQUI_DEBUG_DICTATE=<seconds>`. Each reports to the Go log (`UI-NAV`, `CONN`, `LANG`, `SYS`,
   `PERMS`, `HIST-SHAPE`…), **never transcription text**. `./scripts/capture-overlay.sh` captures the
   pill at native resolution.

4. **A passing test does not prove it tests anything.** In this session **four** of my own tests did
   not test what their names claimed, and all four were found by **mutating the production code**,
   not by the suite: one put the secret in a seam the function never called, one checked only "not
   empty" and blessed an incorrect default, one accepted any error where two checks overlapped, and
   one asserted on a code present in both of the lists it was supposed to distinguish. **Verify every
   new test by deliberately breaking what it says it covers.**

5. **What is still inert** (measured, not from memory): the model row (`#modelRow`), System's
   `#save` — which may be redundant by design, because here every control already persists on
   change —, `#engineHint`, the fields for unported subservices (`azureOpenAiResource`,
   `azureOpenAiDeployment`, `openaiModel`), the footer links (`#openDonate`, `#openTutorial`), the
   **About** and **report** views, and the 17 `wiz*` elements of the **onboarding**.

6. **Read the README before running anything.** Two environment traps: bare `go` does not compile
   (the Speech SDK's cgo flags come from the environment → `./scripts/go.sh`) and `wails3` is not on
   the PATH (→ `./scripts/task.sh`). When adding fields or methods to a service, **regenerate the
   bindings**: `./scripts/task.sh common:generate:bindings` (a bare `generate:bindings` task does not
   exist; `package` already runs it).

## State of the code

Thirteen test packages green with `-race -count=1` (`./scripts/task.sh test`), `vet` and `gofmt`
clean. Five Wails services: `Settings`, `History`, `Clipboard`, `Dictation`, `Permissions`.

- `main.go`, `wiring.go` — the Wails app: 2 windows, tray, `fn` hotkey, permissions, and the
  `LiveHooks` that connect settings to the running engine and listener. The **store is opened in
  `main`** and shared with the engine.
- `internal/app` — the Settings payload (`bootstrap.go`), the setters (`settings_write.go`), and the
  history, clipboard, dictation and permissions services. Every setter returns
  `WriteResult{payload, error}` and **not** a Go error: Wails discards the result of a method that
  also returns an error, and the page needs the payload precisely when it fails.
- `internal/store` — persistence **and** the rules ported from Electron's pure modules: connections,
  language capability, catalogue, shortcut. `UpdateSettings` is transactional; never
  Load-then-Save.
- `internal/session` — the dictation controller (pure decisions, suite ported from Electron).
- `internal/stt` — network-free contract. `azure` (reaches a real 401), `helper` (whisper ✅ and now
  **reports microphone levels**, Apple ⛔), `grok` (✅ 71 tests).
- `internal/{audio,inject,history,hotkey,permissions,macos,assets,settings}` — capture, paste,
  history + filter, `fn` protocol, TCC, AppKit glue, region validation.
- `frontend/` — `index.html` is the Electron markup almost verbatim (delete-key buttons and a status
  line were added); five TS modules, one per view; `overlay.html` is the pill.
- `cmd/stt-probe` — dictation from the CLI, to isolate failures without the app.

## Providers: what is left

- ⬜ **elevenlabs** — the same mould as Grok (WebSocket, `xi-api-key` header, JSON with base64
  instead of binary frames). The moment to **extract the socket lifecycle** out of
  `internal/stt/grok`, with two real implementations in front of us rather than deduced from one.
- ⬜ **openai realtime** — does **not** fit that mould (setup message, different lifecycle): its own
  package.
