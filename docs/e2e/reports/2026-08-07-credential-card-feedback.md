# E2E — feedback al guardar una credencial: plegar y enmascarar

VERDICT: PASS

- **Feature:** the credential cards fold themselves after a successful save, and show a fixed mask
  when the app already holds a key for the slot
- **Branch:** `feat/credential-card-feedback`
- **Run:** 2026-08-07T10:11-10:16-05:00 — seven app launches against the real packaged app
- **Build:** `bin/loqui.app`, repackaged from this tree with `./scripts/task.sh package`, ad-hoc
  signed. Go suite green (14 packages), `gofmt` and `vet` clean, `tsc --target es2022` clean.
- **Stored state at the start:** all four slots hold a real credential;
  `secrets.json` sha256 `1217ba832e398de6…`, `provider=azure`, `region=eastus2`

## How it is driven

Same substitute as every prior report in this project — a Wails WKWebView has no served origin, so
the skill's Playwright harness does not apply. `LOQUI_DEBUG_CONN_CLICK` dispatches **real clicks** on
the handlers a mouse reaches; `LOQUI_DEBUG_CONN_REPORT` reports the state the card **settled** on six
seconds later.

Three observables were added for this change, and **all three are classifications, never values**:

| Observable | Values | Why it had to exist |
| --- | --- | --- |
| `keyField` | `empty` / `masked` / `typed` | the field's state without its contents |
| `formOpen` | bool | the fold is otherwise invisible from outside |
| `KEY-SENT kind` | `typed` / `empty` / `masked-blocked` | **what was actually sent** |

The last one is the important one. Watching the settled field cannot tell "the mask was blocked"
apart from "the mask was stored as the key": both leave the slot reading `present` and the field
showing the same mask. The design review raised exactly this, and reporting the *value* instead was
its P0 — card reports are written to the app log verbatim (`wiring.go:148`).

---

## UC-MASK-01 — I open a card whose key I saved earlier: PASS

- **Actor:** someone returning to a provider they configured before.
- **Intent:** see that there IS a credential, without the app ever showing it.

```
CONN-CARD  provider:openai  keyField:masked
           keyState:(guardada — escribe una nueva para reemplazarla)  badge:Conectado
```

The field reads as filled. What is in it is a **fixed twelve-character constant** — not the key, not
derived from it, and its length says nothing about the real one. The decision to mask comes from
`KeyState.Stored`, computed in Go.

**Persistence:** none; opening a card writes nothing.

## UC-MASK-02 — I press Guardar without touching the masked field: PASS

**This is the case the whole guard exists for.** If the mask were sent, it would overwrite a working
credential with twelve asterisks.

```
KEY-SENT    map[action:save kind:masked-blocked provider:openai]
CONN-CLICK  ran:save(asis)   keyField:empty   save:shown/disabled   status:Guardando…
UI-ACTION   map[action:saveConnection(openai) error:no hay nada que guardar ok:false]
CONN-CARD   status:✗ no hay nada que guardar   formOpen:true   keyField:masked
secrets.json sha256 after: 1217ba832e398de6…   (unchanged)
```

- `kind:masked-blocked` — the mask was withheld and empty was sent, which the backend already reads
  as "leave the stored key alone" (`settings_write.go:466`).
- **The credential file is byte-identical afterwards.** That is the assertion that matters; the rest
  is corroboration.
- `no hay nada que guardar` is correct and is what the backend already said for a provider with no
  region: nothing was offered, so nothing is missing.
- **`formOpen:true`** — the write failed, so the card did NOT fold. Deliberate: the message and the
  red border it explains both live inside the form.

**Persistence:** re-read after the action — credential unchanged, and the repaint put the mask back.

## UC-MASK-03 — I save successfully and the card folds itself: PASS

Run on Azure, where an untouched masked field still saves the region — so the success path is
exercised **without writing any credential**.

```
KEY-SENT    map[action:save kind:masked-blocked provider:azure]
CONN-CLICK  ran:save(asis)   formOpen:true    status:Guardando…
UI-ACTION   map[action:saveConnection(azure) notice:Región guardada ok:true]
CONN-CARD   status:✓ Región guardada   statusClass:status ok   formOpen:false
secrets.json sha256 after: 1217ba832e398de6…   (unchanged)
```

- `formOpen` goes **true → false**: the card folded on its own, ~1.2 s after the ✓.
- The ✓ was on screen first. Folding immediately would have taken the confirmation down with it —
  the status line lives inside `.conn-form`.
- **This also settles a design-review finding.** The plan claimed an untouched masked Azure card
  would answer "no hay nada que guardar"; it answers `Región guardada`, because the page always sends
  the region and `SaveConnection` writes it when supplied. The behaviour is right, the plan's claim
  was wrong, and it is corrected there.

## UC-MASK-04 — the mask never overwrites what I am typing: PASS

`paint()` runs after **every** write in the window — Sistema, idiomas, onboarding and the permissions
refresh all repaint — so a mask applied unconditionally would wipe a key mid-paste.

```
CONN-CLICK  ran:test(badkey)          keyField:typed
PROBE       slot=openai region= source=typed
CONN-CARD   keyField:typed            (after the probe's repaint)
```

The typed value survived a full repaint. Only an **empty** field is ever masked.

## UC-MASK-05 — the debug driver does not lie to its own test: PASS

Raised by the design review as a P2, and it would have corrupted this very report.

```
KEY-SENT    map[action:test kind:typed provider:openai]
PROBE       slot=openai region= source=typed
PROBE-DONE  slot=openai ok=false code=invalid_api_key
```

`debugConnStep` used to assign `key.value` directly, leaving the mask mark in place. On an
already-masked card the invalid sentinel would then have been classified as a mask, empty would have
been sent, Go would have tested **the real stored key**, and this negative case would have come back
`ok=true` — an E2E proving the opposite of what it claims. It now writes through the same helper a
keystroke uses: `kind:typed`, `source=typed`, rejection.

## UC-MASK-06 — deleting the key removes the mask: PASS

The inverse transition, missing from the first draft of the plan and raised as a P1.

```
UI-ACTION  map[action:deleteKey(openai) notice:La clave ya no está guardada ok:true]
CONN-CARD  keyField:empty   keyState:(no configurada)   badge:Sin configurar
           delete:shown/disabled   formOpen:true
```

Without this, a deleted key left its mask behind and the card went on claiming a credential that no
longer existed. `formOpen:true` is also correct: only Guardar folds.

**Persistence / cleanup:** this case destroys a credential, so the file was **backed up before and
restored after**; `sha256` returned to `1217ba832e398de6…`. No residual state.

## UC-MASK-07 — a ✓ never lands beside a key nobody tested: PASS

Pre-existing defect, found by the cross-engine review of the **previous** branch and deferred here on
purpose, because this is the change that edits that handler.

```
KEY-SENT    map[action:test kind:masked-blocked provider:openai]
PROBE       slot=openai region= source=stored
PROBE-DONE  slot=openai ok=true code=
UI-PROBE    map[error:stale-form ok:false provider:openai]
CONN-CARD   status:El formulario cambió durante la prueba — vuelve a probar
            statusClass:status        keyField:typed
```

Go really did say the stored key is good — `ok=true`. The page **refused to show it**, because by
then the field held something else. Note `statusClass:status`: neither ✓ nor ✗, because nothing was
proved or disproved about what is on screen now. It is said rather than swallowed — an empty status
line is indistinguishable from a click that never arrived.

## UC-MASK-08 — the same, for Azure's region: PASS

The half the plan first forgot; a probe captures both the key and the region.

```
PROBE       slot=azure-speech region=eastus2 source=stored
PROBE-DONE  slot=azure-speech ok=true code=
UI-PROBE    map[error:stale-form ok:false provider:azure]
CONN-CARD   status:El formulario cambió durante la prueba — vuelve a probar   region:westeurope
(disk after) region = eastus2
```

Tested against `eastus2`, switched to `westeurope` mid-flight, no ✓. Two older guarantees still hold
alongside the new one: the unsaved `westeurope` **survives** on screen (UC-6 of
`2026-08-01-connection-card-actions.md`), and a probe writes nothing — the region on disk did not
move.

---

## Go tests behind this

`KeyState.Stored` is decided in Go and covered by four cases, **each verified by mutation**: a key the
app stored, an empty slot, an env-var key, and unreadable credentials. The one that earns its place is
the env-var case: such a key IS present and dictation will use it, but the app never stored it, so a
mask there would claim a credential it cannot read or delete. Removing `&& !FromEnv` kills that test
and only that test.

The payload's schema guard (`TestAKeyStateCarriesPresenceAndNothingElse`) fired on its own when the
field was added, which is the guard working: it pins that every field is a fact *about* the credential
and none is derived from its value.

## What this report does NOT cover

1. **The 1.2 s fold being cancelled by a keystroke or a reopen.** The cancellations are wired to
   `beforeinput`, the region `change`, the toggle, and `beginAction`. Reproducing the race from outside
   needs a step that fires *during* the 1.2 s window; the `+` grammar runs its steps immediately, so the
   timing cannot be expressed today. **Read the code, not this report, for that guarantee** — and it is
   the weakest claim in this change.
2. **What the mask looks like to a human.** `keyField:masked` says the page put a mask there; that it
   renders as twelve dots rests on the input being `type="password"`, which is markup, not behaviour.
3. **A successful save of a NEW typed key, and its fold.** UC-MASK-03 exercises the success path via
   Azure's region because typing a key and saving it would overwrite a real credential on this machine.
   The guard against storing the mask is proven directly (UC-MASK-02); what is not separately proven is
   that a genuinely typed key still stores — that path is unchanged by this branch and covered by
   `2026-08-06-keys-in-a-file.md`.
