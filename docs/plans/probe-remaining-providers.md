# Plan — "Probar conexión" for Grok, OpenAI and ElevenLabs

- **Date:** 2026-08-06
- **Asked for by the user** on 2026-08-01 and again on 2026-08-06: OpenAI has no way to test the
  connection, so add one.
- **Reference:** Azure's green path, now verified — UC-7 of
  `docs/e2e/reports/2026-08-06-keys-in-a-file.md`.
- **Design review:** codex — three iterations, all **REWORK** (1 P0/5 P1/3 P2, then 3 P1/2 P2, then
  1 P0/5 P1/4 P2). All three confirmed the safety property: no path reports `OK` for an invalid
  credential. Every finding was verified against the code or the vendored library before being accepted.
- **This is v5, and it deliberately CUTS SCOPE.** Three rounds of review kept growing one thing — an
  exhaustive per-event classification matrix across three services — and none of it is verifiable
  without credentials this machine does not have. v5 keeps every safety and honesty finding and replaces
  that matrix with something simpler and better. See "The scope cut" below. The log is at the end.

## The scope cut, and why it is an improvement

The button the user asked for answers one question: **is my key good?** Three review rounds pushed the
design towards answering a second one — *exactly* which of nine failure categories a provider is
reporting — and that is where it kept breaking:

- OpenAI returns **429** for both rate limits and exhausted credit, so status cannot classify it;
  `error.code` must win, and today's `handshakeCode` maps every 429 to throttling (`errors.go:97`).
- ElevenLabs' `Outcome` does not carry the event name at all (`wire.go:116`), so `quota_exceeded` and
  `unaccepted_terms` are indistinguishable after decoding.
- Azure groups 401 and 403 into one credential verdict (`token.go:124`), contradicting a shared matrix
  that sends every 403 to `Forbidden`.
- And **none of those categories can be provoked without an account**: an invalid key is measurable, an
  exhausted quota is not. Nine buckets, one of which is testable.

So instead of mapping every documented signal into a bucket I cannot verify:

> **Three user-facing outcomes — the key works, the key was rejected, or something else went wrong —
> and for the third, SHOW THE PROVIDER'S OWN MACHINE-READABLE CODE.**

`insufficient_quota`, `unaccepted_terms`, `queue_overflow` are short, non-prose, contain no key
material, and tell the user exactly what to search for. Passing one through is **more honest and more
useful** than my classifying it into `Quota` from documentation I cannot test, and it cannot go stale
when a provider adds a code.

The machine-readable signal is still **preserved** in the decoders (ElevenLabs' event name, OpenAI's
`error.type`/`error.code`) — that part of the review stands, because without it there is nothing to show
and nothing useful in the log. What is dropped is only the mapping layer on top.

**Kept in full, because these were about safety and honesty, not granularity:** the first-message
protocol, redaction at both boundaries including logs, the conditional region, the injectable registry
and its contract, per-provider wording, the corrected close mechanics, and every false comment.

## Goal

The three ported cloud engines that carry a credential get a working "Probar conexión", so a user can
find out whether their key is good **before** discovering it halfway through a dictation.

## What a probe must never do: report a bad key as good

A false green is the worst outcome this feature can produce — worse than having no button, because the
user would then go looking for the problem anywhere else. v1 of this plan would have produced one.

### Measured against the real services, 2026-08-06

An invalid key, over HTTP/1.1, with the exact handshake each provider's runtime uses. It costs nothing
and needs no valid credential — it is how xAI's real behaviour was found in July. **Method, raw payloads
and the traps are recorded in `docs/research/2026-08-06-where-realtime-stt-auth-fails.md`**; the summary:

| Service | Handshake | The auth failure arrives as |
| --- | --- | --- |
| **xAI** | **400** `{"code":"Client specified an invalid argument","error":"Incorrect API key provided…"}` | the handshake |
| **OpenAI** | **101 — accepted** | post-upgrade event `{"type":"error","error":{"code":"invalid_api_key","message":"Incorrect API key provided: sk-proj-********************ueba…"}}`, then close **3000** `invalid_request_error.invalid_api_key` |
| **ElevenLabs** | **101 — accepted** | post-upgrade event `{"message_type":"auth_error","error":"You must be authenticated to use this endpoint."}`, then close **1000** |

**Two of three accept the upgrade with a garbage key.** `dial → close` would have answered
`✓ Conexión correcta` to any string pasted as an OpenAI or ElevenLabs key. v1 claimed all three
authenticate at the handshake; that is true only for xAI. Our own ElevenLabs runtime already implied it
— `provider.go:322-328` arms a readiness timer the moment the socket opens and `wire.go:149` decodes
`session_started` — but the measurement is what settles it.

Three things fall out that no amount of reading would have decided:

1. **A NORMAL close code does not mean success.** ElevenLabs closes with **1000** after refusing the
   credential. Inferring a good key from a clean close would reproduce the false green through another
   door, so readiness must be *positively* confirmed and never inferred from the absence of an error.
2. **Both hand over a machine-readable code**: `error.code = invalid_api_key` plus close 3000 for
   OpenAI, `message_type = auth_error` for ElevenLabs. Classification keys off those, never off prose —
   the bug `policy.go:36` already documents.
3. **The P0 leak is worse than "redact the exact string".** OpenAI's message masks the middle of the key
   and leaves **the last four characters** intact, so an exact-string redaction would not catch it.

### The protocol, corrected

> **dial → wait for the first server message → classify it → bounded close**

- A **readiness** message means the session exists, so the credential was accepted:
  `transcript.created` (Grok), `session_started` (ElevenLabs), `session.created` (OpenAI).
- An **error** message is classified and reported, even after a successful upgrade.
- **The socket closing before any message is a FAILURE**, not a success.
- **A budget expiring with no message is a FAILURE** (`Service`), not a success. Silence is not consent.

Nothing is ever sent: no `session.update`, no audio, no `commit`, no `audio.done`/`finalize`. Those
configure, start or flush transcription work. All three services send their first message unprompted.

**OpenAI needs one addition, and it is worth naming.** Its provider does not wait for a server event at
all — `provider.go:30` says "ready here means the socket is open and configured" — and its `Decode`
does not recognise `session.created`, so that message currently falls through to `Ignore`. The probe
must recognise it. That is a probe-only concern; the dictation path is not changed by this work.

## Architecture

Unchanged in shape. Each provider package gains an exported `TestConnection` beside its provider, and
`internal/app/settings_probe.go` dispatches through a registry. Rules stay in Go; the page paints what
it is handed.

## The trap this plan exists to avoid

`TestConnection` in `settings_probe.go` resolves the region **unconditionally**, before the key:

```go
regionID, res, ok := s.resolveRegion(region)   // ← for every slot
```

Widening the allowlist and nothing else would make testing a Grok key fail with *"elige una región
antes de probar la conexión"* — an Azure region demanded of a service that has none.
`store.UsesAzureRegion` already draws that line (`settings_write.go:493` uses it). The review confirmed
this covers every current slot: only Azure Speech reaches `resolveRegion`.

## Design

### 1. Never surface a provider's own auth text — P0, sharpened by measurement

`readReason` extracts **server-controlled text** and `describeHandshake` concatenates it into the
message (`grok/errors.go:62`, `openai/errors.go:35`, `elevenlabs/errors.go:29`), which reaches the
webview and the log. The review called this a realistic shape; the real service does it, and worse than
assumed — the key comes back masked in the middle with its last four characters intact.

So redacting the exact secret is necessary and **not sufficient**:

- **For auth-class outcomes the user-visible message is OURS**, never the provider's prose. We already
  know what to say — "OpenAI rechazó la API key — revísala en Ajustes" — and the provider's wording adds
  nothing the user can act on.
- The provider's raw text goes to the **log only**, with the exact secret redacted there too. Worth
  keeping for diagnosis; not worth showing.
- For non-auth outcomes (5xx, a malformed request) the provider's text IS the useful part, and is
  passed through redacted.
- **The test has to earn it**: the fake server echoes the key it received, and a partially-masked
  variant. v1's "the secret is not in the result" case would have passed while the leak existed, because
  its fake server never put the key in the body at all.
- This leak is **latent in the dictation path too** — the same functions word the overlay's messages.

**And `handshakeFailure` is not enough on its own** (P1, iteration 2, and it is the sharpest catch of the
round): the leak I actually **measured** arrives as a **post-101 event**, not as an HTTP rejection body.
`Outcome.Error` carries arbitrary server text and is propagated unredacted in all three packages. So
redaction has to sit at both boundaries:

1. `handshakeFailure` — the HTTP rejection path. Classify from the **original** reason, redact only the
   server-derived text, then hand the redacted reason to `describeHandshake`, so classification is not
   broken and our own wording is not mangled.
2. Wherever a post-upgrade `Outcome.Error` becomes a user-visible message or a log line — which is the
   path OpenAI's key-echoing message travels.

Fixing only the first would have left the measured leak wide open while the test suite went green.

### 2. One `TestConnection` per package, reusing that package's classification

**Who owns the result type — decided, because v3 left three incompatible readings.** A shared
`stt.ProbeResult` + `stt.ProbeKind` in `internal/stt`, the contract package. It is a plain struct with
no network dependency, so the constraint from the Grok plan holds (`internal/stt` must not import
`coder/websocket`). Azure is adapted to it **without changing its behaviour**; its `ConnResult` stays
where it is for its own callers, or becomes an alias. What is NOT done: making `internal/stt/azure` the
owner of every other provider's contract, which is what "reuse azure.ConnResult" would have meant.

```go
func TestConnection(ctx context.Context, key string, opts ProbeOptions) stt.ProbeResult
```

`ProbeOptions` carries only `Endpoint` (already a Config field in all three, for tests) and the ready
budget. **`Model` is NOT in it** — v1 claimed OpenAI's URL depends on the model and the code says the
opposite: `openai/wire.go:28`, "the model goes in the session payload, not the query".

Not a shared WebSocket prober: the three handshakes differ in URL, auth mechanism and failure reading,
and each package owns a tested `handshakeFailure`. Extracting the shared socket *lifecycle* is a
separate, still-open refactor.

### 3. Three outcomes, and the provider's own code for the third — v5

`NoKey`, `KeyRejected`, `Failed`. Azure keeps `BadRegion`, which only it can produce.

| Signal | Outcome |
| --- | --- |
| no key typed and none stored | `NoKey` — never touches the network |
| 401; `auth_error`; `error.code = invalid_api_key`; a 400 whose body names the key (and, for OpenAI only, an **empty** 400 — see the per-package note) | `KeyRejected` |
| a readiness event by name: `transcript.created`, `session_started`, `session.created` | **OK** |
| any other error event, any other status, malformed JSON, socket closed before readiness, or no message within the budget | `Failed`, carrying `Code` — the provider's own machine-readable string when it gave one |

**`Failed.Code` is the design decision, not a fallback.** `insufficient_quota`, `unaccepted_terms`,
`queue_overflow`, `server_error`, close `3000` — short, non-prose, no key material, and precisely what
the user would search for. Showing it beats classifying it into a bucket derived from documentation that
cannot be tested here, and it does not go stale when a provider adds a code.

**Azure's exception, declared rather than papered over** (P1, iteration 3): Azure groups 401 **and** 403
into one credential verdict (`token.go:124`). It keeps doing that. A shared matrix that sent every 403
somewhere else would have silently changed Azure's verified behaviour — the review caught the
contradiction between "Azure unchanged" and the matrix, and this is the resolution.

**What is NOT claimed:** that 403 means "a valid key without access". OpenAI documents 403 for an
unsupported region too. So a 403 is `Failed` with its code shown, not a diagnosis.

#### The decoders still have to preserve the signal — and this reaches dictation

Dropping the mapping layer does **not** drop this part of the review, because without it there is no
code to show and nothing useful in the log:

- **ElevenLabs' `Outcome` does not carry the event name at all** (`wire.go:116`), and `Decode` only
  treats a name as an error when it contains `"error"` (`wire.go:157`) — so `quota_exceeded`,
  `unaccepted_terms`, `rate_limited`, `queue_overflow` and `resource_exhausted` fall through to
  `Ignore`. Both have to change: recognise them, and keep the name.
- **OpenAI's `wireError` keeps only `message`** (`wire.go:120-122`), discarding `type` and `code`. Keep
  both.
- **A latent dictation bug, with the consequence stated correctly this time** (P2, iteration 3
  corrected my overstatement): the specific cause is lost, not the session. ElevenLabs closes the socket
  after its error event, and the current code reports that close as a generic connection loss
  (`provider.go:404`) — it does **not** hang to the readiness timeout, and after `session_started` that
  timer cannot end the session anyway. So the defect is a wrong, vaguer message, which recognising the
  events fixes. It is a **behaviour change in dictation** and needs tests either side of readiness.
- **Two false comments in production must go** (P2, iteration 3): `elevenlabs/errors.go:13` and
  `openai/errors.go:13` both state that the handshake is the only reliable machine-readable signal and
  that post-upgrade errors carry only prose. The measurements disprove both. This repo has now shipped
  three bugs from comments that outlived their code; these are not left standing.
- **OpenAI's `wireError` keeps only `Message`** (`wire.go:120-122`), discarding `type` and `code` — so
  v3's claim that classification would key off `error.code` was **not implementable**. `Type` and `Code`
  have to be preserved. Keeping them changes nothing for dictation today beyond making its error
  reporting sharper.

**The runtime's own message strings are not reused verbatim.** Several are written for a session that
will retry (`grok/errors.go:137` and its peers say so); a probe does not retry, and telling the user it
will is false.

**Two corrections, and the second is a correction of the first.** v1 said reading the body changes the
message and never the classification — false. v3 then said a 400 with a key-mentioning **or empty** body
becomes `AuthenticationFailure` in all three — also false, and wrong in the same way: it assumed the
three packages are symmetric after reading only one. Verified individually:

- **grok** requires the reason to mention the key (`errors.go:64`).
- **elevenlabs** requires the same (`errors.go:33`).
- **openai** additionally treats an **empty** 400 body as authentication (`errors.go:40`).

The invariant that IS true, and it is narrower still: reading the body never changes the *retry*
decision, because both codes are non-retryable. Recorded this carefully because the wrong version was
load-bearing twice.

### 4. The close: always `CloseNow`, and the verdict decided before it — P1, iteration 2

v3 proposed "normal close with a short slice of the budget, then `CloseNow`". **That is
unimplementable**, verified in the vendored `coder/websocket v1.8.14`:

- `Close(code, reason)` takes **no context** (`close.go:99`). It uses two internal
  `context.WithTimeout(context.Background(), 5*time.Second)` — one to write the close frame
  (`close.go:185`), one in `waitCloseHandshake` (`close.go:199`) — and then `waitGoroutines`, which
  waits on a **15 s** timer (`close.go:230`).
- `Close` and `CloseNow` both begin with `casClosing()`, which is `closing.Swap(true)`
  (`close.go:325`). So calling `CloseNow` after a slow `Close` does **not** interrupt it: the second
  caller falls into `waitGoroutines` and waits too.

Worst case for `Close` is therefore ~5 s + ~5 s + 15 s, none of it interruptible. So:

- **The probe always uses `CloseNow`**, and — corrected a third time, from the source — that is
  effectively immediate rather than the 15 s tail v4 claimed. `CloseNow` closes the `rwc` directly
  without sending a close frame or waiting for the peer (`close.go:130` → `conn.go:151`), then calls
  `waitGoroutines`, whose 15 s timer applies **only** to the goroutine `CloseRead` starts
  (`close.go:230`, gated on `closeReadCtx != nil`).
- **So the probe reads synchronously with `conn.Read(ctx)`, never `CloseRead` and never its own reader
  goroutine.** With no such goroutine there is nothing for `waitGoroutines` to wait on, and the close
  cannot stall. This makes the design simpler, not more careful — v4's "bounded close" machinery was
  solving a problem that only existed because of how it read.
- **The verdict is computed BEFORE the close**, from dial and readiness — the two phases that *are*
  context-bounded. Whether the close succeeds cannot change the answer.
- The close is still **awaited**. If a future version ever does spawn its own reader, `CloseNow` will
  **not** join it and it must be waited on explicitly.
- **What the budget promises, and now it holds:** dial + readiness, bounded by the context.

v3's and v4's "peer never answers the close" case is **deleted**: with `CloseNow` it exercises nothing.
What replaces it: assert no reader goroutine outlives the call, and that the server observes the
connection closed.

### 5. A registry that cannot drift, and is injectable — P1 and P2 from the review

```go
type prober struct {
    name  string                 // "xAI", "OpenAI", "ElevenLabs", "Azure Speech"
    probe func(ctx context.Context, key string, region string) ConnResult
}
```

The `name` is not decoration: today's success, timeout and network messages hard-code Azure
(`settings_probe.go:101,108,116`), so Grok with no DNS would say *"No se pudo contactar con Azure"* and
a green OpenAI would claim Azure accepted the key "for that region". Each entry words its own outcomes.

- The registry replaces `slotsWithProbe`, so "listed but unreachable" stops being expressible.
- **Injectable per `SettingsService` instance**, not a mutated global: parallel tests and concurrent
  probes would otherwise race. The production registry stays immutable.
- **Contract test, in the reviewer's stronger form** — no exemptions, since none are needed:
  for every slot in `store.AllKeySlots`, `hasProber(slot) == store.IsAvailableKeySlot(slot)`.
  A ported slot without a prober and a prober for an unusable slot fail the same test. `azure-openai`
  satisfies it by being neither. **`hasProber` requires a non-nil function**, not merely a present map
  key — otherwise a registered `nil` would satisfy the contract and panic at the first click.

### 6. Region only where a region exists

```go
regionID := ""
if store.UsesAzureRegion(keySlot) {
    if regionID, res, ok = s.resolveRegion(region); !ok { return res }
}
```

The `PROBE` log line keeps its shape, with `region=` empty for the others, so the E2E probes that assert
on it keep working.

## Cost, with the unknown left as unknown

- **OpenAI**: documents that the connection itself is not billed; transcription bills when audio is
  sent or committed.
- **ElevenLabs**: bills by the duration of audio sent, so a probe that sends none should not bill.
- **xAI**: publishes an hourly streaming price with **no documented minimum unit or rounding**. v1
  called this "negligible"; that was an assumption, not a finding. It is **unknown**, and it is the one
  place where a probe might cost something. It is also what every real dictation already does at its
  start.

## Task list

1. Redaction in `handshakeFailure` for all three packages + tests whose fake server echoes the key.
2. `internal/stt/grok/probe.go` + tests — dial, wait for `transcript.created`, bounded close.
3. `internal/stt/elevenlabs/probe.go` + tests — same, `session_started`, and the post-101 error events.
4. `internal/stt/openai/probe.go` + tests — same, `session.created` (which its decoder must learn).
5. `internal/app/settings_probe.go` — the registry, per-provider wording, conditional region, dispatch.
6. Contract test: `hasProber(slot) == IsAvailableKeySlot(slot)`.
7. App-level tests: each slot reaches its own prober, region not demanded where absent.
8. E2E against the packaged app: the invalid-key path against the **real** services (costs nothing and
   is how xAI's 400 was found); the green path only where a credential exists.

## Tests (TDD, red before each piece)

Against a **real local WebSocket server** per package (`httptest` + `websocket.Accept`, the pattern
already in each `fake_server_test.go`) — the thing under test is a handshake and a first message, which
a mocked dialer cannot show.

| # | Case | Asserts |
|---|---|---|
| 1 | empty key | `NoKey`, **no dial attempted** |
| 2 | 101 then the readiness message | `OK=true` |
| 3 | 101 then `auth_error` (ElevenLabs' real shape) | `BadCredentials` — **the measured false green v1 would have produced** |
| 3b | 101 then `{"type":"error","error":{"code":"invalid_api_key"}}` then close 3000 (OpenAI's real shape) | `BadCredentials`, classified from `error.code` and not from the prose |
| 3c | 101 then `auth_error` then close **1000** | `BadCredentials` — a NORMAL close after a refusal must not read as success |
| 4 | 101 then `unaccepted_terms` | `AccountAction` |
| 5 | 101 then `quota_exceeded` | `Quota` |
| 6 | 101 then close, no message | failure, never `OK` |
| 7 | 101 then silence past the budget | `Service`, and it returns near the budget |
| 8 | 101 then malformed JSON before readiness | failure, no panic |
| 9 | 401 / 403 | `BadCredentials` / `Forbidden`, kept apart |
| 10 | 400 with a body naming the key | `BadCredentials` |
| 11 | 400 with an unrelated body | `BadRequest`, so the two 400s stay apart |
| 12 | 429 | `RateLimited`, and the message does not promise a retry |
| 13 | 5xx | `Service` |
| 14 | unreachable host | `Network` |
| 15 | context already cancelled | `Network`, promptly |
| 16 | **rejection body echoes the key verbatim** | the key appears in **no** field of the result |
| 16b | **rejection body echoes it partially masked**, `sk-proj-****ueba` — OpenAI's real shape | no suffix of the key reaches the result: the auth message is ours, not the provider's |
| 16c | a non-auth failure with a useful body (5xx) | the body IS passed through, redacted — the useful case is not thrown away with the leak |
| 16d | **101 then an error EVENT carrying the key** — the path actually measured | no field of the result and no log line contains the key or a suffix of it. This is the case `handshakeFailure`-only redaction would have missed |
| 17 | the auth actually travels | the server asserts the exact header/subprotocol received |
| 18 | nothing else travels | the server sees **no** message frames from the client |
| 19 | dial and readiness nearly exhaust the budget, **then** the peer never answers the close | the SUM is bounded, asserted on wall-clock — v3 tested the close in isolation, which proves less |
| 19b | 402, and `quota_exceeded` as an event | `Quota` both ways |
| 19c | `unaccepted_terms` | `AccountAction` |
| 19d | an unrecognised event name before readiness | conservative failure, never `OK` |
| 20 | the connection does not outlive the call | the server observes it closed |

At the app level: each slot reaches **its own** prober and no other (the mistake `IsAvailableKeySlot`
once invited, where a Grok key would have gone to Azure's endpoint); a region is not demanded for slots
that have none and still is for Azure; a non-empty region is ignored for a non-Azure slot; each result
names its own provider and not another's; two concurrent probes on different slots do not interfere.

**Every new test verified by mutation.**

## Risks

1. ~~**"Auth fails at the handshake" is verified only for Grok."**~~ **Resolved by measurement**, and it
   went the other way: two of three do NOT fail at the handshake. The rejection path of all three is now
   measured against the real service, including the exact machine-readable codes the classification will
   key off. What remains unmeasured is the **green** path for these three — nobody has seen a real
   `session.created`, `session_started` or `transcript.created` from these services, because there is no
   credential on this machine. So the readiness names come from the docs and from our own runtime, not
   from observation, and a wrong name would show up as a probe that always times out. That is a visible,
   safe failure — never a false green — which is the property that matters.
2. **OpenAI's handshake may not match its current documentation.** The runtime puts a persistent API key
   in the `openai-insecure-api-key.*` subprotocol (`provider.go:506`); OpenAI now documents
   `Authorization: Bearer` for server-side use and reserves that subprotocol for browser-like clients
   with an ephemeral token. The probe will use **exactly** dictation's handshake, so the two cannot
   disagree — but if the subprotocol form is deprecated, dictation is the thing that breaks, and the
   probe would then correctly report it. **Resolving that is out of scope here and is recorded as its
   own item**; changing dictation's auth is not a thing to do inside a plan about a test button.
3. **A green probe proves the credential, not the transcription.** Same limit Azure's has.
4. **No credential exists on this machine for any of the three** — confirmed with the user on
   2026-08-06 — so this feature's E2E is PARTIAL by construction: the rejection paths are executable
   against the real services and already are, the green paths are not. Whoever gets a credential for any
   of the three should run its green path and add a UC, exactly as Azure's was closed.

## What is NOT included

- Extracting the shared WebSocket lifecycle out of `internal/stt/grok`. Its condition is met; separate
  refactor.
- A probe for `azure-openai`: the subservice is not ported.
- Changing what a probe does for Azure, or how dictation authenticates anywhere.

## Review log

**Iteration 1 — codex — REWORK.** 1 P0, 5 P1, 3 P2. Every finding verified against the code before
being accepted; none dismissed.

| Finding | Resolution in v2 |
| --- | --- |
| **P0** — a provider's error body can echo the key into `ConnResult.Error` and the log | Redaction in `handshakeFailure`, and the fake server now echoes the key so the test can fail. Fixes the same latent leak in the dictation path |
| **P1** — ElevenLabs authenticates AFTER the upgrade, so `dial → close` returns a false green | **The protocol changed**: dial → first message → classify → bounded close. Verified in our own code: `elevenlabs/provider.go:322-328`, `wire.go:149` |
| **P1** — Azure's wording leaks into the other providers' messages | Each registry entry carries its provider name and words its own outcomes |
| **P1** — the `Kind` set collapses cases needing different actions | Nine kinds, `403` and account/quota states kept apart; the runtime's retry-promising strings not reused |
| **P1** — the 15 s budget does not bound `Close` | One total budget for dial + readiness + close, `CloseNow` fallback, close awaited |
| **P1** — the OpenAI section contradicts the code (`Model` in the URL) and its current docs | `Model` dropped; the handshake discrepancy recorded as risk 2 and explicitly out of scope |
| **P2** — no test seam for the dispatch in `internal/app` | Registry injectable per service instance; production registry immutable |
| **P2** — "or explicitly exempt" reintroduced a second allowlist | Dropped for the reviewer's stronger contract: `hasProber(slot) == IsAvailableKeySlot(slot)` |
| **P2** — missing test cases | Table grown from 13 to 20, plus the app-level list |
| Also corrected | v1's false claim that reading the body never changes the classification; and xAI's cost changed from "negligible" to **unknown** |

**Iteration 2 — codex — REWORK.** 3 P1, 2 P2. It confirmed the safety property first: no documented path
produces `OK` for an invalid credential under the first-message protocol, and `session.created` does
arrive unprompted in OpenAI. Every finding verified against the code or the vendored library before
acceptance; none dismissed.

| Finding | Resolution in v4 |
| --- | --- |
| **P1** — the proposed close is unimplementable: `Close` takes no context, has two internal 5 s timeouts and a 15 s `waitGoroutines`, and `CloseNow` afterwards just waits for it (`close.go:99,185,199,230,325`) | **Always `CloseNow`**, and the verdict is computed before the close so its tail cannot change the answer. The library's 15 s floor is documented rather than pretended away. Verified in the vendored source |
| **P1** — no exhaustive event/status → `Kind` mapping, and two decoders cannot express one: ElevenLabs matches only names containing `"error"` (`wire.go:157`); OpenAI discards `error.type`/`error.code` (`wire.go:120-122`) | Full mapping table added. Both decoders change — and the ElevenLabs one is a **latent dictation bug**: an out-of-credit session is silently ignored today |
| **P1** — redaction was scoped to `handshakeFailure`, but the measured leak arrives as a post-101 event, where `Outcome.Error` propagates unredacted | Redaction at **both** boundaries. Case 16d covers the path that was open |
| **P2** — three incompatible readings of who owns the result type | Decided: shared `stt.ProbeResult` in the contract package, Azure adapted without behaviour change |
| **P2** — v3 still asserted, falsely, that an empty 400 is auth in all three | Corrected per package: grok and elevenlabs need the body to name the key; only OpenAI treats an empty 400 as auth. Twice wrong in the same way — assuming symmetry from reading one package |
| Also | `hasProber` must require a non-nil function, not a present key |
