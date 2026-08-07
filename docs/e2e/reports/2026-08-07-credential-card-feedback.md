# E2E — feedback al guardar una credencial: plegar y enmascarar

VERDICT: PASS

- **Feature:** the credential cards fold themselves after a successful save, and show a fixed mask
  when the app already holds a key for the slot
- **Branch:** `feat/credential-card-feedback`
- **Run:** 2026-08-07T10:11-10:16 and 12:47-12:56-05:00 — fifteen app launches against the real
  packaged app
- **Build:** `bin/loqui.app`, repackaged from this tree with `./scripts/task.sh package`, ad-hoc
  signed. Go suite green (14 packages), `gofmt` and `vet` clean, `tsc --target es2022` clean.
- **Stored state at the start:** all four slots hold a real credential;
  `secrets.json` sha256 `1217ba832e398de6…`, `provider=azure`, `region=eastus2`

## How it is driven

Same substitute as every prior report in this project — a Wails WKWebView has no served origin, so
the skill's Playwright harness does not apply. `LOQUI_DEBUG_CONN_CLICK` dispatches **real clicks** on
the handlers a mouse reaches; `LOQUI_DEBUG_CONN_REPORT` reports the state the card **settled** on six
seconds later.

Four observables were added for this change, and **every one is a classification, never a value**:

| Observable | Values | Why it had to exist |
| --- | --- | --- |
| `keyField` | `empty` / `masked` / `revealed` / `typed` | the field's state without its contents |
| `eye` | shown/hidden + enabled/disabled | the reveal button's state |
| `keyVisible` | bool | whether the characters are READABLE — a different question from who put them there |
| `formOpen` | bool | the fold is otherwise invisible from outside |
| button state | `shown/disabled/busy` | `disabled` alone cannot say whether a control is WORKING or was switched off |
| `KEY-SENT kind` | `typed` / `empty` / `masked-blocked` / `revealed` | **what was actually sent** |

The last one is the important one. Both `masked-blocked` and `revealed` mean *nothing was sent* —
the page held content it did not put there on the user's behalf, so it withheld it. Watching the settled field cannot tell "the mask was blocked"
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

## UC-MASK-03 — I press Guardar: the button spins, then the card folds: PASS

Run on Azure, where an untouched masked field still saves the region — so the success path is
exercised **without writing any credential**.

```
KEY-SENT    map[action:save kind:masked-blocked provider:azure]
CONN-CLICK  ran:save(asis)   formOpen:true   save:shown/disabled/busy   status:Guardando…
UI-ACTION   map[action:saveConnection(azure) notice:Región guardada ok:true]
CONN-CARD   formOpen:false   save:shown/enabled
secrets.json sha256 after: 1217ba832e398de6…   (unchanged)
```

- In flight: `save:shown/disabled/busy` — the button is dead to a second click AND says why.
- On landing: `formOpen` **true → false**, immediately, and the spinner is gone with the button live
  again.
- **This is the owner's revision of 2026-08-07**, replacing a first design that held `✓ Clave
  guardada` on screen for 1.2 s before folding. The spinner moved the feedback to *during* the write,
  so the delay after it stopped earning its keep. Stated plainly because it is a real loss: **the ✓ is
  no longer read.** What confirms the save now is the row badge, on the folded card.
- **It also settles a design-review finding.** The plan claimed an untouched masked Azure card would
  answer "no hay nada que guardar"; it answers `Región guardada`, because the page always sends the
  region and `SaveConnection` writes it when supplied. The behaviour is right, the plan's claim was
  wrong, and it is corrected there.

## UC-MASK-03b — a save that FAILS keeps the card open: PASS

The other half, and the reason folding is tied to success rather than to completion.

```
KEY-SENT    map[action:save kind:masked-blocked provider:openai]
CONN-CLICK  formOpen:true  save:shown/disabled/busy  status:Guardando…
UI-ACTION   map[action:saveConnection(openai) ok=false]
CONN-CARD   formOpen:true  save:shown/enabled  status:✗ no hay nada que guardar
```

The spinner clears, the button comes back, and **the form stays open** — the message and the red
border it explains both live inside it, so folding would hide the complaint together with the field
to fix.

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

## UC-EYE-01 — pulso el ojo y veo mi clave: PASS

Asked for by the owner after the rest was built, and it is the one place the secret is allowed to
cross. Fetched on the press — it is in no payload.

```
CONN-CLICK  ran:eye        keyField:masked   (at click time, before the fetch lands)
REVEAL      slot=openai ok=true
CONN-CARD   keyField:revealed   eye:shown/enabled
```

- `REVEAL slot=openai ok=true` records **the act**, never the value.
- **The credential does not reach the log.** Scanned the whole run for any 8-character window of the
  four real stored keys, vendor prefix excluded: **0 hits**, on a run where the key was demonstrably
  on screen. This is the assertion that matters, not `keyField:revealed`.

## UC-EYE-01b — the second press HIDES, it does not fetch again: PASS

```
ran:    eye | wait(1500) | eye
REVEAL calls across the whole run: 1
CONN-CARD  keyField=masked   eye=shown/enabled
```

One fetch for two presses. Before the fix there were two: clicking the eye while the field had focus
fired the input's `blur` first, which re-masked, so the click then saw a hidden field and revealed
again — the toggle never turned anything off. It only bites once the user has clicked into the field,
which is exactly what they do to select and copy the key. Fixed by `preventDefault` on `mousedown`.

## UC-EYE-05 — an EDITED revealed key does not stay legible: PASS

The nastiest of the review findings, because the credential on screen was the real one.

```
ran:        eye | wait(1200) | set-key(other) | wait(300) | blur-key
CONN-CARD   keyField=typed   keyVisible=false
```

Reveal, edit, look away — and the characters are behind dots again. Before the fix, the first
keystroke dropped the "revealed" mark and cancelled the 15 s timer while leaving `type="text"`, so
every automatic way back was switched off at once and the key stayed readable indefinitely.

The root cause was mine and worth naming: **visibility and provenance were the same flag.** They are
now separate — `type="text"` is whether the characters can be read, the WeakSets are who put them
there — and `keyVisible` exists in this report precisely because provenance alone could not see the
bug.

## UC-EYE-02 — it goes back behind the mask, three ways: PASS

A credential left on screen outlives the reason it was shown, and this window can stay open for hours.

```
A) ran:eye | wait(1500) | eye         → settled keyField=masked
B) ran:eye | wait(1500) | blur-key    → settled keyField=masked
```

Both re-mask. The third path — a 15 s auto-hide timer — is wired but is **not** exercised here; see
"What this report does NOT cover".

**`wait` had to be added to the step grammar to test this at all.** The first attempt,
`eye+eye`, produced one reveal and no toggle: steps run in one tick, and the first press disables the
button while it fetches, so the second click hit a dead control. That is correct behaviour, but it
meant the test proved nothing. `wait` also closes a gap this report's predecessor had to declare open.

## UC-EYE-03 — the eye is dead where the app holds nothing: PASS

```
stored key      eye=shown/enabled   keyField=masked
                keyState=guardada — escribe una nueva para reemplazarla

env-var slot    eye=shown/disabled  keyField=empty
                keyState=definida por variable de entorno — no se puede borrar desde aquí
```

An env-var credential is not the app's to show: it did not store it, cannot delete it, and returning
its value would make the button answer a different question depending on the slot, with nothing on
screen to say which. Disabled rather than hidden — a control that vanishes reads as a rendering bug.

**Go refuses it independently**, because the binding is reachable from the webview whatever the button
does: `TestRevealKeyRefusesWhatItCannotHonestlyShow/a_key_supplied_by_the_environment` asserts the
refusal names the variable and that **neither** credential appears in it. That path is defence in
depth — the UI does not let you reach it.

## UC-EYE-04 — the deferred-fold race, now GONE rather than fixed

This case was executed and passed against the 1.2 s design: save, type at 400 ms, form stays open —
the cancellation worked. **It is no longer meaningful**, because the owner's revision removed the
timer: the card folds the instant the write lands, so there is no window in which to type.

Recorded rather than deleted because the reasoning is what matters for the next person: a deferred
UI action needed a cancel path, and finding that the epoch guard did not provide one cost a design
review. Deleting the timer deleted the whole class of bug — a better outcome than the cancellation
machinery that had to exist while it was there.

What survives from that work is the `wait` step, which is what made the sequencing testable at all
and is used by UC-EYE-02.

Also from it: `set-key` now dispatches a real `beforeinput` before writing, so the page's own listener
runs — the same lesson as UC-MASK-05, that a driver which does not behave like the user cannot verify
the user's path.

---

## The code review, and the two P0s it found

A second Codex round, on the diff rather than the plan: **2 P0 and 5 P1**. Six accepted, one dismissed
with reason. Both P0s were credential leaks, and both were introduced by this change.

**P0 — Wails was logging the revealed key.** `messageprocessor_call.go:131` logs
`"result", string(jsonResult)` beside the arguments, and `RevealKey` returns the secret by design.
The app's Wails logger sits at Info, so the line is dropped today — but `logging.go` was written
precisely to reject that reasoning: *"relying on the log LEVEL is not a safeguard: the level is
exactly what someone changes when debugging."* That file had redacted `args` since it was written;
`result` was safe **by accident**, because no bound method returned a secret. This change ended that
and left the file protecting one direction of a two-way log line. Reproduced:

```
level=DEBUG msg="Binding call complete:" method=SettingsService.RevealKey
  args=[redacted] result="{\"ok\":true,\"key\":\"sk-proj-la-clave-que-el-ojo-revela\",...}"
```

Both keys are now redacted, with a test and a mutation.

**P0 — the debug affordance accepted and logged arbitrary key material.** `set-key:<anything>` echoed
its argument into the step report, which is logged verbatim — so `set-key:sk-live-…` would have
written a real credential to the log through the very affordance whose comment promises it "never
accepts a key from the environment". It now takes fixed tokens only (`badkey|other|empty`) and reports
the token; `wiring.go` additionally strips arguments before logging the raw env string.

The four accepted P1s are covered by UC-EYE-01b and UC-EYE-05 above, plus two that are structural: a
reveal response landing after the user typed is now discarded, and `paint()` recomputes the field's
state after its own transitions instead of using a stale read.

**Dismissed, with reason:** "the ~1.2 s confirmation window no longer exists". True, and deliberate —
the owner asked for the fold to happen as soon as the write lands. Codex was reviewing a diff taken
before the spinner landed and said so itself.

---

## Go tests behind this

`KeyState.Stored` is decided in Go and covered by four cases, **each verified by mutation**: a key the
app stored, an empty slot, an env-var key, and unreadable credentials. The one that earns its place is
the env-var case: such a key IS present and dictation will use it, but the app never stored it, so a
mask there would claim a credential it cannot read or delete. Removing `&& !FromEnv` kills that test
and only that test.

`RevealKey` adds five: the stored key comes back, and four refusals — empty slot, env-var slot,
unknown slot, and a slot no engine reads. **One of them was vacuous and mutation caught it.** The
"slot no engine reads" case originally left the slot empty, so the refusal came from "nothing stored"
and deleting the availability gate left the suite green. It now seeds a credential into
`azure-openai`, so only the gate can refuse it — and asserts on *which* refusal it got.

The payload's schema guard (`TestAKeyStateCarriesPresenceAndNothingElse`) fired on its own when the
field was added, which is the guard working: it pins that every field is a fact *about* the credential
and none is derived from its value.

## What this report does NOT cover

1. ~~**The 1.2 s fold being cancelled.**~~ **Closed by UC-EYE-04**, after `wait` was added to the step
   grammar. What is still unverified is the *reopen* cancellation (the toggle) and the region-change
   one; only the keystroke path was executed.
1b. **The 15 s auto-hide.** Wired, and the same `wait` mechanism could exercise it, but a 15 s pause
   inside a run whose settled report is taken at 6 s would need the reporter's delay reworked. The
   other two re-mask paths ARE executed (UC-EYE-02), so what rests on reading the code is the timer
   itself, not the re-masking it triggers.
2. **What the mask looks like to a human.** `keyField:masked` says the page put a mask there; that it
   renders as twelve dots rests on the input being `type="password"`, which is markup, not behaviour.
3. **A successful save of a NEW typed key, and its fold.** UC-MASK-03 exercises the success path via
   Azure's region because typing a key and saving it would overwrite a real credential on this machine.
   The guard against storing the mask is proven directly (UC-MASK-02); what is not separately proven is
   that a genuinely typed key still stores — that path is unchanged by this branch and covered by
   `2026-08-06-keys-in-a-file.md`.
