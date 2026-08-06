# xAI (Grok) Speech-to-Text streaming — API verification

- **Date:** 2026-07-28
- **For:** the `internal/stt/grok` provider, phase 3 of the port.
- **Reason:** the Electron code (`../loqui/src/shared/grokStt.ts`) says "validated
  against docs.x.ai (2026)". Before porting it, that has to be confirmed as still current — and
  it turned out **one of the six assumptions was wrong**, with text loss as the consequence.

## Authoritative source

`https://docs.x.ai/stt-streaming.ws.json` — the endpoint's **machine-readable schema**.
It is more precise than the rendered page, which in at least one place contradicts itself
(see "error" below). Two tricks worth remembering:

- appending `.md` to any `docs.x.ai` path returns the source markdown;
- WebSocket endpoints publish their schema at the root (`/stt-streaming.ws.json`,
  `/tts-streaming.ws.json`).

## Electron's six assumptions

| # | Assumption | Verdict |
| --- | --- | --- |
| 1 | Endpoint `wss://api.x.ai/v1/stt` | ✅ confirmed |
| 2 | Configuration via query params, no setup message | ✅ confirmed, verbatim: "Configuration is done via URL query parameters — no setup message required." All four params are **optional** (`sample_rate` 16000, `encoding` pcm, `interim_results` false, `language` "") |
| 3 | Auth via `Authorization: Bearer <key>` header in the handshake | ✅ confirmed; `required: true` in the schema. There is no auth via query param or subprotocol |
| 4 | Raw binary PCM16 LE frames, no base64; 16 kHz mono native | ✅ confirmed, verbatim: "Audio is sent as raw binary frames (no base64 encoding)". 16 kHz is "the model's native rate and avoids resampling on the server" |
| 5 | `{"type":"audio.done"}` to close, `{"type":"Finalize"}` to force a final | ✅ confirmed. `finalize` accepts both capitalisations (`enum: ["finalize","Finalize"]`); `audio.done` is `const`, only that exact string |
| 6 | Events `transcript.created` / `.partial` / `.done` / `error` | ⚠️ **almost** — see the correction, it is the important one |

## The corrections that matter

### 1. The final is NOT distinguished by the event name (the bug that was almost ported)

`transcript.partial` carries `type, text, words, is_final, speech_final, start, duration`.
The three states:

| `is_final` | `speech_final` | meaning |
| --- | --- | --- |
| `false` | — | interim hypothesis |
| `true` | `false` | chunk final (~3 s) |
| `true` | `true` | **utterance final** |

Electron's `parseGrokEvent` maps every `transcript.partial` to `{kind:"partial"}` and only
`transcript.done` to a final. In a multi-sentence session that **discards the utterance
finals**. That parsing must not be ported as-is.

### 2. The `error` payload is FLAT

`{"type":"error","message":"..."}`, required fields `["type","message"]`. The nested
`error.message` shape that the Electron code looks for first **does not exist** — that one belongs
to `wss://api.x.ai/v1/responses` (the Responses API), a different endpoint.

⚠️ **Do not import the 25-minute limit** nor the `{"type":"error","status":400,"error":{...}}`
envelope: they belong to the Responses API. Context7 returns them glued to the STT docs, which
invites exactly that confusion.

### 3. The documentation contradicts itself about whether `error` is terminal

- The guide says: "Connection stays open."
- The reference and the schema say: most errors (pipeline failures, stream timeouts) **close** the
  connection; only parse errors on a client message leave it alive.

The reference wins. Treat `error` as terminal.

### 4. Other details

- `transcript.created` carries a required `id` (session UUID) — useful for correlating logs.
- `transcript.done` carries `type, text, words, duration`, and "Connection closes after this event".
- **There is no `model`**: STT is a single model, billed by the hour ($0.10/h REST, $0.20/h
  streaming). Electron's assumption of not sending `model` is correct. (The Voice Agent API,
  `wss://api.x.ai/v1/realtime`, does take `model` — do not confuse the two.)
- **There is no detected-language field** in streaming: neither in `.partial` nor in `.done`. In
  REST it exists but is disabled ("Currently empty — language detection is not yet enabled").
  ⇒ our `Event.Language` stays empty for Grok.
- **`language` is not a recognition hint, it is a formatting one.** Verbatim: "The model
  transcribes speech in any of these languages regardless of the `language` parameter —
  setting it enables formatting of numbers, currencies, and units into their written form."
  25 codes; almost all ISO-639-1 but `fil` has three letters ⇒ treat as a free string,
  do not validate as strict ISO-639-1.
- **Frame size / maximum session duration: not verifiable** — nothing documented for
  streaming (the 500 MB limit is REST-only). The only advice: "Send 100 ms audio chunks
  (3,200 bytes at 16 kHz PCM16)". "Stream timeouts" is mentioned as a cause of error with no
  threshold.
- **Authentication failure: not fully verifiable.** xAI's error table lists
  `401 Unauthorized — API key is missing or invalid`, but it is organised by HTTP code,
  which means it describes the handshake. **There are no documented close codes** for `/v1/stt`.
  Since auth travels in a header, a bad key almost certainly breaks the upgrade with a 401 before
  any frame — but the docs do not say so. Check empirically.

## Second round: what text does each event carry?

The decisive question for the mapping, because the controller **accumulates** every `Final`
(`internal/session/controller.go:293`): if two events carry overlapping text, it gets appended
twice.

**The docs do not say so explicitly for any event — "not verifiable".** But the schema's own
example payloads settle the practical part.

### `transcript.done.text` — can arrive EMPTY

The field's description is useless (`"Final transcript text."`), and the sources contradict each
other:

- In favour of "the tail only": the guide says `audio.done` asks the server to "flush the
  **remaining** transcript"; the reference says it emits the final events first and *then* the
  `transcript.done` (redundant if it repeated everything).
- In favour of "the whole session": both official code samples print it as
  `Full transcript: {event['text']}`, and the same-named field in REST *is* documented as
  "Full transcript text".

**The decisive artefact** — the official `transcript.done` example:

```json
{ "type": "transcript.done", "text": "", "words": [], "duration": 6.43 }
```

`text` empty after **6.43 seconds** of audio. A full-session transcript cannot be empty; a tail
flush can.

> **Consequence for the port:** Electron's mapping (finals **only** from `transcript.done`) can
> deliver an **empty string** and lose the entire dictation. This is not an optional improvement:
> it is a bug.

### `transcript.partial.text` — does not accumulate across the session

- `start` is **session-relative**: "seconds from stream start". `duration` is the window this
  particular result covers. The `words` carry `start`/`end` that are also session-relative.
- Strong negative evidence against the cumulative reading: these same docs say "cumulative" when
  they mean it — the Voice Agent API event
  `conversation.item.input_audio_transcription.updated` is documented as "the
  **cumulative transcript so far** … this is different from a transcript delta". The STT streaming
  docs never use that word. Same authors, same family of pages: the omission means something.
- **Per-utterance reset: not verifiable.** There is **no** multi-utterance example anywhere in the
  docs; every valued `start` is `0.0`.

### The real danger: overlap WITHIN an utterance (unresolved)

- The guide's state table says `is_final=true, speech_final=true` is the
  "**complete stitched utterance**" — meaning it **re-emits** the text already delivered by that
  utterance's chunk finals.
- The schema says `text` is "Transcript text for this **chunk**" — meaning incremental.

If "stitched" is the correct reading and you accumulate every `is_final=true`, every utterance
longer than ~3 s is duplicated.

### `transcript.done` is terminal, and a final is not guaranteed to precede it

- It only arrives after `audio.done`, and "the connection closes after this event". Confirmed in
  all three sources.
- **`finalize` does NOT produce it**: "The session stays open so you can continue streaming audio" —
  `finalize` produces a `transcript.partial` with `speech_final`, never a `done`. ⇒ for
  push-to-talk, `audio.done` alone is enough; sending `Finalize` first is redundant.
- An `error` can close the connection **without** any `transcript.done` ⇒ do not wait for the
  `done` as the only terminator; the timeout is needed.
- With `interim_results=false` and audio shorter than the ~3 s window, the only event with text
  could **be** the `transcript.done`. Both extremes have to be supported.

### The most complete sequence that exists

There is no real multi-event capture in the docs. These are the only payloads with values,
assembled from three different sections:

```json
{"type":"transcript.created","id":"83f2f6fd-1cd1-4747-bc52-cebddc961c32"}

{"type":"transcript.partial","text":"The balance is $167,983.15.",
 "words":[{"text":"The","start":0.24,"end":0.48,"confidence":0.95},
          {"text":"balance","start":0.48,"end":0.96,"confidence":0.92},
          {"text":"is","start":0.96,"end":1.12,"confidence":0.98},
          {"text":"$167,983.15.","start":1.12,"end":3.2,"confidence":0.89}],
 "is_final":true,"speech_final":false,"start":0.0,"duration":3.2}

{"type":"transcript.partial","text":"I will buy two of those, please.","words":[...],
 "is_final":true,"speech_final":true,"start":0.0,"duration":2.4,
 "end_of_turn_confidence":0.983}

{"type":"transcript.done","text":"","words":[],"duration":6.43}
```

The official Python SDK **has no** STT streaming helper, so there is no reference implementation
to copy the semantics from.

### The way out: a temporal watermark (correct under ALL interpretations)

No interpretation has to be chosen. `start`/`duration` are session-relative and the `words`
carry absolute times ⇒ that gives a monotonic watermark:

- `committedUpTo := 0.0`
- Events with `is_final=false` **do not enter** the buffer (live preview only).
- On each `transcript.partial` with `is_final=true` (chunk **or** utterance):
  - `start+duration <= committedUpTo` → total overlap, **discard** (this is what kills the double
    append of the "stitched" utterance);
  - `start < committedUpTo` → partial overlap, keep only the `words` whose
    `start >= committedUpTo`;
  - otherwise → all of it;
  - and then `committedUpTo = max(committedUpTo, start+duration)`.
- On `transcript.done`: the same rule by word times. If it were the full session, everything
  before the watermark is discarded and only the tail enters; if it were the tail only, it enters
  normally; if it arrives empty, nothing enters. **All three interpretations land correctly.**

Two constraints to encode: `transcript.done` does **not** carry a top-level `start` (its
requireds are `type, text, words, duration`), so it is deduplicated by word times; and
if `words` arrived empty with a non-empty `text`, it falls back to a string comparison — if the
buffer is a prefix/substring of `done.text`, replace the buffer with `done.text`, otherwise
append. `word.confidence` is "Omitted when 0" ⇒ a pointer/optional, not a required float.

### The five-minute experiment that settles all three questions

Dictate two sentences with a clear pause, `interim_results=true`, log every event verbatim, and
look at: (a) is the `start` of the `speech_final=true` event equal to that of the first chunk
final of that utterance (stitched) or does it continue from its end (incremental)?; (b) does the
second utterance's `start` keep rising (session-relative, as documented) or reset to ~0?;
(c) does `done.text` arrive empty, with the last utterance, or with everything? The watermark
logic is correct either way, so this confirms rather than unblocks.

## Verified against the real service (2026-07-28)

The only thing checkable without a paid account: **the handshake with an invalid key**. And
**the docs are wrong**.

xAI's error table says `401 Unauthorized — API key is missing or invalid`. The reality:

```
$ curl -i -H "Authorization: Bearer xai-...-000000" \
    -H "Connection: Upgrade" -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
    "https://api.x.ai/v1/stt?encoding=pcm&sample_rate=16000&interim_results=true&language=es"

HTTP/2 400
content-type: application/json

{"code":"Client specified an invalid argument",
 "error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}
```

**HTTP 400, not 401.** Consequences for the client:

1. Mapping by status alone would give `BadRequest` with the message "xAI rechazó la petición
   (status 400)" — which sends the user off to audit their configuration when what is actually
   wrong is the key. The **body has to be read** to tell them apart (`internal/stt/grok/errors.go`,
   `handshakeFailure`).
2. That does **not** reintroduce the bug of classifying by prose: the retry decision comes from the
   **code**, and both `AuthenticationFailure` and `BadRequest` are non-retryable, so reading the
   body changes **the message**, never the behaviour. That is the invariant to keep.
3. It is still **unverified** whether a post-connection `error` can carry an auth failure. With a
   bad key the socket never opens, so the assumption "auth fails in the handshake" is confirmed;
   what cannot be confirmed is what else travels through the `error` event.

One side result: the response carries a `code` field shaped like a constant
(`"Client specified an invalid argument"`) that is **not** documented anywhere in
`docs.x.ai`. It should not be depended on.

## Capabilities Electron was not using

- `smart_turn` (ML end-of-turn detection) + `smart_turn_timeout`. For dictation it would avoid
  cutting the user off mid-number. Not used now; noted.
- `keyterm` (repeatable, max 100 × 50 chars) to bias proper nouns.
- `vad_threshold`: the streaming default is `0.08`, REST's is `0.5`.

## What it implies for the Go client

1. The transport ports intact: endpoint, query params, auth header, binary frames.
2. Branch on `is_final`/`speech_final`, **not** on the event name.
3. Assume `error` kills the socket.
4. The only reliable structured code comes from the handshake: `coder/websocket` returns the
   `*http.Response` in `Dial`'s error, so the 401/429 comes from there. After connecting there is
   only a prose `message` ⇒ a `ServiceError` bucket rather than guessing.

## Sources

- https://docs.x.ai/stt-streaming.ws.json (schema, authoritative)
- https://docs.x.ai/developers/model-capabilities/audio/speech-to-text (guide)
- https://docs.x.ai/developers/rest-api-reference/inference/voice (reference)
- https://docs.x.ai/developers/models (pricing)
- https://docs.x.ai/developers/rate-limits
- https://docs.x.ai/developers/advanced-api-usage/websocket-mode — **Responses API, NOT STT**
