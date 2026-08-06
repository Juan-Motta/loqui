# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port of **Loqui** (Electron/TS, in `../loqui`) to **Go + Wails v3**, macOS arm64 only.
  **The app dictates for real through Azure** as of 2026-08-06 — the thing phase 1 had carried as
  outstanding since the beginning. Phases 0-3 done except two providers' real transcription. Phase 4
  (the UI) nearly closed: the app is navigated, configured and used.

- **Next step:** **`availableKeySlots` lists only `azure-speech` and `grok`** (`store/secrets.go`),
  but OpenAI and ElevenLabs were ported afterwards. `SetKey`/`SaveConnection` reject slots that are not
  listed, so **today the app cannot save an OpenAI or ElevenLabs key at all**: two ported engines are
  unusable through the interface. This goes ahead of anything else because it is small, it is a
  one-line-plus-tests fix, and everything below depends on being able to store those credentials.

- **Then:** **port "Test connection" to OpenAI, Grok and ElevenLabs** — asked for by the user on
  2026-08-01. All three are WebSocket with different handshakes
  (`wss://api.openai.com/v1/realtime`, `wss://api.x.ai/v1/stt`,
  `wss://api.elevenlabs.io/v1/speech-to-text/realtime`), so they do not share a test with Azure's
  HTTP token exchange: each needs its own, reusing the dial from its package. The mould is in
  `internal/app/settings_probe.go` (the `slotsWithProbe` allowlist, preflight before the network,
  three distinct outcomes from reading the credential) and the card already has the button wired.
  **Azure's green path is now verified** and is the reference for what a working one looks like:
  UC-7 of `docs/e2e/reports/2026-08-06-keys-in-a-file.md`.

- **After that:** **port the whisper model row**, the only thing left from the fidelity assignment.
  Start with the red
  `modelSpec` test in Go: port `../loqui/src/shared/modelSpec.ts` to `internal/store/model.go`
  (file name, expected size, download URL) with tests, and only then the download service with
  progress and the DOM of `renderModelInto` in `#modelRow`. It is **load-bearing**: without
  `ggml-small.bin` whisper does not start, and today the only way to get it is
  `./scripts/build-whisper-stt.sh`.

- **Blockers:**
  1. ~~**No remote.**~~ **Closed 2026-08-06:** `origin` is
     `git@github-jualopezmo:Juan-Motta/loqui.git` and `main` is pushed. Two loose ends: the repo is
     called `loqui` while the module path says `github.com/Juan-Motta/loqui-go` (harmless for a desktop
     app, but inconsistent), and `../loqui` — the Electron project — still has that same remote in its
     local config, so pushing from there would collide with 46 unrelated commits.
  2. **Ad-hoc signing.** Down from three symptoms to two: permissions are revoked on every rebuild,
     and probably Apple's engine. The third — the Keychain not answering — was routed around rather
     than fixed: the credentials now live in a cleartext file (`store/secrets.go`), a trade the owner
     accepted for a personal build. Signing is still the only thing that fixes the rest, and the only
     thing that would let the keys go back to being encrypted at rest. Pending decision: a fixed
     self-signed identity vs a Developer ID.
  3. **Cloud keys — Azure is DONE, the rest are not.** A working Azure key is stored and has
     transcribed for real (UC-9 of `docs/e2e/reports/2026-08-06-keys-in-a-file.md`). There is still no
     xAI, OpenAI or ElevenLabs credential, so those three routes remain unverified end to end.

- **Credentials now live in a cleartext file**, `~/Library/Application Support/LoquiGo/secrets.json`,
  mode 0600 — not the Keychain, which hangs under ad-hoc signing. A deliberate trade the owner accepted
  with FileVault off; see `store/secrets.go` and the About view, which says so to the user. The Azure
  key was pasted through the app on 2026-08-06 and works. **An orphan remains**: the older item is
  still in the login Keychain (`security find-generic-password -s com.jualopezmo.loquigo -a
  azure-speech`) and nothing reads it — worth deleting so there is one copy, not two.

- **Debt, unowned: the frontend does not type-check.** `typescript@^4.9.3` against a `tsconfig.json`
  with TS5 options, so `tsc` cannot read the config and vite strips the types without validating
  them. Around 1500 lines of TS have already been written with no safety net. **Upgrade typescript
  before writing more.**

- **Known debt, owned:** two pre-existing bugs in `internal/session` that affect Azure **today** —
  the retry budget bounds nothing if the connection opens (a spend loop against a service that bills
  by the hour) and reconnection leaks the previous capture. With `file:line` at the end of
  `docs/plans/grok-stt-provider.md`. They go in their own change.

- **Active workflow:** none. The last one closed (credentials into a file) is in `.workflow/state.md` —
  **gitignored**, so a fresh clone does not have it. It is the first workflow whose
  `check-gates.sh` came back green.
- **Updated:** 2026-08-06

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

## Providers: where each one stands

This section was stale for two sessions: it listed elevenlabs and openai as unported when both had
been done on 2026-07-30. Corrected here.

| Engine | Ported | Real transcription | Blocked on |
| --- | --- | --- | --- |
| **whisper** | ✅ | ✅ 2026-07-28 | the model row — `ggml-small.bin` is downloaded by hand today |
| **azure** | ✅ | ✅ **2026-08-06** | nothing |
| **macos** (SpeechAnalyzer) | ✅ | ⛔ | blocked before `started`, cause unknown — risk 5 of the port plan |
| **grok** (xAI) | ✅ | ⬜ | no xAI credential |
| **openai realtime** | ✅ | ⬜ | no credential — **and the UI cannot store one** (`availableKeySlots`) |
| **elevenlabs** | ✅ | ⬜ | no credential — **and the UI cannot store one** (`availableKeySlots`) |

**Still not done, and worth keeping visible:** the WebSocket lifecycle was never extracted out of
`internal/stt/grok` into a shared package. Two real implementations now exist (grok and elevenlabs),
which was the condition set for doing it — see the approach section of
`docs/plans/grok-stt-provider.md`. It is refactoring, not a feature, so it goes behind the items
above.
