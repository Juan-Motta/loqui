# E2E evidence — API keys in a file instead of the Keychain

VERDICT: PASS

**The first PASS in this project's E2E history, and it took a real credential to get there.** The
three previous reports were all PARTIAL for the same reason: `✓ Conexión correcta` had never been
executed, because there was no Azure key the app could read. Mid-session the user pasted one into the
new store and it worked, which closed that gap and five others behind it — the `active` connection
state, the save and select notices, the enabled Delete button, and the first real Azure transcription
in the port. Those are UC-6 through UC-9 below.

- **Date:** 2026-08-06
- **Branch:** `feat/secrets-in-a-file` (merged as `9f5b4c6`)
- **Target:** `bin/loqui.app`, repackaged and ad-hoc signed, against the real data directory
- **Starting state:** `provider=azure`, `region=eastus`, no `secrets.json` on disk

## UC-1 — the three-second hang is gone: PASS

This is the failure the whole change exists to remove. Compare the same three log lines, same
machine, same build configuration, hours apart:

```
BEFORE (Keychain backend, run of 2026-08-06 10:15)
  10:15:15 UI-PAINT       view rendered: map[provider:azure]
  10:15:18 ENGINE-CHECK   No se pudo comprobar la clave de Azure: el Keychain no respondió, ...

AFTER (file backend, run of 2026-08-06 13:53)
  13:53:23 CONN           rows:whisper=available macos=available azure=unconfigured ...
  13:53:23 UI-PAINT       view rendered: map[provider:azure]
  13:53:23 ENGINE-CHECK   Azure no está listo para dictar: se cambió a Whisper
```

- **Three seconds → the same second.** The gap was `store.GetKey`'s read timeout, paid on every
  launch because `SecItemCopyMatching` never returned. It is not "faster": the operation that used to
  time out now completes.
- The whole reason the check was deliberately scheduled *after* the first paint was that gap. That
  reasoning is now stale rather than wrong — the ordering still holds, it just costs nothing.

## UC-2 — an empty slot reads as ABSENT, not as unreadable: PASS

```
CONN-CARD  map[card:map[badge:Sin configurar  keyState:(no configurada)
           engineStatus:✓ Azure no está listo para dictar: se cambió a Whisper
           homeEngine:whisper  provider:azure  region:eastus
           delete:shown/disabled  test:shown/enabled  use:shown/disabled  save:shown/enabled]]
```

- `keyState:(no configurada)` — this morning the same card read
  `(el Keychain no respondió — la app no está firmada con una identidad estable)`. The three-state
  model is intact and the `unreadable` shield no longer fires by default.
- The button matrix is unchanged, which is the point: nothing about the UI contract moved.

## UC-3 — the engine fallback finally runs its MAIN branch: PASS

The most valuable line in this report, because it had never been executed. The fallback shipped on
2026-08-02 with two branches: move off an unusable engine, or refuse to move when the credential
could not be *checked*. On this machine the Keychain always hung, so **every** real run took the
refusing branch (UC-8 of the previous report). With a readable credential store, the main branch
runs:

```
(disk before)  provider = azure
ENGINE-CHECK   Azure no está listo para dictar: se cambió a Whisper
CONN           rows:whisper=active  macos=available azure=unconfigured ...
(disk after)   provider = whisper
```

- Azure has no key and no shield, so it is genuinely unusable → the app moves to Whisper and says so.
- **`✓` not `✗`**: it travels over `engine:changed`, so an actual change gets the tick, and the
  "could not check" case keeps the cross. Both tones verified live, in different runs.
- `settings.json` diffed before and after: **only** `provider` changed, azure → whisper. Nothing else
  in the user's configuration moved.

## UC-4 — a DAMAGED credentials file does not move the engine: PASS

Added after a cross-review P1. The first version of the store treated a zero-byte `secrets.json` as
"no keys", on the false premise that its own writer could produce one. It cannot — the destination is
only replaced by renaming an already-synced file — so zero bytes means external damage, and the
contents are unknown rather than absent. Reported as "absent", the fallback would have moved the user
off a working engine over a truncated file.

Arranged by truncating the file to zero bytes with `provider=azure`:

```
14:10:48 ENGINE-CHECK   No se pudo comprobar la clave de Azure: no se pudieron leer las claves
                        guardadas, así que el motor no se ha cambiado
14:10:54 CONN-CARD      ... engineStatus:✗ No se pudo comprobar la clave de Azure: ...
(disk before and after) provider = azure
```

- **The engine did not move.** The shield fired for the right reason, on the right input.
- **The message names the credentials, not the Keychain** — the wording a second cross-review P1
  found still pointing at the old backend.
- **`✗` not `✓`**, over `engine:blocked`: "could not check" is not an accomplishment.

## UC-5 — reading does not create the file: PASS

After a full launch that read every slot, `secrets.json` **does not exist**. Consistent with
`TestReadingDoesNotCreateTheFile`: an empty credentials file is indistinguishable from one whose
contents were lost, so nothing is created until something is stored.

## UC-6 — a key saved FROM THE INTERFACE: PASS

Performed by the user, not by a probe, which is what makes it worth more than the rest: they opened
the packaged app and pasted an Azure key into the card. What the session verified is the artefact it
left, and only that — the click itself was not observed:

```
-rw-------@  57 bytes  6 Aug 14:20  secrets.json
slots: {'azure-speech': '<32 characters>'}
mode : 0o600
```

- **The file appeared from a click**, which was declared as an uncovered gap in the first draft of
  this report. It no longer is.
- **`0600` on a file this code created through the real app**, not through a test.
- `provider` stayed `azure`: the launch check found a usable engine and left it alone.

## UC-7 — `✓ Conexión correcta`, against the real Azure: PASS

The one that had never run. `LOQUI_DEBUG_CONN_CLICK=azure:test` with the key field empty, so the probe
resolves the credential the same way dictation would:

```
CONN         rows:whisper=available macos=available azure=active openai=unconfigured ...
PROBE        slot=azure-speech region=eastus2 source=stored
PROBE-DONE   slot=azure-speech ok=true
UI-PROBE     map[error: field: ok:true provider:azure]
CONN-CARD    badge:Activo  badgeClass:conn-state active  delete:shown/enabled
             keyState:(guardada — escribe una nueva para reemplazarla)
             status:✓ Conexión correcta: Azure aceptó la clave para esa región
             statusClass:status ok  test:shown/enabled  use:hidden/disabled
             engineStatus:
```

Six things closed at once, every one of them previously unexecuted:

- **`ok=true` from a real token exchange** with `eastus2`. Page → binding → region and key resolution
  → HTTPS to Azure's STS → `✓`. The whole chain, green.
- **`source=stored`** — it read the new file backend. The credential store works end to end through
  the packaged app, not just in tests.
- **`azure=active` / `badge:Activo`** — the fifth connection state, which no report had ever seen
  because Azure had never been fully configured.
- **`delete:shown/enabled`** — "Borrar clave" enabled for the first time; it needs a key to exist.
- **`use:hidden/disabled`** — hidden on the active engine, as the original does. The matrix row that
  was unreachable while Azure was never active.
- **`engineStatus:` empty and zero `ENGINE-CHECK` lines** — the launch check said *nothing*, which is
  the correct outcome for a healthy engine and what `TestAUsableEngineIsLeftAloneAndSaysNothing`
  claims. A notice here would be noise on every launch.

**The secret does not appear in the log**, checked by searching the run for the credential and for
every 12-character substring of it: both absent.

## UC-8 — `✓ Región guardada` and `✓ Motor activo: Azure`: PASS

The other two success notices that had only ever been pinned by Go tests. Arranged by moving the
engine to Whisper first, so that "Usar este motor" is visible on the Azure card again, then chaining
both actions:

```
UI-ACTION  map[action:saveConnection(azure) error: field: notice:Región guardada ok:true]
UI-ACTION  map[action:setProvider(azure) error: field: notice:Motor activo: Azure ok:true]
CONN-CARD  status:✓ Motor activo: Azure  statusClass:status ok
           badge:Activo  use:hidden/disabled
(disk after) provider = azure
```

- **"Región guardada", not "Clave guardada"** — and that is the correct notice, not a near miss: the
  key field was empty, so the stored credential was left alone and only the region was written. The
  notice names what was *actually* written, which is the whole reason it is computed in Go.
- **`Motor activo: Azure`** needed the `ConnActive` branch of `providerNotice`, reachable only with a
  configured engine.
- `use` hides itself again once the engine is active, so the chain also shows the transition.

## UC-9 — the first real Azure transcription in the port: PASS (user-performed, inferred from the artefact)

**Attribution matters here: the user did this, and the session only read what it left behind.** No
paste was observed and no audio was measured. What `history.jsonl` holds:

```
2026-07-29 22:49  lang=auto     trigger=hold  chars=11
2026-07-30 11:30  lang=auto     trigger=hold  chars=11
2026-08-06 14:21  lang=es-CO    trigger=hold  chars=25
2026-08-06 14:21  lang=es-CO    trigger=hold  chars=186
2026-08-06 14:22  lang=es-CO    trigger=hold  chars=18
2026-08-06 14:22  lang=es-CO    trigger=hold  chars=21
```

The inference, with its reasoning rather than as a bare claim: `lang=es-CO` is a **detected** locale,
and only the Azure path reports one. Whisper stores the *setting* instead — a documented wart, and the
two 2026-07 records show it as `lang=auto`. `provider` was `azure` at 14:21, one minute after the key
was saved. A 186-character record is a real multi-sentence dictation that was pasted and stored.

This is what phase 1 of the port has carried as outstanding since 2026-07-27: "DONE except real
transcription (needs a key)". The full chain — `fn` key → capture → Azure v2 endpoint with continuous
LID → transcript → paste → history — has now run for a real user. **Transcript text was never read
by the session**: only the record count, the timestamps, the language field and the character counts.

---

## What this report does NOT cover

The two gaps this report was drafted with — the green path and a write through the UI — are **closed**
by UC-6 and UC-7. What remains is either not a defect or not this change's business:

1. **The cleartext trade itself** is a decision, not a defect, and not something E2E can verify.
   Recorded in `secrets.go`, the README, `.workflow/state.md` and the About view.
2. **Permission re-granting.** Unchanged by this work and still broken on every rebuild: same root
   cause (the signature), different symptom. Only signing closes it.
3. **The paste and the audio in UC-9 were not observed.** The user dictated; the session read
   `history.jsonl` afterwards. The conclusion "Azure transcribed this" is an inference from the
   `language` field, argued in UC-9 rather than asserted.
4. **Deleting a key that exists** — now finally reachable (`delete:shown/enabled`) and not exercised.
   It is the action that destroyed a credential in silence on 2026-08-01, so it deserves its own run:
   the notice, the postcondition wording, and the fallback moving off Azure the moment its key goes.
5. **The other four providers' credentials.** OpenAI, Grok, ElevenLabs and Azure OpenAI were not
   stored or read through the app. Two of them *cannot* be, which is the next thing to fix:
   `availableKeySlots` lists only `azure-speech` and `grok`, so the UI refuses to save an OpenAI or
   ElevenLabs key at all.

### What the unit tests cover, stated exactly

Because the first draft of this report overclaimed it (a cross-review P2): the round trip, the three
states including a zero-byte and a corrupt file, per-slot isolation, refusal of unknown slots and blank
secrets, `0600` on create and on rewrite, narrowing a file that arrives at `0644`, no secret in
`settings.json`, no file created by reading, and — the ones that were missing — that a **failed** write
leaves the previous contents intact and leaves no temp file behind. A regression to a plain in-place
write now fails `TestAFailedWriteLeavesThePreviousFileAndNoTempBehind`; before those tests it would
have kept the whole suite green.
