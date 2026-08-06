# Do not port an STT provider's event parsing verbatim

- **Date:** 2026-07-28
- **Found while:** porting the Grok (xAI) provider from Electron to Go
- **Applies to:** the two remaining providers (**elevenlabs**, **openai**), and to any future
  review of Azure

## Symptom

None visible. The Electron code (`../loqui/src/shared/grokStt.ts`) carried a comment saying
*"Validated against docs.x.ai (Speech to Text, 2026)"*, had tests, and was in production. Porting it
looked mechanical.

## Root cause

Two distinct bugs, both of **silent text loss**, and neither detectable without reading the service's
schema:

1. **The final is not distinguished by the event name.** `parseGrokEvent` mapped every
   `transcript.partial` to "interim" and took the final only from `transcript.done`. The schema
   distinguishes with two flags (`is_final`, `speech_final`). Ignoring them discards the utterance
   finals of a multi-sentence session.
2. **The official `transcript.done` example carries `text: ""`** after 6.43 s of audio. If the only
   final comes from there, the dictation is delivered **empty**.

And a third, from the service rather than the code: **xAI's docs lie about the auth failure.** Their
table says `401`; the real service returns **400** with
`{"error":"Incorrect API key provided..."}`.

## Fix

- Read the **machine-readable schema**, not the rendered page: xAI's WebSocket endpoints publish it
  at the root (`docs.x.ai/stt-streaming.ws.json`). Appending `.md` to any `docs.x.ai` path returns
  the source markdown. The page contradicts itself in at least two places where the schema is clear.
- The provider **assembles** the transcript and emits **one** final on the way out, instead of
  emitting progressively: the session controller only **accumulates**
  (`internal/session/controller.go:293`), so a provider that emits in chunks cannot withdraw
  anything, and these services do retract.
- Two replacement rules according to what the protocol says about each event — a chunk final is
  incremental, an utterance final (`speech_final`) is authoritative for its declared span.
  See `internal/stt/grok/timeline.go`.
- Check the auth failure **against the real service** with an invalid key. It costs a minute, it
  costs no money, and it was the only part of the cloud route verifiable without an account.

## How it was verified

`internal/stt/grok` with 71 tests (`-race`), against a **real** local WebSocket server
(`httptest` + `websocket.Accept`, not a library mock), plus the handshake against `api.x.ai`.
Evidence in `docs/e2e/reports/2026-07-28-grok-stt.md`; the API verification, with what the docs do
**not** say, in `docs/research/2026-07-28-xai-stt-streaming.md`.

## The reusable part

1. **A comment saying "validated against the docs" is not evidence.** That comment was correct *and*
   the code was wrong: it described the transport, not the semantics of the events.
2. **Look for the machine-readable schema before believing the prose.** All three findings came from
   there or from the real service, none from the guide.
3. **In a dictation provider, the expensive bug is the silent one**: text lost or duplicated, not an
   exception. The tests that matter are the ones over event sequences with overlap, retraction and
   empty terminators — not "does this JSON parse?".
4. **Four cross-engine review rounds each found something real, all in the same place** (the
   transcript assembly). When one component concentrates the findings, keep digging there instead of
   declaring it done.
5. **A case that looks unsolvable may have a signal in the protocol.** Word retraction was written
   off as "best effort" until the review pointed out that `speech_final` is exactly that signal, and
   it was being discarded in the decode.
