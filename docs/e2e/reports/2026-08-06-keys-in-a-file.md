# E2E evidence — API keys in a file instead of the Keychain

VERDICT: PARTIAL

**PARTIAL on purpose, and the missing piece is the same one as always:** the green path
(`✓ Conexión correcta`, a real credential accepted by Azure) still has not been executed. The user's
Azure key exists — created 2026-08-03 — but it lives in the **login Keychain**, which this change
stops the app from reading. Migrating it needs an interactive Keychain authorisation dialog that only
the user can approve; it was attempted from the session and hung waiting for that prompt, and was
cancelled. Until they migrate it or paste it again, the slot is empty.

- **Date:** 2026-08-06
- **Branch:** `feat/secrets-in-a-file`
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

---

## What this report does NOT cover

1. **`✓ Conexión correcta`** — needs a real credential in the new store. See the header. The same gap
   as the previous two reports, now for a different reason: the key exists, it is just in the backend
   the app no longer reads.
2. **A write through the UI.** Saving a key from the card was not exercised against the packaged app,
   because doing so needs a credential to type and the only one available is the one that cannot be
   read. **No `secrets.json` has ever been created by a click** — every file in this work was written
   by a test or by hand, which is a weaker claim than having seen it appear from the interface.

   What the unit tests do cover, stated exactly because the first draft of this report overclaimed it
   (a cross-review P2): the round trip, the three states including a zero-byte and a corrupt file,
   per-slot isolation, refusal of unknown slots and blank secrets, `0600` on create and on rewrite,
   narrowing a file that arrives at `0644`, no secret in `settings.json`, no file created by reading,
   and — the ones that were missing — that a **failed** write leaves the previous contents intact and
   leaves no temp file behind. A regression to a plain in-place write now fails
   `TestAFailedWriteLeavesThePreviousFileAndNoTempBehind`; before those tests it would have kept the
   whole suite green.
3. **The cleartext trade itself** is a decision, not a defect, and not something E2E can verify.
   Recorded in `secrets.go`, the README, `.workflow/state.md` and the About view.
4. **Permission re-granting.** Unchanged by this work and still broken on every rebuild: same root
   cause (the signature), different symptom.
