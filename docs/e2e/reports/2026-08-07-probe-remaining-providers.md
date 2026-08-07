# E2E — "Probar conexión" for OpenAI, Grok and ElevenLabs

VERDICT: PASS

**The green path of all three ran, and this is the first time it ever has.** Every previous report
on a cloud provider other than Azure was PARTIAL for the same reason: no credential existed on this
machine. The plan itself declared this feature's E2E "PARTIAL by construction" (risk 4). That is no
longer true — real credentials for the three slots were found stored on 2026-08-06 at 17:16, after
the plan was written, and the owner authorised using them for this run on 2026-08-07.

What that buys is not just a checked box. Risk 1 of the plan said the readiness event names
(`session.created`, `transcript.created`, `session_started`) came from documentation and from our own
ported runtime, and that **nobody had ever observed one**. All three are now observed against the
real service.

- **Feature:** a working "Probar conexión" on the three ported cloud cards that carry a credential
- **Branch:** `feat/probe-remaining-providers`
- **Run:** 2026-08-06T18:07-18:11-05:00 (rejection paths) and 2026-08-07T09:15-09:20-05:00
  (green paths, settled states) — twelve app launches
- **Build:** `bin/loqui.app`, repackaged from this exact tree with `./scripts/task.sh package`,
  ad-hoc signed. Suite green first: 14 packages with `-race -count=1`.
- **Stored state at the start:** `keys:azure-speech=present azure-openai=absent openai=present
  grok=present elevenlabs=present`, `provider=azure`, `region=eastus2`

## How it is driven, and why not Playwright

The `verify-e2e` skill's reference harness targets Playwright against a served origin. **It does not
apply here and is not a shortcut being skipped:** this is a Wails WKWebView, not a page on a URL, so
there is no origin for `chromium.connect` to reach and `@playwright/test` is not a dependency of this
repo. Classifying that as `FAIL_INFRA` would be wrong — the tooling is not broken, it is inapplicable.

The substitute is the one this project established across four prior reports:
`LOQUI_DEBUG_CONN_CLICK=<provider>:<action>[:<arg>]` dispatches **real clicks** on the same handlers a
mouse reaches (`frontend/src/settings.ts:1171`), and `LOQUI_DEBUG_CONN_REPORT=<provider>` reports what
the card **settled on** six seconds later rather than the intermediate "…". Assertions are made on
what the card shows the user — its status sentence and button states — never on a CSS selector shape
and never through a database or log back channel. The one place a log line is used as evidence
(`PROBE source=…`) is not a back channel for the outcome: it is the separate fact of *which*
credential the probe resolved, which the page deliberately cannot see.

Two traps, both hit and both worth recording:

1. **The sandbox silently killed the GUI.** Launched under the tool sandbox the app printed its
   platform line and then produced nothing at all — no `UI-PAINT`, no probe — which reads exactly
   like a broken feature. It needs an unsandboxed launch.
2. **zsh ate the step grammar.** `"$p:test:badkey"` expands `:t` as a *tail* modifier, so the app was
   asked for a card called `grokest`. It reported `no such card: grokest` rather than pretending —
   but a less honest affordance would have made this look like a passing run. `"${p}:test:badkey"`.

---

## UC-PROBE-01 — my OpenAI key is good and I want to know before I dictate: PASS

- **Actor:** someone who has pasted an OpenAI key and wants to confirm it works.
- **Interface:** UI
- **Setup:** the key already stored (Setup does not perform the action under test — it does not
  press the button). The key field left **empty**, so the probe resolves the credential exactly the
  way dictation would.
- **Steps:** open the OpenAI card → press "Probar conexión".

```
CONN-CLICK  ran:test(asis)  status:Probando la conexión…  test:shown/disabled
PROBE       slot=openai region= source=stored
PROBE-DONE  slot=openai ok=true code=
UI-PROBE    map[error: field: ok:true provider:openai]
CONN-CARD   status:✓ Conexión correcta: OpenAI abrió la sesión con esa clave
            statusClass:status ok   test:shown/enabled   use:shown/enabled
```

- The user **sees** `✓ Conexión correcta: OpenAI abrió la sesión con esa clave`, in the ok style.
- `source=stored`: it used the saved credential, not something typed for the test.
- **`session.created` was really received.** The probe returns `OK` only on a positively identified
  readiness event, so `ok=true` is the observation risk 1 said had never been made. OpenAI's decoder
  learning that event was a change this branch had to make (`internal/stt/openai/wire.go`).
- The button is disabled in flight and enabled again after; it never disappears.
- **Persistence:** a probe writes nothing. Verified, not assumed — see UC-PROBE-08.

## UC-PROBE-02 — the same, for Grok (xAI): PASS

```
PROBE       slot=grok region= source=stored
PROBE-DONE  slot=grok ok=true code=
CONN-CARD   status:✓ Conexión correcta: xAI aceptó la clave   statusClass:status ok
```

The user sees `✓ Conexión correcta: xAI aceptó la clave`. The wording is **per-provider**, which the
design review insisted on: three cards that all said "Conexión correcta" would leave the user unsure
which one they had just tested. `transcript.created` observed for the first time.

## UC-PROBE-03 — the same, for ElevenLabs: PASS

```
PROBE       slot=elevenlabs region= source=stored
PROBE-DONE  slot=elevenlabs ok=true code=
CONN-CARD   status:✓ Conexión correcta: ElevenLabs abrió la sesión con esa clave
            statusClass:status ok
```

`session_started` observed for the first time.

## UC-PROBE-04 — I pasted my OpenAI key wrong: PASS

- **Actor:** someone whose key is mistyped or revoked.
- **Setup:** the affordance types a fixed, obviously invalid sentinel into the field
  (`loqui-debug-clave-invalida`). It never accepts a key from the environment — that would put a real
  credential into the process environment and into every log that captures it.

```
CONN-CLICK  ran:test(badkey)
PROBE       slot=openai region= source=typed
PROBE-DONE  slot=openai ok=false code=invalid_api_key
UI-PROBE    map[error:OpenAI rechazó la API key — revísala en Ajustes (invalid_api_key)
            field: ok:false provider:openai]
CONN-CARD   status:✗ OpenAI rechazó la API key — revísala en Ajustes (invalid_api_key)
            statusClass:status err   test:shown/enabled
```

**This is the case the whole design exists for.** Measured beforehand
(`docs/research/2026-08-06-where-realtime-stt-auth-fails.md`), OpenAI returns **HTTP 101 — accepted**
for a garbage key and refuses afterwards as an event. The original "open the socket, close it, report
success" design would have answered `✓ Conexión correcta` here. It answers `✗`.

- `source=typed`: the form's value won over the stored one, so a good stored key could not mask a bad
  typed one.
- The provider's own machine-readable code is passed through — `invalid_api_key` — which is what the
  user can actually search for. The provider's **prose** is not, and that is the point of UC-PROBE-07.
- `field:` is empty, so no red border: a credential the service rejected is not a badly filled field.

## UC-PROBE-05 — the same, for Grok: PASS

```
PROBE       slot=grok region= source=typed
PROBE-DONE  slot=grok ok=false code=AuthenticationFailure
CONN-CARD   status:✗ xAI rechazó la API key — revísala en Ajustes (AuthenticationFailure)
            statusClass:status err
```

xAI is the one of the three that refuses **at the handshake** (HTTP 400), not as a post-upgrade
event. Both routes through the protocol reach the same user-facing shape.

## UC-PROBE-06 — the same, for ElevenLabs: PASS

```
PROBE       slot=elevenlabs region= source=typed
PROBE-DONE  slot=elevenlabs ok=false code=auth_error
CONN-CARD   status:✗ ElevenLabs rechazó la API key — revísala en Ajustes (auth_error)
            statusClass:status err
```

ElevenLabs accepts the upgrade **and then closes with 1000 — a normal close — after refusing**.
Inferring success from a clean close would have produced a false green through a second door. It
reports `✗`.

## UC-PROBE-07 — my key never turns up in a log or on screen: PASS

- **Actor:** anyone whose key is echoed back by the service in its rejection.
- **Intent:** the P0 of this change. OpenAI's rejection masks the middle of the key and leaves **the
  last four characters intact**, so an exact-string redaction would not have caught it.
- **Verification:** scan all twelve logs from this run for the invalid sentinel and for any
  fragment of the four real stored credentials.

```
sentinel 'loqui-debug-clave-invalida' occurrences across the three rejection logs:  0  0  0
logs from this session: 12
secret-body fragments (8-char windows, vendor prefix excluded) leaked: 0
```

- Zero occurrences of the sentinel, in runs where the service demonstrably echoed it back.
- Zero fragments of any real key — including the green runs, which are the ones that actually put a
  live credential on the wire.
- One apparent hit was chased down and dismissed: files from an earlier session matched `sk-proj-`,
  the **public format prefix** shared by every OpenAI project key, not secret material.

## UC-PROBE-08 — testing a connection changes nothing: PASS

- **Intent:** a probe is a read. The plan requires that a failed probe leave the stored configuration
  exactly as it was, and the same must hold for a successful one.
- **Verification:** the credential file and the settings file, before and after twelve launches and
  twelve probes.

```
secrets.json    406 bytes, mtime 2026-08-06 17:16, sha256:1217ba832e398de6…   (unchanged)
settings.json   598 bytes, mtime 2026-08-06 14:24, provider=azure            (unchanged)
```

Neither file moved. This is the **Persistence** re-check for every UC above: for a read, the
persistence claim is that nothing persisted, and it is checked rather than asserted.

## UC-PROBE-09 — a region is not demanded from a service that has none: PASS

- **Intent:** the trap the plan was written to avoid. `TestConnection` resolved the region
  unconditionally for every slot, so widening the allowlist alone would have made testing a Grok key
  fail with *"elige una región antes de probar la conexión"* — an Azure region demanded of a service
  that has no such concept.

Across all six probes above, every non-Azure slot resolved with an **empty** region:

```
PROBE  slot=openai      region=  source=…
PROBE  slot=grok        region=  source=…
PROBE  slot=elevenlabs  region=  source=…
```

`region=` is empty in all six while the Azure region picker still holds `eastus2` (visible as
`region:eastus2` in every card report — it is a single global setting, and the probe correctly
ignores it for these slots).

---

## What this report does NOT cover

1. **That any of the three can actually transcribe.** A green probe proves the credential and that a
   session opens — not that audio flows, arrives, or comes back as text. Same limit Azure's probe has.
   The three engines still have `⬜` in the "real transcription" column of `CONTINUITY.md`, and this
   report does not change that. It is now, however, a *much* shorter step: the credential is no longer
   the unknown.
2. **The non-auth failure buckets** — quota, rate limit, account action, 5xx. The plan cut them from
   scope deliberately (see "The scope cut"): they cannot be provoked without an account in that state,
   and classifying them from documentation alone was the thing three review rounds kept growing and
   could never verify. They are covered by Go tests against a local WebSocket server, which is a
   weaker claim than this one and is stated as such.
3. **Concurrency between a probe and a save.** `probe()` deliberately stays out of the write queue but
   waits for it. Provoking the overlap needs the `+` step grammar and belongs with the next change
   that touches that queue.
4. **A cross-engine review of the diff.** Codex reviewed the *plan* three times; the code written
   afterwards has not been reviewed by another engine. That is a ship-gate matter, not an E2E one, and
   it is recorded in `.workflow/state.md`.
5. **Azure's own card.** Unchanged by this work and already covered by
   `2026-08-06-keys-in-a-file.md` UC-7.
