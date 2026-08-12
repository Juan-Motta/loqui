# Changelog

Notable changes to this project, newest first — one short entry (or small block) per
shipped change. Written at ship time (the `finish-branch` skill records an entry before the
ship commit). See `shared/rules/docs-layout.md`.

## Intuitive bilingual DMG installer — 2026-08-12

- **Installing Loqui now explains itself.** The DMG opens in a compact branded Finder window with
  English and Spanish drag-to-Applications instructions, a clear arrow, and fixed real app/folder
  icons.
- **The presentation is reproducible and release-safe.** A hash-locked headless builder creates the
  Finder metadata, while the generated image is mounted and its layout, copied app signature, and
  designated requirements are verified before signing or notarization continues.

## MIT license — 2026-08-11

- **Loqui is now distributed under the MIT License.** The repository includes the standard license
  text with Juan Andres Lopez Motta as the copyright holder.

## User-friendly bilingual project guide — 2026-08-11

- **The repository now opens with the product instead of maintainer internals.** The English README
  explains what Loqui does, where to download the current Apple Silicon release, its engines,
  first-use flow, permissions, privacy tradeoffs, local development, and build commands.
- **A complete Spanish guide is one click away.** Both languages share the same section hierarchy,
  commands, compatibility limits, credential warnings, and progressively disclosed release runbook.
- **The owner-supplied Loqui banner now lives in the repository.** GitHub Markdown rendering, public
  release links/assets, the full project gate, and both rendered language journeys were verified.

## Protected GitHub macOS releases — 2026-08-10

- **A manual `Release` Action now publishes Loqui from the exact `main` version and commit.** A
  secret-free hosted-runner preflight runs first; the signing/notarization job can start only after
  approval of the branch-restricted `release` Environment. Apple credentials live only in that
  Environment and are imported into an ephemeral Keychain that is removed on every outcome.
- **The first automated release, `v0.1.0`, is public and immutable.** GitHub contains exactly the
  Apple Silicon DMG and its SHA-256 file, while sanitized signing/notarization evidence is retained
  separately for 14 days. Re-running the same version fails before protected access, and a
  non-`main` dispatch is rejected before Environment approval.
- **The published download passed the real journey.** A fresh copy matched its checksum, notarized
  ticket, Gatekeeper assessments, deep app signature, read-only bundle audit, and exact tag/SHA.
  The owner also installed the public DMG and confirmed that Loqui launches and works.

## Signed and notarized macOS distribution — 2026-08-10

- **Loqui can now be distributed outside the App Store as an Apple Silicon DMG.** The release task
  builds every native helper from pinned sources, assembles the portable bundle, signs its nested
  code inside-out with Developer ID, notarizes the outer DMG, staples the ticket, verifies
  Gatekeeper, and publishes the artifact plus scrubbed evidence only after every phase succeeds.
- **The supported floor is macOS 14.** Main, Whisper, SDL and ggml target Sonoma or newer; Apple
  Speech remains an explicit macOS 26-only capability instead of raising the whole application to
  26. Packaging is self-contained and no longer depends on ambient helper or Homebrew output.
- **Release identity and payload integrity are audited.** The pipeline verifies the optional Whisper
  model, Mach-O architectures/load paths/minimum OS, entitlements, Team ID, timestamps and designated
  requirements before and after DMG assembly. Publication and cleanup are physically contained and
  failure evidence is preserved without checkout paths or credentials.
- **The real journey passed.** Two separately built releases installed on a clean second Apple
  Silicon Mac, launched and passed Gatekeeper after an offline reboot, dictated through local/cloud
  engines, and preserved established macOS permissions across the upgrade.

## API keys move out of the Keychain and into a file — 2026-08-06

- **The app can read its own credential again.** On an ad-hoc-signed build — every development build
  of this project — `SecItemCopyMatching` never returns: macOS wants to authorise the access and
  cannot show the prompt. So the key was there and unreadable, every launch paid a three-second
  timeout, and the engine check could not tell "no key" from "I could not look". The credentials now
  live in `secrets.json` in the data directory, mode `0600`, written atomically.
- **The trade, in the app's own words.** They are stored **in the clear**. The About view says so now
  instead of promising they are "encrypted in the system keychain", which stopped being true with this
  change — a false privacy claim is worse than an uncomfortable true one. Turning FileVault on
  restores encryption at rest.
- **A declared residual is closed, not moved.** The Keychain's timeout was indeterminate: the cgo call
  was abandoned, not cancelled, so a failed write could land seconds later and leave the new key
  paired with the old region. Every credential path had to treat a failure as a possible change. A
  rename either happened or it did not, so that accounting is gone and the messages no longer have to
  say "unconfirmed".
- **What did NOT change:** the three states of a credential (present / absent / unreadable). A file can
  be corrupt or unreadable after a restore, and collapsing that into "you have no key" is what sends
  someone to paste a credential they already have.
- **What this does not fix:** Accessibility and Input Monitoring are still revoked on every rebuild.
  That is the same root cause — the signature — and only signing fixes it.

## The engine that cannot dictate no longer stays selected — 2026-08-02

- **At launch, the app moves to the default engine (Whisper) if the active one cannot dictate**, and
  it says so: `✓ Azure no está listo para dictar: se cambió a Whisper`. Before, it stayed on an
  unconfigured engine and the failure showed up when the shortcut was pressed, far from whatever
  caused it. The same on deleting the key of the engine in use, which is the exact moment it stops
  being usable.
- **Two cases where it does NOT move, and they are half the fix:** if the Keychain did not answer,
  the app could not CHECK the key — and on this build that is the common case, so switching engines
  over a three-second timeout would take a working configuration away from the user. And if Whisper
  cannot work either (its model is missing, something the connection state knows nothing about), it
  does not switch: replacing one silent failure with another is not a fallback.
- **No sentence outlives the screen it describes.** The home status line was the only one that
  belonged to no action, so switching engines from a Connections card left
  `✓ … se cambió a Whisper` under a picker that already said something else. The next repaint now
  clears it. The price, declared: the check's notice disappears on the user's next action — better
  than contradicting what is on screen.
- **And the check goes quiet if the world moves while it decides.** If you finish pasting your key
  while it is checking it, it withdraws the sentence and decides again instead of announcing a
  problem you have already fixed.
- **The hero's title block sat 2 px low.** Flexbox centres the box, and that block's ink is not
  symmetric: the title reserves leading above and the subtitle descender space below, so the pair
  ended up aligned with the logo's bottom edge. Measured, not eyeballed.

## The connection card's actions: saying what happens — 2026-08-01

- **"Test connection" finally exists.** `azure.TestConnection` had been written and tested since the
  port with nobody calling it: there is now a bound method (`Settings.TestConnection`) and a handler.
  It validates region and key before going to the network, distinguishes the three outcomes of a
  Keychain read, and only accepts slots that really have a test — `IsAvailableKeySlot` is true for
  Grok, and using it would have sent a Grok key to the Azure endpoint.
- **A success is stated.** The setters return `Notice` as well as `Error`, and the page paints
  `✓`/`✗` with the classes the CSS already had. Before, the `…` was replaced by an empty string, so a
  correct save and a lost click looked identical — and **"Delete key" destroyed a credential in
  silence** during validation. The deletion notice is a postcondition ("La clave ya no está
  guardada"), because deleting an empty slot is also a success.
- **A disabled button looks disabled** (`button.btn:disabled`), and validation points at the missing
  input with a red border, from the `Field` value Go decides. The message takes its own line above
  the buttons: beside them it sat in a 60 px column breaking words apart.
- **"Use this engine" is only enabled with the connection saved**, and "Delete key" only with a key
  that exists. Local engines (`available`) stay enabled: they carry no credential.
- **Repaints are ordered.** `SettingsPayload.Revision` stamps them as they start being built, and the
  page discards any arriving with a lower revision: several producers repaint the whole window
  without queueing against each other, so the last to arrive is not the newest.
- **Fixed along the way, found by this work:** the tutorial compared the key state against
  `"configured"`, a value that does not exist, so anyone who already had a key was told "Paste your
  key"; and `TestUnportedProviderIsReported` used ElevenLabs, which is already ported, passing for
  the wrong reason. See `docs/solutions/silent-success-is-a-bug.md`.

## Fidelity to the original layout, and the UI that did not respond — 2026-07-29

- **The sidebar navigation was not wired**, and since every Settings control lives inside that view,
  the app responded to nothing. Also wired: the Settings tabs, the record button and the footer
  links.
- **History** ported faithfully: `.hrow` rows with expand and copy, the chevron only where the text
  is truncated, empty states in both variants, and recent activity with relative time. The inherited
  CSS expects those classes; a first attempt invented others and the rows came out unstyled.
- **Connections** with the real model ported (`connectionStatus.ts`): five states, Azure as two
  services with two keys and two required fields, and `unsupported` by platform/OS/helper.
- **Language pickers** by capability: chips with one-locale-per-base-language for Azure, full locales
  for Apple, base + "Automatic detection" for those with an optional hint.
- **System tab**: shortcut with key capture, appearance (which needed cgo because Wails only applies
  it when constructing the window), mode, device and interface language.
- **Permissions tab** with three-way state: what macOS does not allow querying is "unverified", not
  "missing".
- **The audio meters measured nothing with the local engines** — the helpers open the microphone
  themselves, so Go never saw levels. Whisper now reports them. And the idle pulse, which looked like
  continuous speech, was replaced with a flat line: three distinguishable states and none of them
  claiming audio that does not exist.
- **The pill**: shadow clipped against the window edge, then a halo too strong on a light background,
  and the half-pixel border removed.


## The app can be configured from the interface (phase 4) — 2026-07-28

- Setters in the Settings service (`SetProvider`, `SetRegion`, `SetKey`, `DeleteKey`,
  `SaveConnection`) and the DOM for the engine picker, the key fields and the regions dropdown.
  It closes the loop: until now you had to edit `settings.json` by hand to try a provider.
- The setters return `WriteResult{payload, error}` and **not** a Go error: Wails discards the result
  of a method that also returns an error, and the page needs the payload exactly when the write
  fails.
- **Keychain:** writing and deleting are now bounded (they used to hang the window), replacement uses
  `SecItemUpdate` instead of delete-then-add (which lost the old key if the add failed) and
  operations are serialised per slot.
- **Saving no longer deletes settings:** `Settings` is a declared subset of the model and Settings
  rewrites the whole file, so the write merges over the raw JSON. A `settings.json` with `null` made
  every write panic.
- **Azure:** choosing the OpenAI subservice and saving overwrote the Speech credential, and a
  region-only save could move the live endpoint. Both closed in backend and UI.
- Unported engines are no longer selectable, with a contract test that fails if the available list
  and `buildProvider`'s switch diverge.
- `logging.go` redacts binding arguments: Wails logs them and you end up with an API key in the log.


## Settings bootstrap payload (phase 4) — 2026-07-28

- `Settings.Load()`, a Wails service that returns in **one** call everything the Settings page needs
  to paint itself. Until now the app **could not be configured through the interface**: you had to
  edit `settings.json` by hand and pass keys through environment variables.
- Key presence becomes **three states** (`store.KeyStatus`: present / absent / unreadable).
  `HasKey` collapsed `ErrKeychainTimeout` into `false`, so on an ad-hoc build a slot that did have a
  key was reported empty — and that sends the user off to retype a credential that was already
  there.
- Slots resolved by `LOQUI_*_KEY` are not queried and the rest are read in parallel: in series it was
  15s (5 × 3s of timeout) with the page blank.
- Languages normalised per slot (`store.AllLanguageSlots`, `store.LanguagesIn`). Four cloud slots
  fell through to the `en-US` last resort, which pins a cloud engine to English instead of letting it
  auto-detect.
- `.task/` stops being tracked: it is Taskfile cache.


## Unreleased

### Reconnects now replace resources instead of orphaning them

- A retryable failure now closes the failed provider and microphone before waiting for the next
  attempt. Audio is deliberately not recorded or buffered during backoff.
- Providers, captures, pumps, idle guards, and reconnect timers are owned by their controller
  generation, so a stale stop cannot close newer resources and a user stop cannot be undone by a
  timer or resource that returns late.
- Retry delays, the cumulative six-retry budget, transcript delivery, and provider error
  classification are unchanged.

### Reconnect retries are now a real bound

- One dictation can schedule at most six reconnects even when every replacement connection opens
  before failing; the seventh retryable failure stops, keeps the terminal error visible, and delivers
  any transcript accumulated across generations exactly once.
- Grok in-socket server errors now retry behind that bound instead of being mislabeled as a client
  `BadRequest`. A permanent ambiguous error can therefore take about 61 seconds of backoff plus
  handshake time to surface, but it cannot create an open-ended billing loop.
- A late `Started` after terminal exhaustion no longer repaints a stopped session as listening.

### Grok (xAI) STT provider — phase 3 of the port

- **`internal/stt/grok`**: streaming dictation provider against `wss://api.x.ai/v1/stt`, over the
  `stt.Provider` contract. Binary PCM16 frames, header auth, `audio.done` to close. Wired into
  `buildProvider` and available as `cmd/stt-probe -provider grok`.
- **Electron's event parsing was NOT ported verbatim, because it loses text.**
  `grokStt.ts` takes the final only from `transcript.done`, and ignores the `is_final` /
  `speech_final` flags. The official `transcript.done` example carries `text: ""` after 6.43 s of
  audio ⇒ that mapping can deliver an **empty** dictation. The provider now assembles a word
  timeline and emits **one** final when it ends, which also absorbs "stitched" resends and server
  corrections without duplicating. Two replacement rules, according to what the protocol says about
  each event: a chunk final is **incremental** (per word), while an utterance final (`speech_final`)
  and `transcript.done` are **authoritative** for their span — which is what lets a retraction
  actually erase.
- **xAI's docs are wrong about the auth failure**: an invalid key returns **400**, not the documented
  401. The rejection body is read so it can be reported as a key problem rather than "invalid
  request". Verified against the real service.
- **Per-provider Keychain escape hatch**: `keyReaderFor` was Azure-only, so another provider's key
  was silently ignored. There is now one variable per slot (`LOQUI_GROK_KEY`, …) and one does not
  satisfy another's read.
- `store.DefaultSettings()` gains the `grok` language slot (`auto`), and `store.NewAt` so that other
  packages' tests do not write into `~/Library/Application Support`.
- Design, alternatives and the four review rounds: `docs/plans/grok-stt-provider.md`.
  API verification: `docs/research/2026-07-28-xai-stt-streaming.md`. The lesson transferable to the
  two remaining providers: `docs/solutions/do-not-port-stt-providers-verbatim.md`.
- **Outstanding**: real transcription (needs an xAI key). And the review uncovered **two pre-existing
  bugs** in `internal/session` + `internal/app` that affect Azure today — a retry budget that does
  not bound anything if the connection does open, and a capture leak on reconnection. Documented at
  the end of the plan; they go in their own change.
