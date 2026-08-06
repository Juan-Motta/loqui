# The connection card's actions: "Test connection", and saying that something happened

## Goal

That the four actions on an engine's card in Settings → Connections **do something observable**:
port "Test connection", make a disabled button look disabled, and make a success be stated instead
of leaving the status line blank.

## Architecture

No structural changes. The port's division stays: the rules and the messages live in Go, the page
paints what it receives and decides nothing. What is new is a read-only bound method
(`SettingsService.TestConnection`) and one more field in the result the setters already return.

## Tech Stack

Go 1.x + Wails v3 (generated bindings), TypeScript with no type-checking (debt declared in
`CONTINUITY.md:31-33`), CSS from Electron's original layout.

## The symptom that opened it

The user, validating the link for a new engine: put the Azure key in and pressed **Test
connection**, **Use this engine**, **Delete key** and **Save** — "none of the actions work".

## Root cause — four different causes, one symptom

The app's logs during that session (`UI-ACTION`) prove that **two of the four buttons did call the
backend, and both calls succeeded**:

```
11:18:39  UI-ACTION  map[action:setProvider(azure) error: ok:true]
11:41:02  UI-ACTION  map[action:saveConnection(azure) error: ok:true]
11:41:02  CONN       map[... azure=active ...]
```

Of the other two there is no trace, for different reasons: "Test connection" has no handler to emit
anything, and "Delete key" was disabled, so its click went nowhere.

| # | Action | What actually happens | Evidence |
|---|--------|--------------------|-----------|
| 1 | Test connection | **Never ported.** `#test` has no handler and there is no bound method. The logic exists and is tested but nobody calls it in production. | `index.html:979`; `settings.ts:665-717` does not hook `#test`; `azure.TestConnection` in `internal/stt/azure/token.go:148` with no callers; already declared inert in `CONTINUITY.md:86-91` |
| 2 | Delete key | Correctly disabled (there was no key), but **indistinguishable from an active one**: there is no `:disabled` rule and `button.btn` sets colour and background and keeps the `:hover`. | `settings.ts:534-541`; `index.html:317-325` |
| 3 | Save | It worked. `run()` paints `res.error`, which on success is the empty string → **success is indistinguishable from a lost click**. | `settings.ts:626`; log `saveConnection(azure) ok:true` |
| 4 | Use this engine | It worked, but without a key the state is still `unconfigured`: the badge still says "Sin configurar" and the button is still there. No visible change **on the card**. | `settings_write.go:54-73`; `connection.go:ConnectionStateFor`; log `setProvider(azure) ok:true` |

All four share a perception cause: **the status line only speaks when something fails**, so a
success and a click that never arrived look the same. Fixes 3 and 4 attack that cause; 1 and 2 are
controls that were never finished.

## The fixes

### 1. `SettingsService.TestConnection(slot, region, secret) ProbeResult` — new, in `internal/app/settings_probe.go`

```go
type ProbeResult struct {
    OK      bool            `json:"ok"`
    Error   string          `json:"error"`
    Message string          `json:"message"`
    Payload SettingsPayload `json:"payload"`
}
```

**It does not return a Go error**, for the same reason as `WriteResult`: Wails discards the result
of a method that also returns an error.

**It does return a payload, even though it mutates nothing.** The first version of this plan did not
carry it — "it does not mutate, there is nothing to repaint" — and that was false in a case this
code deliberately preserves: a Keychain write that **exhausts its deadline and lands afterwards**
(the cgo call cannot be cancelled; see `SetKey` and the test pinning that behaviour in
`settings_write_test.go:582`). The real sequence on this ad-hoc-signed build: Save exhausts its 10 s
→ the payload says `unreadable` → the write lands late → the user tests with the field empty → the
probe **does** find the key and says everything is fine, while the badge, the label and the Delete
button keep saying the opposite. The `Settings.Load` recovery does not cover it: it only runs on a
transport exception (`settings.ts:632-644`), and a Keychain timeout arrives as a normal
`WriteResult`.

The **whole** payload is returned and handed to `paint()`, instead of a partial snapshot of
`KeyState` + `ConnectionRow`: a second painting path with its own consistency rules is more surface
than this bug justifies. **One arbiter for all payloads**: `paint()` discards any that carries a
`Revision` lower than the last painted, whether it comes from the probe, from a write or from
System. The card epoch does not take part here — it decides only who writes the message.

It costs one more Keychain read (the `keyStates` fan-out, up to ~3 s on this build). It is paid
because its whole reason for existing is precisely to tell the truth about the credential's state.

**And that forces a precise statement of what a rejection does NOT do.** A rejected probe does not
go to the network and does not resolve the key it was going to test — but it **does** read the
Keychain, because the payload it returns reads it, just as any `Load()` or any setter does
(`bootstrap.go:477` → `store.KeyStatusFor`). Saying "zero Keychain reads" was false. The tests
assert what actually holds: zero HTTP and zero calls to the probe's resolution seam.

**Order of operations, deliberate and tested** — everything that can fail without the network is
resolved before going to the network:

1. Known slot → if not, error.
2. A slot with a test available, against an **explicit list of slots that have a probe** — today
   only `azure-speech`. **`store.IsAvailableKeySlot` will not do**: it is also true for `grok`
   (`keychain_darwin.go:119-125`), so using it would let a Grok key through to Azure's probe, which
   would send it to Azure's STS endpoint. Any slot without a probe — `grok` included, not only the
   unported `azure-openai` — returns an honest "test not available" error, without going to the
   network and without resolving any credential.
3. **Region**: the form's if it is there, the stored one if not; `settings.NormalizeRegion`
   validates it. It fails → it is returned, **zero HTTP**. This matters because `LoadSettings`
   accepts any string that is valid JSON (`config.go:178`), so the stored region may not be valid.
4. **Key**: the typed one if it is there; if not, `LOQUI_AZURE_KEY` when it is not empty; if not,
   the Keychain. Testing before saving is the use case, so the form's key wins; **from there on**
   the precedence is `keyReaderFor`'s (`dictation.go:624-632`), which only chooses between the
   environment and the Keychain. With the field empty, test and dictation read the same thing — if
   they disagreed, the test would bless a credential dictation does not use.
5. Only then: `context.WithTimeout` and `azure.TestConnection`.

**A Keychain failure is not confused with "there is no key"** (Codex P1, iteration 1). `GetKey`
returns three different outcomes and the distinction is load-bearing: `ErrNoSecret` means type it
in, `ErrKeychainTimeout` means the app's signature is wrong and retyping it will fix nothing
(`keychain_darwin.go:256-260`, and that is already how dictation treats it in
`dictation.go:248-260`). They are classified with `errors.Is`. None of the three goes to the
network.

**The timeout message is its own, not `keychainMessage`'s.** That text
(`settings_write.go:418-427`) describes an indeterminate **write** that may still land and tells the
user to reopen Settings to see the real state; on a read there is no late mutation to wait for and
that instruction would be noise. The correct wording here is the one dictation already uses
(`dictation.go:252`): the Keychain did not answer, sign the app with a stable identity or pass the
key in `LOQUI_AZURE_KEY` to test.

**Time budgets, stated precisely.** The probe builds its own
`&http.Client{Timeout: 15 * time.Second}` when `probeClient` is nil, instead of passing nil and
letting `NewTokenService` install its own 10 s one (`token.go:73-75`): that way the number in the
code and the number on this line are the same. The 15 s `context.WithTimeout` bounds the HTTP
exchange; the Keychain read carries its own 3 s limit inside `store.GetKey` and happens **before**.
Adding the payload at the end, which reads the Keychain again, the worst case for the Go call is
**~21 s**: 3 to resolve + 15 of network + 3 of payload. **From the click there is no maximum**: the
page also waits for the write queue to drain (fix 1b), and that is not bounded.

**Test seams** (the network and the Keychain are never touched in a unit test), both on
`SettingsService` alongside the existing ones:

- `probeClient azure.Doer` — nil = the real `http.Client`.
- `getSecret func(store.KeySlot) (string, error)` — nil = `store.GetKey`. Today the service only
  has a write and a delete seam (`bootstrap.go:339-342`); without this one, the Keychain's three
  outcomes are not testable.

**The button stays enabled at all times**, except while its own test is in flight. That is what the
user asked for and it is correct: the test is used precisely when the engine is NOT properly
configured, so gating it on the state would make it useless exactly when it is needed.

### 1b. Coordinating the feedback: one epoch per card (Codex P1, iteration 1)

The probe and the three writes all write to **the same** card `.status` (`settings.ts:670`). Since
the probe does not go into the write queue (`writes`, `settings.ts:583-593`), a slow test can finish
**after** a later Save and overwrite its message with a stale result — the same out-of-order arrival
the queue prevents for writes.

Two measures, each for a different problem:

- **An epoch per card, and ONLY over the `.status`.** A counter per `.conn` element; each action
  (write or test) keeps the value when it starts and **only writes the message if nobody has started
  another action on that card since**. The stale message is discarded silently: it no longer
  describes anything.

  **A WRITE's payload is not governed by the epoch.** This corrects a flaw in the first version of
  this plan: if the epoch decided over the whole result, the Save → Test sequence would discard the
  Save's repaint — and that payload is the only thing that updates the badge, the key's state and
  the Delete button (`settings.ts:519-541`). The key would end up saved with the card showing the
  opposite. Writes repaint **always** and **in order**, which is what the queue already guarantees
  (`settings.ts:583-593`). The epoch arbitrates two things: who speaks on the status line, and
  whether the **probe's** payload is applied or thrown away (fix 1) — that one, yes, because if it
  arrives late the card has already been repainted by something newer.

  **The payload is arbitrated by a REVISION the backend stamps, not by a paint counter.** An epoch
  per card does not serve for it and this is a flaw from the previous iteration of this plan:
  `paint()` does not repaint a card, it repaints **the whole page** — region, engine picker, all six
  cards, System and onboarding (`settings.ts:334-549`). With per-card arbitration, an Azure test
  could capture state A, an action on Grok or System paint B, and the probe apply A on top because
  **Azure's** epoch was still intact.

  A counter that `paint()` incremented does not serve either, and this is the second correction: it
  would measure "somebody painted", not the snapshot's age. A language or System save can **start
  earlier** than the test, outside `writes`, and paint **after** the test captures the counter
  (`language.ts:35`, `system.ts:36`, `onboarding.ts:36`, `settings.ts:791-793`): the stale payload
  would pass and the test's fresh one would be discarded for having arrived behind it.

  So recency is stamped by whoever produces the snapshot: `SettingsPayload` gains a `Revision`
  field, and `paint()` ignores any payload whose revision is lower than the last painted. The
  counter is an **`atomic.Uint64` on `Bootstrap`**, and its `Add(1)` is the **first** operation in
  `Payload()`. Atomic is not decoration: Wails dispatches every bound call in its own goroutine, so
  two payloads can be built at once — which is exactly the situation the stamp exists to order. A
  plain `revision++` would give a race, lost increments and repeated revisions, and two snapshots
  with the same revision are two snapshots that cannot be ordered. The message is still arbitrated
  by the card epoch, which is where it is shown.

  **What that guarantees, exactly:** that a snapshot which STARTED earlier does not overwrite one
  that started later. It is not a total guarantee — `Payload()` reads the disk, the Keychain and the
  devices at several instants, so in theory a snapshot that started earlier may have read a
  particular field later. It is strictly better than today (where there is no arbitration at all)
  and it is documented as what it is, not as a total order.

  **A deliberate side effect:** the revision arbitrates EVERYONE who paints, not just the probe — so
  it also closes the pre-existing race between System, languages, onboarding and Connections, which
  until now could repaint a card with an older snapshot. It was not sought in this change; it comes
  free from the correct mechanism, and leaving the other producers out would take more code in order
  to keep a race.

- **`await writes` before the test.** The test does not enter the queue (it must not block a later
  Save), but it waits for what was already pending to drain. Without that, testing with an empty
  field right after a Save would read the **previous** key from the Keychain and say the new one
  fails. The form's region and secret are captured **at the click**, before that wait, just as Save
  already does (`settings.ts:690-698`): if they were read afterwards, the test would use whatever
  was in the DOM when the wait ended, not what the user pressed.

### 2. A visible `:disabled` state — `frontend/index.html`

```css
button.btn:disabled { opacity: 0.45; cursor: not-allowed; }
button.btn:disabled:hover { background: var(--card-bg); filter: none; }
button.btn.primary:disabled:hover { background: var(--accent); }
```

`button.btn:disabled:hover` has specificity 0-3-1 against `button.btn:hover`'s 0-2-1, so the hover
stops responding on a dead button. It reaches every `.btn` in the app, which is what we want: by
state, `.conn-delete` without a key and `.conn-save` on an unsupported card are disabled today
(`settings.ts:531-541`), and on top of that `run()` disables the pressed button while it is in
flight, the same as the Permissions ones do — that flicker was not visible either. It does not reach
the language chips or `.btn-record`, which are not `button.btn`.

### 3. `WriteResult.Notice` — success gets stated too

A new field, `json:"notice"`, empty by default. **`WriteResult`'s comment
(`settings_write.go:29-40`) is updated to describe the three fields as current behaviour** — that
text is copied verbatim into `models.ts` in the bindings, so leaving it describing two fields turns
it into false documentation in two places.

| Setter | Notice |
|--------|--------|
| `SaveConnection` | "Clave guardada" / "Región guardada" / "Clave y región guardadas", according to what was actually written |
| `DeleteKey` | "La clave ya no está guardada" — **a postcondition, not an action**: `DeleteKey` is idempotent ("Absent is success", `keychain_darwin.go:312`) and the service does not know the previous state, so "Clave borrada" would lie when there was none |
| `SetProvider` | see fix 4 |

**Why the text comes from Go and not from the page**, even though `system.ts:36-54` passes an
`okText` down from TypeScript for the System panel: the three messages here depend on facts **only
Go has** — what was actually written, what the engine's resulting state is, and what Azure replied.
A fixed `okText` on the page would be a string that may not be true.

The **painting convention** is taken from what already exists, without over-generalising it: the
`ok`/`err` classes on the status element and the `✓`/`✗` in front of the text come from two places
that use them similarly but not identically — `permissions.ts:139-144` paints `status ok|err` with a
`✓` on success and no `✗` on error, and `system.ts:44-45` paints `lang-status ok|err` with a `✗` on
error. Here the element is `.status`, whose CSS already defines `.ok` and `.err`
(`index.html:328`), and both marks are used: `✓ <notice>` and `✗ <error>`.

**Declared scope, with the counting done:** the service has **12** setters
(`settings_write.go`). The **3** on the connections card get a notice — `SaveConnection`,
`DeleteKey`, `SetProvider`. The remaining **9** do not, and they are not all "System" ones: **5**
are reached from the System panel (`SetAppearance`, `SetAppLanguage`, `SetInputDevice`, `SetMode`,
`SetTrigger`), **1** from languages (`SetLanguages`), **3** from the tutorial or from nowhere
(`SetKey`, `SetOnboarded`, and `SetRegion`, which today has not one caller in the frontend). The
System and language ones already give feedback through their own `okText` and persist on change
without a button, which is a different pattern; the reported bug is the connections card.

### 4. `SetProvider` says whether the chosen engine is usable

The notice is derived from the state of the just-saved engine, read from the rows
`store.ConnectionRows` already computes in the payload the setter itself returns — the rule is not
reimplemented:

- `active` → "Motor activo: Azure"
- `connected` / `available` → does not happen for the just-chosen engine (both mean "not
  selected"); if it did appear, it falls back to the generic "Motor guardado".
- `unsupported` → "Motor guardado, pero no puede funcionar en este sistema"
- `unconfigured` → **two texts, not one**, because this state collapses two different situations:
  `presenceMap` reduces `unreadable` to "there is no key" (`bootstrap.go:303-313`) and
  `ConnectionStateFor` cannot tell them apart afterwards (`connection.go:199-201`). Saying "it is
  missing configuration, complete it" to someone whose key IS saved but whose Keychain did not
  answer is false and sends them off to retype it for nothing — exactly the mistake the three-state
  distinction exists to avoid. So the notice also consults `Payload.Keys` for the engine's slot:
  - a key in the `unreadable` state → "Motor seleccionado, pero el Keychain no respondió — no se
    puede confirmar si su clave está disponible"
  - any other case → "Motor seleccionado, pero le falta configuración — no podrá dictar hasta que la
    completes"

**The `unsupported` case IS reachable** (Codex P1, iteration 1; my first version claimed the
opposite and it was false): `SetProvider` only consults `IsAvailableProvider`, which is a global
"ported" map (`config.go:389-396`), while `unsupported` also depends on the machine — platform,
macOS version and the helper's presence (`connection.go:IsAvailableOn`). On a Mac with macOS 15,
`macos` is a ported and unsupported engine at the same time, and the binding accepts it.

**What this change does NOT do: reject it.** Whether `SetProvider` should refuse to save an engine
this machine cannot run is a behaviour decision distinct from the reported bug — it changes what is
accepted, not what is reported, and it affects the picker states that were verified in the previous
session. Here it is reported honestly and left noted.

**The `caps` seam, being careful not to break what already exists.** `hostCapabilities()`
(`bootstrap.go:320-329`) reads the real machine with no possible injection, so
`caps func() store.HostCapabilities` is added to `Bootstrap` alongside `keyStatus` / `perms` /
`devices`. But `Bootstrap` **is built as a literal in two test helpers** (`bootstrap_test.go:28-39`,
`settings_write_test.go:63-85`), not only by `NewBootstrap`, so calling `b.caps()` bare would panic
across the whole suite. Two measures together:

- **An accessor with a nil-safe fallback**: `caps == nil` → `hostCapabilities()`. No caller can blow
  up for not knowing about a new field.
- **A deterministic value in both test helpers: `store.HostCapabilities{}`.** Today those tests read
  the developer's machine — platform, macOS version and the presence of helpers — so their result
  depends on where they run. Fixing it isolates them. If some existing expectation changes when it
  is fixed, that **is** the finding: the test depended on the machine. It gets handled in its own
  place, not by reverting the seam.

### 5. Form validation and button enabling (asked for by the user, 2026-08-01)

The assignment grew during validation: as well as the actions saying something, the card must
**prevent** the ones that make no sense and **flag the field** that is missing.

| Rule asked for | How it is implemented |
|---|---|
| Save with no key (neither typed nor stored) → a red border on the input and "la clave es obligatoria" | `SaveConnection` already rejects "there is nothing to save"; the case "there is a region but no key at all, neither typed nor stored" is added, plus a new field in `WriteResult` saying **which input** to flag |
| Test with no key → the same validation, without going to the network | The probe already returns "the key is missing" with no HTTP; now it also says which field to flag |
| After testing, say whether the connection worked | `ProbeResult.Message` / `.Error` (fix 1) |
| "Use this engine" only with the connection saved | A complete matrix by state, not `state !== "connected"`: `active` and `unsupported` stay **hidden** (as today); `unconfigured` becomes **visible and disabled**; `connected` and **`available`** visible and enabled. `available` is load-bearing: it is the state of Whisper and macOS, which carry no credential — treating them as "unconfigured" would make both local engines unselectable (`connection.go:190-215`) |
| "Delete key" only with the config saved | **Already the case** (`settings.ts:534-541`); what was missing was for it to be noticeable — fix 2 |

**`WriteResult.Field` / `ProbeResult.Field`**, with values `"key"`, `"region"` or empty. The page
does not decide what is wrong: it receives the input's name and applies the error class. It is the
same division of labour as the rest of the port — if the page deduced "the error talks about the
key", it would be reimplementing in TypeScript a validation that lives in Go.

**The red border has to be drawn: the class does not exist.** Today the CSS only defines the normal
and focus borders for the inputs, and `.status.err` for the text (`index.html:275-283`, `:328`) —
adding a class to an input would not make it red. So:

```css
.conn-form input.invalid, .conn-form select.invalid { border-color: var(--err); }
.conn-form input.invalid:focus, .conn-form select.invalid:focus {
  border-color: var(--err); box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 22%, transparent);
}
```

**When it is cleared**, which is the half that gets forgotten: on typing in that input (`input`) and
on starting any new action on the card. A red border that outlives the correction is worse than not
having one — the user fixes what they were asked to and the interface goes on accusing them.

**What exactly `SaveConnection`'s validation requires.** Today a region-only save is valid on
purpose and there are two regressions pinning it (`settings_write_test.go:379`, `:554`), so the new
rule is written not to break them — resolving the key with the SAME precedence as everything else:

| Situation | Result |
|---|---|
| A secret typed in the form | valid, it is saved |
| Empty field + a non-empty `LOQUI_AZURE_KEY` | valid; **the Keychain is not consulted** and only the region is saved |
| Empty field + the Keychain returns the key | valid, only the region is saved |
| Empty field + `ErrNoSecret` (there is no key anywhere) | **rejection** with `Field="key"` and "la clave es obligatoria" |
| Empty field + `ErrKeychainTimeout` or another error | its own failed-read message, `Field` empty — **never** "the key is missing": accusing someone who has the key stored of not having one is the same mistake already corrected in the probe |

**What does NOT become a new state: "tested".** The user also asked that if there is a key but the
connection has not been tested, there should be a warning. With the answer that "established" =
**configured and saved** (no test is needed to use the engine), that warning does not need
persisting: it is shown **after saving** — "Clave guardada. Pulsa Probar conexión para comprobar que
sirve" — and it disappears when the test passes in that session. Persisting a "tested" flag would
force invalidating it on every key or region change, and without persisting it properly the app
would ask to re-test an engine that has been working for months on every launch. Electron's original
does not have it either: `connectionStatus.ts` computes availability the same way the port does.

**Saving only the region is still valid** when there is already a key stored: that is how you change
region without pasting the credential again. The new validation only fires when **there is no key
anywhere**.

## Task list

1. `internal/app/settings_probe.go` + `settings_probe_test.go` — the probe, its two seams and the
   classification of Keychain errors (red first).
2. `internal/app/bootstrap.go` — the `caps` seam on `Bootstrap`.
3. `internal/app/settings_write.go` + `settings_write_test.go` — `Notice` on the three setters and
   `WriteResult`'s comment updated.
4. `./scripts/task.sh common:generate:bindings` — the new method, struct and field.
5. `frontend/src/settings.ts` — a handler for `#test`, the per-card epoch, `await writes` before the
   test, `✓`/`✗` painting with `ok`/`err` classes.
6. `frontend/index.html` — the `:disabled` rule.
7. `wiring.go` + `settings.ts` — the `LOQUI_DEBUG_CONN_CLICK` probe. A button inside a Wails webview
   cannot be clicked from a script (`CONTINUITY.md:71-77`). Grammar:
   `<provider>:<action>[:<argument>]`, and with `+` actions are chained **without waiting** for the
   previous one to finish, which is what makes UC-3 and UC-4 verifiable:
   - `azure:test` — presses "Test connection" with the form as it stands.
   - `azure:test:badkey` — types a fixed, invalid sentinel (`loqui-debug-clave-invalida`) into the
     field before pressing. **It never accepts an arbitrary key from the environment**: that would
     put a real secret into the environment and into the logs.
   - `azure:save-region:<id>` — picks that region in the dropdown and presses Save.
   - `azure:clear-region` — leaves the dropdown on the empty placeholder, pressing nothing. That is
     what makes UC-3 verifiable.
   - `azure:test+save`, `azure:save-region:<id>+clear-region+test` — chained without waiting.

   **The probe fires on `ui:painted`, not on a `time.Sleep`.** The existing
   `LOQUI_DEBUG_SET_PROVIDER` hook waits a fixed 3 s (`wiring.go:272-278`), and that is fragile
   here: `wire()` only runs after `Settings.Load` resolves (`settings.ts:753`), and its Keychain read
   can consume those same 3 s on this build. A command that arrives before the wiring is lost in
   silence and the UC looks like it failed for another reason.
8. `internal/app/settings_probe.go` — two log lines, with no secret: `PROBE region=<id>
   source=<typed|env|keychain>` when it resolves its inputs (UC-3 asserts on it which configuration
   the test used) and `PROBE-DONE ok=<bool>` when it finishes (UC-4 needs it to establish the real
   order of completion). The region is logged already **normalised**, which is the validated form.
9. `internal/app/provider_test.go` — `TestUnportedProviderIsReported` uses `elevenlabs` as the
   unported engine (`provider_test.go:77-83`), but ElevenLabs **is** ported since the previous
   session (`dictation.go:325`, `config.go:394`): the test passes through the `ErrNoSecret` that
   `testDictation` gives it, not by reaching the "unported" `default`. It is exactly the kind of
   vacuous test that the previous session found four times (`CONTINUITY.md:79-84`). It gets fixed
   here, with the correct unknown engine and asserting on the message, because this review found it —
   it is not parked.

## Tests

Go, with the network and the Keychain behind seams (`internal/app`):

| # | Test | Asserts |
|---|------|--------|
| 1 | an unknown slot | error, `OK=false`, zero HTTP |
| 2 | the `azure-openai` slot (known, unported) | "test not available", zero HTTP, zero `getSecret` |
| 2b | the `grok` slot (known, available, **with no probe**) | "test not available", zero HTTP, zero `getSecret` — it cannot fall through to Azure's probe |
| 3 | no typed key, empty Keychain (`ErrNoSecret`) | "the key is missing", zero HTTP |
| 4 | no typed key, Keychain timing out (`ErrKeychainTimeout`) | it says the Keychain did not answer, **not** "the key is missing", zero HTTP |
| 5 | no typed key, Keychain with another error | a failed-read message, zero HTTP |
| 6 | no region typed and none stored | it asks for a region, **zero HTTP and zero `getSecret` reads** |
| 7 | an invalid stored region and an empty argument | `NormalizeRegion` rejects it, **zero HTTP and zero `getSecret` reads**: a region that is unusable does not justify touching the Keychain (nor paying its 3 s) |
| 8 | typed key + region + 200 | `OK=true`, `Message` non-empty |
| 9 | 401 | a credential error, `OK=false` |
| 10 | empty field + a stored key | it uses the Keychain's (the seam), reaches the network |
| 11 | `LOQUI_AZURE_KEY` set | it wins over the Keychain, just like `keyReaderFor` |
| 12 | `LOQUI_AZURE_KEY=""` | it does **not** count as an override: it falls through to the Keychain |
| 13 | `LOQUI_AZURE_KEY="   "` | it counts as an override (which is what `keyReaderFor` does today) and the message **names the variable**, so nobody hunts the Keychain for a key that comes from the environment |
| 14 | a Doer that blocks until `ctx.Done()` | the deadline is respected and the result says so |
| 14b | a `getSecret` that takes a controlled amount of time, and a Doer that measures how much context it has left | the HTTP budget **is born after** the preflight: the deadline the Doer sees is not bitten into by what reading the key cost |
| 15 | the secret does not appear in `ProbeResult` | no field of the result contains it |
| 16 | `SaveConnection` with a key / with a region / with both | three different notices, `Error` empty |
| 17 | `DeleteKey` on a slot with a key and on an empty slot | the same postcondition notice in both, and it is true in both |
| 18 | `SetProvider` to a configured / unconfigured / unsupported-on-this-machine engine (the `caps` seam) | three different notices and the last two warn |
| 18b | `SetProvider` to an engine whose key is `unreadable` (the `keyStatus` seam) | the notice says the Keychain did not answer, **not** "configuration missing" — it is iteration 2's P1 pinned with a test |
| 20 | `SaveConnection` with no key anywhere | rejection with `Field="key"`, nothing written |
| 21 | region-only `SaveConnection` with a stored key, and with `LOQUI_AZURE_KEY` | both stay valid (the regressions at `settings_write_test.go:379` and `:554` are not broken) |
| 22 | `SaveConnection` with no key and an unreadable Keychain | a failed-read message, `Field` empty, it does not say "the key is missing" |
| 23 | `Payload().Revision` | it grows on every call **and orders by start, not by finish**: a snapshot that starts earlier and takes longer comes out with a lower revision. That `paint()` discards it is frontend and this test does not cover it — see the declared gap |
| 19 | probe with a payload: a write that exhausts its deadline and lands late, then a probe with an empty field | the `ProbeResult.Payload` **contains** the already-present key — and only that: it is a Go test and it cannot assert anything about what the page does with it |

**Every new test is verified by deliberately breaking what it says it covers**
(`CONTINUITY.md:79-84`: in the previous session four green tests proved nothing, and mutating
production found them).

The frontend has no runner and no type-checking (declared debt, `CONTINUITY.md:31-33`), so fix 1b
(the epoch + `await writes`) and the CSS are verified by E2E against the packaged app, not by unit
test. So that this verification does not depend on what Azure replies, **the probe logs which region
and which key source it resolved to the Go log** — `PROBE region=<id>
source=<typed|env|keychain>`, never the secret, like the rest of the probes
(`CONTINUITY.md:71-77`). Without that line, "it used the new key" is not observable from outside.

## E2E Use Cases

#### Surface coverage decision

Interfaces the project exposes: **UI** (Settings' only surface).

- **UI** — Covered (UC-1, UC-2, UC-3, UC-4).
- **API** — N/A: the app exposes no HTTP; Wails' bindings are not a user surface, only the transport
  for this same UI.
- **CLI** — N/A: `cmd/stt-probe` dictates from the terminal to isolate network failures and does not
  configure engines; linking an engine is not a capability the CLI offers today nor should offer (the
  key is stored in the user's Keychain from the signed app).

#### UC-1 — testing the key before trusting it

- **Actor:** someone who has just taken an Azure Speech key out of the portal and does not yet know
  whether they copied all of it.
- **Scenario:** they have the Azure card open in Settings with the region chosen and the key pasted.
  They want to know whether it works **before** saving it and finding out halfway through a
  dictation.
- **Interface:** UI
- **Intent:** to check that the key and region they have just typed work, and to be told so in
  words.
- **Setup:** open the packaged app with the configuration already on the machine (Azure key stored
  and its region). The key is not touched from outside the app.
- **Steps:** Settings → Connections → Configure on Azure → press "Test connection" with the key
  field **empty** (it uses the stored one) → press it again with the invalid sentinel in the field
  (`LOQUI_DEBUG_CONN_CLICK=azure:test:badkey`).
- **Verification:** the first time, the card's status line says the connection is correct, in green;
  the second says the key or the region are invalid, in red. In neither case does the button
  disappear or go dead: it can be pressed again.
- **Persistence:** after the failed test, close and reopen Settings → the stored key is still the
  good one and the engine is still active: **testing writes nothing**.

#### UC-2 — the save and the selection get stated

- **Actor:** the same person, now with the key validated.
- **Scenario:** they save the key and choose Azure as the engine. Before, this produced no visible
  change on the card and it looked like the button was broken.
- **Interface:** UI
- **Intent:** to save the key and activate the engine, and see each step confirmed.
- **Setup:** the packaged app, the Azure card open, an active engine other than Azure.
- **Steps:** Save with the key field empty and the region set (it saves the region) → press "Use
  this engine".
- **Verification:** after Save, the line says in green exactly what was saved; after "Use this
  engine", it says Azure is now active and the badge changes to "Activo". "Delete key", which now
  looks dimmed when it is, becomes lit.
- **Persistence:** close and reopen the Settings window → Azure is still active with its key, and
  "Delete key" is still lit.

#### UC-3 — a test launched right after saving does not read the old configuration

- **Actor:** the same person, correcting the region because they copied the wrong one.
- **Scenario:** they change the region and, without waiting, press "Test connection" with the key
  field empty. They expect the test to check **what they have just saved**, not the previous thing.
- **Interface:** UI
- **Intent:** that testing right after saving measures the new configuration.
- **Setup:** the packaged app with the Azure key already stored and its current region. The current
  region is noted down so it can be restored at the end.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK=azure:save-region:westeurope+clear-region+test` — pick another
  valid region and press Save; **without waiting**, leave the dropdown on the empty placeholder and
  press "Test connection" with the key field also empty.
- **Verification:** the Go log shows `PROBE region=westeurope source=keychain` — and that can only
  come from the store, because the form's region was empty at the moment of the click. The card's
  status line ends up showing the test's result, and the badge and the Delete button end up as the
  saved state dictates (the Save's payload did get painted).

> **Why the `clear-region` is not decoration** (a Codex finding, iteration 3): without it, the
> handler captures `westeurope` **from the DOM** on the click, so the log would say
> `region=westeurope` even if the `await writes` were deleted entirely. The UC would prove nothing.
> By emptying the dropdown before pressing, the only way for the probe to know that region is to
> have waited for the write to drain.
- **Persistence:** restore the original region the same way (pick it in the dropdown and Save) →
  close and reopen Settings → the original region is still set and the engine is still active.

> The discrimination **does not depend on what Azure replies**: the `PROBE region=…` line asserts
> it. If the 401 also shows up because of the wrong region, that is extra confirmation, not the
> evidence.

#### UC-4 — a stale result does not overwrite a newer action's message

- **Actor:** the same person, impatient: they press Test and, while it spins, press Save.
- **Scenario:** the test takes a network round trip; the Save finishes first. What ends up written
  has to describe the last thing they did, not the first.
- **Interface:** UI
- **Intent:** that the status line describes the last action, and that the card does not go stale.
- **Setup:** the packaged app, the Azure card open, a stored key.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK=azure:test+save` — press "Test connection" and immediately
  afterwards "Save".
- **Verification:** the log's timestamps establish the real order of completion —
  `PROBE-DONE ok=<bool>` when the test resolves, and the Save's `UI-ACTION` when the write resolves.
  With `PROBE-DONE` **after** the Save's `UI-ACTION`, the status line has to show the Save's notice:
  the late result was discarded. The badge, the key's state and the Delete button correspond to that
  Save's payload.
- **Persistence:** close and reopen Settings → what was saved is still there, and the card reflects
  it the same as before reloading.

> **Without `PROBE-DONE` this UC proves nothing** (a Codex finding, iteration 3): `PROBE
> region/source` is emitted when it **resolves its inputs**, not when it finishes, and today's
> `UI-ACTION` is only emitted by the write wrapper (`settings.ts:627-631`). Without a completion mark
> for the probe, "it ended up showing the Save's notice" does not distinguish "the probe finished
> first" from "it finished later and the epoch discarded it" — which is exactly what we want to
> demonstrate.
>
> **Honesty about determinism:** who wins the race is decided by a real network round trip. The UC
> does not force the order; it **observes** it in the log and only then asserts. If in one run the
> probe finishes first, that run says nothing about the epoch and is repeated.

## Declared gaps, not papered over

**Revision arbitration INSIDE `paint()` is not covered.** The Go tests pin that revisions grow and
that they order by start; that the page discards the lower one is frontend logic, with no runner
(`CONTINUITY.md:31-33`) and no deterministic way to provoke two crossed payloads from outside.

**That the PAGE applies the probe's payload is not covered by any test.** Test 19 shows that the
payload carries the fresh state; UC-3 and UC-4 end with a Save payload that already leaves the card
correct, so **deleting `paint(res.payload)` from the probe's handler would not break them**.
Covering it properly requires starting from a stale card, and its only real cause is a Keychain write
that exhausts its deadline and lands late — which cannot be provoked at will without putting a hook
into production that fakes the timeout of a credential write. It is not worth it. It is noted here
and in `.workflow/state.md`, not disguised (`CONTINUITY.md` carries its own declared-gaps section for
the same reason).

## What is NOT included

- **Rejecting**, in `SetProvider`, an engine this machine does not support (see fix 4). It is
  reported; what is accepted does not change.
- Upgrading `typescript` so the frontend type-checks (declared, unowned debt,
  `CONTINUITY.md:31-33`). This change adds ~60 lines of TS with no type safety net, just like the
  ~1500 already there. It goes in its own change; I say so, I do not hide it.
- The Azure OpenAI subservice fields (`azureOpenAiResource`, `azureOpenAiDeployment`), which stay
  inert because the subservice is not ported.
- Notices on the other nine setters (see the declared scope, with the counting, in fix 3).
- Keeping "Use this engine" visible on the active engine: today it is hidden on purpose, the same as
  the original. The user asked about that button believing it was "Test connection"; once clarified,
  they did not ask to change it.
