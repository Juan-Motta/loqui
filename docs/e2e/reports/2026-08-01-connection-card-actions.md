# E2E — the connection card's actions (Azure)

VERDICT: PARTIAL

**PARTIAL on purpose, and this is the only thing missing:** the green path of "Test connection" —
`✓ Conexión correcta` — has not been executed, because there is no valid Azure key on this machine.
The very bug this change fixes deleted it: the user pressed "Delete key" on 2026-08-01, the call
succeeded and said nothing, and the `azure-speech` item vanished from the Keychain (checked with
`security find-generic-password -s com.jualopezmo.loquigo`). Putting PASS while claiming a journey
nobody has walked would be signing off as tested something that is not.

Everything else is verified against `bin/loqui.app`, packaged and ad-hoc signed, with the probe
output pasted verbatim.

- **Date:** 2026-08-01
- **Build:** `bin/loqui.app`, `provider=azure`, `region=eastus`, `azure-speech` slot **empty**
- **Interface:** UI (Settings' only surface)
- **How it is driven:** `LOQUI_DEBUG_CONN_CLICK` / `LOQUI_DEBUG_CONN_REPORT` — a button inside a
  Wails webview cannot be clicked from a script, so the probes dispatch REAL clicks on the same
  handlers the mouse reaches. The card report is taken 6 s later, to measure the state it SETTLES
  in and not the intermediate "…".

---

## UC-5 — saving without a key: PASS

Asked for by the user on 2026-08-01: "if there is no api key configured, clicking save should
highlight the key field in red and a message saying the key is required".

```
CONN-CLICK  ran:save
UI-ACTION   map[action:saveConnection(azure) error:la clave es obligatoria: pégala antes de guardar
            field:key notice: ok:false]
CONN-CARD   map[card:map[badge:Sin configurar  invalid:key
            status:✗ la clave es obligatoria: pégala antes de guardar  statusClass:status err
            test:shown/enabled  use:shown/disabled  delete:shown/disabled  save:shown/enabled]]
```

- The flagged field is the right one: `invalid:key` — the class is applied by the page from
  `WriteResult.Field`, which Go decides.
- The message stays written and in red (`statusClass:status err`), it is not cleared.
- **Nothing was written**: the region did not move.

## UC-1a — testing without a key: PASS

```
CONN-CLICK  ran:test(nokey)   status:Probando la conexión…   test:shown/disabled
UI-PROBE    map[error:falta la clave: escríbela o guárdala antes de probar field:key ok:false]
CONN-CARD   invalid:key   status:✗ falta la clave: escríbela o guárdala antes de probar
            statusClass:status err   test:shown/enabled
```

- **There is no `PROBE` line**, which is the one the probe emits when it resolves its inputs:
  validation cut in before going to the network, as the plan requires.
- The button is disabled while it is in flight and available again afterwards. It never disappears.

## UC-1b — testing with an invalid key, against the real Azure: PASS

This is the one that walks the whole chain: page → binding → region and key resolution → real HTTPS
to `eastus`'s STS endpoint → classification of the 401 → message.

```
CONN-CLICK  ran:test(badkey)
PROBE       slot=azure-speech region=eastus source=typed
PROBE-DONE  slot=azure-speech ok=false
UI-PROBE    map[error:Clave o región inválida (401/403) field: ok:false provider:azure]
CONN-CARD   status:✗ Clave o región inválida (401/403)  statusClass:status err  test:shown/enabled
```

- `source=typed` confirms it used the key from the form (the fixed invalid sentinel
  `loqui-debug-clave-invalida`, never a real credential from the environment).
- The secret does not appear in any line of the log.
- `field:` empty: a credential rejected by Azure is not a badly filled field, so no red border is
  painted.

## UC-2 — the button matrix by state: PASS

Azure unconfigured, across the three captures above:

| Button | State | Correct because |
|---|---|---|
| Test connection | `shown/enabled` | it is used precisely when the engine is NOT properly configured |
| Use this engine | `shown/disabled` | asked for by the user: only with the connection saved |
| Delete key | `shown/disabled` | there is no key to delete |
| Save | `shown/enabled` | it is the action that fixes the state |

And the local engines, which the new rule must not break:

```
CONN      rows:whisper=available macos=available azure=unconfigured openai=unconfigured ...
CONN-CARD map[card:map[provider:whisper badge:Disponible badgeClass:conn-state available
          use:shown/enabled  test:absent  delete:absent  save:absent]]
```

`available` is the state of Whisper and macOS, which carry no credential: they stay selectable.
Treating "no key" as "unconfigured" would have left them unusable.

## Message layout: PASS (verified by screenshot)

The user reported that the message "doesn't look right" beside the buttons: `.conn-actions` was a
flex row and the status ended up in a ~60 px column breaking one or two words per line. It now takes
its own row above the buttons, left-aligned, and when empty it reserves no space. Checked with a
screenshot of the packaged app, not by inspecting the CSS.

## UC-6 — a test does not touch the form: PASS

A code-review finding (iteration 4): `paint()` fills the region picker from what is SAVED, so
testing a region before saving it silently reverted it to the previous one — and the next Save would
have persisted the key against a region different from the one tested. It is the same trap that had
already been fixed for the key.

```
CONN-CLICK  ran:set-region(westeurope) | test(badkey)
PROBE       slot=azure-speech region=westeurope source=typed
UI-PROBE    map[error:Clave o región inválida (401/403) field: ok:false provider:azure]
CONN-CARD   map[card:map[... provider:azure region:westeurope ...]]
```

And on disk, afterwards: `region = eastus`.

- The test used the region **from the form** (`region=westeurope`), not the saved one.
- After the repaint, the picker is **still** on `westeurope`: the unsaved choice survives.
- Disk did not move: a probe writes nothing.

## UC-7 — the engine that cannot dictate does not stay selected: PASS

Asked for by the user on 2026-08-02: "if the engine isn't configured it should switch to the default
engine which is whisper". The starting state is the one the bug left: `provider=azure` with no key.

```
(disk before)  provider = azure
CONN           rows:whisper=available macos=available azure=unconfigured ...
UI-PAINT       view rendered: map[provider:azure]
ENGINE-CHECK   Azure no está listo para dictar: se cambió a Whisper
CONN           rows:whisper=active  macos=available azure=unconfigured ...
(disk after)   provider = whisper
```

On screen, checked by screenshot: the picker moves to "Whisper — local (offline)", the orange "this
engine needs configuration" warning disappears, and under the card, in green, sits
`✓ Azure no está listo para dictar: se cambió a Whisper`.

- The order matters and it shows in the log: the page paints Azure first and the check repaints
  after. That is deliberate — the check reads the Keychain, and putting it before the first paint
  would be up to three seconds of empty window.
- **It is not silent.** Something the user chose stops being in effect; finding that out by
  noticing a different name in the dropdown would be worse than being told.

The two cases that must NOT move anything are pinned by Go tests, not by E2E, because provoking them
in the real app requires a Keychain that does not answer and a Whisper with no model:
`TestUnreadableCredentialsNeverChangeTheEngine` and
`TestNoFallbackWhenTheDefaultEngineCannotRunEither`.

## Hero alignment: PASS (measured in pixels)

The user reported that the description did not look vertically centred. Measured on the packaged
app, before: logo tile `y=55..100` (centre 77.5), select `60..94` (centre 77), the text block's ink
`59..100` (centre **79.5**) — 2 px low, with the subtitle ending exactly level with the tile's bottom
edge, which is what made it read as stuck to the bottom.

After: tile centre **76.5**, ink **77.0**, select **76.0** — all within half a pixel.

## UC-8 — the "I could not check it" path: PASS (run of 2026-08-06)

Added when closing the increment, and it is the case that **must not move anything**. It is the one
the machine produces on its own: ad-hoc signing makes the Keychain not answer, so this is the common
path on this build, not the rare one.

```
10:15:15 UI-PAINT       view rendered: map[provider:azure]
10:15:18 ENGINE-CHECK   No se pudo comprobar la clave de Azure: el Keychain no respondió, así que el motor no se ha cambiado
10:15:21 CONN-CARD      ... engineStatus:✗ No se pudo comprobar la clave de Azure: ... homeEngine:azure ... provider:azure
(disk before and after) provider = azure
```

- **Exactly three seconds** between the paint and the check — the Keychain timeout, which is the
  documented reason the check runs after the first paint and not before.
- **`✗`, not `✓`.** It travels over `engine:blocked`, so "could not check" carries no green tick.
- **`homeEngine: azure` and disk untouched:** a timeout does not take a working configuration away
  from the user.
- The debug reporter now includes `engineStatus` and `homeEngine` — without that, a stranded
  sentence on the home line is invisible to a per-card report.

Second run, with a card action in flight (`azure:test`): the card shows its own
`Probando la conexión…` while `engineStatus` holds the check's sentence. **Declared trade-off:**
`paint()` now clears `engineStatus`, so when that action completes the check's sentence disappears.
Useful information ("I could not verify your key") is lost on the user's next action; the
alternative was leaving it and having it contradict the screen, which is worse. Every other status
line on the page already behaves this way — they belong to an action and are rewritten with it.

---

## What this report does NOT cover

1. **`✓ Conexión correcta`** — needs a valid Azure key. See the header.
2. **`✓ Clave guardada` and `✓ Motor activo: Azure`** — the success path of Save and of "Use this
   engine" has not been executed either, for the same reason: without a valid key you never reach
   `connected`. The text is pinned by Go tests (`TestASuccessfulWriteCarriesSomethingToSay`,
   `TestTheSaveNoticeNamesWhatWasActuallyWritten`), which is a different and weaker claim than
   having seen it on screen.
3. **UC-3 and UC-4 (concurrency)** — `await writes` and the epoch/revision arbitration. They need a
   valid configuration for the two chained actions to make sense, so they go with the next report.
4. **That the page applies the probe's payload** — a gap declared in the plan: its only real cause
   is a Keychain write that exhausts its deadline and lands late, and provoking that at will would
   mean putting a hook into production that fakes the timeout of a credential write.
5. **The stranded sentence itself** — the bug that clearing `engineStatus` in `paint()` fixes.
   Reproducing it requires the check's sentence to be on screen **and then** a card action, and the
   `LOQUI_DEBUG_CONN_CLICK` affordance fires on the first `ui:painted`, that is **before** the check
   gets to speak (it takes the Keychain's three seconds). Sequencing it would need a delay in that
   affordance. What is verified live is the mechanism: the sentence appears, is painted with the
   right class and survives its own repaint (UC-8). The fix itself rests on reading the code:
   `run()` calls `paint()` before its own `say()`, and `isCurrent(null, …)` is always true, so no
   path that writes there is left blank.
6. **The `confirmNotice` window** — the gap between the check at the top of `repairEngine` and the
   sentence. In the real app it is microseconds (a settings read and a `stat`), so it is not
   provoked by hand; it is pinned by `TestANoticeIsWithdrawnWhenTheWorldMovesUnderIt`, with the seam
   injected where the real gap is.
