# Plan — Grok (xAI) STT provider

- **Date:** 2026-07-28
- **Phase:** 3 of the port (`docs/plans/loqui-go-port.md`)
- **Branch:** `port/foundation`
- **Prior research:** `docs/research/2026-07-28-xai-stt-streaming.md` (mandatory — it
  invalidated one of the six assumptions in the Electron code)
- **Design review:** codex `gpt-5.6-sol` xhigh × 2 iterations, both REWORK. This is
  v3. The log of what changed in each round is at the end.
- **The user's scope decision (2026-07-28):** Grok only; the controller's two pre-existing bugs
  get documented and go in their own change.

## Goal

That `provider: "grok"` actually dictates: a provider satisfying `stt.Provider`
(`internal/stt/stt.go:65`), fed by the host's single audio capture, speaking xAI's
WebSocket with header auth.

## Non-goals

- ElevenLabs and OpenAI realtime.
- The Settings UI (phase 4). Verification through `cmd/stt-probe` and `settings.json` by hand.
- `smart_turn`, `keyterm`, `vad_threshold`.
- **The controller's two pre-existing bugs** (see the end). The user's decision. This plan
  limits itself to **opening no new route** towards them.

## The finding that changes the port

`parseGrokEvent` (`../loqui/src/shared/grokStt.ts:38`) maps **every** `transcript.partial` to
interim and **only** `transcript.done` to final. Three things are wrong:

1. The schema distinguishes the final with `is_final`/`speech_final`, not with the event name.
2. **The official `transcript.done` example carries `text: ""` after 6.43 s of audio** ⇒ if the
   only final comes from there, the dictation is delivered **empty**.
3. The guide calls the `speech_final=true` event the "complete **stitched** utterance" ⇒ it
   re-emits text already sent. Accumulating all of it would duplicate every sentence longer
   than ~3 s.

The server can also **correct** text it has already sent. And the docs pin down the scope of no
text at all ("not verifiable").

## The timeline (the heart of the change)

The controller only **accumulates** (`controller.go:293`, `history.go:40`), so the provider has
to deliver text already resolved: **a single `Final`** at the end, assembled from a timeline the
provider maintains.

**The timeline's unit is the word, not the segment.** v2 stored text segments and replaced every
segment overlapping the new event's interval. The review knocked that down with correct
counterexamples: a segment with words in `[0,1]` and `[4,5]` has hull `[0,5]`, so a new event in
`[2,3]` erased it entirely and lost two legitimate words. Likewise a `done` partially
overlapping two segments.

v3 design:

- state: `words []word`, `word = {start, end float64; text string}`, ordered by
  `(start, end)`;
- **half-open intervals `[start, end)`**. The API uses adjacent words where one ends exactly
  when the next begins (`"The" 0.24→0.48`, `"balance" 0.48→0.96`), so with closed intervals
  every word would erase its neighbour;
- `is_final=false` → `Partial` with the raw text. It does **not** touch the timeline;
- `is_final=true` (chunk **or** utterance) → from the event's words take the interval
  `[min(start), max(end))` and **remove only the existing words overlapping that interval**
  (`w.start < evEnd && w.end > evStart`), then insert the new ones. Only what the server is
  really replacing disappears;
- tie-break on equal marks: a stable order by `(start, end, arrival order)`, so two words with
  the same `start` do not dance between runs;
- an `is_final=true` event **without** `words` but with `text` → use `[start, start+duration)`
  as the interval and the whole text as one "word" spanning it. It is the only option without
  per-word data, and it keeps the replacement rule;
- at the end, by **any** route: join the texts in temporal order → one `Final`.

### `transcript.done` without `words` (an explicit limitation)

`done` carries no top-level `start`. If it also arrives with `words: []`, there is **no**
positional evidence at all. The review proved that no string-comparison rule is correct: with
`"I agree"` assembled, a tail `done` of `"I agree again"` and a corrected `done` of
`"I disagree"` are indistinguishable, and prefix matching gets one right and the other wrong.

v3 rule, deliberately conservative:

- if the timeline **is empty** → use `done.text` (that is the short, real case: audio briefer
  than the ~3 s window, where `done` is the only event with text);
- if the timeline **has something** → **ignore** `done.text` and keep what was assembled,
  **logging** it.

Rationale: the timeline is built from per-word evidence; a `done` without `words` contributes
none. This **never duplicates** — the possible failure is missing a final correction, which is a
minor and visible failure, not silent duplication. It is **declared best effort**, not a
guarantee; the experiment in the research resolves it with data.

### Output order: `Final → [Canceled] → Stopped`

v2 said `[Canceled] → Final → Stopped`. **That is wrong, and the review traced it exactly.**
On the retryable route, `handleCancelLocked` does `tracker.Bump()` (`controller.go:350`) and
from then on `Accepts(gen)` requires `gen == t.gen` (`tracker.go:57`), so a `Final` arriving
afterwards with the old gen is **discarded** (`controller.go:287`) and so is the `Stopped`.
The whole dictation would be lost precisely in the case where there is something to save.

With `Final` **first**: it is accepted and accumulated with the gen still current; if the cancel
is retryable, `c.parts` survives the `Bump` and is delivered when the session really ends; if it
is terminal, `flushLocked` (`controller.go:343`) delivers it there and then. Correct in both.

**Verified by reading `tracker.go:56-58` and `controller.go:286-289, 333-363`.**

### `Finalize` is not sent

`finalize` keeps the session open and does **not** produce `transcript.done`; `audio.done` does,
and it flushes. But the `done` cannot be waited on as the only terminator (an `error` closes
without emitting it) ⇒ timeouts.

## Approach: everything in `internal/stt/grok`

v1 wanted to extract a generic `internal/stt/wsstream` right away, leaning on
`StreamingSttSession` + `SttAdapter` being a design proven in production. The review took that
apart: **that precedent contains the bug I am fixing** (`streamingStt.ts:65` closes on any final
after the finalize, which truncates when there are several `is_final=true` before the `done`),
and the proposed interface could no longer express the terminal/non-terminal distinction.
A precedent that has to be corrected does not validate the abstraction.

It will be extracted **when ElevenLabs arrives**, reading the common lifecycle from two real
implementations. ⇒ `docs/plans/loqui-go-port.md:71` (`streamingStt.ts` →
`internal/stt/stream.go`) is obsolete: there will be no `stream.go` until then, and it will be
its own package, never part of the contract.

```
internal/stt/grok/
  url.go        # BuildURL(language) — query params (pure)          ✅ done
  events.go     # protocol types + decoding of one message (pure)
  timeline.go   # the per-word timeline (pure)
  provider.go   # Provider: the socket's lifecycle
  errors.go     # handshake HTTP → structured ErrorCode
```

## Concurrency

The review found a liveness P0 in v2: a single command channel that also carries audio
saturates while `run()` is blocked writing, and then either the `stop` is dropped (the session
hangs) or `PushAudio` blocks (the microphone freezes).

v3 structure — **three separate paths**, each with its own guarantee:

1. **`run()`** is the only goroutine that touches session state and the only one that writes to
   the socket ⇒ frame order is acceptance order, by construction.
2. **Audio: a byte-bounded ring, with its own mutex.** It does not go through a channel.
   `PushAudio` takes the mutex, appends, and if it goes over the cap **drops the oldest**; then
   it makes a non-blocking "there is audio" signal to `run()`. That way the drop always falls on
   the oldest PCM, never on a control signal, and `PushAudio` cannot block and never loses the
   most recent frame (which was v2's defect).
   - Cap: **30 s** = 960,000 bytes at 16 kHz/16-bit/mono. Logged once when dropping starts.
3. **Control: its own channel that is never dropped.** `stop`, and the notices from the reader
   and the timers. Small fixed capacity, and each signal is sent **exactly once**, so it cannot
   fill up. `Stop` is idempotent (`sync.Once`) and **returns immediately**, as the contract
   requires (`stt.go:73`) — it never blocks, so it can even be called from inside a sink
   callback with no risk of deadlock.

More:

- **`Stop` rejects new audio atomically** (a flag under the ring's mutex) before asking for
  finalisation, and keeps what was accepted before. It is needed because `stopCapture` signals
  and closes the capture but does **not** wait for the pump (`dictation.go:357-369`), so a late
  frame can arrive afterwards. It is what Azure already does (`recognizer.go:196`).
- **The audio channel is never closed** (there is no channel; and the ring is marked closed
  under the mutex), so a concurrent `PushAudio` never writes into something closed.
- `PushAudio` and `Stop` **before** `Start`, and `Start` **twice**: defined and tested —
  no-op and error respectively, like the helper (`helper/provider.go:108`).
- Everything hangs off a cancellable `context` ⇒ neither the dial nor the reader nor the writer
  outlives `Stop`. The suite waits on the **provider's `sync.WaitGroup`**, not on a global
  goroutine count (which would be flaky, as the review pointed out).
- `Stopped` comes out exactly once, only from `run()`'s exit.

### Timeouts (a design, not a loose timer)

| Timeout | Value | Rule |
| --- | --- | --- |
| **dial** | 10 s | From the `Dial`'s `context`. Expired → `Canceled{ConnectionFailure}` |
| **ready** (`transcript.created`) | 15 s | Armed when the socket opens, disarmed when `Ready` arrives. Expired → `Canceled{ServiceTimeout}` |
| **write** | 5 s per frame | A `context` per `Write`. Expired → terminal route |
| **finalize** | 10 s | Armed **when `audio.done` is sent**, not before. Expired → close and emit what was assembled |

All four are `Config` fields with a zero default ⇒ only the tests lower them, just like the
helper (`helper/provider.go:46-50`).

## Mapping to `ErrorCode` (the ones in `internal/session/policy.go:31`)

| Situation | `ErrorCode` | Class | Retries |
| --- | --- | --- | --- |
| No API key | `NotConfigured` | config | no |
| Handshake 401 / 403 | `AuthenticationFailure` | auth | no |
| Handshake 429 | `TooManyRequests` | network | yes |
| Handshake other 4xx | `BadRequest` | config | no |
| Handshake 5xx | `ServiceUnavailable` | network | yes |
| Dial fails with no response | `ConnectionFailure` | network | yes |
| Ready timeout | `ServiceTimeout` | network | yes |
| Unexpected socket close | `ConnectionFailure` | network | yes |
| Server `error` event | `BadRequest` | config | **no** |

The last row is the contested point. The review objects, with reason, that those errors include
"pipeline failures" and "stream timeouts", which are not the configuration's fault, and that the
policy ends up incoherent (an `error` does not retry but an abrupt close does, being
equivalent).

It stays as it is, and it is a **decision with a declared cost**: as long as `controller.go:278`
resets `reconnectAttempt` on every `Started`, any retryable code can turn into an unbounded loop
against a service that **bills by the hour**. A misleading message is a smaller cost than an
open-ended bill. When the retry budget is fixed (the separate change), this row becomes
`ServiceError` with bounded retry — it is noted there.

Classification is never by prose: `policy.go:36` documents that bug.

## Language and logging

- The `grok` slot in `store.DefaultSettings()` with `["auto"]` ✅ done. `auto` ⇒ the parameter
  is **omitted**. In xAI, `language` is a **formatting** switch, not a recognition hint.
- `Event.Language` **empty**: streaming does not report a detected language.
- `Log func(tag, msg string)` like the helper. **Never** transcription text, **never** the
  key. Yes to: parse/read/write/close failures, `transcript.created`'s `id`, the drop from a
  full ring, and the `done`-without-`words` branch. **With a test** that neither the key nor the
  text ends up in the log.

## Environment escape hatch ✅ done

`keyReaderFor` was Azure-only, so another provider's key was silently ignored and the read fell
through to the Keychain that does not answer under ad-hoc signing (blocker 1 in `CONTINUITY.md`).
Generalised to one env var per slot (`LOQUI_GROK_KEY`, and the rest), with a test that one slot
does not satisfy another's read.

## Test plan (TDD: red before each piece)

### `url.go` ✅ done (4 cases, green)
Fixed params; `language` omitted with `auto`/empty; present with a real language; **no** `model`.

### `events.go` (pure)
`transcript.created` → `Ready` + exposes the `id`; `is_final=false` → `Partial`; a **flat**
`error` → `Error`; unknown type → `Ignore`; invalid JSON → `Ignore` without panic; `text` absent
or of another type → empty without panic; `confidence` absent ("omitted when 0") does not break
the decode; `words` absent does not break the decode.

### `timeline.go` (pure) — the review's counterexamples, one per test
- two finals with no overlap → joined in order;
- **silence gap**: words in `[0,1)` and `[4,5)`, then a final in `[2,3)` → **all three**
  survive (the case that broke v2);
- a "stitched" `speech_final` repeating the chunk → replaces, does not duplicate;
- a correction of the same span with a different number of words → the new one wins;
- a **`done` partially overlapping** two spans (`[0,2)`, `[2,4)`, done in `[1,3)`) → `"one"` and
  `"four"` are kept;
- a word straddling the boundary (previous up to 3.2 with its last word at 2.9, new 3.1→3.4)
  → not lost;
- **adjacent words** (`0.24→0.48`, `0.48→0.96`) → do not erase each other (half-open);
- identical marks → a stable, deterministic order;
- an out-of-order final (lower start) → inserted in its place;
- `done` with `text: ""` → erases nothing;
- `done` without `words` with the timeline **non-empty** → ignored and logged;
- `done` without `words` with the timeline **empty** → its text is used;
- interims never modify the timeline.

### `provider.go` (against a real local WS server: `httptest` + `websocket.Accept`)
Each case asserts the **complete sequence** and that nothing arrives after `Stopped`:
- audio queued before `Ready` is flushed **in order**; `Started` once;
- `Partial` with the sealed `gen`; **every** event carries `Gen`;
- `Stop` sends `audio.done` (a **text** frame) and waits for the `done` ⇒ `Final` → `Stopped`;
- **several `is_final=true` before the `done`** → a single `Final` with everything, nothing
  truncated;
- the `done` that never arrives → finalize timeout, `Final` of what was assembled → `Stopped`;
- an `error` event → **`Final` first**, then `Canceled{BadRequest}`, then `Stopped`;
- abrupt close → `Final` → `Canceled{ConnectionFailure}` → `Stopped`;
- handshake 401/403/429/4xx/5xx → the codes from the table, with no `Final`;
- dialling a non-existent host → `Canceled{ConnectionFailure}`;
- no key → `Canceled{NotConfigured}`;
- ready timeout → `Canceled{ServiceTimeout}`;
- **`Stop` during the dial** → `Stopped`, without hanging;
- **`Stop` racing `Ready`** → the queue is sent **before** the `audio.done`, asserted on the
  server side;
- **deterministic saturation**: a server that does not read, the ring full → the **old** is
  dropped, the `stop` **still arrives** and the session ends bounded (the liveness P0);
- `PushAudio` after `Stop` → rejected, without panic or blocking;
- `PushAudio` and `Stop` before `Start` → no-op; `Start` twice → error;
- `Stop` twice, and `Stop` **from inside** the sink callback → a single `Stopped`, no
  deadlock;
- an incoming 200 KB message → processed (read limit at 1 MiB, against `coder/websocket`'s
  32 KiB default);
- **wire**: the PCM goes out as a **binary** frame, the `audio.done` as **text**, the exact
  `Authorization: Bearer <key>` header, and the exact query params;
- **privacy**: neither the key nor the transcription text appears in the log;
- **goroutines**: the provider's `WaitGroup` closes on every terminal route;
- `-race` clean.

### `internal/app` ✅ partial
Done: the per-slot env override (3 cases). Missing: `buildProvider("grok")` with no key → a
configuration error distinguishing "there is no key" from "the Keychain does not answer"; with
`LOQUI_GROK_KEY` → it builds.

## Acceptance criteria

1. `./scripts/task.sh test` green with `-race`; `vet` and `gofmt` clean.
2. `cmd/stt-probe --provider grok` transcribes several sentences and they appear in
   `history.jsonl` as **one** message, **without duplicates**.
3. An invalid key → **one** `Canceled{AuthenticationFailure}`, with no retry.
4. `internal/stt` still does not import `coder/websocket`.
5. No new route opens an unbounded reconnection loop.

## Risks

1. **A session-relative `start` is not demonstrated.** Documented ("seconds from stream
   start") but with no multi-utterance example at all. The timeline depends on it. If it were
   utterance-relative, they would all overlap in `[0, dur)` and overwrite each other: the
   symptom would be "it only transcribes the last sentence" — unmistakable, not silent.
   Mitigation: the research's five-minute experiment as soon as there is a key.
2. **The handshake's 401 is not documented.** Checked with a bad key.
3. **Does `error` kill the socket?** The docs contradict each other; it is assumed terminal.
4. **Without an xAI key there is no criterion 2.** Everything else is verified against the
   local server.
5. **The `done`-without-`words` branch is declared best effort**, not a guarantee. It never
   duplicates; it can lose a final correction. (Retraction *with* `words` was resolved by
   `speech_final` — see point 4 of the implementation findings.)

## Out of scope: two pre-existing bugs (the user's decision)

They are **already** in `main` and affect Azure today. Verified by reading the code. They go in
their own change, with their own review cycle:

1. **The retry budget bounds nothing if the connection does open.**
   `controller.go:278` sets `reconnectAttempt = 0` on every `Started`; the only cap is at
   `controller.go:339`. A `Started → Canceled(retryable) → reconnect` cycle repeats **forever**
   at the first backoff interval. With hourly billing that is a spend loop. Probable fix: reset
   when the session produces text, not when it connects; or a budget per time window. **When it
   is fixed, Grok's `error` event becomes `ServiceError` with bounded retry.**
2. **Reconnection leaks the previous capture.** `controller.go:359` calls `StartEngine` without
   `StopEngine`; `dictation.go:115` overwrites `d.provider` and `dictation.go:359-361` the only
   handles to `capture`/`pumpDone` ⇒ every retry leaks a device and a goroutine, and speech
   during the backoff is lost. Related: `stopCapture` does not **wait** for the pump, which is
   why the provider has to reject late audio on its own (already covered above).

## What changed while implementing (findings from the code phase)

Three things that neither the plan nor the two design reviews saw, and that only appeared while
writing the code and talking to the real service:

1. **The service returns 400, not 401, with an invalid key.** Verified against
   `api.x.ai` on 2026-07-28, contradicting xAI's own error table. Mapping by status alone gave
   the message "xAI rechazó la petición (status 400)", which sends the user off to audit their
   configuration when what is wrong is the key. `handshakeFailure`
   (`internal/stt/grok/errors.go`) reads the body to tell them apart. It does **not** reintroduce
   the bug of classifying by prose: both branches are non-retryable, so it changes the message
   and never the behaviour.
2. **`Stop` was losing the tail of the dictation** (found in self-review, and confirmed
   independently by the code review as a P0). `select` chooses at random among ready cases, so
   the `stop` could beat its own audio and send `audio.done` before the last frames. The test
   reproduced it: **4 of 80 bytes** were arriving. It now drains before finalising.
3. **The timeline replacement used the convex hull of the incoming words**
   (a code-review P1). Incoming words in `[0,1)` and `[4,5)` gave `[0,5)` and erased a stored
   word in `[2,3)` that nothing incoming overlapped. The existing silence-gap test only covered
   the opposite arrival order, which passes either way — that is how it slipped through. It now
   compares against **each incoming word**.
4. **`speech_final` resolves the case that had been written off as unresolvable.** v3 of the plan
   declared "best effort" in the face of a **retraction**: if the server resends a span with
   *fewer* words ("a b c" → "a c"), the "b" overlaps nothing incoming and survives as stale text.
   The code review pointed out that the signal exists and was being thrown away: the docs call
   the `speech_final=true` event the "complete **stitched** utterance", i.e. **authoritative for
   its whole span**. So there are now two rules, not one:
   - a chunk final (`speech_final=false`) → **incremental**, per-word replacement;
   - an utterance final (`speech_final=true`) and `transcript.done` → **authoritative**, the
     whole span is cleared and its words inserted.

   And the span an authoritative event clears is the **declared** one (`start`,
   `start+duration`), unioned with the extent of its own words — **not** the hull of those that
   survive. That distinction took one more round: with the hull, a retraction at the **start** or
   the **end** of the utterance falls outside and survives (`[a b c]` minus `[b c]` left the
   "a"). `transcript.done` carries no declared span, so it falls back to its words — which is
   exactly what keeps it safe without knowing whether it repeats the whole session or only the
   tail.

   With that, retraction works in all three positions and the silence gap still works, with no
   heuristics. There is no residual case left.

## Review log

**Iteration 1 — codex `gpt-5.6-sol` xhigh — REWORK.** 2 P0, 7 P1, 3 P2.

| Finding | Resolution |
| --- | --- |
| P0 — the retryable `error` opens an unbounded loop | `error` → terminal. The controller bug documented separately |
| P0 — closing on the first `Final` truncates before the `done` | An explicit terminal route; **one** assembled `Final` on the way out |
| P1 — the watermark loses words and cannot take corrections | Replaced by the timeline |
| P1 — the fallback without `words` duplicates | Rule revised again in it. 2 |
| P1 — `sync.Once` does not order `Final` before `Stopped` | One owning goroutine; order corrected again in it. 2 |
| P1 — `Stop` before `Ready` loses the queue | `pendingFinalize`: flush and **then** `audio.done` |
| P1 — the flush can reorder the PCM | A single writer |
| P1 — extracting `wsstream` is premature | **Accepted**: everything in `stt/grok` |
| P1 — reconnection leaks the capture | Pre-existing; out of scope by the user's decision |
| P2 — 32 KiB read limit | 1 MiB, with a test |
| P2 — "400 frames" does not bound anything | A byte-based ring |
| P2 — the tests assert little | Complete sequences + leaks |

**Iteration 2 — codex `gpt-5.6-sol` xhigh — REWORK.** 2 new P0, 5 P1, and 5 from it. 1
marked as unresolved. All verified against the code; none dismissed.

| Finding | Resolution in v3 |
| --- | --- |
| **P0** — `[Canceled] → Final` loses the dictation on the retryable route (`Bump` invalidates the gen) | **Order corrected to `Final → [Canceled] → Stopped`.** Verified in `tracker.go:56-58` and `controller.go:286-289, 333-363` |
| **P0** — liveness under backpressure: a single channel either drops the `stop` or blocks the microphone | **Three separate paths**: a byte-based audio ring with its own mutex, a control channel that is never dropped, an idempotent `Stop` that does not block |
| P1 — per-segment replacement loses non-overlapping spans (gaps, partial overlap) | **Per-WORD timeline** with **half-open** intervals; only the words that overlap are erased |
| P1 — no string rule for the `done` without `words` is correct | **Accepted**: no comparison any more. `done.text` is ignored if there is a timeline (and logged); it is used only if the timeline is empty. **A declared limitation** |
| P1 — late frames from the pump cross the finalisation | `Stop` rejects new audio atomically before finalising, like Azure (`recognizer.go:196`) |
| P1 — `BadRequest` is semantically wrong for server errors | **Kept, with the cost declared**: it avoids an open-ended bill while the retry budget is still broken. Noted to change to `ServiceError` when it is fixed |
| P1 — the capture leak is in scope | **A conscious disagreement**: the user's decision. Grok opens no new routes towards it (hence the terminal mapping) |
| P2 — missing cases (saturation, reentrancy, wire, privacy, timeouts) | All added, plus the design of the four timeouts with their rules |
| P2 — the global goroutine count is flaky | Replaced by the provider's `WaitGroup` |
