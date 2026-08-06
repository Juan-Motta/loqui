# Where realtime STT auth actually fails — xAI, OpenAI and ElevenLabs, measured

- **Date:** 2026-08-06
- **Question:** does an invalid API key fail the WebSocket **handshake**, or after it?
- **Why it had to be answered before writing code:** the design for "Probar conexión" turned on it. If
  auth fails at the handshake, a probe can dial and close. If it fails afterwards, that same probe
  reports a broken key as good — the worst outcome the feature can produce.
- **Verdict: it differs per provider, and the majority case is the dangerous one.** Two of the three
  accept the upgrade with a garbage key.

## Method

An invalid key, over **HTTP/1.1**, with the exact handshake each provider's runtime builds. No valid
credential is needed, nothing is transcribed, and nothing is billed — the same approach that found
xAI's real behaviour in July (`2026-07-28-xai-stt-streaming.md`).

```sh
N="dGhlIHNhbXBsZSBub25jZQ=="   # any base16-of-16-bytes nonce
curl -s -i --http1.1 --max-time 20 \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: $N" \
  <the provider's auth header or subprotocol> "<its URL>"
```

> **`--http1.1` is not optional, and leaving it out invalidates the result.** curl negotiates HTTP/2
> with all three hosts, and a WebSocket upgrade is an HTTP/1.1 mechanism. Without the flag the first
> attempt here came back `405` from OpenAI and `404` from ElevenLabs — both artefacts of the protocol,
> nothing to do with the key, and both convincing enough to be written down as findings if unexamined.

Reading what arrives after a 101 needs a WebSocket frame parser, not `cat`: the payloads are length-
prefixed and the close frame carries its code in the first two bytes. A ~20-line Python parser over
curl's saved body is enough.

## What each service does

| Service | Handshake with an invalid key | The auth failure arrives as |
| --- | --- | --- |
| **xAI** `wss://api.x.ai/v1/stt` | **400 Bad Request** — `{"code":"Client specified an invalid argument","error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}` | the handshake |
| **OpenAI** `wss://api.openai.com/v1/realtime?intent=transcription` | **101 Switching Protocols** — accepted, subprotocol `realtime` negotiated | a post-upgrade event, then close **3000** `invalid_request_error.invalid_api_key` |
| **ElevenLabs** `wss://api.elevenlabs.io/v1/speech-to-text/realtime` | **101 Switching Protocols** — accepted | a post-upgrade event, then close **1000** |

OpenAI's event, verbatim except for the key:

```json
{"type":"error","event_id":"event_…","error":{"type":"invalid_request_error",
 "code":"invalid_api_key",
 "message":"Incorrect API key provided: sk-proj-********************ueba. You can find your API key at https://platform.openai.com/account/api-keys.",
 "param":null,"event_id":null}}
```

ElevenLabs':

```json
{"message_type":"auth_error","error":"You must be authenticated to use this endpoint."}
```

## Three consequences, none of them derivable from the docs

### 1. A "dial and close" probe is a false green for two of three

Any string pasted as an OpenAI or ElevenLabs key would produce a successful upgrade. A probe that
concluded from the upgrade alone would answer "connection correct" to nonsense. The only safe protocol
is: **dial → wait for the first server message → classify it**, treating silence, EOF and timeout as
failure.

### 2. A NORMAL close code does not mean success

ElevenLabs closes with **1000** — the code for a clean, ordinary closure — *after* refusing the
credential. So success cannot be inferred from a tidy shutdown; readiness has to be confirmed
positively, by name (`session_started`, `session.created`, `transcript.created`).

### 3. The error text contains key material, partially masked

OpenAI returns `sk-proj-********************ueba`: the middle is masked, **the last four characters are
not**. Anything that forwards a provider's error text to a screen or a log is forwarding key material,
and redacting "the exact secret" does not catch it — the string on the wire is not the string we hold.
For auth failures the safe rule is to use our own wording and keep the provider's prose out of the UI.

## Gaps this exposed in the ported code

Both are real today, independently of the probe work:

- **`elevenlabs.Decode` recognises an error only when the event name contains `"error"`**
  (`internal/stt/elevenlabs/wire.go:157`). Its documented event set includes `quota_exceeded`,
  `unaccepted_terms`, `rate_limited`, `queue_overflow`, `resource_exhausted` and `commit_throttled`,
  none of which contain that substring — so they fall to `Ignore`. **During a real dictation an
  out-of-credit session is therefore silently ignored and hangs until its readiness timeout**, telling
  the user nothing.
- **`openai.wireError` keeps only `message`** (`internal/stt/openai/wire.go:120-122`), discarding
  `type` and `code`. The machine-readable `invalid_api_key` measured above is thrown away, leaving only
  prose to classify from — which is the failure mode `internal/session/policy.go:36` already documents.

## What is still NOT measured

**Every green path.** No `session.created`, `session_started` or `transcript.created` has been observed
from any of these three services, because there is no credential for any of them on this machine
(confirmed with the project's owner, 2026-08-06). The readiness event names come from each provider's
docs and from our own ported runtime, not from observation.

The failure mode if a name is wrong is worth stating: the probe would **wait and time out**, reporting a
service problem. Visible and safe — never a false green. Whoever obtains a credential for any of the
three should run its green path and record it, exactly as Azure's was closed in
`docs/e2e/reports/2026-08-06-keys-in-a-file.md`.

## Reusable lessons

1. **Ask "where does auth fail" before designing anything that tests a credential.** It is one curl per
   service and it decided the whole shape of this feature.
2. **Force HTTP/1.1 when probing a WebSocket endpoint**, or measure the wrong thing convincingly.
3. **Read the frames after a 101.** The interesting answer is often there, not in the status line.
4. **Check the constant before believing a service rejected your request.** A rejection here that looked
   like a shipped bug — ElevenLabs refusing `model_id=scribe_v1` — came from a hand-typed curl; the code
   sends `scribe_v2_realtime`, which is supported. The service was right and the test was wrong.
