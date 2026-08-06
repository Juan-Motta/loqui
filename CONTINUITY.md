# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port of **Loqui** (Electron/TS, in `../loqui`) to **Go + Wails v3**, macOS arm64 only.
  **The app dictates for real through Azure** as of 2026-08-06 — the thing phase 1 had carried as
  outstanding since the beginning. Phases 0-3 done except two providers' real transcription. Phase 4
  (the UI) nearly closed: the app is navigated, configured and used.

- **Next step, and it is one concrete action:** **write
  `docs/e2e/reports/2026-08-06-probe-remaining-providers.md`.** The branch
  `feat/probe-remaining-providers` (4 commits, tree clean, **not merged, not pushed**) has the whole
  feature working and verified against the real services — the evidence is already captured and pasted
  in `.workflow/state.md`, it just has to be written up as a report. `check-gates.sh` correctly refuses
  the branch today because the E2E box still points at the previous change's report, which is not fresh
  here. After the report: run check-gates, merge fast-forward, push.

  Worth doing before merging, and not blocking: **a cross-engine review of the DIFF.** Codex reviewed
  the plan three times; nobody has reviewed the code.

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

- **Active workflow: YES** — "Probar conexión" for the three remaining providers, in
  `.workflow/state.md` (**gitignored**, so a fresh clone does not have it). That file holds the
  ship-gate boxes, the review log and the captured E2E evidence. Do not duplicate it here; read it.
- **Updated:** 2026-08-06 (second checkpoint of the day)

## Handoff notes

0. **THE ONE THING THAT WOULD HAVE SHIPPED A LIE, and how it was caught.** The connection test was
   designed as "open the socket, close it, report success". Measuring the three services with an
   invalid key showed **OpenAI and ElevenLabs return HTTP 101 for a garbage key** and refuse afterwards
   as an event — so that design would have answered `✓ Conexión correcta` to any string a user pasted.
   The protocol is now: dial → wait for the FIRST server message → classify it → CloseNow. Success is
   positive only, by event name; a clean close is **not** success (ElevenLabs closes with 1000 *after*
   refusing). Full measurement: `docs/research/2026-08-06-where-realtime-stt-auth-fails.md`.

   The transferable part: **ask where auth fails before designing anything that tests a credential.**
   One curl per service, no account needed, and it decided the whole shape of the feature.


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
| **openai realtime** | ✅ | ⬜ | no credential |
| **elevenlabs** | ✅ | ⬜ | no credential |

All four cloud engines can now **store** a key and **test** it. Storing was fixed in `07c14a3`; testing
is on the unmerged branch. What none of the three non-Azure ones has ever done is transcribe — that
needs a credential, and there is none on this machine.

**Still not done, and worth keeping visible:** the WebSocket lifecycle was never extracted out of
`internal/stt/grok` into a shared package. Two real implementations now exist (grok and elevenlabs),
which was the condition set for doing it — see the approach section of
`docs/plans/grok-stt-provider.md`. It is refactoring, not a feature, so it goes behind the items
above.
