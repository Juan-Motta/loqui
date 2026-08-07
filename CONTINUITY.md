# Continuity — session handoff

> First thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it SMALL. Refresh with the `checkpoint` skill before closing a session.

- **Focus:** port of **Loqui** (Electron/TS, in `../loqui`) to **Go + Wails v3**, macOS arm64 only.
  **The port is finished.** Every engine is ported, the app is navigated/configured/used, it dictates,
  it translates itself, and it downloads its own whisper model. `main` is `4f8cbb5`, pushed;
  `./scripts/task.sh check` green (15 test packages, `vet`, frontend types).

- **NEXT STEP — one action, and it is a real bug that costs money.**
  **`internal/session/controller.go:278` sets `c.reconnectAttempt = 0` on `stt.Started`**, so
  `maxReconnects` (`:26`) bounds nothing whenever the connection OPENS before failing: a provider that
  connects and immediately drops reconnects for ever, against services billed by the hour.

  Start red, in `internal/session`: a provider that emits `Started` then a retryable `Canceled`,
  `maxReconnects`+1 times, must stop reconnecting — today it never does.
  `internal/stt/grok/errors.go:29-35` documents this from the other side and says its
  `serverErrorCode` should become `ServiceError` once the budget is fixed; do both in one change.

- **After that:** the other known bug — **reconnection leaks the previous capture** (declared
  out-of-scope twice by the owner; see the review log of `docs/plans/grok-stt-provider.md`). Then the
  refactor nobody has done: **extract the WebSocket lifecycle out of `internal/stt/grok`** into a
  shared package. Three real implementations now exist, which was the stated condition.

- **Blockers — all owner decisions, none is code:**
  1. **Ad-hoc signing.** The root cause of credentials living in cleartext
     (`~/Library/Application Support/LoquiGo/secrets.json`, mode 0600, FileVault off — a trade the
     owner accepted, and the About view tells the user). It also revokes permissions on every rebuild.
     Pending: a fixed self-signed identity vs a Developer ID.
  2. **Apple's engine awaits a voice.** It reaches the same live state as whisper, but nobody has seen
     it produce text — and **an agent cannot check this**: the debug affordance starts and stops a
     dictation, it cannot speak. To make it machine-checkable, feed audio from a FILE through
     `cmd/stt-probe`.
  3. **An orphan Keychain item** nothing reads: `security find-generic-password -s
     com.jualopezmo.loquigo -a azure-speech`. Delete it so there is one copy of the key, not two.

- **Active workflow: NO.** Nothing is half-done. `.workflow/state.md` describes the last shipped
  change and is gitignored; start the next one from `shared/state.template.md`.
- **Updated:** 2026-08-07

## Providers

| Engine | Ported | Transcribes |
| --- | --- | --- |
| **whisper** | ✅ | ✅ 2026-07-28 · model row ported, a real 465 MB download ran |
| **azure** | ✅ | ✅ 2026-08-06 · E2E report |
| **grok** (xAI) | ✅ | ✅ **owner**, 2026-08-07 |
| **openai realtime** | ✅ | ✅ **owner**, 2026-08-07 |
| **elevenlabs** | ✅ | ✅ **owner**, 2026-08-07 |
| **macos** (SpeechAnalyzer) | ✅ | ⬜ reaches the live state 2026-08-07 · text unconfirmed |

The column says **"owner"** rather than a date alone on purpose: there is no E2E report behind those
three and **there cannot be** — see blocker 2. Do not upgrade them to a verified claim.

## Handoff notes

1. **THE MOST EXPENSIVE LESSON HERE, and it cost weeks.** Apple's engine was marked ⛔ *"blocked before
   `started`, cause unknown"*. **It was never blocked.** `stt.Started` was logged nowhere (it is
   consumed to move the overlay pill) and the Apple helper reported no microphone levels, so its
   `MIC peak 0.00` read as silence. A working engine and a dead one produced **the same log**. What
   exposed it: whisper — which works — also logged zero mentions of "started".

   **A log that cannot tell "it did not happen" from "nobody wrote it down" gets read as the first,
   and the wrong conclusion outlives the session that drew it.** Before believing a blocker of that
   shape, compare against a component that works. Both gaps are closed
   (`docs/e2e/reports/2026-08-07-speechanalyzer.md`).

2. **Verify by MUTATING; distrust a green test.** Repeatedly, tests that looked thorough proved
   nothing until deliberately broken: an i18n scan whose regex had the *same blind spot* as the bug it
   existed to catch (Go joins concatenated literals, the catalogue held fragments, so nine messages
   reached users in Spanish while the guard reported "covered"); a redaction test asserting an 8-char
   suffix against a fixture that preserved five; a payload test that passed only because the strings
   were absent from the catalogue. **Break what each new test covers and watch it fail.**

3. **Cross-engine review of the DIFF is where the real defects were**, far more than in plan reviews.
   Recent hauls: two credential leaks in provider prose; a download writing into the canonical model
   path (whisper would have loaded a partial file); packaging that copied the 465 MB model into the
   bundle so the downloader was **never exercised** by anyone (497 MB → 32 MB); blocking I/O on a
   realtime audio thread. Run it on every diff that touches a boundary.

4. **Environment traps, one lost run each.** Bare `go` does not compile (Azure SDK cgo flags →
   `./scripts/go.sh`); `task`/`wails3` are not on PATH (→ `./scripts/task.sh`); **launch the packaged
   app UNSANDBOXED** or it prints its platform line and goes silent, which looks exactly like a broken
   feature; kill the previous instance first (`SingleInstance` makes the second exit 0 without a word);
   use braces in debug steps (`"${p}:test"` — zsh eats `:t`); and **regenerate bindings** after adding
   a service field or method (`./scripts/task.sh common:generate:bindings`).

5. **Where the rules live — check this before adding any user-facing text.** Decisions are in Go
   (`internal/store`, `internal/app`) with tests; the page paints what it is handed, including the
   CSS class that colours a badge. **i18n keys ARE the Spanish source strings**, so editing copy breaks
   its key — the eight coverage scans in `internal/i18n` are what stop that rotting silently, and two
   of them hunt Spanish prose that was never routed at all. Run
   `./scripts/go.sh test ./internal/i18n/` and it will name what is missing. Only `es` and `en` exist,
   and **no native speaker has reviewed the English**.

6. **Three things "just persist it" does not cover.** Any new setting must check them, and each has
   bitten once: the **mode** is read when the engine is built (`LiveHooks.ModeChanged`); the
   **shortcut** lives in a child process started at launch (`LiveHooks.TriggerChanged`); the
   **appearance** is applied by Wails once and cannot be changed through it (cgo in
   `internal/macos/appearance_darwin.go`). Hooks are passed in the **constructor** — Wails binds every
   exported method of a service to the webview.

7. **Still inert** (measured): System's `#save` (possibly redundant — every control persists on
   change), `#engineHint`, the unported-subservice fields (`azureOpenAiResource`,
   `azureOpenAiDeployment`, `openaiModel`), the footer links (`#openDonate`, `#openTutorial`), the
   **report** view, and the 17 `wiz*` elements of the **onboarding**.

## State of the code

**8 Wails services:** `Settings`, `History`, `Clipboard`, `Dictation`, `Permissions`, `About`,
`Links`, `Model`.

- `main.go`, `wiring.go` — the Wails app: 2 windows, tray, `fn` hotkey, permissions, and the
  `LiveHooks` connecting settings to the running engine and listener. The **store is opened in `main`**
  and shared, because two Stores over one directory each hold their own lock.
- `internal/app` — the Settings payload (`bootstrap.go`), the setters (`settings_write.go`), the probe
  (`settings_probe.go`), reveal, the model downloader, and the history/clipboard/dictation/permissions
  services. Every setter returns `WriteResult{payload, error}` and **not** a Go error: Wails discards
  the result of a method that also returns an error, and the page needs the payload precisely when it
  fails. User-facing text is translated at the **boundary** (`i18n_payload.go`) — the pure rules keep
  emitting Spanish, which is the catalogue's key format.
- `internal/store` — persistence **and** the rules ported from Electron's pure modules: connections,
  language capability, catalogue, shortcut, and the whisper model spec (`model.go`, pinned by size AND
  sha256). `UpdateSettings` is transactional; never Load-then-Save.
- `internal/session` — the dictation controller (pure decisions, suite ported from Electron). **Holds
  the two known bugs above.**
- `internal/stt` — network-free contract. `azure`, `helper` (whisper and Apple, both reporting
  microphone levels), `grok`, `openai`, `elevenlabs`, each with a connection probe.
- `internal/i18n` — the catalogue and the eight coverage scans.
- `internal/{audio,inject,history,hotkey,permissions,macos,assets,settings}` — capture, paste, history
  + filter, `fn` protocol, TCC, AppKit glue, region validation.
- `frontend/` — `index.html` is the Electron markup almost verbatim, so **what the page emits must
  match the classes the CSS expects** (a first attempt invented `.hist-item` and produced unstyled
  rows). One TS module per view, plus `i18n.ts` and `model.ts`; `overlay.html` is the pill.
- `cmd/stt-probe` — dictation from the CLI, to isolate failures without the app. **The place to add
  file-based audio** if anyone wants transcription machine-verifiable.

## Debug affordances

All gated by environment variable, all reporting to the Go log and **never transcript text**:
`LOQUI_DEBUG_NAVIGATE`, `RECORD_CLICK`, `SET_PROVIDER`, `SET_LANGUAGE`, `APPEARANCE`,
`HISTORY_EVENT`, `OVERLAY`, `DICTATE=<s>`, `CONN_CLICK`/`CONN_REPORT`, `MODEL_CLICK`, `TIME_TEXT`.
A `<select>` inside a Wails webview cannot be clicked from a script, which is why these exist — and
they dispatch **real** events on the real controls, so what runs is the handler a mouse reaches.
`CONN_CLICK` supports `+` to chain steps and `wait:<ms>` to sequence them.
